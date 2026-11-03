package quic

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"slices"
	"sync"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/qlog"
)

// packetEmission owns packet construction, registration, output handoff and
// temporary storage. The closed handler retains an immutable close payload.
type packetEmission struct {
	pathCredit       *pathCreditRegistry
	terminalReceipts [2]*pathAbandonReceipt
	bbr              *bbrSendPolicy
	reservation      *sendReservation // connection-owned until handoff
	paceReservation  bool
	pacingBytes      protocol.ByteCount
	reservationTime  monotime.Time
	// One capability snapshot per BBR opportunity, taken on first use.
	inOpportunity, capsTaken bool
	caps                     connCapabilities

	packer   *packetPacker
	recovery *ackhandler.SentPacketHandler
	queue    sender
	local    localPacketSender
	version  protocol.Version

	policy emissionPolicy
	conn   *sendConn
	// shortSegments lets a short DATAGRAM packet set a segmented send's
	// segment size (see gsoBatch); fixed at construction.
	shortSegments bool
	// authority gates queued work and the close submission; nil is ungrouped.
	authority *connAuthority
	// sendQoS is the connection's send thread QoS, bound to every queue; nil
	// is off.
	sendQoS *sendThreadQoS

	// The connection goroutine is the only writer of *conn. It may read the
	// slot directly; application inspection must take a snapshot via activeConn.
	// This lock publishes both the interface and the initialized replacement.
	connMutex sync.Mutex
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
	supplyExhausted bool // set only by an otherwise-permitted ordinary pack
	progress        bool
	stop            emissionStop
	blocked         blockMode
	retry           bool
	available       <-chan struct{}
	deadline        monotime.Time
	err             error
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
	if e.local != nil {
		defer e.local.finish()
	}
	if e.bbr != nil {
		return e.sendBounded(now, confirmed)
	}
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
	if e.pathCredit != nil && e.pathCredit.needsReceipt() {
		gso = false // no accepted-prefix ambiguity for terminal control datagrams
	}
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

// appendPacket packs, logs and registers one packet. With datagramOnly, the
// packet carries only the next queued DATAGRAM frame, and errNothingToPack
// leaves a frame that doesn't fit queued.
func (e *packetEmission) appendPacket(buf *packetBuffer, maxSize protocol.ByteCount, datagramOnly bool, ecn protocol.ECN, now monotime.Time) (shortHeaderPacket, protocol.ByteCount, error) {
	start := buf.Len()
	var p shortHeaderPacket
	var err error
	if datagramOnly {
		p, err = e.packer.AppendDatagramPacket(buf, maxSize, e.version)
	} else {
		p, err = e.packer.AppendPacket(buf, maxSize, now, e.version)
	}
	if err != nil {
		return shortHeaderPacket{}, 0, err
	}
	size := buf.Len() - start
	if e.bbr != nil && e.paceReservation && (p.IsAckEliciting() || len(p.StreamFrames) != 0) {
		e.pacingBytes += size
	}
	e.policy.logShortHeaderPacket(p, ecn, size)
	if err := e.registerPacket(p, ecn, now); err != nil {
		return shortHeaderPacket{}, 0, err
	}
	return p, size, nil
}

func (e *packetEmission) withoutGSO(now monotime.Time) (result emissionResult) {
	for {
		buf := getPacketBuffer()
		ecn := (*e.recovery).ECNMode(true)
		if _, _, err := e.appendPacket(buf, e.policy.maxPacketSize(), false, ecn, now); err != nil {
			if err == errNothingToPack {
				buf.Release()
				return result
			}
			buf.Release()
			result.err = err // Storage is reclaimed; protocol registration is not refunded.
			return result
		}
		e.handoff(buf, 0, ecn, sendMetadata{})
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
	batch := newGSOBatch(maxSize, e.shortSegments)
	for {
		var done, more bool
		limit, datagramOnly := batch.next()
		p, size, err := e.appendPacket(buf, limit, datagramOnly, ecn, now)
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
			// A DATAGRAM-only continuation that finds no DATAGRAM of the segment
			// size only ends the batch; whatever is queued starts the next one.
			done = !datagramOnly
		} else {
			more = batch.add(size, p.Frames)
			mode := (*e.recovery).SendMode(now)
			if mode == ackhandler.SendPacingLimited {
				result.stop, result.deadline = emissionPaced, e.pacingDeadline()
			} else if mode != ackhandler.SendAny {
				result.stop = emissionRecoveryStop(mode)
			}
			done = mode != ackhandler.SendAny
		}
		nextECN := (*e.recovery).ECNMode(true)
		if !done && more && nextECN == ecn && batch.fits(buf.Len(), buf.Cap()) {
			continue
		}
		e.handoff(buf, batch.gsoSize(), ecn, sendMetadata{})
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
		buf, batch = getLargePacketBuffer(), newGSOBatch(maxSize, e.shortSegments)
	}
}

func (e *packetEmission) registerPacket(p shortHeaderPacket, ecn protocol.ECN, now monotime.Time) error {
	e.captureTerminalReceipts(p.Frames)
	if p.IsPathProbePacket {
		return e.register(
			p.Context,
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
	}
	e.policy.noteEmissionActivity(p, now)

	largestAcked := protocol.InvalidPacketNumber
	if p.Ack != nil {
		largestAcked = p.Ack.LargestAcked()
	}
	if err := e.register(
		p.Context,
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
	); err != nil {
		return err
	}
	e.policy.noteEmissionRegistration()
	return nil
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
		return emissionResult{err: err, supplyExhausted: err == nil}
	}
	e.policy.noteFirstEmission()
	if err := e.sendCoalesced(packet, (*e.recovery).ECNMode(packet.IsOnlyShortHeaderPacket()), now); err != nil {
		return emissionResult{err: err}
	}
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
		return true, e.sendCoalesced(packet, ecn, now)
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
	if err := e.registerPacket(p, ecn, now); err != nil {
		buf.Release()
		return false, err
	}
	e.handoff(buf, 0, ecn, sendMetadata{})
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
		var wasQueued bool
		if h, ok := (*e.recovery).(interface {
			QueueProbePacketAt(protocol.EncryptionLevel, monotime.Time) bool
		}); ok {
			wasQueued = h.QueueProbePacketAt(encLevel, now)
		} else {
			wasQueued = (*e.recovery).QueueProbePacket(encLevel)
		}
		if !wasQueued {
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
	return e.sendCoalesced(packet, (*e.recovery).ECNMode(packet.IsOnlyShortHeaderPacket()), now)
}

func (e *packetEmission) sendCoalesced(packet *coalescedPacket, ecn protocol.ECN, now monotime.Time) error {
	handed := false
	defer func() {
		if !handed {
			packet.buffer.Release()
		}
	}()
	e.policy.logCoalescedPacket(packet, ecn)
	if e.bbr != nil && e.paceReservation {
		paced := packet.shortHdrPacket != nil && (packet.shortHdrPacket.IsAckEliciting() || len(packet.shortHdrPacket.StreamFrames) != 0)
		for _, p := range packet.longHdrPackets {
			paced = paced || p.IsAckEliciting() || len(p.streamFrames) != 0
		}
		if paced {
			e.pacingBytes = packet.buffer.Len()
		}
	}
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
		if err := e.register(
			p.context,
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
		); err != nil {
			return err
		}
		e.policy.noteCoalescedRegistration(p.EncryptionLevel(), now)
	}
	if p := packet.shortHdrPacket; p != nil {
		e.captureTerminalReceipts(p.Frames)
		e.policy.noteCoalescedActivity(p.IsAckEliciting(), now)
		largestAcked := protocol.InvalidPacketNumber
		if p.Ack != nil {
			largestAcked = p.Ack.LargestAcked()
		}
		if err := e.register(
			p.Context,
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
		); err != nil {
			return err
		}
	}
	e.policy.noteEmissionRegistration()
	e.handoff(packet.buffer, 0, ecn, e.policy.coalescedSendMetadata(hasHandshakePacket))
	handed = true
	return nil
}

// Direct operations borrow the destination and preserve best-effort write errors.
// Storage belongs to emission until the synchronous write has returned. On the
// private endpoint route, every commitment still precedes the offer, but storage
// transfers to the endpoint and the connection loop never waits for the write.
func (e *packetEmission) serverProbe(connID protocol.ConnectionID, frames []ackhandler.Frame, path *path, addr net.Addr, info packetInfo, checksum qlog.DatagramPayloadChecksum, now monotime.Time) error {
	isChallenge := func(f ackhandler.Frame) bool {
		_, ok := f.Frame.(*wire.PathChallengeFrame)
		return ok
	}
	for len(frames) > 0 && (*e.recovery).CanSendPathProbe() {
		if !path.canProbe(monotime.Now()) {
			frames = slices.DeleteFunc(frames, isChallenge)
			if len(frames) == 0 {
				return nil
			}
		}
		prepared, buf, err := e.packer.preparePathProbePacket(connID, frames, path.sendAllowance(), e.version)
		if err != nil {
			return err
		}
		if buf == nil {
			if len(frames) == 2 {
				// Prefer the peer response when both frames cannot fit the allowance.
				frames = slices.DeleteFunc(frames, isChallenge)
				continue
			}
			return nil
		}
		// The packer shuffles control frames. Select by type, never by position.
		// Policy time is current processing time, not a queued receive timestamp.
		policyNow := monotime.Now()
		if slices.ContainsFunc(frames, isChallenge) && !path.canProbe(policyNow) {
			buf.Release()
			frames = slices.DeleteFunc(frames, isChallenge)
			continue
		}
		transferred := false
		defer func() {
			if !transferred {
				buf.Release()
			}
		}()
		if !path.reserveProbe(prepared.Length) {
			return nil
		}
		p, err := e.packer.commitShortHeaderPacket(buf, prepared)
		if err != nil {
			return err
		}
		for _, f := range frames {
			if challenge, ok := f.Frame.(*wire.PathChallengeFrame); ok {
				path.commitChallenge(challenge.Data, buf.Len(), policyNow)
			}
		}
		e.policy.logPathProbe(p, addr, buf.Len(), checksum)
		if err := e.registerPacket(p, protocol.ECNNon, now); err != nil {
			return err
		}
		if e.local != nil {
			transferred = true
			return e.local.sendProbe(buf, nil, addr, info, &path.probeEpoch)
		}
		return e.queue.SendProbe(buf, addr, info)
	}
	return nil
}

func (e *packetEmission) clientProbe(connID protocol.ConnectionID, frame ackhandler.Frame, tr *Transport, addr net.Addr, now monotime.Time) error {
	if !(*e.recovery).CanSendPathProbe() {
		return nil
	}
	if e.local != nil {
		// A refused target costs no packet number; the challenge stays charged.
		if ready, err := e.local.probeTargetReady(tr); !ready || err != nil {
			return err
		}
	}
	p, buf, err := e.packer.PackPathProbePacket(connID, []ackhandler.Frame{frame}, e.version)
	if err != nil {
		return err
	}
	e.policy.logPathProbe(p, nil, buf.Len(), 0)
	if err := e.registerPacket(p, protocol.ECNNon, now); err != nil {
		buf.Release()
		return err
	}
	if e.local != nil {
		var epoch *responseEpoch
		if handler, ok := frame.Handler.(*pathProbeHandler); ok {
			epoch = handler.epoch
		}
		return e.local.sendProbe(buf, tr, addr, packetInfo{}, epoch)
	}
	defer buf.Release()
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
	if err := e.registerPacket(p, ecn, now); err != nil {
		buf.Release()
		return emissionResult{err: err}
	}
	if handler, ok := ping.Handler.(*mtuFinderAckHandler); ok && e.local != nil {
		e.local.registerMTUProbe(handler)
	}
	e.handoff(buf, 0, ecn, sendMetadata{})
	return emissionResult{progress: true}
}

// activeConn snapshots the path for application inspection. Mutable state inside
// sendConn (remote address and GSO fallback) provides its own synchronization.
func (e *packetEmission) activeConn() sendConn {
	e.connMutex.Lock()
	conn := *e.conn
	e.connMutex.Unlock()
	return conn
}

// The connection still starts workers and owns failure/lifecycle policy. Publish
// the path as before, then drain and join the old worker before replacing its queue.
func (e *packetEmission) replacePath(conn sendConn, feedback *handshakeSendFeedback) sender {
	if e.local != nil {
		e.local.reject()
		return e.local
	}
	e.connMutex.Lock()
	*e.conn = conn
	e.connMutex.Unlock()
	e.queue.Close()
	queue := newSendQueue(conn, feedback)
	e.queue = queue
	e.bindAuthority(e.authority)
	e.bindQueueSendQoS(queue)
	return queue
}

// In-place rebinding deliberately retains the active queue: pending writes use
// the destination current at their syscall, as before.
func (e *packetEmission) rebindPath(addr net.Addr, info packetInfo) {
	(*e.conn).ChangeRemoteAddr(addr, info)
}

func (e *packetEmission) close(cause error) ([]byte, error) {
	if e.local != nil {
		return nil, errLocalPacketRoute
	}
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
	// Close-only authority survives fencing; revocation stops this submission.
	if !e.authority.permitsClose() {
		return nil, errCandidateRevoked
	}
	retained := bytes.Clone(packet.buffer.Data)
	if w, ok := (*e.conn).(authorizedWriter); ok {
		// Each further submission, such as the permission retry, is authorized
		// with close-only authority, never with work authority.
		return retained, w.writeAuthorized(packet.buffer.Data, 0, ecn, submissionPermit{authority: e.authority, close: true})
	}
	return retained, (*e.conn).Write(packet.buffer.Data, 0, ecn)
}
