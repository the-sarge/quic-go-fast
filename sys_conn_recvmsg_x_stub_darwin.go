//go:build darwin && (ios || quic_go_no_private_syscalls)

package quic

// Fallback stub for darwin builds that must never invoke the private
// recvmsg_x syscall: iOS, and distributors opting out with
// -tags quic_go_no_private_syscalls. It preserves today's single-packet
// read behavior exactly. This file defines the same symbols as the active
// sys_conn_recvmsg_x_darwin.go, so exactly one of the pair compiles for
// every darwin tag set — the same terminating mechanism the sendmsg_x pair
// uses for its compile-time-absence claim.

import "syscall"

// ReadBatch only returns a single packet on OSX,
// see https://godoc.org/golang.org/x/net/ipv4#PacketConn.ReadBatch.
const batchSize = 1

func receiveBatchSize() int { return batchSize }

// wrapReadBatchConn is the identity on stub builds: no batching wrapper
// exists, and the read path is byte-identical to the historical one.
func wrapReadBatchConn(bc batchConn, _ syscall.RawConn) batchConn { return bc }
