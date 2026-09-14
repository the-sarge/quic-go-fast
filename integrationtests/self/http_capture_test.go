package self_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quic-go/quic-go/qlogwriter/jsontext"

	"github.com/stretchr/testify/require"
)

func TestHTTPCaptureRetainsBeyondEarlyDial(t *testing.T) {
	root := t.TempDir()
	c := openHTTPCapture(root, "controlled")
	c.record("client", "dial_return", "early success")
	c.record("server", "handshake_complete", "after early return")
	c.record("client", "response_headers", "200")
	c.beginCleanup()
	c.record("server", "close", "during cleanup")
	c.finish(true)
	data, err := os.ReadFile(c.path)
	require.NoError(t, err)
	require.Contains(t, string(data), "handshake_complete")
	require.Contains(t, string(data), "response_headers")
	require.Contains(t, string(data), `"phase":"cleanup"`)
	require.Contains(t, string(data), "capture_finished")
	require.FileExists(t, filepath.Join(filepath.Dir(c.path), "SHA256SUMS"))
	c.record("late", "outside_capture", "ignored")
	after, err := os.ReadFile(c.path)
	require.NoError(t, err)
	require.Equal(t, data, after)
}

func TestHTTPCapturePassingCleanup(t *testing.T) {
	root := t.TempDir()
	c := openHTTPCapture(root, t.Name())
	c.record("fixture", "pass", nil)
	c.finish(false)
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestHTTPCaptureIncomplete(t *testing.T) {
	for _, mode := range []string{"limit", "oversized", "encoding", "write"} {
		t.Run(mode, func(t *testing.T) {
			c := openHTTPCapture(t.TempDir(), t.Name())
			switch mode {
			case "limit":
				c.limit = c.written
			case "write":
				require.NoError(t, c.file.Close())
			}
			switch mode {
			case "oversized":
				c.record("fixture", "oversized", strings.Repeat("x", httpCaptureEventBytes))
			case "encoding":
				c.record("fixture", "invalid", make(chan int))
			default:
				c.record("fixture", "event", "data")
			}
			c.finish(false)
			data, err := os.ReadFile(c.path)
			require.NoError(t, err)
			if mode == "write" {
				require.NotContains(t, string(data), "capture_finished")
				require.Error(t, c.err)
			} else {
				require.Contains(t, string(data), `"complete_recording_window":false`)
				require.Contains(t, string(data), `"dropped":1`)
			}
			info, err := os.Stat(c.path)
			require.NoError(t, err)
			require.Less(t, info.Size(), int64(httpCaptureSlotBytes))
		})
	}
}

func TestHTTPCaptureQuota(t *testing.T) {
	root := t.TempDir()
	for range httpCaptureSlots {
		c := openHTTPCapture(root, t.Name())
		require.NoError(t, c.err)
		c.finish(true)
	}
	c := openHTTPCapture(root, t.Name())
	require.ErrorContains(t, c.err, "quota exhausted")
	c.finish(true)
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, entries, httpCaptureSlots)
}

type heldHTTPCaptureEvent struct{ entered, release chan struct{} }

func (heldHTTPCaptureEvent) Name() string { return "held_event" }
func (e heldHTTPCaptureEvent) Encode(enc *jsontext.Encoder, _ time.Time) error {
	close(e.entered)
	<-e.release
	return enc.WriteToken(jsontext.String("owned before finalization"))
}

func TestHTTPCaptureConcurrentFinalization(t *testing.T) {
	c := openHTTPCapture(t.TempDir(), t.Name())
	entered, release := make(chan struct{}), make(chan struct{})
	recorded, finished := make(chan struct{}), make(chan struct{})
	// The event is admitted through the real recorder interface before finish.
	r := &httpCaptureRecorder{capture: c, source: "server"}
	go func() { r.RecordEvent(heldHTTPCaptureEvent{entered: entered, release: release}); close(recorded) }()
	<-entered
	go func() { c.finish(true); close(finished) }()
	close(release)
	<-recorded
	<-finished
	data, err := os.ReadFile(c.path)
	require.NoError(t, err)
	require.Contains(t, string(data), "owned before finalization")
	require.Less(t, bytes.Index(data, []byte("held_event")), bytes.Index(data, []byte("capture_finished")))
}

func TestHTTPCaptureFixtureFailure(t *testing.T) {
	binary, err := os.Executable()
	require.NoError(t, err)
	for _, fixture := range []string{"TestHTTPServerIdleTimeout", "TestHTTPReestablishConnectionAfterDialError"} {
		t.Run(fixture, func(t *testing.T) {
			root := t.TempDir()
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary, "-test.run=^"+fixture+"$", "-test.timeout=15s", "-version=2")
			cmd.Env = append(os.Environ(), "QUIC_GO_HTTP_CAPTURE_DIR="+root, "QUIC_GO_HTTP_CAPTURE_FAIL_TEST="+fixture)
			output, err := cmd.CombinedOutput()
			require.Error(t, err)
			require.NoError(t, ctx.Err(), string(output))
			require.Contains(t, string(output), "controlled HTTP capture fixture failure")
			files, err := filepath.Glob(filepath.Join(root, "slot-*", "capture.jsonl"))
			require.NoError(t, err)
			require.Len(t, files, 1)
			data, err := os.ReadFile(files[0])
			require.NoError(t, err)
			require.Contains(t, string(data), "client=true initial_cid=")
			require.Contains(t, string(data), "client=false initial_cid=")
			require.Contains(t, string(data), "response_headers")
			require.Contains(t, string(data), "pre_teardown")
			require.Contains(t, string(data), `"failed":true`)
			require.Contains(t, string(data), `"complete_recording_window":true`)
			require.FileExists(t, filepath.Join(filepath.Dir(files[0]), "SHA256SUMS"))
			if fixture == "TestHTTPServerIdleTimeout" {
				require.Contains(t, string(data), "connection_published")
				require.Contains(t, string(data), "HTTP idle timer")
			} else {
				require.Contains(t, string(data), "deliberate_dial_error")
				require.Contains(t, string(data), "client dial=2")
			}
		})
	}
}
