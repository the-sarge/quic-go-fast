package quic

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/testdata"
	"github.com/quic-go/quic-go/testutils/simnet"

	"github.com/stretchr/testify/require"
)

// The schedules are #758's: 10 ms one way, links that carry 1452 bytes, and a router that
// drops datagrams over 1250 bytes, so 1280-byte packets are lost and 1200-byte packets pass.
const (
	silentDropOneWay  = 10 * time.Millisecond
	silentDropLinkMTU = 1452
	silentDropLimit   = 1250
)

// The synctest clock starts in 2000, before the test certificate's validity.
var silentDropTLSTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

type silentDropDatagram struct {
	at       time.Time
	toServer bool
	size     int
	dropped  bool
}

// silentDropRouter decides delivery for every datagram and records it.
type silentDropRouter struct {
	simnet.PerfectRouter
	server string
	drop   func(toServer bool, size int, now time.Time) bool

	mx        sync.Mutex
	datagrams []silentDropDatagram
}

func (r *silentDropRouter) SendPacket(p simnet.Packet) error {
	d := silentDropDatagram{at: time.Now(), toServer: p.To.String() == r.server, size: len(p.Data)}
	d.dropped = r.drop(d.toServer, d.size, d.at)
	r.mx.Lock()
	r.datagrams = append(r.datagrams, d)
	r.mx.Unlock()
	if d.dropped {
		return nil
	}
	return r.PerfectRouter.SendPacket(p)
}

// largest returns the largest datagram each endpoint sent since t.
func (r *silentDropRouter) largest(t time.Time) (client, server int) {
	r.mx.Lock()
	defer r.mx.Unlock()
	for _, d := range r.datagrams {
		if d.at.Before(t) {
			continue
		}
		if d.toServer {
			client = max(client, d.size)
		} else {
			server = max(server, d.size)
		}
	}
	return client, server
}

// clientFlightsLost counts the instants at which client datagrams were dropped. Each
// flight, such as one PTO's probes, is sent at a single instant of virtual time.
func (r *silentDropRouter) clientFlightsLost() int {
	r.mx.Lock()
	defer r.mx.Unlock()
	instants := make(map[time.Time]struct{})
	for _, d := range r.datagrams {
		if d.toServer && d.dropped {
			instants[d.at] = struct{}{}
		}
	}
	return len(instants)
}

func (r *silentDropRouter) clientDelivered() int {
	r.mx.Lock()
	defer r.mx.Unlock()
	var n int
	for _, d := range r.datagrams {
		if d.toServer && !d.dropped {
			n++
		}
	}
	return n
}

type silentDropPolicy func(start time.Time) func(toServer bool, size int, now time.Time) bool

func dropOversized(toServer, toClient bool) silentDropPolicy {
	return func(time.Time) func(bool, int, time.Time) bool {
		return func(dirToServer bool, size int, _ time.Time) bool {
			return size > silentDropLimit && ((dirToServer && toServer) || (!dirToServer && toClient))
		}
	}
}

// dropClientUntil loses every client datagram sent before start+d. With the default
// initial RTT, the client's first PTO fires at 200 ms and its second at 600 ms.
func dropClientUntil(d time.Duration) silentDropPolicy {
	return func(start time.Time) func(bool, int, time.Time) bool {
		return func(toServer bool, _ int, now time.Time) bool { return toServer && now.Before(start.Add(d)) }
	}
}

func deliverAllDatagrams(time.Time) func(bool, int, time.Time) bool {
	return func(bool, int, time.Time) bool { return false }
}

// TestHandshakeSilentDrop pins the unset-size fallback at the second Initial or Handshake
// PTO before completion (#786): it recovers silent drops of oversized datagrams, and its
// false-positive profile is the one #759 measured. Sizes are each endpoint's largest
// datagram in a transfer after confirmation, so they show the size it kept.
func TestHandshakeSilentDrop(t *testing.T) {
	for _, tc := range []struct {
		name              string
		oneWay            time.Duration
		policy            silentDropPolicy
		clientFlightsLost int
		client, server    int
	}{
		// The client's second PTO probes already go out at 1200 bytes, so only two flights are lost.
		{"client direction", silentDropOneWay, dropOversized(true, false), 2, 1200, 1280},
		// The client's reduction is collateral: its own direction was never limited (#759's known limit).
		{"server direction", silentDropOneWay, dropOversized(false, true), 0, 1200, 1200},
		{"both directions", silentDropOneWay, dropOversized(true, true), 2, 1200, 1200},
		{"one lost round", silentDropOneWay, dropClientUntil(100 * time.Millisecond), 1, 1280, 1280},
		// Two lost rounds are indistinguishable from a size limit: the documented false positive.
		{"two lost rounds", silentDropOneWay, dropClientUntil(400 * time.Millisecond), 2, 1200, 1280},
		// The client's spurious Handshake PTO at 500 ms comes after completion at 300 ms.
		{"lossless 300 ms RTT", 150 * time.Millisecond, deliverAllDatagrams, 0, 1280, 1280},
		{"healthy", silentDropOneWay, deliverAllDatagrams, 0, 1280, 1280},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				router, clientTr, serverTr := newSilentDropNetwork(t, tc.oneWay, tc.policy)
				serverTLS, clientTLS := silentDropTLSConfigs()
				ln, err := serverTr.Listen(serverTLS, &Config{})
				require.NoError(t, err)
				t.Cleanup(func() { ln.Close() })

				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				conn, err := clientTr.Dial(ctx, ln.Addr(), clientTLS, &Config{})
				require.NoError(t, err)
				server, err := ln.Accept(ctx)
				require.NoError(t, err)
				require.Equal(t, tc.clientFlightsLost, router.clientFlightsLost())

				// Reductions only happen before completion; wait past confirmation.
				time.Sleep(time.Second)
				transferStart := time.Now()
				silentDropEcho(t, conn, server)
				client, srv := router.largest(transferStart)
				require.Equal(t, tc.client, client, "client packet size")
				require.Equal(t, tc.server, srv, "server packet size")

				require.NoError(t, conn.CloseWithError(0, ""))
				<-server.Context().Done()
			})
		})
	}

	// An explicit size is a floor: the handshake keeps #756's documented timeout, with no early close.
	t.Run("explicit 1280, client direction", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			router, clientTr, serverTr := newSilentDropNetwork(t, silentDropOneWay, dropOversized(true, false))
			serverTLS, clientTLS := silentDropTLSConfigs()
			ln, err := serverTr.Listen(serverTLS, &Config{InitialPacketSize: 1280})
			require.NoError(t, err)
			t.Cleanup(func() { ln.Close() })

			start := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_, err = clientTr.Dial(ctx, ln.Addr(), clientTLS, &Config{InitialPacketSize: 1280})
			var idleErr *IdleTimeoutError
			require.ErrorAs(t, err, &idleErr)
			require.GreaterOrEqual(t, time.Since(start), protocol.DefaultHandshakeIdleTimeout)
			require.Zero(t, router.clientDelivered(), "every client datagram stays at 1280 bytes")
		})
	})
}

func newSilentDropNetwork(t *testing.T, oneWay time.Duration, policy silentDropPolicy) (_ *silentDropRouter, client, server *Transport) {
	t.Helper()
	clientAddr := &net.UDPAddr{IP: net.ParseIP("1.0.0.1"), Port: 9001}
	serverAddr := &net.UDPAddr{IP: net.ParseIP("1.0.0.2"), Port: 9002}
	router := &silentDropRouter{server: serverAddr.String(), drop: policy(time.Now())}
	n := &simnet.Simnet{Router: router}
	settings := simnet.NodeBiDiLinkSettings{
		Uplink:   simnet.LinkSettings{MTU: silentDropLinkMTU},
		Downlink: simnet.LinkSettings{MTU: silentDropLinkMTU},
		Latency:  oneWay,
	}
	clientConn := n.NewEndpoint(clientAddr, settings)
	serverConn := n.NewEndpoint(serverAddr, settings)
	require.NoError(t, n.Start())
	t.Cleanup(func() { n.Close() })
	t.Cleanup(func() { clientConn.Close() })
	t.Cleanup(func() { serverConn.Close() })
	client = &Transport{Conn: clientConn}
	t.Cleanup(func() { client.Close() })
	server = &Transport{Conn: serverConn}
	t.Cleanup(func() { server.Close() })
	return router, client, server
}

func silentDropTLSConfigs() (server, client *tls.Config) {
	server = testdata.GetTLSConfig()
	server.Time = func() time.Time { return silentDropTLSTime }
	client = &tls.Config{
		RootCAs:    testdata.GetRootCA(),
		ServerName: "localhost",
		NextProtos: server.NextProtos,
		Time:       func() time.Time { return silentDropTLSTime },
	}
	return server, client
}

// silentDropEcho sends 4096 bytes each way, enough for full-size packets in both directions.
func silentDropEcho(t *testing.T, client, server *Conn) {
	t.Helper()
	payload := make([]byte, 4096)
	for i := range payload {
		payload[i] = byte(i)
	}
	echoErr := make(chan error, 1)
	go func() {
		echoErr <- func() error {
			str, err := server.AcceptStream(context.Background())
			if err != nil {
				return err
			}
			data, err := io.ReadAll(str)
			if err != nil {
				return err
			}
			if _, err := str.Write(data); err != nil {
				return err
			}
			return str.Close()
		}()
	}()
	str, err := client.OpenStream()
	require.NoError(t, err)
	_, err = str.Write(payload)
	require.NoError(t, err)
	require.NoError(t, str.Close())
	got, err := io.ReadAll(str)
	require.NoError(t, err)
	require.NoError(t, <-echoErr)
	require.Equal(t, payload, got)
}
