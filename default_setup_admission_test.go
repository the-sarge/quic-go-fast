package quic

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/internal/testdata"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/testutils/events"

	"github.com/stretchr/testify/require"
)

func TestDefaultSetupAdmissionLimits(t *testing.T) {
	t.Run("process capacity and Retry", func(t *testing.T) {
		b := &serverSetupBudget{maxTotal: defaultMaxPendingHandshakes, maxPerIP: defaultMaxPendingHandshakesPerIP}
		var held []*setupReservation
		t.Cleanup(func() {
			for _, r := range held {
				r.teardown()
			}
			require.Zero(t, b.total)
			require.Empty(t, b.byIP)
		})
		for i := range defaultMaxPendingHandshakes {
			remote := net.UDPAddrFromAddrPort(netip.AddrPortFrom(netip.AddrFrom4([4]byte{192, 0, byte(i >> 8), byte(i)}), 1234))
			if i >= defaultMaxPendingHandshakes/2 {
				r, retry, ok := b.acquire(remote, false)
				require.Nil(t, r)
				require.True(t, retry)
				require.False(t, ok)
			}
			r, retry, ok := b.acquire(remote, i >= defaultMaxPendingHandshakes/2)
			require.True(t, ok)
			require.False(t, retry)
			held = append(held, r)
		}
		r, retry, ok := b.acquire(&net.UDPAddr{IP: net.IPv4(198, 51, 100, 1), Port: 5678}, true)
		require.Nil(t, r)
		require.False(t, retry)
		require.False(t, ok, "even a validated source cannot exceed the process limit")
		require.Equal(t, defaultMaxPendingHandshakes, b.total)
	})
	t.Run("IP capacity ignores ports and address aliases", func(t *testing.T) {
		for _, addresses := range [][]string{
			{"192.0.2.1:1234", "[::ffff:192.0.2.1]:5678"},
			{"[fe80::1%en0]:1234", "[fe80::1%en1]:5678"},
		} {
			b := &serverSetupBudget{maxTotal: defaultMaxPendingHandshakes, maxPerIP: defaultMaxPendingHandshakesPerIP}
			var held []*setupReservation
			for i := range defaultMaxPendingHandshakesPerIP {
				remote := net.UDPAddrFromAddrPort(netip.MustParseAddrPort(addresses[i%len(addresses)]))
				if i >= defaultMaxPendingHandshakesPerIP/2 {
					_, retry, ok := b.acquire(remote, false)
					require.True(t, retry)
					require.False(t, ok)
				}
				r, retry, ok := b.acquire(remote, true)
				require.True(t, ok)
				require.False(t, retry)
				held = append(held, r)
			}
			_, retry, ok := b.acquire(net.UDPAddrFromAddrPort(netip.MustParseAddrPort(addresses[0])), true)
			require.False(t, retry)
			require.False(t, ok)
			require.Len(t, b.byIP, 1)
			for _, r := range held {
				r.teardown()
			}
			require.Empty(t, b.byIP)
		}
	})
}

func TestDefaultSetupAdmissionConcurrent(t *testing.T) {
	b := &serverSetupBudget{maxTotal: 32, maxPerIP: 8}
	reservations := make(chan *setupReservation, 256)
	var workers sync.WaitGroup
	for i := range 256 {
		workers.Go(func() {
			remote := &net.UDPAddr{IP: net.IPv4(192, 0, 2, byte(i%8)), Port: 1234 + i}
			if r, _, ok := b.acquire(remote, true); ok {
				reservations <- r
			}
		})
	}
	workers.Wait()
	close(reservations)
	require.Equal(t, 32, b.total)
	for _, count := range b.byIP {
		require.LessOrEqual(t, count, 8)
	}
	for r := range reservations {
		workers.Go(func() { r.teardown(); r.teardown() })
	}
	workers.Wait()
	require.Zero(t, b.total)
	require.Empty(t, b.byIP)
}

func TestDefaultSetupAdmissionLifetime(t *testing.T) {
	b := &serverSetupBudget{maxTotal: 2, maxPerIP: 2}
	r, _, ok := b.acquire(nil, true)
	require.True(t, ok)
	c := &Conn{}
	require.True(t, r.bind(c))
	// ListenEarly's completion and acceptance happen before TLS completion.
	require.True(t, c.completeSetup())
	c.claimSetup()
	require.ErrorIs(t, c.TransferSetupReservationV1(), errNoSetupReservation)
	require.Equal(t, 1, b.total)
	r.handshakeFinished()
	require.Zero(t, b.total)
	r.teardown()
	require.Zero(t, b.total)

	r, _, ok = b.acquire(nil, true)
	require.True(t, ok)
	retired := r.holdClosing()
	r.teardown()
	require.Equal(t, 1, b.total, "failed handshakes retain capacity through closing-state retirement")
	retired()
	require.Zero(t, b.total)
	require.Empty(t, b.byIP)
}

// These are real connections with undecryptable Initials, kept alive by their
// handshake timers. Two listeners must share the same per-IP pending budget.
func TestDefaultSetupAdmissionInitialFlood(t *testing.T) {
	constructed := countServerConstructors(t)
	first := newTestServer(t, &serverOpts{newConn: newConnection, eventRecorder: &events.Recorder{}})
	second := newTestServer(t, &serverOpts{newConn: newConnection, eventRecorder: &events.Recorder{}})
	first.statelessResetter = newStatelessResetter(nil)
	second.statelessResetter = newStatelessResetter(nil)
	client := newUDPConnLocalhost(t)
	for i := range defaultMaxPendingHandshakesPerIP * 2 {
		id := randConnID(8)
		s := first
		if i%2 == 0 {
			s = second
		}
		p := getValidInitialPacket(t, client.LocalAddr(), randConnID(8), id)
		if !s.handlePacketImpl(p) {
			p.buffer.Release()
		}
	}
	require.EqualValues(t, defaultMaxPendingHandshakesPerIP/2, constructed.Load())
	ip := endpoint(t, client.LocalAddr()).Addr()
	defaultServerSetupBudget.mu.Lock()
	count := defaultServerSetupBudget.byIP[ip]
	defaultServerSetupBudget.mu.Unlock()
	require.Equal(t, defaultMaxPendingHandshakesPerIP/2, count)
	// Retry is sent without constructing another connection.
	require.NoError(t, client.SetReadDeadline(time.Now().Add(time.Second)))
	buf := make([]byte, 1500)
	n, _, err := client.ReadFrom(buf)
	require.NoError(t, err)
	hdr, _, _, err := wire.ParsePacket(buf[:n])
	require.NoError(t, err)
	require.Equal(t, protocol.PacketTypeRetry, hdr.Type)
	// Join connection teardown before later tests reuse the process-wide budget.
	require.NoError(t, (*Transport)(first.tr).Close())
	require.NoError(t, (*Transport)(second.tr).Close())
}

func TestDefaultSetupAdmissionHandshakeCompletion(t *testing.T) {
	// Half-full per-IP capacity forces the real client through Retry. Its
	// validated Initial can still complete, and returns only its own slot.
	for range defaultMaxPendingHandshakesPerIP / 2 {
		r, _, ok := defaultServerSetupBudget.acquire(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}, true)
		require.True(t, ok)
		t.Cleanup(r.teardown)
	}
	tr := &Transport{Conn: newUDPConnLocalhost(t)}
	t.Cleanup(func() { tr.Close() })
	validated := make(chan bool, 1)
	ln, err := tr.Listen(testdata.GetTLSConfig(), &Config{GetConfigForClient: func(info *ClientInfo) (*Config, error) {
		validated <- info.AddrVerified
		return nil, nil
	}})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = dialSetup(t, ctx, ln)
	require.NoError(t, err)
	server, err := ln.Accept(ctx)
	require.NoError(t, err)
	require.NotNil(t, server.setup)
	server.setup.mu.Lock()
	settled := server.setup.settled
	server.setup.mu.Unlock()
	require.True(t, settled, "TLS completion returns the default reservation without application transfer")
	require.True(t, <-validated, "the pressured listener requires Retry with a valid token")
	require.ErrorIs(t, server.TransferSetupReservationV1(), errNoSetupReservation)
}

func TestDefaultSetupAdmissionValidatedInitialRefused(t *testing.T) {
	// Fill the shared process budget without allocating connection objects.
	var held []*setupReservation
	t.Cleanup(func() {
		for _, r := range held {
			r.teardown()
		}
	})
	for i := range defaultMaxPendingHandshakes {
		remote := &net.UDPAddr{IP: net.IPv4(192, 0, byte(i>>8), byte(i)), Port: 1234}
		r, _, ok := defaultServerSetupBudget.acquire(remote, true)
		if !ok {
			break
		}
		held = append(held, r)
	}
	var callbacks atomic.Int32
	server := newRefusalServer(t, &serverOpts{config: &Config{GetConfigForClient: func(*ClientInfo) (*Config, error) {
		callbacks.Add(1)
		return nil, errors.New("unexpected callback")
	}}})
	destID := randConnID(8)
	token, err := server.tokenGenerator.NewRetryToken(server.conn.LocalAddr(), randConnID(8), destID)
	require.NoError(t, err)
	p := getLongHeaderPacket(t, server.conn.LocalAddr(), &wire.ExtendedHeader{
		Header: wire.Header{
			Type: protocol.PacketTypeInitial, Version: protocol.Version1,
			SrcConnectionID: server.srcID, DestConnectionID: destID,
			Token: token, Length: protocol.MinInitialPacketSize,
		},
		PacketNumberLen: protocol.PacketNumberLen4,
	}, make([]byte, protocol.MinInitialPacketSize))
	server.handlePacket(p)
	checkConnectionClose(t, server.conn, server.recorder, destID, server.srcID, qerr.ConnectionRefused)
	require.Zero(t, callbacks.Load(), "capacity is checked before application callbacks")
	require.Zero(t, server.constructed.Load(), "a valid Retry token cannot bypass the global limit")
}
