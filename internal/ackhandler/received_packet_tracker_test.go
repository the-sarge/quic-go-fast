package ackhandler

import (
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"

	"github.com/stretchr/testify/require"
)

func TestReceivedPacketTrackerGenerateACKs(t *testing.T) {
	tracker := newReceivedPacketTracker()

	require.NoError(t, tracker.ReceivedPacket(protocol.PacketNumber(3), protocol.ECNNon, true))
	ack := tracker.GetAckFrame()
	require.NotNil(t, ack)
	require.Equal(t, []wire.AckRange{{Smallest: 3, Largest: 3}}, ack.AckRanges)
	require.Zero(t, ack.DelayTime)

	require.NoError(t, tracker.ReceivedPacket(protocol.PacketNumber(4), protocol.ECNNon, true))
	ack = tracker.GetAckFrame()
	require.NotNil(t, ack)
	require.Equal(t, []wire.AckRange{{Smallest: 3, Largest: 4}}, ack.AckRanges)
	require.Zero(t, ack.DelayTime)

	require.NoError(t, tracker.ReceivedPacket(protocol.PacketNumber(1), protocol.ECNNon, true))
	ack = tracker.GetAckFrame()
	require.NotNil(t, ack)
	require.Equal(t, []wire.AckRange{
		{Smallest: 3, Largest: 4},
		{Smallest: 1, Largest: 1},
	}, ack.AckRanges)
	require.Zero(t, ack.DelayTime)

	// non-ack-eliciting packets don't trigger ACKs
	require.NoError(t, tracker.ReceivedPacket(protocol.PacketNumber(10), protocol.ECNNon, false))
	require.Nil(t, tracker.GetAckFrame())

	require.NoError(t, tracker.ReceivedPacket(protocol.PacketNumber(11), protocol.ECNNon, true))
	ack = tracker.GetAckFrame()
	require.NotNil(t, ack)
	require.Equal(t, []wire.AckRange{
		{Smallest: 10, Largest: 11},
		{Smallest: 3, Largest: 4},
		{Smallest: 1, Largest: 1},
	}, ack.AckRanges)
}

func TestAppDataReceivedPacketTrackerECN(t *testing.T) {
	tr := newAppDataReceivedPacketTracker(utils.DefaultLogger)

	require.NoError(t, tr.ReceivedPacket(0, protocol.ECT0, monotime.Now(), true))
	pn := protocol.PacketNumber(1)
	for range 2 {
		require.NoError(t, tr.ReceivedPacket(pn, protocol.ECT1, monotime.Now(), true))
		pn++
	}
	for range 3 {
		require.NoError(t, tr.ReceivedPacket(pn, protocol.ECNCE, monotime.Now(), true))
		pn++
	}
	ack := tr.GetAckFrame(monotime.Now(), false)
	require.Equal(t, uint64(1), ack.ECT0)
	require.Equal(t, uint64(2), ack.ECT1)
	require.Equal(t, uint64(3), ack.ECNCE)
}

func TestAppDataReceivedPacketTrackerAckEverySecondPacket(t *testing.T) {
	tr := newAppDataReceivedPacketTracker(utils.DefaultLogger)
	require.Nil(t, tr.GetAckFrame(monotime.Now(), true))

	for p := protocol.PacketNumber(1); p <= 20; p++ {
		require.NoError(t, tr.ReceivedPacket(p, protocol.ECNNon, monotime.Now(), true))
		switch p % 2 {
		case 0:
			require.NotNil(t, tr.GetAckFrame(monotime.Now(), true))
		case 1:
			require.Nil(t, tr.GetAckFrame(monotime.Now(), true))
		}
	}
}

func TestAppDataReceivedPacketTrackerAlarmTimeout(t *testing.T) {
	tr := newAppDataReceivedPacketTracker(utils.DefaultLogger)

	now := monotime.Now()
	require.NoError(t, tr.ReceivedPacket(1, protocol.ECNNon, now, false))
	require.Nil(t, tr.GetAckFrame(monotime.Now(), true))
	require.Zero(t, tr.GetAlarmTimeout())

	rcvTime := now.Add(10 * time.Millisecond)
	require.NoError(t, tr.ReceivedPacket(2, protocol.ECNNon, rcvTime, true))
	require.Equal(t, rcvTime.Add(protocol.MaxAckDelay), tr.GetAlarmTimeout())
	require.Nil(t, tr.GetAckFrame(monotime.Now(), true))

	// no timeout after the ACK has been dequeued
	require.NotNil(t, tr.GetAckFrame(monotime.Now(), false))
	require.Zero(t, tr.GetAlarmTimeout())
}

func TestAppDataReceivedPacketTrackerQueuesECNCE(t *testing.T) {
	tr := newAppDataReceivedPacketTracker(utils.DefaultLogger)

	require.NoError(t, tr.ReceivedPacket(1, protocol.ECNCE, monotime.Now(), true))
	ack := tr.GetAckFrame(monotime.Now(), true)
	require.NotNil(t, ack)
	require.Equal(t, protocol.PacketNumber(1), ack.LargestAcked())
	require.EqualValues(t, 1, ack.ECNCE)
}

func TestAppDataReceivedPacketTrackerMissingPackets(t *testing.T) {
	tr := newAppDataReceivedPacketTracker(utils.DefaultLogger)

	now := monotime.Now()
	require.NoError(t, tr.ReceivedPacket(0, protocol.ECNNon, now, true))
	require.Nil(t, tr.GetAckFrame(now, true))

	require.NoError(t, tr.ReceivedPacket(5, protocol.ECNNon, now, true))
	ack := tr.GetAckFrame(now, true) // ACK: 0 and 5, missing: 1, 2, 3, 4
	require.NotNil(t, ack)
	require.Equal(t, []wire.AckRange{{Smallest: 5, Largest: 5}, {Smallest: 0, Largest: 0}}, ack.AckRanges)

	// now receive one of the missing packets
	require.NoError(t, tr.ReceivedPacket(3, protocol.ECNNon, now, true))
	ack = tr.GetAckFrame(now, true)
	require.NotNil(t, ack)
	require.Equal(t, []wire.AckRange{
		{Smallest: 5, Largest: 5},
		{Smallest: 3, Largest: 3},
		{Smallest: 0, Largest: 0},
	}, ack.AckRanges)

	require.NoError(t, tr.ReceivedPacket(6, protocol.ECNNon, now, true))
	require.Nil(t, tr.GetAckFrame(now, true))
	require.NoError(t, tr.ReceivedPacket(8, protocol.ECNNon, now, true))
	require.NotNil(t, tr.GetAckFrame(now, true))
}

func TestAppDataReceivedPacketTrackerDelayTime(t *testing.T) {
	tr := newAppDataReceivedPacketTracker(utils.DefaultLogger)

	now := monotime.Now()
	require.NoError(t, tr.ReceivedPacket(1, protocol.ECNNon, now, true))
	require.NoError(t, tr.ReceivedPacket(2, protocol.ECNNon, now.Add(-1337*time.Millisecond), true))
	ack := tr.GetAckFrame(now, true)
	require.NotNil(t, ack)
	require.Equal(t, 1337*time.Millisecond, ack.DelayTime)

	// don't use a negative delay time
	require.NoError(t, tr.ReceivedPacket(3, protocol.ECNNon, now.Add(time.Hour), true))
	ack = tr.GetAckFrame(now, false)
	require.NotNil(t, ack)
	require.Zero(t, ack.DelayTime)
}

func TestAppDataReceivedPacketTrackerIgnoreBelow(t *testing.T) {
	tr := newAppDataReceivedPacketTracker(utils.DefaultLogger)

	tr.IgnoreBelow(4)
	// check that packets below 7 are considered duplicates
	require.True(t, tr.IsPotentiallyDuplicate(3))
	require.False(t, tr.IsPotentiallyDuplicate(4))

	for i := 5; i <= 10; i++ {
		require.NoError(t, tr.ReceivedPacket(protocol.PacketNumber(i), protocol.ECNNon, monotime.Now(), true))
	}
	ack := tr.GetAckFrame(monotime.Now(), true)
	require.NotNil(t, ack)
	require.Equal(t, []wire.AckRange{{Smallest: 5, Largest: 10}}, ack.AckRanges)

	tr.IgnoreBelow(7)

	require.NoError(t, tr.ReceivedPacket(11, protocol.ECNNon, monotime.Now(), true))
	require.NoError(t, tr.ReceivedPacket(12, protocol.ECNNon, monotime.Now(), true))
	ack = tr.GetAckFrame(monotime.Now(), true)
	require.NotNil(t, ack)
	require.Equal(t, []wire.AckRange{{Smallest: 7, Largest: 12}}, ack.AckRanges)

	// make sure that old packets are not accepted
	require.ErrorContains(t,
		tr.ReceivedPacket(4, protocol.ECNNon, monotime.Now(), true),
		"receivedPacketTracker BUG: ReceivedPacket called for old / duplicate packet 4",
	)
}

func newThresholdTracker(t *testing.T, threshold int) *appDataReceivedPacketTracker {
	t.Helper()
	h := NewReceivedPacketHandler(utils.DefaultLogger)
	h.SetAppDataAckThreshold(threshold)
	require.Equal(t, threshold, h.AppDataAckThreshold())
	return &h.appDataPackets
}

func TestAppDataReceivedPacketTrackerAckThreshold(t *testing.T) {
	tr := newThresholdTracker(t, 16)

	// Packets arriving within the group cap are acknowledged every 16.
	now := monotime.Now()
	for p := protocol.PacketNumber(1); p <= 48; p++ {
		require.NoError(t, tr.ReceivedPacket(p, protocol.ECNNon, now, true))
		if p%16 == 0 {
			ack := tr.GetAckFrame(now, true)
			require.NotNil(t, ack, "packet %d", p)
			require.Equal(t, []wire.AckRange{{Smallest: 1, Largest: p}}, ack.AckRanges)
			continue
		}
		require.Nil(t, tr.GetAckFrame(now, true), "packet %d", p)
	}
}

func TestAppDataReceivedPacketTrackerAckThresholdTimedFromOldest(t *testing.T) {
	tr := newThresholdTracker(t, 16)

	t0 := monotime.Now()
	require.NoError(t, tr.ReceivedPacket(1, protocol.ECNNon, t0, true))
	require.Equal(t, t0.Add(protocol.MaxAckDelay), tr.GetAlarmTimeout())
	// Later packets never restart the timer: it runs from the oldest
	// unacknowledged packet, and a group of two or more waits at most 1 ms.
	require.NoError(t, tr.ReceivedPacket(2, protocol.ECNNon, t0.Add(300*time.Microsecond), true))
	require.Equal(t, t0.Add(time.Millisecond), tr.GetAlarmTimeout())
	require.NoError(t, tr.ReceivedPacket(3, protocol.ECNNon, t0.Add(600*time.Microsecond), true))
	require.Equal(t, t0.Add(time.Millisecond), tr.GetAlarmTimeout())
	require.NoError(t, tr.ReceivedPacket(4, protocol.ECNNon, t0.Add(999*time.Microsecond), false))
	require.Equal(t, t0.Add(time.Millisecond), tr.GetAlarmTimeout())

	require.Nil(t, tr.GetAckFrame(t0.Add(999*time.Microsecond), true))
	ack := tr.GetAckFrame(t0.Add(time.Millisecond), true)
	require.NotNil(t, ack)
	require.Equal(t, []wire.AckRange{{Smallest: 1, Largest: 4}}, ack.AckRanges)
	require.Zero(t, tr.GetAlarmTimeout())

	// The next packet starts a new group.
	t1 := t0.Add(5 * time.Millisecond)
	require.NoError(t, tr.ReceivedPacket(5, protocol.ECNNon, t1, true))
	require.Equal(t, t1.Add(protocol.MaxAckDelay), tr.GetAlarmTimeout())
	require.NoError(t, tr.ReceivedPacket(6, protocol.ECNNon, t1.Add(100*time.Microsecond), true))
	require.Equal(t, t1.Add(time.Millisecond), tr.GetAlarmTimeout())
}

// Buffered 0-RTT and undecryptable packets are processed after packets
// received later; the timer still runs from the earliest receive time.
func TestAppDataReceivedPacketTrackerAckThresholdOldestByReceiveTime(t *testing.T) {
	tr := newThresholdTracker(t, 16)

	t0 := monotime.Now()
	require.NoError(t, tr.ReceivedPacket(2, protocol.ECNNon, t0.Add(500*time.Microsecond), true))
	require.Equal(t, t0.Add(500*time.Microsecond+protocol.MaxAckDelay), tr.GetAlarmTimeout())
	require.NoError(t, tr.ReceivedPacket(1, protocol.ECNNon, t0, true))
	require.Equal(t, t0.Add(time.Millisecond), tr.GetAlarmTimeout())
	require.Nil(t, tr.GetAckFrame(t0.Add(999*time.Microsecond), true))
	require.NotNil(t, tr.GetAckFrame(t0.Add(time.Millisecond), true))

	// A lone packet received late doesn't make a later packet the oldest.
	t1 := t0.Add(10 * time.Millisecond)
	require.NoError(t, tr.ReceivedPacket(3, protocol.ECNNon, t1, true))
	require.NoError(t, tr.ReceivedPacket(4, protocol.ECNNon, t1.Add(200*time.Microsecond), true))
	require.Equal(t, t1.Add(time.Millisecond), tr.GetAlarmTimeout())
}

func TestAppDataReceivedPacketTrackerAckThresholdGroupCap(t *testing.T) {
	tr := newThresholdTracker(t, 16)

	// A second packet arriving 1 ms or more after the oldest is acknowledged
	// at once, as the default rule would.
	t0 := monotime.Now()
	require.NoError(t, tr.ReceivedPacket(1, protocol.ECNNon, t0, true))
	require.NoError(t, tr.ReceivedPacket(2, protocol.ECNNon, t0.Add(time.Millisecond), true))
	require.Zero(t, tr.GetAlarmTimeout())
	ack := tr.GetAckFrame(t0.Add(time.Millisecond), true)
	require.NotNil(t, ack)
	require.Equal(t, []wire.AckRange{{Smallest: 1, Largest: 2}}, ack.AckRanges)

	t1 := t0.Add(10 * time.Millisecond)
	require.NoError(t, tr.ReceivedPacket(3, protocol.ECNNon, t1, true))
	require.NoError(t, tr.ReceivedPacket(4, protocol.ECNNon, t1.Add(5*time.Millisecond), true))
	require.NotNil(t, tr.GetAckFrame(t1.Add(5*time.Millisecond), true))
}

func TestAppDataReceivedPacketTrackerAckThresholdLonePacket(t *testing.T) {
	tr := newThresholdTracker(t, 16)

	t0 := monotime.Now()
	require.NoError(t, tr.ReceivedPacket(1, protocol.ECNNon, t0, true))
	require.Equal(t, t0.Add(protocol.MaxAckDelay), tr.GetAlarmTimeout())
	require.Nil(t, tr.GetAckFrame(t0.Add(protocol.MaxAckDelay-time.Nanosecond), true))
	ack := tr.GetAckFrame(t0.Add(protocol.MaxAckDelay), true)
	require.NotNil(t, ack)
	require.Equal(t, []wire.AckRange{{Smallest: 1, Largest: 1}}, ack.AckRanges)
	require.Zero(t, tr.GetAlarmTimeout())
}

func TestAppDataReceivedPacketTrackerAckThresholdImmediateTriggers(t *testing.T) {
	tr := newThresholdTracker(t, 16)

	now := monotime.Now()
	// ECN-CE
	require.NoError(t, tr.ReceivedPacket(0, protocol.ECNCE, now, true))
	require.NotNil(t, tr.GetAckFrame(now, true))

	// a new gap
	require.NoError(t, tr.ReceivedPacket(5, protocol.ECNNon, now, true))
	ack := tr.GetAckFrame(now, true) // ACK: 0 and 5, missing: 1, 2, 3, 4
	require.NotNil(t, ack)
	require.Equal(t, []wire.AckRange{{Smallest: 5, Largest: 5}, {Smallest: 0, Largest: 0}}, ack.AckRanges)

	// a packet reported missing before
	require.NoError(t, tr.ReceivedPacket(3, protocol.ECNNon, now, true))
	ack = tr.GetAckFrame(now, true)
	require.NotNil(t, ack)
	require.Equal(t, []wire.AckRange{
		{Smallest: 5, Largest: 5},
		{Smallest: 3, Largest: 3},
		{Smallest: 0, Largest: 0},
	}, ack.AckRanges)

	// in order again: wait for the threshold or the timer
	require.NoError(t, tr.ReceivedPacket(6, protocol.ECNNon, now, true))
	require.NoError(t, tr.ReceivedPacket(7, protocol.ECNNon, now, true))
	require.Nil(t, tr.GetAckFrame(now, true))
}

func TestAppDataReceivedPacketTrackerDefaultThreshold(t *testing.T) {
	h := NewReceivedPacketHandler(utils.DefaultLogger)
	require.Zero(t, h.AppDataAckThreshold())
	tr := &h.appDataPackets

	// Without the mode, every second packet queues an ACK, however close
	// together they arrive, and the timer runs from the latest packet.
	t0 := monotime.Now()
	require.NoError(t, tr.ReceivedPacket(1, protocol.ECNNon, t0, true))
	require.Equal(t, t0.Add(protocol.MaxAckDelay), tr.GetAlarmTimeout())
	require.NoError(t, tr.ReceivedPacket(2, protocol.ECNNon, t0.Add(time.Microsecond), true))
	require.Zero(t, tr.GetAlarmTimeout())
	require.NotNil(t, tr.GetAckFrame(t0.Add(time.Microsecond), true))
	require.NoError(t, tr.ReceivedPacket(3, protocol.ECNNon, t0.Add(3*time.Millisecond), true))
	require.Equal(t, t0.Add(3*time.Millisecond+protocol.MaxAckDelay), tr.GetAlarmTimeout())
}
