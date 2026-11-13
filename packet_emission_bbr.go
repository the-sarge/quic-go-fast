package quic

import (
	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

// boundedRecovery is internal module plumbing, not a controller plugin API.
// The real recovery owner supplies every gate; emission owns byte quantization.
type boundedRecovery interface {
	PrepareBBRSend(monotime.Time)
	SendAllowance(monotime.Time) (ackhandler.SendMode, protocol.ByteCount)
}

func (e *packetEmission) handoff(buf *packetBuffer, gso uint16, ecn protocol.ECN, metadata sendMetadata, protection *nativeProtectionConstruction) {
	// The run loop stamps every handoff, including Reno and discovery probes.
	metadata.pathGeneration = e.policy.sendPathGeneration()

	if e.bbr != nil {
		if e.reservation == nil {
			panic("BBR handoff without reservation")
		}
		p := e.grantor(e.reservation)
		e.reservation.resize(buf.Len())
		if e.reservation.isolated {
			p.sentProbe(buf.Len(), e.reservationTime)
		} else if e.paceReservation {
			p.sent(e.pacingBytes, e.reservationTime)
		}
		metadata.credit = e.reservation
		if p == e.bbr {
			// Only path zero's credit generation is the connection's path generation.
			metadata.pathGeneration = e.reservation.generation
		}
		e.reservation = nil
	}
	metadata.terminalReceipts = e.terminalReceipts
	e.terminalReceipts = nil
	if protection.hasPlan() {
		protection.send(e, buf, gso, ecn, metadata)
		return
	}
	e.queue.Send(buf, gso, ecn, metadata)
}

func (e *packetEmission) localBlocked() emissionResult {
	return e.localBlockedOn(e.routePolicy())
}

func (e *packetEmission) localBlockedOn(p *bbrSendPolicy) emissionResult {
	return emissionResult{stop: emissionQueueFull, blocked: blockModeHardBlocked, available: p.credit.available}
}

// reserveLocal takes construction allowance from the selected route's credit.
func (e *packetEmission) reserveLocal(n protocol.ByteCount, ordinary, isolated bool, now monotime.Time) bool {
	return e.reserveOn(e.routePolicy(), n, ordinary, isolated, now)
}

func (e *packetEmission) reserveOn(p *bbrSendPolicy, n protocol.ByteCount, ordinary, isolated bool, now monotime.Time) bool {
	e.reservation = p.credit.reserve(n, 2*p.quantum, ordinary, isolated)
	e.paceReservation, e.reservationTime, e.pacingBytes = ordinary, now, 0
	return e.reservation != nil
}

// boundedGSO applies Reno's route rule to the opportunity's capability: a target
// transport or a tuple's own fallback disables segmentation, as does a terminal
// control datagram awaiting its receipt.
func (e *packetEmission) boundedGSO() bool {
	if e.pathCredit != nil && e.pathCredit.needsReceipt() {
		return false
	}
	if r := e.route; r != nil && (r.transport != nil || r.writeState.gsoDisabled.Load()) {
		return false
	}
	return e.opportunityCapabilities().GSO
}

// pathTurn is how a multipath connection's path turns ended an opportunity.
type pathTurn uint8

const (
	pathTurnNone      pathTurn = iota // the opportunity continues on the selected route
	pathTurnHeld                      // the selected route's opportunity, holding ordinary work
	pathTurnSignaling                 // an admission probe or announcement, which isn't the selected route's opportunity
)

// boundedPathTurns gives a multipath connection Reno's turns in Reno's order:
// admission probes, route selection, the announcement a cutover owes and the
// held opportunity. Each packet charges the credit and pacing of the route that
// carries it.
func (e *packetEmission) boundedPathTurns(now monotime.Time, confirmed bool) (emissionResult, pathTurn) {
	if sent, err := e.pathProbeOpportunity(now); err != nil || sent {
		return emissionResult{err: err, retry: err == nil}, pathTurnSignaling
	}
	e.selectRoute(now)
	if m := e.pathCredit.nextAnnouncement(now); m != nil {
		route := e.pathCredit.routeOf(m.id)
		size := e.policy.maxPacketSize()
		if route != nil {
			size = route.mtu.CurrentSize()
		}
		if !e.reserveOn(e.policyOn(route), size, false, false, now) {
			// Like a full endpoint, local credit is no reason to hold the
			// selected route's work; the announcement retries after backoff.
			e.pathCredit.noteAnnounced(m, now)
		} else {
			if sent, err := e.announcePath(now); err != nil || sent {
				return emissionResult{err: err, retry: err == nil}, pathTurnSignaling
			}
			e.reservation.completeLocal()
			e.reservation = nil
		}
	}
	if e.held {
		if !e.reserveLocal(e.packetSize(), false, false, now) {
			return e.localBlocked(), pathTurnHeld
		}
		return e.heldOpportunity(now, confirmed), pathTurnHeld
	}
	return emissionResult{}, pathTurnNone
}

func (e *packetEmission) resetLocalPath(generation uint64, now monotime.Time) {
	if e.bbr == nil {
		return
	}
	e.bbr.resetGeneration(generation, now)
}

func (p *bbrSendPolicy) resetGeneration(generation uint64, now monotime.Time) {
	p.credit.resetGeneration(generation)
	p.tokens = int64(p.quantum) * 1e9
	p.updated, p.refilledAt = now, now
	// An open supply gap belongs to the old path's samples.
	p.supplyGap = congestion.SendUnknown
}

// sendBounded performs at most one owned handoff per opportunity. Continuation
// goes through the connection loop, which services receive, timer and close
// events on every pass.
// ACK/PTO exemptions bypass only ordinary pacing, never local byte ownership.
func (e *packetEmission) sendBounded(now monotime.Time, confirmed bool) (result emissionResult) {
	e.inOpportunity = true
	defer func() { e.inOpportunity, e.capsTaken = false, false }()
	turn := pathTurnNone
	defer func() {
		e.reservation.completeLocal() // unused construction allowance / packing failure
		e.reservation = nil
		if result.err == nil {
			// The opportunity's route, before capacity selects again.
			route := e.route
			if available := e.capacity(); available != nil {
				result.stop, result.available, result.deadline = emissionQueueFull, available, 0
				result.blocked, result.retry = blockModeHardBlocked, false
			}
			// A signaling turn says nothing about any route's supply: the probe
			// or announcement can leave on a route other than the selected one.
			if turn != pathTurnSignaling {
				e.observeDeliveryResult(route, result, now)
			}
		}
	}()
	if e.queue.WouldBlock() {
		return emissionResult{stop: emissionQueueFull, available: e.queue.Available(), blocked: blockModeHardBlocked}
	}
	if !confirmed {
		e.policy.applyHandshakeMTUFallback()
	}
	if e.pathCredit != nil {
		var turnResult emissionResult
		if turnResult, turn = e.boundedPathTurns(now, confirmed); turn != pathTurnNone {
			return turnResult
		}
		// Selection can move the opportunity off the route whose native
		// construction capacity the first check reserved, so take it again.
		if e.queue.WouldBlock() {
			return emissionResult{stop: emissionQueueFull, available: e.queue.Available(), blocked: blockModeHardBlocked}
		}
	}
	if intent := e.policy.preparePacketSizeRecovery(now); intent.probe != nil {
		return e.packetSizeRecovery(intent, now)
	}
	// Emission selects the chosen route's policy; it never chooses the route.
	p := e.routePolicy()
	recovery := e.selected().(boundedRecovery)
	size := e.packetSize()
	if b := p.controller; b != nil {
		if b.GetCongestionWindow() == 0 {
			return emissionResult{stop: emissionHardBlocked, blocked: blockModeHardBlocked}
		}
		recovery.PrepareBBRSend(now)
		b.SetMaxDatagramSize(size)
		p.update(b.PacingRate(), size, now)
	}
	// An MTU change changes future quantization, never historical pending bytes.
	p.budget(now)
	p.quantum = max(2*size, protocol.ByteCount(min(uint64(65536), p.rate/1000)))
	p.tokens = min(p.tokens, int64(p.quantum)*1e9)
	mode, allowance := recovery.SendAllowance(now)
	switch mode {
	case ackhandler.SendNone:
		return emissionResult{stop: emissionHardBlocked, blocked: blockModeHardBlocked}
	case ackhandler.SendAck:
		if !e.reserveLocal(size, false, false, now) {
			return e.localBlocked()
		}
		progress, err := e.maybeSendAckOnlyPacket(now, confirmed)
		return emissionResult{progress: progress, err: err, stop: emissionCongestionLimited, blocked: blockModeCongestionLimited}
	case ackhandler.SendPTOInitial, ackhandler.SendPTOHandshake, ackhandler.SendPTOAppData:
		if !e.reserveLocal(size, false, false, now) {
			return e.localBlocked()
		}
		err := e.sendProbePacket(mode, now)
		return emissionResult{progress: err == nil, retry: err == nil, err: err}
	case ackhandler.SendAny:
		// Continue below, preserving recovery's flight and anti-amplification gates.
	case ackhandler.SendPacingLimited:
		panic("invalid unpaced recovery mode")
	}
	intent := e.policy.prepareEmission(now)
	if intent.transport != nil {
		err := e.clientProbe(intent.connID, intent.frame, intent.transport, intent.addr, now)
		return emissionResult{err: err, retry: err == nil}
	}
	if intent.mtu != nil {
		if deadline := p.deadline(min(intent.mtu.probeSize(), p.quantum), now); deadline != 0 {
			if !e.reserveLocal(size, false, false, now) {
				return e.localBlocked()
			}
			progress, err := e.maybeSendAckOnlyPacket(now, confirmed)
			return emissionResult{progress: progress, err: err, stop: emissionPaced, deadline: deadline}
		}
		if !e.reserveLocal(intent.mtu.probeSize(), false, true, now) {
			return e.waitForReservation(now, confirmed, size, intent.mtu.probeSize(), false, true)
		}
		result := e.mtuProbe(intent.mtu, now)
		result.retry = result.progress
		return result
	}
	if allowance < size {
		// ACKs retain their exemption when the remaining window cannot fit M.
		if !e.reserveLocal(size, false, false, now) {
			return e.localBlocked()
		}
		progress, err := e.maybeSendAckOnlyPacket(now, confirmed)
		return emissionResult{progress: progress, err: err, stop: emissionCongestionLimited, blocked: blockModeCongestionLimited}
	}
	if deadline := p.deadline(size, now); deadline != 0 {
		if !e.reserveLocal(size, false, false, now) {
			return e.localBlocked()
		}
		progress, err := e.maybeSendAckOnlyPacket(now, confirmed)
		return emissionResult{progress: progress, err: err, stop: emissionPaced, deadline: deadline}
	}
	e.confirmSupplyGap(now)
	if e.steered {
		// The steered DATAGRAM leaves alone, so no stream data follows it off
		// the primary; selection then runs again for whatever is next.
		if !e.reserveLocal(size, true, false, now) {
			return e.waitForReservation(now, confirmed, size, size, true, false)
		}
		result := e.steeredDatagram(now)
		result.retry = result.progress
		return result
	}
	limit := size
	if confirmed && e.boundedGSO() {
		limit = min(p.budget(now), allowance, gsoPacketSendLimit(size))
		limit -= limit % size
	}
	if !e.reserveLocal(limit, true, false, now) {
		return e.waitForReservation(now, confirmed, size, limit, true, false)
	}
	if !confirmed {
		result := e.coalesced(now)
		result.deadline = 0 // legacy recovery pacing does not govern this policy
		result.retry = result.progress
		return result
	}
	return e.boundedDatagrams(now, size, limit)
}

func (e *packetEmission) boundedDatagrams(now monotime.Time, size, limit protocol.ByteCount) emissionResult {
	exhausted := false
	gso := e.boundedGSO()
	buf := getPacketBufferForCeiling(packetCeiling(size))
	if gso {
		buf.Release()
		buf = getGSOPacketBuffer(size)
	}
	if !e.reserveNativeBuffer(buf) {
		buf.Release()
		return emissionResult{stop: emissionQueueFull, available: e.queue.Available()}
	}
	var protection nativeProtectionConstruction
	protection.begin(e, buf)
	recovery := e.selected()
	ecn := recovery.ECNMode(true)
	// The reservation covers whole maximum-size packets; handoff resizes it to
	// the physical bytes, so shorter segments release the remainder.
	batch := newGSOBatch(size, e.shortSegments)
	for {
		next, datagramOnly := batch.next()
		receipt, err := e.appendPacketProtected(buf, next, datagramOnly, ecn, now, &protection)
		if err != nil {
			if err != errNothingToPack || buf.Len() == 0 {
				protection.discard()
				buf.Release()
				if err == errNothingToPack {
					err = nil
					exhausted = true
				}
				return emissionResult{err: err, supplyExhausted: exhausted}
			}
			// A DATAGRAM-only continuation that finds no DATAGRAM of the
			// segment size only ends the batch, like a short packet does.
			exhausted = !datagramOnly
			break
		}
		more := batch.add(receipt.size, receipt.frames)
		mode, allowance := recovery.(boundedRecovery).SendAllowance(now)
		if !e.localPacketRoom() || !gso || !more || !batch.fits(buf.Len(), limit) || mode != ackhandler.SendAny || allowance < size || recovery.ECNMode(true) != ecn {
			break
		}
	}
	e.handoff(buf, batch.gsoSize(), ecn, sendMetadata{}, &protection)
	// Continue as Reno's receive-yield stop does: the connection makes its next
	// full pass (close, receive, timers, capacity) without a scheduling token,
	// timer re-arm or select between consecutive datagrams.
	return emissionResult{progress: true, deadline: deadlineSendImmediately, supplyExhausted: exhausted}
}

// opportunityCapabilities queries the connection once per BBR opportunity and
// reuses that snapshot within it; a concurrent change (a GSO error, managed ECN
// or receive state, lease DF) is seen by the next opportunity. Outside an
// opportunity it queries live.
func (e *packetEmission) opportunityCapabilities() connCapabilities {
	if !e.inOpportunity {
		return (*e.conn).capabilities()
	}
	if !e.capsTaken {
		e.caps, e.capsTaken = (*e.conn).capabilities(), true
	}
	return e.caps
}

// A refused ordinary or isolated-probe request must not suppress control
// traffic or its timers. If no ACK is due, rearm under the completion lock after
// releasing the unused ACK reservation; otherwise that release would spin the
// connection on its own stale wakeup. A concurrent completion cannot be lost.
func (e *packetEmission) waitForReservation(now monotime.Time, confirmed bool, size, requested protocol.ByteCount, ordinary, isolated bool) emissionResult {
	if !e.reserveLocal(size, false, false, now) {
		return e.localBlocked()
	}
	progress, err := e.maybeSendAckOnlyPacket(now, confirmed)
	if err != nil || progress {
		return emissionResult{progress: progress, retry: progress, err: err}
	}
	e.reservation.completeLocal()
	e.reservation = nil
	p := e.routePolicy()
	result := e.localBlockedOn(p)
	if p.credit.waitForReservation(requested, 2*p.quantum, size, ordinary, isolated) {
		result.blocked = blockModeCongestionLimited
	}
	return result
}
