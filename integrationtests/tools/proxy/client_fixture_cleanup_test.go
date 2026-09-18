package quicproxy

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProxyClientCleanup(t *testing.T) {
	const env = "QUIC_GO_PROXY_CLIENT_CLEANUP"
	if mode := os.Getenv(env); mode != "" {
		checkProxyClientCleanup(t, mode, true)
		return
	}
	for _, mode := range []string{"socket", "reading", "abandoned"} {
		t.Run(mode, func(t *testing.T) {
			checkProxyClientCleanup(t, mode, false)
			executable, err := os.Executable()
			require.NoError(t, err)
			cmd := exec.CommandContext(t.Context(), executable, "-test.run=^TestProxyClientCleanup$", "-test.timeout=10s")
			cmd.Env = append(os.Environ(), env+"="+mode)
			output, err := cmd.CombinedOutput()
			var exitErr *exec.ExitError
			require.ErrorAs(t, err, &exitErr, "%s", output)
			require.Equal(t, 1, exitErr.ExitCode(), "%s", output)
			require.Contains(t, string(output), "intentional proxy fixture failure")
			require.Contains(t, string(output), "proxy cleanup observed: "+mode)
		})
	}
}

func checkProxyClientCleanup(t *testing.T, mode string, fail bool) {
	t.Helper()
	server := newUPDConnLocalhost(t)
	var client *net.UDPConn
	var done <-chan struct{}
	t.Run("owner", func(t *testing.T) {
		client = dialProxyClient(t, server.LocalAddr().(*net.UDPAddr))
		if mode != "socket" {
			var packets <-chan []byte
			packets, done = readProxyClient(t, client, 1)
			if mode == "abandoned" {
				// Fill the result buffer, then abandon consumption while another packet arrives.
				_, err := server.WriteToUDP([]byte("first"), client.LocalAddr().(*net.UDPAddr))
				require.NoError(t, err)
				require.Eventually(t, func() bool { return len(packets) == 1 }, time.Second, time.Millisecond)
				_, err = server.WriteToUDP([]byte("second"), client.LocalAddr().(*net.UDPAddr))
				require.NoError(t, err)
			}
		}
		if fail {
			t.Fatal("intentional proxy fixture failure")
		}
	})
	// Retain the actual socket through the child cleanup boundary.
	defer client.Close()
	require.ErrorIs(t, client.SetReadDeadline(time.Now()), net.ErrClosed)
	if done != nil {
		select {
		case <-done:
		default:
			t.Fatal("client reader outlived fixture cleanup")
		}
	}
	fmt.Println("proxy cleanup observed: " + mode)
}
