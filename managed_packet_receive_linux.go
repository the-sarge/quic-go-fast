package quic

import "net"

// Called with the endpoint lock and no active I/O. No view exposes the socket:
// policy wrappers keep receiving normalized datagrams through their ReadFrom.
func (e *managedPacketEndpoint) configureReceive() error {
	if e.receiver != nil {
		return nil
	}
	udp, ok := e.conn.(*net.UDPConn)
	if !ok {
		return nil
	}
	// Complete all fallible decoder setup without receive-format mutation.
	reader, err := newConn(udp, false, false)
	if err != nil {
		return err
	}
	raw, err := udp.SyscallConn()
	if err != nil {
		return err
	}
	// Install the sole interpretation/storage owner before changing the format.
	e.receiver = reader
	reader.cap.GRO, reader.cap.receiveCoalescing.disabledReason = enableGRO(raw)
	e.receiveState = reader.cap.receiveCoalescing
	if !reader.cap.GRO {
		e.receiver = nil
	}
	return nil
}
