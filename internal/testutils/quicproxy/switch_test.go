package quicproxy

import (
	"bytes"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func sendSwitchPacket(t *testing.T, socket *net.UDPConn, to net.Addr, payload string) {
	t.Helper()
	require.NoError(t, socket.SetWriteDeadline(time.Now().Add(2*time.Second)))
	n, err := socket.WriteTo([]byte(payload), to)
	require.NoError(t, err)
	require.Equal(t, len(payload), n)
}

func readSwitchPacket(t *testing.T, socket *net.UDPConn) (string, net.Addr) {
	t.Helper()
	require.NoError(t, socket.SetReadDeadline(time.Now().Add(2*time.Second)))
	buf := make([]byte, 128)
	n, from, err := socket.ReadFrom(buf)
	require.NoError(t, err)
	return string(buf[:n]), from
}

func startSwitchProxy(t *testing.T, p *Proxy, client, server *net.UDPConn) net.Addr {
	t.Helper()
	require.NoError(t, p.Start())
	t.Cleanup(func() { require.NoError(t, p.Close()) })
	sendSwitchPacket(t, client, p.LocalAddr(), "establish")
	payload, original := readSwitchPacket(t, server)
	require.Equal(t, "establish", payload)
	return original
}

func TestProxySwitchDiscardsRetiredRead(t *testing.T) {
	listener, client, server := newUPDConnLocalhost(t), newUPDConnLocalhost(t), newUPDConnLocalhost(t)
	replacement := newUPDConnLocalhost(t)
	held := make(chan SocketEvent, 1)
	releaseRead := make(chan struct{})
	release := sync.OnceFunc(func() { close(releaseRead) })
	defer release()
	var dropped, delayed []string
	p := &Proxy{
		Conn: listener, ServerAddr: server.LocalAddr().(*net.UDPAddr),
		ObserveSocket: func(ev SocketEvent) {
			if ev.Direction == DirectionOutgoing && ev.Operation == "read" && ev.Err == nil && string(ev.Data) == "retired" {
				ev.Data = bytes.Clone(ev.Data)
				held <- ev
				<-releaseRead
			}
		},
		DropPacket: func(dir Direction, _, _ net.Addr, b []byte) bool {
			if dir == DirectionOutgoing {
				dropped = append(dropped, string(b))
			}
			return false
		},
		DelayPacket: func(dir Direction, _, _ net.Addr, b []byte) time.Duration {
			if dir == DirectionOutgoing {
				delayed = append(delayed, string(b))
			}
			return 0
		},
	}
	original := startSwitchProxy(t, p, client, server)
	sendSwitchPacket(t, server, original, "retired")
	select {
	case ev := <-held:
		require.NoError(t, ev.Err)
		require.Equal(t, len("retired"), ev.N)
		require.Equal(t, "retired", string(ev.Data))
		require.Equal(t, server.LocalAddr(), ev.From)
		require.Equal(t, original, ev.To)
	case <-time.After(2 * time.Second):
		t.Fatal("old socket read did not reach its observer")
	}
	require.NoError(t, p.SwitchConn(client.LocalAddr().(*net.UDPAddr), replacement))
	sendSwitchPacket(t, server, replacement.LocalAddr(), "replacement")
	release()
	payload, from := readSwitchPacket(t, client)
	require.Equal(t, "replacement", payload)
	require.Equal(t, p.LocalAddr(), from)
	require.NoError(t, p.Close()) // Join callbacks before inspecting their records.
	require.Equal(t, []string{"replacement"}, dropped)
	require.Equal(t, []string{"replacement"}, delayed)
}

func TestProxySwitchFromOutgoingCallback(t *testing.T) {
	for _, callback := range []string{"drop", "delay"} {
		t.Run(callback, func(t *testing.T) {
			listener, client, server := newUPDConnLocalhost(t), newUPDConnLocalhost(t), newUPDConnLocalhost(t)
			replacement := newUPDConnLocalhost(t)
			switched := make(chan error, 1)
			p := &Proxy{Conn: listener, ServerAddr: server.LocalAddr().(*net.UDPAddr)}
			switchPacket := func(dir Direction, b []byte) {
				if dir == DirectionOutgoing && string(b) == "admitted" {
					switched <- p.SwitchConn(client.LocalAddr().(*net.UDPAddr), replacement)
				}
			}
			if callback == "drop" {
				p.DropPacket = func(dir Direction, _, _ net.Addr, b []byte) bool {
					switchPacket(dir, b)
					return false
				}
			} else {
				p.DelayPacket = func(dir Direction, _, _ net.Addr, b []byte) time.Duration {
					switchPacket(dir, b)
					return 0
				}
			}
			original := startSwitchProxy(t, p, client, server)
			sendSwitchPacket(t, server, original, "admitted")
			select {
			case err := <-switched:
				require.NoError(t, err)
			case <-time.After(2 * time.Second):
				t.Fatal("outgoing callback could not switch its admitted packet")
			}
			payload, _ := readSwitchPacket(t, client)
			require.Equal(t, "admitted", payload)
			sendSwitchPacket(t, server, replacement.LocalAddr(), "replacement")
			payload, _ = readSwitchPacket(t, client)
			require.Equal(t, "replacement", payload)
		})
	}
}

func TestProxySwitchPreservesQueuedOutgoingPacket(t *testing.T) {
	listener, client, server := newUPDConnLocalhost(t), newUPDConnLocalhost(t), newUPDConnLocalhost(t)
	replacement := newUPDConnLocalhost(t)
	consumerHeld, readerHeld := make(chan struct{}), make(chan struct{})
	releaseCallbacks := make(chan struct{})
	release := sync.OnceFunc(func() { close(releaseCallbacks) })
	defer release()
	p := &Proxy{
		Conn: listener, ServerAddr: server.LocalAddr().(*net.UDPAddr),
		DelayPacket: func(dir Direction, _, _ net.Addr, _ []byte) time.Duration {
			if dir == DirectionOutgoing {
				return time.Nanosecond
			}
			return 0
		},
		ObserveSocket: func(ev SocketEvent) {
			if ev.Direction != DirectionOutgoing || ev.Err != nil {
				return
			}
			if ev.Operation == "write" && string(ev.Data) == "hold consumer" {
				close(consumerHeld)
				<-releaseCallbacks
			}
			if ev.Operation == "read" && string(ev.Data) == "barrier" {
				// The preceding packet completed its delayed handoff before the
				// reader reached this next read. The consumer is still held above.
				close(readerHeld)
				<-releaseCallbacks
			}
		},
	}
	original := startSwitchProxy(t, p, client, server)
	sendSwitchPacket(t, server, original, "hold consumer")
	waitProxySignal(t, consumerHeld)
	payload, _ := readSwitchPacket(t, client)
	require.Equal(t, "hold consumer", payload)
	sendSwitchPacket(t, server, original, "queued")
	sendSwitchPacket(t, server, original, "barrier")
	waitProxySignal(t, readerHeld)
	require.NoError(t, p.SwitchConn(client.LocalAddr().(*net.UDPAddr), replacement))
	release()
	payload, _ = readSwitchPacket(t, client)
	require.Equal(t, "queued", payload)
	sendSwitchPacket(t, server, replacement.LocalAddr(), "replacement")
	payload, _ = readSwitchPacket(t, client)
	require.Equal(t, "replacement", payload)
}
