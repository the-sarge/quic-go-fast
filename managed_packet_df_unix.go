//go:build (darwin && !ios) || linux

package quic

import (
	"errors"
	"net"

	"golang.org/x/sys/unix"
)

func managedDFControlFor(conn net.PacketConn) managedDFControl {
	udp, ok := conn.(*net.UDPConn)
	if !ok {
		return nil
	}
	return func(fn func(managedDFSocket) error) error {
		raw, err := udp.SyscallConn()
		if err != nil {
			return err
		}
		var operationErr error
		if err := raw.Control(func(fd uintptr) { operationErr = fn(managedDFNativeSocket{fd: int(fd)}) }); err != nil {
			return err
		}
		return operationErr
	}
}

type managedDFNativeSocket struct{ fd int }

func (s managedDFNativeSocket) get(o managedDFOption) (int, error) {
	value, err := unix.GetsockoptInt(s.fd, o.level, o.name)
	return value, managedDFOptionError(err)
}

func (s managedDFNativeSocket) set(o managedDFOption, value int) error {
	return managedDFOptionError(unix.SetsockoptInt(s.fd, o.level, o.name, value))
}

func managedDFOptionError(err error) error {
	if errors.Is(err, unix.ENOPROTOOPT) || errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.EACCES) || errors.Is(err, unix.EPERM) {
		return errors.Join(errManagedDFUnavailable, err)
	}
	return err
}
