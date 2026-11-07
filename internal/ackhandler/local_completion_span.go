package ackhandler

import (
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

// TryCompleteLocalAcceptedPackets consumes one fully accepted native member.
// It declines without modifying recovery. Tokens are borrowed
// for this call only, must be in increasing PN order, and share the member's
// actual return time. Every other case must use CompleteLocalPacket in order.
func TryCompleteLocalAcceptedPackets(handler LocalSentPacketHandler, tokens []LocalCompletionToken, returnedAt monotime.Time) bool {
	h, ok := handler.(*sentPacketHandler)
	if !ok || h == nil || len(tokens) < 2 || !returnedAt.After(0) ||
		h.local == nil || h.local.closed || h.congestionEvents != nil || h.bbrECN != nil ||
		h.qlogger != nil || !h.handshakeConfirmed || h.isAmplificationLimited() || len(h.lostPackets.lostPackets) != 0 {
		return false
	}
	ps := h.appDataPackets
	if ps == nil || ps.localEpoch == nil || ps.localEpoch.owner != &h.local.owner ||
		!ps.history.localPTOTracked || !ps.history.HasOutstandingPackets() || !ps.history.latestLocalPTO().After(0) {
		return false
	}
	// Use the physical prefix, including retained holes and probe placeholders.
	// Qualification cannot change it, and this closed loop does not change ACKs.
	lowest := ps.history.LowestPacketNumber()
	if ps.largestAcked != protocol.InvalidPacketNumber && lowest != protocol.InvalidPacketNumber && lowest <= ps.largestAcked {
		return false
	}
	if e := h.local.ecn; e != nil {
		tracker, ok := h.ecnTracker.(*ecnTracker)
		if e.pending || !ok || tracker == nil || tracker.qlogger != nil {
			return false
		}
	}
	previous := protocol.InvalidPacketNumber
	for _, token := range tokens {
		r := token.registration
		// Epoch and owner are immutable. Reject foreign identities before reading
		// mutable registration fields owned by another connection.
		if r == nil || r.epoch == nil || r.epoch.owner != &h.local.owner || r.epoch != ps.localEpoch {
			return false
		}
		if r.level != protocol.Encryption1RTT || r.outcome != localPacketPending || r.acked || r.earlyACK ||
			r.pn <= previous || returnedAt.Before(r.registeredAt) {
			return false
		}
		p := r.packet
		if p == nil || p.local != r || p.EncryptionLevel != protocol.Encryption1RTT || !p.Outstanding() {
			return false
		}
		index, present := ps.history.getIndex(r.pn)
		if !present || ps.history.packets[index] != p || index >= len(ps.history.unresolvedLocal) || !ps.history.unresolvedLocal[index] {
			return false
		}
		previous = r.pn
	}

	for i, token := range tokens {
		r := token.registration
		h.local.owner.pending--
		h.local.owner.live--
		ps.history.completeLocal(r.pn)
		r.outcome = LocalPacketAccepted
		r.eligibleAt = returnedAt
		ps.history.localPTOChanged(r.packet)
		// The pending live identity cannot have a lost ECN record. With no
		// pending ECN report, this preserves only callback-free per-packet work.
		h.completeLocalAcceptedECN(r)
		ps.history.qualifyLocal(r.pn)
		ps.lossTime = 0
		if i == len(tokens)-1 {
			// Every scalar intermediate alarm would be nonzero: a live accepted
			// application packet supplies positive PTO evidence. There is no
			// cancellation logger call, qlog, rich feedback, or undo observer.
			// Read current RTT and final evidence; never reuse a cached deadline.
			h.setLossDetectionTimer(returnedAt)
		}
	}
	return true
}
