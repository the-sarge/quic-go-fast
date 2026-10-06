//go:build linux

package quic

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/mocks/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func newTestPacingWaker(t *testing.T) *pacingWaker {
	t.Helper()
	w := newPacingWaker()
	require.NotNil(t, w)
	t.Cleanup(w.close)
	return w
}

func receiveWake(w *pacingWaker, within time.Duration) (monotime.Time, bool) {
	select {
	case <-w.C:
		return monotime.Now(), true
	case <-time.After(within):
		return 0, false
	}
}

func TestPacingWakerNeverWakesBeforeTheDeadline(t *testing.T) {
	w := newTestPacingWaker(t)
	for range 200 {
		d := monotime.Now().Add(200 * time.Microsecond)
		w.arm(d)
		at, ok := receiveWake(w, time.Second)
		require.True(t, ok)
		require.False(t, at.Before(d), "woke %v before the deadline", d.Sub(at))
	}
}

func TestPacingWakerRearmsTheSameDeadlineOnlyAfterExpiry(t *testing.T) {
	w := newTestPacingWaker(t)
	d := monotime.Now().Add(time.Millisecond)
	w.arm(d)
	w.arm(d) // unchanged: no new expiry
	_, ok := receiveWake(w, time.Second)
	require.True(t, ok)
	_, ok = receiveWake(w, 20*time.Millisecond)
	require.False(t, ok, "one deadline, one wake")
	w.arm(d) // the loop still waits for the expired deadline: wake again
	_, ok = receiveWake(w, time.Second)
	require.True(t, ok)
}

func TestPacingWakerMovesWithTheDeadline(t *testing.T) {
	w := newTestPacingWaker(t)
	// A rate decrease moves the deadline later: no wake at the earlier one.
	w.arm(monotime.Now().Add(time.Millisecond))
	later := monotime.Now().Add(30 * time.Millisecond)
	w.arm(later)
	at, ok := receiveWake(w, time.Second)
	require.True(t, ok)
	require.False(t, at.Before(later))
	// An increase moves it earlier.
	w.arm(monotime.Now().Add(time.Second))
	earlier := monotime.Now().Add(time.Millisecond)
	w.arm(earlier)
	at, ok = receiveWake(w, 500*time.Millisecond)
	require.True(t, ok)
	require.False(t, at.Before(earlier))
}

func TestPacingWakerClearedDeadlineWakesNothing(t *testing.T) {
	w := newTestPacingWaker(t)
	w.arm(monotime.Now().Add(time.Millisecond))
	w.arm(0)
	_, ok := receiveWake(w, 20*time.Millisecond)
	require.False(t, ok)
}

func TestPacingWakerFiltersStaleExpiries(t *testing.T) {
	w := &pacingWaker{C: make(chan struct{}, 1)}
	now := monotime.Now()
	w.want.Store(0)
	w.deliver(now) // cleared
	w.want.Store(int64(now.Add(time.Millisecond)))
	w.deliver(now) // moved later
	require.Empty(t, w.C)
	w.want.Store(int64(now))
	w.deliver(now)
	w.deliver(now) // coalesced
	require.Len(t, w.C, 1)
}

func TestPacingWakerCloseEndsItsGoroutine(t *testing.T) {
	w := newPacingWaker()
	require.NotNil(t, w)
	w.arm(monotime.Now().Add(time.Hour))
	w.close()
	select {
	case <-w.done:
	default:
		t.Fatal("waker goroutine still running")
	}
}

func pacingWakerGoroutines() int {
	buf := make([]byte, 1<<20)
	return strings.Count(string(buf[:runtime.Stack(buf, true)]), "(*pacingWaker).run")
}

func TestPacingWakerFollowsTheConnectionLifetime(t *testing.T) {
	for _, selection := range []string{"reno", "bbrv3"} {
		t.Run(selection, func(t *testing.T) {
			before := pacingWakerGoroutines()
			conf := &Config{EnableDatagrams: true}
			require.NoError(t, conf.SetCongestionControlV1(selection))
			client, server := congestionControlPair(t, conf, conf)
			exchangeCongestionControlTraffic(t, client, server)
			want := 0
			if selection == "bbrv3" {
				want = 2 // client and server
			}
			require.Equal(t, before+want, pacingWakerGoroutines())
			require.NoError(t, client.CloseWithError(0, "finished"))
			require.NoError(t, server.CloseWithError(0, "finished"))
			require.Eventually(t, func() bool { return pacingWakerGoroutines() == before }, 5*time.Second, 10*time.Millisecond)
		})
	}
}

// The waker takes only the pacing deadline. ACK, loss-detection and idle
// deadlines stay on the connection's Go timer, congestion-limited and
// hard-blocked connections have no pacing wake, and the timer's fallback never
// precedes the waker.
func TestPacingWakerTakesOnlyThePacingDeadline(t *testing.T) {
	ctrl := gomock.NewController(t)
	sph := mockackhandler.NewMockSentPacketHandler(ctrl)
	conf := &Config{MaxIdleTimeout: time.Minute}
	require.NoError(t, conf.SetCongestionControlV1("bbrv3"))
	tc := newServerTestConnection(t, ctrl, conf, false, connectionOptHandshakeConfirmed(), connectionOptSentPacketHandler(sph))
	c := tc.conn
	require.NotNil(t, c.emission.bbr)
	c.pacingWake = newTestPacingWaker(t)
	c.timer = time.NewTimer(time.Hour)
	c.lastPacketReceivedTime = monotime.Now()
	c.idleTimeout = time.Minute // normally from the transport parameters
	var lossTimeout monotime.Time
	sph.EXPECT().GetLossDetectionTimeout().DoAndReturn(func() monotime.Time { return lossTimeout }).AnyTimes()
	timerFired := func(within time.Duration) bool {
		select {
		case <-c.timer.C:
			return true
		case <-time.After(within):
			return false
		}
	}

	// Pacing first: the waker owns it; the timer waits behind the fallback.
	now := monotime.Now()
	lossTimeout = now.Add(time.Hour)
	c.pacingDeadline = now.Add(20 * time.Millisecond)
	c.maybeResetTimer()
	require.Equal(t, int64(c.pacingDeadline), c.pacingWake.want.Load())
	at, ok := receiveWake(c.pacingWake, time.Second)
	require.True(t, ok)
	require.False(t, at.Before(c.pacingDeadline))
	if timerFired(0) {
		require.False(t, monotime.Now().Before(c.pacingDeadline.Add(pacingWakeFallback)), "timer before its fallback")
	} else {
		require.True(t, timerFired(time.Second), "fallback never fired")
	}

	// A loss-detection deadline first (an exemption from pacing): the timer takes it.
	now = monotime.Now()
	lossTimeout = now.Add(5 * time.Millisecond)
	c.pacingDeadline = now.Add(50 * time.Millisecond)
	c.maybeResetTimer()
	require.Zero(t, c.pacingWake.want.Load())
	require.True(t, timerFired(time.Second))
	_, ok = receiveWake(c.pacingWake, 100*time.Millisecond)
	require.False(t, ok, "the waker woke for a deadline the timer owns")

	// A pacing deadline within the fallback of a later deadline: the timer keeps
	// that deadline, and the waker takes the pacing one.
	now = monotime.Now()
	lossTimeout = now.Add(11 * time.Millisecond)
	c.pacingDeadline = now.Add(10 * time.Millisecond)
	c.maybeResetTimer()
	require.Equal(t, int64(c.pacingDeadline), c.pacingWake.want.Load())
	_, ok = receiveWake(c.pacingWake, time.Second)
	require.True(t, ok)
	require.True(t, timerFired(time.Second))

	// No pacing wake while congestion-limited or hard-blocked.
	lossTimeout = monotime.Now().Add(time.Hour)
	for _, mode := range []blockMode{blockModeCongestionLimited, blockModeHardBlocked} {
		c.blocked = blockModeNone
		c.pacingDeadline = monotime.Now().Add(time.Second)
		c.maybeResetTimer()
		require.NotZero(t, c.pacingWake.want.Load())
		c.blocked = mode
		c.maybeResetTimer()
		require.Zero(t, c.pacingWake.want.Load())
	}
}
