package ackhandler

import "github.com/quic-go/quic-go/internal/protocol"

// AckCarriage is one emission call's acknowledgment placement. Each held target
// is home: only a packet on its own path may carry it. Every other target is
// released and may ride any carrier under the ordinary piggyback rules. The zero
// value holds nothing, which is ordinary carriage. Emission builds it at the
// start of a call and drops it at the end; the receive owner alone applies it.
type AckCarriage struct {
	held [maxRecoveryPaths + 1]protocol.PathID
	n    uint8
}

// Hold makes id home for this call. A target that doesn't fit stays released.
func (c *AckCarriage) Hold(id protocol.PathID) {
	if c.Holds(id) || int(c.n) == len(c.held) {
		return
	}
	c.held[c.n] = id
	c.n++
}

// Release lets id ride any carrier for the rest of the call.
func (c *AckCarriage) Release(id protocol.PathID) {
	for i := 0; i < int(c.n); i++ {
		if c.held[i] == id {
			c.n--
			c.held[i] = c.held[c.n]
			return
		}
	}
}

// Holds reports whether id is home for this call.
func (c AckCarriage) Holds(id protocol.PathID) bool {
	for i := 0; i < int(c.n); i++ {
		if c.held[i] == id {
			return true
		}
	}
	return false
}

// Held returns the home targets in no particular order.
func (c *AckCarriage) Held() []protocol.PathID { return c.held[:c.n] }

// carries reports whether a packet on carrier may take target's acknowledgment.
func (c AckCarriage) carries(target, carrier protocol.PathID) bool {
	return target == carrier || !c.Holds(target)
}
