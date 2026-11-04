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
	request := &pathOpenRequest{ctx: ctx, transport: target, addr: addr, done: make(chan error, 1)}
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

// cutTuple starts a new generation on a member. R13 owns the cutover itself.
func (p *pathCreditRegistry) cutTuple(m *pathMember) {
	m.tuple++
	m.attempts = 0
}

// setLocalUsePrivate is the private driver behind R16's SetLocalUseV1. It overrides
// the demotion of a recovered path.
func (p *pathCreditRegistry) setLocalUsePrivate(id protocol.PathID, backup bool, now monotime.Time) error {
	if err := p.validateID(id); err != nil {
		return err
	}
	m := p.members[id]
	if m == nil || !m.live || p.terminals[id] != nil {
		return ErrPathClosed
	}
	m.backup = backup
	p.reselect(now)
	p.conn.scheduleSending()
	return nil
}
