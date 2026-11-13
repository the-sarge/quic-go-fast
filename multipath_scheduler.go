package quic

import (
	"math/bits"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

// pathScheduler is a multipath connection's private scheduling policy. It owns the
// choice of route only; emission keeps packing, credit, registration and each
// DATAGRAM's eligibility. Primary/backup is the default and the only public policy.
// Aggregate stays privately selected until its support decision.
type pathScheduler uint8

const (
	schedulePrimaryBackup pathScheduler = iota
	// scheduleAggregate sends each opportunity on the eligible path with the earliest
	// estimated arrival, or by paced byte round-robin while any lacks a rate sample.
	scheduleAggregate
	// scheduleRoundRobin is the evidence fixture's comparison arm: aggregate with
	// the estimate off. Only a fixture selects it; it is not a maintained policy.
	scheduleRoundRobin
)

// schedule chooses the opportunity's route from immutable views in path ID order.
func (p *pathCreditRegistry) schedule(views []pathView, now monotime.Time) (chosen, steerTo protocol.PathID, steering, held bool) {
	if p.scheduler == schedulePrimaryBackup {
		return selectPath(views, p.primary)
	}
	chosen, steerTo, steering, held, floor := selectAggregate(views, p.primary, now, p.scheduler == scheduleAggregate)
	// Rebase the ledger on its least-served candidate, so it stays within about one
	// handoff and a returning path is neither starved nor owed a backlog.
	for _, m := range p.members {
		m.scheduled -= min(m.scheduled, floor)
	}
	return chosen, steerTo, steering, held
}

// viewOf reads one eligible path for selection, after bringing a nonzero route up
// to its current tuple.
func (p *pathCreditRegistry) viewOf(m *pathMember, now monotime.Time) pathView {
	var route *pathRoute
	recovery := p.conn.sentPacketHandler
	if m.id != 0 {
		p.refreshRoute(m)
		route, recovery = &m.route, m.route.recovery
	}
	view := p.conn.emission.pathView(m.id, route, recovery, now)
	view.backup, view.scheduled = m.backup, m.scheduled
	if rtt := p.rttOf(m.id); rtt != nil && rtt.HasMeasurement() {
		view.rtt = rtt.SmoothedRTT()
	}
	return view
}

// noteScheduled charges a handoff's bytes to the round-robin ledger of the path
// that carried it.
func (p *pathCreditRegistry) noteScheduled(route *pathRoute, n protocol.ByteCount) {
	if p.scheduler == schedulePrimaryBackup {
		return
	}
	var id protocol.PathID
	if route != nil {
		id = route.context.PathID
	}
	if m := p.members[id]; m != nil {
		m.scheduled += uint64(n)
	}
}

// selectAggregate is the aggregate policy. A pending PTO goes first, so a path's
// probe retries its reliable data there. Active paths are the candidates while any is
// eligible, and backups only when none is. Among candidates that can take a whole
// packet, the earliest estimated arrival wins when every one has a valid rate sample
// and estimate is set; otherwise paced byte round-robin chooses. Ties go to the lower
// wire ID. With no candidate ready, the primary waits, as it does under primary/backup.
// floor is the least ledger among the round-robin candidates.
func selectAggregate(views []pathView, primary protocol.PathID, now monotime.Time, estimate bool) (chosen, steerTo protocol.PathID, steering, held bool, floor uint64) {
	for _, view := range views {
		if view.probing {
			return view.id, 0, false, false, 0
		}
	}
	active := false
	for _, view := range views {
		active = active || !view.backup
	}
	candidate := func(view pathView) bool { return !active || !view.backup }
	for _, view := range views {
		if candidate(view) && view.ready && (view.rate == 0 || view.rtt <= 0) {
			estimate = false
		}
	}
	pick, wait, first := -1, -1, true
	for i, view := range views {
		if !candidate(view) {
			continue
		}
		if wait < 0 || view.id == primary {
			wait = i
		}
		if !view.ready {
			continue
		}
		if first || view.scheduled < floor {
			floor, first = view.scheduled, false
		}
		if pick < 0 || (estimate && arrival(view, now) < arrival(views[pick], now)) || (!estimate && roundRobinBefore(view, views[pick], now)) {
			pick = i
		}
	}
	if pick < 0 {
		if wait < 0 {
			return 0, 0, false, true, 0
		}
		pick = wait
	}
	if choice := views[pick]; !choice.fits {
		for _, view := range views {
			if view.fits && view.ready {
				return choice.id, view.id, true, false, floor
			}
		}
	}
	return views[pick].id, 0, false, false, floor
}

// arrival estimates when a packet sent on this path next would arrive: its next
// eligible send time, plus its queued bytes at its conservative delivery rate, plus
// half its path-local RTT.
func arrival(view pathView, now monotime.Time) time.Duration {
	hi, lo := bits.Mul64(uint64(view.queued), uint64(time.Second))
	if hi >= view.rate {
		return time.Duration(1<<63 - 1)
	}
	drain, _ := bits.Div64(hi, lo, view.rate)
	return max(0, view.sendAt.Sub(now)) + time.Duration(min(drain, 1<<62)) + view.rtt/2
}

// roundRobinBefore orders paced byte round-robin: a path pacing allows now before
// one it doesn't, the earlier send time among paced paths, then fewer bytes served.
func roundRobinBefore(a, b pathView, now monotime.Time) bool {
	aNow, bNow := !a.sendAt.After(now), !b.sendAt.After(now)
	switch {
	case aNow != bNow:
		return aNow
	case !aNow && a.sendAt != b.sendAt:
		return a.sendAt < b.sendAt
	default:
		return a.scheduled < b.scheduled
	}
}
