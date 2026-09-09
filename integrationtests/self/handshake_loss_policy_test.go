package self_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHandshakeRandomLossOptIn(t *testing.T) {
	executable, err := os.Executable()
	require.NoError(t, err)
	for _, enabled := range []bool{false, true} {
		name := "default"
		if enabled {
			name = "enabled"
		}
		t.Run(name, func(t *testing.T) {
			// Select the random-loss group but no leaf: inspect the gate without
			// accidentally making a random stress transfer part of mandatory CI.
			cmd := exec.CommandContext(t.Context(), executable, "-test.v", "-test.run=^TestHandshakeWithPacketLoss$/^drop_1/3_of_packets_in_direction_to_client$/^gate_probe_no_matching_child$", "-test.timeout=30s", "-version=2")
			for _, env := range os.Environ() {
				if !strings.HasPrefix(env, "QUIC_GO_TEST_RANDOM_LOSS=") {
					cmd.Env = append(cmd.Env, env)
				}
			}
			if enabled {
				cmd.Env = append(cmd.Env, "QUIC_GO_TEST_RANDOM_LOSS=1")
			}
			output, runErr := cmd.CombinedOutput()
			require.NoError(t, runErr, "%s", output)
			require.Contains(t, string(output), "=== RUN   TestHandshakeWithPacketLoss/drop_1/3_of_packets_in_direction_to_client")
			require.NotContains(t, string(output), "retry:_true/nobody_speaks")
			if enabled {
				require.NotContains(t, string(output), "set QUIC_GO_TEST_RANDOM_LOSS=1")
				require.NotContains(t, string(output), "--- SKIP:")
			} else {
				require.Contains(t, string(output), "set QUIC_GO_TEST_RANDOM_LOSS=1")
				require.Contains(t, string(output), "--- SKIP:")
			}
		})
	}
}
