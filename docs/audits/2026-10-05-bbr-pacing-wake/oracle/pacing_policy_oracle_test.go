package quic

// #734 frozen-policy oracle, copied by build.py into the exported trees of
// d0fabc4d and the new revision; never part of either revision. It feeds one
// fixed, seeded sequence of timestamped inputs (opportunity times from precise,
// late and early wakes; rate increases and decreases; a worker draining queued
// bytes) to the BBR pacing policy and local send credit, hashes every decision
// (budget, deadline, credit, admissions, refusals), and checks the bounds a
// wake path must not change: an opportunity admits at most one quantum of
// paced bytes, and admission never takes pending work above 2Q.

import (
	"crypto/sha256"
	"encoding/binary"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

func TestPacingWakePolicyOracle(t *testing.T) {
	const size = protocol.ByteCount(1400)
	rng := rand.New(rand.NewPCG(734, 2026))
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
	drain := uint64(12_500_000) // worker drain rate, bytes per second
	workerFree := now
	var opps, admitted, refused, rateChanges int
	var maxBurstOverQ, maxPendingOver2Q float64
	deadline := monotime.Time(0)
	for step := 0; step < 400_000; step++ {
		// The next opportunity: at the deadline exactly, a late wake (up to 2 ms),
		// or an early wake from a packet.
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
			drain = rate
			rateChanges++
		}
		for len(queue) > 0 && !queue[0].done.After(now) {
			queue[0].r.complete()
			queue = queue[1:]
		}
		opps++
		var burst protocol.ByteCount
		for p.budget(now) >= size {
			r := p.credit.reserve(size, 2*p.quantum, true, false)
			if r == nil {
				refused++
				put(-1, int64(now))
				break
			}
			if pending := p.credit.pending; float64(pending)/float64(2*p.quantum) > maxPendingOver2Q {
				maxPendingOver2Q = float64(pending) / float64(2*p.quantum)
			}
			p.sent(size, now)
			burst += size
			admitted++
			workerFree = max(workerFree, now).Add(time.Duration(uint64(size) * uint64(time.Second) / max(1, drain)))
			queue = append(queue, queued{r, workerFree})
		}
		if f := float64(burst) / float64(p.quantum); f > maxBurstOverQ {
			maxBurstOverQ = f
		}
		deadline = p.deadline(size, now)
		put(int64(now), int64(p.budget(now)), int64(deadline), p.tokens, int64(p.quantum), int64(p.rate), int64(burst), int64(p.credit.pending))
	}
	t.Logf("oracle hash %x", h.Sum(nil))
	t.Logf("oracle counts opportunities=%d admitted=%d refused=%d rate_changes=%d max_burst_over_Q=%.4f max_pending_over_2Q=%.4f",
		opps, admitted, refused, rateChanges, maxBurstOverQ, maxPendingOver2Q)
	if maxBurstOverQ > 1 {
		t.Errorf("an opportunity admitted %.3f quanta", maxBurstOverQ)
	}
	if maxPendingOver2Q > 1 {
		t.Errorf("admission took pending work to %.3f × 2Q", maxPendingOver2Q)
	}
}
