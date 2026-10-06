//go:build linux

package quic

import (
	"os"
	"time"

	"golang.org/x/sys/unix"

	"github.com/quic-go/quic-go/internal/monotime"
)

// pacingKick makes the runtime service a BBR connection's pacing timer on time.
// On Linux, Go runs an idle process's timers from a thread blocked in
// epoll_wait, whose timeout has millisecond granularity, or at the next
// scheduling event, so the timer fires after the pacing deadline; D08 caps
// pacing credit at one quantum, so that lateness is lost sending. The kick is a
// timerfd registered with the runtime's netpoller, set just after the deadline:
// its expiry ends the netpoller's wait, and the runtime then runs the timer,
// which is already due. Nothing reads the timerfd, and no goroutine waits on
// it; the connection still wakes from its own timer. The kick changes only
// when the runtime notices that timer, never what an opportunity admits.
type pacingKick struct {
	file   *os.File
	fd     int
	offset int64         // CLOCK_MONOTONIC nanoseconds minus monotime nanoseconds
	armed  monotime.Time // deadline the timerfd is set for (0: none)
}

// pacingKickMargin sets the timerfd after the deadline, so the timer is due
// when the runtime checks it; a check just before the deadline would put the
// netpoller back to sleep for a whole millisecond.
const pacingKickMargin = 2 * time.Microsecond

func newPacingKick() *pacingKick {
	fd, err := unix.TimerfdCreate(unix.CLOCK_MONOTONIC, unix.TFD_NONBLOCK|unix.TFD_CLOEXEC)
	if err != nil {
		return nil
	}
	k := &pacingKick{fd: fd}
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
			width, k.offset = d, a.Nano()+d/2-m
		}
	}
	// A non-blocking descriptor is registered with the runtime's netpoller.
	k.file = os.NewFile(uintptr(fd), "pacing-kick")
	return k
}

// arm sets the kick for a pacing deadline; 0 clears it. Called only by the run
// loop; a nil kick does nothing. The timerfd is set only when the deadline
// changes.
func (k *pacingKick) arm(d monotime.Time) {
	if k == nil || d == k.armed {
		return
	}
	k.armed = d
	var its unix.ItimerSpec
	if d != 0 {
		its.Value = unix.NsecToTimespec(int64(d) + k.offset + int64(pacingKickMargin))
	}
	_ = unix.TimerfdSettime(k.fd, unix.TFD_TIMER_ABSTIME, &its, nil)
}

func (k *pacingKick) close() {
	if k != nil {
		k.file.Close()
	}
}
