//go:build darwin || linux || freebsd

package quic

import (
	"context"
	"fmt"
	"net"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

// openPathPrivate is the private driver for client initiation, behind OpenPathV1. The
// returned context error says nothing about wire retirement, which continues on the
// connection. A fixture may pass a nil target to use the origin; the public method can't.
func (c *Conn) openPathPrivate(ctx context.Context, target *Transport, addr net.Addr) (protocol.PathID, error) {
	if target != nil {
		if err := c.prepareTarget(target); err != nil {
			return 0, err
		}
	}
	request := &pathOpenRequest{ctx: ctx, transport: target, addr: addr, done: make(chan error, 1)}
	if err := c.awaitPathOperation(ctx, request); err != nil {
		return 0, err
	}
	return request.id, nil
}

// migratePathPrivate is the private driver behind MigrateV1. It moves one path
// to target under the same wire ID, and returns success only after cutover.
func (c *Conn) migratePathPrivate(ctx context.Context, id protocol.PathID, target *Transport) error {
	if err := c.prepareTarget(target); err != nil {
		return err
	}
	return c.awaitPathOperation(ctx, &pathOpenRequest{ctx: ctx, transport: target, migrate: true, id: id, done: make(chan error, 1)})
}

// waitTransportDetachedPrivate is the private driver behind WaitTransportDetachedV1. It observes publication and changes nothing. A connection
// that ends first reports its cause, since that is what a winning close looks like.
func (c *Conn) waitTransportDetachedPrivate(ctx context.Context, t *Transport) error {
	if t == nil {
		return fmt.Errorf("%w: a nil transport", ErrMultipathInvalidConfigV1)
	}
	attachment := c.pathCredit.detachmentOf(t)
	if attachment == nil {
		return nil
	}
	select {
	case <-attachment.detached:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.ctx.Done():
		return context.Cause(c.ctx)
	}
}

// detachmentOf is the waiter's view: nil means no attachment, so already detached.
func (p *pathCreditRegistry) detachmentOf(t *Transport) *pathAttachment {
	p.attachMu.Lock()
	defer p.attachMu.Unlock()
	return p.attachments[p.attachmentKey(t)]
}

// prepareTarget runs on the caller's goroutine, because initialization can touch the
// socket. The effective length comes from configuration, so a mismatch refuses before
// the target starts, and initialization selects that same length.
func (c *Conn) prepareTarget(target *Transport) error {
	if target == nil {
		return fmt.Errorf("%w: a nil target transport", ErrMultipathInvalidConfigV1)
	}
	if nativeTransportEndpoint(target) == nil {
		return fmt.Errorf("%w: the target has no native grant", ErrMultipathNotConfiguredV1)
	}
	if err := target.nativeIOConflict(); err != nil {
		return err
	}
	if length := target.effectiveConnIDLen(); length != c.srcConnIDLen {
		return fmt.Errorf("%w: the target routes %d-byte connection IDs, not %d", ErrMultipathInvalidConfigV1, length, c.srcConnIDLen)
	}
	return target.init(false)
}

// effectiveConnIDLen is the routing length init(false) selects.
func (t *Transport) effectiveConnIDLen() int {
	if t.ConnectionIDGenerator != nil {
		return t.ConnectionIDGenerator.ConnectionIDLen()
	}
	if t.ConnectionIDLength == 0 {
		return protocol.DefaultConnectionIDLength
	}
	return t.ConnectionIDLength
}

// awaitPathOperation runs the connection's one local path operation.
func (c *Conn) awaitPathOperation(ctx context.Context, request *pathOpenRequest) error {
	if !c.pathOpen.CompareAndSwap(nil, request) {
		return fmt.Errorf("%w: a local path operation is in progress", ErrMultipathBackpressureV1)
	}
	c.scheduleSending()
	select {
	case err := <-request.done:
		return err
	case <-ctx.Done():
		if !request.outcome.CompareAndSwap(pathOpenPending, pathOpenCanceled) {
			select { // the connection decided first, and publishes at once
			case err := <-request.done:
				return err
			case <-c.ctx.Done():
				c.pathOpen.CompareAndSwap(request, nil)
				return context.Cause(c.ctx)
			}
		}
		c.scheduleSending()
		return ctx.Err()
	case <-c.ctx.Done():
		c.pathOpen.CompareAndSwap(request, nil) // installed too late for the connection to end it
		return context.Cause(c.ctx)
	}
}

// zeroEpoch fences ordinary path-zero requests, once admission can retire path zero.
func (p *pathCreditRegistry) zeroEpoch() *responseEpoch {
	if zero := p.members[0]; zero != nil {
		return &zero.established.probeEpoch
	}
	return nil
}

// sendFailed applies a final destination error to the tuple it was sent on. A late
// result from an old tuple can't demote its replacement.
func (p *pathCreditRegistry) sendFailed(id protocol.PathID, tuple uint64, now monotime.Time) {
	m := p.members[id]
	if m == nil || !m.live || m.suspect || p.terminals[id] != nil || tuple != m.tuple {
		return
	}
	p.markSuspect(m, now)
}

// cutTuple starts a new generation on a member. Only policy fixtures call it, since a
// real cutover goes through commitTuple.
func (p *pathCreditRegistry) cutTuple(m *pathMember) {
	m.tuple++
	m.attempts = 0
}

// noteOutcome records the local result of one submission on its current tuple.
func (p *pathCreditRegistry) noteOutcome(id protocol.PathID, tuple uint64, accepted, uncertain, datagrams int) {
	m := p.members[id]
	if m == nil || m.tuple != tuple {
		return
	}
	switch {
	case uncertain > 0:
		m.lastOutcome = "uncertain"
	case accepted < datagrams:
		m.lastOutcome = "unsubmitted"
	default:
		m.lastOutcome = "accepted"
	}
}

func (f *pathFailures) cause() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.first
}

// failed is the one survival decision, on each pass after a failure: the connection
// ends with the first cause once no path could even carry its close. Without a failure,
// the same empty list is a path waiting for a CID, and stays non-fatal.
func (p *pathCreditRegistry) failed() error {
	cause := p.failures.cause()
	if cause == nil {
		return nil
	}
	var storage [maxPathHistoryUnits + 1]*pathMember
	if len(p.closeContexts(storage[:0])) != 0 {
		return nil
	}
	return cause
}

// endpointFaulted reads what a path on this transport stays a live member through: its
// endpoint's fault or its read failure, and not an explicit close.
func endpointFaulted(t *Transport) bool {
	endpoint := nativeTransportEndpoint(t)
	if endpoint == nil {
		return false
	}
	if _, faulted := endpoint.faultObservation(); faulted {
		return true
	}
	t.mutex.Lock()
	defer t.mutex.Unlock()
	return t.readFailed
}
