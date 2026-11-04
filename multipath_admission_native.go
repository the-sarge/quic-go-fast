//go:build darwin || linux || freebsd

package quic

import (
	"context"
	"fmt"
	"net"
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
