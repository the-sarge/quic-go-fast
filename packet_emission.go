package quic

import (
	"fmt"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

// packetEmission owns ordinary/GSO, handshake, ACK and PTO construction,
// registration and queue transfer. Typed slots borrow the single path state;
// probes and close retain their bounded connection-owned paths.
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
	logCoalescedPacket(*coalescedPacket, protocol.ECN)
	noteFirstEmission()
	noteCoalescedActivity(bool, monotime.Time)
	noteCoalescedRegistration(protocol.EncryptionLevel, monotime.Time)
	coalescedSendMetadata(bool) sendMetadata
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
	emissionSendAny
	emissionProbeSent
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

// Preserve the recovery owner's distinction between ordinary batches.
func emissionRecoveryStop(mode ackhandler.SendMode) emissionStop {
	//nolint:exhaustive // SendAny and pacing are consumed by the loops; remaining modes request PTO dispatch on the next opportunity.
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

// dispatch interprets recovery's opportunity before any destructive packing.
// SendAny returns to the connection's bounded path/control prelude. A completed
// PTO returns for synchronous connection-owned feedback before the next mode.
func (e *packetEmission) dispatch(now monotime.Time, confirmed bool) emissionResult {
	mode := (*e.recovery).SendMode(now)
	if mode != ackhandler.SendAny && (*e.queue).WouldBlock() {
		return emissionResult{stop: emissionQueueFull, available: (*e.queue).Available()}
	}
	switch mode {
	case ackhandler.SendAny:
		return emissionResult{stop: emissionSendAny}
	case ackhandler.SendNone:
		return emissionResult{stop: emissionHardBlocked}
	case ackhandler.SendAck, ackhandler.SendPacingLimited:
		result := emissionResult{stop: emissionCongestionLimited}
		if mode == ackhandler.SendPacingLimited {
			result.stop, result.deadline = emissionPaced, e.pacingDeadline()
		}
		result.progress, result.err = e.maybeSendAckOnlyPacket(now, confirmed)
		return result
	case ackhandler.SendPTOInitial, ackhandler.SendPTOHandshake, ackhandler.SendPTOAppData:
		if err := e.sendProbePacket(mode, now); err != nil {
			return emissionResult{err: err}
		}
		if (*e.queue).WouldBlock() {
			return emissionResult{progress: true, stop: emissionQueueFull, available: (*e.queue).Available()}
		}
		return emissionResult{progress: true, stop: emissionProbeSent}
	default:
		return emissionResult{err: fmt.Errorf("BUG: invalid send mode %d", mode)}
	}
}

func (e *packetEmission) coalesced(now monotime.Time) emissionResult {
	if (*e.queue).WouldBlock() {
		return emissionResult{stop: emissionQueueFull, available: (*e.queue).Available()}
	}
	packet, err := (*e.packer).PackCoalescedPacket(false, e.policy.maxPacketSize(), now, e.version)
	if err != nil || packet == nil {
		return emissionResult{err: err}
	}
	e.policy.noteFirstEmission()
	e.sendCoalesced(packet, (*e.recovery).ECNMode(packet.IsOnlyShortHeaderPacket()), now)
	result := emissionResult{progress: true}
	//nolint:exhaustive // Preserve only the handshake flight's pacing/immediate-retry policy.
	switch (*e.recovery).SendMode(now) {
	case ackhandler.SendPacingLimited:
		result.stop, result.deadline = emissionPaced, e.pacingDeadline()
	case ackhandler.SendAny:
		result.deadline = deadlineSendImmediately
	}
	return result
}

func (e *packetEmission) maybeSendAckOnlyPacket(now monotime.Time, confirmed bool) (bool, error) {
	if !confirmed {
		ecn := (*e.recovery).ECNMode(false)
		packet, err := (*e.packer).PackCoalescedPacket(true, e.policy.maxPacketSize(), now, e.version)
		if err != nil {
			return false, err
		}
		if packet == nil {
			return false, nil
		}
		e.sendCoalesced(packet, ecn, now)
		return true, nil
	}

	ecn := (*e.recovery).ECNMode(true)
	p, buf, err := (*e.packer).PackAckOnlyPacket(e.policy.maxPacketSize(), now, e.version)
	if err != nil {
		if err == errNothingToPack {
			return false, nil
		}
		return false, err
	}
	e.policy.logShortHeaderPacket(p, ecn, buf.Len())
	e.registerPacket(p, ecn, now)
	(*e.queue).Send(buf, 0, ecn, sendMetadata{})
	return true, nil
}

func (e *packetEmission) sendProbePacket(sendMode ackhandler.SendMode, now monotime.Time) error {
	var encLevel protocol.EncryptionLevel
	//nolint:exhaustive // We only need to handle the PTO send modes here.
	switch sendMode {
	case ackhandler.SendPTOInitial:
		encLevel = protocol.EncryptionInitial
	case ackhandler.SendPTOHandshake:
		encLevel = protocol.EncryptionHandshake
	case ackhandler.SendPTOAppData:
		encLevel = protocol.Encryption1RTT
	default:
		return fmt.Errorf("connection BUG: unexpected send mode: %d", sendMode)
	}
	// Queue probe packets until we actually send out a packet,
	// or until there are no more packets to queue.
	var packet *coalescedPacket
	for packet == nil {
		if wasQueued := (*e.recovery).QueueProbePacket(encLevel); !wasQueued {
			break
		}
		var err error
		packet, err = (*e.packer).PackPTOProbePacket(encLevel, e.policy.maxPacketSize(), false, now, e.version)
		if err != nil {
			return err
		}
	}
	if packet == nil {
		var err error
		packet, err = (*e.packer).PackPTOProbePacket(encLevel, e.policy.maxPacketSize(), true, now, e.version)
		if err != nil {
			return err
		}
	}
	if packet == nil || (len(packet.longHdrPackets) == 0 && packet.shortHdrPacket == nil) {
		return fmt.Errorf("connection BUG: couldn't pack %s probe packet: %v", encLevel, packet)
	}
	e.sendCoalesced(packet, (*e.recovery).ECNMode(packet.IsOnlyShortHeaderPacket()), now)
	return nil
}

func (e *packetEmission) sendCoalesced(packet *coalescedPacket, ecn protocol.ECN, now monotime.Time) {
	e.policy.logCoalescedPacket(packet, ecn)
	var hasHandshakePacket bool
	for _, p := range packet.longHdrPackets {
		if p.EncryptionLevel() == protocol.EncryptionInitial || p.EncryptionLevel() == protocol.EncryptionHandshake {
			hasHandshakePacket = true
		}
		e.policy.noteCoalescedActivity(p.IsAckEliciting(), now)
		largestAcked := protocol.InvalidPacketNumber
		if p.ack != nil {
			largestAcked = p.ack.LargestAcked()
		}
		(*e.recovery).SentPacket(
			now,
			p.header.PacketNumber,
			largestAcked,
			p.streamFrames,
			p.frames,
			p.EncryptionLevel(),
			ecn,
			p.length,
			false,
			false,
		)
		e.policy.noteCoalescedRegistration(p.EncryptionLevel(), now)
	}
	if p := packet.shortHdrPacket; p != nil {
		e.policy.noteCoalescedActivity(p.IsAckEliciting(), now)
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
	}
	e.policy.noteEmissionRegistration()
	(*e.queue).Send(packet.buffer, 0, ecn, e.policy.coalescedSendMetadata(hasHandshakePacket))
}
