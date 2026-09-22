//go:build darwin && !ios && !quic_go_no_private_syscalls

package quic

import (
	"runtime"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/stretchr/testify/require"
)

func TestDarwinManagedECNNativeBatch(t *testing.T) {
	for _, mode := range []string{"native", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			resetSendmsgXForTesting(t)
			if mode == "disabled" {
				t.Setenv(sendmsgXDisableEnv, "true")
			} else {
				major, err := getMacOSVersion()
				require.NoError(t, err)
				if _, ok := sendmsgXQualifiedKernel(major, runtime.GOARCH); !ok {
					t.Skipf("native evidence requires a qualified Darwin host; kernel %d", major)
				}
			}
			sendmsgXEnsureQualified()
			require.Equal(t, mode == "native", sendmsgXAvailable())
			for _, tc := range []struct{ network, peerNetwork, ip string }{
				{"udp4", "udp4", "127.0.0.1"},
				{"udp6", "udp6", "::1"},
				{"udp", "udp4", "127.0.0.1"},
				{"udp", "udp6", "::1"},
			} {
				t.Run(tc.network+"/"+tc.peerNetwork, func(t *testing.T) {
					_, _, conn, peer, _ := newDarwinECNTestPath(t, tc.network, tc.peerNetwork, tc.ip, "checked")
					reader, err := newConn(peer, false, false)
					require.NoError(t, err)
					reader.managedRead = true
					defer reader.releaseReadBuffers()
					sc := newSendConn(conn, peer.LocalAddr(), packetInfo{}, utils.DefaultLogger)
					before, _, fallback := sendmsgXCountersSnapshot()
					n, err := sc.sendBatch([][]byte{[]byte("first"), []byte("second")}, protocol.ECT0)
					require.NoError(t, err)
					require.Equal(t, 2, n)
					require.NoError(t, peer.SetReadDeadline(time.Now().Add(time.Second)))
					for _, want := range []string{"first", "second"} {
						p, err := reader.ReadPacket()
						require.NoError(t, err)
						require.Equal(t, want, string(p.data))
						require.Equal(t, protocol.ECT0, p.ecn)
						p.buffer.Release()
					}
					after, _, fallbackAfter := sendmsgXCountersSnapshot()
					if mode == "native" {
						require.Greater(t, after, before)
					} else {
						require.Equal(t, before, after)
						require.Greater(t, fallbackAfter, fallback)
					}
				})
			}
		})
	}
}
