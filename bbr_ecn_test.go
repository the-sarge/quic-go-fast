package quic

import (
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type ecnTestSendConn struct {
	sendConn
	capable bool
}

func (c *ecnTestSendConn) capabilities() connCapabilities {
	caps := c.sendConn.capabilities()
	caps.ECN = c.capable
	return caps
}

func installEmissionBBRECN(c *Conn) *deliveryTestSink {
	c.conn = &ecnTestSendConn{sendConn: c.conn, capable: true}
	c.emission.bbr = newBBRSendPolicy(1_000_000, 1200, monotime.Now())
	sink := &deliveryTestSink{}
	ackhandler.EnableDeliverySampling(c.sentPacketHandler, sink, c.emission.deliveryPendingBytes)
	c.emission.enableBBRECN()
	return sink
}

func TestBBRECNCoalescedAndPathProbeMarking(t *testing.T) {
	t.Run("unavailable metadata uses basic writer", func(t *testing.T) {
		tc := newEmissionTestConnection(t, false)
		c := tc.conn
		installEmissionBBRECN(c)
		socket := NewMockPacketConn(gomock.NewController(t))
		addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4242}
		socket.EXPECT().LocalAddr().Return(addr).AnyTimes()
		raw := &basicConn{PacketConn: socket}
		c.conn = newSendConn(raw, addr, packetInfo{}, nil)
		c.emission.queue = newSendQueue(c.conn, &c.handshakeSendFeedback)
		socket.EXPECT().WriteTo(gomock.Any(), addr).DoAndReturn(func(b []byte, _ net.Addr) (int, error) { return len(b), nil }).Times(2)
		require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("fallback")}))
		require.True(t, c.triggerSending(monotime.Now()).progress)
		q := c.emission.queue.(*sendQueue)
		close(q.closeCalled)
		require.NoError(t, q.Run())
		_, err := raw.WritePacket([]byte("coalesced metadata"), addr, nil, 0, c.sentPacketHandler.ECNMode(false))
		require.NoError(t, err)
	})

	t.Run("coalesced short header is Not-ECT", func(t *testing.T) {
		tc := newHandshakeEmissionConnection(t, false)
		c := tc.conn
		sink := installEmissionBBRECN(c)
		ctrl := gomock.NewController(t)
		sealing := NewMockSealingManager(ctrl)
		sealing.EXPECT().GetInitialSealer().Return(nil, handshake.ErrKeysDropped).AnyTimes()
		sealing.EXPECT().GetHandshakeSealer().Return(newMockShortHeaderSealer(ctrl), nil).AnyTimes()
		sealing.EXPECT().Get1RTTSealer().Return(newMockShortHeaderSealer(ctrl), nil).AnyTimes()
		c.emission.packer = newPacketPacker(tc.srcConnID, c.connIDManager.Get, c.initialStream, c.handshakeStream, c.sentPacketHandler, c.retransmissionQueue, sealing, c.framer, &c.receivedPacketHandler, c.datagramQueue, c.perspective)
		c.handshakeStream.Write([]byte("handshake"))
		c.queueControlFrame(&wire.PingFrame{})
		result := c.emission.sendBounded(monotime.Now(), false)
		require.NoError(t, result.err)
		require.True(t, result.progress)
		entry := <-c.emission.queue.(*sendQueue).queue
		defer entry.release()
		require.Equal(t, protocol.ECNNon, entry.ecn)
		require.Len(t, sink.sends, 2)
		require.Equal(t, protocol.Encryption1RTT, sink.sends[1].Packet.EncryptionLevel)
		require.Equal(t, protocol.ECNNon, sink.sends[1].Packet.ECN)
		pn := sink.sends[1].Packet.PacketNumber
		_, err := c.sentPacketHandler.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: pn, Largest: pn}}}, protocol.Encryption1RTT, monotime.Now().Add(time.Millisecond))
		require.NoError(t, err)
		require.False(t, sink.events[len(sink.events)-1].ECN.Failed)
		require.Equal(t, protocol.ECT0, c.sentPacketHandler.ECNMode(true))
	})
	t.Run("ordinary and direct probe hole", func(t *testing.T) {
		tc := newEmissionTestConnection(t, false)
		c := tc.conn
		sink := installEmissionBBRECN(c)
		require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("marked")}))
		require.True(t, c.triggerSending(monotime.Now()).progress)
		entry := <-c.emission.queue.(*sendQueue).queue
		require.Equal(t, protocol.ECT0, entry.ecn)
		entry.release()
		addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4242}
		tc.sendConn.EXPECT().WriteTo(gomock.Any(), addr, packetInfo{}).Return(nil)
		require.NoError(t, c.emission.serverProbe(c.connIDManager.Get(), []ackhandler.Frame{{Frame: &wire.PathChallengeFrame{}}}, addr, packetInfo{}, 0, monotime.Now()))
		require.Len(t, sink.sends, 2)
		require.Equal(t, protocol.ECNNon, sink.sends[1].Packet.ECN)
		first, last := sink.sends[0].Packet.PacketNumber, sink.sends[1].Packet.PacketNumber
		_, err := c.sentPacketHandler.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: first, Largest: last}}, ECT0: 1}, protocol.Encryption1RTT, monotime.Now().Add(time.Millisecond))
		require.NoError(t, err)
		require.True(t, sink.events[len(sink.events)-1].ECN.Eligible)
	})
	for _, replace := range []bool{false, true} {
		t.Run(map[bool]string{false: "server rebind worker fence", true: "client replacement worker fence"}[replace], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				tc := newEmissionTestConnection(t, false)
				c := tc.conn
				sink := installEmissionBBRECN(c)
				require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("old marked worker")}))
				require.True(t, c.triggerSending(monotime.Now()).progress)
				q := c.emission.queue.(*sendQueue)
				release := make(chan struct{})
				tc.sendConn.EXPECT().Write(gomock.Any(), gomock.Any(), protocol.ECT0).DoAndReturn(func([]byte, uint16, protocol.ECN) error { <-release; return nil })
				done := make(chan error, 1)
				go func() { done <- q.Run() }()
				synctest.Wait()
				c.pathGeneration++
				c.emission.resetLocalPath(c.pathGeneration, monotime.Now())
				c.sentPacketHandler.MigratedPath(monotime.Now(), 1200)
				pn := sink.sends[0].Packet.PacketNumber
				_, err := c.sentPacketHandler.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: pn, Largest: pn}}, ECT0: 1}, protocol.Encryption1RTT, monotime.Now().Add(time.Millisecond))
				require.NoError(t, err)
				require.Equal(t, protocol.ECNNon, c.sentPacketHandler.ECNMode(true))
				require.False(t, sink.events[len(sink.events)-1].ECN.Eligible)
				if replace {
					replaced := make(chan struct{})
					go func() { c.emission.replacePath(c.conn, &c.handshakeSendFeedback); close(replaced) }()
					synctest.Wait()
					select {
					case <-replaced:
						t.Fatal("replacement did not join old worker")
					default:
					}
					close(release)
					synctest.Wait()
					<-replaced
				} else {
					addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4343}
					tc.sendConn.EXPECT().ChangeRemoteAddr(addr, packetInfo{})
					c.emission.rebindPath(addr, packetInfo{})
					close(release)
					synctest.Wait()
					q.Close()
				}
				require.NoError(t, <-done)
				require.Equal(t, protocol.ECT0, c.sentPacketHandler.ECNMode(true))
				c.conn.(*ecnTestSendConn).capable = false
				require.Equal(t, protocol.ECNUnsupported, c.sentPacketHandler.ECNMode(true))
			})
		})
	}
}
