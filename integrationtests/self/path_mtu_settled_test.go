package self_test

import (
	"context"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	quicproxy "github.com/quic-go/quic-go/integrationtests/tools/proxy"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/testutils/events"

	"github.com/stretchr/testify/require"
)

// A consumer finds the signal by interface assertion.
var _ interface{ PathMTUSettledV1() <-chan struct{} } = (*quic.Conn)(nil)

// datagramOverhead is the packet overhead the DATAGRAM payload estimate deducts:
// the type byte, the longest connection ID and the AEAD tag.
const datagramOverhead = 1 + 20 + 16

func isPathMTUSettled(conn *quic.Conn) bool {
	select {
	case <-conn.PathMTUSettledV1():
		return true
	default:
		return false
	}
}

func datagramLimit(t *testing.T, conn *quic.Conn) int64 {
	t.Helper()
	var tooLarge *quic.DatagramTooLargeError
	require.ErrorAs(t, conn.SendDatagram(make([]byte, 2000)), &tooLarge)
	return tooLarge.MaxDatagramPayloadSize
}

// runPathMTUSettledServer accepts connections, echoes their streams and hands each
// connection to the returned channel.
func runPathMTUSettledServer(t *testing.T, conf *quic.Config) (*quic.Listener, <-chan *quic.Conn) {
	t.Helper()
	ln, err := quic.Listen(newUDPConnLocalhost(t), getTLSConfig(), getQuicConfig(conf))
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })
	conns := make(chan *quic.Conn, 1)
	go func() {
		for {
			conn, err := ln.Accept(context.Background())
			if err != nil {
				return
			}
			conns <- conn
			go func() {
				for {
					str, err := conn.AcceptStream(context.Background())
					if err != nil {
						return
					}
					go func() {
						defer str.Close()
						io.Copy(str, str)
					}()
				}
			}()
		}
	}()
	return ln, conns
}

func dialPathMTUSettled(t *testing.T, addr net.Addr, conf *quic.Config) *quic.Conn {
	t.Helper()
	// A v4-only socket, so that DF can be set on older versions of macOS.
	tr := &quic.Transport{Conn: newUDPConnLocalhost(t)}
	t.Cleanup(func() { tr.Close() })
	conn, err := tr.Dial(context.Background(), addr, getTLSClientConfig(), getQuicConfig(conf))
	require.NoError(t, err)
	t.Cleanup(func() { conn.CloseWithError(0, "") })
	return conn
}

// echoUntilSettled keeps a stream echo running, since discovery sends its probes
// only while the connection is sending.
func echoUntilSettled(t *testing.T, conn *quic.Conn) {
	t.Helper()
	str, err := conn.OpenStream()
	require.NoError(t, err)
	defer str.Close()
	deadline := time.Now().Add(scaleDuration(20 * time.Second))
	require.NoError(t, str.SetDeadline(deadline))
	data := PRData[:16*1024]
	echoed := make([]byte, len(data))
	for !isPathMTUSettled(conn) {
		require.True(t, time.Now().Before(deadline), "path MTU did not settle")
		_, err := str.Write(data)
		require.NoError(t, err)
		_, err = io.ReadFull(str, echoed)
		require.NoError(t, err)
		require.Equal(t, data, echoed)
	}
}

func TestPathMTUSettled(t *testing.T) {
	initialLimit := int64(protocol.InitialPacketSize - datagramOverhead)
	for _, tc := range []struct {
		name     string
		pathCap  int // the largest packet the proxy forwards, or 0 for no proxy
		disabled bool
	}{
		{name: "loopback"},
		{name: "lost probes above the path limit", pathCap: 1400},
		{name: "discovery disabled", disabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ln, _ := runPathMTUSettledServer(t, &quic.Config{EnableDatagrams: true, DisablePathMTUDiscovery: true})
			addr := ln.Addr()
			if tc.pathCap > 0 {
				proxy := &quicproxy.Proxy{
					Conn:       newUDPConnLocalhost(t),
					ServerAddr: ln.Addr().(*net.UDPAddr),
					DropPacket: func(_ quicproxy.Direction, _, _ net.Addr, packet []byte) bool {
						return len(packet) > tc.pathCap
					},
				}
				require.NoError(t, proxy.Start())
				t.Cleanup(func() { proxy.Close() })
				addr = proxy.LocalAddr()
			}
			var recorder events.Recorder
			conn := dialPathMTUSettled(t, addr, &quic.Config{
				EnableDatagrams:         true,
				DisablePathMTUDiscovery: tc.disabled,
				Tracer:                  newTracer(&recorder),
			})

			if tc.disabled {
				select {
				case <-conn.PathMTUSettledV1():
				case <-time.After(scaleDuration(5 * time.Second)):
					t.Fatal("not settled at handshake confirmation")
				}
				require.Equal(t, initialLimit, datagramLimit(t, conn))
				require.Empty(t, recorder.Events(qlog.MTUUpdated{}))
				return
			}

			echoUntilSettled(t, conn)
			limit := datagramLimit(t, conn)
			updates := recorder.Events(qlog.MTUUpdated{})
			require.NotEmpty(t, updates)
			final := updates[len(updates)-1].(qlog.MTUUpdated).Value
			t.Logf("settled at %d bytes, DATAGRAM limit %d (initial %d)", final, limit, initialLimit)
			if tc.pathCap > 0 {
				require.LessOrEqual(t, final, tc.pathCap)
			}
			require.Greater(t, limit, initialLimit)
			require.Equal(t, int64(final-datagramOverhead), limit)

			// More traffic doesn't move a settled limit.
			str, err := conn.OpenStream()
			require.NoError(t, err)
			require.NoError(t, str.SetDeadline(time.Now().Add(scaleDuration(10*time.Second))))
			data := PRData[:64*1024]
			go func() {
				str.Write(data)
				str.Close()
			}()
			echoed, err := io.ReadAll(str)
			require.NoError(t, err)
			require.Equal(t, data, echoed)
			require.Equal(t, limit, datagramLimit(t, conn), "a later read")
		})
	}
}

// The search needs several probes, each five RTTs apart, so on a slow path every way
// a connection ends arrives before it concludes.
func TestPathMTUSettledConnectionEnd(t *testing.T) {
	rtt := scaleDuration(100 * time.Millisecond)
	for _, tc := range []struct {
		name  string
		end   func(t *testing.T, client, server *quic.Conn, blackhole *atomic.Bool)
		cause func(t *testing.T, err error)
	}{
		{
			name: "local close",
			end: func(t *testing.T, client, _ *quic.Conn, _ *atomic.Bool) {
				require.NoError(t, client.CloseWithError(0, ""))
			},
			cause: func(t *testing.T, err error) {
				var appErr *quic.ApplicationError
				require.ErrorAs(t, err, &appErr)
				require.False(t, appErr.Remote)
			},
		},
		{
			name: "peer close",
			end: func(t *testing.T, _, server *quic.Conn, _ *atomic.Bool) {
				require.NoError(t, server.CloseWithError(0, ""))
			},
			cause: func(t *testing.T, err error) {
				var appErr *quic.ApplicationError
				require.ErrorAs(t, err, &appErr)
				require.True(t, appErr.Remote)
			},
		},
		{
			name:  "idle timeout",
			end:   func(_ *testing.T, _, _ *quic.Conn, blackhole *atomic.Bool) { blackhole.Store(true) },
			cause: func(t *testing.T, err error) { require.ErrorIs(t, err, &quic.IdleTimeoutError{}) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ln, serverConns := runPathMTUSettledServer(t, &quic.Config{EnableDatagrams: true, DisablePathMTUDiscovery: true})
			var blackhole atomic.Bool
			proxy := &quicproxy.Proxy{
				Conn:        newUDPConnLocalhost(t),
				ServerAddr:  ln.Addr().(*net.UDPAddr),
				DelayPacket: func(quicproxy.Direction, net.Addr, net.Addr, []byte) time.Duration { return rtt / 2 },
				DropPacket:  func(quicproxy.Direction, net.Addr, net.Addr, []byte) bool { return blackhole.Load() },
			}
			require.NoError(t, proxy.Start())
			t.Cleanup(func() { proxy.Close() })
			client := dialPathMTUSettled(t, proxy.LocalAddr(), &quic.Config{
				EnableDatagrams: true,
				MaxIdleTimeout:  10 * rtt,
			})
			var server *quic.Conn
			select {
			case server = <-serverConns:
			case <-time.After(scaleDuration(5 * time.Second)):
				t.Fatal("server did not accept")
			}
			require.False(t, isPathMTUSettled(client), "settled before the connection ended")

			tc.end(t, client, server, &blackhole)
			select {
			case <-client.PathMTUSettledV1():
			case <-time.After(scaleDuration(10 * time.Second)):
				t.Fatal("not settled after the connection ended")
			}
			// The signal can close just before the context is cancelled.
			select {
			case <-client.Context().Done():
			case <-time.After(scaleDuration(5 * time.Second)):
				t.Fatal("context not cancelled")
			}
			tc.cause(t, context.Cause(client.Context()))
		})
	}
}
