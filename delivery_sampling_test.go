package quic

import (
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type deliveryTestSink struct {
	sends  []congestion.SendEvent
	events []congestion.FeedbackEvent
}

func (s *deliveryTestSink) Sent(e congestion.SendEvent) { s.sends = append(s.sends, e) }
func (s *deliveryTestSink) Feedback(e congestion.FeedbackEvent) {
	e.Acked = slices.Clone(e.Acked)
	e.Lost = slices.Clone(e.Lost)
	s.events = append(s.events, e)
}

func TestDeliverySamplerNoDataReasons(t *testing.T) {
	t.Run("flow-controlled stream supply", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			c := newEmissionTestConnection(t, false).conn
			now := monotime.Now()
			c.emission.bbr = newBBRSendPolicy(1_000_000, 1200, now)
			ackhandler.EnableDeliverySampling(c.sentPacketHandler, &deliveryTestSink{}, c.emission.deliveryPendingBytes)
			c.framer.enableDeliveryObservations()
			c.peerParams = &wire.TransportParameters{}
			c.streamsMap.HandleMaxStreamsFrame(&wire.MaxStreamsFrame{Type: protocol.StreamTypeUni, MaxStreamNum: 1})
			s, err := c.OpenUniStream()
			require.NoError(t, err)
			done := make(chan struct{})
			go func() { _, _ = s.Write([]byte("blocked payload")); close(done) }()
			synctest.Wait()
			t.Cleanup(func() { s.CancelWrite(0); synctest.Wait(); <-done })
			require.NoError(t, c.triggerSending(now).err)
			stats := c.sentPacketHandler.(interface {
				DeliveryStats() congestion.DeliveryStats
			}).DeliveryStats()
			require.Equal(t, congestion.SendFlowControlLimited, stats.Stop)
			require.False(t, stats.Idle)
		})
	})
	for _, reason := range []congestion.SendLimitation{congestion.SendPacingLimited, congestion.SendLocalLimited, congestion.SendCongestionLimited} {
		t.Run(map[congestion.SendLimitation]string{congestion.SendPacingLimited: "pacer", congestion.SendLocalLimited: "queue", congestion.SendCongestionLimited: "cwnd"}[reason], func(t *testing.T) {
			c := newEmissionTestConnection(t, false).conn
			now := monotime.Now()
			c.emission.bbr = newBBRSendPolicy(1_000_000, 1200, now)
			ackhandler.EnableDeliverySampling(c.sentPacketHandler, &deliveryTestSink{}, c.emission.deliveryPendingBytes)
			c.framer.enableDeliveryObservations()
			switch reason {
			case congestion.SendPacingLimited:
				c.emission.bbr.tokens = 0
			case congestion.SendLocalLimited:
				r := c.emission.bbr.credit.reserve(4800, 4800, true, false)
				defer r.complete()
			case congestion.SendCongestionLimited:
				pn := c.sentPacketHandler.PopPacketNumber(protocol.Encryption1RTT)
				c.sentPacketHandler.SentPacket(now, pn, protocol.InvalidPacketNumber, nil, []ackhandler.Frame{{Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, protocol.ECNNon, 1<<20, false, false)
			}
			require.NoError(t, c.triggerSending(now).err)
			stats := c.sentPacketHandler.(interface {
				DeliveryStats() congestion.DeliveryStats
			}).DeliveryStats()
			require.Equal(t, reason, stats.Stop)
			require.False(t, stats.Idle)
		})
	}
	for _, gso := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "GSO"}[gso], func(t *testing.T) {
			c := newEmissionTestConnection(t, gso).conn
			now := monotime.Now()
			c.emission.bbr = newBBRSendPolicy(1_000_000, 1200, now)
			sink := &deliveryTestSink{}
			ackhandler.EnableDeliverySampling(c.sentPacketHandler, sink, c.emission.deliveryPendingBytes)
			c.framer.enableDeliveryObservations()
			require.NoError(t, c.triggerSending(now).err)
			stats := c.sentPacketHandler.(interface {
				DeliveryStats() congestion.DeliveryStats
			}).DeliveryStats()
			require.Equal(t, congestion.SendApplicationLimited, stats.Stop)
			require.True(t, stats.Idle)
			require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{Data: []byte("delivery"), DataLenPresent: true}))
			require.True(t, c.triggerSending(now).progress)
			require.True(t, sink.sends[0].Packet.Delivery.Valid, "our own unfilled reservation is not prior queued work")
			require.Equal(t, congestion.SendApplicationLimited, sink.sends[0].Packet.Delivery.Limited)
			entry := <-c.emission.queue.(*sendQueue).queue
			pn := sink.sends[0].Packet.PacketNumber
			_, err := c.sentPacketHandler.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: pn, Largest: pn}}}, protocol.Encryption1RTT, now.Add(time.Millisecond))
			require.NoError(t, err)
			require.Equal(t, uint64(sink.sends[0].Packet.Length), sink.events[0].Delivery.Delivered)
			require.NoError(t, c.triggerSending(now.Add(2*time.Millisecond)).err)
			require.False(t, c.sentPacketHandler.(interface{ DeliveryIdle() bool }).DeliveryIdle(), "dequeued worker ownership remains pending")
			entry.release()
			require.NoError(t, c.triggerSending(now.Add(3*time.Millisecond)).err)
			require.True(t, c.sentPacketHandler.(interface{ DeliveryIdle() bool }).DeliveryIdle())
		})
	}
	t.Run("expiry wakes a hard-blocked connection", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			tc := newEmissionTestConnection(t, false)
			c := tc.conn
			c.idleTimeout = 10 * time.Second
			tc.connRunner.EXPECT().Remove(gomock.Any()).AnyTimes()
			now := monotime.Now()
			c.emission.bbr = newBBRSendPolicy(1_000_000, 1200, now)
			ackhandler.EnableDeliverySampling(c.sentPacketHandler, &deliveryTestSink{}, c.emission.deliveryPendingBytes)
			c.framer.enableDeliveryObservations()
			h := c.sentPacketHandler.(interface {
				QueueProbePacketAt(protocol.EncryptionLevel, monotime.Time) bool
				DeliveryExpiry() monotime.Time
				DeliveryStats() congestion.DeliveryStats
			})
			pn := c.sentPacketHandler.PopPacketNumber(protocol.Encryption1RTT)
			c.sentPacketHandler.SentPacket(now, pn, protocol.InvalidPacketNumber, nil, []ackhandler.Frame{{Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, protocol.ECNNon, 1000, false, false)
			require.True(t, h.QueueProbePacketAt(protocol.Encryption1RTT, now))
			expiry := h.DeliveryExpiry()
			held := c.emission.bbr.credit.reserve(4800, 4800, true, false)
			require.NotNil(t, held)
			c.blocked = blockModeHardBlocked
			tc.sendConn.EXPECT().Write(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
			done := make(chan error, 1)
			go func() { done <- c.run() }()
			t.Cleanup(func() {
				c.destroyImpl(nil)
				held.complete()
				synctest.Wait()
				require.NoError(t, <-done)
			})
			synctest.Wait()
			time.Sleep(monotime.Until(expiry))
			synctest.Wait()
			require.Equal(t, uint64(1), h.DeliveryStats().Expired)
			require.Zero(t, h.DeliveryStats().Retained)
		})
	})
}
