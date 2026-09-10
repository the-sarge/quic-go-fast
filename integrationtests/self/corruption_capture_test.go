package self_test

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	quicproxy "github.com/quic-go/quic-go/integrationtests/tools/proxy"
	"github.com/quic-go/quic-go/qlog"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/ipv4"
)

func readCorruptionCapture(t *testing.T, path string) []corruptionCaptureRecord {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var records []corruptionCaptureRecord
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var record corruptionCaptureRecord
		require.NoError(t, json.Unmarshal([]byte(line), &record))
		records = append(records, record)
	}
	return records
}

func TestCorruptionCaptureBeforeMutation(t *testing.T) {
	t.Setenv("QUIC_GO_CORRUPTION_CAPTURE_DIR", t.TempDir())
	c := newCorruptionCapture(t, "coalesced datagram")
	// Literal QUIC v1 Initial (10 bytes), Handshake (9 bytes), then short header.
	// Length fields include a one-byte protected packet number/payload placeholder.
	packet := []byte{0xc0, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0xe0, 0, 0, 0, 1, 0, 0, 1, 0, 0x40, 0xaa}
	draws := []int{0, 18, 0xff}
	proxy := corruptionProxy{
		direction: quicproxy.DirectionOutgoing, diagnostics: &handshakeDiagnostics{capture: c},
		intN: func(int) int { v := draws[0]; draws = draws[1:]; return v },
		write: func(_ quicproxy.Direction, b []byte) (int, error) {
			require.Equal(t, byte(0xff), b[18])
			return len(b), nil
		},
	}
	addr := &net.UDPAddr{Port: 1234}
	require.True(t, proxy.drop(quicproxy.DirectionOutgoing, addr, addr, packet))
	c.finish("controlled mutation")
	records := readCorruptionCapture(t, c.path)
	require.Equal(t, "before_mutation", records[1].Source)
	require.Contains(t, records[1].Data, "Initial@0+10,Handshake@10+9,1-RTT@19+2")
	require.Contains(t, records[1].Data, "payload=c0000000010000000100e0000000010000010040aa")
	require.Contains(t, records[2].Data, "offset=18 before=0 after=255")
}

func TestCorruptionCaptureSocketErrors(t *testing.T) {
	t.Setenv("QUIC_GO_CORRUPTION_CAPTURE_DIR", t.TempDir())
	c := newCorruptionCapture(t, "socket errors")
	udp := newUDPConnLocalhost(t)
	socket := &corruptionUDPConn{UDPConn: udp, batch: ipv4.NewPacketConn(udp), capture: c, source: "socket controlled"}
	require.NoError(t, udp.Close())
	n, err := socket.WriteTo([]byte("original bytes"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234})
	require.Zero(t, n)
	require.ErrorIs(t, err, net.ErrClosed)
	n, err = socket.ReadBatch([]ipv4.Message{{Buffers: [][]byte{make([]byte, 128)}}}, 0)
	require.LessOrEqual(t, n, 0)
	require.Error(t, err)
	c.finish("controlled errors")
	records := readCorruptionCapture(t, c.path)
	require.Contains(t, records[1].Data, "operation=write")
	require.Contains(t, records[1].Data, "result_bytes=0")
	require.Contains(t, records[1].Data, "closed network connection")
	require.Contains(t, records[1].Data, "payload=6f726967696e616c206279746573")
	require.Contains(t, records[2].Data, "operation=read_batch")
}

func TestCorruptionCaptureHandshakePrefix(t *testing.T) {
	t.Setenv("QUIC_GO_CORRUPTION_CAPTURE_DIR", t.TempDir())
	c := newCorruptionCapture(t, "prefix")
	d := &handshakeDiagnostics{capture: c}
	r := d.tracer(t.Context(), true, quic.ConnectionID{}).AddProducer()
	defer r.Close()
	for i := range 300 {
		r.RecordEvent(qlog.ConnectionClosed{ApplicationError: new(qlog.ApplicationErrorCode(0)), Reason: fmt.Sprintf("handshake-%03d", i)})
	}
	longReason := strings.Repeat("x", 10000)
	r.RecordEvent(qlog.ConnectionClosed{ApplicationError: new(qlog.ApplicationErrorCode(0)), Reason: longReason})
	c.finish("Dial returned: context deadline exceeded")
	before, err := os.ReadFile(c.path)
	require.NoError(t, err)
	for range 400 {
		d.record(time.Now(), "teardown", "later traffic")
	}
	after, err := os.ReadFile(c.path)
	require.NoError(t, err)
	require.Equal(t, before, after, "teardown cannot alter the finalized handshake prefix")
	records := readCorruptionCapture(t, c.path)
	require.Len(t, records, 303)
	require.Contains(t, records[1].Data, "handshake-000")
	require.Contains(t, records[300].Data, "handshake-299")
	require.Contains(t, records[301].Data, longReason)
	require.Equal(t, "dial_finished", records[302].Source)
	require.Contains(t, records[302].Data, "dropped_or_truncated=0")
}

func TestCorruptionCaptureOverflow(t *testing.T) {
	t.Setenv("QUIC_GO_CORRUPTION_CAPTURE_DIR", t.TempDir())
	c := newCorruptionCapture(t, "overflow")
	for range 150 {
		c.record(time.Now(), "large", strings.Repeat("x", corruptionEventBytes))
	}
	c.finish("controlled overflow")
	info, err := os.Stat(c.path)
	require.NoError(t, err)
	require.Less(t, info.Size(), int64(corruptionCaptureBytes+4096), "reserve only small status records beyond the event budget")
	records := readCorruptionCapture(t, c.path)
	var limitRecords int
	for _, record := range records {
		if record.Source == "capture_limit" {
			limitRecords++
		}
	}
	require.Equal(t, 1, limitRecords, "overflow must be visible even if finalization never runs")
	require.Contains(t, records[len(records)-1].Data, "dropped_or_truncated=")
	require.NotContains(t, records[len(records)-1].Data, "dropped_or_truncated=0")
}

func TestCorruptionCaptureWithoutCleanup(t *testing.T) {
	const childMode = "QUIC_GO_CAPTURE_ABORT_TEST"
	if os.Getenv(childMode) == "1" {
		c := newCorruptionCapture(t, "abrupt process exit")
		c.record(time.Now(), "last_completed_observation", "saved before exit")
		os.Exit(23) // Deliberately bypass all test cleanup and file Close/Sync.
	}
	dir := t.TempDir()
	executable, err := os.Executable()
	require.NoError(t, err)
	cmd := exec.CommandContext(t.Context(), executable, "-test.run=^TestCorruptionCaptureWithoutCleanup$", "-test.timeout=10s")
	cmd.Env = append(os.Environ(), childMode+"=1", "QUIC_GO_CORRUPTION_CAPTURE_DIR="+dir)
	output, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "%s", output)
	require.Equal(t, 23, exitErr.ExitCode())
	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	require.NoError(t, err)
	require.Len(t, files, 1)
	records := readCorruptionCapture(t, files[0])
	require.Len(t, records, 2)
	require.Equal(t, "capture_start", records[0].Source)
	require.Equal(t, "last_completed_observation", records[1].Source)
}

func TestCorruptionCaptureInterruptedFixtureCleanup(t *testing.T) {
	const childMode = "QUIC_GO_CAPTURE_FIXTURE_ABORT_TEST"
	if os.Getenv(childMode) == "1" {
		// FailNow unwinds this test's defers before testing runs registered cleanup.
		// Exit here to prove the actual Dial failure was persisted without it.
		defer os.Exit(24)
		testMITMCorruptPacketsWithRandom(t, quicproxy.DirectionOutgoing, func(int) int { return 0 })
		return
	}
	dir := t.TempDir()
	executable, err := os.Executable()
	require.NoError(t, err)
	cmd := exec.CommandContext(t.Context(), executable, "-test.run=^TestCorruptionCaptureInterruptedFixtureCleanup$", "-test.timeout=15s")
	cmd.Env = append(os.Environ(), childMode+"=1", "QUIC_GO_CORRUPTION_CAPTURE_DIR="+dir)
	output, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "%s", output)
	require.Equal(t, 24, exitErr.ExitCode())
	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	require.NoError(t, err)
	require.Len(t, files, 1)
	records := readCorruptionCapture(t, files[0])
	last := records[len(records)-1]
	require.Equal(t, "dial_finished", last.Source)
	require.Contains(t, last.Data, "Dial returned: context deadline exceeded")
	require.Contains(t, last.Data, "dropped_or_truncated=0")
}

func TestCorruptionCaptureIncompleteRecords(t *testing.T) {
	t.Setenv("QUIC_GO_CORRUPTION_CAPTURE_DIR", t.TempDir())
	t.Run("oversized event", func(t *testing.T) {
		c := newCorruptionCapture(t, "oversized qlog")
		d := &handshakeDiagnostics{capture: c}
		r := d.tracer(t.Context(), true, quic.ConnectionID{}).AddProducer()
		defer r.Close()
		r.RecordEvent(qlog.ConnectionClosed{ApplicationError: new(qlog.ApplicationErrorCode(0)), Reason: strings.Repeat("x", corruptionEventBytes+1)})
		c.finish("controlled large event")
		records := readCorruptionCapture(t, c.path)
		require.Contains(t, records[1].Data, "encode_error=short write")
		require.Contains(t, records[1].Data, "[capture record truncated]")
		require.Contains(t, records[len(records)-1].Data, "dropped_or_truncated=1")
	})
	t.Run("failed file write", func(t *testing.T) {
		c := newCorruptionCapture(t, "failed output")
		require.NoError(t, c.file.Close())
		c.record(time.Now(), "unwritten", "must not report success")
		c.finish("must not appear")
		require.Error(t, c.err)
		records := readCorruptionCapture(t, c.path)
		require.Len(t, records, 1, "missing final marker explicitly leaves the capture incomplete")
	})
}
