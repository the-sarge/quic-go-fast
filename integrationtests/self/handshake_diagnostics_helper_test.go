package self_test

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/quic-go/quic-go/qlogwriter/jsontext"
	"github.com/quic-go/quic-go/testutils/events"
	"github.com/quic-go/quic-go/testutils/simnet"
)

const handshakeDiagnosticBytes = 4096

type handshakeDiagnosticEvent struct {
	time         time.Time
	source, data string
}

type handshakeDiagnostics struct {
	scenario                 string
	label                    string
	mu                       sync.Mutex
	clientPhase, serverPhase string
	clientClose, serverClose string
	events                   [256]handshakeDiagnosticEvent
	total                    uint64
}

func newHandshakeDiagnostics(t *testing.T, scenario string) *handshakeDiagnostics {
	d := &handshakeDiagnostics{scenario: scenario}
	t.Cleanup(func() { d.logFailure(t) })
	return d
}

func (d *handshakeDiagnostics) observeDrop(drop func(direction, simnet.Packet) bool) func(direction, simnet.Packet) bool {
	return func(dir direction, packet simnet.Packet) bool {
		decision := drop(dir, packet)
		d.record(time.Now(), "router", fmt.Sprintf("direction=%s drop=%t from=%s to=%s bytes=%d crc32c=%d", dir, decision, packet.From, packet.To, len(packet.Data), qlog.CalculateDatagramPayloadChecksum(packet.Data)))
		return decision
	}
}

func (d *handshakeDiagnostics) closeConnection(client bool, conn *quic.Conn) {
	setPhase := func(phase string) {
		d.mu.Lock()
		defer d.mu.Unlock()
		if client {
			d.clientClose = boundedHandshakeDiagnostic(phase)
		} else {
			d.serverClose = boundedHandshakeDiagnostic(phase)
		}
	}
	setPhase("closing")
	err := conn.CloseWithError(0, "")
	setPhase(fmt.Sprintf("returned: %v", err))
}

func (d *handshakeDiagnostics) phase(client bool, phase string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if client {
		d.clientPhase = boundedHandshakeDiagnostic(phase)
	} else {
		d.serverPhase = boundedHandshakeDiagnostic(phase)
	}
}

func boundedHandshakeDiagnostic(s string) string {
	const marker = " [truncated]"
	if len(s) > handshakeDiagnosticBytes {
		return s[:handshakeDiagnosticBytes-len(marker)] + marker
	}
	return s
}

func (d *handshakeDiagnostics) record(at time.Time, source, data string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.events[d.total%uint64(len(d.events))] = handshakeDiagnosticEvent{time: at, source: source, data: boundedHandshakeDiagnostic(data)}
	d.total++
}

// Serialize while the event is valid, retaining neither borrowed frame storage
// nor an unbounded event object. The existing qlog encoder owns the format.
type handshakeDiagnosticRecorder struct {
	diagnostics *handshakeDiagnostics
	source      string
}

func (r *handshakeDiagnosticRecorder) RecordEvent(ev qlogwriter.Event) {
	at := time.Now()
	var buffer handshakeDiagnosticBuffer
	err := ev.Encode(jsontext.NewEncoder(&buffer), at)
	r.diagnostics.record(at, r.source, fmt.Sprintf("event=%s encode_error=%v data=%s", ev.Name(), err, buffer.String()))
}

func (*handshakeDiagnosticRecorder) Close() error { return nil }

func (d *handshakeDiagnostics) tracer(_ context.Context, client bool, id quic.ConnectionID) qlogwriter.Trace {
	return &events.Trace{Recorder: &handshakeDiagnosticRecorder{diagnostics: d, source: fmt.Sprintf("transport client=%t conn=%s", client, id)}}
}

type handshakeDiagnosticBuffer struct{ strings.Builder }

func (b *handshakeDiagnosticBuffer) Write(p []byte) (int, error) {
	n, _ := b.Builder.Write(p[:min(len(p), handshakeDiagnosticBytes-b.Len())])
	if n < len(p) {
		return n, io.ErrShortWrite
	}
	return n, nil
}

func (d *handshakeDiagnostics) logFailure(t interface {
	Failed() bool
	Logf(string, ...any)
},
) {
	if !t.Failed() {
		return
	}
	d.mu.Lock()
	client, server := d.clientPhase, d.serverPhase
	clientClose, serverClose := d.clientClose, d.serverClose
	events, total := d.events, d.total
	d.mu.Unlock()
	label := d.label
	if label == "" {
		label = "packet-loss"
	}
	t.Logf("%s diagnostics: %s", label, d.scenario)
	t.Logf("latest phases: client=%s server=%s", client, server)
	t.Logf("connection close: client=%s server=%s", clientClose, serverClose)
	retained := min(total, uint64(len(events)))
	t.Logf("event tail: retained=%d omitted=%d; observation order, not replay", retained, total-retained)
	for i := total - retained; i < total; i++ {
		event := events[i%uint64(len(events))]
		t.Logf("%s %s %s", event.time.Format(time.RFC3339Nano), event.source, event.data)
	}
}
