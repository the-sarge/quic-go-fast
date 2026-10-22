package congestion

import (
	"fmt"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"

	"github.com/stretchr/testify/require"
)

func TestRecoveryEligibleFrontier(t *testing.T) {
	for _, path := range []bool{false, true} {
		for _, before := range []bool{false, true} {
			t.Run(fmt.Sprintf("path=%t/before=%t", path, before), func(t *testing.T) {
				c := NewCubicSender(DefaultClock{}, utils.NewRTTStats(), &utils.ConnectionStats{}, 1200, true, nil)
				now := monotime.Time(time.Second)
				excluded := NewPacketKey(protocol.Encryption1RTT, 0)
				eligible := NewPacketKey(protocol.EncryptionHandshake, 0)
				if before {
					c.OnPacketSent(now, 0, excluded, 1200, path, path)
				}
				c.OnPacketSent(now, 0, eligible, 1200, true, false)
				if !before {
					c.OnPacketSent(now, 0, excluded, 1200, path, path)
				}
				c.OnCongestionEvent(eligible, 1200, 1200)
				require.Equal(t, protocol.ByteCount(26880), c.GetCongestionWindow())
				require.False(t, c.InRecovery(), "a cut before the first ACK does not report recovery")
				// A later eligible send must not move the old boundary on a suppressed event.
				c.OnPacketSent(now, 0, NewPacketKey(protocol.EncryptionInitial, 0), 1200, true, false)
				c.OnCongestionEvent(eligible, 1200, 1200)
				require.Equal(t, protocol.ByteCount(26880), c.GetCongestionWindow())
				c.OnCongestionEvent(excluded, 0, 1200)
				want := protocol.ByteCount(26880)
				if !before {
					want = 18816
				}
				require.Equal(t, want, c.GetCongestionWindow())
			})
		}
	}
}

func TestRecoveryACKState(t *testing.T) {
	c := newTestCubicSender(false).sender
	now := monotime.Time(time.Second)
	old := NewPacketKey(protocol.EncryptionHandshake, 0)
	c.OnPacketSent(now, 0, old, 1200, true, false)
	c.OnCongestionEvent(old, 1200, 1200)
	require.False(t, c.InRecovery())
	c.OnPacketAcked(old, 1200, 0, now)
	require.True(t, c.InRecovery(), "equality remains inside the frozen flight")
	newer := NewPacketKey(protocol.EncryptionInitial, 0)
	c.OnPacketSent(now, 0, newer, 1200, true, false)
	c.OnPacketAcked(newer, 1200, 0, now)
	require.False(t, c.InRecovery(), "an absent space is after the prefix")
	c.OnPacketAcked(old, 1200, 0, now)
	require.False(t, c.InRecovery(), "an old ACK cannot reactivate recovery")
}

func TestApplicationPacketIdentity(t *testing.T) {
	var prefix packetPrefix
	prefix.note(NewPacketKey(protocol.Encryption0RTT, 3))
	require.False(t, prefix.after(NewPacketKey(protocol.Encryption1RTT, 3)))
	require.True(t, prefix.after(NewPacketKey(protocol.Encryption1RTT, 4)))
	require.True(t, prefix.after(NewPacketKey(protocol.EncryptionHandshake, 0)))
}

func TestHybridSlowStartInterleavedSpaces(t *testing.T) {
	c := newTestCubicSender(false).sender
	now := monotime.Time(time.Second)
	for i := range 8 {
		c.OnPacketSent(now, 0, NewPacketKey(protocol.EncryptionHandshake, protocol.PacketNumber(i)), 1200, true, false)
		if i == 3 {
			c.OnPacketSent(now, 0, NewPacketKey(protocol.Encryption1RTT, 0), 1200, true, false)
		}
	}
	c.OnPacketSent(now, 0, NewPacketKey(protocol.Encryption1RTT, 1), 1200, true, false)
	s := &c.hybridSlowStart
	for i := range 8 {
		require.Equal(t, i == 7, s.ShouldExitSlowStart(80*time.Millisecond, 60*time.Millisecond, 32))
		c.OnPacketAcked(NewPacketKey(protocol.EncryptionHandshake, protocol.PacketNumber(i)), 1200, 0, now)
		require.True(t, s.Started())
	}
	require.True(t, s.ShouldExitSlowStart(80*time.Millisecond, 60*time.Millisecond, 32), "eight samples survive interleaved old-prefix ACKs")
	// Start a fresh detection episode and establish another round.
	s.Restart()
	require.False(t, s.ShouldExitSlowStart(66*time.Millisecond, 60*time.Millisecond, 32))
	c.OnPacketSent(now, 0, NewPacketKey(protocol.Encryption1RTT, 2), 1200, true, false)
	c.OnPacketAcked(NewPacketKey(protocol.Encryption1RTT, 2), 1200, 0, now)
	require.False(t, s.Started())
}

func TestPathRegistrationPreservesPacing(t *testing.T) {
	c := newTestCubicSender(false).sender
	now := monotime.Time(time.Second)
	deadline := c.TimeUntilSend(0)
	require.True(t, c.HasPacingBudget(now))
	c.OnPacketSent(now, 0, NewPacketKey(protocol.Encryption1RTT, 0), 100000, true, true)
	require.True(t, c.HasPacingBudget(now))
	require.Equal(t, deadline, c.TimeUntilSend(0))
}
