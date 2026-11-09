package quic

import (
	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

// withSoftwareSegments changes physical storage, not the off-path packet turn.
// Every packet retains its capacity -> SendMode -> receive boundary; only the
// already available full-size prefix can share a member. Short packets flush.
func (e *packetEmission) withSoftwareSegments(now monotime.Time) (result emissionResult) {
	var buf *packetBuffer
	var protection nativeProtectionConstruction
	var batch gsoBatch
	var bufferECN protocol.ECN
	flush := func() {
		if buf == nil {
			return
		}
		if buf.Len() == 0 {
			protection.discard()
			buf.Release()
		} else {
			e.handoff(buf, batch.gsoSize(), bufferECN, sendMetadata{}, &protection)
			result.progress = true
		}
		buf = nil
	}
	for {
		// This pure query checks the existing physical allowance. Calling
		// WouldBlock here could finish pending before its buffer is handed off.
		if !e.local.softwarePacketRoom() {
			flush()
			result.stop, result.available = emissionQueueFull, e.queue.Available()
			return result
		}
		size := e.packetSize()
		if buf == nil {
			buf = getGSOPacketBuffer(size)
			reservation := e.local.reserveBuffer(buf.Cap())
			if !reservation.available || (reservation.finished && e.local != nil && !e.local.softwarePacketRoom()) {
				buf.Release()
				result.stop, result.available = emissionQueueFull, e.queue.Available()
				return result
			}
			if reservation.finished {
				buf.Release()
				result.retry = true
				return result
			}
			protection.begin(e, buf)
			protection.synchronous()
			batch = newGSOBatch(size, false)
		}
		ecn := e.selected().ECNMode(true)
		if buf.Len() != 0 && (ecn != bufferECN || batch.maxSize != size) {
			flush()
			buf = getGSOPacketBuffer(size)
			reservation := e.local.reserveBuffer(buf.Cap())
			if !reservation.available || (reservation.finished && e.local != nil && !e.local.softwarePacketRoom()) {
				buf.Release()
				result.stop, result.available = emissionQueueFull, e.queue.Available()
				return result
			}
			if reservation.finished {
				buf.Release()
				result.retry = true
				return result
			}
			protection.begin(e, buf)
			protection.synchronous()
			batch = newGSOBatch(size, false)
		}
		bufferECN = ecn
		receipt, err := e.appendPacketProtected(buf, size, false, ecn, now, &protection)
		if err != nil {
			if err == errNothingToPack {
				flush()
				return result
			}
			protection.discard()
			buf.Release()
			result.err = err
			return result
		}
		more := batch.add(receipt.size, receipt.frames) && batch.fits(buf.Len(), gsoPacketSendLimit(size))
		resourceFull := !more && receipt.size == size // full-size stops use the existing byte/segment bound
		if !more {
			flush()
		}
		// Same stop order as withoutGSO: capacity before SendMode, then receive.
		if !e.local.softwarePacketRoom() {
			flush()
			result.stop, result.available = emissionQueueFull, e.queue.Available()
			return result
		}
		mode := e.selected().SendMode(now)
		if mode == ackhandler.SendPacingLimited {
			flush()
			result.stop, result.deadline = emissionPaced, e.pacingDeadline()
			return result
		}
		if mode != ackhandler.SendAny {
			flush()
			result.stop = emissionRecoveryStop(mode)
			return result
		}
		if e.policy.emissionReceivePending() {
			flush()
			result.stop, result.deadline = emissionReceivePending, deadlineSendImmediately
			return result
		}
		if resourceFull && e.local.finishPrefixWhenNativeEmpty() {
			// Finish can settle callbacks. Return through the current receiver;
			// no earlier packet-policy observation authorizes another pack.
			if e.local != nil && !e.local.softwarePacketRoom() {
				result.stop, result.available = emissionQueueFull, e.queue.Available()
			} else {
				result.retry = true
			}
			return result
		}
	}
}
