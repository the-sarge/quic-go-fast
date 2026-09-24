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

func TestDeliverySamplerPacketIdentity(t *testing.T) {
	r := &congestionRecorder{}
	h := newCongestionTestHandler(r)
	now := monotime.Now()
	p0 := sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1000)
	p1 := sendCongestionTestPacket(h, now, protocol.EncryptionHandshake, 1200)
	require.Equal(t, p0, p1)
	_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(p0)}, protocol.EncryptionInitial, now.Add(100*time.Millisecond))
	require.NoError(t, err)
	require.Equal(t, uint64(1000), r.feedback[0].Delivery.Delivered)
	require.Equal(t, uint64(10000), r.feedback[0].Delivery.BytesPerSecond)
	_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(p1)}, protocol.EncryptionHandshake, now.Add(200*time.Millisecond))
	require.NoError(t, err)
	require.Equal(t, uint64(2200), r.feedback[1].Delivery.Delivered)
	require.Equal(t, uint64(11000), r.feedback[1].Delivery.BytesPerSecond)
	require.Equal(t, uint64(2), r.feedback[1].Delivery.Ordinal)
	_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(p1)}, protocol.EncryptionHandshake, now.Add(300*time.Millisecond))
	require.NoError(t, err)
	require.Len(t, r.feedback, 2)
}

func TestDeliverySamplerCompressedAndLimitedAck(t *testing.T) {
	r := &congestionRecorder{}
	h := newCongestionTestHandler(r)
	now := monotime.Now()
	p0 := sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1000)
	h.ObserveDeliveryLimitation(congestion.SendFlowControlLimited)
	p1 := sendCongestionTestPacket(h, now.Add(time.Second), protocol.EncryptionInitial, 1000)
	_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(p0, p1)}, protocol.EncryptionInitial, now.Add(1100*time.Millisecond))
	require.NoError(t, err)
	s := r.feedback[0].Delivery
	require.Equal(t, uint64(1818), s.BytesPerSecond)
	require.Equal(t, congestion.SendFlowControlLimited, s.Limited)
	require.Equal(t, uint64(2), s.Ordinal)
	// A newer nonmonotonic registration must invalidate the chosen anchor,
	// not let the sampler select the faster older candidate.
	p2 := sendCongestionTestPacket(h, now.Add(2*time.Second), protocol.EncryptionInitial, 1000)
	p3 := sendCongestionTestPacket(h, now.Add(1500*time.Millisecond), protocol.EncryptionInitial, 1000)
	_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(p2, p3)}, protocol.EncryptionInitial, now.Add(3*time.Second))
	require.NoError(t, err)
	require.False(t, r.feedback[1].Delivery.Valid)
	require.Equal(t, uint64(4), r.feedback[1].Delivery.Ordinal)
}

func TestDeliverySamplerLateOnlyAck(t *testing.T) {
	t.Run("compressed late ACK below measured minimum", func(t *testing.T) {
		r := &congestionRecorder{}
		h := newCongestionTestHandler(r)
		now := monotime.Now()
		pn := sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1000)
		_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pn)}, protocol.EncryptionInitial, now.Add(100*time.Millisecond))
		require.NoError(t, err)
		pn = sendCongestionTestPacket(h, now.Add(100*time.Millisecond), protocol.EncryptionInitial, 1000)
		require.True(t, h.QueueProbePacketAt(protocol.EncryptionInitial, now.Add(100*time.Millisecond)))
		_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pn)}, protocol.EncryptionInitial, now.Add(110*time.Millisecond))
		require.NoError(t, err)
		require.Equal(t, uint64(2000), r.feedback[1].Delivery.Delivered)
		require.False(t, r.feedback[1].Delivery.Valid)
		require.Zero(t, r.feedback[1].Delivery.RawRTT)
	})
	for _, pto := range []bool{false, true} {
		t.Run(map[bool]string{false: "loss", true: "PTO"}[pto], func(t *testing.T) {
			r := &congestionRecorder{}
			h := newCongestionTestHandler(r)
			now := monotime.Now()
			pn := sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1000)
			if pto {
				require.True(t, h.QueueProbePacket(protocol.EncryptionInitial))
			} else {
				last := sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1200)
				_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(last)}, protocol.EncryptionInitial, now.Add(100*time.Millisecond))
				require.NoError(t, err)
				require.NoError(t, h.OnLossDetectionTimeout(h.GetLossDetectionTimeout()))
			}
			before := len(r.feedback)
			_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pn)}, protocol.EncryptionInitial, now.Add(200*time.Millisecond))
			require.NoError(t, err)
			require.Len(t, r.feedback, before+1)
			e := r.feedback[before]
			require.Zero(t, e.PriorInFlight)
			require.Zero(t, e.PostInFlight)
			require.Equal(t, uint64(map[bool]int{true: 1000, false: 2200}[pto]), e.Delivery.Delivered)
			_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pn)}, protocol.EncryptionInitial, now.Add(250*time.Millisecond))
			require.NoError(t, err)
			require.Len(t, r.feedback, before+1)
		})
	}
}

func TestDeliverySamplerRawRTT(t *testing.T) {
	t.Run("registration ordering is independent of rate origin", func(t *testing.T) {
		for _, backward := range []bool{false, true} {
			r := &congestionRecorder{}
			h := newCongestionTestHandler(r)
			now := monotime.Now()
			if backward {
				pn := sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1000)
				_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pn)}, protocol.EncryptionInitial, now.Add(100*time.Millisecond))
				require.NoError(t, err)
				sendCongestionTestPacket(h, now.Add(200*time.Millisecond), protocol.EncryptionInitial, 1000)
			}
			h.congestionEvents.pending = func() protocol.ByteCount { return 1200 }
			pn := sendCongestionTestPacket(h, now.Add(190*time.Millisecond), protocol.EncryptionInitial, 1000)
			_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pn)}, protocol.EncryptionInitial, now.Add(195*time.Millisecond))
			require.NoError(t, err)
			sample := r.feedback[len(r.feedback)-1].Delivery
			if backward {
				require.Zero(t, sample.RawRTT)
				require.Equal(t, 100*time.Millisecond, h.congestionEvents.sampler.minimumRTT)
			} else {
				require.Equal(t, 5*time.Millisecond, sample.RawRTT, "missing bandwidth origin does not invalidate a monotonic RTT")
				require.False(t, sample.Valid)
			}
		}
	})
	r := &congestionRecorder{}
	h := newCongestionTestHandler(r)
	h.rttStats.SetInitialRTT(time.Second)
	now := monotime.Now()
	pn := sendCongestionTestPacket(h, now, protocol.Encryption1RTT, 1000)
	_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pn), DelayTime: 20 * time.Millisecond}, protocol.Encryption1RTT, now.Add(50*time.Millisecond))
	require.NoError(t, err)
	require.Equal(t, 50*time.Millisecond, r.feedback[0].Delivery.RawRTT)
	require.True(t, r.feedback[0].Delivery.Valid, "restored RTT is not a measured minimum")
	mtu := h.PopPacketNumber(protocol.Encryption1RTT)
	h.SentPacket(now.Add(time.Second), mtu, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, protocol.ECNNon, 1400, true, false)
	sendCongestionTestPacket(h, now.Add(time.Second), protocol.Encryption1RTT, 1000)
	_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(mtu)}, protocol.Encryption1RTT, now.Add(2*time.Second))
	require.NoError(t, err)
	require.Zero(t, r.feedback[1].Delivery.RawRTT)
	require.False(t, r.feedback[1].Delivery.Valid)
	require.Equal(t, uint64(2400), r.feedback[1].Delivery.Delivered)
	sendCongestionTestPacket(h, now.Add(2*time.Second), protocol.Encryption1RTT, 1000)
	require.Equal(t, now.Add(2*time.Second), r.sends[3].Packet.Delivery.DeliveredTime, "MTU receipts advance the delivery clock without becoming rate anchors")
}

func TestDeliverySamplerIdleExpiry(t *testing.T) {
	t.Run("pending work prevents a new origin", func(t *testing.T) {
		r := &congestionRecorder{}
		h := newCongestionTestHandler(r)
		now := monotime.Now()
		pending := protocol.ByteCount(1200)
		h.congestionEvents.pending = func() protocol.ByteCount { return pending }
		pn := sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1000)
		require.False(t, r.sends[0].Packet.Delivery.Valid)
		_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pn)}, protocol.EncryptionInitial, now.Add(time.Millisecond))
		require.NoError(t, err)
		require.False(t, r.feedback[0].Delivery.Valid)
		pn = sendCongestionTestPacket(h, now.Add(2*time.Millisecond), protocol.EncryptionInitial, 1000)
		require.False(t, r.sends[1].Packet.Delivery.Valid)
		pending = 0
		_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pn)}, protocol.EncryptionInitial, now.Add(3*time.Millisecond))
		require.NoError(t, err)
		sendCongestionTestPacket(h, now.Add(4*time.Millisecond), protocol.EncryptionInitial, 1000)
		require.True(t, r.sends[2].Packet.Delivery.Valid)
	})
	t.Run("retention is capped at thirty seconds", func(t *testing.T) {
		h := newCongestionTestHandler(&congestionRecorder{})
		now := monotime.Now()
		h.rttStats.UpdateRTT(time.Minute, 0)
		sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1000)
		require.True(t, h.QueueProbePacketAt(protocol.EncryptionInitial, now))
		require.Equal(t, now.Add(30*time.Second), h.DeliveryExpiry())
	})
	r := &congestionRecorder{}
	h := newCongestionTestHandler(r)
	now := monotime.Now()
	pn := sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1000)
	require.True(t, h.QueueProbePacketAt(protocol.EncryptionInitial, now))
	require.Equal(t, now.Add(600*time.Millisecond), h.DeliveryExpiry())
	h.ObserveDeliveryLimitation(congestion.SendApplicationLimited)
	require.False(t, h.DeliveryIdle(), "zero flight alone is not idle")
	h.ExpireDelivery(h.DeliveryExpiry())
	require.True(t, h.DeliveryExpiry().IsZero())
	require.False(t, h.DeliveryIdle(), "expiry is evidence loss, not proof of idle")
	_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pn)}, protocol.EncryptionInitial, now.Add(time.Second))
	require.NoError(t, err)
	require.Empty(t, r.feedback)
	next := sendCongestionTestPacket(h, now.Add(time.Second), protocol.EncryptionInitial, 1000)
	require.Greater(t, r.sends[1].Packet.SampleGeneration, r.sends[0].Packet.SampleGeneration)
	_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(next)}, protocol.EncryptionInitial, now.Add(1100*time.Millisecond))
	require.NoError(t, err)
	require.Equal(t, uint64(10000), r.feedback[0].Delivery.BytesPerSecond)
	h.ObserveDeliveryLimitation(congestion.SendApplicationLimited)
	require.True(t, h.DeliveryIdle())
	h.ObserveDeliveryLimitation(congestion.SendFlowControlLimited)
	require.False(t, h.DeliveryIdle())
}

func TestDeliverySamplerBoundedEviction(t *testing.T) {
	r := &congestionRecorder{}
	h := newCongestionTestHandler(r)
	now := monotime.Now()
	for range 4097 {
		sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1)
		require.True(t, h.QueueProbePacketAt(protocol.EncryptionInitial, now))
	}
	s := h.DeliveryStats()
	require.Equal(t, 4096, s.Retained)
	require.Equal(t, uint64(1), s.Evicted)
	_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(0)}, protocol.EncryptionInitial, now.Add(time.Millisecond))
	require.NoError(t, err)
	require.Empty(t, r.feedback, "the oldest ordinal no longer supplies evidence")
	_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: 0, Largest: 4096}}}, protocol.EncryptionInitial, now.Add(2*time.Millisecond))
	require.NoError(t, err)
	require.Equal(t, uint64(4096), r.feedback[0].Delivery.Delivered)
	require.Zero(t, h.DeliveryStats().Retained)
	// Direct registrations above recovery's ordinary send allowance can only
	// lose optional evidence, never mandatory frame or flight ownership.
	for range 25001 {
		sendCongestionTestPacket(h, now.Add(time.Second), protocol.EncryptionInitial, 1)
	}
	require.Equal(t, 25000, h.DeliveryStats().Live)
	require.Equal(t, uint64(1), h.DeliveryStats().Missing)
	require.Equal(t, protocol.ByteCount(25001), h.bytesInFlight)
}

func TestDeliverySamplerPTOAndLossRetirement(t *testing.T) {
	for _, mtu := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "MTU"}[mtu], func(t *testing.T) {
			r := &congestionRecorder{}
			h := newCongestionTestHandler(r)
			now := monotime.Now()
			pn := h.PopPacketNumber(protocol.EncryptionInitial)
			h.SentPacket(now, pn, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.PingFrame{}}}, protocol.EncryptionInitial, protocol.ECNNon, 1400, mtu, false)
			last := sendCongestionTestPacket(h, now.Add(90*time.Millisecond), protocol.EncryptionInitial, 1000)
			_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(last)}, protocol.EncryptionInitial, now.Add(100*time.Millisecond))
			require.NoError(t, err)
			_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pn)}, protocol.EncryptionInitial, now.Add(110*time.Millisecond))
			require.NoError(t, err)
			e := r.feedback[len(r.feedback)-1]
			require.Equal(t, congestion.DeliveryLost, e.Acked[0].Retirement)
			require.Equal(t, uint64(map[bool]int{false: 1400, true: 0}[mtu]), e.Delivery.Lost)
			require.Equal(t, uint64(2400), e.Delivery.Delivered)
			if mtu {
				require.False(t, e.Delivery.Valid)
			}
		})
	}
	r := &congestionRecorder{}
	h := newCongestionTestHandler(r)
	now := monotime.Now()
	pn := sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1000)
	require.True(t, h.QueueProbePacketAt(protocol.EncryptionInitial, now))
	_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pn)}, protocol.EncryptionInitial, now.Add(time.Millisecond))
	require.NoError(t, err)
	require.Equal(t, congestion.DeliveryPTO, r.feedback[0].Acked[0].Retirement)
	require.Zero(t, r.feedback[0].Delivery.Lost)
}

func TestDeliverySamplerSpaceRetryAndPathDisposal(t *testing.T) {
	for _, retry := range []bool{false, true} {
		t.Run(map[bool]string{false: "overlapping 0RTT rejection", true: "Retry with pending credit"}[retry], func(t *testing.T) {
			r := &congestionRecorder{}
			h := newCongestionTestHandler(r)
			now := monotime.Now()
			pending := protocol.ByteCount(0)
			h.congestionEvents.pending = func() protocol.ByteCount { return pending }
			level := protocol.Encryption1RTT
			if retry {
				level = protocol.EncryptionInitial
				sendCongestionTestPacket(h, now, level, 1000)
				h.ResetForRetry(now.Add(time.Millisecond))
				pending = 1200
				sendCongestionTestPacket(h, now.Add(2*time.Millisecond), level, 1000)
				pending = 0
			} else {
				sendCongestionTestPacket(h, now, protocol.Encryption0RTT, 1000)
				sendCongestionTestPacket(h, now, level, 1000)
				h.DropPackets(protocol.Encryption0RTT, now.Add(time.Millisecond))
			}
			pn := sendCongestionTestPacket(h, now.Add(3*time.Millisecond), level, 1200)
			_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pn)}, level, now.Add(4*time.Millisecond))
			require.NoError(t, err)
			require.True(t, r.feedback[0].Delivery.Valid, "a new-generation sample must not require draining old recovery")
			require.Equal(t, uint64(1200), r.feedback[0].Delivery.Delivered)
			require.Positive(t, h.DeliveryStats().Live+h.DeliveryStats().Retained, "the old transmission remains available to recovery/retirement")
			pn = sendCongestionTestPacket(h, now.Add(5*time.Millisecond), level, 1200)
			_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pn)}, level, now.Add(6*time.Millisecond))
			require.NoError(t, err)
			require.True(t, r.feedback[1].Delivery.Valid)
		})
	}
	for _, tc := range []struct {
		name    string
		level   protocol.EncryptionLevel
		dispose func(*sentPacketHandler, monotime.Time)
	}{
		{"Initial", protocol.EncryptionInitial, func(h *sentPacketHandler, t monotime.Time) { h.DropPackets(protocol.EncryptionInitial, t) }},
		{"Handshake", protocol.EncryptionHandshake, func(h *sentPacketHandler, t monotime.Time) { h.DropPackets(protocol.EncryptionHandshake, t) }},
		{"0RTT", protocol.Encryption0RTT, func(h *sentPacketHandler, t monotime.Time) { h.DropPackets(protocol.Encryption0RTT, t) }},
		{"Retry", protocol.EncryptionInitial, func(h *sentPacketHandler, t monotime.Time) { h.ResetForRetry(t) }},
		{"path", protocol.Encryption1RTT, func(h *sentPacketHandler, t monotime.Time) { h.MigratedPath(t, 1200) }},
		{"close", protocol.Encryption1RTT, func(h *sentPacketHandler, _ monotime.Time) { h.CloseDelivery() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &congestionRecorder{}
			h := newCongestionTestHandler(r)
			now := monotime.Now()
			sendCongestionTestPacket(h, now, tc.level, 1000)
			require.True(t, h.QueueProbePacketAt(tc.level, now))
			sendCongestionTestPacket(h, now, tc.level, 1000)
			tc.dispose(h, now.Add(time.Millisecond))
			require.Zero(t, h.DeliveryStats().Live)
			require.Zero(t, h.DeliveryStats().Retained)
			require.Zero(t, h.DeliveryStats().Outstanding)
			require.Empty(t, r.feedback)
		})
	}
	t.Run("rejected generation cannot update model", func(t *testing.T) {
		r := &congestionRecorder{}
		h := newCongestionTestHandler(r)
		now := monotime.Now()
		sendCongestionTestPacket(h, now, protocol.Encryption0RTT, 1000)
		pn := sendCongestionTestPacket(h, now, protocol.Encryption1RTT, 1200)
		h.DropPackets(protocol.Encryption0RTT, now)
		_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pn)}, protocol.Encryption1RTT, now.Add(time.Millisecond))
		require.NoError(t, err)
		require.Zero(t, r.feedback[0].Delivery.Delivered)
		require.Zero(t, r.feedback[0].Delivery.RawRTT)
		require.False(t, r.feedback[0].Delivery.Valid)
		require.Zero(t, h.bytesInFlight, "stale evidence still permits recovery ACK actions")
	})
}

func TestDeliverySamplerRenoHasNoSidecar(t *testing.T) {
	t.Run("pending authority is explicit", func(t *testing.T) {
		for _, authority := range []bool{false, true} {
			r := &congestionRecorder{}
			h := newSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, false, nil, protocol.PerspectiveServer, nil, utils.DefaultLogger, r)
			if authority {
				h.congestionEvents.pending = func() protocol.ByteCount { return 0 }
			}
			h.ObserveDeliveryLimitation(congestion.SendApplicationLimited)
			require.Equal(t, authority, h.DeliveryIdle())
			sendCongestionTestPacket(h, monotime.Now(), protocol.EncryptionInitial, 1000)
			require.Equal(t, authority, r.sends[0].Packet.Delivery.Valid)
		}
	})
	h := NewSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, false, nil, protocol.PerspectiveServer, nil, utils.DefaultLogger).(*sentPacketHandler)
	now := monotime.Now()
	pn := sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1000)
	require.Nil(t, h.congestionEvents)
	require.True(t, h.QueueProbePacketAt(protocol.EncryptionInitial, now))
	_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pn)}, protocol.EncryptionInitial, now.Add(time.Millisecond))
	require.NoError(t, err)
	require.Nil(t, h.congestionEvents)
	require.True(t, h.DeliveryExpiry().IsZero())
}
