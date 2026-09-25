package congestion

import (
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
)

// Keep the reducer seam: registrations freeze delivery state, then a logical
// ACK supplies independently chosen capacity and raw RTT observations.
func probeRTTAck(x *bbrProbeTrace, elapsed, rtt time.Duration, rate uint64, flight protocol.ByteCount) {
	x.b.Feedback(probeRTTEvent(x, elapsed, rtt, rate, flight))
}

func probeRTTEvent(x *bbrProbeTrace, elapsed, rtt time.Duration, rate uint64, flight protocol.ByteCount) FeedbackEvent {
	x.now = x.now.Add(elapsed)
	x.ordinal++
	p := PacketInfo{Ordinal: x.ordinal, PathGeneration: x.b.pathGeneration, SampleGeneration: x.b.sampleGeneration, Length: 1200, AckEliciting: true, RegistrationValid: true, SendTime: x.now.Add(-rtt), Delivery: DeliverySnapshot{Delivered: x.delivered, Valid: true, PostInFlight: flight + 1200}}
	x.b.Sent(SendEvent{Packet: p, PriorInFlight: flight, PostInFlight: flight + 1200})
	x.delivered += 1200
	return FeedbackEvent{PathGeneration: p.PathGeneration, SampleGeneration: p.SampleGeneration, Time: x.now, HasAck: true, RawRTT: rtt, PriorInFlight: flight + 1200, PostInFlight: flight, Acked: []PacketInfo{p}, Delivery: DeliverySample{Delivered: x.delivered, Ordinal: p.Ordinal, BytesPerSecond: rate, Interval: rtt, Valid: true}}
}

func TestBBRProbeRTTExpiredQueuedSample(t *testing.T) {
	x := newBBRProbeTrace()
	x.cruise()
	// The old model has 100kB/s and 100ms: half BDP is 5000 bytes.
	// Both filters expire on an ACK with an inflated RTT and bandwidth.
	probeRTTAck(x, 11*time.Second, time.Second, 200000, 9000)
	require.EqualValues(t, 5000, x.b.GetCongestionWindow(), "entry uses the pre-update model, not the queued sample")
	require.EqualValues(t, 200000, x.b.PacingRate(), "ProbeRTT pacing gain is one")
	probeRTTAck(x, 2*time.Second, 2*time.Second, 300000, 9000)
	require.EqualValues(t, 5000, x.b.GetCongestionWindow(), "later inflated estimates cannot raise the saved cap")
}

func TestBBRProbeRTTLongRoundGate(t *testing.T) {
	for _, startup := range []bool{false, true} {
		t.Run(map[bool]string{false: "return to Cruise", true: "return to Startup"}[startup], func(t *testing.T) {
			x := newBBRProbeTrace()
			rate := uint64(100000)
			if startup {
				rate = 10000
				probeRTTAck(x, time.Second, time.Second, rate, 100000)
			} else {
				x.cruise()
			}
			cap := protocol.ByteCount(5000)
			probeRTTAck(x, 6*time.Second, 2*time.Second, rate, cap)
			require.Equal(t, cap, x.b.GetCongestionWindow())
			boundary := x.delivered
			// Receipt of a packet registered before the hold is not its new round.
			x.now = x.now.Add(201 * time.Millisecond)
			x.delivered += 1200
			p := PacketInfo{Ordinal: x.ordinal, AckEliciting: true, Delivery: DeliverySnapshot{Delivered: boundary - 1200, Valid: true}}
			e := FeedbackEvent{Time: x.now, HasAck: true, PostInFlight: 0, Acked: []PacketInfo{p}, Delivery: DeliverySample{Delivered: x.delivered, Ordinal: p.Ordinal, BytesPerSecond: rate, Interval: time.Second, Valid: true}}
			x.b.Feedback(e)
			require.Equal(t, cap, x.b.GetCongestionWindow(), "elapsed hold alone cannot complete a long round")
			probeRTTAck(x, 2*time.Second, 2*time.Second, rate, 0)
			require.Greater(t, x.b.GetCongestionWindow(), cap, "both conditions restore the saved window")
			require.Equal(t, startup, x.b.InSlowStart())
		})
	}
}

func TestBBRProbeRTTSizeAndReset(t *testing.T) {
	for _, action := range []string{"size", "reset", "close", "sampling fence"} {
		t.Run(action, func(t *testing.T) {
			x := newBBRProbeTrace()
			x.cruise()
			probeRTTAck(x, 6*time.Second, 200*time.Millisecond, 100000, 0)
			require.EqualValues(t, 5000, x.b.GetCongestionWindow())
			switch action {
			case "size":
				x.b.SetMaxDatagramSize(1400)
				require.EqualValues(t, 5600, x.b.GetCongestionWindow(), "current four-packet floor")
				x.b.SetMaxDatagramSize(1200)
				require.EqualValues(t, 5000, x.b.GetCongestionWindow(), "size updates still apply the saved probe cap")
			case "reset":
				x.b.Reset(1, 1, x.delivered)
				x.b.Feedback(FeedbackEvent{Time: x.now.Add(time.Second), HasAck: true, Delivery: DeliverySample{Delivered: x.delivered + 1200, Valid: true}})
				require.EqualValues(t, 12000, x.b.GetCongestionWindow(), "old feedback cannot restore disposed state")
				require.True(t, x.b.InSlowStart())
				probeRTTAck(x, 20*time.Second, 300*time.Millisecond, 100000, 0)
				require.Greater(t, x.b.GetCongestionWindow(), protocol.ByteCount(5000), "a fresh model has no inherited probe deadline")
			case "close":
				x.b.Close()
				probeRTTAck(x, time.Second, 100*time.Millisecond, 100000, 0)
				require.Zero(t, x.b.GetCongestionWindow())
			case "sampling fence":
				x.b.Sent(SendEvent{Packet: PacketInfo{SampleGeneration: 1, RegistrationValid: true, Delivery: DeliverySnapshot{Delivered: x.delivered}}})
				probeRTTAck(x, time.Second, 100*time.Millisecond, 100000, 0)
				require.Greater(t, x.b.GetCongestionWindow(), protocol.ByteCount(5000), "a fresh qualifying round can finish the same path's probe")
			}
		})
	}
}

func TestBBRProbeRTTSendSideExitGate(t *testing.T) {
	for _, roundDone := range []bool{false, true} {
		t.Run(map[bool]string{false: "no qualifying round", true: "completed round"}[roundDone], func(t *testing.T) {
			x := newBBRProbeTrace()
			x.cruise()
			probeRTTAck(x, 6*time.Second, 200*time.Millisecond, 100000, 0)
			holdStart := x.now
			if roundDone {
				probeRTTAck(x, 100*time.Millisecond, 100*time.Millisecond, 100000, 0)
			}
			for _, offset := range []time.Duration{199 * time.Millisecond, 200 * time.Millisecond, 201 * time.Millisecond} {
				x.b.BeforeSend(holdStart.Add(offset), true)
				if roundDone && offset > 200*time.Millisecond {
					require.False(t, x.b.InProbeRTT())
					require.Greater(t, x.b.GetCongestionWindow(), protocol.ByteCount(5000))
				} else {
					require.True(t, x.b.InProbeRTT(), "send resume uses the same strict time-plus-round gate")
				}
			}
		})
	}
}

func TestBBRProbeRTTRisingBase(t *testing.T) {
	x := newBBRProbeTrace()
	x.cruise()
	// A real increase in propagation delay is eventually learned, but cannot
	// enlarge the probe that first observes expiration of the old minimum.
	for _, want := range []protocol.ByteCount{5000, 5000, 10000} {
		probeRTTAck(x, 5*time.Second+time.Nanosecond, 200*time.Millisecond, 100000, 0)
		require.Equal(t, want, x.b.GetCongestionWindow())
		probeRTTAck(x, 201*time.Millisecond, 200*time.Millisecond, 100000, 0)
		require.False(t, x.b.InProbeRTT())
	}
	t.Run("qualification and cadence", func(t *testing.T) {
		x := newBBRProbeTrace()
		probeRTTAck(x, 100*time.Millisecond, 100*time.Millisecond, 100000, 0)
		stamp := x.now
		// A timer is not an RTT observation, even if it has a RawRTT value.
		x.b.Feedback(FeedbackEvent{Time: stamp.Add(time.Second), RawRTT: time.Nanosecond})
		x.now = stamp
		probeRTTAck(x, 5*time.Second, 200*time.Millisecond, 100000, 0)
		require.False(t, x.b.InProbeRTT(), "five-second scheduling boundary is strict")
		e := probeRTTEvent(x, time.Nanosecond, 200*time.Millisecond, 100000, 0)
		e.RawRTT = 0 // no new eligible measurement; do not re-read recovery's estimate
		x.b.Feedback(e)
		require.True(t, x.b.InProbeRTT())
		require.EqualValues(t, 5000, x.b.GetCongestionWindow())
	})
}

func TestBBRProbeRTTAlreadyInflatedCap(t *testing.T) {
	x := newBBRProbeTrace()
	for range 4 {
		probeRTTAck(x, time.Second, time.Second, 100000, 100000)
	}
	probeRTTAck(x, time.Second, time.Second, 100000, 0)
	// An already inflated measured minimum is not known propagation truth.
	// The safeguard preserves its 50kB half-BDP estimate, not a four-M drain.
	probeRTTAck(x, 6*time.Second, 2*time.Second, 100000, 60000)
	require.True(t, x.b.InProbeRTT())
	require.EqualValues(t, 50000, x.b.probeRTTCap)
	require.False(t, x.b.probeRTTRoundDone)
	probeRTTAck(x, 100*time.Millisecond, 50*time.Millisecond, 100000, 60000)
	require.EqualValues(t, 4800, x.b.GetCongestionWindow(), "a smaller fresh estimate may lower the saved cap")
	probeRTTAck(x, 2*time.Second, 2*time.Second, 200000, 60000)
	require.EqualValues(t, 4800, x.b.GetCongestionWindow(), "inflation cannot undo a smaller target")
}

func TestBBRProbeRTTRestoreBounds(t *testing.T) {
	for _, pending := range []protocol.ByteCount{-1, 9000} {
		t.Run(map[protocol.ByteCount]string{-1: "unknown local debt", 9000: "pending local debt"}[pending], func(t *testing.T) {
			x := newBBRProbeTrace()
			x.cruise()
			e := probeRTTEvent(x, 6*time.Second, 200*time.Millisecond, 100000, 0)
			e.PendingLocal = pending
			x.b.Feedback(e)
			e = probeRTTEvent(x, time.Second, 200*time.Millisecond, 100000, 0)
			e.PendingLocal = pending
			x.b.Feedback(e)
			require.True(t, x.b.InProbeRTT(), "low recovery flight alone cannot start the hold")
			probeRTTAck(x, time.Second, 200*time.Millisecond, 100000, 0)
			require.True(t, x.b.InProbeRTT(), "the hold starts only after pending work fits")
			probeRTTAck(x, 201*time.Millisecond, 200*time.Millisecond, 100000, 0)
			require.False(t, x.b.InProbeRTT())
		})
	}
	t.Run("loss cap and restoration", func(t *testing.T) {
		x := newBBRProbeTrace()
		x.up(t)
		x.lose(20000, SendUnknown)
		x.ackFlight(100000, 20000, 18000, SendUnknown)
		cap := x.b.GetCongestionWindow()
		probeRTTAck(x, 6*time.Second, 200*time.Millisecond, 100000, 0)
		probeRTTAck(x, 201*time.Millisecond, 200*time.Millisecond, 100000, 0)
		require.False(t, x.b.InProbeRTT())
		require.Less(t, x.b.GetCongestionWindow(), cap, "Cruise restoration still respects long-term headroom")
		require.Greater(t, x.b.GetCongestionWindow(), protocol.ByteCount(5000))
	})
}
