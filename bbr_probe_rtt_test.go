package quic

import (
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

func TestBBRProbeRTTIdleAndResume(t *testing.T) {
	for _, reason := range []string{"application", "pending worker", "flow control"} {
		t.Run(reason, func(t *testing.T) {
			c := newEmissionTestConnection(t, false).conn
			now := monotime.Now()
			b := c.emission.enableBBR(now)
			q := c.emission.queue.(*sendQueue)
			defer func() {
				for len(q.queue) > 0 {
					(<-q.queue).release()
				}
			}()
			send := func(delay time.Duration) queueEntry {
				t.Helper()
				require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: make([]byte, 1100)}))
				pn, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
				result := c.triggerSending(now)
				require.NoError(t, result.err)
				require.True(t, result.progress)
				entry := <-q.queue
				now = now.Add(delay)
				_, err := c.sentPacketHandler.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: pn, Largest: pn}}}, protocol.Encryption1RTT, now)
				require.NoError(t, err)
				return entry
			}
			for range 6 {
				entry := send(100 * time.Millisecond)
				entry.release()
			}
			entry := send(100 * time.Millisecond)
			if reason == "pending worker" {
				pendingEntry := entry
				defer pendingEntry.release()
			} else {
				entry.release()
			}
			require.NoError(t, c.triggerSending(now).err, "ordinary empty packing observes exhaustion")
			if reason == "flow control" {
				c.sentPacketHandler.(interface {
					ObserveDeliveryLimitation(congestion.SendLimitation)
				}).ObserveDeliveryLimitation(congestion.SendFlowControlLimited)
			}
			stats := c.sentPacketHandler.(interface {
				DeliveryStats() congestion.DeliveryStats
			}).DeliveryStats()
			require.Equal(t, reason == "application", stats.Idle)
			now = now.Add(11 * time.Second)
			entry = send(200 * time.Millisecond)
			entry.release()
			require.Equal(t, reason != "application", b.InProbeRTT(), "only genuine idle suppresses expired ProbeRTT entry")
			require.LessOrEqual(t, c.emission.bbr.budget(now), c.emission.bbr.quantum, "resume never accumulates more than one quantum")
		})
	}
}
