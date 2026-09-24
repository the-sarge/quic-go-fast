package congestion

import (
	"fmt"
	"math"
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
	t.Run("Cruise loss retains seventy percent", func(t *testing.T) {
		b := NewBBRSender(1200)
		for i := uint64(1); i <= 4; i++ {
			feedbackRound(b, i, i*1200, 100000, SendUnknown, 0)
		}
		for i := uint64(5); i <= 6; i++ {
			feedbackRound(b, i, i*1200, 20000, SendUnknown, 20000)
		}
		lost := PacketInfo{Space: protocol.Encryption1RTT, Ordinal: 7, Length: 1200, AckEliciting: true, RegistrationValid: true, Delivery: DeliverySnapshot{Valid: true, PostInFlight: 20000}}
		b.Feedback(FeedbackEvent{Time: monotime.Time(7100 * time.Millisecond), Lost: []PacketInfo{lost}, Delivery: DeliverySample{Delivered: 7200}, RecoveryEpisode: RecoveryEpisode{Entered: true, Active: true}})
		feedbackRound(b, 8, 8400, 20000, SendUnknown, 20000)
		require.EqualValues(t, 70000, b.PacingRate())
		require.EqualValues(t, 12600, b.GetCongestionWindow())
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
			require.EqualValues(t, 100000, b.PacingRate(), "terminal Cruise gain is one")

			if tc.name == "flight" {
				feedbackRound(b, 6, 7200, 100000, SendUnknown, 20000)
			} else {
				feedbackRound(b, 9, 10800, 100000, SendUnknown, 20000)
			}
			require.EqualValues(t, 100000, b.PacingRate(), "B1 does not start another probe")
			if tc.name == "flight" {
				require.EqualValues(t, 19200, b.GetCongestionWindow())
			} else {
				require.EqualValues(t, 21200, b.GetCongestionWindow(), "two BDP plus one ACK of aggregation")
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBBRSender(1200)
			feedbackRound(b, 1, 1200, 100000, SendApplicationLimited, 60000)
			var lost []PacketInfo
			for i := 0; i < tc.count; i++ {
				lost = append(lost, PacketInfo{Space: protocol.Encryption1RTT, PacketNumber: protocol.PacketNumber(i) * tc.step, Ordinal: uint64(i + 2), Length: tc.length, AckEliciting: true, RegistrationValid: true, Delivery: DeliverySnapshot{Valid: true, PostInFlight: 60000}})
			}
			b.Feedback(FeedbackEvent{Time: monotime.Time(2100 * time.Millisecond), Lost: lost, PriorInFlight: 60000, PostInFlight: 50000, Delivery: DeliverySample{Delivered: 1200, Lost: uint64(tc.length) * uint64(tc.count)}, RecoveryEpisode: RecoveryEpisode{ID: 1, Entered: true, Active: true}})
			require.True(t, b.InSlowStart(), "loss alone cannot complete a recovery round")
			feedbackRound(b, 20, 2400, 100000, SendApplicationLimited, 20000)
			require.Equal(t, tc.wantStartup, b.InSlowStart())
			if !tc.wantStartup {
				require.EqualValues(t, 50000, b.PacingRate())
			}
		})
	}
}

// Each ACK covers a fresh packet sent after the preceding ACK. These are value
// inputs at the reducer seam; transport integration separately uses recovery.
func feedbackRound(b *BBRSender, ordinal, delivered, rate uint64, limited SendLimitation, flight protocol.ByteCount) {
	now := monotime.Time(ordinal+1) * monotime.Time(time.Second)
	p := PacketInfo{Ordinal: ordinal, Length: 1200, AckEliciting: true, RegistrationValid: true, SendTime: now.Add(-100 * time.Millisecond), Delivery: DeliverySnapshot{Delivered: delivered - 1200, Valid: true}}
	b.Sent(SendEvent{Packet: p})
	b.Feedback(FeedbackEvent{Time: now, HasAck: true, RawRTT: 100 * time.Millisecond, PostInFlight: flight, Acked: []PacketInfo{p}, Delivery: DeliverySample{Delivered: delivered, Ordinal: ordinal, BytesPerSecond: rate, Interval: 100 * time.Millisecond, Limited: limited, Valid: true}})
}
