package self_test

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	quicproxy "github.com/quic-go/quic-go/integrationtests/tools/proxy"
	"github.com/stretchr/testify/require"
)

func TestCorruptionDiagnosticsCallback(t *testing.T) {
	for _, direction := range []quicproxy.Direction{quicproxy.DirectionIncoming, quicproxy.DirectionOutgoing} {
		t.Run(direction.String(), func(t *testing.T) {
			d := &handshakeDiagnostics{scenario: "callback"}
			var bounds []int
			draws := []int{1, 1, 0, 1, 42, 0, 0, 0x80}
			var writes [][]byte
			writeErr := errors.New("controlled write error")
			p := &corruptionProxy{
				direction: direction, diagnostics: d,
				intN: func(n int) int {
					bounds = append(bounds, n)
					require.NotEmpty(t, draws)
					v := draws[0]
					draws = draws[1:]
					return v
				},
				write: func(dir quicproxy.Direction, b []byte) (int, error) {
					require.Equal(t, direction, dir)
					writes = append(writes, append([]byte(nil), b...))
					return 1, writeErr
				},
			}
			from, to := &net.UDPAddr{Port: 9001}, &net.UDPAddr{Port: 9002}
			opposite := quicproxy.DirectionIncoming
			if direction == opposite {
				opposite = quicproxy.DirectionOutgoing
			}
			unchanged := []byte{0x80, 7}
			require.False(t, p.drop(opposite, from, to, unchanged))
			require.Empty(t, bounds, "opposite direction consumes no randomness")
			require.False(t, p.drop(direction, from, to, unchanged))
			require.Equal(t, []byte{0x80, 7}, unchanged)
			require.False(t, p.drop(direction, from, to, []byte{0x40, 7}))
			changed := []byte{0x80, 7}
			require.True(t, p.drop(direction, from, to, changed), "write errors must not change the original drop decision")
			require.Equal(t, []byte{0x80, 42}, changed)
			noOp := []byte{0x80, 7}
			require.True(t, p.drop(direction, from, to, noOp))
			require.Equal(t, []byte{0x80, 7}, noOp, "selection may choose the original byte")
			require.Equal(t, []int{4, 20, 4, 2, 256, 4, 2, 256}, bounds)
			require.Empty(t, draws)
			require.Equal(t, [][]byte{{0x80, 42}, {0x80, 7}}, writes)
			require.EqualValues(t, 2, p.numCorrupted.Load())
			log := &handshakeDiagnosticLog{failed: true}
			d.logFailure(log)
			output := strings.Join(log.lines, "\n")
			require.Contains(t, output, "drop=false")
			require.Contains(t, output, "offset=1 before=7 after=42")
			require.Contains(t, output, "offset=0 before=128 after=128")
			require.Contains(t, output, "write_bytes=1 write_err=controlled write error")
			require.Contains(t, output, "from=:9001 to=:9002")
		})
	}
}

func TestCorruptionDiagnosticsCleanup(t *testing.T) {
	const childMode = "QUIC_GO_CORRUPTION_DIAGNOSTIC_TEST"
	if mode := os.Getenv(childMode); mode != "" {
		draw := 0
		testMITMCorruptPacketsWithRandom(t, quicproxy.DirectionOutgoing, func(n int) int {
			if mode == "fail" {
				return 0
			}
			// Damage the first outgoing datagram, then forward subsequent traffic.
			values := []int{0, 1, 42}
			if draw < len(values) {
				v := values[draw]
				draw++
				return v
			}
			return 1
		})
		return
	}
	executable, err := os.Executable()
	require.NoError(t, err)
	for _, mode := range []string{"pass", "fail"} {
		t.Run(mode, func(t *testing.T) {
			captureDir := t.TempDir()
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestCorruptionDiagnosticsCleanup$", "-test.v", "-test.timeout=15s")
			cmd.Env = append(os.Environ(), childMode+"="+mode, "QUIC_GO_CORRUPTION_CAPTURE_DIR="+captureDir)
			output, err := cmd.CombinedOutput()
			files, globErr := filepath.Glob(filepath.Join(captureDir, "*.jsonl"))
			require.NoError(t, globErr)
			if mode == "pass" {
				require.NoError(t, err, "%s", output)
				require.NotContains(t, string(output), "corruption diagnostics:")
				require.Empty(t, files, "successful fixtures remove their capture")
				return
			}
			require.Error(t, err)
			require.Contains(t, string(output), "context deadline exceeded")
			require.Contains(t, string(output), "corruption diagnostics: requested_direction=Outgoing")
			require.Contains(t, string(output), "client=Dial returned: context deadline exceeded")
			require.Contains(t, string(output), "transport client=true")
			require.Contains(t, string(output), "corruption proxy")
			require.Contains(t, string(output), "offset=0")
			require.Contains(t, string(output), "write_err=<nil>")
			require.NotContains(t, string(output), "panic:")
			require.Len(t, files, 1, "failed Dial must leave a handshake artifact")
			capture, readErr := os.ReadFile(files[0])
			require.NoError(t, readErr)
			require.Contains(t, string(capture), "Dial returned: context deadline exceeded")
			require.Contains(t, string(capture), "capture_start")
			require.Contains(t, string(capture), "dial_finished")
			require.Contains(t, string(capture), "socket endpoint client=true")
			require.Contains(t, string(capture), "socket endpoint client=false")
			require.Contains(t, string(capture), "socket proxy")
			require.Contains(t, string(capture), "operation=write")
			require.Contains(t, string(capture), "operation=read")
			require.Contains(t, string(capture), "before_mutation")
		})
	}
}
