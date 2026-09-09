package self_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/testutils/simnet"

	"github.com/stretchr/testify/require"
)

type handshakeDiagnosticLog struct {
	failed bool
	lines  []string
}

func TestHandshakeDiagnosticsBoundedTrace(t *testing.T) {
	d := &handshakeDiagnostics{scenario: "bounded trace"}
	d.phase(true, "read stream")
	r := d.tracer(context.Background(), true, quic.ConnectionID{}).AddProducer()
	for i := range 300 {
		r.RecordEvent(qlog.ConnectionClosed{ApplicationError: new(qlog.ApplicationErrorCode(0)), Reason: fmt.Sprintf("reason-%03d", i)})
	}
	require.NoError(t, r.Close())
	l := &handshakeDiagnosticLog{failed: true}
	d.logFailure(l)
	output := strings.Join(l.lines, "\n")
	require.Contains(t, output, "omitted=44")
	require.Contains(t, output, "client=read stream")
	require.NotContains(t, output, "reason-043")
	require.Contains(t, output, "reason-044")
	require.Contains(t, output, "reason-299")
	require.Contains(t, output, `"reason":"reason-299"`)
	require.Contains(t, output, "transport:connection_closed")
}

func TestHandshakeDiagnosticsTruncation(t *testing.T) {
	d := &handshakeDiagnostics{scenario: "oversized event"}
	r := d.tracer(context.Background(), false, quic.ConnectionID{}).AddProducer()
	r.RecordEvent(qlog.ConnectionClosed{ApplicationError: new(qlog.ApplicationErrorCode(0)), Reason: strings.Repeat("x", 10000)})
	d.phase(false, strings.Repeat("p", 10000))
	l := &handshakeDiagnosticLog{failed: true}
	d.logFailure(l)
	output := strings.Join(l.lines, "\n")
	require.Contains(t, output, "[truncated]")
	require.Contains(t, output, "short write")
	require.Less(t, len(output), 9000, "both the retained event and latest phase must be bounded")
}

func TestHandshakeDiagnosticsEventSnapshot(t *testing.T) {
	d := &handshakeDiagnostics{scenario: "event lifetime"}
	r := d.tracer(t.Context(), true, quic.ConnectionID{}).AddProducer()
	code := qlog.ApplicationErrorCode(7)
	r.RecordEvent(qlog.ConnectionClosed{ApplicationError: &code, Reason: "original"})
	code = 42
	l := &handshakeDiagnosticLog{failed: true}
	d.logFailure(l)
	output := strings.Join(l.lines, "\n")
	require.Contains(t, output, `"error_code":7`)
	require.NotContains(t, output, `"error_code":42`)
}

func (l *handshakeDiagnosticLog) Failed() bool { return l.failed }
func (l *handshakeDiagnosticLog) Logf(format string, args ...any) {
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func TestHandshakeDiagnosticsFailureReport(t *testing.T) {
	d := &handshakeDiagnostics{scenario: "direction=to server retry=false post_quantum=true long_chain=false speaking=server first version=v1"}
	d.phase(true, "accept stream")
	d.phase(false, "close stream: complete")
	l := &handshakeDiagnosticLog{}
	d.logFailure(l)
	require.Empty(t, l.lines, "passing tests must not emit diagnostics")
	l.failed = true
	d.logFailure(l)
	output := strings.Join(l.lines, "\n")
	require.Contains(t, output, d.scenario)
	require.Contains(t, output, "client=accept stream")
	require.Contains(t, output, "server=close stream: complete")
}

func TestHandshakeDiagnosticsLossObserver(t *testing.T) {
	d := &handshakeDiagnostics{scenario: "loss forwarding"}
	wantDirections := []direction{directionToServer, directionToClient, directionToServer}
	wantDecisions := []bool{true, false, false}
	var seen []direction
	packet := simnet.Packet{
		From: &net.UDPAddr{IP: net.IPv4(1, 0, 0, 1), Port: 9001},
		To:   &net.UDPAddr{IP: net.IPv4(1, 0, 0, 2), Port: 9002},
		Data: []byte("123456789"),
	}
	observe := d.observeDrop(func(dir direction, p simnet.Packet) bool {
		require.Equal(t, packet, p)
		seen = append(seen, dir)
		return wantDecisions[len(seen)-1]
	})
	for i, dir := range wantDirections {
		require.Equal(t, wantDecisions[i], observe(dir, packet))
	}
	require.Equal(t, wantDirections, seen)
	l := &handshakeDiagnosticLog{failed: true}
	d.logFailure(l)
	output := strings.Join(l.lines, "\n")
	require.Contains(t, output, "direction=to server drop=true")
	require.Contains(t, output, "direction=to client drop=false")
	require.Contains(t, output, "crc32c=3808858755") // CRC32c's published check value for "123456789".
}

func TestHandshakeDiagnosticsConcurrentProducers(t *testing.T) {
	d := &handshakeDiagnostics{scenario: "concurrent producers"}
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Go(func() {
			r := d.tracer(context.Background(), i%2 == 0, quic.ConnectionID{}).AddProducer()
			defer r.Close()
			for range 100 {
				d.phase(i%2 == 0, "reading")
				r.RecordEvent(qlog.MTUUpdated{Value: 1200})
				d.logFailure(&handshakeDiagnosticLog{failed: true})
			}
		})
	}
	wg.Wait()
	l := &handshakeDiagnosticLog{failed: true}
	d.logFailure(l)
	require.Contains(t, strings.Join(l.lines, "\n"), "retained=256 omitted=144")
}

func TestHandshakeDiagnosticsCleanup(t *testing.T) {
	const childMode = "QUIC_GO_HANDSHAKE_DIAGNOSTIC_TEST"
	if mode := os.Getenv(childMode); mode != "" {
		synctest.Test(t, func(t *testing.T) {
			d := newHandshakeDiagnostics(t, "requested_direction=to server loss=controlled retry=false speaking=server first post_quantum=true long_chain=false version=v1")
			d.phase(true, "read stream")
			d.phase(false, "write stream returned: bytes=5000 err=<nil>")
			r := d.tracer(t.Context(), true, quic.ConnectionID{}).AddProducer()
			r.RecordEvent(qlog.MTUUpdated{Value: 1200})
			require.NoError(t, r.Close())
			packet := simnet.Packet{From: &net.UDPAddr{Port: 9001}, To: &net.UDPAddr{Port: 9002}, Data: []byte("123456789")}
			d.observeDrop(func(direction, simnet.Packet) bool { return false })(directionToServer, packet)
			if mode == "fail" {
				t.Fatal("synthetic failure exercises the actual cleanup path")
			}
		})
		return
	}
	executable, err := os.Executable()
	require.NoError(t, err)
	for _, mode := range []string{"pass", "fail"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestHandshakeDiagnosticsCleanup$", "-test.v", "-test.timeout=10s")
			cmd.Env = append(os.Environ(), childMode+"="+mode)
			output, err := cmd.CombinedOutput()
			if mode == "pass" {
				require.NoError(t, err, "%s", output)
				require.NotContains(t, string(output), "packet-loss diagnostics:")
				return
			}
			require.Error(t, err)
			require.Contains(t, string(output), "synthetic failure")
			require.Contains(t, string(output), "packet-loss diagnostics: requested_direction=to server loss=controlled retry=false speaking=server first post_quantum=true long_chain=false version=v1")
			require.Contains(t, string(output), "client=read stream")
			require.Contains(t, string(output), "server=write stream returned: bytes=5000")
			require.Contains(t, string(output), "mtu_updated")
			require.Contains(t, string(output), "direction=to server drop=false")
		})
	}
}
