package ackhandler

import (
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
)

// ResetForMTUReduction preserves the path RTT while retiring the old flight and
// restarting congestion control at the smaller size. Recovery retains ownership
// of registration order and lifetimes. The synchronous observer reports ACK facts
// only; the connection run loop owns the confirmation verdict.
func ResetForMTUReduction(handler SentPacketHandler, now monotime.Time, size protocol.ByteCount, acked func(PacketContext, protocol.ByteCount, monotime.Time)) {
	h := handler.(*sentPacketHandler)
	h.mtuAcked = acked
	h.resetCongestionCapture(true)
	h.resetForPacketSize(now, size)
}

// hasMTURecoveryDeadline identifies transport recovery probes whose clock and
// verdict belong to the connection run loop. Discovery probes keep ordinary loss.
func (p *packet) hasMTURecoveryDeadline() bool {
	return p.IsPathMTUProbePacket && p.isMTURecoveryPacket()
}

// isMTURecoveryPacket classifies the transport's dedicated recovery emission for
// registration credit. Both padded attempts and follow-up PINGs belong to it;
// IsPathMTUProbePacket independently retains their different loss classification.
func (p *packet) isMTURecoveryPacket() bool {
	for _, frame := range p.Frames {
		if _, ok := frame.Handler.(interface{ IsMTURecoveryPacket() }); ok {
			return true
		}
	}
	return false
}

// RetireMTURecoveryProbes releases recovery-probe flight without congestion loss.
// While recovery continues, keepForACK retains the bounded probe records so a
// later admitted ACK still confirms the size. Terminal recovery drops them.
func RetireMTURecoveryProbes(handler SentPacketHandler, now monotime.Time, keepForACK bool) {
	h := handler.(*sentPacketHandler)
	if !keepForACK {
		clear(h.mtuRecoveryAcks[:])
	}
	for pn, p := range h.appDataPackets.history.Packets() {
		if !p.hasMTURecoveryDeadline() {
			continue
		}
		h.removeFromBytesInFlight(p)
		if keepForACK {
			h.retainMTURecoveryAck(pn, p)
		}
		h.retainAckCarrier(pn, p, now)
		h.discardCongestionPacket(protocol.Encryption1RTT, pn)
		h.retireLocal(p, false)
		h.appDataPackets.history.DeclareLost(pn)
	}
	h.setLossDetectionTimer(now)
}

// MaxMTURecoveryProbes bounds one size-confirmation attempt and its retained ACK facts.
const MaxMTURecoveryProbes = 3

type mtuRecoveryAck struct {
	valid   bool
	number  protocol.PacketNumber
	context PacketContext
	size    protocol.ByteCount
	sent    monotime.Time
}

func (h *sentPacketHandler) retainMTURecoveryAck(pn protocol.PacketNumber, p *packet) {
	for i := range h.mtuRecoveryAcks {
		if !h.mtuRecoveryAcks[i].valid {
			h.mtuRecoveryAcks[i] = mtuRecoveryAck{valid: true, number: pn, context: p.Context, size: p.Length, sent: p.SendTime}
			return
		}
	}
	panic("ackhandler BUG: too many MTU recovery probes")
}

// Admission of the decoded ACK precedes these bounded facts. Retaining metadata
// separately releases the live-history span and flight after the owned deadline.
func (h *sentPacketHandler) acknowledgeMTURecoveryProbes(ack *wire.AckFrame) {
	for i, p := range h.mtuRecoveryAcks {
		if !p.valid || !ack.AcksPacket(p.number) {
			continue
		}
		h.mtuRecoveryAcks[i] = mtuRecoveryAck{}
		if h.mtuAcked != nil {
			h.mtuAcked(p.context, p.size, p.sent)
		}
	}
}

// CanSendMTURecoveryProbe preserves actual capacity, lifecycle and amplification
// limits while exempting recovery from congestion and path-validation counts.
func CanSendMTURecoveryProbe(handler SentPacketHandler) bool {
	h := handler.(*sentPacketHandler)
	return h.canTrackPacket() && !h.isAmplificationLimited()
}

// ObservePacketSize reports synchronous transport facts to the connection owner.
// It adds no policy or cross-context handoff to recovery.
func ObservePacketSize(handler SentPacketHandler, acked func(PacketContext, protocol.ByteCount, monotime.Time), lost func(PacketContext, protocol.ByteCount, monotime.Time)) {
	h := handler.(*sentPacketHandler)
	h.mtuAcked, h.mtuLost = acked, lost
}

func (h *sentPacketHandler) reportMTULoss(p *packet, now monotime.Time) {
	if h.mtuLost != nil && p.EncryptionLevel == protocol.Encryption1RTT && !p.IsPathMTUProbePacket && !p.isPathProbePacket && p.Length > protocol.MaxPacketBufferSize {
		h.mtuLost(p.Context, p.Length, now)
	}
}
