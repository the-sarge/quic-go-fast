package ackhandler

import "fmt"

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
		_, _, capable := e.path()
		if !capable {
			state = "unsupported"
		}
	}
	limitation := [...]string{"unknown", "application", "flow-control", "ProbeRTT", "congestion", "pacing", "local", "receive-yield", "recovery"}[d.sampler.stop]
	return fmt.Sprintf("ecn=%s path=%d sample_generation=%d limitation=%s sample_valid=%t sample_interval=%s sample_evidence_lost=%t live=%d retained=%d evicted=%d expired=%d missing=%d", state, d.pathGeneration, d.sampleGeneration, limitation, d.event.Delivery.Valid, d.event.Delivery.Interval, d.sampler.evidenceLost, len(d.packets), len(d.sampler.retained), d.sampler.evicted, d.sampler.expired, d.sampler.missing)
}
