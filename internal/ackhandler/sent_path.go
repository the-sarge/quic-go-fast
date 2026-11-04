package ackhandler

import (
	"fmt"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
)

// AddPath installs the second recovery context with its own RTT object.
func (h *sentPacketHandler) AddPath(id protocol.PathID, size protocol.ByteCount) error {
	return h.AddPathWithRTT(id, size, utils.NewRTTStats())
}

// AddPathWithRTT installs the second recovery context after authentication or
// local commitment, sampling the RTT object crypto already reads for key retention.
// Neither an ACK nor an untrusted path identifier can allocate recovery state.
func (h *sentPacketHandler) AddPathWithRTT(id protocol.PathID, size protocol.ByteCount, rtt *utils.RTTStats) error {
	if (h.local != nil && h.local.closed) || h.pathRoot != nil || id == 0 || len(h.paths) != 0 || !h.handshakeConfirmed ||
		h.congestionEvents != nil || len(h.lostPackets.lostPackets) > 32 || len(h.ackCarriers.carriers) > maxRetiredAckCarriers/2 || size < protocol.MinInitialPacketSize {
		return fmt.Errorf("ackhandler: recovery path unavailable: %d", id)
	}
	rtt.SetMaxAckDelay(h.rttStats.MaxAckDelay())
	path := newSentPacketHandler(0, size, rtt, h.connStats, true, h.enableECN, h.ignorePacketsBelow, h.perspective, nil, h.logger, nil)
	path.pathID, path.pathRoot, path.pathPolicy = id, h, h.pathPolicy
	path.initialPackets, path.handshakePackets = nil, nil
	path.handshakeConfirmed, path.peerCompletedAddressValidation, path.peerAddressValidated = true, true, true
	if h.local != nil {
		path.local = &localCompletions{records: make(map[uint64]*localRegistration)}
		if h.local.ecn != nil && h.bbrECN == nil {
			// Each path validates its own ECN, exactly as the root does.
			path.local.ecn = &localECNEvidence{watermark: protocol.InvalidPacketNumber}
			path.ecnTracker.(*ecnTracker).local = path.local.ecn
		}
	}
	// Optional late-receipt stores share their existing aggregate ceilings. A
	// missing receipt cannot discharge Reno undo or create a delivery callback.
	h.lostPackets.maxLength, path.lostPackets.maxLength = 32, 32
	h.ackCarriers.limit, path.ackCarriers.limit = maxRetiredAckCarriers/2, maxRetiredAckCarriers/2
	h.paths = map[protocol.PathID]*sentPacketHandler{id: path}
	return nil
}

// EnablePathPolicy starts the liveness facts, from private admission on. Ordinary
// connections never pay for them.
func (h *sentPacketHandler) EnablePathPolicy() { h.pathPolicy = true }

func (h *sentPacketHandler) path(id protocol.PathID) *sentPacketHandler {
	if id == h.pathID {
		return h
	}
	if h.pathRoot != nil {
		return nil
	}
	return h.paths[id]
}

func (h *sentPacketHandler) validPacketContext(context PacketContext, level protocol.EncryptionLevel) bool {
	if h.pathID == 0 {
		return validPacketContext(context, level)
	}
	return context.PathID == h.pathID && level == protocol.Encryption1RTT
}

func (h *sentPacketHandler) earliestRecoveryPath() *sentPacketHandler {
	selected := h
	for _, path := range h.paths {
		if !path.alarm.Time.IsZero() && (selected.alarm.Time.IsZero() || path.alarm.Time.Before(selected.alarm.Time)) {
			selected = path
		}
	}
	return selected
}

// RecoveryOnPath exposes the existing per-path recovery operations only after
// explicit private admission. The root alone aggregates wakeups and capacity;
// frame callbacks remain owned by the connection's logical delivery machinery.
func (h *sentPacketHandler) RecoveryOnPath(id protocol.PathID) SentPacketHandler {
	if path := h.path(id); path != nil {
		return path
	}
	return nil
}

// PathLossDetectionTimeout is used by fixtures that need the unaggregated alarm.
func (h *sentPacketHandler) PathLossDetectionTimeout(id protocol.PathID) monotime.Time {
	if path := h.path(id); path != nil {
		return path.alarm.Time
	}
	return 0
}

// TakeAckedTuple reports the newest tuple whose ACK-eliciting traffic an ACK has
// newly acknowledged since the last call, whichever path carried that ACK. A path
// validation packet doesn't count, since it can be probing another tuple.
func (h *sentPacketHandler) TakeAckedTuple() (uint64, bool) {
	tuple, live := h.ackedTuple, h.ackedLive
	h.ackedTuple, h.ackedLive = 0, false
	return tuple, live
}
