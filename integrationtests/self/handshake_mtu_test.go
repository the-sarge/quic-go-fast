//go:build linux || darwin || windows

package self_test

import (
	"context"
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

func TestHandshakeMTUFallback(t *testing.T) {
	for _, role := range []string{"client", "server"} {
		t.Run(role, func(t *testing.T) {
			client, server := net.PacketConn(newUDPConnLocalhost(t)), net.PacketConn(newUDPConnLocalhost(t))
			rejecting := &handshakeMTUConn{}
			if role == "client" {
				rejecting.PacketConn, client = client, rejecting
			} else {
				rejecting.PacketConn, server = server, rejecting
			}
			conf := getQuicConfig(&quic.Config{InitialPacketSize: 1452, DisablePathMTUDiscovery: true})
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
	}
}
