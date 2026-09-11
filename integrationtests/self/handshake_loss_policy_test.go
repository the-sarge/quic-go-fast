package self_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/quic-go/quic-go/testutils/simnet"

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
			require.NotContains(t, string(output), "/retry:")
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

func TestDropOneThirdDirection(t *testing.T) {
	for _, tc := range []struct {
		direction direction
		want      []bool
	}{
		{directionToClient, []bool{true, false, false, false, true, false}},
		{directionToServer, []bool{false, true, false, false, false, true}},
		{directionBoth, []bool{true, false, true, false, true, false}},
	} {
		t.Run(tc.direction.String(), func(t *testing.T) {
			decisions := []bool{true, false, true, false, true, false}
			var calls int
			drop := dropCallbackDropOneThirdWithDecision(tc.direction, func() bool {
				decision := decisions[calls]
				calls++
				return decision
			})
			for i, dir := range []direction{directionToClient, directionToServer, directionToClient, directionToServer, directionToClient, directionToServer} {
				require.Equal(t, tc.want[i], drop(dir, simnet.Packet{}), "packet %d", i)
			}
			wantCalls := 3
			if tc.direction == directionBoth {
				wantCalls = 6
			}
			require.Equal(t, wantCalls, calls, "unselected directions must not consume loss decisions")
		})
	}
}

func TestDropOneThirdConsecutiveLimit(t *testing.T) {
	for _, dir := range []direction{directionToClient, directionToServer, directionBoth} {
		t.Run(dir.String(), func(t *testing.T) {
			decision := true
			drop := dropCallbackDropOneThirdWithDecision(dir, func() bool { return decision })
			var selected direction = directionToClient
			var other direction = directionToServer
			if dir == directionToServer {
				selected, other = other, selected
			}
			for range 10 {
				require.True(t, drop(selected, simnet.Packet{}))
			}
			// Traffic in the other direction neither shares nor resets this streak.
			require.Equal(t, dir == directionBoth, drop(other, simnet.Packet{}))
			require.False(t, drop(selected, simnet.Packet{}), "eleventh consecutive drop is forwarded")
			require.True(t, drop(selected, simnet.Packet{}), "forced forward resets the streak")
			if dir == directionBoth {
				for range 9 {
					require.True(t, drop(other, simnet.Packet{}))
				}
				require.False(t, drop(other, simnet.Packet{}), "each direction has its own limit")
			}
			decision = false
			require.False(t, drop(selected, simnet.Packet{}))
			decision = true
			for range 10 {
				require.True(t, drop(selected, simnet.Packet{}), "natural forward resets the streak")
			}
			require.False(t, drop(selected, simnet.Packet{}))
		})
	}
}
