//go:build darwin || linux || freebsd

package quic

import (
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
)

// aggregate selects the private aggregate policy and makes path 1 active beside
// path zero, as SetLocalUseV1("active") would.
func (f *reliableFixture) aggregate(t *testing.T, scheduler pathScheduler) {
	t.Helper()
	f.c.pathCredit.scheduler = scheduler
	require.NoError(t, f.c.pathCredit.setLocalUsePrivate(1, false, monotime.Now()))
}

func (f *reliableFixture) view(id protocol.PathID) pathView {
	return f.c.pathCredit.viewOf(f.c.pathCredit.members[id], monotime.Now())
}

// fillWindow sends until path id's recovery allows less than one packet of size.
func (f *reliableFixture) fillWindow(t *testing.T, id protocol.PathID, size protocol.ByteCount) {
	t.Helper()
	for range 256 {
		f.pass(t)
		if _, allowance := f.recovery(id).(boundedRecovery).SendAllowance(monotime.Now()); allowance < size {
			return
		}
		if deadline := f.c.pacingDeadline; deadline != 0 && deadline != deadlineSendImmediately {
			time.Sleep(time.Until(deadline.ToTime()) + time.Microsecond)
		}
	}
	t.Fatal("the window never filled")
}

// A path whose BBR window holds less than a whole packet can't carry the DATAGRAM
// only it fits, so steering leaves it alone and the primary keeps sending. The
// message waits for that path's ACK.
func TestBBRPathDatagramHeldWhileFittingPathCongested(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newBBRPathFixture(t, protocol.PerspectiveServer)
		f.setPathMTU(0, 1400)
		f.write(t, 1<<20)
		f.fillWindow(t, 0, 1400)
		mode, allowance := f.recovery(0).(boundedRecovery).SendAllowance(monotime.Now())
		require.Equal(t, ackhandler.SendAny, mode, "the window isn't full")
		require.Positive(t, allowance)
		require.False(t, f.view(0).ready, "but it can't take a whole packet")
		congested := f.parseWrites(t, 0)
		f.socket.writes = nil

		f.c.pathCredit.selectPrimaryPrivate(1)
		f.queueDatagram(t, 1300)
		f.drain(t)
		require.Positive(t, reliableBytes(f.parseWrites(t, 1)), "the primary keeps sending")
		require.NotNil(t, f.c.datagramQueue.Peek(), "and the message is held")
		require.Zero(t, f.c.datagramQueue.sendDrops.Load())
		f.socket.writes = nil

		f.ackPath(t, 0, 0, congested)
		f.drain(t)
		require.Equal(t, []int{1300}, reliableDatagrams(f.parseWrites(t, 0)))
		require.Nil(t, f.c.datagramQueue.Peek())
	})
}

// With no valid rate sample yet, BBR aggregation takes paced byte round-robin, and
// each path pays its own pacing. Same-path ACKs then give each path the rate
// estimate that turns the arrival estimate on.
func TestAggregateBBRFallsBackUntilSampled(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newBBRPathFixture(t, protocol.PerspectiveServer)
		f.aggregate(t, scheduleAggregate)
		e := &f.c.emission
		e.bbr.supplyGap = congestion.SendUnknown
		require.Zero(t, f.view(0).rate)
		require.Zero(t, f.view(1).rate)
		start := monotime.Now()

		f.write(t, 12000)
		f.drain(t)
		zero, one := f.parseWrites(t, 0), f.parseWrites(t, 1)
		require.Equal(t, 12000, reliableBytes(zero)+reliableBytes(one))
		require.GreaterOrEqual(t, reliableBytes(zero), 4000, "round-robin shares the bytes")
		require.GreaterOrEqual(t, reliableBytes(one), 4000)
		require.Greater(t, e.bbr.refilledAt, start, "path zero's pacing paid for its share")
		require.Greater(t, f.bbrPath(1).refilledAt, start, "and path 1's for its own")

		time.Sleep(20 * time.Millisecond)
		f.ackPath(t, 0, 0, zero)
		f.ackPath(t, 1, 1, one)
		for _, id := range []protocol.PathID{0, 1} {
			view := f.view(id)
			require.Positive(t, view.rate, "path %d's own samples give it a rate", id)
			require.Positive(t, view.rtt, "and a path-local RTT")
		}
	})
}

// Reno has no delivery sampler, so its aggregation always takes the paced
// round-robin fallback. Selection allocates nothing and installs no sampling store.
func TestAggregateRenoWithoutEstimator(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		f.aggregate(t, scheduleAggregate)
		// Reno's opportunity sends its burst on one route; pacing then hands the
		// next opportunity to the other.
		f.write(t, 40000)
		f.drain(t)
		zero, one := f.parseWrites(t, 0), f.parseWrites(t, 1)
		require.Equal(t, 40000, reliableBytes(zero)+reliableBytes(one))
		require.Positive(t, reliableBytes(zero), "both paths carry data")
		require.Positive(t, reliableBytes(one))

		time.Sleep(20 * time.Millisecond)
		f.ackPath(t, 0, 0, zero)
		f.ackPath(t, 1, 1, one)
		for _, id := range []protocol.PathID{0, 1} {
			view := f.view(id)
			require.Zero(t, view.rate, "path %d has no estimator", id)
			require.Positive(t, view.rtt)
			require.Equal(t, congestion.DeliveryStats{}, f.recovery(id).(deliveryProbe).DeliveryStats(), "and no sampling store")
		}
		now := monotime.Now()
		require.Zero(t, testing.AllocsPerRun(10, func() { f.c.pathCredit.selectRoute(now) }))
	})
}

// Selection precedes destructive packing: while no candidate can take a whole
// packet, nothing is popped, and the DATAGRAM leaves on the first path an ACK frees.
func TestAggregateNoDestructivePopBeforeSelection(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newBBRPathFixture(t, protocol.PerspectiveServer)
		f.aggregate(t, scheduleAggregate)
		f.write(t, 1<<20)
		for f.view(0).ready || f.view(1).ready {
			f.fillWindow(t, 0, protocol.MinInitialPacketSize)
			f.fillWindow(t, 1, protocol.MinInitialPacketSize)
		}
		onOne := f.parseWrites(t, 1)
		f.socket.writes = nil

		f.queueDatagram(t, 100)
		f.drain(t)
		require.Zero(t, reliableBytes(f.parseWrites(t, 0)))
		require.Zero(t, reliableBytes(f.parseWrites(t, 1)))
		require.NotNil(t, f.c.datagramQueue.Peek(), "nothing took the message")

		f.ackPath(t, 1, 1, onOne)
		f.pass(t)
		require.Equal(t, []int{100}, reliableDatagrams(f.parseWrites(t, 1)), "it leaves on the path the ACK freed")
		require.Empty(t, reliableDatagrams(f.parseWrites(t, 0)))
		require.Zero(t, reliableBytes(f.parseWrites(t, 0)))
	})
}

// The fixture's comparison arm keeps paced byte round-robin after both paths
// have valid rate samples.
func TestAggregateComparisonArmIgnoresSamples(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newBBRPathFixture(t, protocol.PerspectiveServer)
		f.aggregate(t, scheduleRoundRobin)
		f.write(t, 12000)
		f.drain(t)
		zero, one := f.parseWrites(t, 0), f.parseWrites(t, 1)
		require.Positive(t, reliableBytes(zero))
		require.Positive(t, reliableBytes(one))
		time.Sleep(20 * time.Millisecond)
		f.ackPath(t, 0, 0, zero)
		f.ackPath(t, 1, 1, one)
		require.Positive(t, f.view(0).rate)
		require.Positive(t, f.view(1).rate)

		p := f.c.pathCredit
		p.members[0].scheduled, p.members[1].scheduled = 10000, 0
		route, _, _, held := p.selectRoute(monotime.Now())
		require.False(t, held)
		require.NotNil(t, route, "the less-served path goes, whatever its estimate")
		require.Equal(t, protocol.PathID(1), route.context.PathID)
		require.Zero(t, p.members[1].scheduled, "and the ledger rebases on it")
		require.EqualValues(t, 10000, p.members[0].scheduled)
	})
}

// A path's view projects its pacing to the controller's current rate, as the
// opportunity's refresh will, so feedback since the path last sent moves its
// next send time and credit limit in the view too. Reading it consumes nothing.
func TestAggregateViewProjectsEnforcedPacing(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newBBRPathFixture(t, protocol.PerspectiveServer)
		f.aggregate(t, scheduleAggregate)
		f.write(t, 12000)
		f.drain(t)
		one := f.parseWrites(t, 1)
		time.Sleep(20 * time.Millisecond)
		f.ackPath(t, 1, 1, one)

		p := f.bbrPath(1)
		now := monotime.Now()
		p.tokens, p.updated = 0, now // path 1's credit is spent
		cached := p.rate
		require.NotEqual(t, cached, p.controller.PacingRate()/100*99+p.controller.PacingRate()%100*99/100, "feedback moved the rate since path 1 last sent")
		view := f.c.pathCredit.viewOf(f.c.pathCredit.members[1], now)
		require.Equal(t, cached, p.rate, "the view changes nothing")
		require.Zero(t, p.tokens)

		size := f.c.pathCredit.members[1].route.mtu.CurrentSize()
		p.update(p.controller.PacingRate(), size, now) // what the opportunity enforces
		enforced := now
		if deadline := p.deadline(size, now); deadline != 0 {
			enforced = deadline
		}
		require.Equal(t, enforced, view.sendAt)
		require.Equal(t, p.credit.admits(size, p.creditLimit()), view.ready)
	})
}
