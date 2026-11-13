package quic

import (
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"

	"github.com/stretchr/testify/require"
)

// sampled is an active, ready view with a valid rate sample: 1 MB/s, a 20 ms RTT,
// and one 1,200-byte packet queued, so its estimated arrival is 11.2 ms when unpaced.
func sampled(id protocol.PathID) pathView {
	return pathView{id: id, fits: true, ready: true, queued: 1200, rate: 1_000_000, rtt: 20 * time.Millisecond}
}

// unsampled is an active, ready view whose controller has no valid rate sample.
func unsampled(id protocol.PathID) pathView {
	return pathView{id: id, fits: true, ready: true, queued: 1200}
}

func aggregateChoice(t *testing.T, views []pathView, primary protocol.PathID, now monotime.Time, estimate bool) protocol.PathID {
	t.Helper()
	chosen, _, steering, held, _ := selectAggregate(views, primary, now, estimate)
	require.False(t, held)
	require.False(t, steering)
	return chosen
}

func TestSelectAggregateBalancedRates(t *testing.T) {
	now := monotime.Now()
	require.Equal(t, protocol.PathID(0), aggregateChoice(t, []pathView{sampled(0), sampled(1)}, 0, now, true), "equal arrivals tie to the lower wire ID")

	queued := sampled(0)
	queued.queued += 2400 // 2.4 ms more to drain at 1 MB/s
	require.Equal(t, protocol.PathID(1), aggregateChoice(t, []pathView{queued, sampled(1)}, 0, now, true), "bytes already queued on a path delay its arrival")
}

func TestSelectAggregateAsymmetricDelayAndRate(t *testing.T) {
	now := monotime.Now()
	slow := sampled(0)
	slow.rtt = 100 * time.Millisecond
	require.Equal(t, protocol.PathID(1), aggregateChoice(t, []pathView{slow, sampled(1)}, 0, now, true), "half the RTT counts toward arrival")

	// 60 kB at 1 MB/s is 60 ms on path zero; at 10 MB/s it is 6 ms on path 1.
	narrow, wide := sampled(0), sampled(1)
	narrow.queued, wide.queued = 60_000, 60_000
	wide.rate = 10_000_000
	require.Equal(t, protocol.PathID(1), aggregateChoice(t, []pathView{narrow, wide}, 0, now, true), "the faster rate drains its queue sooner")

	wide.sendAt = now.Add(50 * time.Millisecond)
	require.Equal(t, protocol.PathID(1), aggregateChoice(t, []pathView{narrow, wide}, 0, now, true), "66 ms still beats 70 ms")
	wide.sendAt = now.Add(60 * time.Millisecond)
	require.Equal(t, protocol.PathID(0), aggregateChoice(t, []pathView{narrow, wide}, 0, now, true), "the next eligible send time counts too")
}

func TestSelectAggregateNoRateFallsBackToByteRoundRobin(t *testing.T) {
	now := monotime.Now()
	fast, fresh := sampled(0), unsampled(1)
	fast.rate = 100_000_000
	fast.scheduled, fresh.scheduled = 3000, 1200
	chosen, _, _, held, floor := selectAggregate([]pathView{fast, fresh}, 0, now, true)
	require.False(t, held)
	require.Equal(t, protocol.PathID(1), chosen, "one path without a valid sample puts every path on byte round-robin")
	require.EqualValues(t, 1200, floor, "the ledger floor is the least-served candidate's bytes")

	fresh.scheduled = 4000
	require.Equal(t, protocol.PathID(0), aggregateChoice(t, []pathView{fast, fresh}, 0, now, true))
	require.Equal(t, protocol.PathID(0), aggregateChoice(t, []pathView{unsampled(0), unsampled(1)}, 1, now, true), "no samples at all is the same fallback")
}

func TestSelectAggregatePacingCreditAndPMTU(t *testing.T) {
	now := monotime.Now()
	t.Run("pacing", func(t *testing.T) {
		paced, open := unsampled(0), unsampled(1)
		paced.sendAt, open.scheduled = now.Add(time.Millisecond), 5000
		require.Equal(t, protocol.PathID(1), aggregateChoice(t, []pathView{paced, open}, 0, now, true), "byte round-robin is paced: a path pacing allows now goes first")
		open.sendAt = now.Add(2 * time.Millisecond)
		require.Equal(t, protocol.PathID(0), aggregateChoice(t, []pathView{paced, open}, 0, now, true), "with every path paced, the earliest goes")
	})
	t.Run("congestion and local credit", func(t *testing.T) {
		limited := sampled(0)
		limited.ready = false
		limited.rate = 100_000_000
		require.Equal(t, protocol.PathID(1), aggregateChoice(t, []pathView{limited, sampled(1)}, 0, now, true), "a path that can't take a whole packet isn't a candidate")
		other := sampled(1)
		other.ready = false
		require.Equal(t, protocol.PathID(1), aggregateChoice(t, []pathView{limited, other}, 1, now, true), "with none ready, the primary waits for its ACK")
		require.Equal(t, protocol.PathID(0), aggregateChoice(t, []pathView{limited, other}, noPrimary, now, true), "or the lowest wire ID without one")
	})
	t.Run("PMTU", func(t *testing.T) {
		small, large := sampled(0), sampled(1)
		small.fits = false
		chosen, steerTo, steering, held, _ := selectAggregate([]pathView{small, large}, 0, now, true)
		require.False(t, held)
		require.Equal(t, protocol.PathID(0), chosen)
		require.True(t, steering, "the next DATAGRAM keeps its own eligibility")
		require.Equal(t, protocol.PathID(1), steerTo)
		large.ready = false
		chosen, _, steering, _, _ = selectAggregate([]pathView{small, large}, 0, now, true)
		require.Equal(t, protocol.PathID(0), chosen)
		require.False(t, steering, "a fitting path that can't send isn't named")
	})
}

func TestSelectAggregateBackupExclusion(t *testing.T) {
	now := monotime.Now()
	active, backup := sampled(0), sampled(1)
	active.queued = 1_000_000
	backup.backup = true
	require.Equal(t, protocol.PathID(0), aggregateChoice(t, []pathView{active, backup}, 0, now, true), "a backup stays out while an active path is eligible")
	active.ready = false
	require.Equal(t, protocol.PathID(0), aggregateChoice(t, []pathView{active, backup}, 0, now, true), "even when the active path waits on congestion")

	first, second := sampled(0), sampled(1)
	first.backup, second.backup = true, true
	first.queued = 1_000_000
	require.Equal(t, protocol.PathID(1), aggregateChoice(t, []pathView{first, second}, 0, now, true), "with only backups eligible, they aggregate")

	steer := sampled(1)
	steer.backup = true
	active = sampled(0)
	active.fits = false
	_, steerTo, steering, _, _ := selectAggregate([]pathView{active, steer}, 0, now, true)
	require.True(t, steering, "a DATAGRAM only a backup fits may still leave there")
	require.Equal(t, protocol.PathID(1), steerTo)
}

func TestSelectAggregateDeterministicTies(t *testing.T) {
	now := monotime.Now()
	for range 3 {
		require.Equal(t, protocol.PathID(0), aggregateChoice(t, []pathView{sampled(0), sampled(1)}, 1, now, true), "the estimate ties to the wire ID, not the primary")
		require.Equal(t, protocol.PathID(0), aggregateChoice(t, []pathView{unsampled(0), unsampled(1)}, 1, now, true), "so does the ledger")
	}
	_, _, _, held, _ := selectAggregate(nil, noPrimary, now, true)
	require.True(t, held, "no eligible path carries nothing ordinary")
}

// A path whose PTO fired goes first, so its probe retries the reliable data it
// carried on that same path, even ahead of a better estimate or as a backup.
func TestSelectAggregateSamePathRetryFirst(t *testing.T) {
	now := monotime.Now()
	probing := pathView{id: 1, probing: true, backup: true}
	require.Equal(t, protocol.PathID(1), aggregateChoice(t, []pathView{sampled(0), probing}, 0, now, true))
	require.Equal(t, protocol.PathID(1), aggregateChoice(t, []pathView{unsampled(0), probing}, 0, now, false))
}

// The fixture's comparison arm ignores valid rate samples and stays on paced byte
// round-robin.
func TestSelectAggregateComparisonIgnoresRates(t *testing.T) {
	now := monotime.Now()
	fast, slow := sampled(0), sampled(1)
	fast.rate, fast.scheduled = 100_000_000, 2400
	require.Equal(t, protocol.PathID(0), aggregateChoice(t, []pathView{fast, slow}, 0, now, true))
	require.Equal(t, protocol.PathID(1), aggregateChoice(t, []pathView{fast, slow}, 0, now, false))
}
