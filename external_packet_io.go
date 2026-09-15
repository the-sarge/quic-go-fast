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
// retried. Known zero-progress message-size or first-send permission rejections
// should return the accepted prefix with no error, allowing the ordinary
// per-packet path to retain its feedback and retry handling. Zero progress with
// no error also uses that fallback.
// Initialization reports capability state to Transport.Tracer as the
// external_packet_io debug event; debug logging reports per-registration
// batch_calls and accepted_packets counters after callback submissions.
func (t *Transport) ConfigureExternalPacketIOV1(conn net.PacketConn, allowReceiveCoalescing bool, sendBatch func([][]byte, []byte, *net.UDPAddr) (int, error)) error {
	c := &t.packetIO
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if err := t.checkPacketIORegistration(conn); err != nil {
		return err
	}
	c.external = &externalPacketIO{conn: conn, allowReceiveCoalescing: allowReceiveCoalescing, sendBatch: sendBatch}
	return nil
}

// checkPacketIORegistration requires the packetIO mutex.
func (t *Transport) checkPacketIORegistration(conn net.PacketConn) error {
	c := &t.packetIO
	if c.started {
		return errors.New("quic: external packet I/O registration after initialization")
	}
	if c.external != nil {
		return errors.New("quic: external packet I/O already registered")
	}
	if !samePacketConn(conn, t.Conn) {
		return errors.New("quic: external packet I/O requires the same non-nil pointer as Transport.Conn")
	}
	return nil
}

// ConfigureManagedPacketIOV1 binds an active factory lease and the exact outer
// connection to this transport. It claims the same immutable slot as
// ConfigureExternalPacketIOV1. lease must be the original factory value, never
// a wrapper with promoted methods; conn may be a caller's policy wrapper.
//
// The caller must join ordinary establishment readers and stop starting ordinary
// operations before registration. Registration rejects active lease I/O and
// seals the QUIC phase until lease Close, including on initialization failure.
// Packet and deadline methods continue serving the registered transport/wrapper.
// Registration preserves lease deadlines. Clear any establishment deadlines
// before handing the lease to QUIC if they should no longer apply.
// A non-nil sendBatch must preserve that wrapper's policy and submit through the
// lease's WriteBatchV1 method, following ConfigureExternalPacketIOV1's callback
// contract. Nil retains ordinary sends. Receive coalescing remains disabled.
// Transport.Close does not release the lease or own the native socket; lease
// Close revokes and joins I/O before another lease can use the endpoint.
func (t *Transport) ConfigureManagedPacketIOV1(conn net.PacketConn, lease net.PacketConn, sendBatch func([][]byte, []byte, *net.UDPAddr) (int, error)) error {
	c := &t.packetIO
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if err := t.checkPacketIORegistration(conn); err != nil {
		return err
	}
	l, ok := lease.(*managedPacketConn)
	if !ok || l == nil || l.lease == nil {
		return errors.New("quic: managed packet I/O requires an exact factory lease")
	}
	if direct, ok := conn.(*managedPacketConn); ok && direct != l {
		return errors.New("quic: managed packet I/O connection is not the supplied lease")
	}
	e := l.endpoint
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if err := l.checkLocked(); err != nil {
		return err
	}
	if l.lease.quic || e.active != 0 {
		return errors.New("quic: managed packet lease already bound or I/O active")
	}
	l.lease.quic = true
	c.external = &externalPacketIO{conn: conn, sendBatch: sendBatch}
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
	if p := t.policyConn.policy; p != nil && !samePacketConn(p.conn, t.Conn) {
		return errors.New("quic: Transport.Conn changed after fixed peer configuration")
	}
	if c.external != nil && !samePacketConn(c.external.conn, t.Conn) {
		return errors.New("quic: Transport.Conn changed after external packet I/O registration")
	}
	return nil
}

// UDPBatchWriterV1 creates an ordinary synchronous writer for a known UDP socket.
// The writer neither starts a reader nor owns Close, and constructing it does
// not initialize or configure the transport. It is safe for concurrent calls.
// Each payload is one UDP message with the supplied destination and OOB data.
// Known zero-progress message-size and first-send permission rejections return
// the definite prefix with no error for per-packet fallback. Other errors are
// terminal; callers must not resend uncertain data after an error. Buffers are
// borrowed until return.
func (t *Transport) UDPBatchWriterV1(conn *net.UDPConn) (func([][]byte, []byte, *net.UDPAddr) (int, error), error) {
	if conn == nil {
		return nil, errors.New("quic: nil UDP batch writer socket")
	}
	return newUDPBatchWriter(conn), nil
}

type udpMessageWriter interface {
	WriteMsgUDP([]byte, []byte, *net.UDPAddr) (int, int, error)
}

func newUDPBatchWriter(conn udpMessageWriter) func([][]byte, []byte, *net.UDPAddr) (int, error) {
	return func(bufs [][]byte, oob []byte, addr *net.UDPAddr) (int, error) {
		for i, buf := range bufs {
			n, oobn, err := conn.WriteMsgUDP(buf, oob, addr)
			if err != nil {
				if isSendMsgSizeErr(err) || isPermissionError(err) {
					return i, nil
				}
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
	}
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
		t.Tracer.RecordEvent(qlog.DebugEvent{EventName: "external_packet_io", Message: fmt.Sprintf("receive_requested=%t receive_permitted=%t receive_supported=false receive_enabled=false receive_disabled_reason=external_coalescing_unavailable batch_requested=%t batch_permitted=%t batch_supported=true batch_enabled=%t batch_disabled_reason=%s", c.allowReceiveCoalescing, c.allowReceiveCoalescing, c.sendBatch != nil, c.sendBatch != nil, c.sendBatch != nil, batchReason)})
	}
	return &externalPacketConn{rawConn: conn, config: c}
}

func (c *externalPacketConn) releaseReadBuffers() {
	if reader, ok := c.rawConn.(interface{ releaseReadBuffers() }); ok {
		reader.releaseReadBuffers()
	}
}
