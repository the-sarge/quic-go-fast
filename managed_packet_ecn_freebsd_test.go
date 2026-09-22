//go:build freebsd

package quic

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/quic-go/quic-go/internal/utils"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

type freeBSDManagedECNFamily struct {
	name, network, peerNetwork string
	ip, peerIP                 net.IP
}

func freeBSDManagedECNFamilies() []freeBSDManagedECNFamily {
	return []freeBSDManagedECNFamily{
		{"IPv4", "udp4", "udp4", net.IPv4zero, net.IPv4(127, 0, 0, 1)},
		{"IPv6", "udp6", "udp6", net.IPv6zero, net.IPv6loopback},
		{"dual IPv4", "udp", "udp4", net.IPv6zero, net.IPv4(127, 0, 0, 1)},
		{"dual IPv6", "udp", "udp6", net.IPv6zero, net.IPv6loopback},
	}
}

func newFreeBSDManagedECN(t *testing.T, family freeBSDManagedECNFamily, wrapped bool) (net.PacketConn, func() (net.PacketConn, error), net.PacketConn, rawConn, *net.UDPConn) {
	t.Helper()
	endpoint, acquire, err := (&Transport{}).NewManagedPacketEndpointV1(family.network, &net.UDPAddr{IP: family.ip})
	require.NoError(t, err)
	t.Cleanup(func() { endpoint.Close() })
	lease, err := acquire()
	require.NoError(t, err)
	t.Cleanup(func() { lease.Close() })
	require.NoError(t, lease.SetDeadline(time.Now().Add(5*time.Second)))
	peer, err := net.ListenUDP(family.peerNetwork, &net.UDPAddr{IP: family.peerIP})
	require.NoError(t, err)
	t.Cleanup(func() { peer.Close() })
	require.NoError(t, peer.SetDeadline(time.Now().Add(5*time.Second)))
	conn := lease
	callback := lease.(managedBatchWriterV1).WriteBatchV1
	if wrapped {
		filter := &managedPeerFilter{PacketConn: lease, peer: peer.LocalAddr().(*net.UDPAddr), batch: callback, rejectedReads: make(chan struct{}, 1)}
		conn, callback = filter, filter.sendBatch
	}
	tr := &Transport{Conn: conn}
	require.NoError(t, tr.ConfigureManagedPacketIOV1(conn, lease, callback))
	raw := tr.wrapExternalPacketIO(&basicConn{PacketConn: conn})
	t.Cleanup(func() {
		if r, ok := raw.(interface{ releaseReadBuffers() }); ok {
			r.releaseReadBuffers()
		}
	})
	require.True(t, raw.capabilities().ECN)
	require.False(t, raw.capabilities().GRO)
	require.False(t, raw.capabilities().GSO)
	e := endpoint.(*managedPacketConn).endpoint
	require.False(t, e.receiveCoalescing)
	t.Logf("family=%s qualification=%+v", family.name, e.managedECNSetup)
	return endpoint, acquire, lease, raw, peer
}

func TestManagedFreeBSDECNReceive(t *testing.T) {
	t.Setenv("QUIC_GO_DISABLE_ECN", "false")
	for _, family := range freeBSDManagedECNFamilies() {
		for _, wrapped := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/wrapped=%t", family.name, wrapped), func(t *testing.T) {
				endpoint, _, _, raw, peer := newFreeBSDManagedECN(t, family, wrapped)
				destination := &net.UDPAddr{IP: family.peerIP, Port: endpoint.LocalAddr().(*net.UDPAddr).Port}
				if wrapped {
					foreign, err := net.DialUDP(family.peerNetwork, nil, destination)
					require.NoError(t, err)
					_, err = foreign.Write([]byte("foreign"))
					require.NoError(t, err)
					require.NoError(t, foreign.Close())
				}
				fd, err := peer.SyscallConn()
				require.NoError(t, err)
				for _, mark := range []protocol.ECN{protocol.ECNNon, protocol.ECT0, protocol.ECT1, protocol.ECNCE} {
					require.NoError(t, fd.Control(func(fd uintptr) {
						if family.peerNetwork == "udp4" {
							require.NoError(t, unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_TOS, int(mark.ToHeaderBits())))
						} else {
							require.NoError(t, unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_TCLASS, int(mark.ToHeaderBits())))
						}
					}))
					_, err = peer.WriteToUDP([]byte(mark.String()), destination)
					require.NoError(t, err)
					p, err := raw.ReadPacket()
					require.NoError(t, err)
					require.Equal(t, mark, p.ecn)
					require.Equal(t, mark.String(), string(p.data))
					require.True(t, sameManagedPacketAddr(peer.LocalAddr(), p.remoteAddr))
					p.buffer.Release()
				}
			})
		}
	}
}

// Characterize the kernel-owned representation independently of the managed
// capability gate: socket option success alone cannot qualify a family.
func TestManagedFreeBSDNativeRepresentation(t *testing.T) {
	for _, tc := range []struct {
		name, network, peerNetwork string
		ip, peerIP                 net.IP
	}{
		{"IPv4", "udp4", "udp4", net.IPv4zero, net.IPv4(127, 0, 0, 1)},
		{"IPv6", "udp6", "udp6", net.IPv6zero, net.IPv6loopback},
		{"dual IPv4", "udp", "udp4", net.IPv6zero, net.IPv4(127, 0, 0, 1)},
		{"dual IPv6", "udp", "udp6", net.IPv6zero, net.IPv6loopback},
	} {
		t.Run(tc.name, func(t *testing.T) {
			socket, err := net.ListenUDP(tc.network, &net.UDPAddr{IP: tc.ip})
			require.NoError(t, err)
			defer socket.Close()
			require.NoError(t, socket.SetDeadline(time.Now().Add(5*time.Second)))
			reader, setup, err := newConnWithSetup(socket, false, false)
			require.NoError(t, err)
			defer reader.releaseReadBuffers()
			reader.managedRead = true
			q := inspectManagedECNQualification(socket, setup)
			t.Logf("IP_RECVTOS=%v IPV6_RECVTCLASS=%v domain=%+v", setup.ecnIPv4Err, setup.ecnIPv6Err, q)
			peer, err := net.ListenUDP(tc.peerNetwork, &net.UDPAddr{IP: tc.peerIP})
			require.NoError(t, err)
			defer peer.Close()
			require.NoError(t, peer.SetDeadline(time.Now().Add(5*time.Second)))
			receiver, err := newConn(peer, false, false)
			require.NoError(t, err)
			defer receiver.releaseReadBuffers()
			receiver.managedRead = true
			destination := &net.UDPAddr{IP: tc.peerIP, Port: socket.LocalAddr().(*net.UDPAddr).Port}
			for _, mark := range []protocol.ECN{protocol.ECNNon, protocol.ECT0, protocol.ECT1, protocol.ECNCE} {
				t.Run("receive "+mark.String(), func(t *testing.T) {
					fd, err := peer.SyscallConn()
					require.NoError(t, err)
					require.NoError(t, fd.Control(func(fd uintptr) {
						if tc.peerNetwork == "udp4" {
							require.NoError(t, unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_TOS, int(mark.ToHeaderBits())))
						} else {
							require.NoError(t, unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_TCLASS, int(mark.ToHeaderBits())))
						}
					}))
					_, err = peer.WriteToUDP([]byte("native receive"), destination)
					require.NoError(t, err)
					p, err := reader.ReadPacket()
					require.NoError(t, err)
					defer p.buffer.Release()
					require.Equal(t, mark, p.ecn)
				})
				t.Run("send "+mark.String(), func(t *testing.T) {
					n, err := reader.WritePacket([]byte("native send"), peer.LocalAddr(), nil, 0, mark)
					require.NoError(t, err)
					require.Equal(t, len("native send"), n)
					p, err := receiver.ReadPacket()
					require.NoError(t, err)
					defer p.buffer.Release()
					require.Equal(t, mark, p.ecn)
				})
			}
			for _, count := range []int{1, 2} {
				t.Run(fmt.Sprintf("ordinary batch %d", count), func(t *testing.T) {
					ep, acquire, err := newManagedPacketEndpoint(socket)
					require.NoError(t, err)
					_ = ep
					lease, err := acquire()
					require.NoError(t, err)
					defer lease.Close()
					bufs := make([][]byte, count)
					for i := range bufs {
						bufs[i] = []byte("native batch")
					}
					addr := peer.LocalAddr().(*net.UDPAddr)
					n, err := lease.(managedBatchWriterV1).WriteBatchV1(bufs, appendExternalECN(nil, addr, protocol.ECNCE), addr)
					require.NoError(t, err)
					require.Equal(t, count, n)
					for range count {
						p, err := receiver.ReadPacket()
						require.NoError(t, err)
						require.Equal(t, protocol.ECNCE, p.ecn)
						p.buffer.Release()
					}
				})
			}
		})
	}
}

func TestManagedFreeBSDECNSend(t *testing.T) {
	t.Setenv("QUIC_GO_DISABLE_ECN", "false")
	for _, family := range freeBSDManagedECNFamilies() {
		for _, wrapped := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/wrapped=%t", family.name, wrapped), func(t *testing.T) {
				_, _, _, raw, peer := newFreeBSDManagedECN(t, family, wrapped)
				receiver, err := newConn(peer, false, false)
				require.NoError(t, err)
				defer receiver.releaseReadBuffers()
				receiver.managedRead = true
				sc := newSendConn(raw, peer.LocalAddr(), packetInfo{}, utils.DefaultLogger)
				for _, mark := range []protocol.ECN{protocol.ECNNon, protocol.ECT0, protocol.ECT1, protocol.ECNCE} {
					require.NoError(t, sc.Write([]byte(mark.String()), 0, mark))
					p, err := receiver.ReadPacket()
					require.NoError(t, err)
					require.Equal(t, mark, p.ecn)
					require.Equal(t, mark.String(), string(p.data))
					p.buffer.Release()
				}
				n, err := sc.sendBatch([][]byte{[]byte("one"), []byte("two")}, protocol.ECNCE)
				require.NoError(t, err)
				require.Equal(t, 2, n)
				for _, want := range []string{"one", "two"} {
					p, err := receiver.ReadPacket()
					require.NoError(t, err)
					require.Equal(t, protocol.ECNCE, p.ecn)
					require.Equal(t, want, string(p.data))
					p.buffer.Release()
				}
				n, err = sc.sendBatch([][]byte{[]byte("prefix"), make([]byte, 65536), []byte("unsent")}, protocol.ECT1)
				require.ErrorIs(t, err, unix.EMSGSIZE)
				require.Equal(t, 1, n)
				p, err := receiver.ReadPacket()
				require.NoError(t, err)
				require.Equal(t, "prefix", string(p.data))
				require.Equal(t, protocol.ECT1, p.ecn)
				p.buffer.Release()
				if wrapped {
					foreign, err := net.ListenUDP(family.peerNetwork, &net.UDPAddr{IP: family.peerIP})
					require.NoError(t, err)
					defer foreign.Close()
					sc.ChangeRemoteAddr(foreign.LocalAddr(), packetInfo{})
					require.ErrorContains(t, sc.Write([]byte("foreign"), 0, protocol.ECT0), "foreign peer")
					n, err = sc.sendBatch([][]byte{[]byte("foreign")}, protocol.ECT0)
					require.ErrorContains(t, err, "foreign peer")
					require.Zero(t, n)
				}
			})
		}
	}
}

func TestManagedFreeBSDFullDatagramsAndRelease(t *testing.T) {
	t.Setenv("QUIC_GO_DISABLE_ECN", "false")
	for _, family := range freeBSDManagedECNFamilies() {
		t.Run(family.name, func(t *testing.T) {
			endpoint, acquire, lease, raw, peer := newFreeBSDManagedECN(t, family, false)
			destination := &net.UDPAddr{IP: family.peerIP, Port: endpoint.LocalAddr().(*net.UDPAddr).Port}
			for _, size := range []int{0, 1452, 2000, 65507} {
				payload := bytes.Repeat([]byte{0x5a}, size)
				_, err := peer.WriteToUDP(payload, destination)
				require.NoError(t, err)
				buffer := make([]byte, 65535)
				n, _, err := lease.ReadFrom(buffer)
				require.NoError(t, err)
				require.Equal(t, payload, buffer[:n])
			}
			_, err := peer.WriteToUDP([]byte("truncate this datagram"), destination)
			require.NoError(t, err)
			b := make([]byte, 4)
			n, _, err := lease.ReadFrom(b)
			require.NoError(t, err)
			require.Equal(t, 4, n)
			require.Equal(t, "trun", string(b))
			_, err = peer.WriteToUDP([]byte("across release"), destination)
			require.NoError(t, err)
			require.NoError(t, lease.Close())
			_, err = raw.ReadPacket()
			require.ErrorIs(t, err, net.ErrClosed)
			_, err = raw.WritePacket([]byte("stale"), peer.LocalAddr(), nil, 0, protocol.ECT0)
			require.ErrorIs(t, err, net.ErrClosed)
			next, err := acquire()
			require.NoError(t, err)
			defer next.Close()
			require.NoError(t, next.SetReadDeadline(time.Now().Add(5*time.Second)))
			buffer := make([]byte, 65535)
			n, _, err = next.ReadFrom(buffer)
			require.NoError(t, err)
			require.Equal(t, "across release", string(buffer[:n]))
			tr := &Transport{Conn: next}
			require.NoError(t, tr.ConfigureManagedPacketIOV1(next, next, nil))
			require.True(t, tr.wrapExternalPacketIO(&basicConn{PacketConn: next}).capabilities().ECN)
			require.NoError(t, endpoint.Close())
			_, err = acquire()
			require.ErrorIs(t, err, net.ErrClosed)
		})
	}
}

func TestManagedFreeBSDSetupFallback(t *testing.T) {
	for _, family := range freeBSDManagedECNFamilies() {
		for _, failure := range []string{"opt out", "IPv4 setup", "IPv6 setup", "both denied", "descriptor"} {
			t.Run(family.name+"/"+failure, func(t *testing.T) {
				t.Setenv("QUIC_GO_DISABLE_ECN", fmt.Sprint(failure == "opt out"))
				endpoint, acquire, err := (&Transport{}).NewManagedPacketEndpointV1(family.network, &net.UDPAddr{IP: family.ip})
				require.NoError(t, err)
				defer endpoint.Close()
				e := endpoint.(*managedPacketConn).endpoint
				socket := e.conn.(*net.UDPConn)
				reader, setup, err := newConnWithSetup(socket, false, false)
				require.NoError(t, err)
				defer reader.releaseReadBuffers()
				var setupErr error
				switch failure {
				case "IPv4 setup":
					setup.ecnIPv4Err = unix.EPERM
				case "IPv6 setup":
					setup.ecnIPv6Err = unix.EPERM
				case "both denied":
					setup = oobConnSetup{ecnIPv4Err: unix.EPERM, ecnIPv6Err: unix.EPERM, ecnUnavailable: true}
					setupErr = errECNSetupDenied
				case "descriptor":
					setupErr = net.ErrClosed
				}
				err = e.finishFreeBSDReceiveSetup(socket, reader, setup, setupErr)
				if failure == "descriptor" {
					require.ErrorIs(t, err, net.ErrClosed)
					require.Nil(t, e.receiver)
					return
				}
				require.NoError(t, err)
				want := failure == "IPv4 setup" && family.network != "udp4" || failure == "IPv6 setup" && family.network == "udp4"
				require.Equal(t, want, e.managedECN)
				if !want {
					require.Nil(t, e.receiver)
					require.Nil(t, e.managedNative)
					require.Nil(t, e.managedPacketRawFactory(nil))
				}
				// Ordinary datagrams remain usable after optional metadata failure.
				peer, err := net.DialUDP(family.peerNetwork, nil, &net.UDPAddr{IP: family.peerIP, Port: endpoint.LocalAddr().(*net.UDPAddr).Port})
				require.NoError(t, err)
				defer peer.Close()
				_, err = peer.Write([]byte("fallback"))
				require.NoError(t, err)
				lease, err := acquire()
				require.NoError(t, err)
				defer lease.Close()
				require.NoError(t, lease.SetReadDeadline(time.Now().Add(5*time.Second)))
				b := make([]byte, 32)
				n, _, err := lease.ReadFrom(b)
				require.NoError(t, err)
				require.Equal(t, "fallback", string(b[:n]))
			})
		}
	}
}

func TestManagedFreeBSDSingletonResults(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"success", nil},
		{"permission", &os.SyscallError{Syscall: "sendmsg", Err: unix.EPERM}},
		{"message size", &os.SyscallError{Syscall: "sendmsg", Err: unix.EMSGSIZE}},
		{"terminal", net.ErrClosed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			native := &managedECNFixtureConn{writeResult: tc.err}
			lease := &managedPacketLease{done: make(chan struct{})}
			e := &managedPacketEndpoint{managedNative: native, managedECN: true, lease: lease}
			e.idle = sync.NewCond(&e.mutex)
			conn := &managedPacketConn{endpoint: e, lease: lease}
			raw := newManagedPacketRawConn(&managedECNFixtureConn{}, conn, &externalPacketIO{sendBatch: conn.WriteBatchV1}, false)
			sc := newSendConn(raw, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4242}, packetInfo{}, utils.DefaultLogger)
			err := sc.Write([]byte("singleton"), 0, protocol.ECT0)
			if tc.err == nil {
				require.NoError(t, err)
			} else {
				require.Same(t, tc.err, err)
			}
			require.Len(t, native.writes, 1, "FreeBSD must never retry EPERM")
		})
	}
	for _, result := range []int{-1, 0, 2} {
		t.Run(fmt.Sprintf("invalid callback %d", result), func(t *testing.T) {
			native := &managedECNFixtureConn{}
			lease := &managedPacketLease{done: make(chan struct{})}
			e := &managedPacketEndpoint{managedNative: native, managedECN: true, lease: lease}
			e.idle = sync.NewCond(&e.mutex)
			conn := &managedPacketConn{endpoint: e, lease: lease}
			callback := func(b [][]byte, oob []byte, addr *net.UDPAddr) (int, error) {
				_, err := conn.WriteBatchV1(b, oob, addr)
				require.NoError(t, err)
				return result, nil
			}
			raw := newManagedPacketRawConn(&managedECNFixtureConn{}, conn, &externalPacketIO{sendBatch: callback}, false)
			n, err := raw.WritePacket([]byte("singleton"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4242}, nil, 0, protocol.ECT0)
			require.Zero(t, n)
			require.ErrorContains(t, err, "unchanged")
			require.Len(t, native.writes, 1)
		})
	}
}

func TestManagedFreeBSDCloseJoinsRead(t *testing.T) {
	t.Setenv("QUIC_GO_DISABLE_ECN", "false")
	for _, parentClose := range []bool{false, true} {
		t.Run(fmt.Sprint(parentClose), func(t *testing.T) {
			endpoint, acquire, lease, raw, _ := newFreeBSDManagedECN(t, freeBSDManagedECNFamilies()[0], false)
			entered := make(chan struct{})
			done := make(chan error, 1)
			go func() {
				close(entered)
				p, err := raw.ReadPacket()
				if p.buffer != nil {
					p.buffer.Release()
				}
				done <- err
			}()
			<-entered
			closer := lease
			if parentClose {
				closer = endpoint
			}
			require.NoError(t, closer.Close())
			err := <-done
			require.True(t, errors.Is(err, net.ErrClosed) || errors.Is(err, os.ErrDeadlineExceeded), "%v", err)
			if !parentClose {
				next, err := acquire()
				require.NoError(t, err)
				require.NoError(t, next.Close())
			}
		})
	}
}
