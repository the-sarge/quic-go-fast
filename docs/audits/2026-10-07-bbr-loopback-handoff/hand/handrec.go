package quic

import (
	"encoding/json"
	"os"
	"sync/atomic"
	"time"
	_ "unsafe" // go:linkname
)

// Measurement-only #738 hand-off recorder: aggregates only, in the fixture's
// sender only, over the measured window. It extends #734's loopback recorder
// (`cand-lb-timeline`, whose loopback rule read only its aggregates): the same
// window aggregates (time in each stop class between opportunities, time
// inside opportunities, paced stops, lateness and the pacer credit discarded
// during it), with #734's local-wait class split into a local credit refusal
// and a full send queue, plus the two goroutines that the local send credit
// couples. Counts are exact; durations are timed on a systematic sample (one
// exit, opportunity, blocked interval and local wait in eight), so that the
// recorder stays off most hand-offs on a path whose stages exchange every
// packet. Lateness uses each opportunity's own now and is exact.
//
//   - Connection loop: its blocked intervals in the run-loop select, by the
//     reason the last stop gave (all counted; one in eight timed, with the
//     worker's busy time inside it).
//   - Send worker: a busy/idle clock (busy from the first dequeue after an
//     empty queue until the queue is empty again), and, for one group in eight,
//     dequeue, submission start, submission end and credit completion.
//   - Each local wait (an opportunity refused by credit, or stopped by a full
//     queue): its start, the first credit completion or queue slot freed after
//     it, the first signal after it, the connection's wake and the next
//     opportunity, and whether that opportunity reserved and handed off.
//   - Pending local bytes: the maximum, and admissions that took pending above
//     2Q (the 4Q diagnostic arm's engagement check).
//
// build.py copies this file into the exported fixture tree and rewrites
// handBoundQ for the 4Q diagnostic arm. Not a maintained hook.

//go:linkname handNanotime runtime.nanotime
func handNanotime() int64

// handBoundQ is the pending bound in quanta that this build passes to
// localSendCredit; build.py rewrites it to 4 for the diagnostic arm.
const handBoundQ = 2

// handParts enables recorder parts (development only; every registered
// variant enables all): 1 opportunity aggregates, 2 blocked intervals,
// 4 send worker, 8 credit admissions and completions.
const handParts = 15

// handMask selects the systematic sample of timed exits, opportunities and
// blocked intervals (seq&handMask == 0: one in eight); 0 times all
// (development only).
const handMask = 7

// Stop classes (#734's, with its "credit" class split in two), which are also
// the connection loop's blocked reasons. hcRunning is an immediate
// continuation: progress with no stop.
const (
	hcRunning = iota
	hcCredit  // a local credit refusal (the result waits on the credit's channel)
	hcQueue   // the send queue was full
	hcCwnd
	hcPaced
	hcApp
	hcHard
	hcOther
	hcStates
)

var hcNames = []string{"immediate", "credit", "queue", "cwnd", "paced", "app", "hard", "other"}

// handWorkerClock is the send worker's busy clock, written only by the worker
// and read by the connection loop and the recorder under a sequence lock.
type handWorkerClock struct {
	seq  atomic.Uint64
	cum  atomic.Int64 // busy nanoseconds before last
	last atomic.Int64 // time of the last transition
	busy atomic.Bool
}

func (w *handWorkerClock) transition(t int64, busy bool) {
	s := w.seq.Load()
	w.seq.Store(s + 1)
	if w.busy.Load() {
		if d := t - w.last.Load(); d > 0 {
			w.cum.Store(w.cum.Load() + d)
		}
	}
	w.last.Store(t)
	w.busy.Store(busy)
	w.seq.Store(s + 2)
}

// read returns cumulative busy time at t (t at or after the last transition).
func (w *handWorkerClock) read(t int64) int64 {
	for {
		s := w.seq.Load()
		if s&1 != 0 {
			continue
		}
		cum, last, busy := w.cum.Load(), w.last.Load(), w.busy.Load()
		if w.seq.Load() == s {
			if busy && t > last {
				cum += t - last
			}
			return cum
		}
	}
}

// handWaitAgg sums the local waits of one kind. Every wait is counted and
// timed from its start to the next opportunity; one wait in eight (the sampled
// waits) is split at its block and wake, and published to the send worker,
// which stamps the first credit completion (credit) or slot freed by a dequeue
// (queue) after the wait began, and the first signal on the channel the loop
// waits on.
type handWaitAgg struct {
	Waits        int64 `json:"waits"`         // local waits that ended in an opportunity inside the window
	TotalNS      int64 `json:"total_ns"`      // wait start -> next opportunity
	NoBlock      int64 `json:"no_block"`      // reached the next opportunity without blocking
	Resumed      int64 `json:"resumed"`       // the next opportunity handed off
	RefusedAgain int64 `json:"refused_again"` // the next opportunity stopped on a local wait again without progress
	// Sampled waits only (one wait in eight, published to the worker):
	PreBlockNS int64    `json:"pre_block_ns"` // wait start -> first block in the run-loop select (loop work)
	BlockedNS  int64    `json:"blocked_ns"`   // first block -> last wake (re-blocks included)
	ResumeNS   int64    `json:"resume_ns"`    // last wake (or wait start, if it never blocked) -> next opportunity
	WakeCases  [6]int64 `json:"wake_cases"`   // last wake's select case (1 timer, 2 scheduled, 3 handshake, 4 local, 5 packet)

	Sampled          int64 `json:"sampled"`
	SampledTotalNS   int64 `json:"sampled_total_ns"`
	SampledBlocked   int64 `json:"sampled_blocked"`    // sampled waits that blocked
	SampledEarlyWake int64 `json:"sampled_early_wake"` // ... and whose last wake came before any signal after the wait began
	SampledRewakes   int64 `json:"sampled_rewakes"`    // wakes that found the wait unfinished and blocked again
	SampledReleaseNS int64 `json:"sampled_release_ns"` // wait start -> first completion or freed slot
	SampledSignalNS  int64 `json:"sampled_signal_ns"`  // that completion -> first signal
	SampledWakeNS    int64 `json:"sampled_wake_ns"`    // signal (or block, if later) -> wake
}

var hand struct {
	on         atomic.Bool
	finished   atomic.Bool
	start, end int64
	busy0      int64
	prefix     string
	startUnix  int64
	endUnix    int64
	_          [64]byte
	worker     handWorkerClock
	_          [64]byte
	// The local wait in progress, shared with the worker: its start time with
	// the kind in the low bit (1 credit, 0 queue), or 0 when none; and the
	// first completion and signal after it (stale when not later than it).
	waitAt   atomic.Int64
	_        [64]byte
	firstRel atomic.Int64
	firstSig atomic.Int64
	_        [64]byte
}

// handC is written only by the connection goroutine.
var handC struct {
	_             [64]byte
	exitT, entryT int64 // stamps of a sampled exit and opportunity, else 0
	exitClass     int
	oppSeq        int64
	exitSeq       int64
	stateNS       [hcStates]int64 // sampled exits: exit -> next opportunity, by the exit's class (#734's state_ns)
	stateN        [hcStates]int64
	emitNS        int64 // sampled opportunities: time inside them
	emitN         int64
	opps          int64
	stops         [hcStates]int64
	progressOps   int64
	paced         int64 // paced deadline pending (#734's)
	rate          int64
	lateN, lateNS int64
	lateBytes     float64
	reason        int
	blocking      bool  // blocked in the run-loop select
	blockT        int64 // block time of a sampled blocked interval, else 0
	blockBusy     int64
	blockSeq      int64
	blocks        [hcStates]int64 // blocked intervals, by reason (all)
	sampledBlocks [hcStates]int64 // the sampled ones: one in eight, plus those of a sampled local wait
	sampledNS     [hcStates]int64 // their blocked time
	overlapNS     [hcStates]int64 // the worker's busy time inside them
	entries       int64
	entryBytes    int64
	entryPkts     int64
	// The local wait being decomposed.
	waitKind      int // hcCredit, hcQueue or 0
	lastKind      int
	waitSeq       int64
	waitSampled   bool
	pendingResume bool
	waitX         int64
	waitBlocked   bool
	waitB         int64 // block time of a sampled wait, 0 if not blocked
	waitW         int64 // first wake, 0 if none
	waitCase      int
	waitRewakes   int64
	waitSig       int64
	waitRel       int64
	wait          [hcStates]handWaitAgg
	// Pending bytes (reserve is connection-owned, under the credit lock).
	maxPending       int64
	maxRatioPending  int64 // the admission with the largest pending ÷ limit
	maxRatioLimit    int64
	admissions       int64
	admissionsOver2Q int64
	refusals         int64
	refusalPendSum   int64
	_                [64]byte
}

// handW is written only by the send worker.
var handW struct {
	_          [64]byte
	wCount     int64
	wSampling  bool
	wDeq       int64
	wSub       int64
	wSubEnd    int64
	wDeqToSub  int64
	wSubmit    int64
	wSubToDone int64
	wGroups    int64
	wIdleTrans int64
	_          [64]byte
}

func handIn(t int64) bool { return t >= hand.start && t < hand.end }

// --- connection loop ---

// handSetReason records the run loop's own full-queue check as the reason it
// will block.
func handSetReason(r int) {
	if handParts&2 == 0 {
		return
	}
	if hand.on.Load() {
		handC.reason = r
	}
}

func handBlocked() {
	if handParts&2 == 0 {
		return
	}
	if !hand.on.Load() {
		return
	}
	handC.blocking = true
	handC.blockSeq++
	inWait := handC.waitKind != 0
	first := inWait && !handC.waitBlocked
	if first {
		handC.waitBlocked = true
	}
	// Every blocked interval of a sampled wait is timed, so its last wake is known.
	if handC.blockSeq&handMask != 0 && !(inWait && handC.waitSampled) {
		handC.blockT = 0
		return
	}
	t := handNanotime()
	handC.blockT = t
	handC.blockBusy = hand.worker.read(t)
	if first && handC.waitSampled {
		handC.waitB = t
	}
}

func handWoken(k int) {
	if handParts&2 == 0 {
		return
	}
	if !hand.on.Load() || !handC.blocking {
		return
	}
	handC.blocking = false
	r := handC.reason
	if r == hcRunning {
		r = hcOther // blocked after an immediate continuation: no recorded stop
	}
	handC.blocks[r]++
	b := handC.blockT
	if b == 0 {
		return
	}
	handC.blockT = 0
	t := handNanotime()
	if b >= hand.start && t < hand.end {
		handC.sampledBlocks[r]++
		handC.sampledNS[r] += t - b
		if busy := hand.worker.read(t) - handC.blockBusy; busy > 0 {
			handC.overlapNS[r] += min(busy, t-b)
		}
	}
	// A queue wait can wake on a token sent before it began, find the queue
	// still full and block again: decompose at the last wake before the next
	// opportunity, and count the earlier ones.
	if handC.waitKind != 0 && handC.waitSampled && handC.waitB != 0 {
		if handC.waitW != 0 {
			handC.waitRewakes++
		}
		handC.waitW, handC.waitCase = t, k
		handC.waitRel, handC.waitSig = hand.firstRel.Load(), hand.firstSig.Load()
		if handC.waitRel <= handC.waitX {
			handC.waitRel = 0 // stale: from an earlier wait
		}
		if handC.waitSig <= handC.waitX {
			handC.waitSig = 0
		}
	}
}

// handOpportunity runs at every BBR opportunity with its now.
func handOpportunity(now int64) {
	if handParts&1 == 0 {
		return
	}
	if !hand.on.Load() {
		return
	}
	handC.oppSeq++
	waitSampled := handC.waitKind != 0 && handC.waitSampled
	var t int64
	if handC.exitT != 0 || handC.oppSeq&handMask == 0 || waitSampled {
		t = handNanotime()
		if t >= hand.end {
			handFinish()
			return
		}
	}
	handC.opps++
	if handC.exitT != 0 {
		if handC.exitT >= hand.start {
			handC.stateNS[handC.exitClass] += t - handC.exitT
			handC.stateN[handC.exitClass]++
		}
		handC.exitT = 0
	}
	handC.entryT = 0
	if handC.oppSeq&handMask == 0 {
		handC.entryT = t
	}
	// Lateness after a paced stop uses the opportunity's own now: exact, every opportunity.
	if handC.paced != 0 && now >= handC.paced {
		late := now - handC.paced
		handC.lateN++
		handC.lateNS += late
		handC.lateBytes += float64(late) * float64(handC.rate) / 1e9
		handC.paced = 0
	}
	if handC.waitKind == 0 {
		return
	}
	k, x := handC.waitKind, handC.waitX
	handC.waitKind = 0
	a := &handC.wait[k]
	a.Waits++
	handC.pendingResume = true
	if !handC.waitBlocked {
		a.NoBlock++
	}
	if !waitSampled {
		return
	}
	hand.waitAt.Store(0)
	if !handIn(x) {
		return
	}
	a.Sampled++
	a.SampledTotalNS += t - x
	if !handC.waitBlocked || handC.waitB == 0 {
		a.ResumeNS += t - x
		return
	}
	a.SampledBlocked++
	a.SampledRewakes += handC.waitRewakes
	w := handC.waitW
	if w == 0 {
		w = t
	}
	a.PreBlockNS += handC.waitB - x
	a.BlockedNS += w - handC.waitB
	a.ResumeNS += t - w
	a.WakeCases[min(handC.waitCase, 5)]++
	rel, sig := handC.waitRel, handC.waitSig
	if sig == 0 || sig > w {
		a.SampledEarlyWake++
		return
	}
	if rel == 0 || rel > sig {
		rel = sig
	}
	a.SampledReleaseNS += max(0, rel-x)
	a.SampledSignalNS += sig - max(rel, x)
	a.SampledWakeNS += w - max(sig, handC.waitB)
}

// handExited records an opportunity's stop class; credit and queue stops start
// a local wait. deadline is the paced deadline (0 if none), rate the pacing rate.
func handExited(class int, progress bool, deadline, rate int64) {
	if handParts&1 == 0 {
		return
	}
	if !hand.on.Load() {
		return
	}
	handC.exitSeq++
	waitStart := class == hcCredit || class == hcQueue
	sampledWait := false
	if waitStart {
		handC.waitSeq++
		sampledWait = handC.waitSeq&7 == 0
	}
	sampledExit := handC.exitSeq&handMask == 0
	var t int64
	if handC.entryT != 0 || sampledExit || sampledWait {
		t = handNanotime()
		if t >= hand.end {
			handFinish()
			return
		}
	}
	handC.stops[class]++
	if progress {
		handC.progressOps++
	}
	if handC.entryT != 0 {
		if handC.entryT >= hand.start {
			handC.emitNS += t - handC.entryT
			handC.emitN++
		}
		handC.entryT = 0
	}
	handC.exitT, handC.exitClass = 0, class
	if sampledExit {
		handC.exitT = t
	}
	if deadline != 0 {
		handC.paced, handC.rate = deadline, rate
	}
	if handC.pendingResume {
		handC.pendingResume = false
		a := &handC.wait[handC.lastKind]
		if progress {
			a.Resumed++
		} else if waitStart {
			a.RefusedAgain++
		}
	}
	handC.reason = class
	if waitStart {
		x := t &^ 1
		if class == hcCredit {
			x |= 1
		}
		handC.waitSampled = sampledWait
		if sampledWait {
			hand.waitAt.Store(x)
		}
		handC.waitKind, handC.lastKind, handC.waitX = class, class, x
		handC.waitBlocked, handC.waitB, handC.waitW, handC.waitCase, handC.waitRel, handC.waitSig = false, 0, 0, 0, 0, 0
		handC.waitRewakes = 0
	}
}

func handEntry(bytes, gso int64) {
	if handParts&1 == 0 {
		return
	}
	if !hand.on.Load() {
		return
	}
	handC.entries++
	handC.entryBytes += bytes
	if gso > 0 {
		handC.entryPkts += (bytes + gso - 1) / gso
	} else {
		handC.entryPkts++
	}
}

// handPending runs under the credit lock after an admission; integer work only.
func handPending(pending, limit int64) {
	if handParts&8 == 0 {
		return
	}
	if !hand.on.Load() {
		return
	}
	handC.admissions++
	if pending > handC.maxPending {
		handC.maxPending = pending
	}
	if pending*handBoundQ > 2*limit {
		handC.admissionsOver2Q++
	}
	if pending*handC.maxRatioLimit >= handC.maxRatioPending*limit {
		handC.maxRatioPending, handC.maxRatioLimit = pending, limit
	}
}

// handRefused runs under the credit lock after a refused reservation.
func handRefused(pending int64) {
	if handParts&8 == 0 {
		return
	}
	if !hand.on.Load() {
		return
	}
	handC.refusals++
	handC.refusalPendSum += pending
}

// --- send worker ---

func handWorkerIdle(queued int) {
	if handParts&4 == 0 {
		return
	}
	if !hand.on.Load() || queued != 0 {
		return
	}
	hand.worker.transition(handNanotime(), false)
	handW.wIdleTrans++
}

func handWorkerDequeued() {
	if handParts&4 == 0 {
		return
	}
	if !hand.on.Load() {
		return
	}
	handW.wCount++
	sample := handW.wCount&7 == 0
	idle := !hand.worker.busy.Load()
	w := hand.waitAt.Load()
	queueWait := w != 0 && w&1 == 0
	var t int64
	if idle || sample || queueWait {
		t = handNanotime()
	}
	if idle {
		hand.worker.transition(t, true)
	}
	if queueWait {
		handFirst(&hand.firstRel, w, t)
	}
	handW.wSampling = sample
	if sample {
		handW.wDeq = t
	}
}

func handSubmit() {
	if handParts&4 == 0 {
		return
	}
	if handW.wSampling {
		handW.wSub = handNanotime()
	}
}

func handSubmitted() {
	if handParts&4 == 0 {
		return
	}
	if handW.wSampling {
		handW.wSubEnd = handNanotime()
	}
}

// handCompleted runs on the send worker after a queued entry returned its
// credit (outside the credit lock).
func handCompleted() {
	if handParts&8 == 0 {
		return
	}
	if !hand.on.Load() {
		return
	}
	sampled := handW.wSampling && handW.wSubEnd != 0
	w := hand.waitAt.Load()
	creditWait := w&1 == 1
	if !sampled && !creditWait {
		return
	}
	t := handNanotime()
	if creditWait {
		handFirst(&hand.firstRel, w, t)
	}
	if sampled {
		handW.wSampling = false
		if handIn(handW.wDeq) && handIn(t) {
			handW.wGroups++
			handW.wDeqToSub += handW.wSub - handW.wDeq
			handW.wSubmit += handW.wSubEnd - handW.wSub
			handW.wSubToDone += t - handW.wSubEnd
		}
		handW.wSubEnd = 0
	}
}

// handFirst records t as the first event after the wait that started at x,
// unless one is already recorded for it.
func handFirst(a *atomic.Int64, x, t int64) {
	for t > x {
		old := a.Load()
		if old > x || a.CompareAndSwap(old, t) {
			return
		}
	}
}

// handSignaled runs when a signal reaches the channel the loop waits on. The
// wait's signal is the first one after its first freed capacity: a credit
// completion and its signal are one event, while a queue signal counts only
// after a dequeue has freed a slot (an earlier one announces no space).
func handSignaled(credit bool) {
	if handParts&4 == 0 {
		return
	}
	w := hand.waitAt.Load()
	if w == 0 || (w&1 == 1) != credit || !hand.on.Load() {
		return
	}
	t := handNanotime()
	if credit {
		handFirst(&hand.firstRel, w, t)
	} else if hand.firstRel.Load() <= w {
		return
	}
	handFirst(&hand.firstSig, w, t)
}

func handCreditSignaled() { handSignaled(true) }
func handQueueSignaled()  { handSignaled(false) }

// --- window ---

// handStart opens the window. The first connection hook after the window
// writes the aggregates (the sender may exit soon after its measured window),
// as #734's recorder hands off; a fallback writes them if no hook runs.
func handStart(prefix string, startUnix, endUnix int64) {
	hand.prefix, hand.startUnix, hand.endUnix = prefix, startUnix, endUnix
	go func() {
		time.Sleep(time.Until(time.Unix(0, startUnix)))
		t0, w0 := handNanotime(), time.Now().UnixNano()
		hand.start, hand.end = t0, t0+(endUnix-w0)
		hand.busy0 = hand.worker.read(t0)
		hand.on.Store(true)
		time.Sleep(time.Until(time.Unix(0, endUnix)) + 50*time.Millisecond)
		handFinish()
	}()
}

func handFinish() {
	if !hand.finished.CompareAndSwap(false, true) {
		return
	}
	hand.on.Store(false)
	busy1 := hand.worker.read(hand.end)
	w := hand.end - hand.start
	named := func(a [hcStates]int64, from int) map[string]int64 {
		m := map[string]int64{}
		for i := from; i < hcStates; i++ {
			m[hcNames[i]] = a[i]
		}
		return m
	}
	var maxQ float64
	if handC.maxRatioLimit > 0 {
		maxQ = float64(handC.maxRatioPending*handBoundQ) / float64(handC.maxRatioLimit)
	}
	meta := map[string]any{
		"start_unix_ns": hand.startUnix, "end_unix_ns": hand.endUnix, "window_ns": w, "bound_q": handBoundQ, "parts": handParts,
		"state_sampled_ns": named(handC.stateNS, 0), "state_samples": named(handC.stateN, 0),
		"emit_sampled_ns": handC.emitNS, "emit_samples": handC.emitN, "opportunities": handC.opps, "stops": named(handC.stops, 0),
		"progress_opportunities": handC.progressOps, "late_count": handC.lateN, "late_ns": handC.lateNS, "late_bytes": handC.lateBytes,
		"worker_busy_ns": busy1 - hand.busy0, "worker_idle_transitions": handW.wIdleTrans, "worker_dequeues": handW.wCount,
		"blocks": named(handC.blocks, 1), "sampled_blocks": named(handC.sampledBlocks, 1),
		"sampled_blocked_ns": named(handC.sampledNS, 1), "worker_busy_in_sampled_blocked_ns": named(handC.overlapNS, 1),
		"entries": handC.entries, "entry_bytes": handC.entryBytes, "entry_packets": handC.entryPkts,
		"waits":             map[string]handWaitAgg{"credit": handC.wait[hcCredit], "queue": handC.wait[hcQueue]},
		"max_pending_bytes": handC.maxPending, "max_pending_q": maxQ,
		"admissions": handC.admissions, "admissions_over_2q": handC.admissionsOver2Q,
		"refusals": handC.refusals, "refusal_pending_sum": handC.refusalPendSum,
		"sampled_groups": handW.wGroups, "sampled_dequeue_to_submit_ns": handW.wDeqToSub,
		"sampled_submit_ns": handW.wSubmit, "sampled_submit_to_completion_ns": handW.wSubToDone,
	}
	if b, err := json.Marshal(meta); err == nil {
		os.WriteFile(hand.prefix+".json", b, 0o644)
	}
}
