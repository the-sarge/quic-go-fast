package quic

import (
	"sync"

	"github.com/quic-go/quic-go/internal/protocol"
)

// frozenLocalSendCredit is the predecessor ledger (fc4c1bf1), verbatim apart
// from its type names: pointer reservations allocated per reserve, and every
// release signalling the wakeup channel.
type frozenLocalSendCredit struct {
	mu               sync.Mutex
	pending, current protocol.ByteCount
	generation       uint64
	isolated         bool
	available        chan struct{}
}

type frozenSendReservation struct {
	owner      *frozenLocalSendCredit
	bytes      protocol.ByteCount
	generation uint64
	isolated   bool
}

func newFrozenLocalSendCredit() *frozenLocalSendCredit {
	return &frozenLocalSendCredit{available: make(chan struct{}, 1)}
}

func (c *frozenLocalSendCredit) resetGeneration(generation uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if generation != c.generation {
		c.generation, c.current = generation, 0
	}
}

func (c *frozenLocalSendCredit) canReserve(n, limit protocol.ByteCount, ordinary, isolated bool) bool {
	return !c.isolated && (!isolated || c.pending == 0) && (isolated || n <= limit-c.pending) && (!ordinary || c.pending == c.current)
}

func (c *frozenLocalSendCredit) reserve(n, limit protocol.ByteCount, ordinary, isolated bool) *frozenSendReservation {
	c.mu.Lock()
	defer c.mu.Unlock()
	if n <= 0 {
		panic("invalid local send reservation")
	}
	if !c.canReserve(n, limit, ordinary, isolated) {
		select {
		case <-c.available:
		default:
		}
		return nil
	}
	c.pending += n
	c.current += n
	c.isolated = isolated
	return &frozenSendReservation{owner: c, bytes: n, generation: c.generation, isolated: isolated}
}

func (r *frozenSendReservation) resize(n protocol.ByteCount) {
	if r == nil {
		return
	}
	c := r.owner
	c.mu.Lock()
	defer c.mu.Unlock()
	if n < 0 || n > r.bytes {
		panic("invalid local send completion")
	}
	released := r.bytes - n
	c.pending -= released
	if r.generation == c.generation {
		c.current -= released
	}
	r.bytes = n
	if n == 0 && r.isolated {
		c.isolated = false
	}
	if released != 0 {
		select {
		case c.available <- struct{}{}:
		default:
		}
	}
}

func (r *frozenSendReservation) complete() { r.resize(0) }

func (c *frozenLocalSendCredit) waitForReservation(n, limit, controlSize protocol.ByteCount, ordinary, isolated bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.available:
	default:
	}
	if c.canReserve(n, limit, ordinary, isolated) {
		c.available <- struct{}{}
	}
	return c.canReserve(controlSize, limit, false, false)
}
