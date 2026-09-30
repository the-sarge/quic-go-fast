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

type receivePhaseState uint8

const (
	receivePhaseClosed  receivePhaseState = iota
	receivePhaseOpening                   // requested, not yet applied by the loop
	receivePhaseOpened
)

// receivePhase is the single owner of a connection's receive epoch. The
// connection loop applies the transition between packets, so every frame is
// admitted wholly before or wholly after it; admitting is read only on that
// loop. The mutex orders a request, its withdrawal, its application and the
// loop's termination. Unconfigured connections carry a nil phase and always
// admit.
type receivePhase struct {
	mu        sync.Mutex
	state     receivePhaseState
	pending   context.Context // the opening request's context
	admitting bool            // loop-confined
	requested chan struct{}   // wakes the loop; capacity one
	opened    chan struct{}   // closed when the loop applies the transition
	// stopped closes when the connection loop ends. It is fork-owned: an
	// application-cancelled context does not end a connection.
	stopped chan struct{}
	stopErr error
}

func newReceivePhase() *receivePhase {
	return &receivePhase{
		requested: make(chan struct{}, 1),
		opened:    make(chan struct{}),
		stopped:   make(chan struct{}),
	}
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
	if p.state != receivePhaseOpening {
		return // withdrawn before the loop observed it
	}
	if p.pending.Err() != nil {
		p.state = receivePhaseClosed
		p.pending = nil
		return
	}
	p.state = receivePhaseOpened
	p.pending = nil
	p.admitting = true
	invalidate()
	close(p.opened)
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
	if p.state == receivePhaseOpening {
		p.state = receivePhaseClosed
		p.pending = nil
	}
	close(p.stopped)
}

func (p *receivePhase) request(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch {
	case p.state == receivePhaseOpened:
		return errReceivePhaseOpen
	case p.stopErr != nil:
		return p.stopErr
	case p.state == receivePhaseOpening:
		return errReceivePhaseOpening
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	p.state = receivePhaseOpening
	p.pending = ctx
	select {
	case p.requested <- struct{}{}:
	default:
	}
	return nil
}

// withdraw ends a request the loop has not applied. It reports whether the
// phase opened anyway.
func (p *receivePhase) withdraw() (opened bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state == receivePhaseOpened {
		return true
	}
	p.state = receivePhaseClosed
	p.pending = nil
	return false
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
	if err := p.request(ctx); err != nil {
		return 0, err
	}
	var err error
	select {
	case <-p.opened:
		return openReceivePhaseNumber, nil
	case <-ctx.Done():
		err = ctx.Err()
	case <-p.stopped:
		err = p.stopErr
	}
	if p.withdraw() {
		return openReceivePhaseNumber, nil
	}
	return 0, err
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
