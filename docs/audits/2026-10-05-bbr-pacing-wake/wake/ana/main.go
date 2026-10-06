// Wake-path analysis for #734: joins the recorder's events (wakerec.go) with the
// Go execution trace of the same window and splits every late opportunity's
// interval [deadline, opportunity] by what the recorded goroutine was doing. A
// finite measurement aid, not a maintained tool.
//
// The recorded goroutine is the one that blocks in a select whose stack holds
// -loop. Its state intervals come from the trace. A late event follows the #715
// timeline overlay exactly: a paced exit makes its deadline pending, a later
// exit replaces or clears it, and the first opportunity whose now is at or
// after the pending deadline is late by now − deadline. Each part of a late
// interval is:
//
//	delivery     blocked in the run-loop select while the timer was armed at or
//	             before the deadline (the runtime had not yet woken the
//	             goroutine); split by what ended the wait: the timer (an unblock
//	             from scheduler context, or from the runtime's timer code) or
//	             another wake (packet, send queue, scheduled sending, ...);
//	unarmed      blocked in the select while the timer was armed after the
//	             deadline (nothing was due to wake the goroutine by then);
//	scheduling   runnable, not running;
//	loop         running or in a syscall (loop_running), or blocked anywhere
//	             other than the run-loop select (loop_blocked);
//	ambiguous    no trace state known.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"unsafe"

	"golang.org/x/exp/trace"
)

type event struct{ T, K, A, B, C, D int64 }

const (
	wkArm = 1 + iota
	wkWoke
	wkOpp
	wkExit
)

type meta struct {
	StartNano   int64  `json:"start_nanotime"`
	EndNano     int64  `json:"end_nanotime"`
	MonoOffset  int64  `json:"mono_offset_ns"`
	MonoWidth   int64  `json:"mono_offset_width_ns"`
	Events      int    `json:"events"`
	Handoff     string `json:"handoff"`
	Trace       bool   `json:"trace"`
	TraceError  string `json:"trace_error"`
	EventsError string `json:"events_error"`
}

type interval struct {
	t0, t1     int64
	state      trace.GoState
	selectWait bool
	endedBy    string // for a wait: the unblock class of the transition that ended it
}

type segs struct {
	DeliveryTimer float64            `json:"delivery_timer"`
	DeliveryOther float64            `json:"delivery_other"`
	Unarmed       float64            `json:"unarmed"`
	Scheduling    float64            `json:"scheduling"`
	LoopRunning   float64            `json:"loop_running"`
	LoopBlocked   float64            `json:"loop_blocked"`
	Ambiguous     float64            `json:"ambiguous"`
	OtherBy       map[string]float64 `json:"delivery_other_by"`
}

func (s *segs) add(o segs) {
	s.DeliveryTimer += o.DeliveryTimer
	s.DeliveryOther += o.DeliveryOther
	s.Unarmed += o.Unarmed
	s.Scheduling += o.Scheduling
	s.LoopRunning += o.LoopRunning
	s.LoopBlocked += o.LoopBlocked
	s.Ambiguous += o.Ambiguous
	for k, v := range o.OtherBy {
		if s.OtherBy == nil {
			s.OtherBy = map[string]float64{}
		}
		s.OtherBy[k] += v
	}
}

type lateEvent struct {
	DMono      int64  `json:"d_mono"`
	OMono      int64  `json:"o_mono"`
	LateNS     int64  `json:"late_ns"`
	Arms       int    `json:"arms"`
	Folded     int    `json:"folded"`
	Superseded int    `json:"superseded"`
	Segs       segs   `json:"segs"`
	EndedBy    string `json:"ended_by"`
	Leading    string `json:"leading"`
}

type stat struct {
	N      int     `json:"n"`
	Mean   float64 `json:"mean_ns"`
	Median float64 `json:"median_ns"`
	P90    float64 `json:"p90_ns"`
	P99    float64 `json:"p99_ns"`
	Max    float64 `json:"max_ns"`
	Over50 float64 `json:"share_over_50us"`
	Over1m float64 `json:"share_over_1ms"`
}

func stats(v []float64) stat {
	if len(v) == 0 {
		return stat{}
	}
	sort.Float64s(v)
	q := func(p float64) float64 { return v[min(len(v)-1, int(math.Ceil(p*float64(len(v))))-1)] }
	s := stat{N: len(v), Median: q(.5), P90: q(.9), P99: q(.99), Max: v[len(v)-1]}
	for _, x := range v {
		s.Mean += x
		if x > 50e3 {
			s.Over50++
		}
		if x > 1e6 {
			s.Over1m++
		}
	}
	s.Mean /= float64(len(v))
	s.Over50 /= float64(len(v))
	s.Over1m /= float64(len(v))
	return s
}

func stackHas(st trace.Stack, fn string) bool {
	for f := range st.Frames() {
		if f.Func == fn {
			return true
		}
	}
	return false
}

func frames(st trace.Stack, n int) []string {
	var out []string
	for f := range st.Frames() {
		out = append(out, f.Func)
		if len(out) == n {
			break
		}
	}
	return out
}

// classify names what unblocked the recorded goroutine.
func classify(e trace.Event) (string, []string) {
	if e.Goroutine() == trace.NoGoroutine {
		return "timer", nil
	}
	fs := frames(e.Stack(), 32)
	j := strings.Join(fs, " ")
	switch {
	case strings.Contains(j, "runtime.sendTime") || strings.Contains(j, "(*timers).run") || strings.Contains(j, "unlockAndRun"):
		return "timer", nil
	case strings.Contains(j, "handlePacket") || strings.Contains(j, "main.readLoop"):
		return "packet", nil
	case strings.Contains(j, "scheduleSending"):
		return "scheduled", nil
	case strings.Contains(j, "sendQueue") || strings.Contains(j, "redit"):
		return "sendq", nil
	case strings.Contains(j, "andshake"):
		return "handshake", nil
	}
	return "other", fs[:min(len(fs), 6)]
}

func main() {
	tracePath := flag.String("trace", "", "Go execution trace (empty for a trace-off run)")
	eventsPath := flag.String("events", "", "recorder events")
	metaPath := flag.String("meta", "", "recorder meta JSON")
	loopFn := flag.String("loop", "github.com/quic-go/quic-go.(*Conn).run", "function whose select is the run-loop wait")
	outPath := flag.String("out", "", "output JSON")
	withLate := flag.Bool("late", false, "include every late event")
	flag.Parse()
	out := map[string]any{}
	defer func() {
		b, _ := json.MarshalIndent(out, "", " ")
		if *outPath == "" {
			os.Stdout.Write(append(b, '\n'))
		} else {
			os.WriteFile(*outPath, append(b, '\n'), 0o644)
		}
	}()

	var m meta
	b, err := os.ReadFile(*metaPath)
	if err == nil {
		err = json.Unmarshal(b, &m)
	}
	if err != nil || m.Handoff != "window end" || m.EventsError != "" {
		out["error"] = "recorder incomplete"
		out["meta"] = m
		return
	}
	out["meta"] = m
	raw, err := os.ReadFile(*eventsPath)
	if err != nil {
		out["error"] = err.Error()
		return
	}
	sz := int(unsafe.Sizeof(event{}))
	evs := unsafe.Slice((*event)(unsafe.Pointer(unsafe.SliceData(raw))), len(raw)/sz)
	if len(evs) != m.Events {
		out["error"] = "event count mismatch"
		return
	}
	toNano := func(mono int64) int64 { return mono + m.MonoOffset }

	// ---- trace ----
	var ivs []interval
	traceOff := int64(0)
	haveTrace := *tracePath != "" && m.Trace && m.TraceError == ""
	unblocks := map[string]int{}
	otherFrames := map[string]int{}
	if haveTrace {
		f, err := os.Open(*tracePath)
		if err != nil {
			out["error"] = err.Error()
			return
		}
		r, err := trace.NewReader(f)
		if err != nil {
			out["error"] = err.Error()
			return
		}
		conn := trace.NoGoroutine
		var snaps []int64
		var first, last int64
		var cur *interval
		var parseErr error
		for {
			e, err := r.ReadEvent()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				parseErr = err
				break
			}
			t := int64(e.Time())
			if first == 0 {
				first = t
			}
			last = t
			switch e.Kind() {
			case trace.EventSync:
				if cs := e.Sync().ClockSnapshot; cs != nil {
					snaps = append(snaps, int64(cs.Trace)-int64(cs.Mono))
				}
			case trace.EventStateTransition:
				st := e.StateTransition()
				if st.Resource.Kind != trace.ResourceGoroutine {
					continue
				}
				g := st.Resource.Goroutine()
				from, to := st.Goroutine()
				sel := to == trace.GoWaiting && strings.HasPrefix(st.Reason, "select") &&
					(stackHas(st.Stack, *loopFn) || stackHas(e.Stack(), *loopFn))
				if conn == trace.NoGoroutine {
					if !sel {
						continue
					}
					conn = g
				}
				if g != conn {
					continue
				}
				if cur != nil {
					cur.t1 = t
					if from == trace.GoWaiting && to == trace.GoRunnable {
						cls, fs := classify(e)
						cur.endedBy = cls
						unblocks[cls]++
						if fs != nil {
							otherFrames[strings.Join(fs, " < ")]++
						}
					}
					ivs = append(ivs, *cur)
				}
				cur = &interval{t0: t, state: to, selectWait: sel}
			}
		}
		if cur != nil {
			cur.t1 = math.MaxInt64
			ivs = append(ivs, *cur)
		}
		f.Close()
		if parseErr != nil {
			out["trace_error"] = parseErr.Error()
		}
		if len(snaps) == 0 {
			out["error"] = "no clock snapshot"
			return
		}
		sort.Slice(snaps, func(i, j int) bool { return snaps[i] < snaps[j] })
		traceOff = snaps[len(snaps)/2]
		out["trace"] = map[string]any{"first_ns": first, "last_ns": last, "snapshots": len(snaps),
			"snapshot_offset_spread_ns": snaps[len(snaps)-1] - snaps[0], "conn_goroutine": int64(conn), "intervals": len(ivs),
			"unblocks_all": unblocks, "other_unblock_frames": otherFrames}
	}
	tt := func(nano int64) int64 { return nano + traceOff } // nanotime → trace time
	w0, w1 := tt(m.StartNano), tt(m.EndNano)
	out["window_trace_ns"] = []int64{w0, w1}
	if haveTrace {
		tr := out["trace"].(map[string]any)
		tr["covers_window"] = len(ivs) > 0 && tr["first_ns"].(int64) <= w0 && tr["last_ns"].(int64) >= w1 && ivs[0].t0 <= w0
	}
	at := func(t int64) int { // index of the interval holding t, or -1
		i := sort.Search(len(ivs), func(i int) bool { return ivs[i].t1 > t })
		if i < len(ivs) && ivs[i].t0 <= t {
			return i
		}
		return -1
	}

	// ---- recorder events: arms and exits for lookup ----
	type arm struct{ t, armed, pacing, mode int64 }
	type exit struct{ t, deadline, stop, class int64 }
	var arms []arm
	var exits []exit
	woke := map[int64]int{}
	var violations, checked int
	for _, e := range evs {
		t := tt(e.T)
		switch e.K {
		case wkArm:
			arms = append(arms, arm{t, e.B, e.C, e.D})
		case wkExit:
			exits = append(exits, exit{t, e.A, e.B, e.C})
		case wkWoke:
			woke[e.A]++
		}
		if haveTrace && (e.K == wkOpp || e.K == wkWoke) {
			checked++
			if i := at(t); i < 0 || (ivs[i].state != trace.GoRunning && ivs[i].state != trace.GoSyscall) {
				violations++
			}
		}
	}
	armAt := func(t int64) *arm {
		i := sort.Search(len(arms), func(i int) bool { return arms[i].t > t }) - 1
		if i < 0 {
			return nil
		}
		return &arms[i]
	}
	exitAt := func(t int64) *exit {
		i := sort.Search(len(exits), func(i int) bool { return exits[i].t > t }) - 1
		if i < 0 {
			return nil
		}
		return &exits[i]
	}

	decompose := func(D, O int64) (segs, string) {
		var s segs
		ended := ""
		if !haveTrace {
			return s, ended
		}
		d, o := tt(toNano(D)), tt(toNano(O))
		t := d
		if i := at(d); i < 0 {
			end := o
			if len(ivs) > 0 && ivs[0].t0 > d {
				end = min(o, ivs[0].t0)
			}
			s.Ambiguous += float64(end - d)
			t = end
		}
		for t < o {
			i := at(t)
			if i < 0 {
				s.Ambiguous += float64(o - t)
				break
			}
			iv := ivs[i]
			e := min(o, iv.t1)
			dur := float64(e - t)
			switch iv.state {
			case trace.GoRunning, trace.GoSyscall:
				s.LoopRunning += dur
			case trace.GoRunnable:
				s.Scheduling += dur
			case trace.GoWaiting:
				if !iv.selectWait {
					s.LoopBlocked += dur
					break
				}
				a := armAt(iv.t0)
				switch {
				case a == nil:
					s.Ambiguous += dur
				case a.armed > D:
					s.Unarmed += dur
				case iv.endedBy == "timer":
					s.DeliveryTimer += dur
				default:
					s.DeliveryOther += dur
					if s.OtherBy == nil {
						s.OtherBy = map[string]float64{}
					}
					s.OtherBy[iv.endedBy] += dur
				}
				if ended == "" && iv.t1 <= o {
					ended = iv.endedBy
				}
			default:
				s.Ambiguous += dur
			}
			t = e
		}
		return s, ended
	}
	leading := func(s segs, total float64) string {
		parts := map[string]float64{"delivery": s.DeliveryTimer + s.DeliveryOther, "unarmed": s.Unarmed,
			"scheduling": s.Scheduling, "loop": s.LoopRunning + s.LoopBlocked, "ambiguous": s.Ambiguous}
		best, bv := "", -1.0
		for k, v := range parts {
			if v > bv {
				best, bv = k, v
			}
		}
		if total <= 0 {
			return ""
		}
		return best
	}

	// ---- late events, the #715 overlay's pending rule ----
	var lates []lateEvent
	var total segs
	var lateSum, lateMax float64
	leadCount := map[string]int{}
	pending := int64(0)
	var c lateEvent
	cleared, pacedStops := 0, 0
	inWindow := func(nano int64) bool { return nano >= m.StartNano && nano < m.EndNano }
	for _, e := range evs {
		switch e.K {
		case wkArm:
			c.Arms++
			if e.C != 0 && e.B < e.C {
				c.Folded++
			}
		case wkExit:
			d := e.A
			if d != 0 {
				pacedStops++
			}
			switch {
			case pending == 0:
				c = lateEvent{}
			case d == 0:
				cleared++
				c = lateEvent{}
			case d != pending:
				c.Superseded++
			}
			pending = d
		case wkOpp:
			now := e.A
			if pending != 0 && now >= pending {
				c.DMono, c.OMono, c.LateNS = pending, now, now-pending
				if inWindow(toNano(pending)) && inWindow(toNano(now)) {
					c.Segs, c.EndedBy = decompose(pending, now)
					c.Leading = leading(c.Segs, float64(c.LateNS))
					total.add(c.Segs)
					lateSum += float64(c.LateNS)
					lateMax = max(lateMax, float64(c.LateNS))
					leadCount[c.Leading]++
					lates = append(lates, c)
				}
				pending, c = 0, lateEvent{}
			}
		}
	}

	// ---- timer delivery lag and run-loop wakes inside the window ----
	var lagPacing, lagOther []float64
	pacingTimerWakes := 0
	wakes := map[string]int{}
	share := map[string]float64{}
	if haveTrace {
		for i, iv := range ivs {
			if iv.t1 <= w0 || iv.t0 >= w1 {
				continue
			}
			lo, hi := max(iv.t0, w0), min(iv.t1, w1)
			dur := float64(hi - lo)
			switch iv.state {
			case trace.GoRunning, trace.GoSyscall:
				share["running"] += dur
			case trace.GoRunnable:
				share["runnable"] += dur
			case trace.GoWaiting:
				if !iv.selectWait {
					share["waiting_other"] += dur
					break
				}
				a, x := armAt(iv.t0), exitAt(iv.t0)
				cls := "select_other"
				if x != nil {
					switch {
					case x.deadline != 0:
						cls = "select_paced"
					default:
						cls = "select_" + []string{"immediate", "app", "paced", "cwnd", "credit", "hard", "other"}[min(max(x.class, 0), 6)]
					}
				}
				if cls == "select_paced" && a != nil && a.armed > x.deadline {
					cls = "select_paced_unarmed"
				}
				share[cls] += dur
			default:
				share["unknown"] += dur
			}
			if iv.state == trace.GoWaiting && iv.selectWait && iv.t1 >= w0 && iv.t1 < w1 && i+1 < len(ivs) {
				wakes[iv.endedBy]++
				if iv.endedBy == "timer" {
					if a := armAt(iv.t0); a != nil {
						lag := float64(iv.t1 - tt(toNano(a.armed)))
						if a.mode == 0 && a.pacing != 0 && a.armed == a.pacing {
							lagPacing = append(lagPacing, lag)
							pacingTimerWakes++
						} else {
							lagOther = append(lagOther, lag)
						}
					}
				}
			}
		}
		for k := range share {
			share[k] /= float64(w1 - w0)
		}
	}
	norm := func(s segs) map[string]float64 {
		if lateSum == 0 {
			return nil
		}
		return map[string]float64{"delivery": (s.DeliveryTimer + s.DeliveryOther) / lateSum,
			"delivery_timer": s.DeliveryTimer / lateSum, "delivery_other": s.DeliveryOther / lateSum,
			"unarmed": s.Unarmed / lateSum, "scheduling": s.Scheduling / lateSum,
			"loop": (s.LoopRunning + s.LoopBlocked) / lateSum, "loop_running": s.LoopRunning / lateSum,
			"loop_blocked": s.LoopBlocked / lateSum, "ambiguous": s.Ambiguous / lateSum}
	}
	out["late"] = map[string]any{"count": len(lates), "sum_ns": lateSum, "max_ns": lateMax, "segments_ns": total,
		"shares": norm(total), "leading_events": leadCount, "paced_stops": pacedStops, "cleared": cleared}
	out["alignment"] = map[string]any{"checked": checked, "violations": violations,
		"share": float64(violations) / math.Max(1, float64(checked)), "mono_offset_width_ns": m.MonoWidth}
	out["timer_lag_pacing"], out["timer_lag_other"] = stats(lagPacing), stats(lagOther)
	out["pacing_timer_wakes"], out["select_wakes"], out["woke_cases"] = pacingTimerWakes, wakes, woke
	out["window_shares"] = share
	out["window_s"] = float64(w1-w0) / 1e9
	if *withLate {
		out["late_events"] = lates
	}
}
