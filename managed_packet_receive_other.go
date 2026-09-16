//go:build !linux

package quic

func (e *managedPacketEndpoint) configureReceive() error { return nil }
