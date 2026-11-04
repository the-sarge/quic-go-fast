//go:build darwin || linux || freebsd

package quic

import (
	"context"
	"fmt"
	"net"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

// openPathPrivate is the private driver for client initiation. The returned
// context error says nothing about wire retirement, which continues on the
// connection. Public activation belongs to R16 and R19.
func (c *Conn) openPathPrivate(ctx context.Context, target *Transport, addr net.Addr) error {
	if target != nil {
		if err := c.prepareTarget(target); err != nil {
			return err
		}
	}
	return c.awaitPathOperation(ctx, &pathOpenRequest{ctx: ctx, transport: target, addr: addr, done: make(chan error, 1)})
}

// migratePathPrivate is the private driver behind R16's MigrateV1. It moves one path
// to target under the same wire ID, and returns success only after cutover.
func (c *Conn) migratePathPrivate(ctx context.Context, id protocol.PathID, target *Transport) error {
	if err := c.prepareTarget(target); err != nil {
		return err
	}
	return c.awaitPathOperation(ctx, &pathOpenRequest{ctx: ctx, transport: target, migrate: true, id: id, done: make(chan error, 1)})
}

// waitTransportDetachedPrivate is the private driver behind R16's
// WaitTransportDetachedV1. It observes publication and changes nothing. A connection
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
		return context.Cause(ctx)
	case <-c.ctx.Done():
		return context.Cause(c.ctx)
	}
}

// prepareTarget runs on the caller's goroutine, because initialization can touch the
// socket. The effective length comes from configuration, so a mismatch refuses before
// the target starts, and initialization selects that same length.
func (c *Conn) prepareTarget(target *Transport) error {
	if target == nil {
		return fmt.Errorf("%w: a nil target transport", ErrMultipathInvalidConfigV1)
	}
	if nativeTransportEndpoint(target) == nil {
		return fmt.Errorf("%w: the target has no native grant", ErrMultipathUnsupportedV1)
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
				return context.Cause(c.ctx)
			}
		}
		c.scheduleSending()
		return context.Cause(ctx)
	case <-c.ctx.Done():
		return context.Cause(c.ctx)
	}
}

// useLocalPrivate is setLocalUsePrivate for a caller off the connection goroutine.
// It takes the one local operation slot, so it also waits for an open to finish.
func (c *Conn) useLocalPrivate(ctx context.Context, id protocol.PathID, backup bool) error {
	request := &pathOpenRequest{ctx: ctx, use: &pathLocalUse{id: id, backup: backup}, done: make(chan error, 1)}
	if !c.pathOpen.CompareAndSwap(nil, request) {
		return fmt.Errorf("%w: a local path operation is in progress", ErrMultipathBackpressureV1)
	}
	c.scheduleSending()
	select {
	case err := <-request.done:
		return err
	case <-c.ctx.Done():
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
