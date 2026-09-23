//go:build (darwin && !ios) || linux

package self_test

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/testutils/events"
	"github.com/stretchr/testify/require"
)

func TestManagedPathMTUDiscovery(t *testing.T) {
	for _, tc := range []struct {
		name, network, peerNetwork, ip string
		disabled, noECN                bool
	}{
		{"ipv4", "udp4", "udp4", "127.0.0.1", false, false},
		{"ipv6", "udp6", "udp6", "::1", false, false},
		{"dual_ipv4", "udp", "udp4", "127.0.0.1", false, false},
		{"dual_ipv6", "udp", "udp6", "::1", false, false},
		{"without_ecn", "udp4", "udp4", "127.0.0.1", false, true},
		{"turn_profile", "udp4", "udp4", "127.0.0.1", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.noECN {
				t.Setenv("QUIC_GO_DISABLE_ECN", "true")
			}
			endpoint, acquire, err := (&quic.Transport{}).NewManagedPacketEndpointV1(tc.network, nil)
			require.NoError(t, err)
			defer endpoint.Close()
			for generation := range 2 {
				t.Run(string(rune('1'+generation)), func(t *testing.T) {
					ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
					defer cancel()
					socket, err := net.ListenUDP(tc.peerNetwork, &net.UDPAddr{IP: net.ParseIP(tc.ip)})
					require.NoError(t, err)
					defer socket.Close()
					listener, err := quic.Listen(socket, getTLSConfig(), getQuicConfig(&quic.Config{DisablePathMTUDiscovery: true, EnableDatagrams: true}))
					require.NoError(t, err)
					defer listener.Close()
					done := make(chan error, 1)
					go func() {
						conn, err := listener.Accept(ctx)
						if err != nil {
							done <- err
							return
						}
						stream, err := conn.AcceptStream(ctx)
						if err == nil {
							_, err = io.Copy(stream, stream)
							stream.Close()
						}
						done <- err
					}()
					// Cleanup joins the peer even when a client-side assertion aborts.
					defer func() { cancel(); listener.Close(); <-done }()
					lease, err := acquire()
					require.NoError(t, err)
					defer lease.Close()
					tr := &quic.Transport{Conn: lease}
					defer tr.Close()
					require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil))
					initial := uint16(1232)
					if tc.disabled {
						initial = 1200
					}
					var recorder events.Recorder
					conn, err := tr.Dial(ctx, listener.Addr(), getTLSClientConfig(), getQuicConfig(&quic.Config{
						InitialPacketSize: initial, DisablePathMTUDiscovery: tc.disabled, EnableDatagrams: true, Tracer: newTracer(&recorder),
					}))
					require.NoError(t, err)
					defer conn.CloseWithError(0, "")
					require.Error(t, conn.SendDatagram(make([]byte, 65535)), "public DATAGRAM ceiling remains enforced")
					stream, err := conn.OpenStream()
					require.NoError(t, err)
					deadline, _ := ctx.Deadline()
					require.NoError(t, stream.SetDeadline(deadline))
					payload := PRData[:16*1024]
					echo := make([]byte, len(payload))
					for exchanges := 0; ; exchanges++ {
						_, err = stream.Write(payload)
						require.NoError(t, err)
						_, err = io.ReadFull(stream, echo)
						require.NoError(t, err)
						require.Equal(t, payload, echo)
						increased := false
						for _, event := range recorder.Events(qlog.MTUUpdated{}) {
							size := event.(qlog.MTUUpdated).Value
							if tc.disabled {
								require.LessOrEqual(t, size, int(initial))
							}
							if size > int(initial) {
								increased = true
							}
						}
						if increased || tc.disabled && exchanges >= 15 {
							break
						}
					}
					require.NoError(t, stream.Close())
					_, err = io.ReadAll(stream)
					require.NoError(t, err)
					t.Logf("MTU updates: %v", recorder.Events(qlog.MTUUpdated{}))
				})
			}
		})
	}
}
