package quic

import (
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go/internal/utils"
)

// rttReader gives flow-control autotuning an observation without granting
// access to a path's recovery estimator.
type rttReader interface{ SmoothedRTT() time.Duration }

// rttObservation is stable for the connection and every existing stream. The
// connection owner selects zero while live, then the designated primary. This
// observation never supplies a different path's loss/PTO estimator.
type rttObservation struct {
	source atomic.Pointer[utils.RTTStats]
}

func newRTTObservation(source *utils.RTTStats) *rttObservation {
	observation := &rttObservation{}
	observation.observe(source)
	return observation
}

func (r *rttObservation) observe(source *utils.RTTStats) {
	if source == nil {
		panic("nil RTT observation source")
	}
	r.source.Store(source)
}

func (r *rttObservation) SmoothedRTT() time.Duration { return r.source.Load().SmoothedRTT() }

// freeze runs in the connection context after its final protocol transition.
func (r *rttObservation) freeze() { r.observe(r.source.Load().Clone()) }

func (c *Conn) observedRTT() *utils.RTTStats {
	if c.rttObservation != nil {
		return c.rttObservation.source.Load()
	}
	// Small connection fixtures can supply just the legacy recovery estimator.
	return c.rttStats
}

func (c *Conn) flowControlRTT() rttReader {
	if c.rttObservation != nil {
		return c.rttObservation
	}
	return c.rttStats
}
