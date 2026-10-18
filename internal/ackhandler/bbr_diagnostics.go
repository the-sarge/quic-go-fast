package ackhandler

import (
	"fmt"

	"github.com/quic-go/quic-go/internal/congestion"
)

// BBRDiagnostics observes recovery-owned facts without advancing ECN validation
// or lending mutable controller state to application callers.
func (h *sentPacketHandler) BBRDiagnostics() string {
	d := h.congestionEvents
	e := h.bbrECN
	if d == nil || e == nil {
		return "ecn=unavailable"
	}
	state := [...]string{"initial", "testing", "unknown", "capable", "failed"}[e.state]
	switch {
	case e.closed:
		state = "closed"
	case e.counterFailed:
		state = "invalid-counters"
	case e.evidenceLost:
		state = "missing-marking-evidence"
	case e.draining:
		state = "draining-counter-fence"
	}
	if !e.closed {
		_, _, capable := e.path(false)
		if !capable {
			state = "unsupported"
		}
	}
	// stop is the latest opportunity's stop; limitation is the marker a packet
	// registered now would carry, which a later stop does not replace.
	reasons := [...]string{"unknown", "application", "flow-control", "ProbeRTT", "congestion", "pacing", "local", "receive-yield", "recovery"}
	limitation := "none"
	if l := d.sampler.activeLimitation(); l != congestion.SendUnknown {
		limitation = reasons[l]
	}
	return fmt.Sprintf("ecn=%s path=%d sample_generation=%d stop=%s limitation=%s sample_valid=%t sample_interval=%s sample_evidence_lost=%t live=%d retained=%d evicted=%d expired=%d missing=%d", state, d.pathGeneration, d.sampleGeneration, reasons[d.sampler.stop], limitation, d.event.Delivery.Valid, d.event.Delivery.Interval, d.sampler.evidenceLost, d.packets.len(), len(d.sampler.retained), d.sampler.evicted, d.sampler.expired, d.sampler.missing)
}
