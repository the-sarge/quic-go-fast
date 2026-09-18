package qtls

import (
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCipherFixtureCleanup(t *testing.T) {
	const env = "QUIC_GO_CIPHER_FIXTURE_CLEANUP"
	if mode := os.Getenv(env); mode != "" {
		checkCipherFixtureCleanup(t, mode, true)
		return
	}
	for _, mode := range []string{"complete", "reading", "accept", "dial"} {
		t.Run(mode, func(t *testing.T) {
			checkCipherFixtureCleanup(t, mode, false)
			executable, err := os.Executable()
			require.NoError(t, err)
			cmd := exec.CommandContext(t.Context(), executable, "-test.run=^TestCipherFixtureCleanup$", "-test.timeout=10s")
			cmd.Env = append(os.Environ(), env+"="+mode)
			output, err := cmd.CombinedOutput()
			var exitErr *exec.ExitError
			require.ErrorAs(t, err, &exitErr, "%s", output)
			require.Equal(t, 1, exitErr.ExitCode(), "%s", output)
			require.Contains(t, string(output), "intentional cipher fixture failure")
			require.Contains(t, string(output), "cipher cleanup observed: "+mode)
		})
	}
}

func checkCipherFixtureCleanup(t *testing.T, mode string, fail bool) {
	t.Helper()
	var f *cipherFixture
	// Restoration is checked against the original slices, including their order.
	original := append([]uint16(nil), defaultCipherSuitesTLS13...)
	originalNoAES := append([]uint16(nil), defaultCipherSuitesTLS13NoAES...)
	originalSuites := append(cipherSuitesTLS13[:0:0], cipherSuitesTLS13...)
	t.Run("owner", func(t *testing.T) {
		f = newCipherFixture(t, tls.TLS_AES_128_GCM_SHA256)
		switch mode {
		case "accept", "dial":
			require.NoError(t, f.listener.Close())
			if mode == "dial" {
				_, err := f.dial(t.Context())
				require.Error(t, err)
			}
			require.ErrorIs(t, f.wait(t).err, net.ErrClosed)
		default:
			conn, err := f.dial(t.Context())
			require.NoError(t, err)
			if mode == "complete" {
				_, err = conn.Write([]byte("foobar"))
				require.NoError(t, err)
				result := f.wait(t)
				require.NoError(t, result.err)
				require.Equal(t, uint16(tls.TLS_AES_128_GCM_SHA256), result.cipher)
				// Completion must not be published before the accepted socket is closed.
				require.ErrorIs(t, f.accepted.SetDeadline(time.Now()), net.ErrClosed)
			}
		}
		if fail {
			t.Fatal("intentional cipher fixture failure")
		}
	})
	select {
	case <-f.done:
	default:
		t.Fatal("cipher worker outlived fixture cleanup")
	}
	_, err := f.listener.Accept()
	require.ErrorIs(t, err, net.ErrClosed)
	if f.client != nil {
		require.ErrorIs(t, f.client.SetDeadline(time.Now()), net.ErrClosed)
	}
	if f.accepted != nil {
		require.ErrorIs(t, f.accepted.SetDeadline(time.Now()), net.ErrClosed)
	}
	require.False(t, cipherSuitesModified)
	require.Equal(t, original, defaultCipherSuitesTLS13)
	require.Equal(t, originalNoAES, defaultCipherSuitesTLS13NoAES)
	require.Equal(t, originalSuites, cipherSuitesTLS13)
	fmt.Println("cipher cleanup observed: " + mode)
}
