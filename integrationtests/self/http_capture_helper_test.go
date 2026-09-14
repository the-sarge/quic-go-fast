package self_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/quic-go/quic-go/qlogwriter/jsontext"
	"github.com/quic-go/quic-go/testutils/events"
)

const (
	httpCaptureSlotBytes    = 32 << 20
	httpCaptureEventBytes   = 256 << 10
	httpCaptureSummaryBytes = 8 << 10
	httpCaptureSlots        = 8
)

// Slots reserve a finite amount of disk space across test processes. A crashed
// process keeps its reservation and evidence; only an intact passing capture
// releases its own slot. This directory belongs exclusively to this recorder.
type httpCapture struct {
	mu                                          sync.Mutex
	file                                        *os.File
	path                                        string
	phase                                       string
	written, limit                              int
	observed, retained, dropped, encodingErrors uint64
	err                                         error
	done                                        bool
	stop                                        chan struct{}
	workers                                     sync.WaitGroup
}

type httpCaptureRecord struct {
	Time   time.Time `json:"time"`
	Phase  string    `json:"phase"`
	Source string    `json:"source"`
	Event  string    `json:"event"`
	Data   any       `json:"data"`
}

func openHTTPCapture(root, name string) *httpCapture {
	c := &httpCapture{phase: "test", limit: httpCaptureSlotBytes - httpCaptureSummaryBytes - 1024, stop: make(chan struct{})}
	if err := os.MkdirAll(root, 0o700); err != nil {
		c.err = err
		return c
	}
	for i := range httpCaptureSlots {
		dir := filepath.Join(root, fmt.Sprintf("slot-%d", i))
		err := os.Mkdir(dir, 0o700)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			c.err = err
			return c
		}
		c.path = filepath.Join(dir, "capture.jsonl")
		c.file, c.err = os.OpenFile(c.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		break
	}
	if c.file == nil {
		if c.err == nil {
			c.err = fmt.Errorf("HTTP capture quota exhausted: %d slots reserved", httpCaptureSlots)
		}
		return c
	}
	env := make(map[string]string)
	for _, key := range []string{"TIMESCALE_FACTOR", "GOTOOLCHAIN", "GODEBUG", "GOMAXPROCS", "QUIC_GO_DISABLE_GSO", "QUIC_GO_DISABLE_ECN", "GITHUB_SHA", "GITHUB_RUN_ID", "GITHUB_RUN_ATTEMPT", "GITHUB_JOB", "QUIC_GO_HTTP_RUN_ID"} {
		env[key] = os.Getenv(key)
	}
	c.record("fixture", "capture_start", map[string]any{"schema": 1, "test": name, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "version": version.String(), "args": os.Args, "environment": env, "source": httpCaptureSource(), "provenance": "full command output and actual seed belong to the matching run artifact", "slot_bytes": httpCaptureSlotBytes, "event_bytes": httpCaptureEventBytes})
	return c
}

// Resolve once, before either fixture opens sockets. Git failures are explicit;
// the CI run artifact also retains the checked-out HEAD/tree and source patch.
var httpCaptureSource = sync.OnceValue(func() map[string]string {
	source := make(map[string]string)
	for _, query := range []struct {
		key  string
		args []string
	}{
		{"head", []string{"rev-parse", "HEAD"}},
		{"tree", []string{"rev-parse", "HEAD^{tree}"}},
		{"status", []string{"status", "--porcelain"}},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		output, err := exec.CommandContext(ctx, "git", query.args...).CombinedOutput()
		cancel()
		if len(output) > 16<<10 {
			output = output[:16<<10]
			source[query.key+"_truncated"] = "true"
		}
		source[query.key] = strings.TrimSpace(string(output))
		if err != nil {
			source[query.key+"_error"] = err.Error()
		}
	}
	return source
})

func newHTTPCapture(t *testing.T) *httpCapture {
	t.Helper()
	root := os.Getenv("QUIC_GO_HTTP_CAPTURE_DIR")
	if root == "" {
		root = filepath.Join(os.TempDir(), "quic-go-http")
	}
	c := openHTTPCapture(root, t.Name())
	if c.err != nil {
		t.Logf("HTTP capture unavailable: %v", c.err)
	}
	// Register before the fixture's resource cleanups so this runs last.
	t.Cleanup(func() {
		c.finish(t.Failed())
		if t.Failed() || c.err != nil || c.dropped != 0 || c.encodingErrors != 0 {
			t.Logf("HTTP capture: path=%s observed=%d retained=%d dropped=%d encoding_errors=%d error=%v", c.path, c.observed, c.retained, c.dropped, c.encodingErrors, c.err)
		}
	})
	return c
}

func (c *httpCapture) record(source, event string, data any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.recordLocked(source, event, data, false)
}

// Admission, encoding and persistence share serialization with finalization.
// We never hold this mutex while waiting for network I/O or a fixture channel.
func (c *httpCapture) recordLocked(source, event string, data any, terminal bool) {
	if c.done {
		return
	}
	if !terminal {
		c.observed++
	}
	if c.err != nil {
		c.dropped++
		return
	}
	line, err := json.Marshal(httpCaptureRecord{Time: time.Now().UTC(), Phase: c.phase, Source: source, Event: event, Data: data})
	if err != nil {
		c.encodingErrors++
		c.dropped++
		return
	}
	line = append(line, '\n')
	if !terminal && (len(line) > httpCaptureEventBytes || c.written+len(line) > c.limit) {
		c.dropped++
		return
	}
	n, err := c.file.Write(line)
	c.written += n
	if err == nil && n != len(line) {
		err = io.ErrShortWrite
	}
	if err != nil {
		c.err = err
		c.dropped++
		fmt.Fprintf(os.Stderr, "HTTP capture write failed: path=%s error=%v\n", c.path, err)
		return
	}
	c.retained++
}

func (c *httpCapture) beginCleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done || c.phase == "cleanup" {
		return
	}
	c.recordLocked("fixture", "pre_teardown", "observations after this boundary belong to cleanup", false)
	c.phase = "cleanup"
}

func (c *httpCapture) finish(failed bool) {
	c.mu.Lock()
	if c.done {
		c.mu.Unlock()
		return
	}
	select {
	case <-c.stop:
		c.mu.Unlock()
		return
	default:
		close(c.stop)
	}
	c.mu.Unlock()
	c.workers.Wait() // every watcher selects stop; none waits for transport shutdown
	c.mu.Lock()
	defer c.mu.Unlock()
	c.recordLocked("fixture", "capture_finished", map[string]any{"failed": failed, "observed": c.observed, "retained_before_summary": c.retained, "dropped": c.dropped, "encoding_errors": c.encodingErrors, "complete_recording_window": c.err == nil && c.dropped == 0 && c.encodingErrors == 0, "scope": "admitted observations through fixture cleanup; not proof of absent protocol activity"}, true)
	c.done = true
	if c.file == nil {
		fmt.Fprintf(os.Stderr, "HTTP capture unavailable: %v\n", c.err)
		return
	}
	if err := c.file.Sync(); err != nil && c.err == nil {
		c.err = err
	}
	if err := c.file.Close(); err != nil && c.err == nil {
		c.err = err
	}
	// Stream the bounded file rather than retaining a second copy in memory.
	if err := checksumHTTPFile(c.path); err != nil && c.err == nil {
		c.err = err
	}
	if !failed && c.err == nil && c.dropped == 0 && c.encodingErrors == 0 {
		for _, path := range []string{filepath.Join(filepath.Dir(c.path), "SHA256SUMS"), c.path, filepath.Dir(c.path)} {
			if err := os.Remove(path); err != nil {
				c.err = err
				break
			}
		}
	}
}

func checksumHTTPFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, readErr := io.Copy(h, f)
	closeErr := f.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.WriteFile(filepath.Join(filepath.Dir(path), "SHA256SUMS"), fmt.Appendf(nil, "%x  capture.jsonl\n", h.Sum(nil)), 0o600)
}

func (c *httpCapture) observeConn(source string, conn *quic.Conn) {
	if conn == nil {
		return
	}
	c.mu.Lock()
	select {
	case <-c.stop:
		c.mu.Unlock()
		return
	default:
	}
	c.workers.Add(1)
	c.mu.Unlock()
	c.record(source, "connection", fmt.Sprintf("conn=%p local=%s remote=%s", conn, conn.LocalAddr(), conn.RemoteAddr()))
	go func() {
		defer c.workers.Done()
		handshake := conn.HandshakeComplete()
		for {
			select {
			case <-handshake:
				c.record(source, "handshake_complete", fmt.Sprintf("conn=%p", conn))
				handshake = nil
			case <-conn.Context().Done():
				c.record(source, "connection_done", fmt.Sprintf("conn=%p cause=%v", conn, context.Cause(conn.Context())))
				return
			case <-c.stop:
				complete := false
				select {
				case <-conn.HandshakeComplete():
					complete = true
				default:
				}
				c.record(source, "watcher_stop", fmt.Sprintf("conn=%p handshake_complete=%t cause=%v", conn, complete, context.Cause(conn.Context())))
				return
			}
		}
	}()
}

func (c *httpCapture) tracer(attempt string) func(context.Context, bool, quic.ConnectionID) qlogwriter.Trace {
	return func(_ context.Context, client bool, id quic.ConnectionID) qlogwriter.Trace {
		source := fmt.Sprintf("%s client=%t initial_cid=%s", attempt, client, id)
		c.record(source, "trace_created", nil)
		return &events.Trace{Recorder: &httpCaptureRecorder{capture: c, source: source}}
	}
}

// Preserve explicitly enabled qlog output while adding fixture-local recording.
func (c *httpCapture) config(cfg *quic.Config, attempt string) *quic.Config {
	cfg = cfg.Clone()
	previous := cfg.Tracer
	capture := c.tracer(attempt)
	cfg.Tracer = func(ctx context.Context, client bool, id quic.ConnectionID) qlogwriter.Trace {
		trace := capture(ctx, client, id)
		if previous != nil {
			if other := previous(ctx, client, id); other != nil {
				return &multiplexedTrace{Traces: []qlogwriter.Trace{trace, other}}
			}
		}
		return trace
	}
	return cfg
}

type httpCaptureRecorder struct {
	capture *httpCapture
	source  string
}

func (r *httpCaptureRecorder) RecordEvent(ev qlogwriter.Event) {
	c := r.capture
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done {
		return
	}
	var buffer handshakeDiagnosticBuffer
	buffer.limit = httpCaptureEventBytes
	err := ev.Encode(jsontext.NewEncoder(&buffer), time.Now())
	if err != nil {
		c.encodingErrors++
	}
	// Data is a string so a partial encoder output remains valid outer JSON.
	c.recordLocked(r.source, ev.Name(), map[string]any{"encoded": buffer.String(), "encode_error": fmt.Sprint(err)}, false)
}

func (r *httpCaptureRecorder) Close() error {
	r.capture.record(r.source, "producer_closed", nil)
	return nil
}

// The handler retains scalar log values synchronously, never a live slog.Record.
type httpCaptureLog struct {
	capture *httpCapture
	attrs   []slog.Attr
	group   string
}

func (*httpCaptureLog) Enabled(context.Context, slog.Level) bool { return true }
func (h *httpCaptureLog) Handle(_ context.Context, r slog.Record) error {
	values := make(map[string]string)
	for _, a := range h.attrs {
		values[a.Key] = a.Value.String()
	}
	r.Attrs(func(a slog.Attr) bool { values[h.group+a.Key] = a.Value.String(); return true })
	h.capture.record("http3", r.Message, values)
	return nil
}

func (h *httpCaptureLog) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	clone.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &clone
}

func (h *httpCaptureLog) WithGroup(name string) slog.Handler {
	clone := *h
	if strings.TrimSpace(name) != "" {
		clone.group += name + "."
	}
	return &clone
}

// Only the explicit subprocess test arms this seam; ordinary invocations have
// no injected failure, network delay, or altered assertion.
func maybeFailHTTPCaptureFixture(t *testing.T) {
	t.Helper()
	if os.Getenv("QUIC_GO_HTTP_CAPTURE_FAIL_TEST") == t.Name() {
		t.Fatal("controlled HTTP capture fixture failure")
	}
}
