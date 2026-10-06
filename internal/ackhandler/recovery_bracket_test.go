package ackhandler

import (
	"math/rand/v2"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
)

// asMap is the oracle view of a witness set: the map the ledger used to hold,
// nil until the first number is noted.
func (w spacePackets) asMap() map[protocol.EncryptionLevel]protocol.PacketNumber {
	var m map[protocol.EncryptionLevel]protocol.PacketNumber
	for s, ok := range w.noted {
		if ok {
			if m == nil {
				m = make(map[protocol.EncryptionLevel]protocol.PacketNumber, 3)
			}
			m[deliverySpaces[s]] = w.number[s]
		}
	}
	return m
}

// frozenLowerBound is the predecessor's search, verbatim apart from its receiver.
func frozenLowerBound(r *recoveryEvidence, q *spaceSlots, pn protocol.PacketNumber) int {
	lo, hi := 0, q.n
	for lo < hi {
		countNodes(1)
		mid := int(uint(lo+hi) >> 1)
		if r.index.outcomes[q.at(mid)].number < pn {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

// The bracketed search returns the predecessor's index for every query: below,
// inside, between and above the registered numbers of each space, with skipped
// numbers, ring eviction and resets.
func TestRecoveryLowerBoundEquivalence(t *testing.T) {
	seeds := 60
	if testing.Short() {
		seeds = 6
	}
	var queries, evicted int
	for seed := range seeds {
		rng := rand.New(rand.NewPCG(uint64(seed), 0x7143))
		r := &recoveryEvidence{measured: true}
		var next [3]protocol.PacketNumber
		var ordinal uint64
		skip := []float64{0, 0.001, 0.05, 0.5}[seed%4]
		sends := []int{50, 3000, maxRecoveryOutcomes + 5000}[seed%3]
		for i := range sends {
			s := 2
			if rng.IntN(20) == 0 {
				s = rng.IntN(3)
			}
			for rng.Float64() < skip {
				next[s] += protocol.PacketNumber(1 + rng.IntN(4))
			}
			ordinal++
			level := deliverySpaces[s]
			r.sent(&congestion.PacketInfo{Space: level, EncryptionLevel: level, PacketNumber: next[s], Ordinal: ordinal, RegistrationValid: true, AckEliciting: true}, 0)
			next[s]++
			if i%97 == 0 || i == sends-1 {
				for sp := range 3 {
					q := &r.index.spaces[sp]
					probe := func(pn protocol.PacketNumber) {
						queries++
						require.Equal(t, frozenLowerBound(r, q, pn), r.lowerBound(q, pn), "space %d pn %d", sp, pn)
					}
					probe(q.first - 1)
					probe(q.last + 1)
					probe(next[sp] + 7)
					for range 12 {
						lo, hi := int64(q.first)-3, int64(q.last)+3
						probe(protocol.PacketNumber(lo + rng.Int64N(hi-lo+1)))
					}
					if q.n > 0 {
						probe(r.index.outcomes[q.at(rng.IntN(q.n))].number)
					}
				}
			}
			if i%997 == 0 || i == sends-1 {
				checkRecoveryIndex(t, r)
			}
		}
		evicted += int(r.evicted)
		if seed%5 == 4 {
			r.reset()
			require.Zero(t, r.index)
		}
	}
	require.Positive(t, evicted, "ring eviction must move the bracket's front")
	t.Logf("seeds=%d queries=%d evicted=%d", seeds, queries, evicted)
}

// A space with no registration when an episode froze its boundary witnesses has
// no witness, so its first loss moves the boundary, even at packet number 0.
func TestRecoveryBoundaryWitnessAbsentSpace(t *testing.T) {
	w := newRecoveryTwin(t)
	w.send(twinSend{level: protocol.EncryptionInitial})
	w.send(twinSend{level: protocol.EncryptionInitial})
	w.advance(time.Millisecond)
	w.timerLoss(protocol.EncryptionInitial, func(p *twinPacket) bool { return p.pn == 0 })
	require.True(t, w.d.recovery.episode.Active)
	_, ok := w.d.recovery.boundaryPackets.get(protocol.EncryptionHandshake)
	require.False(t, ok, "no Handshake registration before the episode")
	hs := w.send(twinSend{level: protocol.EncryptionHandshake})
	require.Zero(t, hs.pn)
	w.advance(time.Millisecond)
	w.timerLoss(protocol.EncryptionHandshake, func(p *twinPacket) bool { return p == hs })
	require.Equal(t, hs.ordinal, w.d.recovery.episode.Boundary, "the witness-free space moves the boundary")
	pn, ok := w.d.recovery.boundaryPackets.get(protocol.EncryptionHandshake)
	require.True(t, ok)
	require.Zero(t, pn)
}
