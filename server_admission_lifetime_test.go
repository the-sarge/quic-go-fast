package quic

import (
	"net"
	"testing"
	"testing/synctest"

	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"

	"github.com/stretchr/testify/require"
)

func newServerAdmissionLifetimeFixture() *baseServer {
	return &baseServer{
		receivedPackets: make(chan receivedPacket, 1),
		errorChan:       make(chan struct{}),
		running:         make(chan struct{}),
		stopAccepting:   make(chan struct{}),
		onClose:         func() {},
		logger:          utils.DefaultLogger,
	}
}

func serverAdmissionLifetimePacket() receivedPacket {
	b := &packetBuffer{Data: make([]byte, 128, protocol.MaxPacketBufferSize), refCount: 1}
	return receivedPacket{buffer: b, data: b.Data, remoteAddr: &net.UDPAddr{Port: 42}}
}

func TestServerAdmissionLifetime(t *testing.T) {
	t.Run("accepted and full", func(t *testing.T) {
		s := newServerAdmissionLifetimeFixture()
		accepted, rejected := serverAdmissionLifetimePacket(), serverAdmissionLifetimePacket()
		s.handlePacket(accepted)
		require.Equal(t, 1, accepted.buffer.refCount)
		s.handlePacket(rejected)
		require.Zero(t, rejected.buffer.refCount)
		got := <-s.receivedPackets
		require.Same(t, accepted.buffer, got.buffer)
		got.buffer.Release()
	})
	t.Run("closed", func(t *testing.T) {
		s := newServerAdmissionLifetimeFixture()
		close(s.running)
		require.NoError(t, s.Close())
		p := serverAdmissionLifetimePacket()
		s.handlePacket(p)
		require.Empty(t, s.receivedPackets)
		require.Zero(t, p.buffer.refCount)
	})
}

func TestServerAdmissionLifetimeReceiveExit(t *testing.T) {
	s := newServerAdmissionLifetimeFixture()
	pending := serverAdmissionLifetimePacket()
	s.handlePacket(pending)
	id := protocol.ParseConnectionID([]byte{1})
	retained := serverAdmissionLifetimePacket()
	s.zeroRTTQueues = map[protocol.ConnectionID]*zeroRTTQueue{id: {packets: []receivedPacket{retained}}}
	closed := make(chan struct{})
	go func() { s.Close(); close(closed) }()
	<-s.errorChan
	// Close must release admission synchronization before waiting for run.
	rejected := serverAdmissionLifetimePacket()
	admitted := make(chan struct{})
	go func() { s.handlePacket(rejected); close(admitted) }()
	<-admitted
	s.run()
	<-closed
	require.Empty(t, s.receivedPackets)
	require.Empty(t, s.zeroRTTQueues)
	require.Zero(t, pending.buffer.refCount)
	require.Zero(t, retained.buffer.refCount)
	require.Zero(t, rejected.buffer.refCount)
}

func TestServerAdmissionLifetimeEnqueueClose(t *testing.T) {
	s := newServerAdmissionLifetimeFixture()
	p := serverAdmissionLifetimePacket()
	start, sent, closed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() { <-start; s.handlePacket(p); close(sent) }()
	go func() { <-start; s.Close(); close(closed) }()
	close(start)
	<-s.errorChan
	s.run()
	<-sent
	<-closed
	require.Empty(t, s.receivedPackets)
	require.Zero(t, p.buffer.refCount)
}

func TestServerAdmissionLifetimeResponseExit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newServerAdmissionLifetimeFixture()
		c := newQueueLifetimeConn()
		c.writeGate = make(chan struct{})
		defer close(c.writeGate)
		s.conn = c
		s.config = populateConfig(nil)
		s.connIDGenerator = &protocol.DefaultConnectionIDGenerator{}
		s.tokenGenerator = handshake.NewTokenGenerator(TokenGeneratorKey{})
		s.versionNegotiationQueue = make(chan receivedPacket, 1)
		s.retryQueue = make(chan rejectedPacket, 1)
		s.invalidTokenQueue = make(chan rejectedPacket, 1)
		s.connectionRefusedQueue = make(chan rejectedPacket, 1)
		active := serverAdmissionLifetimePacket()
		active.data = active.data[:7]
		copy(active.data, []byte{0xc0, 0, 0, 0, 2, 0, 0})
		active.remoteAddr = &net.UDPAddr{Port: 42}
		pending := []receivedPacket{serverAdmissionLifetimePacket(), serverAdmissionLifetimePacket(), serverAdmissionLifetimePacket(), serverAdmissionLifetimePacket()}
		for i := range pending {
			pending[i].remoteAddr = active.remoteAddr
		}
		pending[0].data = pending[0].data[:len(active.data)]
		copy(pending[0].data, active.data)
		hdr := &wire.Header{Type: protocol.PacketTypeInitial, Version: protocol.Version1, Length: 100}
		s.versionNegotiationQueue <- active
		done := make(chan struct{})
		go func() { s.runSendQueue(); close(done) }()
		synctest.Wait()
		require.Equal(t, 1, c.writes)
		closed := make(chan struct{})
		go func() { s.Close(); close(closed) }()
		synctest.Wait()
		require.Equal(t, 1, active.buffer.refCount, "close request cannot retire an active response")
		// The receive producer may publish these after the close request.
		s.versionNegotiationQueue <- pending[0]
		s.retryQueue <- rejectedPacket{receivedPacket: pending[1], hdr: hdr}
		s.invalidTokenQueue <- rejectedPacket{receivedPacket: pending[2], hdr: hdr}
		s.connectionRefusedQueue <- rejectedPacket{receivedPacket: pending[3], hdr: hdr}
		close(s.running)
		<-closed
		// Public Close still doesn't join a blocked response write.
		// Open the write gate without closing it (the deferred close owns cleanup).
		c.writeGate <- struct{}{}
		synctest.Wait()
		select {
		case <-done:
		default:
			t.Fatal("response worker did not terminate after producer exit")
		}
		require.Zero(t, active.buffer.refCount)
		for _, p := range pending {
			require.Zero(t, p.buffer.refCount)
		}
		require.Empty(t, s.versionNegotiationQueue)
		require.Empty(t, s.retryQueue)
		require.Empty(t, s.invalidTokenQueue)
		require.Empty(t, s.connectionRefusedQueue)
		require.False(t, c.closed)
	})
}

func TestServerAdmissionLifetimeRetryResponse(t *testing.T) {
	s := newServerAdmissionLifetimeFixture()
	c := newQueueLifetimeConn()
	s.conn = c
	s.connIDGenerator = &protocol.DefaultConnectionIDGenerator{}
	s.tokenGenerator = handshake.NewTokenGenerator(TokenGeneratorKey{})
	p := serverAdmissionLifetimePacket()
	p.remoteAddr = &net.UDPAddr{Port: 42}
	s.sendRetry(rejectedPacket{receivedPacket: p, hdr: &wire.Header{
		Type: protocol.PacketTypeInitial, Version: protocol.Version1,
		SrcConnectionID:  protocol.ParseConnectionID([]byte{1}),
		DestConnectionID: protocol.ParseConnectionID([]byte{2}),
	}})
	require.Equal(t, 1, c.writes)
	require.Zero(t, p.buffer.refCount)
}
