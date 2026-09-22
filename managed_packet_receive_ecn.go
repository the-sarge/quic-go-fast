//go:build darwin || linux

package quic

import (
	"net"
	"strings"

	"golang.org/x/sys/unix"
)

func (e *managedPacketEndpoint) managedPacketRawFactory(lease *managedPacketConn) func(rawConn, *externalPacketIO) rawConn {
	if e.managedNative == nil {
		return nil
	}
	return func(base rawConn, config *externalPacketIO) rawConn {
		return newManagedPacketRawConn(base, lease, config, samePacketConn(config.conn, lease))
	}
}

func inspectManagedECNQualification(udp *net.UDPConn, setup oobConnSetup) managedECNQualification {
	q := managedECNQualification{}
	raw, err := udp.SyscallConn()
	if err != nil {
		q.failedFamily = "family_domain"
		return q
	}
	var inspectErr error
	if err := raw.Control(func(fd uintptr) {
		sa, err := unix.Getsockname(int(fd))
		if err != nil {
			inspectErr = err
			return
		}
		switch sa.(type) {
		case *unix.SockaddrInet4:
			q.admittedIPv4 = true
		case *unix.SockaddrInet6:
			q.admittedIPv6 = true
			v6Only, err := unix.GetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_V6ONLY)
			if err != nil {
				inspectErr = err
				return
			}
			q.ipv6Only = v6Only != 0
			if !q.ipv6Only {
				q.admittedIPv4 = true
				q.ipv4Mapped = true
			}
		default:
			q.failedFamily = "family_domain"
		}
	}); err != nil {
		inspectErr = err
	}
	if inspectErr != nil {
		q.failedFamily = "family_domain"
		return q
	}
	q.receiveIPv4 = q.admittedIPv4 && managedIPv4ECNSetupError(q, setup) == nil
	q.receiveIPv6 = q.admittedIPv6 && setup.ecnIPv6Err == nil
	var failed []string
	if q.admittedIPv4 && !q.receiveIPv4 {
		failed = append(failed, "ipv4")
	}
	if q.admittedIPv6 && !q.receiveIPv6 {
		failed = append(failed, "ipv6")
	}
	q.failedFamily = strings.Join(failed, ",")
	return refreshManagedECNAvailability(q)
}
