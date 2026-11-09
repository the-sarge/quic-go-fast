//go:build picoquic

package quic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/stretchr/testify/require"
)

// Temporary [R17D2] observers; remove before maintained qualification.
func matrixDiagnosticEnabled() bool {
	return os.Getenv("QGF_R17_DIAGNOSTIC") == "mtu-server-v1"
}

func matrixDiagnosticStatus(path string, status any) {
	data, err := json.MarshalIndent(status, "", "  ")
	if err == nil {
		err = os.WriteFile(path, append(data, '\n'), 0o600)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "[R17D2] status file %s: %v\n", path, err)
	}
}

type matrixDiagnosticRecorder struct {
	matrix *matrixTrace
	file   qlogwriter.Recorder
	path   string
	once   sync.Once
	err    error
}

func newMatrixDiagnosticRecorder(matrix *matrixTrace, file qlogwriter.Recorder, path string) *matrixDiagnosticRecorder {
	r := &matrixDiagnosticRecorder{matrix: matrix, file: file, path: path}
	matrixDiagnosticStatus(path, map[string]any{"stage": "D2", "qualification": false, "producer_closed": false})
	return r
}

func (r *matrixDiagnosticRecorder) RecordEvent(event qlogwriter.Event) {
	r.matrix.RecordEvent(event)
	switch event.(type) {
	case qlog.PacketSent, qlog.PacketReceived, qlog.PacketDropped, qlog.PacketBuffered, qlog.PacketLost, qlog.MetricsUpdated, qlog.PathLifecycleUpdated, qlog.ConnectionClosed:
		r.file.RecordEvent(event)
	}
}

func (r *matrixDiagnosticRecorder) Close() error {
	r.once.Do(func() {
		r.err = r.file.Close() // The last FileSeq producer drains Run and closes its file.
		matrixDiagnosticStatus(r.path, map[string]any{"stage": "D2", "qualification": false, "producer_closed": true, "producer_close_error": fmt.Sprint(r.err), "file_error_limit": "FileSeq reports encode/write errors to stderr; producer Close does not report underlying file close errors"})
		fmt.Fprintf(os.Stderr, "[R17D2] producer closed %s: %v\n", r.path, r.err)
	})
	return r.err
}

func matrixDiagnosticTransport(t *testing.T, trace *matrixTrace, address string) qlogwriter.Recorder {
	t.Helper()
	if !matrixDiagnosticEnabled() {
		return trace
	}
	path := filepath.Join(os.Getenv("PICOQUIC_LOG_DIR"), "go-transport-"+strings.ReplaceAll(address, ":", "_")+".sqlog")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[R17D2] transport log %s: %v\n", path, err)
		return trace
	}
	seq := qlogwriter.NewFileSeq(f)
	r := newMatrixDiagnosticRecorder(trace, seq.AddProducer(), path+".producer.json")
	go seq.Run()
	// Transport.Close normally owns this close. The fallback only covers failed
	// initialization; the wrapper prevents a second close of the file producer.
	t.Cleanup(func() { _ = r.Close() })
	fmt.Fprintf(os.Stderr, "[R17D2] transport address=%s log=%s\n", address, path)
	return r
}

type matrixDiagnosticTrace struct {
	matrix   *matrixTrace
	path     string
	isClient bool
	cid      ConnectionID
	once     sync.Once
	seq      *qlogwriter.FileSeq
	mutex    sync.Mutex
	next     int
}

func (d *matrixDiagnosticTrace) SupportsSchemas(schema string) bool {
	return d.matrix.SupportsSchemas(schema)
}

func (d *matrixDiagnosticTrace) AddProducer() qlogwriter.Recorder {
	d.once.Do(func() {
		f, err := os.OpenFile(d.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[R17D2] connection log %s: %v\n", d.path, err)
			return
		}
		d.seq = qlogwriter.NewConnectionFileSeq(f, d.isClient, d.cid, []string{qlog.EventSchema})
		go d.seq.Run()
	})
	if d.seq == nil {
		return d.matrix
	}
	producer := d.seq.AddProducer()
	if producer == nil {
		fmt.Fprintf(os.Stderr, "[R17D2] connection producer unavailable %s\n", d.path)
		return d.matrix
	}
	d.mutex.Lock()
	d.next++
	path := fmt.Sprintf("%s.producer-%d.json", d.path, d.next)
	d.mutex.Unlock()
	// Existing connection producers own their closes. Do not force-close the
	// asynchronous connection recorder during test cleanup to complete a trace.
	return newMatrixDiagnosticRecorder(d.matrix, producer, path)
}

func matrixDiagnosticTracer(dir string, trace *matrixTrace) func(context.Context, bool, ConnectionID) qlogwriter.Trace {
	if !matrixDiagnosticEnabled() {
		return trace.tracer
	}
	return func(_ context.Context, isClient bool, cid ConnectionID) qlogwriter.Trace {
		role := "server"
		if isClient {
			role = "client"
		}
		return &matrixDiagnosticTrace{matrix: trace, path: filepath.Join(dir, fmt.Sprintf("go-%s-%s.sqlog", cid, role)), isClient: isClient, cid: cid}
	}
}

func matrixDiagnosticPeerStatus(t *testing.T, dir string, peerStatus func() (bool, string)) {
	t.Helper()
	if !matrixDiagnosticEnabled() {
		return
	}
	// Register before startSmokePeer: its original cancel/join cleanup runs first.
	t.Cleanup(func() {
		joined, exitError := peerStatus()
		status := map[string]any{"stage": "D2", "qualification": false, "peer_join_observed": joined, "exit_error": exitError}
		matrixDiagnosticStatus(filepath.Join(dir, "peer-terminal.json"), status)
		fmt.Fprintf(os.Stderr, "[R17D2] peer terminal %v\n", status)
	})
}

func matrixDiagnosticExchangeStart(stream StreamID, op, path byte, payload int) {
	if matrixDiagnosticEnabled() {
		fmt.Fprintf(os.Stderr, "[R17D2] exchange stream %d op %c path %d request %d useful %d\n", stream, op, path, payload+2, payload)
	}
}

func matrixDiagnosticExchangeRead(conn *Conn, stream StreamID, op, path byte, got []byte, err error) {
	if matrixDiagnosticEnabled() {
		fmt.Fprintf(os.Stderr, "[R17D2] read stream %d op %c path %d bytes %d error %v connection-cause %v\n", stream, op, path, len(got), err, context.Cause(conn.Context()))
	}
}

type matrixDiagnosticSink struct {
	bytes.Buffer
	closes int
}

func (s *matrixDiagnosticSink) Close() error { s.closes++; return nil }

// Exercise the real serializer and last-producer drain, without either endpoint.
func TestPicoquicMatrixDiagnosticSerializerLifecycle(t *testing.T) {
	sink := &matrixDiagnosticSink{}
	seq := qlogwriter.NewFileSeq(sink)
	trace := newMatrixTrace()
	recorder := newMatrixDiagnosticRecorder(trace, seq.AddProducer(), filepath.Join(t.TempDir(), "producer.json"))
	go seq.Run()
	event := qlog.PacketReceived{Header: qlog.PacketHeader{PacketType: qlog.PacketType1RTT}, Path: &qlog.PathIdentity{PathID: 0}, Frames: []qlog.Frame{{Frame: &qlog.StreamFrame{StreamID: 1, Length: 321, Fin: true}}}}
	recorder.RecordEvent(event)
	require.NoError(t, recorder.Close())
	require.NoError(t, recorder.Close())
	require.Equal(t, 1, sink.closes)
	_, received := trace.traffic(0)
	require.Equal(t, uint64(321), received.Stream, "the original recorder receives the event once")
	records := bytes.Split(sink.Bytes(), []byte{0x1e})
	require.Len(t, records, 3, "one standard header and one drained event")
	var encoded struct {
		Name string `json:"name"`
	}
	require.NoError(t, json.Unmarshal(records[2], &encoded))
	require.Equal(t, event.Name(), encoded.Name)
}
