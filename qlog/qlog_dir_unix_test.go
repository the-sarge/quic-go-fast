//go:build unix

package qlog

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/qlogwriter"

	"golang.org/x/sys/unix"

	"github.com/stretchr/testify/require"
)

func TestQLOGDIRPermissions(t *testing.T) {
	if os.Getenv("QUIC_GO_TEST_QLOGDIR_PERMISSIONS") != "1" {
		// umask is process-wide; isolate it from other tests and their goroutines.
		cmd := exec.Command(os.Args[0], "-test.run=^TestQLOGDIRPermissions$")
		cmd.Env = append(os.Environ(), "QUIC_GO_TEST_QLOGDIR_PERMISSIONS=1")
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", output)
		return
	}
	oldUmask := unix.Umask(0)
	defer unix.Umask(oldUmask)

	for _, name := range []string{"default", "with schemas"} {
		t.Run(name, func(t *testing.T) {
			for _, existingDir := range []bool{false, true} {
				qlogDir := filepath.Join(t.TempDir(), "parent", "qlogs")
				if existingDir {
					require.NoError(t, os.MkdirAll(qlogDir, 0o755))
				}
				t.Setenv("QLOGDIR", qlogDir)
				connID := protocol.ParseConnectionID([]byte{1, 2, 3, 4})
				var tracer qlogwriter.Trace
				if name == "default" {
					tracer = DefaultConnectionTracer(context.Background(), true, connID)
				} else {
					tracer = DefaultConnectionTracerWithSchemas(context.Background(), false, connID, []string{"extra"})
				}
				require.NotNil(t, tracer)
				require.NoError(t, tracer.AddProducer().Close())
				testQLOGDIRSetPermissions(t, qlogDir, existingDir)
			}
		})
	}
}

func testQLOGDIRSetPermissions(t *testing.T, qlogDir string, existingDir bool) {
	t.Helper()
	dirMode := os.FileMode(0o700)
	if existingDir {
		dirMode = 0o755
	}
	for _, dir := range []string{filepath.Dir(qlogDir), qlogDir} {
		info, err := os.Stat(dir)
		require.NoError(t, err)
		require.Equal(t, dirMode, info.Mode().Perm(), dir)
	}
	entries, err := os.ReadDir(qlogDir)
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	for _, entry := range entries {
		info, err := entry.Info()
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm(), entry.Name())
	}
}
