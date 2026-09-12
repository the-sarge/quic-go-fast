//go:build darwin && !ios && !quic_go_no_private_syscalls

package quic

// macOS receive batching over the private recvmsg_x(2) syscall (XNU syscall
// 480), Slice D2 of docs/adr/2026-09-11-darwin-batch-plan.md. New
// development: the KeibiSoft receive half is documented prior art that never
// batched and is not ported. recvmsg_x shares struct msghdr_x with the D1
// sendmsg_x path (layout pinned byte-exact by TestSendmsgXMsghdrXLayout);
// on receive, msg_datalen returns each message's received byte count.
//
// recvmsg_x is a private, undocumented XNU syscall with no ABI stability
// guarantee from Apple. Every use is gated by the fail-closed qualification
// in sys_conn_recvmsg_x_qual_darwin.go: the shared Darwin-kernel-major
// allowlist, a production-shape receive self-check, per-call structural
// bounds, and a process-lifetime latch. Distributors whose policies exclude
// private-syscall use build with -tags quic_go_no_private_syscalls; iOS
// builds never compile this file.

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/net/ipv4"
)

// sysRecvmsgX is SYS_recvmsg_x from the macOS SDK <sys/syscall.h>.
const sysRecvmsgX = 480

// batchSize is the Darwin receive batch ceiling with the recvmsg_x path
// compiled in: the shared read loop sizes its buffers for this many
// messages, and receiveBatchSize below gates how many are actually used per
// read. 8 mirrors the Linux batch; it must stay smaller than MaxUint8
// (oobConn.readPos). The declared per-connection receive memory budget is
// batchSize × (MaxPacketBufferSize + oobBufferSize + the name/iovec/msghdr
// scratch below); the D2 protocol accounts for it.
const batchSize = 8

// receiveBatchSize reports how many messages the shared read loop may fill
// on the next read: the full batch only when the receive capability is
// engaged, otherwise exactly one, which keeps the capability-off read path
// byte-identical to the batchSize-1 behavior this experiment must preserve.
func receiveBatchSize() int {
	if recvmsgXAvailable() {
		return batchSize
	}
	return 1
}

// rawRecvmsgX performs the raw syscall; tests inject a fake to script kernel
// behavior without invoking the private ABI. It returns the number of
// messages received and the errno (0 for success).
var rawRecvmsgX = func(fd int, msgs []msghdrX, flags int) (int, syscall.Errno) {
	n, _, errno := syscall.Syscall6(sysRecvmsgX, uintptr(fd),
		uintptr(unsafe.Pointer(&msgs[0])), uintptr(len(msgs)), uintptr(flags), 0, 0)
	return int(n), errno
}

// recvmsgXSockaddrLen is the per-message msg_name buffer size; sockaddr_in6
// (28 bytes) is the largest source address a UDP socket surfaces.
const recvmsgXSockaddrLen = syscall.SizeofSockaddrInet6

// recvmsgXScratch holds the per-connection msghdr_x, iovec, and sockaddr
// arrays a batched read reuses across calls, so the hot path is
// allocation-free. The kernel writes into them during the call.
type recvmsgXScratch struct {
	msgs  []msghdrX
	iovs  []syscall.Iovec
	names [][recvmsgXSockaddrLen]byte
	bufs  [][]byte
	oobs  [][]byte
}

func newRecvmsgXScratch(n int) *recvmsgXScratch {
	return &recvmsgXScratch{
		msgs:  make([]msghdrX, n),
		iovs:  make([]syscall.Iovec, n),
		names: make([][recvmsgXSockaddrLen]byte, n),
		bufs:  make([][]byte, 0, n),
		oobs:  make([][]byte, 0, n),
	}
}

// prepare points message i at bufs[i]/oobs[i] and this scratch's name
// buffer. len(bufs) messages are prepared; bufs and oobs must be the same
// length, within the scratch capacity, and non-empty per entry.
func (s *recvmsgXScratch) prepare(bufs, oobs [][]byte) {
	for i := range bufs {
		s.iovs[i] = syscall.Iovec{Base: &bufs[i][0], Len: uint64(len(bufs[i]))}
		s.msgs[i] = msghdrX{
			Name:    &s.names[i][0],
			Namelen: recvmsgXSockaddrLen,
			Iov:     &s.iovs[i],
			Iovlen:  1,
			Control: &oobs[i][0], Controllen: uint32(len(oobs[i])),
		}
	}
}

// recvmsgXSourceAddr decodes message i's kernel-written msg_name into the
// *net.UDPAddr shape the existing x/net read path produces: raw address
// bytes without v4-mapped unmapping, and a link-local zone resolved to the
// interface name (falling back to the decimal index). Behavioral parity with
// the fallback path matters because the capability can engage mid-connection
// and a changed address representation would masquerade as a path migration.
func (s *recvmsgXScratch) sourceAddr(i int) (*net.UDPAddr, error) {
	namelen := s.msgs[i].Namelen
	name := &s.names[i]
	switch {
	case namelen == syscall.SizeofSockaddrInet6 && name[1] == syscall.AF_INET6:
		sa := (*syscall.RawSockaddrInet6)(unsafe.Pointer(name))
		addr := &net.UDPAddr{
			IP:   append([]byte(nil), sa.Addr[:]...),
			Port: int(sa.Port>>8)&0xff | int(sa.Port&0xff)<<8,
		}
		if sa.Scope_id != 0 {
			if ifi, err := net.InterfaceByIndex(int(sa.Scope_id)); err == nil {
				addr.Zone = ifi.Name
			} else {
				addr.Zone = strconv.Itoa(int(sa.Scope_id))
			}
		}
		return addr, nil
	case namelen == syscall.SizeofSockaddrInet4 && name[1] == syscall.AF_INET:
		sa := (*syscall.RawSockaddrInet4)(unsafe.Pointer(name))
		return &net.UDPAddr{
			IP:   append([]byte(nil), sa.Addr[:]...),
			Port: int(sa.Port>>8)&0xff | int(sa.Port&0xff)<<8,
		}, nil
	}
	return nil, structuralRecvmsgXError{field: "msg_name", value: int(namelen)}
}

// structuralRecvmsgXError reports a kernel result outside the pinned ABI
// contract: delivery of the batch is unknowable, so the capability latches.
type structuralRecvmsgXError struct {
	field string
	value int
}

func (e structuralRecvmsgXError) Error() string {
	return "recvmsg_x: structurally invalid " + e.field + ": " + strconv.Itoa(e.value)
}

// recvmsgXConn wraps the fallback batchConn on transport-created Darwin
// conns: while the receive capability is engaged it fills up to batchSize
// messages per recvmsg_x syscall, and in every other case it delegates to
// the wrapped x/net path unchanged. The Darwin conn owns receive batching;
// buffers stay ordinary per-datagram pool buffers (receive batching delivers
// separate datagrams, not a coalesced buffer, so no slab is involved).
type recvmsgXConn struct {
	base    batchConn
	rc      syscall.RawConn
	scratch *recvmsgXScratch

	// Per-instance counters for the D2 protocol's per-endpoint accounting;
	// the process-wide recvmsgX counters aggregate across conns. Reads on
	// one conn are single-goroutine, but Stats may be read from another
	// goroutine after the transfer, so keep them atomic.
	batchReads     atomic.Uint64
	batchDatagrams atomic.Uint64
	eagainWaits    atomic.Uint64
	fallbackReads  atomic.Uint64
}

func newRecvmsgXConn(base batchConn, rc syscall.RawConn) *recvmsgXConn {
	return &recvmsgXConn{base: base, rc: rc, scratch: newRecvmsgXScratch(batchSize)}
}

// wrapReadBatchConn installs the recvmsg_x wrapper on the batchConn the
// transport builds itself. Caller-provided batchConn implementations own
// their batching and are never wrapped (newConn passes them through before
// this hook is consulted).
func wrapReadBatchConn(bc batchConn, rc syscall.RawConn) batchConn {
	return newRecvmsgXConn(bc, rc)
}

// ReadBatch fills up to len(ms) messages in one recvmsg_x syscall while the
// capability is engaged, delegating to the wrapped x/net path otherwise. On
// ENOSYS or any structurally invalid kernel result it latches the capability
// off and completes the read through the fallback path: the datagrams of the
// suspect batch are dropped, which UDP permits, and no datagram can be
// duplicated or misattributed because nothing from the suspect batch is
// surfaced.
func (c *recvmsgXConn) ReadBatch(ms []ipv4.Message, flags int) (int, error) {
	if len(ms) <= 1 || flags != 0 || !recvmsgXAvailable() {
		c.fallbackReads.Add(1)
		recvmsgX.fallbackReads.Add(1)
		return c.base.ReadBatch(ms, flags)
	}
	n, err := c.readBatchX(ms)
	if err == nil && n < 0 {
		// The capability latched mid-read (ENOSYS or a structural result):
		// complete the read on the preserved path.
		c.fallbackReads.Add(1)
		recvmsgX.fallbackReads.Add(1)
		return c.base.ReadBatch(ms[:1], flags)
	}
	return n, err
}

// readBatchX performs one engaged batched read. It returns (-1, nil) when
// the capability latched and the caller must fall back; any error return is
// a genuine read error surfaced with the same semantics as the fallback
// path (deadline and close errors come from the runtime poller unchanged).
func (c *recvmsgXConn) readBatchX(ms []ipv4.Message) (int, error) {
	s := c.scratch
	bufs := s.bufs[:0]
	oobs := s.oobs[:0]
	for i := range ms {
		bufs = append(bufs, ms[i].Buffers[0])
		oobs = append(oobs, ms[i].OOB)
	}
	var n int
	var errno syscall.Errno
	err := c.rc.Read(func(fd uintptr) bool {
		for {
			s.prepare(bufs, oobs)
			n, errno = rawRecvmsgX(int(fd), s.msgs[:len(ms)], 0)
			switch errno {
			case syscall.EINTR:
				continue
			case syscall.EAGAIN: // == EWOULDBLOCK on darwin
				c.eagainWaits.Add(1)
				return false
			}
			return true
		}
	})
	if err != nil {
		return 0, err
	}
	if errno == syscall.ENOSYS {
		recvmsgXLatchOff("kernel returned ENOSYS")
		return -1, nil
	}
	if errno != 0 {
		return 0, os.NewSyscallError("recvmsg_x", errno)
	}
	if n <= 0 || n > len(ms) {
		recvmsgXLatchOff(fmt.Sprintf("structurally invalid message count %d of %d", n, len(ms)))
		return -1, nil
	}
	for i := range n {
		if s.msgs[i].Datalen > uint64(len(bufs[i])) {
			recvmsgXLatchOff(fmt.Sprintf("structurally invalid datalen %d for a %d-byte buffer", s.msgs[i].Datalen, len(bufs[i])))
			return -1, nil
		}
		if s.msgs[i].Controllen > uint32(len(oobs[i])) {
			recvmsgXLatchOff(fmt.Sprintf("structurally invalid controllen %d for a %d-byte buffer", s.msgs[i].Controllen, len(oobs[i])))
			return -1, nil
		}
		addr, saErr := s.sourceAddr(i)
		if saErr != nil {
			recvmsgXLatchOff(saErr.Error())
			return -1, nil
		}
		ms[i].N = int(s.msgs[i].Datalen)
		ms[i].NN = int(s.msgs[i].Controllen)
		ms[i].Flags = int(s.msgs[i].Flags)
		ms[i].Addr = addr
	}
	c.batchReads.Add(1)
	c.batchDatagrams.Add(uint64(n))
	recvmsgX.batchReads.Add(1)
	recvmsgX.batchDatagrams.Add(uint64(n))
	return n, nil
}
