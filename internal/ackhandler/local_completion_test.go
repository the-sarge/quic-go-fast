package ackhandler

import (
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

func newLocalTestHandler(t *testing.T) LocalSentPacketHandler {
	t.Helper()
	h, err := NewLocalSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, false, nil, protocol.PerspectiveServer, nil, utils.DefaultLogger)
	require.NoError(t, err)
	return h
}

func registerLocalTestPacket(t *testing.T, h LocalSentPacketHandler, now monotime.Time, level protocol.EncryptionLevel, frames []Frame) (protocol.PacketNumber, LocalCompletionToken) {
	t.Helper()
	pn := h.PopPacketNumber(level)
	token, err := h.RegisterLocalPacket(now, pn, protocol.InvalidPacketNumber, nil, frames, level, protocol.ECNNon, 1200)
	require.NoError(t, err)
	return pn, token
}

func TestLocalCompletionPendingOnly(t *testing.T) {
	now := monotime.Now()
	for _, level := range []protocol.EncryptionLevel{protocol.EncryptionInitial, protocol.EncryptionHandshake, protocol.Encryption0RTT, protocol.Encryption1RTT} {
		t.Run(level.String(), func(t *testing.T) {
			h := newLocalTestHandler(t)
			if level == protocol.Encryption1RTT {
				h.DropPackets(protocol.EncryptionHandshake, now)
			}
			_, before := h.(interface {
				SendAllowance(monotime.Time) (SendMode, protocol.ByteCount)
			}).SendAllowance(now)
			var tracker packetTracker
			_, token := registerLocalTestPacket(t, h, now, level, []Frame{tracker.NewPingFrame(0)})
			_, after := h.(interface {
				SendAllowance(monotime.Time) (SendMode, protocol.ByteCount)
			}).SendAllowance(now)
			require.Equal(t, before-1200, after, "pending registration reserves congestion capacity")
			require.Zero(t, h.GetLossDetectionTimeout())
			outcome, err := h.OnLossDetectionTimeout(now.Add(time.Second))
			require.NoError(t, err)
			require.False(t, outcome.PTO)
			require.False(t, h.QueueProbePacket(level))
			require.Empty(t, tracker.Lost)
			require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now.Add(time.Second)))
			if level != protocol.Encryption0RTT {
				require.Greater(t, h.GetLossDetectionTimeout(), now.Add(time.Second))
			}
		})
	}
	for _, registered := range []bool{false, true} {
		h := newLocalTestHandler(t)
		if registered {
			registerLocalTestPacket(t, h, now, protocol.EncryptionInitial, nil)
		}
		require.Panics(t, func() { EnableDeliverySampling(h, &congestionRecorder{}, func() protocol.ByteCount { return 0 }) })
		require.Panics(t, func() { EnableBBR(h, 1200, func() protocol.ByteCount { return 0 }) })
		require.Panics(t, func() { EnableBBRECN(h, func(bool) (uint64, bool, bool) { return 0, true, true }) })
		require.Panics(t, func() {
			h.SentPacket(now, 1, protocol.InvalidPacketNumber, nil, nil, protocol.EncryptionInitial, protocol.ECNNon, 1200, false, false)
		})
	}
	_, err := NewLocalSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, true, nil, protocol.PerspectiveServer, nil, utils.DefaultLogger)
	require.Error(t, err)
}

func TestLocalCompletionHigherACK(t *testing.T) {
	h := newLocalTestHandler(t)
	now := monotime.Now()
	var tracker packetTracker
	_, pending := registerLocalTestPacket(t, h, now, protocol.EncryptionInitial, []Frame{tracker.NewPingFrame(0)})
	var higher protocol.PacketNumber
	for i := 1; i <= 4; i++ {
		pn, token := registerLocalTestPacket(t, h, now, protocol.EncryptionInitial, []Frame{tracker.NewPingFrame(protocol.PacketNumber(i))})
		require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now.Add(time.Second)))
		higher = pn
	}
	_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: 1, Largest: higher}}}, protocol.EncryptionInitial, now.Add(2*time.Second))
	require.NoError(t, err)
	require.Empty(t, tracker.Lost, "higher authenticated ACK cannot declare pending packet lost")
	require.Zero(t, h.GetLossDetectionTimeout())
	require.NoError(t, h.CompleteLocalPacket(pending, LocalPacketUnsubmitted, now.Add(3*time.Second)))
	require.Equal(t, []protocol.PacketNumber{0}, tracker.Lost)
}

func TestLocalCompletionMixedPTO(t *testing.T) {
	for _, disposition := range []LocalPacketOutcome{LocalPacketAccepted, LocalPacketUncertain} {
		h := newLocalTestHandler(t)
		now := monotime.Now()
		var tracker packetTracker
		registerLocalTestPacket(t, h, now, protocol.EncryptionInitial, []Frame{tracker.NewPingFrame(0)})
		_, token := registerLocalTestPacket(t, h, now, protocol.EncryptionInitial, []Frame{tracker.NewPingFrame(1)})
		require.NoError(t, h.CompleteLocalPacket(token, disposition, now.Add(time.Second)))
		require.Greater(t, h.GetLossDetectionTimeout(), now.Add(time.Second))
		outcome, err := h.OnLossDetectionTimeout(h.GetLossDetectionTimeout())
		require.NoError(t, err)
		require.True(t, outcome.PTO)
		require.True(t, h.QueueProbePacket(protocol.EncryptionInitial))
		require.Equal(t, []protocol.PacketNumber{1}, tracker.Lost)
		require.False(t, h.QueueProbePacket(protocol.EncryptionInitial))
		require.Zero(t, h.GetLossDetectionTimeout())
	}
}

func TestLocalCompletionUnsubmitted(t *testing.T) {
	h := newLocalTestHandler(t)
	now := monotime.Now()
	var tracker packetTracker
	_, before := h.(interface {
		SendAllowance(monotime.Time) (SendMode, protocol.ByteCount)
	}).SendAllowance(now)
	pn := h.PopPacketNumber(protocol.EncryptionInitial)
	streamReturns := 0
	token, err := h.RegisterLocalPacket(now, pn, protocol.InvalidPacketNumber, []StreamFrame{{Frame: &wire.StreamFrame{StreamID: 4, Data: []byte("reliable")}, Handler: &customFrameHandler{onLost: func(wire.Frame) { streamReturns++ }}}}, []Frame{tracker.NewPingFrame(0), {Frame: &wire.DatagramFrame{Data: []byte("discard")}}}, protocol.EncryptionInitial, protocol.ECNNon, 1200)
	require.NoError(t, err)
	require.NoError(t, h.CompleteLocalPacket(token, LocalPacketUnsubmitted, now.Add(time.Second)))
	require.Equal(t, []protocol.PacketNumber{0}, tracker.Lost)
	require.Empty(t, tracker.Acked)
	_, after := h.(interface {
		SendAllowance(monotime.Time) (SendMode, protocol.ByteCount)
	}).SendAllowance(now)
	require.Equal(t, before, after, "local disposal is not congestion loss or delivery")
	require.Greater(t, h.PopPacketNumber(protocol.EncryptionInitial), pn)
	require.Error(t, h.CompleteLocalPacket(token, LocalPacketUnsubmitted, now.Add(2*time.Second)))
	require.Len(t, tracker.Lost, 1)
	require.Equal(t, 1, streamReturns)
	require.Zero(t, h.GetLossDetectionTimeout())
}

func TestLocalCompletionEarlyACK(t *testing.T) {
	for _, disposition := range []LocalPacketOutcome{LocalPacketAccepted, LocalPacketUncertain, LocalPacketUnsubmitted} {
		h := newLocalTestHandler(t)
		now := monotime.Now()
		var tracker packetTracker
		pn, token := registerLocalTestPacket(t, h, now, protocol.EncryptionInitial, []Frame{tracker.NewPingFrame(0)})
		ack := &wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: pn, Largest: pn}}}
		_, err := h.ReceivedAck(ack, protocol.EncryptionInitial, now.Add(time.Second))
		require.NoError(t, err)
		_, err = h.ReceivedAck(ack, protocol.EncryptionInitial, now.Add(2*time.Second))
		require.NoError(t, err)
		require.False(t, h.(*sentPacketHandler).rttStats.HasMeasurement())
		err = h.CompleteLocalPacket(token, disposition, now.Add(3*time.Second))
		if disposition == LocalPacketUnsubmitted {
			require.ErrorIs(t, err, ErrLocalCompletionContradiction)
		} else {
			require.NoError(t, err)
		}
		require.Equal(t, []protocol.PacketNumber{0}, tracker.Acked)
		require.Empty(t, tracker.Lost)
		require.Zero(t, h.GetLossDetectionTimeout())
		require.Error(t, h.CompleteLocalPacket(token, disposition, now.Add(4*time.Second)))
	}
}

func TestLocalCompletionTerminalLifetime(t *testing.T) {
	for _, action := range []string{"retry", "initial", "handshake", "0rtt", "close"} {
		t.Run(action, func(t *testing.T) {
			h := newLocalTestHandler(t)
			now := monotime.Now()
			level := protocol.EncryptionInitial
			if action == "handshake" {
				level = protocol.EncryptionHandshake
			}
			if action == "0rtt" {
				level = protocol.Encryption0RTT
			}
			var tracker packetTracker
			_, token := registerLocalTestPacket(t, h, now, level, []Frame{tracker.NewPingFrame(0)})
			switch action {
			case "retry":
				h.ResetForRetry(now.Add(time.Second))
			case "close":
				h.CloseLocal(now.Add(time.Second))
			default:
				h.DropPackets(level, now.Add(time.Second))
			}
			lost := len(tracker.Lost)
			var replacementPN protocol.PacketNumber
			var replacement LocalCompletionToken
			if action != "close" {
				replacementPN, replacement = registerLocalTestPacket(t, h, now.Add(time.Second), protocol.Encryption1RTT, []Frame{tracker.NewPingFrame(100)})
			} else {
				_, err := h.RegisterLocalPacket(now, 0, protocol.InvalidPacketNumber, nil, nil, protocol.Encryption1RTT, protocol.ECNNon, 1200)
				require.Error(t, err)
				require.False(t, h.QueueProbePacket(protocol.EncryptionInitial))
			}
			fresh := newLocalTestHandler(t)
			require.Error(t, fresh.CompleteLocalPacket(token, LocalPacketAccepted, now.Add(2*time.Second)))
			require.NoError(t, h.CompleteLocalPacket(token, LocalPacketUnsubmitted, now.Add(2*time.Second)))
			require.Error(t, h.CompleteLocalPacket(token, LocalPacketUnsubmitted, now.Add(3*time.Second)))
			require.Len(t, tracker.Lost, lost)
			require.Empty(t, tracker.Acked)
			require.False(t, h.(*sentPacketHandler).rttStats.HasMeasurement())
			if action != "close" {
				require.NoError(t, h.CompleteLocalPacket(replacement, LocalPacketAccepted, now.Add(3*time.Second)))
				_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: replacementPN, Largest: replacementPN}}}, protocol.Encryption1RTT, now.Add(4*time.Second))
				require.NoError(t, err)
				require.Equal(t, []protocol.PacketNumber{100}, tracker.Acked)
				require.Len(t, tracker.Lost, lost)
			}
		})
	}
	t.Run("capacity retained through early ACK and Retry", func(t *testing.T) {
		h := newLocalTestHandler(t)
		now := monotime.Now()
		var first LocalCompletionToken
		for i := 0; i < protocol.MaxTrackedSentPackets; i++ {
			_, token := registerLocalTestPacket(t, h, now, protocol.EncryptionInitial, []Frame{{Frame: &wire.PingFrame{}}})
			if i == 0 {
				first = token
			}
		}
		pn, _ := h.PeekPacketNumber(protocol.EncryptionInitial)
		_, err := h.RegisterLocalPacket(now, pn, protocol.InvalidPacketNumber, nil, nil, protocol.EncryptionInitial, protocol.ECNNon, 1200)
		require.ErrorIs(t, err, ErrLocalCompletionCapacity)
		// Retire an interior record before the prefix, then all remaining history.
		_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: pn - 1, Largest: pn - 1}}}, protocol.EncryptionInitial, now.Add(time.Millisecond))
		require.NoError(t, err)
		_, err = h.RegisterLocalPacket(now, pn, protocol.InvalidPacketNumber, nil, nil, protocol.EncryptionInitial, protocol.ECNNon, 1200)
		require.ErrorIs(t, err, ErrLocalCompletionCapacity)
		_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: 0, Largest: pn - 1}}}, protocol.EncryptionInitial, now.Add(time.Millisecond))
		require.NoError(t, err)
		h.ResetForRetry(now.Add(2 * time.Millisecond))
		pn, _ = h.PeekPacketNumber(protocol.EncryptionInitial)
		_, err = h.RegisterLocalPacket(now, pn, protocol.InvalidPacketNumber, nil, nil, protocol.EncryptionInitial, protocol.ECNNon, 1200)
		require.ErrorIs(t, err, ErrLocalCompletionCapacity)
		require.Equal(t, SendNone, h.SendMode(now))
		require.NoError(t, h.CompleteLocalPacket(first, LocalPacketAccepted, now.Add(3*time.Millisecond)))
		registerLocalTestPacket(t, h, now.Add(4*time.Millisecond), protocol.EncryptionInitial, nil)
	})
}
