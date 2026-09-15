package quic

import (
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/quic-go/quic-go/qlog"
)

// externalPacketIO holds immutable registration and atomic diagnostic counters.
// Registration is synchronized independently of the connection-handler lock.
type externalPacketIO struct {
	batchCalls             atomic.Uint64
	acceptedPackets        atomic.Uint64
	conn                   net.PacketConn
	allowReceiveCoalescing bool
	sendBatch              func([][]byte, []byte, *net.UDPAddr) (int, error)
}

type packetIOConfig struct {
	mutex    sync.Mutex
	started  bool
	external *externalPacketIO
}

// ConfigureExternalPacketIOV1 binds explicit packet-I/O permission to Conn.
// Call it once, before any operation that initializes the transport. conn must
// be the same non-nil pointer as Transport.Conn; replacing Conn invalidates the
// binding. Registration does not transfer ownership of Close.
//
// Receive coalescing is currently unavailable on externally registered sockets,
// even when allowed. A nil sendBatch retains ordinary sends. A non-nil callback
// must preserve the wrapper's policy, support concurrent calls, and borrow each
// call's complete UDP payloads and shared OOB data only until return. It returns
// the definitely accepted prefix. Any error is terminal: uncertain data is not
// retried. Zero progress with no error uses the ordinary per-packet fallback.
// Initialization reports capability state to Transport.Tracer as the
// external_packet_io debug event; debug logging reports per-registration
// batch_calls and accepted_packets counters after callback submissions.
func (t *Transport) ConfigureExternalPacketIOV1(conn net.PacketConn, allowReceiveCoalescing bool, sendBatch func([][]byte, []byte, *net.UDPAddr) (int, error)) error {
	c := &t.packetIO
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.started {
		return errors.New("quic: external packet I/O registration after initialization")
	}
	if c.external != nil {
		return errors.New("quic: external packet I/O already registered")
	}
	if !samePacketConn(conn, t.Conn) {
		return errors.New("quic: external packet I/O requires the same non-nil pointer as Transport.Conn")
	}
	c.external = &externalPacketIO{conn: conn, allowReceiveCoalescing: allowReceiveCoalescing, sendBatch: sendBatch}
	return nil
}

func samePacketConn(a, b net.PacketConn) bool {
	av, bv := reflect.ValueOf(a), reflect.ValueOf(b)
	return av.Kind() == reflect.Pointer && bv.Kind() == reflect.Pointer && !av.IsNil() && !bv.IsNil() && av.Type() == bv.Type() && av.Interface() == bv.Interface()
}

func (t *Transport) beginPacketIO() error {
	c := &t.packetIO
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.started = true
	if c.external != nil && !samePacketConn(c.external.conn, t.Conn) {
		return errors.New("quic: Transport.Conn changed after external packet I/O registration")
	}
	return nil
}

// UDPBatchWriterV1 creates an ordinary synchronous writer for a known UDP socket.
// The writer neither starts a reader nor owns Close, and constructing it does
// not initialize or configure the transport. It is safe for concurrent calls.
// Each payload is one UDP message with the supplied destination and OOB data.
// It returns the number of complete messages preceding a terminal error; callers
// must not resend uncertain data after an error. Buffers are borrowed until return.
func (t *Transport) UDPBatchWriterV1(conn *net.UDPConn) (func([][]byte, []byte, *net.UDPAddr) (int, error), error) {
	if conn == nil {
		return nil, errors.New("quic: nil UDP batch writer socket")
	}
	return func(bufs [][]byte, oob []byte, addr *net.UDPAddr) (int, error) {
		for i, buf := range bufs {
			n, oobn, err := conn.WriteMsgUDP(buf, oob, addr)
			if err != nil {
				return i, err
			}
			// Go's overlapped WSASendMsg path can report zero bytes on
			// synchronous success. UDP messages are atomic, so success
			// still accounts for this complete payload (including empty).
			if runtime.GOOS == "windows" && n == 0 {
				n = len(buf)
			}
			if n != len(buf) || oobn != len(oob) {
				return i, io.ErrShortWrite
			}
		}
		return len(bufs), nil
	}, nil
}

// The wrapper carries explicit registration without promoting any native
// descriptor extraction through a caller's policy boundary.
type externalPacketConn struct {
	rawConn
	config *externalPacketIO
}

func (t *Transport) wrapExternalPacketIO(conn rawConn) rawConn {
	c := t.packetIO.external
	if c == nil {
		return conn
	}
	if t.Tracer != nil {
		batchReason := "none"
		if c.sendBatch == nil {
			batchReason = "no_callback"
		}
		t.Tracer.RecordEvent(qlog.DebugEvent{EventName: "external_packet_io", Message: fmt.Sprintf("receive_requested=%t receive_permitted=%t receive_supported=false receive_enabled=false receive_disabled_reason=external_coalescing_unavailable batch_requested=%t batch_permitted=%t batch_supported=true batch_enabled=%t batch_disabled_reason=%s batch_calls=0 accepted_packets=0 receive_exercised=0", c.allowReceiveCoalescing, c.allowReceiveCoalescing, c.sendBatch != nil, c.sendBatch != nil, c.sendBatch != nil, batchReason)})
	}
	return &externalPacketConn{rawConn: conn, config: c}
}

func (c *externalPacketConn) releaseReadBuffers() {
	if reader, ok := c.rawConn.(interface{ releaseReadBuffers() }); ok {
		reader.releaseReadBuffers()
	}
}
