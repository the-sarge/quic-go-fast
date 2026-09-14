package self_test

import (
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"
	quicproxy "github.com/quic-go/quic-go/integrationtests/tools/proxy"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/qlog"
	"golang.org/x/net/ipv4"
)

// Preserve the UDP socket's syscall/OOB capabilities. In particular, ReadBatch
// delegates to the same ipv4 batch reader used by quic's ordinary OOB connection;
// wrapping only ReadFrom would silently lose batched reads or miss them entirely.
type corruptionUDPConn struct {
	*net.UDPConn
	batch       *ipv4.PacketConn
	capture     *corruptionCapture
	source      string
	readBatches atomic.Uint64

	// Scheduling seam for receive/finalization regressions, set before use.
	afterBatchRead func()
}

var _ quic.OOBCapablePacketConn = (*corruptionUDPConn)(nil)

func (d *handshakeDiagnostics) observeCorruptionSocket(tr *quic.Transport, client bool) {
	conn := tr.Conn.(*net.UDPConn)
	tr.Conn = &corruptionUDPConn{UDPConn: conn, batch: ipv4.NewPacketConn(conn), capture: d.capture, source: fmt.Sprintf("socket endpoint client=%t", client)}
}

func (c *corruptionUDPConn) ReadFrom(b []byte) (int, net.Addr, error) {
	n, addr, err := c.UDPConn.ReadFrom(b)
	c.capture.socket(c.source, "read", time.Time{}, addr, c.LocalAddr(), b[:n], nil, n, 0, err)
	return n, addr, err
}

func (c *corruptionUDPConn) ReadMsgUDP(b, oob []byte) (int, int, int, *net.UDPAddr, error) {
	n, nn, flags, addr, err := c.UDPConn.ReadMsgUDP(b, oob)
	c.capture.socket(c.source, "read", time.Time{}, addr, c.LocalAddr(), b[:n], oob[:nn], n, flags, err)
	return n, nn, flags, addr, err
}

func (c *corruptionUDPConn) ReadBatch(messages []ipv4.Message, flags int) (int, error) {
	n, err := c.batch.ReadBatch(messages, flags)
	if c.afterBatchRead != nil {
		c.afterBatchRead()
	}
	if c.capture.capturing() {
		batch := c.readBatches.Add(1)
		c.capture.readBatch(c.source, batch, messages, n, flags, err, c.LocalAddr())
	}
	return n, err
}

// readBatch admits an observation only after the socket read returns. The result
// and members share finalization's mutex, but keep ordinary per-record limits
// and streaming writes: this is not a disk transaction or a larger byte budget.
func (c *corruptionCapture) readBatch(source string, batch uint64, messages []ipv4.Message, n, flags int, err error, local net.Addr) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done || c.err != nil {
		return
	}
	c.recordLocked(time.Now(), source, fmt.Sprintf("operation=read_batch batch=%d result_messages=%d flags=%d error=%v", batch, n, flags, err))
	if c.afterBatchResult != nil {
		c.afterBatchResult()
	}
	for index, msg := range messages[:max(n, 0)] {
		// quic supplies one buffer per message. Join only the populated bytes
		// to retain the socket boundary even if that caller changes later.
		var payload []byte
		left := msg.N
		for _, b := range msg.Buffers {
			l := min(left, len(b))
			payload = append(payload, b[:l]...)
			left -= l
		}
		memberSource := fmt.Sprintf("%s batch=%d index=%d count=%d", source, batch, index, n)
		c.recordLocked(time.Now(), memberSource, corruptionSocketRecord("read", time.Time{}, msg.Addr, local, payload, msg.OOB[:msg.NN], msg.N, msg.Flags, err))
	}
}

func (c *corruptionUDPConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	start := time.Now()
	n, err := c.UDPConn.WriteTo(b, addr)
	c.capture.socket(c.source, "write", start, c.LocalAddr(), addr, b, nil, n, 0, err)
	return n, err
}

func (c *corruptionUDPConn) WriteMsgUDP(b, oob []byte, addr *net.UDPAddr) (int, int, error) {
	start := time.Now()
	n, nn, err := c.UDPConn.WriteMsgUDP(b, oob, addr)
	c.capture.socket(c.source, "write", start, c.LocalAddr(), addr, b, oob, n, nn, err)
	return n, nn, err
}

func (c *corruptionCapture) socket(source, operation string, start time.Time, from, to net.Addr, b, oob []byte, n, flags int, err error) {
	if !c.capturing() {
		return
	}
	c.record(time.Now(), source, corruptionSocketRecord(operation, start, from, to, b, oob, n, flags, err))
}

func corruptionSocketRecord(operation string, start time.Time, from, to net.Addr, b, oob []byte, n, flags int, err error) string {
	return fmt.Sprintf("operation=%s started=%s from=%v to=%v submitted_bytes=%d result_bytes=%d flags_or_oob_bytes=%d error=%v crc32c=%d payload=%x oob=%x", operation, start.Format(time.RFC3339Nano), from, to, len(b), n, flags, err, qlog.CalculateDatagramPayloadChecksum(b), b, oob)
}

func (d *handshakeDiagnostics) observeProxySocket(ev quicproxy.SocketEvent) {
	d.capture.socket("socket proxy "+ev.Direction.String(), ev.Operation, ev.Started, ev.From, ev.To, ev.Data, nil, ev.N, 0, ev.Err)
}

func corruptionPacketBoundaries(b []byte) string {
	var parts []string
	for offset := 0; len(b) > 0; {
		if !wire.IsLongHeaderPacket(b[0]) {
			parts = append(parts, fmt.Sprintf("1-RTT@%d+%d", offset, len(b)))
			break
		}
		if wire.IsVersionNegotiationPacket(b) {
			parts = append(parts, fmt.Sprintf("version_negotiation@%d+%d", offset, len(b)))
			break
		}
		hdr, packet, rest, err := wire.ParsePacket(b)
		if err != nil || len(packet) == 0 {
			parts = append(parts, fmt.Sprintf("unparsed@%d+%d:%v", offset, len(b), err))
			break
		}
		parts = append(parts, fmt.Sprintf("%s@%d+%d", hdr.Type, offset, len(packet)))
		offset += len(packet)
		b = rest
	}
	return strings.Join(parts, ",")
}
