package congestion

import (
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

// PacketInfo contains transport facts copied before recovery retires a packet.
// It never carries frames, payloads or pointers into pooled recovery storage.
type PacketInfo struct {
	Space            protocol.EncryptionLevel // 0-RTT and 1-RTT share Encryption1RTT
	Ordinal          uint64                   // connection-wide registration order, never reset
	PathGeneration   uint64
	SampleGeneration uint64
	PacketNumber     protocol.PacketNumber
	EncryptionLevel  protocol.EncryptionLevel
	SendTime         monotime.Time
	Length           protocol.ByteCount
	AckEliciting     bool
	InFlight         bool
	PathProbe        bool
	MTUProbe         bool
	ECN              protocol.ECN
}

// SendEvent distinguishes registration's original flight from post-send flight.
type SendEvent struct {
	Packet        PacketInfo
	PriorInFlight protocol.ByteCount
	PostInFlight  protocol.ByteCount
}

// FeedbackEvent is one logical recovery event. Acked and Lost are borrowed only
// for the synchronous call; consumers must copy any values they retain.
// Lost contains actual congestion losses, excluding ACK-only and probe packets.
type FeedbackEvent struct {
	PathGeneration   uint64
	SampleGeneration uint64
	Time             monotime.Time
	Space            protocol.EncryptionLevel // 0-RTT and 1-RTT share Encryption1RTT
	HasAck           bool
	LargestAcked     protocol.PacketNumber
	PriorInFlight    protocol.ByteCount
	PostInFlight     protocol.ByteCount
	Acked            []PacketInfo
	Lost             []PacketInfo
	RTTEligible      bool // ACK meets recovery's new-largest / ack-eliciting criteria
	RTTUpdated       bool // send-time order and positive interval also permit an update
	ECNChecked       bool
	Congested        bool // the existing ECN validator's congestion signal, not packet loss
}
