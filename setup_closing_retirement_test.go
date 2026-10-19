package quic

import (
	"context"
	"net/netip"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/testdata"

	"github.com/stretchr/testify/require"
)

func TestDefaultSetupAdmissionBubbleCleanup(t *testing.T) {
	for _, mode := range []string{"transport", "Listen", "ListenEarly"} {
		t.Run(mode, func(t *testing.T) {
			for range 2 {
				ip := netip.MustParseAddr("127.0.0.1")
				defaultServerSetupBudget.mu.Lock()
				baseline := defaultServerSetupBudget.byIP[ip]
				defaultServerSetupBudget.mu.Unlock()
				synctest.Test(t, func(t *testing.T) {
					clientSocket, serverSocket, closeNetwork := newSimnetLink(t, time.Millisecond)
					defer closeNetwork()
					tlsConf := testdata.GetTLSConfig().Clone()
					tlsConf.NextProtos = []string{"incompatible-alpn"}
					var closeServer func() error
					if mode == "ListenEarly" {
						ln, err := ListenEarly(serverSocket, tlsConf, nil)
						require.NoError(t, err)
						closeServer = ln.Close
					} else {
						var ln *Listener
						var err error
						if mode == "transport" {
							server := &Transport{Conn: serverSocket}
							defer server.Close()
							ln, err = server.Listen(tlsConf, nil)
						} else {
							ln, err = Listen(serverSocket, tlsConf, nil)
						}
						require.NoError(t, err)
						closeServer = ln.Close
					}
					defer func() { require.NoError(t, closeServer()) }()
					client := &Transport{Conn: clientSocket}
					defer client.Close()
					ctx, cancel := context.WithTimeout(context.Background(), time.Second)
					defer cancel()
					_, err := client.Dial(ctx, serverSocket.LocalAddr(), candidateClientTLS(), nil)
					require.Error(t, err)
				})
				defaultServerSetupBudget.mu.Lock()
				count := defaultServerSetupBudget.byIP[ip]
				defaultServerSetupBudget.mu.Unlock()
				require.Equal(t, baseline, count, "closed transports return capacity before their fake clock stops")
			}
		})
	}
}

func TestTransportSetupClosingRetirement(t *testing.T) {
	for _, side := range []string{"local", "remote"} {
		for _, mode := range []string{"expiry", "close", "close_at_expiry", "close_before_teardown", "late_publication", "single_use", "single_use_late"} {
			t.Run(side+"/"+mode, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					_, socket, closeNetwork := newSimnetLink(t, time.Millisecond)
					defer closeNetwork()
					tr := &Transport{Conn: socket, isSingleUse: mode == "single_use" || mode == "single_use_late"}
					require.NoError(t, tr.init(true))
					defer tr.Close()
					m := (*packetHandlerMap)(tr)
					budget := newSetupBudget(1, 1, time.Hour)
					reservation, ok := newSetupReservation(budget.acquire(netip.AddrPort{}))
					require.True(t, ok)
					ids := []protocol.ConnectionID{randConnID(8), randConnID(8)}
					var packet []byte
					if side == "local" {
						packet = []byte("connection close")
					}
					const expiry = time.Second
					conn := &wrappedConn{Conn: &Conn{setup: reservation}, testHooks: &connTestHooks{}}
					if mode == "late_publication" {
						// Model connection workers finishing a local or remote close
						// after transport shutdown has already taken its snapshot.
						conn.testHooks.destroy = func(error) {
							m.ReplaceWithClosed(ids, packet, expiry)
							reservation.teardown()
						}
					}
					for _, id := range ids {
						require.True(t, m.Add(id, conn))
					}
					switch mode {
					case "single_use_late":
						tr.closeServer()
						m.ReplaceWithClosed(ids, packet, expiry)
						reservation.teardown()
					case "late_publication":
						require.NoError(t, tr.Close())
					default:
						m.ReplaceWithClosed(ids, packet, expiry)
						if mode != "close_before_teardown" {
							reservation.teardown()
						}
						require.Equal(t, 1, budget.state(t).held)
						switch mode {
						case "single_use":
							tr.closeServer()
						case "expiry":
							time.Sleep(expiry)
							synctest.Wait()
						case "close_at_expiry":
							closed := make(chan error, 1)
							go func() { time.Sleep(expiry); closed <- tr.Close() }()
							require.NoError(t, <-closed)
						default:
							require.NoError(t, tr.Close())
						}
						if mode == "close_before_teardown" {
							require.Equal(t, 1, budget.state(t).held, "retirement still waits for connection work")
							reservation.teardown()
						}
					}
					for _, id := range ids {
						_, found := m.Get(id)
						require.False(t, found)
					}
					time.Sleep(2 * expiry)
					synctest.Wait()
					require.NoError(t, tr.Close())
					state := budget.state(t)
					require.Zero(t, state.held)
					require.Equal(t, []bool{false}, state.settlements, "expiry and duplicate-ID close retire once")
				})
			})
		}
	}
}
