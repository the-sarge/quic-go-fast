//go:build darwin && (ios || quic_go_no_private_syscalls)

package quic

// Stub half of the sendmsg_x active/stub pair: iOS builds and builds with
// the quic_go_no_private_syscalls opt-out tag must not contain the private
// syscall path at compile time, so this file defines the same sconn symbols
// as send_conn_sendmsg_x_darwin.go and always declines native submission.
// Explicit external callbacks remain available through the ordinary writer.
// Common dispatch references the native methods, requiring an active or stub
// implementation; Go rejects duplicate methods if their build tags overlap.

import "github.com/quic-go/quic-go/internal/protocol"

func (c *sconn) nativeBatchSendAvailable() bool { return false }

func (c *sconn) sendNativeBatch([][]byte, protocol.ECN) (int, error) { return 0, nil }
