package quic

import (
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/testutils/events"

	"github.com/stretchr/testify/require"
)

func TestMTUDiscovererTiming(t *testing.T) {
	const rtt = 100 * time.Millisecond
	rttStats := utils.NewRTTStats()
	rttStats.UpdateRTT(rtt, 0)
	d := newMTUDiscoverer(rttStats, 1000, 2000, nil)

	now := monotime.Now()
	require.False(t, d.ShouldSendProbe(now))
	d.Start(now)
	require.False(t, d.ShouldSendProbe(now))
	require.False(t, d.ShouldSendProbe(now.Add(rtt*9/2)))
	now = now.Add(5 * rtt)
	require.True(t, d.ShouldSendProbe(now))

	// only a single outstanding probe packet is permitted
	ping, _ := d.GetPing(now)
	require.False(t, d.ShouldSendProbe(now))
	now = now.Add(5 * rtt)
	require.False(t, d.ShouldSendProbe(now))
	ping.Handler.OnLost(ping.Frame)
	require.True(t, d.ShouldSendProbe(now))
}

func TestMTUDiscovererAckAndLoss(t *testing.T) {
	const rtt = 200 * time.Millisecond
	rttStats := utils.NewRTTStats()
	rttStats.UpdateRTT(rtt, 0)
	d := newMTUDiscoverer(rttStats, 1000, 2000, nil)
	now := monotime.Now()
	ping, size := d.GetPing(now)
	require.Equal(t, protocol.ByteCount(1500), size)
	// the MTU is reduced if the frame is lost
	ping.Handler.OnLost(ping.Frame)
	require.Equal(t, protocol.ByteCount(1000), d.CurrentSize()) // no change to the MTU yet

	now = now.Add(5 * rtt)
	require.True(t, d.ShouldSendProbe(now))
	ping, size = d.GetPing(now)
	require.Equal(t, protocol.ByteCount(1250), size)
	ping.Handler.OnAcked(ping.Frame)
	require.Equal(t, protocol.ByteCount(1250), d.CurrentSize()) // the MTU is increased

	// Even though the 1500 byte MTU probe packet was lost, we try again with a higher MTU.
	// This protects against regular (non-MTU-related) packet loss.
	now = now.Add(5 * rtt)
	require.True(t, d.ShouldSendProbe(now))
	ping, size = d.GetPing(now)
	require.Greater(t, size, protocol.ByteCount(1500))
	ping.Handler.OnAcked(ping.Frame)
	require.Equal(t, size, d.CurrentSize())

	// We continue probing until the MTU is close to the maximum.
	var steps int
	oldSize := size
	now = now.Add(5 * rtt)
	for d.ShouldSendProbe(now) {
		ping, size = d.GetPing(now)
		require.Greater(t, size, oldSize)
		oldSize = size
		ping.Handler.OnAcked(ping.Frame)
		steps++
		require.Less(t, steps, 10)
		now = now.Add(5 * rtt)
	}
	require.Less(t, 2000-maxMTUDiff, size)
}

func TestMTUDiscovererMTUDiscovery(t *testing.T) {
	for i := range 5 {
		t.Run(fmt.Sprintf("test %d", i), func(t *testing.T) {
			testMTUDiscovererMTUDiscovery(t)
		})
	}
}

func testMTUDiscovererMTUDiscovery(t *testing.T) {
	const rtt = 100 * time.Millisecond
	const startMTU protocol.ByteCount = 1000

	rttStats := utils.NewRTTStats()
	rttStats.UpdateRTT(rtt, 0)

	maxMTU := protocol.ByteCount(rand.IntN(int(3000-startMTU))) + startMTU + 1
	var eventRecorder events.Recorder
	d := newMTUDiscoverer(rttStats, startMTU, maxMTU, &eventRecorder)
	now := monotime.Now()
	d.Start(now)
	realMTU := protocol.ByteCount(rand.IntN(int(maxMTU-startMTU))) + startMTU
	t.Logf("MTU: %d, max: %d", realMTU, maxMTU)
	now = now.Add(mtuProbeDelay * rtt)
	var probes []protocol.ByteCount
	var lastProbeAcked bool
	for d.ShouldSendProbe(now) {
		require.Less(t, len(probes), 25, fmt.Sprintf("too many iterations: %v", probes))
		ping, size := d.GetPing(now)
		probes = append(probes, size)
		lastProbeAcked = size <= realMTU
		if lastProbeAcked {
			ping.Handler.OnAcked(ping.Frame)
		} else {
			ping.Handler.OnLost(ping.Frame)
		}
		now = now.Add(mtuProbeDelay * rtt)
	}
	currentMTU := d.CurrentSize()
	diff := realMTU - currentMTU
	require.GreaterOrEqual(t, diff, protocol.ByteCount(0))
	if currentMTU > startMTU {
		updates := eventRecorder.Events(qlog.MTUUpdated{})
		require.NotEmpty(t, updates)
		// Only an acknowledged probe emits an update. A final loss can end
		// discovery without changing the last update's completion flag.
		require.Equal(t, qlog.MTUUpdated{Value: int(currentMTU), Done: lastProbeAcked}, updates[len(updates)-1])
	}
	t.Logf("MTU discovered: %d (diff: %d)", currentMTU, diff)
	t.Logf("probes sent (%d): %v", len(probes), probes)
	require.LessOrEqual(t, diff, maxMTUDiff)
}

func TestMTUDiscovererWithRandomLoss(t *testing.T) {
	for i := range 5 {
		t.Run(fmt.Sprintf("test %d", i), func(t *testing.T) {
			testMTUDiscovererWithRandomLoss(t)
		})
	}
}

func testMTUDiscovererWithRandomLoss(t *testing.T) {
	const rtt = 100 * time.Millisecond
	const startMTU protocol.ByteCount = 1000
	const maxRandomLoss = maxLostMTUProbes - 1

	rttStats := utils.NewRTTStats()
	rttStats.SetInitialRTT(rtt)
	require.Equal(t, rtt, rttStats.SmoothedRTT())

	maxMTU := protocol.ByteCount(rand.IntN(int(3000-startMTU))) + startMTU + 1
	var eventRecorder events.Recorder
	d := newMTUDiscoverer(rttStats, startMTU, maxMTU, &eventRecorder)
	d.Start(monotime.Now())
	now := monotime.Now()
	realMTU := protocol.ByteCount(rand.IntN(int(maxMTU-startMTU))) + startMTU
	t.Logf("MTU: %d, max: %d", realMTU, maxMTU)
	now = now.Add(mtuProbeDelay * rtt)
	var probes, randomLosses []protocol.ByteCount
	var lastProbeAcked bool

	for d.ShouldSendProbe(now) {
		require.Less(t, len(probes), 32, fmt.Sprintf("too many iterations: %v", probes))
		ping, size := d.GetPing(now)
		probes = append(probes, size)
		packetFits := size <= realMTU
		var acked bool
		if packetFits {
			randomLoss := rand.IntN(maxLostMTUProbes) == 0 && len(randomLosses) < maxRandomLoss
			if randomLoss {
				randomLosses = append(randomLosses, size)
			} else {
				ping.Handler.OnAcked(ping.Frame)
				acked = true
			}
		}
		lastProbeAcked = acked
		if !acked {
			ping.Handler.OnLost(ping.Frame)
		}
		now = now.Add(mtuProbeDelay * rtt)
	}

	currentMTU := d.CurrentSize()
	diff := realMTU - currentMTU
	require.GreaterOrEqual(t, diff, protocol.ByteCount(0))
	if currentMTU > startMTU {
		updates := eventRecorder.Events(qlog.MTUUpdated{})
		require.NotEmpty(t, updates)
		// Only an acknowledged probe emits an update. A final loss can end
		// discovery without changing the last update's completion flag.
		require.Equal(t, qlog.MTUUpdated{Value: int(currentMTU), Done: lastProbeAcked}, updates[len(updates)-1])
	}
	t.Logf("MTU discovered with random losses %v: %d (diff: %d)", randomLosses, currentMTU, diff)
	t.Logf("probes sent (%d): %v", len(probes), probes)
	require.LessOrEqual(t, diff, maxMTUDiff)
}

func TestMTUDiscovererCompletion(t *testing.T) {
	for _, lostProbes := range []int{0, maxLostMTUProbes - 1} {
		t.Run(fmt.Sprintf("after %d losses", lostProbes), func(t *testing.T) {
			const startMTU protocol.ByteCount = 1000
			const maxMTU protocol.ByteCount = 2000
			const rtt = 100 * time.Millisecond
			rttStats := utils.NewRTTStats()
			rttStats.SetInitialRTT(rtt)
			var recorder events.Recorder
			d := newMTUDiscoverer(rttStats, startMTU, maxMTU, &recorder)
			now := monotime.Now()
			d.Start(now)
			now = now.Add(mtuProbeDelay * rtt)
			var probes int
			for d.ShouldSendProbe(now) {
				require.Less(t, probes, 25)
				ping, _ := d.GetPing(now)
				if probes < lostProbes {
					ping.Handler.OnLost(ping.Frame)
				} else {
					ping.Handler.OnAcked(ping.Frame)
				}
				probes++
				now = now.Add(mtuProbeDelay * rtt)
			}
			require.Greater(t, probes, lostProbes)
			require.LessOrEqual(t, maxMTU-d.CurrentSize(), maxMTUDiff)
			require.LessOrEqual(t, d.CurrentSize(), maxMTU)
			updates := recorder.Events(qlog.MTUUpdated{})
			require.NotEmpty(t, updates)
			require.Equal(t, qlog.MTUUpdated{Value: int(d.CurrentSize()), Done: true}, updates[len(updates)-1])
		})
	}
}

func TestMTUDiscovererReset(t *testing.T) {
	t.Run("probe on old path acknowledged", func(t *testing.T) {
		testMTUDiscovererReset(t, true)
	})
	t.Run("probe on old path lost", func(t *testing.T) {
		testMTUDiscovererReset(t, false)
	})
}

func testMTUDiscovererReset(t *testing.T, ackLastProbe bool) {
	const startMTU protocol.ByteCount = 1000
	const maxMTU = 1400
	const rtt = 100 * time.Millisecond

	rttStats := utils.NewRTTStats()
	rttStats.SetInitialRTT(rtt)

	now := monotime.Now()
	d := newMTUDiscoverer(rttStats, startMTU, maxMTU, nil)
	d.Start(now)

	ping, _ := d.GetPing(now.Add(5 * rtt))
	ping.Handler.OnAcked(ping.Frame)
	require.Greater(t, d.CurrentSize(), startMTU)
	now = now.Add(5 * rtt)

	// send another probe packet, but neither acknowledge nor lose it before resetting
	ping, _ = d.GetPing(now.Add(5 * rtt))
	now = now.Add(2 * rtt) // advance the timer by an arbitrary amount

	const newStartMTU protocol.ByteCount = 900
	const newMaxMTU = 1500
	d.Reset(newStartMTU, newMaxMTU)
	require.Equal(t, d.CurrentSize(), newStartMTU)

	// Now acknowledge / lose the probe packet.
	// This should be ignored, since it's on the old path.
	if ackLastProbe {
		ping.Handler.OnAcked(ping.Frame)
	} else {
		ping.Handler.OnLost(ping.Frame)
	}

	// the MTU should not have changed
	require.Equal(t, d.CurrentSize(), newStartMTU)
	// resetting stops probing until discovery is started again
	require.False(t, d.ShouldSendProbe(now.Add(time.Hour)))
	d.Start(now)
	// the next probe should be sent after 5 RTTs
	require.False(t, d.ShouldSendProbe(now.Add(5*rtt).Add(-time.Microsecond)))
	require.True(t, d.ShouldSendProbe(now.Add(5*rtt)))
}

// The finder settles a search that concludes on a lost probe, and only that loss.
// A conclusion on an acknowledgement is left to the connection, which settles once
// it has raised its estimate.
func TestMTUDiscovererSettledOnLoss(t *testing.T) {
	const startMTU protocol.ByteCount = 1000
	const maxMTU protocol.ByteCount = 1400
	rttStats := utils.NewRTTStats()
	rttStats.SetInitialRTT(100 * time.Millisecond)
	now := monotime.Now()
	newFinder := func() (*mtuFinder, *pathMTUSettled) {
		settled := newPathMTUSettled()
		d := newMTUDiscoverer(rttStats, startMTU, maxMTU, nil)
		d.settled = &settled
		d.Start(now)
		return d, &settled
	}
	isSettled := func(s *pathMTUSettled) bool {
		select {
		case <-s.ch:
			return true
		default:
			return false
		}
	}

	t.Run("lost", func(t *testing.T) {
		d, settled := newFinder()
		var losses int
		for !d.done() {
			require.Less(t, losses, 25)
			require.False(t, isSettled(settled))
			ping, _ := d.GetPing(now)
			ping.Handler.OnLost(ping.Frame)
			losses++
		}
		require.True(t, isSettled(settled))
		require.Equal(t, startMTU, d.CurrentSize())

		// A probe from before a reset is ignored, even if its loss would have concluded.
		d, settled = newFinder()
		for range losses - 1 {
			ping, _ := d.GetPing(now)
			ping.Handler.OnLost(ping.Frame)
		}
		ping, _ := d.GetPing(now)
		d.Reset(startMTU, maxMTU)
		ping.Handler.OnLost(ping.Frame)
		require.False(t, d.done())
		require.False(t, isSettled(settled))
	})

	t.Run("acknowledged", func(t *testing.T) {
		d, settled := newFinder()
		for i := 0; !d.done(); i++ {
			require.Less(t, i, 25)
			ping, _ := d.GetPing(now)
			ping.Handler.OnAcked(ping.Frame)
		}
		require.False(t, isSettled(settled))
	})
}

func TestMTUFinderLocalOutcomes(t *testing.T) {
	const start, limit protocol.ByteCount = 1000, 2000
	newProbe := func(now monotime.Time) (*mtuFinder, *mtuFinderAckHandler, ackhandler.Frame) {
		rttStats := utils.NewRTTStats()
		rttStats.UpdateRTT(100*time.Millisecond, 0)
		f := newMTUDiscoverer(rttStats, start, limit, nil)
		f.Start(now)
		ping, _ := f.GetPing(now)
		return f, ping.Handler.(*mtuFinderAckHandler), ping
	}
	now := monotime.Now()

	t.Run("disposal is not loss", func(t *testing.T) {
		f, h, ping := newProbe(now)
		h.disposeLocal()
		require.Equal(t, protocol.InvalidByteCount, f.inFlight)
		h.OnLost(ping.Frame)
		require.Equal(t, limit, f.max())
		require.False(t, f.lastProbeWasLost)
		require.True(t, f.ShouldSendProbe(now.Add(time.Minute)), "the probe delay schedules the next attempt")
	})
	t.Run("uncertain probe's ACK raises the size", func(t *testing.T) {
		f, h, ping := newProbe(now)
		size := f.inFlight
		h.markLocalUncertain()
		require.Equal(t, size, f.inFlight)
		h.OnAcked(ping.Frame)
		require.Equal(t, size, f.CurrentSize())
	})
	t.Run("uncertain probe's loss is not PMTU evidence", func(t *testing.T) {
		f, h, ping := newProbe(now)
		h.markLocalUncertain()
		h.OnLost(ping.Frame)
		require.Equal(t, protocol.InvalidByteCount, f.inFlight)
		require.Equal(t, limit, f.max())
		require.False(t, f.lastProbeWasLost)
	})
	t.Run("late results leave a newer probe alone", func(t *testing.T) {
		f, h, ping := newProbe(now)
		h.OnAcked(ping.Frame)
		_, newer := f.GetPing(now.Add(time.Minute))
		h.disposeLocal()
		h.markLocalUncertain()
		require.Equal(t, newer, f.inFlight)
	})
	t.Run("results from before a path reset are ignored", func(t *testing.T) {
		f, h, _ := newProbe(now)
		f.Reset(start, limit)
		newerPing, newer := f.GetPing(now.Add(time.Minute))
		require.NotNil(t, newerPing.Handler)
		h.disposeLocal()
		require.Equal(t, newer, f.inFlight)
	})
}

// These are the worked path examples in the jumbo plan. Acknowledgements and
// losses model UDP payload limits, without putting a jumbo packet on a socket.
func TestMTUDiscovererTwoStages(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		pathLimit, ceiling     protocol.ByteCount
		wantSize, wantStageOne protocol.ByteCount
		wantProbes, wantLost   int
		firstProbes            []protocol.ByteCount
	}{
		{name: "IPv4 MTU 1500", pathLimit: 1472, ceiling: 8972, wantSize: 1470, wantStageOne: 1438, wantProbes: 17, wantLost: 11, firstProbes: []protocol.ByteCount{1342, 1397, 1424, 1438}},
		{name: "IPv4 MTU 9000", pathLimit: 8972, ceiling: 8972, wantSize: 8957, wantStageOne: 1438, wantProbes: 13, firstProbes: []protocol.ByteCount{1342, 1397, 1424, 1438}},
		{name: "WireGuard 1420", pathLimit: 1392, ceiling: 8972, wantSize: 1390, wantStageOne: 1390, wantProbes: 8, wantLost: 4, firstProbes: []protocol.ByteCount{1342, 1397, 1369, 1410, 1383, 1417, 1390, 1403}},
		{name: "peer advertises 1452", pathLimit: 8972, ceiling: 1452, wantSize: 1438, wantStageOne: 1438, wantProbes: 4, firstProbes: []protocol.ByteCount{1342, 1397, 1424, 1438}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rtt := utils.NewRTTStats()
			rtt.SetInitialRTT(100 * time.Millisecond)
			f := newTwoStageMTUDiscoverer(rtt, 1232, tc.ceiling, nil)
			now := monotime.Now()
			f.Start(now)
			var probes []protocol.ByteCount
			var lost int
			for !f.done() {
				require.Less(t, len(probes), 30, "search terminates")
				now = now.Add(time.Second)
				require.True(t, f.ShouldSendProbe(now))
				ping, size := f.GetPing(now)
				probes = append(probes, size)
				if size > tc.pathLimit {
					lost++
					ping.Handler.OnLost(ping.Frame)
				} else {
					ping.Handler.OnAcked(ping.Frame)
				}
			}
			require.Equal(t, tc.wantSize, f.CurrentSize())
			require.Equal(t, tc.wantStageOne, f.StageOneSize())
			require.Len(t, probes, tc.wantProbes)
			require.Equal(t, tc.wantLost, lost)
			require.Equal(t, tc.firstProbes, probes[:len(tc.firstProbes)])
			require.False(t, f.ShouldSendProbe(now.Add(time.Hour)))
		})
	}
}
