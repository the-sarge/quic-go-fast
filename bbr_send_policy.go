package quic

import (
	"fmt"
	"time"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
)

// bbrSendPolicy is connection-owned. rate includes BBR's 1% margin;
// Reno's pacer and its burst/rate adjustments are deliberately independent.
type bbrSendPolicy struct {
	ceiling    protocol.ByteCount
	controller *congestion.BBRSender
	credit     *localSendCredit
	rate       uint64 // UDP payload bytes per second
	quantum    protocol.ByteCount
	tokens     int64 // byte-nanoseconds, retaining fractional byte credit
	updated    monotime.Time
	lastTrace  monotime.Time
	// refilledAt is when all credit spent so far will have refilled to the
	// quantum, at the rate and quantum when it was spent. Only spending moves
	// it, and only later: a rate cut that clamps the credit is not refill.
	refilledAt monotime.Time
	// supplyGap is the application or flow-control exhaustion observed at the
	// latest opportunity, not yet established as supply limitation.
	supplyGap congestion.SendLimitation
	// paths holds, on the root policy only, each nonzero path's own policy.
	paths map[protocol.PathID]*bbrSendPolicy
}

// addBBRPath gives a nonzero path its own BBR controller, pacing and local
// credit beside the root's. Recovery owns the path context and its delivery
// sampler, whose pending-local authority is this path's credit alone. When the
// root validates ECN, so does the path, fenced by this credit's generation. Like
// a Reno path context, it starts at the minimum initial size; the route's own
// PMTU raises it at send time.
func (e *packetEmission) addBBRPath(id protocol.PathID, rtt *utils.RTTStats, now monotime.Time) (*bbrSendPolicy, error) {
	const size = protocol.MinInitialPacketSize
	recovery, ok := (*e.recovery).(interface {
		AddBBRPath(protocol.PathID, protocol.ByteCount, protocol.ByteCount, *utils.RTTStats, func() protocol.ByteCount, func(bool) (uint64, bool, bool)) (*congestion.BBRSender, error)
	})
	if e.bbr == nil || !ok || e.bbr.paths[id] != nil || size > e.bbr.ceiling {
		return nil, fmt.Errorf("BBR path unavailable: %d", id)
	}
	p := newBBRSendPolicyWithCeiling(1, size, e.bbr.ceiling, now)
	b, err := recovery.AddBBRPath(id, size, e.bbr.ceiling, rtt, func() protocol.ByteCount { return e.bbrPendingBytes(p) }, e.bbrECNAuthority(func() *bbrSendPolicy { return p }))
	if err != nil {
		return nil, err
	}
	p.controller = b
	p.update(b.PacingRate(), size, now)
	if e.bbr.paths == nil {
		e.bbr.paths = make(map[protocol.PathID]*bbrSendPolicy, 1)
	}
	e.bbr.paths[id] = p
	return p, nil
}

// resetBBRPath starts a nonzero BBR path's next tuple generation, as
// resetLocalPath does for path zero. It pairs with the MigratedPath call on the
// path's recovery context in the same cutover: both generations start at zero
// and advance once per migration, so the context's ECN validation waits for
// exactly this credit's older sends.
func (e *packetEmission) resetBBRPath(id protocol.PathID, now monotime.Time) {
	if e.bbr == nil || e.bbr.paths[id] == nil {
		return
	}
	p := e.bbr.paths[id]
	p.resetGeneration(p.credit.generation+1, now)
}

// removeBBRPath reclaims a BBR path's recovery context and policy together, at
// the path's final reclamation. Reservations still held by queued buffers
// complete against the removed credit.
func (e *packetEmission) removeBBRPath(id protocol.PathID) {
	if e.bbr == nil || e.bbr.paths[id] == nil {
		return
	}
	(*e.recovery).(interface{ RemovePath(protocol.PathID) }).RemovePath(id)
	delete(e.bbr.paths, id)
}

// policyOn is the BBR send policy of the route that carries an opportunity: the
// root for path zero, otherwise the path's own. Admission installs a path's
// policy before its route can be selected, and final reclamation removes it only
// after the route can no longer be selected.
func (e *packetEmission) policyOn(route *pathRoute) *bbrSendPolicy {
	if route == nil {
		return e.bbr
	}
	if p := e.bbr.paths[route.context.PathID]; p != nil {
		return p
	}
	panic("BBR route without its path policy")
}

func (e *packetEmission) routePolicy() *bbrSendPolicy { return e.policyOn(e.route) }

// grantor is the policy whose credit granted r, which pays for its packet.
func (e *packetEmission) grantor(r *sendReservation) *bbrSendPolicy {
	if r.owner == e.bbr.credit {
		return e.bbr
	}
	for _, p := range e.bbr.paths {
		if p.credit == r.owner {
			return p
		}
	}
	panic("BBR reservation without its policy")
}

func newBBRSendPolicy(rate uint64, size protocol.ByteCount, now monotime.Time) *bbrSendPolicy {
	return newBBRSendPolicyWithCeiling(rate, size, protocol.MaxPacketBufferSize, now)
}

func newBBRSendPolicyWithCeiling(rate uint64, size, ceiling protocol.ByteCount, now monotime.Time) *bbrSendPolicy {
	p := &bbrSendPolicy{ceiling: ceiling, updated: now, refilledAt: now, credit: newLocalSendCredit()}
	p.update(rate, size, now)
	p.tokens = int64(p.quantum) * 1e9
	return p
}

func (p *bbrSendPolicy) update(rate uint64, size protocol.ByteCount, now monotime.Time) {
	if rate == 0 || size <= 0 || size > p.ceiling {
		panic("invalid BBR send policy")
	}
	p.budget(now)
	p.rate = max(1, rate/100*99+rate%100*99/100)
	p.quantum = max(2*size, protocol.ByteCount(min(uint64(65536), p.rate/1000)))
	p.tokens = min(p.tokens, int64(p.quantum)*1e9)
}

func (p *bbrSendPolicy) budget(now monotime.Time) protocol.ByteCount {
	if now > p.updated {
		p.tokens, p.updated = p.accrued(now), now
	}
	return max(0, protocol.ByteCount(p.tokens/1e9))
}

// accrued is the credit, in byte-nanoseconds, that refill gives by now, without
// recording it.
func (p *bbrSendPolicy) accrued(now monotime.Time) int64 {
	if now <= p.updated {
		return p.tokens
	}
	missing := int64(p.quantum)*1e9 - p.tokens
	delta := uint64(now.Sub(p.updated))
	if p.rate != 0 && delta >= ceilSendDelay(uint64(missing), p.rate) {
		return p.tokens + missing
	}
	return p.tokens + int64(delta*p.rate) // bounded above by missing
}

// sendAt is when this pacing next lets size leave: now when credit covers it,
// otherwise when credit reaches a full quantum, as deadline wakes. It spends and
// records nothing.
func (p *bbrSendPolicy) sendAt(size protocol.ByteCount, now monotime.Time) monotime.Time {
	tokens := p.accrued(now)
	if max(0, protocol.ByteCount(tokens/1e9)) >= size {
		return now
	}
	return now.Add(time.Duration(ceilSendDelay(uint64(int64(p.quantum)*1e9-tokens), p.rate)))
}

func ceilSendDelay(deficit, rate uint64) uint64 {
	d := deficit / rate
	if deficit%rate != 0 {
		d++
	}
	return d
}

// deadline returns zero when credit covers size. Otherwise the next opportunity
// is when credit reaches a full quantum, so each wakeup can release Q.
func (p *bbrSendPolicy) deadline(size protocol.ByteCount, now monotime.Time) monotime.Time {
	if p.budget(now) >= size {
		return 0
	}
	return p.fullAt(now)
}

// fullAt is when credit, refilling at the current rate, reaches the quantum.
// Call it after budget.
func (p *bbrSendPolicy) fullAt(now monotime.Time) monotime.Time {
	return now.Add(time.Duration(ceilSendDelay(uint64(int64(p.quantum)*1e9-p.tokens), p.rate)))
}

func (p *bbrSendPolicy) sent(size protocol.ByteCount, now monotime.Time) {
	if size < 0 || size > p.budget(now) {
		panic("BBR pacing credit exceeded")
	}
	p.tokens -= int64(size) * 1e9
	// An unpaced send spends nothing; recomputing at a changed rate or quantum
	// would move the refill time without any spending.
	if size > 0 {
		p.refilledAt = max(p.refilledAt, p.fullAt(now))
	}
}

// An isolated oversized probe waits for a full quantum, then retains its excess
// cost as pacing debt. Ordinary sends cannot borrow against future credit.
func (p *bbrSendPolicy) sentProbe(size protocol.ByteCount, now monotime.Time) {
	if size <= 0 || min(size, p.quantum) > p.budget(now) {
		panic("BBR probe pacing credit exceeded")
	}
	p.tokens -= int64(size) * 1e9
	p.refilledAt = max(p.refilledAt, p.fullAt(now))
}
