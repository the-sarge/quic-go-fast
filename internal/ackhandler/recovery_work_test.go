//go:build bbrworkcount

package ackhandler

import (
	"encoding/json"
	"fmt"
	"math"
	"math/bits"
	"os"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
)

// Finite occupancy-scaling checks for recovery service work. Each case holds
// the useful work (ACK ranges, transitions, confirmations, returned or expired
// records) fixed and raises one retained population toward its unchanged cap.
// The same file runs against any tree that provides the counting hooks, so
// set BBR_WORK_REPORT_ONLY=1 to record counts without enforcing the candidate
// bounds, and BBR_WORK_OUT to write them as JSON.

type workCase struct {
	Mechanism  string
	Scenario   string
	Population string
	Occupancy  int
	Ranges     int
	Useful     int // transitions, confirmations, returned or expired records
	LostChange int // outcome lost-state changes, which maintain run indexes
	Work       serviceWork
	Bound      *workBound `json:",omitempty"`
}

type workBound struct{ Inspected, Failed, Nodes uint64 }

func log2ceil(n int) uint64 { return uint64(bits.Len(uint(n))) }

// avlHeight bounds an AVL tree of n nodes.
func avlHeight(n int) uint64 { return uint64(1.4405*math.Log2(float64(n+2)) - 0.3277) }

// The stated per-call bounds. n is the per-space ring occupancy and m the
// retained occupancy. Searches of the three-level slot bitsets read at most five
// words per physical range and at most ten per logical range. A lost-state
// change makes one left-run search (10) and refreshes at most three 16-slot
// blocks, each iterating at most 8 run starts (9 searches of 5 words) and
// evaluating each start with three logical searches (30), then updating at most
// 12 tree nodes: 10 + 3·(45 + 8·30 + 12) = 901, stated as 1000.
const (
	logicalSearchNodes = 10
	lostChangeNodes    = 1000
	// Up to three 16-slot blocks per physical range, each holding at most
	// eight run starts, plus the returned run's endpoint.
	spanQueryInspected = 2*3*8 + 1
	spanQueryNodes     = 2000
)

func boundFor(c workCase) workBound {
	n, m := c.Occupancy, c.Occupancy
	switch c.Mechanism {
	case "ack-transitions":
		return workBound{
			Inspected: uint64(c.Useful),
			Nodes:     uint64(c.Ranges)*(2*log2ceil(n)+logicalSearchNodes) + uint64(c.Useful)*logicalSearchNodes + uint64(c.LostChange)*lostChangeNodes,
		}
	case "ack-witness":
		return workBound{Inspected: uint64(c.Ranges), Nodes: uint64(c.Ranges) * logicalSearchNodes}
	case "pto-confirmation":
		return workBound{Inspected: uint64(c.Useful) + 1, Failed: 1, Nodes: uint64(c.Useful+1)*logicalSearchNodes + uint64(c.LostChange)*lostChangeNodes}
	case "persistent-span":
		return workBound{Inspected: spanQueryInspected, Failed: spanQueryInspected, Nodes: spanQueryNodes}
	case "retained-discovery":
		h := avlHeight(m)
		return workBound{Inspected: uint64(c.Useful), Failed: uint64(c.Ranges) * 2 * h, Nodes: uint64(c.Ranges)*(2*h+2) + uint64(c.Useful)*(3*h+2*log2ceil(m)+2)}
	case "retained-expiry":
		h := avlHeight(m)
		return workBound{Inspected: uint64(c.Useful) + 1, Failed: 1, Nodes: 1 + uint64(c.Useful)*(3*h+4*log2ceil(m)+2)}
	}
	panic(c.Mechanism)
}

var mechanismNames = map[serviceMechanism]string{
	serviceAckTransitions:    "ack-transitions",
	serviceAckWitness:        "ack-witness",
	servicePTOConfirmation:   "pto-confirmation",
	servicePersistentSpan:    "persistent-span",
	serviceRetainedDiscovery: "retained-discovery",
	serviceRetainedExpiry:    "retained-expiry",
}

func measureService(f func()) [serviceMechanisms]serviceWork {
	resetServiceWork()
	f()
	w := serviceCounters
	resetServiceWork()
	return w
}

var workBase = monotime.Time(time.Hour)

func workInfo(pn protocol.PacketNumber, at time.Duration) congestion.PacketInfo {
	return congestion.PacketInfo{
		Space: protocol.Encryption1RTT, EncryptionLevel: protocol.Encryption1RTT, PacketNumber: pn, Ordinal: uint64(pn) + 1,
		SendTime: workBase.Add(at), RegistrationValid: true, AckEliciting: true, InFlight: true, Length: 1200,
	}
}

// ledgerShape lays out registrations in this order: ACKed history, lost runs
// (two lost endpoints and one ACKed separator each), c eligible PTO outcomes,
// one unresolved witness and an ineligible PTO backlog. Send times advance by
// 1µs per registration.
type ledgerShape struct {
	acked, lostRuns, confirmable, backlog int
	ptoLayout                             bool // register the witness and PTO outcomes
}

type builtLedger struct {
	r       *recoveryEvidence
	witness protocol.PacketNumber
	cutoff  monotime.Time
	next    protocol.PacketNumber
}

func buildLedger(s ledgerShape) builtLedger {
	r := &recoveryEvidence{measured: true}
	var pn protocol.PacketNumber
	reg := func() protocol.PacketNumber {
		r.sent(new(workInfo(pn, time.Duration(pn)*time.Microsecond)), 0)
		pn++
		return pn - 1
	}
	app := protocol.Encryption1RTT
	first := pn
	for range s.acked {
		reg()
	}
	if pn > first {
		r.ack(ackFrame(ackRange(first, pn-1)), app)
	}
	for range s.lostRuns {
		a, b, sep := reg(), reg(), reg()
		r.lost(congestionKey(app, a), false, false, 0)
		r.lost(congestionKey(app, b), false, false, 0)
		r.ack(ackFrame(ackRange(sep, sep)), app)
	}
	if !s.ptoLayout {
		return builtLedger{r: r, witness: protocol.InvalidPacketNumber, next: pn}
	}
	for range s.confirmable {
		r.retirePTO(congestionKey(app, reg()))
	}
	cutoff := workBase.Add(time.Duration(pn) * time.Microsecond)
	witness := reg()
	for range s.backlog {
		r.retirePTO(congestionKey(app, reg()))
	}
	return builtLedger{r: r, witness: witness, cutoff: cutoff, next: pn}
}

func (b *builtLedger) register(n int) (lo, hi protocol.PacketNumber) {
	lo = b.next
	for range n {
		b.r.sent(new(workInfo(b.next, time.Duration(b.next)*time.Microsecond)), 0)
		b.next++
	}
	return lo, b.next - 1
}

type workRecorder struct {
	t     *testing.T
	cases []workCase
}

func (w *workRecorder) record(c workCase, m serviceMechanism, work [serviceMechanisms]serviceWork) {
	c.Mechanism = mechanismNames[m]
	c.Work = work[m]
	if os.Getenv("BBR_WORK_REPORT_ONLY") == "" {
		b := boundFor(c)
		c.Bound = &b
		require.LessOrEqual(w.t, c.Work.Inspected, b.Inspected, "%s %s %s=%d inspected", c.Mechanism, c.Scenario, c.Population, c.Occupancy)
		require.LessOrEqual(w.t, c.Work.Failed, b.Failed, "%s %s %s=%d failed", c.Mechanism, c.Scenario, c.Population, c.Occupancy)
		require.LessOrEqual(w.t, c.Work.Nodes, b.Nodes, "%s %s %s=%d nodes", c.Mechanism, c.Scenario, c.Population, c.Occupancy)
	}
	w.cases = append(w.cases, c)
	w.t.Logf("%-18s %-34s %-16s %6d inspected=%-6d failed=%-6d nodes=%d", c.Mechanism, c.Scenario, c.Population, c.Occupancy, c.Work.Inspected, c.Work.Failed, c.Work.Nodes)
}

// The last population exceeds the ring threefold: the ring stays full and wraps.
var ringPopulations = []int{1024, 4096, 16384, 3 * maxRecoveryOutcomes}

func TestRecoveryServiceWorkScaling(t *testing.T) {
	rec := &workRecorder{t: t}
	app := protocol.Encryption1RTT

	for _, n := range ringPopulations {
		occupancy := min(n, maxRecoveryOutcomes)
		// Fresh ACK: two ranges resolve seven new registrations.
		b := buildLedger(ledgerShape{acked: n})
		lo, hi := b.register(8)
		work := measureService(func() { b.r.ack(ackFrame(ackRange(lo+4, hi), ackRange(lo, lo+2)), app) })
		c := workCase{Scenario: "fresh ACK", Population: "acked-history", Occupancy: occupancy, Ranges: 2, Useful: 7}
		rec.record(c, serviceAckTransitions, work)
		rec.record(c, serviceAckWitness, work)

		// Duplicate-only cumulative ACK over the whole history.
		b = buildLedger(ledgerShape{acked: n})
		work = measureService(func() { b.r.ack(ackFrame(ackRange(0, b.next-1)), app) })
		c = workCase{Scenario: "duplicate cumulative ACK", Population: "acked-history", Occupancy: occupancy, Ranges: 1}
		rec.record(c, serviceAckTransitions, work)
		rec.record(c, serviceAckWitness, work)

		// Three new receipts plus a range over the entire history; once the
		// history exceeds the ring, it covers packet numbers the ring no longer
		// holds.
		b = buildLedger(ledgerShape{acked: n})
		lo, hi = b.register(3)
		wide := ackFrame(ackRange(lo, hi), ackRange(0, lo-2))
		work = measureService(func() { b.r.ack(wide, app) })
		c = workCase{Scenario: fmt.Sprintf("sparse ACK over %d numbers", lo), Population: "acked-history", Occupancy: occupancy, Ranges: 2, Useful: 3}
		rec.record(c, serviceAckTransitions, work)
		rec.record(c, serviceAckWitness, work)
	}

	// Late ACKs of lost outcomes split runs; each transition maintains them.
	for _, runs := range []int{16, 1024, 4096, 10000} {
		b := buildLedger(ledgerShape{acked: 64, lostRuns: runs})
		first := protocol.PacketNumber(64) // the first lost run
		work := measureService(func() { b.r.ack(ackFrame(ackRange(first, first+1)), app) })
		c := workCase{Scenario: "late ACK of lost outcomes", Population: "lost-runs", Occupancy: runs, Ranges: 1, Useful: 2, LostChange: 2}
		rec.record(c, serviceAckTransitions, work)
	}

	// PTO confirmation with zero or one eligible outcome before a growing
	// ineligible backlog.
	for _, confirm := range []int{0, 1} {
		for _, backlog := range []int{0, 1024, 8192, maxRecoveryOutcomes - 128} {
			b := buildLedger(ledgerShape{acked: 32, confirmable: confirm, backlog: backlog, ptoLayout: true})
			b.r.ack(ackFrame(ackRange(b.witness, b.witness)), app)
			work := measureService(func() { b.r.confirmPTO(protocol.Encryption1RTT, b.witness, b.cutoff) })
			c := workCase{Scenario: fmt.Sprintf("%d confirmation(s)", confirm), Population: "pto-backlog", Occupancy: backlog, Useful: confirm, LostChange: confirm}
			rec.record(c, servicePTOConfirmation, work)
		}
	}

	// Persistent-span queries over a growing population of short lost runs,
	// with and without one qualifying run at the end.
	for _, qualifying := range []bool{false, true} {
		for _, runs := range []int{1, 1024, 4096, 10000} {
			b := buildLedger(ledgerShape{acked: 64, lostRuns: runs})
			if qualifying {
				lo, hi := b.next, b.next+1
				b.r.sent(new(workInfo(lo, time.Duration(lo)*time.Microsecond)), 0)
				b.r.sent(new(workInfo(hi, time.Duration(lo)*time.Microsecond+time.Second)), 0)
				b.next += 2
				b.r.lost(congestionKey(app, lo), false, false, 0)
				b.r.lost(congestionKey(app, hi), false, false, 0)
			}
			var e congestion.FeedbackEvent
			e.HasAck = true
			work := measureService(func() { b.r.feedback(&e, 100*time.Millisecond) })
			require.Equal(t, qualifying, e.PersistentCongestion.EndOrdinal != 0)
			scenario := map[bool]string{false: "no qualifying run", true: "latest run qualifies"}[qualifying]
			rec.record(workCase{Scenario: scenario, Population: "lost-runs", Occupancy: runs}, servicePersistentSpan, work)
		}
	}

	// Retained ACK discovery and expiry against a growing retained population.
	for _, m := range []int{16, 256, 1024, maxDeliveryRetained} {
		for _, scenario := range []string{"one retained receipt", "duplicate-only ACK", "sparse over-wide ACK"} {
			h, d := retainedDispatch(m, 10*time.Second)
			covered := protocol.PacketNumber(m / 4)
			frame := ackFrame(ackRange(covered, covered))
			useful := 1
			switch scenario {
			case "duplicate-only ACK":
				frame, useful = ackFrame(ackRange(protocol.PacketNumber(m), protocol.PacketNumber(m))), 0
			case "sparse over-wide ACK":
				frame = ackFrame(ackRange(protocol.PacketNumber(m)+2, 8*maxRecoveryOutcomes), ackRange(covered, covered))
			}
			h.beginCongestionFeedback(d.registrationTime, app, 0, nil, frame.LargestAcked())
			work := measureService(func() { h.appendRetainedAck(frame, app) })
			require.Len(t, d.event.Acked, useful)
			rec.record(workCase{Scenario: scenario, Population: "retained", Occupancy: m, Ranges: len(frame.AckRanges), Useful: useful}, serviceRetainedDiscovery, work)
		}
		for _, entry := range []string{"timer", "send", "retirement", "stale deadline"} {
			// One record (m/2) is due 3 ms after setup; every other record
			// expires 30 s after it.
			h, d := retainedDispatch(m, 10*time.Second)
			due := protocol.PacketNumber(m / 2)
			deadline := d.registrationTime.Add(3 * time.Millisecond)
			require.Equal(t, deadline, d.sampler.nextExpiry)
			useful := 1
			if entry == "stale deadline" {
				// The earliest record leaves through an ACK; its deadline remains.
				h.beginCongestionFeedback(d.registrationTime, app, 0, nil, due)
				h.appendRetainedAck(ackFrame(ackRange(due, due)), app)
				require.Equal(t, deadline, d.sampler.nextExpiry)
				useful = 0
			}
			now := d.registrationTime.Add(10 * time.Millisecond)
			var work [serviceMechanisms]serviceWork
			switch entry {
			case "timer", "stale deadline":
				work = measureService(func() { h.ExpireDelivery(now) })
			case "send":
				p := &packet{SendTime: now, Length: 1200, EncryptionLevel: app, Frames: []Frame{{}}, includedInBytesInFlight: true}
				work = measureService(func() { h.captureCongestionSend(protocol.PacketNumber(m+10), p, protocol.ECNNon, 0) })
			case "retirement":
				live := protocol.PacketNumber(m + 1)
				work = measureService(func() { d.retire(congestionKey(app, live), now, 10*time.Second, deliveryRetiredLoss) })
			}
			require.EqualValues(t, useful, d.sampler.expired, entry)
			rec.record(workCase{Scenario: entry + " entry point", Population: "retained", Occupancy: m, Useful: useful}, serviceRetainedExpiry, work)
		}
	}

	if out := os.Getenv("BBR_WORK_OUT"); out != "" {
		data, err := json.MarshalIndent(rec.cases, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(out, data, 0o644))
	}
}

// retainedDispatch retires m registrations (packet numbers 0..m-1) as lost
// with the given PTO, except m/2, which expires 3 ms after the registration
// time. Two live registrations, m and m+1, remain.
func retainedDispatch(m int, pto time.Duration) (*sentPacketHandler, *congestionDispatch) {
	d := &congestionDispatch{sink: &twinSink{}, pending: func() protocol.ByteCount { return 0 }}
	h := &sentPacketHandler{congestionEvents: d}
	app := protocol.Encryption1RTT
	for pn := range protocol.PacketNumber(m + 2) {
		info := workInfo(pn, time.Duration(pn)*time.Microsecond)
		d.ordinal = info.Ordinal
		d.recovery.sent(&info, 0)
		d.packets.set(congestionKey(app, pn), info)
	}
	d.registrationTime = workBase.Add(time.Duration(m+2) * time.Microsecond)
	for pn := range protocol.PacketNumber(m) {
		ttl := pto
		if pn == protocol.PacketNumber(m/2) {
			ttl = time.Millisecond // due 3 ms after setup
		}
		d.retire(congestionKey(app, pn), d.registrationTime, ttl, deliveryRetiredLoss)
	}
	return h, d
}
