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
	possible, confirmed        congestion.ECNCounts
	constraints                []localECNConstraint
	frontier                   protocol.PacketNumber
	records                    []localECNRecord
	receipts                   []wire.AckRange
	watermark                  protocol.PacketNumber
	reported                   congestion.ECNCounts
	newly                      congestion.ECNCounts
	testingReceipt             bool
	unresolved, testingPending int
	pending, failed, closed    bool
}

// A constraint contains only a cumulative counter bound and the last marked
// registration that could have contributed. It is not an ACK snapshot. Later
// registrations cannot repair it; native facts update it until it is discharged.
type localECNConstraint struct {
	frontier                    protocol.PacketNumber
	counts, possible, confirmed congestion.ECNCounts
}

func ecnCountsFit(counts, sent congestion.ECNCounts) bool {
	return counts.ECT0 <= sent.ECT0 && counts.ECT1 <= sent.ECT1 && counts.ECT0+counts.ECT1+counts.CE <= sent.ECT0+sent.ECT1
}

func (e *localECNEvidence) updateConstraints(h *sentPacketHandler, pn protocol.PacketNumber, mark protocol.ECN, submitted bool) {
	for i := range e.constraints {
		c := &e.constraints[i]
		if pn > c.frontier {
			continue
		}
		target := &c.possible
		if submitted {
			target = &c.confirmed
		}
		if mark == protocol.ECT0 {
			if submitted {
				target.ECT0++
			} else {
				target.ECT0--
			}
		}
		if mark == protocol.ECT1 {
			if submitted {
				target.ECT1++
			} else {
				target.ECT1--
			}
		}
		if !ecnCountsFit(c.counts, c.possible) {
			e.fail(h)
			return
		}
	}
	e.constraints = slices.DeleteFunc(e.constraints, func(c localECNConstraint) bool { return ecnCountsFit(c.counts, c.confirmed) })
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
	if e.failed || e.closed || !marked(mark) {
		return
	}
	e.frontier = r.pn
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
	if len(e.records) == cap(e.records) {
		records := make([]localECNRecord, len(e.records), min(maxECNMarkRanges, max(8, 2*cap(e.records))))
		copy(records, e.records)
		e.records = records
	}
	e.records = append(e.records, localECNRecord{registration: r, pn: r.pn, mark: mark, testing: testing})
	if mark == protocol.ECT0 {
		e.possible.ECT0++
	}
	if mark == protocol.ECT1 {
		e.possible.ECT1++
	}
	if testing {
		e.testingPending++
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
	if r.testing {
		e.testingPending--
	}
	defer e.recheckTesting(h)
	if r.mark == protocol.ECT0 {
		e.confirmed.ECT0++
	} else {
		e.confirmed.ECT1++
	}
	e.updateConstraints(h, r.pn, r.mark, true)
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
				e.testingPending--
				e.releaseTest(h, r.pn)
			}
			if b := h.bbrECN; b != nil {
				e.removeMark(b, r.pn)
			}
			e.updateConstraints(h, r.pn, r.mark, false)
			r.mark = protocol.ECNNon
			r.testing = false
		}
	}
	e.recheckTesting(h)
	if e.pending && len(e.constraints) == 0 {
		h.beginCongestionFeedback(now, protocol.Encryption1RTT, h.bytesInFlight, nil, protocol.InvalidPacketNumber)
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
	e.constraints = nil
	e.pending = false
}

func (e *localECNEvidence) close() {
	e.closed = true
	e.records = nil
	e.receipts = nil
	e.constraints = nil
	e.pending = false
}

func (e *localECNEvidence) lost(h *sentPacketHandler, pn protocol.PacketNumber) {
	r := e.find(pn)
	if r == nil || r.lost {
		return
	}
	r.lost = true
	// Loss cannot establish native submission. Retire this unavailable proof
	// through the existing failed-evidence policy without inventing test credit.
	if !r.qualified && r.registration.outcome == LocalPacketUncertain {
		e.fail(h)
		return
	}
	if r.qualified {
		e.reportLoss(h, pn)
	}
}

func (e *localECNEvidence) recheckTesting(h *sentPacketHandler) {
	if e.failed || e.closed || e.testingPending != 0 {
		return
	}
	if b := h.bbrECN; b != nil {
		b.failTesting()
	} else if t, ok := h.ecnTracker.(*ecnTracker); ok && t.state == ecnStateUnknown {
		t.failIfMangled()
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
	if !ecnCountsFit(counts, e.possible) {
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
	if !ecnCountsFit(counts, e.confirmed) {
		c := localECNConstraint{frontier: e.frontier, counts: counts, possible: e.possible, confirmed: e.confirmed}
		if n := len(e.constraints); n > 0 && e.constraints[n-1].frontier == c.frontier {
			e.constraints[n-1] = c
		} else {
			if n == maxECNMarkRanges {
				e.fail(h)
				return
			}
			if len(e.constraints) == cap(e.constraints) {
				constraints := make([]localECNConstraint, len(e.constraints), min(maxECNMarkRanges, max(8, 2*cap(e.constraints))))
				copy(constraints, e.constraints)
				e.constraints = constraints
			}
			e.constraints = append(e.constraints, c)
		}
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
	if needed := len(e.receipts) + len(ack.AckRanges); needed > cap(e.receipts) {
		receipts := make([]wire.AckRange, len(e.receipts), min(maxECNMarkRanges, max(needed, 2*cap(e.receipts))))
		copy(receipts, e.receipts)
		e.receipts = receipts
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
	if !e.pending || e.failed || e.closed || len(e.constraints) != 0 {
		return
	}
	retireBelow := protocol.InvalidPacketNumber
	if b := h.bbrECN; b != nil {
		if b.state == ecnStateCapable {
			retireBelow = e.receipts[0].Smallest
		}
	} else if t, ok := h.ecnTracker.(*ecnTracker); ok && t.state == ecnStateCapable {
		retireBelow = e.receipts[0].Smallest
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
	// Retire only discharged obligations. Qualified records below a capable
	// report span follow the existing ECN ledger retirement rule; unresolved
	// transmissions retain their identity regardless of the reported span.
	n := 0
	for n < len(e.records) {
		r := &e.records[n]
		if !r.acked && r.registration.outcome != LocalPacketUnsubmitted && (!r.qualified || r.pn >= retireBelow) {
			break
		}
		n++
	}
	copy(e.records, e.records[n:])
	clear(e.records[len(e.records)-n:])
	e.records = e.records[:len(e.records)-n]
}
