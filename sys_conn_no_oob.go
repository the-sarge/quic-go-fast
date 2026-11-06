//go:build !darwin && !linux && !freebsd && !windows

package quic

import (
	"net"
	"net/netip"
)

func newConn(c net.PacketConn, supportsDF, _ bool) (*basicConn, error) {
	return newConnWithPacketCeiling(c, supportsDF, false, 0)
}

func newConnWithPacketCeiling(c net.PacketConn, supportsDF, _ bool, ceiling packetCeiling) (*basicConn, error) {
	return &basicConn{PacketConn: c, supportsDF: supportsDF, packetCeiling: ceiling}, nil
}

type packetInfo struct {
	addr netip.Addr
}

func (i *packetInfo) OOB() []byte { return nil }
