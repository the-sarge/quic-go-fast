package ackhandler

import (
	"fmt"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
)

// maxRecoveryPaths bounds nonzero contexts. The registry's four retained-history
// units already bound them, as each admitted path holds one until reclamation.
const maxRecoveryPaths = 4

// AddPath installs a further recovery context with its own RTT object.
func (h *sentPacketHandler) AddPath(id protocol.PathID, size protocol.ByteCount) error {
	return h.AddPathWithRTT(id, size, utils.NewRTTStats())
}

// AddPathWithRTT installs a recovery context after authentication or local
// commitment, sampling the RTT object crypto already reads for key retention.
// Neither an ACK nor an untrusted path identifier can allocate recovery state.
// IDs only rise, and retired contexts don't count against the two live slots.
func (h *sentPacketHandler) AddPathWithRTT(id protocol.PathID, size protocol.ByteCount, rtt *utils.RTTStats) error {
	if (h.local != nil && h.local.closed) || h.pathRoot != nil || id == 0 || id <= h.largestPath || h.liveContexts() >= 2 || len(h.paths) >= maxRecoveryPaths || !h.handshakeConfirmed ||
		h.congestionEvents != nil || len(h.lostPackets.lostPackets) > 32 || len(h.ackCarriers.carriers) > maxRetiredAckCarriers/2 || size < protocol.MinInitialPacketSize {
		return fmt.Errorf("ackhandler: recovery path unavailable: %d", id)
	}
	rtt.SetMaxAckDelay(h.rttStats.MaxAckDelay())
	path := newSentPacketHandler(0, size, rtt, h.connStats, true, h.enableECN, h.ignorePacketsBelow, h.perspective, nil, h.logger, nil)
	path.pathID, path.pathRoot = id, h
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
	// Path zero keeps half, and the nonzero contexts share the other half.
	h.lostPackets.maxLength, path.lostPackets.maxLength = 32, 32/maxRecoveryPaths
	h.ackCarriers.limit, path.ackCarriers.limit = maxRetiredAckCarriers/2, maxRetiredAckCarriers/2/maxRecoveryPaths
	if h.paths == nil {
		h.paths = make(map[protocol.PathID]*sentPacketHandler, 1)
	}
	h.paths[id] = path
	h.largestPath = id
	return nil
}

func (h *sentPacketHandler) liveContexts() int {
	live := 0
	if !h.retiredPath {
		live++
	}
	for _, path := range h.paths {
		if !path.retiredPath {
			live++
		}
	}
	return live
}

// RetirePath marks a context retired at the abandonment signal, once no submission
// can follow it. Its evidence stays until RemovePath.
func (h *sentPacketHandler) RetirePath(id protocol.PathID) {
	if path := h.path(id); path != nil {
		path.retiredPath = true
	}
}

// PathQuiescent reports that a context has no unconsumed local result and no frames
// left to return to their logical owners, so reclamation can strand nothing. An early
// ACK clears a registration's packet but not its record, whose token still arrives.
func (h *sentPacketHandler) PathQuiescent(id protocol.PathID) bool {
	path := h.path(id)
	if path == nil {
		return true
	}
	if path.local != nil && len(path.local.records) != 0 {
		return false
	}
	for _, p := range path.appDataPackets.history.Packets() {
		if p.Outstanding() {
			return false
		}
	}
	return true
}

// RemovePath ends a retired context at its final reclamation. A later ACK for the
// ID is ignored, because ReceivedAck can't tell it from any other absent lower ID.
// The root stays for its aggregation, and only releases path zero's retained span,
// which would otherwise charge every survivor's share of the tracking ceiling.
func (h *sentPacketHandler) RemovePath(id protocol.PathID) {
	if id == h.pathID {
		h.appDataPackets.history.releaseRetained()
		return
	}
	delete(h.paths, id)
}

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
