//go:build darwin || linux || freebsd

package quic

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/multipathguard"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"

	"github.com/stretchr/testify/require"
)

// activateMultipathProfile is the fixtures' one activation. A test using it stays
// serial: Go holds every parallel test until the serial ones have finished.
func activateMultipathProfile(t *testing.T) {
	t.Helper()
	multipathguard.Active.Store(true)
	t.Cleanup(func() { multipathguard.Active.Store(false) })
}

func multipathConfig(t *testing.T) *Config {
	t.Helper()
	conf := &Config{}
	require.NoError(t, conf.ConfigureMultipathV1(true, 2, "primary-backup"))
	return conf
}

func grantedTransport(t *testing.T) *Transport {
	t.Helper()
	tr := &Transport{Conn: newUDPConnLocalhost(t)}
	require.NoError(t, tr.ConfigureMultipathIOV1(time.Second))
	t.Cleanup(func() { tr.Close() })
	return tr
}

type multipathPair struct {
	client, server     *Conn
	clientTr, serverTr *Transport
	serverAddr         *net.UDPAddr
}

func newMultipathPair(t *testing.T) *multipathPair {
	t.Helper()
	activateMultipathProfile(t)
	p := &multipathPair{clientTr: grantedTransport(t), serverTr: grantedTransport(t)}
	p.serverAddr = p.serverTr.Conn.LocalAddr().(*net.UDPAddr)
	ln, err := p.serverTr.Listen(nativeServerTLS(), multipathConfig(t))
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	p.client, err = p.clientTr.Dial(ctx, p.serverAddr, nativeClientTLS(), multipathConfig(t))
	require.NoError(t, err)
	t.Cleanup(func() { p.client.CloseWithError(0, "") })
	p.server, err = ln.Accept(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { p.server.CloseWithError(0, "") })
	<-p.server.HandshakeComplete()
	return p
}

// waitPathState polls the published observation, which the connection updates on its own.
func waitPathState(t *testing.T, path *Path, want func(MultipathPathStateV1) bool) MultipathPathStateV1 {
	t.Helper()
	var state MultipathPathStateV1
	require.Eventually(t, func() bool {
		var err error
		state, err = path.StateV1()
		require.NoError(t, err)
		return want(state)
	}, 5*time.Second, time.Millisecond, "last state: %+v", state)
	return state
}

func TestMultipathConfigSelection(t *testing.T) {
	var conf Config
	require.False(t, conf.MultipathEnabledV1(), "disabled by default")
	require.False(t, (*Config)(nil).MultipathEnabledV1())
	require.ErrorIs(t, (*Config)(nil).ConfigureMultipathV1(true, 2, "primary-backup"), ErrMultipathInvalidConfigV1)
	for _, bad := range []struct {
		paths  uint32
		policy string
	}{{0, "primary-backup"}, {3, "primary-backup"}, {2, "aggregate"}, {2, ""}} {
		require.ErrorIs(t, conf.ConfigureMultipathV1(true, bad.paths, bad.policy), ErrMultipathInvalidConfigV1)
		require.False(t, conf.MultipathEnabledV1(), "an error doesn't mutate")
	}
	require.NoError(t, conf.ConfigureMultipathV1(true, 1, "primary-backup"))
	require.Equal(t, multipathSelection{enabled: true, maxPaths: 1}, conf.Clone().multipath, "Clone keeps the selection")
	require.Equal(t, multipathSelection{enabled: true, maxPaths: 1}, populateConfig(&conf).multipath, "the effective copy keeps it")
	require.ErrorIs(t, conf.ConfigureMultipathV1(true, 9, "primary-backup"), ErrMultipathInvalidConfigV1)
	require.Equal(t, uint32(1), conf.multipath.maxPaths)
	require.NoError(t, conf.ConfigureMultipathV1(false, 0, ""))
	require.False(t, conf.MultipathEnabledV1())
}

// Without the guard, every public entry point refuses and nothing advertises. This is
// the semantic matrix of the one capability gate.
func TestMultipathPublicRefusal(t *testing.T) {
	conf := multipathConfig(t)
	ctx := t.Context()
	socket := func() *net.UDPConn { return newUDPConnLocalhost(t) }
	target := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9}
	dials := map[string]func() (*Conn, error){
		"Transport.Dial": func() (*Conn, error) { return (&Transport{Conn: socket()}).Dial(ctx, target, nativeClientTLS(), conf) },
		"Transport.DialEarly": func() (*Conn, error) {
			return (&Transport{Conn: socket()}).DialEarly(ctx, target, nativeClientTLS(), conf)
		},
		"Dial":          func() (*Conn, error) { return Dial(ctx, socket(), target, nativeClientTLS(), conf) },
		"DialEarly":     func() (*Conn, error) { return DialEarly(ctx, socket(), target, nativeClientTLS(), conf) },
		"DialAddr":      func() (*Conn, error) { return DialAddr(ctx, target.String(), nativeClientTLS(), conf) },
		"DialAddrEarly": func() (*Conn, error) { return DialAddrEarly(ctx, target.String(), nativeClientTLS(), conf) },
	}
	for name, dial := range dials {
		t.Run(name, func(t *testing.T) {
			_, err := dial()
			require.ErrorIs(t, err, ErrMultipathUnsupportedV1)
		})
	}
	listens := map[string]func() error{
		"Transport.Listen":      func() error { _, err := (&Transport{Conn: socket()}).Listen(nativeServerTLS(), conf); return err },
		"Transport.ListenEarly": func() error { _, err := (&Transport{Conn: socket()}).ListenEarly(nativeServerTLS(), conf); return err },
		"Listen":                func() error { _, err := Listen(socket(), nativeServerTLS(), conf); return err },
		"ListenEarly":           func() error { _, err := ListenEarly(socket(), nativeServerTLS(), conf); return err },
		"ListenAddr":            func() error { _, err := ListenAddr("127.0.0.1:0", nativeServerTLS(), conf); return err },
		"ListenAddrEarly":       func() error { _, err := ListenAddrEarly("127.0.0.1:0", nativeServerTLS(), conf); return err },
	}
	for name, listen := range listens {
		t.Run(name, func(t *testing.T) { require.ErrorIs(t, listen(), ErrMultipathUnsupportedV1) })
	}
	t.Run("grant", func(t *testing.T) {
		tr := &Transport{Conn: socket()}
		require.ErrorIs(t, tr.ConfigureMultipathIOV1(time.Second), ErrMultipathUnsupportedV1)
		require.ErrorIs(t, tr.WaitMultipathIOStoppedV1(ctx), ErrMultipathNotConfiguredV1)
		require.Nil(t, tr.responses)
	})
	t.Run("server selected", func(t *testing.T) {
		srv := &Transport{Conn: socket()}
		defer srv.Close()
		ln, err := srv.Listen(nativeServerTLS(), &Config{GetConfigForClient: func(*ClientInfo) (*Config, error) { return conf, nil }})
		require.NoError(t, err)
		defer ln.Close()
		cl := &Transport{Conn: socket()}
		defer cl.Close()
		dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_, err = cl.Dial(dialCtx, srv.Conn.LocalAddr(), nativeClientTLS(), nil)
		var transportErr *TransportError
		require.ErrorAs(t, err, &transportErr)
		require.Equal(t, ConnectionRefused, transportErr.ErrorCode)
	})
}

func TestMultipathExplicitGrantLifecycle(t *testing.T) {
	p := newMultipathPair(t)
	for _, c := range []*Conn{p.client, p.server} {
		negotiated, err := c.MultipathNegotiatedV1()
		require.NoError(t, err)
		require.True(t, negotiated)
		paths := c.PathsV1()
		require.Len(t, paths, 1)
		id, err := paths[0].PathIDV1()
		require.NoError(t, err)
		require.Zero(t, id)
		state, err := paths[0].StateV1()
		require.NoError(t, err)
		require.Equal(t, "live", state.Lifecycle)
		require.Equal(t, "active", state.LocalUse)
		require.Equal(t, "unknown", state.PeerPreference)
		require.True(t, state.Primary)
		require.True(t, state.Validated)
		require.True(t, state.Local.IsValid())
		require.True(t, state.Remote.IsValid())
	}
	socket := p.clientTr.Conn.(*net.UDPConn)
	require.ErrorIs(t, p.clientTr.WaitMultipathIOStoppedV1(t.Context()), ErrMultipathNotStoppingV1)
	paths := p.client.PathsV1()
	require.NoError(t, p.clientTr.Close())
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	require.NoError(t, p.clientTr.WaitMultipathIOStoppedV1(ctx))
	// The caller keeps its socket, and authority over it again.
	require.NoError(t, socket.SetWriteDeadline(time.Time{}))
	_, err := socket.WriteToUDP([]byte("after the join"), p.serverAddr)
	require.NoError(t, err)
	state := waitPathState(t, paths[0], func(s MultipathPathStateV1) bool { return s.Lifecycle == "closed" })
	require.False(t, state.Primary)
	require.Len(t, p.client.PathsV1(), 1, "the last snapshot stays readable")
	require.Error(t, paths[0].SetLocalUseV1("backup"), "the terminal error, not a hang")
	require.Empty(t, p.client.pathSurface.commands, "a late mutation isn't retained")
	require.NotEqual(t, "endpoint-fault", state.LastOutcome, "an explicit close isn't a fault")
}

func TestMultipathAddrOwnedLifecycle(t *testing.T) {
	activateMultipathProfile(t)
	var sockets []*net.UDPConn
	listen := listenUDPConn
	listenUDPConn = func(network string, addr *net.UDPAddr) (*net.UDPConn, error) {
		socket, err := listen(network, addr)
		if err == nil {
			sockets = append(sockets, socket)
		}
		return socket, err
	}
	t.Cleanup(func() { listenUDPConn = listen })

	ln, err := ListenAddr("127.0.0.1:0", nativeServerTLS(), multipathConfig(t))
	require.NoError(t, err)
	defer ln.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	client, err := DialAddr(ctx, ln.Addr().String(), nativeClientTLS(), multipathConfig(t))
	require.NoError(t, err)
	require.Len(t, sockets, 2)
	require.NotZero(t, client.srcConnIDLen, "the helper chose nonzero connection IDs")
	negotiated, err := client.MultipathNegotiatedV1()
	require.NoError(t, err)
	require.True(t, negotiated)
	server, err := ln.Accept(ctx)
	require.NoError(t, err)
	<-server.HandshakeComplete()
	negotiated, err = server.MultipathNegotiatedV1()
	require.NoError(t, err)
	require.True(t, negotiated)

	granted := func(socket *net.UDPConn) bool {
		nativeIOGrants.Lock()
		defer nativeIOGrants.Unlock()
		_, held := nativeIOGrants.sockets[socket]
		return held
	}
	require.True(t, granted(sockets[1]))
	// The connection owns its helper's socket, so closing it ends the endpoint too.
	require.NoError(t, client.CloseWithError(0, ""))
	require.Eventually(t, func() bool {
		if granted(sockets[1]) {
			return false
		}
		_, err := sockets[1].WriteToUDP([]byte("x"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9})
		return errors.Is(err, net.ErrClosed)
	}, 5*time.Second, time.Millisecond, "grant released and socket closed")
	require.NoError(t, ln.Close())
	require.Eventually(t, func() bool { return !granted(sockets[0]) }, 5*time.Second, time.Millisecond)
}

// socketTouchCounter reports whether a refused constructor read from a caller's socket or
// changed its read deadline.
type socketTouchCounter struct {
	net.PacketConn
	touches atomic.Int32
}

func (c *socketTouchCounter) ReadFrom(b []byte) (int, net.Addr, error) {
	c.touches.Add(1)
	return c.PacketConn.ReadFrom(b)
}

func (c *socketTouchCounter) SetReadDeadline(deadline time.Time) error {
	c.touches.Add(1)
	return c.PacketConn.SetReadDeadline(deadline)
}

func TestMultipathCallerSocketRefusal(t *testing.T) {
	activateMultipathProfile(t)
	socket := newUDPConnLocalhost(t)
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9}
	conf := multipathConfig(t)
	counted := &socketTouchCounter{PacketConn: newUDPConnLocalhost(t)}
	_, err := Dial(t.Context(), counted, addr, nativeClientTLS(), conf)
	require.ErrorIs(t, err, ErrMultipathUnsupportedV1)
	_, err = DialEarly(t.Context(), counted, addr, nativeClientTLS(), conf)
	require.ErrorIs(t, err, ErrMultipathUnsupportedV1)
	time.Sleep(scaleDuration(5 * time.Millisecond))
	require.Zero(t, counted.touches.Load(), "a refusal starts no reader and moves no deadline")
	_, err = Dial(t.Context(), socket, addr, nativeClientTLS(), conf)
	require.ErrorIs(t, err, ErrMultipathUnsupportedV1)
	_, err = DialEarly(t.Context(), socket, addr, nativeClientTLS(), conf)
	require.ErrorIs(t, err, ErrMultipathUnsupportedV1)
	_, err = Listen(socket, nativeServerTLS(), conf)
	require.ErrorIs(t, err, ErrMultipathUnsupportedV1)
	_, err = ListenEarly(socket, nativeServerTLS(), conf)
	require.ErrorIs(t, err, ErrMultipathUnsupportedV1)
	nativeIOGrants.Lock()
	_, held := nativeIOGrants.sockets[socket]
	nativeIOGrants.Unlock()
	require.False(t, held, "no takeover")
	require.NoError(t, socket.SetReadDeadline(time.Time{}), "the socket stays the caller's")
	_, err = socket.WriteToUDP([]byte("x"), addr)
	require.NoError(t, err)
	// An explicit transport without a grant is told what's missing.
	_, err = (&Transport{Conn: newUDPConnLocalhost(t)}).Dial(t.Context(), addr, nativeClientTLS(), conf)
	require.ErrorIs(t, err, ErrMultipathNotConfiguredV1)
}

func TestMultipathServerSelectedConfig(t *testing.T) {
	activateMultipathProfile(t)
	selected := multipathConfig(t)
	getConfig := func(*ClientInfo) (*Config, error) { return selected, nil }
	dial := func(t *testing.T, srv *Transport) (*Conn, error) {
		cl := &Transport{Conn: newUDPConnLocalhost(t)}
		t.Cleanup(func() { cl.Close() })
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		return cl.Dial(ctx, srv.Conn.LocalAddr(), nativeClientTLS(), nil)
	}
	t.Run("ungranted transport refuses before advertising", func(t *testing.T) {
		srv := &Transport{Conn: newUDPConnLocalhost(t)}
		defer srv.Close()
		ln, err := srv.Listen(nativeServerTLS(), &Config{GetConfigForClient: getConfig})
		require.NoError(t, err)
		defer ln.Close()
		_, err = dial(t, srv)
		var transportErr *TransportError
		require.ErrorAs(t, err, &transportErr)
		require.Equal(t, ConnectionRefused, transportErr.ErrorCode)
	})
	t.Run("granted transport advertises only the selected config", func(t *testing.T) {
		srv := grantedTransport(t)
		ln, err := srv.Listen(nativeServerTLS(), &Config{GetConfigForClient: getConfig})
		require.NoError(t, err)
		defer ln.Close()
		client, err := dial(t, srv)
		require.NoError(t, err)
		defer client.CloseWithError(0, "")
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		server, err := ln.Accept(ctx)
		require.NoError(t, err)
		require.NotNil(t, server.pathSurface)
		require.NotNil(t, client.ConnectionState())
		<-server.HandshakeComplete()
		negotiated, err := server.MultipathNegotiatedV1()
		require.NoError(t, err)
		require.False(t, negotiated, "the client never offered it")
	})
	t.Run("a late request on an address listener can't retrofit", func(t *testing.T) {
		ln, err := ListenAddr("127.0.0.1:0", nativeServerTLS(), &Config{GetConfigForClient: getConfig})
		require.NoError(t, err)
		defer ln.Close()
		cl := &Transport{Conn: newUDPConnLocalhost(t)}
		defer cl.Close()
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		_, err = cl.Dial(ctx, ln.Addr(), nativeClientTLS(), nil)
		var transportErr *TransportError
		require.ErrorAs(t, err, &transportErr)
		require.Equal(t, ConnectionRefused, transportErr.ErrorCode)
	})
}

func TestMultipathUnsupportedCombinations(t *testing.T) {
	activateMultipathProfile(t)
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9}
	try := func(t *testing.T, tr *Transport, conf *Config) error {
		t.Helper()
		_, err := tr.Dial(t.Context(), addr, nativeClientTLS(), conf)
		return err
	}
	t.Run("v2", func(t *testing.T) {
		for _, versions := range [][]Version{{Version2}, {Version1, Version2}} {
			conf := multipathConfig(t)
			conf.Versions = versions
			require.ErrorIs(t, try(t, grantedTransport(t), conf), ErrMultipathUnsupportedV1)
		}
	})
	t.Run("BBR", func(t *testing.T) {
		conf := multipathConfig(t)
		require.NoError(t, conf.SetCongestionControlV1("bbrv3"))
		require.ErrorIs(t, try(t, grantedTransport(t), conf), ErrMultipathUnsupportedV1)
	})
	t.Run("send-thread QoS", func(t *testing.T) {
		before := &Transport{Conn: newUDPConnLocalhost(t)}
		require.NoError(t, before.ConfigureSendThreadQoSV1())
		require.ErrorIs(t, before.ConfigureMultipathIOV1(time.Second), ErrMultipathUnsupportedV1)
		// The other order: the gate looks again, for the origin and for a target.
		after := grantedTransport(t)
		require.NoError(t, after.ConfigureSendThreadQoSV1())
		require.ErrorIs(t, try(t, after, multipathConfig(t)), ErrMultipathUnsupportedV1)
	})
	t.Run("restricted", func(t *testing.T) {
		tr := grantedTransport(t)
		tr.policyConn.policy = &fixedPeerPolicy{}
		require.ErrorIs(t, try(t, tr, multipathConfig(t)), ErrMultipathUnsupportedV1)
		tr.policyConn.policy = nil
	})
	t.Run("zero-length connection IDs", func(t *testing.T) {
		tr := grantedTransport(t)
		tr.ConnectionIDGenerator = &protocol.DefaultConnectionIDGenerator{ConnLen: 0}
		require.ErrorIs(t, try(t, tr, multipathConfig(t)), ErrMultipathInvalidConfigV1)
	})
}

func TestMultipathPeerZeroLengthCID(t *testing.T) {
	f := newAdmissionFixtureUnactivated(t)
	f.c.config.multipath = multipathSelection{enabled: true, maxPaths: 2}
	f.c.advertiseMultipath(&wire.TransportParameters{})
	maximum := protocol.PathID(1)
	f.c.handshakeDestConnID = protocol.ConnectionID{}
	err := f.c.checkTransportParameters(&wire.TransportParameters{InitialMaxPathID: &maximum})
	require.ErrorContains(t, err, "zero-length")
	require.NotContains(t, errString(f.c.checkTransportParameters(&wire.TransportParameters{})), "zero-length", "an ordinary peer with the same CID is fine")

	// A server never advertises to that client, but the client's offer is still an error.
	server := newAdmissionFixture(t, protocol.PerspectiveServer)
	server.c.pathCredit, server.c.pathSurface = nil, nil
	server.c.config.multipath = multipathSelection{enabled: true, maxPaths: 2}
	server.c.handshakeDestConnID = protocol.ConnectionID{}
	params := &wire.TransportParameters{}
	server.c.advertiseMultipath(params)
	require.Nil(t, params.InitialMaxPathID)
	require.Nil(t, server.c.pathSurface)
	require.ErrorContains(t, server.c.checkTransportParameters(&wire.TransportParameters{InitialMaxPathID: &maximum}), "zero-length")
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestMultipathOrdinaryPeerFallback(t *testing.T) {
	activateMultipathProfile(t)
	srv := &Transport{Conn: newUDPConnLocalhost(t)}
	defer srv.Close()
	ln, err := srv.Listen(nativeServerTLS(), nil)
	require.NoError(t, err)
	defer ln.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	client, err := grantedTransport(t).Dial(ctx, srv.Conn.LocalAddr(), nativeClientTLS(), multipathConfig(t))
	require.NoError(t, err)
	defer client.CloseWithError(0, "")
	negotiated, err := client.MultipathNegotiatedV1()
	require.NoError(t, err)
	require.False(t, negotiated)
	require.Empty(t, client.PathsV1())
	_, err = client.OpenPathV1(ctx, grantedTransport(t), srv.Conn.LocalAddr().(*net.UDPAddr))
	require.ErrorIs(t, err, ErrMultipathNotNegotiatedV1)
	require.ErrorIs(t, client.WaitTransportDetachedV1(ctx, srv), ErrMultipathNotNegotiatedV1)
	_, err = client.AddPath(grantedTransport(t))
	require.NoError(t, err, "legacy migration stays available once the result is known")

	t.Run("a zero-length client CID sees no advertisement", func(t *testing.T) {
		enabled := grantedTransport(t)
		ln, err := enabled.Listen(nativeServerTLS(), multipathConfig(t))
		require.NoError(t, err)
		defer ln.Close()
		ordinary, err := Dial(ctx, newUDPConnLocalhost(t), enabled.Conn.LocalAddr(), nativeClientTLS(), nil)
		require.NoError(t, err)
		defer ordinary.CloseWithError(0, "")
		require.Zero(t, ordinary.srcConnIDLen)
		require.Nil(t, ordinary.peerParams.InitialMaxPathID)
		server, err := ln.Accept(ctx)
		require.NoError(t, err)
		require.Nil(t, server.pathSurface)
	})

	t.Run("a v2 client of an enabled server stays ordinary", func(t *testing.T) {
		enabled := grantedTransport(t)
		ln, err := enabled.Listen(nativeServerTLS(), multipathConfig(t))
		require.NoError(t, err)
		defer ln.Close()
		cl := &Transport{Conn: newUDPConnLocalhost(t)}
		defer cl.Close()
		v2, err := cl.Dial(ctx, enabled.Conn.LocalAddr(), nativeClientTLS(), &Config{Versions: []Version{Version2}})
		require.NoError(t, err)
		defer v2.CloseWithError(0, "")
		server, err := ln.Accept(ctx)
		require.NoError(t, err)
		require.Nil(t, server.pathSurface)
		negotiated, err := server.MultipathNegotiatedV1()
		require.NoError(t, err)
		require.False(t, negotiated)
	})
}

func TestMultipathLegacyModes(t *testing.T) {
	t.Run("pending and disabled", func(t *testing.T) {
		f := newAdmissionFixtureUnactivated(t)
		f.c.config.multipath = multipathSelection{enabled: true, maxPaths: 2}
		f.c.advertiseMultipath(&wire.TransportParameters{})
		_, err := f.c.MultipathNegotiatedV1()
		require.ErrorIs(t, err, ErrMultipathHandshakeIncompleteV1)
		_, err = f.c.AddPath(&Transport{Conn: newUDPConnLocalhost(t)})
		require.ErrorIs(t, err, ErrMultipathHandshakeIncompleteV1)
		_, err = f.c.OpenPathV1(t.Context(), nil, &net.UDPAddr{})
		require.ErrorIs(t, err, ErrMultipathHandshakeIncompleteV1)
		require.ErrorIs(t, f.c.WaitTransportDetachedV1(t.Context(), &Transport{}), ErrMultipathHandshakeIncompleteV1)
		require.Empty(t, f.c.PathsV1())
		require.ErrorIs(t, f.c.WaitTransportDetachedV1(t.Context(), nil), ErrMultipathInvalidConfigV1)

		disabled := newAdmissionFixtureUnactivated(t)
		negotiated, err := disabled.c.MultipathNegotiatedV1()
		require.NoError(t, err)
		require.False(t, negotiated)
		select {
		case <-disabled.c.PathChangesV1():
			t.Fatal("a disabled connection has no path changes")
		default:
		}
	})

	p := newMultipathPair(t)
	_, err := p.client.AddPath(&Transport{Conn: newUDPConnLocalhost(t)})
	require.ErrorIs(t, err, ErrMultipathWrongModeV1)
	zero := p.client.PathsV1()[0]
	require.ErrorIs(t, zero.Probe(t.Context()), ErrMultipathWrongModeV1)
	require.ErrorIs(t, zero.Switch(), ErrMultipathWrongModeV1)
	require.ErrorIs(t, zero.Close(), ErrMultipathWrongModeV1)

	ordinary := &Path{}
	_, err = ordinary.PathIDV1()
	require.ErrorIs(t, err, ErrMultipathWrongModeV1)
	_, err = ordinary.StateV1()
	require.ErrorIs(t, err, ErrMultipathWrongModeV1)
	require.ErrorIs(t, ordinary.MigrateV1(t.Context(), nil), ErrMultipathWrongModeV1)
	require.ErrorIs(t, ordinary.SetLocalUseV1("backup"), ErrMultipathWrongModeV1)
	require.ErrorIs(t, ordinary.AdvertiseStatusV1("backup"), ErrMultipathWrongModeV1)
	require.ErrorIs(t, ordinary.AbandonV1(0), ErrMultipathWrongModeV1)
}

func TestMultipathOpenSnapshotsAndAbandon(t *testing.T) {
	p := newMultipathPair(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	zero := p.client.PathsV1()[0]
	require.ErrorIs(t, zero.AbandonV1(0), ErrMultipathLastPathV1)

	second := grantedTransport(t)
	path, err := p.client.OpenPathV1(ctx, second, p.serverAddr)
	require.NoError(t, err)
	id, err := path.PathIDV1()
	require.NoError(t, err)
	require.Equal(t, uint32(1), id)
	state, err := path.StateV1()
	require.NoError(t, err)
	require.Equal(t, "live", state.Lifecycle, "the state is current when OpenPathV1 returns")
	require.Equal(t, "backup", state.LocalUse, "a validated alternative starts as backup")
	require.False(t, state.Primary)
	require.True(t, state.Validated)
	require.Equal(t, second.Conn.LocalAddr().(*net.UDPAddr).Port, int(state.Local.Port()))

	// Coalescing: several changes leave one signal, and a new snapshot sees them all.
	select {
	case <-p.client.PathChangesV1():
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.NoError(t, path.SetLocalUseV1("active"))
	require.NoError(t, zero.SetLocalUseV1("backup"))
	require.Equal(t, 1, len(p.client.PathChangesV1()))
	state, err = path.StateV1()
	require.NoError(t, err)
	require.True(t, state.Primary)
	require.Equal(t, "active", state.LocalUse)
	copied := p.client.PathsV1()
	copied[0] = nil
	require.NotNil(t, p.client.PathsV1()[0], "a snapshot is the caller's copy")
	require.Same(t, path, p.client.PathsV1()[1], "a wire ID keeps its one handle")
	require.ErrorIs(t, path.SetLocalUseV1("sideways"), ErrMultipathInvalidConfigV1)
	require.ErrorIs(t, path.AdvertiseStatusV1("sideways"), ErrMultipathInvalidConfigV1)

	// Peer-created handle: status and abandon are allowed, a local move isn't.
	var peer *Path
	require.Eventually(t, func() bool {
		paths := p.server.PathsV1()
		if len(paths) != 2 {
			return false
		}
		peer = paths[1]
		state, err := peer.StateV1()
		require.NoError(t, err)
		return state.Lifecycle == "live"
	}, 5*time.Second, time.Millisecond)
	require.NoError(t, peer.AdvertiseStatusV1("backup"))
	waitPathState(t, path, func(s MultipathPathStateV1) bool { return s.PeerPreference == "backup" })
	require.NoError(t, peer.AdvertiseStatusV1("available"))
	waitPathState(t, path, func(s MultipathPathStateV1) bool { return s.PeerPreference == "available" })
	require.ErrorIs(t, peer.MigrateV1(ctx, grantedTransport(t)), ErrMultipathUnsupportedV1)
	_, err = p.server.OpenPathV1(ctx, grantedTransport(t), p.client.RemoteAddr().(*net.UDPAddr))
	require.ErrorIs(t, err, ErrMultipathUnsupportedV1)

	// Abandon: accepted at once, idempotent, and the handle stays safe.
	require.ErrorIs(t, zero.AbandonV1(1<<62), ErrMultipathInvalidConfigV1)
	require.NoError(t, zero.SetLocalUseV1("active"))
	require.NoError(t, path.AbandonV1(7))
	require.NoError(t, path.AbandonV1(7))
	state, err = path.StateV1()
	require.NoError(t, err)
	require.Contains(t, []string{"retiring", "retained"}, state.Lifecycle)
	require.False(t, state.Primary)
	require.ErrorIs(t, path.SetLocalUseV1("backup"), ErrPathClosed)
	require.ErrorIs(t, path.AdvertiseStatusV1("backup"), ErrPathClosed)
	require.ErrorIs(t, path.MigrateV1(ctx, grantedTransport(t)), ErrPathClosed)
	id, err = path.PathIDV1()
	require.NoError(t, err)
	require.Equal(t, uint32(1), id, "the retained stable ID")
	require.ErrorIs(t, zero.AbandonV1(0), ErrMultipathLastPathV1, "path one no longer survives it")

	require.NoError(t, p.client.CloseWithError(0, ""))
	final := waitPathState(t, zero, func(s MultipathPathStateV1) bool { return s.Lifecycle == "closed" })
	require.Equal(t, "accepted", final.LastOutcome)
	require.NotZero(t, final.RTT, "the last observation stays")
}

// An incompatible target refuses on the caller's goroutine: the connection stays healthy,
// no ID is consumed and no route exists. This is R9's deferred target question.
func TestMultipathOpenRefusesTargetUnharmed(t *testing.T) {
	p := newMultipathPair(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, err := p.client.OpenPathV1(ctx, nil, p.serverAddr)
	require.ErrorIs(t, err, ErrMultipathInvalidConfigV1)
	_, err = p.client.OpenPathV1(ctx, grantedTransport(t), nil)
	require.ErrorIs(t, err, ErrMultipathInvalidConfigV1)
	for _, remote := range []*net.UDPAddr{{}, {IP: net.IPv4(127, 0, 0, 1)}, {IP: net.IPv4zero, Port: 443}} {
		_, err = p.client.OpenPathV1(ctx, grantedTransport(t), remote)
		require.ErrorIs(t, err, ErrMultipathInvalidConfigV1, "%v", remote)
	}
	ungranted := &Transport{Conn: newUDPConnLocalhost(t)}
	_, err = p.client.OpenPathV1(ctx, ungranted, p.serverAddr)
	require.ErrorIs(t, err, ErrMultipathNotConfiguredV1)
	require.ErrorIs(t, p.client.PathsV1()[0].MigrateV1(ctx, ungranted), ErrMultipathNotConfiguredV1)
	mismatched := &Transport{Conn: newUDPConnLocalhost(t), ConnectionIDLength: 8}
	require.NoError(t, mismatched.ConfigureMultipathIOV1(time.Second))
	defer mismatched.Close()
	_, err = p.client.OpenPathV1(ctx, mismatched, p.serverAddr)
	require.ErrorIs(t, err, ErrMultipathInvalidConfigV1)
	conflicted := grantedTransport(t)
	require.NoError(t, conflicted.ConfigureSendThreadQoSV1())
	_, err = p.client.OpenPathV1(ctx, conflicted, p.serverAddr)
	require.ErrorIs(t, err, ErrMultipathUnsupportedV1)
	require.Nil(t, mismatched.handlers, "the refused target never started")
	require.NoError(t, p.client.Context().Err())
	require.Len(t, p.client.PathsV1(), 1)

	remote := &net.UDPAddr{IP: slices.Clone(p.serverAddr.IP), Port: p.serverAddr.Port}
	path, err := p.client.OpenPathV1(ctx, grantedTransport(t), remote)
	require.NoError(t, err)
	id, err := path.PathIDV1()
	require.NoError(t, err)
	require.Equal(t, uint32(1), id, "the refusals consumed no ID")
	state, err := path.StateV1()
	require.NoError(t, err)
	want := state.Remote
	remote.IP[len(remote.IP)-1]++
	remote.Port++
	require.NoError(t, path.SetLocalUseV1("backup"))
	state, err = path.StateV1()
	require.NoError(t, err)
	require.Equal(t, want, state.Remote, "the connection holds its own copy of the address")
}

// What ended a connection stays visible: a pending operation reports the terminal error
// with its identity, and the final observation of an endpoint fault says so, though the
// sender's route check assumes the connection's own endpoint is healthy (R20 changes that).
func TestMultipathTerminalObservations(t *testing.T) {
	t.Run("pending open", func(t *testing.T) {
		p := newMultipathPair(t)
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		silent := newUDPConnLocalhost(t)
		pending := make(chan error, 1)
		go func() {
			_, err := p.client.OpenPathV1(ctx, grantedTransport(t), silent.LocalAddr().(*net.UDPAddr))
			pending <- err
		}()
		require.Eventually(t, func() bool { return len(p.client.PathsV1()) == 2 }, 5*time.Second, time.Millisecond)
		require.NoError(t, p.server.CloseWithError(42, "bye"))
		var appErr *ApplicationError
		require.ErrorAs(t, <-pending, &appErr)
		require.Equal(t, ApplicationErrorCode(42), appErr.ErrorCode)
		require.True(t, appErr.Remote)
	})
	t.Run("endpoint fault", func(t *testing.T) {
		p := newMultipathPair(t)
		zero := p.client.PathsV1()[0]
		nativeTransportEndpoint(p.clientTr).do(func(s *endpointState) { s.fail(endpointFaultOverdue, errors.New("injected")) })
		str, err := p.client.OpenUniStream()
		require.NoError(t, err)
		_, _ = str.Write([]byte("local work finds the fault"))
		final := waitPathState(t, zero, func(s MultipathPathStateV1) bool { return s.Lifecycle == "closed" })
		require.Equal(t, "endpoint-fault", final.LastOutcome)
		require.False(t, final.Primary)
	})
}

// A session ticket doesn't record multipath, so resumption must not issue the ordinary
// CID window before activation learns the peer's offer.
func TestMultipathZeroRTTResumption(t *testing.T) {
	activateMultipathProfile(t)
	srv := grantedTransport(t)
	serverConf := multipathConfig(t)
	serverConf.Allow0RTT = true
	ln, err := srv.ListenEarly(nativeServerTLS(), serverConf)
	require.NoError(t, err)
	defer ln.Close()
	tlsConf := nativeClientTLS()
	tickets := &ticketWaitCache{ClientSessionCache: tls.NewLRUClientSessionCache(1), put: make(chan struct{}, 4)}
	tlsConf.ClientSessionCache = tickets
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	first, err := grantedTransport(t).Dial(ctx, srv.Conn.LocalAddr(), tlsConf, multipathConfig(t))
	require.NoError(t, err)
	select {
	case <-tickets.put:
	case <-ctx.Done():
		t.Fatal("no session ticket")
	}
	require.NoError(t, first.CloseWithError(0, ""))

	resumed, err := grantedTransport(t).DialEarly(ctx, srv.Conn.LocalAddr(), tlsConf, multipathConfig(t))
	require.NoError(t, err)
	defer resumed.CloseWithError(0, "")
	select {
	case <-resumed.HandshakeComplete():
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.True(t, resumed.ConnectionState().Used0RTT)
	negotiated, err := resumed.MultipathNegotiatedV1()
	require.NoError(t, err)
	require.True(t, negotiated)
	require.NoError(t, resumed.Context().Err())
}

type ticketWaitCache struct {
	tls.ClientSessionCache
	put chan struct{}
}

func (c *ticketWaitCache) Put(key string, state *tls.ClientSessionState) {
	c.ClientSessionCache.Put(key, state)
	if state != nil {
		c.put <- struct{}{}
	}
}

func TestMultipathMigrateAndDetach(t *testing.T) {
	p := newMultipathPair(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	second := grantedTransport(t)
	path, err := p.client.OpenPathV1(ctx, second, p.serverAddr)
	require.NoError(t, err)
	require.NoError(t, path.SetLocalUseV1("active"))
	before, err := path.StateV1()
	require.NoError(t, err)

	third := grantedTransport(t)
	stranger := grantedTransport(t)
	require.NoError(t, p.client.WaitTransportDetachedV1(ctx, stranger), "never attached")
	shortCtx, shortCancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer shortCancel()
	require.ErrorIs(t, p.client.WaitTransportDetachedV1(shortCtx, second), context.DeadlineExceeded, "still in use")

	require.NoError(t, path.MigrateV1(ctx, third))
	after, err := path.StateV1()
	require.NoError(t, err)
	require.Equal(t, before.WireID, after.WireID)
	require.Greater(t, after.TupleGeneration, before.TupleGeneration)
	require.Equal(t, third.Conn.LocalAddr().(*net.UDPAddr).Port, int(after.Local.Port()))
	require.Equal(t, before.Remote, after.Remote)
	require.NoError(t, p.client.WaitTransportDetachedV1(ctx, second), "routing retention ended")
	require.NoError(t, second.Close(), "safe once detached")
	require.NoError(t, p.client.Context().Err(), "the connection survives")
}

// Every identity is distinct, and a backpressure cause matches only its own set.
func TestMultipathErrorIdentities(t *testing.T) {
	all := []error{
		ErrMultipathInvalidConfigV1, ErrMultipathHandshakeIncompleteV1, ErrMultipathNotNegotiatedV1,
		ErrMultipathWrongModeV1, ErrMultipathUnsupportedV1, ErrMultipathBackpressureV1, ErrMultipathDeadlineV1,
		ErrMultipathUncertainV1, ErrMultipathResourceLimitV1, ErrMultipathLastPathV1,
		ErrMultipathNotConfiguredV1, ErrMultipathNotStoppingV1,
	}
	for i, a := range all {
		for j, b := range all {
			require.Equal(t, i == j, errors.Is(a, b), "%v vs %v", a, b)
		}
	}
	// TestEndpointResponseNativeResultClasses owns the capacity, timeout and uncertain sets.
}

// cap-plus-one: multipath setup takes the same refusal as an ordinary connection, on the
// one participant owner.
func TestMultipathSetupParticipantCap(t *testing.T) {
	activateMultipathProfile(t)
	tr := grantedTransport(t)
	require.NoError(t, tr.beginPacketIO())
	endpoint := nativeTransportEndpoint(tr)
	for id := range uint64(endpointParticipants) {
		_, err := endpoint.attach(id + 1)
		require.NoError(t, err)
	}
	_, err := tr.Dial(t.Context(), nativeShutdownAddress, nativeClientTLS(), multipathConfig(t))
	require.ErrorIs(t, err, ErrMultipathResourceLimitV1)
}
