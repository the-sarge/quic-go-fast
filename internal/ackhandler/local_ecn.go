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
	records, scratch           []localECNRecord
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

// An unresolved transmission identity is a singleton. Once transmission is proved,
// the completion owner keeps any pending token and this projection retains only
// equivalent immutable marking facts, coalesced over consecutive packet numbers.
type localECNRecord struct {
	registration         *localRegistration
	pn, last             protocol.PacketNumber
	mark                 protocol.ECN
	testing, acked, lost bool
}

func (e *localECNEvidence) search(pn protocol.PacketNumber) int {
	i, _ := slices.BinarySearchFunc(e.records, pn, func(r localECNRecord, p protocol.PacketNumber) int {
		if r.last < p {
			return -1
		}
		if r.pn > p {
			return 1
		}
		return 0
	})
	return i
}

func (e *localECNEvidence) find(pn protocol.PacketNumber) *localECNRecord {
	i := e.search(pn)
	if i == len(e.records) || e.records[i].pn > pn {
		return nil
	}
	return &e.records[i]
}

func extendsLocalECNRange(a, b localECNRecord) bool {
	return a.registration == nil && b.registration == nil && a.last+1 == b.pn && a.mark == b.mark && a.testing == b.testing && a.acked == b.acked && a.lost == b.lost
}

// Only the two neighbours can become mergeable when a singleton is settled.
func (e *localECNEvidence) coalesce(pn protocol.PacketNumber) {
	i := e.search(pn)
	if i > 0 && extendsLocalECNRange(e.records[i-1], e.records[i]) {
		e.records[i-1].last = e.records[i].last
		e.records = slices.Delete(e.records, i, i+1)
		i--
	}
	if i+1 < len(e.records) && extendsLocalECNRange(e.records[i], e.records[i+1]) {
		e.records[i].last = e.records[i+1].last
		e.records = slices.Delete(e.records, i+1, i+2)
	}
}

func (e *localECNEvidence) appendScratch(h *sentPacketHandler, r localECNRecord) bool {
	if n := len(e.scratch); n > 0 && extendsLocalECNRange(e.scratch[n-1], r) {
		e.scratch[n-1].last = r.last
		return true
	}
	if len(e.scratch) == maxECNMarkRanges {
		e.fail(h)
		return false
	}
	if len(e.scratch) == cap(e.scratch) {
		scratch := make([]localECNRecord, len(e.scratch), min(maxECNMarkRanges, max(8, 2*cap(e.scratch))))
		copy(scratch, e.scratch)
		e.scratch = scratch
	}
	e.scratch = append(e.scratch, r)
	return true
}

// Rewrites use the same range ceiling and relocate the unaffected suffix once.
func (e *localECNEvidence) splice(h *sentPacketHandler, start, end int) bool {
	old, n := len(e.records), len(e.records)-(end-start)+len(e.scratch)
	if n > maxECNMarkRanges {
		e.fail(h)
		return false
	}
	if n > cap(e.records) {
		records := make([]localECNRecord, old, min(maxECNMarkRanges, max(n, 2*cap(e.records))))
		copy(records, e.records)
		e.records = records
	}
	e.records = e.records[:max(n, old)]
	copy(e.records[start+len(e.scratch):], e.records[end:old])
	copy(e.records[start:], e.scratch)
	clear(e.records[n:])
	e.records = e.records[:n]
	clear(e.scratch)
	e.scratch = e.scratch[:0]
	return true
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
	e.records = append(e.records, localECNRecord{registration: r, pn: r.pn, last: r.pn, mark: mark, testing: testing})
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
	if r.registration == nil || !marked(r.mark) {
		return
	}
	r.registration = nil
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
	if e.failed {
		return
	}
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
	e.reconcileTerminal(h, reg)
	if e.failed || e.closed || reg.level != protocol.Encryption1RTT {
		return
	}
	r := e.find(reg.pn)
	if r == nil || r.registration != reg {
		return
	}
	switch reg.outcome {
	case localPacketPending, LocalPacketUncertain:
		// Live possible transmission awaits an authenticated receipt or terminal reconciliation.
	case LocalPacketAccepted:
		e.qualify(h, r)
	case LocalPacketUnsubmitted:
		if marked(r.mark) {
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
				b.unsubmitted(r.pn, reg.richOrdinal)
			}
			e.updateConstraints(h, r.pn, r.mark, false)
			r.mark = protocol.ECNNon
			r.testing = false
			r.registration = nil
		}
	}
	if e.failed {
		return
	}
	e.coalesce(reg.pn)
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

func (e *localECNEvidence) fail(h *sentPacketHandler) {
	e.failed = true
	if b := h.bbrECN; b != nil {
		b.failEvidence()
	} else if t, ok := h.ecnTracker.(*ecnTracker); ok {
		t.state = ecnStateFailed
	}
	e.records = nil
	e.scratch = nil
	e.receipts = nil
	e.constraints = nil
	e.pending = false
}

func (e *localECNEvidence) close() {
	e.closed = true
	e.records = nil
	e.scratch = nil
	e.receipts = nil
	e.constraints = nil
	e.pending = false
}

// reconcileTerminal consumes protocol ownership and native outcome together.
// Retirement can precede native return, and packet content does not grant ECN
// authority. Neither fact alone establishes unavailable uncertain transmission.
func (e *localECNEvidence) reconcileTerminal(h *sentPacketHandler, reg *localRegistration) {
	if e.failed || e.closed || reg.level != protocol.Encryption1RTT || reg.packet != nil || reg.acked || reg.outcome != LocalPacketUncertain {
		return
	}
	r := e.find(reg.pn)
	if r == nil || r.registration != reg || !marked(r.mark) {
		return
	}
	e.fail(h)
}

func (e *localECNEvidence) lost(h *sentPacketHandler, pn protocol.PacketNumber) {
	r := e.find(pn)
	if r == nil || r.lost {
		return
	}
	if r.registration != nil {
		r.lost = true
		return
	}
	// Loss splits a settled range at one packet, preserving exact-once testing
	// loss accounting without restoring a per-packet completion identity.
	i := e.search(pn)
	start, end := max(0, i-1), min(len(e.records), i+2)
	for j := start; j < end; j++ {
		part := e.records[j]
		if j != i {
			if !e.appendScratch(h, part) {
				return
			}
			continue
		}
		if part.pn < pn {
			left := part
			left.last = pn - 1
			if !e.appendScratch(h, left) {
				return
			}
		}
		one := part
		one.pn, one.last, one.lost = pn, pn, true
		if !e.appendScratch(h, one) {
			return
		}
		if pn < part.last {
			part.pn = pn + 1
			if !e.appendScratch(h, part) {
				return
			}
		}
	}
	if e.splice(h, start, end) {
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
	// Rewrite only overlapping ranges and immediate neighbours. ACK ranges are
	// parser-owned, descending and non-overlapping; both cursors advance once.
	start, end := e.search(ack.LowestAcked()), e.search(ack.LargestAcked()+1)
	if end < len(e.records) && e.records[end].pn <= ack.LargestAcked() {
		end++
	}
	start, end = max(0, start-1), min(len(e.records), end+1)
	j := len(ack.AckRanges) - 1
	for _, r := range e.records[start:end] {
		for j >= 0 && ack.AckRanges[j].Largest < r.pn {
			j--
		}
		cursor := r.pn
		for i := j; i >= 0 && ack.AckRanges[i].Smallest <= r.last; i-- {
			a := ack.AckRanges[i]
			first, last := max(cursor, a.Smallest), min(r.last, a.Largest)
			if cursor < first {
				part := r
				part.pn, part.last = cursor, first-1
				if !e.appendScratch(h, part) {
					return
				}
			}
			part := r
			part.pn, part.last = first, last
			if reg := part.registration; reg != nil {
				reg.acked = true
				if reg.outcome == localPacketPending && !reg.earlyACK {
					reg.earlyACK = true
					if d := h.congestionEvents; d != nil {
						d.sampler.fenceTiming()
						d.advanceLocalQualification()
					}
				}
				e.qualify(h, &part)
				if e.failed {
					return
				}
			}
			if !part.acked {
				n := uint64(last - first + 1)
				if part.mark == protocol.ECT0 {
					newly.ECT0 += n
				}
				if part.mark == protocol.ECT1 {
					newly.ECT1 += n
				}
				testing = testing || part.testing
			}
			part.acked = true
			if !e.appendScratch(h, part) {
				return
			}
			cursor = last + 1
		}
		if cursor <= r.last {
			r.pn = cursor
			if !e.appendScratch(h, r) {
				return
			}
		}
	}
	if !e.splice(h, start, end) {
		return
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
		if r.registration != nil {
			break
		}
		if !r.acked && marked(r.mark) && r.last >= retireBelow {
			r.pn = max(r.pn, retireBelow)
			break
		}
		n++
	}
	copy(e.records, e.records[n:])
	clear(e.records[len(e.records)-n:])
	e.records = e.records[:len(e.records)-n]
}
