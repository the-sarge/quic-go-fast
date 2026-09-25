package congestion

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBBRUndoAllSpuriousOnly(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(map[bool]string{false: "mixed", true: "all"}[all], func(t *testing.T) {
			x := newBBRProbeTrace()
			x.cruise()
			for range 2 {
				x.ack(20000, 9000, SendUnknown)
			}
			saved := x.b.GetCongestionWindow()
			x.b.Feedback(FeedbackEvent{Time: x.now, RecoveryEpisode: RecoveryEpisode{ID: 1, Boundary: x.ordinal, Entered: true, Active: true, UndoPossible: true}})
			x.lose(20000, SendUnknown)
			x.ack(20000, 9000, SendUnknown)
			require.Less(t, x.b.GetCongestionWindow(), saved)
			x.b.Feedback(FeedbackEvent{Time: x.now.Add(time.Millisecond), RecoveryEpisode: RecoveryEpisode{ID: 1, Exited: true, UndoEligible: all}})
			if all {
				require.Equal(t, saved, x.b.GetCongestionWindow())
				require.EqualValues(t, 100000, x.b.PacingRate())
			} else {
				require.Less(t, x.b.GetCongestionWindow(), saved)
				require.EqualValues(t, 70000, x.b.PacingRate())
			}
			require.False(t, x.b.InRecovery())
		})
	}
}

func TestBBRPersistentRestartTwoPackets(t *testing.T) {
	t.Run("PTO-only proof on a CE release boundary", func(t *testing.T) {
		x := ceTrace(t)
		x.b.Feedback(ceEvent(x, x.ordinal, 1))
		x.b.Feedback(ceAck(x))
		x.b.Feedback(ceAck(x))
		rate, flight := x.b.ce.rate, x.b.ce.flight
		e := ceAck(x)
		e.SmoothedRTT = 100 * time.Millisecond
		e.PersistentCongestion = PersistentCongestion{StartOrdinal: 1, EndOrdinal: 2}
		x.b.Feedback(e)
		require.Equal(t, rate, x.b.ce.rate, "persistent congestion cannot count as a clean CE recovery round")
		require.Equal(t, flight, x.b.ce.flight)
		require.True(t, x.b.ce.active)
		require.EqualValues(t, 2400, x.b.GetCongestionWindow())
	})

	for _, ce := range []bool{false, true} {
		t.Run(map[bool]string{false: "loss", true: "simultaneous CE"}[ce], func(t *testing.T) {
			x := newBBRProbeTrace()
			x.cruise()
			e := FeedbackEvent{Time: x.now, HasAck: true, SmoothedRTT: 200 * time.Millisecond, PriorInFlight: 20000, Delivery: DeliverySample{Delivered: x.delivered}, PersistentCongestion: PersistentCongestion{StartOrdinal: 1, EndOrdinal: 2}}
			if ce {
				e.ECN = ECNResult{Eligible: true, Ordinal: x.ordinal, Delta: ECNCounts{CE: 1}}
			}
			x.b.Feedback(e)
			require.EqualValues(t, 2400, x.b.GetCongestionWindow())
			require.True(t, x.b.InSlowStart())
			require.EqualValues(t, 33271, x.b.PacingRate(), "two packets / recovery RTT with Startup gain")
			x.b.SetMaxDatagramSize(1200)
			x.b.BeforeSend(x.now.Add(time.Millisecond), true)
			require.EqualValues(t, 2400, x.b.GetCongestionWindow(), "send refresh cannot restore four-M floor")
			x.ack(12000, 0, SendUnknown)
			require.EqualValues(t, 4800, x.b.GetCongestionWindow(), "first fresh receipt permits normal floor")
			if ce {
				require.LessOrEqual(t, x.b.PacingRate(), uint64(50000), "independent CE cap remains")
			}
		})
	}
}

func TestBBRUndoKeepsCEAndProbeCaps(t *testing.T) {
	for _, mode := range []string{"CE", "ProbeRTT", "both"} {
		t.Run(mode, func(t *testing.T) {
			x := newBBRProbeTrace()
			x.cruise()
			saved := x.b.GetCongestionWindow()
			x.b.Feedback(FeedbackEvent{Time: x.now, RecoveryEpisode: RecoveryEpisode{ID: 1, Entered: true, Active: true, UndoPossible: true}})
			x.lose(20000, SendUnknown)
			x.ack(20000, 9000, SendUnknown)
			if mode != "ProbeRTT" {
				x.b.Feedback(ceEvent(x, x.ordinal, 1))
			}
			ceWindow := x.b.GetCongestionWindow()
			if mode != "CE" {
				probeRTTAck(x, 6*time.Second, 100*time.Millisecond, 100000, 20000)
			}
			window, rate := x.b.GetCongestionWindow(), x.b.PacingRate()
			x.b.Feedback(FeedbackEvent{Time: x.now, RecoveryEpisode: RecoveryEpisode{ID: 1, Exited: true, UndoEligible: true}})
			require.Equal(t, window, x.b.GetCongestionWindow(), "restoration honors independent flight cap")
			if mode != "ProbeRTT" {
				require.LessOrEqual(t, x.b.PacingRate(), rate, "undo cannot remove CE rate cap")
			}
			if mode != "CE" {
				require.True(t, x.b.InProbeRTT(), "undo does not complete measurement")
				probeRTTAck(x, time.Millisecond, 100*time.Millisecond, 100000, 0)
				probeRTTAck(x, 201*time.Millisecond, 100*time.Millisecond, 100000, 0)
				require.False(t, x.b.InProbeRTT())
				if mode == "ProbeRTT" {
					require.GreaterOrEqual(t, x.b.GetCongestionWindow(), saved, "successful probe exit restores the repaired saved window")
				} else {
					require.LessOrEqual(t, x.b.GetCongestionWindow(), ceWindow)
				}
			}
		})
	}
}

func TestBBRPersistentRestartDeduplicated(t *testing.T) {
	x := newBBRProbeTrace()
	x.cruise()
	e := FeedbackEvent{Time: x.now, HasAck: true, SmoothedRTT: 100 * time.Millisecond, Delivery: DeliverySample{Delivered: x.delivered}, PersistentCongestion: PersistentCongestion{StartOrdinal: 1, EndOrdinal: 2}}
	x.b.Feedback(e)
	x.ack(100000, 0, SendUnknown)
	before := x.b.GetCongestionWindow()
	e.SampleGeneration = x.b.sampleGeneration
	e.Delivery.Delivered = x.delivered
	e.Time = x.now
	x.b.Feedback(e)
	require.Equal(t, before, x.b.GetCongestionWindow(), "repeated span cannot reset the new model")
	e.PersistentCongestion.EndOrdinal = 3
	x.b.Feedback(e)
	require.EqualValues(t, 2400, x.b.GetCongestionWindow(), "a newly extended proof can restart again")
}
