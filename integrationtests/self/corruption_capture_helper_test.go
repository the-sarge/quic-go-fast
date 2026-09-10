package self_test

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

const (
	corruptionCaptureBytes = 32 << 20
	corruptionEventBytes   = 256 << 10
)

// This is an append-only handshake prefix, independent of the diagnostic tail.
// Each record is written before returning to its producer. No userspace buffer
// or cleanup is required to retain completed writes after a process is killed.
// It is not a power-loss guarantee, and an interrupted final line can be partial.
type corruptionCapture struct {
	mu      sync.Mutex
	file    *os.File
	path    string
	written int
	dropped uint64
	err     error
	done    bool
	limited bool
}

type corruptionCaptureRecord struct {
	Time   time.Time `json:"time"`
	Source string    `json:"source"`
	Data   string    `json:"data"`
}

func newCorruptionCapture(t *testing.T, scenario string) *corruptionCapture {
	root := os.Getenv("QUIC_GO_CORRUPTION_CAPTURE_DIR")
	if root == "" {
		root = filepath.Join(os.TempDir(), "quic-go-corruption")
	}
	c := &corruptionCapture{}
	if err := os.MkdirAll(root, 0o700); err != nil {
		c.err = err
	} else {
		c.file, c.err = os.CreateTemp(root, "handshake-*.jsonl")
		if c.file != nil {
			c.path = c.file.Name()
		}
	}
	c.record(time.Now(), "capture_start", fmt.Sprintf("schema=1 test=%s scenario=%s go=%s os=%s arch=%s GSO_disabled=%q ECN_disabled=%q TIMESCALE_FACTOR=%q GITHUB_SHA=%q GODEBUG=%q GOMAXPROCS=%q; prefix through Dial return; limits_bytes=%d event_bytes=%d", t.Name(), scenario, runtime.Version(), runtime.GOOS, runtime.GOARCH, os.Getenv("QUIC_GO_DISABLE_GSO"), os.Getenv("QUIC_GO_DISABLE_ECN"), os.Getenv("TIMESCALE_FACTOR"), os.Getenv("GITHUB_SHA"), os.Getenv("GODEBUG"), os.Getenv("GOMAXPROCS"), corruptionCaptureBytes, corruptionEventBytes))
	if c.err != nil {
		t.Logf("corruption capture unavailable: %v", c.err)
	}
	t.Cleanup(func() {
		c.finish("fixture cleanup without Dial completion")
		if t.Failed() || c.err != nil {
			t.Logf("corruption capture: path=%s dropped=%d error=%v", c.path, c.dropped, c.err)
			return
		}
		if err := os.Remove(c.path); err != nil {
			t.Logf("corruption capture removal: %v", err)
		}
	})
	return c
}

func (c *corruptionCapture) record(at time.Time, source, data string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done || c.err != nil {
		return
	}
	if c.limited {
		c.dropped++
		return
	}
	c.write(at, source, data, false)
}

func (c *corruptionCapture) write(at time.Time, source, data string, terminal bool) {
	// JSON represents Data as UTF-8 text. Normalize before applying the byte
	// limit, and never cut a multi-byte character at the truncation boundary.
	data = strings.ToValidUTF8(data, "�")
	if len(data) > corruptionEventBytes {
		const marker = " [capture record truncated]"
		end := corruptionEventBytes - len(marker)
		for !utf8.RuneStart(data[end]) {
			end--
		}
		data = data[:end] + marker
		c.dropped++
	}
	line, err := json.Marshal(corruptionCaptureRecord{Time: at, Source: source, Data: data})
	if err != nil {
		c.err = err
		return
	}
	line = append(line, '\n')
	if !terminal && c.written+len(line) > corruptionCaptureBytes {
		c.limited = true
		c.dropped++
		c.write(at, "capture_limit", "event byte budget exhausted; subsequent events omitted", true)
		return
	}
	n, err := c.file.Write(line)
	c.written += n
	if err == nil && n != len(line) {
		err = io.ErrShortWrite
	}
	if err != nil {
		c.err = err
		fmt.Fprintf(os.Stderr, "corruption capture write failed: path=%s error=%v\n", c.path, err)
	}
}

// Finalization precedes transport/proxy cleanup. Later traffic cannot evict or
// append to the handshake prefix. A missing final record means incomplete capture.
func (c *corruptionCapture) finish(outcome string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done {
		return
	}
	c.done = true
	if c.file == nil {
		return
	}
	if c.err == nil {
		c.write(time.Now(), "dial_finished", fmt.Sprintf("%s dropped_or_truncated=%d", outcome, c.dropped), true)
	}
	if err := c.file.Sync(); err != nil && c.err == nil {
		c.err = err
	}
	if err := c.file.Close(); err != nil && c.err == nil {
		c.err = err
	}
}

func (c *corruptionCapture) capturing() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.done
}
