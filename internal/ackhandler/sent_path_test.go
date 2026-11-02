package ackhandler

import (
	"slices"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

func TestSentPathIndependentLoss(t *testing.T) {
	h := NewSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, false, nil, protocol.PerspectiveClient, nil, utils.DefaultLogger).(*sentPacketHandler)
	now := monotime.Time(time.Second)
	h.DropPackets(protocol.EncryptionInitial, now)
	h.DropPackets(protocol.EncryptionHandshake, now)
	require.NoError(t, h.AddPath(17, 1200))
	var zero, other packetTracker
	for pn := range protocol.PacketNumber(4) {
		h.SentPacketWithContext(PacketContext{}, now, pn, protocol.InvalidPacketNumber, nil, []Frame{zero.NewPingFrame(pn)}, protocol.Encryption1RTT, protocol.ECNNon, 1200, false, false)
		h.SentPacketWithContext(PacketContext{PathID: 17, TupleGeneration: 2}, now, pn, protocol.InvalidPacketNumber, nil, []Frame{other.NewPingFrame(pn)}, protocol.Encryption1RTT, protocol.ECNNon, 1200, false, false)
	}
	receipt, err := h.ReceivedAck(&wire.AckFrame{HasPathID: true, PathID: 17, AckRanges: ackRanges(3)}, protocol.Encryption1RTT, now.Add(100*time.Millisecond))
	require.NoError(t, err)
	require.Equal(t, protocol.PathID(17), receipt.PathID)
	require.Equal(t, []protocol.PacketNumber{0}, other.Lost)
	require.Equal(t, []protocol.PacketNumber{3}, other.Acked)
	require.Empty(t, zero.Lost)
	require.Empty(t, zero.Acked)
	require.EqualValues(t, 4800, h.bytesInFlight)
	require.EqualValues(t, 38400, h.congestion.GetCongestionWindow())
}

func TestSentPathLocalCompletion(t *testing.T) {
	handler, err := NewLocalSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, false, nil, protocol.PerspectiveClient, nil, utils.DefaultLogger)
	require.NoError(t, err)
	h := handler.(*sentPacketHandler)
	now := monotime.Time(time.Second)
	h.DropPackets(protocol.EncryptionInitial, now)
	h.DropPackets(protocol.EncryptionHandshake, now)
	require.NoError(t, h.AddPath(17, 1200))
	path := h.RecoveryOnPath(17)
	pn := path.PopPacketNumber(protocol.Encryption1RTT)
	var tracker packetTracker
	token, err := h.RegisterLocalPacket(PacketContext{PathID: 17, TupleGeneration: 3}, now, pn, protocol.InvalidPacketNumber, nil, []Frame{tracker.NewPingFrame(pn)}, protocol.Encryption1RTT, protocol.ECNNon, 1200)
	require.NoError(t, err)
	require.True(t, h.GetLossDetectionTimeout().IsZero(), "pending registration cannot arm network PTO")
	require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now.Add(time.Millisecond)))
	require.False(t, h.GetLossDetectionTimeout().IsZero())
	_, err = h.ReceivedAck(&wire.AckFrame{HasPathID: true, PathID: 17, AckRanges: ackRanges(pn)}, protocol.Encryption1RTT, now.Add(20*time.Millisecond))
	require.NoError(t, err)
	require.Equal(t, []protocol.PacketNumber{pn}, tracker.Acked)
	require.Error(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now.Add(30*time.Millisecond)))
	h.CloseLocal(now.Add(time.Second))
	require.Equal(t, SendNone, path.SendMode(now.Add(time.Second)))
}

func newSentPathFixture(t *testing.T) *sentPacketHandler {
	t.Helper()
	h := NewSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, false, nil, protocol.PerspectiveClient, nil, utils.DefaultLogger).(*sentPacketHandler)
	now := monotime.Time(time.Second)
	h.DropPackets(protocol.EncryptionInitial, now)
	h.DropPackets(protocol.EncryptionHandshake, now)
	require.NoError(t, h.AddPath(17, 1200))
	return h
}

func sendOnRecoveryPath(h *sentPacketHandler, id protocol.PathID, now monotime.Time, frames []Frame) protocol.PacketNumber {
	pn := h.RecoveryOnPath(id).PopPacketNumber(protocol.Encryption1RTT)
	h.SentPacketWithContext(PacketContext{PathID: id, TupleGeneration: 2}, now, pn, protocol.InvalidPacketNumber, nil, frames, protocol.Encryption1RTT, protocol.ECNNon, 1200, false, false)
	return pn
}

func ackOnRecoveryPath(t *testing.T, h *sentPacketHandler, id protocol.PathID, now monotime.Time, pns ...protocol.PacketNumber) {
	t.Helper()
	_, err := h.ReceivedAck(&wire.AckFrame{HasPathID: true, PathID: id, AckRanges: ackRanges(slices.Clone(pns)...)}, protocol.Encryption1RTT, now)
	require.NoError(t, err)
}

func TestSentPathIndependentPTO(t *testing.T) {
	h := newSentPathFixture(t)
	now := monotime.Time(time.Second)
	zero := sendOnRecoveryPath(h, 0, now, []Frame{{Frame: &wire.PingFrame{}}})
	sendOnRecoveryPath(h, 17, now.Add(5*time.Millisecond), []Frame{{Frame: &wire.PingFrame{}}})
	first := h.GetLossDetectionTimeout()
	outcome, err := h.OnLossDetectionTimeout(first)
	require.NoError(t, err)
	require.True(t, outcome.PTO)
	require.Zero(t, outcome.PathID)
	next := h.GetLossDetectionTimeout()
	require.Equal(t, first.Add(5*time.Millisecond), next)
	outcome, err = h.OnLossDetectionTimeout(next)
	require.NoError(t, err)
	require.True(t, outcome.PTO)
	require.Equal(t, protocol.PathID(17), outcome.PathID)
	ackOnRecoveryPath(t, h, 0, next.Add(time.Millisecond), zero)
	require.Equal(t, SendPTOAppData, h.RecoveryOnPath(17).SendMode(next))
	require.False(t, h.GetLossDetectionTimeout().IsZero(), "ACK on zero cannot cancel the other path's PTO")
}

func TestSentPathUndoAndAdaptiveIsolation(t *testing.T) {
	h := newSentPathFixture(t)
	now := monotime.Time(time.Second)
	var zero, other packetTracker
	var pns []protocol.PacketNumber
	for pn := range protocol.PacketNumber(5) {
		sendOnRecoveryPath(h, 0, now.Add(time.Duration(pn)*time.Millisecond), []Frame{zero.NewPingFrame(pn)})
		pns = append(pns, sendOnRecoveryPath(h, 17, now.Add(time.Duration(pn)*time.Millisecond), []Frame{other.NewPingFrame(pn)}))
	}
	ackOnRecoveryPath(t, h, 17, now.Add(100*time.Millisecond), pns[4])
	otherOwner := h.path(17)
	require.EqualValues(t, 26880, otherOwner.congestion.GetCongestionWindow())
	ackOnRecoveryPath(t, h, 17, now.Add(105*time.Millisecond), pns...)
	require.EqualValues(t, 38400, otherOwner.congestion.GetCongestionWindow(), "only the proven path restores its cutback")
	require.EqualValues(t, 38400, h.congestion.GetCongestionWindow())
	ackOnRecoveryPath(t, h, 0, now.Add(106*time.Millisecond), pns[4])
	require.Equal(t, pns[:2], zero.Lost, "proof on 17 cannot train zero's adaptive threshold")
	var fresh []protocol.PacketNumber
	for range 5 {
		next, _ := h.RecoveryOnPath(17).PeekPacketNumber(protocol.Encryption1RTT)
		fresh = append(fresh, sendOnRecoveryPath(h, 17, now.Add(110*time.Millisecond), []Frame{other.NewPingFrame(next)}))
	}
	ackOnRecoveryPath(t, h, 17, now.Add(210*time.Millisecond), fresh[4])
	require.Len(t, other.Lost, 2, "fresh packet-distance loss is suppressed on the trained path")
	require.False(t, otherOwner.PathLossDetectionTimeout(17).IsZero())
}

func TestSentPathAggregateCapacity(t *testing.T) {
	h := newSentPathFixture(t)
	now := monotime.Time(time.Second)
	h.appDataPackets.pns = newSequentialPacketNumberGenerator(0)
	h.path(17).appDataPackets.pns = newSequentialPacketNumberGenerator(0)
	// Both paths individually remain below their history and congestion limits.
	for n := range protocol.MaxTrackedSentPackets {
		id := protocol.PathID(0)
		if n%2 != 0 {
			id = 17
		}
		pn := h.RecoveryOnPath(id).PopPacketNumber(protocol.Encryption1RTT)
		h.SentPacketWithContext(PacketContext{PathID: id}, now, pn, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, protocol.ECNNon, 1, false, false)
	}
	require.Equal(t, SendNone, h.SendMode(now))
	require.Equal(t, SendNone, h.RecoveryOnPath(17).SendMode(now))
	require.False(t, h.CanSendPathProbe())
	require.False(t, h.RecoveryOnPath(17).CanSendPathProbe())
	require.Equal(t, protocol.MaxTrackedSentPackets, h.numTrackedPackets())
}

func TestSentPathACKCarrierPruning(t *testing.T) {
	h := newSentPathFixture(t)
	received := NewReceivedPacketHandler(utils.DefaultLogger)
	require.NoError(t, received.AddPath(17))
	now := monotime.Time(time.Second)
	for _, id := range []protocol.PathID{0, 17} {
		require.NoError(t, received.ReceivedPacketOnPath(id, 9, protocol.ECNNon, now, true))
	}
	h.ignorePacketsBelow = received.IgnorePacketsBelowOnPath
	h.path(17).ignorePacketsBelow = received.IgnorePacketsBelowOnPath
	pn := sendOnRecoveryPath(h, 17, now, []Frame{
		{Frame: &wire.AckFrame{AckRanges: ackRanges(9)}},
		{Frame: &wire.AckFrame{HasPathID: true, PathID: 17, AckRanges: ackRanges(9)}},
		{Frame: &wire.PingFrame{}},
	})
	ackOnRecoveryPath(t, h, 17, now.Add(time.Millisecond), pn)
	require.Nil(t, received.GetAckFrame(protocol.Encryption1RTT, now, false))
	require.Nil(t, received.GetPathAckFrame(17, now, false))
}

func TestSentPathAdmissionAndUnsupportedModes(t *testing.T) {
	h := newSentPathFixture(t)
	require.Error(t, h.AddPath(17, 1200))
	require.Error(t, h.AddPath(18, 1200))
	require.Nil(t, h.RecoveryOnPath(99))
	_, err := h.ReceivedAck(&wire.AckFrame{HasPathID: true, PathID: 99, AckRanges: ackRanges(0)}, protocol.Encryption1RTT, monotime.Time(time.Second))
	require.Error(t, err)
	require.Panics(t, func() { EnableBBR(h, 1200, func() protocol.ByteCount { return 0 }) })
	require.Panics(t, func() { EnableBBR(h.RecoveryOnPath(17), 1200, func() protocol.ByteCount { return 0 }) })
	for _, level := range []protocol.EncryptionLevel{protocol.EncryptionInitial, protocol.EncryptionHandshake, protocol.Encryption0RTT} {
		require.Panics(t, func() {
			h.SentPacketWithContext(PacketContext{PathID: 17}, monotime.Time(time.Second), 0, protocol.InvalidPacketNumber, nil, nil, level, protocol.ECNNon, 1200, false, false)
		})
	}
	require.Zero(t, h.connStats.BytesSent.Load())
	fresh := NewSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, false, nil, protocol.PerspectiveClient, nil, utils.DefaultLogger).(*sentPacketHandler)
	require.Error(t, fresh.AddPath(17, 1200), "handshake spaces remain unique before confirmation")
}

func TestSentPathIndependentPacing(t *testing.T) {
	h := newSentPathFixture(t)
	now := monotime.Time(time.Second)
	for range 32 {
		sendOnRecoveryPath(h, 0, now, []Frame{{Frame: &wire.PingFrame{}}})
	}
	require.NotEqual(t, SendAny, h.SendMode(now))
	require.Equal(t, SendAny, h.RecoveryOnPath(17).SendMode(now))
	require.True(t, h.TimeUntilSend().After(now))
	require.True(t, h.RecoveryOnPath(17).TimeUntilSend().IsZero())
}

func TestSentPathRetainedGenerationCarrier(t *testing.T) {
	h := newSentPathFixture(t)
	received := NewReceivedPacketHandler(utils.DefaultLogger)
	now := monotime.Time(time.Second)
	require.NoError(t, received.ReceivedPacketOnPath(0, 9, protocol.ECNNon, now, true))
	h.path(17).ignorePacketsBelow = received.IgnorePacketsBelowOnPath
	pn := sendOnRecoveryPath(h, 17, now, []Frame{{Frame: &wire.AckFrame{AckRanges: ackRanges(9)}}, {Frame: &wire.PingFrame{}}})
	sendOnRecoveryPath(h, 0, now, []Frame{{Frame: &wire.PingFrame{}}})
	h.RecoveryOnPath(17).MigratedPath(now.Add(time.Millisecond), 1200)
	// The old tuple's carrier receipt still belongs to path 17. It can prune
	// zero's receive watermark, but cannot ACK zero's same-number transmission.
	ackOnRecoveryPath(t, h, 17, now.Add(2*time.Millisecond), pn)
	require.Nil(t, received.GetAckFrame(protocol.Encryption1RTT, now, false))
	require.EqualValues(t, 1200, h.bytesInFlight)
	require.EqualValues(t, 38400, h.congestion.GetCongestionWindow())
	require.Equal(t, utils.DefaultInitialRTT, h.path(17).rttStats.SmoothedRTT(), "old generation ACK cannot sample the replacement tuple")
}

func TestSentPathSkipCapacity(t *testing.T) {
	h := newSentPathFixture(t)
	now := monotime.Time(time.Second)
	h.appDataPackets.pns = newSequentialPacketNumberGenerator(0)
	h.path(17).appDataPackets.pns = newSequentialPacketNumberGenerator(0)
	for n := range protocol.MaxTrackedSentPackets - 1 {
		id := protocol.PathID(0)
		if n%2 != 0 {
			id = 17
		}
		pn := h.RecoveryOnPath(id).PopPacketNumber(protocol.Encryption1RTT)
		h.SentPacketWithContext(PacketContext{PathID: id}, now, pn, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, protocol.ECNNon, 1, false, false)
	}
	next := h.appDataPackets.pns.Peek()
	h.appDataPackets.pns = &skippingPacketNumberGenerator{next: next, nextToSkip: next, period: 1, maxPeriod: 2}
	require.Equal(t, SendNone, h.SendMode(now), "one remaining slot cannot hold both the skip marker and packet")
	require.False(t, h.CanSendPathProbe())
}

func TestSentPathAdmissionPreservesExistingFlight(t *testing.T) {
	h := NewSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, false, nil, protocol.PerspectiveClient, nil, utils.DefaultLogger).(*sentPacketHandler)
	now := monotime.Time(time.Second)
	h.DropPackets(protocol.EncryptionInitial, now)
	h.DropPackets(protocol.EncryptionHandshake, now)
	var tracked packetTracker
	pn := h.PopPacketNumber(protocol.Encryption1RTT)
	h.SentPacketWithContext(PacketContext{}, now, pn, protocol.InvalidPacketNumber, nil, []Frame{tracked.NewPingFrame(pn)}, protocol.Encryption1RTT, protocol.ECNNon, 1200, false, false)
	deadline := h.GetLossDetectionTimeout()
	require.NoError(t, h.AddPath(17, 1200))
	require.Equal(t, deadline, h.GetLossDetectionTimeout())
	ackOnRecoveryPath(t, h, 0, now.Add(10*time.Millisecond), pn)
	require.Equal(t, []protocol.PacketNumber{pn}, tracked.Acked)
	require.Zero(t, h.bytesInFlight)
	require.Equal(t, SendAny, h.RecoveryOnPath(17).SendMode(now))
}
