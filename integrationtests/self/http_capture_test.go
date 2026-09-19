package self_test

import (
	"bytes"
	"context"
	"encoding/json"
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
	for _, fixture := range []string{"TestHTTPServerIdleTimeout", "TestHTTPReestablishConnectionAfterDialError", "TestHTTP3ServerHotswap"} {
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
			switch fixture {
			case "TestHTTPServerIdleTimeout":
				require.Contains(t, string(data), "connection_published")
				require.Contains(t, string(data), "HTTP idle timer")
			case "TestHTTP3ServerHotswap":
				milestones := make(map[string]string)
				for line := range bytes.SplitSeq(bytes.TrimSpace(data), []byte("\n")) {
					var record httpCaptureRecord
					require.NoError(t, json.Unmarshal(line, &record))
					milestones[record.Source+"/"+record.Event] = record.Phase
					if record.Event == "transport:packet_sent" || record.Event == "transport:packet_received" {
						// Initial CID suffixes vary; endpoint identity does not.
						endpoint, _, _ := strings.Cut(record.Source, " client=")
						milestones[endpoint+"/"+record.Event] = "observed"
					}
				}
				for _, source := range []string{"client1", "client2"} {
					for _, event := range []string{"early_dial_return", "response_headers", "body_consumed", "response_tls"} {
						require.Equal(t, "test", milestones[source+"/"+event], source+"/"+event)
					}
				}
				for _, source := range []string{"client1", "client2", "listener"} {
					for _, event := range []string{"transport:packet_sent", "transport:packet_received"} {
						require.Contains(t, milestones, source+"/"+event)
					}
				}
				for _, source := range []string{"server1", "server2"} {
					// Watcher scheduling can place its handshake observation in
					// cleanup; phase is observation time, not handshake time.
					require.Contains(t, milestones, source+"/handshake_complete")
					for _, event := range []string{"accept_enter", "accept_return", "admitted", "handler_enter", "handler_write", "serve_return"} {
						require.Equal(t, "test", milestones[source+"/"+event], source+"/"+event)
					}
				}
				require.Equal(t, "cleanup", milestones["listener/close_enter"])
				require.Equal(t, "cleanup", milestones["client2/close_enter"])
			default:
				require.Contains(t, string(data), "deliberate_dial_error")
				require.Contains(t, string(data), "client dial=2")
			}
		})
	}
}

func TestHTTPCaptureHotswapPassingCleanup(t *testing.T) {
	binary, err := os.Executable()
	require.NoError(t, err)
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestHTTP3ServerHotswap$", "-test.timeout=15s", "-version=2")
	cmd.Env = append(os.Environ(), "QUIC_GO_HTTP_CAPTURE_DIR="+root, "QUIC_GO_HTTP_CAPTURE_FAIL_TEST=")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Empty(t, entries)
}
