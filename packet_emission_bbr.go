package quic

import (
	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

// boundedRecovery is internal module plumbing, not a controller plugin API.
// The real recovery owner supplies every gate; emission owns byte quantization.
type boundedRecovery interface {
	PrepareBBRSend(monotime.Time)
	SendAllowance(monotime.Time) (ackhandler.SendMode, protocol.ByteCount)
}

func (e *packetEmission) handoff(buf *packetBuffer, gso uint16, ecn protocol.ECN, metadata sendMetadata) {
	if e.bbr != nil {
		if e.reservation == nil {
			panic("BBR handoff without reservation")
		}
		e.reservation.resize(buf.Len())
		if e.reservation.isolated {
			e.bbr.sentProbe(buf.Len(), e.reservationTime)
		} else if e.paceReservation {
			e.bbr.sent(e.pacingBytes, e.reservationTime)
		}
		metadata.credit = e.reservation
		metadata.pathGeneration = e.reservation.generation
		e.reservation = nil
	}
	e.queue.Send(buf, gso, ecn, metadata)
}

func (e *packetEmission) localBlocked() emissionResult {
	return emissionResult{stop: emissionQueueFull, blocked: blockModeHardBlocked, available: e.bbr.credit.available}
}

func (e *packetEmission) reserveLocal(n protocol.ByteCount, ordinary, isolated bool, now monotime.Time) bool {
	e.reservation = e.bbr.credit.reserve(n, 2*e.bbr.quantum, ordinary, isolated)
	e.paceReservation, e.reservationTime, e.pacingBytes = ordinary, now, 0
	return e.reservation != nil
}

func (e *packetEmission) resetLocalPath(generation uint64, now monotime.Time) {
	if e.bbr == nil {
		return
	}
	e.bbr.credit.resetGeneration(generation)
	e.bbr.tokens = int64(e.bbr.quantum) * 1e9
	e.bbr.updated = now
}

// sendBounded performs at most one owned handoff per opportunity. Continuation
// goes through the connection loop, which can service receive and close events.
// ACK/PTO exemptions bypass only ordinary pacing, never local byte ownership.
func (e *packetEmission) sendBounded(now monotime.Time, confirmed bool) (result emissionResult) {
	defer func() {
		e.reservation.complete() // unused construction allowance / packing failure
		e.reservation = nil
		if result.err == nil {
			if available := e.capacity(); available != nil {
				result.stop, result.available, result.deadline = emissionQueueFull, available, 0
				result.blocked, result.retry = blockModeHardBlocked, false
			}
			e.observeDeliveryResult(result)
		}
	}()
	if e.queue.WouldBlock() {
		return emissionResult{stop: emissionQueueFull, available: e.queue.Available(), blocked: blockModeHardBlocked}
	}
	if !confirmed {
		e.policy.applyHandshakeMTUFallback()
	}
	size := e.policy.maxPacketSize()
	if b := e.bbr.controller; b != nil {
		if b.GetCongestionWindow() == 0 {
			return emissionResult{stop: emissionHardBlocked, blocked: blockModeHardBlocked}
		}
		(*e.recovery).(boundedRecovery).PrepareBBRSend(now)
		b.SetMaxDatagramSize(size)
		e.bbr.update(b.PacingRate(), size, now)
	}
	// An MTU change changes future quantization, never historical pending bytes.
	e.bbr.budget(now)
	e.bbr.quantum = max(2*size, protocol.ByteCount(min(uint64(65536), e.bbr.rate/1000)))
	e.bbr.tokens = min(e.bbr.tokens, int64(e.bbr.quantum)*1e9)
	mode, allowance := (*e.recovery).(boundedRecovery).SendAllowance(now)
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
		if deadline := e.bbr.deadline(min(intent.mtu.probeSize(), e.bbr.quantum), now); deadline != 0 {
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
	if deadline := e.bbr.deadline(size, now); deadline != 0 {
		if !e.reserveLocal(size, false, false, now) {
			return e.localBlocked()
		}
		progress, err := e.maybeSendAckOnlyPacket(now, confirmed)
		return emissionResult{progress: progress, err: err, stop: emissionPaced, deadline: deadline}
	}
	limit := size
	if confirmed && (*e.conn).capabilities().GSO {
		limit = min(e.bbr.budget(now), allowance, protocol.MaxLargePacketBufferSize)
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
	gso := (*e.conn).capabilities().GSO
	buf := getPacketBuffer()
	if gso {
		buf.Release()
		buf = getLargePacketBuffer()
	}
	ecn := (*e.recovery).ECNMode(true)
	for {
		n, err := e.appendPacket(buf, size, ecn, now)
		if err != nil {
			if err != errNothingToPack || buf.Len() == 0 {
				buf.Release()
				if err == errNothingToPack {
					err = nil
					exhausted = true
				}
				return emissionResult{err: err, supplyExhausted: exhausted}
			}
			exhausted = true
			break
		}
		mode, allowance := (*e.recovery).(boundedRecovery).SendAllowance(now)
		if !gso || n != size || buf.Len()+size > limit || mode != ackhandler.SendAny || allowance < size || (*e.recovery).ECNMode(true) != ecn {
			break
		}
	}
	var gsoSize uint16
	if gso {
		gsoSize = uint16(size)
	}
	e.handoff(buf, gsoSize, ecn, sendMetadata{})
	return emissionResult{progress: true, retry: true, supplyExhausted: exhausted}
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
	e.reservation.complete()
	e.reservation = nil
	result := e.localBlocked()
	if e.bbr.credit.waitForReservation(requested, 2*e.bbr.quantum, size, ordinary, isolated) {
		result.blocked = blockModeCongestionLimited
	}
	return result
}
