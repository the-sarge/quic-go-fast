//go:build freebsd

package quic

import (
	"net/netip"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	msgTypeIPTOS = unix.IP_RECVTOS
	ipv4PKTINFO  = 0x7
)

const ecnIPv4DataLen = 1

const batchSize = 8

// receiveBatchSize reports how many messages the shared read loop fills per
// ReadBatch call; freebsd always uses the full compile-time batch.
func receiveBatchSize() int { return batchSize }

// wrapReadBatchConn is the identity on freebsd: no platform receive-batching
// wrapper exists.
func wrapReadBatchConn(bc batchConn, _ syscall.RawConn) batchConn { return bc }

func parseIPv4PktInfo(body []byte) (ip netip.Addr, _ uint32, ok bool) {
	// struct in_pktinfo {
	// 	struct in_addr ipi_addr;     /* Header Destination address */
	// };
	if len(body) != 4 {
		return netip.Addr{}, 0, false
	}
	return netip.AddrFrom4(*(*[4]byte)(body)), 0, true
}

func isGSOEnabled(syscall.RawConn) bool { return false }

func isECNEnabled() bool { return !isECNDisabledUsingEnv() }

// GRO is a Linux/Windows receive offload; no FreeBSD equivalent is adopted.
func isGROEnabled(syscall.RawConn) bool { return false }

func parseUDPGROSegmentSize(*unix.Cmsghdr, []byte) (segmentSize int, ok bool) { return 0, false }
