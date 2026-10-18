package quic

import (
	"context"
	"fmt"
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

// supplyGapFixture drives a BBR-paced connection whose stream writer sends
// 16 KiB writes; resupply releases each write after the first.
type supplyGapFixture struct {
	c        *Conn
	sink     *deliveryTestSink
	next     []chan struct{} // next[i] releases write i+1
	released int
	held     *queueEntry // the newest handed-off entry, still with the worker
}

// At 100 MB/s one quantum (64 KiB) holds a whole write, so the stream runs
// dry mid-quantum.
func newSupplyGapFixture(t *testing.T, gso bool, writes int) *supplyGapFixture {
	t.Helper()
	return newSupplyGapFixtureAt(t, gso, writes, 100_000_000)
}

func newSupplyGapFixtureAt(t *testing.T, gso bool, writes int, rate uint64) *supplyGapFixture {
	t.Helper()
	c := newEmissionTestConnection(t, gso).conn
	now := monotime.Now()
	c.emission.bbr = newBBRSendPolicy(rate, 1200, now)
	f := &supplyGapFixture{c: c, sink: &deliveryTestSink{}, next: make([]chan struct{}, writes)}
	for i := range f.next {
		f.next[i] = make(chan struct{})
	}
	ackhandler.EnableDeliverySampling(c.sentPacketHandler, f.sink, c.emission.deliveryPendingBytes)
	c.framer.enableDeliveryObservations()
	c.peerParams = &wire.TransportParameters{InitialMaxStreamDataUni: 1 << 20}
	c.connFlowController.UpdateSendWindow(1 << 20)
	c.streamsMap.HandleMaxStreamsFrame(&wire.MaxStreamsFrame{Type: protocol.StreamTypeUni, MaxStreamNum: 1})
	s, err := c.OpenUniStream()
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range writes {
			if i > 0 {
				<-f.next[i-1]
			}
			_, _ = s.Write(make([]byte, 16384))
		}
	}()
	synctest.Wait()
	t.Cleanup(func() {
		// A test that stops early still lets the writer finish.
		for _, next := range f.next[f.released:] {
			close(next)
		}
		s.CancelWrite(0)
		synctest.Wait()
		<-done
	})
	t.Cleanup(func() {
		if f.held != nil {
			f.held.release()
		}
	})
	return f
}

// take releases each handed-off entry once the next one arrives.
func (f *supplyGapFixture) take() {
	for {
		select {
		case e := <-f.c.emission.queue.(*sendQueue).queue:
			if f.held != nil {
				f.held.release()
			}
			f.held = &e
		default:
			return
		}
	}
}

// drain sends at now until the current write is packed and supply runs out.
func (f *supplyGapFixture) drain(t *testing.T, now monotime.Time) {
	t.Helper()
	stats := f.c.sentPacketHandler.(interface {
		DeliveryStats() congestion.DeliveryStats
	})
	for i := 0; stats.DeliveryStats().Stop != congestion.SendApplicationLimited; i++ {
		require.Less(t, i, 64, "the write drains")
		require.NoError(t, f.c.triggerSending(now).err)
		f.take()
	}
}

// ackOnly sends an ACK-only packet, which spends no pacing credit, and then
// lets an empty opportunity observe the stream still dry.
func (f *supplyGapFixture) ackOnly(t *testing.T, now monotime.Time) {
	t.Helper()
	for pn := range protocol.PacketNumber(2) {
		require.NoError(t, f.c.receivedPacketHandler.ReceivedPacket(100+pn, protocol.ECNNon, protocol.Encryption1RTT, now, true))
	}
	sends := len(f.sink.sends)
	r := f.c.triggerSending(now)
	require.NoError(t, r.err)
	require.True(t, r.progress, "the ACK is sent")
	require.Len(t, f.sink.sends, sends+1)
	require.False(t, f.sink.sends[sends].Packet.AckEliciting, "an ACK-only packet")
	require.NoError(t, f.c.triggerSending(now).err, "the next opportunity still finds no data")
	f.take()
	f.held.release()
	f.held = nil
}

// resupply releases the next write and sends once at now, returning the index
// of its first packet.
func (f *supplyGapFixture) resupply(t *testing.T, now monotime.Time) int {
	t.Helper()
	first := len(f.sink.sends)
	close(f.next[f.released])
	f.released++
	synctest.Wait()
	require.NoError(t, f.c.triggerSending(now).err)
	f.take()
	require.Greater(t, len(f.sink.sends), first, "the next write is sent")
	return first
}

// SendStream.Write returns while the last packet of a write is still buffered,
// so a bulk writer's stream runs dry between writes. That gap is supply
// limitation only once it costs sending time: the pacing credit has had time to
// refill since it was last spent, and no ordinary local work is pending (#800).
func TestDeliverySupplyGap(t *testing.T) {
	for _, tc := range []struct {
		name     string
		resupply time.Duration
		hold     bool   // keep the last queued entry with the worker
		reset    bool   // replace the local path before resupplying
		rateCut  uint64 // lower the pacing rate, clamping the credit, within the gap
		ackOnly  bool   // send an ACK-only packet, which spends no pacing credit, within the gap
		want     congestion.SendLimitation
	}{
		{name: "resupplied within the pacing credit", resupply: time.Microsecond, want: congestion.SendUnknown},
		{name: "gap fills the pacing credit", resupply: 10 * time.Millisecond, want: congestion.SendApplicationLimited},
		{name: "pending local work", resupply: 10 * time.Millisecond, hold: true, want: congestion.SendUnknown},
		{name: "path reset discards the gap", resupply: 10 * time.Millisecond, reset: true, want: congestion.SendUnknown},
		// A smaller quantum clamps the credit to full, but no sending time passed.
		{name: "rate cut within the gap", resupply: time.Microsecond, rateCut: 25_000_000, want: congestion.SendUnknown},
		{name: "ACK-only send within the gap", resupply: time.Microsecond, ackOnly: true, want: congestion.SendUnknown},
		{name: "rate cut and an ACK-only send within the gap", resupply: time.Microsecond, rateCut: 25_000_000, ackOnly: true, want: congestion.SendUnknown},
	} {
		for _, gso := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/ordinary", true: "/GSO"}[gso], func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					f := newSupplyGapFixture(t, gso, 2)
					c, now := f.c, monotime.Now()
					f.drain(t, now)
					require.Less(t, c.emission.bbr.tokens, int64(c.emission.bbr.quantum)*1e9, "the write left the quantum partly spent")
					require.NotEmpty(t, f.sink.sends)
					for _, e := range f.sink.sends {
						require.Equal(t, congestion.SendUnknown, e.Packet.Delivery.Limited)
					}
					if !tc.hold {
						f.held.release()
						f.held = nil
					}
					if tc.reset {
						c.emission.resetLocalPath(c.pathGeneration, now)
					}
					if tc.rateCut != 0 {
						c.emission.bbr.update(tc.rateCut, 1200, now)
						require.Equal(t, int64(c.emission.bbr.quantum)*1e9, c.emission.bbr.tokens, "the cut clamps the credit to its quantum")
						require.NoError(t, c.triggerSending(now).err, "an ACK-driven opportunity still finds no data")
					}
					if tc.ackOnly {
						f.ackOnly(t, now)
					}
					first := f.resupply(t, now.Add(tc.resupply))
					require.Equal(t, tc.want, f.sink.sends[first].Packet.Delivery.Limited)
				})
			})
		}
	}
	// Each write spends more credit, so the gap after it takes longer to refill
	// than the one before.
	for _, gso := range []bool{false, true} {
		t.Run("a later gap refills from the latest spending"+map[bool]string{false: "/ordinary", true: "/GSO"}[gso], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := newSupplyGapFixture(t, gso, 3)
				now := monotime.Now()
				f.drain(t, now)
				fill := f.c.emission.bbr.refilledAt
				f.resupply(t, now.Add(time.Microsecond))
				f.drain(t, now.Add(time.Microsecond))
				require.Greater(t, f.c.emission.bbr.refilledAt, fill)
				f.held.release()
				f.held = nil
				// Past the first gap's refill, short of the second's.
				at := fill.Add(f.c.emission.bbr.refilledAt.Sub(fill) / 2)
				first := f.resupply(t, at)
				require.Equal(t, congestion.SendUnknown, f.sink.sends[first].Packet.Delivery.Limited)
			})
		})
	}
	// A rate rise enlarges the quantum, so a recomputed refill would be later.
	// The gap still counts from the spend-time refill, with or without an
	// ACK-only send in it.
	for _, ack := range []bool{false, true} {
		for _, gso := range []bool{false, true} {
			t.Run(fmt.Sprintf("rate rise within the gap, ACK-only %t", ack)+map[bool]string{false: "/ordinary", true: "/GSO"}[gso], func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					f := newSupplyGapFixtureAt(t, gso, 2, 20_000_000)
					c, now := f.c, monotime.Now()
					f.drain(t, now)
					f.held.release()
					f.held = nil
					refill := c.emission.bbr.refilledAt
					require.Greater(t, refill, now)
					c.emission.bbr.update(40_000_000, 1200, now)
					if ack {
						f.ackOnly(t, now)
					}
					require.Equal(t, refill, c.emission.bbr.refilledAt)
					first := f.resupply(t, refill.Add(10*time.Microsecond))
					require.Equal(t, congestion.SendApplicationLimited, f.sink.sends[first].Packet.Delivery.Limited)
				})
			})
		}
	}
	// Only spending moves the refill time, and only later: a smaller quantum
	// that clamps the credit does not bring it earlier.
	t.Run("pacer refill clock", func(t *testing.T) {
		now := monotime.Now()
		p := newBBRSendPolicy(100_000_000, 1200, now)
		require.Equal(t, now, p.refilledAt, "a new policy starts with full credit")
		at := now.Add(time.Microsecond)
		p.sent(12000, at)
		require.Equal(t, p.fullAt(at), p.refilledAt)
		first := p.refilledAt
		p.sent(0, at.Add(time.Microsecond))
		require.Equal(t, first, p.refilledAt, "an unpaced send changes no refill")
		// A larger quantum would put a recomputed refill later; spending nothing
		// must not move it.
		grown := newBBRSendPolicy(20_000_000, 1200, now)
		grown.sent(12000, at)
		first = grown.refilledAt
		grown.update(40_000_000, 1200, at.Add(time.Microsecond))
		require.Greater(t, grown.fullAt(at.Add(time.Microsecond)), first, "the larger quantum has more to refill")
		grown.sent(0, at.Add(time.Microsecond))
		require.Equal(t, first, grown.refilledAt, "an unpaced send after a rate rise changes no refill")
		// At the 2-packet quantum floor a rate cut slows the refill without a
		// clamp, so a recomputed refill would also be later.
		floor := newBBRSendPolicy(1_000_000, 1200, now)
		floor.sent(1200, at)
		first = floor.refilledAt
		floor.update(500_000, 1200, at.Add(time.Microsecond))
		require.Equal(t, protocol.ByteCount(2400), floor.quantum)
		require.GreaterOrEqual(t, floor.budget(at.Add(time.Microsecond)), protocol.ByteCount(1200), "credit for a packet remains")
		require.Greater(t, floor.fullAt(at.Add(time.Microsecond)), first)
		floor.sent(0, at.Add(time.Microsecond))
		require.Equal(t, first, floor.refilledAt, "an unpaced send after a slower rate changes no refill")
		p.sentProbe(1200, at.Add(time.Microsecond))
		require.Equal(t, p.fullAt(at.Add(time.Microsecond)), p.refilledAt, "probe credit is spending too")
		spent := p.refilledAt
		p.update(25_000_000, 1200, at.Add(2*time.Microsecond))
		require.Equal(t, int64(p.quantum)*1e9, p.tokens, "the cut clamps the credit")
		require.Equal(t, spent, p.refilledAt, "a clamp is not refill")
		p.sent(1200, at.Add(2*time.Microsecond))
		require.Less(t, p.fullAt(at.Add(2*time.Microsecond)), spent, "the clamped credit refills sooner")
		require.Equal(t, spent, p.refilledAt, "later spending cannot bring the refill time earlier")
	})
	t.Run("path reset refills the credit", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			f := newSupplyGapFixture(t, false, 1)
			c, now := f.c, monotime.Now()
			f.drain(t, now)
			f.held.release()
			f.held = nil
			at := now.Add(time.Microsecond)
			c.emission.resetLocalPath(c.pathGeneration, at)
			require.NoError(t, c.triggerSending(at).err)
			first := len(f.sink.sends)
			pn := c.sentPacketHandler.PopPacketNumber(protocol.Encryption1RTT)
			c.sentPacketHandler.SentPacket(at, pn, protocol.InvalidPacketNumber, nil, []ackhandler.Frame{{Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, protocol.ECNNon, 1200, false, false)
			require.Equal(t, congestion.SendApplicationLimited, f.sink.sends[first].Packet.Delivery.Limited, "a dry opportunity at refilled credit marks at once")
		})
	})
	// An empty opportunity at full credit with nothing queued establishes the
	// limitation itself, before any later opportunity checks again.
	t.Run("exhaustion at full credit marks at once", func(t *testing.T) {
		c := newEmissionTestConnection(t, false).conn
		now := monotime.Now()
		c.emission.bbr = newBBRSendPolicy(100_000_000, 1200, now)
		sink := &deliveryTestSink{}
		ackhandler.EnableDeliverySampling(c.sentPacketHandler, sink, c.emission.deliveryPendingBytes)
		c.framer.enableDeliveryObservations()
		require.NoError(t, c.triggerSending(now).err)
		pn := c.sentPacketHandler.PopPacketNumber(protocol.Encryption1RTT)
		c.sentPacketHandler.SentPacket(now, pn, protocol.InvalidPacketNumber, nil, []ackhandler.Frame{{Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, protocol.ECNNon, 1200, false, false)
		require.Equal(t, congestion.SendApplicationLimited, sink.sends[0].Packet.Delivery.Limited)
	})
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

// The BBR diagnostic reports a drained writer's stop at once, but its
// limitation only once the gap costs sending time, as the packet marker does.
func TestDeliverySupplyGapDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name       string
		resupply   time.Duration
		want       congestion.SendLimitation
		limitation string
	}{
		{name: "resupplied within the pacing credit", resupply: time.Microsecond, want: congestion.SendUnknown, limitation: "none"},
		{name: "gap fills the pacing credit", resupply: 10 * time.Millisecond, want: congestion.SendApplicationLimited, limitation: "application"},
	} {
		for _, gso := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/ordinary", true: "/GSO"}[gso], func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					f := newSupplyGapFixture(t, gso, 2)
					c, now := f.c, monotime.Now()
					c.emission.enableBBRECN() // without it the diagnostic reports only ecn=unavailable
					diagnostics := c.sentPacketHandler.(interface{ BBRDiagnostics() string }).BBRDiagnostics
					f.drain(t, now)
					require.Contains(t, diagnostics(), " stop=application limitation=none ")
					f.held.release()
					f.held = nil
					first := f.resupply(t, now.Add(tc.resupply))
					require.Equal(t, tc.want, f.sink.sends[first].Packet.Delivery.Limited)
					require.Contains(t, diagnostics(), " limitation="+tc.limitation+" ")
				})
			})
		}
	}
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
