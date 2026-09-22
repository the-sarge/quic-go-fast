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

// Called with the endpoint lock and no active I/O. No view exposes the socket:
// policy wrappers keep receiving normalized datagrams through their ReadFrom.
func (e *managedPacketEndpoint) configureReceive() error {
	if e.managedNative != nil || e.managedECNSetup.failedFamily != "" {
		return nil
	}
	udp, ok := e.conn.(*net.UDPConn)
	if !ok {
		return nil
	}
	// Complete all fallible decoder setup without receive-format mutation.
	reader, setup, err := newConnWithSetup(udp, false, false)
	qualification := inspectManagedECNQualification(udp, setup)
	e.managedECNSetup = qualification
	if err == errECNSetupDenied {
		// The factory socket still has ordinary receive format. Only the
		// demonstrated optional ECN denial permits this fallback; descriptor
		// failures and required packet-info failures remain fatal.
		e.receiveState = receiveCoalescingState{eligible: true, disabledReason: "ancillary_setup_denied"}
		return nil
	}
	if err != nil {
		return err
	}
	e.managedNative = reader
	e.managedECN = qualification.qualified
	raw, err := udp.SyscallConn()
	if err != nil {
		return err
	}
	// Install the sole interpretation/storage owner before changing the format.
	e.receiver = reader
	reader.cap.GRO, reader.cap.receiveCoalescing.disabledReason = enableGRO(raw)
	e.receiveState = reader.cap.receiveCoalescing
	e.receiveCoalescing = reader.cap.GRO
	reader.managedRead = qualification.qualified && !reader.cap.GRO
	if !reader.cap.GRO && !qualification.qualified {
		e.receiver = nil
	}
	return nil
}

func inspectManagedECNQualification(udp *net.UDPConn, setup oobConnSetup) managedECNQualification {
	q := managedECNQualification{sendIPv4: true, sendIPv6: true}
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
	q.receiveIPv4 = q.admittedIPv4 && setup.ecnIPv4Err == nil
	q.receiveIPv6 = q.admittedIPv6 && setup.ecnIPv6Err == nil
	var failed []string
	if q.admittedIPv4 && (!q.receiveIPv4 || !q.sendIPv4) {
		failed = append(failed, "ipv4")
	}
	if q.admittedIPv6 && (!q.receiveIPv6 || !q.sendIPv6) {
		failed = append(failed, "ipv6")
	}
	if !isECNEnabled() {
		failed = append(failed, "disabled")
	}
	q.failedFamily = strings.Join(failed, ",")
	q.qualified = q.failedFamily == "" && (q.admittedIPv4 || q.admittedIPv6)
	return q
}
