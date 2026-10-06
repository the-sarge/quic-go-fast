//go:build linux

package quic

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	mockackhandler "github.com/quic-go/quic-go/internal/mocks/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func newTestPacingKick(t *testing.T) *pacingKick {
	t.Helper()
	k := newPacingKick()
	require.NotNil(t, k)
	t.Cleanup(k.close)
	return k
}

func kickSetFor(t *testing.T, k *pacingKick) time.Duration {
	t.Helper()
	var its unix.ItimerSpec
	require.NoError(t, unix.TimerfdGettime(k.fd, &its))
	return time.Duration(its.Value.Nano())
}

// An otherwise idle process notices a due Go timer when the kick expires,
// instead of at the netpoller's next whole-millisecond timeout.
func TestPacingKickServicesTheTimerOnTime(t *testing.T) {
	k := newTestPacingKick(t)
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	var late []time.Duration
	for i := range 200 {
		d := monotime.Now().Add(200*time.Microsecond + time.Duration(i%9)*100*time.Microsecond)
		timer.Reset(monotime.Until(d))
		k.arm(d)
		<-timer.C
		late = append(late, monotime.Since(d))
	}
	slices.Sort(late)
	require.Less(t, late[len(late)/2], 250*time.Microsecond, "median timer lateness with the kick")
}

func TestPacingKickArmsOnlyOnChange(t *testing.T) {
	k := newTestPacingKick(t)
	require.Zero(t, kickSetFor(t, k))
	d := monotime.Now().Add(time.Hour)
	k.arm(d)
	require.Equal(t, d, k.armed)
	remaining := kickSetFor(t, k)
	require.InDelta(t, time.Hour, remaining, float64(time.Second))
	k.arm(d) // unchanged: not set again
	require.LessOrEqual(t, kickSetFor(t, k), remaining)
	k.arm(0)
	require.Zero(t, kickSetFor(t, k))
	var nilKick *pacingKick
	nilKick.arm(d) // Reno connections and failed setup: no kick
	nilKick.close()
}

func timerfds(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	require.NoError(t, err)
	n := 0
	for _, e := range entries {
		if l, err := os.Readlink("/proc/self/fd/" + e.Name()); err == nil && strings.Contains(l, "timerfd") {
			n++
		}
	}
	return n
}

func TestPacingKickFollowsTheConnectionLifetime(t *testing.T) {
	for _, selection := range []string{"reno", "bbrv3"} {
		t.Run(selection, func(t *testing.T) {
			before := timerfds(t)
			conf := &Config{EnableDatagrams: true}
			require.NoError(t, conf.SetCongestionControlV1(selection))
			client, server := congestionControlPair(t, conf, conf)
			exchangeCongestionControlTraffic(t, client, server)
			want := 0
			if selection == "bbrv3" {
				want = 2 // client and server
			}
			require.Equal(t, before+want, timerfds(t))
			require.NoError(t, client.CloseWithError(0, "finished"))
			require.NoError(t, server.CloseWithError(0, "finished"))
			require.Eventually(t, func() bool { return timerfds(t) == before }, 5*time.Second, 10*time.Millisecond)
		})
	}
}

// The kick follows only the pacing deadline: ACK, loss-detection and idle
// deadlines, and congestion-limited or hard-blocked connections, never set it.
func TestPacingKickFollowsOnlyThePacingDeadline(t *testing.T) {
	ctrl := gomock.NewController(t)
	sph := mockackhandler.NewMockSentPacketHandler(ctrl)
	conf := &Config{MaxIdleTimeout: time.Minute}
	require.NoError(t, conf.SetCongestionControlV1("bbrv3"))
	tc := newServerTestConnection(t, ctrl, conf, false, connectionOptHandshakeConfirmed(), connectionOptSentPacketHandler(sph))
	c := tc.conn
	require.NotNil(t, c.emission.bbr)
	c.pacingKick = newTestPacingKick(t)
	c.timer = time.NewTimer(time.Hour)
	c.lastPacketReceivedTime = monotime.Now()
	c.idleTimeout = time.Minute // normally from the transport parameters
	var lossTimeout monotime.Time
	sph.EXPECT().GetLossDetectionTimeout().DoAndReturn(func() monotime.Time { return lossTimeout }).AnyTimes()

	now := monotime.Now()
	lossTimeout = now.Add(time.Hour)
	c.pacingDeadline = now.Add(time.Second)
	c.maybeResetTimer()
	require.Equal(t, c.pacingDeadline, c.pacingKick.armed)
	first := c.pacingDeadline

	lossTimeout = now.Add(500 * time.Millisecond) // a loss-detection deadline first
	c.pacingDeadline = now.Add(2 * time.Second)
	c.maybeResetTimer()
	require.Equal(t, first, c.pacingKick.armed)

	lossTimeout = now.Add(time.Hour)
	for _, mode := range []blockMode{blockModeCongestionLimited, blockModeHardBlocked} {
		c.blocked = mode
		c.pacingDeadline = monotime.Now().Add(3 * time.Second)
		c.maybeResetTimer()
		require.Equal(t, first, c.pacingKick.armed)
	}
}
