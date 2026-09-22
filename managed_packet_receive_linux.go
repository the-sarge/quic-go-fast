package quic

import (
	"net"
)

// Called with the endpoint lock and no active I/O. No view exposes the socket:
// policy wrappers keep receiving normalized datagrams through their ReadFrom.
func (e *managedPacketEndpoint) configureReceive() error {
	udp, ok := e.conn.(*net.UDPConn)
	if !ok {
		return nil
	}
	if reader, ok := e.receiver.(*oobConn); ok {
		q := e.managedECNSetup
		if e.managedNative == nil && q.failedFamily == "" && (q.admittedIPv4 || q.admittedIPv6) {
			q = refreshManagedECNAvailability(q)
			e.managedECNSetup = q
			e.managedECN = q.qualified
			if q.qualified {
				e.managedNative = reader
				reader.managedRead = !reader.cap.GRO
			}
		}
		if e.receiveCoalescing {
			return nil
		}
		if e.managedNative != nil {
			raw, err := udp.SyscallConn()
			if err != nil {
				return err
			}
			reader.cap.GRO, reader.cap.receiveCoalescing.disabledReason = enableGRO(raw)
			e.receiveState = reader.cap.receiveCoalescing
			e.receiveCoalescing = reader.cap.GRO
			reader.managedRead = !reader.cap.GRO
			return nil
		}
	}
	// Complete all fallible decoder setup before publishing any endpoint state.
	reader, setup, err := newConnWithSetup(udp, false, false)
	if err == errECNSetupDenied {
		// The factory socket still has ordinary receive format. Only the
		// demonstrated optional ECN denial permits this fallback; descriptor
		// failures and required packet-info failures remain fatal.
		e.managedECNSetup = inspectManagedECNQualification(udp, setup)
		e.receiveState = receiveCoalescingState{eligible: true, disabledReason: "ancillary_setup_denied"}
		return nil
	}
	if err != nil {
		return err
	}
	qualification := inspectManagedECNQualification(udp, setup)
	raw, err := udp.SyscallConn()
	if err != nil {
		return err
	}
	// Install the sole interpretation/storage owner before changing the format.
	e.receiver = reader
	reader.cap.GRO, reader.cap.receiveCoalescing.disabledReason = enableGRO(raw)
	e.receiveState = reader.cap.receiveCoalescing
	e.receiveCoalescing = reader.cap.GRO
	e.managedECNSetup = qualification
	e.managedECN = qualification.qualified
	reader.managedRead = qualification.qualified && !reader.cap.GRO
	if qualification.qualified {
		e.managedNative = reader
	}
	if !reader.cap.GRO && !qualification.qualified {
		e.receiver = nil
	}
	return nil
}

func refreshManagedECNAvailability(q managedECNQualification) managedECNQualification {
	q.disabled = isECNDisabledUsingEnv()
	q.kernelUnsupported = kernelVersionMajor < 5
	q.qualified = q.failedFamily == "" && !q.disabled && !q.kernelUnsupported && (q.admittedIPv4 || q.admittedIPv6)
	return q
}

func managedIPv4ECNSetupError(_ managedECNQualification, setup oobConnSetup) error {
	return setup.ecnIPv4Err
}
