//go:build darwin && !ios

package quic

import (
	"bytes"
	"io"
	"net"
	"os"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/stretchr/testify/require"
)

func TestDarwinManagedECNReceive(t *testing.T) {
	for _, tc := range []struct{ name, network, peerNetwork, ip string }{
		{"ipv4", "udp4", "udp4", "127.0.0.1"},
		{"ipv6", "udp6", "udp6", "::1"},
		{"mapped", "udp", "udp4", "127.0.0.1"},
		{"dual_ipv6", "udp", "udp6", "::1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			endpoint, acquire, err := (&Transport{}).NewManagedPacketEndpointV1(tc.network, nil)
			require.NoError(t, err)
			defer endpoint.Close()
			lease, err := acquire()
			require.NoError(t, err)
			defer lease.Close()
			tr := &Transport{Conn: lease}
			require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil))
			base, err := wrapConnWithManagedBuffers(lease, false, tr.packetIO.external.managedBuffers)
			require.NoError(t, err)
			conn := tr.wrapExternalPacketIO(base)
			t.Logf("family qualification: %+v", lease.(*managedPacketConn).endpoint.managedECNSetup)
			require.True(t, conn.capabilities().ECN, "managed ECN must qualify the admitted family")
			peer, err := net.ListenUDP(tc.peerNetwork, &net.UDPAddr{IP: net.ParseIP(tc.ip)})
			require.NoError(t, err)
			defer peer.Close()
			target := &net.UDPAddr{IP: net.ParseIP(tc.ip), Port: lease.LocalAddr().(*net.UDPAddr).Port}
			for _, mark := range []protocol.ECN{protocol.ECNNon, protocol.ECT0, protocol.ECT1, protocol.ECNCE} {
				oob := appendExternalECN(nil, target, mark)
				_, _, err := peer.WriteMsgUDP([]byte("marked"), oob, target)
				require.NoError(t, err)
				require.NoError(t, lease.SetReadDeadline(time.Now().Add(time.Second)))
				packet, err := conn.ReadPacket()
				require.NoError(t, err)
				require.Equal(t, "marked", string(packet.data))
				require.Equal(t, mark, packet.ecn)
				packet.buffer.Release()
			}
		})
	}
}

// This socket-boundary fixture returns real Darwin option errors from an AF_UNIX
// descriptor, without adding a production syscall hook or changing the UDP owner.
type darwinSetupSocket struct {
	*net.UDPConn
	control syscall.RawConn
	address net.Addr
}

func (c *darwinSetupSocket) SyscallConn() (syscall.RawConn, error) { return c.control, nil }
func (c *darwinSetupSocket) LocalAddr() net.Addr                   { return c.address }

type darwinSetupControl struct {
	fd  int
	err error
}

func (c darwinSetupControl) Control(f func(uintptr)) error {
	if c.err != nil {
		return c.err
	}
	f(uintptr(c.fd))
	return nil
}
func (darwinSetupControl) Read(func(uintptr) bool) error  { panic("unused") }
func (darwinSetupControl) Write(func(uintptr) bool) error { panic("unused") }

func TestDarwinManagedECNSetupFailure(t *testing.T) {
	for _, stage := range []string{"optional_ecn", "descriptor", "packet_info"} {
		t.Run(stage, func(t *testing.T) {
			endpoint, acquire, err := (&Transport{}).NewManagedPacketEndpointV1("udp", nil)
			require.NoError(t, err)
			defer endpoint.Close()
			e := endpoint.(*managedPacketConn).endpoint
			fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_DGRAM, 0)
			require.NoError(t, err)
			defer unix.Close(fd)
			ctl := darwinSetupControl{fd: fd}
			address := &net.UDPAddr{IP: net.ParseIP("::1")}
			if stage == "descriptor" {
				ctl.err = net.ErrClosed
			}
			if stage == "packet_info" {
				address.IP = net.IPv6unspecified
			}
			probe := &darwinSetupSocket{UDPConn: e.conn.(*net.UDPConn), control: ctl, address: address}
			reader, setup, setupErr := newConnWithSetup(probe, false, false)
			require.Error(t, setupErr)
			err = e.finishDarwinReceiveSetup(e.conn.(*net.UDPConn), reader, setup, setupErr)
			if stage != "optional_ecn" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err, "optional ECN failure must retain usable ordinary I/O")
			require.False(t, e.managedECN)
			require.Nil(t, e.receiver)
			lease, err := acquire()
			require.NoError(t, err)
			defer lease.Close()
			peer, err := net.ListenUDP("udp6", &net.UDPAddr{IP: net.IPv6loopback})
			require.NoError(t, err)
			defer peer.Close()
			_, err = lease.WriteTo([]byte("ordinary"), peer.LocalAddr())
			require.NoError(t, err)
			require.NoError(t, peer.SetReadDeadline(time.Now().Add(time.Second)))
			b := make([]byte, 32)
			n, _, err := peer.ReadFrom(b)
			require.NoError(t, err)
			require.Equal(t, "ordinary", string(b[:n]))
		})
	}
}

type darwinECNPeerConn struct {
	net.PacketConn
	peer  *net.UDPAddr
	batch func([][]byte, []byte, *net.UDPAddr) (int, error)
	calls int
}

func (c *darwinECNPeerConn) ReadFrom(b []byte) (int, net.Addr, error) {
	for {
		n, a, err := c.PacketConn.ReadFrom(b)
		if err != nil || sameManagedPacketAddr(a, c.peer) {
			return n, a, err
		}
	}
}

func (c *darwinECNPeerConn) WriteTo(b []byte, a net.Addr) (int, error) {
	if !sameManagedPacketAddr(a, c.peer) {
		return 0, net.ErrClosed
	}
	return c.PacketConn.WriteTo(b, a)
}

func (c *darwinECNPeerConn) sendBatch(b [][]byte, oob []byte, a *net.UDPAddr) (int, error) {
	c.calls++
	if !sameManagedPacketAddr(a, c.peer) {
		return 0, net.ErrClosed
	}
	return c.batch(b, oob, a)
}

func newDarwinECNTestPath(t *testing.T, network, peerNetwork, ip, route string) (net.PacketConn, *managedPacketConn, rawConn, *net.UDPConn, *darwinECNPeerConn) {
	t.Helper()
	endpoint, acquire, err := (&Transport{}).NewManagedPacketEndpointV1(network, nil)
	require.NoError(t, err)
	t.Cleanup(func() { endpoint.Close() })
	lease, err := acquire()
	require.NoError(t, err)
	t.Cleanup(func() { lease.Close() })
	peer, err := net.ListenUDP(peerNetwork, &net.UDPAddr{IP: net.ParseIP(ip)})
	require.NoError(t, err)
	t.Cleanup(func() { peer.Close() })
	l := lease.(*managedPacketConn)
	wrapper := &darwinECNPeerConn{PacketConn: l, peer: peer.LocalAddr().(*net.UDPAddr), batch: l.WriteBatchV1}
	conn := net.PacketConn(l)
	var batch func([][]byte, []byte, *net.UDPAddr) (int, error)
	if route == "registered" {
		batch = l.WriteBatchV1
	}
	if route == "checked" || route == "unchecked" {
		conn = wrapper
		if route == "checked" {
			batch = wrapper.sendBatch
		}
	}
	tr := &Transport{Conn: conn}
	require.NoError(t, tr.ConfigureManagedPacketIOV1(conn, l, batch))
	base, err := wrapConnWithManagedBuffers(conn, false, tr.packetIO.external.managedBuffers)
	require.NoError(t, err)
	raw := tr.wrapExternalPacketIO(base)
	t.Cleanup(func() {
		if r, ok := raw.(interface{ releaseReadBuffers() }); ok {
			r.releaseReadBuffers()
		}
	})
	return endpoint, l, raw, peer, wrapper
}

func TestDarwinManagedECNSendRoutes(t *testing.T) {
	for _, tc := range []struct{ name, network, peerNetwork, ip string }{
		{"ipv4", "udp4", "udp4", "127.0.0.1"},
		{"ipv6", "udp6", "udp6", "::1"},
		{"mapped", "udp", "udp4", "127.0.0.1"},
		{"dual_ipv6", "udp", "udp6", "::1"},
	} {
		for _, route := range []string{"direct", "registered", "checked"} {
			t.Run(tc.name+"/"+route, func(t *testing.T) {
				_, lease, conn, peer, wrapper := newDarwinECNTestPath(t, tc.network, tc.peerNetwork, tc.ip, route)
				cap := conn.capabilities()
				require.True(t, cap.ECN)
				require.False(t, cap.GSO)
				require.False(t, cap.GRO)
				require.False(t, cap.DF)
				reader, err := newConn(peer, false, false)
				require.NoError(t, err)
				reader.managedRead = true
				defer reader.releaseReadBuffers()
				sc := newSendConn(conn, peer.LocalAddr(), packetInfo{}, utils.DefaultLogger)
				for _, mark := range []protocol.ECN{protocol.ECNNon, protocol.ECT0, protocol.ECNCE} {
					require.NoError(t, sc.Write([]byte("ordinary"), 0, mark))
					require.NoError(t, peer.SetReadDeadline(time.Now().Add(time.Second)))
					p, err := reader.ReadPacket()
					require.NoError(t, err)
					require.Equal(t, mark, p.ecn)
					require.Equal(t, "ordinary", string(p.data))
					p.buffer.Release()
					if route != "direct" {
						n, err := sc.sendBatch([][]byte{[]byte("one"), []byte("two")}, mark)
						require.NoError(t, err)
						require.Equal(t, 2, n)
						for _, want := range []string{"one", "two"} {
							p, err := reader.ReadPacket()
							require.NoError(t, err)
							require.Equal(t, mark, p.ecn)
							require.Equal(t, want, string(p.data))
							p.buffer.Release()
						}
					}
				}
				if route == "checked" {
					foreign, err := net.ListenUDP(tc.peerNetwork, &net.UDPAddr{IP: net.ParseIP(tc.ip)})
					require.NoError(t, err)
					defer foreign.Close()
					sc.ChangeRemoteAddr(foreign.LocalAddr(), packetInfo{})
					require.ErrorIs(t, sc.Write([]byte("reject"), 0, protocol.ECT0), net.ErrClosed)
					n, err := sc.sendBatch([][]byte{[]byte("reject"), []byte("reject")}, protocol.ECT0)
					require.ErrorIs(t, err, net.ErrClosed)
					require.Zero(t, n)
					require.Positive(t, wrapper.calls)
					target := &net.UDPAddr{IP: net.ParseIP(tc.ip), Port: lease.LocalAddr().(*net.UDPAddr).Port}
					_, _, err = foreign.WriteMsgUDP([]byte("foreign"), appendExternalECN(nil, target, protocol.ECNCE), target)
					require.NoError(t, err)
					_, _, err = peer.WriteMsgUDP([]byte("selected"), appendExternalECN(nil, target, protocol.ECT1), target)
					require.NoError(t, err)
					require.NoError(t, lease.SetReadDeadline(time.Now().Add(time.Second)))
					p, err := conn.ReadPacket()
					require.NoError(t, err)
					require.Equal(t, "selected", string(p.data))
					require.Equal(t, protocol.ECT1, p.ecn)
					require.Equal(t, packetInfo{}, p.info)
					p.buffer.Release()
				}
			})
		}
	}
}

func TestDarwinManagedECNFamilyGate(t *testing.T) {
	for _, network := range []string{"udp4", "udp6", "udp"} {
		for _, tc := range []struct {
			name   string
			v4, v6 error
		}{
			{"both_available", nil, nil},
			{"v4_unavailable", unix.EPERM, nil},
			{"v6_unavailable", nil, unix.EPERM},
			{"both_unavailable", unix.EPERM, unix.EPERM},
		} {
			t.Run(network+"/"+tc.name, func(t *testing.T) {
				socket, err := net.ListenUDP(network, nil)
				require.NoError(t, err)
				defer socket.Close()
				q := inspectManagedECNQualification(socket, oobConnSetup{ecnIPv4Err: tc.v4, ecnIPv6Err: tc.v6})
				want := tc.v6 == nil
				if network == "udp4" {
					want = tc.v4 == nil
				}
				require.Equal(t, want, q.qualified)
				require.Equal(t, network != "udp6", q.admittedIPv4)
				require.Equal(t, network != "udp4", q.admittedIPv6)
				require.Equal(t, network == "udp", q.ipv4Mapped)
				require.Equal(t, network == "udp6", q.ipv6Only)
				require.Equal(t, want && q.admittedIPv4, q.receiveIPv4)
				require.Equal(t, want && q.admittedIPv6, q.receiveIPv6)
			})
		}
	}
}

func TestDarwinManagedECNLeaseDatagrams(t *testing.T) {
	for _, tc := range []struct{ network, peerNetwork, ip string }{
		{"udp4", "udp4", "127.0.0.1"},
		{"udp6", "udp6", "::1"},
		{"udp", "udp4", "127.0.0.1"},
		{"udp", "udp6", "::1"},
	} {
		t.Run(tc.network+"/"+tc.peerNetwork, func(t *testing.T) {
			endpoint, lease, conn, peer, _ := newDarwinECNTestPath(t, tc.network, tc.peerNetwork, tc.ip, "direct")
			require.True(t, conn.capabilities().ECN)
			require.False(t, lease.endpoint.receiveCoalescing)
			target := &net.UDPAddr{IP: net.ParseIP(tc.ip), Port: lease.LocalAddr().(*net.UDPAddr).Port}
			for _, size := range []int{0, int(protocol.MaxPacketBufferSize) + 1, 8192} {
				payload := bytes.Repeat([]byte{0xa5}, size)
				var err error
				if size == 0 {
					_, err = peer.WriteToUDP(payload, target)
				} else {
					_, _, err = peer.WriteMsgUDP(payload, appendExternalECN(nil, target, protocol.ECT0), target)
				}
				require.NoError(t, err)
				require.NoError(t, lease.SetReadDeadline(time.Now().Add(time.Second)))
				buf := make([]byte, size+1)
				n, _, err := lease.ReadFrom(buf)
				require.NoError(t, err)
				require.Equal(t, payload, buf[:n])
			}
			_, err := peer.WriteToUDP(bytes.Repeat([]byte{'x'}, 8192), target)
			require.NoError(t, err)
			_, err = peer.WriteToUDP([]byte("queued"), target)
			require.NoError(t, err)
			buf := make([]byte, 17)
			n, _, err := lease.ReadFrom(buf)
			require.NoError(t, err)
			require.Equal(t, 17, n)
			require.NoError(t, lease.Close())
			require.NoError(t, endpoint.SetReadDeadline(time.Now().Add(time.Second)))
			n, _, err = endpoint.ReadFrom(buf)
			require.NoError(t, err)
			require.Equal(t, "queued", string(buf[:n]))
			again, err := lease.endpoint.acquire()
			require.NoError(t, err)
			defer again.Close()
			tr := &Transport{Conn: again}
			require.NoError(t, tr.ConfigureManagedPacketIOV1(again, again, nil))
			base, err := wrapConnWithManagedBuffers(again, false, tr.packetIO.external.managedBuffers)
			require.NoError(t, err)
			current := tr.wrapExternalPacketIO(base)
			require.True(t, current.capabilities().ECN)
			_, _, err = peer.WriteMsgUDP([]byte("new lease"), appendExternalECN(nil, target, protocol.ECNCE), target)
			require.NoError(t, err)
			require.NoError(t, again.SetReadDeadline(time.Now().Add(time.Second)))
			p, err := current.ReadPacket()
			require.NoError(t, err)
			require.Equal(t, protocol.ECNCE, p.ecn)
			p.buffer.Release()
			_, err = conn.ReadPacket()
			require.ErrorIs(t, err, net.ErrClosed)
			_, err = conn.WritePacket([]byte("stale"), peer.LocalAddr(), nil, 0, protocol.ECT0)
			require.ErrorIs(t, err, net.ErrClosed)
			require.NoError(t, endpoint.Close())
			_, err = current.ReadPacket()
			require.ErrorIs(t, err, net.ErrClosed)
		})
	}
}

func TestDarwinManagedECNFallback(t *testing.T) {
	for _, mode := range []string{"environment", "unchecked_wrapper"} {
		t.Run(mode, func(t *testing.T) {
			route := "direct"
			if mode == "environment" {
				t.Setenv("QUIC_GO_DISABLE_ECN", "true")
			} else {
				route = "unchecked"
			}
			_, lease, conn, peer, _ := newDarwinECNTestPath(t, "udp4", "udp4", "127.0.0.1", route)
			require.False(t, conn.capabilities().ECN)
			require.False(t, conn.capabilities().GRO)
			if mode == "environment" {
				require.Nil(t, lease.endpoint.receiver)
			}
			target := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: lease.LocalAddr().(*net.UDPAddr).Port}
			_, _, err := peer.WriteMsgUDP([]byte("fallback"), appendExternalECN(nil, target, protocol.ECNCE), target)
			require.NoError(t, err)
			require.NoError(t, lease.SetReadDeadline(time.Now().Add(time.Second)))
			p, err := conn.ReadPacket()
			require.NoError(t, err)
			require.Equal(t, protocol.ECNUnsupported, p.ecn)
			require.Equal(t, "fallback", string(p.data))
			p.buffer.Release()
			n, err := conn.WritePacket([]byte("ordinary"), peer.LocalAddr(), nil, 0, protocol.ECNUnsupported)
			require.NoError(t, err)
			require.Equal(t, 8, n)
			require.NoError(t, peer.SetReadDeadline(time.Now().Add(time.Second)))
			b := make([]byte, 32)
			n, _, err = peer.ReadFrom(b)
			require.NoError(t, err)
			require.Equal(t, "ordinary", string(b[:n]))
		})
	}
}

type darwinECNWriteResult struct {
	OOBCapablePacketConn
	err   error
	short bool
	calls int
}

func (c *darwinECNWriteResult) WriteMsgUDP(b, oob []byte, _ *net.UDPAddr) (int, int, error) {
	c.calls++
	if c.err != nil {
		return 0, 0, c.err
	}
	if c.short {
		return len(b) - 1, len(oob), nil
	}
	return len(b), len(oob), nil
}

func TestDarwinManagedECNSingletonResults(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		short bool
	}{
		{name: "success"},
		{name: "permission", err: &os.SyscallError{Syscall: "sendmsg", Err: unix.EPERM}},
		{name: "message_size", err: &os.SyscallError{Syscall: "sendmsg", Err: unix.EMSGSIZE}},
		{name: "terminal", err: net.ErrClosed},
		{name: "short", short: true},
		{name: "normalized"},
		{name: "not_forwarded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, lease, conn, peer, wrapper := newDarwinECNTestPath(t, "udp4", "udp4", "127.0.0.1", "checked")
			native := lease.endpoint.managedNative.(*oobConn)
			socket := &darwinECNWriteResult{OOBCapablePacketConn: native.OOBCapablePacketConn, err: tc.err, short: tc.short}
			native.OOBCapablePacketConn = socket
			if tc.name == "normalized" {
				wrapper.batch = func(b [][]byte, oob []byte, a *net.UDPAddr) (int, error) {
					n, err := lease.WriteBatchV1(b, oob, a)
					return n + 1, err
				}
			}
			if tc.name == "not_forwarded" {
				wrapper.batch = func([][]byte, []byte, *net.UDPAddr) (int, error) { return 1, nil }
			}
			sc := newSendConn(conn, peer.LocalAddr(), packetInfo{}, utils.DefaultLogger)
			err := sc.Write([]byte("payload"), 0, protocol.ECT0)
			switch {
			case tc.err != nil:
				require.ErrorIs(t, err, tc.err)
			case tc.short:
				require.ErrorIs(t, err, io.ErrShortWrite)
			case tc.name == "normalized" || tc.name == "not_forwarded":
				require.Error(t, err)
			default:
				require.NoError(t, err)
			}
			wantCalls := 1
			if tc.name == "not_forwarded" {
				wantCalls = 0
			}
			require.Equal(t, wantCalls, socket.calls, "Darwin EPERM must remain terminal without a retry")
		})
	}
	t.Run("native_message_size", func(t *testing.T) {
		_, _, conn, peer, _ := newDarwinECNTestPath(t, "udp4", "udp4", "127.0.0.1", "checked")
		n, err := conn.WritePacket(make([]byte, 65536), peer.LocalAddr(), nil, 0, protocol.ECT0)
		require.True(t, isSendMsgSizeErr(err))
		require.Zero(t, n)
	})
}

type darwinECNReadMetadata struct {
	OOBCapablePacketConn
	peer  *net.UDPAddr
	calls int
}

func (c *darwinECNReadMetadata) ReadMsgUDP(b, oob []byte) (int, int, int, *net.UDPAddr, error) {
	c.calls++
	var control []byte
	switch c.calls {
	case 1:
		control = lifetimeControlMessage(unix.IPPROTO_IP, msgTypeIPTOS, []byte{2})
	case 2:
		control = []byte{1}
	}
	return copy(b, "metadata"), copy(oob, control), 0, c.peer, nil
}

func TestDarwinManagedECNMissingMetadata(t *testing.T) {
	for _, route := range []string{"direct", "checked"} {
		t.Run(route, func(t *testing.T) {
			_, lease, conn, peer, _ := newDarwinECNTestPath(t, "udp4", "udp4", "127.0.0.1", route)
			native := lease.endpoint.managedNative.(*oobConn)
			socket := &darwinECNReadMetadata{OOBCapablePacketConn: native.OOBCapablePacketConn, peer: peer.LocalAddr().(*net.UDPAddr)}
			native.OOBCapablePacketConn = socket
			p, err := conn.ReadPacket()
			require.NoError(t, err)
			require.Equal(t, protocol.ECT0, p.ecn)
			p.buffer.Release()
			p, err = conn.ReadPacket()
			require.NoError(t, err)
			require.Equal(t, protocol.ECNUnsupported, p.ecn)
			p.buffer.Release()
			require.Equal(t, 3, socket.calls, "malformed metadata must be dropped before the unmarked datagram")
		})
	}
}

func TestDarwinManagedECNCloseJoinsRead(t *testing.T) {
	for _, owner := range []string{"lease", "endpoint"} {
		t.Run(owner, func(t *testing.T) {
			endpoint, lease, conn, _, _ := newDarwinECNTestPath(t, "udp4", "udp4", "127.0.0.1", "checked")
			done := make(chan error, 1)
			go func() {
				p, err := conn.ReadPacket()
				if p.buffer != nil {
					p.buffer.Release()
				}
				done <- err
			}()
			// Both the correlation owner and nested lease read have entered I/O.
			require.Eventually(t, func() bool {
				lease.endpoint.mutex.Lock()
				defer lease.endpoint.mutex.Unlock()
				return lease.endpoint.active == 2
			}, time.Second, time.Millisecond)
			if owner == "lease" {
				require.NoError(t, lease.Close())
			} else {
				require.NoError(t, endpoint.Close())
			}
			// Lease revocation interrupts native I/O with its deadline error.
			require.Error(t, <-done)
			_, err := conn.ReadPacket()
			require.ErrorIs(t, err, net.ErrClosed)
			lease.endpoint.mutex.Lock()
			active, read := lease.endpoint.active, lease.endpoint.managedRead
			lease.endpoint.mutex.Unlock()
			require.Zero(t, active)
			require.Nil(t, read)
		})
	}
}
