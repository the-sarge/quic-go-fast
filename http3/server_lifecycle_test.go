package http3

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go"

	"github.com/stretchr/testify/require"
)

type lifecycleListener struct {
	accept func(context.Context) (*quic.Conn, error)
	close  func() error
}

func (l *lifecycleListener) Accept(ctx context.Context) (*quic.Conn, error) {
	return l.accept(ctx)
}

func (*lifecycleListener) Addr() net.Addr { return &net.UDPAddr{Port: 443} }

func (l *lifecycleListener) Close() error { return l.close() }

func TestServerListenerCloseErrors(t *testing.T) {
	for _, graceful := range []bool{false, true} {
		name := "Close"
		if graceful {
			name = "Shutdown"
		}
		t.Run(name, func(t *testing.T) {
			var s Server
			first, second := errors.New("first listener"), errors.New("second listener")
			var calls int
			for _, closeErr := range []error{first, second} {
				var ln QUICListener = &lifecycleListener{close: func() error {
					calls++
					return closeErr
				}}
				require.NoError(t, s.addListener(&ln, true))
			}
			var external QUICListener = &lifecycleListener{close: func() error {
				t.Error("closed application listener")
				return nil
			}}
			require.NoError(t, s.addListener(&external, false))
			for call := 1; call <= 2; call++ {
				var err error
				if graceful {
					err = s.Shutdown(context.Background())
				} else {
					err = s.Close()
				}
				require.ErrorIs(t, err, first)
				if graceful {
					require.ErrorIs(t, err, second)
				} else {
					require.Same(t, first, err)
				}
				require.Equal(t, 2*call, calls)
			}
		})
	}
}

func TestServerServeCompletionSignal(t *testing.T) {
	var s Server
	require.True(t, s.admitConn())
	done := s.connHandlingDone
	require.True(t, s.admitConn())
	require.Equal(t, done, s.connHandlingDone, "admission must preserve the completion signal")
	s.decreaseConnCount()
	select {
	case <-done:
		t.Fatal("completion before sealing")
	default:
	}
	work, sealedDone := s.seal(false)
	require.Empty(t, s.closeListeners(work))
	require.Equal(t, (<-chan struct{})(done), sealedDone)
	require.False(t, s.admitConn())
	select {
	case <-done:
		t.Fatal("completion before last reservation released")
	default:
	}
	s.decreaseConnCount()
	<-done
	require.False(t, s.admitConn())
	require.NoError(t, s.Close())
	require.NoError(t, s.Shutdown(context.Background()))
}

func TestServerServeListenerRejectsAcceptedConnAfterClose(t *testing.T) {
	_, conn := newConnPair(t)
	var s Server
	s.ConnContext = func(context.Context, *quic.Conn) context.Context {
		t.Error("connection setup ran after sealing")
		return context.Background()
	}
	ln := &lifecycleListener{
		accept: func(context.Context) (*quic.Conn, error) {
			require.NoError(t, s.Close())
			return conn, nil
		},
		close: func() error { t.Error("closed application listener"); return nil },
	}
	require.ErrorIs(t, s.ServeListener(ln), http.ErrServerClosed)
	require.Error(t, conn.Context().Err(), "a connection accepted after sealing must be closed")
	var appErr *quic.ApplicationError
	require.ErrorAs(t, context.Cause(conn.Context()), &appErr)
	require.Equal(t, quic.ApplicationErrorCode(ErrCodeNoError), appErr.ErrorCode)
}

func TestServerServeQUICConnRejectsWithoutTakingOwnership(t *testing.T) {
	_, conn := newConnPair(t)
	s := Server{ConnContext: func(context.Context, *quic.Conn) context.Context {
		t.Error("connection setup ran after sealing")
		return context.Background()
	}}
	require.NoError(t, s.Close())
	require.ErrorIs(t, s.ServeQUICConn(conn), http.ErrServerClosed)
	require.NoError(t, conn.Context().Err())
}

func TestServerConcurrentListenerCloseCompletion(t *testing.T) {
	for _, gracefulFirst := range []bool{false, true} {
		name := "CloseThenShutdown"
		if gracefulFirst {
			name = "ShutdownThenClose"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var s Server
				entered, release := make(chan struct{}), make(chan struct{})
				firstErr := errors.New("first listener close")
				var ln QUICListener = &lifecycleListener{close: func() error {
					close(entered)
					<-release
					return firstErr
				}}
				require.NoError(t, s.addListener(&ln, true))
				// Pause the first shutdown after sealing, before acquiring the
				// listener-close mutex. Its serving goroutine can already exit.
				work, _ := s.seal(!gracefulFirst)
				s.removeListener(&ln)
				second := make(chan error, 1)
				go func() {
					if gracefulFirst {
						second <- s.Close()
					} else {
						second <- s.Shutdown(context.Background())
					}
				}()
				synctest.Wait()
				select {
				case <-second:
					t.Error("later shutdown overtook pending owned-listener closure")
				default:
				}
				first := make(chan []error, 1)
				go func() { first <- s.closeListeners(work) }()
				<-entered
				synctest.Wait()
				select {
				case <-second:
					t.Error("later shutdown overtook running owned-listener closure")
				default:
				}
				close(release)
				require.Equal(t, []error{firstErr}, <-first)
				// The later call waits, but does not inherit the earlier call's
				// removed listener or its error.
				require.NoError(t, <-second)
			})
		})
	}
}

func waitServerResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("server operation did not finish")
		return nil
	}
}

func TestServerCloseWaitsForAdmittedSetup(t *testing.T) {
	for _, direct := range []bool{true, false} {
		name := "ServeListener"
		if direct {
			name = "ServeQUICConn"
		}
		t.Run(name, func(t *testing.T) {
			_, conn := newConnPair(t)
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			s := Server{ConnContext: func(ctx context.Context, _ *quic.Conn) context.Context {
				close(entered)
				<-release
				return ctx
			}}
			served := make(chan error, 1)
			if direct {
				go func() { served <- s.ServeQUICConn(conn) }()
			} else {
				accepted := false
				ln := &lifecycleListener{
					accept: func(ctx context.Context) (*quic.Conn, error) {
						if !accepted {
							accepted = true
							return conn, nil
						}
						<-ctx.Done()
						return nil, ctx.Err()
					},
					close: func() error { t.Error("closed application listener"); return nil },
				}
				go func() { served <- s.ServeListener(ln) }()
			}
			<-entered
			closed := make(chan error, 1)
			go func() { closed <- s.Close() }()
			<-s.closeCtx.Done()
			select {
			case <-closed:
				t.Error("Close returned during admitted connection setup")
			default:
			}
			// Late admission must reject even while Close waits for setup.
			require.ErrorIs(t, s.ServeQUICConn(nil), http.ErrServerClosed)
			unblock()
			require.NoError(t, waitServerResult(t, closed))
			require.ErrorIs(t, waitServerResult(t, served), http.ErrServerClosed)
		})
	}
}

func TestServerCloseAfterSetupFailure(t *testing.T) {
	_, conn := newConnPair(t)
	require.NoError(t, conn.CloseWithError(42, "setup failure"))
	var s Server
	err := s.ServeQUICConn(conn)
	require.ErrorContains(t, err, "opening the control stream failed")
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	require.NoError(t, waitServerResult(t, closed))
}

func TestServerCloseWaitsForManagedHandler(t *testing.T) {
	client, conn := newConnPair(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	s := Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(entered)
		<-release
	})}
	served := make(chan error, 1)
	go func() { served <- s.ServeQUICConn(conn) }()
	str, err := client.OpenStream()
	require.NoError(t, err)
	_, err = str.Write(encodeRequest(t, httptest.NewRequest(http.MethodGet, "https://example.com", nil)))
	require.NoError(t, err)
	<-entered
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	// Closing the connection also unblocks the managed unidirectional accept loop.
	<-conn.Context().Done()
	select {
	case <-closed:
		t.Error("Close returned before the managed handler")
	default:
	}
	unblock()
	require.ErrorIs(t, waitServerResult(t, served), http.ErrServerClosed)
	require.NoError(t, waitServerResult(t, closed))
}

func TestServerUnusedConcurrentShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var s Server
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		results := make(chan error, 2)
		go func() { results <- s.Close() }()
		go func() { results <- s.Shutdown(ctx) }()
		require.NoError(t, <-results)
		require.NoError(t, <-results)
		require.NoError(t, s.Shutdown(ctx))
		require.NoError(t, s.Close())
		require.ErrorIs(t, s.ServeQUICConn(nil), http.ErrServerClosed)
	})
}
