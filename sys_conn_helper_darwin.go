//go:build darwin

package quic

import (
	"encoding/binary"
	"net/netip"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	msgTypeIPTOS = unix.IP_RECVTOS
	ipv4PKTINFO  = unix.IP_RECVPKTINFO
)

const ecnIPv4DataLen = 4

// batchSize (the receive batch ceiling) is defined by the recvmsg_x
// active/stub pair: the D2 receive-batching path raises it behind the
// fail-closed capability, and the ios/opt-out stub keeps the historical
// single-packet read (x/net ReadBatch returns a single packet on darwin,
// see https://godoc.org/golang.org/x/net/ipv4#PacketConn.ReadBatch).

// Exactly one of the sendmsg_x active/stub file pair
// (send_conn_sendmsg_x_darwin.go / send_conn_sendmsg_x_stub_darwin.go) must
// compile for every darwin tag set. This assertion breaks the build if the
// pair ever leaves a gap, and the compiler rejects the duplicate definitions
// if they overlap — the terminating mechanism for the compile-time-absence
// claim of the iOS and opt-out builds.
var _ batchSender = (*sconn)(nil)

func parseIPv4PktInfo(body []byte) (ip netip.Addr, ifIndex uint32, ok bool) {
	// struct in_pktinfo {
	// 	unsigned int   ipi_ifindex;  /* Interface index */
	// 	struct in_addr ipi_spec_dst; /* Local address */
	// 	struct in_addr ipi_addr;     /* Header Destination address */
	// };
	if len(body) != 12 {
		return netip.Addr{}, 0, false
	}
	return netip.AddrFrom4(*(*[4]byte)(body[8:12])), binary.NativeEndian.Uint32(body), true
}

func isGSOEnabled(syscall.RawConn) bool { return false }

func isECNEnabled() bool { return !isECNDisabledUsingEnv() }

// GRO is a Linux/Windows receive offload; no Darwin equivalent is adopted.
func isGROEnabled(syscall.RawConn) bool { return false }

func parseUDPGROSegmentSize(*unix.Cmsghdr, []byte) (segmentSize int, ok bool) { return 0, false }
