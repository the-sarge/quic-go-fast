package self_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/require"
)

func TestGetConfigForClientLifecycle(t *testing.T) {
	t.Run("dial error before accept error", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			original := errors.New("controlled handshake accept failure")
			accept := &configForClientWorker{result: make(chan configForClientResult, 1)}
			dial := &configForClientWorker{result: make(chan configForClientResult)}
			done := make(chan struct{})
			go func() {
				defer close(done)
				// The unbuffered handoff makes the owner see the secondary error first.
				dial.result <- configForClientResult{err: errors.New("secondary dial failure")}
				time.Sleep(time.Nanosecond)
				accept.result <- configForClientResult{err: original}
			}()
			_, err := waitConfigForClient(accept, dial)
			<-done
			require.ErrorIs(t, err, original)
		})
	})

	t.Run("missing publication", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			accept := &configForClientWorker{result: make(chan configForClientResult)}
			dial := &configForClientWorker{result: make(chan configForClientResult)}
			_, err := waitConfigForClient(accept, dial)
			require.ErrorContains(t, err, "timeout waiting for config-for-client")
		})
	})
	t.Run("dial error without accept publication", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			accept := &configForClientWorker{result: make(chan configForClientResult)}
			dial := &configForClientWorker{result: make(chan configForClientResult, 1)}
			original := errors.New("dial failure without acceptance")
			dial.result <- configForClientResult{err: original}
			_, err := waitConfigForClient(accept, dial)
			require.ErrorIs(t, err, original)
		})
	})

	for _, fail := range []bool{false, true} {
		name := "success"
		if fail {
			name = "accept failure"
		}
		t.Run(name, func(t *testing.T) {
			ln, err := quic.Listen(newUDPConnLocalhost(t), getTLSConfig(), getQuicConfig(nil))
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(context.Background())
			var joins []func() error
			cleanup := func() {
				if err := cleanupConfigForClientLifecycle(cancel, ln.Close, joins); err != nil {
					t.Error(err)
				}
			}
			defer cleanup()
			original := errors.New("controlled handshake accept failure")
			accept := startConfigForClientWorker(ctx, func(ctx context.Context) (*quic.Conn, error) {
				conn, err := ln.Accept(ctx)
				if err == nil && fail {
					err = original
				}
				return conn, err
			})
			joins = append(joins, accept.join)
			udp := newUDPConnLocalhost(t)
			dialCtx, cancelDial := context.WithTimeout(ctx, time.Second)
			defer cancelDial()
			dial := startConfigForClientWorker(ctx, func(context.Context) (*quic.Conn, error) {
				conn, err := quic.Dial(dialCtx, udp, ln.Addr(), getTLSClientConfig(), getQuicConfig(nil))
				if fail {
					// Acceptance must report its error without waiting for the dial result.
					<-ctx.Done()
				}
				return conn, err
			})
			joins = append(joins, dial.join)
			if fail {
				_, err := waitConfigForClient(accept, dial)
				require.ErrorIs(t, err, original)
			} else {
				conn, err := waitConfigForClient(accept, dial)
				require.NoError(t, err)
				require.NotNil(t, conn)
			}
			cleanup()
		})
	}
}

// cleanupConfigForClientLifecycle releases blocked operations before joining workers.
func cleanupConfigForClientLifecycle(cancel context.CancelFunc, closeListener func() error, joins []func() error) error {
	cancel()
	err := closeListener()
	for _, join := range joins {
		err = errors.Join(err, join())
	}
	return err
}

func TestGetConfigForClientLifecycleCleanup(t *testing.T) {
	closeErr := errors.New("controlled listener close failure")
	joinErr := errors.New("controlled worker join timeout")
	for _, tc := range []struct {
		name     string
		closeErr error
		joinErr  error
	}{
		{name: "success"},
		{name: "join error", joinErr: joinErr},
		{name: "close error", closeErr: closeErr},
		{name: "close and join errors", closeErr: closeErr, joinErr: joinErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, fallback := range []bool{false, true} {
				name := "explicit then deferred"
				if fallback {
					name = "deferred fallback"
				}
				t.Run(name, func(t *testing.T) {
					var calls []string
					var results []error
					func() {
						var joins []func() error
						cleanup := func() {
							results = append(results, cleanupConfigForClientLifecycle(
								func() { calls = append(calls, "cancel") },
								func() error {
									calls = append(calls, "close")
									return tc.closeErr
								},
								joins,
							))
						}
						defer cleanup()
						// Register after deferring cleanup, as the real fixture does.
						joins = append(joins, func() error {
							calls = append(calls, "join first")
							return tc.joinErr
						}, func() error {
							calls = append(calls, "join later")
							return nil
						})
						if fallback {
							return
						}
						cleanup()
					}()
					wantCalls := []string{"cancel", "close", "join first", "join later"}
					wantResults := 1
					if !fallback {
						wantCalls = append(wantCalls, "cancel", "close", "join first", "join later")
						wantResults = 2
					}
					require.Equal(t, wantCalls, calls)
					require.Len(t, results, wantResults)
					for _, err := range results {
						if tc.closeErr == nil && tc.joinErr == nil {
							require.NoError(t, err)
						}
						if tc.closeErr != nil {
							require.ErrorIs(t, err, tc.closeErr)
						}
						if tc.joinErr != nil {
							require.ErrorIs(t, err, tc.joinErr)
						}
					}
				})
			}
		})
	}
}
