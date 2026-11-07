package ackhandler

import (
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

// PacketPreparation owns only unregistered construction metadata. It is private
// emission plumbing, not a reservation of packet identity, flight or history.
// Keep one handle: successful registration or Discard consumes it.
type PacketPreparation struct {
	p       *packet
	streams []StreamFrame
}

// PreparedPacketView borrows unregistered metadata. It must not outlive the
// preparation's transfer to recovery. Passing this value does not expose the
// stack-owned preparation handle to policy callbacks.
type PreparedPacketView struct {
	p       *packet
	streams []StreamFrame
}

func (p *PacketPreparation) SetContext(context PacketContext) { p.p.Context = context }
func (p *PacketPreparation) SetKeyGeneration(generation protocol.KeyPhase) {
	p.p.Context.KeyGeneration = generation
}
func (p *PacketPreparation) SetLength(size protocol.ByteCount)        { p.p.Length = size }
func (p *PacketPreparation) SetLargestAcked(pn protocol.PacketNumber) { p.p.LargestAcked = pn }

// Keep the original borrowed stream header in the packet until acceptance, so
// Discard still clears its retained slot if construction grew a new backing.
func (p *PacketPreparation) SetFrames(frames []Frame, streams []StreamFrame) {
	p.p.Frames, p.streams = frames, streams
}

func (p *PacketPreparation) View() PreparedPacketView {
	return PreparedPacketView{p: p.p, streams: p.streams}
}
func (p PreparedPacketView) Context() PacketContext      { return p.p.Context }
func (p PreparedPacketView) Frames() []Frame             { return p.p.Frames }
func (p PreparedPacketView) StreamFrames() []StreamFrame { return p.streams }
func (p PreparedPacketView) Length() protocol.ByteCount  { return p.p.Length }

func PreparePacket() PacketPreparation { return PacketPreparation{p: acquirePacket(true)} }

// StreamStorage is borrowed only by this construction attempt. Registration
// adopts the packer's final slice, including any growth beyond this backing.
func (p *PacketPreparation) StreamStorage() []StreamFrame {
	if p == nil || p.p == nil {
		return nil
	}
	return p.p.StreamFrames[:0]
}

// Discard reclaims metadata only; it never refunds packet numbers or invokes
// frame callbacks. It is harmless after successful registration consumed p.
func (p *PacketPreparation) Discard() {
	if p == nil || p.p == nil {
		return
	}
	packet := p.p
	p.p, p.streams = nil, nil
	putPacket(packet)
}

// RegisterPreparedPacket adopts ordinary 1-RTT construction facts. Refusal
// retains the preparation; success consumes it before any recovery callback.
func RegisterPreparedPacket(handler LocalSentPacketHandler, prepared *PacketPreparation, now monotime.Time, pn protocol.PacketNumber, ecn protocol.ECN) (LocalCompletionToken, error) {
	h, ok := handler.(*sentPacketHandler)
	if !ok || prepared == nil || prepared.p == nil {
		return LocalCompletionToken{}, errLocalPacketMode
	}
	return h.registerPreparedPacket(prepared, now, pn, ecn)
}

// SentPreparedPacket preserves the value registration path when a policy callback
// changed emission to nonlocal sending. Its receiver owns the frame slices, while
// the unused preparation metadata can return to the pool before callbacks.
func SentPreparedPacket(handler SentPacketHandler, prepared *PacketPreparation, now monotime.Time, pn protocol.PacketNumber, ecn protocol.ECN) {
	p, streams := prepared.p, prepared.streams
	context, largestAcked, frames, size := p.Context, p.LargestAcked, p.Frames, p.Length
	prepared.p, prepared.streams = nil, nil
	if cap(p.StreamFrames) == 1 && (len(streams) == 0 || &p.StreamFrames[:1][0] != &streams[0]) {
		clear(p.StreamFrames[:1])
	}
	p.StreamFrames = nil
	putPacket(p)
	handler.SentPacketWithContext(context, now, pn, largestAcked, streams, frames, protocol.Encryption1RTT, ecn, size, false, false)
}
