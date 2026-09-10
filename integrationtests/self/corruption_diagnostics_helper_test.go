package self_test

import (
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"
	quicproxy "github.com/quic-go/quic-go/integrationtests/tools/proxy"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/qlogwriter"
)

// The injected draw and writer preserve the original callback's decisions while
// allowing deterministic checks of observation transparency.
type corruptionProxy struct {
	direction    quicproxy.Direction
	diagnostics  *handshakeDiagnostics
	intN         func(int) int
	write        func(quicproxy.Direction, []byte) (int, error)
	numCorrupted atomic.Int32
	datagrams    atomic.Uint64
}

func (p *corruptionProxy) drop(dir quicproxy.Direction, from, to net.Addr, b []byte) (drop bool) {
	seq := p.datagrams.Add(1)
	checksum := qlog.CalculateDatagramPayloadChecksum(b)
	if p.diagnostics.capture.capturing() {
		p.diagnostics.capture.record(time.Now(), "before_mutation", fmt.Sprintf("datagram=%d direction=%s from=%s to=%s bytes=%d crc32c=%d parts=%s payload=%x", seq, dir, from, to, len(b), checksum, corruptionPacketBoundaries(b), b))
	}
	offset, before, after := -1, byte(0), byte(0)
	var written int
	var writeErr error
	defer func() {
		p.diagnostics.record(time.Now(), "corruption proxy", fmt.Sprintf("datagram=%d direction=%s from=%s to=%s bytes=%d crc32c_before=%d crc32c_after=%d drop=%t offset=%d before=%d after=%d write_bytes=%d write_err=%v", seq, dir, from, to, len(b), checksum, qlog.CalculateDatagramPayloadChecksum(b), drop, offset, before, after, written, writeErr))
	}()
	if dir != p.direction {
		return false
	}
	isLongHeaderPacket := wire.IsLongHeaderPacket(b[0])
	// Preserve the original selection: one in four long-header datagrams and
	// one in twenty short-header datagrams. A replacement may equal the old byte.
	if isLongHeaderPacket && p.intN(4) != 0 {
		return false
	}
	if !isLongHeaderPacket && p.intN(20) != 0 {
		return false
	}
	p.numCorrupted.Add(1)
	offset = p.intN(len(b))
	before = b[offset]
	b[offset] = byte(p.intN(256))
	after = b[offset]
	written, writeErr = p.write(dir, b)
	return true
}

func (d *handshakeDiagnostics) addCorruptionTransport(tr *quic.Transport, client bool) {
	recorder := &handshakeDiagnosticRecorder{diagnostics: d, source: fmt.Sprintf("endpoint client=%t", client)}
	if tr.Tracer == nil {
		tr.Tracer = recorder
		return
	}
	tr.Tracer = &multiplexedRecorder{Recorders: []qlogwriter.Recorder{tr.Tracer, recorder}}
}
