package quic

import (
	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
)

// nativeHeaderFacts expire before registration. They never enlarge recovery's
// retained packet and only concrete packing helpers borrow their address.
type nativeHeaderFacts struct {
	ack             *wire.AckFrame
	packetNumber    protocol.PacketNumber
	destConnID      protocol.ConnectionID
	packetNumberLen protocol.PacketNumberLen
	keyPhase        protocol.KeyPhaseBit
}

// The original control-frame header survives recovery callbacks. Both later
// DATAGRAM scans must observe this backing at their existing points.
type nativePacketReceipt struct {
	frames []ackhandler.Frame
	size   protocol.ByteCount
}

func (e *packetEmission) usesPreparedPacket(datagramOnly bool) bool {
	return e.local != nil && !datagramOnly && e.route == nil && e.pathCredit == nil
}

func (e *packetEmission) appendNativePacket(buf *packetBuffer, maxSize protocol.ByteCount, ecn protocol.ECN, now monotime.Time) (nativePacketReceipt, error) {
	start := buf.Len()
	prepared := ackhandler.PreparePacket()
	var header nativeHeaderFacts
	if err := e.packer.appendPreparedPacket(buf, maxSize, now, e.version, &prepared, &header); err != nil {
		prepared.Discard()
		return nativePacketReceipt{}, err
	}
	view := prepared.View()
	receipt := nativePacketReceipt{frames: view.Frames(), size: buf.Len() - start}
	if e.bbr != nil && e.paceReservation && (ackhandler.HasAckElicitingFrames(view.Frames()) || len(view.StreamFrames()) != 0) {
		e.pacingBytes += receipt.size
	}
	e.policy.logPreparedShortHeaderPacket(view, header, ecn, receipt.size)
	e.captureTerminalReceipts(receipt.frames)
	e.policy.notePreparedEmissionActivity(view, now)
	largestAcked := protocol.InvalidPacketNumber
	if header.ack != nil {
		largestAcked = header.ack.LargestAcked()
	}
	prepared.SetLargestAcked(largestAcked)
	if err := e.registerPrepared(&prepared, header.packetNumber, receipt.frames, ecn, now); err != nil {
		prepared.Discard()
		return nativePacketReceipt{}, err
	}
	// This lane selected path zero. Do not inspect view after recovery adopted it.
	e.policy.noteEmissionRegistration()
	return receipt, nil
}

func (e *packetEmission) registerPrepared(prepared *ackhandler.PacketPreparation, pn protocol.PacketNumber, frames []ackhandler.Frame, ecn protocol.ECN, now monotime.Time) error {
	// Resolve both receivers at the original registration observation points.
	if e.local == nil {
		ackhandler.SentPreparedPacket(*e.recovery, prepared, now, pn, ecn)
		return nil
	}
	recovery := (*e.recovery).(ackhandler.LocalSentPacketHandler)
	token, err := ackhandler.RegisterPreparedPacket(recovery, prepared, now, pn, ecn)
	if err != nil {
		return err
	}
	e.local.register(token, carriesDatagram(frames))
	return nil
}
