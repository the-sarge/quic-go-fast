package quic

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

type retryTestPacketConn struct {
	net.PacketConn
	readFrom       func([]byte) (int, net.Addr, error)
	deadlineWrites []time.Time
}

func (c *retryTestPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	return c.readFrom(b)
}

func (c *retryTestPacketConn) SetReadDeadline(deadline time.Time) error {
	c.deadlineWrites = append(c.deadlineWrites, deadline)
	return c.PacketConn.SetReadDeadline(deadline)
}

func newRetryTestTransport(t *testing.T, managed bool) (*Transport, *retryTestPacketConn) {
	t.Helper()
	var conn net.PacketConn
	if managed {
		_, acquire := newTestManagedEndpoint(t)
		var err error
		conn, err = acquire()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, conn.Close()) })
	} else {
		conn = newUDPConnLocalhost(t)
	}
	outer := &retryTestPacketConn{PacketConn: conn, readFrom: conn.ReadFrom}
	require.NoError(t, outer.SetReadDeadline(time.Unix(1, 0)))
	tr := &Transport{Conn: outer}
	if managed {
		require.NoError(t, tr.ConfigureManagedPacketIOV1(outer, conn, nil))
	}
	return tr, outer
}

func TestTransportTemporaryReadBackoff(t *testing.T) {
	t.Setenv("QUIC_GO_DISABLE_RECEIVE_BUFFER_WARNING", "true")
	for _, managed := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "managed"}[managed], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				tr, conn := newRetryTestTransport(t, managed)
				stop := errors.New("bounded read limit reached")
				var attempts []time.Time
				var timeouts int
				conn.readFrom = func(b []byte) (int, net.Addr, error) {
					attempts = append(attempts, time.Now())
					if len(attempts) > 10 {
						return 0, nil, stop
					}
					n, addr, err := conn.PacketConn.ReadFrom(b)
					//nolint:staticcheck // Exercise the existing transport retry classification.
					if e, ok := err.(net.Error); ok && e.Timeout() && e.Temporary() {
						timeouts++
					}
					return n, addr, err
				}
				require.NoError(t, tr.init(false))
				defer tr.Close()
				<-tr.listening
				require.Equal(t, 10, timeouts)
				require.ErrorIs(t, tr.closeErr, stop)
				var delays []time.Duration
				for i := 1; i < len(attempts); i++ {
					delays = append(delays, attempts[i].Sub(attempts[i-1]))
				}
				require.Equal(t, []time.Duration{
					5 * time.Millisecond, 10 * time.Millisecond, 20 * time.Millisecond,
					40 * time.Millisecond, 80 * time.Millisecond, 100 * time.Millisecond,
					100 * time.Millisecond, 100 * time.Millisecond, 100 * time.Millisecond,
					100 * time.Millisecond,
				}, delays)
			})
		})
	}
}

func TestTransportTemporaryReadRecovery(t *testing.T) {
	t.Setenv("QUIC_GO_DISABLE_RECEIVE_BUFFER_WARNING", "true")
	for _, managed := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "managed"}[managed], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				tr, conn := newRetryTestTransport(t, managed)
				peer := newUDPConnLocalhost(t)
				_, err := peer.WriteTo([]byte("\x00recovered packet"), conn.LocalAddr())
				require.NoError(t, err)
				resume := make(chan struct{})
				stop := errors.New("bounded recovery read limit reached")
				var recovered bool
				var attempts atomic.Int32
				var laterReads []time.Time
				conn.readFrom = func(b []byte) (int, net.Addr, error) {
					attempt := attempts.Add(1)
					if attempt > 20 {
						return 0, nil, stop
					}
					if recovered {
						<-resume
						laterReads = append(laterReads, time.Now())
						if len(laterReads) == 2 {
							return 0, nil, stop
						}
					}
					n, addr, err := conn.PacketConn.ReadFrom(b)
					if err == nil {
						recovered = true
					}
					return n, addr, err
				}
				require.NoError(t, tr.init(false))
				defer tr.Close()
				// Six failures reach the maximum wait at 155 ms.
				defer func() {
					select {
					case <-resume:
					default:
						close(resume)
					}
				}()
				time.Sleep(155 * time.Millisecond)
				synctest.Wait()
				require.EqualValues(t, 6, attempts.Load())
				require.Equal(t, []time.Time{time.Unix(1, 0)}, conn.deadlineWrites)
				cleared := time.Now()
				require.NoError(t, conn.SetReadDeadline(time.Time{}))
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				buf := make([]byte, 100)
				n, _, err := tr.ReadNonQUICPacket(ctx, buf)
				require.NoError(t, err)
				require.Equal(t, "\x00recovered packet", string(buf[:n]))
				require.Equal(t, 100*time.Millisecond, time.Since(cleared))
				require.Equal(t, []time.Time{time.Unix(1, 0), {}}, conn.deadlineWrites)
				// The caller starts a new timeout episode after the successful packet.
				require.NoError(t, conn.SetReadDeadline(time.Unix(1, 0)))
				close(resume)
				<-tr.listening
				require.Len(t, laterReads, 2)
				require.Equal(t, 5*time.Millisecond, laterReads[1].Sub(laterReads[0]))
				require.ErrorIs(t, tr.closeErr, stop)
			})
		})
	}
}

func TestTransportTemporaryReadShutdown(t *testing.T) {
	t.Setenv("QUIC_GO_DISABLE_RECEIVE_BUFFER_WARNING", "true")
	for _, managed := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "managed"}[managed], func(t *testing.T) {
			for _, shutdown := range []string{"Close", "internal", "single_use_server"} {
				t.Run(shutdown, func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						tr, conn := newRetryTestTransport(t, managed)
						var attempts atomic.Int32
						conn.readFrom = func(b []byte) (int, net.Addr, error) {
							attempt := attempts.Add(1)
							if attempt > 10 {
								return 0, nil, errors.New("bounded shutdown read limit reached")
							}
							return conn.PacketConn.ReadFrom(b)
						}
						tr.isSingleUse = shutdown == "single_use_server"
						require.NoError(t, tr.init(false))
						defer tr.Close()
						time.Sleep(155 * time.Millisecond)
						synctest.Wait()
						require.EqualValues(t, 6, attempts.Load())
						start := time.Now()
						switch shutdown {
						case "Close":
							require.NoError(t, tr.Close())
						case "internal":
							tr.close(errors.New("internal shutdown"))
						case "single_use_server":
							tr.closeServer()
						}
						<-tr.listening
						require.Zero(t, time.Since(start), "shutdown must interrupt the pending 100 ms wait")
						require.EqualValues(t, 6, attempts.Load(), "shutdown must not issue another read")
						// Transport shutdown must not close the caller's socket or lease.
						require.NoError(t, conn.SetReadDeadline(time.Time{}))
						peer := newUDPConnLocalhost(t)
						_, err := conn.WriteTo([]byte("still owned by caller"), peer.LocalAddr())
						require.NoError(t, err)
					})
				})
			}
		})
	}
}
