package self_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/require"
)

func TestGetConfigForClientLifecycle(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "success"
		if fail {
			name = "accept failure"
		}
		t.Run(name, func(t *testing.T) {
			ln, err := quic.Listen(newUDPConnLocalhost(t), getTLSConfig(), getQuicConfig(nil))
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(context.Background())
			var workers []*configForClientWorker
			defer func() {
				cancel()
				require.NoError(t, ln.Close())
				for _, worker := range workers {
					require.NoError(t, worker.join())
				}
			}()
			original := errors.New("controlled handshake accept failure")
			accept := startConfigForClientWorker(ctx, func(ctx context.Context) (*quic.Conn, error) {
				conn, err := ln.Accept(ctx)
				if err == nil && fail {
					err = original
				}
				return conn, err
			})
			workers = append(workers, accept)
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
			workers = append(workers, dial)
			if fail {
				_, err := waitConfigForClient(accept, dial)
				require.ErrorIs(t, err, original)
			} else {
				conn, err := waitConfigForClient(accept, dial)
				require.NoError(t, err)
				require.NotNil(t, conn)
			}
			cancel()
			require.NoError(t, ln.Close())
			for _, worker := range workers {
				require.NoError(t, worker.join())
			}
		})
	}
}
