package quic

import (
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestCongestionControlV1QueuedMigrationAndECNFence(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		for _, accounted := range []bool{false, true} {
			name := map[bool]string{false: "rebind", true: "replace"}[replacement] + "/" + map[bool]string{false: "missing report", true: "counter fence"}[accounted]
			t.Run(name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					conf := &Config{InitialPacketSize: 1200, EnableDatagrams: true, DisablePathMTUDiscovery: true}
					require.NoError(t, conf.SetCongestionControlV1("bbrv3"))
					tc := newConfiguredEmissionTestConnection(t, false, conf)
					c := tc.conn
					c.conn = &ecnTestSendConn{sendConn: c.conn, capable: true}
					now := monotime.Now()
					pn, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
					require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("old marked traffic")}))
					require.True(t, c.triggerSending(now).progress)
					q := c.emission.queue.(*sendQueue)
					release := make(chan struct{})
					tc.sendConn.EXPECT().Write(gomock.Any(), gomock.Any(), protocol.ECT0).DoAndReturn(func([]byte, uint16, protocol.ECN) error { <-release; return nil })
					done := make(chan error, 1)
					go func() { done <- q.Run() }()
					synctest.Wait()
					c.sentPacketHandler.MigratedPath(now, 1200)
					c.pathGeneration++
					c.emission.resetLocalPath(c.pathGeneration, now)
					require.Equal(t, "bbrv3", c.CongestionControlV1())
					b := c.emission.bbr.controller
					require.True(t, b.InSlowStart())
					require.EqualValues(t, 12000, b.GetCongestionWindow())
					require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("new path waits")}))
					require.False(t, c.triggerSending(now.Add(time.Millisecond)).progress)
					require.NotNil(t, c.datagramQueue.Peek())
					require.Equal(t, protocol.ECNNon, c.sentPacketHandler.ECNMode(true))
					if accounted {
						_, err := c.sentPacketHandler.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: pn, Largest: pn}}, ECT0: 1}, protocol.Encryption1RTT, now.Add(time.Millisecond))
						require.NoError(t, err)
						require.EqualValues(t, 12000, b.GetCongestionWindow(), "old ACK cannot inflate fresh model")
						require.Equal(t, protocol.ECNNon, c.sentPacketHandler.ECNMode(true), "counter receipt alone cannot release worker fence")
					}
					if replacement {
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
						// Continue with a fresh worker for the post-fence admission assertion.
						c.emission.queue = newSendQueue(c.conn, &c.handshakeSendFeedback)
					}
					require.NoError(t, <-done)
					expected := protocol.ECNNon
					if accounted {
						expected = protocol.ECT0
					}
					require.Equal(t, expected, c.sentPacketHandler.ECNMode(true))
					require.True(t, c.triggerSending(now.Add(time.Second)).progress)
					entry := <-c.emission.queue.(*sendQueue).queue
					require.Equal(t, expected, entry.ecn)
					entry.release()
					require.Nil(t, c.datagramQueue.Peek())
					require.Equal(t, "bbrv3", c.CongestionControlV1())
				})
			})
		}
	}
}
