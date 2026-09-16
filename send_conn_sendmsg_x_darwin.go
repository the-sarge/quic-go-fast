//go:build darwin && !ios && !quic_go_no_private_syscalls

package quic

// sconn's batchSender implementation: the Darwin conn owns batch encoding
// and syscall submission, while the send worker (send_queue.go) remains the
// single owner of send ordering and error attribution. Adapted from
// KeibiSoft/quic-go commit 24ecf34c (MIT).

import (
	"net"
	"sync"
	"syscall"

	"github.com/quic-go/quic-go/internal/protocol"
)

// darwinBatch caches the per-connection state for sendmsg_x batching: the
// destination sockaddr (rebuilt when the remote address changes, e.g. on
// migration) and reusable msghdr/iovec scratch. The native sconn uses it only
// from its send worker; a factory writer serializes access with its own mutex.
type darwinBatch struct {
	raw      syscall.RawConn
	rawErr   bool // the underlying conn exposes no usable raw fd; decline forever
	family   int  // socket address family (AF_INET / AF_INET6), from getsockname
	dest     *sendmsgXDest
	destAddr string
	msgs     []msghdrX
	iovs     []syscall.Iovec
}

func (c *sconn) darwinBatchState() *darwinBatch {
	bs, _ := c.batch.(*darwinBatch)
	if bs == nil {
		bs = &darwinBatch{}
		c.batch = bs
	}
	return bs
}

func (bs *darwinBatch) scratch(n int) ([]msghdrX, []syscall.Iovec) {
	if cap(bs.msgs) < n {
		bs.msgs = make([]msghdrX, n)
		bs.iovs = make([]syscall.Iovec, n)
	}
	return bs.msgs[:n], bs.iovs[:n]
}

// batchSendAvailable reports whether this connection can submit batches
// right now: the process-wide capability is qualified and unlatched, the
// remote is a UDP address, and the socket exposes a raw fd. Every datagram
// that proceeds per-packet because this returned false is counted as a
// fallback, which is what makes the inert path observable.
func (c *sconn) nativeBatchSendAvailable() bool {
	if !sendmsgXAvailable() {
		sendmsgX.fallbackPackets.Add(1)
		return false
	}
	bs := c.darwinBatchState()
	if bs.rawErr {
		sendmsgX.fallbackPackets.Add(1)
		return false
	}
	if _, ok := c.remoteAddrInfo.Load().addr.(*net.UDPAddr); !ok {
		sendmsgX.fallbackPackets.Add(1)
		return false
	}
	return true
}

// udpSyscallConn exposes the raw descriptor for batched submission only
// when the wrapped socket is a native *net.UDPConn. quic-go documents
// OOBCapablePacketConn as a caller extension point: a custom implementation
// may override WriteMsgUDP, and every datagram must keep flowing through
// that override, so batching — which bypasses WriteMsgUDP by design — is
// reserved for sockets whose write semantics are the kernel's own.
func (c *oobConn) udpSyscallConn() (syscall.RawConn, bool) {
	udpConn, ok := c.OOBCapablePacketConn.(*net.UDPConn)
	if !ok {
		return nil, false
	}
	raw, err := udpConn.SyscallConn()
	if err != nil {
		return nil, false
	}
	return raw, true
}

// sendBatch submits bufs as individual datagrams to the connection's remote
// address in one sendmsg_x syscall and returns how many the kernel
// accepted. An observable shortfall — a short count, a zero-progress errno,
// a closed conn, the structural latch — reports fewer accepted entries with
// a nil error, and the send worker retries the first unaccepted entry
// through the per-packet path. A non-nil error means kernel progress is
// unknowable; the worker fails the send path without resending.
func (c *sconn) sendNativeBatch(bufs [][]byte, ecn protocol.ECN) (int, error) {
	bs := c.darwinBatchState()
	ai := c.remoteAddrInfo.Load()
	udpAddr, ok := ai.addr.(*net.UDPAddr)
	if !ok {
		return 0, nil
	}
	if err := c.checkPacketSend(udpAddr); err != nil {
		return 0, err
	}
	if bs.raw == nil {
		rc, ok := packetIOConn(c.rawConn).(interface {
			udpSyscallConn() (syscall.RawConn, bool)
		})
		if !ok {
			bs.rawErr = true
			return 0, nil
		}
		raw, ok := rc.udpSyscallConn()
		if !ok {
			bs.rawErr = true
			return 0, nil
		}
		bs.raw = raw
	}

	// Build the control message the way WritePacket does. gsoSize is always 0
	// here (batches never carry GSO segments), so the only control message is
	// the ECN marking; it is constant across the batch (one remote, one ecn),
	// so build it once. ai.oob carries spare capacity for exactly this append.
	oob := ai.oob
	if ecn != protocol.ECNUnsupported {
		if udpAddr.IP.To4() != nil {
			oob = appendIPv4ECNMsg(oob, ecn)
		} else {
			oob = appendIPv6ECNMsg(oob, ecn)
		}
	}

	return bs.submit(bufs, oob, udpAddr)
}

// initFamily caches the socket family for native encoding and factory admission.
func (bs *darwinBatch) initFamily() bool {
	if bs.family == 0 { // determine the socket's address family once
		var family int
		if err := bs.raw.Control(func(fd uintptr) {
			if sa, err := syscall.Getsockname(int(fd)); err == nil {
				switch sa.(type) {
				case *syscall.SockaddrInet4:
					family = syscall.AF_INET
				case *syscall.SockaddrInet6:
					family = syscall.AF_INET6
				}
			}
		}); err != nil || family == 0 {
			bs.rawErr = true
			return false
		}
		bs.family = family
	}
	return true
}

// submit is the shared native owner. Callers serialize access and own fallback.
func (bs *darwinBatch) submit(bufs [][]byte, oob []byte, udpAddr *net.UDPAddr) (int, error) {
	if !bs.initFamily() {
		return 0, nil
	}
	if bs.dest == nil || bs.destAddr != udpAddr.String() {
		bs.dest = newSendmsgXDest(udpAddr, bs.family)
		bs.destAddr = udpAddr.String()
	}

	msgs, iovs := bs.scratch(len(bufs))
	// Borrowed payloads and OOB must not remain reachable through cached scratch.
	defer clear(msgs)
	defer clear(iovs)
	var accepted int
	var submitErr error
	if err := bs.raw.Write(func(fd uintptr) bool {
		accepted, submitErr = sendmsgXSubmit(int(fd), bufs, bs.dest.name(), bs.dest.namelen, oob, msgs, iovs)
		return true // never wait for writability: the worker's per-packet retry owns backpressure
	}); err != nil {
		// The conn is closed or unusable and the submission never ran; the
		// per-packet retry surfaces the real error with correct attribution.
		return 0, nil
	}
	return accepted, submitErr
}

// newUDPBatchWriter accelerates only the existing unconnected, nonempty native
// batch domain. The ordinary writer preserves net.UDPConn validation and the
// remaining message shapes, without widening the private syscall contract.
func newUDPBatchWriter(conn udpMessageWriter) func([][]byte, []byte, *net.UDPAddr) (int, error) {
	fallback := ordinaryUDPBatchWriter(conn)
	udp, ok := conn.(*net.UDPConn)
	if !ok || udp.RemoteAddr() != nil {
		return fallback
	}
	raw, err := udp.SyscallConn()
	if err != nil {
		return fallback
	}
	bs := &darwinBatch{raw: raw}
	var mutex sync.Mutex
	return func(bufs [][]byte, oob []byte, addr *net.UDPAddr) (int, error) {
		// Keep scratch bounded to the send worker's existing batch size. Empty
		// payloads and addresses outside the native domain retain standard writes.
		if len(bufs) < 2 || len(bufs) > maxSendBatch || addr == nil || addr.Port < 0 || addr.Port > 65535 || addr.IP.To16() == nil {
			return fallback(bufs, oob, addr)
		}
		for _, buf := range bufs {
			if len(buf) == 0 {
				return fallback(bufs, oob, addr)
			}
		}
		if !sendmsgXAvailable() {
			sendmsgX.fallbackPackets.Add(uint64(len(bufs)))
			return fallback(bufs, oob, addr)
		}
		mutex.Lock()
		defer mutex.Unlock()
		if bs.rawErr || !sendmsgXAvailable() || !bs.initFamily() || (bs.family == syscall.AF_INET && addr.IP.To4() == nil) {
			return fallback(bufs, oob, addr)
		}
		return bs.submit(bufs, oob, addr)
	}
}
