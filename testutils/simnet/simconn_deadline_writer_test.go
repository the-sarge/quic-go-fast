package simnet

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// simConnDeadlineWriter belongs only to the read-deadline fixture. Its result
// becomes readable when done closes, without requiring an active consumer.
type simConnDeadlineWriter struct {
	done chan struct{}
	n    int
	err  error
}

func newSimConnDeadlineWriter(t *testing.T, stop, reset func()) *simConnDeadlineWriter {
	t.Helper()
	w := &simConnDeadlineWriter{}
	t.Cleanup(func() {
		stop()
		if w.done != nil {
			n, err := w.wait(t)
			t.Logf("simnet deadline writer joined: n=%d err=%v", n, err)
		}
		// A join timeout fails above without replacing endpoints still in use.
		reset()
	})
	return w
}

func (w *simConnDeadlineWriter) start(write func() (int, error)) {
	w.done = make(chan struct{})
	go func() {
		defer close(w.done)
		w.n, w.err = write()
	}()
}

func (w *simConnDeadlineWriter) wait(t *testing.T) (int, error) {
	t.Helper()
	select {
	case <-w.done:
		return w.n, w.err
	case <-time.After(5 * time.Second):
		t.Fatal("timeout joining simnet deadline writer")
		return 0, nil
	}
}

func TestSimConnDeadlineWriterLifecycle(t *testing.T) {
	const env = "QUIC_GO_SIMNET_DEADLINE_WRITER_SCENARIO"
	if scenario := os.Getenv(env); scenario != "" {
		require.Contains(t, []string{"success", "writer-error", "parent-failure"}, scenario)
		router := &FixedLatencyRouter{latency: 100 * time.Millisecond}
		addr1 := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 1234}
		addr2 := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 2), Port: 1234}
		conn1, conn2 := NewSimConn(addr1, router), NewSimConn(addr2, router)
		started, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
		sentinel := errors.New("injected simnet write error")
		writer := newSimConnDeadlineWriter(t, func() {
			conn1.Close()
			conn2.Close()
			// The test gate, not Close, releases this deliberately held write.
			close(release)
		}, func() {
			select {
			case <-returned:
			default:
				t.Fatal("reset before write returned")
			}
			router.RemoveNode(addr1)
			router.RemoveNode(addr2)
			conn1, conn2 = NewSimConn(addr1, router), NewSimConn(addr2, router)
			t.Log("simnet endpoints reset after write returned")
		})
		writer.start(func() (int, error) {
			defer close(returned)
			n, err := conn1.WriteTo([]byte("test"), addr2)
			close(started)
			if scenario == "parent-failure" {
				<-release
			}
			if scenario == "writer-error" && err == nil {
				return n, sentinel
			}
			return n, err
		})
		if scenario == "parent-failure" {
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("writer did not start")
			}
			t.Fatal("injected simnet parent failure")
		}
		n, err := writer.wait(t)
		require.Equal(t, 4, n)
		if scenario == "writer-error" {
			require.Same(t, sentinel, err)
			t.Log("owner received original write error")
		}
		require.NoError(t, err, "owning subtest checks write result")
		return
	}

	executable, err := os.Executable()
	require.NoError(t, err)
	for _, tc := range []struct {
		name, failure, result string
	}{
		{"success", "", "n=4 err=<nil>"},
		{"writer-error", "owner received original write error", "n=4 err=injected simnet write error"},
		{"parent-failure", "injected simnet parent failure", "n=4 err=<nil>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestSimConnDeadlineWriterLifecycle$", "-test.v", "-test.timeout=12s")
			cmd.Env = append(os.Environ(), env+"="+tc.name)
			output, err := cmd.CombinedOutput()
			require.NoError(t, ctx.Err(), "%s", output)
			if tc.failure == "" {
				require.NoError(t, err, "%s", output)
			} else {
				var exitErr *exec.ExitError
				require.ErrorAs(t, err, &exitErr, "%s", output)
				require.Equal(t, 1, exitErr.ExitCode(), "%s", output)
				require.Contains(t, string(output), tc.failure)
			}
			joined := "simnet deadline writer joined: " + tc.result
			reset := "simnet endpoints reset after write returned"
			require.Contains(t, string(output), joined)
			require.Contains(t, string(output), reset)
			require.Less(t, strings.Index(string(output), joined), strings.Index(string(output), reset))
			require.NotContains(t, string(output), "timeout joining simnet deadline writer")
			require.NotContains(t, string(output), "DATA RACE")
		})
	}
}
