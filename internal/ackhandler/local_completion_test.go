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
	token, err := h.RegisterLocalPacket(PacketContext{}, now, pn, protocol.InvalidPacketNumber, nil, frames, level, protocol.ECNNon, 1200)
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
	t.Run("current path-zero context", func(t *testing.T) {
		h := newLocalTestHandler(t)
		pn := h.PopPacketNumber(protocol.Encryption1RTT)
		_, err := h.RegisterLocalPacket(PacketContext{PathID: 1}, now, pn, protocol.InvalidPacketNumber, nil, nil, protocol.Encryption1RTT, protocol.ECNNon, 1200)
		require.Error(t, err)
		context := PacketContext{TupleGeneration: 7}
		token, err := h.RegisterLocalPacket(context, now, pn, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, protocol.ECNNon, 1200)
		require.NoError(t, err)
		context.TupleGeneration++
		_, packet := h.(*sentPacketHandler).appDataPackets.history.FirstOutstanding()
		require.Equal(t, PacketContext{TupleGeneration: 7}, packet.Context)
		require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now.Add(time.Millisecond)))
		require.Panics(t, func() {
			h.SentPacketWithContext(PacketContext{}, now, pn+1, protocol.InvalidPacketNumber, nil, nil, protocol.Encryption1RTT, protocol.ECNNon, 1200, false, false)
		})
	})
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
	for _, wait := range []time.Duration{20 * time.Millisecond, 100 * time.Millisecond} {
		t.Run("qualified age "+wait.String(), func(t *testing.T) {
			h := newLocalTestHandler(t).(*sentPacketHandler)
			now := monotime.Now()
			h.DropPackets(protocol.EncryptionInitial, now)
			h.DropPackets(protocol.EncryptionHandshake, now)
			h.rttStats.UpdateRTT(5*time.Millisecond, 0)
			var tracker packetTracker
			pn, token := registerLocalTestPacket(t, h, now, protocol.Encryption1RTT, []Frame{tracker.NewPingFrame(0)})
			require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now.Add(wait)))
			var higher protocol.PacketNumber
			for i := 1; i <= 3; i++ {
				n, tok := registerLocalTestPacket(t, h, now.Add(wait), protocol.Encryption1RTT, []Frame{tracker.NewPingFrame(protocol.PacketNumber(i))})
				require.NoError(t, h.CompleteLocalPacket(tok, LocalPacketAccepted, now.Add(wait)))
				higher = n
			}
			window := h.congestion.GetCongestionWindow()
			_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(higher)}, protocol.Encryption1RTT, now.Add(wait+time.Millisecond))
			require.NoError(t, err)
			require.Equal(t, []protocol.PacketNumber{0}, tracker.Lost)
			require.Less(t, h.congestion.GetCongestionWindow(), window)
			_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pn)}, protocol.Encryption1RTT, now.Add(wait+2*time.Millisecond))
			require.NoError(t, err)
			require.GreaterOrEqual(t, h.congestion.GetCongestionWindow(), window, "qualified loss receipt permits Reno undo")
			require.True(t, h.packetThresholdDisabled)
			require.Equal(t, uint8(defaultTimeThresholdEighths), h.timeThresholdEighths, "local wait is not network reordering")
		})
	}
	for _, expire := range []bool{false, true} {
		t.Run(map[bool]string{false: "unordered receipts", true: "unordered expiry"}[expire], func(t *testing.T) {
			h := newLocalTestHandler(t).(*sentPacketHandler)
			now := monotime.Now()
			h.DropPackets(protocol.EncryptionInitial, now)
			h.DropPackets(protocol.EncryptionHandshake, now)
			h.rttStats.UpdateRTT(10*time.Millisecond, 0)
			var tracker packetTracker
			var pns [6]protocol.PacketNumber
			var tokens [6]LocalCompletionToken
			for i := range pns {
				var frames []Frame
				if i == 0 || i == 2 || i == 5 {
					frames = []Frame{tracker.NewPingFrame(protocol.PacketNumber(i))}
				}
				pns[i], tokens[i] = registerLocalTestPacket(t, h, now, protocol.Encryption1RTT, frames)
				if i != 0 {
					require.NoError(t, h.CompleteLocalPacket(tokens[i], LocalPacketAccepted, now.Add(3*time.Millisecond)))
				}
			}
			window := h.congestion.GetCongestionWindow()
			_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pns[5])}, protocol.Encryption1RTT, now.Add(4*time.Millisecond))
			require.NoError(t, err)
			// An earlier native return is consumed later, after the higher ACK.
			require.NoError(t, h.CompleteLocalPacket(tokens[0], LocalPacketAccepted, now.Add(time.Millisecond)))
			require.Equal(t, []protocol.PacketNumber{2, 0}, tracker.Lost)
			if expire {
				at := now.Add(3*h.rttStats.PTO(false) + 2*time.Millisecond)
				_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pns[5])}, protocol.Encryption1RTT, at)
				require.NoError(t, err)
				var retained []protocol.PacketNumber
				for pn := range h.lostPackets.All() {
					retained = append(retained, pn)
				}
				require.Equal(t, []protocol.PacketNumber{pns[2]}, retained, "expiry examines every bounded receipt")
				_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pns[2])}, protocol.Encryption1RTT, at.Add(time.Microsecond))
				require.NoError(t, err)
				require.True(t, h.packetThresholdDisabled, "the retained fresh receipt remains usable")
			} else {
				_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pns[0], pns[2], pns[5])}, protocol.Encryption1RTT, now.Add(5*time.Millisecond))
				require.NoError(t, err)
				require.GreaterOrEqual(t, h.congestion.GetCongestionWindow(), window)
				require.Empty(t, h.lostPackets.lostPackets)
			}
		})
	}
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
	token, err := h.RegisterLocalPacket(PacketContext{}, now, pn, protocol.InvalidPacketNumber, []StreamFrame{{Frame: &wire.StreamFrame{StreamID: 4, Data: []byte("reliable")}, Handler: &customFrameHandler{onLost: func(wire.Frame) { streamReturns++ }}}}, []Frame{tracker.NewPingFrame(0), {Frame: &wire.DatagramFrame{Data: []byte("discard")}}}, protocol.EncryptionInitial, protocol.ECNNon, 1200)
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
	for _, outcome := range []LocalPacketOutcome{localPacketPending, LocalPacketUnsubmitted, LocalPacketAccepted, LocalPacketUncertain} {
		t.Run(map[LocalPacketOutcome]string{localPacketPending: "pending distance", LocalPacketUnsubmitted: "unsubmitted distance", LocalPacketAccepted: "accepted distance", LocalPacketUncertain: "uncertain distance"}[outcome], func(t *testing.T) {
			h := newLocalTestHandler(t).(*sentPacketHandler)
			now := monotime.Now()
			h.rttStats.UpdateRTT(5*time.Millisecond, 0)
			var tracker packetTracker
			_, token := registerLocalTestPacket(t, h, now, protocol.EncryptionInitial, []Frame{tracker.NewPingFrame(0)})
			require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now.Add(time.Millisecond)))
			for i := 1; i <= 2; i++ {
				_, tok := registerLocalTestPacket(t, h, now, protocol.EncryptionInitial, nil)
				if outcome != localPacketPending {
					require.NoError(t, h.CompleteLocalPacket(tok, outcome, now.Add(time.Millisecond)))
				}
			}
			higher, tok := registerLocalTestPacket(t, h, now, protocol.EncryptionInitial, []Frame{tracker.NewPingFrame(3)})
			require.NoError(t, h.CompleteLocalPacket(tok, LocalPacketAccepted, now.Add(time.Millisecond)))
			window := h.congestion.GetCongestionWindow()
			_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(higher)}, protocol.EncryptionInitial, now.Add(2*time.Millisecond))
			require.NoError(t, err)
			if outcome == LocalPacketAccepted || outcome == LocalPacketUncertain {
				require.Equal(t, []protocol.PacketNumber{0}, tracker.Lost, "qualified positive control still detects packet loss")
				require.Less(t, h.congestion.GetCongestionWindow(), window)
			} else {
				require.Empty(t, tracker.Lost)
				require.GreaterOrEqual(t, h.congestion.GetCongestionWindow(), window)
			}
		})
	}
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
				_, err := h.RegisterLocalPacket(PacketContext{}, now, 0, protocol.InvalidPacketNumber, nil, nil, protocol.Encryption1RTT, protocol.ECNNon, 1200)
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
	for _, generatorSkip := range []bool{true, false} {
		name := "capacity and PTO without generator skip"
		if generatorSkip {
			name = "capacity and PTO with generator skip"
		}
		t.Run(name, func(t *testing.T) {
			h := newLocalTestHandler(t)
			owner := h.(*sentPacketHandler)
			now := monotime.Now()
			var release [4]LocalCompletionToken
			for i := range protocol.MaxTrackedSentPackets - 4 {
				_, token := registerLocalTestPacket(t, h, now, protocol.EncryptionInitial, nil)
				if i < len(release) {
					release[i] = token
				}
			}
			h.DropPackets(protocol.EncryptionInitial, now)
			h.DropPackets(protocol.EncryptionHandshake, now)
			generator := owner.appDataPackets.pns.(*skippingPacketNumberGenerator)
			generator.nextToSkip = 4
			if generatorSkip {
				generator.nextToSkip = 3
			}
			var tracker packetTracker
			for i := range 3 {
				_, token := registerLocalTestPacket(t, h, now, protocol.Encryption1RTT, []Frame{tracker.NewPingFrame(protocol.PacketNumber(i))})
				require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now))
			}
			require.Equal(t, SendAck, h.SendMode(now))
			pn := h.PopPacketNumber(protocol.Encryption1RTT)
			frames := []Frame{tracker.NewPingFrame(pn)}
			register := func() error {
				token, err := h.RegisterLocalPacket(PacketContext{}, now, pn, protocol.InvalidPacketNumber, nil, frames, protocol.Encryption1RTT, protocol.ECNNon, 1200)
				if err == nil {
					require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now))
				}
				return err
			}
			if generatorSkip {
				require.ErrorIs(t, register(), ErrLocalCompletionCapacity)
			} else {
				require.NoError(t, register())
			}
			next, _ := h.PeekPacketNumber(protocol.Encryption1RTT)
			deadline := h.GetLossDetectionTimeout()
			require.False(t, deadline.IsZero())
			outcome, err := h.OnLossDetectionTimeout(deadline)
			require.NoError(t, err)
			require.True(t, outcome.PTO)
			require.Equal(t, protocol.Encryption1RTT, outcome.EncLevel)
			after, _ := h.PeekPacketNumber(protocol.Encryption1RTT)
			require.Equal(t, next, after, "PTO cannot consume a held PN or exceed capacity")
			require.EqualValues(t, 1, owner.ptoCount)
			require.EqualValues(t, 2, owner.numProbesToSend)
			require.Equal(t, protocol.MaxTrackedSentPackets, owner.numTrackedPackets())
			require.NoError(t, h.CompleteLocalPacket(release[0], LocalPacketUnsubmitted, now))
			if generatorSkip {
				require.NoError(t, register(), "retry the original PN after releasing capacity")
			}
			for _, token := range release[1:] {
				require.NoError(t, h.CompleteLocalPacket(token, LocalPacketUnsubmitted, now))
			}
			outcome, err = h.OnLossDetectionTimeout(deadline.Add(time.Second))
			require.NoError(t, err)
			require.True(t, outcome.PTO)
			after, _ = h.PeekPacketNumber(protocol.Encryption1RTT)
			require.Greater(t, after, next, "ordinary PTO marker resumes after settlement and capacity release")
			require.EqualValues(t, 2, owner.ptoCount)
			require.LessOrEqual(t, owner.numTrackedPackets(), protocol.MaxTrackedSentPackets)
			_, token := registerLocalTestPacket(t, h, now, protocol.Encryption1RTT, nil)
			require.NoError(t, h.CompleteLocalPacket(token, LocalPacketUnsubmitted, now))
			for range 4 {
				require.True(t, h.QueueProbePacket(protocol.Encryption1RTT))
			}
			require.False(t, h.QueueProbePacket(protocol.Encryption1RTT))
			require.Equal(t, []protocol.PacketNumber{0, 1, 2, pn}, tracker.Lost)
			require.Empty(t, tracker.Acked)
		})
	}
	for _, action := range []string{"Retry", "0-RTT rejection"} {
		t.Run("held application PN through "+action, func(t *testing.T) {
			h := newLocalTestHandler(t)
			owner := h.(*sentPacketHandler)
			now := monotime.Now()
			var first LocalCompletionToken
			for i := range protocol.MaxTrackedSentPackets - 2 {
				_, token := registerLocalTestPacket(t, h, now, protocol.EncryptionInitial, nil)
				if i == 0 {
					first = token
				}
			}
			generator := owner.appDataPackets.pns.(*skippingPacketNumberGenerator)
			generator.nextToSkip = 3
			var tracker packetTracker
			var old [2]LocalCompletionToken
			for i := range old {
				_, old[i] = registerLocalTestPacket(t, h, now, protocol.Encryption0RTT, []Frame{tracker.NewPingFrame(protocol.PacketNumber(i))})
			}
			pn := h.PopPacketNumber(protocol.Encryption0RTT)
			require.EqualValues(t, 2, pn)
			frames := []Frame{tracker.NewPingFrame(pn)}
			_, err := h.RegisterLocalPacket(PacketContext{}, now, pn, protocol.InvalidPacketNumber, nil, frames, protocol.Encryption0RTT, protocol.ECNNon, 1200)
			require.ErrorIs(t, err, ErrLocalCompletionCapacity)
			require.Equal(t, generator.nextToSkip, generator.next, "pending skip immediately follows held PN")
			if action == "Retry" {
				h.ResetForRetry(now)
			} else {
				h.DropPackets(protocol.EncryptionInitial, now)
				h.DropPackets(protocol.Encryption0RTT, now)
			}
			lost := append([]protocol.PacketNumber(nil), tracker.Lost...)
			for _, token := range old {
				require.NoError(t, h.CompleteLocalPacket(token, LocalPacketUnsubmitted, now))
			}
			require.NoError(t, h.CompleteLocalPacket(first, LocalPacketUnsubmitted, now))
			token, err := h.RegisterLocalPacket(PacketContext{}, now, pn, protocol.InvalidPacketNumber, nil, frames, protocol.Encryption0RTT, protocol.ECNNon, 1200)
			require.NoError(t, err, "first typed registration of the retained PN")
			require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now))
			next, token := registerLocalTestPacket(t, h, now, protocol.Encryption1RTT, []Frame{tracker.NewPingFrame(4)})
			require.EqualValues(t, 4, next, "the intervening generator skip survives Retry")
			require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now))
			require.LessOrEqual(t, owner.numTrackedPackets(), protocol.MaxTrackedSentPackets)
			_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: next, Largest: next}, {Smallest: pn, Largest: pn}}}, protocol.Encryption1RTT, now.Add(time.Millisecond))
			require.NoError(t, err)
			require.Equal(t, []protocol.PacketNumber{2, 4}, tracker.Acked)
			require.Equal(t, lost, tracker.Lost, "late outcomes cannot requeue retired frames")
		})
	}
	t.Run("capacity retained through early ACK and Retry", func(t *testing.T) {
		h := newLocalTestHandler(t)
		now := monotime.Now()
		var first LocalCompletionToken
		for i := range protocol.MaxTrackedSentPackets {
			_, token := registerLocalTestPacket(t, h, now, protocol.EncryptionInitial, []Frame{{Frame: &wire.PingFrame{}}})
			if i == 0 {
				first = token
			}
		}
		pn := h.PopPacketNumber(protocol.EncryptionInitial)
		_, err := h.RegisterLocalPacket(PacketContext{}, now, pn, protocol.InvalidPacketNumber, nil, nil, protocol.EncryptionInitial, protocol.ECNNon, 1200)
		require.ErrorIs(t, err, ErrLocalCompletionCapacity)
		// Retire an interior record before the prefix, then all remaining history.
		_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: pn - 1, Largest: pn - 1}}}, protocol.EncryptionInitial, now.Add(time.Millisecond))
		require.NoError(t, err)
		_, err = h.RegisterLocalPacket(PacketContext{}, now, pn, protocol.InvalidPacketNumber, nil, nil, protocol.EncryptionInitial, protocol.ECNNon, 1200)
		require.ErrorIs(t, err, ErrLocalCompletionCapacity)
		_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: 0, Largest: pn - 1}}}, protocol.EncryptionInitial, now.Add(time.Millisecond))
		require.NoError(t, err)
		h.ResetForRetry(now.Add(2 * time.Millisecond))
		next, _ := h.PeekPacketNumber(protocol.EncryptionInitial)
		require.Greater(t, next, pn)
		_, err = h.RegisterLocalPacket(PacketContext{}, now, pn, protocol.InvalidPacketNumber, nil, nil, protocol.EncryptionInitial, protocol.ECNNon, 1200)
		require.ErrorIs(t, err, ErrLocalCompletionCapacity)
		require.Equal(t, SendNone, h.SendMode(now))
		require.NoError(t, h.CompleteLocalPacket(first, LocalPacketAccepted, now.Add(3*time.Millisecond)))
		_, err = h.RegisterLocalPacket(PacketContext{}, now.Add(4*time.Millisecond), pn, protocol.InvalidPacketNumber, nil, nil, protocol.EncryptionInitial, protocol.ECNNon, 1200)
		require.NoError(t, err)
	})
}
