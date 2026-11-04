package ackhandler

import (
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
)

// SentPacketHandler handles ACKs received for outgoing packets
type SentPacketHandler interface {
	// SentPacket may modify the packet
	SentPacket(t monotime.Time, pn, largestAcked protocol.PacketNumber, streamFrames []StreamFrame, frames []Frame, encLevel protocol.EncryptionLevel, ecn protocol.ECN, size protocol.ByteCount, isPathMTUProbePacket, isPathProbePacket bool)
	// SentPacketWithContext retains the identity selected before packing.
	SentPacketWithContext(context PacketContext, t monotime.Time, pn, largestAcked protocol.PacketNumber, streamFrames []StreamFrame, frames []Frame, encLevel protocol.EncryptionLevel, ecn protocol.ECN, size protocol.ByteCount, isPathMTUProbePacket, isPathProbePacket bool)
	// ReceivedAck processes an ACK frame.
	// It does not store a copy of the frame.
	ReceivedAck(f *wire.AckFrame, encLevel protocol.EncryptionLevel, rcvTime monotime.Time) (protocol.ApplicationKeyReceipt, error)
	ReceivedPacket(protocol.EncryptionLevel, monotime.Time)
	ReceivedBytes(_ protocol.ByteCount, rcvTime monotime.Time)
	DropPackets(_ protocol.EncryptionLevel, rcvTime monotime.Time)
	ResetForRetry(rcvTime monotime.Time)

	// The SendMode determines if and what kind of packets can be sent.
	SendMode(now monotime.Time) SendMode
	// CanSendPathProbe checks history capacity before allocating a probe packet number.
	CanSendPathProbe() bool
	// TimeUntilSend is the time when the next packet should be sent.
	// It is used for pacing packets.
	TimeUntilSend() monotime.Time
	SetMaxDatagramSize(count protocol.ByteCount)

	// only to be called once the handshake is complete
	QueueProbePacket(protocol.EncryptionLevel) bool /* was a packet queued */

	ECNMode(isShortHeaderPacket bool) protocol.ECN // isShortHeaderPacket should only be true for non-coalesced 1-RTT packets
	PeekPacketNumber(protocol.EncryptionLevel) (protocol.PacketNumber, protocol.PacketNumberLen)
	PopPacketNumber(protocol.EncryptionLevel) protocol.PacketNumber

	GetLossDetectionTimeout() monotime.Time
	OnLossDetectionTimeout(now monotime.Time) (TimeoutOutcome, error)

	MigratedPath(now monotime.Time, initialMaxPacketSize protocol.ByteCount)
}

// TimeoutOutcome reports what an OnLossDetectionTimeout call did.
// It does not depend on tracing.
type TimeoutOutcome struct {
	// PathID identifies the application recovery owner whose PTO expired.
	PathID protocol.PathID

	// PTO is set if a probe timeout expired: recovery counted the expiration and
	// requested probe packets. Time-threshold loss and calls that expire nothing
	// leave it unset.
	PTO bool
	// EncLevel is the packet number space whose probe timeout expired. It is only set if PTO is set.
	EncLevel protocol.EncryptionLevel
	// Accepted is set if an ACK-eliciting packet the socket is known to have accepted
	// is outstanding and no earlier PTO has counted it. TupleGeneration is that
	// packet's tuple. Recovery's own backoff doesn't depend on either.
	Accepted        bool
	TupleGeneration uint64
}
