//go:build freebsd

package quic

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestManagedFreeBSDECNReceive(t *testing.T) {
	t.Setenv("QUIC_GO_DISABLE_ECN", "false")
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
			endpoint, acquire, err := (&Transport{}).NewManagedPacketEndpointV1(tc.network, &net.UDPAddr{IP: tc.ip})
			require.NoError(t, err)
			defer endpoint.Close()
			lease, err := acquire()
			require.NoError(t, err)
			defer lease.Close()
			require.NoError(t, lease.SetReadDeadline(time.Now().Add(5*time.Second)))
			destination := &net.UDPAddr{IP: tc.peerIP, Port: endpoint.LocalAddr().(*net.UDPAddr).Port}
			peer, err := net.DialUDP(tc.peerNetwork, nil, destination)
			require.NoError(t, err)
			defer peer.Close()
			tr := &Transport{Conn: lease}
			require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil))
			raw := tr.wrapExternalPacketIO(&basicConn{PacketConn: lease})
			t.Logf("qualification: %+v", endpoint.(*managedPacketConn).endpoint.managedECNSetup)
			require.True(t, raw.capabilities().ECN, "native managed ECN must qualify")
			fd, err := peer.SyscallConn()
			require.NoError(t, err)
			for _, mark := range []protocol.ECN{protocol.ECNNon, protocol.ECT0, protocol.ECT1, protocol.ECNCE} {
				require.NoError(t, fd.Control(func(fd uintptr) {
					if tc.peerNetwork == "udp4" {
						require.NoError(t, unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_TOS, int(mark.ToHeaderBits())))
					} else {
						require.NoError(t, unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_TCLASS, int(mark.ToHeaderBits())))
					}
				}))
				_, err := peer.Write([]byte(mark.String()))
				require.NoError(t, err)
				p, err := raw.ReadPacket()
				require.NoError(t, err)
				require.Equal(t, mark, p.ecn)
				require.Equal(t, mark.String(), string(p.data))
				p.buffer.Release()
			}
		})
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
