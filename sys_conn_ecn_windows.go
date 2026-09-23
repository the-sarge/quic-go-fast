package quic

import (
	"encoding/binary"

	"github.com/quic-go/quic-go/internal/protocol"
	"golang.org/x/sys/windows"
)

// Winsock uses option 50 for RECVECN and message type 50 for ECN at each IP level.
const windowsECN = 50

func appendIPv4ECNMsg(oob []byte, ecn protocol.ECN) []byte {
	return appendWindowsECN(oob, windows.IPPROTO_IP, ecn)
}

func appendIPv6ECNMsg(oob []byte, ecn protocol.ECN) []byte {
	return appendWindowsECN(oob, windows.IPPROTO_IPV6, ecn)
}

func appendWindowsECN(oob []byte, level int32, ecn protocol.ECN) []byte {
	oob, data := appendCmsg(oob, level, windowsECN, 4)
	binary.NativeEndian.PutUint32(data, uint32(ecn.ToHeaderBits()))
	return oob
}
