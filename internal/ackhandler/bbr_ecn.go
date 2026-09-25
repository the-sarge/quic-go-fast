package ackhandler

import (
	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
)

// EnableBBRECN installs the private validation policy before any registration.
// path reports connection-owned generation and endpoint capability, plus the
// synchronized completion of all old-generation local sends.
func EnableBBRECN(handler SentPacketHandler, path func() (generation uint64, drained, capable bool)) {
	h, ok := handler.(*sentPacketHandler)
	if !ok || h.bytesSent != 0 || h.congestionEvents == nil || h.bbrECN != nil || path == nil {
		panic("invalid BBR ECN installation")
	}
	h.bbrECN = &bbrECNTracker{path: path, watermark: protocol.InvalidPacketNumber}
	h.ecnTracker = nil
}

const maxECNMarkRanges = 4096

type ecnMarkRange struct {
	first, last         protocol.PacketNumber
	ordinal, generation uint64
	mark                protocol.ECN
	acked               bool
}

// The marking ledger is independent of recovery and sampler retirement. Only
// sent facts enter it; packet-number interval membership never implies ECT.
type bbrECNTracker struct {
	draining, closed bool
	fence            uint64
	testing          [numECNTestingPackets]struct {
		pn   protocol.PacketNumber
		lost bool
	}
	testingLost      uint64
	offsets          congestion.ECNCounts
	compacted        congestion.ECNCounts
	compactedOrdinal uint64
	evidenceLost     bool
	counterFailed    bool
	path             func() (uint64, bool, bool)
	ranges           []ecnMarkRange
	state            ecnState
	generation       uint64
	watermark        protocol.PacketNumber
	sent, accepted   congestion.ECNCounts
	testingSent      uint64
}

func (e *bbrECNTracker) mode(shortHeader bool) protocol.ECN {
	if e.closed {
		return protocol.ECNNon
	}
	generation, drained, capable := e.path()
	// Unsupported endpoints must omit ECN ancillary data entirely, including
	// explicit Not-ECT. The basic and unqualified OOB writers enforce this.
	if !capable {
		return protocol.ECNUnsupported
	}
	if !shortHeader {
		return protocol.ECNNon
	}
	if e.draining {
		if e.evidenceLost || e.counterFailed || generation != e.generation || !drained || !capable || e.accepted.ECT0+e.accepted.ECT1+e.accepted.CE != e.fence {
			return protocol.ECNNon
		}
		e.draining = false
		e.offsets = e.accepted
		e.state = ecnStateInitial
		e.testingSent, e.testingLost = 0, 0
		clear(e.testing[:])
	}
	if e.state == ecnStateInitial {
		e.state = ecnStateTesting
	}
	if e.state == ecnStateTesting || e.state == ecnStateCapable {
		return protocol.ECT0
	}
	return protocol.ECNNon
}

func (e *bbrECNTracker) sentPacket(pn protocol.PacketNumber, ordinal, generation uint64, mark protocol.ECN) {
	if mark == protocol.ECT0 {
		e.sent.ECT0++
	}
	if mark == protocol.ECT1 {
		e.sent.ECT1++
	}
	if e.evidenceLost || e.counterFailed || e.closed {
		return
	}
	if mark == protocol.ECNUnsupported {
		mark = protocol.ECNNon
	}
	r := ecnMarkRange{first: pn, last: pn, ordinal: ordinal, generation: generation, mark: mark}
	if !e.appendRange(&e.ranges, r) {
		return
	}

	if e.state == ecnStateTesting && (mark == protocol.ECT0 || mark == protocol.ECT1) {
		e.testing[e.testingSent].pn = pn
		e.testingSent++
		if e.testingSent == numECNTestingPackets {
			e.state = ecnStateUnknown
		}
	}
}

func (e *bbrECNTracker) feedback(ack *wire.AckFrame) congestion.ECNResult {
	result := congestion.ECNResult{Reported: congestion.ECNCounts{ECT0: ack.ECT0, ECT1: ack.ECT1, CE: ack.ECNCE}, Deferred: ack.LargestAcked() <= e.watermark, Accepted: e.accepted, Watermark: e.watermark, PathGeneration: e.generation, Failed: e.state == ecnStateFailed}
	if e.closed || e.evidenceLost || e.state == ecnStateFailed || ack.LargestAcked() <= e.watermark {
		return result
	}
	counts := congestion.ECNCounts{ECT0: ack.ECT0, ECT1: ack.ECT1, CE: ack.ECNCE}
	var newly congestion.ECNCounts
	var anchor uint64
	for _, r := range e.ranges {
		for _, a := range ack.AckRanges {
			first, last := max(r.first, a.Smallest), min(r.last, a.Largest)
			if first > last {
				continue
			}
			if last == ack.LargestAcked() {
				anchor = r.ordinal + uint64(last-r.first)
			}
			if r.acked {
				continue
			}
			n := uint64(last - first + 1)
			if r.mark == protocol.ECT0 {
				newly.ECT0 += n
			}
			if r.mark == protocol.ECT1 {
				newly.ECT1 += n
			}
		}
	}
	if counts.ECT0 < e.accepted.ECT0 || counts.ECT1 < e.accepted.ECT1 || counts.CE < e.accepted.CE || counts.ECT0 > e.sent.ECT0 || counts.ECT1 > e.sent.ECT1 || counts.CE > e.sent.ECT0+e.sent.ECT1-counts.ECT0-counts.ECT1 {
		e.failCounters()
		result.Failed = true
		return result
	}
	if anchor == 0 {
		e.failEvidence()
		result.Failed = true
		return result
	}
	delta := congestion.ECNCounts{ECT0: counts.ECT0 - e.accepted.ECT0, ECT1: counts.ECT1 - e.accepted.ECT1, CE: counts.CE - e.accepted.CE}
	if delta.ECT0+delta.CE < newly.ECT0 || delta.ECT1+delta.CE < newly.ECT1 || delta.ECT0+delta.ECT1+delta.CE < newly.ECT0+newly.ECT1 {
		e.failCounters()
		result.Failed = true
		return result
	}
	// Split only at ACK boundaries, retaining actual marks and affine ordinals.
	var next []ecnMarkRange
	for _, r := range e.ranges {
		cursor := r.first
		for i := len(ack.AckRanges) - 1; i >= 0; i-- {
			a := ack.AckRanges[i]
			first, last := max(cursor, a.Smallest), min(r.last, a.Largest)
			if first > last {
				continue
			}
			if cursor < first {
				part := r
				part.first = cursor
				part.last = first - 1
				part.ordinal += uint64(cursor - r.first)
				if !e.appendRange(&next, part) {
					result.Failed = true
					return result
				}
			}
			part := r
			part.first = first
			part.last = last
			part.ordinal += uint64(first - r.first)
			part.acked = true
			if !e.appendRange(&next, part) {
				result.Failed = true
				return result
			}
			cursor = last + 1
		}
		if cursor <= r.last {
			part := r
			part.first = cursor
			part.ordinal += uint64(cursor - r.first)
			if !e.appendRange(&next, part) {
				result.Failed = true
				return result
			}
		}
	}
	e.ranges = next
	e.compact()
	e.accepted, e.watermark = counts, ack.LargestAcked()
	if (e.state == ecnStateTesting || e.state == ecnStateUnknown) && newly.ECT0+newly.ECT1 > 0 && delta.ECT0+delta.ECT1 > 0 {
		e.state = ecnStateCapable
	}
	result.Accepted, result.Delta, result.Watermark, result.Ordinal = counts, delta, e.watermark, anchor
	e.failTesting()
	result.Failed = e.state == ecnStateFailed
	result.Eligible = !e.draining && e.state == ecnStateCapable
	return result
}

// All ACKed marked records were covered by accepted counter increments. Only
// that fully accounted prefix can be replaced with cumulative ordinal offsets.
func (e *bbrECNTracker) compact() {
	n := 0
	for n < len(e.ranges) && e.ranges[n].acked {
		r := e.ranges[n]
		count := uint64(r.last - r.first + 1)
		if r.mark == protocol.ECT0 {
			e.compacted.ECT0 += count
		}
		if r.mark == protocol.ECT1 {
			e.compacted.ECT1 += count
		}
		e.compactedOrdinal = r.ordinal + count - 1
		n++
	}
	copy(e.ranges, e.ranges[n:])
	clear(e.ranges[len(e.ranges)-n:])
	e.ranges = e.ranges[:len(e.ranges)-n]
}

func (e *bbrECNTracker) failEvidence() {
	e.evidenceLost = true
	e.state = ecnStateFailed
}

func (e *bbrECNTracker) failTesting() {
	if e.state == ecnStateUnknown && e.accepted.CE-e.offsets.CE+e.testingLost >= e.testingSent {
		e.state = ecnStateFailed
	}
}

func (e *bbrECNTracker) lostPacket(pn protocol.PacketNumber) {
	if e.state != ecnStateTesting && e.state != ecnStateUnknown {
		return
	}
	for i := uint64(0); i < e.testingSent; i++ {
		if e.testing[i].pn == pn && !e.testing[i].lost {
			e.testing[i].lost = true
			e.testingLost++
			break
		}
	}
	e.failTesting()
}

func (e *bbrECNTracker) resetPath(generation uint64) {
	e.generation = generation
	e.draining = true
	e.fence = e.sent.ECT0 + e.sent.ECT1
	// Keep sent history, accepted counters and watermark across every reset.
	// Failed evidence cannot be repaired by a new address or a sampler reset.
	e.state = ecnStateInitial
	if e.counterFailed || e.evidenceLost {
		e.state = ecnStateFailed
	}
}

// appendRange is the common representation boundary for registration and ACK
// splitting. Capacity is charged only after merging equivalent adjacent facts.
func (e *bbrECNTracker) appendRange(ranges *[]ecnMarkRange, r ecnMarkRange) bool {
	if n := len(*ranges); n > 0 {
		last := &(*ranges)[n-1]
		if last.last+1 == r.first && last.mark == r.mark && last.generation == r.generation && last.acked == r.acked && last.ordinal+uint64(r.first-last.first) == r.ordinal {
			last.last = r.last
			return true
		}
	}
	if len(*ranges) == maxECNMarkRanges {
		e.failEvidence()
		return false
	}
	if len(*ranges) == cap(*ranges) {
		grown := make([]ecnMarkRange, len(*ranges), min(maxECNMarkRanges, max(8, 2*cap(*ranges))))
		copy(grown, *ranges)
		*ranges = grown
	}
	*ranges = append(*ranges, r)
	return true
}

// Counter consistency is connection-wide; a new address cannot repair invalid
// cumulative evidence. Path-local all-CE/loss testing uses state alone instead.
func (e *bbrECNTracker) failCounters() {
	e.counterFailed = true
	e.state = ecnStateFailed
}
