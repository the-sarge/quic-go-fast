package quicproxy

import (
	"bytes"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProxySocketObservations(t *testing.T) {
	for _, delay := range []time.Duration{0, time.Millisecond} {
		for _, failWrite := range []bool{false, true} {
			t.Run(fmt.Sprintf("delay=%s/fail=%t", delay, failWrite), func(t *testing.T) {
				server, client, proxySocket := newUPDConnLocalhost(t), newUPDConnLocalhost(t), newUPDConnLocalhost(t)
				require.NoError(t, server.SetReadDeadline(time.Now().Add(3*time.Second)))
				require.NoError(t, client.SetReadDeadline(time.Now().Add(3*time.Second)))
				events := make(chan SocketEvent, 16)
				p := &Proxy{
					Conn: proxySocket, ServerAddr: server.LocalAddr().(*net.UDPAddr),
					DelayPacket: func(Direction, net.Addr, net.Addr, []byte) time.Duration { return delay },
					DropPacket: func(dir Direction, _, _ net.Addr, _ []byte) bool {
						if failWrite && dir == DirectionOutgoing {
							proxySocket.Close() // The subsequent forwarding write must report its real failure.
						}
						return false
					},
					ObserveSocket: func(ev SocketEvent) {
						ev.Data = bytes.Clone(ev.Data)
						events <- ev
					},
				}
				require.NoError(t, p.Start())
				t.Cleanup(func() { require.NoError(t, p.Close()) })
				_, err := client.WriteTo([]byte("request"), p.LocalAddr())
				require.NoError(t, err)
				buf := make([]byte, 128)
				n, addr, err := server.ReadFrom(buf)
				require.NoError(t, err)
				require.Equal(t, "request", string(buf[:n]))
				_, err = server.WriteTo([]byte("response"), addr)
				require.NoError(t, err)
				if !failWrite {
					n, _, err = client.ReadFrom(buf)
					require.NoError(t, err)
					require.Equal(t, "response", string(buf[:n]))
				}
				seen := make(map[string]bool)
				timer := time.NewTimer(3 * time.Second)
				defer timer.Stop()
				for len(seen) < 4 {
					select {
					case ev := <-events:
						if ev.Operation == "read" && ev.Err != nil {
							continue // Closing the listening socket can also finish its pending read.
						}
						seen[ev.Direction.String()+ev.Operation] = true
						want := "request"
						if ev.Direction == DirectionOutgoing {
							want = "response"
						}
						require.Equal(t, want, string(ev.Data))
						if ev.Operation == "write" {
							require.False(t, ev.Started.IsZero())
						}
						if failWrite && ev.Direction == DirectionOutgoing && ev.Operation == "write" {
							require.ErrorIs(t, ev.Err, net.ErrClosed)
							require.Zero(t, ev.N)
						} else {
							require.NoError(t, ev.Err)
							require.Equal(t, len(want), ev.N)
						}
					case <-timer.C:
						t.Fatalf("missing socket observations: %v", seen)
					}
				}
			})
		}
	}
}
