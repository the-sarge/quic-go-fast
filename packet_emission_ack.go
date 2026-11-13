package quic

import (
	"slices"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

// beginAckCarriage reads this call's home targets before any packing, and takes the
// alternation fact: the call after a sent turn runs no turn of its own.
func (e *packetEmission) beginAckCarriage() {
	e.acks = ackhandler.AckCarriage{}
	if e.local != nil {
		e.pathCredit.holdHomes(&e.acks)
	}
	e.ackTurnSkip, e.ackTurnSent = e.ackTurnSent, false
}

// endAckCarriage drops the call's carriage. A call that skipped its turn requests a
// retry while a due home target still waits, since an ACK queued by threshold,
// missing packets or ECN-CE arms no alarm. A queue-full call leaves that to the
// capacity wakeup.
func (e *packetEmission) endAckCarriage(now monotime.Time, result *emissionResult) {
	if e.ackTurnSkip && result.err == nil && result.stop != emissionQueueFull && e.homeAckWaiting(now) {
		result.retry = true
	}
	e.acks, e.ackTurnSkip = ackhandler.AckCarriage{}, false
}

func (e *packetEmission) homeAckWaiting(now monotime.Time) bool {
	held := e.acks
	for _, id := range held.Held() {
		if e.pathCredit.conn.receivedPacketHandler.AckDueOnPath(id, now) {
			return true
		}
	}
	return false
}

// ackTurn gives one due home target an ACK-only turn on its own route, after the
// announcement and before the held opportunity. The opportunity's own route
// carries its target in its packets, so it gets no turn. A refused target is
// released, and any carrier in this call may take it under the ordinary rules.
func (e *packetEmission) ackTurn(now monotime.Time) (bool, error) {
	if e.pathCredit == nil || e.local == nil || e.ackTurnSkip {
		return false, nil
	}
	held := e.acks
	targets := held.Held()
	slices.Sort(targets)
	opportunity := carrierOf(e.route)
	for _, id := range targets {
		if id == opportunity || !e.pathCredit.conn.receivedPacketHandler.AckDueOnPath(id, now) {
			continue
		}
		sent, err := e.ackTurnOn(id, now)
		if err != nil || sent {
			return sent, err
		}
		e.acks.Release(id)
	}
	return false, nil
}

// ackTurnOn follows the announcement's shape: switch the local route, check
// native construction capacity, take BBR local credit (the ACK exemption bypasses
// pacing, never local byte ownership), pack, register and hand off, then restore
// the selected route. Its packet takes only acknowledgments, plus the carrier's
// periodic PING. It reports false, sending nothing, for a refusal.
func (e *packetEmission) ackTurnOn(id protocol.PathID, now monotime.Time) (bool, error) {
	route := e.pathCredit.routeOf(id)
	recovery := *e.recovery
	if route != nil {
		recovery = route.recovery
	}
	// Recovery reports history room; emission decides.
	if room, ok := recovery.(interface{ AckTurnRoom(monotime.Time) bool }); !ok || !room.AckTurnRoom(now) {
		return false, nil
	}
	if !e.canConstruct(route) || recovery.SendMode(now) == ackhandler.SendNone {
		return false, nil
	}
	size := e.packetSizeOn(route)
	if e.bbr != nil && !e.reserveOn(e.policyOn(route), size, false, false, now) {
		return false, nil
	}
	selected := e.route
	e.useRoute(route)
	defer e.useRoute(selected)
	if e.queue.WouldBlock() {
		e.reservation.completeLocal()
		e.reservation = nil
		return false, nil
	}
	ecn := recovery.ECNMode(true)
	p, buf, err := e.packer.packAckOnlyPacket(route, e.acks, size, now, e.version)
	if err != nil {
		e.reservation.completeLocal()
		e.reservation = nil
		if err == errNothingToPack {
			return false, nil
		}
		return false, err
	}
	e.policy.logShortHeaderPacket(p, ecn, buf.Len())
	if err := e.registerPacket(p, ecn, now); err != nil {
		buf.Release()
		return false, err
	}
	e.handoff(buf, 0, ecn, sendMetadata{}, nil)
	e.ackTurnSent = true
	return true, nil
}

// bareProbe reports that a 1-RTT PTO probe on the opportunity's route is a PING
// with no payload: the route isn't the designated primary, and every outstanding
// ack-eliciting record it holds is PINGs and acknowledgments, or there is none.
// It reads recovery before any extraction.
func (e *packetEmission) bareProbe(recovery ackhandler.SentPacketHandler) bool {
	if e.pathCredit == nil || !e.pathCredit.hasAdmission() || carrierOf(e.route) == e.pathCredit.primary {
		return false
	}
	pings, ok := recovery.(interface{ OnlyPingsOutstanding() bool })
	return ok && pings.OnlyPingsOutstanding()
}
