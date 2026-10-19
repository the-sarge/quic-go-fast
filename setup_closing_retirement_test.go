package quic

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/testdata"

	"github.com/stretchr/testify/require"
)

type closingPacketObserver struct {
	net.PacketConn
	writes chan []byte
}

func (c *closingPacketObserver) WriteTo(p []byte, addr net.Addr) (int, error) {
	n, err := c.PacketConn.WriteTo(p, addr)
	if err == nil {
		c.writes <- append([]byte(nil), p[:n]...)
	}
	return n, err
}

func TestListenerClosingPreservesClosedConnections(t *testing.T) {
	verificationTime := time.Now()
	for _, mode := range []string{"Listen", "ListenEarly"} {
		for _, side := range []string{"local", "remote"} {
			for _, order := range []string{"listener_first", "connection_first"} {
				t.Run(mode+"/"+side+"/"+order, func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						clientSocket, serverSocket, closeNetwork := newSimnetLink(t, time.Millisecond)
						defer closeNetwork()
						observer := &closingPacketObserver{PacketConn: serverSocket, writes: make(chan []byte, 128)}
						var listener interface {
							Accept(context.Context) (*Conn, error)
							Close() error
						}
						var server *baseServer
						if mode == "ListenEarly" {
							ln, err := ListenEarly(observer, testdata.GetTLSConfig(), nil)
							require.NoError(t, err)
							listener, server = ln, ln.baseServer
						} else {
							ln, err := Listen(observer, testdata.GetTLSConfig(), nil)
							require.NoError(t, err)
							listener, server = ln, ln.baseServer
						}
						defer listener.Close()
						tr := (*Transport)(server.tr)
						defer tr.Close()
						client := &Transport{Conn: clientSocket}
						defer client.Close()
						ctx, cancel := context.WithTimeout(context.Background(), time.Second)
						defer cancel()
						tlsConf := candidateClientTLS()
						tlsConf.Time = func() time.Time { return verificationTime }
						peer, err := client.Dial(ctx, serverSocket.LocalAddr(), tlsConf, nil)
						require.NoError(t, err)
						conn, err := listener.Accept(ctx)
						require.NoError(t, err)
						<-conn.HandshakeComplete()
						synctest.Wait()
						conn.setup.mu.Lock()
						settled := conn.setup.settled
						conn.setup.mu.Unlock()
						require.True(t, settled)
						var ids []protocol.ConnectionID
						tr.mutex.Lock()
						for id := range tr.handlers {
							ids = append(ids, id)
						}
						tr.mutex.Unlock()
						require.NotEmpty(t, ids)
						for len(observer.writes) > 0 {
							<-observer.writes
						}
						if order == "listener_first" {
							require.NoError(t, listener.Close())
						}
						if side == "local" {
							require.NoError(t, conn.CloseWithError(123, "test close"))
						} else {
							require.NoError(t, peer.CloseWithError(123, "test close"))
							<-conn.Context().Done()
						}
						synctest.Wait()
						if order == "connection_first" {
							require.NoError(t, listener.Close())
						}
						for _, id := range ids {
							handler, found := server.tr.Get(id)
							require.True(t, found, "listener closure preserves established closing/draining routes")
							if side == "local" {
								require.IsType(t, &closedLocalConn{}, handler)
							} else {
								require.IsType(t, &closedRemoteConn{}, handler)
							}
						}
						var originalClose []byte
						for len(observer.writes) > 0 {
							originalClose = <-observer.writes
						}
						latePacket := make([]byte, 100)
						latePacket[0] = 0x40
						// The original client-chosen Initial ID may have a different
						// length. Short headers use the transport's server-issued IDs.
						var lateID protocol.ConnectionID
						for _, id := range ids {
							if id.Len() == tr.connIDLen {
								lateID = id
								break
							}
						}
						require.Equal(t, tr.connIDLen, lateID.Len())
						copy(latePacket[1:], lateID.Bytes())
						_, err = clientSocket.WriteTo(latePacket, serverSocket.LocalAddr())
						require.NoError(t, err)
						if side == "local" {
							require.NotEmpty(t, originalClose)
							select {
							case retransmission := <-observer.writes:
								require.Equal(t, originalClose, retransmission)
							case <-time.After(time.Second):
								t.Fatal("no CONNECTION_CLOSE retransmission after listener closure")
							}
						} else {
							time.Sleep(2 * time.Millisecond)
							synctest.Wait()
							require.Empty(t, observer.writes, "remote draining absorbs late packets")
						}
						if order == "listener_first" {
							require.NoError(t, tr.Close())
						} else {
							time.Sleep(time.Second)
							synctest.Wait()
						}
						for _, id := range ids {
							_, found := server.tr.Get(id)
							require.False(t, found, "transport shutdown and ordinary expiry retire established routes")
						}
					})
				})
			}
		}
	}
}

func TestListenerClosingWithoutSetupReservation(t *testing.T) {
	for _, side := range []string{"local", "remote"} {
		t.Run(side, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				_, socket, closeNetwork := newSimnetLink(t, time.Millisecond)
				defer closeNetwork()
				tr := &Transport{Conn: socket, isSingleUse: true}
				require.NoError(t, tr.init(true))
				defer tr.Close()
				m := (*packetHandlerMap)(tr)
				id := randConnID(8)
				require.True(t, m.Add(id, &mockPacketHandler{}))
				tr.closeServer()
				var packet []byte
				if side == "local" {
					packet = []byte("connection close")
				}
				m.ReplaceWithClosed([]protocol.ConnectionID{id}, packet, time.Second)
				_, found := m.Get(id)
				require.True(t, found, "reservation-free connections keep closing/draining state")
				require.NoError(t, tr.Close())
				_, found = m.Get(id)
				require.False(t, found, "actual transport shutdown removes all closing state")
			})
		})
	}
}

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
