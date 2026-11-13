package ackhandler

import (
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

// newRichPathFixture is a confirmed rich root with one rich nonzero context. Each
// context reports to its own recorder, and neither has pending local work.
func newRichPathFixture(t *testing.T) (h *sentPacketHandler, root, path *congestionRecorder) {
	t.Helper()
	root, path = &congestionRecorder{}, &congestionRecorder{}
	h = newCongestionTestHandler(root)
	now := monotime.Time(time.Second)
	h.DropPackets(protocol.EncryptionInitial, now)
	h.DropPackets(protocol.EncryptionHandshake, now)
	_, err := h.addRichPath(17, 1200, utils.NewRTTStats(), path, func() protocol.ByteCount { return 0 })
	require.NoError(t, err)
	return h, root, path
}

// newLocalRichPathFixture is the granted-endpoint form, whose registrations start
// pending-local.
func newLocalRichPathFixture(t *testing.T) (h *sentPacketHandler, path *congestionRecorder) {
	t.Helper()
	handler, err := NewLocalSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, false, nil, protocol.PerspectiveClient, nil, utils.DefaultLogger)
	require.NoError(t, err)
	EnableDeliverySampling(handler, &congestionRecorder{}, func() protocol.ByteCount { return 0 })
	h = handler.(*sentPacketHandler)
	now := monotime.Time(time.Second)
	h.DropPackets(protocol.EncryptionInitial, now)
	h.DropPackets(protocol.EncryptionHandshake, now)
	path = &congestionRecorder{}
	_, err = h.addRichPath(17, 1200, utils.NewRTTStats(), path, func() protocol.ByteCount { return 0 })
	require.NoError(t, err)
	return h, path
}

func sendRichPathPacket(h *sentPacketHandler, id protocol.PathID, now monotime.Time) protocol.PacketNumber {
	return sendOnRecoveryPath(h, id, now, []Frame{{Frame: &wire.PingFrame{}}})
}

func pathAck(t *testing.T, h *sentPacketHandler, id, carrier protocol.PathID, now monotime.Time, pns ...protocol.PacketNumber) {
	t.Helper()
	ack := &wire.AckFrame{HasPathID: id != 0, PathID: id, AckRanges: ackRanges(slices.Clone(pns)...)}
	_, err := h.ReceivedPathAck(ack, protocol.Encryption1RTT, now, carrier)
	require.NoError(t, err)
}

func lastFeedback(t *testing.T, r *congestionRecorder) congestion.FeedbackEvent {
	t.Helper()
	require.NotEmpty(t, r.feedback)
	return r.feedback[len(r.feedback)-1]
}

func TestBBRPathSamePNIndependentSamples(t *testing.T) {
	h, root, path := newRichPathFixture(t)
	now := monotime.Time(2 * time.Second)
	z0 := sendRichPathPacket(h, 0, now)
	p0 := sendRichPathPacket(h, 17, now)
	z1 := sendRichPathPacket(h, 0, now.Add(10*time.Millisecond))
	p1 := sendRichPathPacket(h, 17, now.Add(10*time.Millisecond))
	require.Equal(t, z0, p0, "each path numbers its own space")
	require.Equal(t, z1, p1)

	pathAck(t, h, 17, 17, now.Add(100*time.Millisecond), p0, p1)
	require.Len(t, path.feedback, 1)
	require.Empty(t, root.feedback, "a path's ACK reaches only its own controller")
	s := lastFeedback(t, path).Delivery
	require.Equal(t, uint64(2400), s.Delivered)
	require.True(t, s.Valid)
	require.NotZero(t, s.RawRTT)
	require.True(t, h.RecoveryOnPath(17).(*sentPacketHandler).rttStats.HasMeasurement())
	require.False(t, h.rttStats.HasMeasurement())

	pathAck(t, h, 0, 0, now.Add(200*time.Millisecond), z0)
	require.Len(t, root.feedback, 1)
	s = lastFeedback(t, root).Delivery
	require.Equal(t, uint64(1200), s.Delivered, "the root counts only its own deliveries")
	require.True(t, s.Valid)
	require.Equal(t, uint64(1200*time.Second/(200*time.Millisecond)), s.BytesPerSecond)
	require.Len(t, path.feedback, 1)
}

func TestBBRPathCrossPathAckNoSample(t *testing.T) {
	t.Run("known carrier", func(t *testing.T) {
		h, root, path := newRichPathFixture(t)
		now := monotime.Time(2 * time.Second)
		pn := sendRichPathPacket(h, 17, now)
		ctx := h.RecoveryOnPath(17).(*sentPacketHandler)
		require.Equal(t, protocol.ByteCount(1200), ctx.bytesInFlight)
		pathAck(t, h, 17, 0, now.Add(50*time.Millisecond), pn)
		require.Zero(t, ctx.bytesInFlight, "a cross-path ACK still retires flight")
		e := lastFeedback(t, path)
		require.Equal(t, uint64(1200), e.Delivery.Delivered, "logical delivery counts once")
		require.False(t, e.Delivery.Valid)
		require.Zero(t, e.Delivery.RawRTT)
		require.False(t, e.RTTEligible)
		require.False(t, ctx.rttStats.HasMeasurement(), "PTO keeps the initial RTT")
		require.Empty(t, root.feedback)

		zero := sendRichPathPacket(h, 0, now.Add(60*time.Millisecond))
		pathAck(t, h, 0, 17, now.Add(90*time.Millisecond), zero)
		require.False(t, lastFeedback(t, root).Delivery.Valid, "path zero is excluded the same way")
		require.False(t, h.rttStats.HasMeasurement())

		// The excluded ACK still orders samples: a reordered same-path ACK of an
		// older packet takes no RTT sample.
		older := sendRichPathPacket(h, 17, now.Add(100*time.Millisecond))
		newer := sendRichPathPacket(h, 17, now.Add(110*time.Millisecond))
		pathAck(t, h, 17, 0, now.Add(150*time.Millisecond), newer)
		pathAck(t, h, 17, 17, now.Add(400*time.Millisecond), older)
		require.False(t, ctx.rttStats.HasMeasurement())
	})

	t.Run("unknown carrier", func(t *testing.T) {
		h, _, path := newRichPathFixture(t)
		now := monotime.Time(2 * time.Second)
		pn := sendRichPathPacket(h, 17, now)
		ackOnRecoveryPath(t, h, 17, now.Add(50*time.Millisecond), pn)
		e := lastFeedback(t, path)
		require.Equal(t, uint64(1200), e.Delivery.Delivered)
		require.False(t, e.Delivery.Valid, "an ACK without carrier provenance cannot train a rich path")
		require.False(t, h.RecoveryOnPath(17).(*sentPacketHandler).rttStats.HasMeasurement())
	})

	t.Run("Reno contexts ignore the carrier", func(t *testing.T) {
		h := newSentPathFixture(t)
		now := monotime.Time(2 * time.Second)
		pn := sendOnRecoveryPath(h, 17, now, []Frame{{Frame: &wire.PingFrame{}}})
		pathAck(t, h, 17, 0, now.Add(50*time.Millisecond), pn)
		require.True(t, h.RecoveryOnPath(17).(*sentPacketHandler).rttStats.HasMeasurement())
	})
}

func TestBBRPathFreshAnchorAfterCrossPathInterval(t *testing.T) {
	h, _, path := newRichPathFixture(t)
	now := monotime.Time(2 * time.Second)
	a := sendRichPathPacket(h, 17, now)
	b := sendRichPathPacket(h, 17, now.Add(10*time.Millisecond))
	pathAck(t, h, 17, 17, now.Add(100*time.Millisecond), a)
	require.True(t, lastFeedback(t, path).Delivery.Valid)
	c := sendRichPathPacket(h, 17, now.Add(110*time.Millisecond))
	pathAck(t, h, 17, 0, now.Add(150*time.Millisecond), b)
	require.False(t, lastFeedback(t, path).Delivery.Valid)

	pathAck(t, h, 17, 17, now.Add(300*time.Millisecond), c)
	e := lastFeedback(t, path)
	require.Equal(t, uint64(3600), e.Delivery.Delivered)
	require.False(t, e.Delivery.Valid, "an interval bridging the excluded ACK stays invalid")

	d := sendRichPathPacket(h, 17, now.Add(400*time.Millisecond))
	pathAck(t, h, 17, 17, now.Add(500*time.Millisecond), d)
	e = lastFeedback(t, path)
	require.True(t, e.Delivery.Valid, "a fresh path-local anchor samples again")
	require.Equal(t, uint64(4800), e.Delivery.Delivered)
	// Only d's bytes enter the rate: the excluded ACK does not re-enter
	// through the cumulative counter.
	require.Equal(t, uint64(1200*time.Second)/uint64(e.Delivery.Interval), e.Delivery.BytesPerSecond)
	require.LessOrEqual(t, e.Delivery.BytesPerSecond, uint64(1200*time.Second/(100*time.Millisecond)))
}

func TestBBRPathOldGenerationFeedback(t *testing.T) {
	h, root, path := newRichPathFixture(t)
	now := monotime.Time(2 * time.Second)
	ctx := h.RecoveryOnPath(17).(*sentPacketHandler)
	old := sendRichPathPacket(h, 17, now)
	zero := sendRichPathPacket(h, 0, now)
	ctx.MigratedPath(now.Add(5*time.Millisecond), 1200)
	require.Equal(t, uint64(1), ctx.congestionEvents.pathGeneration)
	require.Zero(t, h.congestionEvents.pathGeneration, "migration is path scoped")
	fresh := sendRichPathPacket(h, 17, now.Add(10*time.Millisecond))
	pathAck(t, h, 17, 17, now.Add(100*time.Millisecond), old, fresh)
	e := lastFeedback(t, path)
	require.Equal(t, uint64(1), e.PathGeneration)
	for _, p := range e.Acked {
		require.Equal(t, uint64(1), p.PathGeneration, "old-generation feedback cannot reach the new model")
	}
	require.Equal(t, uint64(1200), e.Delivery.Delivered)
	require.True(t, e.Delivery.Valid)
	require.Zero(t, ctx.bytesInFlight)

	pathAck(t, h, 0, 0, now.Add(120*time.Millisecond), zero)
	require.Equal(t, uint64(1200), lastFeedback(t, root).Delivery.Delivered)
	require.True(t, lastFeedback(t, root).Delivery.Valid)
}

func TestBBRPathAggregateDeliveryStore(t *testing.T) {
	h, _, path := newRichPathFixture(t)
	ctx := h.RecoveryOnPath(17).(*sentPacketHandler)
	now := monotime.Time(2 * time.Second)
	var tracker packetTracker
	var zero []protocol.PacketNumber
	for range maxDeliveryLive - 10 {
		zero = append(zero, sendRichPathPacket(h, 0, now))
	}
	var pns []protocol.PacketNumber
	for range 20 {
		pn := ctx.PopPacketNumber(protocol.Encryption1RTT)
		h.SentPacketWithContext(PacketContext{PathID: 17, TupleGeneration: 2}, now, pn, protocol.InvalidPacketNumber, nil, []Frame{tracker.NewPingFrame(pn)}, protocol.Encryption1RTT, protocol.ECNNon, 1200, false, false)
		pns = append(pns, pn)
	}
	rootStats, pathStats := h.DeliveryStats(), ctx.DeliveryStats()
	require.Equal(t, maxDeliveryLive, rootStats.Live+pathStats.Live, "the contexts share one live ceiling")
	require.Equal(t, uint64(10), pathStats.Missing)
	require.Equal(t, protocol.ByteCount(20*1200), ctx.bytesInFlight, "optional evidence never limits recovery")

	pathAck(t, h, 17, 17, now.Add(100*time.Millisecond), pns...)
	require.Equal(t, pns, tracker.Acked, "every frame is still delivered once")
	require.Zero(t, ctx.bytesInFlight)
	require.Equal(t, uint64(10*1200), lastFeedback(t, path).Delivery.Delivered, "only recorded evidence enters the sampler")

	// Feedback scratch is bounded by the same aggregate: two contexts that
	// saturate in turn cannot each keep a full-ceiling buffer.
	pathAck(t, h, 0, 0, now.Add(200*time.Millisecond), zero...)
	pns = pns[:0]
	for range maxDeliveryLive - 10 {
		pns = append(pns, sendRichPathPacket(h, 17, now.Add(300*time.Millisecond)))
	}
	pathAck(t, h, 17, 17, now.Add(400*time.Millisecond), pns...)
	require.LessOrEqual(t, cap(h.congestionEvents.scratch)+cap(ctx.congestionEvents.scratch), maxDeliveryLive+maxDeliveryRetained)

	// Retained loss evidence shares one ceiling too. A context never evicts
	// another's records; with none of its own, it forgoes retention.
	h2, _, path2 := newRichPathFixture(t)
	ctx2 := h2.RecoveryOnPath(17).(*sentPacketHandler)
	var last protocol.PacketNumber
	for range maxDeliveryRetained + 4 {
		last = sendRichPathPacket(h2, 0, now)
	}
	pathAck(t, h2, 0, 0, now.Add(100*time.Millisecond), last)
	require.Equal(t, maxDeliveryRetained, h2.DeliveryStats().Retained)
	for range 5 {
		last = sendRichPathPacket(h2, 17, now.Add(200*time.Millisecond))
	}
	pathAck(t, h2, 17, 17, now.Add(300*time.Millisecond), last)
	lost := len(lastFeedback(t, path2).Lost)
	require.NotZero(t, lost)
	require.Zero(t, ctx2.DeliveryStats().Retained)
	require.Equal(t, uint64(lost), ctx2.DeliveryStats().Evicted)
	require.Equal(t, maxDeliveryRetained, h2.DeliveryStats().Retained, "the root keeps its own evidence")
	require.Equal(t, protocol.ByteCount(4-lost)*1200, ctx2.bytesInFlight, "forgone evidence still settles each loss")
}

func TestBBRPathPendingLocalBelowAckWatermark(t *testing.T) {
	h, path := newLocalRichPathFixture(t)
	now := monotime.Time(2 * time.Second)
	ctx := h.RecoveryOnPath(17).(*sentPacketHandler)
	var tracker packetTracker
	register := func(at monotime.Time) (protocol.PacketNumber, LocalCompletionToken) {
		pn := ctx.PopPacketNumber(protocol.Encryption1RTT)
		token, err := h.RegisterLocalPacket(PacketContext{PathID: 17, TupleGeneration: 2}, at, pn, protocol.InvalidPacketNumber, nil, []Frame{tracker.NewPingFrame(pn)}, protocol.Encryption1RTT, protocol.ECNNon, 1200)
		require.NoError(t, err)
		return pn, token
	}
	a, tokenA := register(now)
	var d protocol.PacketNumber
	for i := range 4 {
		pn, token := register(now.Add(time.Duration(i+1) * time.Millisecond))
		require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now.Add(time.Duration(i+1)*time.Millisecond)))
		d = pn
	}
	pathAck(t, h, 17, 17, now.Add(time.Second), d)
	require.NotContains(t, tracker.Lost, a, "a pending-local packet is not network-lost below the ACK watermark")
	for _, p := range lastFeedback(t, path).Lost {
		require.NotEqual(t, a, p.PacketNumber)
	}
	require.NoError(t, h.CompleteLocalPacket(tokenA, LocalPacketUnsubmitted, now.Add(1100*time.Millisecond)))
	// Proven-unsubmitted returns the frame for retransmission, never as network loss.
	for _, e := range path.feedback {
		for _, p := range e.Lost {
			require.NotEqual(t, a, p.PacketNumber)
		}
	}
}

func TestBBRPathEarlyAckExcludedFromTiming(t *testing.T) {
	h, path := newLocalRichPathFixture(t)
	now := monotime.Time(2 * time.Second)
	ctx := h.RecoveryOnPath(17).(*sentPacketHandler)
	var tracker packetTracker
	pn := ctx.PopPacketNumber(protocol.Encryption1RTT)
	token, err := h.RegisterLocalPacket(PacketContext{PathID: 17, TupleGeneration: 2}, now, pn, protocol.InvalidPacketNumber, nil, []Frame{tracker.NewPingFrame(pn)}, protocol.Encryption1RTT, protocol.ECNNon, 1200)
	require.NoError(t, err)
	pathAck(t, h, 17, 17, now.Add(50*time.Millisecond), pn)
	e := lastFeedback(t, path)
	require.Equal(t, uint64(1200), e.Delivery.Delivered, "early delivery counts once")
	require.False(t, e.Delivery.Valid)
	require.Zero(t, e.Delivery.RawRTT)
	require.False(t, ctx.rttStats.HasMeasurement())
	require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now.Add(60*time.Millisecond)))
	require.Equal(t, []protocol.PacketNumber{pn}, tracker.Acked)
	require.Equal(t, uint64(1200), lastFeedback(t, path).Delivery.Delivered)
}

func TestBBRPathAdmission(t *testing.T) {
	now := monotime.Time(time.Second)
	reno := newSentPathFixture(t)
	_, err := reno.AddBBRPath(18, 1200, protocol.MaxPacketBufferSize, utils.NewRTTStats(), func() protocol.ByteCount { return 0 })
	require.Error(t, err, "a Reno root cannot gain a BBR path")

	h := NewSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, false, nil, protocol.PerspectiveClient, nil, utils.DefaultLogger).(*sentPacketHandler)
	rootBBR := EnableBBR(h, 1200, func() protocol.ByteCount { return 0 })
	h.DropPackets(protocol.EncryptionInitial, now)
	h.DropPackets(protocol.EncryptionHandshake, now)
	require.Error(t, h.AddPath(17, 1200), "no silent Reno substitution beside BBR")
	_, err = h.AddBBRPath(17, 1200, protocol.MaxPacketBufferSize, utils.NewRTTStats(), nil)
	require.Error(t, err, "a path needs its own pending-local authority")
	_, err = h.AddBBRPath(17, 1500, 1400, utils.NewRTTStats(), func() protocol.ByteCount { return 0 })
	require.Error(t, err)
	b, err := h.AddBBRPath(17, 1200, protocol.MaxPacketBufferSize, utils.NewRTTStats(), func() protocol.ByteCount { return 0 })
	require.NoError(t, err)
	require.NotSame(t, rootBBR, b)
	ctx := h.RecoveryOnPath(17).(*sentPacketHandler)
	require.Same(t, b, ctx.congestion)
	require.Equal(t, protocol.ECNUnsupported, ctx.ECNMode(true), "no marking before the path validates ECN")
	_, err = h.AddBBRPath(17, 1200, protocol.MaxPacketBufferSize, utils.NewRTTStats(), func() protocol.ByteCount { return 0 })
	require.Error(t, err)

	// Delivery expiry, close and reclamation reach every context.
	var last protocol.PacketNumber
	for range 5 {
		last = sendRichPathPacket(h, 17, now.Add(time.Second))
	}
	pathAck(t, h, 17, 17, now.Add(1100*time.Millisecond), last)
	require.NotZero(t, ctx.DeliveryStats().Retained)
	expiry := ctx.DeliveryExpiry()
	require.False(t, expiry.IsZero())
	require.Equal(t, expiry, h.DeliveryExpiry(), "the root reports the earliest path deadline")
	h.ExpireDelivery(expiry)
	require.Zero(t, ctx.DeliveryStats().Retained)
	require.Zero(t, h.DeliveryExpiry())

	h.RetirePath(17)
	h.RemovePath(17)
	require.Nil(t, ctx.congestionEvents, "reclamation closes the path's delivery state")
	_, err = h.AddBBRPath(18, 1200, protocol.MaxPacketBufferSize, utils.NewRTTStats(), func() protocol.ByteCount { return 0 })
	require.NoError(t, err)
	next := h.RecoveryOnPath(18).(*sentPacketHandler)
	h.CloseDelivery()
	require.Nil(t, h.congestionEvents)
	require.Nil(t, next.congestionEvents, "close reaches every path")
	_, err = h.AddBBRPath(19, 1200, protocol.MaxPacketBufferSize, utils.NewRTTStats(), func() protocol.ByteCount { return 0 })
	require.Error(t, err)
}
