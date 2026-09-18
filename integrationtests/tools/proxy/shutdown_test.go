package quicproxy

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProxyShutdownListenerHandback(t *testing.T) {
	for _, closeListener := range []bool{false, true} {
		name := "borrowed listener"
		if closeListener {
			name = "already closed listener"
		}
		t.Run(name, func(t *testing.T) {
			listener, peer := newUPDConnLocalhost(t), newUPDConnLocalhost(t)
			readFinished := make(chan SocketEvent, 1)
			p := &Proxy{Conn: listener, ServerAddr: peer.LocalAddr().(*net.UDPAddr), ObserveSocket: func(ev SocketEvent) {
				if ev.Operation == "read" {
					readFinished <- ev
				}
			}}
			require.NoError(t, p.Start())
			if closeListener {
				require.NoError(t, listener.Close())
			}
			closed := make(chan error, 1)
			go func() { closed <- p.Close() }()
			select {
			case err := <-closed:
				require.NoError(t, err)
			case <-time.After(time.Second):
				t.Fatal("Close did not finish pending I/O")
			}
			select {
			case ev := <-readFinished:
				require.Error(t, ev.Err)
			default:
				t.Fatal("Close returned before the listener read completed")
			}
			if closeListener {
				return
			}
			// Exercise both directions without replacing the listener's deadlines.
			_, err := listener.WriteTo([]byte("returned"), peer.LocalAddr())
			require.NoError(t, err)
			require.NoError(t, peer.SetReadDeadline(time.Now().Add(time.Second)))
			buf := make([]byte, 32)
			n, _, err := peer.ReadFrom(buf)
			require.NoError(t, err)
			require.Equal(t, "returned", string(buf[:n]))
			_, err = peer.WriteTo([]byte("caller"), listener.LocalAddr())
			require.NoError(t, err)
			watchdog := time.AfterFunc(time.Second, func() { listener.Close() })
			defer watchdog.Stop()
			n, _, err = listener.ReadFrom(buf)
			require.NoError(t, err)
			require.Equal(t, "caller", string(buf[:n]))
		})
	}
}

func waitProxyShutdown(t *testing.T, closed <-chan error) {
	t.Helper()
	select {
	case err := <-closed:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("proxy workers did not finish")
	}
}

func waitProxySignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal("missing coordinated proxy event")
	}
}

func TestProxyShutdownDelayedHandoffs(t *testing.T) {
	for _, dir := range []Direction{DirectionIncoming, DirectionOutgoing} {
		t.Run(dir.String(), func(t *testing.T) {
			listener, client, server := newUPDConnLocalhost(t), newUPDConnLocalhost(t), newUPDConnLocalhost(t)
			writeHeld, releaseWrite, handoffReady := make(chan struct{}), make(chan struct{}), make(chan struct{})
			release := sync.OnceFunc(func() { close(releaseWrite) })
			defer release()
			writes := make(chan error, 1)
			var delayed int
			var p *Proxy
			p = &Proxy{Conn: listener, ServerAddr: server.LocalAddr().(*net.UDPAddr),
				DelayPacket: func(d Direction, _, _ net.Addr, _ []byte) time.Duration {
					if d != dir {
						return 0
					}
					delayed++
					if delayed == 1 {
						// Force the consumer's first forwarding operation to fail. Its observer
						// holds the consumer until its ten-slot handoff and next send are full.
						if dir == DirectionIncoming {
							p.mutex.Lock()
							p.clientDict[client.LocalAddr().String()].GetServerConn().Close()
							p.mutex.Unlock()
						} else {
							listener.Close()
						}
					}
					if delayed == 12 {
						close(handoffReady)
					}
					return time.Nanosecond
				},
				ObserveSocket: func(ev SocketEvent) {
					if ev.Direction == dir && ev.Operation == "write" {
						writes <- ev.Err
						close(writeHeld)
						<-releaseWrite
					}
				},
			}
			require.NoError(t, p.Start())
			var destination net.Addr
			sender := client
			if dir == DirectionOutgoing {
				_, err := client.WriteTo([]byte("establish"), listener.LocalAddr())
				require.NoError(t, err)
				require.NoError(t, server.SetReadDeadline(time.Now().Add(time.Second)))
				_, destination, err = server.ReadFrom(make([]byte, 32))
				require.NoError(t, err)
				sender = server
			} else {
				destination = listener.LocalAddr()
			}
			_, err := sender.WriteTo([]byte("consumer fails"), destination)
			require.NoError(t, err)
			waitProxySignal(t, writeHeld)
			require.ErrorIs(t, <-writes, net.ErrClosed)
			for range 11 {
				_, err = sender.WriteTo([]byte("delayed"), destination)
				require.NoError(t, err)
			}
			waitProxySignal(t, handoffReady)
			closed := make(chan error, 1)
			go func() { closed <- p.Close() }()
			waitProxySignal(t, p.closeChan)
			release()
			waitProxyShutdown(t, closed)
			require.Equal(t, 12, delayed) // Close joined the callback's producer too.
		})
	}
}

func TestProxyShutdownAdmissionAndSwitch(t *testing.T) {
	t.Run("held read cannot admit after shutdown", func(t *testing.T) {
		listener, client, server := newUPDConnLocalhost(t), newUPDConnLocalhost(t), newUPDConnLocalhost(t)
		entered, releaseRead, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
		release := sync.OnceFunc(func() { close(releaseRead) })
		defer release()
		p := &Proxy{Conn: listener, ServerAddr: server.LocalAddr().(*net.UDPAddr), ObserveSocket: func(ev SocketEvent) {
			if ev.Direction == DirectionIncoming && ev.Err == nil {
				close(entered)
				<-releaseRead
				close(returned)
			}
		}}
		require.NoError(t, p.Start())
		_, err := client.WriteTo([]byte("late client"), listener.LocalAddr())
		require.NoError(t, err)
		waitProxySignal(t, entered)
		closed := make(chan error, 2)
		for range 2 {
			go func() { closed <- p.Close() }()
		}
		waitProxySignal(t, p.closeChan)
		replacement := newUPDConnLocalhost(t)
		require.ErrorIs(t, p.SwitchConn(client.LocalAddr().(*net.UDPAddr), replacement), net.ErrClosed)
		_, err = replacement.WriteTo([]byte("still owned by caller"), server.LocalAddr())
		require.NoError(t, err)
		select {
		case <-closed:
			t.Fatal("Close returned with an outstanding callback")
		default:
		}
		release()
		waitProxyShutdown(t, closed)
		waitProxyShutdown(t, closed)
		select {
		case <-returned:
		default:
			t.Fatal("callback still active")
		}
		require.Empty(t, p.clientDict)
		require.NoError(t, p.Close())
	})

	t.Run("live callback switch and socket ownership", func(t *testing.T) {
		listener, client, server := newUPDConnLocalhost(t), newUPDConnLocalhost(t), newUPDConnLocalhost(t)
		first, active := newUPDConnLocalhost(t), newUPDConnLocalhost(t)
		switched := make(chan error, 1)
		held, releaseDelay := make(chan struct{}), make(chan struct{})
		release := sync.OnceFunc(func() { close(releaseDelay) })
		defer release()
		var original *net.UDPConn
		var p *Proxy
		p = &Proxy{Conn: listener, ServerAddr: server.LocalAddr().(*net.UDPAddr), DelayPacket: func(_ Direction, from, _ net.Addr, _ []byte) time.Duration {
			p.mutex.Lock()
			original = p.clientDict[from.String()].GetServerConn()
			p.mutex.Unlock()
			err := p.SwitchConn(from.(*net.UDPAddr), first)
			if err == nil {
				err = p.SwitchConn(from.(*net.UDPAddr), active)
			}
			switched <- err
			close(held)
			<-releaseDelay
			return time.Hour
		}}
		require.NoError(t, p.Start())
		_, err := client.WriteTo([]byte("switch"), listener.LocalAddr())
		require.NoError(t, err)
		waitProxySignal(t, held)
		require.NoError(t, <-switched)
		// An already-closed active replacement must not skip the retained original.
		require.NoError(t, active.Close())
		closed := make(chan error, 2)
		for range 2 {
			go func() { closed <- p.Close() }()
		}
		waitProxySignal(t, p.closeChan)
		rejected := newUPDConnLocalhost(t)
		require.ErrorIs(t, p.SwitchConn(client.LocalAddr().(*net.UDPAddr), rejected), net.ErrClosed)
		release()
		waitProxyShutdown(t, closed)
		waitProxyShutdown(t, closed)
		_, err = original.WriteTo([]byte("closed"), server.LocalAddr())
		require.ErrorIs(t, err, net.ErrClosed)
		_, err = first.WriteTo([]byte("retired caller socket"), server.LocalAddr())
		require.NoError(t, err)
		_, err = rejected.WriteTo([]byte("rejected caller socket"), server.LocalAddr())
		require.NoError(t, err)
	})
}
