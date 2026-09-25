package ackhandler

import (
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

func TestBBRPrivateResetAndClose(t *testing.T) {
	t.Run("0RTT fence before another registration", func(t *testing.T) {
		h := newSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, false, nil, protocol.PerspectiveClient, nil, utils.DefaultLogger, nil)
		b := EnableBBR(h, 1200, func() protocol.ByteCount { return 0 })
		now := monotime.Now()
		sendCongestionTestPacket(h, now, protocol.Encryption0RTT, 1200)
		var last protocol.PacketNumber
		for range 4 {
			last = sendCongestionTestPacket(h, now.Add(time.Millisecond), protocol.Encryption1RTT, 1200)
		}
		h.DropPackets(protocol.Encryption0RTT, now.Add(2*time.Millisecond))
		before, rate := b.GetCongestionWindow(), b.PacingRate()
		_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(last)}, protocol.Encryption1RTT, now.Add(100*time.Millisecond))
		require.NoError(t, err)
		require.True(t, h.congestionEvents.recovery.episode.Active, "real current-path loss entered recovery")
		require.True(t, b.InRecovery(), "feedback after disposal reaches BBR before the next registration")
		require.Equal(t, before, b.GetCongestionWindow(), "old sampling generation adds no delivery growth")
		require.Equal(t, rate, b.PacingRate(), "old sampling generation supplies no measured rate")
		pn := sendCongestionTestPacket(h, now.Add(200*time.Millisecond), protocol.Encryption1RTT, 1200)
		require.True(t, b.InRecovery(), "new registration preserves pending recovery")
		_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pn)}, protocol.Encryption1RTT, now.Add(300*time.Millisecond))
		require.NoError(t, err)
		require.False(t, b.InRecovery(), "fresh receipt crosses the recovery boundary")
		require.Equal(t, before+1200, b.GetCongestionWindow())
	})
	for _, action := range []string{"retry", "path", "close"} {
		t.Run(action, func(t *testing.T) {
			h := newSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, false, nil, protocol.PerspectiveClient, nil, utils.DefaultLogger, nil)
			b := EnableBBR(h, 1200, func() protocol.ByteCount { return 0 })
			now := monotime.Now()
			pn := sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1200)
			_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pn)}, protocol.EncryptionInitial, now.Add(100*time.Millisecond))
			require.NoError(t, err)
			require.EqualValues(t, 13200, b.GetCongestionWindow())
			switch action {
			case "retry":
				h.ResetForRetry(now.Add(200 * time.Millisecond))
			case "path":
				h.MigratedPath(now.Add(200*time.Millisecond), 1280)
			case "close":
				h.CloseDelivery()
			}
			before := b.GetCongestionWindow()
			b.Feedback(congestion.FeedbackEvent{Time: now.Add(time.Second), HasAck: true, RawRTT: time.Nanosecond, Delivery: congestion.DeliverySample{Valid: true, Delivered: 999999, BytesPerSecond: 999999, Interval: time.Second}})
			require.Equal(t, before, b.GetCongestionWindow(), "stale feedback cannot mutate a reset or closed model")
			if action == "close" {
				require.Zero(t, before)
				require.False(t, b.CanSend(0))
				return
			}
			probe := h.PopPacketNumber(protocol.Encryption1RTT)
			h.SentPacket(now.Add(300*time.Millisecond), probe, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, protocol.ECNNon, 1200, false, true)
			_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(probe)}, protocol.Encryption1RTT, now.Add(400*time.Millisecond))
			require.NoError(t, err)
			require.Equal(t, before, b.GetCongestionWindow(), "a path-probe receipt cannot import pre-reset delivered bytes")
			require.Same(t, b, h.congestion)
			if action == "path" {
				require.EqualValues(t, 12800, before)
			} else {
				require.EqualValues(t, 12000, before)
			}
			pn = sendCongestionTestPacket(h, now.Add(time.Second), protocol.EncryptionInitial, 1200)
			_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pn)}, protocol.EncryptionInitial, now.Add(1100*time.Millisecond))
			require.NoError(t, err)
			require.Equal(t, before+1200, b.GetCongestionWindow(), "fresh-generation ACK can grow the fresh model")
		})
	}
}

func TestPublicConstructionStillReno(t *testing.T) {
	for _, perspective := range []protocol.Perspective{protocol.PerspectiveClient, protocol.PerspectiveServer} {
		h := NewSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, false, nil, perspective, nil, utils.DefaultLogger).(*sentPacketHandler)
		require.Nil(t, h.congestionEvents)
		require.EqualValues(t, 38400, h.congestion.GetCongestionWindow())
		_, isBBR := h.congestion.(*congestion.BBRSender)
		require.False(t, isBBR)
	}
}

func TestBBRCEIsNotLoss(t *testing.T) {
	stats := &utils.ConnectionStats{}
	h := newSentPacketHandler(0, 1200, utils.NewRTTStats(), stats, true, false, nil, protocol.PerspectiveClient, nil, utils.DefaultLogger, nil)
	b := EnableBBR(h, 1200, func() protocol.ByteCount { return 0 })
	EnableBBRECN(h, func() (uint64, bool, bool) { return h.congestionEvents.pathGeneration, true, true })
	now := monotime.Now()
	send := func(at monotime.Time) protocol.PacketNumber {
		pn := h.PopPacketNumber(protocol.Encryption1RTT)
		h.SentPacket(at, pn, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, h.ECNMode(true), 1200, false, false)
		return pn
	}
	first := send(now)
	_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(first), ECT0: 1}, protocol.Encryption1RTT, now.Add(100*time.Millisecond))
	require.NoError(t, err)
	require.Equal(t, protocol.ECT0, h.ECNMode(true))
	second := send(now.Add(110 * time.Millisecond))
	beforePackets, beforeBytes := stats.PacketsLost.Load(), stats.BytesLost.Load()
	_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(second), ECT0: 1, ECNCE: 1}, protocol.Encryption1RTT, now.Add(210*time.Millisecond))
	require.NoError(t, err)
	require.False(t, b.InSlowStart())
	require.EqualValues(t, 4800, b.GetCongestionWindow())
	require.False(t, b.InRecovery())
	require.Zero(t, h.congestionEvents.sampler.lost)
	require.Equal(t, beforePackets, stats.PacketsLost.Load())
	require.Equal(t, beforeBytes, stats.BytesLost.Load())
	require.Zero(t, h.bytesInFlight)
	// Sustained all-CE is valid after capability; registration/sample epochs
	// between these isolated sends do not erase the independent CE boundary.
	third := send(now.Add(220 * time.Millisecond))
	_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(third), ECT0: 1, ECNCE: 2}, protocol.Encryption1RTT, now.Add(320*time.Millisecond))
	require.NoError(t, err)
	require.Equal(t, protocol.ECT0, h.ECNMode(true))
	require.Zero(t, h.congestionEvents.sampler.lost)
}
