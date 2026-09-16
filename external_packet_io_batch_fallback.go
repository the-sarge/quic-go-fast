//go:build !darwin || ios || quic_go_no_private_syscalls

package quic

import "net"

func newUDPBatchWriter(conn udpMessageWriter) func([][]byte, []byte, *net.UDPAddr) (int, error) {
	return ordinaryUDPBatchWriter(conn)
}
