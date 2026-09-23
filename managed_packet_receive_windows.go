//go:build windows

package quic

import (
	"net"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/windows"
)

func (e *managedPacketEndpoint) managedPacketRawFactory(lease *managedPacketConn) func(rawConn, *externalPacketIO) rawConn {
	if e.managedNative == nil {
		return nil
	}
	return func(base rawConn, config *externalPacketIO) rawConn {
		return newManagedPacketRawConn(base, lease, config, samePacketConn(config.conn, lease))
	}
}

// configureReceive runs under the endpoint lock after ordinary I/O has joined.
// The same decoder owns URO across leases, including when ECN is opted out.
func (e *managedPacketEndpoint) configureReceive() error {
	udp, ok := e.conn.(*net.UDPConn)
	if !ok {
		return nil
	}
	reader, retained := e.receiver.(*windowsConn)
	if !retained {
		var err error
		reader, err = newConn(udp, false, false)
		if err != nil {
			return err
		}
	}
	q := configureWindowsECN(udp)
	raw, err := udp.SyscallConn()
	if err != nil {
		return err
	}
	reader.managed = true
	e.receiver = reader // interpretation is installed before receive-format mutation
	if !reader.cap.GRO {
		reader.cap.GRO, reader.cap.receiveCoalescing.disabledReason = enableURO(raw)
	}
	e.receiveState = reader.cap.receiveCoalescing
	e.receiveCoalescing = reader.cap.GRO
	e.managedECNSetup, e.managedECN = q, q.qualified
	e.managedNative = nil
	if q.qualified {
		e.managedNative = reader
	}
	if !reader.cap.GRO && !q.qualified {
		e.receiver = nil
	}
	return nil
}

func configureWindowsECN(udp *net.UDPConn) managedECNQualification {
	disabled, _ := strconv.ParseBool(os.Getenv("QUIC_GO_DISABLE_ECN"))
	q := managedECNQualification{disabled: disabled}
	raw, err := udp.SyscallConn()
	if err != nil {
		q.failedFamily = "family_domain"
		return q
	}
	err = raw.Control(func(fd uintptr) {
		handle := windows.Handle(fd)
		sa, inspectErr := windows.Getsockname(handle)
		if inspectErr != nil {
			q.failedFamily = "family_domain"
			return
		}
		switch addr := sa.(type) {
		case *windows.SockaddrInet4:
			q.admittedIPv4 = true
		case *windows.SockaddrInet6:
			q.admittedIPv6 = true
			v6Only, err := windows.GetsockoptInt(handle, windows.IPPROTO_IPV6, windows.IPV6_V6ONLY)
			if err != nil {
				q.failedFamily = "family_domain"
				return
			}
			q.ipv6Only = v6Only != 0
			// A specific native IPv6 binding cannot receive mapped IPv4.
			if !q.ipv6Only {
				if net.IP(addr.Addr[:]).To4() != nil {
					q.admittedIPv4, q.ipv4Mapped, q.admittedIPv6 = true, true, false
				} else if addr.Addr == [16]byte{} {
					q.admittedIPv4, q.ipv4Mapped = true, true
				}
			}
		default:
			q.failedFamily = "family_domain"
			return
		}
		q = setupWindowsECNFamilies(q, func(level int) error {
			return windows.SetsockoptInt(handle, level, windowsECN, 1)
		})
	})
	if err != nil {
		q.failedFamily = "family_domain"
		q.qualified = false
	}
	return q
}

func setupWindowsECNFamilies(q managedECNQualification, enable func(int) error) managedECNQualification {
	var failed []string
	if q.admittedIPv4 {
		q.receiveIPv4 = enable(windows.IPPROTO_IP) == nil
		if !q.receiveIPv4 {
			failed = append(failed, "ipv4")
		}
	}
	if q.admittedIPv6 {
		q.receiveIPv6 = enable(windows.IPPROTO_IPV6) == nil
		if !q.receiveIPv6 {
			failed = append(failed, "ipv6")
		}
	}
	q.failedFamily = strings.Join(failed, ",")
	q.qualified = q.failedFamily == "" && !q.disabled && (q.admittedIPv4 || q.admittedIPv6)
	return q
}
