//go:build darwin && (ios || quic_go_no_private_syscalls)

package quic

// Stub half of the sendmsg_x active/stub pair: iOS builds and builds with
// the quic_go_no_private_syscalls opt-out tag must not contain the private
// syscall path at compile time, so this file defines the same sconn symbols
// as send_conn_sendmsg_x_darwin.go and always declines, keeping today's
// per-packet send behavior. The batchSender assertion in
// sys_conn_helper_darwin.go guarantees exactly one of the pair compiles for
// every darwin tag set.

import "github.com/quic-go/quic-go/internal/protocol"

func (c *sconn) batchSendAvailable() bool { return false }

func (c *sconn) sendBatch([][]byte, protocol.ECN) int { return 0 }
