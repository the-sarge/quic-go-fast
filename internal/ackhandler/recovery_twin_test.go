package ackhandler

import (
	"cmp"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

// recoveryTwin drives the candidate dispatch and the frozen reference through
// identical operations in the sent-packet handler's order and compares every
// observable after each operation. Handler glue that C1 does not change, such
// as loss capture and controller-independent feedback completion, is applied
// to both sides with explicit RTT-derived inputs.
type recoveryTwin struct {
	t   testing.TB
	h   *sentPacketHandler
	d   *congestionDispatch
	ref *frozenDispatch

	sink      *twinSink
	now       monotime.Time
	pto       time.Duration
	lossDelay time.Duration
	inflight  protocol.ByteCount
	pending   protocol.ByteCount

	nextPN       [3]protocol.PacketNumber
	outstanding  [3][]*twinPacket
	coverage     twinCoverage
	priorExpiry  monotime.Time
	priorExpired uint64
	last         congestion.FeedbackEvent // latest reference feedback event
	ops          int
	checkEvery   int
}

type twinPacket struct {
	pn      protocol.PacketNumber
	ordinal uint64
	p       packet
}

// twinCoverage counts semantic events produced by the frozen reference.
type twinCoverage struct {
	Persistent, Entered, Exited, UndoEligible        int
	OutcomeEvicted, RetainedEvicted, RetainedExpired uint64
	StaleDeadlines, Feedbacks, SuppressedFeedbacks   int
}

func newRecoveryTwin(t testing.TB) *recoveryTwin {
	w := &recoveryTwin{t: t, sink: &twinSink{}, now: monotime.Time(time.Hour), pto: 60 * time.Millisecond, lossDelay: 25 * time.Millisecond, checkEvery: 1}
	w.d = &congestionDispatch{sink: w.sink, pending: func() protocol.ByteCount { return w.pending }}
	w.h = &sentPacketHandler{congestionEvents: w.d}
	w.ref = newFrozenDispatch()
	return w
}

func (w *recoveryTwin) advance(d time.Duration) { w.now = w.now.Add(d) }

type twinSend struct {
	level                   protocol.EncryptionLevel
	length                  protocol.ByteCount
	ackOnly, pathProbe, mtu bool
	skip                    protocol.PacketNumber
	backdate                time.Duration
}

func (w *recoveryTwin) send(o twinSend) *twinPacket {
	w.t.Helper()
	if o.level == 0 {
		o.level = protocol.Encryption1RTT
	}
	if o.length == 0 {
		o.length = 1200
	}
	s := recoverySpace(congestionKey(o.level, 0).space)
	w.nextPN[s] += o.skip
	pn := w.nextPN[s]
	w.nextPN[s]++
	p := packet{SendTime: w.now - monotime.Time(o.backdate), Length: o.length, EncryptionLevel: o.level, IsPathMTUProbePacket: o.mtu, isPathProbePacket: o.pathProbe, LargestAcked: protocol.InvalidPacketNumber}
	if !o.ackOnly {
		p.Frames = []Frame{{}}
	}
	prior := w.inflight
	if !o.pathProbe && p.IsAckEliciting() {
		p.includedInBytesInFlight = true
		w.inflight += p.Length
	}
	cand, ref := p, p
	w.h.bytesInFlight = w.inflight
	w.sink.sent = w.sink.sent[:0]
	w.h.captureCongestionSend(pn, &cand, protocol.ECNNon, prior)
	info := w.ref.send(pn, &ref, prior, w.inflight, w.pending)
	require.Len(w.t, w.sink.sent, 1)
	require.Equal(w.t, info, w.sink.sent[0].Packet, "send event")
	tp := &twinPacket{pn: pn, ordinal: info.Ordinal, p: p}
	w.outstanding[s] = append(w.outstanding[s], tp)
	w.after()
	return tp
}

func (w *recoveryTwin) take(level protocol.EncryptionLevel, keep func(*twinPacket) bool) []*twinPacket {
	s := recoverySpace(congestionKey(level, 0).space)
	var taken []*twinPacket
	w.outstanding[s] = slices.DeleteFunc(w.outstanding[s], func(p *twinPacket) bool {
		if keep(p) {
			return false
		}
		taken = append(taken, p)
		if p.p.includedInBytesInFlight {
			w.inflight -= p.p.Length
		}
		return true
	})
	return taken
}

type twinAck struct {
	rtt bool
	// lose declares outstanding packets lost after PTO confirmation, in
	// packet-number order, as the handler's loss detection does.
	lose func(*twinPacket) bool
}

func (w *recoveryTwin) ack(level protocol.EncryptionLevel, frame *wire.AckFrame, o twinAck) protocol.PacketNumber {
	w.t.Helper()
	prior := w.inflight
	newly := w.take(level, func(p *twinPacket) bool { return !frame.AcksPacket(p.pn) })
	w.h.ExpireDelivery(w.now)
	w.refExpire(w.now)
	space := congestionKey(level, 0).space
	cutoff := w.now.Add(-w.lossDelay)
	largest := frame.LargestAcked()
	acked := make([]packetWithPacketNumber, len(newly))
	for i, p := range newly {
		acked[i] = packetWithPacketNumber{PacketNumber: p.pn, packet: &p.p}
	}
	if len(acked) == 0 {
		acked = nil
	}
	w.h.beginCongestionFeedback(w.now, level, prior, acked, largest)
	w.ref.begin(w.now, level, prior, acked, largest)
	x, y := w.h.appendRetainedAck(frame, level), w.ref.appendRetainedAck(frame, level)
	require.Equal(w.t, y, x, "ACK witness")
	if len(acked) == 0 {
		w.d.recovery.confirmPTO(space, x, cutoff)
		w.ref.recovery.confirmPTO(space, y, cutoff)
		need := len(w.d.event.Acked) > 0 || w.d.recovery.needsAckFeedback()
		require.Equal(w.t, len(w.ref.event.Acked) > 0 || w.ref.recovery.needsAckFeedback(), need, "ACK feedback need")
		if need {
			w.finish()
		} else {
			w.coverage.SuppressedFeedbacks++
			w.after()
		}
		return x
	}
	if o.rtt {
		last := newly[len(newly)-1]
		for _, e := range []*congestion.FeedbackEvent{&w.d.event, &w.ref.event} {
			e.RTTEligible, e.RTTUpdated = true, w.now.After(last.p.SendTime)
			e.RawRTT = max(0, w.now.Sub(last.p.SendTime))
		}
	}
	w.d.recovery.confirmPTO(space, x, cutoff)
	w.ref.recovery.confirmPTO(space, y, cutoff)
	if o.lose != nil {
		for _, p := range w.take(level, func(p *twinPacket) bool { return p.pn > largest || !o.lose(p) }) {
			w.lose(level, p)
		}
	}
	w.finish()
	return x
}

// timerLoss runs a loss-timer feedback event without an ACK.
func (w *recoveryTwin) timerLoss(level protocol.EncryptionLevel, lose func(*twinPacket) bool) {
	w.t.Helper()
	prior := w.inflight
	w.h.beginCongestionFeedback(w.now, level, prior, nil, protocol.InvalidPacketNumber)
	w.ref.begin(w.now, level, prior, nil, protocol.InvalidPacketNumber)
	for _, p := range w.take(level, func(p *twinPacket) bool { return !lose(p) }) {
		w.lose(level, p)
	}
	w.finish()
}

func (w *recoveryTwin) lose(level protocol.EncryptionLevel, tp *twinPacket) {
	d, p, pn := w.d, &tp.p, tp.pn
	// Candidate: captureCongestionLoss with the level's PTO supplied here.
	key := congestionKey(level, pn)
	congestionLoss := p.includedInBytesInFlight && !p.IsPathMTUProbePacket && !p.isPathProbePacket
	_, retained := d.packets.get(key)
	d.recovery.lost(key, congestionLoss, retained, d.ordinal)
	if info, ok := d.packets.get(key); ok && congestionLoss {
		start := len(d.scratch) - len(d.event.Lost) - 1
		d.scratch[start] = info
		d.event.Lost = d.scratch[start:]
	}
	d.retire(key, d.event.Time, w.pto, deliveryRetiredLoss)
	ref := tp.p
	w.ref.loss(level, pn, &ref, w.pto)
}

func (w *recoveryTwin) finish() {
	w.t.Helper()
	d := w.d
	smoothed := w.pto / 3
	d.event.SmoothedRTT = smoothed
	d.event.PostInFlight = w.inflight
	d.event.PendingLocal = w.pending
	slices.SortFunc(d.event.Lost, func(a, b congestion.PacketInfo) int { return cmp.Compare(a.Ordinal, b.Ordinal) })
	d.sampler.feedback(&d.event)
	d.recovery.feedback(&d.event, w.pto)
	emitted := d.event.HasAck || len(d.event.Lost) > 0 || d.event.PriorInFlight != d.event.PostInFlight
	got := d.event
	got.Acked, got.Lost = slices.Clone(got.Acked), slices.Clone(got.Lost)
	clear(d.event.Acked)
	clear(d.event.Lost)
	d.event.Acked = d.event.Acked[:0]
	d.event.Lost = d.event.Lost[:0]

	want, wantEmitted := w.ref.finish(smoothed, w.inflight, w.pending, w.pto)
	normalizeEvent(&got)
	normalizeEvent(&want)
	require.Equal(w.t, wantEmitted, emitted, "feedback presence")
	require.Equal(w.t, want, got, "feedback event")
	w.coverage.Feedbacks++
	w.last = want
	if want.PersistentCongestion.EndOrdinal != 0 {
		w.coverage.Persistent++
	}
	if want.RecoveryEpisode.Entered {
		w.coverage.Entered++
	}
	if want.RecoveryEpisode.Exited {
		w.coverage.Exited++
	}
	if want.RecoveryEpisode.UndoEligible {
		w.coverage.UndoEligible++
	}
	w.after()
}

func normalizeEvent(e *congestion.FeedbackEvent) {
	if len(e.Acked) == 0 {
		e.Acked = nil
	}
	if len(e.Lost) == 0 {
		e.Lost = nil
	}
}

// retirePTO extracts the first outstanding packet of a space for a probe, as
// QueueProbePacketAt does.
func (w *recoveryTwin) retirePTO(level protocol.EncryptionLevel) bool {
	w.t.Helper()
	s := recoverySpace(congestionKey(level, 0).space)
	if len(w.outstanding[s]) == 0 {
		return false
	}
	tp := w.outstanding[s][0]
	w.take(level, func(p *twinPacket) bool { return p != tp })
	key := congestionKey(tp.p.EncryptionLevel, tp.pn)
	w.d.recovery.retirePTO(key)
	w.d.retire(key, w.now, w.pto, deliveryRetiredPTO)
	w.ref.recovery.retirePTO(key)
	w.ref.retire(key, w.now, w.pto, deliveryRetiredPTO)
	w.after()
	return true
}

func (w *recoveryTwin) discardPacket(tp *twinPacket) {
	w.t.Helper()
	w.take(tp.p.EncryptionLevel, func(p *twinPacket) bool { return p != tp })
	w.h.discardCongestionPacket(tp.p.EncryptionLevel, tp.pn)
	w.ref.discardPacket(tp.p.EncryptionLevel, tp.pn)
	w.after()
}

func (w *recoveryTwin) discardSpace(level protocol.EncryptionLevel) {
	w.t.Helper()
	w.take(level, func(p *twinPacket) bool { return p.p.EncryptionLevel != level })
	w.h.discardCongestionSpace(level)
	w.ref.discardSpace(level)
	w.after()
}

func (w *recoveryTwin) reset(pathChanged bool) {
	w.t.Helper()
	w.h.resetCongestionCapture(pathChanged)
	w.ref.resetCapture(pathChanged)
	w.after()
}

func (w *recoveryTwin) expireTimer() {
	w.t.Helper()
	w.h.ExpireDelivery(w.now)
	w.refExpire(w.now)
	w.after()
}

func (w *recoveryTwin) refExpire(now monotime.Time) { w.ref.expire(now) }

func (w *recoveryTwin) after() {
	w.t.Helper()
	w.ops++
	require.Equal(w.t, frozenDispatchSummary(w.ref), candidateDispatchSummary(w.d), "dispatch state after operation %d", w.ops)
	// A stale deadline fired when a due deadline advanced with nothing expired,
	// whichever entry point (send, retirement, ACK or timer) ran expiry.
	s := &w.ref.sampler
	if !w.priorExpiry.IsZero() && !w.now.Before(w.priorExpiry) && s.expired == w.priorExpired && s.nextExpiry > w.priorExpiry {
		w.coverage.StaleDeadlines++
	}
	w.priorExpiry, w.priorExpired = s.nextExpiry, s.expired
	// Reset clears these counters, so coverage keeps the largest value seen.
	w.coverage.OutcomeEvicted = max(w.coverage.OutcomeEvicted, w.ref.recovery.evicted)
	w.coverage.RetainedEvicted = max(w.coverage.RetainedEvicted, w.ref.sampler.evicted)
	w.coverage.RetainedExpired = max(w.coverage.RetainedExpired, w.ref.sampler.expired)
	if w.checkEvery > 0 && w.ops%w.checkEvery == 0 {
		w.checkpoint()
	}
}

// checkpoint compares full ledger and retained contents and checks every
// derived candidate index against a recomputation from the ring.
func (w *recoveryTwin) checkpoint() {
	w.t.Helper()
	require.Equal(w.t, frozenOutcomes(&w.ref.recovery), candidateOutcomes(&w.d.recovery), "ledger contents")
	for key, i := range w.ref.recovery.keys {
		p := w.d.recovery.find(key)
		require.GreaterOrEqual(w.t, p, 0, "lookup %v", key)
		o := w.ref.recovery.outcomes[i]
		require.Equal(w.t, frozenOutcomeView(&o), candidateOutcomeView(&w.d.recovery.index.outcomes[p]), "lookup %v", key)
	}
	for s, space := range []protocol.EncryptionLevel{protocol.EncryptionInitial, protocol.EncryptionHandshake, protocol.Encryption1RTT} {
		for _, pn := range []protocol.PacketNumber{-1, w.nextPN[s], w.nextPN[s] + 1000} {
			_, known := w.ref.recovery.keys[congestionPacketKey{space: space, number: pn}]
			require.Equal(w.t, known, w.d.recovery.find(congestionPacketKey{space: space, number: pn}) >= 0)
		}
	}
	require.Equal(w.t, frozenRetainedView(w.ref), candidateRetainedView(w.d), "retained contents")
	require.Equal(w.t, w.ref.packets, w.d.packets.asMap(), "live delivery records")
	checkDeliveryRecords(w.t, &w.d.packets)
	checkRecoveryIndex(w.t, &w.d.recovery)
	checkRetainedIndex(w.t, &w.d.sampler)
}

type dispatchSummary struct {
	Ordinal, PathGeneration, SampleGeneration uint64
	RegistrationTime                          monotime.Time
	Live                                      int

	Count                    int
	OutcomeEvicted, Reported uint64
	Measured, Unconfirmed    bool
	NeedsAckFeedback         bool
	Episode                  congestion.RecoveryEpisode
	Members                  map[uint64]struct{}
	Latest, Boundary         map[protocol.EncryptionLevel]protocol.PacketNumber

	Delivered, Lost                                uint64
	DeliveredTime, SendOrigin                      monotime.Time
	Outstanding                                    protocol.ByteCount
	MinimumRTT                                     time.Duration
	Evicted, Expired, Missing                      uint64
	LimitedUntil                                   uint64
	Limited, Stop                                  congestion.SendLimitation
	ApplicationExhausted, EvidenceLost, Originless bool
	NextExpiry                                     monotime.Time
	Retained                                       int
}

func frozenDispatchSummary(d *frozenDispatch) dispatchSummary {
	r, s := &d.recovery, &d.sampler
	return dispatchSummary{
		Ordinal: d.ordinal, PathGeneration: d.pathGeneration, SampleGeneration: d.sampleGeneration, RegistrationTime: d.registrationTime, Live: len(d.packets),
		Count: r.count, OutcomeEvicted: r.evicted, Reported: r.reported, Measured: r.measured, Unconfirmed: r.unconfirmedLoss, NeedsAckFeedback: r.needsAckFeedback(),
		Episode: r.episode, Members: r.members, Latest: r.latestPackets, Boundary: r.boundaryPackets,
		Delivered: s.delivered, Lost: s.lost, DeliveredTime: s.deliveredTime, SendOrigin: s.sendOrigin, Outstanding: s.outstanding, MinimumRTT: s.minimumRTT,
		Evicted: s.evicted, Expired: s.expired, Missing: s.missing, LimitedUntil: s.limitedUntil, Limited: s.limited, Stop: s.stop,
		ApplicationExhausted: s.applicationExhausted, EvidenceLost: s.evidenceLost, Originless: s.originlessRegistrations, NextExpiry: s.nextExpiry, Retained: len(s.retained),
	}
}

func candidateDispatchSummary(d *congestionDispatch) dispatchSummary {
	r, s := &d.recovery, &d.sampler
	return dispatchSummary{
		Ordinal: d.ordinal, PathGeneration: d.pathGeneration, SampleGeneration: d.sampleGeneration, RegistrationTime: d.registrationTime, Live: d.packets.len(),
		Count: r.count, OutcomeEvicted: r.evicted, Reported: r.reported, Measured: r.measured, Unconfirmed: r.unconfirmedLoss, NeedsAckFeedback: r.needsAckFeedback(),
		Episode: r.episode, Members: r.members, Latest: r.latestPackets, Boundary: r.boundaryPackets,
		Delivered: s.delivered, Lost: s.lost, DeliveredTime: s.deliveredTime, SendOrigin: s.sendOrigin, Outstanding: s.outstanding, MinimumRTT: s.minimumRTT,
		Evicted: s.evicted, Expired: s.expired, Missing: s.missing, LimitedUntil: s.limitedUntil, Limited: s.limited, Stop: s.stop,
		ApplicationExhausted: s.applicationExhausted, EvidenceLost: s.evidenceLost, Originless: s.originlessRegistrations, NextExpiry: s.nextExpiry, Retained: len(s.retained),
	}
}

type outcomeView struct {
	Key                                   congestionPacketKey
	Level                                 protocol.EncryptionLevel
	Ordinal                               uint64
	Sent                                  monotime.Time
	State                                 recoveryOutcomeState
	Endpoint, ReceiptEligible, PTORetired bool
}

func frozenOutcomeView(o *frozenOutcome) outcomeView {
	return outcomeView{Key: o.key, Level: o.level, Ordinal: o.ordinal, Sent: o.sent, State: o.state, Endpoint: o.endpoint, ReceiptEligible: o.receiptEligible, PTORetired: o.ptoRetired}
}

func candidateOutcomeView(o *recoveryOutcome) outcomeView {
	return outcomeView{Key: congestionPacketKey{space: o.space, number: o.number}, Level: o.level, Ordinal: o.ordinal, Sent: o.sent, State: o.state, Endpoint: o.endpoint, ReceiptEligible: o.receiptEligible, PTORetired: o.ptoRetired}
}

func frozenOutcomes(r *frozenRecovery) []outcomeView {
	views := make([]outcomeView, r.count)
	for i := range views {
		views[i] = frozenOutcomeView(&r.outcomes[(r.head+i)%maxRecoveryOutcomes])
	}
	return views
}

func candidateOutcomes(r *recoveryEvidence) []outcomeView {
	views := make([]outcomeView, r.count)
	for i := range views {
		views[i] = candidateOutcomeView(&r.index.outcomes[r.slot(i)])
	}
	return views
}

type retainedView struct {
	Key     congestionPacketKey
	Packet  congestion.PacketInfo
	Expires monotime.Time
}

func frozenRetainedView(d *frozenDispatch) []retainedView {
	var views []retainedView
	for key, r := range d.sampler.retained {
		views = append(views, retainedView{Key: key, Packet: r.packet, Expires: r.expires})
	}
	slices.SortFunc(views, func(a, b retainedView) int { return cmp.Compare(a.Packet.Ordinal, b.Packet.Ordinal) })
	return views
}

func candidateRetainedView(d *congestionDispatch) []retainedView {
	var views []retainedView
	for key, r := range d.sampler.retained {
		views = append(views, retainedView{Key: key, Packet: r.packet, Expires: r.expires})
	}
	slices.SortFunc(views, func(a, b retainedView) int { return cmp.Compare(a.Packet.Ordinal, b.Packet.Ordinal) })
	return views
}

// checkRecoveryIndex recomputes every derived ledger index from the ring.
func checkRecoveryIndex(t testing.TB, r *recoveryEvidence) {
	t.Helper()
	x := r.index
	if x == nil {
		return
	}
	var want recoveryIndex
	for l := range r.count {
		p := r.slot(l)
		o := &x.outcomes[p]
		s := recoverySpace(o.space)
		want.spaces[s].push(p)
		want.pending[s].assign(p, pendingOutcome(o.state))
		want.eligible[s].assign(p, o.receiptEligible && o.state != outcomeDisposed)
		want.ptoPending[s].assign(p, o.ptoRetired && o.state == outcomeUnresolved)
		want.spans.lost.assign(p, o.state == outcomeLost)
		want.spans.endpoints.assign(p, o.state == outcomeLost && o.endpoint)
	}
	for s := range 3 {
		require.Equal(t, want.spaces[s].n, x.spaces[s].n, "space %d size", s)
		for i := range want.spaces[s].n {
			require.Equal(t, want.spaces[s].at(i), x.spaces[s].at(i), "space %d position %d", s, i)
		}
		requireSlotSet(t, &want.pending[s], &x.pending[s], "pending")
		requireSlotSet(t, &want.eligible[s], &x.eligible[s], "eligible")
		requireSlotSet(t, &want.ptoPending[s], &x.ptoPending[s], "PTO pending")
	}
	requireSlotSet(t, &want.spans.lost, &x.spans.lost, "lost")
	requireSlotSet(t, &want.spans.endpoints, &x.spans.endpoints, "lost endpoints")
	// Walk runs in ring order exactly as the frozen reducer does.
	start, first, last := -1, -1, -1
	closeRun := func() {
		if start >= 0 {
			want.spans.starts.set(start)
			if first >= 0 && last != first {
				b := start / spanBlockSlots
				want.spans.tree[spanBlocks+b] = max(want.spans.tree[spanBlocks+b], x.outcomes[last].sent.Sub(x.outcomes[first].sent))
			}
		}
		start, first, last = -1, -1, -1
	}
	for l := range r.count {
		p := r.slot(l)
		o := &x.outcomes[p]
		if o.state != outcomeLost {
			closeRun()
			continue
		}
		if start < 0 {
			start = p
		}
		if o.endpoint {
			if first < 0 {
				first = p
			}
			last = p
		}
	}
	closeRun()
	requireSlotSet(t, &want.spans.starts, &x.spans.starts, "run starts")
	for i := spanBlocks - 1; i >= 1; i-- {
		want.spans.tree[i] = max(want.spans.tree[2*i], want.spans.tree[2*i+1])
	}
	require.Equal(t, want.spans.tree, x.spans.tree, "run duration tree")
}

func requireSlotSet(t testing.TB, want, got *slotSet, name string) {
	t.Helper()
	require.Equal(t, want.words, got.words, "%s words", name)
	require.Equal(t, want.any1, got.any1, "%s summary", name)
	require.Equal(t, want.any2, got.any2, "%s summary", name)
	require.Equal(t, want.full1, got.full1, "%s full summary", name)
	require.Equal(t, want.full2, got.full2, "%s full summary", name)
}

// checkRetainedIndex verifies the key, ordinal, coverage and expiry indexes
// all hold exactly the retained records.
func checkRetainedIndex(t testing.TB, s *deliverySampler) {
	t.Helper()
	require.Len(t, s.order, len(s.retained))
	require.Len(t, s.expiry, len(s.retained))
	for i, r := range s.order {
		require.Equal(t, i, r.index)
		require.Same(t, s.retained[r.key], r)
	}
	for i, r := range s.expiry {
		require.Equal(t, i, r.expiryIndex)
		require.Same(t, s.retained[r.key], r)
		if i > 0 {
			require.False(t, s.expiry.less(i, (i-1)/2), "expiry heap order")
		}
	}
	var inorder []*retainedDelivery
	var walk func(*retainedDelivery) int8
	walk = func(n *retainedDelivery) int8 {
		if n == nil {
			return 0
		}
		l := walk(n.left)
		inorder = append(inorder, n)
		r := walk(n.right)
		require.Equal(t, 1+max(l, r), n.height)
		require.LessOrEqual(t, l-r, int8(1))
		require.GreaterOrEqual(t, l-r, int8(-1))
		return n.height
	}
	walk(s.covered)
	require.Len(t, inorder, len(s.retained))
	for i, r := range inorder {
		require.Same(t, s.retained[r.key], r)
		if i > 0 {
			require.True(t, retainedBefore(inorder[i-1].key, r.key))
		}
	}
}

// twinWorkload is a finite seeded history generator, not a fuzz target.
type twinWorkload struct {
	seed             uint64
	sends            int
	loss, burst      float64 // independent loss and burst-start probability
	burstLen         int
	pto, probe, mtu  float64
	ackOnly, invalid float64
	skip, late       float64
	wide, duplicate  float64
	ptoShift, expire float64
	mixed            bool
	retainedPressure bool
	resetEvery       int
}

type peerPacket struct {
	level   protocol.EncryptionLevel
	pn      protocol.PacketNumber
	arrives monotime.Time
}

func runTwinWorkload(t *testing.T, wl twinWorkload, checkEvery int) twinCoverage {
	w := newRecoveryTwin(t)
	w.checkEvery = checkEvery
	rng := rand.New(rand.NewPCG(wl.seed, wl.seed^0x9e3779b97f4a7c15))
	rtt := 20 * time.Millisecond
	var inflightToPeer []peerPacket
	received := map[protocol.EncryptionLevel][]wire.AckRange{}
	lastFrame := map[protocol.EncryptionLevel]*wire.AckFrame{}
	burst := 0
	levels := []protocol.EncryptionLevel{protocol.EncryptionInitial, protocol.EncryptionHandshake, protocol.Encryption0RTT, protocol.Encryption1RTT}
	ackLevel := func(l protocol.EncryptionLevel) protocol.EncryptionLevel {
		if l == protocol.Encryption0RTT {
			return protocol.Encryption1RTT
		}
		return l
	}
	dropped := map[protocol.EncryptionLevel]bool{}
	for i := range wl.sends {
		w.advance(time.Duration(50+rng.IntN(400)) * time.Microsecond)
		level := protocol.Encryption1RTT
		if wl.mixed && i < wl.sends/3 {
			level = levels[rng.IntN(len(levels))]
		}
		if wl.mixed && i == wl.sends/3 {
			for _, l := range []protocol.EncryptionLevel{protocol.EncryptionInitial, protocol.Encryption0RTT, protocol.EncryptionHandshake} {
				w.discardSpace(l)
				dropped[l] = true
				delete(received, l)
			}
			inflightToPeer = slices.DeleteFunc(inflightToPeer, func(p peerPacket) bool { return dropped[p.level] })
		}
		if dropped[level] {
			level = protocol.Encryption1RTT
		}
		o := twinSend{level: level, length: protocol.ByteCount(200 + rng.IntN(1100))}
		o.ackOnly = rng.Float64() < wl.ackOnly
		o.pathProbe = rng.Float64() < wl.probe
		o.mtu = !o.pathProbe && rng.Float64() < wl.mtu
		if rng.Float64() < wl.skip {
			o.skip = 1
		}
		if rng.Float64() < wl.invalid {
			o.backdate = time.Duration(1+rng.IntN(5)) * time.Millisecond
		}
		tp := w.send(o)
		if burst == 0 && rng.Float64() < wl.burst {
			burst = wl.burstLen
		}
		if wl.retainedPressure && i%10000 == 0 {
			burst = maxDeliveryRetained + 400 // outlives the retained capacity
		}
		if burst > 0 {
			burst--
		} else if rng.Float64() >= wl.loss {
			arrive := w.now.Add(rtt / 2)
			if rng.Float64() < wl.late {
				arrive = arrive.Add(time.Duration(rng.IntN(int(4 * rtt))))
			}
			inflightToPeer = append(inflightToPeer, peerPacket{level: tp.p.EncryptionLevel, pn: tp.pn, arrives: arrive})
		}
		if tp.p.isPathProbePacket && rng.Float64() < 0.5 {
			w.discardPacket(tp)
		}
		// Deliver arrivals and occasionally ACK the receiver's state.
		slices.SortStableFunc(inflightToPeer, func(a, b peerPacket) int { return cmp.Compare(a.arrives, b.arrives) })
		n := 0
		for _, p := range inflightToPeer {
			if p.arrives.After(w.now) {
				break
			}
			al := ackLevel(p.level)
			received[al] = insertAckRange(received[al], p.pn)
			n++
		}
		inflightToPeer = inflightToPeer[n:]
		if rng.IntN(4) == 0 {
			for _, al := range []protocol.EncryptionLevel{protocol.EncryptionInitial, protocol.EncryptionHandshake, protocol.Encryption1RTT} {
				rs := received[al]
				if len(rs) == 0 || dropped[al] {
					continue
				}
				frame := ackFrame(slices.Clone(rs[:min(len(rs), 16)])...)
				switch x := rng.Float64(); {
				case x < wl.duplicate && lastFrame[al] != nil:
					frame = lastFrame[al]
				case x < wl.duplicate+wl.wide:
					frame = wideAckFrame(rs, rng)
				}
				lastFrame[al] = frame
				w.ack(al, frame, twinAck{rtt: rng.IntN(3) > 0, lose: func(p *twinPacket) bool {
					return frame.LargestAcked()-p.pn >= 3 || !p.p.SendTime.After(w.now.Add(-w.lossDelay))
				}})
			}
		}
		// Under retained pressure, the window's packets stay outstanding until
		// one long-PTO timer loss retires them all.
		pressure := wl.retainedPressure && i%10000 < maxDeliveryRetained+400
		if !pressure && rng.Float64() < wl.pto {
			w.advance(5 * time.Millisecond)
			w.retirePTO(protocol.Encryption1RTT)
		}
		if rng.Float64() < wl.expire {
			w.expireTimer()
		}
		if !pressure && rng.IntN(64) == 0 {
			cutoff := w.now.Add(-w.lossDelay)
			w.timerLoss(protocol.Encryption1RTT, func(p *twinPacket) bool { return !p.p.SendTime.After(cutoff.Add(-2 * rtt)) })
		}
		if rng.Float64() < wl.ptoShift {
			switch rng.IntN(5) {
			case 0:
				w.pto = 0 // invalid estimates leave spans unreported
			case 1:
				w.pto = time.Duration(1<<63-1)/3 + 1
			case 2:
				w.pto = time.Duration(10+rng.IntN(60)) * time.Millisecond
			default:
				w.pto = time.Duration(30+rng.IntN(400)) * time.Millisecond
			}
		} else if w.pto <= 0 || w.pto > time.Second {
			w.pto = 60 * time.Millisecond
		}
		if wl.retainedPressure && i%10000 == maxDeliveryRetained+400 {
			// A long blackout under a long PTO accumulates retained loss evidence.
			saved := w.pto
			w.pto = 5 * time.Second
			w.timerLoss(protocol.Encryption1RTT, func(*twinPacket) bool { return true })
			w.pto = saved
		}
		if wl.resetEvery > 0 && i%wl.resetEvery == wl.resetEvery-1 {
			w.reset(rng.IntN(2) == 0)
			inflightToPeer = inflightToPeer[:0]
			clear(received)
			clear(lastFrame)
		}
	}
	w.checkpoint()
	return w.coverage
}

// insertAckRange records pn in a descending, gap-separated range list.
func insertAckRange(rs []wire.AckRange, pn protocol.PacketNumber) []wire.AckRange {
	i, _ := slices.BinarySearchFunc(rs, pn, func(r wire.AckRange, pn protocol.PacketNumber) int { return cmp.Compare(pn, r.Largest) })
	// rs[i-1].Largest > pn >= rs[i].Largest ordering is descending.
	switch {
	case i < len(rs) && rs[i].Smallest <= pn && pn <= rs[i].Largest:
		return rs
	case i > 0 && rs[i-1].Smallest <= pn:
		return rs
	}
	rs = slices.Insert(rs, i, wire.AckRange{Smallest: pn, Largest: pn})
	if i+1 < len(rs) && rs[i+1].Largest+1 == pn {
		rs[i].Smallest = rs[i+1].Smallest
		rs = slices.Delete(rs, i+1, i+2)
	}
	if i > 0 && rs[i-1].Smallest == pn+1 {
		rs[i-1].Smallest = rs[i].Smallest
		rs = slices.Delete(rs, i, i+1)
	}
	if len(rs) > 256 {
		rs = rs[:256]
	}
	return rs
}

// wideAckFrame covers far more packet numbers than the ring holds: either one
// cumulative range or sparse ranges with wide gaps.
func wideAckFrame(rs []wire.AckRange, rng *rand.Rand) *wire.AckFrame {
	largest := rs[0].Largest
	if rng.IntN(2) == 0 {
		return ackFrame(ackRange(0, largest))
	}
	ranges := []wire.AckRange{ackRange(max(0, largest-3), largest)}
	for lo := largest - 3 - 2*maxRecoveryOutcomes; lo > 10; lo -= 3 * maxRecoveryOutcomes {
		if lo+maxRecoveryOutcomes < ranges[len(ranges)-1].Smallest-1 {
			ranges = append(ranges, ackRange(lo, lo+maxRecoveryOutcomes))
		}
	}
	if ranges[len(ranges)-1].Smallest > 2 {
		ranges = append(ranges, ackRange(0, 0))
	}
	return ackFrame(ranges...)
}
