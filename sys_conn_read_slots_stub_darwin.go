//go:build darwin && (ios || quic_go_no_private_syscalls)

package quic

// iOS builds and builds with the quic_go_no_private_syscalls tag offer a
// batch adapter Darwin's single pool slot, whatever it declares (#857).
func readSlotsFor(OOBCapablePacketConn) (int, bool, error) { return batchSize, false, nil }
