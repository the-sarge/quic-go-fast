package congestion

import (
	"fmt"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
)

// Drive registered packets and typed feedback through the reducer. Absolute
// expected rates below use draft-06 gains before emission's pacing margin.
type bbrProbeTrace struct {
	step                     time.Duration
	b                        *BBRSender
	now                      monotime.Time
	ordinal, delivered, lost uint64
}

func newBBRProbeTrace() *bbrProbeTrace {
	b := NewBBRSender(1200)
	b.random = func(int32) int32 { return 0 }
	return &bbrProbeTrace{b: b, step: 100 * time.Millisecond, now: monotime.Time(time.Second)}
}

func (x *bbrProbeTrace) ack(rate uint64, flight protocol.ByteCount, limited SendLimitation) {
	x.ackFlight(rate, flight, flight+1200, limited)
}

func (x *bbrProbeTrace) ackFlight(rate uint64, flight, sentFlight protocol.ByteCount, limited SendLimitation) {
	x.ackFrom(rate, flight, sentFlight, limited, x.delivered)
}

func (x *bbrProbeTrace) ackFrom(rate uint64, flight, sentFlight protocol.ByteCount, limited SendLimitation, priorDelivered uint64) {
	x.ordinal++
	x.now = x.now.Add(x.step)
	p := PacketInfo{Ordinal: x.ordinal, PathGeneration: x.b.pathGeneration, SampleGeneration: x.b.sampleGeneration, Length: 1200, AckEliciting: true, RegistrationValid: true, SendTime: x.now.Add(-100 * time.Millisecond), Delivery: DeliverySnapshot{Delivered: priorDelivered, Lost: x.lost, Valid: true, PriorInFlight: max(0, sentFlight-1200), PostInFlight: sentFlight, Limited: limited}}
	x.b.Sent(SendEvent{Packet: p, PriorInFlight: flight, PostInFlight: flight + 1200})
	x.delivered += 1200
	x.b.Feedback(FeedbackEvent{PathGeneration: p.PathGeneration, SampleGeneration: p.SampleGeneration, Time: x.now, HasAck: true, RawRTT: 100 * time.Millisecond, PriorInFlight: flight + 1200, PostInFlight: flight, Acked: []PacketInfo{p}, Delivery: DeliverySample{Delivered: x.delivered, Lost: x.lost, Ordinal: p.Ordinal, BytesPerSecond: rate, Interval: 100 * time.Millisecond, Limited: limited, Valid: true}})
}

func TestBBRProbeBWTransitions(t *testing.T) {
	x := newBBRProbeTrace()
	for range 4 {
		x.ack(100000, 20000, SendUnknown)
	}
	require.EqualValues(t, 50000, x.b.PacingRate(), "Drain")
	x.ack(100000, 9000, SendUnknown)
	require.EqualValues(t, 100000, x.b.PacingRate(), "Drain exit reaches Cruise on the same ACK")
	x.ack(100000, 9000, SendUnknown)
	require.EqualValues(t, 100000, x.b.PacingRate(), "Cruise")
	for range 10 {
		x.ack(100000, 9000, SendUnknown)
	}
	require.EqualValues(t, 125000, x.b.PacingRate(), "Refill lasts one packet-timed round then Up")
	require.False(t, x.b.InSlowStart())
	for _, from := range []string{"Drain", "Up"} {
		t.Run("boundary seed from "+from, func(t *testing.T) {
			x := newBBRProbeTrace()
			if from == "Up" {
				x.up(t)
				for range 3 {
					x.ack(100000, 9000, SendUnknown)
				}
			} else {
				for range 4 {
					x.ack(100000, 20000, SendUnknown)
				}
				x.ack(100000, 9000, SendUnknown)
			}
			require.EqualValues(t, 100000, x.b.latestRate, "reseed after the Down reset")
			require.EqualValues(t, 1200, x.b.latestVolume)
			x.lose(60000, SendUnknown)
			x.ack(20000, 8000, SendUnknown)
			require.EqualValues(t, 100000, x.b.bandwidthShort, "the boundary sample protects the first loss round")
			x.lose(60000, SendUnknown)
			x.ack(20000, 8000, SendUnknown)
			require.EqualValues(t, 70000, x.b.bandwidthShort, "a later low-rate loss round may reduce the bound")
		})
	}
	t.Run("Cruise uses maximum bandwidth", func(t *testing.T) {
		x := newBBRProbeTrace()
		x.up(t)
		for range 3 {
			x.ack(100000, 9000, SendUnknown)
		}
		x.ack(20000, 20000, SendUnknown) // retain Down and seed the next loss round
		x.lose(60000, SendUnknown)       // exactly two percent: no long-term reduction
		x.ack(20000, 8000, SendUnknown)
		require.EqualValues(t, 70000, x.b.bandwidthShort)
		require.Equal(t, bbrCruise, x.b.phase, "8000 bytes is below max-BDP 10000, above bounded-BDP 7000")
	})
}

func (x *bbrProbeTrace) cruise() {
	for range 4 {
		x.ack(100000, 20000, SendUnknown)
	}
	x.ack(100000, 9000, SendUnknown)
	x.ack(100000, 9000, SendUnknown)
}

func (x *bbrProbeTrace) up(t *testing.T) {
	t.Helper()
	x.cruise()
	for range 12 {
		if x.b.PacingRate() == 125000 {
			return
		}
		x.ack(100000, 9000, SendUnknown)
	}
	t.Fatal("did not enter Up")
}

func TestBBRProbeBWPlateauAndRiskyReprobe(t *testing.T) {
	t.Run("plateau ignores supply limitation", func(t *testing.T) {
		x := newBBRProbeTrace()
		x.up(t)
		for range 5 {
			x.ack(100000, 9000, SendApplicationLimited)
		}
		require.EqualValues(t, 125000, x.b.PacingRate())
		for range 2 {
			x.ack(100000, 9000, SendUnknown)
			require.EqualValues(t, 125000, x.b.PacingRate())
		}
		x.ack(100000, 9000, SendUnknown)
		require.EqualValues(t, 90000, x.b.PacingRate(), "three qualifying plateau rounds end Up")
	})
	t.Run("precautionary probe retries after feedback", func(t *testing.T) {
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
		require.EqualValues(t, 90000, x.b.PacingRate(), "reaching the previous loss boundary ends the cautious probe")
		x.ack(100000, 0, SendUnknown)
		require.Equal(t, bbrRefill, x.b.phase, "clean feedback immediately permits a full probe")
		x.ack(100000, 0, SendUnknown)
		require.EqualValues(t, 125000, x.b.PacingRate())
	})
}

func TestBBRProbeBWAckPhaseAging(t *testing.T) {
	x := newBBRProbeTrace()
	x.up(t)
	for range 3 {
		x.ack(10000, 9000, SendUnknown)
	}
	require.EqualValues(t, 90000, x.b.PacingRate())
	// The first lower-rate probe still retains the preceding cycle's maximum.
	x.ack(10000, 9000, SendUnknown)
	require.EqualValues(t, 100000, x.b.PacingRate())
	for range 40 {
		x.ack(10000, 0, SendUnknown)
	}
	require.LessOrEqual(t, x.b.PacingRate(), uint64(12500), "two completed probe cycles age the old maximum")
}

func TestBBRProbeBWAppLimitedFilter(t *testing.T) {
	for _, limited := range []SendLimitation{SendApplicationLimited, SendFlowControlLimited, SendProbeRTTLimited} {
		x := newBBRProbeTrace()
		x.up(t)
		for range 3 {
			x.ack(100000, 9000, SendUnknown)
		}
		for range 30 {
			x.ack(10000, 0, limited)
		}
		require.GreaterOrEqual(t, x.b.PacingRate(), uint64(90000), "low supply-limited samples cannot expire capacity")
		x.ack(200000, 0, limited)
		require.GreaterOrEqual(t, x.b.PacingRate(), uint64(180000), "limited samples can raise capacity")
	}
}

func (x *bbrProbeTrace) lose(flight protocol.ByteCount, limited SendLimitation) {
	x.ordinal++
	p := PacketInfo{Ordinal: x.ordinal, PathGeneration: x.b.pathGeneration, SampleGeneration: x.b.sampleGeneration, Length: 1200, AckEliciting: true, RegistrationValid: true, Delivery: DeliverySnapshot{Delivered: x.delivered, Lost: x.lost, Valid: true, PriorInFlight: flight - 1200, PostInFlight: flight, Limited: limited}}
	x.b.Sent(SendEvent{Packet: p, PriorInFlight: flight - 1200, PostInFlight: flight})
	x.lost += 1200
	x.now = x.now.Add(time.Millisecond)
	x.b.Feedback(FeedbackEvent{PathGeneration: p.PathGeneration, SampleGeneration: p.SampleGeneration, Time: x.now, Lost: []PacketInfo{p}, PriorInFlight: flight, PostInFlight: flight - 1200, Delivery: DeliverySample{Delivered: x.delivered, Lost: x.lost}})
}

func TestBBRProbeBWLossBoundAttribution(t *testing.T) {
	for _, limited := range []SendLimitation{SendUnknown, SendApplicationLimited} {
		t.Run(fmt.Sprint(limited), func(t *testing.T) {
			x := newBBRProbeTrace()
			x.up(t)
			x.lose(20000, limited)
			x.ackFlight(100000, 20000, 18000, SendUnknown)
			require.EqualValues(t, 90000, x.b.PacingRate(), "loss ends the probe even with limited supply")
			if limited == SendUnknown {
				require.EqualValues(t, 19183, x.b.GetCongestionWindow(), "bound uses lost packet flight and the two-percent crossing prefix")
			} else {
				require.Greater(t, x.b.GetCongestionWindow(), protocol.ByteCount(19183), "limited loss cannot establish the long-term capacity bound")
			}
		})
	}
}

func TestBBRProbeBWCapsAcrossPhases(t *testing.T) {
	t.Run("utilization lifetime", func(t *testing.T) {
		b := NewBBRSender(1200)
		event := SendEvent{Packet: PacketInfo{AckEliciting: true, RegistrationValid: true}, PostInFlight: 12000}
		b.Sent(event)
		require.True(t, b.roundWindowLimited())
		b.Reset(1, 1, 0)
		b.Sent(event)
		require.False(t, b.roundWindowLimited(), "old path registration cannot restore utilization after reset")
		event.Packet.PathGeneration, event.Packet.SampleGeneration = 1, 1
		b.Sent(event)
		require.True(t, b.roundWindowLimited())
		event.Packet.SampleGeneration = 2
		event.PostInFlight = 1200
		b.Sent(event)
		require.False(t, b.roundWindowLimited(), "a new sample-round boundary retires old utilization")
		event.Packet.SampleGeneration = 1
		event.PostInFlight = 12000
		b.Sent(event)
		require.False(t, b.roundWindowLimited(), "stale registration cannot revive retired round history")
		b.Close()
		b.Sent(event)
		require.False(t, b.roundWindowLimited())
	})

	t.Run("remember utilization over a packet round", func(t *testing.T) {
		x := newBBRProbeTrace()
		x.up(t)
		x.lose(20000, SendUnknown)
		for range 12 {
			x.ackFlight(100000, 0, 1200, SendUnknown)
			if x.b.phase == bbrUp {
				break
			}
		}
		require.Equal(t, bbrUp, x.b.phase)
		x.ackFlight(100000, 0, 1200, SendUnknown)
		cap := x.b.GetCongestionWindow()
		require.EqualValues(t, 19183, cap)
		boundary := x.delivered
		// Earlier in this round, ordinary packets used all whole-packet credit.
		// Subsequent ACKs can arrive after flight has fallen below that level.
		x.b.Sent(SendEvent{Packet: PacketInfo{RegistrationValid: true, AckEliciting: true, Delivery: DeliverySnapshot{Delivered: boundary}}, PostInFlight: 18000})
		for range 10 {
			x.ackFrom(100000, 0, 1200, SendUnknown, boundary)
		}
		require.Greater(t, x.b.GetCongestionWindow(), cap, "low-flight ACKs consume the round's remembered full-window observation")
		require.Equal(t, bbrUp, x.b.phase)
		require.Zero(t, x.b.plateau, "remembered utilization suppresses a cap-limited plateau")
		// A completed round with no full-window use replaces that observation.
		for range 3 {
			x.ackFlight(100000, 0, 1200, SendUnknown)
		}
		require.Equal(t, bbrDown, x.b.phase, "old utilization cannot suppress plateaus indefinitely")
	})

	t.Run("Up window gain and quantization", func(t *testing.T) {
		x := newBBRProbeTrace()
		x.up(t)
		for range 6 {
			x.ack(100000, 0, SendApplicationLimited)
		}
		require.EqualValues(t, 26100, x.b.GetCongestionWindow(), "2.25 BDP + 1200 aggregation + two packets")
	})
	t.Run("safe capacity grows a learned cap", func(t *testing.T) {
		x := newBBRProbeTrace()
		x.up(t)
		x.lose(20000, SendUnknown)
		x.ackFlight(100000, 20000, 18000, SendUnknown)
		require.EqualValues(t, 19183, x.b.GetCongestionWindow())
		x.ackFlight(100000, 0, 18000, SendUnknown)
		require.EqualValues(t, 16306, x.b.GetCongestionWindow(), "Cruise leaves fifteen percent headroom")
		for range 12 {
			x.ackFlight(100000, 0, 18000, SendUnknown)
			if x.b.phase == bbrUp {
				break
			}
		}
		require.Equal(t, bbrUp, x.b.phase)
		// Stay below the risky level while fully using cwnd at ACK entry. The
		// delivered packet's snapshot does not itself raise the capacity estimate.
		for range 6 {
			flight := x.b.GetCongestionWindow() / 1200 * 1200
			x.ackFlight(100000, flight-1200, 18000, SendUnknown)
		}
		require.Greater(t, x.b.GetCongestionWindow(), protocol.ByteCount(19183), "packet-scaled exponential slope grows a fully used cap")
		require.Equal(t, bbrUp, x.b.phase, "a cap-limited sender must not declare a bandwidth plateau")
	})
}

func TestBBRProbeBWCoexistenceUnits(t *testing.T) {
	for _, tc := range []struct {
		name   string
		rate   uint64
		size   protocol.ByteCount
		rounds int
	}{
		{"subpacket", 2000, 1200, 1}, {"ceil", 12010, 1200, 2}, {"current MTU", 12500, 1280, 1}, {"ceiling", 1000000, 1200, 63},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := newBBRProbeTrace()
			x.step = time.Millisecond
			for range 4 {
				x.ack(tc.rate, 1000000, SendUnknown)
			}
			x.ack(tc.rate, 0, SendUnknown)
			require.Equal(t, bbrCruise, x.b.phase)
			x.b.SetMaxDatagramSize(tc.size)
			for range tc.rounds - 1 {
				x.ack(tc.rate, 0, SendUnknown)
				require.NotEqual(t, bbrRefill, x.b.phase)
			}
			x.ack(tc.rate, 0, SendUnknown)
			require.Equal(t, bbrRefill, x.b.phase, "round trigger is measured in current packet equivalents")
		})
	}
}

func TestBBRProbeBWRandomizedWait(t *testing.T) {
	for _, high := range []bool{false, true} {
		t.Run(fmt.Sprint(high), func(t *testing.T) {
			x := newBBRProbeTrace()
			x.b.random = func(n int32) int32 {
				if high {
					return n - 1
				}
				return 0
			}
			x.cruise()
			wait := 2 * time.Second
			if high {
				wait = 3*time.Second - time.Nanosecond
			}
			x.step = time.Nanosecond
			x.now = x.b.cycleStamp.Add(wait - x.step)
			x.ackFrom(100000, 0, 1200, SendUnknown, 0)
			require.Equal(t, bbrCruise, x.b.phase, "strict time boundary")
			x.ackFrom(100000, 0, 1200, SendUnknown, 0)
			require.Equal(t, bbrRefill, x.b.phase)
			x.b.Reset(1, 1, x.delivered)
			require.EqualValues(t, 12000, x.b.GetCongestionWindow())
			require.EqualValues(t, map[bool]int{false: 0, true: 1}[high], x.b.random(2), "reset retains injected randomness, clears phase state")
		})
	}
}

func TestBBRProbeBWAggregationBudget(t *testing.T) {
	x := newBBRProbeTrace()
	x.up(t)
	x.step = time.Microsecond
	for range 15 {
		x.ack(100000, 0, SendApplicationLimited)
	}
	require.Greater(t, x.b.GetCongestionWindow(), protocol.ByteCount(26100), "ACK bursts add measured aggregation")
	x.step = 100 * time.Millisecond
	for range 12 {
		x.ack(100000, 0, SendApplicationLimited)
	}
	require.EqualValues(t, 26100, x.b.GetCongestionWindow(), "ten-round aggregation ages without changing capacity")
}

func TestBBRProbeBWDelayedFeedback(t *testing.T) {
	t.Run("ACK facts without a rate sample", func(t *testing.T) {
		x := newBBRProbeTrace()
		x.up(t)
		for range 3 {
			x.ack(100000, 9000, SendUnknown)
		}
		round, bandwidth := x.b.round, x.b.bandwidth
		x.delivered += 1200
		e := FeedbackEvent{HasAck: true, PostInFlight: 0, Delivery: DeliverySample{Delivered: x.delivered}}
		x.b.Feedback(e)
		require.Equal(t, bbrDown, x.b.phase, "invalid time cannot drive phase checks")
		x.delivered += 1200
		e.Time, e.Delivery.Delivered = x.now.Add(time.Millisecond), x.delivered
		x.b.Feedback(e)
		require.Equal(t, bbrCruise, x.b.phase, "authoritative flight can drain Down without a rate sample")
		e.Time = x.b.cycleStamp.Add(x.b.probeWait + time.Nanosecond)
		e.Delivery.Delivered += 1200
		x.b.Feedback(e)
		require.Equal(t, bbrRefill, x.b.phase, "authoritative time can expire the wait")
		require.Equal(t, round, x.b.round, "invalid samples do not advance packet rounds")
		require.Equal(t, bandwidth, x.b.bandwidth)
	})

	t.Run("probe losses arrive during Down", func(t *testing.T) {
		x := newBBRProbeTrace()
		x.up(t)
		for range 3 {
			x.ack(100000, 9000, SendUnknown)
		}
		require.Equal(t, bbrDown, x.b.phase)
		cycle, round := x.b.cycle, x.b.round
		x.lose(20000, SendUnknown)
		require.Equal(t, round, x.b.round, "timer loss cannot advance packet rounds")
		require.Equal(t, cycle, x.b.cycle)
		x.ackFrom(100000, 20000, 18000, SendUnknown, 0)
		require.EqualValues(t, 19183, x.b.GetCongestionWindow(), "late probing loss still establishes the bound")
		x.lose(12000, SendUnknown)
		x.ackFrom(100000, 20000, 18000, SendUnknown, 0)
		require.EqualValues(t, 19183, x.b.GetCongestionWindow(), "one long-term reduction per probe")
	})
	t.Run("old samples cannot finish probe feedback", func(t *testing.T) {
		x := newBBRProbeTrace()
		x.up(t)
		for range 3 {
			x.ack(100000, 9000, SendUnknown)
		}
		cycle := x.b.cycle
		x.ackFrom(10000, 0, 1200, SendUnknown, 0)
		require.Equal(t, cycle, x.b.cycle)
		x.ack(10000, 0, SendUnknown)
		require.Equal(t, cycle+1, x.b.cycle, "only a packet sent after the stop boundary ages the filter")
	})
}
