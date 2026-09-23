//go:build (darwin && !ios) || linux

package quic

import (
	"bytes"
	"net"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Qualification uses a valid UDP payload exceeding a known native interface
// MTU. Linux runs in a disposable namespace, never changing host networking.
func TestManagedDFNativeDatagrams(t *testing.T) {
	payloadSize := 20000 // exceeds Darwin lo0's 16384 MTU, below UDP's payload limit
	if runtime.GOOS == "linux" {
		size, err := strconv.Atoi(os.Getenv("QUIC_TEST_DF_PAYLOAD"))
		if err != nil {
			t.Skip("requires isolated native path with known MTU and QUIC_TEST_DF_PAYLOAD")
		}
		payloadSize = size
	}
	require.Less(t, payloadSize, 65507)
	for _, tc := range []struct{ name, network, peerNetwork, ip string }{
		{"ipv4", "udp4", "udp4", "127.0.0.1"},
		{"ipv6", "udp6", "udp6", "::1"},
		{"dual_ipv4", "udp", "udp4", "127.0.0.1"},
		{"dual_ipv6", "udp", "udp6", "::1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			endpoint, acquire, err := (&Transport{}).NewManagedPacketEndpointV1(tc.network, nil)
			require.NoError(t, err)
			defer endpoint.Close()
			peer, err := net.ListenUDP(tc.peerNetwork, &net.UDPAddr{IP: net.ParseIP(tc.ip)})
			require.NoError(t, err)
			defer peer.Close()
			require.NoError(t, peer.SetReadBuffer(1<<20))
			payload := bytes.Repeat([]byte{0x53}, payloadSize)
			send := func(conn net.PacketConn, data []byte) {
				t.Helper()
				n, err := conn.WriteTo(data, peer.LocalAddr())
				require.NoError(t, err)
				require.Equal(t, len(data), n)
				require.NoError(t, peer.SetReadDeadline(time.Now().Add(time.Second)))
				buf := make([]byte, len(data)+1)
				n, _, err = peer.ReadFrom(buf)
				require.NoError(t, err)
				require.Equal(t, data, buf[:n])
			}
			send(endpoint, payload)
			for range 2 {
				lease, err := acquire()
				require.NoError(t, err)
				tr := &Transport{Conn: lease}
				require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil))
				_, err = lease.WriteTo(payload, peer.LocalAddr())
				require.Error(t, err)
				require.True(t, isSendMsgSizeErr(err), "native MTU rejection: %v", err)
				// A known zero-progress batch rejection must preserve fallback attribution.
				n, err := lease.(managedBatchWriterV1).WriteBatchV1([][]byte{payload}, nil, peer.LocalAddr().(*net.UDPAddr))
				require.NoError(t, err)
				require.Zero(t, n)
				send(lease, []byte("small"))
				require.NoError(t, lease.Close())
				send(endpoint, payload)
			}
		})
	}
}
