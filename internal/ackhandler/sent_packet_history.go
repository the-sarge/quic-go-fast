package ackhandler

import (
	"fmt"
	"iter"
	"slices"

	"github.com/quic-go/quic-go/internal/protocol"
	list "github.com/quic-go/quic-go/internal/utils/linkedlist"
)

const maxSkippedPackets = 4

type sentPacketHistory struct {
	packets        []*packet
	skippedPackets []protocol.PacketNumber
	// Keep probes in send order for timeouts, with indexed O(1) removal on ACKs.
	pathProbePackets list.List[packetWithPacketNumber]
	pathProbeIndex   map[protocol.PacketNumber]*list.Element[packetWithPacketNumber]

	numOutstanding int

	firstPacketNumber   protocol.PacketNumber
	highestPacketNumber protocol.PacketNumber
}

func newSentPacketHistory(isAppData bool) *sentPacketHistory {
	h := &sentPacketHistory{
		highestPacketNumber: protocol.InvalidPacketNumber,
		firstPacketNumber:   protocol.InvalidPacketNumber,
	}
	if isAppData {
		h.packets = make([]*packet, 0, 32)
		h.skippedPackets = make([]protocol.PacketNumber, 0, maxSkippedPackets)
	} else {
		h.packets = make([]*packet, 0, 6)
	}
	return h
}

func (h *sentPacketHistory) checkSequentialPacketNumberUse(pn protocol.PacketNumber) {
	if h.highestPacketNumber != protocol.InvalidPacketNumber {
		if pn != h.highestPacketNumber+1 {
			panic("non-sequential packet number use")
		}
	}
	h.highestPacketNumber = pn
	if len(h.packets) == 0 {
		h.firstPacketNumber = pn
	}
}

func (h *sentPacketHistory) SkippedPacket(pn protocol.PacketNumber) {
	h.checkSequentialPacketNumberUse(pn)
	if len(h.packets) > 0 {
		h.packets = append(h.packets, nil)
	}
	if len(h.skippedPackets) == maxSkippedPackets {
		h.skippedPackets = slices.Delete(h.skippedPackets, 0, 1)
	}
	h.skippedPackets = append(h.skippedPackets, pn)
}

func (h *sentPacketHistory) SentPacket(pn protocol.PacketNumber, p *packet) {
	h.checkSequentialPacketNumberUse(pn)
	h.packets = append(h.packets, p)
	if p.Outstanding() {
		h.numOutstanding++
	}
}

func (h *sentPacketHistory) SentPathProbePacket(pn protocol.PacketNumber, p *packet) {
	h.checkSequentialPacketNumberUse(pn)
	h.packets = append(h.packets, &packet{isPathProbePacket: true})
	if h.pathProbeIndex == nil {
		h.pathProbeIndex = make(map[protocol.PacketNumber]*list.Element[packetWithPacketNumber])
	}
	h.pathProbeIndex[pn] = h.pathProbePackets.PushBack(packetWithPacketNumber{PacketNumber: pn, packet: p})
}

func (h *sentPacketHistory) Packets() iter.Seq2[protocol.PacketNumber, *packet] {
	return func(yield func(protocol.PacketNumber, *packet) bool) {
		// h.firstPacketNumber might be updated in the yield function,
		// so we need to save it here.
		firstPacketNumber := h.firstPacketNumber
		for i, p := range h.packets {
			if p == nil {
				continue
			}
			if !yield(firstPacketNumber+protocol.PacketNumber(i), p) {
				return
			}
		}
	}
}

// PathProbes permits removing the current probe while iterating.
func (h *sentPacketHistory) PathProbes() iter.Seq2[protocol.PacketNumber, *packet] {
	return func(yield func(protocol.PacketNumber, *packet) bool) {
		for el := h.pathProbePackets.Front(); el != nil; {
			next := el.Next()
			p := el.Value
			if !yield(p.PacketNumber, p.packet) {
				return
			}
			el = next
		}
	}
}

// FirstOutstanding returns the first outstanding packet.
func (h *sentPacketHistory) FirstOutstanding() (protocol.PacketNumber, *packet) {
	if !h.HasOutstandingPackets() {
		return protocol.InvalidPacketNumber, nil
	}
	for i, p := range h.packets {
		if p != nil && p.Outstanding() {
			return h.firstPacketNumber + protocol.PacketNumber(i), p
		}
	}
	return protocol.InvalidPacketNumber, nil
}

// FirstOutstandingPathProbe returns the first outstanding path probe packet
func (h *sentPacketHistory) FirstOutstandingPathProbe() (protocol.PacketNumber, *packet) {
	el := h.pathProbePackets.Front()
	if el == nil {
		return protocol.InvalidPacketNumber, nil
	}
	return el.Value.PacketNumber, el.Value.packet
}

func (h *sentPacketHistory) SkippedPackets() iter.Seq[protocol.PacketNumber] {
	return func(yield func(protocol.PacketNumber) bool) {
		for _, p := range h.skippedPackets {
			if !yield(p) {
				return
			}
		}
	}
}

func (h *sentPacketHistory) Len() int {
	return len(h.packets)
}

func (h *sentPacketHistory) NumOutstanding() int {
	return h.numOutstanding
}

// Remove removes a packet from the sent packet history.
// It must not be used for skipped packet numbers.
func (h *sentPacketHistory) Remove(pn protocol.PacketNumber) error {
	idx, ok := h.getIndex(pn)
	if !ok {
		return fmt.Errorf("packet %d not found in sent packet history", pn)
	}
	p := h.packets[idx]
	if p.Outstanding() {
		h.numOutstanding--
		if h.numOutstanding < 0 {
			panic("negative number of outstanding packets")
		}
	}
	h.packets[idx] = nil
	// clean up all skipped packets directly before this packet number
	var hasPacketBefore bool
	for idx > 0 {
		idx--
		if h.packets[idx] != nil {
			hasPacketBefore = true
			break
		}
	}
	if !hasPacketBefore {
		h.cleanupStart()
	}
	if len(h.packets) > 0 && h.packets[0] == nil {
		panic("cleanup failed")
	}
	return nil
}

// RemovePathProbe retires both the probe and its regular history marker.
// The marker may already have been removed by loss detection on the active path.
// Indexed removal is O(1); marker prefix cleanup is amortized O(1).
func (h *sentPacketHistory) RemovePathProbe(pn protocol.PacketNumber) *packet {
	el := h.pathProbeIndex[pn]
	if el == nil {
		return nil
	}
	p := h.pathProbePackets.Remove(el).packet
	delete(h.pathProbeIndex, pn)
	h.DeclareLost(pn)
	return p
}

// getIndex gets the index of packet p in the packets slice.
func (h *sentPacketHistory) getIndex(p protocol.PacketNumber) (int, bool) {
	if len(h.packets) == 0 {
		return 0, false
	}
	if p < h.firstPacketNumber {
		return 0, false
	}
	index := int(p - h.firstPacketNumber)
	if index > len(h.packets)-1 {
		return 0, false
	}
	return index, true
}

func (h *sentPacketHistory) HasOutstandingPackets() bool {
	return h.numOutstanding > 0
}

func (h *sentPacketHistory) HasOutstandingPathProbes() bool {
	return h.pathProbePackets.Len() > 0
}

// delete all nil entries at the beginning of the packets slice
func (h *sentPacketHistory) cleanupStart() {
	for i, p := range h.packets {
		if p != nil {
			h.packets = h.packets[i:]
			h.firstPacketNumber += protocol.PacketNumber(i)
			return
		}
	}
	h.packets = h.packets[:0]
	h.firstPacketNumber = protocol.InvalidPacketNumber
}

func (h *sentPacketHistory) LowestPacketNumber() protocol.PacketNumber {
	if len(h.packets) == 0 {
		return protocol.InvalidPacketNumber
	}
	return h.firstPacketNumber
}

func (h *sentPacketHistory) DeclareLost(pn protocol.PacketNumber) {
	idx, ok := h.getIndex(pn)
	if !ok {
		return
	}
	p := h.packets[idx]
	if p == nil {
		return
	}
	if p.Outstanding() {
		h.numOutstanding--
		if h.numOutstanding < 0 {
			panic("negative number of outstanding packets")
		}
	}
	h.packets[idx] = nil
	if idx == 0 {
		h.cleanupStart()
	}
}

// Difference returns the difference between two packet numbers a and b (a - b),
// taking into account any skipped packet numbers between them.
//
// Note that old skipped packets are garbage collected at some point,
// so this function is not guaranteed to return the correct result after a while.
func (h *sentPacketHistory) Difference(a, b protocol.PacketNumber) protocol.PacketNumber {
	diff := a - b
	if len(h.skippedPackets) == 0 {
		return diff
	}
	if a < h.skippedPackets[0] || b > h.skippedPackets[len(h.skippedPackets)-1] {
		return diff
	}
	for _, p := range h.skippedPackets {
		if p > b && p < a {
			diff--
		}
	}
	return diff
}
