package quic

import (
	"bytes"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
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
	// Public callers own the returned mutable address. It must not remain
	// shared with a buffered sibling consumed by another view/generation.
	returned := addr.(*net.UDPAddr)
	returned.Port = 1
	returned.IP[0] ^= 0xff
	returned.Zone = "changed"
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

func TestManagedReceiveRetainedDiagnostics(t *testing.T) {
	for _, useLease := range []bool{false, true} {
		t.Run(map[bool]string{false: "parent", true: "next lease"}[useLease], func(t *testing.T) {
			t.Setenv("QUIC_GO_DISABLE_GRO", "false")
			endpoint, acquire := newTestManagedEndpoint(t)
			lease, err := acquire()
			require.NoError(t, err)
			require.NoError(t, (&Transport{Conn: lease}).ConfigureManagedPacketIOV1(lease, lease, nil))
			require.NoError(t, lease.Close())
			conn := endpoint
			if useLease {
				conn, err = acquire()
				require.NoError(t, err)
				defer conn.Close()
			}
			var recorder events.Recorder
			tr := &Transport{Conn: conn, Tracer: &recorder}
			require.NoError(t, tr.Close())
			message := managedBufferEvent(t, &recorder)
			require.Contains(t, message, "receive_mode=normalized")
			require.Contains(t, message, "coalescing=true")
		})
	}
	t.Run("inactive setup retries on the next lease", func(t *testing.T) {
		t.Setenv("QUIC_GO_DISABLE_ECN", "true")
		t.Setenv("QUIC_GO_DISABLE_GRO", "true")
		endpoint, acquire := newTestManagedEndpoint(t)
		lease, err := acquire()
		require.NoError(t, err)
		require.NoError(t, (&Transport{Conn: lease}).ConfigureManagedPacketIOV1(lease, lease, nil))
		e := endpoint.(*managedPacketConn).endpoint
		require.Nil(t, e.receiver)
		require.Nil(t, e.managedNative)
		require.NoError(t, lease.Close())

		t.Setenv("QUIC_GO_DISABLE_ECN", "false")
		t.Setenv("QUIC_GO_DISABLE_GRO", "false")
		next, err := acquire()
		require.NoError(t, err)
		defer next.Close()
		require.NoError(t, (&Transport{Conn: next}).ConfigureManagedPacketIOV1(next, next, nil))
		require.True(t, e.receiveCoalescing)
		require.True(t, e.managedECN)
		require.NotNil(t, e.receiver)
		require.NotNil(t, e.managedNative)
	})
	t.Run("retained normalizer gains ECN without replacement", func(t *testing.T) {
		t.Setenv("QUIC_GO_DISABLE_ECN", "true")
		t.Setenv("QUIC_GO_DISABLE_GRO", "false")
		endpoint, acquire := newTestManagedEndpoint(t)
		lease, err := acquire()
		require.NoError(t, err)
		require.NoError(t, (&Transport{Conn: lease}).ConfigureManagedPacketIOV1(lease, lease, nil))
		e := endpoint.(*managedPacketConn).endpoint
		require.True(t, e.receiveCoalescing)
		retained := e.receiver
		require.NotNil(t, retained)
		require.Nil(t, e.managedNative)
		require.NoError(t, lease.Close())

		t.Setenv("QUIC_GO_DISABLE_ECN", "false")
		next, err := acquire()
		require.NoError(t, err)
		defer next.Close()
		require.NoError(t, (&Transport{Conn: next}).ConfigureManagedPacketIOV1(next, next, nil))
		require.Same(t, retained, e.receiver)
		require.Same(t, retained, e.managedNative)
		require.True(t, e.managedECN)
	})
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

func TestManagedECNReceiveMarks(t *testing.T) {
	for _, family := range []struct {
		name            string
		endpointNetwork string
		endpointIP      net.IP
		senderNetwork   string
		senderIP        net.IP
		setMark         func(*testing.T, uintptr, protocol.ECN)
	}{
		{name: "IPv4", endpointNetwork: "udp4", endpointIP: net.IPv4(127, 0, 0, 1), senderNetwork: "udp4", senderIP: net.IPv4(127, 0, 0, 1), setMark: func(t *testing.T, fd uintptr, ecn protocol.ECN) {
			require.NoError(t, unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_TOS, int(ecn.ToHeaderBits())))
		}},
		{name: "IPv6", endpointNetwork: "udp6", endpointIP: net.IPv6loopback, senderNetwork: "udp6", senderIP: net.IPv6loopback, setMark: func(t *testing.T, fd uintptr, ecn protocol.ECN) {
			require.NoError(t, unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_TCLASS, int(ecn.ToHeaderBits())))
		}},
		{name: "dual stack IPv4 mapped", endpointNetwork: "udp", endpointIP: net.IPv6zero, senderNetwork: "udp4", senderIP: net.IPv4(127, 0, 0, 1), setMark: func(t *testing.T, fd uintptr, ecn protocol.ECN) {
			require.NoError(t, unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_TOS, int(ecn.ToHeaderBits())))
		}},
	} {
		for _, wrapped := range []bool{false, true} {
			t.Run(family.name+map[bool]string{false: "/direct", true: "/wrapped"}[wrapped], func(t *testing.T) {
				t.Setenv("QUIC_GO_DISABLE_ECN", "false")
				t.Setenv("QUIC_GO_DISABLE_GRO", "true")
				endpoint, acquire, err := (&Transport{}).NewManagedPacketEndpointV1(family.endpointNetwork, &net.UDPAddr{IP: family.endpointIP})
				require.NoError(t, err)
				defer endpoint.Close()
				lease, err := acquire()
				require.NoError(t, err)
				defer lease.Close()
				destination := *endpoint.LocalAddr().(*net.UDPAddr)
				destination.IP = family.senderIP
				sender, err := net.DialUDP(family.senderNetwork, nil, &destination)
				require.NoError(t, err)
				defer sender.Close()
				conn := lease
				var callback func([][]byte, []byte, *net.UDPAddr) (int, error)
				if wrapped {
					filter := &managedPeerFilter{PacketConn: lease, peer: sender.LocalAddr().(*net.UDPAddr), rejectedReads: make(chan struct{}, 1), batch: lease.(managedBatchWriterV1).WriteBatchV1}
					conn, callback = filter, filter.sendBatch
				}
				tr := &Transport{Conn: conn}
				require.NoError(t, tr.ConfigureManagedPacketIOV1(conn, lease, callback))
				rawConn := tr.wrapExternalPacketIO(&basicConn{PacketConn: conn})
				require.True(t, rawConn.capabilities().ECN)
				require.False(t, rawConn.capabilities().GRO)

				raw, err := sender.SyscallConn()
				require.NoError(t, err)
				for _, mark := range []protocol.ECN{protocol.ECNNon, protocol.ECT0, protocol.ECT1, protocol.ECNCE} {
					require.NoError(t, raw.Control(func(fd uintptr) { family.setMark(t, fd, mark) }))
					_, err = sender.Write([]byte(mark.String()))
					require.NoError(t, err)
					packet, err := rawConn.ReadPacket()
					require.NoError(t, err)
					require.Equal(t, mark.String(), string(packet.data))
					require.Equal(t, mark, packet.ecn)
					packet.buffer.Release()
				}
			})
		}
	}
}

func TestManagedECNNonGROPreservesDatagramsAcrossRelease(t *testing.T) {
	t.Setenv("QUIC_GO_DISABLE_ECN", "false")
	t.Setenv("QUIC_GO_DISABLE_GRO", "true")
	endpoint, acquire := newTestManagedEndpoint(t)
	lease, err := acquire()
	require.NoError(t, err)
	tr := &Transport{Conn: lease}
	require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil))
	e := endpoint.(*managedPacketConn).endpoint
	require.True(t, e.managedECN)
	require.False(t, e.receiveCoalescing)
	require.True(t, e.receiver.(*oobConn).managedRead)

	peer := listenExternalUDP(t)
	first := bytes.Repeat([]byte{0x5a}, 65507)
	_, err = peer.WriteTo(first, endpoint.LocalAddr())
	require.NoError(t, err)
	_, err = peer.WriteTo([]byte("next generation"), endpoint.LocalAddr())
	require.NoError(t, err)
	buffer := make([]byte, 65535)
	n, _, err := lease.ReadFrom(buffer)
	require.NoError(t, err)
	require.Equal(t, first, buffer[:n])
	require.NoError(t, lease.Close())

	next, err := acquire()
	require.NoError(t, err)
	defer next.Close()
	n, _, err = next.ReadFrom(buffer)
	require.NoError(t, err)
	require.Equal(t, "next generation", string(buffer[:n]))
}

func TestManagedECNSendsMarksThroughDirectAndCheckedRoutes(t *testing.T) {
	for _, family := range []struct {
		name            string
		endpointNetwork string
		endpointAddr    *net.UDPAddr
		peerNetwork     string
		peerAddr        *net.UDPAddr
	}{
		{name: "IPv4", endpointNetwork: "udp4", endpointAddr: &net.UDPAddr{IP: net.IPv4zero}, peerNetwork: "udp4", peerAddr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)}},
		{name: "IPv6", endpointNetwork: "udp6", endpointAddr: &net.UDPAddr{IP: net.IPv6zero}, peerNetwork: "udp6", peerAddr: &net.UDPAddr{IP: net.IPv6loopback}},
		{name: "dual stack IPv4 mapped", endpointNetwork: "udp", endpointAddr: &net.UDPAddr{IP: net.IPv6zero}, peerNetwork: "udp4", peerAddr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)}},
	} {
		for _, wrapped := range []bool{false, true} {
			t.Run(family.name+map[bool]string{false: "/direct", true: "/wrapped"}[wrapped], func(t *testing.T) {
				t.Setenv("QUIC_GO_DISABLE_ECN", "false")
				t.Setenv("QUIC_GO_DISABLE_GRO", "true")
				endpoint, acquire, err := (&Transport{}).NewManagedPacketEndpointV1(family.endpointNetwork, family.endpointAddr)
				require.NoError(t, err)
				defer endpoint.Close()
				lease, err := acquire()
				require.NoError(t, err)
				defer lease.Close()
				selected, err := net.ListenUDP(family.peerNetwork, family.peerAddr)
				require.NoError(t, err)
				defer selected.Close()
				foreign, err := net.ListenUDP(family.peerNetwork, family.peerAddr)
				require.NoError(t, err)
				defer foreign.Close()
				conn := lease
				callback := lease.(managedBatchWriterV1).WriteBatchV1
				if wrapped {
					filter := &managedPeerFilter{PacketConn: lease, peer: selected.LocalAddr().(*net.UDPAddr), rejectedReads: make(chan struct{}, 1), batch: lease.(managedBatchWriterV1).WriteBatchV1}
					conn, callback = filter, filter.sendBatch
				}
				tr := &Transport{Conn: conn}
				require.NoError(t, tr.ConfigureManagedPacketIOV1(conn, lease, callback))
				rawConn := tr.wrapExternalPacketIO(&basicConn{PacketConn: conn})
				require.True(t, rawConn.capabilities().ECN)
				sc := newSendConn(rawConn, selected.LocalAddr(), packetInfo{}, utils.DefaultLogger)
				receiver, err := newConn(selected, false, false)
				require.NoError(t, err)
				defer receiver.releaseReadBuffers()

				for _, mark := range []protocol.ECN{protocol.ECNNon, protocol.ECT0, protocol.ECT1, protocol.ECNCE} {
					require.NoError(t, sc.Write([]byte(mark.String()), 0, mark))
					packet, err := receiver.ReadPacket()
					require.NoError(t, err)
					require.Equal(t, mark, packet.ecn)
					packet.buffer.Release()
				}
				if wrapped {
					sc.ChangeRemoteAddr(foreign.LocalAddr(), packetInfo{})
					require.ErrorContains(t, sc.Write([]byte("foreign"), 0, protocol.ECT0), "foreign peer")
					sc.ChangeRemoteAddr(selected.LocalAddr(), packetInfo{})
				}
				n, err := sc.sendBatch([][]byte{[]byte("batch one"), []byte("batch two")}, protocol.ECNCE)
				require.NoError(t, err)
				require.Equal(t, 2, n)
				for range 2 {
					packet, err := receiver.ReadPacket()
					require.NoError(t, err)
					require.Equal(t, protocol.ECNCE, packet.ecn)
					packet.buffer.Release()
				}
			})
		}
	}
}

func TestManagedECNQualificationMatchesAdmittedFamilies(t *testing.T) {
	t.Setenv("QUIC_GO_DISABLE_ECN", "false")
	t.Setenv("QUIC_GO_DISABLE_GRO", "true")
	for _, tc := range []struct {
		name     string
		network  string
		addr     *net.UDPAddr
		wantIPv4 bool
		wantIPv6 bool
		mapped   bool
	}{
		{name: "IPv4", network: "udp4", addr: &net.UDPAddr{IP: net.IPv4zero}, wantIPv4: true},
		{name: "IPv6", network: "udp6", addr: &net.UDPAddr{IP: net.IPv6zero}, wantIPv6: true},
		{name: "dual stack", network: "udp", addr: &net.UDPAddr{IP: net.IPv6zero}, wantIPv4: true, wantIPv6: true, mapped: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			endpoint, acquire, err := (&Transport{}).NewManagedPacketEndpointV1(tc.network, tc.addr)
			require.NoError(t, err)
			defer endpoint.Close()
			lease, err := acquire()
			require.NoError(t, err)
			defer lease.Close()
			tr := &Transport{Conn: lease}
			require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil))
			e := endpoint.(*managedPacketConn).endpoint
			q := e.managedECNSetup
			require.True(t, q.qualified)
			require.Equal(t, tc.wantIPv4, q.admittedIPv4)
			require.Equal(t, tc.wantIPv6, q.admittedIPv6)
			require.Equal(t, tc.mapped, q.ipv4Mapped)
			require.Equal(t, tc.wantIPv6 && !tc.mapped, q.ipv6Only)
			require.Equal(t, tc.wantIPv4, q.receiveIPv4)
			require.Equal(t, tc.wantIPv6, q.receiveIPv6)
			require.Empty(t, q.failedFamily)
			require.True(t, tr.wrapExternalPacketIO(&basicConn{PacketConn: lease}).capabilities().ECN)
		})
	}
	t.Run("opt out is not a family failure", func(t *testing.T) {
		t.Setenv("QUIC_GO_DISABLE_ECN", "true")
		t.Setenv("QUIC_GO_DISABLE_GRO", "true")
		endpoint, acquire, err := (&Transport{}).NewManagedPacketEndpointV1("udp4", &net.UDPAddr{IP: net.IPv4zero})
		require.NoError(t, err)
		defer endpoint.Close()
		lease, err := acquire()
		require.NoError(t, err)
		defer lease.Close()
		var recorder events.Recorder
		tr := &Transport{Conn: lease, Tracer: &recorder}
		require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil))
		e := endpoint.(*managedPacketConn).endpoint
		q := e.managedECNSetup
		require.False(t, q.qualified)
		require.True(t, q.disabled)
		require.False(t, q.kernelUnsupported)
		require.Empty(t, q.failedFamily)
		require.Nil(t, e.receiver)
		require.Nil(t, e.managedNative)
		require.Nil(t, tr.packetIO.external.managedRawFactory)
		require.NoError(t, tr.Close())
		message := managedBufferEvent(t, &recorder)
		require.Contains(t, message, "ecn=false")
		require.Contains(t, message, "ecn_disabled=true")
		require.Contains(t, message, "ecn_kernel_unsupported=false")
		require.Contains(t, message, "ecn_failed_family=\"\"")
	})
}

func TestManagedECNCheckedSingletonPreservesLinuxSendErrors(t *testing.T) {
	permissionErr := &os.SyscallError{Syscall: "sendmsg", Err: unix.EPERM}
	native := &managedECNFixtureConn{writeResults: []error{permissionErr, nil}}
	lease := &managedPacketLease{done: make(chan struct{})}
	endpoint := &managedPacketEndpoint{managedNative: native, managedECN: true, lease: lease}
	endpoint.idle = sync.NewCond(&endpoint.mutex)
	conn := &managedPacketConn{endpoint: endpoint, lease: lease}
	callback := conn.WriteBatchV1
	config := &externalPacketIO{sendBatch: callback}
	raw := newManagedPacketRawConn(&managedECNFixtureConn{}, conn, config, false)
	sc := newSendConn(raw, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4242}, packetInfo{}, utils.DefaultLogger)
	require.NoError(t, sc.Write([]byte("retry once"), 0, protocol.ECT0))
	require.Len(t, native.writes, 2)

	messageSizeErr := &os.SyscallError{Syscall: "sendmsg", Err: unix.EMSGSIZE}
	native.writeResult = messageSizeErr
	_, err := raw.WritePacket([]byte("too large"), sc.RemoteAddr(), nil, 0, protocol.ECT0)
	require.Same(t, messageSizeErr, err)
}
