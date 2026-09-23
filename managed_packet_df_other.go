//go:build (!darwin && !linux) || ios

package quic

import "net"

// Platforms without native DF lifecycle qualification retain ordinary sends.
func managedDFControlFor(net.PacketConn) managedDFControl { return nil }
