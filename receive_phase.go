package quic

import (
	"context"
	"errors"
	"sync"
)

var (
	errNoReceivePhase   = errors.New("quic: connection has no receive phase")
	errReceivePhaseOpen = errors.New("quic: receive phase already open")
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
// loop. The mutex orders a request, its withdrawal and its application.
// Unconfigured connections carry a nil phase and always admit.
type receivePhase struct {
	mu        sync.Mutex
	state     receivePhaseState
	admitting bool          // loop-confined
	requested chan struct{} // wakes the loop; capacity one
	opened    chan struct{} // closed when the loop applies the transition
}

func newReceivePhase() *receivePhase {
	return &receivePhase{
		requested: make(chan struct{}, 1),
		opened:    make(chan struct{}),
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

// apply runs on the connection loop between packets.
func (p *receivePhase) apply(invalidate func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state != receivePhaseOpening {
		return // withdrawn before the loop observed it
	}
	p.state = receivePhaseOpened
	p.admitting = true
	invalidate()
	close(p.opened)
}

func (p *receivePhase) request() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state != receivePhaseClosed {
		return errReceivePhaseOpen
	}
	p.state = receivePhaseOpening
	select {
	case p.requested <- struct{}{}:
	default:
	}
	return nil
}

// withdraw cancels a request the loop has not applied. It reports whether
// the phase opened anyway.
func (p *receivePhase) withdraw() (opened bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state == receivePhaseOpened {
		return true
	}
	p.state = receivePhaseClosed
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
// It fails for a connection without a configured phase, a phase already
// open, or a connection that closed first. If ctx ends before the loop
// performs the transition, the request is withdrawn and the phase stays
// closed; a later call may retry.
func (c *Conn) OpenReceivePhaseV1(ctx context.Context) (uint64, error) {
	p := c.receivePhase
	if p == nil {
		return 0, errNoReceivePhase
	}
	if err := p.request(); err != nil {
		return 0, err
	}
	var err error
	select {
	case <-p.opened:
		return openReceivePhaseNumber, nil
	case <-ctx.Done():
		err = ctx.Err()
	case <-c.ctx.Done():
		err = context.Cause(c.ctx)
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
