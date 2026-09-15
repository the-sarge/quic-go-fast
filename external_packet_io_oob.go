//go:build darwin || linux || freebsd

package quic

import (
	"net"

	"github.com/quic-go/quic-go/internal/protocol"
)

func appendExternalECN(oob []byte, addr *net.UDPAddr, ecn protocol.ECN) []byte {
	if ecn == protocol.ECNUnsupported {
		return oob
	}
	if addr.IP.To4() != nil {
		return appendIPv4ECNMsg(oob, ecn)
	}
	return appendIPv6ECNMsg(oob, ecn)
}
