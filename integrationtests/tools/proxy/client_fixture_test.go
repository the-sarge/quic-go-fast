package quicproxy

import (
	"net"
	"sync/atomic"
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

type proxyClientReader struct {
	packets  chan []byte
	done     chan struct{}
	received atomic.Int32
}

func readProxyClient(t testing.TB, conn *net.UDPConn, capacity int) *proxyClientReader {
	t.Helper()
	reader := &proxyClientReader{packets: make(chan []byte, capacity), done: make(chan struct{})}
	stop := make(chan struct{})
	t.Cleanup(func() {
		close(stop)
		conn.Close()
		select {
		case <-reader.done:
		case <-time.After(time.Second):
			t.Error("timeout joining proxy client reader")
		}
	})
	go func() {
		defer close(reader.done)
		for {
			buf := make([]byte, protocol.MaxPacketBufferSize)
			n, _, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			reader.received.Add(1)
			select {
			case reader.packets <- buf[:n]:
			case <-stop:
				return
			}
		}
	}()
	return reader
}
