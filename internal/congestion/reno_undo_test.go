package congestion

import (
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/stretchr/testify/require"
)

func newRenoUndoSender() *cubicSender {
	return NewCubicSender(DefaultClock{}, utils.NewRTTStats(), &utils.ConnectionStats{}, 1200, true, nil)
}

func undoKey(pn protocol.PacketNumber) PacketKey { return NewPacketKey(protocol.Encryption1RTT, pn) }

func undoSend(c *cubicSender, pn protocol.PacketNumber) {
	c.OnPacketSent(monotime.Time(time.Second), 0, undoKey(pn), 1200, true, false)
}

func TestRenoUndoPriorRecoveryExit(t *testing.T) {
	c := newRenoUndoSender()
	undoSend(c, 0)
	c.OnCongestionEvent(undoKey(0), 1200, 1200)
	undoSend(c, 1)
	c.OnCongestionEvent(undoKey(1), 1200, 1200)
	require.EqualValues(t, 18816, c.GetCongestionWindow())
	c.OnPacketAcked(undoKey(1), 1200, 0, 0)
	require.True(t, c.InRecovery(), "ACK equals the current boundary")
	c.OnSpuriousLoss(undoKey(1))
	require.EqualValues(t, 26880, c.GetCongestionWindow())
	require.False(t, c.InRecovery(), "the eligible ACK already crossed the restored prior boundary")
	c.OnPacketAcked(undoKey(0), 1200, 0, 0)
	require.False(t, c.InRecovery(), "old ACK cannot reactivate recovery")
	// The restored prefix admits a later loss from the undone flight again.
	c.OnCongestionEvent(undoKey(1), 1200, 1200)
	require.EqualValues(t, 18816, c.GetCongestionWindow())
}

func TestRenoUndoSavedAvoidanceState(t *testing.T) {
	c := newRenoUndoSender()
	undoSend(c, 0)
	c.OnCongestionEvent(undoKey(0), 1200, 1200)
	// Three congestion-avoidance ACKs accrue credit before the next cut.
	for pn := protocol.PacketNumber(1); pn <= 3; pn++ {
		undoSend(c, pn)
		c.OnPacketAcked(undoKey(pn), 1200, 26880, 0)
	}
	require.False(t, c.InSlowStart())
	undoSend(c, 4)
	c.OnCongestionEvent(undoKey(4), 1200, 1200)
	c.OnSpuriousLoss(undoKey(4))
	require.EqualValues(t, 26880, c.GetCongestionWindow())
	require.False(t, c.InSlowStart(), "restore the saved avoidance threshold")
	// The 22-packet window needs only 19 more ACKs after the saved three.
	for pn := protocol.PacketNumber(5); pn <= 22; pn++ {
		undoSend(c, pn)
		c.OnPacketAcked(undoKey(pn), 1200, 26880, 0)
	}
	require.EqualValues(t, 26880, c.GetCongestionWindow())
	undoSend(c, 23)
	c.OnPacketAcked(undoKey(23), 1200, 26880, 0)
	require.EqualValues(t, 28080, c.GetCongestionWindow(), "restore additive-increase ACK credit")
}

func TestRenoUndoAfterRecovery(t *testing.T) {
	for _, grown := range []bool{false, true} {
		t.Run(map[bool]string{false: "late", true: "already_grown"}[grown], func(t *testing.T) {
			c := newRenoUndoSender()
			undoSend(c, 0)
			c.OnCongestionEvent(undoKey(0), 1200, 1200)
			undoSend(c, 1)
			c.OnPacketAcked(undoKey(1), 1200, 0, 0)
			require.False(t, c.InRecovery())
			if grown {
				for pn := protocol.PacketNumber(2); c.GetCongestionWindow() <= 38400; pn++ {
					undoSend(c, pn)
					c.OnPacketAcked(undoKey(pn), 1200, c.GetCongestionWindow(), 0)
				}
				require.EqualValues(t, 38880, c.GetCongestionWindow())
			}
			c.OnSpuriousLoss(undoKey(0))
			want := protocol.ByteCount(38400)
			if grown {
				want = 38880
			}
			require.Equal(t, want, c.GetCongestionWindow())
			require.False(t, c.InRecovery())
			c.OnPacketAcked(undoKey(0), 1200, 0, 0)
			require.False(t, c.InRecovery())
		})
	}
}

func TestRenoUndoSuppressedOldCE(t *testing.T) {
	c := newRenoUndoSender()
	undoSend(c, 0)
	c.OnCongestionEvent(undoKey(0), 1200, 1200)
	undoSend(c, 1)
	c.OnCongestionEvent(undoKey(1), 1200, 1200)
	// A validated CE fact is not a loss receipt, even when its anchor is
	// older than the saved prior prefix and its additional cut is suppressed.
	c.OnCongestionEvent(undoKey(0), 0, 0)
	c.OnSpuriousLoss(undoKey(1))
	require.EqualValues(t, 18816, c.GetCongestionWindow())
}
