package ackhandler

import (
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

func TestLocalHistoryEagerPTO(t *testing.T) {
	h := newLocalTestHandler(t).(*sentPacketHandler)
	now := monotime.Time(time.Second)
	h.DropPackets(protocol.EncryptionHandshake, now)
	var tracker packetTracker
	pn, first := registerLocalTestPacket(t, h, now, protocol.Encryption1RTT, []Frame{tracker.NewPingFrame(0)})
	_, last := registerLocalTestPacket(t, h, now, protocol.Encryption1RTT, []Frame{tracker.NewPingFrame(1)})
	require.Zero(t, h.appDataPackets.history.latestLocalPTO())
	require.NoError(t, h.CompleteLocalPacket(last, LocalPacketAccepted, now.Add(time.Millisecond)))
	require.NoError(t, h.CompleteLocalPacket(first, LocalPacketAccepted, now.Add(2*time.Millisecond)))
	require.Equal(t, now.Add(2*time.Millisecond), h.appDataPackets.history.latestLocalPTO())
	_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pn)}, protocol.Encryption1RTT, now.Add(3*time.Millisecond))
	require.NoError(t, err)
	require.Equal(t, now.Add(time.Millisecond), h.appDataPackets.history.latestLocalPTO())
}

func TestLocalHistoryRankBoundaries(t *testing.T) {
	h := newSentPacketHistory(true)
	for pn := protocol.PacketNumber(0); pn <= 260; pn++ {
		h.SentPacket(pn, &packet{local: &localRegistration{}})
	}
	require.Zero(t, h.Difference(260, 0))
	for _, pn := range []protocol.PacketNumber{1, 63, 64, 127, 128, 255, 256, 260} {
		h.qualifyLocal(pn)
	}
	require.Equal(t, protocol.PacketNumber(8), h.Difference(260, 0))
	require.Equal(t, protocol.PacketNumber(3), h.Difference(128, 63))
	require.Equal(t, protocol.PacketNumber(3), h.Difference(260, 254))
	for pn := protocol.PacketNumber(0); pn <= 128; pn++ {
		require.NoError(t, h.Remove(pn))
	}
	require.Equal(t, protocol.PacketNumber(3), h.Difference(260, 128))
	h.releaseRetained()
	h.SentPacket(261, &packet{local: &localRegistration{}})
	require.Zero(t, h.Difference(261, 260))
	h.qualifyLocal(261)
	require.Equal(t, protocol.PacketNumber(1), h.Difference(261, 260))
}
