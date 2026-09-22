//go:build (!darwin || ios) && !linux && !windows

package quic

func (e *managedPacketEndpoint) configureReceive() error { return nil }

func (e *managedPacketEndpoint) managedPacketRawFactory(*managedPacketConn) func(rawConn, *externalPacketIO) rawConn {
	return nil
}
