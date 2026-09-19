package tools_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/integrationtests/tools"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/quic-go/quic-go/qlogwriter/jsontext"

	"github.com/stretchr/testify/require"
)

type transportTestEvent string

func (transportTestEvent) Name() string { return "transport:test_event" }

func (e transportTestEvent) Encode(enc *jsontext.Encoder, _ time.Time) error {
	return enc.WriteToken(jsontext.String(string(e)))
}

func closeTransportProducer(t *testing.T, producer qlogwriter.Recorder) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- producer.Close() }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("transport qlog producer did not finish draining and flushing")
	}
}

func TestQlogTracerPreservesIndependentHistories(t *testing.T) {
	testQlogHistories(t, 2, false)
}

func TestQlogTracerConcurrentHistories(t *testing.T) {
	testQlogHistories(t, 4, true)
}

func testQlogHistories(t *testing.T, count int, concurrent bool) {
	t.Helper()
	t.Chdir(t.TempDir())
	synctest.Test(t, func(t *testing.T) {
		started := time.Now()
		var existingLog bytes.Buffer
		existing := tools.QlogTracer(&existingLog).AddProducer()
		existing.RecordEvent(transportTestEvent("existing-owner"))
		closeTransportProducer(t, existing)
		existingFiles, err := filepath.Glob("*_transport.qlog")
		require.NoError(t, err)
		require.Len(t, existingFiles, 1)
		existingBytes, err := os.ReadFile(existingFiles[0])
		require.NoError(t, err)

		logs := make([]bytes.Buffer, count)
		producers := make([]qlogwriter.Recorder, count)
		if concurrent {
			start := make(chan struct{})
			done := make(chan struct{}, count)
			for i := range count {
				go func() {
					<-start
					producers[i] = tools.QlogTracer(&logs[i]).AddProducer()
					done <- struct{}{}
				}()
			}
			close(start)
			deadline := time.After(5 * time.Second)
			for range count {
				select {
				case <-done:
				case <-deadline:
					t.Fatal("concurrent transport qlog creation did not finish")
				}
			}
		} else {
			for i := range count {
				producers[i] = tools.QlogTracer(&logs[i]).AddProducer()
			}
		}
		require.Equal(t, started.Unix(), time.Now().Unix(), "creation must exercise the same-second collision")
		for i, producer := range producers {
			producer.RecordEvent(transportTestEvent(fmt.Sprintf("owner-%d", i)))
			closeTransportProducer(t, producer)
		}

		files, err := filepath.Glob("*_transport.qlog")
		require.NoError(t, err)
		require.Len(t, files, count+1)
		preserved, err := os.ReadFile(existingFiles[0])
		require.NoError(t, err)
		require.Equal(t, existingBytes, preserved, "creating a trace must not truncate an existing history")
		for i := range count {
			filename := filepath.Clean(strings.TrimSuffix(strings.TrimPrefix(logs[i].String(), "Creating "), ".\n"))
			require.Contains(t, files, filename, "logger must identify the created artifact")
			data, err := os.ReadFile(filename)
			require.NoError(t, err)
			records := bytes.Split(data, []byte{qlogwriter.RecordSeparator})
			require.Len(t, records, 3, "one header and one event must survive")
			require.Empty(t, records[0])
			var header map[string]any
			require.NoError(t, json.Unmarshal(records[1], &header))
			require.NotEmpty(t, header)
			var event struct {
				Name string `json:"name"`
				Data string `json:"data"`
			}
			require.NoError(t, json.Unmarshal(records[2], &event))
			require.Equal(t, "transport:test_event", event.Name)
			require.Equal(t, fmt.Sprintf("owner-%d", i), event.Data)
		}
	})
}
