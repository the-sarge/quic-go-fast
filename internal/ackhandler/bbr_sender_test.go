package ackhandler

import (
	"fmt"
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
	for _, limitation := range []congestion.SendLimitation{congestion.SendUnknown, congestion.SendApplicationLimited, congestion.SendFlowControlLimited} {
		t.Run(fmt.Sprintf("limitation after registration %d", limitation), func(t *testing.T) {
			h := newSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, false, nil, protocol.PerspectiveClient, nil, utils.DefaultLogger, nil)
			b := EnableBBR(h, 1200, func() protocol.ByteCount { return 0 })
			EnableBBRECN(h, func() (uint64, bool, bool) { return h.congestionEvents.pathGeneration, true, true })
			now := monotime.Now()
			var pending []protocol.PacketNumber
			var receipts, ce uint64
			send := func() {
				pn := h.PopPacketNumber(protocol.Encryption1RTT)
				h.SentPacket(now, pn, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, h.ECNMode(true), 1200, false, false)
				pending = append(pending, pn)
			}
			send()
			send() // Keep live delivery evidence between rounds.
			round := func(n int, mark bool, observe congestion.SendLimitation) {
				for range n {
					send()
				}
				if observe != congestion.SendUnknown {
					h.ObserveDeliveryLimitation(observe)
				}
				receipts += uint64(n)
				if mark {
					ce++
				}
				now = now.Add(100 * time.Millisecond)
				_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pending[:n]...), ECT0: receipts - ce, ECNCE: ce}, protocol.Encryption1RTT, now)
				require.NoError(t, err)
				pending = pending[n:]
			}
			for range 5 {
				round(8, false, congestion.SendUnknown)
			}
			round(2, true, congestion.SendUnknown)
			for range 5 {
				round(2, false, congestion.SendUnknown)
			}
			beforeRate, beforeWindow := b.PacingRate(), b.GetCongestionWindow()
			round(2, false, limitation) // Observation follows the final registration.
			if limitation == congestion.SendUnknown {
				require.Greater(t, b.PacingRate(), beforeRate, "control proves this is a release boundary")
				return
			}
			require.Equal(t, beforeRate, b.PacingRate(), "an observed limited round cannot raise the rate cap")
			round(2, false, congestion.SendUnknown)
			require.Equal(t, beforeWindow, b.GetCongestionWindow(), "a later ACK cannot expose an improperly raised flight cap")
		})
	}

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

func TestBBRPersistentRestartOldAckExcluded(t *testing.T) {
	for _, lateFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(lateFirst), func(t *testing.T) {
			h := newSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, false, nil, protocol.PerspectiveClient, nil, utils.DefaultLogger, nil)
			b := EnableBBR(h, 1200, func() protocol.ByteCount { return 0 })
			now := monotime.Now()
			pn := sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1200)
			acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(10*time.Millisecond), pn)
			now = now.Add(20 * time.Millisecond)
			first := sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1200)
			last := sendCongestionTestPacket(h, now.Add(time.Second), protocol.EncryptionHandshake, 1200)
			i := sendCongestionTestPacket(h, now.Add(2*time.Second), protocol.EncryptionInitial, 1200)
			j := sendCongestionTestPacket(h, now.Add(2*time.Second), protocol.EncryptionHandshake, 1200)
			acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(2010*time.Millisecond), i)
			generation := h.congestionEvents.sampleGeneration
			acknowledgeRecoveryPacket(t, h, protocol.EncryptionHandshake, now.Add(2020*time.Millisecond), j)
			require.EqualValues(t, 2400, b.GetCongestionWindow())
			require.Greater(t, h.congestionEvents.sampleGeneration, generation, "persistent restart fences transport snapshots")
			rate := b.PacingRate()
			if lateFirst {
				acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(2025*time.Millisecond), first)
				acknowledgeRecoveryPacket(t, h, protocol.EncryptionHandshake, now.Add(2026*time.Millisecond), last)
			}
			h.PrepareBBRSend(now.Add(2030 * time.Millisecond))
			b.SetMaxDatagramSize(1200)
			require.EqualValues(t, 2400, b.GetCongestionWindow())
			require.Equal(t, rate, b.PacingRate(), "old receipts cannot seed the fresh model")
			fresh := sendCongestionTestPacket(h, now.Add(2040*time.Millisecond), protocol.EncryptionHandshake, 1200)
			acknowledgeRecoveryPacket(t, h, protocol.EncryptionHandshake, now.Add(2050*time.Millisecond), fresh)
			require.EqualValues(t, 4800, b.GetCongestionWindow())
			require.True(t, b.InSlowStart())
		})
	}
}

func recoveryPolicyHandler(t *testing.T) (*sentPacketHandler, *congestion.BBRSender, monotime.Time) {
	t.Helper()
	h := newSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, false, nil, protocol.PerspectiveClient, nil, utils.DefaultLogger, nil)
	b := EnableBBR(h, 1200, func() protocol.ByteCount { return 0 })
	now := monotime.Now()
	for range 6 {
		var packets []protocol.PacketNumber
		for range 10 {
			packets = append(packets, sendCongestionTestPacket(h, now, protocol.Encryption1RTT, 1200))
		}
		now = now.Add(100 * time.Millisecond)
		acknowledgeRecoveryPacket(t, h, protocol.Encryption1RTT, now, packets...)
	}
	require.False(t, b.InSlowStart())
	return h, b, now
}

func TestBBRUndoMissingAndSuperseded(t *testing.T) {
	for _, mode := range []string{"all", "mixed", "expired", "superseded"} {
		t.Run(mode, func(t *testing.T) {
			h, b, now := recoveryPolicyHandler(t)
			saved := b.GetCongestionWindow()
			a := sendCongestionTestPacket(h, now, protocol.Encryption1RTT, 1200)
			c := sendCongestionTestPacket(h, now, protocol.Encryption1RTT, 1200)
			carrier := sendCongestionTestPacket(h, now.Add(200*time.Millisecond), protocol.Encryption1RTT, 1200)
			acknowledgeRecoveryPacket(t, h, protocol.Encryption1RTT, now.Add(300*time.Millisecond), carrier)
			require.True(t, b.InRecovery())
			// Exit through a newer real registration, without proving the lost members.
			next := sendCongestionTestPacket(h, now.Add(310*time.Millisecond), protocol.Encryption1RTT, 1200)
			acknowledgeRecoveryPacket(t, h, protocol.Encryption1RTT, now.Add(410*time.Millisecond), next)
			require.False(t, b.InRecovery())
			if mode == "expired" {
				h.ExpireDelivery(now.Add(2 * time.Second))
			}
			if mode == "superseded" {
				sendCongestionTestPacket(h, now.Add(420*time.Millisecond), protocol.Encryption1RTT, 1200)
				next = sendCongestionTestPacket(h, now.Add(620*time.Millisecond), protocol.Encryption1RTT, 1200)
				acknowledgeRecoveryPacket(t, h, protocol.Encryption1RTT, now.Add(720*time.Millisecond), next)
			}
			receipts := []protocol.PacketNumber{a, c}
			if mode == "mixed" {
				receipts = receipts[:1]
			}
			at := now.Add(430 * time.Millisecond)
			if mode == "superseded" {
				at = now.Add(730 * time.Millisecond)
			}
			if mode == "expired" {
				at = now.Add(2010 * time.Millisecond)
			}
			acknowledgeRecoveryPacket(t, h, protocol.Encryption1RTT, at, receipts...)
			if mode == "all" {
				require.GreaterOrEqual(t, b.GetCongestionWindow(), saved, "complete retained episode restores saved window")
			} else {
				require.Less(t, b.GetCongestionWindow(), saved, "unproven or superseded episode cannot undo loss bounds")
			}
		})
	}
}

func TestBBRPTOAndRetryPolicy(t *testing.T) {
	for _, mode := range []string{"PTO", "Retry"} {
		t.Run(mode, func(t *testing.T) {
			h, b, now := recoveryPolicyHandler(t)
			h.DropPackets(protocol.EncryptionHandshake, now)
			sendCongestionTestPacket(h, now, protocol.Encryption1RTT, 1200)
			before := b.GetCongestionWindow()
			if mode == "PTO" {
				require.NoError(t, h.OnLossDetectionTimeout(h.GetLossDetectionTimeout()))
				require.True(t, h.QueueProbePacketAt(protocol.Encryption1RTT, h.GetLossDetectionTimeout()))
				require.Equal(t, before, b.GetCongestionWindow())
				require.False(t, b.InRecovery())
			} else {
				h.ResetForRetry(now.Add(time.Millisecond))
				require.EqualValues(t, 12000, b.GetCongestionWindow())
				require.True(t, b.InSlowStart())
				require.False(t, b.InRecovery())
			}
		})
	}
}

func TestBBRFullLifecycleDisposal(t *testing.T) {
	for _, mode := range []string{"key discard", "0RTT", "Retry", "path", "close"} {
		t.Run(mode, func(t *testing.T) {
			h, b, now := recoveryPolicyHandler(t)
			level := protocol.EncryptionHandshake
			if mode == "0RTT" {
				level = protocol.Encryption0RTT
			}
			lost := sendCongestionTestPacket(h, now, level, 1200)
			carrier := sendCongestionTestPacket(h, now.Add(200*time.Millisecond), level, 1200)
			ackLevel := level
			if level == protocol.Encryption0RTT {
				ackLevel = protocol.Encryption1RTT
			}
			acknowledgeRecoveryPacket(t, h, ackLevel, now.Add(300*time.Millisecond), carrier)
			generation := h.congestionEvents.sampleGeneration
			switch mode {
			case "key discard", "0RTT":
				h.DropPackets(level, now.Add(310*time.Millisecond))
			case "Retry":
				h.ResetForRetry(now.Add(310 * time.Millisecond))
			case "path":
				h.MigratedPath(now.Add(310*time.Millisecond), 1280)
			case "close":
				h.CloseDelivery()
			}
			before, rate := b.GetCongestionWindow(), b.PacingRate()
			if mode == "key discard" || mode == "0RTT" {
				if mode == "0RTT" {
					acknowledgeRecoveryPacket(t, h, ackLevel, now.Add(320*time.Millisecond), lost)
				}
				next := sendCongestionTestPacket(h, now.Add(330*time.Millisecond), protocol.Encryption1RTT, 1200)
				acknowledgeRecoveryPacket(t, h, protocol.Encryption1RTT, now.Add(430*time.Millisecond), next)
				require.False(t, b.InRecovery())
				require.LessOrEqual(t, b.GetCongestionWindow(), before, "disposed membership cannot restore saved loss bounds")
				return
			}
			// A copied pre-reset event cannot regain authority after model disposal.
			b.Feedback(congestion.FeedbackEvent{SampleGeneration: generation, Time: now.Add(320 * time.Millisecond), HasAck: true, RecoveryEpisode: congestion.RecoveryEpisode{ID: 1, UndoEligible: true}, Delivery: congestion.DeliverySample{Delivered: 999999}})
			require.Equal(t, before, b.GetCongestionWindow())
			require.Equal(t, rate, b.PacingRate())
		})
	}
}
