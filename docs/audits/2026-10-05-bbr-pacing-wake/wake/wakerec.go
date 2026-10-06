package quic

import (
	"encoding/json"
	"os"
	"runtime/trace"
	"sync/atomic"
	"time"
	"unsafe"
)

// Measurement-only #734 wake-path recorder. During the measured window it
// records, on the one goroutine that calls wakeRecord, every timer arm,
// run-loop wake case, send opportunity and opportunity exit, stamped with
// runtime.nanotime, and (unless built with wakeTrace false) captures the Go
// execution trace over the same window. The trace supplies what the recorder
// cannot see: when the runtime ran the timer, when the goroutine became
// runnable and when it began running. build.py copies this file unchanged
// except for its package clause into the fixture tree and the synthetic
// harness. Not a maintained hook.

//go:linkname wakeNanotime runtime.nanotime
func wakeNanotime() int64

// wakeTrace is rewritten to false by build.py for the trace-off variant.
const wakeTrace = true

// Event kinds. Times A–D are monotonic values in the caller's clock (the
// recorder stores the offset to runtime.nanotime) except where noted.
const (
	wkArm  = 1 + iota // A gen, B armed deadline, C pacing deadline (0 none), D mode (0 normal, 1 hard-blocked, 2 congestion-limited)
	wkWoke            // A run-loop case (1 timer, 2 sending scheduled, 3 handshake feedback, 4 send queue, 5 received packet)
	wkOpp             // A the opportunity's now
	wkExit            // A paced deadline (0 when the exit was not a paced stop), B stop reason, C state class, D pacing rate (bytes/s)
)

// State classes after an opportunity exit, as the #715 overlay classifies them.
const (
	wsImmediate = iota // progress with no data left to pack now: an immediate continuation
	wsApp              // supply exhausted
	wsPaced
	wsCwnd
	wsCredit // send queue or local send credit full
	wsHard
	wsOther
	wsClasses
)

type wakeEvent struct{ T, K, A, B, C, D int64 }

const wakeChunk = 1 << 16

var wake struct {
	on      atomic.Bool
	end     int64 // runtime.nanotime at which the recording goroutine stops itself
	gen     int64
	pending int64 // the #715 overlay's pending paced deadline, as the recorded events imply it
	armed   bool  // an arm was recorded since the last recorded exit
	// Window aggregates over every opportunity, recorded or not: time in each
	// state class between opportunities, time inside opportunities, and lateness.
	class     int64
	rate      int64
	exitT     int64
	entryT    int64
	stateNS   [wsClasses]int64
	emitNS    int64
	opps      int64
	lateN     int64
	lateNS    int64
	lateBytes float64 // lateness × the pacing rate at the stop: credit the pacer discarded
	chunks    [][]wakeEvent
	cur       []wakeEvent
	done      chan struct{}
}

// wakeEnded is called only by the recorded goroutine, from every hook. Once
// the window ends it turns recording off itself and hands the buffers to the
// goroutine started by wakeStart.
func wakeEnded(t int64) bool {
	if t < wake.end {
		return false
	}
	wake.on.Store(false)
	close(wake.done)
	return true
}

func wakeRecord(k, a, b, c, d int64) {
	if !wake.on.Load() {
		return
	}
	t := wakeNanotime()
	if wakeEnded(t) {
		return
	}
	if len(wake.cur) == cap(wake.cur) {
		if wake.cur != nil {
			wake.chunks = append(wake.chunks, wake.cur)
		}
		wake.cur = make([]wakeEvent, 0, wakeChunk)
	}
	wake.cur = append(wake.cur, wakeEvent{t, k, a, b, c, d})
}

// wakeArmed records a timer arm with a new generation number, unless neither
// a pending nor a pacing deadline exists (such an arm cannot bear on a late
// opportunity).
func wakeArmed(armed, pacing, mode int64) {
	if !wake.on.Load() || (wake.pending == 0 && pacing == 0) {
		return
	}
	wake.gen++
	wake.armed = true
	wakeRecord(wkArm, wake.gen, armed, pacing, mode)
}

// wakeWoken records which run-loop case returned, while a deadline is pending.
func wakeWoken(c int64) {
	if !wake.on.Load() || wake.pending == 0 {
		return
	}
	wakeRecord(wkWoke, c, 0, 0, 0)
}

// wakeOpportunity records an opportunity only when a paced deadline is
// pending, the only case in which it can be late; at high packet rates most
// opportunities are immediate continuations with nothing pending.
func wakeOpportunity(now int64) {
	if !wake.on.Load() {
		return
	}
	t := wakeNanotime()
	if wakeEnded(t) {
		return
	}
	if wake.exitT != 0 {
		wake.stateNS[wake.class] += t - wake.exitT
	}
	wake.entryT = t
	wake.opps++
	if wake.pending == 0 {
		return
	}
	wakeRecord(wkOpp, now, 0, 0, 0)
	if now >= wake.pending {
		late := now - wake.pending
		wake.lateN++
		wake.lateNS += late
		wake.lateBytes += float64(late) * float64(wake.rate) / 1e9
		wake.pending = 0
	}
}

// wakeExited records an opportunity exit unless it changes nothing the
// analysis reads: no deadline pending or set, and no arm since the last
// recorded exit (an exit resets the analysis's per-deadline arm count).
func wakeExited(deadline, stop, class, rate int64) {
	if !wake.on.Load() {
		return
	}
	t := wakeNanotime()
	if wakeEnded(t) {
		return
	}
	if wake.entryT != 0 {
		wake.emitNS += t - wake.entryT
	}
	wake.exitT, wake.class = t, class
	if deadline != 0 {
		wake.rate = rate
	}
	if deadline == 0 && wake.pending == 0 && !wake.armed {
		return
	}
	wake.armed = false
	wake.pending = deadline
	wakeRecord(wkExit, deadline, stop, class, rate)
}

// wakeOffset estimates runtime.nanotime minus the caller's monotonic clock
// from the narrowest of 64 bracketed readings.
func wakeOffset(mono func() int64) (offset, width int64) {
	width = 1 << 62
	for range 64 {
		a := wakeNanotime()
		m := mono()
		b := wakeNanotime()
		if b-a < width {
			width, offset = b-a, a+(b-a)/2-m
		}
	}
	return offset, width
}

// wakeStart runs the window: the trace starts 50 ms before startUnix so its
// initial state is settled, recording runs from startUnix to endUnix, and the
// files are written after the recorded goroutine hands its buffers back.
func wakeStart(prefix string, startUnix, endUnix int64, mono func() int64) {
	go func() {
		meta := map[string]any{"start_unix_ns": startUnix, "end_unix_ns": endUnix, "trace": wakeTrace}
		time.Sleep(time.Until(time.Unix(0, startUnix).Add(-50 * time.Millisecond)))
		var f *os.File
		if wakeTrace {
			var err error
			if f, err = os.Create(prefix + ".trace"); err == nil {
				err = trace.Start(f)
			}
			if err != nil {
				meta["trace_error"] = err.Error()
			}
		}
		time.Sleep(time.Until(time.Unix(0, startUnix)))
		offset, width := wakeOffset(mono)
		t0, w0 := wakeNanotime(), time.Now().UnixNano()
		wake.end = t0 + (endUnix - w0)
		wake.done = make(chan struct{})
		wake.on.Store(true)
		select {
		case <-wake.done:
			meta["handoff"] = "window end"
		case <-time.After(time.Until(time.Unix(0, endUnix)) + 2*time.Second):
			// The goroutine never recorded after the window: its buffers are stale and unread.
			wake.on.Store(false)
			meta["handoff"] = "timeout"
		}
		if wakeTrace && meta["trace_error"] == nil {
			trace.Stop()
			f.Close()
		}
		n := 0
		if meta["handoff"] == "window end" {
			out, err := os.Create(prefix + ".events")
			if err == nil {
				for _, c := range append(wake.chunks, wake.cur) {
					if len(c) > 0 {
						_, err = out.Write(unsafe.Slice((*byte)(unsafe.Pointer(&c[0])), len(c)*int(unsafe.Sizeof(c[0]))))
						n += len(c)
					}
				}
				if cerr := out.Close(); err == nil {
					err = cerr
				}
			}
			if err != nil {
				meta["events_error"] = err.Error()
			}
		}
		meta["start_nanotime"], meta["start_wall_ns"], meta["end_nanotime"] = t0, w0, wake.end
		meta["mono_offset_ns"], meta["mono_offset_width_ns"] = offset, width
		meta["events"], meta["event_fields"] = n, []string{"t_nanotime", "kind", "a", "b", "c", "d"}
		if meta["handoff"] == "window end" {
			meta["aggregate"] = map[string]any{"state_ns": wake.stateNS, "state_classes": []string{"immediate", "app", "paced", "cwnd", "credit", "hard", "other"},
				"emit_ns": wake.emitNS, "opportunities": wake.opps, "late_count": wake.lateN, "late_ns": wake.lateNS, "late_bytes": wake.lateBytes}
		}
		if b, err := json.Marshal(meta); err == nil {
			os.WriteFile(prefix+".json", b, 0o644)
		}
	}()
}
