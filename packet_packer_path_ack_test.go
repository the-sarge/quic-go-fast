package quic

import (
	"testing"

	"github.com/quic-go/quic-go/internal/mocks"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestPackerBundlesPathAckWithOrdinaryAck(t *testing.T) {
	tc := newEmissionTestConnection(t, false)
	c := tc.conn
	now := monotime.Now()
	require.NoError(t, c.receivedPacketHandler.AddPath(17))
	for _, id := range []protocol.PathID{0, 17} {
		require.NoError(t, c.receivedPacketHandler.ReceivedPacketOnPath(id, 9, protocol.ECNNon, now, true))
	}
	buffer := getPacketBuffer()
	defer buffer.Release()
	packet, err := c.emission.packer.AppendPacket(buffer, 1200, now.Add(protocol.MaxAckDelay), protocol.Version1)
	require.NoError(t, err)
	require.NotNil(t, packet.Ack)
	require.False(t, packet.Ack.HasPathID)
	require.Len(t, packet.Frames, 1)
	path, ok := packet.Frames[0].Frame.(*wire.AckFrame)
	require.True(t, ok)
	require.Equal(t, protocol.PathID(17), path.PathID)
	require.True(t, path.HasPathID)
	require.False(t, packet.IsAckEliciting())
}

func TestCarrierReceiptThroughConnectionOnAnotherReceivePath(t *testing.T) {
	tc := newEmissionTestConnection(t, false)
	c := tc.conn
	now := monotime.Now()
	require.NoError(t, c.receivedPacketHandler.AddPath(17))
	require.NoError(t, c.receivedPacketHandler.ReceivedPacketOnPath(17, 9, protocol.ECNNon, now, true))
	require.NoError(t, c.receivedPacketHandler.ReceivedPacketOnPath(0, 12, protocol.ECNNon, now, true))
	require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("carrier")}))
	now = now.Add(protocol.MaxAckDelay)
	pn, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
	require.NoError(t, c.triggerSending(now).err)
	// R9 owns authenticated admission of this receive path. Exercise the admitted
	// receive view and the real connection ACK consumer without aliasing their PNs.
	require.NoError(t, c.receivedPacketHandler.ReceivedPacketOnPath(17, 20, protocol.ECNNon, now, false))
	ctrl := gomock.NewController(t)
	crypto := mocks.NewMockCryptoSetup(ctrl)
	crypto.EXPECT().Set1RTTAcked(gomock.Any(), now).Return(nil)
	c.cryptoStreamHandler = crypto
	c.lastPacketReceivedTime = now
	ack := &wire.AckFrame{HasPathID: true, PathID: 0, AckRanges: []wire.AckRange{{Smallest: pn, Largest: pn}}}
	require.NoError(t, c.handleAckFrame(ack, protocol.Encryption1RTT, now))
	require.True(t, c.receivedPacketHandler.IsPotentiallyDuplicateOnPath(0, 11))
	require.True(t, c.receivedPacketHandler.IsPotentiallyDuplicateOnPath(17, 8))
	require.False(t, c.receivedPacketHandler.IsPotentiallyDuplicateOnPath(17, 11))
	require.NoError(t, c.handleAckFrame(ack, protocol.Encryption1RTT, now))
	q := c.emission.queue.(*sendQueue)
	tc.sendConn.EXPECT().Write(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	close(q.closeCalled)
	require.NoError(t, q.Run())
}
