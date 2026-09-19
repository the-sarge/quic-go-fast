package self_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/require"
)

func TestNATRebindingReceiveFailure(t *testing.T) {
	f := newNATRebindingFixture(t)
	acquired := make(chan struct{})
	f.startWriter(t, nil, func(str *quic.SendStream) error {
		close(acquired)
		<-str.Context().Done()
		return context.Cause(str.Context())
	})
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("NAT writer did not acquire its stream")
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	f.ctx = ctx
	_, err := f.receive()
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorContains(t, err, "accepting NAT stream")
	require.ErrorContains(t, err, "writing NAT stream")
}

func TestNATRebindingWorkerFailure(t *testing.T) {
	for _, operation := range []string{"opening", "writing"} {
		t.Run(operation, func(t *testing.T) {
			sentinel := errors.New("injected " + operation + " failure")
			f := newNATRebindingFixture(t)
			var open func() (*quic.SendStream, error)
			var write func(*quic.SendStream) error
			if operation == "opening" {
				open = func() (*quic.SendStream, error) { return nil, sentinel }
			} else {
				// No bytes or FIN are sent: the local failure interruption must
				// release the parent's accept, not successful stream completion.
				write = func(*quic.SendStream) error { return sentinel }
			}
			f.startWriter(t, open, write)
			_, err := f.receive()
			require.ErrorIs(t, err, sentinel)
			require.ErrorContains(t, err, operation+" NAT stream")
			// The worker must release dependent waits before the existing scenario
			// context expires, rather than hiding the cause behind its timeout.
			require.NoError(t, f.ctx.Err())
			var closeErr *quic.ApplicationError
			require.ErrorAs(t, context.Cause(f.conn.Context()), &closeErr)
			require.Equal(t, "NAT writer failed", closeErr.ErrorMessage)
			select {
			case <-f.workerDone:
			default:
				t.Fatal("NAT worker outlived result handoff")
			}
		})
	}
}

func TestNATRebindingCleanup(t *testing.T) {
	const env = "QUIC_GO_NAT_CLEANUP_FAILURE"
	if os.Getenv(env) != "" {
		checkNATRebindingCleanup(t, true)
		return
	}
	checkNATRebindingCleanup(t, false)
	executable, err := os.Executable()
	require.NoError(t, err)
	cmd := exec.CommandContext(t.Context(), executable, "-test.run=^TestNATRebindingCleanup$", "-test.timeout=15s", "-version=1")
	cmd.Env = append(os.Environ(), env+"=1")
	output, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "%s", output)
	require.Equal(t, 1, exitErr.ExitCode(), "%s", output)
	require.Contains(t, string(output), "intentional NAT owner failure")
	require.Contains(t, string(output), "NAT cleanup observed worker result and completion")
}

func checkNATRebindingCleanup(t *testing.T, fail bool) {
	t.Helper()
	var f *natRebindingFixture
	t.Run("owner", func(t *testing.T) {
		f = newNATRebindingFixture(t)
		acquired := make(chan struct{})
		f.startWriter(t, nil, func(str *quic.SendStream) error {
			close(acquired)
			<-str.Context().Done()
			return context.Cause(str.Context())
		})
		select {
		case <-acquired:
		case <-time.After(time.Second):
			t.Fatal("NAT writer did not acquire its stream")
		}
		if fail {
			t.Fatal("intentional NAT owner failure")
		}
		// Return without receiving either stream data or the worker result.
	})
	require.NotNil(t, f, "NAT fixture setup failed")
	require.NotNil(t, f.workerDone, "NAT writer was not started")
	select {
	case <-f.workerDone:
	default:
		t.Fatal("NAT worker outlived fixture cleanup")
	}
	require.ErrorContains(t, f.workerErr, "writing NAT stream")
	require.Error(t, f.conn.Context().Err())
	require.Error(t, f.serverConn.Context().Err())
	fmt.Println("NAT cleanup observed worker result and completion")
}
