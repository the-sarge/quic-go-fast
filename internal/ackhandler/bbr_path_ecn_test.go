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

// testECNAuthority stands in for a path's connection-owned generation, local
// drain and endpoint capability.
type testECNAuthority struct {
	generation       uint64
	drained, capable bool
}

func (a *testECNAuthority) report(bool) (uint64, bool, bool) {
	return a.generation, a.drained, a.capable
}

// teeSink records a context's events and still drives its BBR controller.
type teeSink struct {
	inner congestionEventSink
	congestionRecorder
}

func (s *teeSink) Sent(e congestion.SendEvent) {
	s.congestionRecorder.Sent(e)
	s.inner.Sent(e)
}

func (s *teeSink) Feedback(e congestion.FeedbackEvent) {
	s.congestionRecorder.Feedback(e)
	s.inner.Feedback(e)
}

func addTestBBRPath(t *testing.T, h *sentPacketHandler, id protocol.PathID, auth *testECNAuthority) (*sentPacketHandler, *teeSink) {
	t.Helper()
	_, err := h.AddBBRPath(id, 1200, protocol.MaxPacketBufferSize, utils.NewRTTStats(), func() protocol.ByteCount { return 0 }, auth.report)
	require.NoError(t, err)
	ctx := h.RecoveryOnPath(id).(*sentPacketHandler)
	sink := &teeSink{inner: ctx.congestionEvents.sink}
	ctx.congestionEvents.sink = sink
	return ctx, sink
}

// newBBRECNPathFixture is a BBR root that validates ECN, with one BBR path whose
// ECN authority the test controls.
func newBBRECNPathFixture(t *testing.T) (h, ctx *sentPacketHandler, path *teeSink, auth *testECNAuthority) {
	t.Helper()
	now := monotime.Time(time.Second)
	h = NewSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, false, nil, protocol.PerspectiveClient, nil, utils.DefaultLogger).(*sentPacketHandler)
	EnableBBR(h, 1200, func() protocol.ByteCount { return 0 })
	EnableBBRECN(h, func(bool) (uint64, bool, bool) { return h.congestionEvents.pathGeneration, true, true })
	h.DropPackets(protocol.EncryptionInitial, now)
	h.DropPackets(protocol.EncryptionHandshake, now)
	auth = &testECNAuthority{drained: true, capable: true}
	ctx, path = addTestBBRPath(t, h, 17, auth)
	return h, ctx, path, auth
}

// sendMarkedPathPacket sends one 1-RTT packet on a path with the mark its own
// context selects.
func sendMarkedPathPacket(h *sentPacketHandler, id protocol.PathID, now monotime.Time, frame func(protocol.PacketNumber) Frame) protocol.PacketNumber {
	ctx := h.RecoveryOnPath(id)
	pn := ctx.PopPacketNumber(protocol.Encryption1RTT)
	h.SentPacketWithContext(PacketContext{PathID: id}, now, pn, protocol.InvalidPacketNumber, nil, []Frame{frame(pn)}, protocol.Encryption1RTT, ctx.ECNMode(true), 1200, false, false)
	return pn
}

func pingFrame(protocol.PacketNumber) Frame { return Frame{Frame: &wire.PingFrame{}} }

// ecnPathAck delivers an ACK for path id's space, carried on carrier, with the
// space's cumulative ECN counts.
func ecnPathAck(t *testing.T, h *sentPacketHandler, id, carrier protocol.PathID, now monotime.Time, ect0, ce uint64, pns ...protocol.PacketNumber) {
	t.Helper()
	ack := &wire.AckFrame{HasPathID: id != 0, PathID: id, AckRanges: ackRanges(slices.Clone(pns)...), ECT0: ect0, ECNCE: ce}
	_, err := h.ReceivedPathAck(ack, protocol.Encryption1RTT, now, carrier)
	require.NoError(t, err)
}

// Each BBR path validates the ECN counts of its own packet-number space,
// whichever path carried the ACK. An invalid report fails only that path.
func TestBBRPathECNCrossPathAckTarget(t *testing.T) {
	h, ctx, path, _ := newBBRECNPathFixture(t)
	now := monotime.Time(2 * time.Second)
	require.Equal(t, protocol.ECT0, ctx.ECNMode(true), "a BBR path validates its own ECN")
	require.Equal(t, protocol.ECNNon, ctx.ECNMode(false))

	p0 := sendMarkedPathPacket(h, 17, now, pingFrame)
	p1 := sendMarkedPathPacket(h, 17, now, pingFrame)
	z0 := sendMarkedPathPacket(h, 0, now, pingFrame)
	ecnPathAck(t, h, 17, 0, now.Add(50*time.Millisecond), 2, 0, p0, p1)
	e := lastFeedback(t, &path.congestionRecorder).ECN
	require.Equal(t, congestion.ECNCounts{ECT0: 2}, e.Accepted, "the target path's validator takes the counts")
	require.True(t, e.Eligible)
	require.Zero(t, h.bbrECN.accepted, "the carrier's validator sees none of them")
	require.Equal(t, protocol.InvalidPacketNumber, h.bbrECN.watermark)

	p2 := sendMarkedPathPacket(h, 17, now.Add(60*time.Millisecond), pingFrame)
	ecnPathAck(t, h, 17, 17, now.Add(110*time.Millisecond), 5, 0, p2)
	require.True(t, lastFeedback(t, &path.congestionRecorder).ECN.Failed, "more ECT0 than the path sent")
	require.Equal(t, protocol.ECNNon, ctx.ECNMode(true))

	ecnPathAck(t, h, 0, 17, now.Add(120*time.Millisecond), 1, 0, z0)
	require.Equal(t, congestion.ECNCounts{ECT0: 1}, h.bbrECN.accepted)
	require.Equal(t, protocol.ECT0, h.ECNMode(true), "the failure stays with the path that reported it")
}

// A same-ID migration keeps the path's PN space and cumulative counts. A late
// old-generation CE is accounted but not attributed to the new tuple, and the
// path revalidates only after its own authority drains the old generation.
func TestBBRPathECNOldGenerationCEAfterMigration(t *testing.T) {
	h, ctx, path, auth := newBBRECNPathFixture(t)
	now := monotime.Time(2 * time.Second)
	first := sendMarkedPathPacket(h, 17, now, pingFrame)
	ecnPathAck(t, h, 17, 17, now.Add(50*time.Millisecond), 1, 0, first)
	require.Equal(t, ecnStateCapable, ctx.bbrECN.state)
	old := sendMarkedPathPacket(h, 17, now.Add(60*time.Millisecond), pingFrame)

	ctx.MigratedPath(now.Add(70*time.Millisecond), 1200)
	require.Equal(t, protocol.ECNNon, ctx.ECNMode(true), "the authority has not reached the new generation")
	auth.generation, auth.drained = 1, false
	require.Equal(t, protocol.ECNNon, ctx.ECNMode(true), "old-generation local sends are still pending")

	ecnPathAck(t, h, 17, 0, now.Add(120*time.Millisecond), 1, 1, old)
	f := lastFeedback(t, &path.congestionRecorder)
	require.Equal(t, congestion.ECNCounts{ECT0: 1, CE: 1}, f.ECN.Accepted, "old-generation CE is still accounted")
	require.Equal(t, uint64(1), f.ECN.PathGeneration)
	require.False(t, f.ECN.Eligible)
	require.False(t, f.Congested, "old-generation CE is not the new tuple's congestion")
	require.Equal(t, protocol.ECNNon, ctx.ECNMode(true))

	auth.drained = true
	require.Equal(t, protocol.ECT0, ctx.ECNMode(true), "the fence is met")
	fresh := sendMarkedPathPacket(h, 17, now.Add(130*time.Millisecond), pingFrame)
	require.Greater(t, fresh, old, "the PN space persists")
	ecnPathAck(t, h, 17, 17, now.Add(180*time.Millisecond), 2, 1, fresh)
	f = lastFeedback(t, &path.congestionRecorder)
	require.True(t, f.ECN.Eligible)
	require.Equal(t, congestion.ECNCounts{ECT0: 1}, f.ECN.Delta)
	require.Equal(t, congestion.ECNCounts{ECT0: 2, CE: 1}, f.ECN.Accepted, "counts are cumulative across the migration")

	require.Zero(t, h.bbrECN.generation, "the root's validation is untouched")
	require.Equal(t, protocol.ECT0, h.ECNMode(true))
}

// A lost path packet settles its frames and flight once; a late receipt does
// not deliver its requeued frames again, and a duplicate changes nothing.
func TestBBRPathLossThenLateAck(t *testing.T) {
	h, ctx, path, _ := newBBRECNPathFixture(t)
	now := monotime.Time(2 * time.Second)
	var tracker packetTracker
	var pns []protocol.PacketNumber
	for i := range 5 {
		pns = append(pns, sendMarkedPathPacket(h, 17, now.Add(time.Duration(i)*time.Millisecond), tracker.NewPingFrame))
	}
	ecnPathAck(t, h, 17, 17, now.Add(50*time.Millisecond), 3, 0, pns[2:]...)
	require.Equal(t, pns[:2], tracker.Lost)
	require.Len(t, lastFeedback(t, &path.congestionRecorder).Lost, 2)
	require.Zero(t, ctx.bytesInFlight)

	ecnPathAck(t, h, 17, 0, now.Add(60*time.Millisecond), 5, 0, pns...)
	require.Equal(t, pns[:2], tracker.Lost, "each loss is declared once")
	require.Equal(t, pns[2:], tracker.Acked, "a late receipt does not deliver requeued frames again")
	require.Empty(t, lastFeedback(t, &path.congestionRecorder).Lost)
	require.Zero(t, ctx.bytesInFlight)
	delivered := ctx.congestionEvents.sampler.delivered
	require.LessOrEqual(t, delivered, uint64(5*1200))

	ecnPathAck(t, h, 17, 17, now.Add(70*time.Millisecond), 5, 0, pns...)
	require.Equal(t, delivered, ctx.congestionEvents.sampler.delivered, "a duplicate receipt counts nothing")
	require.Equal(t, pns[2:], tracker.Acked)
	require.False(t, lastFeedback(t, &path.congestionRecorder).ECN.Failed)
}

// An abandoned path keeps its retained evidence until reclamation, which then
// closes its ECN ledger and delivery state without touching the root.
func TestBBRPathAbandonmentWhileRetained(t *testing.T) {
	h, ctx, _, _ := newBBRECNPathFixture(t)
	now := monotime.Time(2 * time.Second)
	var tracker packetTracker
	var pns []protocol.PacketNumber
	for i := range 5 {
		pns = append(pns, sendMarkedPathPacket(h, 17, now.Add(time.Duration(i)*time.Millisecond), tracker.NewPingFrame))
	}
	ecnPathAck(t, h, 17, 17, now.Add(50*time.Millisecond), 1, 0, pns[4])
	require.NotZero(t, ctx.DeliveryStats().Retained)
	h.RetirePath(17)
	require.False(t, h.PathQuiescent(17))

	ecnPathAck(t, h, 17, 0, now.Add(60*time.Millisecond), 5, 0, pns...)
	require.Equal(t, pns[:2], tracker.Lost)
	require.ElementsMatch(t, pns[2:], tracker.Acked, "a retired path still settles its flight once")
	require.Zero(t, ctx.bytesInFlight)
	require.True(t, h.PathQuiescent(17))
	require.NotEmpty(t, ctx.bbrECN.ranges)

	h.RemovePath(17)
	require.Nil(t, ctx.congestionEvents)
	require.True(t, ctx.bbrECN.closed)
	require.Nil(t, ctx.bbrECN.ranges, "reclamation releases the path's ECN ledger")
	require.Equal(t, protocol.ECNNon, ctx.ECNMode(true))
	ecnPathAck(t, h, 17, 0, now.Add(70*time.Millisecond), 5, 0, pns...)
	require.Len(t, tracker.Acked, 3, "a reclaimed path's ACK is ignored")

	z := sendMarkedPathPacket(h, 0, now.Add(80*time.Millisecond), pingFrame)
	ecnPathAck(t, h, 0, 0, now.Add(130*time.Millisecond), 1, 0, z)
	require.Equal(t, congestion.ECNCounts{ECT0: 1}, h.bbrECN.accepted)
	require.Equal(t, protocol.ECT0, h.ECNMode(true))
}

// Nonzero contexts divide one further outcome-ring allowance beside the root's,
// including after a migration reset. Two paths saturating their rings evict only
// optional evidence: every frame and byte of flight still settles once.
func TestBBRPathRecoveryStoreBound(t *testing.T) {
	h, ctx, _, _ := newBBRECNPathFixture(t)
	now := monotime.Time(2 * time.Second)
	share := maxRecoveryOutcomes / maxRecoveryPaths
	saturate := func(id protocol.PathID, ctx *sentPacketHandler) {
		t.Helper()
		var tracker packetTracker
		var batch []protocol.PacketNumber
		var acked int
		for i := range share + 300 {
			at := now.Add(time.Duration(i) * time.Microsecond)
			pn := ctx.PopPacketNumber(protocol.Encryption1RTT)
			h.SentPacketWithContext(PacketContext{PathID: id}, at, pn, protocol.InvalidPacketNumber, nil, []Frame{tracker.NewPingFrame(pn)}, protocol.Encryption1RTT, protocol.ECNNon, 1200, false, false)
			batch = append(batch, pn)
			if len(batch) == 100 {
				ecnPathAck(t, h, id, 0, at.Add(time.Millisecond), 0, 0, batch...)
				acked += len(batch)
				batch = batch[:0]
			}
		}
		ecnPathAck(t, h, id, 0, now.Add(time.Second), 0, 0, batch...)
		acked += len(batch)
		require.Len(t, tracker.Acked, acked, "every frame is delivered once")
		require.Empty(t, tracker.Lost)
		require.Zero(t, ctx.bytesInFlight)
		r := &ctx.congestionEvents.recovery
		require.Len(t, r.index.outcomes, share, "a path ring holds its share")
		require.Equal(t, share, r.count)
		require.NotZero(t, r.evicted)
	}
	saturate(17, ctx)
	h.RetirePath(17)
	next, _ := addTestBBRPath(t, h, 18, &testECNAuthority{drained: true, capable: true})
	saturate(18, next)
	require.Len(t, ctx.congestionEvents.recovery.index.outcomes, share, "a retired path keeps only its share")

	sendMarkedPathPacket(h, 0, now, pingFrame)
	require.Len(t, h.congestionEvents.recovery.index.outcomes, maxRecoveryOutcomes, "the root keeps the single-path allowance")

	next.MigratedPath(now.Add(2*time.Second), 1200)
	sendMarkedPathPacket(h, 18, now.Add(2*time.Second), pingFrame)
	require.Len(t, next.congestionEvents.recovery.index.outcomes, share, "a migration reset keeps the share")
}

func TestBBRPathECNAdmission(t *testing.T) {
	now := monotime.Time(time.Second)
	h := NewSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, false, nil, protocol.PerspectiveClient, nil, utils.DefaultLogger).(*sentPacketHandler)
	EnableBBR(h, 1200, func() protocol.ByteCount { return 0 })
	EnableBBRECN(h, func(bool) (uint64, bool, bool) { return h.congestionEvents.pathGeneration, true, true })
	h.DropPackets(protocol.EncryptionInitial, now)
	h.DropPackets(protocol.EncryptionHandshake, now)
	_, err := h.AddBBRPath(17, 1200, protocol.MaxPacketBufferSize, utils.NewRTTStats(), func() protocol.ByteCount { return 0 }, nil)
	require.Error(t, err, "a path beside a validating root needs its own ECN authority")
	require.Nil(t, h.RecoveryOnPath(17))

	auth := &testECNAuthority{drained: true}
	ctx, _ := addTestBBRPath(t, h, 17, auth)
	require.Equal(t, protocol.ECNUnsupported, ctx.ECNMode(true), "an incapable endpoint omits ECN entirely")
	auth.capable = true
	require.Equal(t, protocol.ECT0, ctx.ECNMode(true))
}

// On the local route, a BBR path's validator reconciles its own local ECN
// evidence, never the root's.
func TestBBRPathECNLocalEvidence(t *testing.T) {
	now := monotime.Time(time.Second)
	handler, err := NewLocalSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, true, nil, protocol.PerspectiveClient, nil, utils.DefaultLogger)
	require.NoError(t, err)
	h := handler.(*sentPacketHandler)
	EnableBBR(h, 1200, func() protocol.ByteCount { return 0 })
	EnableBBRECN(h, func(bool) (uint64, bool, bool) { return h.congestionEvents.pathGeneration, true, true })
	h.DropPackets(protocol.EncryptionInitial, now)
	h.DropPackets(protocol.EncryptionHandshake, now)
	ctx, _ := addTestBBRPath(t, h, 17, &testECNAuthority{drained: true, capable: true})
	require.NotNil(t, ctx.local.ecn)
	require.Same(t, ctx.local.ecn, ctx.bbrECN.local)
	require.NotSame(t, h.local.ecn, ctx.local.ecn)
	require.Equal(t, protocol.ECT0, ctx.ECNMode(true))
}
