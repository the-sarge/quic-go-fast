package ackhandler

import (
	"fmt"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
)

type ReceivedPacketHandler struct {
	initialPackets   *receivedPacketTracker
	handshakePackets *receivedPacketTracker
	appDataPackets   appDataReceivedPacketTracker

	lowest1RTTPacket protocol.PacketNumber
	paths            map[protocol.PathID]*appDataReceivedPacketTracker
	largestPath      protocol.PathID
}

func NewReceivedPacketHandler(logger utils.Logger) *ReceivedPacketHandler {
	return &ReceivedPacketHandler{
		initialPackets:   newReceivedPacketTracker(),
		handshakePackets: newReceivedPacketTracker(),
		appDataPackets:   *newAppDataReceivedPacketTracker(logger),
		lowest1RTTPacket: protocol.InvalidPacketNumber,
	}
}

// SetAppDataAckThreshold acknowledges Application Data every threshold
// ack-eliciting packets, between MinAckThreshold and MaxAckThreshold, instead
// of every second one. Two or more unacknowledged packets then wait at most
// 1 ms, a lone packet waits max_ack_delay, and both delays run from the oldest
// unacknowledged packet. The immediate triggers are unchanged. Call it before
// the first packet is received.
func (h *ReceivedPacketHandler) SetAppDataAckThreshold(threshold int) {
	if threshold < MinAckThreshold || threshold > MaxAckThreshold {
		panic(fmt.Sprintf("ackhandler: acknowledgment threshold %d outside [%d, %d]", threshold, MinAckThreshold, MaxAckThreshold))
	}
	h.appDataPackets.ackThreshold = threshold
	for _, tracker := range h.paths {
		tracker.ackThreshold = threshold
	}
}

// AppDataAckThreshold returns the threshold set by SetAppDataAckThreshold, or
// 0 for the default rule.
func (h *ReceivedPacketHandler) AppDataAckThreshold() int {
	return h.appDataPackets.ackThreshold
}

func (h *ReceivedPacketHandler) ReceivedPacket(
	pn protocol.PacketNumber,
	ecn protocol.ECN,
	encLevel protocol.EncryptionLevel,
	rcvTime monotime.Time,
	ackEliciting bool,
) error {
	switch encLevel {
	case protocol.EncryptionInitial:
		return h.initialPackets.ReceivedPacket(pn, ecn, ackEliciting)
	case protocol.EncryptionHandshake:
		// The Handshake packet number space might already have been dropped as a result
		// of processing the CRYPTO frame that was contained in this packet.
		if h.handshakePackets == nil {
			return nil
		}
		return h.handshakePackets.ReceivedPacket(pn, ecn, ackEliciting)
	case protocol.Encryption0RTT:
		if h.lowest1RTTPacket != protocol.InvalidPacketNumber && pn > h.lowest1RTTPacket {
			return fmt.Errorf("received packet number %d on a 0-RTT packet after receiving %d on a 1-RTT packet", pn, h.lowest1RTTPacket)
		}
		return h.appDataPackets.ReceivedPacket(pn, ecn, rcvTime, ackEliciting)
	case protocol.Encryption1RTT:
		if h.lowest1RTTPacket == protocol.InvalidPacketNumber || pn < h.lowest1RTTPacket {
			h.lowest1RTTPacket = pn
		}
		return h.appDataPackets.ReceivedPacket(pn, ecn, rcvTime, ackEliciting)
	default:
		panic(fmt.Sprintf("received packet with unknown encryption level: %s", encLevel))
	}
}

func (h *ReceivedPacketHandler) IgnorePacketsBelow(pn protocol.PacketNumber) {
	h.appDataPackets.IgnoreBelow(pn)
}

func (h *ReceivedPacketHandler) DropPackets(encLevel protocol.EncryptionLevel) {
	//nolint:exhaustive // 1-RTT packet number space is never dropped.
	switch encLevel {
	case protocol.EncryptionInitial:
		h.initialPackets = nil
	case protocol.EncryptionHandshake:
		h.handshakePackets = nil
	case protocol.Encryption0RTT:
		// Nothing to do here.
		// If we are rejecting 0-RTT, no 0-RTT packets will have been decrypted.
	default:
		panic(fmt.Sprintf("Cannot drop keys for encryption level %s", encLevel))
	}
}

func (h *ReceivedPacketHandler) GetAlarmTimeout() monotime.Time {
	alarm := h.appDataPackets.GetAlarmTimeout()
	for _, tracker := range h.paths {
		if next := tracker.GetAlarmTimeout(); !next.IsZero() && (alarm.IsZero() || next.Before(alarm)) {
			alarm = next
		}
	}
	return alarm
}

func (h *ReceivedPacketHandler) GetAckFrame(encLevel protocol.EncryptionLevel, now monotime.Time, onlyIfQueued bool) *wire.AckFrame {
	//nolint:exhaustive // 0-RTT packets can't contain ACK frames.
	switch encLevel {
	case protocol.EncryptionInitial:
		if h.initialPackets != nil {
			return h.initialPackets.GetAckFrame()
		}
		return nil
	case protocol.EncryptionHandshake:
		if h.handshakePackets != nil {
			return h.handshakePackets.GetAckFrame()
		}
		return nil
	case protocol.Encryption1RTT:
		return h.appDataPackets.GetAckFrame(now, onlyIfQueued)
	default:
		// 0-RTT packets can't contain ACK frames
		return nil
	}
}

func (h *ReceivedPacketHandler) IsPotentiallyDuplicate(pn protocol.PacketNumber, encLevel protocol.EncryptionLevel) bool {
	switch encLevel {
	case protocol.EncryptionInitial:
		if h.initialPackets != nil {
			return h.initialPackets.IsPotentiallyDuplicate(pn)
		}
	case protocol.EncryptionHandshake:
		if h.handshakePackets != nil {
			return h.handshakePackets.IsPotentiallyDuplicate(pn)
		}
	case protocol.Encryption0RTT, protocol.Encryption1RTT:
		return h.appDataPackets.IsPotentiallyDuplicate(pn)
	}
	panic("unexpected encryption level")
}

// AddPath admits an additional application receive space under a new, higher ID.
// The path registry calls this after authentication or local commitment.
func (h *ReceivedPacketHandler) AddPath(id protocol.PathID) error {
	if id == 0 || id <= h.largestPath || len(h.paths) >= maxRecoveryPaths {
		return fmt.Errorf("ackhandler: receive path unavailable: %d", id)
	}
	h.largestPath = id
	if h.paths == nil {
		h.paths = make(map[protocol.PathID]*appDataReceivedPacketTracker)
	}
	tracker := newAppDataReceivedPacketTracker(h.appDataPackets.logger)
	tracker.ackThreshold = h.appDataPackets.ackThreshold
	h.paths[id] = tracker
	return nil
}

// RemovePath ends a retired path's receive space at its final reclamation.
func (h *ReceivedPacketHandler) RemovePath(id protocol.PathID) { delete(h.paths, id) }

func (h *ReceivedPacketHandler) appDataOnPath(id protocol.PathID) *appDataReceivedPacketTracker {
	if id == 0 {
		return &h.appDataPackets
	}
	return h.paths[id]
}

func (h *ReceivedPacketHandler) ReceivedPacketOnPath(id protocol.PathID, pn protocol.PacketNumber, ecn protocol.ECN, now monotime.Time, ackEliciting bool) error {
	if id == 0 {
		return h.ReceivedPacket(pn, ecn, protocol.Encryption1RTT, now, ackEliciting)
	}
	tracker := h.appDataOnPath(id)
	if tracker == nil {
		return fmt.Errorf("ackhandler: unknown receive path: %d", id)
	}
	return tracker.ReceivedPacket(pn, ecn, now, ackEliciting)
}

func (h *ReceivedPacketHandler) IsPotentiallyDuplicateOnPath(id protocol.PathID, pn protocol.PacketNumber) bool {
	tracker := h.appDataOnPath(id)
	return tracker == nil || tracker.IsPotentiallyDuplicate(pn)
}

func (h *ReceivedPacketHandler) GetPathAckFrame(id protocol.PathID, now monotime.Time, onlyIfQueued bool) *wire.AckFrame {
	tracker := h.appDataOnPath(id)
	if tracker == nil {
		return nil
	}
	ack := tracker.GetAckFrame(now, onlyIfQueued)
	if ack != nil {
		ack.HasPathID, ack.PathID = true, id
	}
	return ack
}

// IgnorePacketsBelowOnPath applies a confirmed carrier's target watermark.
// Unknown targets never allocate state.
func (h *ReceivedPacketHandler) IgnorePacketsBelowOnPath(id protocol.PathID, pn protocol.PacketNumber) {
	if tracker := h.appDataOnPath(id); tracker != nil {
		tracker.IgnoreBelow(pn)
	}
}

// AppendPathAckFrames selects only admitted nonzero targets. Space is checked
// before consuming a queued ACK so a full carrier cannot lose its wakeup.
func (h *ReceivedPacketHandler) AppendPathAckFrames(frames []Frame, maxSize protocol.ByteCount, now monotime.Time, onlyIfQueued bool, v protocol.Version) ([]Frame, protocol.ByteCount) {
	var length protocol.ByteCount
	for id, tracker := range h.paths {
		if !tracker.hasNewAck {
			continue
		}
		ranges := tracker.packetHistory.ranges
		if len(ranges) == 0 {
			continue
		}
		newest := ranges[len(ranges)-1]
		minimum := wire.AckFrame{HasPathID: true, PathID: id, DelayTime: max(0, now.Sub(tracker.largestObservedRcvdTime)), ECT0: tracker.ect0, ECT1: tracker.ect1, ECNCE: tracker.ecnce, AckRanges: []wire.AckRange{{Smallest: newest.Start, Largest: newest.End}}}
		if minimum.Length(v) > maxSize-length {
			continue
		}
		ack := h.GetPathAckFrame(id, now, onlyIfQueued)
		if ack == nil {
			continue
		}
		ack.Truncate(maxSize-length, v)
		frames = append(frames, Frame{Frame: ack})
		length += ack.Length(v)
	}
	return frames, length
}
