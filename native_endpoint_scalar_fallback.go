//go:build unix && (!linux || !amd64)

package quic

import "net"

func newEndpointNativeIO(socket *net.UDPConn) endpointNativeIO { return socket }
