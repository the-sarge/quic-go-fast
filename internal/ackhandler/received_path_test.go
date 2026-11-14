package ackhandler

import (
	"testing"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

func TestReceivePathHistories(t *testing.T) {
	h := NewReceivedPacketHandler(utils.DefaultLogger)
	require.NoError(t, h.AddPath(17))
	now := monotime.Now()
	require.NoError(t, h.ReceivedPacketOnPath(17, 9, protocol.ECT1, now, true))
	require.False(t, h.IsPotentiallyDuplicate(9, protocol.Encryption1RTT))
	require.True(t, h.IsPotentiallyDuplicateOnPath(17, 9))
	require.NoError(t, h.ReceivedPacket(9, protocol.ECT0, protocol.Encryption1RTT, now, true))
	ordinary := h.GetAckFrame(protocol.Encryption1RTT, now, false)
	require.False(t, ordinary.HasPathID)
	require.EqualValues(t, 1, ordinary.ECT0)
	path := h.GetPathAckFrame(17, now, false)
	require.True(t, path.HasPathID)
	require.Equal(t, protocol.PathID(17), path.PathID)
	require.EqualValues(t, 1, path.ECT1)
	require.Equal(t, protocol.PacketNumber(9), path.LargestAcked())
	require.Error(t, h.ReceivedPacketOnPath(17, 9, protocol.ECT1, now, true))
	require.NoError(t, h.ReceivedPacketOnPath(17, 8, protocol.ECNNon, now, true))
	h.IgnorePacketsBelowOnPath(17, 10)
	require.Nil(t, h.GetPathAckFrame(17, now, false))
	require.True(t, h.GetAlarmTimeout().IsZero())
	require.NoError(t, h.ReceivedPacketOnPath(17, 11, protocol.ECNNon, now, true))
	require.Equal(t, protocol.PacketNumber(11), h.GetPathAckFrame(17, now, false).LargestAcked())
}

func TestBundledCarrierPrunesTargets(t *testing.T) {
	h := NewReceivedPacketHandler(utils.DefaultLogger)
	require.NoError(t, h.AddPath(17))
	now := monotime.Now()
	for _, id := range []protocol.PathID{0, 17} {
		require.NoError(t, h.ReceivedPacketOnPath(id, 9, protocol.ECNNon, now, true))
		require.NoError(t, h.ReceivedPacketOnPath(id, 12, protocol.ECNNon, now, true))
	}
	sent := NewSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, false, h.IgnorePacketsBelowOnPath, protocol.PerspectiveClient, nil, utils.DefaultLogger)
	target := &wire.AckFrame{HasPathID: true, PathID: 17, AckRanges: []wire.AckRange{{Smallest: 9, Largest: 9}}}
	sent.SentPacket(now, 0, 12, nil, []Frame{{Frame: target}, {Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, protocol.ECNNon, 100, false, false)
	// Registration must snapshot facts before the reusable ACK frame changes.
	target.PathID = 0
	target.AckRanges[0].Largest = 100
	_, err := sent.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: 0, Largest: 0}}}, protocol.Encryption1RTT, now)
	require.NoError(t, err)
	require.True(t, h.IsPotentiallyDuplicateOnPath(0, 11))
	require.False(t, h.IsPotentiallyDuplicateOnPath(17, 11))
	require.True(t, h.IsPotentiallyDuplicateOnPath(17, 8))
}

func TestRetiredCarrierPrunesOnlyAfterAck(t *testing.T) {
	for _, retirement := range []string{"loss", "pto", "migration"} {
		t.Run(retirement, func(t *testing.T) {
			h := NewReceivedPacketHandler(utils.DefaultLogger)
			require.NoError(t, h.AddPath(17))
			now := monotime.Now()
			require.NoError(t, h.ReceivedPacketOnPath(17, 9, protocol.ECNNon, now, true))
			sent := NewSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, false, h.IgnorePacketsBelowOnPath, protocol.PerspectiveClient, nil, utils.DefaultLogger)
			target := &wire.AckFrame{HasPathID: true, PathID: 17, AckRanges: []wire.AckRange{{Smallest: 9, Largest: 9}}}
			sent.SentPacket(now, 0, protocol.InvalidPacketNumber, nil, []Frame{{Frame: target}, {Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, protocol.ECNNon, 100, false, false)
			switch retirement {
			case "pto":
				require.True(t, sent.QueueProbePacket(protocol.Encryption1RTT))
			case "migration":
				sent.MigratedPath(now, 1200)
			case "loss":
				for pn := protocol.PacketNumber(1); pn <= 3; pn++ {
					sent.SentPacket(now, pn, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, protocol.ECNNon, 100, false, false)
				}
				_, err := sent.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: 3, Largest: 3}}}, protocol.Encryption1RTT, now)
				require.NoError(t, err)
			}
			require.False(t, h.IsPotentiallyDuplicateOnPath(17, 8))
			_, err := sent.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: 0, Largest: 0}}}, protocol.Encryption1RTT, now)
			require.NoError(t, err)
			require.True(t, h.IsPotentiallyDuplicateOnPath(17, 8))
		})
	}
}

func TestPathHistoryBoundsAndAdmission(t *testing.T) {
	h := NewReceivedPacketHandler(utils.DefaultLogger)
	now := monotime.Now()
	require.Error(t, h.ReceivedPacketOnPath(23, 1, protocol.ECNNon, now, true))
	require.True(t, h.IsPotentiallyDuplicateOnPath(23, 1))
	require.NoError(t, h.AddPath(protocol.MaxPathID))
	require.Error(t, h.AddPath(23))
	require.Error(t, h.AddPath(0))
	for pn := protocol.PacketNumber(0); pn <= 2*protocol.MaxNumAckRanges; pn += 2 {
		require.NoError(t, h.ReceivedPacketOnPath(protocol.MaxPathID, pn, protocol.ECNNon, now, true))
	}
	ack := h.GetPathAckFrame(protocol.MaxPathID, now, false)
	require.Len(t, ack.AckRanges, protocol.MaxNumAckRanges)
	require.Equal(t, protocol.PacketNumber(2), ack.LowestAcked())
	require.False(t, h.IsPotentiallyDuplicateOnPath(0, 0))
	fresh := NewReceivedPacketHandler(utils.DefaultLogger)
	require.NoError(t, fresh.AddPath(23))
	require.False(t, fresh.IsPotentiallyDuplicateOnPath(23, 0))
}

func TestPathAckRemainsQueuedWhenCarrierIsFull(t *testing.T) {
	h := NewReceivedPacketHandler(utils.DefaultLogger)
	h.SetAppDataAckThreshold(4)
	require.NoError(t, h.AddPath(17))
	now := monotime.Now()
	require.NoError(t, h.ReceivedPacketOnPath(17, 9, protocol.ECT0, now, true))
	deadline := h.GetAlarmTimeout()
	require.Equal(t, now.Add(protocol.MaxAckDelay), deadline)
	frames, size := h.AppendPathAckFrames(nil, 1, deadline, true, protocol.Version1, AckCarriage{}, 0)
	require.Empty(t, frames)
	require.Zero(t, size)
	require.Equal(t, deadline, h.GetAlarmTimeout())
	frames, size = h.AppendPathAckFrames(nil, 100, deadline, true, protocol.Version1, AckCarriage{}, 0)
	require.Len(t, frames, 1)
	require.Equal(t, frames[0].Frame.Length(protocol.Version1), size)
	require.True(t, h.GetAlarmTimeout().IsZero())
}

func TestRetiredCarrierExpiresWithoutPruning(t *testing.T) {
	h := NewReceivedPacketHandler(utils.DefaultLogger)
	require.NoError(t, h.AddPath(17))
	now := monotime.Now()
	require.NoError(t, h.ReceivedPacketOnPath(17, 9, protocol.ECNNon, now, true))
	rtt := utils.NewRTTStats()
	sent := NewSentPacketHandler(0, 1200, rtt, &utils.ConnectionStats{}, true, false, h.IgnorePacketsBelowOnPath, protocol.PerspectiveClient, nil, utils.DefaultLogger)
	sent.SentPacket(now, 0, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.AckFrame{HasPathID: true, PathID: 17, AckRanges: []wire.AckRange{{Smallest: 9, Largest: 9}}}}, {Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, protocol.ECNNon, 100, false, false)
	require.True(t, sent.(*sentPacketHandler).QueueProbePacketAt(protocol.Encryption1RTT, now))
	_, err := sent.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: 0, Largest: 0}}}, protocol.Encryption1RTT, now.Add(4*rtt.PTO(true)))
	require.NoError(t, err)
	require.False(t, h.IsPotentiallyDuplicateOnPath(17, 8))
}

func TestUnownedAckTargetCannotPrunePathZeroCarrier(t *testing.T) {
	h := NewReceivedPacketHandler(utils.DefaultLogger)
	now := monotime.Now()
	require.NoError(t, h.ReceivedPacket(9, protocol.ECNNon, protocol.Encryption1RTT, now, true))
	sent := NewSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, false, h.IgnorePacketsBelowOnPath, protocol.PerspectiveClient, nil, utils.DefaultLogger)
	sent.SentPacket(now, 0, 9, nil, []Frame{{Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, protocol.ECNNon, 100, false, false)
	_, err := sent.ReceivedAck(&wire.AckFrame{HasPathID: true, PathID: 17, AckRanges: []wire.AckRange{{Smallest: 0, Largest: 0}}}, protocol.Encryption1RTT, now)
	require.Error(t, err)
	require.False(t, h.IsPotentiallyDuplicateOnPath(0, 8))
}

func TestPathAckIsNotAckEliciting(t *testing.T) {
	require.False(t, IsFrameTypeAckEliciting(wire.FrameTypePathAck))
	require.False(t, IsFrameTypeAckEliciting(wire.FrameTypePathAckECN))
}

func TestUnsubmittedCarrierDoesNotPrune(t *testing.T) {
	h := NewReceivedPacketHandler(utils.DefaultLogger)
	require.NoError(t, h.AddPath(17))
	now := monotime.Now()
	require.NoError(t, h.ReceivedPacketOnPath(17, 9, protocol.ECNNon, now, true))
	sent, err := NewLocalSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, false, h.IgnorePacketsBelowOnPath, protocol.PerspectiveClient, nil, utils.DefaultLogger)
	require.NoError(t, err)
	token, err := sent.RegisterLocalPacket(PacketContext{}, now, 0, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.AckFrame{HasPathID: true, PathID: 17, AckRanges: []wire.AckRange{{Smallest: 9, Largest: 9}}}}, {Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, protocol.ECNNon, 100)
	require.NoError(t, err)
	require.NoError(t, sent.CompleteLocalPacket(token, LocalPacketUnsubmitted, now))
	require.False(t, h.IsPotentiallyDuplicateOnPath(17, 8))
	sent.CloseLocal(now)
	require.False(t, h.IsPotentiallyDuplicateOnPath(17, 8))
}

func TestPendingRetiredCarrierAckPrecedesLocalCompletion(t *testing.T) {
	h := NewReceivedPacketHandler(utils.DefaultLogger)
	require.NoError(t, h.AddPath(17))
	now := monotime.Now()
	require.NoError(t, h.ReceivedPacketOnPath(17, 9, protocol.ECNNon, now, true))
	sent, err := NewLocalSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, false, h.IgnorePacketsBelowOnPath, protocol.PerspectiveClient, nil, utils.DefaultLogger)
	require.NoError(t, err)
	token, err := sent.RegisterLocalPacket(PacketContext{}, now, 0, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.AckFrame{HasPathID: true, PathID: 17, AckRanges: []wire.AckRange{{Smallest: 9, Largest: 9}}}}, {Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, protocol.ECNNon, 100)
	require.NoError(t, err)
	sent.MigratedPath(now, 1200)
	_, err = sent.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: 0, Largest: 0}}}, protocol.Encryption1RTT, now)
	require.NoError(t, err)
	require.True(t, h.IsPotentiallyDuplicateOnPath(17, 8))
	require.ErrorIs(t, sent.CompleteLocalPacket(token, LocalPacketUnsubmitted, now), ErrLocalCompletionContradiction)
}

func TestRetiredCarrierReceiptCap(t *testing.T) {
	h := NewReceivedPacketHandler(utils.DefaultLogger)
	require.NoError(t, h.AddPath(17))
	now := monotime.Now()
	require.NoError(t, h.ReceivedPacketOnPath(17, 100, protocol.ECNNon, now, true))
	sent := NewSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, false, h.IgnorePacketsBelowOnPath, protocol.PerspectiveClient, nil, utils.DefaultLogger)
	for pn := protocol.PacketNumber(0); pn <= 64; pn++ {
		sent.SentPacket(now, pn, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.AckFrame{HasPathID: true, PathID: 17, AckRanges: []wire.AckRange{{Smallest: 9, Largest: 9}}}}, {Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, protocol.ECNNon, 100, false, false)
		require.True(t, sent.QueueProbePacket(protocol.Encryption1RTT))
	}
	_, err := sent.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: 0, Largest: 0}}}, protocol.Encryption1RTT, now)
	require.NoError(t, err)
	require.False(t, h.IsPotentiallyDuplicateOnPath(17, 8))
	_, err = sent.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: 64, Largest: 64}}}, protocol.Encryption1RTT, now)
	require.NoError(t, err)
	require.True(t, h.IsPotentiallyDuplicateOnPath(17, 8))
}

// pathAckTargets returns the PATH_ACK target one packet on carrier selects.
func pathAckTargets(h *ReceivedPacketHandler, acks AckCarriage, carrier protocol.PathID, now monotime.Time, onlyIfQueued bool) []protocol.PathID {
	frames, _ := h.AppendPathAckFrames(nil, 1000, now, onlyIfQueued, protocol.Version1, acks, carrier)
	var ids []protocol.PathID
	for _, f := range frames {
		ids = append(ids, f.Frame.(*wire.AckFrame).PathID)
	}
	return ids
}

// A packet carries its own path's target and the released ones, one PATH_ACK at a
// time, and path zero only on its own path or once released. Under any
// replenishment of the others, every due target leaves within maxRecoveryPaths
// packets that may carry it.
func TestPathAckCarriageBoundedService(t *testing.T) {
	now := monotime.Now()
	var next protocol.PacketNumber
	receive := func(h *ReceivedPacketHandler, ids ...protocol.PathID) {
		for _, id := range ids {
			next++ // fresh on every path, with no gap to report
			require.NoError(t, h.ReceivedPacketOnPath(id, next, protocol.ECNNon, now, true))
		}
	}
	t.Run("path zero rides a nonzero carrier only when released", func(t *testing.T) {
		h := NewReceivedPacketHandler(utils.DefaultLogger)
		require.NoError(t, h.AddPath(1))
		receive(h, 0)
		var held AckCarriage
		held.Hold(0)
		require.Nil(t, h.GetAckFrameOn(held, 1, now, false), "a home path zero is withheld from path 1")
		require.NotNil(t, h.GetAckFrameOn(held, 0, now, false), "path zero carries its own")
		receive(h, 0)
		held.Release(0)
		require.NotNil(t, h.GetAckFrameOn(held, 1, now, false), "released, it rides any carrier")
	})
	t.Run("a home target stays off another carrier", func(t *testing.T) {
		h := NewReceivedPacketHandler(utils.DefaultLogger)
		require.NoError(t, h.AddPath(1))
		require.NoError(t, h.AddPath(2))
		receive(h, 2, 2)
		var held AckCarriage
		held.Hold(2)
		require.Empty(t, pathAckTargets(h, held, 1, now, false))
		require.True(t, h.AckDueOnPath(2, now), "the due read agrees with consumption")
		require.Equal(t, []protocol.PathID{2}, pathAckTargets(h, held, 2, now, true))
		require.False(t, h.AckDueOnPath(2, now))
	})
	// Path zero is retired; carrier X=1 owns its target, Y=2 and Z=3 are released.
	bound := func(t *testing.T, refill func(h *ReceivedPacketHandler, served protocol.PathID)) {
		h := NewReceivedPacketHandler(utils.DefaultLogger)
		for _, id := range []protocol.PathID{1, 2, 3} {
			require.NoError(t, h.AddPath(id))
		}
		receive(h, 1, 2, 3)
		var held AckCarriage
		held.Hold(1)
		waiting := map[protocol.PathID]int{1: 0, 2: 0, 3: 0}
		for range 40 {
			served := pathAckTargets(h, held, 1, now, false)
			require.Len(t, served, 1, "one PATH_ACK per packet")
			for id := range waiting {
				if id == served[0] {
					waiting[id] = 0
					continue
				}
				waiting[id]++
				require.Less(t, waiting[id], maxRecoveryPaths, "path %d waited past the bound", id)
			}
			refill(h, served[0])
		}
	}
	t.Run("own target replenished while a released target waits", func(t *testing.T) {
		bound(t, func(h *ReceivedPacketHandler, served protocol.PathID) { receive(h, served) })
	})
	t.Run("released targets refilled while the own target waits", func(t *testing.T) {
		bound(t, func(h *ReceivedPacketHandler, served protocol.PathID) {
			receive(h, served)
			if served != 2 {
				receive(h, 2)
			}
			if served != 3 {
				receive(h, 3)
			}
		})
	})
	t.Run("the own turn carries an earlier-waiting released target", func(t *testing.T) {
		h := NewReceivedPacketHandler(utils.DefaultLogger)
		require.NoError(t, h.AddPath(1))
		require.NoError(t, h.AddPath(2))
		receive(h, 1, 1, 2, 2)
		var held AckCarriage
		held.Hold(1)
		// Path 1's own packet serves X=1 and passes Y=2 over.
		require.Equal(t, []protocol.PathID{1}, pathAckTargets(h, held, 1, now, true))
		receive(h, 1, 1)
		require.Equal(t, []protocol.PathID{2}, pathAckTargets(h, held, 1, now, true), "the earlier-waiting target goes first")
		require.Equal(t, []protocol.PathID{1}, pathAckTargets(h, held, 1, now, true), "the own target follows within the bound")
	})
}
