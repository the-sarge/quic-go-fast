package quic

import (
	"net"
	"net/netip"

	"golang.org/x/sys/unix"
)

// Only the existing exact-socket constructor installs this private mechanism.
// Unsupported sockets keep eager I/O; a failed capability read is not a new
// endpoint-construction error. The first real request still validates deadlines.
func newEndpointNativeIO(socket *net.UDPConn) endpointNativeIO {
	raw, err := socket.SyscallConn()
	if err != nil {
		return socket
	}
	inet4 := false
	if err := raw.Control(func(fd uintptr) {
		address, err := unix.Getsockname(int(fd))
		if err == nil {
			_, inet4 = address.(*unix.SockaddrInet4)
		}
	}); err != nil || !inet4 {
		return socket
	}
	return bindEndpointScalarSocket(socket, raw, func(fd uintptr, data, oob []byte, destination netip.AddrPort) (int, error) {
		address := unix.SockaddrInet4{Addr: destination.Addr().As4(), Port: int(destination.Port())}
		return unix.SendmsgN(int(fd), data, oob, &address, unix.MSG_DONTWAIT)
	})
}
