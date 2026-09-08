package quic

import (
	"bytes"
	"errors"
	"fmt"
	"net"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/qlog"
)

// packetEmission owns packet construction, registration, output handoff and
// temporary storage. The closed handler retains an immutable close payload.
type packetEmission struct {
	packer   *packetPacker
	recovery *ackhandler.SentPacketHandler
	queue    sender
	version  protocol.Version

	policy emissionPolicy
	conn   *sendConn
}

// This interface exposes only the synchronous connection-owned policy effects.
// Binding the receiver once avoids allocating one method-value closure per hook.
type emissionPolicy interface {
	applyHandshakeMTUFallback()
	prepareEmission(monotime.Time) emissionIntent
	maxPacketSize() protocol.ByteCount
	logPathProbe(shortHeaderPacket, net.Addr, protocol.ByteCount, qlog.DatagramPayloadChecksum)
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
	emissionSendAny
	emissionProbeSent
)

type emissionResult struct {
	progress  bool
	stop      emissionStop
	blocked   blockMode
	retry     bool
	available <-chan struct{}
	deadline  monotime.Time
	err       error
}

type emissionIntent struct {
	transport *Transport
	connID    protocol.ConnectionID
	frame     ackhandler.Frame
	addr      net.Addr
	mtu       *mtuFinder
}

func (e *packetEmission) setToken(token []byte) { e.packer.SetToken(token) }
func (e *packetEmission) run() error            { return e.queue.Run() }
func (e *packetEmission) drain()                { e.queue.Close() }
func (e *packetEmission) capacity() <-chan struct{} {
	if e.queue.WouldBlock() {
		return e.queue.Available()
	}
	return nil
}

// Finish capacity accounting here, including ACK, coalesced and final GSO batches.
func (e *packetEmission) finish(result emissionResult) emissionResult {
	if result.err == nil && (result.progress || result.stop == emissionQueueFull) {
		result.available = nil
		if available := e.capacity(); available != nil {
			result.stop, result.available, result.deadline = emissionQueueFull, available, 0
			result.blocked = blockModeHardBlocked
		}
	}
	return result
}

func (e *packetEmission) send(now monotime.Time, confirmed bool) (result emissionResult) {
	defer func() { result = e.finish(result) }()
	progress := false
	for {
		if !confirmed {
			e.policy.applyHandshakeMTUFallback()
		}
		result = e.dispatch(now, confirmed)
		if result.err != nil {
			return result
		}
		switch result.stop {
		case emissionSendAny:
			result = e.sendAny(now, confirmed)
			result.progress = result.progress || progress
			return result
		case emissionProbeSent:
			progress = true
			continue
		case emissionQueueFull:
			result.retry = result.progress
		case emissionHardBlocked:
			result.blocked = blockModeHardBlocked
		case emissionCongestionLimited:
			result.blocked = blockModeCongestionLimited
		case emissionPaced, emissionNoData, emissionReceivePending, emissionProbePending:
		}
		result.progress = result.progress || progress
		return result
	}
}

func (e *packetEmission) sendAny(now monotime.Time, confirmed bool) emissionResult {
	intent := e.policy.prepareEmission(now)
	if intent.transport != nil {
		err := e.clientProbe(intent.connID, intent.frame, intent.transport, intent.addr, now)
		return emissionResult{err: err, retry: err == nil}
	}
	if intent.mtu != nil {
		result := e.mtuProbe(intent.mtu, now)
		result.retry = result.progress
		return result
	}
	if !confirmed {
		return e.coalesced(now)
	}
	return e.advance(now, (*e.conn).capabilities().GSO)
}

func (e *packetEmission) advance(now monotime.Time, gso bool) emissionResult {
	if e.queue.WouldBlock() {
		return emissionResult{stop: emissionQueueFull, available: e.queue.Available()}
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
	p, err := e.packer.AppendPacket(buf, maxSize, now, e.version)
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
		e.queue.Send(buf, 0, ecn, sendMetadata{})
		result.progress = true
		if e.queue.WouldBlock() {
			result.stop, result.available = emissionQueueFull, e.queue.Available()
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
		e.queue.Send(buf, uint16(maxSize), ecn, sendMetadata{})
		result.progress = true
		if done {
			return result
		}
		if e.queue.WouldBlock() {
			result.stop, result.available = emissionQueueFull, e.queue.Available()
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
// The module consumes SendAny and PTO continuation internally.
func (e *packetEmission) dispatch(now monotime.Time, confirmed bool) emissionResult {
	mode := (*e.recovery).SendMode(now)
	if mode != ackhandler.SendAny && e.queue.WouldBlock() {
		return emissionResult{stop: emissionQueueFull, available: e.queue.Available()}
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
		if e.queue.WouldBlock() {
			return emissionResult{progress: true, stop: emissionQueueFull, available: e.queue.Available()}
		}
		return emissionResult{progress: true, stop: emissionProbeSent}
	default:
		return emissionResult{err: fmt.Errorf("BUG: invalid send mode %d", mode)}
	}
}

func (e *packetEmission) coalesced(now monotime.Time) emissionResult {
	if e.queue.WouldBlock() {
		return emissionResult{stop: emissionQueueFull, available: e.queue.Available()}
	}
	packet, err := e.packer.PackCoalescedPacket(false, e.policy.maxPacketSize(), now, e.version)
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
		packet, err := e.packer.PackCoalescedPacket(true, e.policy.maxPacketSize(), now, e.version)
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
	p, buf, err := e.packer.PackAckOnlyPacket(e.policy.maxPacketSize(), now, e.version)
	if err != nil {
		if err == errNothingToPack {
			return false, nil
		}
		return false, err
	}
	e.policy.logShortHeaderPacket(p, ecn, buf.Len())
	e.registerPacket(p, ecn, now)
	e.queue.Send(buf, 0, ecn, sendMetadata{})
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
		packet, err = e.packer.PackPTOProbePacket(encLevel, e.policy.maxPacketSize(), false, now, e.version)
		if err != nil {
			return err
		}
	}
	if packet == nil {
		var err error
		packet, err = e.packer.PackPTOProbePacket(encLevel, e.policy.maxPacketSize(), true, now, e.version)
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
	e.queue.Send(packet.buffer, 0, ecn, e.policy.coalescedSendMetadata(hasHandshakePacket))
}

// Direct operations borrow the destination and preserve best-effort write errors.
// Storage belongs to emission until the synchronous write has returned.
func (e *packetEmission) serverProbe(connID protocol.ConnectionID, frames []ackhandler.Frame, addr net.Addr, info packetInfo, checksum qlog.DatagramPayloadChecksum, now monotime.Time) error {
	p, buf, err := e.packer.PackPathProbePacket(connID, frames, e.version)
	if err != nil {
		return err
	}
	defer buf.Release()
	e.policy.logPathProbe(p, addr, buf.Len(), checksum)
	e.registerPacket(p, protocol.ECNNon, now)
	e.queue.SendProbe(buf, addr, info)
	return nil
}

func (e *packetEmission) clientProbe(connID protocol.ConnectionID, frame ackhandler.Frame, tr *Transport, addr net.Addr, now monotime.Time) error {
	p, buf, err := e.packer.PackPathProbePacket(connID, []ackhandler.Frame{frame}, e.version)
	if err != nil {
		return err
	}
	defer buf.Release()
	e.policy.logPathProbe(p, nil, buf.Len(), 0)
	e.registerPacket(p, protocol.ECNNon, now)
	tr.WriteTo(buf.Data, addr)
	return nil
}

func (e *packetEmission) mtuProbe(finder *mtuFinder, now monotime.Time) emissionResult {
	if e.queue.WouldBlock() {
		return emissionResult{stop: emissionQueueFull, available: e.queue.Available()}
	}
	ping, size := finder.GetPing(now)
	p, buf, err := e.packer.PackMTUProbePacket(ping, size, e.version)
	if err != nil {
		return emissionResult{err: err}
	}
	ecn := (*e.recovery).ECNMode(true)
	e.policy.logShortHeaderPacket(p, ecn, buf.Len())
	e.registerPacket(p, ecn, now)
	e.queue.Send(buf, 0, ecn, sendMetadata{})
	return emissionResult{progress: true}
}

// The connection still starts workers and owns failure/lifecycle policy. Join
// the previous worker before publishing the replacement to the emission slot.
func (e *packetEmission) replacePath(conn sendConn, feedback *handshakeSendFeedback) sender {
	*e.conn = conn
	e.queue.Close()
	queue := newSendQueue(conn, feedback)
	e.queue = queue
	return queue
}

// In-place rebinding deliberately retains the active queue: pending writes use
// the destination current at their syscall, as before.
func (e *packetEmission) rebindPath(addr net.Addr, info packetInfo) {
	(*e.conn).ChangeRemoteAddr(addr, info)
}

func (e *packetEmission) close(cause error) ([]byte, error) {
	var packet *coalescedPacket
	var err error
	if transportErr, ok := errors.AsType[*qerr.TransportError](cause); ok {
		packet, err = e.packer.PackConnectionClose(transportErr, e.policy.maxPacketSize(), e.version)
	} else if applicationErr, ok := errors.AsType[*qerr.ApplicationError](cause); ok {
		packet, err = e.packer.PackApplicationClose(applicationErr, e.policy.maxPacketSize(), e.version)
	} else {
		packet, err = e.packer.PackConnectionClose(&qerr.TransportError{
			ErrorCode:    qerr.InternalError,
			ErrorMessage: fmt.Sprintf("connection BUG: unspecified error type (msg: %s)", cause.Error()),
		}, e.policy.maxPacketSize(), e.version)
	}
	if err != nil {
		return nil, err
	}
	ecn := (*e.recovery).ECNMode(packet.IsOnlyShortHeaderPacket())
	e.policy.logCoalescedPacket(packet, ecn)
	defer packet.buffer.Release()
	retained := bytes.Clone(packet.buffer.Data)
	return retained, (*e.conn).Write(packet.buffer.Data, 0, ecn)
}
