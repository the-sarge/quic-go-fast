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
	require.NoError(t, c.receivedPacketHandler.ReceivedPacketOnPath(0, 9, protocol.ECNNon, now, true))
	require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("carrier")}))
	now = now.Add(protocol.MaxAckDelay)
	pn, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
	require.NoError(t, c.triggerSending(now).err)
	require.NoError(t, c.receivedPacketHandler.ReceivedPacketOnPath(0, 8, protocol.ECNNon, now, true))
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
	require.True(t, c.receivedPacketHandler.IsPotentiallyDuplicateOnPath(0, 8))
	require.True(t, c.receivedPacketHandler.IsPotentiallyDuplicateOnPath(17, 8))
	require.False(t, c.receivedPacketHandler.IsPotentiallyDuplicateOnPath(17, 11))
	require.NoError(t, c.handleAckFrame(ack, protocol.Encryption1RTT, now))
	buffer := getPacketBuffer()
	defer buffer.Release()
	_, err := c.emission.packer.AppendPacket(buffer, 1200, now, protocol.Version1)
	require.ErrorIs(t, err, errNothingToPack)
	require.True(t, c.receivedPacketHandler.GetAlarmTimeout().IsZero())
	require.NoError(t, c.receivedPacketHandler.ReceivedPacketOnPath(0, 11, protocol.ECNNon, now, true))
	require.Equal(t, protocol.PacketNumber(11), c.receivedPacketHandler.GetAckFrame(protocol.Encryption1RTT, now, false).LargestAcked())
	q := c.emission.queue.(*sendQueue)
	tc.sendConn.EXPECT().Write(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	close(q.closeCalled)
	require.NoError(t, q.Run())
}

func TestPackerReservesPeriodicPingForFullAckBundle(t *testing.T) {
	for _, onlyAck := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "ack-only"}[onlyAck], func(t *testing.T) {
			tc := newEmissionTestConnection(t, false)
			c := tc.conn
			p := c.emission.packer
			p.getDestConnID = func() protocol.ConnectionID { return protocol.ParseConnectionID(make([]byte, 20)) }
			p.numNonAckElicitingAcks = protocol.MaxNonAckElicitingAcks
			require.NoError(t, c.receivedPacketHandler.AddPath(64))
			now := monotime.Now()
			for _, id := range []protocol.PathID{0, 64} {
				for i := protocol.PacketNumber(0); i < 64; i++ {
					require.NoError(t, c.receivedPacketHandler.ReceivedPacketOnPath(id, i*((1<<30)+2), protocol.ECT0, now, true))
				}
			}
			// 584-byte ordinary ACK + 586-byte PATH_ACK + 30-byte fixture
			// header/tag overhead exactly fills 1200 bytes before the required PING.
			var packet shortHeaderPacket
			var buffer *packetBuffer
			var err error
			if onlyAck {
				packet, buffer, err = p.PackAckOnlyPacket(1200, now.Add(protocol.MaxAckDelay), protocol.Version1)
			} else {
				buffer = getPacketBuffer()
				packet, err = p.AppendPacket(buffer, 1200, now.Add(protocol.MaxAckDelay), protocol.Version1)
			}
			if buffer != nil {
				defer buffer.Release()
			}
			require.NoError(t, err)
			require.LessOrEqual(t, packet.Length, protocol.ByteCount(1200))
			require.True(t, packet.IsAckEliciting())
			require.NotNil(t, packet.Ack)
			require.Len(t, packet.Frames, 2)
			var hasPathAck, hasPing bool
			for _, frame := range packet.Frames {
				switch frame.Frame.(type) {
				case *wire.AckFrame:
					hasPathAck = true
				case *wire.PingFrame:
					hasPing = true
				}
			}
			require.True(t, hasPathAck)
			require.True(t, hasPing)
		})
	}
}
