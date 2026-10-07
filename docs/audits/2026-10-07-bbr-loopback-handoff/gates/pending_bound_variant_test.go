package quic

// #738 variant gates for the diagnostic pending-bound arm, copied by build.py
// into exported trees only; never part of any revision. variantBoundQ is the
// tree's ordinary pending bound in quanta (2 for r8, 4 for the diagnostic
// arm), written by build.py into zz_variant_bound_test.go. Each test restates,
// for the tree's bound, a contract that r8's own tests check at 2Q, so the
// same file must pass on r8 and on the diagnostic arm; the arm's one
// deviation from D08 is the bound itself.

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand/v2"
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

// The emission path admits ordinary work until pending reaches exactly the
// tree's bound, refuses with the credit's wakeup while queue slots are free,
// never exceeds the bound, and resumes when the worker's completions return
// credit (r8: TestBBRPendingCreditConcurrentDrainRefill).
func TestBBRVariantPendingBoundDrainRefill(t *testing.T) {
	for _, gso := range []bool{false, true} {
		t.Run(fmt.Sprint(gso), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				tc := newEmissionTestConnection(t, gso)
				c := tc.conn
				now := monotime.Now()
				c.emission.bbr = newBBRSendPolicy(1_000_000, 1200, now) // Q = 2400
				bound := protocol.ByteCount(variantBoundQ) * c.emission.bbr.quantum
				for range 2 * variantBoundQ * 2 {
					require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: make([]byte, 1187)}))
				}
				release := make(chan struct{})
				tc.sendConn.EXPECT().Write(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func([]byte, uint16, protocol.ECN) error { <-release; return nil }).AnyTimes()
				q := c.emission.queue.(*sendQueue)
				go func() { require.NoError(t, q.Run()) }()
				t.Cleanup(func() {
					select {
					case <-release:
					default:
						close(release)
					}
					q.Close()
				})
				var refused emissionResult
				for round := 0; round < 4*variantBoundQ && refused.available == nil; round++ {
					for range 6 {
						result := c.emission.send(now, true)
						require.NoError(t, result.err)
						require.LessOrEqual(t, c.emission.bbr.credit.pending, bound, "admission never exceeds the bound")
						if result.stop == emissionQueueFull {
							refused = result
							break
						}
					}
					synctest.Wait()
					now = now.Add(3 * time.Millisecond)
				}
				require.NotNil(t, refused.available, "the bound must refuse")
				require.Equal(t, (<-chan struct{})(c.emission.bbr.credit.available), refused.available, "byte credit, not the queue, refuses")
				require.Less(t, len(q.queue), sendQueueCapacity, "queue slots remain free")
				require.NotNil(t, c.datagramQueue.Peek(), "credit refusal must precede destructive packing")
				require.Equal(t, bound, c.emission.bbr.credit.pending, "pending reaches exactly the bound")
				close(release)
				synctest.Wait()
				require.Zero(t, c.emission.bbr.credit.pending)
				require.True(t, c.emission.send(now, true).progress, "completions return credit")
				synctest.Wait()
			})
		})
	}
}

// Q decreases with existing debt: a rate decrease shrinks Q and so the bound,
// and committed pending work above the new bound is never erased; ordinary
// admission waits until completions return it (r8: TestBBRPendingCreditRateDecrease).
func TestBBRVariantQuantumDecreaseWithDebt(t *testing.T) {
	now := monotime.Now()
	p := newBBRSendPolicy(100_000_000, 1200, now) // Q = 65536
	debt := protocol.ByteCount(variantBoundQ)*2400 - 600
	r := p.credit.reserve(debt, protocol.ByteCount(variantBoundQ)*p.quantum, true, false)
	require.NotNil(t, r)
	p.update(1_000_000, 1200, now) // Q = 2400
	require.EqualValues(t, 2400, p.quantum)
	require.Nil(t, p.credit.reserve(1200, protocol.ByteCount(variantBoundQ)*p.quantum, true, false))
	require.Equal(t, debt, p.credit.pending, "a rate reduction cannot erase committed debt")
	r.complete()
	next := p.credit.reserve(1200, protocol.ByteCount(variantBoundQ)*p.quantum, true, false)
	require.NotNil(t, next)
	next.complete()

	// Through the emission path: the decreased bound refuses with the credit's wakeup.
	c := newEmissionTestConnection(t, true).conn
	c.emission.bbr = newBBRSendPolicy(100_000_000, 1200, now)
	held := c.emission.bbr.credit.reserve(debt, protocol.ByteCount(variantBoundQ)*c.emission.bbr.quantum, true, false)
	require.NotNil(t, held)
	defer held.complete()
	c.emission.bbr.update(1_000_000, 1200, now)
	require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: make([]byte, 1187)}))
	result := c.triggerSending(now)
	require.Equal(t, emissionQueueFull, result.stop)
	require.Equal(t, (<-chan struct{})(c.emission.bbr.credit.available), result.available)
	require.NotNil(t, c.datagramQueue.Peek())
	require.Empty(t, c.emission.queue.(*sendQueue).queue)
}

// ACK and PTO exemptions bypass ordinary pacing, never local byte ownership:
// with exactly one packet of headroom under the tree's bound an ACK proceeds,
// and with less than one packet it is hard-blocked (r8:
// TestBBRQuantumDeadlineExemptions, TestBBRPendingCreditMTUException).
func TestBBRVariantExemptionsUnderBound(t *testing.T) {
	quantumWait := 2424243 * time.Nanosecond // Q = 2400 at 990,000 B/s
	t.Run("ACK with one packet of headroom", func(t *testing.T) {
		c := newEmissionTestConnection(t, false).conn
		now := monotime.Now()
		c.emission.bbr = newBBRSendPolicy(1_000_000, 1200, now)
		p := c.emission.bbr
		q := c.emission.queue.(*sendQueue)
		p.sent(p.budget(now), now)
		held := p.credit.reserve(protocol.ByteCount(variantBoundQ)*p.quantum-1200, protocol.ByteCount(variantBoundQ)*p.quantum, false, false)
		require.NotNil(t, held)
		defer held.complete()
		require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("paced payload")}))
		require.NoError(t, c.receivedPacketHandler.ReceivedPacket(4, protocol.ECNNon, protocol.Encryption1RTT, now, true))
		require.NoError(t, c.receivedPacketHandler.ReceivedPacket(5, protocol.ECNNon, protocol.Encryption1RTT, now, true))
		result := c.triggerSending(now)
		require.NoError(t, result.err)
		require.True(t, result.progress, "ACK proceeds without ordinary credit")
		require.Equal(t, emissionPaced, result.stop)
		require.Equal(t, now.Add(quantumWait), result.deadline, "payload waits for Q")
		require.Len(t, q.queue, 1)
		(<-q.queue).release()
		require.NotNil(t, c.datagramQueue.Peek())
		require.Zero(t, p.budget(now), "ACK is pacing exempt")
	})
	t.Run("PTO with one packet of headroom", func(t *testing.T) {
		c := newEmissionTestConnection(t, false).conn
		now := monotime.Now()
		c.emission.bbr = newBBRSendPolicy(1_000_000, 1200, now)
		p := c.emission.bbr
		q := c.emission.queue.(*sendQueue)
		defer func() {
			for len(q.queue) > 0 {
				(<-q.queue).release()
			}
		}()
		require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("outstanding data")}))
		require.True(t, c.triggerSending(now).progress)
		alarm := c.sentPacketHandler.GetLossDetectionTimeout()
		require.NoError(t, c.sentPacketHandler.OnLossDetectionTimeout(alarm))
		p.sent(p.budget(alarm), alarm)
		held := p.credit.reserve(protocol.ByteCount(variantBoundQ)*p.quantum-p.credit.pending-1200, protocol.ByteCount(variantBoundQ)*p.quantum, false, false)
		require.NotNil(t, held)
		defer held.complete()
		before := len(q.queue)
		result := c.triggerSending(alarm)
		require.NoError(t, result.err)
		require.True(t, result.progress, "authorized PTO proceeds without ordinary credit")
		require.Zero(t, result.deadline)
		require.Len(t, q.queue, before+1)
		require.Zero(t, p.budget(alarm), "PTO is pacing exempt")
	})
	for _, isolated := range []bool{false, true} {
		t.Run(fmt.Sprintf("hard block isolated=%t", isolated), func(t *testing.T) {
			c := newEmissionTestConnection(t, false).conn
			now := monotime.Now()
			c.emission.bbr = newBBRSendPolicy(1_000_000, 1200, now)
			c.mtuDiscoverer = newMTUDiscoverer(c.rttStats, 1200, 1400, nil)
			c.mtuDiscoverer.Start(now.Add(-time.Hour))
			bound := protocol.ByteCount(variantBoundQ) * c.emission.bbr.quantum
			held := c.emission.bbr.credit.reserve(bound-1199, bound, false, isolated)
			require.NotNil(t, held)
			defer held.complete()
			require.NoError(t, c.receivedPacketHandler.ReceivedPacket(4, protocol.ECNNon, protocol.Encryption1RTT, now, true))
			require.NoError(t, c.receivedPacketHandler.ReceivedPacket(5, protocol.ECNNon, protocol.Encryption1RTT, now, true))
			result := c.triggerSending(now)
			require.False(t, result.progress)
			require.Equal(t, blockModeHardBlocked, result.blocked)
			require.NotNil(t, result.available)
			require.Empty(t, c.emission.queue.(*sendQueue).queue)
			require.True(t, c.mtuDiscoverer.ShouldSendProbe(now))
		})
	}
	t.Run("isolated probe waits for an empty ledger", func(t *testing.T) {
		now := monotime.Now()
		p := newBBRSendPolicy(1_000_000, 1200, now)
		bound := protocol.ByteCount(variantBoundQ) * p.quantum
		held := p.credit.reserve(1200, bound, false, false)
		require.Nil(t, p.credit.reserve(1300, bound, false, true), "an isolated probe needs pending zero")
		held.complete()
		probe := p.credit.reserve(7000, bound, false, true)
		require.NotNil(t, probe, "an isolated probe may exceed the bound")
		require.Nil(t, p.credit.reserve(1, bound, false, false), "even exempt control waits for the isolated probe")
		probe.complete()
		require.Zero(t, p.credit.pending)
	})
}

// Under current-generation pressure that leaves room for one control packet
// but not for Q, the ACK goes out and ordinary traffic stays pending (r8:
// TestBBRPendingCreditMigrationDebt, "current generation GSO pressure").
func TestBBRVariantGSOPressurePreservesControl(t *testing.T) {
	c := newEmissionTestConnection(t, true).conn
	now := monotime.Now()
	c.emission.bbr = newBBRSendPolicy(1_000_000, 1200, now)
	bound := protocol.ByteCount(variantBoundQ) * c.emission.bbr.quantum
	held := c.emission.bbr.credit.reserve(bound-1800, bound, true, false)
	require.NotNil(t, held)
	defer held.complete()
	require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("ordinary stays pending")}))
	require.NoError(t, c.receivedPacketHandler.ReceivedPacket(4, protocol.ECNNon, protocol.Encryption1RTT, now, true))
	require.NoError(t, c.receivedPacketHandler.ReceivedPacket(5, protocol.ECNNon, protocol.Encryption1RTT, now, true))
	result := c.triggerSending(now)
	require.True(t, result.progress, "Q cannot fit but one control packet can")
	ack := <-c.emission.queue.(*sendQueue).queue
	ack.release()
	require.NotNil(t, c.datagramQueue.Peek())
	result = c.triggerSending(now)
	require.Equal(t, blockModeCongestionLimited, result.blocked)
	select {
	case <-result.available:
		t.Fatal("unused ACK reservation must not rearm an oversized ordinary request")
	default:
	}
}

// A full ledger reports local limitation to delivery sampling (r8:
// TestDeliverySamplerNoDataReasons "queue").
func TestBBRVariantDeliveryLocalLimited(t *testing.T) {
	c := newEmissionTestConnection(t, false).conn
	now := monotime.Now()
	c.emission.bbr = newBBRSendPolicy(1_000_000, 1200, now)
	ackhandler.EnableDeliverySampling(c.sentPacketHandler, &deliveryTestSink{}, c.emission.deliveryPendingBytes)
	c.framer.enableDeliveryObservations()
	bound := protocol.ByteCount(variantBoundQ) * c.emission.bbr.quantum
	r := c.emission.bbr.credit.reserve(bound, bound, true, false)
	require.NotNil(t, r)
	defer r.complete()
	require.NoError(t, c.triggerSending(now).err)
	stats := c.sentPacketHandler.(interface {
		DeliveryStats() congestion.DeliveryStats
	}).DeliveryStats()
	require.Equal(t, congestion.SendLocalLimited, stats.Stop)
	require.False(t, stats.Idle)
}

// Pacing and admission bounds under the tree's pending bound, from #734's
// frozen-policy oracle with its limit generalized: over 400,000 seeded
// opportunities, no opportunity admits more than one quantum of paced bytes,
// and no admission takes pending work above the bound.
func TestBBRVariantPolicyBounds(t *testing.T) {
	const size = protocol.ByteCount(1400)
	rng := rand.New(rand.NewPCG(738, 2026))
	now := monotime.Time(time.Hour)
	p := newBBRSendPolicy(12_500_000, size, now)
	h := sha256.New()
	put := func(vs ...int64) {
		var b [8]byte
		for _, v := range vs {
			binary.LittleEndian.PutUint64(b[:], uint64(v))
			h.Write(b[:])
		}
	}
	type queued struct {
		r    *sendReservation
		done monotime.Time
	}
	var queue []queued
	drain := uint64(12_500_000)
	workerFree := now
	var refused int
	var maxBurstOverQ, maxPendingOverBound float64
	deadline := monotime.Time(0)
	for step := 0; step < 400_000; step++ {
		switch k := rng.IntN(10); {
		case deadline == 0 || k == 0:
			now = now.Add(time.Duration(rng.Int64N(int64(300 * time.Microsecond))))
		case k < 4:
			now = max(now, deadline)
		case k < 8:
			now = max(now, deadline.Add(time.Duration(rng.Int64N(int64(2*time.Millisecond)))))
		default:
			now = now.Add(time.Duration(rng.Int64N(int64(max(1, deadline.Sub(now))) + 1)))
		}
		if rng.IntN(500) == 0 {
			rate := uint64(1_000_000 + rng.Int64N(2_000_000_000))
			p.update(rate, size, now)
			drain = rate / 2 // a worker slower than the rate, so the bound binds
		}
		for len(queue) > 0 && !queue[0].done.After(now) {
			queue[0].r.complete()
			queue = queue[1:]
		}
		var burst protocol.ByteCount
		for p.budget(now) >= size {
			bound := protocol.ByteCount(variantBoundQ) * p.quantum
			r := p.credit.reserve(size, bound, true, false)
			if r == nil {
				refused++
				put(-1, int64(now))
				break
			}
			if f := float64(p.credit.pending) / float64(bound); f > maxPendingOverBound {
				maxPendingOverBound = f
			}
			p.sent(size, now)
			burst += size
			workerFree = max(workerFree, now).Add(time.Duration(uint64(size) * uint64(time.Second) / max(1, drain)))
			queue = append(queue, queued{r, workerFree})
		}
		if f := float64(burst) / float64(p.quantum); f > maxBurstOverQ {
			maxBurstOverQ = f
		}
		deadline = p.deadline(size, now)
		put(int64(now), int64(p.budget(now)), int64(deadline), p.tokens, int64(p.quantum), int64(burst), int64(p.credit.pending))
	}
	t.Logf("variant bound %dQ: hash %x refused=%d max_burst_over_Q=%.4f max_pending_over_bound=%.4f",
		variantBoundQ, h.Sum(nil), refused, maxBurstOverQ, maxPendingOverBound)
	require.LessOrEqual(t, maxBurstOverQ, 1.0, "an opportunity admitted more than Q")
	require.LessOrEqual(t, maxPendingOverBound, 1.0, "admission took pending work above the bound")
	require.Positive(t, refused, "the bound must bind in this sequence")
}

// Exactly-once completion and the credit-return point: while the worker's
// submission blocks, the in-service group keeps its bytes pending (dequeue
// returns nothing); completion returns them once, and the queue's stop path
// releases queued work exactly once (r8: TestBBRPendingCreditWorkerOwned,
// TestBBRPendingCreditStoppedAndClose), at the tree's bound.
func TestBBRVariantCreditReturnPoint(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctrl := gomock.NewController(t)
		conn := NewMockSendConn(ctrl)
		q := newSendQueue(conn, nil)
		credit := newLocalSendCredit()
		bound := protocol.ByteCount(variantBoundQ) * 2400
		blocked := make(chan struct{})
		conn.EXPECT().Write(gomock.Any(), uint16(0), protocol.ECNNon).DoAndReturn(func([]byte, uint16, protocol.ECN) error { <-blocked; return nil }).AnyTimes()
		var bufs []*packetBuffer
		for range variantBoundQ * 2 {
			r := credit.reserve(1200, bound, true, false)
			require.NotNil(t, r)
			b := getPacketWithContents(make([]byte, 1200))
			bufs = append(bufs, b)
			q.Send(b, 0, protocol.ECNNon, sendMetadata{credit: r})
		}
		require.Equal(t, bound, credit.pending)
		go func() { require.NoError(t, q.Run()) }()
		synctest.Wait()
		require.Nil(t, credit.reserve(1, bound, true, false), "dequeue must not return credit")
		require.Equal(t, bound, credit.pending)
		close(blocked)
		synctest.Wait()
		require.Zero(t, credit.pending, "each completion returns its credit once")
		q.Close()
		for _, b := range bufs {
			require.Zero(t, b.refCount)
		}
	})
}
