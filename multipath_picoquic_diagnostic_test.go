//go:build picoquic && linux && amd64

package quic

import (
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
)

// Temporary [R17D1] observers; remove before maintained qualification.
func matrixDiagnosticEnabled() bool {
	return os.Getenv("QGF_R17_DIAGNOSTIC") == "abandon-client-v1"
}

func matrixDiagnosticStatus(path string, status any) {
	data, err := json.MarshalIndent(status, "", "  ")
	if err == nil {
		err = os.WriteFile(path, append(data, '\n'), 0o600)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "[R17D1] status file %s: %v\n", path, err)
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
	matrixDiagnosticStatus(path, map[string]any{"stage": "D1", "qualification": false, "producer_closed": false})
	return r
}

func (r *matrixDiagnosticRecorder) RecordEvent(event qlogwriter.Event) {
	r.matrix.RecordEvent(event)
	switch event.(type) {
	case qlog.PacketSent, qlog.PacketReceived, qlog.PacketDropped, qlog.PathLifecycleUpdated, qlog.ConnectionClosed:
		r.file.RecordEvent(event)
	}
}

func (r *matrixDiagnosticRecorder) Close() error {
	r.once.Do(func() {
		r.err = r.file.Close() // The last FileSeq producer drains Run and closes its file.
		matrixDiagnosticStatus(r.path, map[string]any{"stage": "D1", "qualification": false, "producer_closed": true, "close_error": fmt.Sprint(r.err)})
		fmt.Fprintf(os.Stderr, "[R17D1] producer closed %s: %v\n", r.path, r.err)
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
		fmt.Fprintf(os.Stderr, "[R17D1] transport log %s: %v\n", path, err)
		return trace
	}
	seq := qlogwriter.NewFileSeq(f)
	r := newMatrixDiagnosticRecorder(trace, seq.AddProducer(), path+".producer.json")
	go seq.Run()
	// Transport.Close normally owns this close. The fallback only covers failed
	// initialization; the wrapper prevents a second close of the file producer.
	t.Cleanup(func() { _ = r.Close() })
	fmt.Fprintf(os.Stderr, "[R17D1] transport address=%s log=%s\n", address, path)
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
			fmt.Fprintf(os.Stderr, "[R17D1] connection log %s: %v\n", d.path, err)
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
		fmt.Fprintf(os.Stderr, "[R17D1] connection producer unavailable %s\n", d.path)
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

func matrixDiagnosticPeerStatus(t *testing.T, s *matrixSession) {
	t.Helper()
	if !matrixDiagnosticEnabled() {
		return
	}
	// Register before startSmokePeer: its existing cancel/join cleanup runs first.
	t.Cleanup(func() {
		if s.peer == nil {
			return
		}
		status := map[string]any{"stage": "D1", "qualification": false, "peer_join_observed": false}
		select {
		case <-s.peer.done:
			status["peer_join_observed"], status["exit_error"] = true, fmt.Sprint(s.peer.err)
		default:
			// Preserve the missing terminal observation; add no wait or shutdown.
		}
		matrixDiagnosticStatus(filepath.Join(s.dir, "peer-terminal.json"), status)
		fmt.Fprintf(os.Stderr, "[R17D1] peer terminal %v\n", status)
	})
}
