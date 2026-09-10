package self_test

import (
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"

	quicproxy "github.com/quic-go/quic-go/integrationtests/tools/proxy"
	"github.com/quic-go/quic-go/qlog"

	"github.com/stretchr/testify/require"
)

func TestCorruptionRandomOptIn(t *testing.T) {
	executable, err := os.Executable()
	require.NoError(t, err)
	for _, enabled := range []bool{false, true} {
		name := "default"
		if enabled {
			name = "enabled"
		}
		t.Run(name, func(t *testing.T) {
			// Select the random-corruption group but no leaf: inspect the gate without
			// accidentally making a random corruption transfer part of mandatory CI.
			cmd := exec.CommandContext(t.Context(), executable, "-test.v", "-test.run=^TestMITCorruptPacketsRandom$/^gate_probe_no_matching_child$", "-test.timeout=30s", "-version=2")
			for _, env := range os.Environ() {
				if !strings.HasPrefix(env, "QUIC_GO_TEST_RANDOM_CORRUPTION=") {
					cmd.Env = append(cmd.Env, env)
				}
			}
			if enabled {
				cmd.Env = append(cmd.Env, "QUIC_GO_TEST_RANDOM_CORRUPTION=1")
			}
			output, runErr := cmd.CombinedOutput()
			require.NoError(t, runErr, "%s", output)
			require.Contains(t, string(output), "=== RUN   TestMITCorruptPacketsRandom")
			require.NotContains(t, string(output), "towards_the_")
			if enabled {
				require.NotContains(t, string(output), "set QUIC_GO_TEST_RANDOM_CORRUPTION=1")
				require.NotContains(t, string(output), "--- SKIP:")
			} else {
				require.Contains(t, string(output), "set QUIC_GO_TEST_RANDOM_CORRUPTION=1")
				require.Contains(t, string(output), "--- SKIP:")
			}
		})
	}
}

func TestCorruptionFiniteCallback(t *testing.T) {
	for _, tc := range []struct {
		kind   qlog.PacketType
		offset int
	}{
		{qlog.PacketTypeInitial, 9}, {qlog.PacketTypeHandshake, 18}, {qlog.PacketType1RTT, 20},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			for _, dir := range []quicproxy.Direction{quicproxy.DirectionIncoming, quicproxy.DirectionOutgoing} {
				t.Run(dir.String(), func(t *testing.T) {
					original := []byte{0xc0, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0xe0, 0, 0, 0, 1, 0, 0, 1, 0, 0x40, 0xaa}
					writes := 0
					p := &corruptionProxy{direction: dir, packetType: &tc.kind, diagnostics: &handshakeDiagnostics{}, intN: func(int) int { return 1 }, write: func(_ quicproxy.Direction, b []byte) (int, error) { writes++; return len(b), nil }}
					addr := &net.UDPAddr{Port: 9001}
					opposite := quicproxy.DirectionIncoming
					if dir == opposite {
						opposite = quicproxy.DirectionOutgoing
					}
					b := append([]byte(nil), original...)
					require.False(t, p.drop(opposite, addr, addr, b))
					require.Equal(t, original, b)
					require.True(t, p.drop(dir, addr, addr, b))
					expected := append([]byte(nil), original...)
					expected[tc.offset] ^= 1
					require.Equal(t, expected, b, "change only the selected packet's final protected byte")
					b = append([]byte(nil), original...)
					require.False(t, p.drop(dir, addr, addr, b), "subsequent traffic must be reliably forwarded")
					require.Equal(t, original, b)
					require.Equal(t, 1, writes)
				})
			}
		})
	}
}
