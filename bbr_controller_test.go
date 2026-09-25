package quic

import (
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

func TestBBRTransportFeedbackToAllowance(t *testing.T) {
	c := newEmissionTestConnection(t, false).conn
	now := monotime.Now()
	b := c.emission.enableBBR(now)
	require.EqualValues(t, 12000, b.GetCongestionWindow())
	q := c.emission.queue.(*sendQueue)
	defer func() {
		for len(q.queue) > 0 {
			(<-q.queue).release()
		}
	}()
	seenDown, seenUp := false, false
	for range 40 {
		require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: make([]byte, 1100)}))
		pn, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
		r := c.triggerSending(now)
		require.NoError(t, r.err)
		require.True(t, r.progress)
		entry := <-q.queue
		length := entry.buf.Len()
		require.Positive(t, length)
		entry.release()
		_, err := c.sentPacketHandler.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: pn, Largest: pn}}}, protocol.Encryption1RTT, now.Add(100*time.Millisecond))
		require.NoError(t, err)
		// With one fixed-size packet acknowledged every 100ms, the real
		// sampler's capacity sample is ten times the packet byte length.
		bandwidth := uint64(length) * 10
		seenDown = seenDown || b.PacingRate() == bandwidth*9/10
		seenUp = seenUp || b.PacingRate() == bandwidth*5/4
		now = now.Add(100 * time.Millisecond)
		if seenDown && b.PacingRate() == bandwidth*5/4 {
			break
		}
	}
	require.False(t, b.InSlowStart(), "real DATAGRAM registration and ACK samples leave Startup")
	require.True(t, seenDown, "real recovery reaches Probe Down")
	require.True(t, seenUp, "real recovery completes refill and enters Probe Up")
	require.Positive(t, b.PacingRate())
	require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: make([]byte, 1100)}))
	require.True(t, c.triggerSending(now).progress)
	require.Equal(t, max(uint64(1), b.PacingRate()*99/100), c.emission.bbr.rate, "one margin, no Reno gain")
	entry := <-q.queue
	entry.release()
	probeRate, probeWindow := b.PacingRate(), b.GetCongestionWindow()
	// Leave that packet outstanding, then ACK a newer registration after the
	// loss delay. Recovery, rather than a hand-built event, supplies real loss.
	now = now.Add(100 * time.Millisecond)
	newest, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
	require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: make([]byte, 1100)}))
	require.True(t, c.triggerSending(now).progress)
	entry = <-q.queue
	entry.release()
	_, err := c.sentPacketHandler.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: newest, Largest: newest}}}, protocol.Encryption1RTT, now.Add(100*time.Millisecond))
	require.NoError(t, err)
	require.True(t, b.InRecovery(), "real recovery loss reaches the BBR owner")
	require.Less(t, b.PacingRate(), probeRate, "probe loss reduces the rate published to emission")
	require.LessOrEqual(t, b.GetCongestionWindow(), probeWindow, "real sampler loss accounting applies the probe bound")
	// Refresh the real emission allowance after the ACK-side model reduction.
	now = now.Add(200 * time.Millisecond)
	require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: make([]byte, 1100)}))
	require.True(t, c.triggerSending(now).progress)
	require.Equal(t, max(uint64(1), b.PacingRate()*99/100), c.emission.bbr.rate)
	entry = <-q.queue
	entry.release()
}
