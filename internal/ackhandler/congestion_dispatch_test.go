package ackhandler

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

type congestionRecorder struct {
	sends      []congestion.SendEvent
	feedback   []congestion.FeedbackEvent
	onFeedback func(congestion.FeedbackEvent)
}

func (r *congestionRecorder) Sent(e congestion.SendEvent) { r.sends = append(r.sends, e) }
func (r *congestionRecorder) Feedback(e congestion.FeedbackEvent) {
	if r.onFeedback != nil {
		r.onFeedback(e)
	}
	e.Acked = slices.Clone(e.Acked)
	e.Lost = slices.Clone(e.Lost)
	r.feedback = append(r.feedback, e)
}

func newCongestionTestHandler(r *congestionRecorder) *sentPacketHandler {
	return newSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, false, nil, protocol.PerspectiveServer, nil, utils.DefaultLogger, r)
}

func sendCongestionTestPacket(h *sentPacketHandler, now monotime.Time, level protocol.EncryptionLevel, size protocol.ByteCount) protocol.PacketNumber {
	pn := h.PopPacketNumber(level)
	h.SentPacket(now, pn, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.PingFrame{}}}, level, protocol.ECNNon, size, false, false)
	return pn
}

func TestCongestionEventPriorAndPostFlight(t *testing.T) {
	r := &congestionRecorder{}
	h := newCongestionTestHandler(r)
	now := monotime.Now()
	p0 := sendCongestionTestPacket(h, now.Add(-time.Second), protocol.EncryptionInitial, 1000)
	p1 := sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1100)
	p2 := sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1200)
	require.Equal(t, protocol.ByteCount(0), r.sends[0].PriorInFlight)
	require.Equal(t, protocol.ByteCount(1000), r.sends[0].PostInFlight)
	require.Equal(t, protocol.ByteCount(2100), r.sends[2].PriorInFlight)
	require.Equal(t, protocol.ByteCount(3300), r.sends[2].PostInFlight)
	_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(p2)}, protocol.EncryptionInitial, now.Add(100*time.Millisecond))
	require.NoError(t, err)
	require.Len(t, r.feedback, 1)
	e := r.feedback[0]
	require.True(t, e.HasAck)
	require.True(t, e.RTTUpdated)
	require.Equal(t, protocol.ByteCount(3300), e.PriorInFlight)
	require.Equal(t, protocol.ByteCount(1100), e.PostInFlight)
	require.Len(t, e.Acked, 1)
	require.Len(t, e.Lost, 1)
	require.Equal(t, p2, e.Acked[0].PacketNumber)
	require.Equal(t, p0, e.Lost[0].PacketNumber)
	require.Equal(t, protocol.ByteCount(1200), e.Acked[0].Length)
	require.Equal(t, protocol.ByteCount(1000), e.Lost[0].Length)
	require.NotEqual(t, p1, e.Lost[0].PacketNumber)
}

func TestCongestionEventTimerOnlyLoss(t *testing.T) {
	r := &congestionRecorder{}
	h := newCongestionTestHandler(r)
	now := monotime.Now()
	p0 := sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1000)
	p1 := sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1200)
	_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(p1)}, protocol.EncryptionInitial, now.Add(time.Second))
	require.NoError(t, err)
	require.Empty(t, r.feedback[0].Lost)
	timeout := h.GetLossDetectionTimeout()
	require.NoError(t, h.OnLossDetectionTimeout(timeout))
	require.Len(t, r.feedback, 2)
	e := r.feedback[1]
	require.False(t, e.HasAck)
	require.False(t, e.RTTEligible)
	require.False(t, e.RTTUpdated)
	require.False(t, e.ECNChecked)
	require.Empty(t, e.Acked)
	require.Equal(t, timeout, e.Time)
	require.Equal(t, protocol.ByteCount(1000), e.PriorInFlight)
	require.Zero(t, e.PostInFlight)
	require.Len(t, e.Lost, 1)
	require.Equal(t, p0, e.Lost[0].PacketNumber)
	// A later PTO creates no loss or synthetic feedback.
	sendCongestionTestPacket(h, timeout, protocol.EncryptionInitial, 1000)
	require.NoError(t, h.OnLossDetectionTimeout(h.GetLossDetectionTimeout()))
	require.Len(t, r.feedback, 2)
}

func TestCongestionEventSpaceAndOrdinal(t *testing.T) {
	r := &congestionRecorder{}
	h := newCongestionTestHandler(r)
	now := monotime.Now()
	for _, level := range []protocol.EncryptionLevel{protocol.EncryptionInitial, protocol.EncryptionHandshake, protocol.Encryption0RTT, protocol.Encryption1RTT} {
		sendCongestionTestPacket(h, now, level, 1000)
	}
	for i, e := range r.sends {
		require.Equal(t, uint64(i+1), e.Packet.Ordinal)
		require.Zero(t, e.Packet.PathGeneration)
		require.Zero(t, e.Packet.SampleGeneration)
	}
	require.Equal(t, protocol.EncryptionInitial, r.sends[0].Packet.Space)
	require.Equal(t, protocol.EncryptionHandshake, r.sends[1].Packet.Space)
	require.Equal(t, protocol.Encryption1RTT, r.sends[2].Packet.Space)
	require.Equal(t, protocol.Encryption1RTT, r.sends[3].Packet.Space)
	require.Equal(t, protocol.PacketNumber(0), r.sends[2].Packet.PacketNumber)
	require.Equal(t, protocol.PacketNumber(1), r.sends[3].Packet.PacketNumber)
	retryRecorder := &congestionRecorder{}
	retryHandler := newCongestionTestHandler(retryRecorder)
	sendCongestionTestPacket(retryHandler, now, protocol.EncryptionInitial, 1000)
	sendCongestionTestPacket(retryHandler, now, protocol.Encryption0RTT, 1000)
	retryHandler.ResetForRetry(now.Add(time.Millisecond))
	sendCongestionTestPacket(retryHandler, now, protocol.EncryptionInitial, 1000)
	require.Equal(t, uint64(3), retryRecorder.sends[2].Packet.Ordinal)
	require.Equal(t, uint64(1), retryRecorder.sends[2].Packet.SampleGeneration)
	require.Zero(t, retryRecorder.sends[2].Packet.PathGeneration)
	h.DropPackets(protocol.EncryptionInitial, now)
	h.DropPackets(protocol.EncryptionHandshake, now)
	h.MigratedPath(now, 1200)
	pn := sendCongestionTestPacket(h, now, protocol.Encryption1RTT, 1000)
	require.Equal(t, uint64(5), r.sends[4].Packet.Ordinal)
	require.Equal(t, uint64(1), r.sends[4].Packet.SampleGeneration)
	require.Equal(t, uint64(1), r.sends[4].Packet.PathGeneration)
	_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pn)}, protocol.Encryption1RTT, now.Add(time.Second))
	require.NoError(t, err)
	require.Equal(t, r.sends[4].Packet, r.feedback[0].Acked[0])
	require.Equal(t, uint64(1), r.feedback[0].PathGeneration)
	require.Equal(t, uint64(1), r.feedback[0].SampleGeneration)
}

func TestCongestionEventBorrowLifetime(t *testing.T) {
	t.Run("borrow ends at dispatch", func(t *testing.T) {
		r := &congestionRecorder{}
		var borrowed []congestion.PacketInfo
		r.onFeedback = func(e congestion.FeedbackEvent) { borrowed = e.Acked }
		h := newCongestionTestHandler(r)
		now := monotime.Now()
		pn := sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1234)
		_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pn)}, protocol.EncryptionInitial, now.Add(time.Second))
		require.NoError(t, err)
		require.Equal(t, []congestion.PacketInfo{{}}, borrowed)
		require.Empty(t, h.congestionEvents.packets)
		// Reuse pooled packets and the dispatch scratch. Retained copies stay values.
		for range 2 {
			next := sendCongestionTestPacket(h, now.Add(time.Second), protocol.EncryptionInitial, 999)
			_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(next)}, protocol.EncryptionInitial, now.Add(2*time.Second))
			require.NoError(t, err)
		}
		require.Equal(t, protocol.ByteCount(1234), r.feedback[0].Acked[0].Length)
		require.Equal(t, now, r.feedback[0].Acked[0].SendTime)
		require.Equal(t, uint64(1), r.feedback[0].Acked[0].Ordinal)
	})
	for _, tc := range []struct {
		name      string
		level     protocol.EncryptionLevel
		pathProbe bool
		retire    func(*sentPacketHandler, monotime.Time)
	}{
		{"initial keys", protocol.EncryptionInitial, false, func(h *sentPacketHandler, now monotime.Time) { h.DropPackets(protocol.EncryptionInitial, now) }},
		{"handshake keys", protocol.EncryptionHandshake, false, func(h *sentPacketHandler, now monotime.Time) { h.DropPackets(protocol.EncryptionHandshake, now) }},
		{"rejected 0RTT", protocol.Encryption0RTT, false, func(h *sentPacketHandler, now monotime.Time) { h.DropPackets(protocol.Encryption0RTT, now) }},
		{"retry", protocol.EncryptionInitial, false, func(h *sentPacketHandler, now monotime.Time) { h.ResetForRetry(now) }},
		{"PTO extraction", protocol.EncryptionInitial, false, func(h *sentPacketHandler, _ monotime.Time) {
			require.True(t, h.QueueProbePacket(protocol.EncryptionInitial))
		}},
		{"migration", protocol.Encryption1RTT, false, func(h *sentPacketHandler, now monotime.Time) { h.MigratedPath(now, 1200) }},
		{"path probe timeout", protocol.Encryption1RTT, true, func(h *sentPacketHandler, now monotime.Time) {
			h.DropPackets(protocol.EncryptionHandshake, now)
			require.NoError(t, h.OnLossDetectionTimeout(now.Add(2*time.Second)))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &congestionRecorder{}
			h := newCongestionTestHandler(r)
			now := monotime.Now()
			pn := h.PopPacketNumber(tc.level)
			h.SentPacket(now, pn, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.PingFrame{}, Handler: &customFrameHandler{}}}, tc.level, protocol.ECNNon, 1000, false, tc.pathProbe)
			tc.retire(h, now.Add(time.Millisecond))
			require.Empty(t, h.congestionEvents.packets)
			require.Empty(t, r.feedback, "disposal is not delivery or congestion loss")
		})
	}
}

// The tap records the accepted legacy callback contract while forwarding every
// call to the real Reno sender. It does not replace recovery or controller logic.
type congestionLegacyTap struct {
	congestion.SendAlgorithmWithDebugInfos
	order *[]string
}

func (c *congestionLegacyTap) OnPacketSent(now monotime.Time, flight protocol.ByteCount, pn protocol.PacketNumber, size protocol.ByteCount, eliciting bool) {
	*c.order = append(*c.order, fmt.Sprintf("sent %d flight %d", pn, flight))
	c.SendAlgorithmWithDebugInfos.OnPacketSent(now, flight, pn, size, eliciting)
}

func (c *congestionLegacyTap) MaybeExitSlowStart() {
	*c.order = append(*c.order, "RTT/slow-start")
	c.SendAlgorithmWithDebugInfos.MaybeExitSlowStart()
}

func (c *congestionLegacyTap) OnCongestionEvent(pn protocol.PacketNumber, size, flight protocol.ByteCount) {
	*c.order = append(*c.order, fmt.Sprintf("congestion %d bytes %d flight %d", pn, size, flight))
	c.SendAlgorithmWithDebugInfos.OnCongestionEvent(pn, size, flight)
}

func (c *congestionLegacyTap) OnPacketAcked(pn protocol.PacketNumber, size, flight protocol.ByteCount, now monotime.Time) {
	*c.order = append(*c.order, fmt.Sprintf("acked %d flight %d", pn, flight))
	c.SendAlgorithmWithDebugInfos.OnPacketAcked(pn, size, flight, now)
}

func TestCongestionDispatchPreservesRenoOrder(t *testing.T) {
	for _, rich := range []bool{false, true} {
		t.Run(fmt.Sprintf("rich=%t", rich), func(t *testing.T) {
			var order []string
			r := &congestionRecorder{onFeedback: func(congestion.FeedbackEvent) { order = append(order, "feedback") }}
			var sink congestionEventSink
			if rich {
				sink = r
			}
			h := newSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, true, nil, protocol.PerspectiveServer, nil, utils.DefaultLogger, sink)
			h.congestion = &congestionLegacyTap{SendAlgorithmWithDebugInfos: h.congestion, order: &order}
			now := monotime.Now()
			for i := range 4 {
				pn := h.PopPacketNumber(protocol.Encryption1RTT)
				sent := now
				if i < 2 {
					sent = now.Add(-time.Second)
				}
				frame := Frame{Frame: &wire.PingFrame{}, Handler: &customFrameHandler{
					onAcked: func(wire.Frame) { order = append(order, fmt.Sprintf("frame ack %d", pn)) },
					onLost:  func(wire.Frame) { order = append(order, fmt.Sprintf("frame loss %d", pn)) },
				}}
				h.SentPacket(sent, pn, protocol.InvalidPacketNumber, nil, []Frame{frame}, protocol.Encryption1RTT, h.ECNMode(true), 1000, i == 1, false)
			}
			require.Equal(t, []string{"sent 0 flight 1000", "sent 1 flight 2000", "sent 2 flight 3000", "sent 3 flight 4000"}, order)
			order = nil
			_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(2, 3), ECT0: 1, ECNCE: 1}, protocol.Encryption1RTT, now.Add(100*time.Millisecond))
			require.NoError(t, err)
			expected := []string{"frame ack 2", "frame ack 3", "RTT/slow-start", "congestion 3 bytes 0 flight 4000", "frame loss 0", "congestion 0 bytes 1000 flight 4000", "frame loss 1", "acked 2 flight 4000", "acked 3 flight 4000"}
			if rich {
				expected = append(expected, "feedback")
			}
			require.Equal(t, expected, order)
			require.Zero(t, h.bytesInFlight)
			require.Equal(t, 100*time.Millisecond, h.rttStats.LatestRTT())
			if rich {
				require.Len(t, r.feedback, 1)
				require.True(t, r.feedback[0].ECNChecked)
				require.True(t, r.feedback[0].Congested)
				require.Len(t, r.feedback[0].Lost, 1, "MTU loss and CE are not real congestion-loss bytes")
				require.Empty(t, h.congestionEvents.packets)
			} else {
				require.Nil(t, h.congestionEvents)
			}
		})
	}
}

func TestCongestionLegacyLateOnlyAck(t *testing.T) {
	r := &congestionRecorder{}
	h := newCongestionTestHandler(r)
	now := monotime.Now()
	lost := sendCongestionTestPacket(h, now.Add(-time.Second), protocol.EncryptionInitial, 1000)
	acked := sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1000)
	_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(acked)}, protocol.EncryptionInitial, now.Add(100*time.Millisecond))
	require.NoError(t, err)
	require.Len(t, r.feedback, 1)
	rtt := h.rttStats.LatestRTT()
	var order []string
	h.congestion = &congestionLegacyTap{SendAlgorithmWithDebugInfos: h.congestion, order: &order}
	for _, pn := range []protocol.PacketNumber{lost, acked} {
		_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pn)}, protocol.EncryptionInitial, now.Add(time.Second))
		require.NoError(t, err)
	}
	require.Empty(t, order)
	require.Len(t, r.feedback, 1)
	require.Equal(t, rtt, h.rttStats.LatestRTT())
	// Rejected ACKs cannot enter the private event path.
	_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(acked + 1)}, protocol.EncryptionInitial, now.Add(time.Second))
	require.Error(t, err)
	require.Len(t, r.feedback, 1)
}
