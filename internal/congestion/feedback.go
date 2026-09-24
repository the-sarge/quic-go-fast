package congestion

import (
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

// PacketInfo contains transport facts copied before recovery retires a packet.
// It never carries frames, payloads or pointers into pooled recovery storage.
type PacketInfo struct {
	Space             protocol.EncryptionLevel // 0-RTT and 1-RTT share Encryption1RTT
	Ordinal           uint64                   // connection-wide registration order, never reset
	PathGeneration    uint64
	SampleGeneration  uint64
	PacketNumber      protocol.PacketNumber
	EncryptionLevel   protocol.EncryptionLevel
	SendTime          monotime.Time
	RegistrationValid bool // positive, nondecreasing connection registration time
	Length            protocol.ByteCount
	AckEliciting      bool
	InFlight          bool
	PathProbe         bool
	MTUProbe          bool
	ECN               protocol.ECN
	Delivery          DeliverySnapshot
	Retirement        DeliveryRetirement
}

// DeliveryRetirement describes recovery removal, not congestion-loss volume.
// A lost MTU probe can deliver bytes late without contributing congestion loss.
type DeliveryRetirement uint8

const (
	DeliveryLive DeliveryRetirement = iota
	DeliveryLost
	DeliveryPTO
)

// DeliverySnapshot freezes registration-time evidence, independently of flight.
type DeliverySnapshot struct {
	Delivered, Lost                          uint64
	DeliveredTime, SendOrigin                monotime.Time
	PriorInFlight, PostInFlight, Outstanding protocol.ByteCount
	Limited                                  SendLimitation
	Valid                                    bool
}

// SendLimitation is an observation, not inferred from an empty output result.
type SendLimitation uint8

const (
	SendUnknown SendLimitation = iota
	SendApplicationLimited
	SendFlowControlLimited
	SendProbeRTTLimited
	SendCongestionLimited
	SendPacingLimited
	SendLocalLimited
	SendReceiveYield
	SendRecoveryLimited
)

// DeliverySample uses QUIC packet bytes and raw registration-to-ACK intervals.
type DeliverySample struct {
	Delivered, Lost, BytesPerSecond, Ordinal uint64
	Interval, RawRTT                         time.Duration
	Limited                                  SendLimitation
	Valid                                    bool
}

// DeliveryStats reports bounded evidence occupancy. RecordBytes counts stored
// value records and allocated heap slots; Go map/runtime overhead is excluded.
type DeliveryStats struct {
	Live, Retained            int
	RecordBytes               uintptr
	Outstanding               protocol.ByteCount
	Evicted, Expired, Missing uint64
	Stop                      SendLimitation
	Idle                      bool
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
	RawRTT           time.Duration // fresh recovery-eligible raw observation, zero if absent
	Delivery         DeliverySample
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
