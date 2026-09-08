package quic

import (
	"errors"
	"net"
	"net/netip"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/testutils/events"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestEmissionDirectProbe(t *testing.T) {
	for _, client := range []bool{false, true} {
		for _, writeError := range []bool{false, true} {
			name := "server"
			if client {
				name = "client"
			}
			if writeError {
				name += "/write-error"
			} else {
				name += "/success"
			}
			t.Run(name, func(t *testing.T) {
				tc := newEmissionTestConnection(t, false)
				c := tc.conn
				recorder := &events.Recorder{}
				c.qlogger = recorder
				addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4242}
				info := packetInfo{addr: netip.MustParseAddr("127.0.0.2")}
				connID := protocol.ParseConnectionID([]byte{8, 7, 6, 5})
				challenge := &wire.PathChallengeFrame{Data: [8]byte{1, 2, 3, 4, 5, 6, 7, 8}}
				frames := []ackhandler.Frame{{Frame: challenge}}
				observed := observeConstructionBuffer(t)
				writes := 0
				now := monotime.Now()
				check := func(b []byte) error {
					writes++
					require.EqualValues(t, 1, (*observed).refCount, "storage stays live during syscall")
					require.Len(t, b, 1200)
					hdrLen, pn, _, _, err := wire.ParseShortHeader(b, connID.Len())
					require.NoError(t, err)
					require.EqualValues(t, 0, pn)
					require.Equal(t, connID.Bytes(), b[1:1+connID.Len()])
					parser := wire.NewFrameParser(true, false, false)
					payload := b[hdrLen : len(b)-7]
					typ, n, err := parser.ParseType(payload, protocol.Encryption1RTT)
					require.NoError(t, err)
					f, _, err := parser.ParseLessCommonFrame(typ, payload[n:], c.version)
					require.NoError(t, err)
					require.Equal(t, challenge, f)
					// Path probes are registered before I/O but don't start the idle clock.
					_, err = c.sentPacketHandler.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: pn, Largest: pn}}}, protocol.Encryption1RTT, now.Add(time.Millisecond))
					require.NoError(t, err)
					require.Zero(t, c.firstAckElicitingPacketAfterIdleSentTime)
					if writeError {
						return errors.New("direct socket failure")
					}
					return nil
				}
				if client {
					raw := NewMockRawConn(gomock.NewController(t))
					tr := &Transport{conn: raw}
					tr.initOnce.Do(func() {}) // Transport initialization is outside the write seam.
					raw.EXPECT().WritePacket(gomock.Any(), addr, []byte(nil), uint16(0), protocol.ECNUnsupported).DoAndReturn(func(b []byte, _ net.Addr, _ []byte, _ uint16, _ protocol.ECN) (int, error) { return len(b), check(b) })
					require.NoError(t, c.emission.clientProbe(connID, frames[0], tr, addr, now))
				} else {
					tc.sendConn.EXPECT().WriteTo(gomock.Any(), addr, info).DoAndReturn(func(b []byte, _ net.Addr, _ packetInfo) error { return check(b) })
					require.NoError(t, c.emission.serverProbe(connID, frames, addr, info, 42, now))
				}
				require.Equal(t, 1, writes)
				require.Zero(t, (*observed).refCount)
				require.Empty(t, c.sendQueue.(*sendQueue).queue, "direct probes never enter the ordinary queue")
				sent := recorder.Events(qlog.PacketSent{})
				require.Len(t, sent, 1)
				if !client {
					require.Equal(t, qlog.DatagramPayloadChecksum(42), sent[0].(qlog.PacketSent).DatagramPayloadChecksum)
				}
			})
		}
	}
}

func TestEmissionMTUProbe(t *testing.T) {
	for _, writeError := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "write-error"}[writeError], func(t *testing.T) {
			tc := newEmissionTestConnection(t, false)
			c := tc.conn
			now := monotime.Now()
			c.mtuDiscoverer = newMTUDiscoverer(c.rttStats, 1200, 1400, nil)
			c.mtuDiscoverer.Start(now.Add(-time.Hour))
			observed := observeConstructionBuffer(t)
			result := c.emitPackets(now)
			require.NoError(t, result.err)
			require.True(t, result.progress)
			require.EqualValues(t, 1, (*observed).refCount)
			q := c.sendQueue.(*sendQueue)
			require.Len(t, q.queue, 1)
			cause := errors.New("MTU write failure")
			tc.sendConn.EXPECT().Write(gomock.Any(), uint16(0), protocol.ECNUnsupported).DoAndReturn(func(b []byte, _ uint16, _ protocol.ECN) error {
				require.Len(t, b, 1300)
				_, pn, _, _, err := wire.ParseShortHeader(b, c.connIDManager.Get().Len())
				require.NoError(t, err)
				if writeError {
					return cause
				}
				_, err = c.sentPacketHandler.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: pn, Largest: pn}}}, protocol.Encryption1RTT, now.Add(time.Millisecond))
				require.NoError(t, err)
				require.EqualValues(t, 1300, c.mtuDiscoverer.CurrentSize())
				return nil
			})
			close(q.closeCalled)
			err := q.Run()
			if writeError {
				require.ErrorIs(t, err, cause)
			} else {
				require.NoError(t, err)
			}
			require.Zero(t, (*observed).refCount)
		})
	}
}

func TestEmissionMTUProbeFullQueue(t *testing.T) {
	tc := newEmissionTestConnection(t, false)
	c := tc.conn
	now := monotime.Now()
	c.mtuDiscoverer = newMTUDiscoverer(c.rttStats, 1200, 1400, nil)
	c.mtuDiscoverer.Start(now.Add(-time.Hour))
	q := c.sendQueue.(*sendQueue)
	for range sendQueueCapacity {
		q.Send(getPacketBuffer(), 0, protocol.ECNUnsupported, sendMetadata{})
	}
	result := c.emitPackets(now)
	require.Equal(t, emissionQueueFull, result.stop)
	require.True(t, c.mtuDiscoverer.ShouldSendProbe(now), "full queue must not consume probe intent")
	for len(q.queue) > 0 {
		(<-q.queue).buf.Release()
	}
}

func TestEmissionPathReplacement(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tc := newEmissionTestConnection(t, false)
		c := tc.conn
		q := c.sendQueue.(*sendQueue)
		first, second := getPacketWithContents([]byte("active")), getPacketWithContents([]byte("pending"))
		q.Send(first, 0, protocol.ECNNon, sendMetadata{})
		q.Send(second, 0, protocol.ECNNon, sendMetadata{})
		release := make(chan struct{})
		tc.sendConn.EXPECT().Write([]byte("active"), uint16(0), protocol.ECNNon).DoAndReturn(func([]byte, uint16, protocol.ECN) error { <-release; return nil })
		tc.sendConn.EXPECT().Write([]byte("pending"), uint16(0), protocol.ECNNon).Return(nil)
		worker := make(chan error, 1)
		go func() { worker <- q.Run() }()
		synctest.Wait()
		next := NewMockSendConn(gomock.NewController(t))
		replaced := make(chan sender, 1)
		go func() { replaced <- c.emission.replacePath(next, &c.handshakeSendFeedback) }()
		synctest.Wait()
		require.Same(t, q, c.sendQueue, "old worker must finish before queue replacement")
		require.EqualValues(t, 1, first.refCount)
		require.EqualValues(t, 1, second.refCount)
		close(release)
		synctest.Wait()
		require.NoError(t, <-worker)
		newQueue := <-replaced
		require.Same(t, newQueue, c.sendQueue)
		require.Same(t, next, c.conn)
		require.Zero(t, first.refCount)
		require.Zero(t, second.refCount)
		// The replacement remains connection-started and borrows the same feedback owner.
		require.Same(t, &c.handshakeSendFeedback, newQueue.(*sendQueue).feedback)
		go func() { worker <- newQueue.Run() }()
		newQueue.Close()
		require.NoError(t, <-worker)
	})
}

func TestEmissionPathRebinding(t *testing.T) {
	tc := newEmissionTestConnection(t, false)
	c := tc.conn
	raw := NewMockRawConn(gomock.NewController(t))
	oldAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1111}
	newAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 2222}
	raw.EXPECT().LocalAddr().Return(oldAddr)
	conn := newSendConn(raw, oldAddr, packetInfo{}, c.logger)
	q := newSendQueue(conn, &c.handshakeSendFeedback).(*sendQueue)
	c.sendQueue = q
	c.conn = conn
	buf := getPacketWithContents([]byte("queued before rebinding"))
	q.Send(buf, 0, protocol.ECNNon, sendMetadata{})
	c.emission.rebindPath(newAddr, packetInfo{})
	require.Same(t, q, c.sendQueue)
	raw.EXPECT().WritePacket(buf.Data, newAddr, gomock.Any(), uint16(0), protocol.ECNNon).Return(len(buf.Data), nil)
	close(q.closeCalled)
	require.NoError(t, q.Run())
	require.Zero(t, buf.refCount)
}
