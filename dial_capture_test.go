package quic

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDialCaptureFixture(t *testing.T) {
	binary, err := os.Executable()
	require.NoError(t, err)
	for _, fail := range []bool{true, false} {
		name := "passing"
		if fail {
			name = "controlled_failure"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary, "-test.run=^TestDial/DialAddr$", "-test.timeout=10s", "-test.v")
			target := ""
			if fail {
				target = "TestDial/DialAddr"
			}
			cmd.Env = append(os.Environ(), "QUIC_GO_DIAL_CAPTURE_FAIL_TEST="+target)
			output, err := cmd.CombinedOutput()
			if !fail {
				require.NoError(t, err, string(output))
				require.NotContains(t, string(output), "dial-capture ")
				return
			}
			require.Error(t, err)
			require.NoError(t, ctx.Err(), string(output))
			if !bytes.Contains(output, []byte("controlled dial capture failure")) {
				t.Logf("%s", output)
				t.Fatal("controlled failure was not reached")
			}
			var report map[string]any
			for line := range bytes.SplitSeq(output, []byte("\n")) {
				if _, data, ok := bytes.Cut(line, []byte("dial-capture ")); ok {
					require.NoError(t, json.Unmarshal(data, &report))
				}
			}
			require.NotNil(t, report)
			require.Equal(t, true, report["complete_observations"])
			require.NotEmpty(t, report["goroutines"])
			var sockets int
			for _, raw := range report["events"].([]any) {
				event := raw.(map[string]any)
				if event["event"] == "original_socket" {
					sockets++
					data := event["data"].(map[string]any)
					require.Equal(t, true, data["observed_closed"])
					require.NotEmpty(t, data["identity"])
					require.NotEmpty(t, data["address"])
				}
			}
			require.Equal(t, 1, sockets)
			encoded, err := json.Marshal(report)
			require.NoError(t, err)
			for _, event := range []string{"original_socket", "datagram_received", "cancel_requested", "dial_return", "rebind_enter", "rebind_return", "goroutines", "production_close_result", "receive_loop_completion"} {
				require.Contains(t, string(encoded), event)
			}
		})
	}
}

func TestDialCaptureSocketEvidence(t *testing.T) {
	socket := newUDPConnLocalhost(t)
	capture := newDialCapture(t.Name())
	capture.observeSocket(socket)
	conn, err := capture.rebind(socket.LocalAddr().(*net.UDPAddr))
	require.Error(t, err)
	require.Nil(t, conn)
	require.NoError(t, socket.Close())
	capture.observeSocket(socket)
	var report struct {
		Events []struct {
			Event string
			Data  map[string]any
		}
	}
	require.NoError(t, json.Unmarshal(capture.finish(true), &report))
	var states []bool
	var bindFailures int
	for _, event := range report.Events {
		switch event.Event {
		case "original_socket":
			states = append(states, event.Data["observed_closed"].(bool))
		case "rebind_return":
			bindFailures++
			bindError := event.Data["error"].(map[string]any)
			require.Equal(t, "*net.OpError", bindError["type"])
			require.NotZero(t, bindError["errno"])
			require.NotEmpty(t, bindError["message"])
		}
	}
	require.Equal(t, []bool{false, true}, states)
	require.Equal(t, 1, bindFailures)
}

func TestDialCaptureIncomplete(t *testing.T) {
	for _, mode := range []string{"overflow", "encoding"} {
		t.Run(mode, func(t *testing.T) {
			capture := newDialCapture(t.Name())
			if mode == "overflow" {
				capture.record("oversized", strings.Repeat("x", dialCaptureEventBytes))
			} else {
				capture.record("invalid", make(chan int))
			}
			data := capture.finish(true)
			require.Less(t, len(data), 256<<10)
			var report map[string]any
			require.NoError(t, json.Unmarshal(data, &report))
			require.Equal(t, false, report["complete_observations"])
			require.Equal(t, float64(1), report["dropped"])
		})
	}
	capture := newDialCapture(t.Name())
	require.Nil(t, capture.finish(false))
	capture.record("late", "outside window")
	require.Nil(t, capture.finish(true))
}
