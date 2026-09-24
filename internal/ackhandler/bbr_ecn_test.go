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

func newBBRECNTestHandler() (*sentPacketHandler, *congestionRecorder) {
	r := &congestionRecorder{}
	h := newCongestionTestHandler(r)
	EnableBBRECN(h, func() (uint64, bool, bool) { return h.congestionEvents.pathGeneration, true, true })
	return h, r
}

func sendBBRECNPacket(h *sentPacketHandler, mark protocol.ECN, pathProbe bool) protocol.PacketNumber {
	pn := h.PopPacketNumber(protocol.Encryption1RTT)
	h.SentPacket(monotime.Now(), pn, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, mark, 1200, false, pathProbe)
	return pn
}

func ackBBRECN(t *testing.T, h *sentPacketHandler, ranges []wire.AckRange, ect0, ce uint64) {
	t.Helper()
	_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ranges, ECT0: ect0, ECNCE: ce}, protocol.Encryption1RTT, monotime.Now().Add(time.Millisecond))
	require.NoError(t, err)
}

func TestBBRECNActualMarkingHoles(t *testing.T) {
	for _, pathProbe := range []bool{false, true} {
		t.Run(map[bool]string{false: "coalesced hole", true: "path probe hole"}[pathProbe], func(t *testing.T) {
			h, r := newBBRECNTestHandler()
			first := sendBBRECNPacket(h, h.ECNMode(true), false)
			hole := sendBBRECNPacket(h, protocol.ECNNon, pathProbe)
			last := sendBBRECNPacket(h, h.ECNMode(true), false)
			ackBBRECN(t, h, ackRanges(first, hole, last), 2, 0)
			require.Equal(t, protocol.ECT0, h.ECNMode(true))
			require.True(t, r.feedback[len(r.feedback)-1].ECN.Eligible)
			require.Equal(t, congestion.ECNCounts{ECT0: 2}, r.feedback[len(r.feedback)-1].ECN.Delta)
		})
	}
}

func TestBBRECNAdvancingLateOnlyFeedback(t *testing.T) {
	h, r := newBBRECNTestHandler()
	first := sendBBRECNPacket(h, h.ECNMode(true), false)
	ackBBRECN(t, h, ackRanges(first), 1, 0)
	late := sendBBRECNPacket(h, h.ECNMode(true), false)
	require.True(t, h.QueueProbePacket(protocol.Encryption1RTT))
	h.ExpireDelivery(monotime.Now().Add(time.Minute))
	ackBBRECN(t, h, ackRanges(late), 1, 1)
	e := r.feedback[len(r.feedback)-1]
	require.Empty(t, e.Acked)
	require.True(t, e.ECN.Eligible)
	require.Equal(t, uint64(1), e.ECN.Delta.CE)
	require.Equal(t, uint64(2), e.ECN.Ordinal)
	ackBBRECN(t, h, ackRanges(late), 1, 1)
	require.False(t, r.feedback[len(r.feedback)-1].ECN.Eligible)
}

func TestBBRECNReorderedAndInvalidCounters(t *testing.T) {
	for _, name := range []string{"decreased", "missing", "excess CE", "remarked", "skipped", "reordered"} {
		t.Run(name, func(t *testing.T) {
			h, r := newBBRECNTestHandler()
			first := sendBBRECNPacket(h, h.ECNMode(true), false)
			ackBBRECN(t, h, ackRanges(first), 1, 0)
			next := sendBBRECNPacket(h, h.ECNMode(true), false)
			switch name {
			case "decreased", "missing":
				ackBBRECN(t, h, ackRanges(next), 0, 0)
			case "excess CE":
				ackBBRECN(t, h, ackRanges(next), 1, 2)
			case "remarked":
				_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(next), ECT0: 1, ECT1: 1}, protocol.Encryption1RTT, monotime.Now())
				require.NoError(t, err)
			case "skipped":
				// A forgotten unsent number must not acquire marking or ordinal authority.
				gap := h.PopPacketNumber(protocol.Encryption1RTT)
				h.appDataPackets.history.SkippedPacket(gap)
				sendBBRECNPacket(h, protocol.ECNNon, false)
				_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(gap), ECT0: 2}, protocol.Encryption1RTT, monotime.Now())
				if err != nil {
					return
				}
				require.False(t, r.feedback[len(r.feedback)-1].ECN.Eligible)
			case "reordered":
				ackBBRECN(t, h, ackRanges(first), 0, 0)
				require.Equal(t, protocol.ECT0, h.ECNMode(true))
				ackBBRECN(t, h, ackRanges(next), 2, 0)
				require.True(t, r.feedback[len(r.feedback)-1].ECN.Eligible)
				return
			}
			require.Equal(t, protocol.ECNNon, h.ECNMode(true))
			require.False(t, r.feedback[len(r.feedback)-1].ECN.Eligible)
		})
	}
}

func TestBBRECNRangeBudgetFallback(t *testing.T) {
	t.Run("insertion", func(t *testing.T) {
		h, r := newBBRECNTestHandler()
		first := sendBBRECNPacket(h, h.ECNMode(true), false)
		ackBBRECN(t, h, ackRanges(first), 1, 0)
		for i := 0; i < 4100; i++ {
			mark := protocol.ECNNon
			if i%2 == 0 {
				mark = h.ECNMode(true)
			}
			sendBBRECNPacket(h, mark, false)
		}
		require.Equal(t, protocol.ECNNon, h.ECNMode(true))
		require.LessOrEqual(t, len(h.bbrECN.ranges), 4096)
		last := h.appDataPackets.largestSent
		ackBBRECN(t, h, ackRanges(last), 1, 1)
		require.False(t, r.feedback[len(r.feedback)-1].ECN.Eligible)
		require.Equal(t, congestion.ECNCounts{ECT0: 1}, r.feedback[len(r.feedback)-1].ECN.Accepted)
	})
	t.Run("ACK splits", func(t *testing.T) {
		h, _ := newBBRECNTestHandler()
		first := sendBBRECNPacket(h, h.ECNMode(true), false)
		ackBBRECN(t, h, ackRanges(first), 1, 0)
		var ranges []wire.AckRange
		// Bypass random skipping, but use real recovery registration for one
		// contiguous wire packet-number run and one sparse decoded ACK.
		for pn := protocol.PacketNumber(1); pn <= 8200; pn++ {
			h.SentPacket(monotime.Now(), pn, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, protocol.ECT0, 1200, false, false)
			if pn%2 == 0 {
				ranges = append([]wire.AckRange{{Smallest: pn, Largest: pn}}, ranges...)
			}
		}
		ackBBRECN(t, h, ranges, 4101, 0)
		require.Equal(t, protocol.ECNNon, h.ECNMode(true))
		require.LessOrEqual(t, len(h.bbrECN.ranges), 4096)
	})
	t.Run("accounted prefix compacts", func(t *testing.T) {
		h, _ := newBBRECNTestHandler()
		for i := 0; i < 4200; i++ {
			pn := sendBBRECNPacket(h, h.ECNMode(true), false)
			ackBBRECN(t, h, ackRanges(pn), uint64(i+1), 0)
		}
		require.Equal(t, protocol.ECT0, h.ECNMode(true))
		require.LessOrEqual(t, len(h.bbrECN.ranges), 1)
	})
}

func TestBBRECNTestingVersusCapableCE(t *testing.T) {
	for _, name := range []string{"testing all CE", "testing loss and CE", "testing all lost", "capable all CE"} {
		t.Run(name, func(t *testing.T) {
			h, r := newBBRECNTestHandler()
			if name == "capable all CE" {
				pn := sendBBRECNPacket(h, h.ECNMode(true), false)
				ackBBRECN(t, h, ackRanges(pn), 1, 0)
				pn = sendBBRECNPacket(h, h.ECNMode(true), false)
				ackBBRECN(t, h, ackRanges(pn), 1, 1)
				require.Equal(t, protocol.ECT0, h.ECNMode(true))
				require.True(t, r.feedback[len(r.feedback)-1].Congested)
				return
			}
			var pns []protocol.PacketNumber
			for i := 0; i < 10; i++ {
				pns = append(pns, sendBBRECNPacket(h, h.ECNMode(true), false))
			}
			if name == "testing all CE" {
				ackBBRECN(t, h, ackRanges(pns...), 0, 10)
			} else {
				// An ACK-only unmarked packet anchors time-loss detection without
				// manufacturing ECT receipts for the missing test transmissions.
				anchor := h.PopPacketNumber(protocol.Encryption1RTT)
				h.SentPacket(monotime.Now(), anchor, protocol.InvalidPacketNumber, nil, nil, protocol.Encryption1RTT, protocol.ECNNon, 50, false, false)
				ranges := ackRanges(anchor)
				ce := uint64(0)
				if name == "testing loss and CE" {
					ranges = ackRanges(pns[9], anchor)
					ce = 1
				}
				ackBBRECN(t, h, ranges, 0, ce)
				require.NoError(t, h.OnLossDetectionTimeout(h.GetLossDetectionTimeout().Add(time.Second)))
			}
			require.Equal(t, protocol.ECNNon, h.ECNMode(true))
			require.True(t, h.bbrECN.state == ecnStateFailed)
			require.False(t, r.feedback[len(r.feedback)-1].Congested)
		})
	}
}

func TestBBRECNMigrationCounterFence(t *testing.T) {
	for _, capable := range []bool{true, false} {
		t.Run(map[bool]string{true: "qualified path", false: "unqualified path"}[capable], func(t *testing.T) {
			h, r := newBBRECNTestHandler()
			drained := false
			h.bbrECN.path = func() (uint64, bool, bool) { return h.congestionEvents.pathGeneration, drained, capable }
			// Initial validation doesn't require an old-generation drain.
			h.bbrECN.path = func() (uint64, bool, bool) { return h.congestionEvents.pathGeneration, drained, true }
			first := sendBBRECNPacket(h, h.ECNMode(true), false)
			ackBBRECN(t, h, ackRanges(first), 1, 0)
			old := sendBBRECNPacket(h, h.ECNMode(true), false)
			h.MigratedPath(monotime.Now(), 1200)
			h.bbrECN.path = func() (uint64, bool, bool) { return h.congestionEvents.pathGeneration, drained, capable }
			require.Equal(t, protocol.ECNNon, h.ECNMode(true))
			ackBBRECN(t, h, ackRanges(old), 1, 1)
			require.False(t, r.feedback[len(r.feedback)-1].ECN.Eligible)
			require.Equal(t, protocol.ECNNon, h.ECNMode(true))
			drained = true
			if !capable {
				require.Equal(t, protocol.ECNNon, h.ECNMode(true))
				return
			}
			require.Equal(t, protocol.ECT0, h.ECNMode(true))
			fresh := sendBBRECNPacket(h, h.ECNMode(true), false)
			ackBBRECN(t, h, ackRanges(fresh), 2, 1)
			e := r.feedback[len(r.feedback)-1].ECN
			require.True(t, e.Eligible)
			require.Equal(t, uint64(1), e.PathGeneration)
			require.Equal(t, congestion.ECNCounts{ECT0: 1}, e.Delta)
			require.Equal(t, congestion.ECNCounts{ECT0: 2, CE: 1}, e.Accepted)
		})
	}
}

func TestBBRECNMissingOldMarkedPacket(t *testing.T) {
	h, r := newBBRECNTestHandler()
	old := sendBBRECNPacket(h, h.ECNMode(true), false)
	h.MigratedPath(monotime.Now(), 1200)
	h.ExpireDelivery(monotime.Now().Add(time.Minute))
	require.Equal(t, protocol.ECNNon, h.ECNMode(true))
	fresh := sendBBRECNPacket(h, h.ECNMode(true), false)
	ackBBRECN(t, h, ackRanges(fresh), 0, 0)
	require.False(t, r.feedback[len(r.feedback)-1].ECN.Eligible)
	require.Equal(t, protocol.ECNNon, h.ECNMode(true))
	// A nonadvancing count-only report deliberately cannot establish the fence.
	ackBBRECN(t, h, ackRanges(old), 1, 0)
	require.Equal(t, protocol.ECNNon, h.ECNMode(true))
}

func TestBBRECNRepeatedMigration(t *testing.T) {
	t.Run("fresh tests forget old loss flags", func(t *testing.T) {
		h, _ := newBBRECNTestHandler()
		var old []protocol.PacketNumber
		for i := 0; i < 10; i++ {
			old = append(old, sendBBRECNPacket(h, h.ECNMode(true), false))
		}
		anchor := sendBBRECNPacket(h, protocol.ECNNon, false)
		ackBBRECN(t, h, ackRanges(old[9], anchor), 0, 1)
		require.NoError(t, h.OnLossDetectionTimeout(h.GetLossDetectionTimeout().Add(time.Second)))
		h.MigratedPath(monotime.Now(), 1200)
		next := sendBBRECNPacket(h, h.ECNMode(true), false)
		ackBBRECN(t, h, ackRanges(append(old, next)...), 9, 1)
		require.Equal(t, protocol.ECT0, h.ECNMode(true))
		for i := 0; i < 10; i++ {
			sendBBRECNPacket(h, h.ECNMode(true), false)
		}
		last := sendBBRECNPacket(h, protocol.ECNNon, false)
		ackBBRECN(t, h, ackRanges(last), 9, 1)
		require.NoError(t, h.OnLossDetectionTimeout(h.GetLossDetectionTimeout().Add(time.Second)))
		require.Equal(t, ecnStateFailed, h.bbrECN.state)
	})

	t.Run("unresolved marking survives repeated resets", func(t *testing.T) {
		h, r := newBBRECNTestHandler()
		old := sendBBRECNPacket(h, h.ECNMode(true), false)
		h.MigratedPath(monotime.Now(), 1200)
		h.MigratedPath(monotime.Now(), 1200)
		require.Equal(t, protocol.ECNNon, h.ECNMode(true))
		ackBBRECN(t, h, ackRanges(old), 1, 0)
		require.False(t, r.feedback[len(r.feedback)-1].ECN.Eligible)
		require.Equal(t, protocol.ECT0, h.ECNMode(true))
		// The fresh epoch gets ten tests, independent of the old count offsets.
		for i := 0; i < 10; i++ {
			require.Equal(t, protocol.ECT0, h.ECNMode(true))
			sendBBRECNPacket(h, h.ECNMode(true), false)
		}
		require.Equal(t, protocol.ECNNon, h.ECNMode(true))
	})
	t.Run("no marked history waits only for old local debt", func(t *testing.T) {
		h, _ := newBBRECNTestHandler()
		drained := false
		h.bbrECN.path = func() (uint64, bool, bool) { return h.congestionEvents.pathGeneration, drained, true }
		h.MigratedPath(monotime.Now(), 1200)
		require.Equal(t, protocol.ECNNon, h.ECNMode(true))
		drained = true
		require.Equal(t, protocol.ECT0, h.ECNMode(true))
	})
	t.Run("close retires the private ledger", func(t *testing.T) {
		h, _ := newBBRECNTestHandler()
		sendBBRECNPacket(h, h.ECNMode(true), false)
		h.CloseDelivery()
		require.Equal(t, protocol.ECNNon, h.ECNMode(true))
		require.Empty(t, h.bbrECN.ranges)
	})
}

func TestLegacyECNDispatchPreserved(t *testing.T) {
	h := NewSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, true, nil, protocol.PerspectiveServer, nil, utils.DefaultLogger).(*sentPacketHandler)
	first := sendBBRECNPacket(h, h.ECNMode(true), false)
	ackBBRECN(t, h, ackRanges(first), 1, 0)
	require.Nil(t, h.bbrECN)
	late := sendBBRECNPacket(h, h.ECNMode(true), false)
	require.True(t, h.QueueProbePacket(protocol.Encryption1RTT))
	// Legacy recovery returns before inspecting a late-only counter decrease.
	ackBBRECN(t, h, ackRanges(late), 0, 0)
	require.Equal(t, protocol.ECT0, h.ECNMode(true))
}
