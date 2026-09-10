package quic

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"

	"github.com/stretchr/testify/require"
)

// These tests run the receive-owner transitions synchronously, without socket
// workers, so ownership can be inspected before any buffer is acquired again.
func newZeroRTTLifetimeServer(t *testing.T) *baseServer {
	t.Helper()
	conn, err := wrapConn(newUDPConnLocalhost(t))
	require.NoError(t, err)
	return &baseServer{
		conn:                   conn,
		tr:                     &packetHandlerMap{handlers: make(map[protocol.ConnectionID]packetHandler), logger: utils.DefaultLogger},
		config:                 populateConfig(nil),
		logger:                 utils.DefaultLogger,
		acceptEarlyConns:       true,
		zeroRTTQueues:          make(map[protocol.ConnectionID]*zeroRTTQueue),
		connIDGenerator:        &protocol.DefaultConnectionIDGenerator{},
		retryQueue:             make(chan rejectedPacket, 1),
		connectionRefusedQueue: make(chan rejectedPacket, 1),
	}
}

func zeroRTTLifetimePacket(t *testing.T, id protocol.ConnectionID) receivedPacket {
	t.Helper()
	return getLongHeaderPacket(t, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 42}, &wire.ExtendedHeader{
		Header:          wire.Header{Type: protocol.PacketType0RTT, DestConnectionID: id, Version: protocol.Version1, Length: 100},
		PacketNumberLen: protocol.PacketNumberLen4,
	}, make([]byte, 100))
}

func TestServerZeroRTTLifetimeRetry(t *testing.T) {
	s := newZeroRTTLifetimeServer(t)
	s.verifySourceAddress = func(net.Addr) bool { return true }
	id := protocol.ParseConnectionID([]byte{1, 2, 3, 4, 5, 6, 7, 8})
	p := zeroRTTLifetimePacket(t, id)
	initial := getValidInitialPacket(t, p.remoteAddr, randConnID(5), id)
	require.True(t, s.handlePacketImpl(p))
	require.True(t, s.handlePacketImpl(initial))
	require.NotContains(t, s.zeroRTTQueues, id)
	require.Zero(t, p.buffer.refCount, "Retry must dispose the invalidated datagram")
	reply := <-s.retryQueue
	require.Same(t, initial.buffer, reply.buffer)
	require.Equal(t, 1, reply.buffer.refCount)
	reply.buffer.Release()
}

func TestServerZeroRTTLifetimeRefusal(t *testing.T) {
	for _, callback := range []string{"config", "context"} {
		t.Run(callback, func(t *testing.T) {
			s := newZeroRTTLifetimeServer(t)
			if callback == "config" {
				s.config.GetConfigForClient = func(*ClientInfo) (*Config, error) { return nil, errors.New("refused") }
			} else {
				s.connContext = func(context.Context, *ClientInfo) (context.Context, error) { return nil, errors.New("refused") }
			}
			id := randConnID(8)
			p := zeroRTTLifetimePacket(t, id)
			initial := getValidInitialPacket(t, p.remoteAddr, randConnID(5), id)
			require.True(t, s.handlePacketImpl(p))
			require.True(t, s.handlePacketImpl(initial))
			require.NotContains(t, s.zeroRTTQueues, id)
			require.Zero(t, p.buffer.refCount)
			reply := <-s.connectionRefusedQueue
			require.Same(t, initial.buffer, reply.buffer)
			require.Equal(t, 1, reply.buffer.refCount)
			reply.buffer.Release()
		})
	}
}

func TestServerZeroRTTLifetimeCollision(t *testing.T) {
	testServerInitialLifetimeCollision(t)
}

func TestServerZeroRTTLifetimeExpiry(t *testing.T) {
	s := newZeroRTTLifetimeServer(t)
	id, otherID := randConnID(8), randConnID(8)
	p, other := zeroRTTLifetimePacket(t, id), zeroRTTLifetimePacket(t, otherID)
	other.rcvTime = p.rcvTime.Add(protocol.Max0RTTQueueingDuration / 2)
	require.True(t, s.handlePacketImpl(p))
	require.True(t, s.handlePacketImpl(other))
	s.cleanupZeroRTTQueues(p.rcvTime.Add(protocol.Max0RTTQueueingDuration))
	require.NotContains(t, s.zeroRTTQueues, id)
	require.Zero(t, p.buffer.refCount)
	require.Contains(t, s.zeroRTTQueues, otherID)
	require.Equal(t, 1, other.buffer.refCount)
	require.Equal(t, other.rcvTime.Add(protocol.Max0RTTQueueingDuration), s.nextZeroRTTCleanup)
	s.cleanupZeroRTTQueues(s.nextZeroRTTCleanup)
	require.Empty(t, s.zeroRTTQueues)
	require.Zero(t, other.buffer.refCount)
	require.True(t, s.nextZeroRTTCleanup.IsZero())
}

func TestServerZeroRTTLifetimeTransfer(t *testing.T) {
	s := newZeroRTTLifetimeServer(t)
	id := randConnID(8)
	first, second := zeroRTTLifetimePacket(t, id), zeroRTTLifetimePacket(t, id)
	initial := getValidInitialPacket(t, first.remoteAddr, randConnID(5), id)
	want := []receivedPacket{initial, first, second}
	var received int
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	recorder := newConnConstructorRecorder(&connTestHooks{
		handlePacket: func(p receivedPacket) {
			require.Less(t, received, len(want))
			require.Same(t, want[received].buffer, p.buffer)
			require.Equal(t, want[received].data, p.data)
			require.Equal(t, 1, p.buffer.refCount)
			received++
			// A consuming receiver can finish synchronously. Server retirement
			// must not release any of these transferred inputs again.
			p.buffer.Release()
		},
		context: func() context.Context { return ctx },
		run:     func() error { cancel(); close(done); return nil },
	})
	s.newConn = recorder.NewConn
	require.True(t, s.handlePacketImpl(first))
	require.True(t, s.handlePacketImpl(second))
	require.True(t, s.handlePacketImpl(initial))
	require.Equal(t, len(want), received)
	require.NotContains(t, s.zeroRTTQueues, id)
	<-done
	s.handshakingCount.Wait()
}

func TestServerZeroRTTLifetimeAdmission(t *testing.T) {
	for _, limit := range []string{"datagrams", "groups"} {
		t.Run(limit, func(t *testing.T) {
			s := newZeroRTTLifetimeServer(t)
			id := randConnID(8)
			count := protocol.Max0RTTQueueLen
			if limit == "groups" {
				count = protocol.Max0RTTQueues
			}
			packets := make([]receivedPacket, count)
			for i := range packets {
				if limit == "groups" {
					id = randConnID(8)
				}
				packets[i] = zeroRTTLifetimePacket(t, id)
			}
			if limit == "groups" {
				id = randConnID(8)
			}
			rejected := zeroRTTLifetimePacket(t, id)
			for _, p := range packets {
				require.True(t, s.handlePacketImpl(p))
			}
			// Run the actual consuming loop to dispose the rejected input.
			s.receivedPackets = make(chan receivedPacket, 1)
			s.receivedPackets <- rejected
			s.running = make(chan struct{})
			s.errorChan = make(chan struct{})
			sent := make(chan struct{})
			finish := make(chan struct{})
			// A second packet transfers through the routing map and supplies a
			// deterministic receive-owner barrier after rejection is complete.
			barrierID := randConnID(8)
			barrier := zeroRTTLifetimePacket(t, barrierID)
			require.True(t, s.tr.Add(barrierID, &wrappedConn{testHooks: &connTestHooks{
				handlePacket: func(p receivedPacket) { p.buffer.Release(); close(sent); <-finish; close(s.errorChan) },
			}}))
			go s.run()
			s.receivedPackets <- barrier
			<-sent
			require.Zero(t, rejected.buffer.refCount)
			for _, p := range packets {
				require.Equal(t, 1, p.buffer.refCount)
			}
			// Accepted groups remain owned until shutdown retires them.
			close(finish)
			<-s.running
			for _, p := range packets {
				require.Zero(t, p.buffer.refCount)
			}
		})
	}
}
