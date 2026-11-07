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

func TestPreparedPacketFactsGrowthAbandonmentKeepsOriginalOwner(t *testing.T) {
	for _, reason := range []string{"before registration", "length", "time", "packet number", "path", "ECN"} {
		t.Run(reason, func(t *testing.T) {
			h := newLocalTestHandler(t).(*sentPacketHandler)
			prepared := testPacketPreparationWithStorage()
			defer prepared.Discard()
			p := prepared.p
			original := prepared.StreamStorage()[:1]
			callbacks := 0
			handler := &customFrameHandler{onAcked: func(wire.Frame) { callbacks++ }, onLost: func(wire.Frame) { callbacks++ }}
			first := &wire.StreamFrame{StreamID: 4, Data: []byte("original")}
			second := &wire.StreamFrame{StreamID: 8, Data: []byte("grown")}
			streams := append(prepared.StreamStorage(), StreamFrame{Frame: first, Handler: handler})
			streams = append(streams, StreamFrame{Frame: second, Handler: handler})
			frames := []Frame{{Frame: &wire.PingFrame{}, Handler: handler}}
			prepared.SetContext(PacketContext{TupleGeneration: 7})
			prepared.SetFrames(frames, streams)
			prepared.SetLength(1200)
			prepared.SetLargestAcked(protocol.InvalidPacketNumber)
			now, pn, ecn := monotime.Now(), h.PopPacketNumber(protocol.Encryption1RTT), protocol.ECNNon
			switch reason {
			case "length":
				prepared.SetLength(0)
			case "time":
				now = 0
			case "packet number":
				pn = protocol.InvalidPacketNumber
			case "path":
				prepared.SetContext(PacketContext{PathID: 7})
			case "ECN":
				ecn = protocol.ECT0
			}
			require.Empty(t, p.StreamFrames, "staging must not replace the original borrowed header")
			require.Same(t, &original[0], &p.StreamFrames[:1][0])
			require.Same(t, &streams[0], &prepared.View().StreamFrames()[0])
			if reason != "before registration" {
				_, err := RegisterPreparedPacket(h, &prepared, now, pn, ecn)
				require.Error(t, err)
			}
			require.Same(t, p, prepared.p)
			require.Zero(t, h.local.owner.pending)
			require.Zero(t, h.local.owner.live)
			require.Zero(t, h.bytesInFlight)
			require.Equal(t, protocol.InvalidPacketNumber, h.appDataPackets.largestSent)
			require.Same(t, first, original[0].Frame)
			prepared.Discard()
			require.Equal(t, PacketPreparation{}, prepared, "discard consumes both packet and staged header")
			require.Equal(t, StreamFrame{}, original[0])
			require.Same(t, first, streams[0].Frame)
			require.Same(t, second, streams[1].Frame)
			require.Zero(t, callbacks)
		})
	}
}

type preparedFactsPreludeLogger struct {
	utils.Logger
	onDebug func()
}

func (l *preparedFactsPreludeLogger) Debug() bool {
	if l.onDebug != nil {
		callback := l.onDebug
		l.onDebug = nil
		callback()
	}
	return false
}

func TestPreparedPacketFactsAdoptionPreservesCallbackBoundary(t *testing.T) {
	for _, count := range []int{1, 2} {
		t.Run(map[int]string{1: "borrowed slot", 2: "grown backing"}[count], func(t *testing.T) {
			exercisePreparedFactsAdoption(t, count)
		})
	}
}

func exercisePreparedFactsAdoption(t *testing.T, count int) {
	t.Helper()
	h := newLocalTestHandler(t).(*sentPacketHandler)
	prepared := testPacketPreparationWithStorage()
	defer prepared.Discard()
	p := prepared.p
	original := prepared.StreamStorage()[:1]
	streams := append(prepared.StreamStorage(), StreamFrame{Frame: &wire.StreamFrame{StreamID: 4}})
	if count == 2 {
		streams = append(streams, StreamFrame{Frame: &wire.StreamFrame{StreamID: 8}})
	}
	frames := []Frame{{Frame: &wire.PingFrame{}}}
	context := PacketContext{TupleGeneration: 7}
	prepared.SetContext(context)
	prepared.SetKeyGeneration(9)
	context.KeyGeneration = 9
	prepared.SetFrames(frames, streams)
	prepared.SetLength(1200)
	prepared.SetLargestAcked(21)
	require.Equal(t, context, prepared.View().Context())
	require.EqualValues(t, 1200, prepared.View().Length())
	require.Same(t, &frames[0], &prepared.View().Frames()[0])
	now, pn := monotime.Now(), h.PopPacketNumber(protocol.Encryption1RTT)
	preludeCalled, publicationCalled := false, false
	h.logger = &preparedFactsPreludeLogger{Logger: h.logger, onDebug: func() {
		preludeCalled = true
		require.Equal(t, PacketPreparation{}, prepared, "the handle is consumed before the first recovery callback")
		require.Nil(t, p.local, "canonical publication must still follow the accounting/logger prelude")
		require.Zero(t, h.local.owner.live)
		require.Empty(t, p.StreamFrames)
		require.Same(t, &original[0], &p.StreamFrames[:1][0], "adoption belongs at the existing recovery initialization point")
		prepared.Discard()
	}}
	h.congestion = &preparationSendCallback{SendAlgorithmWithDebugInfos: h.congestion, onSent: func() {
		publicationCalled = true
		require.True(t, preludeCalled)
		require.Equal(t, PacketPreparation{}, prepared)
		require.NotNil(t, p.local)
		require.Equal(t, 1, h.local.owner.live)
		require.Same(t, &streams[0], &p.StreamFrames[0])
		require.Equal(t, context, p.Context)
		require.Equal(t, protocol.PacketNumber(21), p.LargestAcked)
		require.Equal(t, now, p.SendTime)
		prepared.Discard()
	}}
	token, err := RegisterPreparedPacket(h, &prepared, now, pn, protocol.ECNNon)
	require.NoError(t, err)
	require.True(t, preludeCalled)
	require.True(t, publicationCalled)
	require.Same(t, p, token.registration.packet)
	require.Same(t, &streams[0], &p.StreamFrames[0])
	require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now.Add(time.Millisecond)))
	_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pn)}, protocol.Encryption1RTT, now.Add(2*time.Millisecond))
	require.NoError(t, err)
	if count == 1 {
		require.Empty(t, p.StreamFrames)
		require.Equal(t, 1, cap(p.StreamFrames))
		require.Equal(t, StreamFrame{}, original[0], "the retained single slot is cleared after ACK callbacks")
	} else {
		require.Nil(t, p.StreamFrames, "grown backing is not retained by the packet pool")
	}
}

func TestPreparedPacketInvalidHandleRetainsCallerOwnership(t *testing.T) {
	h := newLocalTestHandler(t)
	prepared := testPacketPreparationWithStorage()
	defer prepared.Discard()
	p := prepared.p
	_, err := RegisterPreparedPacket(nil, &prepared, monotime.Now(), 0, protocol.ECNNon)
	require.Error(t, err)
	require.Same(t, p, prepared.p)
	_, err = RegisterPreparedPacket(h, nil, monotime.Now(), 0, protocol.ECNNon)
	require.Error(t, err)
	var empty PacketPreparation
	_, err = RegisterPreparedPacket(h, &empty, monotime.Now(), 0, protocol.ECNNon)
	require.Error(t, err)
}
