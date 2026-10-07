package quic

import "sync/atomic"

// Measurement-only #738 synthetic injections for the Stage 0 preflight. Only
// the synthetic fixture variants link this file; build.py rewrites the
// constants per case and inserts the calls. Not a maintained hook.
//
//   - synSlowWorkerNS: the send worker spins this long inside every submission.
//   - synBusyLoopNS: the connection loop spins this long at every BBR
//     opportunity, before any reservation (credit stays available).
//   - synSignalDelayNS: a delayed local signal. When the worker dequeues from
//     a full send queue, the queue keeps reporting itself full (WouldBlock)
//     and withholds its availability signal for this long; then a helper
//     goroutine signals. The slot itself is free on time. On this fixture the
//     local wait that binds is the send queue's (credit is never refused), so
//     this is the signal that ends the connection's local waits.
//   - synAlternate: the mixed case alternates phases of synPhaseNS of the
//     window cap (even phases) and the delayed local signal (odd phases), so
//     that each limit holds about half the window.

const (
	synSlowWorkerNS  = 0
	synBusyLoopNS    = 0
	synSignalDelayNS = 0
	synAlternate     = false
	synPhaseNS       = 1_000_000_000
)

// synOddPhase reports whether the monotonic clock is in an odd phase.
func synOddPhase() bool { return (handNanotime()/synPhaseNS)&1 == 1 }

func synSpin(d int64) {
	for end := handNanotime() + d; handNanotime() < end; {
	}
}

var (
	synHoldUntil atomic.Int64 // 0 when not holding
	synRelease   chan chan struct{}
)

func init() {
	if synSignalDelayNS > 0 {
		synRelease = make(chan chan struct{}, 1)
		go func() {
			for ch := range synRelease {
				for handNanotime() < synHoldUntil.Load() {
				}
				synHoldUntil.Store(0)
				select {
				case ch <- struct{}{}:
					handSignaled(false)
				default:
				}
			}
		}()
	}
}

// synHeld reports whether freed queue space is still being withheld.
func synHeld() bool { return synHoldUntil.Load() != 0 }

// synDequeued runs on the worker after a dequeue; full reports that the queue
// was full before it.
func synDequeued(full bool, ch chan struct{}) {
	if synAlternate && !synOddPhase() {
		return
	}
	if full && synHoldUntil.CompareAndSwap(0, handNanotime()+synSignalDelayNS) {
		synRelease <- ch
	}
}
