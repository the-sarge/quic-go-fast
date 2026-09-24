package quic

import (
	"sync"

	"github.com/quic-go/quic-go/internal/protocol"
)

// localSendCredit is the only cross-goroutine byte owner. Pending includes
// construction reservations and every buffer retained by the worker, including
// accepted prefixes until their group is retired. It is not recovery flight.
type localSendCredit struct {
	mu               sync.Mutex
	pending, current protocol.ByteCount
	generation       uint64
	isolated         bool
	available        chan struct{}
}

type sendReservation struct {
	owner      *localSendCredit
	bytes      protocol.ByteCount
	generation uint64
	isolated   bool
}

func newLocalSendCredit() *localSendCredit {
	return &localSendCredit{available: make(chan struct{}, 1)}
}

func (c *localSendCredit) resetGeneration(generation uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if generation != c.generation {
		c.generation, c.current = generation, 0
	}
}

// canReserve is called with mu held by both admission and wakeup rearming.
func (c *localSendCredit) canReserve(n, limit protocol.ByteCount, ordinary, isolated bool) bool {
	return !c.isolated && (!isolated || c.pending == 0) && (isolated || n <= limit-c.pending) && (!ordinary || c.pending == c.current)
}

func (c *localSendCredit) reserve(n, limit protocol.ByteCount, ordinary, isolated bool) *sendReservation {
	c.mu.Lock()
	defer c.mu.Unlock()
	if n <= 0 {
		panic("invalid local send reservation")
	}
	if !c.canReserve(n, limit, ordinary, isolated) {
		// Test capacity and consume stale wakeups under the completion lock.
		select {
		case <-c.available:
		default:
		}
		return nil
	}
	c.pending += n
	c.current += n
	c.isolated = isolated
	return &sendReservation{owner: c, bytes: n, generation: c.generation, isolated: isolated}
}

// resize is connection-owned until handoff. Afterwards only the buffer's final
// owner calls complete. Nil reservations leave the legacy path allocation-free.
func (r *sendReservation) resize(n protocol.ByteCount) {
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

func (r *sendReservation) complete() { r.resize(0) }

// waitForReservation rearms the refused request and reports whether ACK/PTO
// deadlines remain useful. Reservation and wakeup state share the same lock.
func (c *localSendCredit) waitForReservation(n, limit, controlSize protocol.ByteCount, ordinary, isolated bool) bool {
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
