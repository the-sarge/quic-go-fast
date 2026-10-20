package ackhandler

import (
	"fmt"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
)

const reorderingThreshold = 1

// The receivedPacketTracker tracks packets for the Initial and Handshake packet number space.
// Every received packet is acknowledged immediately.
type receivedPacketTracker struct {
	ect0, ect1, ecnce uint64

	packetHistory receivedPacketHistory

	lastAck   *wire.AckFrame
	hasNewAck bool // true as soon as we received an ack-eliciting new packet
}

func newReceivedPacketTracker() *receivedPacketTracker {
	return &receivedPacketTracker{packetHistory: *newReceivedPacketHistory()}
}

func (h *receivedPacketTracker) ReceivedPacket(pn protocol.PacketNumber, ecn protocol.ECN, ackEliciting bool) error {
	if isNew := h.packetHistory.ReceivedPacket(pn); !isNew {
		return fmt.Errorf("receivedPacketTracker BUG: ReceivedPacket called for old / duplicate packet %d", pn)
	}

	//nolint:exhaustive // Only need to count ECT(0), ECT(1) and ECN-CE.
	switch ecn {
	case protocol.ECT0:
		h.ect0++
	case protocol.ECT1:
		h.ect1++
	case protocol.ECNCE:
		h.ecnce++
	}
	if !ackEliciting {
		return nil
	}
	h.hasNewAck = true
	return nil
}

func (h *receivedPacketTracker) GetAckFrame() *wire.AckFrame {
	if !h.hasNewAck {
		return nil
	}

	// This function always returns the same ACK frame struct, filled with the most recent values.
	ack := h.lastAck
	if ack == nil {
		ack = &wire.AckFrame{}
	}
	ack.Reset()
	ack.ECT0 = h.ect0
	ack.ECT1 = h.ect1
	ack.ECNCE = h.ecnce
	for r := range h.packetHistory.Backward() {
		ack.AckRanges = append(ack.AckRanges, wire.AckRange{Smallest: r.Start, Largest: r.End})
	}

	h.lastAck = ack
	h.hasNewAck = false
	return ack
}

func (h *receivedPacketTracker) IsPotentiallyDuplicate(pn protocol.PacketNumber) bool {
	return h.packetHistory.IsPotentiallyDuplicate(pn)
}

// number of ack-eliciting packets received before sending an ACK
const packetsBeforeAck = 2

// MinAckThreshold and MaxAckThreshold bound the opt-in acknowledgment
// threshold. Below 2 the mode would acknowledge more often than
// packetsBeforeAck. Above 32, the initial congestion window in packets, a new
// connection's first flight could never reach the threshold, so every early
// round trip would wait for ackGroupMaxDelay.
const (
	MinAckThreshold = 2
	MaxAckThreshold = 32
)

// ackGroupMaxDelay bounds how long two or more unacknowledged ack-eliciting
// packets wait in the opt-in threshold mode. At up to 1,000 packets a second
// the second packet arrives after this delay, so slower flows are acknowledged
// as often as with packetsBeforeAck.
const ackGroupMaxDelay = time.Millisecond

// The appDataReceivedPacketTracker tracks packets received in the Application Data packet number space.
// It waits until at least 2 packets were received before queueing an ACK, or until the max_ack_delay was reached.
type appDataReceivedPacketTracker struct {
	receivedPacketTracker

	largestObservedRcvdTime monotime.Time

	largestObserved protocol.PacketNumber
	ignoreBelow     protocol.PacketNumber

	maxAckDelay time.Duration
	ackQueued   bool // true if we need send a new ACK

	ackElicitingPacketsReceivedSinceLastAck int
	ackAlarm                                monotime.Time

	// ackThreshold, when nonzero, replaces packetsBeforeAck. The delay alarm
	// then runs from oldestUnackedRcvdTime, the receive time of the first
	// ack-eliciting packet since the last ACK, and is never restarted.
	ackThreshold          int
	oldestUnackedRcvdTime monotime.Time

	logger utils.Logger
}

func newAppDataReceivedPacketTracker(logger utils.Logger) *appDataReceivedPacketTracker {
	h := &appDataReceivedPacketTracker{
		receivedPacketTracker: *newReceivedPacketTracker(),
		maxAckDelay:           protocol.MaxAckDelay,
		logger:                logger,
	}
	return h
}

func (h *appDataReceivedPacketTracker) ReceivedPacket(pn protocol.PacketNumber, ecn protocol.ECN, rcvTime monotime.Time, ackEliciting bool) error {
	if err := h.receivedPacketTracker.ReceivedPacket(pn, ecn, ackEliciting); err != nil {
		return err
	}
	if pn >= h.largestObserved {
		h.largestObserved = pn
		h.largestObservedRcvdTime = rcvTime
	}
	if !ackEliciting {
		return nil
	}
	h.ackElicitingPacketsReceivedSinceLastAck++
	if h.ackThreshold != 0 && h.ackElicitingPacketsReceivedSinceLastAck == 1 {
		h.oldestUnackedRcvdTime = rcvTime
	}
	isMissing := h.isMissing(pn)
	if !h.ackQueued && h.shouldQueueACK(pn, ecn, rcvTime, isMissing) {
		h.ackQueued = true
		h.ackAlarm = 0 // cancel the ack alarm
	}
	if !h.ackQueued {
		if h.ackThreshold != 0 {
			h.setThresholdAlarm()
			return nil
		}
		// No ACK queued, but we'll need to acknowledge the packet after max_ack_delay.
		h.ackAlarm = rcvTime.Add(h.maxAckDelay)
		if h.logger.Debug() {
			h.logger.Debugf("\tSetting ACK timer to max ack delay: %s", h.maxAckDelay)
		}
	}
	return nil
}

// setThresholdAlarm times the alarm from the oldest unacknowledged
// ack-eliciting packet: max_ack_delay for a lone packet, ackGroupMaxDelay for
// two or more. Later packets never push it back.
func (h *appDataReceivedPacketTracker) setThresholdAlarm() {
	delay := h.maxAckDelay
	if h.ackElicitingPacketsReceivedSinceLastAck >= 2 {
		delay = min(delay, ackGroupMaxDelay)
	}
	h.ackAlarm = h.oldestUnackedRcvdTime.Add(delay)
	if h.logger.Debug() {
		h.logger.Debugf("\tSetting ACK timer to %s after the oldest unacknowledged packet", delay)
	}
}

// IgnoreBelow sets a lower limit for acknowledging packets.
// Packets with packet numbers smaller than p will not be acked.
func (h *appDataReceivedPacketTracker) IgnoreBelow(pn protocol.PacketNumber) {
	if pn <= h.ignoreBelow {
		return
	}
	h.ignoreBelow = pn
	h.packetHistory.DeleteBelow(pn)
	if h.logger.Debug() {
		h.logger.Debugf("\tIgnoring all packets below %d.", pn)
	}
}

// isMissing says if a packet was reported missing in the last ACK.
func (h *appDataReceivedPacketTracker) isMissing(p protocol.PacketNumber) bool {
	if h.lastAck == nil || p < h.ignoreBelow {
		return false
	}
	return p < h.lastAck.LargestAcked() && !h.lastAck.AcksPacket(p)
}

func (h *appDataReceivedPacketTracker) hasNewMissingPackets() bool {
	if h.lastAck == nil {
		return false
	}
	if h.largestObserved < reorderingThreshold {
		return false
	}
	highestMissing := h.packetHistory.HighestMissingUpTo(h.largestObserved - reorderingThreshold)
	if highestMissing == protocol.InvalidPacketNumber {
		return false
	}
	if highestMissing < h.lastAck.LargestAcked() {
		// the packet was already reported missing in the last ACK
		return false
	}
	return highestMissing > h.lastAck.LargestAcked()-reorderingThreshold
}

func (h *appDataReceivedPacketTracker) shouldQueueACK(pn protocol.PacketNumber, ecn protocol.ECN, rcvTime monotime.Time, wasMissing bool) bool {
	// Send an ACK if this packet was reported missing in an ACK sent before.
	// Ack decimation with reordering relies on the timer to send an ACK, but if
	// missing packets we reported in the previous ACK, send an ACK immediately.
	if wasMissing {
		if h.logger.Debug() {
			h.logger.Debugf("\tQueueing ACK because packet %d was missing before.", pn)
		}
		return true
	}

	// send an ACK every 2 ack-eliciting packets, or every ackThreshold
	threshold := packetsBeforeAck
	if h.ackThreshold != 0 {
		threshold = h.ackThreshold
	}
	if h.ackElicitingPacketsReceivedSinceLastAck >= threshold {
		if h.logger.Debug() {
			h.logger.Debugf("\tQueueing ACK because packet %d packets were received after the last ACK (using threshold: %d).", h.ackElicitingPacketsReceivedSinceLastAck, threshold)
		}
		return true
	}

	// with the threshold mode, a group of two or more waits at most ackGroupMaxDelay
	if h.ackThreshold != 0 && h.ackElicitingPacketsReceivedSinceLastAck >= 2 && rcvTime.Sub(h.oldestUnackedRcvdTime) >= ackGroupMaxDelay {
		if h.logger.Debug() {
			h.logger.Debugf("\tQueueing ACK because the oldest of %d unacknowledged packets is %s old.", h.ackElicitingPacketsReceivedSinceLastAck, ackGroupMaxDelay)
		}
		return true
	}

	// queue an ACK if there are new missing packets to report
	if h.hasNewMissingPackets() {
		h.logger.Debugf("\tQueuing ACK because there's a new missing packet to report.")
		return true
	}

	// queue an ACK if the packet was ECN-CE marked
	if ecn == protocol.ECNCE {
		h.logger.Debugf("\tQueuing ACK because the packet was ECN-CE marked.")
		return true
	}
	return false
}

func (h *appDataReceivedPacketTracker) GetAckFrame(now monotime.Time, onlyIfQueued bool) *wire.AckFrame {
	if onlyIfQueued && !h.ackQueued {
		if h.ackAlarm.IsZero() || h.ackAlarm.After(now) {
			return nil
		}
		if h.logger.Debug() && !h.ackAlarm.IsZero() {
			h.logger.Debugf("Sending ACK because the ACK timer expired.")
		}
	}
	ack := h.receivedPacketTracker.GetAckFrame()
	if ack == nil {
		return nil
	}
	ack.DelayTime = max(0, now.Sub(h.largestObservedRcvdTime))
	h.ackQueued = false
	h.ackAlarm = 0
	h.ackElicitingPacketsReceivedSinceLastAck = 0
	h.oldestUnackedRcvdTime = 0
	return ack
}

func (h *appDataReceivedPacketTracker) GetAlarmTimeout() monotime.Time { return h.ackAlarm }
