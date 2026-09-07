package quic

import (
	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

// packetEmission owns established ordinary/GSO construction, registration and
// queue transfer. Typed slots borrow the single path state during migration;
// handshake, probes and close retain their bounded connection-owned paths.
type packetEmission struct {
	packer   *packer
	recovery *ackhandler.SentPacketHandler
	queue    *sender
	version  protocol.Version

	policy emissionPolicy
}

// This interface exposes only the synchronous connection-owned policy effects.
// Binding the receiver once avoids allocating one method-value closure per hook.
type emissionPolicy interface {
	maxPacketSize() protocol.ByteCount
	logShortHeaderPacket(shortHeaderPacket, protocol.ECN, protocol.ByteCount)
	noteEmissionActivity(shortHeaderPacket, monotime.Time)
	noteEmissionRegistration()
	emissionReceivePending() bool
}

type emissionStop uint8

const (
	emissionNoData emissionStop = iota
	emissionQueueFull
	emissionHardBlocked
	emissionPaced
	emissionReceivePending
	emissionCongestionLimited
	emissionProbePending
	emissionLegacy
)

type emissionResult struct {
	progress  bool
	available <-chan struct{}
	stop      emissionStop
	deadline  monotime.Time
	err       error
}

// Unmigrated send paths preserve their existing post-send capacity check.
func legacyEmission(err error) emissionResult {
	return emissionResult{stop: emissionLegacy, err: err}
}

func (e *packetEmission) advance(now monotime.Time, gso bool) emissionResult {
	if (*e.queue).WouldBlock() {
		return emissionResult{stop: emissionQueueFull, available: (*e.queue).Available()}
	}
	if gso {
		return e.withGSO(now)
	}
	return e.withoutGSO(now)
}

// Preserve the recovery owner's distinction without moving ACK/PTO dispatch.
func emissionRecoveryStop(mode ackhandler.SendMode) emissionStop {
	switch mode {
	case ackhandler.SendNone:
		return emissionHardBlocked
	case ackhandler.SendAck:
		return emissionCongestionLimited
	default:
		return emissionProbePending
	}
}

func (e *packetEmission) pacingDeadline() monotime.Time {
	deadline := (*e.recovery).TimeUntilSend()
	if deadline.IsZero() {
		return deadlineSendImmediately
	}
	return deadline
}

func (e *packetEmission) appendPacket(buf *packetBuffer, maxSize protocol.ByteCount, ecn protocol.ECN, now monotime.Time) (protocol.ByteCount, error) {
	start := buf.Len()
	p, err := (*e.packer).AppendPacket(buf, maxSize, now, e.version)
	if err != nil {
		return 0, err
	}
	size := buf.Len() - start
	e.policy.logShortHeaderPacket(p, ecn, size)
	e.registerPacket(p, ecn, now)
	return size, nil
}

func (e *packetEmission) withoutGSO(now monotime.Time) (result emissionResult) {
	for {
		buf := getPacketBuffer()
		ecn := (*e.recovery).ECNMode(true)
		if _, err := e.appendPacket(buf, e.policy.maxPacketSize(), ecn, now); err != nil {
			if err == errNothingToPack {
				buf.Release()
				return result
			}
			buf.Release()
			result.err = err // Storage is reclaimed; protocol registration is not refunded.
			return result
		}
		(*e.queue).Send(buf, 0, ecn, sendMetadata{})
		result.progress = true
		if (*e.queue).WouldBlock() {
			result.stop, result.available = emissionQueueFull, (*e.queue).Available()
			return result
		}
		mode := (*e.recovery).SendMode(now)
		if mode == ackhandler.SendPacingLimited {
			result.stop, result.deadline = emissionPaced, e.pacingDeadline()
			return result
		}
		if mode != ackhandler.SendAny {
			result.stop = emissionRecoveryStop(mode)
			return result
		}
		if e.policy.emissionReceivePending() {
			result.stop, result.deadline = emissionReceivePending, deadlineSendImmediately
			return result
		}
	}
}

func (e *packetEmission) withGSO(now monotime.Time) (result emissionResult) {
	buf := getLargePacketBuffer()
	maxSize := e.policy.maxPacketSize()
	ecn := (*e.recovery).ECNMode(true)
	for {
		var done bool
		size, err := e.appendPacket(buf, maxSize, ecn, now)
		if err != nil {
			if err != errNothingToPack {
				buf.Release()
				result.err = err
				return result
			}
			if buf.Len() == 0 {
				buf.Release()
				return result
			}
			done = true
		}
		if !done {
			mode := (*e.recovery).SendMode(now)
			if mode == ackhandler.SendPacingLimited {
				result.stop, result.deadline = emissionPaced, e.pacingDeadline()
			} else if mode != ackhandler.SendAny {
				result.stop = emissionRecoveryStop(mode)
			}
			done = mode != ackhandler.SendAny
		}
		nextECN := (*e.recovery).ECNMode(true)
		if !done && size == maxSize && nextECN == ecn && buf.Len()+maxSize <= buf.Cap() {
			continue
		}
		(*e.queue).Send(buf, uint16(maxSize), ecn, sendMetadata{})
		result.progress = true
		if done {
			return result
		}
		if (*e.queue).WouldBlock() {
			result.stop, result.available = emissionQueueFull, (*e.queue).Available()
			return result
		}
		if e.policy.emissionReceivePending() {
			result.stop, result.deadline = emissionReceivePending, deadlineSendImmediately
			return result
		}
		ecn = nextECN
		buf = getLargePacketBuffer()
	}
}

func (e *packetEmission) registerPacket(p shortHeaderPacket, ecn protocol.ECN, now monotime.Time) {
	if p.IsPathProbePacket {
		(*e.recovery).SentPacket(
			now,
			p.PacketNumber,
			protocol.InvalidPacketNumber,
			p.StreamFrames,
			p.Frames,
			protocol.Encryption1RTT,
			ecn,
			p.Length,
			p.IsPathMTUProbePacket,
			true,
		)
		return
	}
	e.policy.noteEmissionActivity(p, now)

	largestAcked := protocol.InvalidPacketNumber
	if p.Ack != nil {
		largestAcked = p.Ack.LargestAcked()
	}
	(*e.recovery).SentPacket(
		now,
		p.PacketNumber,
		largestAcked,
		p.StreamFrames,
		p.Frames,
		protocol.Encryption1RTT,
		ecn,
		p.Length,
		p.IsPathMTUProbePacket,
		false,
	)
	e.policy.noteEmissionRegistration()
}
