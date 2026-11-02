package ackhandler

import "github.com/quic-go/quic-go/internal/monotime"

// fenceTiming requires a fresh registration snapshot after an excluded local
// interval. Logical delivery totals and registration generations do not change.
func (s *deliverySampler) fenceTiming() {
	s.timingGeneration++
	s.sendOrigin, s.deliveredTime = 0, 0
	s.originlessRegistrations = false
}

func (h *sentPacketHandler) completeLocalCongestion(r *localRegistration) {
	d := h.congestionEvents
	if d == nil {
		return
	}
	defer d.advanceLocalQualification()
	key := congestionKey(r.level, r.pn)
	p, live := d.packets.get(key)
	retained := d.sampler.retained[key]
	if !live && retained != nil {
		p = retained.packet
	}
	if p.Ordinal != r.richOrdinal || r.richOrdinal == 0 {
		return
	}
	slot := d.recovery.find(key)
	missing := slot < 0 || d.recovery.index.outcomes[slot].ordinal != r.richOrdinal
	if (r.outcome != LocalPacketAccepted || missing) && !r.earlyACK {
		d.sampler.fenceTiming()
	}
	p.TimingExcluded = r.outcome != LocalPacketAccepted || r.earlyACK || missing
	if live {
		d.packets.set(key, p)
	} else {
		retained.packet = p
	}
}

// Local qualification is a slot-indexed adjunct of the existing recovery ring.
// Only the local route allocates it; legacy outcomes remain pointer-free.
// Registration, eviction and reset share the ring's identities and ceiling.
type localRecoveryEvidence struct {
	outcomes         []localRecoveryOutcome
	qualifiedOrdinal uint64
	lastEligible     monotime.Time
}

type localRecoveryOutcome struct {
	registration *localRegistration // borrowed authoritative fact until cursor consumption
	eligibleAt   monotime.Time
	qualified    bool
}

func (r *recoveryEvidence) timingQualified(slot int) bool {
	return r.local == nil || r.local.outcomes[slot].qualified
}

// Only qualified native-return endpoints enter time-ordered queries. Raw send
// provenance stays intact even when the local endpoint is excluded.
func (r *recoveryEvidence) evidenceTime(slot int) monotime.Time {
	if r.local != nil {
		return r.local.outcomes[slot].eligibleAt
	}
	return r.index.outcomes[slot].sent
}

// The cursor visits each retained registration once, in registration order.
// Pending predecessors delay this derived proof only, not ordinary loss/PTO.
// Eviction consumes unknown prefixes without reconstructing their facts.
func (d *congestionDispatch) advanceLocalQualification() {
	if !d.localRoute {
		return
	}
	r := &d.recovery
	if r.local == nil || r.count == 0 {
		return
	}
	q := r.local
	first := r.index.outcomes[r.head].ordinal
	if q.qualifiedOrdinal+1 < first {
		q.qualifiedOrdinal = first - 1
		if r.evicted != 0 {
			d.sampler.fenceTiming()
		}
	}
	for l := int(q.qualifiedOrdinal + 1 - first); l < r.count; l++ {
		slot := r.slot(l)
		o := &r.index.outcomes[slot]
		fact := &q.outcomes[slot]
		local := fact.registration
		if local != nil && local.outcome == localPacketPending && !local.earlyACK && o.state != outcomeDisposed {
			return
		}
		fact.registration = nil
		q.qualifiedOrdinal = o.ordinal
		if local != nil && local.outcome == LocalPacketAccepted && !local.earlyACK && !local.timingMissing && o.state != outcomeDisposed && o.state != outcomeExcluded && !local.eligibleAt.Before(q.lastEligible) {
			fact.qualified = true
			fact.eligibleAt = local.eligibleAt
			q.lastEligible = fact.eligibleAt
		} else if local != nil && local.outcome == LocalPacketAccepted && !local.earlyACK && o.state != outcomeDisposed {
			// A backward timestamp or missing timing fact breaks sampling continuity as
			// well. It never rolls back the global endpoint watermark.
			d.sampler.fenceTiming()
			d.excludeLocalTiming(congestionPacketKey{space: o.space, number: o.number}, o.ordinal)
		}
		r.index.ptoPending[recoverySpace(o.space)].assign(slot, fact.qualified && o.ptoRetired && o.state == outcomeUnresolved)
		if o.state == outcomeLost {
			r.setLost(slot, fact.qualified)
			if fact.qualified {
				r.unconfirmedLoss = true
			}
		}
	}
}

func (d *congestionDispatch) excludeLocalTiming(key congestionPacketKey, ordinal uint64) {
	if p, ok := d.packets.get(key); ok && p.Ordinal == ordinal {
		p.TimingExcluded = true
		d.packets.set(key, p)
	}
	if p := d.sampler.retained[key]; p != nil && p.packet.Ordinal == ordinal {
		p.packet.TimingExcluded = true
	}
}
