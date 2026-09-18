package quicproxy

import (
	"net"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
)

func dialProxyClient(t testing.TB, addr *net.UDPAddr) *net.UDPConn {
	t.Helper()
	conn, err := net.DialUDP("udp", nil, addr)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	return conn
}

func readProxyClient(t testing.TB, conn *net.UDPConn, capacity int) (<-chan []byte, <-chan struct{}) {
	t.Helper()
	packets := make(chan []byte, capacity)
	done := make(chan struct{})
	stop := make(chan struct{})
	t.Cleanup(func() {
		close(stop)
		conn.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("timeout joining proxy client reader")
		}
	})
	go func() {
		defer close(done)
		for {
			buf := make([]byte, protocol.MaxPacketBufferSize)
			n, _, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			select {
			case packets <- buf[:n]:
			case <-stop:
				return
			}
		}
	}()
	return packets, done
}
