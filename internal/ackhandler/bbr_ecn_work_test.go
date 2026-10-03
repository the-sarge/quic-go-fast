package ackhandler

import (
	"fmt"
	"math/bits"
	"testing"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

// ecnLedgerAPI is the tracker surface shared by the frozen reference and the
// bounded-work tracker.
type ecnLedgerAPI interface {
	mode(shortHeader bool) protocol.ECN
	sentPacket(pn protocol.PacketNumber, ordinal, generation uint64, mark protocol.ECN)
	feedback(ack *wire.AckFrame) congestion.ECNResult
}

func newECNLedgers() (*frozenBBRECNTracker, *bbrECNTracker) {
	path := func() (uint64, bool, bool) { return 0, true, true }
	return &frozenBBRECNTracker{path: path, watermark: protocol.InvalidPacketNumber},
		&bbrECNTracker{path: func(bool) (uint64, bool, bool) { return path() }, watermark: protocol.InvalidPacketNumber}
}

// ecnSteadyStream sends batches of ECT0 packets behind an optional block of
// retained, never-delivered records, and acknowledges each batch with a
// preallocated frame. With reorder, the middle packet of each batch arrives
// late and is acknowledged by the next frame, so every ACK splits a record.
type ecnSteadyStream struct {
	e           ecnLedgerAPI
	pn          protocol.PacketNumber
	ordinal     uint64
	streamStart protocol.PacketNumber
	late        protocol.PacketNumber
	ect0        uint64
	reorder     bool
	frame       wire.AckFrame
	ranges      [2]wire.AckRange
}

func newECNSteadyStream(e ecnLedgerAPI, retained int, reorder bool) *ecnSteadyStream {
	s := &ecnSteadyStream{e: e, reorder: reorder, late: protocol.InvalidPacketNumber}
	e.mode(true)
	if retained > 0 {
		// An unresolved hole pins the prefix; distinct marks keep every
		// retained record separate, and none of them is ever acknowledged.
		for i := range retained {
			s.ordinal++
			e.sentPacket(s.pn, s.ordinal, 0, [...]protocol.ECN{protocol.ECNNon, protocol.ECT1}[i%2])
			s.pn++
		}
	}
	s.streamStart = s.pn
	return s
}

func (s *ecnSteadyStream) cycle(tb testing.TB, batch int) {
	first := s.pn
	for range batch {
		s.ordinal++
		s.e.sentPacket(s.pn, s.ordinal, 0, protocol.ECT0)
		s.pn++
	}
	newest := s.pn - 1
	s.frame.AckRanges = s.ranges[:1]
	s.ranges[0] = wire.AckRange{Smallest: s.streamStart, Largest: newest}
	s.ect0 += uint64(batch)
	if s.reorder {
		mid := first + protocol.PacketNumber(batch/2)
		s.ranges[0] = wire.AckRange{Smallest: mid + 1, Largest: newest}
		s.ranges[1] = wire.AckRange{Smallest: s.streamStart, Largest: mid - 1}
		s.frame.AckRanges = s.ranges[:2]
		s.ect0--
		if s.late != protocol.InvalidPacketNumber {
			s.ect0++ // last batch's late packet is covered now
		}
		s.late = mid
	}
	s.frame.ECT0 = s.ect0
	res := s.e.feedback(&s.frame)
	if res.Failed || res.Ordinal == 0 {
		tb.Fatalf("steady ACK failed: %+v", res)
	}
}

func TestBBRECNFeedbackSteadyStateAllocations(t *testing.T) {
	for _, tc := range []struct {
		name     string
		retained int
		reorder  bool
	}{
		{name: "in order"},
		{name: "in order behind retained records", retained: 1024},
		{name: "reordered splits behind retained records", retained: 1024, reorder: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frozen, bounded := newECNLedgers()
			allocs := map[string]float64{}
			for name, e := range map[string]ecnLedgerAPI{"frozen": frozen, "bounded": bounded} {
				s := newECNSteadyStream(e, tc.retained, tc.reorder)
				for range 64 {
					s.cycle(t, 10) // warm up: ledger and scratch capacity
				}
				allocs[name] = testing.AllocsPerRun(1000, func() { s.cycle(t, 10) })
			}
			t.Logf("%s: allocations per advancing ACK cycle: frozen %.0f, bounded %.0f (ledger %d records)", tc.name, allocs["frozen"], allocs["bounded"], len(bounded.ranges))
			require.Zero(t, allocs["bounded"])
		})
	}
}

// TestBBRECNFeedbackWorkStaysFlat holds the ACK and its affected window fixed
// while the unaffected ledger around it grows.
func TestBBRECNFeedbackWorkStaysFlat(t *testing.T) {
	r := func(lo, hi protocol.PacketNumber) wire.AckRange { return wire.AckRange{Smallest: lo, Largest: hi} }
	type outcome struct {
		ledger int
		work   ecnLedgerWork
	}
	// Layout: an unresolved hole, `before` retained records, a 20-packet ECT0
	// window, then `after` unacknowledged records sent later.
	run := func(t *testing.T, before, after int, resolve bool) outcome {
		o := newECNOracle(t)
		o.bulk = true
		o.mode(true)
		b := &ecnLedgerBuilder{o: o}
		if !resolve {
			b.send(protocol.ECNNon)
		}
		b.send(alternatingECN(before)...)
		b.ordinal++
		w, _ := b.send(repeatECN(protocol.ECT0, 20)...)
		b.ordinal++
		b.send(alternatingECN(after)...)
		o.bulk = false
		o.check("built")
		f := ackOf(13, 0, 0, r(w+14, w+19), r(w+7, w+9), r(w+2, w+5))
		if resolve {
			// Acknowledge every earlier packet too, so the prefix compacts.
			f = ackOf(15+countECT0(alternatingECN(before)), 0, 0, r(w+14, w+19), r(w+7, w+9), r(0, w+5))
		}
		o.got.work = ecnLedgerWork{}
		res := o.ack(f)
		require.False(t, res.Failed)
		return outcome{len(o.got.ranges), o.got.work}
	}

	logRows := func(t *testing.T, label string, sizes []int, rows []outcome) {
		for i, row := range rows {
			t.Logf("%s=%-5d ledger=%-5d search=%-3d inspections=%-3d rewrites=%-3d relocated=%-5d compaction moves=%d",
				label, sizes[i], row.ledger, row.work.searchSteps, row.work.inspections, row.work.rewrites, row.work.relocated, row.work.compactionMoves)
		}
	}
	sizes := []int{0, 16, 256, 1024, 4000}

	t.Run("unaffected suffix grows", func(t *testing.T) {
		var rows []outcome
		for _, n := range sizes {
			rows = append(rows, run(t, 0, n, false))
		}
		logRows(t, "suffix", sizes, rows)
		// Any suffix contributes exactly one record to the window: the right
		// neighbour, rewritten rather than relocated.
		for i, row := range rows[1:] {
			require.Equal(t, rows[1].work.inspections, row.work.inspections)
			require.Equal(t, rows[1].work.rewrites, row.work.rewrites)
			require.LessOrEqual(t, row.work.searchSteps, uint64(2*bits.Len(uint(row.ledger))))
			require.Equal(t, uint64(sizes[i+1]-1), row.work.relocated, "the split grows the window, so the rest of the suffix moves once")
			require.Zero(t, row.work.compactionMoves, "the hole pins the prefix")
		}
		require.Equal(t, rows[0].work.inspections+1, rows[1].work.inspections)
	})

	t.Run("unaffected prefix grows", func(t *testing.T) {
		var rows []outcome
		for _, n := range sizes[:len(sizes)-1] {
			rows = append(rows, run(t, n, 0, false))
		}
		logRows(t, "prefix", sizes[:len(sizes)-1], rows)
		for _, row := range rows {
			require.Equal(t, rows[0].work.inspections, row.work.inspections)
			require.Equal(t, rows[0].work.rewrites, row.work.rewrites)
			require.Zero(t, row.work.relocated)
			require.Zero(t, row.work.compactionMoves)
		}
	})

	t.Run("compaction moves the retained suffix", func(t *testing.T) {
		var rows []outcome
		for _, n := range sizes[:len(sizes)-1] {
			rows = append(rows, run(t, 0, n, true))
		}
		logRows(t, "suffix", sizes[:len(sizes)-1], rows)
		for i, row := range rows[1:] {
			require.Equal(t, rows[1].work.inspections, row.work.inspections)
			require.Equal(t, rows[1].work.rewrites, row.work.rewrites)
			require.Equal(t, uint64(sizes[i+1]-1), row.work.relocated)
		}
		for _, row := range rows {
			require.Equal(t, uint64(row.ledger), row.work.compactionMoves, "compaction moves every retained record, as the frozen tracker did")
		}
	})
}

// TestBBRECNFeedbackWorkScalesWithWindow varies the ACK's range count over
// one record, showing inspections grow with records plus ranges, never with
// their product, and with no work outside the window.
func TestBBRECNFeedbackWorkScalesWithWindow(t *testing.T) {
	var prev ecnLedgerWork
	for i, n := range []int{1, 4, 16, 64, 256} {
		o := newECNOracle(t)
		o.bulk = true
		o.mode(true)
		b := &ecnLedgerBuilder{o: o}
		b.send(protocol.ECNNon)
		b.send(alternatingECN(1000)...) // unaffected prefix
		b.ordinal++
		w, _ := b.send(repeatECN(protocol.ECT0, 2*n)...)
		b.ordinal++
		b.send(alternatingECN(1000)...) // unaffected suffix
		o.bulk = false
		o.check("built")
		var ranges []wire.AckRange
		for k := n - 1; k >= 0; k-- {
			pn := w + protocol.PacketNumber(2*k+1)
			ranges = append(ranges, wire.AckRange{Smallest: pn, Largest: pn})
		}
		o.got.work = ecnLedgerWork{}
		require.False(t, o.ack(ackOf(uint64(n), 0, 0, ranges...)).Failed)
		work := o.got.work
		t.Logf("ranges=%-4d window records=1 inspections=%-4d rewrites=%-4d relocated=%d", n, work.inspections, work.rewrites, work.relocated)
		// Three window records (two neighbours) plus one per intersecting range.
		require.Equal(t, uint64(3+n), work.inspections)
		require.Equal(t, uint64(2+2*n), work.rewrites)
		if i > 0 {
			require.Equal(t, work.relocated, prev.relocated, "the same suffix moves once whatever the range count")
		}
		prev = work
	}
}

func BenchmarkBBRECNFeedback(b *testing.B) {
	for _, retained := range []int{0, 256, 4000} {
		for _, reorder := range []bool{false, true} {
			for _, impl := range []string{"frozen", "bounded"} {
				b.Run(fmt.Sprintf("retained=%d/reorder=%t/%s", retained, reorder, impl), func(b *testing.B) {
					frozen, bounded := newECNLedgers()
					e := map[string]ecnLedgerAPI{"frozen": frozen, "bounded": bounded}[impl]
					s := newECNSteadyStream(e, retained, reorder)
					for range 64 {
						s.cycle(b, 10)
					}
					b.ReportAllocs()
					b.ResetTimer()
					for range b.N {
						s.cycle(b, 10)
					}
				})
			}
		}
	}
}
