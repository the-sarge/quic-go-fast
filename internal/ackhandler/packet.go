package ackhandler

import (
	"sync"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

type packetWithPacketNumber struct {
	PacketNumber protocol.PacketNumber
	*packet
}

// PacketContext identifies the application protocol path and its address tuple.
// Initial and Handshake use the zero value and remain connection-owned.
type PacketContext struct {
	KeyGeneration   protocol.KeyPhase
	PathID          protocol.PathID
	TupleGeneration uint64
}

// A Packet is a packet
type packet struct {
	Context         PacketContext
	local           *localRegistration
	SendTime        monotime.Time
	StreamFrames    []StreamFrame
	Frames          []Frame
	ackTargets      ackTargets
	LargestAcked    protocol.PacketNumber // InvalidPacketNumber if the packet doesn't contain an ACK
	Length          protocol.ByteCount
	EncryptionLevel protocol.EncryptionLevel

	IsPathMTUProbePacket bool // We don't report the loss of Path MTU probe packets to the congestion controller.

	includedInBytesInFlight bool
	isPathProbePacket       bool
	reusableStreamFrames    bool // preparation-owned metadata, never native storage
}

func (p *packet) Outstanding() bool {
	return !p.IsPathMTUProbePacket && !p.isPathProbePacket && p.IsAckEliciting()
}

func (p *packet) IsAckEliciting() bool {
	return len(p.StreamFrames) > 0 || HasAckElicitingFrames(p.Frames)
}

var packetPool = sync.Pool{New: func() any { return &packet{} }}

func getPacket() *packet { return acquirePacket(false) }

func acquirePacket(reuseStreams bool) *packet {
	p := packetPool.Get().(*packet)
	streams := p.StreamFrames
	p.Context = PacketContext{}
	p.local = nil
	p.StreamFrames = nil
	if reuseStreams && cap(streams) == 1 {
		p.StreamFrames = streams[:0]
	}
	p.reusableStreamFrames = reuseStreams
	p.Frames = nil
	p.LargestAcked = 0
	p.ackTargets = ackTargets{}
	p.Length = 0
	p.EncryptionLevel = protocol.EncryptionLevel(0)
	p.SendTime = 0
	p.IsPathMTUProbePacket = false
	p.includedInBytesInFlight = false
	p.isPathProbePacket = false
	return p
}

// Registered packets return to the pool only after acknowledgment, not loss.
// Discard also returns never-registered construction metadata. Both paths clear
// retained frame references before reuse.
func putPacket(p *packet) {
	p.local = nil
	p.Frames = nil
	if p.reusableStreamFrames && cap(p.StreamFrames) == 1 {
		// Construction may have appended through an alias while this header
		// still has length zero. Clear the retained slot, not just its length.
		clear(p.StreamFrames[:1])
		p.StreamFrames = p.StreamFrames[:0]
	} else {
		p.StreamFrames = nil
	}
	packetPool.Put(p)
}

// validPacketContext is shared by legacy and atomic local registration.
func validPacketContext(context PacketContext, level protocol.EncryptionLevel) bool {
	return context.PathID == 0 && ((level != protocol.EncryptionInitial && level != protocol.EncryptionHandshake) || context == (PacketContext{}))
}
