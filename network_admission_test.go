package quic

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	quicproxy "github.com/quic-go/quic-go/integrationtests/tools/proxy"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/testdata"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/quic-go/quic-go/testutils/events"

	"github.com/stretchr/testify/require"
)

// Consumers can negotiate the extension while retaining upstream QUIC types.
type networkAdmissionV1 interface {
	ConfigureNetworkAdmissionV1(net.PacketConn, func(netip.AddrPort, netip.AddrPort, []byte) bool, func(netip.AddrPort, netip.AddrPort, []byte) bool, bool) error
}

func TestNetworkAdmissionBeforeStatelessAndAllocationEffects(t *testing.T) {
	allowed, denied := newUDPConnLocalhost(t), newUDPConnLocalhost(t)
	tr := &Transport{Conn: newUDPConnLocalhost(t), ConnectionIDLength: 8, StatelessResetKey: &StatelessResetKey{1}}
	var allocations atomic.Int64
	tr.ConnContext = func(ctx context.Context, _ *ClientInfo) (context.Context, error) {
		allocations.Add(1)
		return ctx, nil
	}
	peer := allowed.LocalAddr().(*net.UDPAddr).AddrPort()
	require.NoError(t, configureNetworkAdmission(t, tr,
		func(remote, _ netip.AddrPort, _ []byte) bool { return remote == peer },
		func(netip.AddrPort, netip.AddrPort, []byte) bool { return true }, false))
	_, err := tr.Listen(testdata.GetTLSConfig(), nil)
	require.NoError(t, err)
	defer tr.Close()
	// Use a real encrypted Initial to establish that rejection precedes the
	// new-connection boundary, independently of malformed-packet handling.
	clientTransport := &Transport{Conn: denied}
	dialCtx, cancel := context.WithTimeout(t.Context(), scaleDuration(50*time.Millisecond))
	_, err = clientTransport.Dial(dialCtx, tr.Conn.LocalAddr(), &tls.Config{RootCAs: testdata.GetRootCA(), ServerName: "localhost", NextProtos: testdata.GetTLSConfig().NextProtos}, nil)
	cancel()
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, clientTransport.Close())
	cid := protocol.ParseConnectionID([]byte{1, 2, 3, 4, 5, 6, 7, 8})
	// A forbidden unsupported version and unknown short header must not produce
	// negotiation or a reset. The admitted reset is our receive-loop barrier.
	version := getLongHeaderPacket(t, denied.LocalAddr(), &wire.ExtendedHeader{Header: wire.Header{Type: protocol.PacketTypeInitial, SrcConnectionID: cid, DestConnectionID: cid, Version: 0x42}, PacketNumberLen: protocol.PacketNumberLen4}, make([]byte, protocol.MinInitialPacketSize))
	defer version.buffer.Release()
	_, err = denied.WriteTo(version.data, tr.Conn.LocalAddr())
	require.NoError(t, err)
	short := append([]byte{0x40}, cid.Bytes()...)
	short = append(short, make([]byte, 64)...)
	_, err = denied.WriteTo(short, tr.Conn.LocalAddr())
	require.NoError(t, err)
	_, err = allowed.WriteTo(short, tr.Conn.LocalAddr())
	require.NoError(t, err)
	require.NoError(t, allowed.SetReadDeadline(time.Now().Add(time.Second)))
	b := make([]byte, 1500)
	n, _, err := allowed.ReadFrom(b)
	require.NoError(t, err)
	require.Equal(t, protocol.MinStatelessResetSize, n)
	require.Zero(t, allocations.Load())
	require.NoError(t, denied.SetReadDeadline(time.Now().Add(scaleDuration(20*time.Millisecond))))
	_, _, err = denied.ReadFrom(b)
	require.Error(t, err)
	var ne net.Error
	require.ErrorAs(t, err, &ne)
	require.True(t, ne.Timeout())
}

func TestNetworkAdmissionSubmissionFactsAndRouteLoss(t *testing.T) {
	peer, foreign := newUDPConnLocalhost(t), newUDPConnLocalhost(t)
	tr := &Transport{Conn: newUDPConnLocalhost(t)}
	selected := peer.LocalAddr().(*net.UDPAddr).AddrPort()
	var routeLost atomic.Bool
	var batchCalls atomic.Int64
	writer, err := tr.UDPBatchWriterV1(tr.Conn.(*net.UDPConn))
	require.NoError(t, err)
	require.NoError(t, tr.ConfigureExternalPacketIOV1(tr.Conn, false, func(bufs [][]byte, oob []byte, addr *net.UDPAddr) (int, error) {
		batchCalls.Add(1)
		return writer(bufs, oob, addr)
	}))
	var observed []byte
	require.NoError(t, configureNetworkAdmission(t, tr,
		func(netip.AddrPort, netip.AddrPort, []byte) bool { return true },
		func(remote, local netip.AddrPort, oob []byte) bool {
			if local != tr.Conn.LocalAddr().(*net.UDPAddr).AddrPort() {
				return false
			}
			observed = append([]byte(nil), oob...)
			clear(oob) // callback mutation cannot alter socket submission facts
			return remote == selected && !routeLost.Load()
		}, false))
	_, err = tr.WriteTo([]byte("ordinary"), peer.LocalAddr())
	require.NoError(t, err)
	defer tr.Close()
	info := packetInfo{addr: netip.MustParseAddr("127.0.0.1")}
	sc := newSendConn(tr.conn, peer.LocalAddr(), info, utils.DefaultLogger)
	n, err := sc.sendBatch([][]byte{[]byte("one"), []byte("two")}, protocol.ECNUnsupported)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.EqualValues(t, 1, batchCalls.Load())
	require.Equal(t, info.OOB(), observed)
	require.Equal(t, info.OOB(), sc.remoteAddrInfo.Load().oob)
	sc.ChangeRemoteAddr(foreign.LocalAddr(), packetInfo{})
	n, err = sc.sendBatch([][]byte{[]byte("denied")}, protocol.ECNUnsupported)
	require.Error(t, err)
	require.Zero(t, n)
	require.Error(t, sc.Write([]byte("denied"), 0, protocol.ECNUnsupported))
	require.Error(t, sc.WriteTo([]byte("probe"), foreign.LocalAddr(), packetInfo{}))
	sc.ChangeRemoteAddr(peer.LocalAddr(), packetInfo{})
	routeLost.Store(true)
	n, err = sc.sendBatch([][]byte{[]byte("stale")}, protocol.ECNUnsupported)
	require.Error(t, err)
	require.Zero(t, n)
	require.Error(t, sc.WriteTo([]byte("stale probe"), peer.LocalAddr(), packetInfo{}))
	require.EqualValues(t, 1, batchCalls.Load())
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(time.Second)))
	for _, want := range []string{"ordinary", "one", "two"} {
		b := make([]byte, 64)
		n, _, err = peer.ReadFrom(b)
		require.NoError(t, err)
		require.Equal(t, want, string(b[:n]))
	}
}

func TestNetworkAdmissionEligibleRebindingAndSpoofedTraffic(t *testing.T) {
	serverTransport := &Transport{Conn: newUDPConnLocalhost(t)}
	defer serverTransport.Close()
	denied := newUDPConnLocalhost(t)
	deniedEndpoint := denied.LocalAddr().(*net.UDPAddr).AddrPort()
	var rejected atomic.Int64
	require.NoError(t, configureNetworkAdmission(t, serverTransport,
		func(remote, _ netip.AddrPort, _ []byte) bool {
			if remote == deniedEndpoint {
				rejected.Add(1)
				return false
			}
			return remote.Addr().IsLoopback()
		}, func(remote, _ netip.AddrPort, _ []byte) bool { return remote.Addr().IsLoopback() }, true))
	var recorder events.Recorder
	ln, err := serverTransport.Listen(testdata.GetTLSConfig(), &Config{Tracer: func(context.Context, bool, ConnectionID) qlogwriter.Trace { return &events.Trace{Recorder: &recorder} }})
	require.NoError(t, err)
	var captureMutex sync.Mutex
	var genuine, serverPacket []byte
	var outgoing atomic.Int64
	proxy := &quicproxy.Proxy{Conn: newUDPConnLocalhost(t), ServerAddr: ln.Addr().(*net.UDPAddr), ObserveSocket: func(e quicproxy.SocketEvent) {
		if e.Direction == quicproxy.DirectionIncoming && e.Operation == "write" && e.Err == nil && len(e.Data) > 0 && !wire.IsLongHeaderPacket(e.Data[0]) {
			captureMutex.Lock()
			genuine = append([]byte(nil), e.Data...)
			captureMutex.Unlock()
		}
		if e.Direction == quicproxy.DirectionOutgoing && e.Operation == "write" && e.Err == nil {
			outgoing.Add(1)
			captureMutex.Lock()
			serverPacket = append([]byte(nil), e.Data...)
			captureMutex.Unlock()
		}
	}}
	require.NoError(t, proxy.Start())
	defer proxy.Close()
	clientTransport := &Transport{Conn: newUDPConnLocalhost(t)}
	defer clientTransport.Close()
	serverEndpoint := proxy.LocalAddr().(*net.UDPAddr).AddrPort()
	var clientRejected atomic.Int64
	require.NoError(t, configureNetworkAdmission(t, clientTransport,
		func(remote, _ netip.AddrPort, _ []byte) bool {
			if remote != serverEndpoint {
				clientRejected.Add(1)
				return false
			}
			return true
		}, func(remote, _ netip.AddrPort, _ []byte) bool { return remote == serverEndpoint }, false))
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	client, err := clientTransport.Dial(ctx, proxy.LocalAddr(), &tls.Config{RootCAs: testdata.GetRootCA(), ServerName: "localhost", NextProtos: testdata.GetTLSConfig().NextProtos}, nil)
	require.NoError(t, err)
	defer client.CloseWithError(0, "")
	server, err := ln.Accept(ctx)
	require.NoError(t, err)
	defer server.CloseWithError(0, "")
	require.True(t, client.peerParams.DisableActiveMigration)
	exchangeAdmissionStream(t, ctx, client, server)
	captureMutex.Lock()
	replay := append([]byte(nil), genuine...)
	wrongServer := append([]byte(nil), serverPacket...)
	captureMutex.Unlock()
	require.NotEmpty(t, replay)
	_, err = denied.WriteTo(replay, ln.Addr())
	require.NoError(t, err)
	_, err = denied.WriteTo(wrongServer, clientTransport.Conn.LocalAddr())
	require.NoError(t, err)
	exchangeAdmissionStream(t, ctx, client, server)
	require.Positive(t, rejected.Load())
	require.Eventually(t, func() bool { return clientRejected.Load() > 0 }, time.Second, time.Millisecond)
	require.NoError(t, denied.SetReadDeadline(time.Now().Add(scaleDuration(20*time.Millisecond))))
	_, _, err = denied.ReadFrom(make([]byte, 1500))
	require.Error(t, err, "forbidden genuine input must not generate any response")
	changed := newUDPConnLocalhost(t)
	require.NoError(t, proxy.SwitchConn(clientTransport.Conn.LocalAddr().(*net.UDPAddr), changed))
	before := outgoing.Load()
	exchangeAdmissionStream(t, ctx, client, server)
	require.Eventually(t, func() bool { return outgoing.Load() > before }, time.Second, time.Millisecond)
	require.Eventually(t, func() bool {
		for _, event := range recorder.Events(qlog.PacketSent{}) {
			for _, frame := range event.(qlog.PacketSent).Frames {
				if _, ok := frame.Frame.(*qlog.PathChallengeFrame); ok {
					return true
				}
			}
		}
		return false
	}, time.Second, time.Millisecond, "eligible port rebinding must enter standard path validation")
}

// This system-boundary fixture changes the reported peer IP while forwarding
// bytes through one real socket. It qualifies protocol behavior, not OS routes.
type admissionAddressConn struct {
	net.PacketConn
	ip   atomic.Pointer[net.IP]
	peer atomic.Pointer[net.UDPAddr]
}

func (c *admissionAddressConn) ReadFrom(b []byte) (int, net.Addr, error) {
	n, addr, err := c.PacketConn.ReadFrom(b)
	if err != nil {
		return n, addr, err
	}
	udp := addr.(*net.UDPAddr)
	c.peer.Store(udp)
	if ip := c.ip.Load(); ip != nil {
		copyAddr := *udp
		copyAddr.IP = *ip
		addr = &copyAddr
	}
	return n, addr, nil
}

func (c *admissionAddressConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	if peer := c.peer.Load(); peer != nil {
		addr = peer
	}
	return c.PacketConn.WriteTo(b, addr)
}

func TestNetworkAdmissionEligibleIPChangeBeforeAndAfterHandshake(t *testing.T) {
	socket := &admissionAddressConn{PacketConn: newUDPConnLocalhost(t)}
	ip := net.IPv4(127, 0, 0, 2)
	socket.ip.Store(&ip) // first contact already differs from the real socket tuple
	serverTransport := &Transport{Conn: socket}
	defer serverTransport.Close()
	var initialSeen, handshakeSeen atomic.Bool
	allow := func(remote, _ netip.AddrPort, _ []byte) bool {
		switch remote.Addr().String() {
		case "127.0.0.2":
			initialSeen.Store(true)
		case "127.0.0.3":
			handshakeSeen.Store(true)
		}
		return remote.Addr().IsLoopback()
	}
	require.NoError(t, configureNetworkAdmission(t, serverTransport, allow, allow, true))
	var recorder events.Recorder
	tlsConfig := testdata.GetTLSConfig()
	tlsConfig.GetConfigForClient = func(*tls.ClientHelloInfo) (*tls.Config, error) {
		handshakeIP := net.IPv4(127, 0, 0, 3)
		socket.ip.Store(&handshakeIP)
		return nil, nil
	}
	ln, err := serverTransport.Listen(tlsConfig, &Config{Tracer: func(context.Context, bool, ConnectionID) qlogwriter.Trace { return &events.Trace{Recorder: &recorder} }})
	require.NoError(t, err)
	clientTransport := &Transport{Conn: newUDPConnLocalhost(t)}
	defer clientTransport.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	client, err := clientTransport.Dial(ctx, ln.Addr(), &tls.Config{RootCAs: testdata.GetRootCA(), ServerName: "localhost", NextProtos: testdata.GetTLSConfig().NextProtos}, nil)
	require.NoError(t, err)
	defer client.CloseWithError(0, "")
	server, err := ln.Accept(ctx)
	require.NoError(t, err)
	defer server.CloseWithError(0, "")
	require.True(t, initialSeen.Load())
	require.True(t, handshakeSeen.Load())
	exchangeAdmissionStream(t, ctx, client, server)
	newIP := net.IPv4(127, 0, 0, 4)
	socket.ip.Store(&newIP)
	exchangeAdmissionStream(t, ctx, client, server)
	require.Eventually(t, func() bool {
		for _, event := range recorder.Events(qlog.PacketSent{}) {
			for _, frame := range event.(qlog.PacketSent).Frames {
				if _, ok := frame.Frame.(*qlog.PathChallengeFrame); ok {
					return true
				}
			}
		}
		return false
	}, time.Second, time.Millisecond, "eligible IP changes must enter standard path validation")
}

func exchangeAdmissionStream(t *testing.T, ctx context.Context, client, server *Conn) {
	t.Helper()
	stream, err := client.OpenUniStreamSync(ctx)
	require.NoError(t, err)
	_, err = stream.Write([]byte("admitted reliable content"))
	require.NoError(t, err)
	require.NoError(t, stream.Close())
	received, err := server.AcceptUniStream(ctx)
	require.NoError(t, err)
	require.NoError(t, received.SetReadDeadline(time.Now().Add(5*time.Second)))
	data, err := io.ReadAll(received)
	require.NoError(t, err)
	require.Equal(t, "admitted reliable content", string(data))
}

func configureNetworkAdmission(t *testing.T, tr *Transport, receive, send func(netip.AddrPort, netip.AddrPort, []byte) bool, disableMigration bool) error {
	t.Helper()
	api, ok := any(tr).(networkAdmissionV1)
	require.True(t, ok, "transport must expose a standard-type network admission capability")
	return api.ConfigureNetworkAdmissionV1(tr.Conn, receive, send, disableMigration)
}

func TestNetworkAdmissionConfiguration(t *testing.T) {
	allow := func(netip.AddrPort, netip.AddrPort, []byte) bool { return true }
	tr := &Transport{Conn: newUDPConnLocalhost(t)}
	require.Error(t, configureNetworkAdmission(t, tr, nil, allow, false))
	require.NoError(t, configureNetworkAdmission(t, tr, allow, allow, true))
	require.Error(t, configureNetworkAdmission(t, tr, allow, allow, true))
	tr.Conn = newUDPConnLocalhost(t)
	_, err := tr.WriteTo([]byte("forbidden"), tr.Conn.LocalAddr())
	require.Error(t, err, "changing the resource must fail before initialization")
	require.Error(t, tr.Close())

	late := &Transport{Conn: newUDPConnLocalhost(t)}
	defer late.Close()
	_, err = late.WriteTo([]byte("ordinary"), tr.Conn.LocalAddr())
	require.NoError(t, err)
	require.Error(t, configureNetworkAdmission(t, late, allow, allow, false))
}

type admissionExtractedConn struct {
	net.PacketConn
	ready   chan struct{}
	packets []receivedPacket
}

func (c *admissionExtractedConn) ReadPacket() (receivedPacket, error) {
	<-c.ready
	if len(c.packets) > 0 {
		p := c.packets[0]
		c.packets = c.packets[1:]
		return p, nil
	}
	return (&basicConn{PacketConn: c.PacketConn}).ReadPacket()
}

func (c *admissionExtractedConn) WritePacket(b []byte, addr net.Addr, _ []byte, _ uint16, _ protocol.ECN) (int, error) {
	return c.WriteTo(b, addr)
}

func (c *admissionExtractedConn) capabilities() connCapabilities { return connCapabilities{} }

func TestNetworkAdmissionExtractedDatagramsAndReturnFacts(t *testing.T) {
	// The adapter boundary receives already-extracted datagrams. Exercise real
	// coalesced splitting, storage release and copied destination/OOB facts here;
	// native OS binding qualification is a separate program obligation.
	conn := &admissionExtractedConn{PacketConn: newUDPConnLocalhost(t), ready: make(chan struct{})}
	remote := newUDPConnLocalhost(t).LocalAddr()
	buffer := getCoalescedPacketBuffer()
	copy(buffer.Data[:6], "\x00a\x00b\x00c")
	var delivery coalescedDelivery
	p := delivery.accept(receivedPacket{buffer: buffer, data: buffer.Data[:6], remoteAddr: remote, info: packetInfo{addr: netip.MustParseAddr("10.0.0.9")}}, 2)
	slab := p.buffer.slab
	conn.packets = append(conn.packets, p)
	for {
		p, ok := delivery.next()
		if !ok {
			break
		}
		conn.packets = append(conn.packets, p)
	}
	good := getPacketBuffer()
	copy(good.Data[:3], "\x00ok")
	conn.packets = append(conn.packets, receivedPacket{buffer: good, data: good.Data[:3], remoteAddr: remote, info: packetInfo{addr: netip.MustParseAddr("10.0.0.10")}})
	tr := &Transport{Conn: conn}
	var evaluated atomic.Int64
	require.NoError(t, configureNetworkAdmission(t, tr,
		func(_, local netip.AddrPort, oob []byte) bool {
			evaluated.Add(1)
			// These two binding facts represent wrong ingress / forbidden return
			// route, even though the source itself is otherwise eligible.
			return local.Addr() == netip.MustParseAddr("10.0.0.10")
		}, func(netip.AddrPort, netip.AddrPort, []byte) bool { return true }, false))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, err := tr.ReadNonQUICPacket(ctx, nil)
	close(conn.ready)
	defer tr.Close()
	require.ErrorIs(t, err, context.Canceled)
	ctx, cancel = context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	b := make([]byte, 10)
	n, _, err := tr.ReadNonQUICPacket(ctx, b)
	require.NoError(t, err)
	require.Equal(t, "\x00ok", string(b[:n]))
	require.EqualValues(t, 4, evaluated.Load(), "each extracted datagram must be evaluated")
	require.True(t, slab.released(), "all denied coalesced views must release storage")
}

func TestNetworkAdmissionAlternatePathAndPreferredCIDPreservation(t *testing.T) {
	allow := func(netip.AddrPort, netip.AddrPort, []byte) bool { return true }
	origin := &Transport{Conn: newUDPConnLocalhost(t)}
	target := &Transport{Conn: newUDPConnLocalhost(t)}
	require.NoError(t, configureNetworkAdmission(t, origin, allow, allow, false))
	require.NoError(t, origin.init(false))
	defer origin.Close()
	defer target.Close()
	conn := &Conn{conn: newSendConn(origin.conn, newUDPConnLocalhost(t).LocalAddr(), packetInfo{}, utils.DefaultLogger), peerParams: &wire.TransportParameters{}}
	conn.emission.conn = &conn.conn
	path, err := conn.AddPath(target)
	require.ErrorContains(t, err, "network restricted")
	require.Nil(t, path)
	require.Nil(t, target.conn, "rejected path must not initialize the target")
	// Ordinary upstream behavior already declines preferred addresses. Retain
	// its independent handshake regression for advertised CID/reset bookkeeping.
	testConnectionHandshakeClient(t, true)
}

func TestNetworkAdmissionBeforeNonQUICDelivery(t *testing.T) {
	allowed, denied := newUDPConnLocalhost(t), newUDPConnLocalhost(t)
	tr := &Transport{Conn: newUDPConnLocalhost(t)}
	peer := allowed.LocalAddr().(*net.UDPAddr).AddrPort()
	require.NoError(t, configureNetworkAdmission(t, tr,
		func(remote, local netip.AddrPort, _ []byte) bool {
			return remote == peer && local == tr.Conn.LocalAddr().(*net.UDPAddr).AddrPort()
		}, func(netip.AddrPort, netip.AddrPort, []byte) bool { return true }, false))
	defer tr.Close()
	// A timed read initializes the public non-QUIC delivery seam.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := tr.ReadNonQUICPacket(ctx, nil)
	require.ErrorIs(t, err, context.Canceled)
	_, err = denied.WriteTo([]byte("\x00denied"), tr.Conn.LocalAddr())
	require.NoError(t, err)
	_, err = allowed.WriteTo([]byte("\x00allowed"), tr.Conn.LocalAddr())
	require.NoError(t, err)
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	b := make([]byte, 64)
	n, addr, err := tr.ReadNonQUICPacket(ctx, b)
	require.NoError(t, err)
	require.Equal(t, allowed.LocalAddr(), addr)
	require.Equal(t, "\x00allowed", string(b[:n]))
}
