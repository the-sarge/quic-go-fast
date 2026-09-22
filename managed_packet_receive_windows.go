//go:build windows

package quic

func (e *managedPacketEndpoint) managedPacketRawFactory(*managedPacketConn) func(rawConn, *externalPacketIO) rawConn {
	return nil
}

// configureReceive runs under the endpoint lock after ordinary I/O has joined.
// The endpoint owns the native socket and retains the decoder across leases;
// public reads still pass individual datagrams through the supplied wrapper.
func (e *managedPacketEndpoint) configureReceive() error {
	if e.receiver != nil {
		return nil
	}
	socket, ok := e.conn.(OOBCapablePacketConn)
	if !ok {
		return nil
	}
	// Finish fallible setup without changing the receive format.
	receiver, err := newConn(socket, false, false)
	if err != nil {
		return err
	}
	raw, err := socket.SyscallConn()
	if err != nil {
		return err
	}
	// Install normalization before the socket can queue coalesced data.
	e.receiver = receiver
	receiver.cap.GRO, receiver.cap.receiveCoalescing.disabledReason = enableURO(raw)
	e.receiveState = receiver.cap.receiveCoalescing
	if !receiver.cap.GRO {
		// Ordinary socket reads preserve the full public UDP payload domain
		// when coalescing is disabled or unavailable.
		e.receiver = nil
	}
	return nil
}
