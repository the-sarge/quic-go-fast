//go:build freebsd

package quic

import (
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
