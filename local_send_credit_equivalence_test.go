package quic

import (
	"math/rand/v2"
	"testing"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
)

type creditPair struct {
	frozen *frozenSendReservation
	got    *sendReservation
	queued bool // handed off: only a worker-side complete may follow
}

// The recycling ledger is equivalent to the predecessor on every operation
// sequence emission and the worker can issue: identical ledger state and
// returns after every operation, and identical wakeup tokens wherever a wait
// can begin (after a refusal's drain, after a wait's rearm, and after a worker
// completion). Connection-side releases are the only operations at which the
// predecessor leaves a token the new ledger does not; no wait observes it.
func TestLocalSendCreditFrozenEquivalence(t *testing.T) {
	seeds := 2000
	if testing.Short() {
		seeds = 200
	}
	var ops, refusals, waits, workerCompletions, reused int
	for seed := range seeds {
		rng := rand.New(rand.NewPCG(uint64(seed), 0x7145))
		frozen, got := newFrozenLocalSendCredit(), newLocalSendCredit()
		var live []*creditPair
		var construction *creditPair // at most one connection-owned reservation
		seen := map[*sendReservation]bool{}
		limit := protocol.ByteCount(2400 * (1 + rng.IntN(4)))
		tokens := func(what string) {
			require.Equal(t, len(frozen.available), len(got.available), "wakeup token after %s (op %d)", what, ops)
		}
		state := func(what string) {
			require.Equal(t,
				[4]any{frozen.pending, frozen.current, frozen.generation, frozen.isolated},
				[4]any{got.pending, got.current, got.generation, got.isolated}, "ledger after %s (op %d)", what, ops)
		}
		for range 300 {
			ops++
			switch x := rng.IntN(100); {
			case x < 35 && construction == nil: // reserve before packing
				n := protocol.ByteCount(1 + rng.IntN(int(limit)))
				ordinary, isolated := rng.IntN(3) > 0, rng.IntN(20) == 0
				f, g := frozen.reserve(n, limit, ordinary, isolated), got.reserve(n, limit, ordinary, isolated)
				require.Equal(t, f == nil, g == nil, "admission")
				if g == nil {
					refusals++
					tokens("refusal")
				} else {
					if seen[g] {
						reused++
					}
					seen[g] = true
					require.Equal(t, [3]any{f.bytes, f.generation, f.isolated}, [3]any{g.bytes, g.generation, g.isolated})
					construction = &creditPair{frozen: f, got: g}
				}
				state("reserve")
			case x < 55 && construction != nil: // handoff, or unused allowance
				if rng.IntN(4) == 0 {
					construction.frozen.complete()
					construction.got.completeLocal()
					state("unused allowance")
				} else {
					n := protocol.ByteCount(1 + rng.IntN(int(construction.got.bytes)))
					construction.frozen.resize(n)
					construction.got.resize(n)
					construction.queued = true
					live = append(live, construction)
					state("handoff")
				}
				construction = nil
			case x < 80 && len(live) > 0: // worker completion, in any order
				i := rng.IntN(len(live))
				p := live[i]
				live = append(live[:i], live[i+1:]...)
				p.frozen.complete()
				p.got.complete()
				workerCompletions++
				state("worker completion")
				tokens("worker completion")
			case x < 88: // refused request rearm
				n := protocol.ByteCount(1 + rng.IntN(int(limit)))
				ordinary, isolated := rng.IntN(2) == 0, rng.IntN(10) == 0
				control := protocol.ByteCount(1200)
				require.Equal(t, frozen.waitForReservation(n, limit, control, ordinary, isolated), got.waitForReservation(n, limit, control, ordinary, isolated))
				waits++
				state("wait")
				tokens("wait")
			case x < 92: // path reset
				g := uint64(rng.IntN(3))
				frozen.resetGeneration(g)
				got.resetGeneration(g)
				state("generation reset")
			}
		}
		for _, p := range live {
			p.frozen.complete()
			p.got.complete()
		}
		if construction != nil {
			construction.frozen.complete()
			construction.got.completeLocal()
		}
		state("drain")
		require.Zero(t, got.pending)
		require.LessOrEqual(t, len(got.free), len(seen), "the free list never exceeds the reservations ever outstanding")
	}
	require.Positive(t, refusals)
	require.Positive(t, reused, "completed reservations must be reused")
	t.Logf("seeds=%d ops=%d refusals=%d waits=%d worker-completions=%d reused=%d", seeds, ops, refusals, waits, workerCompletions, reused)
}

// Every handed-off reservation completes exactly once, through the worker, a
// stopped queue or a close drain; a second release of an entry stops at its
// buffer before reaching credit; steady reservation does not allocate.
func TestLocalSendCreditOwnership(t *testing.T) {
	t.Run("stopped queue and close drain", func(t *testing.T) {
		credit := newLocalSendCredit()
		q := newSendQueue(nil, nil).(*sendQueue)
		const limit = 1 << 20
		issued := map[*sendReservation]bool{}
		for range sendQueueCapacity {
			r := credit.reserve(1200, limit, true, false)
			issued[r] = true
			r.resize(1000)
			q.Send(getPacketWithContents(make([]byte, 1000)), 0, protocol.ECNNon, sendMetadata{credit: r})
		}
		close(q.runStopped) // the worker has stopped; the queue is full
		late := credit.reserve(1200, limit, true, false)
		issued[late] = true
		late.resize(1200)
		q.Send(getPacketWithContents(make([]byte, 1200)), 0, protocol.ECNNon, sendMetadata{credit: late})
		require.Equal(t, protocol.ByteCount(sendQueueCapacity*1000), credit.pending, "a send after the worker stopped completes at once")
		for len(q.queue) > 0 { // Close's drain
			(<-q.queue).release()
		}
		require.Zero(t, credit.pending)
		require.Len(t, credit.free, sendQueueCapacity+1)
		require.True(t, issued[credit.reserve(1200, limit, true, false)], "completed reservations are reused")
	})

	t.Run("second release stops at the buffer", func(t *testing.T) {
		credit := newLocalSendCredit()
		r := credit.reserve(1200, 2400, true, false)
		r.resize(1200)
		entry := queueEntry{buf: getPacketWithContents(make([]byte, 1200)), metadata: sendMetadata{credit: r}}
		entry.release()
		require.Zero(t, credit.pending)
		next := credit.reserve(1200, 2400, true, false)
		require.Same(t, r, next)
		require.Panics(t, func() { entry.release() })
		require.Equal(t, protocol.ByteCount(1200), credit.pending, "the reused reservation keeps its credit")
		next.complete()
	})

	t.Run("steady reservation does not allocate", func(t *testing.T) {
		credit := newLocalSendCredit()
		credit.reserve(1, 2400, true, false).complete()
		require.Zero(t, testing.AllocsPerRun(100, func() {
			r := credit.reserve(1200, 2400, true, false)
			r.resize(1100)
			r.complete()
		}))
	})
}
