//go:build linux

package quic

import (
	"os"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"

	"github.com/quic-go/quic-go/internal/monotime"
)

// pacingWaker wakes a BBR connection's run loop at its pacing deadline from a
// timerfd that the runtime's netpoller watches. On Linux, Go runs an idle
// process's timers from a thread blocked in epoll_wait with a millisecond
// timeout, or at the next scheduling event, so the loop wakes after the
// deadline; D08 caps pacing credit at one quantum, so that lateness is lost
// sending. A timerfd expires at the deadline, and its readiness ends the
// netpoller's wait. The waker changes only when the loop wakes, never what an
// opportunity admits: deadlines, credit and admission stay the policy's.
type pacingWaker struct {
	file   *os.File
	fd     int
	offset int64 // CLOCK_MONOTONIC nanoseconds minus monotime nanoseconds

	armed monotime.Time // deadline the timerfd is set for; owned by the run loop
	want  atomic.Int64  // the pacing deadline the loop waits for (0: none)
	fired atomic.Bool   // the timerfd expired since it was last set
	C     chan struct{}
	done  chan struct{}
}

func newPacingWaker() *pacingWaker {
	fd, err := unix.TimerfdCreate(unix.CLOCK_MONOTONIC, unix.TFD_NONBLOCK|unix.TFD_CLOEXEC)
	if err != nil {
		return nil
	}
	w := &pacingWaker{fd: fd, C: make(chan struct{}, 1), done: make(chan struct{})}
	// Both clocks are CLOCK_MONOTONIC; monotime counts from its own origin.
	width := int64(1 << 62)
	for range 16 {
		var a, b unix.Timespec
		if unix.ClockGettime(unix.CLOCK_MONOTONIC, &a) != nil {
			unix.Close(fd)
			return nil
		}
		m := int64(monotime.Now())
		if unix.ClockGettime(unix.CLOCK_MONOTONIC, &b) != nil {
			unix.Close(fd)
			return nil
		}
		if d := b.Nano() - a.Nano(); d < width {
			width, w.offset = d, a.Nano()+d/2-m
		}
	}
	// A non-blocking descriptor is registered with the runtime's netpoller.
	w.file = os.NewFile(uintptr(fd), "pacing-timerfd")
	go w.run()
	return w
}

func (w *pacingWaker) run() {
	defer close(w.done)
	var b [8]byte
	for {
		if _, err := w.file.Read(b[:]); err != nil {
			return // closed
		}
		w.fired.Store(true)
		w.deliver(monotime.Now())
	}
}

// deliver wakes the loop only for the deadline it still waits for: an expiry
// the loop has since cleared or moved later wakes nothing.
func (w *pacingWaker) deliver(now monotime.Time) {
	if d := w.want.Load(); d != 0 && !now.Before(monotime.Time(d)) {
		select {
		case w.C <- struct{}{}:
		default:
		}
	}
}

// arm sets the pacing deadline the loop waits for; 0 clears it. Called only by
// the run loop; a nil waker does nothing. The timerfd is set only when the deadline changes or after it
// expired, and one microsecond late, so a wake never precedes the deadline.
func (w *pacingWaker) arm(d monotime.Time) {
	if w == nil {
		return
	}
	w.want.Store(int64(d))
	if d == w.armed && (d == 0 || !w.fired.Load()) {
		return
	}
	w.armed = d
	w.fired.Store(false)
	var its unix.ItimerSpec
	if d != 0 {
		its.Value = unix.NsecToTimespec(int64(d) + w.offset + int64(time.Microsecond))
	}
	_ = unix.TimerfdSettime(w.fd, unix.TFD_TIMER_ABSTIME, &its, nil)
}

func (w *pacingWaker) close() {
	w.file.Close()
	<-w.done
}
