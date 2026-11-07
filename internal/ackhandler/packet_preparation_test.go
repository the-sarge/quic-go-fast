package ackhandler

import (
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

// These tests supply the already-owned preparation packet directly. They do not
// depend on sync.Pool returning a particular packet, or on a cache surviving GC.
func testPacketPreparationWithStorage() PacketPreparation {
	return PacketPreparation{p: &packet{StreamFrames: make([]StreamFrame, 0, 1), reusableStreamFrames: true}}
}

func TestPacketPreparationDiscardClearsBorrowedCapacity(t *testing.T) {
	prepared := testPacketPreparationWithStorage()
	p := prepared.p
	handler := &customFrameHandler{}
	frame := &wire.StreamFrame{StreamID: 4, Data: []byte("borrowed")}
	borrowed := append(prepared.StreamStorage(), StreamFrame{Frame: frame, Handler: handler})
	require.Empty(t, p.StreamFrames, "the preparation header need not see the packer's append")
	prepared.Discard()
	require.Nil(t, prepared.p)
	require.Equal(t, StreamFrame{}, borrowed[0], "both frame and handler roots must be cleared through the full retained capacity")
	require.Empty(t, p.StreamFrames)
	require.Equal(t, 1, cap(p.StreamFrames))
	prepared.Discard()
	var empty PacketPreparation
	empty.Discard()
	require.Nil(t, empty.StreamStorage())
	var absent *PacketPreparation
	absent.Discard()
	require.Nil(t, absent.StreamStorage())
}

func TestPacketPreparationRefusalRetainsAndSuccessAdopts(t *testing.T) {
	h := newLocalTestHandler(t).(*sentPacketHandler)
	now := monotime.Now()
	prepared := testPacketPreparationWithStorage()
	defer prepared.Discard()
	p := prepared.p
	acked := 0
	streams := append(prepared.StreamStorage(), StreamFrame{Frame: &wire.StreamFrame{StreamID: 4}, Handler: &customFrameHandler{onAcked: func(wire.Frame) { acked++ }}})
	pn := h.PopPacketNumber(protocol.Encryption1RTT)
	_, err := registerPreparedTestPacket(h, &prepared, now, pn, streams, 0)
	require.Error(t, err)
	require.Same(t, p, prepared.p, "validation refusal leaves the sole preparation owner intact")
	require.Zero(t, h.local.owner.pending)
	require.Zero(t, h.bytesInFlight)
	require.Same(t, streams[0].Frame, prepared.StreamStorage()[:1][0].Frame)
	token, err := registerPreparedTestPacket(h, &prepared, now, pn, streams, 1200)
	require.NoError(t, err)
	require.Nil(t, prepared.p)
	require.Same(t, p, token.registration.packet)
	require.Same(t, &streams[0], &p.StreamFrames[0])
	prepared.Discard()
	require.Same(t, p, token.registration.packet, "late discard does not return the adopted recovery packet")
	require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now.Add(time.Millisecond)))
	_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pn)}, protocol.Encryption1RTT, now.Add(2*time.Millisecond))
	require.NoError(t, err)
	require.Equal(t, 1, acked)
	require.Equal(t, StreamFrame{}, streams[0])
}

func TestPacketPreparationAdoptsGrowthWithoutRetainingLargeBacking(t *testing.T) {
	h := newLocalTestHandler(t).(*sentPacketHandler)
	now := monotime.Now()
	prepared := testPacketPreparationWithStorage()
	defer prepared.Discard()
	p := prepared.p
	oldStorage := prepared.StreamStorage()[:1]
	var acked []protocol.StreamID
	streams := prepared.StreamStorage()
	for _, id := range []protocol.StreamID{4, 8} {
		streams = append(streams, StreamFrame{Frame: &wire.StreamFrame{StreamID: id}, Handler: &customFrameHandler{onAcked: func(f wire.Frame) {
			acked = append(acked, f.(*wire.StreamFrame).StreamID)
		}}})
	}
	require.NotSame(t, &oldStorage[0], &streams[0])
	pn := h.PopPacketNumber(protocol.Encryption1RTT)
	token, err := registerPreparedTestPacket(h, &prepared, now, pn, streams, 1200)
	require.NoError(t, err)
	require.Nil(t, prepared.p)
	require.Same(t, p, token.registration.packet)
	require.Same(t, &streams[0], &p.StreamFrames[0], "registration adopts the final grown slice")
	require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now.Add(time.Millisecond)))
	_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pn)}, protocol.Encryption1RTT, now.Add(2*time.Millisecond))
	require.NoError(t, err)
	require.Equal(t, []protocol.StreamID{4, 8}, acked)
	require.Nil(t, p.StreamFrames, "multi-frame backing is not retained by the packet pool")
}

func TestPacketPreparationEarlyACKThenLateCompletionLeavesReusedSlot(t *testing.T) {
	for _, outcome := range []LocalPacketOutcome{LocalPacketAccepted, LocalPacketUncertain, LocalPacketUnsubmitted} {
		t.Run(map[LocalPacketOutcome]string{LocalPacketAccepted: "accepted", LocalPacketUncertain: "uncertain", LocalPacketUnsubmitted: "unsubmitted"}[outcome], func(t *testing.T) {
			h := newLocalTestHandler(t).(*sentPacketHandler)
			now := monotime.Now()
			prepared := testPacketPreparationWithStorage()
			defer prepared.Discard()
			oldPacket := prepared.p
			oldFrame := &wire.StreamFrame{StreamID: 4}
			oldAcked := 0
			oldLost := 0
			oldHandler := &customFrameHandler{onAcked: func(f wire.Frame) {
				oldAcked++
				require.Same(t, oldFrame, f)
				require.Same(t, oldFrame, oldPacket.StreamFrames[0].Frame, "cache clearing follows frame callbacks")
			}, onLost: func(wire.Frame) { oldLost++ }}
			streams := append(prepared.StreamStorage(), StreamFrame{Frame: oldFrame, Handler: oldHandler})
			pn := h.PopPacketNumber(protocol.Encryption1RTT)
			token, err := registerPreparedTestPacket(h, &prepared, now, pn, streams, 1200)
			require.NoError(t, err)
			_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pn)}, protocol.Encryption1RTT, now.Add(time.Millisecond))
			require.NoError(t, err)
			require.Nil(t, token.registration.packet)
			require.Equal(t, 1, oldAcked)
			require.Equal(t, StreamFrame{}, streams[0])
			// Model a subsequent owner's write directly, without asking sync.Pool
			// to return a particular object. No pool acquisition occurs between
			// this write and completion of the retired token below.
			replacementFrame := &wire.StreamFrame{StreamID: 8}
			replacementHandler := &customFrameHandler{}
			streams[0] = StreamFrame{Frame: replacementFrame, Handler: replacementHandler}
			err = h.CompleteLocalPacket(token, outcome, now.Add(2*time.Millisecond))
			if outcome == LocalPacketUnsubmitted {
				require.ErrorIs(t, err, ErrLocalCompletionContradiction)
			} else {
				require.NoError(t, err)
			}
			require.Same(t, replacementFrame, streams[0].Frame)
			require.Same(t, replacementHandler, streams[0].Handler)
			require.Equal(t, 1, oldAcked)
			require.Zero(t, oldLost)
			clear(streams)
		})
	}
}

type preparationSendCallback struct {
	congestion.SendAlgorithmWithDebugInfos
	onSent func()
}

func (c *preparationSendCallback) OnPacketSent(at monotime.Time, inFlight protocol.ByteCount, key congestion.PacketKey, size protocol.ByteCount, ackEliciting, pathProbe bool) {
	c.onSent()
	c.SendAlgorithmWithDebugInfos.OnPacketSent(at, inFlight, key, size, ackEliciting, pathProbe)
}

func TestPacketPreparationConsumedBeforeRegistrationCallback(t *testing.T) {
	h := newLocalTestHandler(t).(*sentPacketHandler)
	now := monotime.Now()
	prepared := testPacketPreparationWithStorage()
	defer prepared.Discard()
	p := prepared.p
	streams := append(prepared.StreamStorage(), StreamFrame{Frame: &wire.StreamFrame{StreamID: 4}, Handler: &customFrameHandler{}})
	called := 0
	h.congestion = &preparationSendCallback{SendAlgorithmWithDebugInfos: h.congestion, onSent: func() {
		called++
		require.Nil(t, prepared.p, "ownership transfers before the first registration callback")
		require.NotNil(t, p.local)
		prepared.Discard()
		prepared.Discard()
		require.NotNil(t, p.local, "reentrant discard must not return the live packet to the pool")
		require.Same(t, streams[0].Frame, p.StreamFrames[0].Frame)
	}}
	pn := h.PopPacketNumber(protocol.Encryption1RTT)
	token, err := registerPreparedTestPacket(h, &prepared, now, pn, streams, 1200)
	require.NoError(t, err)
	require.Equal(t, 1, called)
	require.Same(t, p, token.registration.packet)
	require.NoError(t, h.CompleteLocalPacket(token, LocalPacketUnsubmitted, now.Add(time.Millisecond)))
}

func registerPreparedTestPacket(h LocalSentPacketHandler, prepared *PacketPreparation, now monotime.Time, pn protocol.PacketNumber, streams []StreamFrame, size protocol.ByteCount) (LocalCompletionToken, error) {
	prepared.SetContext(PacketContext{})
	prepared.SetFrames(nil, streams)
	prepared.SetLength(size)
	prepared.SetLargestAcked(protocol.InvalidPacketNumber)
	return RegisterPreparedPacket(h, prepared, now, pn, protocol.ECNNon)
}
