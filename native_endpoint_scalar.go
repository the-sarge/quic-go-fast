//go:build unix

package quic

import (
	"net"
	"net/netip"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// endpointScalarSocket belongs to the existing writer. It contains immutable
// socket mechanics and error context, never request permission or a cached deadline.
type endpointScalarSocket struct {
	endpointNativeIO
	raw     syscall.RawConn
	send    func(uintptr, []byte, []byte, *unix.SockaddrInet4) (int, error)
	network string
	source  net.Addr
}

func bindEndpointScalarSocket(socket *net.UDPConn, raw syscall.RawConn, send func(uintptr, []byte, []byte, *unix.SockaddrInet4) (int, error)) endpointNativeIO {
	// On an unconnected UDPConn, net rejects a missing destination before any
	// syscall. This public error carries fd.net and the actual source object;
	// LocalAddr().Network() alone would lose the udp / udp4 distinction.
	n, nn, err := socket.WriteMsgUDPAddrPort(nil, nil, netip.AddrPort{})
	op, ok := err.(*net.OpError)
	if !ok || n != 0 || nn != 0 || op.Op != "write" || (op.Net != "udp" && op.Net != "udp4") || op.Source != socket.LocalAddr() || op.Addr == nil || op.Addr.Network() != "udp" || op.Addr.String() != (netip.AddrPort{}).String() {
		return socket
	}
	return &endpointScalarSocket{endpointNativeIO: socket, raw: raw, send: send, network: op.Net, source: op.Source}
}

type endpointScalarRequest struct {
	*endpointScalarSocket
	token      *endpointToken
	armed      bool
	controlErr error

	// One writer lends call inputs only until RawWrite returns. The callback
	// and sockaddr belong to this request, never to a cross-request cache.
	callback func(uintptr) bool
	data     []byte
	oob      []byte
	address  unix.SockaddrInet4
	n        int
	sendErr  error
}

// Go's addrPortUDPAddr is private. Preserve its public value/method surface,
// including mapped-address spelling, without depending on its dynamic type.
type endpointScalarAddr struct{ netip.AddrPort }

func (endpointScalarAddr) Network() string { return "udp" }

func (s *endpointScalarRequest) writeError(err error, destination netip.AddrPort) error {
	if op, ok := err.(*net.OpError); ok {
		err = op.Err
	}
	if _, ok := err.(syscall.Errno); ok {
		err = os.NewSyscallError("sendmsg", err)
	}
	return &net.OpError{Op: "write", Net: s.network, Source: s.source, Addr: endpointScalarAddr{destination}, Err: err}
}

func prepareEndpointRequestIO(native endpointNativeIO, token *endpointToken) (endpointNativeIO, *endpointScalarRequest, error) {
	if socket, ok := native.(*endpointScalarSocket); ok && socket.supports(token.request) {
		scope := &endpointScalarRequest{endpointScalarSocket: socket, token: token}
		scope.callback = scope.write
		return scope, scope, native.SetWriteDeadline(time.Time{})
	}
	return native, nil, native.SetWriteDeadline(token.request.deadline)
}

func (*endpointScalarSocket) supports(request *endpointRequest) bool {
	addr := request.destination.Addr()
	if !request.destination.IsValid() || addr.Zone() != "" || (!addr.Is4() && !addr.Is4In6()) || len(request.members) == 0 {
		return false
	}
	for _, member := range request.members {
		// Software segments retain their existing scalar split. Native GSO,
		// even if currently disabled, selects eager I/O for this whole request.
		if member.gsoSize != 0 && member.nativeGSO {
			return false
		}
	}
	return true
}

func (s *endpointScalarRequest) write(fd uintptr) bool {
	for {
		if s.sendErr = endpointSubmissionAllowed(s.token); s.sendErr != nil {
			return true
		}
		s.n, s.sendErr = s.send(fd, s.data, s.oob, &s.address)
		if s.sendErr == syscall.EINTR {
			continue
		}
		if s.sendErr != syscall.EAGAIN && s.sendErr != syscall.EWOULDBLOCK {
			return true
		}
		if s.sendErr = endpointSubmissionAllowed(s.token); s.sendErr != nil {
			return true
		}
		if !s.armed {
			if s.sendErr = s.SetWriteDeadline(s.token.request.deadline); s.sendErr != nil {
				s.controlErr = s.sendErr
				return true
			}
			s.armed = true
		}
		// RawWrite holds the descriptor's write borrow. Its callback must
		// never permit waitWrite before the current request's D is armed.
		return false
	}
}

func (s *endpointScalarRequest) WriteMsgUDPAddrPort(data, oob []byte, destination netip.AddrPort) (int, int, error) {
	s.data, s.oob = data, oob
	s.n, s.sendErr = 0, nil
	s.address = unix.SockaddrInet4{Addr: destination.Addr().As4(), Port: int(destination.Port())}
	err := s.raw.Write(s.callback)
	n, sendErr := s.n, s.sendErr
	// RawWrite has ended every callback/wait borrow. Clear transient state
	// before any return; the armed deadline and control fault span the request.
	s.data, s.oob = nil, nil
	s.n, s.sendErr = 0, nil
	s.address = unix.SockaddrInet4{}
	if err != nil {
		return n, 0, s.writeError(err, destination)
	}
	if sendErr != nil {
		return n, 0, s.writeError(sendErr, destination)
	}
	return n, len(oob), nil
}
