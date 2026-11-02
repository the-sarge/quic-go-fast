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

// Only qualified native-return endpoints enter time-ordered queries. Raw send
// provenance stays intact even when the local endpoint is excluded.
func (o *recoveryOutcome) evidenceTime() monotime.Time {
	if o.local {
		return o.eligibleAt
	}
	return o.sent
}

// The cursor visits each retained registration once, in registration order.
// Pending predecessors delay this derived proof only, not ordinary loss/PTO.
// Eviction consumes unknown prefixes without reconstructing their facts.
func (d *congestionDispatch) advanceLocalQualification() {
	if !d.localRoute {
		return
	}
	r := &d.recovery
	if r.index == nil || r.count == 0 {
		return
	}
	first := r.index.outcomes[r.head].ordinal
	if r.qualifiedOrdinal+1 < first {
		r.qualifiedOrdinal = first - 1
		if r.evicted != 0 {
			d.sampler.fenceTiming()
		}
	}
	for l := int(r.qualifiedOrdinal + 1 - first); l < r.count; l++ {
		slot := r.slot(l)
		o := &r.index.outcomes[slot]
		local := o.qualification
		if !o.local {
			r.qualifiedOrdinal = o.ordinal
			continue
		}
		if local != nil && local.outcome == localPacketPending && !local.earlyACK && o.state != outcomeDisposed {
			return
		}
		o.qualification = nil
		r.qualifiedOrdinal = o.ordinal
		if local != nil && local.outcome == LocalPacketAccepted && !local.earlyACK && !local.timingMissing && o.state != outcomeDisposed && o.state != outcomeExcluded && !local.eligibleAt.Before(r.lastEligible) {
			o.qualified = true
			o.eligibleAt = local.eligibleAt
			r.lastEligible = o.eligibleAt
		} else if local != nil && local.outcome == LocalPacketAccepted && !local.earlyACK && o.state != outcomeDisposed {
			// A backward timestamp or missing timing fact breaks sampling continuity as
			// well. It never rolls back the global endpoint watermark.
			d.sampler.fenceTiming()
			d.excludeLocalTiming(congestionPacketKey{space: o.space, number: o.number}, o.ordinal)
		}
		r.index.ptoPending[recoverySpace(o.space)].assign(slot, o.qualified && o.ptoRetired && o.state == outcomeUnresolved)
		if o.state == outcomeLost {
			r.setLost(slot, o.qualified)
			if o.qualified {
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
