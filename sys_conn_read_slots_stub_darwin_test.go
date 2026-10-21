//go:build darwin && (ios || quic_go_no_private_syscalls)

package quic

import (
	"testing"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
)

// The quic_go_no_private_syscalls tag gives a declaring adapter one pool
// slot (#857).
func TestReadSlotsNoPrivateSyscallsTag(t *testing.T) {
	slots, slotLen, deliveredCap := readThroughSlotDeclaringConn(t, &slotDeclaringConn{UDPConn: newUDPConnLocalhost(t), slots: 8})
	require.Equal(t, 1, slots)
	require.Equal(t, protocol.MaxPacketBufferSize, slotLen)
	require.Equal(t, protocol.MaxPacketBufferSize, deliveredCap)
}
