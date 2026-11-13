package congestion

import (
	"fmt"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
)

func TestBBRInitialWindowAndArithmetic(t *testing.T) {
	for _, tc := range []struct{ size, window protocol.ByteCount }{{1200, 12000}, {1280, 12800}, {1452, 14520}} {
		t.Run(fmt.Sprint(tc.size), func(t *testing.T) {
			b := NewBBRSender(tc.size)
			require.Equal(t, tc.window, b.GetCongestionWindow())
			require.True(t, b.CanSend(tc.window-1))
			require.False(t, b.CanSend(tc.window))
			require.True(t, b.InSlowStart())
		})
	}
	t.Run("invalid rate still acknowledges bytes", func(t *testing.T) {
		b := NewBBRSender(1200)
		b.Feedback(FeedbackEvent{Time: monotime.Time(time.Second), HasAck: true, Delivery: DeliverySample{Delivered: 1200}})
		require.EqualValues(t, 13200, b.GetCongestionWindow())
		require.EqualValues(t, 332710, b.PacingRate(), "no invented rate measurement")
	})
	t.Run("saturation and time order", func(t *testing.T) {
		b := NewBBRSender(1200)
		feedbackRound(b, 1, 1200, math.MaxUint64, SendUnknown, 20000)
		require.EqualValues(t, 13200, b.GetCongestionWindow(), "rate saturation cannot grant flight")
		before := b.GetCongestionWindow()
		feedbackRound(b, 0, 1200, 1, SendUnknown, 0)
		require.Equal(t, before, b.GetCongestionWindow(), "nonmonotonic ACK rejected")
		b.SetMaxDatagramSize(1280)
		require.Equal(t, before, b.GetCongestionWindow(), "packet size does not rescale byte history")
		require.Panics(t, func() { b.SetMaxDatagramSize(0) })
	})
}

func TestBBRStartupPlateau(t *testing.T) {
	b := NewBBRSender(1200)
	for i := uint64(1); i <= 3; i++ {
		feedbackRound(b, i, i*1200, 100000, SendUnknown, 20000)
		require.True(t, b.InSlowStart())
	}
	feedbackRound(b, 4, 4800, 100000, SendUnknown, 20000)
	require.False(t, b.InSlowStart())
	require.EqualValues(t, 50000, b.PacingRate(), "Drain gain is one half, before the emission margin")
}

func TestBBRStartupLimitedSamples(t *testing.T) {
	for _, reason := range []SendLimitation{SendApplicationLimited, SendFlowControlLimited, SendProbeRTTLimited} {
		t.Run(fmt.Sprint(reason), func(t *testing.T) {
			b := NewBBRSender(1200)
			feedbackRound(b, 1, 1200, 100000, SendUnknown, 20000)
			for i := uint64(2); i <= 6; i++ {
				feedbackRound(b, i, i*1200, 50000, reason, 20000)
			}
			require.True(t, b.InSlowStart())
			feedbackRound(b, 7, 8400, 200000, reason, 20000)
			require.EqualValues(t, 554517, b.PacingRate(), "a limited sample may raise the maximum")
			feedbackRound(b, 8, 9600, 1000, reason, 20000)
			require.EqualValues(t, 554517, b.PacingRate(), "a limited sample cannot lower the maximum")
		})
	}
}

func TestBBRDrainFlightAndRoundExit(t *testing.T) {
	for _, sampled := range []bool{false, true} {
		t.Run(fmt.Sprintf("same ACK Cruise sampled=%v", sampled), func(t *testing.T) {
			x := newBBRProbeTrace()
			for range 4 {
				x.ack(100000, 20000, SendUnknown)
			}
			x.b.inflightLong = 12000
			round := x.b.round
			if sampled {
				x.ack(100000, 9000, SendUnknown)
			} else {
				x.now = x.now.Add(100 * time.Millisecond)
				x.delivered += 1200
				x.b.Feedback(FeedbackEvent{Time: x.now, HasAck: true, PostInFlight: 9000, Delivery: DeliverySample{Delivered: x.delivered}})
			}
			require.Equal(t, bbrCruise, x.b.phase)
			require.EqualValues(t, 100000, x.b.PacingRate())
			require.EqualValues(t, 10200, x.b.GetCongestionWindow(), "same-ACK Cruise headroom")
			require.Equal(t, x.delivered, x.b.nextRound)
			require.Zero(t, x.b.roundsSinceProbe, "entry resets the wait after counting this ACK round")
			if sampled {
				require.Equal(t, round+1, x.b.round, "only the existing packet-round boundary advances")
				require.EqualValues(t, 1, x.b.cycle, "StartRound preserves the boundary consumed by AdaptLongTermModel")
				require.Equal(t, bbrAcksInit, x.b.ackPhase)
			} else {
				require.Equal(t, round, x.b.round)
				require.Zero(t, x.b.cycle, "ACK facts alone cannot advance filter cycles")
				require.Equal(t, bbrAcksStopping, x.b.ackPhase)
			}
		})
	}
	for _, zero := range []bool{false, true} {
		t.Run(fmt.Sprintf("invalid Drain clock zero=%v", zero), func(t *testing.T) {
			x := newBBRProbeTrace()
			x.now = monotime.Time(10 * time.Second)
			for range 4 {
				x.ack(100000, 20000, SendUnknown)
			}
			require.Equal(t, bbrDrain, x.b.phase)
			last := x.b.lastEvent
			now := last.Add(-50 * time.Millisecond)
			if zero {
				now = 0
			}
			e := FeedbackEvent{Time: now, HasAck: true, PostInFlight: 9000, Delivery: DeliverySample{Delivered: x.delivered + 1200}}
			x.b.Feedback(e)
			require.Equal(t, bbrDown, x.b.phase)
			require.Equal(t, last, x.b.cycleStamp, "phase entry cannot use rejected clock evidence")
			e.Time = last.Add(2*time.Second - time.Nanosecond)
			e.Delivery.Delivered += 1200
			x.b.Feedback(e)
			require.Equal(t, bbrCruise, x.b.phase, "the next admitted ACK cannot expire the wait early")
		})
	}

	t.Run("two-quantum offload floor", func(t *testing.T) {
		b := NewBBRSender(1200)
		b.phase, b.bandwidth, b.minimumRTT, b.window = bbrDrain, 10000000, 100*time.Microsecond, 40000
		require.EqualValues(t, 4950, b.quantum())
		b.Feedback(FeedbackEvent{Time: monotime.Time(time.Second), HasAck: true, PostInFlight: 20000, Delivery: DeliverySample{Delivered: 1200}})
		require.EqualValues(t, 9900, b.GetCongestionWindow(), "ACK target includes two offload quanta")
		b.Feedback(FeedbackEvent{Time: monotime.Time(2 * time.Second), HasAck: true, PostInFlight: 8000, Delivery: DeliverySample{Delivered: 1200}})
		require.Equal(t, bbrDown, b.phase, "8000 bytes fits the Drain target of 9900")
	})
	t.Run("aggregate before four-packet floor", func(t *testing.T) {
		b := NewBBRSender(1200)
		for i := uint64(1); i <= 4; i++ {
			feedbackRound(b, i, i*1200, 12000, SendUnknown, 20000)
		}
		require.EqualValues(t, 4800, b.GetCongestionWindow(), "2*1200 BDP + 1200 aggregation remains below four M")
	})

	t.Run("Cruise loss retains seventy percent", func(t *testing.T) {
		x := newBBRProbeTrace()
		for range 3 {
			x.ack(100000, 20000, SendUnknown)
		}
		x.ack(100000, 0, SendUnknown)
		for range 2 {
			x.ack(20000, 9000, SendUnknown)
		}
		x.lose(20000, SendUnknown)
		x.ack(20000, 9000, SendUnknown)
		require.EqualValues(t, 70000, x.b.PacingRate())
		require.EqualValues(t, 13440, x.b.GetCongestionWindow())
		x.lose(20000, SendUnknown)
		x.ack(20000, 9000, SendUnknown)
		require.EqualValues(t, 49000, x.b.PacingRate(), "a distinct loss round compounds the seventy-percent bandwidth bound")
		require.EqualValues(t, 9408, x.b.GetCongestionWindow(), "the established inflight bound also compounds")
		x.b.Feedback(FeedbackEvent{Time: x.now.Add(time.Millisecond), HasAck: true, RawRTT: 10 * time.Millisecond, Delivery: DeliverySample{Delivered: x.delivered}})
		require.EqualValues(t, 4800, x.b.GetCongestionWindow(), "smaller BDP caps the window, not the retained loss bound")
	})

	for _, tc := range []struct {
		name   string
		flight protocol.ByteCount
	}{{"flight", 10000}, {"rounds", 20000}} {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBBRSender(1200)
			for i := uint64(1); i <= 4; i++ {
				feedbackRound(b, i, i*1200, 100000, SendUnknown, 20000)
			}
			feedbackRound(b, 5, 6000, 100000, SendUnknown, tc.flight)
			if tc.name == "rounds" {
				require.EqualValues(t, 50000, b.PacingRate())
				for i := uint64(6); i <= 7; i++ {
					feedbackRound(b, i, i*1200, 100000, SendUnknown, tc.flight)
				}
				require.EqualValues(t, 50000, b.PacingRate(), "draft's strict round boundary")
				feedbackRound(b, 8, 9600, 100000, SendUnknown, tc.flight)
			}
			if tc.name == "flight" {
				require.EqualValues(t, 100000, b.PacingRate(), "drained entry reaches Cruise on the same ACK")
			} else {
				require.EqualValues(t, 90000, b.PacingRate(), "high flight retains Probe Down")
			}
		})
	}
}

func TestBBRStartupLossRanges(t *testing.T) {
	for _, tc := range []struct {
		name        string
		count       int
		step        protocol.PacketNumber
		length      protocol.ByteCount
		wantStartup bool
	}{
		{"six ranges", 6, 2, 1200, false},
		{"five ranges", 5, 2, 1200, true},
		{"contiguous burst", 6, 1, 1200, true},
		{"exact two percent", 6, 2, 200, true},
		{"originless", 6, 2, 1200, false},
		{"backdated", 6, 2, 1200, false},
		{"invalid sample exit", 6, 2, 1200, false},
		{"before recovery boundary", 6, 2, 1200, true},
		{"registration fence", 6, 2, 1200, false},
		{"completed recovery across fence", 6, 2, 1200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBBRSender(1200)
			feedbackRound(b, 1, 1200, 100000, SendApplicationLimited, 60000)
			var lost []PacketInfo
			for i := 0; i < tc.count; i++ {
				lost = append(lost, PacketInfo{Space: protocol.Encryption1RTT, PacketNumber: protocol.PacketNumber(i) * tc.step, Ordinal: uint64(i + 2), Length: tc.length, AckEliciting: true, RegistrationValid: true, Delivery: DeliverySnapshot{Valid: true, PostInFlight: 60000}})
			}
			if tc.name == "originless" {
				for i := range lost {
					lost[i].Delivery.Valid = false
				}
			}
			if tc.name == "registration fence" {
				b.Feedback(FeedbackEvent{Time: monotime.Time(2050 * time.Millisecond), Lost: lost[:3], Delivery: DeliverySample{Delivered: 1200}, RecoveryEpisode: RecoveryEpisode{ID: 1, Entered: true, Active: true, Boundary: 7}})
				b.Sent(SendEvent{Packet: PacketInfo{Ordinal: 8, SampleGeneration: 1, RegistrationValid: true, Delivery: DeliverySnapshot{Delivered: 1200}}})
				require.True(t, b.InRecovery(), "sampling fences cannot erase the active recovery episode")
				lost = lost[3:]
			}
			if tc.name == "backdated" {
				b.Feedback(FeedbackEvent{Time: monotime.Time(3 * time.Second), Lost: lost[:1], Delivery: DeliverySample{Delivered: 1200}, RecoveryEpisode: RecoveryEpisode{ID: 1, Entered: true, Active: true, Boundary: 7}})
				lost = lost[1:]
			}
			before := b.GetCongestionWindow()
			boundary := uint64(7)
			if tc.name == "before recovery boundary" {
				boundary = 20
			}
			b.Feedback(FeedbackEvent{SampleGeneration: b.sampleGeneration, HasAck: tc.name == "backdated", Time: monotime.Time(2100 * time.Millisecond), Lost: lost, PriorInFlight: 60000, PostInFlight: 50000, Delivery: DeliverySample{Delivered: 1200, Lost: uint64(tc.length) * uint64(tc.count)}, RecoveryEpisode: RecoveryEpisode{ID: 1, Entered: tc.name != "backdated" && tc.name != "registration fence", Active: true, Boundary: boundary}})
			require.Equal(t, before, b.GetCongestionWindow(), "loss facts do not perform an extra immediate window cut")
			require.True(t, b.InSlowStart(), "loss alone cannot complete a recovery round")
			if tc.name == "invalid sample exit" || tc.name == "completed recovery across fence" {
				b.Feedback(FeedbackEvent{Time: monotime.Time(2200 * time.Millisecond), HasAck: true, Delivery: DeliverySample{Delivered: 1200}, RecoveryEpisode: RecoveryEpisode{ID: 1, Exited: true}})
				require.False(t, b.InRecovery())
			}
			if tc.name == "completed recovery across fence" {
				b.Sent(SendEvent{Packet: PacketInfo{Ordinal: 8, SampleGeneration: 1, RegistrationValid: true, Delivery: DeliverySnapshot{Delivered: 1200}}})
			}
			feedbackRound(b, 20, 2400, 100000, SendApplicationLimited, 20000)
			require.Equal(t, tc.wantStartup, b.InSlowStart())
			if !tc.wantStartup {
				require.EqualValues(t, 50000, b.PacingRate())
			}
			if tc.name == "six ranges" {
				p := PacketInfo{Ordinal: 21, Length: 1200, AckEliciting: true, RegistrationValid: true, Delivery: DeliverySnapshot{Delivered: 2400, Lost: 7200, PostInFlight: 18000, Valid: true}}
				b.Sent(SendEvent{Packet: p})
				b.Feedback(FeedbackEvent{Time: monotime.Time(4 * time.Second), HasAck: true, Acked: []PacketInfo{p}, PostInFlight: 20000, Delivery: DeliverySample{Delivered: 3600, Lost: 7200, Ordinal: 21, BytesPerSecond: 100000, Interval: 100 * time.Millisecond, Valid: true}})
				require.Equal(t, bbrDrain, b.phase)
				require.EqualValues(t, 18000, b.inflightLong, "safe Drain feedback raises the finite Startup-loss cap")
			}
		})
	}
	t.Run("model floor differs from sampling high-water mark", func(t *testing.T) {
		b := NewBBRSender(1200)
		b.Reset(0, 3, 1200)
		b.Sent(SendEvent{Packet: PacketInfo{SampleGeneration: 4, RegistrationValid: true, Delivery: DeliverySnapshot{Delivered: 1200}}})
		old := FeedbackEvent{SampleGeneration: 2, Time: monotime.Time(time.Second), HasAck: true, RawRTT: time.Nanosecond, Delivery: DeliverySample{Delivered: 99999, Valid: true}, RecoveryEpisode: RecoveryEpisode{Entered: true, Active: true}}
		b.Feedback(old)
		require.False(t, b.InRecovery(), "pre-reset event is excluded")
		old.SampleGeneration = 3
		b.Feedback(old)
		require.True(t, b.InRecovery(), "current-model recovery survives an old sample fence")
		require.EqualValues(t, 12000, b.GetCongestionWindow(), "stale sample cannot grow the window")
		require.Zero(t, b.minimumRTT, "stale sample cannot supply RTT")
	})
}

// Each ACK covers a fresh packet sent after the preceding ACK. These are value
// inputs at the reducer seam; transport integration separately uses recovery.
func feedbackRound(b *BBRSender, ordinal, delivered, rate uint64, limited SendLimitation, flight protocol.ByteCount) {
	feedbackRoundRTT(b, ordinal, delivered, rate, limited, flight, 100*time.Millisecond)
}

func feedbackRoundRTT(b *BBRSender, ordinal, delivered, rate uint64, limited SendLimitation, flight protocol.ByteCount, rtt time.Duration) {
	// Keep round/loss tests below the independent five-second ProbeRTT
	// schedule, while retaining ordinal 1 at 2s for explicit ordering cases.
	now := monotime.Time(1900*time.Millisecond) + monotime.Time(ordinal)*monotime.Time(100*time.Millisecond)
	p := PacketInfo{PathGeneration: b.pathGeneration, SampleGeneration: b.sampleGeneration, Ordinal: ordinal, Length: 1200, AckEliciting: true, RegistrationValid: true, SendTime: now.Add(-rtt), Delivery: DeliverySnapshot{Delivered: delivered - 1200, Valid: true}}
	b.Sent(SendEvent{Packet: p})
	b.Feedback(FeedbackEvent{PathGeneration: b.pathGeneration, SampleGeneration: b.sampleGeneration, Time: now, HasAck: true, RawRTT: rtt, PostInFlight: flight, Acked: []PacketInfo{p}, Delivery: DeliverySample{Delivered: delivered, Ordinal: ordinal, BytesPerSecond: rate, Interval: 100 * time.Millisecond, Limited: limited, Valid: true}})
}

func TestBBRPacketCeilingSurvivesReset(t *testing.T) {
	standard := NewBBRSender(1232)
	require.Panics(t, func() { standard.SetMaxDatagramSize(1453) })
	jumbo := NewBBRSenderWithPacketCeiling(1232, 8972)
	require.NotPanics(t, func() { jumbo.SetMaxDatagramSize(8957) })
	jumbo.Reset(1, 1, 0)
	require.NotPanics(t, func() { jumbo.SetMaxDatagramSize(8972) })
	require.Panics(t, func() { jumbo.SetMaxDatagramSize(8973) })
}

// Recovery anchors no timing-ineligible ACK, such as a cross-path or locally
// excluded one. Such feedback settles delivery and loss but supplies no
// aggregation evidence, so the window stays near its initial target (#1329).
func TestBBRTimingIneligibleFeedbackBoundsWindow(t *testing.T) {
	for _, tc := range []struct {
		name        string
		lost        int
		checkpoints []int
	}{
		{"loss-free", 0, []int{40, 400}},
		{"half lost", 5, []int{400, 2000}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBBRSender(1200)
			bound := 2 * b.initialWindow // Startup's initial-window allowance only
			var ordinal, delivered uint64
			var windows []protocol.ByteCount
			for round := 1; round <= tc.checkpoints[len(tc.checkpoints)-1]; round++ {
				now := monotime.Time(2*time.Second) + monotime.Time(round)*monotime.Time(100*time.Millisecond)
				packets := make([]PacketInfo, 10)
				for i := range packets {
					ordinal++
					packets[i] = PacketInfo{Space: protocol.Encryption1RTT, Ordinal: ordinal, PacketNumber: protocol.PacketNumber(ordinal), Length: 1200, AckEliciting: true, InFlight: true, RegistrationValid: true, TimingExcluded: true, SendTime: now.Add(-50 * time.Millisecond), Delivery: DeliverySnapshot{Delivered: delivered, PostInFlight: 12000}}
					b.Sent(SendEvent{Packet: packets[i], PostInFlight: 12000})
				}
				acked := packets[:10-tc.lost]
				delivered += uint64(1200 * len(acked))
				// No anchor ordinal, no valid sample and no RTT: timing-excluded feedback.
				b.Feedback(FeedbackEvent{Time: now, HasAck: true, PriorInFlight: 12000, Acked: acked, Lost: packets[10-tc.lost:], Delivery: DeliverySample{Delivered: delivered}})
				if slices.Contains(tc.checkpoints, round) {
					windows = append(windows, b.GetCongestionWindow())
				}
			}
			for i, w := range windows {
				require.LessOrEqual(t, w, bound, "round %d; windows at rounds %v: %v", tc.checkpoints[i], tc.checkpoints, windows)
			}
		})
	}
}

// The sampler anchors an event before it checks the anchor's snapshot or the
// interval, so the first ACK of a connection is anchored with an invalid sample.
// Anchoring alone, with a valid clock, admits aggregation evidence (#1338).
func TestBBRAnchoredInvalidSampleAccruesAggregation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ordinal uint64
	}{{"anchored", 1}, {"unanchored control", 0}} {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBBRSender(1200)
			// A registration before any send origin carries an invalid snapshot.
			p := PacketInfo{Ordinal: 1, Length: 1200, AckEliciting: true, RegistrationValid: true, SendTime: monotime.Time(1900 * time.Millisecond)}
			b.Sent(SendEvent{Packet: p})
			now := monotime.Time(2 * time.Second)
			b.Feedback(FeedbackEvent{Time: now, HasAck: true, RawRTT: 100 * time.Millisecond, Acked: []PacketInfo{p}, Delivery: DeliverySample{Delivered: 1200, Ordinal: tc.ordinal, Interval: 100 * time.Millisecond}})
			require.EqualValues(t, 1200, b.delivered)
			if tc.ordinal == 0 {
				require.Zero(t, b.aggregationStart, "an unanchored event accrues nothing")
				require.Zero(t, b.aggregationMaximum())
				return
			}
			require.Equal(t, now, b.aggregationStart, "an invalid sample cannot suppress anchored accrual")
			require.EqualValues(t, 1200, b.aggregationDelivered)
			require.EqualValues(t, 1200, b.aggregationMaximum())
		})
	}
}

// An anchored ACK whose clock is backdated or zero settles delivery but accrues
// no aggregation evidence, as the design rejects nonmonotonic inputs (#1338).
// The window may still grow from the existing maximum, so it is not bounded here.
func TestBBRBackdatedAnchoredAckSkipsAggregation(t *testing.T) {
	for _, zero := range []bool{false, true} {
		name := "backdated"
		if zero {
			name = "zero clock"
		}
		t.Run(name, func(t *testing.T) {
			b := NewBBRSender(1200)
			// Valid snapshots and samples: the setup does not rely on anchored
			// accrual from invalid evidence.
			feedbackRound(b, 1, 1200, 100000, SendUnknown, 20000)
			feedbackRound(b, 2, 2400, 100000, SendUnknown, 20000)
			require.EqualValues(t, 1200, b.aggregationMaximum())
			start, accrued, slots, last := b.aggregationStart, b.aggregationDelivered, b.aggregation, b.lastEvent
			now := start.Add(-500 * time.Millisecond)
			if zero {
				now = 0
			}
			require.Less(t, now, last)
			p := PacketInfo{PathGeneration: b.pathGeneration, SampleGeneration: b.sampleGeneration, Ordinal: 3, Length: 1200, AckEliciting: true, RegistrationValid: true, SendTime: last.Add(-100 * time.Millisecond), Delivery: DeliverySnapshot{Delivered: 2400, Valid: true}}
			b.Sent(SendEvent{Packet: p})
			b.Feedback(FeedbackEvent{PathGeneration: b.pathGeneration, SampleGeneration: b.sampleGeneration, Time: now, HasAck: true, RawRTT: 100 * time.Millisecond, PostInFlight: 20000, Acked: []PacketInfo{p}, Delivery: DeliverySample{Delivered: 14400, Ordinal: 3, BytesPerSecond: 100000, Interval: 100 * time.Millisecond, Valid: true}})
			require.EqualValues(t, 14400, b.delivered, "delivery still settles")
			require.Equal(t, last, b.lastEvent)
			require.Equal(t, start, b.aggregationStart, "the epoch cannot move to a rejected clock")
			require.Equal(t, accrued, b.aggregationDelivered)
			require.Equal(t, slots, b.aggregation)
			require.EqualValues(t, 1200, b.aggregationMaximum())
		})
	}
}
