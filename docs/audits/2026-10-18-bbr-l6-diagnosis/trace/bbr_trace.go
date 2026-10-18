package congestion

import (
	"encoding/json"
	"math"
	"os"
	"strconv"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
)

// Measurement-only #792 overlay: the BBR model's state at every packet-timed
// round, every loss-round close with loss, every two-percent probe-loss check,
// and before and after every event that changes the phase, a loss bound, the
// recovery episode or the CE state. Rows live in a fixed array outside the Go
// heap; the connection goroutine writes them once, on its first feedback event
// at or after BBR_TRACE_UNTIL_UNIX_NS (the measured window's end), and records
// nothing afterwards. Enabled only in the fixture's sender process, only for
// the first BBR sender that receives feedback. Not a maintained hook.

const (
	traceKindRound       = 1
	traceKindPre         = 2
	traceKindPost        = 3
	traceKindLossPre     = 4
	traceKindLossPost    = 5
	traceKindProbeLoss   = 6
	traceKindOutsidePre  = 7
	traceKindOutsidePost = 8
	traceKindReset       = 9

	traceCols     = 48
	traceCapacity = 1 << 16
)

// TraceColumns names each row column; columns 40-47 are kind-specific.
var TraceColumns = [traceCols]string{"kind", "unix_ns", "mono_ns", "seq", "round", "phase", "bandwidth", "bandwidth_short",
	"inflight_long", "inflight_short", "window", "rate", "min_rtt_ns", "cycle", "rounds_since_probe", "probe_wait_ns",
	"cycle_stamp_mono", "flags", "plateau", "full_bandwidth", "ack_phase", "size", "bdp1", "extra", "quantum",
	"prior_in_flight", "post_in_flight", "delivered", "lost", "sample_rate", "latest_rate", "latest_volume",
	"loss_bytes", "loss_flight", "probe_rtt_stamp_mono", "probe_rtt_min_ns", "min_rtt_stamp_mono", "ce_rate",
	"ce_flight", "sample_generation", "a0", "a1", "a2", "a3", "a4", "a5", "a6", "a7"}

// TraceFlags names the bits of the flags column, lowest first.
var TraceFlags = []string{"probe_sample", "previous_probe_too_high", "previous_probe_precautionary", "ce_active",
	"recovery_started", "window_used_in_round", "window_used_last_round", "idle_restart", "persistent_waiting",
	"loss_pending", "recovery_eligible", "has_ack", "sample_valid", "sample_limited", "ecn_failed",
	"probe_rtt_holding", "loss_overflow", "episode_entered", "episode_exited", "undo_eligible", "closed"}

type bbrTraceState struct {
	on, dumped     bool
	output         string
	untilNS        int64
	target         *BBRSender
	rows           *[traceCapacity][traceCols]int64
	n, overflow    int
	seq            int64
	foreign        int64
	probeEntries   int64
	ecnFailedSeen  bool
	dumpError      string
	pre, last      BBRSender
	preEvent       [5]int64 // prior, post, delivered, lost, sample rate at event entry
	preFlags       int64
	haveLast       bool
	lossPreWritten bool
	// Accumulators for the current packet-timed round.
	roundLostBytes, roundLostPackets, roundMaxPrior, roundEvents int64
	roundMaxRate, roundLimited, roundDelivered, roundProbeChecks int64
}

var trace bbrTraceState

var traceRowStore [traceCapacity][traceCols]int64

func init() { traceConfigure(os.Args, os.Getenv("BBR_TRACE_OUTPUT"), os.Getenv("BBR_TRACE_UNTIL_UNIX_NS")) }

// traceConfigure enables the overlay only for a sender-role process with both
// an output path and a positive end time.
func traceConfigure(args []string, output, until string) {
	sender := false
	for i, a := range args {
		if ((a == "-role" || a == "--role") && i+1 < len(args) && args[i+1] == "send") || a == "-role=send" || a == "--role=send" {
			sender = true
		}
	}
	ns, err := strconv.ParseInt(until, 10, 64)
	trace = bbrTraceState{on: sender && output != "" && err == nil && ns > 0, output: output, untilNS: ns, rows: &traceRowStore}
}

func traceClampU(v uint64) int64 {
	if v == math.MaxUint64 {
		return -1
	}
	if v > 1<<62 {
		return 1 << 62
	}
	return int64(v)
}

func traceClampB(v protocol.ByteCount) int64 {
	if v == protocol.MaxByteCount {
		return -1
	}
	return int64(v)
}

func traceBool(v bool, bit uint) int64 {
	if v {
		return 1 << bit
	}
	return 0
}

func traceModelFlags(b *BBRSender) int64 {
	return traceBool(b.probeSample, 0) | traceBool(b.previousProbeTooHigh, 1) | traceBool(b.previousProbePrecautionary, 2) |
		traceBool(b.ce.active, 3) | traceBool(b.recoveryStarted, 4) | traceBool(b.windowUsedInRound, 5) |
		traceBool(b.windowUsedLastRound, 6) | traceBool(b.idleRestart, 7) | traceBool(b.persistentWaiting, 8) |
		traceBool(b.lossPending, 9) | traceBool(b.recoveryEligible, 10) | traceBool(b.probeRTTHoldUntil != 0, 15) |
		traceBool(b.lossOverflow, 16) | traceBool(b.closed, 20)
}

func traceEventFlags(e *FeedbackEvent) int64 {
	r := e.RecoveryEpisode
	return traceBool(e.HasAck, 11) | traceBool(e.Delivery.Valid, 12) | traceBool(bbrLimited(e.Delivery.Limited), 13) |
		traceBool(e.ECN.Failed, 14) | traceBool(r.Entered, 17) | traceBool(r.Exited, 18) | traceBool(r.UndoEligible, 19)
}

func traceSampleRate(e *FeedbackEvent) int64 {
	if !e.HasAck || !e.Delivery.Valid {
		return 0
	}
	return traceClampU(e.Delivery.BytesPerSecond)
}

// traceRow appends one row: the model columns from b, the event columns from
// event (or zero), and the kind-specific arguments.
func traceRow(kind int64, b *BBRSender, mono int64, event [5]int64, eventFlags int64, args ...int64) {
	if trace.n >= traceCapacity {
		trace.overflow++
		return
	}
	r := &trace.rows[trace.n]
	trace.n++
	*r = [traceCols]int64{kind, time.Now().UnixNano(), mono, trace.seq, traceClampU(b.round), int64(b.phase),
		traceClampU(b.bandwidth), traceClampU(b.bandwidthShort), traceClampB(b.inflightLong), traceClampB(b.inflightShort),
		int64(b.window), traceClampU(b.rate), int64(b.minimumRTT), traceClampU(b.cycle), traceClampU(b.roundsSinceProbe),
		int64(b.probeWait), int64(b.cycleStamp), traceModelFlags(b) | eventFlags, int64(b.plateau), traceClampU(b.fullBandwidth),
		int64(b.ackPhase), int64(b.size), int64(b.bdp(1)), int64(b.aggregationMaximum()), int64(b.quantum()),
		event[0], event[1], event[2], event[3], event[4], traceClampU(b.latestRate), traceClampU(b.latestVolume),
		int64(b.lossBytes), int64(b.lossFlight), int64(b.probeRTTStamp), int64(b.probeRTTMinimum), int64(b.minimumStamp),
		traceClampU(b.ce.rate), int64(b.ce.flight), traceClampU(b.sampleGeneration)}
	copy(r[40:], args)
}

func traceEvent(e *FeedbackEvent) [5]int64 {
	return [5]int64{int64(e.PriorInFlight), int64(e.PostInFlight), traceClampU(e.Delivery.Delivered), traceClampU(e.Delivery.Lost), traceSampleRate(e)}
}

// traceActive reports whether b is the traced sender, claiming the first one.
func traceActive(b *BBRSender) bool {
	if !trace.on || trace.dumped {
		return false
	}
	if trace.target == nil {
		trace.target = b
	}
	if trace.target != b {
		trace.foreign++
		return false
	}
	return true
}

// traceChanged reports what an event changed: bit 0 phase, 1 short-term
// bandwidth, 2 short-term inflight, 3 long-term inflight decrease, 4 long-term
// inflight set from or reset to unbounded, 5 recovery episode, 6 first ECN
// failure, 7 CE state, 8 ProbeRTT hold, 9 bandwidth decrease.
func traceChanged(a, b *BBRSender, eventFlags int64) int64 {
	var m int64
	if a.phase != b.phase {
		m |= 1 << 0
	}
	if a.bandwidthShort != b.bandwidthShort {
		m |= 1 << 1
	}
	if a.inflightShort != b.inflightShort {
		m |= 1 << 2
	}
	if b.inflightLong < a.inflightLong && a.inflightLong != protocol.MaxByteCount {
		m |= 1 << 3
	}
	if (a.inflightLong == protocol.MaxByteCount) != (b.inflightLong == protocol.MaxByteCount) {
		m |= 1 << 4
	}
	if eventFlags&(1<<17|1<<18|1<<19) != 0 || a.recoveryStarted != b.recoveryStarted {
		m |= 1 << 5
	}
	if a.ce.active != b.ce.active || a.ce.rate != b.ce.rate || a.ce.flight != b.ce.flight {
		m |= 1 << 7
	}
	if (a.probeRTTHoldUntil == 0) != (b.probeRTTHoldUntil == 0) {
		m |= 1 << 8
	}
	if b.bandwidth < a.bandwidth {
		m |= 1 << 9
	}
	return m
}

func traceFeedbackStart(b *BBRSender, e *FeedbackEvent) {
	if !traceActive(b) {
		return
	}
	if time.Now().UnixNano() >= trace.untilNS {
		traceDump()
		return
	}
	trace.seq++
	if trace.haveLast {
		if m := traceChanged(&trace.last, b, 0); m != 0 {
			traceRow(traceKindOutsidePre, &trace.last, int64(e.Time), [5]int64{}, 0)
			traceRow(traceKindOutsidePost, b, int64(e.Time), [5]int64{}, 0, m)
		}
	}
	trace.pre = *b
	trace.preEvent, trace.preFlags = traceEvent(e), traceEventFlags(e)
	trace.lossPreWritten = false
	trace.roundEvents++
	trace.roundMaxPrior = max(trace.roundMaxPrior, int64(e.PriorInFlight))
	trace.roundMaxRate = max(trace.roundMaxRate, traceSampleRate(e))
	if bbrLimited(e.Delivery.Limited) {
		trace.roundLimited++
	}
	if e.PathGeneration == b.pathGeneration {
		for _, p := range e.Lost {
			if p.AckEliciting && !p.PathProbe && !p.MTUProbe && p.Length > 0 && p.PathGeneration == e.PathGeneration {
				trace.roundLostBytes += int64(p.Length)
				trace.roundLostPackets++
			}
		}
	}
}

func traceFeedbackEnd(b *BBRSender, e *FeedbackEvent) {
	if !trace.on || trace.dumped || trace.target != b {
		return
	}
	m := traceChanged(&trace.pre, b, trace.preFlags)
	if e.ECN.Failed && !trace.ecnFailedSeen {
		trace.ecnFailedSeen = true
		m |= 1 << 6
	}
	if m != 0 {
		traceRow(traceKindPre, &trace.pre, int64(e.Time), trace.preEvent, trace.preFlags)
		traceRow(traceKindPost, b, int64(e.Time), traceEvent(e), traceEventFlags(e), m)
	}
	trace.last, trace.haveLast = *b, true
}

// traceRoundStart records the round that has just completed.
func traceRoundStart(b *BBRSender, e *FeedbackEvent) {
	if !trace.on || trace.dumped || trace.target != b {
		return
	}
	traceRow(traceKindRound, b, int64(e.Time), traceEvent(e), traceEventFlags(e), trace.roundLostBytes, trace.roundLostPackets,
		trace.roundMaxPrior, trace.roundEvents, trace.roundMaxRate, trace.roundLimited, trace.roundDelivered, trace.roundProbeChecks)
	trace.roundDelivered = traceClampU(e.Delivery.Delivered)
	trace.roundLostBytes, trace.roundLostPackets, trace.roundMaxPrior, trace.roundEvents = 0, 0, 0, 0
	trace.roundMaxRate, trace.roundLimited, trace.roundProbeChecks = 0, 0, 0
}

// traceLossRoundPre and traceLossRoundPost bracket the loss-round close, only
// for a round that had loss.
func traceLossRoundPre(b *BBRSender, e *FeedbackEvent) {
	if !trace.on || trace.dumped || trace.target != b || !b.lossPending {
		return
	}
	trace.lossPreWritten = true
	traceRow(traceKindLossPre, b, int64(e.Time), traceEvent(e), traceEventFlags(e), traceBool(b.probingBandwidth(), 0))
}

func traceLossRoundPost(b *BBRSender, e *FeedbackEvent) {
	if !trace.on || trace.dumped || trace.target != b || !trace.lossPreWritten {
		return
	}
	trace.lossPreWritten = false
	traceRow(traceKindLossPost, b, int64(e.Time), traceEvent(e), traceEventFlags(e))
}

func traceProbeLossEntry(b *BBRSender) {
	if trace.on && !trace.dumped && trace.target == b && b.probeSample {
		trace.probeEntries++
	}
}

// traceProbeLoss records one two-percent check, before its outcome is applied.
func traceProbeLoss(b *BBRSender, e *FeedbackEvent, p *PacketInfo, lost, flight, remaining uint64) {
	if !trace.on || trace.dumped || trace.target != b {
		return
	}
	trace.roundProbeChecks++
	traceRow(traceKindProbeLoss, b, int64(e.Time), traceEvent(e), traceEventFlags(e), traceClampU(lost), traceClampU(flight),
		int64(p.Length), int64(p.Delivery.PostInFlight), int64(p.Delivery.Limited), traceClampU(p.Ordinal),
		traceClampU(remaining), traceClampU(p.Delivery.Lost))
}

func traceReset(b *BBRSender, path, sample, delivered uint64) {
	if !trace.on || trace.dumped || trace.target != b {
		return
	}
	traceRow(traceKindReset, b, 0, [5]int64{}, 0, traceClampU(path), traceClampU(sample), traceClampU(delivered))
}

func traceDump() {
	trace.dumped = true
	data, err := json.Marshal(map[string]any{
		"columns": TraceColumns, "flags": TraceFlags, "rows": trace.rows[:trace.n],
		"overflow": trace.overflow, "foreign_events": trace.foreign, "probe_loss_entries": trace.probeEntries,
		"until_unix_ns": trace.untilNS, "dumped_unix_ns": time.Now().UnixNano(), "events": trace.seq, "capacity": traceCapacity,
	})
	if err == nil {
		tmp := trace.output + ".tmp"
		if err = os.WriteFile(tmp, data, 0o644); err == nil {
			err = os.Rename(tmp, trace.output)
		}
	}
	if err != nil {
		trace.dumpError = err.Error()
		_ = os.WriteFile(trace.output+".error", []byte(trace.dumpError+"\n"), 0o644)
	}
}
