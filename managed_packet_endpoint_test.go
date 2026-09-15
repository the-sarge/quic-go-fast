package quic

import (
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

type managedEndpointFactoryV1 interface {
	NewManagedPacketEndpointV1(string, *net.UDPAddr) (net.PacketConn, func() (net.PacketConn, error), error)
}

func newTestManagedEndpoint(t *testing.T) (net.PacketConn, func() (net.PacketConn, error)) {
	t.Helper()
	factory, ok := any(&Transport{}).(managedEndpointFactoryV1)
	require.True(t, ok, "factory must be discoverable without fork-only types")
	endpoint, acquire, err := factory.NewManagedPacketEndpointV1("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	t.Cleanup(func() { endpoint.Close() })
	return endpoint, acquire
}

func TestManagedEndpointOrdinaryDatagrams(t *testing.T) {
	endpoint, _ := newTestManagedEndpoint(t)
	peer := listenExternalUDP(t)
	require.NoError(t, endpoint.SetDeadline(time.Now().Add(time.Second)))
	require.NoError(t, peer.SetDeadline(time.Now().Add(time.Second)))
	for _, payload := range []string{"one", "second datagram"} {
		n, err := endpoint.WriteTo([]byte(payload), peer.LocalAddr())
		require.NoError(t, err)
		require.Equal(t, len(payload), n)
		buf := make([]byte, 64)
		n, addr, err := peer.ReadFrom(buf)
		require.NoError(t, err)
		require.Equal(t, payload, string(buf[:n]))
		require.Equal(t, endpoint.LocalAddr(), addr)
		_, err = peer.WriteTo(buf[:n], addr)
		require.NoError(t, err)
		n, addr, err = endpoint.ReadFrom(buf)
		require.NoError(t, err)
		require.Equal(t, payload, string(buf[:n]))
		require.Equal(t, peer.LocalAddr(), addr)
	}
}

func TestManagedEndpointExclusiveLease(t *testing.T) {
	endpoint, acquire := newTestManagedEndpoint(t)
	require.NotNil(t, acquire)
	lease, err := acquire()
	require.NoError(t, err)
	require.Equal(t, endpoint.LocalAddr(), lease.LocalAddr())
	_, err = acquire()
	require.Error(t, err)
	_, _, err = endpoint.ReadFrom(make([]byte, 1))
	require.Error(t, err)
	peer := listenExternalUDP(t)
	_, err = endpoint.WriteTo([]byte("parent"), peer.LocalAddr())
	require.Error(t, err)
	require.Error(t, endpoint.SetDeadline(time.Now()))
	require.NoError(t, lease.SetDeadline(time.Now().Add(time.Second)))
	_, err = lease.WriteTo([]byte("lease"), peer.LocalAddr())
	require.NoError(t, err)
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(time.Second)))
	buf := make([]byte, 64)
	n, addr, err := peer.ReadFrom(buf)
	require.NoError(t, err)
	require.Equal(t, "lease", string(buf[:n]))
	_, err = peer.WriteTo([]byte("reply"), addr)
	require.NoError(t, err)
	n, _, err = lease.ReadFrom(buf)
	require.NoError(t, err)
	require.Equal(t, "reply", string(buf[:n]))
	require.NoError(t, lease.Close())
	next, err := acquire()
	require.NoError(t, err)
	defer next.Close()
	_, err = lease.WriteTo([]byte("stale"), peer.LocalAddr())
	require.ErrorIs(t, err, net.ErrClosed)
	require.ErrorIs(t, lease.SetDeadline(time.Now()), net.ErrClosed)
	require.NoError(t, lease.Close())
	_, err = next.WriteTo([]byte("next"), peer.LocalAddr())
	require.NoError(t, err)
	n, _, err = peer.ReadFrom(buf)
	require.NoError(t, err)
	require.Equal(t, "next", string(buf[:n]))
}

func TestManagedEndpointRestoresDeadlines(t *testing.T) {
	for _, direction := range []string{"read", "write"} {
		t.Run(direction, func(t *testing.T) {
			endpoint, acquire := newTestManagedEndpoint(t)
			past := time.Now().Add(-time.Second)
			if direction == "read" {
				require.NoError(t, endpoint.SetReadDeadline(past))
			} else {
				require.NoError(t, endpoint.SetWriteDeadline(past))
			}
			lease, err := acquire()
			require.NoError(t, err)
			require.NoError(t, lease.SetDeadline(time.Now().Add(time.Second)))
			require.NoError(t, lease.Close())
			peer := listenExternalUDP(t)
			if direction == "read" {
				_, _, err = endpoint.ReadFrom(make([]byte, 64))
			} else {
				_, err = endpoint.WriteTo([]byte("expired"), peer.LocalAddr())
			}
			require.ErrorIs(t, err, os.ErrDeadlineExceeded)
			require.NoError(t, endpoint.SetDeadline(time.Time{}))
			next, err := acquire()
			require.NoError(t, err)
			require.NoError(t, next.SetDeadline(past))
			require.NoError(t, next.Close())
			// Zero logical deadlines must also replace the lease's expired deadlines.
			_, err = endpoint.WriteTo([]byte("ordinary"), peer.LocalAddr())
			require.NoError(t, err)
			_, err = peer.WriteTo([]byte("ordinary"), endpoint.LocalAddr())
			require.NoError(t, err)
			n, _, err := endpoint.ReadFrom(make([]byte, 64))
			require.NoError(t, err)
			require.Equal(t, len("ordinary"), n)
		})
	}
}

func TestManagedEndpointTerminalClose(t *testing.T) {
	endpoint, acquire := newTestManagedEndpoint(t)
	lease, err := acquire()
	require.NoError(t, err)
	require.NoError(t, endpoint.Close())
	_, err = acquire()
	require.ErrorIs(t, err, net.ErrClosed)
	for _, conn := range []net.PacketConn{endpoint, lease} {
		_, _, err = conn.ReadFrom(make([]byte, 1))
		require.ErrorIs(t, err, net.ErrClosed)
		_, err = conn.WriteTo([]byte("closed"), endpoint.LocalAddr())
		require.ErrorIs(t, err, net.ErrClosed)
		require.ErrorIs(t, conn.SetReadDeadline(time.Time{}), net.ErrClosed)
		require.ErrorIs(t, conn.SetWriteDeadline(time.Time{}), net.ErrClosed)
		require.NoError(t, conn.Close())
	}
}

// This socket-boundary fixture models an OS read or write that has been
// interrupted but has not yet returned. UDP writes rarely block on loopback;
// explicit channels let the public Close/acquire contract demonstrate joining.
type managedBlockingSocket struct {
	net.PacketConn
	entered         chan struct{}
	interrupted     chan struct{}
	finish          chan struct{}
	interruptOnce   sync.Once
	deadlineFailure string
}

func newManagedBlockingSocket() *managedBlockingSocket {
	return &managedBlockingSocket{entered: make(chan struct{}), interrupted: make(chan struct{}), finish: make(chan struct{})}
}

func (s *managedBlockingSocket) block() error {
	close(s.entered)
	<-s.interrupted
	<-s.finish
	return os.ErrDeadlineExceeded
}
func (s *managedBlockingSocket) ReadFrom([]byte) (int, net.Addr, error) { return 0, nil, s.block() }
func (s *managedBlockingSocket) WriteTo([]byte, net.Addr) (int, error)  { return 0, s.block() }
func (s *managedBlockingSocket) Close() error {
	s.interruptOnce.Do(func() { close(s.interrupted) })
	return nil
}

func (s *managedBlockingSocket) SetDeadline(t time.Time) error {
	if s.deadlineFailure == "interrupt" {
		return errManagedTestDeadline
	}
	if !t.IsZero() && t.Before(time.Now()) {
		s.interruptOnce.Do(func() { close(s.interrupted) })
	}
	return nil
}

func (s *managedBlockingSocket) SetReadDeadline(time.Time) error {
	if s.deadlineFailure == "read" {
		return errManagedTestDeadline
	}
	return nil
}

func (s *managedBlockingSocket) SetWriteDeadline(time.Time) error {
	if s.deadlineFailure == "write" {
		return errManagedTestDeadline
	}
	return nil
}

var errManagedTestDeadline = errors.New("test socket deadline failure")

func TestManagedEndpointJoinsIO(t *testing.T) {
	for _, direction := range []string{"read", "write"} {
		for _, parent := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/parent=%t", direction, parent), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					socket := newManagedBlockingSocket()
					endpoint, acquire, err := newManagedPacketEndpoint(socket)
					require.NoError(t, err)
					conn := endpoint
					if !parent {
						conn, err = acquire()
						require.NoError(t, err)
					}
					ioDone := make(chan error, 1)
					go func() {
						if direction == "read" {
							_, _, err := conn.ReadFrom(make([]byte, 1))
							ioDone <- err
						} else {
							_, err := conn.WriteTo([]byte("pending"), nil)
							ioDone <- err
						}
					}()
					<-socket.entered
					_, err = acquire()
					require.Error(t, err, "active I/O must make acquisition busy")
					select {
					case <-socket.interrupted:
						t.Fatal("acquisition canceled active I/O")
					default:
					}
					closeDone := make(chan error, 1)
					go func() { closeDone <- conn.Close() }()
					<-socket.interrupted
					synctest.Wait()
					select {
					case <-closeDone:
						t.Fatal("Close returned before I/O joined")
					default:
					}
					_, err = acquire()
					require.Error(t, err, "returning/terminal state must reject acquisition")
					require.ErrorIs(t, conn.SetDeadline(time.Time{}), net.ErrClosed)
					closeAgain := make(chan error, 1)
					go func() { closeAgain <- conn.Close() }()
					synctest.Wait()
					select {
					case <-closeAgain:
						t.Fatal("concurrent Close returned before I/O joined")
					default:
					}
					close(socket.finish)
					require.ErrorIs(t, <-ioDone, os.ErrDeadlineExceeded)
					require.NoError(t, <-closeDone)
					require.NoError(t, <-closeAgain)
					if parent {
						_, err = acquire()
						require.ErrorIs(t, err, net.ErrClosed)
					} else {
						next, err := acquire()
						require.NoError(t, err)
						require.NoError(t, next.Close())
					}
					require.NoError(t, endpoint.Close())
				})
			})
		}
	}
}

func TestManagedEndpointHandbackFailureIsTerminal(t *testing.T) {
	for _, stage := range []string{"interrupt", "read", "write"} {
		t.Run(stage, func(t *testing.T) {
			socket := newManagedBlockingSocket()
			endpoint, acquire, err := newManagedPacketEndpoint(socket)
			require.NoError(t, err)
			lease, err := acquire()
			require.NoError(t, err)
			socket.deadlineFailure = stage
			require.ErrorIs(t, lease.Close(), errManagedTestDeadline)
			require.ErrorIs(t, lease.Close(), errManagedTestDeadline)
			_, err = acquire()
			require.ErrorIs(t, err, net.ErrClosed)
			require.ErrorIs(t, endpoint.SetDeadline(time.Time{}), net.ErrClosed)
			require.ErrorIs(t, endpoint.Close(), errManagedTestDeadline)
		})
	}
}

func TestManagedEndpointConcurrentAcquisition(t *testing.T) {
	endpoint, acquire := newTestManagedEndpoint(t)
	start := make(chan struct{})
	results := make(chan net.PacketConn, 2)
	for range 2 {
		go func() { <-start; lease, _ := acquire(); results <- lease }()
	}
	close(start)
	first, second := <-results, <-results
	require.NotEqual(t, first == nil, second == nil, "exactly one acquisition must succeed")
	if first == nil {
		first = second
	}
	require.NoError(t, first.Close())
	require.ErrorIs(t, first.SetReadDeadline(time.Time{}), net.ErrClosed)
	require.ErrorIs(t, first.SetWriteDeadline(time.Time{}), net.ErrClosed)
	_, _, err := first.ReadFrom(make([]byte, 1))
	require.ErrorIs(t, err, net.ErrClosed)
	require.NoError(t, endpoint.Close())
}

func TestManagedEndpointConstructorFailure(t *testing.T) {
	factory, ok := any(&Transport{}).(managedEndpointFactoryV1)
	require.True(t, ok)
	endpoint, acquire, err := factory.NewManagedPacketEndpointV1("invalid-network", nil)
	require.Error(t, err)
	require.Nil(t, endpoint)
	require.Nil(t, acquire)
}

func TestManagedEndpointParentCloseInterruptsLease(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		socket := newManagedBlockingSocket()
		endpoint, acquire, err := newManagedPacketEndpoint(socket)
		require.NoError(t, err)
		lease, err := acquire()
		require.NoError(t, err)
		ioDone := make(chan error, 1)
		go func() { _, _, err := lease.ReadFrom(make([]byte, 1)); ioDone <- err }()
		<-socket.entered
		closed := make(chan error, 1)
		go func() { closed <- endpoint.Close() }()
		<-socket.interrupted
		synctest.Wait()
		select {
		case <-closed:
			t.Fatal("parent Close did not join lease I/O")
		default:
		}
		_, err = acquire()
		require.ErrorIs(t, err, net.ErrClosed)
		close(socket.finish)
		require.ErrorIs(t, <-ioDone, os.ErrDeadlineExceeded)
		require.NoError(t, <-closed)
		require.NoError(t, lease.Close())
	})
}
