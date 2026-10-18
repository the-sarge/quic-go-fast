package quic

import (
	"context"
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

// SendStream.Write returns while the last packet of a write is still buffered,
// so a bulk writer's stream runs dry between writes. That gap is supply
// limitation only once it costs sending time: the pacing credit is full and no
// ordinary local work is pending (#800).
func TestDeliverySupplyGap(t *testing.T) {
	const record = 16384
	for _, tc := range []struct {
		name     string
		resupply time.Duration
		hold     bool // keep the last queued entry with the worker
		reset    bool // replace the local path before resupplying
		want     congestion.SendLimitation
	}{
		{name: "resupplied within the pacing credit", resupply: time.Microsecond, want: congestion.SendUnknown},
		{name: "gap fills the pacing credit", resupply: 10 * time.Millisecond, want: congestion.SendApplicationLimited},
		{name: "pending local work", resupply: 10 * time.Millisecond, hold: true, want: congestion.SendUnknown},
		{name: "path reset discards the gap", resupply: time.Microsecond, reset: true, want: congestion.SendUnknown},
	} {
		for _, gso := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/ordinary", true: "/GSO"}[gso], func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					c := newEmissionTestConnection(t, gso).conn
					now := monotime.Now()
					// One quantum (64 KiB) holds a whole write, so the stream runs dry mid-quantum.
					c.emission.bbr = newBBRSendPolicy(100_000_000, 1200, now)
					sink := &deliveryTestSink{}
					ackhandler.EnableDeliverySampling(c.sentPacketHandler, sink, c.emission.deliveryPendingBytes)
					c.framer.enableDeliveryObservations()
					c.peerParams = &wire.TransportParameters{InitialMaxStreamDataUni: 1 << 20}
					c.connFlowController.UpdateSendWindow(1 << 20)
					c.streamsMap.HandleMaxStreamsFrame(&wire.MaxStreamsFrame{Type: protocol.StreamTypeUni, MaxStreamNum: 1})
					s, err := c.OpenUniStream()
					require.NoError(t, err)
					next, done := make(chan struct{}), make(chan struct{})
					go func() {
						defer close(done)
						_, _ = s.Write(make([]byte, record))
						<-next
						_, _ = s.Write(make([]byte, record))
					}()
					synctest.Wait()
					t.Cleanup(func() { s.CancelWrite(0); synctest.Wait(); <-done })
					q := c.emission.queue.(*sendQueue).queue
					var held *queueEntry
					t.Cleanup(func() {
						if held != nil {
							held.release()
						}
					})
					// Release each handed-off entry once the next one arrives, so the
					// newest stays with the worker for the hold case.
					take := func() {
						for {
							select {
							case e := <-q:
								if held != nil {
									held.release()
								}
								held = &e
							default:
								return
							}
						}
					}
					stats := c.sentPacketHandler.(interface {
						DeliveryStats() congestion.DeliveryStats
					})
					for i := 0; stats.DeliveryStats().Stop != congestion.SendApplicationLimited; i++ {
						require.Less(t, i, 64, "the first write drains")
						require.NoError(t, c.triggerSending(now).err)
						take()
					}
					require.Less(t, c.emission.bbr.tokens, int64(c.emission.bbr.quantum)*1e9, "the write left the quantum partly spent")
					first := len(sink.sends)
					require.NotZero(t, first)
					for _, e := range sink.sends {
						require.Equal(t, congestion.SendUnknown, e.Packet.Delivery.Limited)
					}
					if !tc.hold {
						held.release()
						held = nil
					}
					if tc.reset {
						c.emission.resetLocalPath(c.pathGeneration, now)
					}
					close(next)
					synctest.Wait()
					require.NoError(t, c.triggerSending(now.Add(tc.resupply)).err)
					take()
					require.Greater(t, len(sink.sends), first, "the second write is sent")
					require.Equal(t, tc.want, sink.sends[first].Packet.Delivery.Limited)
				})
			})
		}
	}
	t.Run("flow-control stall fills the pacing credit", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			c := newEmissionTestConnection(t, false).conn
			now := monotime.Now()
			c.emission.bbr = newBBRSendPolicy(100_000_000, 1200, now)
			sink := &deliveryTestSink{}
			ackhandler.EnableDeliverySampling(c.sentPacketHandler, sink, c.emission.deliveryPendingBytes)
			c.framer.enableDeliveryObservations()
			c.peerParams = &wire.TransportParameters{}
			c.connFlowController.UpdateSendWindow(1 << 20)
			c.streamsMap.HandleMaxStreamsFrame(&wire.MaxStreamsFrame{Type: protocol.StreamTypeUni, MaxStreamNum: 1})
			s, err := c.OpenUniStream()
			require.NoError(t, err)
			done := make(chan struct{})
			go func() { _, _ = s.Write([]byte("blocked payload")); close(done) }()
			synctest.Wait()
			t.Cleanup(func() { s.CancelWrite(0); synctest.Wait(); <-done })
			for range 2 { // the first opportunity carries STREAM_DATA_BLOCKED
				require.NoError(t, c.triggerSending(now).err)
				for len(c.emission.queue.(*sendQueue).queue) > 0 {
					e := <-c.emission.queue.(*sendQueue).queue
					e.release()
				}
			}
			require.Equal(t, congestion.SendFlowControlLimited, c.sentPacketHandler.(interface {
				DeliveryStats() congestion.DeliveryStats
			}).DeliveryStats().Stop)
			first := len(sink.sends)
			s.updateSendWindow(1 << 20)
			require.NoError(t, c.triggerSending(now.Add(10*time.Millisecond)).err)
			require.Greater(t, len(sink.sends), first)
			require.Equal(t, congestion.SendFlowControlLimited, sink.sends[first].Packet.Delivery.Limited)
			e := <-c.emission.queue.(*sendQueue).queue
			e.release()
		})
	})
}

func TestDeliverySamplerNoDataReasons(t *testing.T) {
	t.Run("pending stream opener is not application idle", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			c := newEmissionTestConnection(t, false).conn
			now := monotime.Now()
			c.emission.bbr = newBBRSendPolicy(1_000_000, 1200, now)
			sink := &deliveryTestSink{}
			ackhandler.EnableDeliverySampling(c.sentPacketHandler, sink, c.emission.deliveryPendingBytes)
			c.framer.enableDeliveryObservations()
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { _, _ = c.OpenUniStreamSync(ctx); close(done) }()
			synctest.Wait()
			t.Cleanup(func() { cancel(); synctest.Wait(); <-done })
			require.True(t, c.triggerSending(now).progress, "STREAMS_BLOCKED is sent")
			entry := <-c.emission.queue.(*sendQueue).queue
			entry.release()
			pn := sink.sends[0].Packet.PacketNumber
			_, err := c.sentPacketHandler.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: pn, Largest: pn}}}, protocol.Encryption1RTT, now.Add(time.Millisecond))
			require.NoError(t, err)
			require.NoError(t, c.triggerSending(now.Add(2*time.Millisecond)).err)
			stats := c.sentPacketHandler.(interface {
				DeliveryStats() congestion.DeliveryStats
			}).DeliveryStats()
			require.False(t, stats.Idle, "the opener still waits after its control frame is drained")
			require.Equal(t, congestion.SendFlowControlLimited, stats.Stop)
			cancel()
			synctest.Wait()
			require.NoError(t, c.triggerSending(now.Add(3*time.Millisecond)).err)
			require.True(t, c.sentPacketHandler.(interface{ DeliveryIdle() bool }).DeliveryIdle(), "unused quota alone is not evidence of blocked supply")
		})
	})
	t.Run("missing credit ledger is unknown", func(t *testing.T) {
		c := newEmissionTestConnection(t, false).conn
		sink := &deliveryTestSink{}
		ackhandler.EnableDeliverySampling(c.sentPacketHandler, sink, c.emission.deliveryPendingBytes)
		require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{Data: []byte("no ledger"), DataLenPresent: true}))
		require.True(t, c.triggerSending(monotime.Now()).progress)
		entry := <-c.emission.queue.(*sendQueue).queue
		defer entry.release()
		require.False(t, sink.sends[0].Packet.Delivery.Valid)
	})
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
