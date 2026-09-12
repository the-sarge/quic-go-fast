//go:build darwin && !ios && !quic_go_no_private_syscalls

package quic

// macOS batch send over the private sendmsg_x(2) syscall (XNU syscall 481).
// Adapted from KeibiSoft/quic-go commit 24ecf34c (MIT licensed, courtesy of
// Marius-Florin Cristian / KeibiSoft), with the qualification, fallback, and
// error-attribution contract of docs/adr/2026-09-11-datapath-offload-plan.md.
//
// sendmsg_x is a private, undocumented XNU syscall with no ABI stability
// guarantee from Apple. Every use is gated by the fail-closed qualification
// in sys_conn_sendmsg_x_qual_darwin.go: a Darwin-kernel-major allowlist, a
// production-shape startup self-check, per-call accepted-count bounds, and a
// process-lifetime latch. Distributors whose policies exclude private-syscall
// use build with -tags quic_go_no_private_syscalls; iOS builds never compile
// this file.

import (
	"net"
	"strconv"
	"syscall"
	"unsafe"
)

// sysSendmsgX is SYS_sendmsg_x from the macOS SDK <sys/syscall.h>.
const sysSendmsgX = 481

// msghdrX matches struct msghdr_x (XNU bsd/sys/socket_private.h). The 4-byte
// pads after the two 4-byte fields are the ABI trap; the layout is pinned
// byte-exact (offsets 0/8/16/24/32/40/44/48, size 56) by
// TestSendmsgXMsghdrXLayout.
type msghdrX struct {
	Name       *byte
	Namelen    uint32
	_          uint32
	Iov        *syscall.Iovec
	Iovlen     int32
	_          uint32
	Control    *byte
	Controllen uint32
	Flags      int32
	Datalen    uint64
}

// rawSendmsgX performs the raw syscall; tests inject a fake to script kernel
// behavior without invoking the private ABI.
var rawSendmsgX = func(fd int, msgs []msghdrX) (int, syscall.Errno) {
	sent, _, errno := syscall.Syscall6(sysSendmsgX, uintptr(fd),
		uintptr(unsafe.Pointer(&msgs[0])), uintptr(len(msgs)), 0, 0, 0)
	return int(sent), errno
}

// sendmsgXBatchTo sends each payload as its own datagram in one sendmsg_x
// syscall on an UNCONNECTED socket, which is what quic-go uses: each message
// carries the destination sockaddr (name/namelen) and a shared control
// buffer (oob). A quic-go connection sends to a single remote with a single
// ECN marking per batch, so name and oob are constant across the batch. The
// caller supplies reusable scratch (msgs, iovs) sized >= len(payloads) so
// the hot path is allocation-free. Payload backing arrays are read by the
// kernel during the call and must stay valid until it returns.
//
// It returns the number of leading datagrams the kernel accepted and the
// errno (0 for success). The kernel may accept fewer datagrams than offered;
// the send worker's partial-acceptance algorithm owns that case.
func sendmsgXBatchTo(fd int, payloads [][]byte, name *byte, namelen uint32, oob []byte, msgs []msghdrX, iovs []syscall.Iovec) (int, syscall.Errno) {
	n := len(payloads)
	if n == 0 {
		return 0, 0
	}
	var ctl *byte
	var ctllen uint32
	if len(oob) > 0 {
		ctl = &oob[0]
		ctllen = uint32(len(oob))
	}
	for i, p := range payloads {
		iovs[i] = syscall.Iovec{Base: &p[0], Len: uint64(len(p))}
		msgs[i] = msghdrX{
			Name: name, Namelen: namelen,
			Iov: &iovs[i], Iovlen: 1,
			Control: ctl, Controllen: ctllen,
			Datalen: uint64(len(p)),
		}
	}
	return rawSendmsgX(fd, msgs[:n])
}

// sendmsgXDest holds a prebuilt destination sockaddr for sendmsgXBatchTo,
// reused across batches (one per connection). Keep it alive while the kernel
// reads it.
type sendmsgXDest struct {
	sa4     syscall.RawSockaddrInet4
	sa6     syscall.RawSockaddrInet6
	isV6    bool
	namelen uint32
}

// newSendmsgXDest builds the destination sockaddr matching the SOCKET's
// family, not the address's: quic-go binds dual-stack AF_INET6 sockets
// (network "udp"), which reject a raw sockaddr_in and require a v4-mapped
// sockaddr_in6 for IPv4 destinations. sockFamily is the socket's family
// (AF_INET or AF_INET6), from getsockname.
func newSendmsgXDest(addr *net.UDPAddr, sockFamily int) *sendmsgXDest {
	d := &sendmsgXDest{}
	v4 := addr.IP.To4()
	if sockFamily == syscall.AF_INET && v4 != nil {
		d.sa4.Len = syscall.SizeofSockaddrInet4
		d.sa4.Family = syscall.AF_INET
		bePort(unsafe.Pointer(&d.sa4.Port), addr.Port)
		copy(d.sa4.Addr[:], v4)
		d.namelen = uint32(syscall.SizeofSockaddrInet4)
		return d
	}
	// IPv6 or dual-stack socket: sockaddr_in6, v4-mapped (::ffff:a.b.c.d)
	// for IPv4 destinations.
	d.isV6 = true
	d.sa6.Len = syscall.SizeofSockaddrInet6
	d.sa6.Family = syscall.AF_INET6
	bePort(unsafe.Pointer(&d.sa6.Port), addr.Port)
	if v4 != nil {
		d.sa6.Addr[10], d.sa6.Addr[11] = 0xff, 0xff
		copy(d.sa6.Addr[12:], v4)
	} else {
		copy(d.sa6.Addr[:], addr.IP.To16())
		if addr.Zone != "" {
			// Link-local scope id: an interface name, else a decimal
			// interface index — the same resolution order the standard
			// library applies on the per-packet WriteMsgUDP path.
			if iface, err := net.InterfaceByName(addr.Zone); err == nil {
				d.sa6.Scope_id = uint32(iface.Index)
			} else if n, err := strconv.Atoi(addr.Zone); err == nil && n >= 0 {
				d.sa6.Scope_id = uint32(n)
			}
		}
	}
	d.namelen = uint32(syscall.SizeofSockaddrInet6)
	return d
}

// bePort writes port as big-endian (network order) bytes; darwin is
// little-endian, so this matches how Go's stdlib builds a sockaddr.
func bePort(p unsafe.Pointer, port int) {
	b := (*[2]byte)(p)
	b[0] = byte(port >> 8)
	b[1] = byte(port)
}

func (d *sendmsgXDest) name() *byte {
	if d.isV6 {
		return (*byte)(unsafe.Pointer(&d.sa6))
	}
	return (*byte)(unsafe.Pointer(&d.sa4))
}
