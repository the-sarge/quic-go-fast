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
