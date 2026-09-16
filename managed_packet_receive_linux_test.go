package quic

import (
	"bytes"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/testutils/events"
	"github.com/stretchr/testify/require"
)

func TestManagedReceiveQueuedAcrossHandback(t *testing.T) {
	t.Run("IPv4", func(t *testing.T) { testManagedReceiveQueuedAcrossHandback(t, "udp4", "127.0.0.1") })
	t.Run("IPv6", func(t *testing.T) { testManagedReceiveQueuedAcrossHandback(t, "udp6", "::1") })
}

func testManagedReceiveQueuedAcrossHandback(t *testing.T, network, ip string) {
	t.Helper()
	t.Setenv("QUIC_GO_DISABLE_GRO", "false")
	t.Setenv("QUIC_GO_DISABLE_GSO", "false")
	endpoint, acquire, err := (&Transport{}).NewManagedPacketEndpointV1(network, &net.UDPAddr{IP: net.ParseIP(ip)})
	require.NoError(t, err)
	t.Cleanup(func() { endpoint.Close() })
	require.NoError(t, endpoint.SetDeadline(time.Now().Add(5*time.Second)))
	lease, err := acquire()
	require.NoError(t, err)
	tr := &Transport{Conn: lease}
	require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil))
	// Native option inspection is the fixture's engagement check, not a new API.
	udp := endpoint.(*managedPacketConn).endpoint.conn.(*net.UDPConn)
	require.Equal(t, 1, groSocketOption(t, udp))
	peer, err := net.ListenUDP(network, &net.UDPAddr{IP: net.ParseIP(ip)})
	require.NoError(t, err)
	t.Cleanup(func() { peer.Close() })
	segments := [][]byte{bytes.Repeat([]byte{1}, 1200), bytes.Repeat([]byte{2}, 1200), bytes.Repeat([]byte{3}, 500)}
	_, _, err = peer.WriteMsgUDP(bytes.Join(segments, nil), appendUDPSegmentSizeMsg(nil, 1200), endpoint.LocalAddr().(*net.UDPAddr))
	require.NoError(t, err)
	// No reader has consumed the aggregate: lease return must leave it decodable.
	require.NoError(t, lease.Close())
	buf := make([]byte, 65535)
	n, addr, err := endpoint.ReadFrom(buf)
	require.NoError(t, err)
	require.Equal(t, segments[0], buf[:n])
	require.Equal(t, peer.LocalAddr(), addr)
	next, err := acquire()
	require.NoError(t, err)
	defer next.Close()
	for _, want := range segments[1:] {
		n, addr, err = next.ReadFrom(buf)
		require.NoError(t, err)
		require.Equal(t, want, buf[:n])
		require.Equal(t, peer.LocalAddr(), addr)
	}
}

type managedReceiveFilter struct {
	net.PacketConn
	selected net.Addr
	filtered atomic.Int32
}

func (c *managedReceiveFilter) ReadFrom(b []byte) (int, net.Addr, error) {
	for {
		n, addr, err := c.PacketConn.ReadFrom(b)
		if err != nil || addr.String() == c.selected.String() {
			return n, addr, err
		}
		c.filtered.Add(1)
	}
}

func TestManagedReceiveDiscardsConsumedQUICStorage(t *testing.T) {
	t.Setenv("QUIC_GO_DISABLE_GRO", "false")
	endpoint, acquire := newTestManagedEndpoint(t)
	require.NoError(t, endpoint.SetReadDeadline(time.Now().Add(5*time.Second)))
	lease, err := acquire()
	require.NoError(t, err)
	defer lease.Close()
	selected, foreign := listenExternalUDP(t), listenExternalUDP(t)
	outer := &managedReceiveFilter{PacketConn: lease, selected: selected.LocalAddr()}
	tr := &Transport{Conn: outer}
	require.NoError(t, tr.ConfigureManagedPacketIOV1(outer, lease, nil))
	_, err = foreign.WriteTo([]byte("foreign"), endpoint.LocalAddr())
	require.NoError(t, err)
	segments := [][]byte{bytes.Repeat([]byte{1}, 1200), bytes.Repeat([]byte{2}, 1200), bytes.Repeat([]byte{3}, 1200)}
	_, _, err = selected.WriteMsgUDP(bytes.Join(segments, nil), appendUDPSegmentSizeMsg(nil, 1200), endpoint.LocalAddr().(*net.UDPAddr))
	require.NoError(t, err)
	buf := make([]byte, 1500)
	n, addr, err := outer.ReadFrom(buf)
	require.NoError(t, err)
	require.Equal(t, segments[0], buf[:n])
	require.Equal(t, selected.LocalAddr(), addr)
	require.EqualValues(t, 1, outer.filtered.Load())
	reader := endpoint.(*managedPacketConn).endpoint.receiver.(*oobConn)
	require.Len(t, reader.delivery.pending, 2, "native aggregate engagement must be observed")
	slab := reader.delivery.pending[0].buffer.slab
	retained := protocol.MaxCoalescedPacketBufferSize // one pending aggregate
	for _, buffer := range reader.buffers {
		if buffer != nil {
			retained += cap(buffer.Data)
		}
	}
	require.LessOrEqual(t, retained, batchSize*protocol.MaxCoalescedPacketBufferSize)
	require.NoError(t, lease.Close())
	require.True(t, slab.released(), "consumed QUIC tail must be disposed at handback")
	for _, buffer := range reader.buffers {
		require.Nil(t, buffer)
	}
	// Selected-peer policy belongs to the old wrapper, not the reusable parent.
	_, err = foreign.WriteTo([]byte("next generation"), endpoint.LocalAddr())
	require.NoError(t, err)
	n, addr, err = endpoint.ReadFrom(buf)
	require.NoError(t, err)
	require.Equal(t, "next generation", string(buf[:n]))
	require.Equal(t, foreign.LocalAddr(), addr)
}

func TestManagedReceiveRejectsTruncatedMetadata(t *testing.T) {
	endpoint, _ := newTestManagedEndpoint(t)
	reads := 0
	reader := newGROConn(t, externalGROBatchFunc(func(ms []ipv4.Message, _ int) (int, error) {
		reads++
		ms[0].N = copy(ms[0].Buffers[0], []byte("fresh"))
		ms[0].NN = 0
		ms[0].Flags = 0
		if reads == 1 {
			ms[0].Flags = unix.MSG_CTRUNC
		}
		return 1, nil
	}))
	endpoint.(*managedPacketConn).endpoint.receiver = reader
	b := make([]byte, 20)
	n, _, err := endpoint.ReadFrom(b)
	require.NoError(t, err)
	require.Equal(t, "fresh", string(b[:n]))
	require.Equal(t, 2, reads)
}

func TestManagedReceiveFailedRestoreDisposesStorage(t *testing.T) {
	socket := newManagedBlockingSocket()
	endpoint, acquire, err := newManagedPacketEndpoint(socket)
	require.NoError(t, err)
	defer endpoint.Close()
	reader := newGROConn(t, &groBatchConn{t: t, payloads: [][]byte{make([]byte, 2400)}, oobs: [][]byte{appendUDPGROMsg(nil, 1200)}, addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}})
	endpoint.(*managedPacketConn).endpoint.receiver = reader
	// An ordinary later lease still uses the persistent normalizer.
	lease, err := acquire()
	require.NoError(t, err)
	_, _, err = lease.ReadFrom(make([]byte, 1500))
	require.NoError(t, err)
	slab := reader.delivery.pending[0].buffer.slab
	socket.deadlineFailure = "read"
	require.ErrorIs(t, lease.Close(), errManagedTestDeadline)
	require.True(t, slab.released(), "terminal restoration failure must release normalized storage")
	_, err = acquire()
	require.ErrorIs(t, err, net.ErrClosed)
}

func TestManagedReceiveCloseJoinsReaders(t *testing.T) {
	for _, parentClose := range []bool{false, true} {
		t.Run(map[bool]string{false: "lease", true: "parent"}[parentClose], func(t *testing.T) {
			t.Setenv("QUIC_GO_DISABLE_GRO", "false")
			endpoint, acquire := newTestManagedEndpoint(t)
			lease, err := acquire()
			require.NoError(t, err)
			tr := &Transport{Conn: lease}
			require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil))
			results := make(chan error, 2)
			for range 2 {
				go func() { _, _, err := lease.ReadFrom(make([]byte, 1500)); results <- err }()
			}
			e := endpoint.(*managedPacketConn).endpoint
			require.Eventually(t, func() bool {
				e.mutex.Lock()
				defer e.mutex.Unlock()
				return e.active == 2
			}, 5*time.Second, time.Millisecond)
			closer := lease
			if parentClose {
				closer = endpoint
			}
			require.NoError(t, closer.Close())
			for range 2 {
				require.Error(t, <-results)
			}
			_, _, err = lease.ReadFrom(nil)
			require.ErrorIs(t, err, net.ErrClosed)
			if !parentClose {
				next, err := acquire()
				require.NoError(t, err)
				require.NoError(t, next.Close())
			}
			require.NoError(t, lease.Close())
		})
	}
}

func TestManagedReceiveDiagnostics(t *testing.T) {
	for _, disabled := range []string{"true", "false"} {
		t.Run(disabled, func(t *testing.T) {
			t.Setenv("QUIC_GO_DISABLE_GRO", disabled)
			_, acquire := newTestManagedEndpoint(t)
			lease, err := acquire()
			require.NoError(t, err)
			defer lease.Close()
			var recorder events.Recorder
			tr := &Transport{Conn: lease, Tracer: &recorder}
			require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil))
			require.NoError(t, tr.Close())
			message := managedBufferEvent(t, &recorder)
			if disabled == "true" {
				require.Contains(t, message, "receive_mode=ordinary")
				require.Contains(t, message, "coalescing=false")
			} else {
				require.Contains(t, message, "receive_mode=normalized")
				require.Contains(t, message, "coalescing=true")
			}
		})
	}
}

func TestManagedReceiveBufferedDeadline(t *testing.T) {
	t.Setenv("QUIC_GO_DISABLE_GRO", "false")
	endpoint, acquire := newTestManagedEndpoint(t)
	lease, err := acquire()
	require.NoError(t, err)
	defer lease.Close()
	tr := &Transport{Conn: lease}
	require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil))
	require.NoError(t, lease.SetReadDeadline(time.Now().Add(5*time.Second)))
	peer := listenExternalUDP(t)
	_, _, err = peer.WriteMsgUDP(bytes.Repeat([]byte{1}, 2400), appendUDPSegmentSizeMsg(nil, 1200), endpoint.LocalAddr().(*net.UDPAddr))
	require.NoError(t, err)
	buf := make([]byte, 1500)
	_, _, err = lease.ReadFrom(buf)
	require.NoError(t, err)
	require.NoError(t, lease.SetReadDeadline(time.Now().Add(-time.Second)))
	_, _, err = lease.ReadFrom(buf)
	require.ErrorIs(t, err, os.ErrDeadlineExceeded, "buffered siblings must respect logical deadlines")
	require.NoError(t, lease.SetReadDeadline(time.Time{}))
	n, _, err := lease.ReadFrom(buf)
	require.NoError(t, err)
	require.Equal(t, 1200, n, "a timeout must not consume the buffered datagram")
}
