package quic

import (
	"context"
	"errors"
	"sync"
)

var (
	errNoReceivePhase      = errors.New("quic: connection has no receive phase")
	errReceivePhaseOpen    = errors.New("quic: receive phase already open")
	errReceivePhaseOpening = errors.New("quic: receive phase open already in progress")
	errReceivePhaseStopped = errors.New("quic: connection closed")
)

// The closed phase is numbered zero; opening moves to the next phase.
const openReceivePhaseNumber uint64 = 1

// receivePhase is the single owner of a connection's receive epoch. The
// connection loop applies the transition between packets, so every frame is
// admitted wholly before or wholly after it; admitting is read only on that
// loop. The mutex orders publication, withdrawal, the loop's commit and the
// loop's termination. Each request carries its own outcome, settled exactly
// once, so a stale caller can neither withdraw nor claim a later request.
// Unconfigured connections carry a nil phase and always admit.
type receivePhase struct {
	mu        sync.Mutex
	opened    bool
	pending   *phaseRequest
	stopErr   error         // set when the connection loop ends
	admitting bool          // loop-confined
	requested chan struct{} // wakes the loop; capacity one
}

// phaseRequest is one published open request.
type phaseRequest struct {
	ctx    context.Context
	done   chan struct{} // closed when settled
	opened bool
	err    error
}

// settle records the request's outcome; the caller holds the phase mutex.
func (r *phaseRequest) settle(opened bool, err error) {
	r.opened = opened
	r.err = err
	close(r.done)
}

func newReceivePhase() *receivePhase {
	return &receivePhase{requested: make(chan struct{}, 1)}
}

// admitsApplication reports on the connection loop whether application
// payloads are eligible for delivery.
func (p *receivePhase) admitsApplication() bool {
	return p == nil || p.admitting
}

// requests is the loop's wakeup channel; nil for unconfigured connections.
func (p *receivePhase) requests() <-chan struct{} {
	if p == nil {
		return nil
	}
	return p.requested
}

// apply runs on the connection loop between packets. A request whose caller
// canceled before this commit is rejected here, whichever side observes the
// cancellation first.
func (p *receivePhase) apply(invalidate func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	req := p.pending
	if req == nil {
		return // withdrawn before the loop observed it
	}
	p.pending = nil
	if err := req.ctx.Err(); err != nil {
		req.settle(false, err)
		return
	}
	p.opened = true
	p.admitting = true
	invalidate()
	req.settle(true, nil)
}

// stop runs when the connection loop ends; no transition can follow it.
func (p *receivePhase) stop(err error) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err == nil {
		err = errReceivePhaseStopped
	}
	p.stopErr = err
	if p.pending != nil {
		p.pending.settle(false, err)
		p.pending = nil
	}
}

func (p *receivePhase) request(ctx context.Context) (*phaseRequest, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch {
	case p.opened:
		return nil, errReceivePhaseOpen
	case p.stopErr != nil:
		return nil, p.stopErr
	case p.pending != nil:
		return nil, errReceivePhaseOpening
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	req := &phaseRequest{ctx: ctx, done: make(chan struct{})}
	p.pending = req
	select {
	case p.requested <- struct{}{}:
	default:
	}
	return req, nil
}

// withdraw settles req as canceled if the loop has not settled it yet. It
// never affects another request.
func (p *receivePhase) withdraw(req *phaseRequest, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pending == req {
		p.pending = nil
		req.settle(false, err)
	}
}

// ConfigureReceivePhasesV1 selects whether connections created by this
// transport start in a closed receive phase. Call it once, before any
// operation that initializes the transport. Transports without it, or
// configured with false, keep ordinary behavior.
//
// A connection in the closed phase discards received DATAGRAM payloads before
// they reach the receive queue; the packets carrying them are processed and
// acknowledged as usual. OpenReceivePhaseV1 opens the phase. Receive phases
// govern DATAGRAM delivery only; stream data is unaffected.
func (t *Transport) ConfigureReceivePhasesV1(closed bool) error {
	t.packetIO.mutex.Lock()
	defer t.packetIO.mutex.Unlock()
	if t.packetIO.started {
		return errors.New("quic: receive phase configuration after initialization")
	}
	if t.receivePhasesConfigured {
		return errors.New("quic: receive phases already configured")
	}
	t.receivePhasesConfigured = true
	t.receivePhases = closed
	return nil
}

// OpenReceivePhaseV1 opens a connection's closed receive phase and returns
// the opened phase number. The connection loop performs the transition
// between packets: DATAGRAM payloads it processed earlier are never
// delivered, including any still queued, and those it processes later are
// delivered as usual. A payload's phase is fixed when the loop admits it, not
// when the application receives it.
//
// It fails for a connection without a configured phase, a phase already open
// or being opened by another call, or a connection whose loop has ended, in
// which case it returns the connection's close error. If ctx is done before
// the loop performs the transition, the phase stays closed and a later call
// may retry.
func (c *Conn) OpenReceivePhaseV1(ctx context.Context) (uint64, error) {
	p := c.receivePhase
	if p == nil {
		return 0, errNoReceivePhase
	}
	req, err := p.request(ctx)
	if err != nil {
		return 0, err
	}
	select {
	case <-req.done:
	case <-ctx.Done():
		p.withdraw(req, ctx.Err())
		<-req.done
	}
	if !req.opened {
		return 0, req.err
	}
	return openReceivePhaseNumber, nil
}

// installReceivePhase runs after construction and before the connection
// loop starts or receives a packet.
func (c *Conn) installReceivePhase(closed bool) {
	if closed {
		c.receivePhase = newReceivePhase()
	}
}

// openReceivePhase applies a pending open request on the connection loop.
func (c *Conn) openReceivePhase() {
	c.receivePhase.apply(c.datagramQueue.invalidateReceived)
}
