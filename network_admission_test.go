package quic

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
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
	n, err := sc.sendBatch([][]byte{[]byte("one"), []byte("two")}, protocol.ECT0)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.EqualValues(t, 1, batchCalls.Load())
	require.Equal(t, appendExternalECN(info.OOB(), peer.LocalAddr().(*net.UDPAddr), protocol.ECT0), observed)
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

func TestNetworkAdmissionServerResponseRouteLoss(t *testing.T) {
	peer := newUDPConnLocalhost(t)
	tr := &Transport{Conn: newUDPConnLocalhost(t)}
	require.NoError(t, configureNetworkAdmission(t, tr,
		func(netip.AddrPort, netip.AddrPort, []byte) bool { return true },
		func(netip.AddrPort, netip.AddrPort, []byte) bool { return false }, false))
	require.NoError(t, tr.init(false))
	defer tr.Close()
	// Exercise the real packer and synchronous probe consumer for a mandatory
	// response whose admitted return route became unusable before submission.
	tc := newEmissionTestConnection(t, false)
	conn := tc.conn
	conn.conn = newSendConn(tr.conn, peer.LocalAddr(), packetInfo{}, utils.DefaultLogger)
	conn.emission.queue = newSendQueue(conn.conn, nil)
	err := conn.emission.serverProbe(protocol.ParseConnectionID([]byte{8, 7, 6, 5}),
		[]ackhandler.Frame{{Frame: &wire.PathResponseFrame{Data: [8]byte{1, 2, 3, 4}}}},
		&path{bytesReceived: 400},
		peer.LocalAddr(), packetInfo{}, 0, monotime.Now())
	require.ErrorContains(t, err, "network policy denied")
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(20*time.Millisecond)))
	_, _, err = peer.ReadFrom(make([]byte, 64))
	require.Error(t, err)
	var timeout net.Error
	require.ErrorAs(t, err, &timeout)
	require.True(t, timeout.Timeout())
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
	require.True(t, client.peerParams.DisableActiveMigration)
	server, err := ln.Accept(ctx)
	require.NoError(t, err)
	defer server.CloseWithError(0, "")
	require.True(t, initialSeen.Load())
	require.True(t, handshakeSeen.Load())
	exchangeAdmissionStream(t, ctx, client, server)
	newIP := net.IPv4(127, 0, 0, 4)
	priorEvents := len(recorder.Events(qlog.PacketSent{}))
	socket.ip.Store(&newIP)
	exchangeAdmissionStream(t, ctx, client, server)
	require.Eventually(t, func() bool {
		for _, event := range recorder.Events(qlog.PacketSent{})[priorEvents:] {
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
	// The adapter boundary receives already-extracted datagrams: the
	// datagrams of one coalesced read, each copied into its own buffer and
	// sharing the read's destination/OOB facts. Native OS binding
	// qualification is a separate program obligation.
	conn := &admissionExtractedConn{PacketConn: newUDPConnLocalhost(t), ready: make(chan struct{})}
	remote := newUDPConnLocalhost(t).LocalAddr()
	// The transport's reader never takes a coalesced-tier buffer, so their
	// release is observable without reading a buffer another goroutine took.
	var denied []*packetBuffer
	for _, datagram := range []string{"\x00a", "\x00b", "\x00c"} {
		buffer := getCoalescedPacketBuffer()
		buffer.Data = append(buffer.Data, datagram...)
		denied = append(denied, buffer)
		conn.packets = append(conn.packets, receivedPacket{buffer: buffer, data: buffer.Data, remoteAddr: remote, info: packetInfo{addr: netip.MustParseAddr("10.0.0.9")}})
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
	for i, buffer := range denied {
		require.Zero(t, buffer.refCount, "denied datagram %d must release its storage", i)
	}
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

type networkAdmissionDelegationV1 interface {
	DelegateNetworkAdmissionV1() error
}

func delegateNetworkAdmission(t *testing.T, tr *Transport) error {
	t.Helper()
	api, ok := any(tr).(networkAdmissionDelegationV1)
	require.True(t, ok, "transport must expose the delegated admission capability")
	return api.DelegateNetworkAdmissionV1()
}

func TestNetworkAdmissionDelegatedSkipsCallbacks(t *testing.T) {
	// Without delegation the same traffic reaches both callbacks: V1 is unchanged.
	for _, tc := range []struct {
		name      string
		delegated bool
	}{{"v1", false}, {"delegated", true}} {
		t.Run(tc.name, func(t *testing.T) {
			peer := newUDPConnLocalhost(t)
			tr := &Transport{Conn: newUDPConnLocalhost(t), ConnectionIDLength: 8, StatelessResetKey: &StatelessResetKey{1}}
			writer, err := tr.UDPBatchWriterV1(tr.Conn.(*net.UDPConn))
			require.NoError(t, err)
			var batchCalls atomic.Int64
			require.NoError(t, tr.ConfigureExternalPacketIOV1(tr.Conn, false, func(bufs [][]byte, oob []byte, addr *net.UDPAddr) (int, error) {
				batchCalls.Add(1)
				return writer(bufs, oob, addr)
			}))
			var receives, sends atomic.Int64
			require.NoError(t, configureNetworkAdmission(t, tr,
				func(netip.AddrPort, netip.AddrPort, []byte) bool { receives.Add(1); return true },
				func(netip.AddrPort, netip.AddrPort, []byte) bool { sends.Add(1); return true }, false))
			if tc.delegated {
				require.NoError(t, delegateNetworkAdmission(t, tr))
			}
			_, err = tr.Listen(testdata.GetTLSConfig(), nil)
			require.NoError(t, err)
			defer tr.Close()
			// A timed read initializes the public non-QUIC delivery seam.
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			_, _, err = tr.ReadNonQUICPacket(ctx, nil)
			require.ErrorIs(t, err, context.Canceled)

			// Receive: a non-QUIC datagram, and an unknown short header that is
			// answered by a stateless reset.
			_, err = peer.WriteTo([]byte("\x00non-quic"), tr.Conn.LocalAddr())
			require.NoError(t, err)
			ctx, cancel = context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			b := make([]byte, 64)
			n, _, err := tr.ReadNonQUICPacket(ctx, b)
			require.NoError(t, err)
			require.Equal(t, "\x00non-quic", string(b[:n]))
			short := append([]byte{0x40}, 1, 2, 3, 4, 5, 6, 7, 8)
			short = append(short, make([]byte, 64)...)
			_, err = peer.WriteTo(short, tr.Conn.LocalAddr())
			require.NoError(t, err)
			require.NoError(t, peer.SetReadDeadline(time.Now().Add(time.Second)))
			b = make([]byte, 1500)
			n, _, err = peer.ReadFrom(b)
			require.NoError(t, err)
			require.Equal(t, protocol.MinStatelessResetSize, n, "stateless submission")

			// Send: ordinary transport and connection writes, a probe, and a batch.
			_, err = tr.WriteTo([]byte("ordinary"), peer.LocalAddr())
			require.NoError(t, err)
			sc := newSendConn(tr.conn, peer.LocalAddr(), packetInfo{}, utils.DefaultLogger)
			require.NoError(t, sc.Write([]byte("connection"), 0, protocol.ECNUnsupported))
			require.NoError(t, sc.WriteTo([]byte("probe"), peer.LocalAddr(), packetInfo{}))
			n, err = sc.sendBatch([][]byte{[]byte("one"), []byte("two")}, protocol.ECT0)
			require.NoError(t, err)
			require.Equal(t, 2, n)
			require.EqualValues(t, 1, batchCalls.Load())
			for _, want := range []string{"ordinary", "connection", "probe", "one", "two"} {
				n, _, err = peer.ReadFrom(b)
				require.NoError(t, err)
				require.Equal(t, want, string(b[:n]))
			}
			if tc.delegated {
				require.Zero(t, receives.Load(), "delegated receive must not evaluate the callback")
				require.Zero(t, sends.Load(), "delegated submissions must not evaluate the callback")
			} else {
				require.GreaterOrEqual(t, receives.Load(), int64(2))
				require.GreaterOrEqual(t, sends.Load(), int64(5))
			}
		})
	}
}

func delegatedTestAdmission(t *testing.T) *packetPolicyConn {
	t.Helper()
	never := func(netip.AddrPort, netip.AddrPort, []byte) bool {
		t.Error("delegated admission must not evaluate a callback")
		return false
	}
	return &packetPolicyConn{network: &networkAdmission{
		local:     netip.MustParseAddrPort("192.0.2.2:443"),
		receive:   never,
		send:      never,
		delegated: true,
	}}
}

func TestNetworkAdmissionDelegatedAllocations(t *testing.T) {
	c := delegatedTestAdmission(t)
	remote := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 4433}
	// An extracted destination is exactly the case in which V1 re-encodes facts.
	packet := receivedPacket{remoteAddr: remote, info: packetInfo{addr: netip.MustParseAddr("192.0.2.2")}}
	oob := []byte{1, 2, 3, 4}
	require.Zero(t, testing.AllocsPerRun(100, func() {
		if !c.allowsReceive(packet) {
			t.Fatal("delegated receive rejected a valid endpoint")
		}
	}))
	require.Zero(t, testing.AllocsPerRun(100, func() {
		if err := c.checkSend(remote, oob); err != nil {
			t.Fatal(err)
		}
	}))
}

func TestNetworkAdmissionDelegatedEndpointChecks(t *testing.T) {
	c := delegatedTestAdmission(t)
	for _, addr := range []net.Addr{
		&net.TCPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 4433},
		(*net.UDPAddr)(nil),
		&net.UDPAddr{IP: net.IPv4(192, 0, 2, 1)},
		&net.UDPAddr{Port: 4433},
	} {
		require.ErrorIs(t, c.checkSend(addr, nil), errNetworkAdmissionDenied, "%#v", addr)
		require.False(t, c.allowsReceive(receivedPacket{remoteAddr: addr}), "%#v", addr)
	}
}

func TestNetworkAdmissionDelegationConfiguration(t *testing.T) {
	allow := func(netip.AddrPort, netip.AddrPort, []byte) bool { return true }
	unconfigured := &Transport{Conn: newUDPConnLocalhost(t)}
	require.ErrorContains(t, delegateNetworkAdmission(t, unconfigured), "requires network admission")

	tr := &Transport{Conn: newUDPConnLocalhost(t)}
	require.NoError(t, configureNetworkAdmission(t, tr, allow, allow, false))
	require.NoError(t, delegateNetworkAdmission(t, tr))
	require.ErrorContains(t, delegateNetworkAdmission(t, tr), "already delegated")
	require.ErrorContains(t, configureNetworkAdmission(t, tr, allow, allow, false), "already configured")

	late := &Transport{Conn: newUDPConnLocalhost(t)}
	require.NoError(t, configureNetworkAdmission(t, late, allow, allow, false))
	_, err := late.WriteTo([]byte("ordinary"), newUDPConnLocalhost(t).LocalAddr())
	require.NoError(t, err)
	defer late.Close()
	require.ErrorContains(t, delegateNetworkAdmission(t, late), "after initialization")
}

func TestNetworkAdmissionDelegatedRestrictedProfile(t *testing.T) {
	allow := func(netip.AddrPort, netip.AddrPort, []byte) bool { return true }
	origin := &Transport{Conn: newUDPConnLocalhost(t)}
	require.NoError(t, configureNetworkAdmission(t, origin, allow, allow, true))
	require.NoError(t, delegateNetworkAdmission(t, origin))
	require.NoError(t, origin.init(false))
	defer origin.Close()
	target := &Transport{Conn: newUDPConnLocalhost(t)}
	defer target.Close()
	conn := &Conn{conn: newSendConn(origin.conn, newUDPConnLocalhost(t).LocalAddr(), packetInfo{}, utils.DefaultLogger), peerParams: &wire.TransportParameters{}}
	conn.emission.conn = &conn.conn
	require.True(t, networkDisablesActiveMigration(conn.conn))
	path, err := conn.AddPath(target)
	require.ErrorContains(t, err, "network restricted connection cannot add a path")
	require.Nil(t, path)
	require.Nil(t, target.conn, "rejected path must not initialize the target")

	plain := &Transport{Conn: newUDPConnLocalhost(t)}
	require.NoError(t, plain.init(false))
	defer plain.Close()
	restricted := &Transport{Conn: newUDPConnLocalhost(t)}
	require.NoError(t, configureNetworkAdmission(t, restricted, allow, allow, false))
	require.NoError(t, delegateNetworkAdmission(t, restricted))
	defer restricted.Close()
	conn = &Conn{conn: newSendConn(plain.conn, newUDPConnLocalhost(t).LocalAddr(), packetInfo{}, utils.DefaultLogger), peerParams: &wire.TransportParameters{}}
	conn.emission.conn = &conn.conn
	path, err = conn.AddPath(restricted)
	require.ErrorContains(t, err, "network restricted transport cannot accept a path")
	require.Nil(t, path)
	require.Nil(t, restricted.conn, "rejected path must not initialize the target")
}

// Delegation replaces only the network-admission callbacks; a fixed peer
// remains an independent filter in both directions.
func TestNetworkAdmissionDelegatedWithFixedPeer(t *testing.T) {
	peer, foreign := newUDPConnLocalhost(t), newUDPConnLocalhost(t)
	tr := &Transport{Conn: newUDPConnLocalhost(t)}
	require.NoError(t, tr.ConfigureFixedPeerV1(peer.LocalAddr().(*net.UDPAddr)))
	var calls atomic.Int64
	count := func(netip.AddrPort, netip.AddrPort, []byte) bool { calls.Add(1); return true }
	require.NoError(t, configureNetworkAdmission(t, tr, count, count, false))
	require.NoError(t, delegateNetworkAdmission(t, tr))
	_, err := tr.WriteTo([]byte("foreign"), foreign.LocalAddr())
	require.ErrorContains(t, err, "does not match fixed peer")
	defer tr.Close()
	_, err = tr.WriteTo([]byte("peer"), peer.LocalAddr())
	require.NoError(t, err)
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(time.Second)))
	b := make([]byte, 64)
	n, _, err := peer.ReadFrom(b)
	require.NoError(t, err)
	require.Equal(t, "peer", string(b[:n]))
	require.False(t, tr.policyConn.allowsReceive(receivedPacket{remoteAddr: foreign.LocalAddr()}))
	require.True(t, tr.policyConn.allowsReceive(receivedPacket{remoteAddr: peer.LocalAddr()}))
	require.Zero(t, calls.Load())
}
