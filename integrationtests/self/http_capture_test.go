package self_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
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
	for _, fixture := range []string{"TestHTTPServerIdleTimeout", "TestHTTPReestablishConnectionAfterDialError", "TestHTTP3ServerHotswap"} {
		t.Run(fixture, func(t *testing.T) { checkHTTPCaptureFixture(t, fixture) })
	}
}

// checkHTTPCaptureFixture runs fixture to its controlled failure in a child
// process and checks the retained evidence.
func checkHTTPCaptureFixture(t *testing.T, fixture string) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	root, output, err := runHTTPCaptureChild(ctx, t, fixture, fixture)
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
}

func TestHTTPCaptureHotswapPassingCleanup(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	root, output, err := runHTTPCaptureChild(ctx, t, "TestHTTP3ServerHotswap", "")
	require.NoError(t, err, string(output))
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Empty(t, entries)
}

// runHTTPCaptureChild runs fixture in a child test process whose captures go to
// a private root, arming the controlled failure seam for failTest. If the
// caller's checks fail, the child's evidence is preserved in the outer capture
// directory before the private root is removed.
func runHTTPCaptureChild(ctx context.Context, t *testing.T, fixture, failTest string) (root string, output []byte, err error) {
	t.Helper()
	binary, err := os.Executable()
	require.NoError(t, err)
	root = t.TempDir()
	cmd := exec.CommandContext(ctx, binary, "-test.run=^"+fixture+"$", "-test.timeout=15s", "-version=2")
	cmd.Env = append(os.Environ(), "QUIC_GO_HTTP_CAPTURE_DIR="+root, "QUIC_GO_HTTP_CAPTURE_FAIL_TEST="+failTest)
	// Cleanups run last-in first-out, so this runs before the TempDir removal,
	// including after FailNow. The caller's deferred cancel has run by then.
	var ctxErr error
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		outer := os.Getenv("QUIC_GO_HTTP_CAPTURE_DIR")
		if outer == "" {
			t.Logf("HTTP capture check child not preserved: QUIC_GO_HTTP_CAPTURE_DIR is unset; %s is removed with this test", root)
			return
		}
		c := preserveHTTPCaptureChild(outer, t.Name(), map[string]any{
			"parent_test":             t.Name(),
			"child_fixture":           fixture,
			"child_command":           cmd.Args,
			"controlled_seam":         failTest,
			"controlled_seam_armed":   failTest != "",
			"controlled_seam_reached": bytes.Contains(output, []byte(httpCaptureFailureSeam)),
			"child_error":             fmt.Sprint(err),
			"child_context_error":     fmt.Sprint(ctxErr),
			"private_root":            root,
		}, root, output)
		t.Logf("HTTP capture check child preservation: path=%s observed=%d retained=%d dropped=%d error=%v", c.path, c.observed, c.retained, c.dropped, c.err)
	})
	output, err = cmd.CombinedOutput()
	ctxErr = ctx.Err()
	return root, output, err
}

// The capture check's own child is replaced by a natural failure before the
// controlled seam. The check process applies the unchanged assertions.
func TestHTTPCaptureCheckPreservation(t *testing.T) {
	const checkMode = "QUIC_GO_HTTP_CAPTURE_CHECK"
	if os.Getenv("QUIC_GO_HTTP_CAPTURE_FAIL_TEST") == t.Name() {
		c := newHTTPCapture(t)
		c.record("fixture", "natural_child_marker", "before the controlled seam")
		t.Fatal("natural child failure before the controlled seam")
	}
	if fixture := os.Getenv(checkMode); fixture != "" {
		checkHTTPCaptureFixture(t, fixture)
		return
	}
	binary, err := os.Executable()
	require.NoError(t, err)
	name := t.Name()
	runCheck := func(t *testing.T, fixture string, env ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, "-test.run=^"+name+"$", "-test.v", "-test.timeout=25s", "-version=2")
		for _, kv := range os.Environ() {
			if !strings.HasPrefix(kv, "QUIC_GO_HTTP_CAPTURE_") {
				cmd.Env = append(cmd.Env, kv)
			}
		}
		cmd.Env = append(append(cmd.Env, checkMode+"="+fixture), env...)
		output, err := cmd.CombinedOutput()
		require.NoError(t, ctx.Err(), string(output))
		return output, err
	}

	t.Run("natural child failure", func(t *testing.T) {
		outer := t.TempDir()
		output, err := runCheck(t, name, "QUIC_GO_HTTP_CAPTURE_DIR="+outer)
		require.Error(t, err, "a natural child failure must still fail the check")
		slots, err := filepath.Glob(filepath.Join(outer, "slot-*"))
		require.NoError(t, err)
		require.Len(t, slots, 1, string(output))
		slot := slots[0]

		var provenance map[string]any
		records, err := os.ReadFile(filepath.Join(slot, "capture.jsonl"))
		require.NoError(t, err)
		for line := range bytes.SplitSeq(bytes.TrimSpace(records), []byte("\n")) {
			var record httpCaptureRecord
			require.NoError(t, json.Unmarshal(line, &record))
			if record.Event == "capture_check_child" {
				provenance = record.Data.(map[string]any)
			}
		}
		require.NotNil(t, provenance, string(records))
		require.Equal(t, name, provenance["parent_test"])
		require.Equal(t, name, provenance["child_fixture"])
		require.Contains(t, provenance["child_command"], "-test.run=^"+name+"$")
		require.Equal(t, true, provenance["controlled_seam_armed"])
		require.Equal(t, false, provenance["controlled_seam_reached"])
		require.Contains(t, string(records), `"complete_recording_window":true`)

		// The child's own checksum, written in the child process, proves the
		// preserved capture is complete rather than a truncated quotation.
		child, err := os.ReadFile(filepath.Join(slot, "child-slot-0-capture.jsonl"))
		require.NoError(t, err)
		require.Contains(t, string(child), "natural_child_marker")
		require.Contains(t, string(child), `"failed":true`)
		childSums, err := os.ReadFile(filepath.Join(slot, "child-slot-0-SHA256SUMS"))
		require.NoError(t, err)
		require.Equal(t, fmt.Sprintf("%x  capture.jsonl\n", sha256.Sum256(child)), string(childSums))
		childOutput, err := os.ReadFile(filepath.Join(slot, "child-output.log"))
		require.NoError(t, err)
		require.Contains(t, string(childOutput), "natural child failure before the controlled seam")

		sums, err := os.ReadFile(filepath.Join(slot, "SHA256SUMS"))
		require.NoError(t, err)
		var want strings.Builder
		for _, file := range []string{"capture.jsonl", "child-output.log", "child-slot-0-SHA256SUMS", "child-slot-0-capture.jsonl"} {
			data, err := os.ReadFile(filepath.Join(slot, file))
			require.NoError(t, err)
			fmt.Fprintf(&want, "%x  %s\n", sha256.Sum256(data), file)
		}
		require.Equal(t, want.String(), string(sums))
	})

	t.Run("passing check", func(t *testing.T) {
		outer := t.TempDir()
		output, err := runCheck(t, "TestHTTPServerIdleTimeout", "QUIC_GO_HTTP_CAPTURE_DIR="+outer)
		require.NoError(t, err, string(output))
		entries, err := os.ReadDir(outer)
		require.NoError(t, err)
		require.Empty(t, entries)
	})

	t.Run("outer directory unset", func(t *testing.T) {
		output, err := runCheck(t, name)
		require.Error(t, err, "a natural child failure must still fail the check")
		require.Contains(t, string(output), "HTTP capture check child not preserved: QUIC_GO_HTTP_CAPTURE_DIR is unset")
	})
}

func newHTTPCaptureChildRoot(t *testing.T, capture []byte) string {
	t.Helper()
	private := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(private, "slot-0"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(private, "slot-0", "capture.jsonl"), capture, 0o600))
	return private
}

func TestHTTPCaptureCheckPreservationQuota(t *testing.T) {
	outer := t.TempDir()
	for i := range httpCaptureSlots {
		require.NoError(t, os.Mkdir(filepath.Join(outer, fmt.Sprintf("slot-%d", i)), 0o700))
	}
	c := preserveHTTPCaptureChild(outer, t.Name(), map[string]any{}, newHTTPCaptureChildRoot(t, []byte("{}\n")), []byte("output"))
	require.ErrorContains(t, c.err, "quota exhausted")
	entries, err := os.ReadDir(outer)
	require.NoError(t, err)
	require.Len(t, entries, httpCaptureSlots)
	for _, entry := range entries {
		files, err := os.ReadDir(filepath.Join(outer, entry.Name()))
		require.NoError(t, err)
		require.Empty(t, files, "earlier slots are never overwritten")
	}
}

func TestHTTPCaptureCheckPreservationBudget(t *testing.T) {
	outer := t.TempDir()
	capture := bytes.Repeat([]byte("x"), httpCaptureSlotBytes)
	c := preserveHTTPCaptureChild(outer, t.Name(), map[string]any{}, newHTTPCaptureChildRoot(t, capture), []byte("output"))
	require.NoError(t, c.err)
	slot := filepath.Dir(c.path)
	records, err := os.ReadFile(c.path)
	require.NoError(t, err)
	require.Contains(t, string(records), `"name":"child-slot-0-capture.jsonl","truncated":true`)
	require.Contains(t, string(records), `"name":"child-output.log","truncated":true`)
	require.Contains(t, string(records), `"complete_recording_window":false`)
	var total int64
	entries, err := os.ReadDir(slot)
	require.NoError(t, err)
	for _, entry := range entries {
		info, err := entry.Info()
		require.NoError(t, err)
		total += info.Size()
	}
	require.LessOrEqual(t, total, int64(httpCaptureSlotBytes))
	preserved, err := os.ReadFile(filepath.Join(slot, "child-slot-0-capture.jsonl"))
	require.NoError(t, err)
	require.Equal(t, capture[:len(preserved)], preserved, "truncation keeps the leading bytes")
}
