package ackhandler

import (
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

func acknowledgeRecoveryPacket(t *testing.T, h *sentPacketHandler, level protocol.EncryptionLevel, now monotime.Time, pns ...protocol.PacketNumber) {
	t.Helper()
	_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pns...)}, level, now)
	require.NoError(t, err)
}

func measuredRecoveryHandler(t *testing.T) (*sentPacketHandler, *congestionRecorder, monotime.Time) {
	t.Helper()
	r := &congestionRecorder{}
	h := newCongestionTestHandler(r)
	now := monotime.Now()
	pn := sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1200)
	acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(10*time.Millisecond), pn)
	return h, r, now.Add(20 * time.Millisecond)
}

func TestBBRPersistentCongestionAcrossSpaces(t *testing.T) {
	h, r, now := measuredRecoveryHandler(t)
	first := sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1200)
	last := sendCongestionTestPacket(h, now.Add(time.Second), protocol.EncryptionHandshake, 1200)
	// ACK newer transmissions in each space, proving the two older losses.
	i := sendCongestionTestPacket(h, now.Add(2*time.Second), protocol.EncryptionInitial, 1200)
	j := sendCongestionTestPacket(h, now.Add(2*time.Second), protocol.EncryptionHandshake, 1200)
	acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(2010*time.Millisecond), i)
	require.Zero(t, r.feedback[len(r.feedback)-1].PersistentCongestion.EndOrdinal)
	acknowledgeRecoveryPacket(t, h, protocol.EncryptionHandshake, now.Add(2020*time.Millisecond), j)
	e := r.feedback[len(r.feedback)-1]
	require.NotEqual(t, first, i)
	require.NotEqual(t, last, j)
	require.Equal(t, uint64(2), e.PersistentCongestion.StartOrdinal)
	require.Equal(t, uint64(3), e.PersistentCongestion.EndOrdinal)
}

func TestBBRRecoveryEpisodeAllSpurious(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(map[bool]string{false: "mixed", true: "all"}[all], func(t *testing.T) {
			h, r, now := measuredRecoveryHandler(t)
			a := sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1200)
			b := sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1200)
			latest := sendCongestionTestPacket(h, now.Add(100*time.Millisecond), protocol.EncryptionInitial, 1200)
			acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(110*time.Millisecond), latest)
			e := r.feedback[len(r.feedback)-1].RecoveryEpisode
			require.True(t, e.Entered)
			require.True(t, e.Active)
			require.Equal(t, uint64(4), e.Boundary)
			require.Equal(t, 2, e.Pending)
			acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(120*time.Millisecond), a)
			require.False(t, r.feedback[len(r.feedback)-1].RecoveryEpisode.UndoEligible)
			if all {
				acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(130*time.Millisecond), b)
				e = r.feedback[len(r.feedback)-1].RecoveryEpisode
				require.True(t, e.UndoEligible)
				require.Zero(t, e.Pending)
				require.Equal(t, uint64(1), e.ID)
			}
		})
	}
}

func TestBBRRecoveryEpisodeSupersededOrMissing(t *testing.T) {
	t.Run("retained member eviction", func(t *testing.T) {
		h, r, now := measuredRecoveryHandler(t)
		members := make([]protocol.PacketNumber, 0, 4097)
		for range 4097 {
			members = append(members, sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1200))
		}
		newest := sendCongestionTestPacket(h, now.Add(100*time.Millisecond), protocol.EncryptionInitial, 1200)
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(110*time.Millisecond), newest)
		require.Positive(t, h.DeliveryStats().Evicted)
		require.False(t, r.feedback[len(r.feedback)-1].RecoveryEpisode.UndoPossible)
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(120*time.Millisecond), members...)
		require.False(t, r.feedback[len(r.feedback)-1].RecoveryEpisode.UndoEligible)
	})
	t.Run("later losses extend active boundary", func(t *testing.T) {
		h, r, now := measuredRecoveryHandler(t)
		a := sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1200)
		acked := sendCongestionTestPacket(h, now.Add(100*time.Millisecond), protocol.EncryptionInitial, 1200)
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(110*time.Millisecond), acked)
		b := sendCongestionTestPacket(h, now.Add(120*time.Millisecond), protocol.EncryptionInitial, 1200)
		acked = sendCongestionTestPacket(h, now.Add(140*time.Millisecond), protocol.EncryptionInitial, 1200)
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(150*time.Millisecond), acked)
		e := r.feedback[len(r.feedback)-1].RecoveryEpisode
		require.Equal(t, uint64(1), e.ID)
		require.Equal(t, uint64(5), e.Boundary)
		require.True(t, e.Active)
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(160*time.Millisecond), a)
		require.False(t, r.feedback[len(r.feedback)-1].RecoveryEpisode.UndoEligible)
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(170*time.Millisecond), b)
		require.True(t, r.feedback[len(r.feedback)-1].RecoveryEpisode.UndoEligible)
	})
	for _, mode := range []string{"expired", "superseded", "discard", "retry", "path", "close"} {
		t.Run(mode, func(t *testing.T) {
			h, r, now := measuredRecoveryHandler(t)
			lost := sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1200)
			latest := sendCongestionTestPacket(h, now.Add(100*time.Millisecond), protocol.EncryptionInitial, 1200)
			acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(110*time.Millisecond), latest)
			switch mode {
			case "expired":
				h.ExpireDelivery(now.Add(time.Second))
			case "superseded":
				next := sendCongestionTestPacket(h, now.Add(120*time.Millisecond), protocol.EncryptionInitial, 1200)
				acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(130*time.Millisecond), next)
				require.True(t, r.feedback[len(r.feedback)-1].RecoveryEpisode.Exited)
				sendCongestionTestPacket(h, now.Add(140*time.Millisecond), protocol.EncryptionInitial, 1200)
				next = sendCongestionTestPacket(h, now.Add(240*time.Millisecond), protocol.EncryptionInitial, 1200)
				acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(250*time.Millisecond), next)
				require.Equal(t, uint64(2), r.feedback[len(r.feedback)-1].RecoveryEpisode.ID)
			case "discard":
				h.DropPackets(protocol.EncryptionInitial, now.Add(120*time.Millisecond))
			case "retry":
				h.ResetForRetry(now.Add(120 * time.Millisecond))
			case "path":
				h.MigratedPath(now.Add(120*time.Millisecond), 1200)
			case "close":
				h.CloseDelivery()
			}
			// A fresh space drives observation without relying on a discarded packet
			// number space. No old member may authorize a restoration.
			if mode == "superseded" {
				acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(260*time.Millisecond), lost)
			} else {
				next := sendCongestionTestPacket(h, now.Add(1100*time.Millisecond), protocol.EncryptionHandshake, 1200)
				acknowledgeRecoveryPacket(t, h, protocol.EncryptionHandshake, now.Add(1110*time.Millisecond), next)
			}
			e := r.feedback[len(r.feedback)-1]
			require.False(t, e.RecoveryEpisode.UndoEligible)
			if mode != "superseded" && mode != "close" {
				require.False(t, e.RecoveryEpisode.UndoPossible)
			}
		})
	}
}

func TestBBRPersistentCongestionMeasuredAtSend(t *testing.T) {
	for _, mode := range []string{"unmeasured", "restored", "measured", "retry"} {
		t.Run(mode, func(t *testing.T) {
			r := &congestionRecorder{}
			h := newCongestionTestHandler(r)
			now := monotime.Now()
			if mode == "measured" || mode == "retry" {
				h, r, now = measuredRecoveryHandler(t)
			}
			if mode == "restored" {
				h.rttStats.SetInitialRTT(10 * time.Millisecond)
			}
			if mode == "retry" {
				sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1200)
				h.ResetForRetry(now.Add(10 * time.Millisecond))
				now = now.Add(20 * time.Millisecond)
			}
			sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1200)
			sendCongestionTestPacket(h, now.Add(time.Second), protocol.EncryptionInitial, 1200)
			newest := sendCongestionTestPacket(h, now.Add(2*time.Second), protocol.EncryptionInitial, 1200)
			acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(2010*time.Millisecond), newest)
			require.Equal(t, mode == "measured", r.feedback[len(r.feedback)-1].PersistentCongestion.EndOrdinal != 0)
		})
	}
	t.Run("max ACK delay applies to Initial too", func(t *testing.T) {
		h, r, now := measuredRecoveryHandler(t)
		h.rttStats.SetMaxAckDelay(time.Second)
		sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1200)
		sendCongestionTestPacket(h, now.Add(time.Second), protocol.EncryptionInitial, 1200)
		newest := sendCongestionTestPacket(h, now.Add(2*time.Second), protocol.EncryptionInitial, 1200)
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(2010*time.Millisecond), newest)
		require.Zero(t, r.feedback[len(r.feedback)-1].PersistentCongestion.EndOrdinal)
	})
}

func TestBBRPersistentCongestionAckOnlyBreak(t *testing.T) {
	for _, received := range []bool{false, true} {
		t.Run(map[bool]string{false: "unresolved", true: "received"}[received], func(t *testing.T) {
			h, r, now := measuredRecoveryHandler(t)
			sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1200)
			ackOnly := h.PopPacketNumber(protocol.EncryptionHandshake)
			h.SentPacket(now.Add(500*time.Millisecond), ackOnly, protocol.InvalidPacketNumber, nil, nil, protocol.EncryptionHandshake, protocol.ECNNon, 40, false, false)
			if received {
				acknowledgeRecoveryPacket(t, h, protocol.EncryptionHandshake, now.Add(510*time.Millisecond), ackOnly)
			}
			sendCongestionTestPacket(h, now.Add(time.Second), protocol.EncryptionInitial, 1200)
			newest := sendCongestionTestPacket(h, now.Add(2*time.Second), protocol.EncryptionInitial, 1200)
			acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(2010*time.Millisecond), newest)
			require.Zero(t, r.feedback[len(r.feedback)-1].PersistentCongestion.EndOrdinal)
		})
	}
}

func TestBBRPersistentCongestionGapAndEviction(t *testing.T) {
	for _, mode := range []string{"unresolved", "acked", "discarded", "rejected-0RTT", "MTU", "path-probe", "eviction"} {
		t.Run(mode, func(t *testing.T) {
			h, r, now := measuredRecoveryHandler(t)
			sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1200)
			level := protocol.EncryptionHandshake
			if mode == "rejected-0RTT" {
				level = protocol.Encryption0RTT
			}
			gap := h.PopPacketNumber(level)
			h.SentPacket(now.Add(500*time.Millisecond), gap, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.PingFrame{}}}, level, protocol.ECNNon, 1200, mode == "MTU", mode == "path-probe")
			switch mode {
			case "acked":
				acknowledgeRecoveryPacket(t, h, level, now.Add(510*time.Millisecond), gap)
			case "discarded", "rejected-0RTT":
				h.DropPackets(level, now.Add(510*time.Millisecond))
			case "eviction":
				// Pressure comes from legal ACK-only registrations, not a larger
				// mandatory recovery window. All times and storage remain bounded.
				for range maxRecoveryOutcomes {
					pn := h.PopPacketNumber(protocol.EncryptionHandshake)
					h.SentPacket(now.Add(500*time.Millisecond), pn, protocol.InvalidPacketNumber, nil, nil, protocol.EncryptionHandshake, protocol.ECNNon, 40, false, false)
				}
			}
			sendCongestionTestPacket(h, now.Add(time.Second), protocol.EncryptionInitial, 1200)
			newest := sendCongestionTestPacket(h, now.Add(2*time.Second), protocol.EncryptionInitial, 1200)
			acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(2010*time.Millisecond), newest)
			require.Zero(t, r.feedback[len(r.feedback)-1].PersistentCongestion.EndOrdinal)
			if mode == "eviction" {
				require.LessOrEqual(t, h.DeliveryStats().OutcomeEntries, 32768)
				require.Positive(t, h.DeliveryStats().OutcomeEvicted)
			}
		})
	}
}

func TestBBRPersistentCongestionDeduplication(t *testing.T) {
	t.Run("validated duplicate ACK confirms timer losses", func(t *testing.T) {
		h, r, now := measuredRecoveryHandler(t)
		sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1200)
		sendCongestionTestPacket(h, now.Add(time.Second), protocol.EncryptionInitial, 1200)
		newest := sendCongestionTestPacket(h, now.Add(time.Second), protocol.EncryptionInitial, 1200)
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(1010*time.Millisecond), newest)
		require.NoError(t, h.OnLossDetectionTimeout(h.GetLossDetectionTimeout()))
		require.Zero(t, r.feedback[len(r.feedback)-1].PersistentCongestion.EndOrdinal)
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(1100*time.Millisecond), newest)
		require.NotZero(t, r.feedback[len(r.feedback)-1].PersistentCongestion.EndOrdinal)
	})
	t.Run("timer evidence waits for an ACK-only receipt", func(t *testing.T) {
		h, r, now := measuredRecoveryHandler(t)
		// An RTT increase delays loss of the last endpoint until the timer.
		sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1200)
		sendCongestionTestPacket(h, now.Add(time.Second), protocol.EncryptionInitial, 1200)
		newest := sendCongestionTestPacket(h, now.Add(time.Second), protocol.EncryptionInitial, 1200)
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(1010*time.Millisecond), newest)
		require.Zero(t, r.feedback[len(r.feedback)-1].PersistentCongestion.EndOrdinal)
		require.NoError(t, h.OnLossDetectionTimeout(h.GetLossDetectionTimeout()))
		require.False(t, r.feedback[len(r.feedback)-1].HasAck)
		require.Zero(t, r.feedback[len(r.feedback)-1].PersistentCongestion.EndOrdinal)
		pn := h.PopPacketNumber(protocol.EncryptionHandshake)
		h.SentPacket(now.Add(1100*time.Millisecond), pn, protocol.InvalidPacketNumber, nil, nil, protocol.EncryptionHandshake, protocol.ECNNon, 40, false, false)
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionHandshake, now.Add(1110*time.Millisecond), pn)
		require.NotZero(t, r.feedback[len(r.feedback)-1].PersistentCongestion.EndOrdinal)
	})
	h, r, now := measuredRecoveryHandler(t)
	sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1200)
	sendCongestionTestPacket(h, now.Add(time.Second), protocol.EncryptionInitial, 1200)
	newest := sendCongestionTestPacket(h, now.Add(2*time.Second), protocol.EncryptionInitial, 1200)
	acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(2010*time.Millisecond), newest)
	require.NotZero(t, r.feedback[len(r.feedback)-1].PersistentCongestion.EndOrdinal)
	require.False(t, r.feedback[len(r.feedback)-1].RecoveryEpisode.UndoPossible)
	reports := 0
	for range 2 {
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(2020*time.Millisecond), newest)
		p := sendCongestionTestPacket(h, now.Add(2030*time.Millisecond), protocol.EncryptionHandshake, 1200)
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionHandshake, now.Add(2040*time.Millisecond), p)
	}
	for _, e := range r.feedback {
		if e.PersistentCongestion.EndOrdinal != 0 {
			reports++
		}
	}
	require.Equal(t, 1, reports)
}

func TestBBRPTODoesNotFabricateLoss(t *testing.T) {
	h, r, now := measuredRecoveryHandler(t)
	pn := sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1200)
	for range 3 {
		require.NoError(t, h.OnLossDetectionTimeout(h.GetLossDetectionTimeout()))
	}
	require.True(t, h.QueueProbePacketAt(protocol.EncryptionInitial, now.Add(50*time.Millisecond)))
	acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(60*time.Millisecond), pn)
	e := r.feedback[len(r.feedback)-1]
	require.Empty(t, e.Lost)
	require.Zero(t, e.RecoveryEpisode.ID)
	require.Zero(t, e.PersistentCongestion.EndOrdinal)
	require.Equal(t, uint64(2400), e.Delivery.Delivered)
}
