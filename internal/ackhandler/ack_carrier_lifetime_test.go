package ackhandler

import (
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

func TestACKCarrierResetDuringCallback(t *testing.T) {
	now := monotime.Time(time.Second)
	var carriers ackCarrierHistory
	var targets ackTargets
	targets.add(0, 12)
	carriers.add(3, now.Add(time.Second), targets, nil)
	var seen []protocol.PacketNumber
	carriers.acknowledge(&wire.AckFrame{AckRanges: ackRanges(3)}, now, func(_ protocol.PathID, below protocol.PacketNumber) {
		seen = append(seen, below)
		carriers.reset()
		carriers.add(3, now.Add(time.Second), targets, nil)
	})
	require.Equal(t, []protocol.PacketNumber{12}, seen)
	require.Len(t, carriers.carriers, 1, "an identical replacement belongs to the new population")
	carriers.acknowledge(&wire.AckFrame{AckRanges: ackRanges(3)}, now, func(_ protocol.PathID, below protocol.PacketNumber) { seen = append(seen, below) })
	require.Equal(t, []protocol.PacketNumber{12, 12}, seen)
	require.Empty(t, carriers.carriers)
}
