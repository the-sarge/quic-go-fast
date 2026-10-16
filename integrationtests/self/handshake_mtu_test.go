//go:build linux || darwin || windows

package self_test

import (
	"context"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/testutils"
	"github.com/stretchr/testify/require"
)

type handshakeMTUConn struct {
	net.PacketConn
	rejected atomic.Int32
}

func (c *handshakeMTUConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	if len(p) > 1200 {
		c.rejected.Add(1)
		return 0, &net.OpError{Op: "write", Net: "udp", Err: testutils.SendMsgSizeErr}
	}
	return c.PacketConn.WriteTo(p, addr)
}

func newHandshakeMTUConns(t *testing.T, role string) (client, server net.PacketConn, rejecting *handshakeMTUConn) {
	t.Helper()
	client, server = newUDPConnLocalhost(t), newUDPConnLocalhost(t)
	rejecting = &handshakeMTUConn{}
	if role == "client" {
		rejecting.PacketConn, client = client, rejecting
	} else {
		rejecting.PacketConn, server = server, rejecting
	}
	return client, server, rejecting
}

func TestHandshakeMTUFallback(t *testing.T) {
	for _, role := range []string{"client", "server"} {
		t.Run(role+"/unset", func(t *testing.T) {
			client, server, rejecting := newHandshakeMTUConns(t, role)
			conf := getQuicConfig(&quic.Config{DisablePathMTUDiscovery: true})
			ln, err := quic.Listen(server, getTLSConfigWithLongCertChain(), conf)
			require.NoError(t, err)
			defer ln.Close()
			ctx, cancel := context.WithTimeout(context.Background(), scaleDuration(3*time.Second))
			defer cancel()
			conn, err := quic.Dial(ctx, client, ln.Addr(), getTLSClientConfig(), conf)
			t.Logf("%s rejected oversized writes: %d", role, rejecting.rejected.Load())
			require.Positive(t, rejecting.rejected.Load(), "must exercise the native error")
			require.NoError(t, err)
			defer conn.CloseWithError(0, "")
			peer, err := ln.Accept(ctx)
			require.NoError(t, err)
			defer peer.CloseWithError(0, "")
			str, err := conn.OpenUniStream()
			require.NoError(t, err)
			_, err = str.Write([]byte("handshake recovered"))
			require.NoError(t, err)
			require.NoError(t, str.Close())
			received, err := peer.AcceptUniStream(ctx)
			require.NoError(t, err)
			require.NoError(t, received.SetReadDeadline(time.Now().Add(scaleDuration(time.Second))))
			data, err := io.ReadAll(received)
			require.NoError(t, err)
			require.Equal(t, "handshake recovered", string(data))
		})

		// An explicit size is a floor: the fallback never lowers it, so the handshake keeps
		// retransmitting at that size and times out, as InitialPacketSize documents.
		t.Run(role+"/explicit", func(t *testing.T) {
			client, server, rejecting := newHandshakeMTUConns(t, role)
			conf := getQuicConfig(&quic.Config{
				InitialPacketSize:       1452,
				DisablePathMTUDiscovery: true,
				HandshakeIdleTimeout:    scaleDuration(500 * time.Millisecond),
			})
			ln, err := quic.Listen(server, getTLSConfigWithLongCertChain(), conf)
			require.NoError(t, err)
			defer ln.Close()
			ctx, cancel := context.WithTimeout(context.Background(), scaleDuration(3*time.Second))
			defer cancel()
			conn, err := quic.Dial(ctx, client, ln.Addr(), getTLSClientConfig(), conf)
			if err == nil {
				conn.CloseWithError(0, "")
			}
			t.Logf("%s rejected oversized writes: %d; dial error: %v", role, rejecting.rejected.Load(), err)
			require.Positive(t, rejecting.rejected.Load(), "must exercise the native error")
			requireHandshakeTimeout(t, err)
		})
	}
}

// requireHandshakeTimeout accepts either handshake timeout. The idle variant fires when
// no peer packet is processed in time, for example when every client datagram is rejected.
func requireHandshakeTimeout(t *testing.T, err error) {
	t.Helper()
	_, isIdle := errors.AsType[*quic.IdleTimeoutError](err)
	_, isHandshake := errors.AsType[*quic.HandshakeTimeoutError](err)
	require.True(t, isIdle || isHandshake, "expected a handshake timeout, got %v", err)
	nerr, ok := errors.AsType[net.Error](err)
	require.True(t, ok && nerr.Timeout())
}
