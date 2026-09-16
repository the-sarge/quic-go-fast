package quic

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"runtime"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/testdata"
	"github.com/quic-go/quic-go/testutils/events"

	"github.com/stretchr/testify/require"
)

type managedPacketIOV1 interface {
	ConfigureManagedPacketIOV1(net.PacketConn, net.PacketConn, func([][]byte, []byte, *net.UDPAddr) (int, error)) error
}

func TestManagedPacketIOInvalidLease(t *testing.T) {
	for _, kind := range []string{"nil", "typed nil", "parent", "foreign", "promoted", "closed"} {
		t.Run(kind, func(t *testing.T) {
			endpoint, acquire := newTestManagedEndpoint(t)
			lease, err := acquire()
			require.NoError(t, err)
			defer lease.Close()
			candidate := lease
			switch kind {
			case "nil":
				candidate = nil
			case "typed nil":
				candidate = (*managedPacketConn)(nil)
			case "parent":
				candidate = endpoint
			case "foreign":
				candidate = listenExternalUDP(t)
			case "promoted":
				candidate = &struct{ net.PacketConn }{lease}
			case "closed":
				require.NoError(t, lease.Close())
			}
			tr := &Transport{Conn: lease}
			require.Error(t, tr.ConfigureManagedPacketIOV1(lease, candidate, nil))
			if kind != "closed" {
				require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil), "failed admission must not consume either slot")
			}
		})
	}
}

func TestManagedPacketIOOuterBinding(t *testing.T) {
	for _, kind := range []string{"nil", "nonpointer", "different", "external first", "initialized", "swapped"} {
		t.Run(kind, func(t *testing.T) {
			_, acquire := newTestManagedEndpoint(t)
			lease, err := acquire()
			require.NoError(t, err)
			defer lease.Close()
			tr := &Transport{Conn: lease}
			outer := lease
			switch kind {
			case "nil":
				outer = nil
			case "nonpointer":
				outer = nonPointerPacketConn{PacketConn: lease}
				tr.Conn = outer
			case "different":
				outer = &struct{ net.PacketConn }{lease}
			case "external first":
				require.NoError(t, tr.ConfigureExternalPacketIOV1(lease, false, nil))
			case "initialized":
				require.NoError(t, tr.Close())
			case "swapped":
				require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil))
				tr.Conn = &struct{ net.PacketConn }{lease}
				_, err := tr.WriteTo([]byte("must reject"), lease.LocalAddr())
				require.Error(t, err)
				return
			}
			require.Error(t, tr.ConfigureManagedPacketIOV1(outer, lease, nil))
		})
	}
	for _, parent := range []bool{false, true} {
		t.Run(map[bool]string{false: "different lease", true: "managed parent"}[parent], func(t *testing.T) {
			endpoint, acquire := newTestManagedEndpoint(t)
			lease, err := acquire()
			require.NoError(t, err)
			defer lease.Close()
			_, otherAcquire := newTestManagedEndpoint(t)
			other, err := otherAcquire()
			require.NoError(t, err)
			defer other.Close()
			outer := other
			if parent {
				outer = endpoint
			}
			tr := &Transport{Conn: outer}
			require.Error(t, tr.ConfigureManagedPacketIOV1(outer, lease, nil))
			tr.Conn = lease
			require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil), "rejection must preserve transport slot and supplied lease authority")
			otherTransport := &Transport{Conn: other}
			require.NoError(t, otherTransport.ConfigureManagedPacketIOV1(other, other, nil), "rejection must preserve outer lease authority")
		})
	}
}

func TestManagedPacketIOProbeThenQUIC(t *testing.T) {
	endpoint, acquire := newTestManagedEndpoint(t)
	lease, err := acquire()
	require.NoError(t, err)
	defer lease.Close()
	peer := listenExternalUDP(t)
	require.NoError(t, lease.SetDeadline(time.Now().Add(5*time.Second)))
	require.NoError(t, peer.SetDeadline(time.Now().Add(5*time.Second)))
	_, err = peer.WriteTo([]byte("probe"), lease.LocalAddr())
	require.NoError(t, err)
	buf := make([]byte, 32)
	n, addr, err := lease.ReadFrom(buf)
	require.NoError(t, err)
	require.Equal(t, "probe", string(buf[:n]))
	_, err = lease.WriteTo([]byte("probe reply"), addr)
	require.NoError(t, err)
	n, _, err = peer.ReadFrom(buf)
	require.NoError(t, err)
	require.Equal(t, "probe reply", string(buf[:n]))
	require.NoError(t, lease.SetDeadline(time.Time{}))
	require.NoError(t, peer.SetDeadline(time.Time{}))
	outer := &struct{ net.PacketConn }{lease}
	server := &Transport{Conn: outer}
	writer := lease.(managedBatchWriterV1).WriteBatchV1
	require.NoError(t, server.ConfigureManagedPacketIOV1(outer, lease, writer))
	tlsConf := testdata.GetTLSConfig()
	tlsConf.NextProtos = []string{"managed-lease-test"}
	ln, err := server.Listen(tlsConf, &Config{EnableDatagrams: true})
	require.NoError(t, err)
	defer server.Close()
	client := &Transport{Conn: peer}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cc, err := client.Dial(ctx, endpoint.LocalAddr(), &tls.Config{RootCAs: testdata.GetRootCA(), ServerName: "localhost", NextProtos: tlsConf.NextProtos}, &Config{EnableDatagrams: true})
	require.NoError(t, err)
	sc, err := ln.Accept(ctx)
	require.NoError(t, err)
	require.NoError(t, cc.SendDatagram([]byte("QUIC payload")))
	payload, err := sc.ReceiveDatagram(ctx)
	require.NoError(t, err)
	require.Equal(t, "QUIC payload", string(payload))
	require.NoError(t, server.Close())
	_, err = acquire()
	require.Error(t, err, "Transport.Close must not hand back the lease")
	require.NoError(t, lease.Close())
	next, err := acquire()
	require.NoError(t, err)
	defer next.Close()
	n, err = writer([][]byte{[]byte("stale")}, nil, peer.LocalAddr().(*net.UDPAddr))
	require.Zero(t, n)
	require.ErrorIs(t, err, net.ErrClosed)
	require.ErrorIs(t, lease.SetDeadline(time.Now()), net.ErrClosed)
	// A later transport Close must not change the new generation's deadlines.
	require.NoError(t, server.Close())
	_, err = next.WriteTo([]byte("new lease"), peer.LocalAddr())
	require.NoError(t, err)
}

type delayedManagedWriter struct {
	net.PacketConn
	entered chan struct{}
	resume  chan struct{}
	done    chan error
}

func (c *delayedManagedWriter) WriteTo(b []byte, addr net.Addr) (int, error) {
	if string(b) != "queued" {
		return c.PacketConn.WriteTo(b, addr)
	}
	close(c.entered)
	<-c.resume
	n, err := c.PacketConn.WriteTo(b, addr)
	c.done <- err
	return n, err
}

func TestManagedPacketIODelayedQueuedSend(t *testing.T) {
	_, acquire := newTestManagedEndpoint(t)
	lease, err := acquire()
	require.NoError(t, err)
	outer := &delayedManagedWriter{PacketConn: lease, entered: make(chan struct{}), resume: make(chan struct{}), done: make(chan error, 1)}
	tr := &Transport{Conn: outer}
	require.NoError(t, tr.ConfigureManagedPacketIOV1(outer, lease, nil))
	peer := listenExternalUDP(t)
	_, err = tr.WriteTo([]byte("prime"), peer.LocalAddr())
	require.NoError(t, err)
	// Exercise the real asynchronous transport close-output worker.
	tr.closeQueue <- closePacket{payload: []byte("queued"), addr: peer.LocalAddr()}
	<-outer.entered
	require.NoError(t, tr.Close(), "Transport.Close does not wait for queued output")
	require.NoError(t, lease.Close())
	next, err := acquire()
	require.NoError(t, err)
	defer next.Close()
	close(outer.resume)
	require.ErrorIs(t, <-outer.done, net.ErrClosed)
	for _, deadline := range []func(time.Time) error{lease.SetDeadline, lease.SetReadDeadline, lease.SetWriteDeadline} {
		require.ErrorIs(t, deadline(time.Now().Add(-time.Second)), net.ErrClosed)
	}
	_, _, err = lease.ReadFrom(make([]byte, 1))
	require.ErrorIs(t, err, net.ErrClosed)
	_, err = next.WriteTo([]byte("next"), peer.LocalAddr())
	require.NoError(t, err)
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(time.Second)))
	for _, want := range []string{"prime", "next"} {
		buf := make([]byte, 32)
		n, _, err := peer.ReadFrom(buf)
		require.NoError(t, err)
		require.Equal(t, want, string(buf[:n]), "stale queued output must not reach the new generation")
	}
}

func TestManagedPacketIOBatchDatagrams(t *testing.T) {
	endpoint, acquire := newTestManagedEndpoint(t)
	lease, err := acquire()
	require.NoError(t, err)
	defer lease.Close()
	writer := lease.(managedBatchWriterV1).WriteBatchV1
	peer := listenExternalUDP(t)
	for _, registered := range []bool{false, true} {
		if registered {
			tr := &Transport{Conn: lease}
			require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, writer))
		}
		n, err := writer([][]byte{[]byte("first"), []byte("second")}, nil, peer.LocalAddr().(*net.UDPAddr))
		require.NoError(t, err)
		require.Equal(t, 2, n)
		require.NoError(t, peer.SetReadDeadline(time.Now().Add(time.Second)))
		for _, want := range []string{"first", "second"} {
			buf := make([]byte, 32)
			n, addr, err := peer.ReadFrom(buf)
			require.NoError(t, err)
			require.Equal(t, want, string(buf[:n]))
			require.Equal(t, endpoint.LocalAddr(), addr)
		}
	}
	_, err = endpoint.(managedBatchWriterV1).WriteBatchV1(nil, nil, nil)
	require.Error(t, err, "parent cannot use lease-only batch authority")
}

func TestManagedPacketIOFailedInit(t *testing.T) {
	_, acquire := newTestManagedEndpoint(t)
	lease, err := acquire()
	require.NoError(t, err)
	outer := &faultySyscallConn{PacketConn: lease}
	tr := &Transport{Conn: outer}
	require.NoError(t, tr.ConfigureManagedPacketIOV1(outer, lease, nil))
	_, err = tr.Listen(&tls.Config{}, nil)
	require.Error(t, err)
	require.Error(t, tr.Close())
	_, err = acquire()
	require.Error(t, err)
	other := &Transport{Conn: lease}
	require.Error(t, other.ConfigureManagedPacketIOV1(lease, lease, nil), "failed init must not unseal the QUIC phase")
	require.NoError(t, lease.Close())
	next, err := acquire()
	require.NoError(t, err)
	require.NoError(t, next.Close())
}

func TestManagedPacketIORequiresOrdinaryIOJoin(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		socket := newManagedBlockingSocket()
		endpoint, acquire, err := newManagedPacketEndpoint(socket)
		require.NoError(t, err)
		defer endpoint.Close()
		lease, err := acquire()
		require.NoError(t, err)
		done := make(chan error, 1)
		go func() { _, _, err := lease.ReadFrom(make([]byte, 1)); done <- err }()
		<-socket.entered
		tr := &Transport{Conn: lease}
		require.Error(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil))
		require.NoError(t, lease.SetDeadline(time.Now().Add(-time.Second)))
		close(socket.finish)
		require.ErrorIs(t, <-done, os.ErrDeadlineExceeded)
		require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil))
		require.NoError(t, lease.Close())
	})
}

// The native message boundary deliberately keeps two interrupted submissions
// outstanding, so Close must join both before releasing a new generation.
type managedBlockingMessageSocket struct {
	*managedBlockingSocket
	writes chan struct{}
}

func (s *managedBlockingMessageSocket) WriteMsgUDP([]byte, []byte, *net.UDPAddr) (int, int, error) {
	s.writes <- struct{}{}
	<-s.interrupted
	<-s.finish
	return 0, 0, os.ErrDeadlineExceeded
}

func TestManagedPacketIOConcurrentBatchClose(t *testing.T) {
	for _, parentClose := range []bool{false, true} {
		t.Run(map[bool]string{false: "lease", true: "parent"}[parentClose], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				socket := &managedBlockingMessageSocket{managedBlockingSocket: newManagedBlockingSocket(), writes: make(chan struct{}, 2)}
				endpoint, acquire, err := newManagedPacketEndpoint(socket)
				require.NoError(t, err)
				lease, err := acquire()
				require.NoError(t, err)
				tr := &Transport{Conn: lease}
				writer := lease.(managedBatchWriterV1).WriteBatchV1
				require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, writer))
				var wg sync.WaitGroup
				results := make(chan error, 2)
				for range 2 {
					wg.Go(func() {
						_, err := writer([][]byte{[]byte("pending")}, nil, nil)
						results <- err
					})
				}
				<-socket.writes
				<-socket.writes
				closer := lease
				if parentClose {
					closer = endpoint
				}
				closed := make(chan error, 1)
				go func() { closed <- closer.Close() }()
				<-socket.interrupted
				synctest.Wait()
				select {
				case <-closed:
					t.Fatal("Close returned before batch I/O joined")
				default:
				}
				_, err = acquire()
				require.Error(t, err)
				_, err = writer(nil, nil, nil)
				require.ErrorIs(t, err, net.ErrClosed)
				close(socket.finish)
				wg.Wait()
				for range 2 {
					require.ErrorIs(t, <-results, os.ErrDeadlineExceeded)
				}
				require.NoError(t, <-closed)
				if !parentClose {
					next, err := acquire()
					require.NoError(t, err)
					_, err = writer(nil, nil, nil)
					require.ErrorIs(t, err, net.ErrClosed)
					require.NoError(t, next.Close())
				}
				require.NoError(t, endpoint.Close())
				require.NoError(t, lease.Close())
			})
		})
	}
}

type managedBatchWriterV1 interface {
	WriteBatchV1([][]byte, []byte, *net.UDPAddr) (int, error)
}

func TestManagedPacketIORegistration(t *testing.T) {
	_, acquire := newTestManagedEndpoint(t)
	lease, err := acquire()
	require.NoError(t, err)
	defer lease.Close()
	tr := &Transport{Conn: lease}
	ext, ok := any(tr).(managedPacketIOV1)
	require.True(t, ok, "managed registration must use standard types")
	writer, ok := lease.(managedBatchWriterV1)
	require.True(t, ok, "factory lease must expose its guarded batch writer")
	require.NoError(t, ext.ConfigureManagedPacketIOV1(lease, lease, writer.WriteBatchV1))
	require.Error(t, ext.ConfigureManagedPacketIOV1(lease, lease, nil))
	require.Error(t, tr.ConfigureExternalPacketIOV1(lease, false, nil))
	other := &Transport{Conn: lease}
	require.Error(t, any(other).(managedPacketIOV1).ConfigureManagedPacketIOV1(lease, lease, nil))
	require.NoError(t, tr.Close())
}

// Registration decides receive setup before transport initialization emits the
// event. Subsequent environment changes must not rewrite that decision.
func TestManagedPacketIOReceiveDiagnostics(t *testing.T) {
	for _, disabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("disabled=%t", disabled), func(t *testing.T) {
			t.Setenv("QUIC_GO_DISABLE_GRO", fmt.Sprint(disabled))
			if !disabled {
				requireManagedReceiveCoalescingHost(t)
			}
			_, acquire := newTestManagedEndpoint(t)
			lease, err := acquire()
			require.NoError(t, err)
			t.Cleanup(func() { lease.Close() })
			outer := &nonOOBPacketConn{PacketConn: lease}
			var recorder events.Recorder
			tr := &Transport{Conn: outer, Tracer: &recorder}
			require.NoError(t, tr.ConfigureManagedPacketIOV1(outer, lease, nil))
			t.Setenv("QUIC_GO_DISABLE_GRO", fmt.Sprint(!disabled))
			require.NoError(t, tr.Close())
			want := "receive_permitted=true receive_eligible=false receive_enabled=false receive_disabled_reason=unsupported_platform"
			if runtime.GOOS == "linux" || runtime.GOOS == "windows" {
				if disabled {
					want = "receive_permitted=true receive_eligible=true receive_enabled=false receive_disabled_reason=explicit_opt_out"
				} else {
					want = "receive_permitted=true receive_eligible=true receive_enabled=true receive_disabled_reason=none"
				}
			}
			require.Contains(t, externalPacketIOEvent(t, &recorder), want)
			if disabled {
				return
			}
			// An active normalizer persists into the next lease, even when the
			// environment now disables new activation attempts.
			require.NoError(t, lease.Close())
			next, err := acquire()
			require.NoError(t, err)
			t.Cleanup(func() { next.Close() })
			outer = &nonOOBPacketConn{PacketConn: next}
			var nextRecorder events.Recorder
			nextTransport := &Transport{Conn: outer, Tracer: &nextRecorder}
			require.NoError(t, nextTransport.ConfigureManagedPacketIOV1(outer, next, nil))
			require.NoError(t, nextTransport.Close())
			require.Contains(t, externalPacketIOEvent(t, &nextRecorder), want)
		})
	}
}
