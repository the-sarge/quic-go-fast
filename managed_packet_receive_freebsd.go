package quic

import "net"

// Called with the endpoint lock and no active I/O. FreeBSD retains native ECN
// metadata without receive coalescing or read-ahead across lease generations.
func (e *managedPacketEndpoint) configureReceive() error {
	udp, ok := e.conn.(*net.UDPConn)
	if !ok || e.managedNative != nil {
		return nil
	}
	reader, setup, err := newConnWithSetup(udp, false, false)
	return e.finishFreeBSDReceiveSetup(udp, reader, setup, err)
}

func (e *managedPacketEndpoint) finishFreeBSDReceiveSetup(udp *net.UDPConn, reader *oobConn, setup oobConnSetup, err error) error {
	if err != nil && !setup.ecnUnavailable {
		return err
	}
	q := inspectManagedECNQualification(udp, setup)
	e.managedECNSetup = q
	e.managedECN = q.qualified
	e.receiveState = receiveCoalescingState{disabledReason: "unsupported_platform"}
	if err != nil || !q.qualified {
		return nil
	}
	reader.managedRead = true
	e.receiver = reader
	e.managedNative = reader
	return nil
}

func refreshManagedECNAvailability(q managedECNQualification) managedECNQualification {
	q.disabled = isECNDisabledUsingEnv()
	q.qualified = q.failedFamily == "" && !q.disabled && (q.admittedIPv4 || q.admittedIPv6)
	return q
}

func managedIPv4ECNSetupError(_ managedECNQualification, setup oobConnSetup) error {
	return setup.ecnIPv4Err
}
