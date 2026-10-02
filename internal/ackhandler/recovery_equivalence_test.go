package ackhandler

import (
	"slices"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

// The candidate recovery service must be observably identical to the frozen
// reducer. Every scenario runs through recoveryTwin, which compares witnesses,
// feedback, dispatch state, ledger and retained contents, and recomputed
// indexes; the assertions below additionally prove each scenario engaged the
// semantic case it names.

func is(ps ...*twinPacket) func(*twinPacket) bool {
	return func(p *twinPacket) bool { return slices.Contains(ps, p) }
}

func frameOf(ps ...*twinPacket) *wire.AckFrame {
	pns := make([]protocol.PacketNumber, len(ps))
	for i, p := range ps {
		pns[i] = p.pn
	}
	var ranges []wire.AckRange
	for _, pn := range pns {
		ranges = insertAckRange(ranges, pn)
	}
	return ackFrame(ranges...)
}

// measure makes later registrations persistent-congestion endpoints.
func (w *recoveryTwin) measure() {
	p := w.send(twinSend{})
	w.advance(10 * time.Millisecond)
	w.ack(protocol.Encryption1RTT, frameOf(p), twinAck{rtt: true})
}

// probeAck acknowledges one fresh packet so that an ACK-bearing feedback
// boundary queries persistent spans.
func (w *recoveryTwin) probeAck() {
	p := w.send(twinSend{})
	w.advance(time.Millisecond)
	w.ack(protocol.Encryption1RTT, frameOf(p), twinAck{})
}

func span(start, end *twinPacket) congestion.PersistentCongestion {
	return congestion.PersistentCongestion{StartOrdinal: start.ordinal, EndOrdinal: end.ordinal}
}

func TestRecoveryEquivalenceScenarios(t *testing.T) {
	app := protocol.Encryption1RTT

	t.Run("span threshold, current PTO and report deduplication", func(t *testing.T) {
		w := newRecoveryTwin(t)
		w.pto = 100 * time.Millisecond
		w.measure()
		e1 := w.send(twinSend{})
		w.advance(300 * time.Millisecond)
		e2 := w.send(twinSend{})
		w.advance(150 * time.Millisecond)
		e3 := w.send(twinSend{})
		w.timerLoss(app, is(e1, e2))
		w.probeAck()
		require.Zero(t, w.last.PersistentCongestion, "equality at 3*PTO does not qualify")
		for _, pto := range []time.Duration{0, time.Duration(1<<63-1)/3 + 1} {
			w.pto = pto
			w.probeAck()
			require.Zero(t, w.last.PersistentCongestion, "invalid PTO %d", pto)
		}
		w.pto = 99 * time.Millisecond
		w.probeAck()
		require.Equal(t, span(e1, e2), w.last.PersistentCongestion, "an unchanged span qualifies when PTO falls")
		w.probeAck()
		require.Zero(t, w.last.PersistentCongestion, "a reported endpoint is not reported again")
		w.timerLoss(app, is(e3))
		w.probeAck()
		require.Equal(t, span(e1, e3), w.last.PersistentCongestion, "a later endpoint extends the reported run")
		w.pto = 200 * time.Millisecond
		e4 := w.send(twinSend{})
		w.timerLoss(app, is(e4))
		w.probeAck()
		require.Zero(t, w.last.PersistentCongestion, "PTO increase")
	})

	t.Run("latest of multiple qualifying runs", func(t *testing.T) {
		w := newRecoveryTwin(t)
		w.pto = 100 * time.Millisecond
		w.measure()
		a1 := w.send(twinSend{})
		w.advance(400 * time.Millisecond)
		a2 := w.send(twinSend{})
		gap := w.send(twinSend{})
		b1 := w.send(twinSend{})
		w.advance(400 * time.Millisecond)
		b2 := w.send(twinSend{})
		w.timerLoss(app, is(a1, a2, b1, b2))
		w.ack(app, frameOf(gap), twinAck{})
		require.Equal(t, span(b1, b2), w.last.PersistentCongestion)
		w.probeAck()
		require.Zero(t, w.last.PersistentCongestion, "the earlier run's endpoint precedes the report")
	})

	t.Run("a later run at exactly 3*PTO does not mask an earlier qualifying run", func(t *testing.T) {
		w := newRecoveryTwin(t)
		w.pto = 100 * time.Millisecond
		w.measure()
		a1 := w.send(twinSend{})
		w.advance(400 * time.Millisecond)
		a2 := w.send(twinSend{})
		gap := w.send(twinSend{})
		b1 := w.send(twinSend{})
		w.advance(300 * time.Millisecond)
		b2 := w.send(twinSend{})
		require.Equal(t, (a1.ordinal-1)/spanBlockSlots, (b2.ordinal-1)/spanBlockSlots, "a fresh ring stores ordinal n in slot n-1: both runs share one index block")
		w.timerLoss(app, is(a1, a2, b1, b2))
		w.ack(app, frameOf(gap), twinAck{})
		require.Equal(t, span(a1, a2), w.last.PersistentCongestion)
	})

	t.Run("run in the first block behind later history", func(t *testing.T) {
		w := newRecoveryTwin(t)
		w.pto = 100 * time.Millisecond
		w.measure()
		a1 := w.send(twinSend{})
		w.advance(400 * time.Millisecond)
		a2 := w.send(twinSend{})
		var later []*twinPacket
		for range 3 * spanBlockSlots {
			later = append(later, w.send(twinSend{}))
		}
		w.timerLoss(app, is(a1, a2))
		w.ack(app, frameOf(later...), twinAck{})
		require.Equal(t, span(a1, a2), w.last.PersistentCongestion)
	})

	t.Run("late ACK splits a lost run", func(t *testing.T) {
		w := newRecoveryTwin(t)
		w.pto = 100 * time.Millisecond
		w.measure()
		var e [5]*twinPacket
		for i, at := range []time.Duration{0, 50, 200, 10, 140} {
			w.advance(at * time.Millisecond)
			e[i] = w.send(twinSend{})
		}
		w.timerLoss(app, is(e[:]...))
		w.advance(time.Millisecond)
		w.ack(app, frameOf(e[2]), twinAck{})
		require.Len(t, w.last.Acked, 1, "the late ACK discovers retained loss evidence")
		require.Zero(t, w.last.PersistentCongestion, "split runs no longer span 3*PTO")
	})

	t.Run("PTO confirmation merges runs", func(t *testing.T) {
		w := newRecoveryTwin(t)
		w.pto = 100 * time.Millisecond
		w.measure()
		e1 := w.send(twinSend{})
		w.advance(150 * time.Millisecond)
		e2 := w.send(twinSend{})
		w.advance(250 * time.Millisecond)
		e3 := w.send(twinSend{})
		w.timerLoss(app, is(e1))
		require.True(t, w.retirePTO(app), "e2 is the first outstanding packet")
		w.timerLoss(app, is(e3))
		require.Equal(t, outcomeUnresolved, refState(w, e2))
		w.probeAck()
		require.Equal(t, outcomeLost, refState(w, e2))
		require.Equal(t, span(e1, e3), w.last.PersistentCongestion, "the confirmed PTO outcome joins two lost runs")
	})

	t.Run("duplicate-only ACK before and after the PTO cutoff", func(t *testing.T) {
		w := newRecoveryTwin(t)
		w.measure()
		p := w.send(twinSend{})
		q := w.send(twinSend{})
		w.advance(5 * time.Millisecond)
		require.True(t, w.retirePTO(app))
		w.ack(app, frameOf(q), twinAck{})
		require.Equal(t, outcomeUnresolved, refState(w, p), "before the cutoff")
		w.advance(5 * time.Millisecond)
		suppressed := w.coverage.SuppressedFeedbacks
		w.ack(app, frameOf(q), twinAck{})
		require.Equal(t, outcomeUnresolved, refState(w, p), "still before the cutoff")
		require.Equal(t, suppressed+1, w.coverage.SuppressedFeedbacks, "duplicate without new evidence stays suppressed")
		w.advance(w.lossDelay)
		w.ack(app, frameOf(q), twinAck{})
		require.Equal(t, outcomeLost, refState(w, p), "the duplicate witness confirms after the cutoff")
	})

	t.Run("witness excludes a disposed greatest registration", func(t *testing.T) {
		w := newRecoveryTwin(t)
		w.measure()
		p1 := w.send(twinSend{})
		p2 := w.send(twinSend{})
		p3 := w.send(twinSend{pathProbe: true})
		p4 := w.send(twinSend{})
		w.discardPacket(p4)
		require.Equal(t, p2.pn, w.ack(app, frameOf(p1, p2, p3, p4), twinAck{}), "path probes and disposed outcomes are not witnesses")
	})

	t.Run("lost-to-ACKed and excluded-to-ACKed receipts exit recovery", func(t *testing.T) {
		w := newRecoveryTwin(t)
		w.measure()
		lost := w.send(twinSend{})
		w.timerLoss(app, is(lost))
		require.True(t, w.last.RecoveryEpisode.Entered)
		z := w.send(twinSend{ackOnly: true})
		w.timerLoss(app, is(z)) // ACK-only loss: no delivery evidence is retained
		w.advance(time.Millisecond)
		w.ack(app, frameOf(z), twinAck{})
		require.True(t, w.last.RecoveryEpisode.Exited, "a resolved ACK-only receipt crosses the boundary")

		w2 := newRecoveryTwin(t)
		w2.measure()
		lost = w2.send(twinSend{})
		w2.timerLoss(app, is(lost))
		w2.advance(time.Millisecond)
		x := w2.send(twinSend{ackOnly: true, backdate: time.Hour}) // invalid registration: excluded
		w2.timerLoss(app, is(x))
		require.Equal(t, outcomeExcluded, refState(w2, x))
		w2.ack(app, frameOf(x), twinAck{})
		require.True(t, w2.last.RecoveryEpisode.Exited, "an eligible excluded receipt crosses the boundary")
	})

	t.Run("out-of-order expiry and stale deadlines", func(t *testing.T) {
		w := newRecoveryTwin(t)
		w.measure()
		a := w.send(twinSend{})
		b := w.send(twinSend{})
		c := w.send(twinSend{})
		w.pto = time.Second
		w.timerLoss(app, is(a))
		w.pto = 100 * time.Millisecond
		w.timerLoss(app, is(b))
		bDeadline := w.now.Add(300 * time.Millisecond)
		require.Equal(t, bDeadline, w.ref.sampler.nextExpiry, "a newer record with a shorter PTO expires first")
		w.ack(app, frameOf(b, c), twinAck{})
		require.Equal(t, bDeadline, w.ref.sampler.nextExpiry, "ACK removal leaves the earliest deadline stale")
		w.now = bDeadline
		stale := w.coverage.StaleDeadlines
		w.send(twinSend{}) // send entry point fires the stale deadline
		require.Equal(t, stale+1, w.coverage.StaleDeadlines)
		require.Equal(t, w.ref.sampler.retained[congestionKey(app, a.pn)].expires, w.ref.sampler.nextExpiry)
		w.now = w.ref.sampler.nextExpiry
		w.expireTimer()
		require.EqualValues(t, 1, w.ref.sampler.expired, "the older long-PTO record expires through the timer")

		w = newRecoveryTwin(t)
		w.measure()
		h := w.send(twinSend{level: protocol.EncryptionHandshake})
		d := w.send(twinSend{})
		e := w.send(twinSend{})
		w.pto = 100 * time.Millisecond
		w.timerLoss(protocol.EncryptionHandshake, is(h))
		w.pto = time.Second
		w.timerLoss(app, is(d))
		hDeadline := w.now.Add(300 * time.Millisecond)
		require.Equal(t, hDeadline, w.ref.sampler.nextExpiry)
		w.discardSpace(protocol.EncryptionHandshake)
		require.Equal(t, hDeadline, w.ref.sampler.nextExpiry, "disposal leaves the earliest deadline stale")
		w.now = hDeadline
		stale = w.coverage.StaleDeadlines
		w.timerLoss(app, is(e)) // retirement entry point fires the stale deadline
		require.Equal(t, stale+1, w.coverage.StaleDeadlines)
		require.Len(t, w.ref.sampler.retained, 2)
	})

	t.Run("missing evidence invalidates undo", func(t *testing.T) {
		w := newRecoveryTwin(t)
		w.measure()
		p := w.send(twinSend{})
		w.pto = 10 * time.Millisecond
		w.timerLoss(app, is(p))
		require.True(t, w.ref.recovery.episode.UndoPossible)
		w.advance(time.Second)
		w.expireTimer()
		require.False(t, w.ref.recovery.episode.UndoPossible, "expiry of a member's retained evidence")
	})

	t.Run("eviction of a run's first endpoint and loss of an evicted outcome", func(t *testing.T) {
		w := newRecoveryTwin(t)
		w.checkEvery = 4096
		w.pto = 100 * time.Millisecond
		w.measure()
		e1 := w.send(twinSend{})
		w.advance(time.Millisecond)
		e2 := w.send(twinSend{})
		w.advance(400 * time.Millisecond)
		e3 := w.send(twinSend{})
		k := w.send(twinSend{})
		w.timerLoss(app, is(e1, e2, e3))
		for range maxRecoveryOutcomes - 4 {
			w.send(twinSend{ackOnly: true})
		}
		require.EqualValues(t, 1, w.ref.recovery.evicted, "the measuring packet is evicted")
		w.probeAck()
		require.EqualValues(t, 2, w.ref.recovery.evicted, "the probe's registration evicts e1")
		require.Equal(t, span(e2, e3), w.last.PersistentCongestion, "the run starts at its first retained endpoint")
		for range 3 {
			w.send(twinSend{ackOnly: true})
		}
		w.timerLoss(app, is(k))
		require.True(t, w.ref.recovery.episode.Active)
		require.False(t, w.ref.recovery.episode.UndoPossible, "an evicted outcome cannot support undo")
		w.checkpoint()
	})

	t.Run("multi-space ring wrap with a run across the physical wrap", func(t *testing.T) {
		w := newRecoveryTwin(t)
		w.checkEvery = 4096
		w.pto = 100 * time.Millisecond
		w.measure() // registration 0, slot 0
		var early []*twinPacket
		for _, l := range []protocol.EncryptionLevel{protocol.EncryptionInitial, protocol.EncryptionInitial, protocol.EncryptionHandshake, protocol.EncryptionHandshake} {
			early = append(early, w.send(twinSend{level: l}))
		}
		zero := w.send(twinSend{backdate: time.Duration(w.now)}) // zero send time: an excluded registration
		w.timerLoss(app, is(zero))
		require.Equal(t, outcomeExcluded, refState(w, zero))
		for range maxRecoveryOutcomes - 1 - 6 {
			w.send(twinSend{ackOnly: true})
		}
		e1 := w.send(twinSend{}) // slot 32767
		w.advance(time.Millisecond)
		hx := w.send(twinSend{level: protocol.EncryptionHandshake}) // slot 0
		w.advance(200 * time.Millisecond)
		e2 := w.send(twinSend{}) // slot 1, evicting the first Initial entry
		w.advance(200 * time.Millisecond)
		e3 := w.send(twinSend{}) // slot 2, evicting the second
		_, live := w.ref.recovery.keys[congestionKey(protocol.EncryptionInitial, early[1].pn)]
		require.False(t, live, "the oldest Initial entries are evicted while application entries live")
		w.timerLoss(app, is(e1, e2, e3))
		w.timerLoss(protocol.EncryptionHandshake, is(hx))
		require.NotZero(t, w.ref.recovery.head)
		w.probeAck() // evicts a Handshake entry
		require.Equal(t, span(e1, e3), w.last.PersistentCongestion, "a lost run spanning spaces and the physical wrap")
		w.checkpoint()
		for range maxRecoveryOutcomes - 4 {
			w.send(twinSend{ackOnly: true})
		}
		_, live = w.ref.recovery.keys[congestionKey(app, e1.pn)]
		require.False(t, live, "e1 is evicted; the run now starts in a different index block")
		_, live = w.ref.recovery.keys[congestionKey(protocol.EncryptionHandshake, hx.pn)]
		require.True(t, live)
		w.checkpoint()
		w.pto = 90 * time.Millisecond
		e4 := w.send(twinSend{})
		w.timerLoss(app, is(e4))
		w.probeAck()
		w.checkpoint()
	})

	t.Run("retained capacity eviction", func(t *testing.T) {
		w := newRecoveryTwin(t)
		w.checkEvery = 1024
		w.measure()
		w.pto = 5 * time.Second
		for range maxDeliveryRetained + 4 {
			w.send(twinSend{})
		}
		w.timerLoss(app, func(*twinPacket) bool { return true })
		require.EqualValues(t, 4, w.ref.sampler.evicted)
		require.Len(t, w.ref.sampler.retained, maxDeliveryRetained)
		w.checkpoint()
	})

	t.Run("wide cumulative and sparse ACK ranges", func(t *testing.T) {
		w := newRecoveryTwin(t)
		w.checkEvery = 4096
		w.measure()
		for range 2*maxRecoveryOutcomes + 100 {
			w.send(twinSend{})
		}
		w.pto = time.Second
		w.timerLoss(app, func(p *twinPacket) bool { return p.pn%7 == 0 })
		largest := w.nextPN[2] - 1
		w.ack(app, ackFrame(ackRange(largest-3, largest), ackRange(largest-maxRecoveryOutcomes-10, largest-20), ackRange(0, 5)), twinAck{})
		w.ack(app, ackFrame(ackRange(0, largest)), twinAck{})
		w.ack(app, ackFrame(ackRange(0, largest)), twinAck{})
		w.checkpoint()
	})

	t.Run("space disposal and reset", func(t *testing.T) {
		w := newRecoveryTwin(t)
		w.measure()
		var ps []*twinPacket
		for _, l := range []protocol.EncryptionLevel{protocol.EncryptionInitial, protocol.Encryption0RTT, protocol.EncryptionHandshake, protocol.Encryption1RTT} {
			for range 5 {
				ps = append(ps, w.send(twinSend{level: l}))
			}
		}
		w.timerLoss(app, is(ps[5], ps[15]))
		w.discardSpace(protocol.Encryption0RTT)
		w.discardSpace(protocol.EncryptionInitial)
		w.ack(app, frameOf(ps[16:]...), twinAck{})
		w.reset(true)
		w.measure()
		w.probeAck()
		w.reset(false)
		w.checkpoint()
	})
}

func refState(w *recoveryTwin, p *twinPacket) recoveryOutcomeState {
	i, ok := w.ref.recovery.keys[congestionKey(p.p.EncryptionLevel, p.pn)]
	require.True(w.t, ok)
	return w.ref.recovery.outcomes[i].state
}

func TestRecoveryEquivalenceHistories(t *testing.T) {
	if testing.Short() {
		t.Skip("long differential histories")
	}
	var total twinCoverage
	for _, tc := range []struct {
		name       string
		wl         twinWorkload
		checkEvery int
	}{
		{"loss-free with ring eviction and wide ACKs", twinWorkload{seed: 11, sends: 80000, wide: 0.02, duplicate: 0.05, expire: 0.01}, 4000},
		{"lossy with bursts, spurious losses and PTO shifts", twinWorkload{seed: 12, sends: 60000, loss: 0.01, burst: 0.0005, burstLen: 1500, pto: 0.003, ackOnly: 0.05, late: 0.03, wide: 0.01, duplicate: 0.05, ptoShift: 0.002, expire: 0.05}, 2000},
		{"PTO-heavy with duplicate ACKs", twinWorkload{seed: 13, sends: 30000, loss: 0.02, burst: 0.0005, burstLen: 1200, pto: 0.05, duplicate: 0.25, late: 0.02, expire: 0.05, ptoShift: 0.001}, 2000},
		{"mixed spaces, probes, skips, invalid registrations, retained pressure and resets", twinWorkload{seed: 14, sends: 50000, loss: 0.01, burst: 0.0003, burstLen: 1500, pto: 0.005, probe: 0.005, mtu: 0.005, ackOnly: 0.05, invalid: 0.005, skip: 0.02, late: 0.02, wide: 0.01, duplicate: 0.05, ptoShift: 0.002, expire: 0.05, mixed: true, retainedPressure: true, resetEvery: 20000}, 2000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := runTwinWorkload(t, tc.wl, tc.checkEvery)
			t.Logf("%+v", c)
			total.Persistent += c.Persistent
			total.Exited += c.Exited
			total.UndoEligible += c.UndoEligible
			total.OutcomeEvicted += c.OutcomeEvicted
			total.RetainedEvicted += c.RetainedEvicted
			total.RetainedExpired += c.RetainedExpired
			total.StaleDeadlines += c.StaleDeadlines
		})
	}
	// A history set that never produces these semantic events cannot show
	// equivalence of the paths that produce them.
	require.Positive(t, total.Persistent, "persistent-congestion reports")
	require.Positive(t, total.Exited, "episode exits")
	require.Positive(t, total.UndoEligible, "undo-eligible events")
	require.Positive(t, total.OutcomeEvicted, "outcome-ring evictions")
	require.Positive(t, total.RetainedEvicted, "retained capacity evictions")
	require.Positive(t, total.RetainedExpired, "retained expiry")
	require.Positive(t, total.StaleDeadlines, "stale expiry deadlines")
}

func TestSlotSetSearches(t *testing.T) {
	var s slotSet
	present := make([]bool, maxRecoveryOutcomes)
	rng := newTestRand(7)
	brute := func(from, to int, want bool, forward bool) int {
		if forward {
			for i := from; i <= to; i++ {
				if present[i] == want {
					return i
				}
			}
		} else {
			for i := to; i >= from; i-- {
				if present[i] == want {
					return i
				}
			}
		}
		return -1
	}
	// Full suffixes and a full set exercise the clear-search summaries at
	// their upper edge.
	for _, lo := range []int{maxRecoveryOutcomes - 1, maxRecoveryOutcomes - 64, maxRecoveryOutcomes - 4096, maxRecoveryOutcomes - 5000, 0} {
		var full slotSet
		for i := lo; i < maxRecoveryOutcomes; i++ {
			full.set(i)
		}
		for _, from := range []int{0, lo - 1, lo, maxRecoveryOutcomes - 1} {
			if from < 0 {
				continue
			}
			want := -1
			if from < lo {
				want = from
			}
			require.Equal(t, want, full.nextClear(from, maxRecoveryOutcomes-1), "suffix from %d, search from %d", lo, from)
			require.Equal(t, max(from, lo), full.nextSet(from, maxRecoveryOutcomes-1))
		}
		require.Equal(t, lo-1, full.prevClear(0, maxRecoveryOutcomes-1))
	}
	for round := range 400 {
		switch round % 4 {
		case 0: // sparse random toggles
			for range 8 {
				i := rng.IntN(maxRecoveryOutcomes)
				present[i] = !present[i]
				s.assign(i, present[i])
			}
		case 1: // fill or clear a long run, crossing word and summary boundaries
			lo := rng.IntN(maxRecoveryOutcomes)
			hi := min(maxRecoveryOutcomes-1, lo+rng.IntN(9000))
			v := rng.IntN(2) == 0
			for i := lo; i <= hi; i++ {
				present[i] = v
				s.assign(i, v)
			}
		}
		for range 8 {
			from := rng.IntN(maxRecoveryOutcomes)
			to := min(maxRecoveryOutcomes-1, from+rng.IntN(maxRecoveryOutcomes))
			if rng.IntN(8) == 0 {
				from, to = 0, maxRecoveryOutcomes-1
			}
			require.Equal(t, brute(from, to, true, true), s.nextSet(from, to))
			require.Equal(t, brute(from, to, false, true), s.nextClear(from, to))
			require.Equal(t, brute(from, to, true, false), s.prevSet(from, to))
			require.Equal(t, brute(from, to, false, false), s.prevClear(from, to))
		}
	}
}

func newTestRand(seed uint64) interface{ IntN(int) int } {
	return &splitmix{state: seed}
}

type splitmix struct{ state uint64 }

func (s *splitmix) IntN(n int) int {
	s.state += 0x9e3779b97f4a7c15
	z := s.state
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return int((z ^ (z >> 31)) % uint64(n))
}
