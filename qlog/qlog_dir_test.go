package qlog

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/stretchr/testify/require"
)

func TestQLOGDIRSet(t *testing.T) {
	tmpDir := t.TempDir()

	connID, _ := protocol.GenerateConnectionIDForInitial()
	qlogDir := filepath.Join(tmpDir, "qlogs")
	t.Setenv("QLOGDIR", qlogDir)

	t.Run("default connection tracer", func(t *testing.T) {
		tracer := DefaultConnectionTracer(context.Background(), true, connID)
		testQLOGDIRSet(t, qlogDir, tracer, []string{EventSchema})
	})

	t.Run("default connection tracer with schemas", func(t *testing.T) {
		tracer := DefaultConnectionTracerWithSchemas(context.Background(), true, connID, []string{"urn:ietf:params:qlog:events:foobar"})
		testQLOGDIRSet(t, qlogDir, tracer, []string{EventSchema, "urn:ietf:params:qlog:events:foobar"})
	})
}

func testQLOGDIRSet(t *testing.T, qlogDir string, tracer qlogwriter.Trace, expectedEventSchemas []string) {
	require.NotNil(t, tracer)

	// adding and closing a producer makes the tracer close the file
	recorder := tracer.AddProducer()
	recorder.Close()

	_, err := os.Stat(qlogDir)
	qlogDirCreated := !os.IsNotExist(err)
	require.True(t, qlogDirCreated)

	entries, err := os.ReadDir(qlogDir)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	data, err := os.ReadFile(filepath.Join(qlogDir, entries[0].Name()))
	require.NoError(t, err)

	var obj map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.Split(string(data), "\n")[0])[1:], &obj))
	require.Contains(t, obj, "trace")
	require.IsType(t, obj["trace"], map[string]any{})
	require.Contains(t, obj["trace"], "event_schemas")
	var eventSchemas []string
	for _, v := range obj["trace"].(map[string]any)["event_schemas"].([]any) {
		eventSchemas = append(eventSchemas, v.(string))
	}
	require.Equal(t, eventSchemas, expectedEventSchemas)
}

func TestQLOGDIRNotSet(t *testing.T) {
	connID, _ := protocol.GenerateConnectionIDForInitial()
	t.Setenv("QLOGDIR", "")

	tracer := DefaultConnectionTracer(context.Background(), true, connID)
	require.Nil(t, tracer)
}

func TestQLOGDIRCreationFailure(t *testing.T) {
	if os.Getenv("QUIC_GO_TEST_QLOGDIR_FAILURE") == "1" {
		// The missing parent makes Stat report nonexistence; the oversized path
		// component then makes MkdirAll fail, even when tests run as root.
		dir := filepath.Join(t.TempDir(), "missing", strings.Repeat("x", 300))
		_, err := os.Stat(dir)
		require.True(t, os.IsNotExist(err))
		t.Setenv("QLOGDIR", dir)
		require.Nil(t, DefaultConnectionTracer(context.Background(), true, ConnectionID{}))
		require.Nil(t, DefaultConnectionTracerWithSchemas(context.Background(), false, ConnectionID{}, []string{"extra"}))
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestQLOGDIRCreationFailure$")
	cmd.Env = append(os.Environ(), "QUIC_GO_TEST_QLOGDIR_FAILURE=1")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	require.Contains(t, string(output), "failed to create qlog dir")
}
