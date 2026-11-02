package ackhandler

import (
	"iter"
	"slices"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
)

type lostPacket struct {
	PacketNumber protocol.PacketNumber
	SendTime     monotime.Time
}

type lostPacketTracker struct {
	maxLength   int
	lostPackets []lostPacket
}

func newLostPacketTracker(maxLength int) *lostPacketTracker {
	return &lostPacketTracker{
		maxLength: maxLength,
		// Preallocate a small slice only.
		// Hopefully we won't lose many packets.
		lostPackets: make([]lostPacket, 0, 4),
	}
}

func (t *lostPacketTracker) Add(p protocol.PacketNumber, sendTime monotime.Time) {
	if len(t.lostPackets) == t.maxLength {
		t.lostPackets = t.lostPackets[1:]
	}
	t.lostPackets = append(t.lostPackets, lostPacket{
		PacketNumber: p,
		SendTime:     sendTime,
	})
}

// Delete deletes a packet from the lost packet tracker.
// This function is not optimized for performance if many packets are lost,
// but it is only used when a spurious loss is detected, which is rare.
func (t *lostPacketTracker) Delete(pn protocol.PacketNumber) {
	t.lostPackets = slices.DeleteFunc(t.lostPackets, func(p lostPacket) bool {
		return p.PacketNumber == pn
	})
}

func (t *lostPacketTracker) All() iter.Seq2[protocol.PacketNumber, monotime.Time] {
	return func(yield func(protocol.PacketNumber, monotime.Time) bool) {
		for _, p := range t.lostPackets {
			if !yield(p.PacketNumber, p.SendTime) {
				return
			}
		}
	}
}

func (t *lostPacketTracker) DeleteBefore(ti monotime.Time) {
	if len(t.lostPackets) == 0 {
		return
	}
	if !t.lostPackets[0].SendTime.Before(ti) {
		return
	}
	var idx int
	for ; idx < len(t.lostPackets); idx++ {
		if !t.lostPackets[idx].SendTime.Before(ti) {
			break
		}
	}
	t.lostPackets = slices.Delete(t.lostPackets, 0, idx)
}

func (t *lostPacketTracker) Reset() {
	t.lostPackets = t.lostPackets[:0]
}

// Retired ACK carriers retain only target watermarks. This bounded history is
// separate from spurious-loss evidence: a PTO or migration retirement must not
// train Reno loss adaptation merely because it carried an ACK.
type retiredAckCarrier struct {
	local   *localRegistration // completion identity only; no packet or native storage
	pn      protocol.PacketNumber
	expires monotime.Time
	targets ackTargets
}

type ackCarrierHistory struct{ carriers []retiredAckCarrier }

const maxRetiredAckCarriers = 64

func (h *ackCarrierHistory) add(pn protocol.PacketNumber, expires monotime.Time, targets ackTargets, local *localRegistration) {
	if targets.count == 0 {
		return
	}
	if len(h.carriers) == maxRetiredAckCarriers {
		h.carriers = slices.Delete(h.carriers, 0, 1)
	}
	h.carriers = append(h.carriers, retiredAckCarrier{pn: pn, expires: expires, targets: targets, local: local})
}

func (h *ackCarrierHistory) acknowledge(ack *wire.AckFrame, now monotime.Time, apply func(protocol.PathID, protocol.PacketNumber)) {
	h.carriers = slices.DeleteFunc(h.carriers, func(c retiredAckCarrier) bool {
		if !now.Before(c.expires) {
			return true
		}
		if !ack.AcksPacket(c.pn) {
			return false
		}
		if c.local != nil {
			if c.local.outcome == LocalPacketUnsubmitted {
				return true
			}
			c.local.acked = true
		}
		c.targets.apply(apply)
		return true
	})
}
