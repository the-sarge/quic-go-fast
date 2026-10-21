//go:build linux || freebsd

package quic

import (
	"testing"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
)

// Read slots are a Darwin opt-in (#857): elsewhere a declaring adapter keeps
// the platform batch of pool slots.
func TestReadSlotsDeclarationIgnoredOffDarwin(t *testing.T) {
	slots, slotLen, deliveredCap := readThroughSlotDeclaringConn(t, &slotDeclaringConn{UDPConn: newUDPConnLocalhost(t), slots: 4})
	require.Equal(t, batchSize, slots)
	require.Equal(t, protocol.MaxPacketBufferSize, slotLen)
	require.Equal(t, protocol.MaxPacketBufferSize, deliveredCap)
}
