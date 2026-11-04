//go:build !(darwin || linux || freebsd)

package quic

import "net"

// The native endpoint route has no activation on this platform.
func (c *Conn) installNativeRoute(*Transport, net.Addr, packetInfo) error { return nil }

// isEndpointFault has no native endpoint to report here.
func isEndpointFault(error) bool { return false }
