package ackhandler

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

// ecnSnapshot is every semantic field of the tracker. It excludes the path
// function's identity and representation-only storage such as capacity, the
// rewrite scratch and the work counters.
type ecnSnapshot struct {
	Ranges []ecnMarkRange
	ecnScalars
}

type ecnScalars struct {
	Compacted               congestion.ECNCounts
	CompactedOrdinal        uint64
	Sent, Accepted, Offsets congestion.ECNCounts
	Watermark               protocol.PacketNumber
	Generation, Fence       uint64
	State                   ecnState
	Draining, Closed        bool
	Testing                 [numECNTestingPackets]struct {
		pn   protocol.PacketNumber
		lost bool
	}
	TestingSent, TestingLost    uint64
	EvidenceLost, CounterFailed bool
}

func snapshotFrozenECN(e *frozenBBRECNTracker) ecnSnapshot {
	return ecnSnapshot{append([]ecnMarkRange(nil), e.ranges...), ecnScalars{
		Compacted: e.compacted, CompactedOrdinal: e.compactedOrdinal,
		Sent: e.sent, Accepted: e.accepted, Offsets: e.offsets, Watermark: e.watermark, Generation: e.generation, Fence: e.fence,
		State: e.state, Draining: e.draining, Closed: e.closed, Testing: e.testing, TestingSent: e.testingSent, TestingLost: e.testingLost,
		EvidenceLost: e.evidenceLost, CounterFailed: e.counterFailed,
	}}
}

func snapshotECN(e *bbrECNTracker) ecnSnapshot {
	return ecnSnapshot{append([]ecnMarkRange(nil), e.ranges...), ecnScalars{
		Compacted: e.compacted, CompactedOrdinal: e.compactedOrdinal,
		Sent: e.sent, Accepted: e.accepted, Offsets: e.offsets, Watermark: e.watermark, Generation: e.generation, Fence: e.fence,
		State: e.state, Draining: e.draining, Closed: e.closed, Testing: e.testing, TestingSent: e.testingSent, TestingLost: e.testingLost,
		EvidenceLost: e.evidenceLost, CounterFailed: e.counterFailed,
	}}
}

// ecnOracle drives the frozen and bounded-work trackers with identical
// operations and requires identical results and snapshots after each one.
type ecnOracle struct {
	t          testing.TB
	frozen     *frozenBBRECNTracker
	got        *bbrECNTracker
	generation uint64
	drained    bool
	capable    bool
	ops        int
	stats      ecnOracleStats
	// bulk defers registration snapshots to the next checked operation, for
	// building large ledgers; results are still compared.
	bulk bool
}

type ecnOracleStats struct {
	Accepted, Deferred, Grew, Shrank, Compacted, CounterFailures, AnchorFailures, SplitCapFailures, InsertCapFailures int
	MaxLedger                                                                                                         int
}

func newECNOracle(t testing.TB) *ecnOracle {
	o := &ecnOracle{t: t, drained: true, capable: true}
	path := func() (uint64, bool, bool) { return o.generation, o.drained, o.capable }
	o.frozen = &frozenBBRECNTracker{path: path, watermark: protocol.InvalidPacketNumber}
	o.got = &bbrECNTracker{path: path, watermark: protocol.InvalidPacketNumber}
	return o
}

func (o *ecnOracle) check(op string) {
	o.t.Helper()
	o.ops++
	want, got := snapshotFrozenECN(o.frozen), snapshotECN(o.got)
	if want.ecnScalars != got.ecnScalars || !slices.Equal(want.Ranges, got.Ranges) {
		require.Equal(o.t, want, got, "snapshot diverged after op %d: %s", o.ops, op)
	}
}

func (o *ecnOracle) mode(shortHeader bool) protocol.ECN {
	o.t.Helper()
	want, got := o.frozen.mode(shortHeader), o.got.mode(shortHeader)
	require.Equal(o.t, want, got, "mode(%t) diverged after op %d", shortHeader, o.ops)
	o.check(fmt.Sprintf("mode(%t)", shortHeader))
	return got
}

func (o *ecnOracle) send(pn protocol.PacketNumber, ordinal, generation uint64, mark protocol.ECN) {
	o.t.Helper()
	failed := o.got.counterFailed || o.got.evidenceLost
	o.frozen.sentPacket(pn, ordinal, generation, mark)
	o.got.sentPacket(pn, ordinal, generation, mark)
	if !failed && o.got.evidenceLost {
		o.stats.InsertCapFailures++
	}
	if o.bulk {
		return
	}
	o.check(fmt.Sprintf("send(pn=%d ordinal=%d generation=%d mark=%v)", pn, ordinal, generation, mark))
}

func (o *ecnOracle) ack(f *wire.AckFrame) congestion.ECNResult {
	o.t.Helper()
	before, compacted, failed := len(o.got.ranges), o.got.compactedOrdinal, o.got.counterFailed || o.got.evidenceLost
	want, got := o.frozen.feedback(f), o.got.feedback(f)
	require.Equal(o.t, want, got, "feedback result diverged after op %d: %+v", o.ops, f)
	o.check(fmt.Sprintf("ack(%+v)", f))
	switch {
	case got.Deferred:
		o.stats.Deferred++
	case !failed && o.got.counterFailed:
		o.stats.CounterFailures++
	case !failed && o.got.evidenceLost:
		// A failed ACK leaves the ledger unchanged: an anchor record means
		// validation passed and the split exceeded the budget.
		i := o.got.searchRanges(f.LargestAcked())
		if i < len(o.got.ranges) && o.got.ranges[i].first <= f.LargestAcked() {
			o.stats.SplitCapFailures++
		} else {
			o.stats.AnchorFailures++
		}
	case got.Ordinal != 0:
		o.stats.Accepted++
		if len(o.got.ranges) > before {
			o.stats.Grew++
		}
		if len(o.got.ranges) < before && o.got.compactedOrdinal == compacted {
			o.stats.Shrank++
		}
		if o.got.compactedOrdinal != compacted {
			o.stats.Compacted++
		}
	}
	o.stats.MaxLedger = max(o.stats.MaxLedger, len(o.got.ranges))
	return got
}

func (o *ecnOracle) lost(pn protocol.PacketNumber) {
	o.t.Helper()
	o.frozen.lostPacket(pn)
	o.got.lostPacket(pn)
	o.check(fmt.Sprintf("lost(%d)", pn))
}

func (o *ecnOracle) reset() {
	o.t.Helper()
	o.generation++
	o.drained = false
	o.frozen.resetPath(o.generation)
	o.got.resetPath(o.generation)
	o.check(fmt.Sprintf("reset(%d)", o.generation))
}

// close mirrors CloseDelivery.
func (o *ecnOracle) close() {
	o.t.Helper()
	o.frozen.closed, o.frozen.ranges, o.frozen.path = true, nil, nil
	o.got.closed, o.got.ranges, o.got.scratch, o.got.path = true, nil, nil, nil
	o.check("close")
}

func ackOf(ect0, ect1, ce uint64, ranges ...wire.AckRange) *wire.AckFrame {
	return &wire.AckFrame{AckRanges: ranges, ECT0: ect0, ECT1: ect1, ECNCE: ce}
}

// ecnChooser is the generator's only source of choices, so the same driver
// runs from a seeded PRNG and from fuzz input.
type ecnChooser interface{ IntN(int) int }

type byteChooser struct{ b []byte }

func (c *byteChooser) IntN(n int) int {
	if len(c.b) < 2 {
		c.b = nil
		return 0
	}
	v := int(c.b[0])<<8 | int(c.b[1])
	c.b = c.b[2:]
	return v % n
}

func (c *byteChooser) done() bool { return len(c.b) < 2 }

type ecnSentMark struct {
	pn   protocol.PacketNumber
	mark protocol.ECN
}

// ecnSequence generates caller-valid operation sequences: increasing packet
// numbers with skips, increasing nonzero ordinals with gaps from other packet
// number spaces, wire-shaped ACK frames (descending, disjoint, gap ≥ 1) with
// peer-derived counters and occasional invalid ones, reordered and duplicate
// ACKs, losses, path resets and drains.
type ecnSequence struct {
	o        *ecnOracle
	c        ecnChooser
	nextPN   protocol.PacketNumber
	ordinal  uint64
	sent     []ecnSentMark
	marks    map[protocol.PacketNumber]protocol.ECN
	received map[protocol.PacketNumber]bool
	peer     congestion.ECNCounts
	reported congestion.ECNCounts
	frames   []*wire.AckFrame
	// fragment alternates marks so nearly every packet is its own record,
	// driving the ledger toward the 4,096-record cap.
	fragment  bool
	maxRanges int
	// pin never acknowledges packet 0, so an unresolved prefix keeps every
	// later record retained, as individually ACKed records behind a hole.
	pin bool
	// chaotic frames pick arbitrary ranges below a sent largest. Otherwise a
	// modelled receiver delivers packets late or never and acknowledges its
	// received set, truncated to the newest ranges, in frames that reach the
	// sender reordered and duplicated.
	chaotic  bool
	pending  []protocol.PacketNumber
	inflight []*wire.AckFrame
}

func newECNSequence(o *ecnOracle, c ecnChooser, fragment, chaotic bool, maxRanges int) *ecnSequence {
	return &ecnSequence{o: o, c: c, marks: map[protocol.PacketNumber]protocol.ECN{}, received: map[protocol.PacketNumber]bool{}, fragment: fragment, pin: fragment, chaotic: chaotic, maxRanges: maxRanges}
}

func (s *ecnSequence) step() {
	s.o.t.Helper()
	sendWeight := 50
	if s.fragment {
		sendWeight = 80
	}
	switch k := s.c.IntN(100); {
	case k < sendWeight:
		s.send()
	case k < sendWeight+12:
		s.ack()
	case k < sendWeight+14:
		if len(s.frames) > 0 {
			s.o.ack(s.frames[s.c.IntN(len(s.frames))]) // reordered or duplicate
		}
	case k < sendWeight+16:
		if len(s.sent) > 0 {
			s.o.lost(s.sent[s.c.IntN(len(s.sent))].pn)
		}
	case k < sendWeight+17:
		s.o.reset()
	case k < sendWeight+18:
		s.o.drained = true
		s.o.check("drained")
	case k < sendWeight+19:
		s.o.capable = !s.o.capable
		s.o.check("capable toggled")
	default:
		s.o.mode(s.c.IntN(4) != 0)
	}
}

func (s *ecnSequence) send() {
	s.o.t.Helper()
	if s.c.IntN(10) == 0 {
		s.nextPN += protocol.PacketNumber(1 + s.c.IntN(3)) // skipped packet numbers
	}
	s.ordinal++
	if s.c.IntN(10) == 0 {
		s.ordinal += uint64(1 + s.c.IntN(5)) // Initial/Handshake registrations break affinity
	}
	var mark protocol.ECN
	switch {
	case s.fragment && len(s.sent) > 0 && s.c.IntN(2) == 0:
		mark = s.sent[len(s.sent)-1].mark // runs that late ACKs can split
	case s.fragment:
		mark = [...]protocol.ECN{protocol.ECT0, protocol.ECNNon, protocol.ECT1}[s.c.IntN(3)]
	case s.c.IntN(10) < 7:
		mark = s.o.mode(true)
	default:
		mark = [...]protocol.ECN{protocol.ECNNon, protocol.ECT0, protocol.ECT1, protocol.ECNUnsupported}[s.c.IntN(4)]
	}
	pn := s.nextPN
	s.nextPN++
	s.o.send(pn, s.ordinal, s.o.generation, mark)
	s.sent = append(s.sent, ecnSentMark{pn, mark})
	s.marks[pn] = mark
	// The model has no ack-of-ack, so permanent losses would accumulate gaps
	// past the range limit. The pinned regime's only permanent hole is pn 0.
	if (s.pin || s.c.IntN(20) != 0) && !(s.pin && pn == 0) {
		s.pending = append(s.pending, pn)
	}
}

// receive counts pn at the peer the first time it arrives.
func (s *ecnSequence) receive(pn protocol.PacketNumber) {
	mark, ok := s.marks[pn]
	if !ok || s.received[pn] {
		return
	}
	s.received[pn] = true
	ce := s.c.IntN(8) == 0
	switch {
	case mark == protocol.ECT0 && ce, mark == protocol.ECT1 && ce:
		s.peer.CE++
	case mark == protocol.ECT0:
		s.peer.ECT0++
	case mark == protocol.ECT1:
		s.peer.ECT1++
	}
}

// counters reports the peer's counts, rarely corrupted.
func (s *ecnSequence) counters() congestion.ECNCounts {
	counts := s.peer
	if s.pin {
		return counts // the near-cap regime must survive to the cap
	}
	switch s.c.IntN(1000) {
	case 0:
		counts = s.reported // stale counters
	case 1:
		counts.ECT0 = max(counts.ECT0, 1) - 1
	case 2:
		counts.ECT0 += uint64(1 + s.c.IntN(1<<12))
	case 3:
		counts.CE++
	case 4:
		counts.ECT1++
	}
	s.reported = counts
	return counts
}

func (s *ecnSequence) ack() {
	s.o.t.Helper()
	if s.chaotic {
		s.chaoticAck()
		return
	}
	for n := s.c.IntN(len(s.pending) + 1); n > 0; n-- {
		i := 0
		if s.c.IntN(8) == 0 {
			i = s.c.IntN(len(s.pending)) // late delivery
		}
		s.receive(s.pending[i])
		s.pending = slices.Delete(s.pending, i, i+1)
	}
	received := make([]protocol.PacketNumber, 0, len(s.received))
	for pn := range s.received {
		received = append(received, pn)
	}
	slices.Sort(received)
	// RFC 9000 validation fails when a truncated frame omits counted packets
	// that a later frame acknowledges, so truncate rarely to keep runs live.
	limit := s.maxRanges
	if !s.pin && s.c.IntN(16) == 0 {
		limit = 1 + s.c.IntN(4)
	}
	var ranges []wire.AckRange
	for i := len(received) - 1; i >= 0 && len(ranges) < limit; i-- {
		if n := len(ranges); n > 0 && ranges[n-1].Smallest == received[i]+1 {
			ranges[n-1].Smallest = received[i]
			continue
		}
		ranges = append(ranges, wire.AckRange{Smallest: received[i], Largest: received[i]})
	}
	if len(ranges) > 0 {
		counts := s.counters()
		s.inflight = append(s.inflight, ackOf(counts.ECT0, counts.ECT1, counts.CE, ranges...))
	}
	for n := s.c.IntN(3); n > 0 && len(s.inflight) > 0; n-- {
		i := len(s.inflight) - 1
		if s.c.IntN(4) == 0 {
			i = s.c.IntN(len(s.inflight)) // reordered
		}
		f := s.inflight[i]
		s.inflight = slices.Delete(s.inflight, i, i+1)
		s.frames = append(s.frames, f)
		s.o.ack(f)
	}
}

func (s *ecnSequence) chaoticAck() {
	s.o.t.Helper()
	if len(s.sent) == 0 {
		return
	}
	var largest protocol.PacketNumber
	if s.c.IntN(4) == 0 {
		largest = s.sent[s.c.IntN(len(s.sent))].pn
	} else if s.pin && len(s.sent) == 1 {
		return
	} else {
		largest = s.sent[len(s.sent)-1-s.c.IntN(min(len(s.sent), 8))].pn
	}
	n := 1 + s.c.IntN(4)
	if s.c.IntN(8) == 0 {
		n = 1 + s.c.IntN(s.maxRanges) // heavily fragmented frame
	}
	var ranges []wire.AckRange
	floor := protocol.PacketNumber(0)
	if s.pin {
		floor = 1
	}
	for hi := largest; len(ranges) < n && hi >= floor; {
		lo := max(hi-protocol.PacketNumber(s.c.IntN(6)), floor)
		ranges = append(ranges, wire.AckRange{Smallest: lo, Largest: hi})
		hi = lo - 2 - protocol.PacketNumber(s.c.IntN(3))
	}
	if len(ranges) == 0 {
		return // a pinned packet 0 was the only candidate
	}
	for _, r := range ranges {
		for pn := r.Smallest; pn <= r.Largest; pn++ {
			s.receive(pn)
		}
	}
	counts := s.counters()
	f := ackOf(counts.ECT0, counts.ECT1, counts.CE, ranges...)
	s.frames = append(s.frames, f)
	s.o.ack(f)
}

func TestBBRECNFeedbackFrozenEquivalence(t *testing.T) {
	type regime struct {
		name                  string
		seeds, ops, maxRanges int
		fragment              bool
	}
	regimes := []regime{
		{name: "mixed", seeds: 1500, ops: 300, maxRanges: 64},
		{name: "fragmented frames", seeds: 300, ops: 600, maxRanges: 400},
		{name: "near cap", seeds: 6, ops: 12000, maxRanges: 64, fragment: true},
	}
	if testing.Short() {
		for i := range regimes {
			regimes[i].seeds = max(regimes[i].seeds/10, 2)
		}
	}
	for _, rg := range regimes {
		t.Run(rg.name, func(t *testing.T) {
			var total ecnOracleStats
			var ops, capable int
			for seed := range rg.seeds {
				o := newECNOracle(t)
				s := newECNSequence(o, rand.New(rand.NewPCG(uint64(seed), 0x709)), rg.fragment, seed%3 == 2 && !rg.fragment, rg.maxRanges)
				for range rg.ops {
					s.step()
				}
				if o.got.state == ecnStateCapable {
					capable++
				}
				if seed%7 == 0 {
					o.close()
					s.ack()
				}
				ops += o.ops
				st := o.stats
				total.Accepted += st.Accepted
				total.Deferred += st.Deferred
				total.Grew += st.Grew
				total.Shrank += st.Shrank
				total.Compacted += st.Compacted
				total.CounterFailures += st.CounterFailures
				total.AnchorFailures += st.AnchorFailures
				total.SplitCapFailures += st.SplitCapFailures
				total.InsertCapFailures += st.InsertCapFailures
				total.MaxLedger = max(total.MaxLedger, st.MaxLedger)
			}
			t.Logf("%s: seeds=%d ops=%d capable-at-end=%d %+v", rg.name, rg.seeds, ops, capable, total)
			for name, n := range map[string]int{"accepted": total.Accepted, "deferred": total.Deferred, "grew": total.Grew, "shrank": total.Shrank} {
				require.NotZero(t, n, name)
			}
			if !rg.fragment {
				require.NotZero(t, total.Compacted)
				require.NotZero(t, total.CounterFailures)
				require.NotZero(t, capable)
			}
			if rg.fragment {
				require.Equal(t, maxECNMarkRanges, total.MaxLedger, "the near-cap regime reaches the cap")
				require.NotZero(t, total.InsertCapFailures)
			}
		})
	}
}

func FuzzBBRECNFeedbackFrozenEquivalence(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9})
	f.Add([]byte("\x00\x10\x00\x20\x00\x30\x00\x40\x00\x50\x00\x60\x00\x70\x00\x80\x00\x90\x00\xa0\x00\xb0\x00\xc0"))
	f.Fuzz(func(t *testing.T, b []byte) {
		c := &byteChooser{b: b}
		o := newECNOracle(t)
		s := newECNSequence(o, c, len(b) > 0 && b[0]&1 == 1, len(b) > 0 && b[0]&2 == 2, 64)
		for i := 0; i < 2000 && !c.done(); i++ {
			s.step()
		}
	})
}

// ecnLedgerBuilder registers explicit records for the named adversarial cases.
type ecnLedgerBuilder struct {
	o       *ecnOracle
	pn      protocol.PacketNumber
	ordinal uint64
}

func (b *ecnLedgerBuilder) send(marks ...protocol.ECN) (first, last protocol.PacketNumber) {
	first = b.pn
	for _, m := range marks {
		b.ordinal++
		b.o.send(b.pn, b.ordinal, b.o.generation, m)
		b.pn++
	}
	return first, b.pn - 1
}

func repeatECN(m protocol.ECN, n int) []protocol.ECN {
	s := make([]protocol.ECN, n)
	for i := range s {
		s[i] = m
	}
	return s
}

// alternatingECN returns n marks with no two equal neighbours, so each packet
// is its own record. It starts with Not-ECT, which keeps the prefix unresolved.
func alternatingECN(n int) []protocol.ECN {
	s := make([]protocol.ECN, n)
	for i := range s {
		s[i] = [...]protocol.ECN{protocol.ECNNon, protocol.ECT0}[i%2]
	}
	return s
}

func countECT0(marks []protocol.ECN) uint64 {
	var n uint64
	for _, m := range marks {
		if m == protocol.ECT0 {
			n++
		}
	}
	return n
}

func TestBBRECNFeedbackAdversarialEquivalence(t *testing.T) {
	r := func(lo, hi protocol.PacketNumber) wire.AckRange { return wire.AckRange{Smallest: lo, Largest: hi} }

	t.Run("packet-number gaps and skipped numbers", func(t *testing.T) {
		o := newECNOracle(t)
		b := &ecnLedgerBuilder{o: o}
		o.mode(true)
		b.send(protocol.ECT0, protocol.ECT0)
		b.pn += 2 // skipped
		b.send(protocol.ECT0, protocol.ECT0)
		// The ACK spans the skipped numbers; the frozen tracker ignores them.
		res := o.ack(ackOf(4, 0, 0, r(0, 5)))
		require.True(t, res.Eligible)
		require.Empty(t, o.got.ranges)
		b.send(protocol.ECT0)
		b.pn++
		b.send(protocol.ECT0)
		res = o.ack(ackOf(5, 0, 0, r(8, 8))) // only pn 8; 6 is still unresolved
		require.Equal(t, uint64(6), res.Ordinal)
		require.Len(t, o.got.ranges, 2)
	})

	t.Run("Not-ECT and ECT(1) holes", func(t *testing.T) {
		o := newECNOracle(t)
		b := &ecnLedgerBuilder{o: o}
		b.send(protocol.ECT0, protocol.ECNNon, protocol.ECT1, protocol.ECT0, protocol.ECNUnsupported, protocol.ECT0)
		o.ack(ackOf(3, 1, 0, r(5, 5), r(2, 3), r(0, 0)))
		require.Len(t, o.got.ranges, 5, "unresolved Not-ECT holes split the acked records; pn 0 compacts")
		o.ack(ackOf(3, 1, 0, r(6, 6))) // unsent largest: missing anchor
		require.True(t, o.got.evidenceLost)
	})

	t.Run("non-affine ordinals", func(t *testing.T) {
		o := newECNOracle(t)
		b := &ecnLedgerBuilder{o: o}
		b.send(protocol.ECT0, protocol.ECT0)
		b.ordinal += 3
		b.send(protocol.ECT0, protocol.ECT0)
		require.Len(t, o.got.ranges, 2)
		res := o.ack(ackOf(2, 0, 0, r(1, 2)))
		require.Equal(t, uint64(6), res.Ordinal)
		require.Len(t, o.got.ranges, 4, "an ACK across the ordinal break must not merge it")
	})

	t.Run("generation reset and drain", func(t *testing.T) {
		o := newECNOracle(t)
		b := &ecnLedgerBuilder{o: o}
		b.send(o.mode(true), o.mode(true))
		o.reset()
		require.Equal(t, protocol.ECNNon, o.mode(true), "draining until the fence is accepted")
		b.send(protocol.ECNNon)
		o.ack(ackOf(1, 0, 0, r(2, 2), r(0, 0)))
		require.Len(t, o.got.ranges, 2, "pn 0 compacts; pn 1 and the new generation stay separate")
		require.NotEqual(t, o.got.ranges[0].generation, o.got.ranges[1].generation)
		o.drained = true
		require.Equal(t, protocol.ECNNon, o.mode(true), "pn 1 still holds the fence")
		b.send(protocol.ECNNon)
		o.ack(ackOf(2, 0, 0, r(0, 3)))
		require.Equal(t, protocol.ECT0, o.mode(true), "the accepted fence ends draining")
		b.send(protocol.ECT0)
		require.True(t, o.ack(ackOf(3, 0, 0, r(0, 4))).Eligible)
	})

	t.Run("reordered, duplicate and lower-largest ACKs", func(t *testing.T) {
		o := newECNOracle(t)
		b := &ecnLedgerBuilder{o: o}
		o.mode(true)
		b.send(repeatECN(protocol.ECT0, 10)...)
		early, late := ackOf(3, 0, 0, r(0, 2)), ackOf(6, 0, 0, r(5, 7), r(0, 2))
		o.ack(late)
		require.True(t, o.ack(early).Deferred)
		require.True(t, o.ack(late).Deferred, "duplicate")
		res := o.ack(ackOf(9, 0, 0, r(8, 8), r(0, 7)))
		require.True(t, res.Eligible)
	})

	t.Run("heavily fragmented ACK frames", func(t *testing.T) {
		o := newECNOracle(t)
		b := &ecnLedgerBuilder{o: o}
		_, last := b.send(repeatECN(protocol.ECT0, 2001)...)
		var ranges []wire.AckRange
		for pn := last - 1; pn >= 1; pn -= 2 {
			ranges = append(ranges, r(pn, pn))
		}
		o.ack(ackOf(uint64(len(ranges)), 0, 0, ranges...))
		require.Len(t, o.got.ranges, 2001, "every acked odd packet splits the record")
		// Fill every even hole in one frame; all records merge and compact.
		o.ack(ackOf(2001, 0, 0, r(0, last)))
		require.Empty(t, o.got.ranges)
	})

	t.Run("left-edge merge into an acked neighbour", func(t *testing.T) {
		o := newECNOracle(t)
		b := &ecnLedgerBuilder{o: o}
		b.send(protocol.ECNNon)                 // unresolved hole pins the prefix
		b.send(repeatECN(protocol.ECT0, 10)...) // 1..10
		o.ack(ackOf(5, 0, 0, r(1, 5)))          // [1..5] acked, [6..10] not
		o.ack(ackOf(7, 0, 0, r(6, 7)))          // window [6..10]; [6..7] merges left
		require.Equal(t, []ecnMarkRange{
			{first: 0, last: 0, ordinal: 1, mark: protocol.ECNNon},
			{first: 1, last: 7, ordinal: 2, mark: protocol.ECT0, acked: true},
			{first: 8, last: 10, ordinal: 9, mark: protocol.ECT0},
		}, o.got.ranges)
	})

	t.Run("bridging merge through a fully acked record", func(t *testing.T) {
		o := newECNOracle(t)
		b := &ecnLedgerBuilder{o: o}
		b.send(protocol.ECNNon)
		b.send(repeatECN(protocol.ECT0, 15)...) // 1..15
		o.ack(ackOf(10, 0, 0, r(11, 15), r(1, 5)))
		b.send(protocol.ECNNon, protocol.ECT0) // 16, 17
		o.ack(ackOf(16, 0, 0, r(17, 17), r(6, 10)))
		require.Equal(t, []ecnMarkRange{
			{first: 0, last: 0, ordinal: 1, mark: protocol.ECNNon},
			{first: 1, last: 15, ordinal: 2, mark: protocol.ECT0, acked: true},
			{first: 16, last: 16, ordinal: 17, mark: protocol.ECNNon},
			{first: 17, last: 17, ordinal: 18, mark: protocol.ECT0, acked: true},
		}, o.got.ranges)
	})

	t.Run("right edge never merges with the right neighbour", func(t *testing.T) {
		// Records beyond LargestAcked are newer than the watermark, so they are
		// never acked, and the window's last output keeps the properties of the
		// record it came from. Exercise both endings of the anchor record.
		for _, tail := range []bool{false, true} {
			o := newECNOracle(t)
			b := &ecnLedgerBuilder{o: o}
			b.send(protocol.ECNNon)
			b.send(repeatECN(protocol.ECT0, 6)...) // 1..6
			b.ordinal += 2
			b.send(repeatECN(protocol.ECT0, 3)...) // 7..9, non-affine neighbour
			largest := protocol.PacketNumber(6)
			if tail {
				largest = 4
			}
			o.ack(ackOf(uint64(largest-2), 0, 0, r(3, largest)))
			require.Equal(t, protocol.PacketNumber(7), o.got.ranges[len(o.got.ranges)-1].first)
			require.False(t, o.got.ranges[len(o.got.ranges)-1].acked)
		}
	})

	t.Run("interior right merge into an acked record", func(t *testing.T) {
		o := newECNOracle(t)
		b := &ecnLedgerBuilder{o: o}
		b.send(protocol.ECNNon)
		b.send(repeatECN(protocol.ECT0, 10)...) // 1..10
		o.ack(ackOf(5, 0, 0, r(6, 10)))
		b.send(protocol.ECT0) // 11
		o.ack(ackOf(9, 0, 0, r(11, 11), r(3, 10)))
		require.Equal(t, []ecnMarkRange{
			{first: 0, last: 0, ordinal: 1, mark: protocol.ECNNon},
			{first: 1, last: 2, ordinal: 2, mark: protocol.ECT0},
			{first: 3, last: 11, ordinal: 4, mark: protocol.ECT0, acked: true},
		}, o.got.ranges)
	})
}

func TestBBRECNFeedbackCapEquivalence(t *testing.T) {
	r := func(lo, hi protocol.PacketNumber) wire.AckRange { return wire.AckRange{Smallest: lo, Largest: hi} }

	for _, n := range []int{maxECNMarkRanges - 1, maxECNMarkRanges, maxECNMarkRanges + 1} {
		t.Run(fmt.Sprintf("insertion to %d records", n), func(t *testing.T) {
			o := newECNOracle(t)
			b := &ecnLedgerBuilder{o: o}
			b.send(alternatingECN(n)...)
			require.Equal(t, n > maxECNMarkRanges, o.got.evidenceLost)
			require.Len(t, o.got.ranges, min(n, maxECNMarkRanges))
		})
	}

	// P alternating records, then one ECT0 record of 2m+1 packets whose even
	// offsets an ACK marks individually: the split adds 2m records.
	const m = 100
	splitTo := func(t *testing.T, target int, mutate func(*wire.AckFrame)) *ecnOracle {
		o := newECNOracle(t)
		b := &ecnLedgerBuilder{o: o}
		b.send(alternatingECN(target - (2*m + 1))...)
		b.ordinal++ // keep the split record separate from the prefix
		first, last := b.send(repeatECN(protocol.ECT0, 2*m+1)...)
		var ranges []wire.AckRange
		for pn := last; pn >= first; pn -= 2 {
			ranges = append(ranges, r(pn, pn))
		}
		f := ackOf(m+1, 0, 0, ranges...)
		if mutate != nil {
			mutate(f)
		}
		o.ack(f)
		return o
	}
	for _, n := range []int{maxECNMarkRanges - 1, maxECNMarkRanges, maxECNMarkRanges + 1} {
		t.Run(fmt.Sprintf("ACK-induced split to %d records", n), func(t *testing.T) {
			o := splitTo(t, n, nil)
			require.Equal(t, n > maxECNMarkRanges, o.got.evidenceLost)
			if n > maxECNMarkRanges {
				n -= 2 * m // the ledger is left exactly as it was before the ACK
			}
			require.Len(t, o.got.ranges, n)
		})
	}

	t.Run("combined invalid counters and over-cap split", func(t *testing.T) {
		o := splitTo(t, maxECNMarkRanges+1, func(f *wire.AckFrame) { f.ECT0 = 1 << 20 })
		require.True(t, o.got.counterFailed, "counter validation precedes the budget")
		require.False(t, o.got.evidenceLost)
	})

	t.Run("merge at cap", func(t *testing.T) {
		o := newECNOracle(t)
		b := &ecnLedgerBuilder{o: o}
		b.send(alternatingECN(maxECNMarkRanges - 3)...)
		b.ordinal++
		first, _ := b.send(repeatECN(protocol.ECT0, 10)...)
		o.ack(ackOf(4, 0, 0, r(first+3, first+6)))
		require.Len(t, o.got.ranges, maxECNMarkRanges)
		// Registration merging into the last record is free at the cap.
		b.send(protocol.ECT0)
		require.Len(t, o.got.ranges, maxECNMarkRanges)
		require.False(t, o.got.evidenceLost)
		// An ACK whose acked part merges into the acked neighbour keeps the count.
		o.ack(ackOf(6, 0, 0, r(first+7, first+8)))
		require.Len(t, o.got.ranges, maxECNMarkRanges)
		require.False(t, o.got.evidenceLost)
	})

	t.Run("overflow before compaction despite a reclaimable acked prefix", func(t *testing.T) {
		o := newECNOracle(t)
		b := &ecnLedgerBuilder{o: o}
		prefix := alternatingECN(maxECNMarkRanges - 1)
		_, pl := b.send(prefix...)
		b.ordinal++
		first, _ := b.send(repeatECN(protocol.ECT0, 5)...)
		require.Len(t, o.got.ranges, maxECNMarkRanges)
		// Acking the whole prefix would compact it away afterwards, but the
		// split of the last record is charged before compaction.
		f := ackOf(countECT0(prefix)+1, 0, 0, r(first+2, first+2), r(0, pl))
		o.ack(f)
		require.True(t, o.got.evidenceLost)
		require.Len(t, o.got.ranges, maxECNMarkRanges)
	})
}

// TestBBRECNFeedbackRandomizedCapEquivalence builds pinned ledgers of random
// mark runs a few records below the cap, then applies random island ACKs whose
// splits land on both sides of the 4,096-record budget.
func TestBBRECNFeedbackRandomizedCapEquivalence(t *testing.T) {
	seeds := 200
	if testing.Short() {
		seeds = 20
	}
	var fits, overflows int
	for seed := range seeds {
		c := rand.New(rand.NewPCG(uint64(seed), 0x4096))
		o := newECNOracle(t)
		o.mode(true)
		b := &ecnLedgerBuilder{o: o}
		b.send(protocol.ECNNon) // pinned unresolved hole
		marks := []protocol.ECN{protocol.ECT0, protocol.ECT1, protocol.ECNNon}
		var sent []ecnSentMark
		o.bulk = true
		for k := 0; len(o.got.ranges) < maxECNMarkRanges-c.IntN(6); k++ {
			mark := marks[k%3]
			first, last := b.send(repeatECN(mark, 1+c.IntN(6))...)
			for pn := first; pn <= last; pn++ {
				sent = append(sent, ecnSentMark{pn, mark})
			}
		}
		o.bulk = false
		o.check("built")
		received := map[protocol.PacketNumber]bool{}
		var peer congestion.ECNCounts
		for range 3 {
			// Late deliveries: islands that split runs.
			for range 1 + c.IntN(6) {
				i := c.IntN(len(sent))
				for j := i; j < min(i+1+c.IntN(3), len(sent)); j++ {
					if received[sent[j].pn] {
						continue
					}
					received[sent[j].pn] = true
					switch sent[j].mark {
					case protocol.ECT0:
						peer.ECT0++
					case protocol.ECT1:
						peer.ECT1++
					}
				}
			}
			// The largest received packet anchors the frame; every received
			// packet is acknowledged, so the counters are valid.
			var ranges []wire.AckRange
			for i := len(sent) - 1; i >= 0; i-- {
				pn := sent[i].pn
				if !received[pn] {
					continue
				}
				if n := len(ranges); n > 0 && ranges[n-1].Smallest == pn+1 {
					ranges[n-1].Smallest = pn
					continue
				}
				ranges = append(ranges, wire.AckRange{Smallest: pn, Largest: pn})
			}
			failed := o.got.evidenceLost
			o.ack(ackOf(peer.ECT0, peer.ECT1, peer.CE, ranges...))
			switch {
			case !failed && o.got.evidenceLost:
				overflows++
			case !o.got.evidenceLost:
				fits++
			}
		}
	}
	t.Logf("randomized cap: %d fitting ACKs, %d budget overflows over %d seeds", fits, overflows, seeds)
	require.NotZero(t, fits)
	require.NotZero(t, overflows)
}
