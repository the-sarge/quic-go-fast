package ackhandler

import (
	"slices"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
)

// localECNEvidence qualifies the validators' registration-ordered marking facts.
// It retains no native storage. Both the marking identities and receipt union
// share the existing ECN ledger ceiling; exhaustion fails validation explicitly.
// A deferred report is summarized, never queued or replayed after a newer one.
type localECNEvidence struct {
	possible                congestion.ECNCounts
	records                 []localECNRecord
	receipts                []wire.AckRange
	watermark               protocol.PacketNumber
	reported                congestion.ECNCounts
	newly                   congestion.ECNCounts
	testingReceipt          bool
	unresolved              int
	pending, failed, closed bool
}

type localECNRecord struct {
	registration                    *localRegistration
	pn                              protocol.PacketNumber
	mark                            protocol.ECN
	testing, qualified, acked, lost bool
}

func (e *localECNEvidence) find(pn protocol.PacketNumber) *localECNRecord {
	i, ok := slices.BinarySearchFunc(e.records, pn, func(r localECNRecord, p protocol.PacketNumber) int {
		if r.pn < p {
			return -1
		}
		if r.pn > p {
			return 1
		}
		return 0
	})
	if !ok {
		return nil
	}
	return &e.records[i]
}

func marked(mark protocol.ECN) bool { return mark == protocol.ECT0 || mark == protocol.ECT1 }

func (h *sentPacketHandler) registerLocalECN(r *localRegistration, mark protocol.ECN) {
	e := h.local.ecn
	if e.failed || e.closed {
		return
	}
	if len(e.records) == maxECNMarkRanges {
		e.fail(h)
		return
	}
	testing := false
	if b := h.bbrECN; b != nil {
		for i := uint64(0); i < b.testingSent; i++ {
			testing = testing || b.testing[i].pn == r.pn
		}
		if mark == protocol.ECT0 {
			b.sent.ECT0--
		}
		if mark == protocol.ECT1 {
			b.sent.ECT1--
		}
	} else if tracker, ok := h.ecnTracker.(*ecnTracker); ok {
		testing = marked(mark) && (tracker.state == ecnStateTesting || tracker.state == ecnStateUnknown)
		if mark == protocol.ECT0 {
			tracker.numSentECT0--
		}
		if mark == protocol.ECT1 {
			tracker.numSentECT1--
		}
	}
	e.records = append(e.records, localECNRecord{registration: r, pn: r.pn, mark: mark, testing: testing})
	if mark == protocol.ECT0 {
		e.possible.ECT0++
	}
	if mark == protocol.ECT1 {
		e.possible.ECT1++
	}
	if marked(mark) {
		e.unresolved++
	}
}

func (e *localECNEvidence) qualify(h *sentPacketHandler, r *localECNRecord) {
	if r.qualified || !marked(r.mark) {
		return
	}
	r.qualified = true
	e.unresolved--
	if b := h.bbrECN; b != nil {
		if r.mark == protocol.ECT0 {
			b.sent.ECT0++
		} else {
			b.sent.ECT1++
		}
		// Late old-path transmissions remain part of the same cumulative fence.
		if b.draining {
			b.fence++
		}
	} else if t, ok := h.ecnTracker.(*ecnTracker); ok {
		if r.mark == protocol.ECT0 {
			t.numSentECT0++
		} else {
			t.numSentECT1++
		}
	}
	if r.lost {
		e.reportLoss(h, r.pn)
	}
}

func (h *sentPacketHandler) completeLocalECN(reg *localRegistration, now monotime.Time) {
	if h.local == nil || h.local.ecn == nil {
		return
	}
	e := h.local.ecn
	if e.failed || e.closed || reg.level != protocol.Encryption1RTT {
		return
	}
	r := e.find(reg.pn)
	if r == nil || r.registration != reg {
		return
	}
	switch reg.outcome {
	case localPacketPending, LocalPacketUncertain:
		// Possible transmission remains unresolved until an authenticated receipt.
	case LocalPacketAccepted:
		e.qualify(h, r)
	case LocalPacketUnsubmitted:
		if marked(r.mark) && !r.qualified {
			e.unresolved--
			if r.mark == protocol.ECT0 {
				e.possible.ECT0--
			} else {
				e.possible.ECT1--
			}
			if r.testing {
				e.releaseTest(h, r.pn)
			}
			if b := h.bbrECN; b != nil {
				e.removeMark(b, r.pn)
			}
			r.mark = protocol.ECNNon
			r.testing = false
		}
	}
	if e.pending {
		h.beginCongestionFeedback(now, protocol.Encryption1RTT, h.bytesInFlight, nil, e.watermark)
		e.apply(h, h.bytesInFlight)
		h.finishCongestionFeedback()
	}
}

func (e *localECNEvidence) releaseTest(h *sentPacketHandler, pn protocol.PacketNumber) {
	if b := h.bbrECN; b != nil {
		for i := uint64(0); i < b.testingSent; i++ {
			if b.testing[i].pn == pn {
				copy(b.testing[i:], b.testing[i+1:b.testingSent])
				b.testingSent--
				clear(b.testing[b.testingSent:])
				break
			}
		}
		if b.state == ecnStateUnknown && b.testingSent < numECNTestingPackets {
			b.state = ecnStateTesting
		}
	} else if t, ok := h.ecnTracker.(*ecnTracker); ok {
		t.numSentTesting--
		if t.state == ecnStateUnknown && t.numSentTesting < numECNTestingPackets {
			t.state = ecnStateTesting
			t.lastTestingPacket = protocol.InvalidPacketNumber
		}
	}
}

func (e *localECNEvidence) removeMark(b *bbrECNTracker, pn protocol.PacketNumber) {
	i := b.searchRanges(pn)
	if i == len(b.ranges) || b.ranges[i].first > pn {
		b.failEvidence()
		return
	}
	r := b.ranges[i]
	b.scratch = b.scratch[:0]
	if r.first < pn {
		part := r
		part.last = pn - 1
		b.appendScratch(part)
	}
	part := r
	part.first, part.last = pn, pn
	part.ordinal += uint64(pn - r.first)
	part.mark = protocol.ECNNon
	b.appendScratch(part)
	if pn < r.last {
		part = r
		part.first = pn + 1
		part.ordinal += uint64(part.first - r.first)
		b.appendScratch(part)
	}
	if len(b.ranges)-1+len(b.scratch) > maxECNMarkRanges {
		b.failEvidence()
		return
	}
	b.splice(i, i+1)
}

func (e *localECNEvidence) fail(h *sentPacketHandler) {
	e.failed = true
	if b := h.bbrECN; b != nil {
		b.failEvidence()
	} else if t, ok := h.ecnTracker.(*ecnTracker); ok {
		t.state = ecnStateFailed
	}
	e.records = nil
	e.receipts = nil
	e.pending = false
}

func (e *localECNEvidence) close() {
	e.closed = true
	e.records = nil
	e.receipts = nil
	e.pending = false
}

func (e *localECNEvidence) lost(h *sentPacketHandler, pn protocol.PacketNumber) {
	r := e.find(pn)
	if r == nil || r.lost {
		return
	}
	r.lost = true
	if r.qualified {
		e.reportLoss(h, pn)
	}
}

func (e *localECNEvidence) reportLoss(h *sentPacketHandler, pn protocol.PacketNumber) {
	if b := h.bbrECN; b != nil {
		b.lostPacket(pn)
	} else if h.ecnTracker != nil {
		h.ecnTracker.LostPacket(pn)
	}
}

func (e *localECNEvidence) feedback(h *sentPacketHandler, ack *wire.AckFrame, priorInFlight protocol.ByteCount) {
	if e.failed || e.closed {
		return
	}
	var newly congestion.ECNCounts
	testing := false
	// Search the receipt window, not the whole registration ledger. ACK ranges
	// are parser-owned, descending and non-overlapping.
	i, _ := slices.BinarySearchFunc(e.records, ack.LowestAcked(), func(r localECNRecord, p protocol.PacketNumber) int {
		if r.pn < p {
			return -1
		}
		if r.pn > p {
			return 1
		}
		return 0
	})
	j := len(ack.AckRanges) - 1
	for ; i < len(e.records) && e.records[i].pn <= ack.LargestAcked(); i++ {
		r := &e.records[i]
		for j >= 0 && ack.AckRanges[j].Largest < r.pn {
			j--
		}
		if j < 0 || ack.AckRanges[j].Smallest > r.pn {
			continue
		}
		if reg := r.registration; !reg.acked {
			reg.acked = true
			if reg.outcome == localPacketPending && !reg.earlyACK {
				reg.earlyACK = true
				if d := h.congestionEvents; d != nil {
					d.sampler.fenceTiming()
					d.advanceLocalQualification()
				}
			}
		}
		if marked(r.mark) {
			e.qualify(h, r)
		}
		if r.acked {
			continue
		}
		r.acked = true
		if r.mark == protocol.ECT0 {
			newly.ECT0++
		}
		if r.mark == protocol.ECT1 {
			newly.ECT1++
		}
		testing = testing || r.testing
	}
	if ack.LargestAcked() <= e.watermark {
		e.apply(h, priorInFlight)
		return
	}
	counts := congestion.ECNCounts{ECT0: ack.ECT0, ECT1: ack.ECT1, CE: ack.ECNCE}
	previous := e.reported
	if counts.ECT0 > e.possible.ECT0 || counts.ECT1 > e.possible.ECT1 || (h.bbrECN != nil && counts.ECT0+counts.ECT1+counts.CE > e.possible.ECT0+e.possible.ECT1) {
		e.fail(h)
		return
	}
	// These failures depend only on authenticated receipts/counters. Latch them
	// before replacing a deferred snapshot, even when transmission is unresolved.
	if counts.ECT0 < previous.ECT0 || counts.ECT1 < previous.ECT1 || counts.CE < previous.CE {
		e.fail(h)
		return
	}
	delta := congestion.ECNCounts{ECT0: counts.ECT0 - previous.ECT0, ECT1: counts.ECT1 - previous.ECT1, CE: counts.CE - previous.CE}
	if delta.ECT0+delta.CE < newly.ECT0 || delta.ECT1+delta.CE < newly.ECT1 || (h.bbrECN != nil && delta.ECT0+delta.ECT1+delta.CE < newly.ECT0+newly.ECT1) {
		e.fail(h)
		return
	}
	e.reported, e.watermark = counts, ack.LargestAcked()
	e.newly.ECT0 += newly.ECT0
	e.newly.ECT1 += newly.ECT1
	e.testingReceipt = e.testingReceipt || testing
	// This is a bounded union of marking receipts, not a queue of counter
	// snapshots. Only the latest counters can be delivered to either validator.
	if len(ack.AckRanges) > maxECNMarkRanges-len(e.receipts) {
		e.fail(h)
		return
	}
	e.receipts = append(e.receipts, ack.AckRanges...)
	slices.SortFunc(e.receipts, func(a, b wire.AckRange) int {
		if a.Smallest < b.Smallest {
			return -1
		}
		if a.Smallest > b.Smallest {
			return 1
		}
		return 0
	})
	n := 0
	for _, r := range e.receipts {
		if n > 0 && r.Smallest <= e.receipts[n-1].Largest+1 {
			e.receipts[n-1].Largest = max(e.receipts[n-1].Largest, r.Largest)
		} else {
			e.receipts[n] = r
			n++
		}
	}
	e.receipts = e.receipts[:n]
	if n > maxECNMarkRanges {
		e.fail(h)
		return
	}
	e.pending = true
	e.apply(h, priorInFlight)
}

func (e *localECNEvidence) apply(h *sentPacketHandler, priorInFlight protocol.ByteCount) {
	if !e.pending || e.failed || e.closed || e.unresolved != 0 {
		return
	}
	if b := h.bbrECN; b != nil {
		slices.Reverse(e.receipts)
		result := b.feedback(&wire.AckFrame{AckRanges: e.receipts, ECT0: e.reported.ECT0, ECT1: e.reported.ECT1, ECNCE: e.reported.CE})
		slices.Reverse(e.receipts)
		if h.congestionEvents != nil {
			h.congestionEvents.event.ECN = result
			h.congestionEvents.event.ECNChecked = result.Eligible
			h.congestionEvents.event.Congested = result.Eligible && result.Delta.CE > 0
		}
	} else if t, ok := h.ecnTracker.(*ecnTracker); ok {
		congested := t.handleCounts(int64(e.newly.ECT0), int64(e.newly.ECT1), e.testingReceipt, int64(e.reported.ECT0), int64(e.reported.ECT1), int64(e.reported.CE))
		if congested {
			h.congestion.OnCongestionEvent(congestion.NewPacketKey(protocol.Encryption1RTT, e.watermark), 0, priorInFlight)
		}
	}
	e.pending = false
	e.newly = congestion.ECNCounts{}
	e.testingReceipt = false
	e.receipts = e.receipts[:0]
	// Retain unresolved and unacknowledged identities, including testing holes.
	// ACKed prefixes have discharged their per-packet marking obligation.
	n := 0
	for n < len(e.records) && e.records[n].acked {
		n++
	}
	copy(e.records, e.records[n:])
	clear(e.records[len(e.records)-n:])
	e.records = e.records[:len(e.records)-n]
}
