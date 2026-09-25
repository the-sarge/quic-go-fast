package congestion

import (
	"fmt"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
)

// Use the existing registered-packet trace; CE authority is independent of the
// optional bandwidth sample. All rates observed here include the pacing margin.
func ceTrace(t *testing.T) *bbrProbeTrace {
	t.Helper()
	x := newBBRProbeTrace()
	for range 4 {
		x.ack(100000, 20000, SendUnknown)
	}
	x.ack(100000, 9000, SendUnknown)
	return x
}
func ceRate(b *BBRSender) uint64 { r := b.PacingRate(); return r/100*99 + r%100*99/100 }
func ceEvent(x *bbrProbeTrace, ordinal, marks uint64) FeedbackEvent {
	return FeedbackEvent{Time: x.now, HasAck: true, PathGeneration: x.b.pathGeneration, SampleGeneration: x.b.sampleGeneration, PriorInFlight: 20000, PostInFlight: 20000, Delivery: DeliverySample{Delivered: x.delivered}, ECN: ECNResult{Eligible: true, Ordinal: ordinal, PathGeneration: x.b.pathGeneration, Delta: ECNCounts{CE: marks}}}
}

func TestBBRCESparseAndSustained(t *testing.T) {
	for _, marks := range []uint64{1, 10} {
		x := ceTrace(t)
		x.b.Feedback(ceEvent(x, x.ordinal, marks))
		require.EqualValues(t, 49500, ceRate(x.b))
		require.EqualValues(t, 9000, x.b.GetCongestionWindow())
		x.ack(100000, 20000, SendUnknown)
		require.EqualValues(t, 49500, ceRate(x.b), "ordinary update cannot erase CE")
		x.b.Feedback(ceEvent(x, x.ordinal, marks))
		require.EqualValues(t, 24750, ceRate(x.b))
		require.EqualValues(t, 4800, x.b.GetCongestionWindow())
	}
}

func TestBBRCEPhaseEffects(t *testing.T) {
	t.Run("completed Drain exit", func(t *testing.T) {
		for _, ce := range []bool{false, true} {
			x := newBBRProbeTrace()
			for range 3 {
				x.ack(100000, 20000, SendUnknown)
			}
			e := probeRTTEvent(x, 100*time.Millisecond, 100*time.Millisecond, 100000, 7800)
			if ce {
				e.ECN = ECNResult{Eligible: true, Ordinal: x.ordinal, Delta: ECNCounts{CE: 1}}
			}
			x.b.Feedback(e)
			require.Equal(t, bbrCruise, x.b.phase, "CE must preserve the completed normal exit")
		}
	})

	for _, phase := range []bbrPhase{bbrStartup, bbrDrain, bbrDown, bbrCruise, bbrRefill, bbrUp, bbrProbeRTT} {
		t.Run(fmt.Sprint(phase), func(t *testing.T) {
			x := ceTrace(t)
			// Phase-local entry is fixed; feedback and exposed outputs are real.
			x.b.phase = phase
			if phase == bbrProbeRTT {
				x.b.probeRTTCap = 8000
				x.b.probeRTTReturnStartup = true
			}
			x.b.rate = x.b.phaseRate()
			before := ceRate(x.b)
			x.b.Feedback(ceEvent(x, x.ordinal, 1))
			want := phase
			if phase == bbrStartup {
				want = bbrDrain
			}
			if phase == bbrRefill || phase == bbrUp {
				want = bbrDown
			}
			require.Equal(t, want, x.b.phase)
			require.LessOrEqual(t, ceRate(x.b), before/2)
			for range 30 {
				x.ack(100000, 20000, SendUnknown)
			}
			require.NotEqual(t, bbrRefill, x.b.phase)
			require.NotEqual(t, bbrUp, x.b.phase)
		})
	}
}

func TestBBRCEBoundarySuppression(t *testing.T) {
	x := ceTrace(t)
	x.b.Feedback(ceEvent(x, x.ordinal, 1))
	for _, anchor := range []uint64{x.ordinal - 1, x.ordinal} {
		x.b.Feedback(ceEvent(x, anchor, 1))
		require.EqualValues(t, 49500, ceRate(x.b))
	}
	x.ack(100000, 20000, SendUnknown)
	x.b.Feedback(ceEvent(x, x.ordinal, 1))
	require.EqualValues(t, 24750, ceRate(x.b))
}

func TestBBRCEUnknownAnchor(t *testing.T) {
	for _, kind := range []string{"missing", "future", "old path", "ineligible", "old model", "stale sample"} {
		t.Run(kind, func(t *testing.T) {
			x := ceTrace(t)
			e := ceEvent(x, x.ordinal, 1)
			switch kind {
			case "missing":
				e.ECN.Ordinal = 0
			case "future":
				e.ECN.Ordinal++
			case "old path":
				e.ECN.PathGeneration++
			case "ineligible":
				e.ECN.Eligible = false
			case "old model":
				x.b.Reset(0, 1, x.delivered)
			case "stale sample":
				x.b.Sent(SendEvent{Packet: PacketInfo{Ordinal: x.ordinal + 1, RegistrationValid: true, SampleGeneration: 1}})
			}
			before := ceRate(x.b)
			x.b.Feedback(e)
			if kind == "stale sample" {
				require.EqualValues(t, 49500, ceRate(x.b), "sample fences cannot discard independent validated CE")
				return
			}
			require.Equal(t, before, ceRate(x.b))
		})
	}
}

func TestBBRCEFloorComposition(t *testing.T) {
	for _, tc := range []struct {
		name string
		rate uint64
		rtt  time.Duration
		want uint64
	}{
		{"normal", 100000, 100 * time.Millisecond, 49500},
		{"packet floor", 10000, 100 * time.Millisecond, 9900},
		{"minimum interval", 100000, time.Microsecond, 99000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := ceTrace(t)
			x.b.rate = tc.rate
			x.b.minimumRTT = tc.rtt
			e := ceEvent(x, x.ordinal, 1)
			e.PriorInFlight = 1000
			x.b.Feedback(e)
			require.EqualValues(t, 4800, x.b.GetCongestionWindow())
			require.Equal(t, tc.want, ceRate(x.b))
			x.b.SetMaxDatagramSize(1400)
			require.EqualValues(t, 5600, x.b.GetCongestionWindow())
			x.b.SetMaxDatagramSize(1200)
			require.EqualValues(t, 4800, x.b.GetCongestionWindow(), "temporary floor does not inflate stored evidence")
		})
	}
	t.Run("recovery RTT before first measurement", func(t *testing.T) {
		x := newBBRProbeTrace()
		x.ordinal = 1
		x.b.Sent(SendEvent{Packet: PacketInfo{Ordinal: 1, RegistrationValid: true}})
		e := ceEvent(x, 1, 1)
		x.b.rate = 10000
		e.SmoothedRTT = time.Second
		x.b.Feedback(e)
		require.EqualValues(t, 4950, ceRate(x.b), "recovery RTT supplies the floor before any measured minimum")
	})
}

func ceAck(x *bbrProbeTrace) FeedbackEvent {
	e := probeRTTEvent(x, 100*time.Millisecond, 100*time.Millisecond, 100000, 20000)
	e.ECN = ECNResult{Eligible: true, Ordinal: x.ordinal, PathGeneration: x.b.pathGeneration, Delta: ECNCounts{ECT0: 1}}
	return e
}

func TestBBRCECleanRoundRecovery(t *testing.T) {
	t.Run("precautionary shortcut cannot survive CE", func(t *testing.T) {
		x := newBBRProbeTrace()
		x.up(t)
		x.lose(20000, SendUnknown)
		for range 12 {
			x.ack(100000, 0, SendUnknown)
			if x.b.phase == bbrUp {
				break
			}
		}
		require.Equal(t, bbrUp, x.b.phase)
		x.ackFlight(100000, 19183, 18000, SendUnknown)
		require.True(t, x.b.previousProbePrecautionary, "real prior-loss probe produced shortcut")
		x.b.Feedback(ceEvent(x, x.ordinal, 1))
		for range 20 {
			x.b.Feedback(ceAck(x))
			if !x.b.ce.active {
				break
			}
		}
		require.False(t, x.b.ce.active)
		require.Equal(t, bbrCruise, x.b.phase)
		for range 2 {
			x.b.Feedback(ceAck(x))
			require.Equal(t, bbrCruise, x.b.phase, "fresh wait must precede probing")
		}
	})

	t.Run("unmeasured bandwidth uses actual Cruise fallback", func(t *testing.T) {
		x := newBBRProbeTrace()
		for range 3 {
			e := ceAck(x)
			e.Delivery.Valid = false
			e.ECN.Delta = ECNCounts{CE: 1}
			x.b.Feedback(e)
		}
		require.EqualValues(t, 14850, ceRate(x.b))
		for range 3 {
			e := ceAck(x)
			e.Delivery.Valid = false
			x.b.Feedback(e)
		}
		require.EqualValues(t, 26850, ceRate(x.b), "one bounded step, not fallback-rate restoration")
	})

	for _, dirty := range []string{"none", "missing", "limited", "probe limited", "ambiguous", "loss", "same epoch CE"} {
		t.Run(dirty, func(t *testing.T) {
			x := ceTrace(t)
			x.b.Feedback(ceEvent(x, x.ordinal, 1))
			e := ceAck(x)
			x.b.Feedback(e) // ACK passes the response boundary; start only.
			require.EqualValues(t, 49500, ceRate(x.b))
			if dirty != "none" {
				d := e
				d.Acked = nil
				d.Delivery.Valid = false
				switch dirty {
				case "missing":
					d.ECN = ECNResult{}
				case "limited":
					d.Delivery.Limited = SendApplicationLimited
				case "probe limited":
					d.Delivery.Limited = SendProbeRTTLimited
				case "ambiguous":
					d.ECN.Eligible = false
					d.ECN.Deferred = true
				case "loss":
					d.HasAck = false
					d.Lost = []PacketInfo{{AckEliciting: true, Length: 1200, Delivery: DeliverySnapshot{PostInFlight: 20000}}}
				case "same epoch CE":
					d.ECN.Ordinal = x.ordinal - 1
					d.ECN.Delta = ECNCounts{CE: 1}
				}
				x.b.Feedback(d)
			}
			x.b.Feedback(ceAck(x)) // complete first recovery round; no step yet
			require.EqualValues(t, 49500, ceRate(x.b))
			x.b.Feedback(ceAck(x))
			if dirty == "none" {
				require.EqualValues(t, 61500, ceRate(x.b), "packet/rtt floor dominates six percent here")
				x.b.Feedback(ceAck(x))
				require.EqualValues(t, 10200, x.b.GetCongestionWindow(), "next ACK can use the prior flight-cap step")
			} else {
				require.EqualValues(t, 49500, ceRate(x.b), "isolated clean ACK cannot erase whole-round dirt")
			}
		})
	}
	t.Run("bounded release to fresh Cruise wait", func(t *testing.T) {
		x := ceTrace(t)
		x.b.Feedback(ceEvent(x, x.ordinal, 1))
		for range 15 {
			x.b.Feedback(ceAck(x))
		}
		require.Equal(t, bbrCruise, x.b.phase)
		require.EqualValues(t, 99000, ceRate(x.b))
		require.EqualValues(t, 21200, x.b.GetCongestionWindow())
	})
}

func TestBBRCEFailureRecovery(t *testing.T) {
	x := ceTrace(t)
	x.b.Feedback(ceEvent(x, x.ordinal, 1))
	e := ceAck(x)
	e.ECN = ECNResult{Failed: true}
	x.b.Feedback(e)
	require.EqualValues(t, 49500, ceRate(x.b))
	for range 3 {
		e = ceAck(x)
		e.ECN = ECNResult{Failed: true}
		e.Acked[0].ECN = protocol.ECNNon
		x.b.Feedback(e)
	}
	require.EqualValues(t, 61500, ceRate(x.b), "post-switch Not-ECT rounds recover after definitive failure")
	e = ceAck(x)
	e.ECN = ECNResult{Failed: true, Eligible: true, Ordinal: x.ordinal, Delta: ECNCounts{CE: 1}}
	x.b.Feedback(e)
	require.EqualValues(t, 30750, ceRate(x.b), "independently accepted CE is honored even after failure")
}

func TestBBRCECapsSurviveProbeAndIdle(t *testing.T) {
	t.Run("same ACK probe entry and CE", func(t *testing.T) {
		x := ceTrace(t)
		e := probeRTTEvent(x, 6*time.Second, 200*time.Millisecond, 100000, 4900)
		e.ECN = ECNResult{Eligible: true, Ordinal: x.ordinal, Delta: ECNCounts{CE: 1}}
		x.b.Feedback(e)
		require.True(t, x.b.InProbeRTT())
		probeRTTAck(x, 201*time.Millisecond, 100*time.Millisecond, 100000, 4800)
		require.True(t, x.b.InProbeRTT(), "hold could not start before flight met the CE target")
		probeRTTAck(x, 201*time.Millisecond, 100*time.Millisecond, 100000, 4800)
		require.False(t, x.b.InProbeRTT())
	})

	for _, phase := range []string{"cruise", "startup"} {
		t.Run(phase, func(t *testing.T) {
			x := ceTrace(t)
			if phase == "startup" {
				x = newBBRProbeTrace()
				x.ack(100000, 20000, SendUnknown)
			}
			probeRTTAck(x, 6*time.Second, 200*time.Millisecond, 100000, 0)
			require.True(t, x.b.InProbeRTT())
			x.b.Feedback(ceEvent(x, x.ordinal, 1))
			rate, window := ceRate(x.b), x.b.GetCongestionWindow()
			probeRTTAck(x, 201*time.Millisecond, 100*time.Millisecond, 100000, 0)
			require.False(t, x.b.InProbeRTT())
			require.False(t, x.b.InSlowStart(), "CE full-bandwidth decision survives probe restoration")
			require.LessOrEqual(t, ceRate(x.b), rate)
			require.LessOrEqual(t, x.b.GetCongestionWindow(), window)
			x.b.BeforeSend(x.now.Add(time.Second), true)
			require.LessOrEqual(t, ceRate(x.b), rate)
			e := ceAck(x)
			e.RecoveryEpisode.UndoEligible = true
			x.b.Feedback(e)
			require.LessOrEqual(t, ceRate(x.b), rate, "spurious-loss evidence cannot undo CE")
			x.b.Reset(1, 1, x.delivered)
			require.True(t, x.b.InSlowStart())
			require.EqualValues(t, 12000, x.b.GetCongestionWindow())
			x.b.Close()
			x.b.Feedback(e)
			require.Zero(t, x.b.PacingRate())
		})
	}
}

func TestBBRCESimultaneousLoss(t *testing.T) {
	for _, lossFlight := range []protocol.ByteCount{10000, 200000} {
		t.Run(fmt.Sprint(lossFlight), func(t *testing.T) {
			x := newBBRProbeTrace()
			x.up(t)
			before := ceRate(x.b)
			e := ceAck(x)
			e.Lost = []PacketInfo{{Ordinal: x.ordinal - 1, PacketNumber: 99, AckEliciting: true, Length: 1200, Delivery: DeliverySnapshot{PostInFlight: lossFlight}}}
			e.Delivery.Lost = 10000
			e.ECN.Delta = ECNCounts{CE: 1}
			x.b.Feedback(e)
			require.EqualValues(t, 61875, ceRate(x.b), "CE derives from the pre-loss Up rate, not the subsequent Down rate")
			require.EqualValues(t, 123750, before)
			require.LessOrEqual(t, x.b.GetCongestionWindow(), protocol.ByteCount(10600))
			require.Equal(t, bbrDown, x.b.phase)
		})
	}
}
