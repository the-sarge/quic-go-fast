//go:build !linux && !windows

package quic

func (e *managedPacketEndpoint) configureReceive() error { return nil }
