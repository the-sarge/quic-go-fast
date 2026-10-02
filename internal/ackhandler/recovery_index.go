package ackhandler

import (
	"math/bits"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
)

const (
	recoverySlotWords   = maxRecoveryOutcomes / 64
	recoverySummaryBits = recoverySlotWords / 64
)

// slotSet is a three-level bitset over physical outcome-ring slots. A search
// reads at most five words, independent of how many slots are set.
type slotSet struct {
	words [recoverySlotWords]uint64
	any1  [recoverySummaryBits]uint64 // words[i] != 0
	any2  uint64                      // any1[j] != 0
	full1 [recoverySummaryBits]uint64 // words[i] is all ones
	full2 uint64                      // full1[j] is all ones
}

func (s *slotSet) has(i int) bool { return s.words[i>>6]&(1<<(i&63)) != 0 }

func (s *slotSet) assign(i int, v bool) {
	if v {
		s.set(i)
	} else {
		s.clear(i)
	}
}

func (s *slotSet) set(i int) {
	w := i >> 6
	s.words[w] |= 1 << (i & 63)
	s.any1[w>>6] |= 1 << (w & 63)
	s.any2 |= 1 << (w >> 6)
	if s.words[w] == ^uint64(0) {
		s.full1[w>>6] |= 1 << (w & 63)
		if s.full1[w>>6] == ^uint64(0) {
			s.full2 |= 1 << (w >> 6)
		}
	}
}

func (s *slotSet) clear(i int) {
	w := i >> 6
	s.full1[w>>6] &^= 1 << (w & 63)
	s.full2 &^= 1 << (w >> 6)
	s.words[w] &^= 1 << (i & 63)
	if s.words[w] == 0 {
		s.any1[w>>6] &^= 1 << (w & 63)
		if s.any1[w>>6] == 0 {
			s.any2 &^= 1 << (w >> 6)
		}
	}
}

// nextSet returns the first set slot in [from, to], or -1.
func (s *slotSet) nextSet(from, to int) int {
	return s.next(from, to, false)
}

// nextClear returns the first clear slot in [from, to], or -1.
func (s *slotSet) nextClear(from, to int) int {
	return s.next(from, to, true)
}

func (s *slotSet) prevSet(from, to int) int {
	return s.prev(from, to, false)
}

func (s *slotSet) prevClear(from, to int) int {
	return s.prev(from, to, true)
}

func (s *slotSet) word(w int, clear bool) uint64 {
	if clear {
		return ^s.words[w]
	}
	return s.words[w]
}

func (s *slotSet) summary1(j int, clear bool) uint64 {
	if clear {
		return ^s.full1[j]
	}
	return s.any1[j]
}

func (s *slotSet) summary2(clear bool) uint64 {
	if clear {
		return ^s.full2 & (1<<recoverySummaryBits - 1)
	}
	return s.any2
}

func (s *slotSet) next(from, to int, clear bool) int {
	if from > to {
		return -1
	}
	countNodes(1)
	w := from >> 6
	if m := s.word(w, clear) & (^uint64(0) << (from & 63)); m != 0 {
		return within(w<<6+bits.TrailingZeros64(m), to)
	}
	w++
	if w<<6 > to {
		return -1
	}
	j := w >> 6
	countNodes(1)
	m1 := s.summary1(j, clear) & (^uint64(0) << (w & 63))
	if m1 == 0 {
		countNodes(1)
		m2 := s.summary2(clear) & (^uint64(0) << (j + 1))
		if m2 == 0 {
			return -1
		}
		j = bits.TrailingZeros64(m2)
		countNodes(1)
		m1 = s.summary1(j, clear)
	}
	w = j<<6 + bits.TrailingZeros64(m1)
	countNodes(1)
	return within(w<<6+bits.TrailingZeros64(s.word(w, clear)), to)
}

func (s *slotSet) prev(from, to int, clear bool) int {
	if from > to {
		return -1
	}
	countNodes(1)
	w := to >> 6
	if m := s.word(w, clear) & (^uint64(0) >> (63 - to&63)); m != 0 {
		return atLeast(w<<6+63-bits.LeadingZeros64(m), from)
	}
	if w == 0 || (w-1)<<6+63 < from {
		return -1
	}
	w--
	j := w >> 6
	countNodes(1)
	m1 := s.summary1(j, clear) & (^uint64(0) >> (63 - w&63))
	if m1 == 0 {
		if j == 0 {
			return -1
		}
		countNodes(1)
		m2 := s.summary2(clear) & (^uint64(0) >> (64 - j))
		if m2 == 0 {
			return -1
		}
		j = 63 - bits.LeadingZeros64(m2)
		countNodes(1)
		m1 = s.summary1(j, clear)
	}
	w = j<<6 + 63 - bits.LeadingZeros64(m1)
	countNodes(1)
	return atLeast(w<<6+63-bits.LeadingZeros64(s.word(w, clear)), from)
}

func within(i, to int) int {
	if i > to {
		return -1
	}
	return i
}

func atLeast(i, from int) int {
	if i < from {
		return -1
	}
	return i
}

// spaceSlots lists one packet-number space's ring slots in registration order.
// Packet numbers are monotonic within a space, so the list is sorted by number.
type spaceSlots struct {
	slots   []uint16
	head, n int
}

func (q *spaceSlots) at(i int) int { return int(q.slots[(q.head+i)&(len(q.slots)-1)]) }

func (q *spaceSlots) push(slot int) {
	if q.n == len(q.slots) {
		grown := make([]uint16, max(64, 2*len(q.slots)))
		for i := range q.n {
			grown[i] = uint16(q.at(i))
		}
		q.slots, q.head = grown, 0
	}
	q.slots[(q.head+q.n)&(len(q.slots)-1)] = uint16(slot)
	q.n++
}

func (q *spaceSlots) popFront() {
	q.head = (q.head + 1) & (len(q.slots) - 1)
	q.n--
}

// recoveryIndex holds the outcome ring and every derived index. It is
// allocated with the ring on first registration and dropped with it on reset.
type recoveryIndex struct {
	outcomes []recoveryOutcome
	spaces   [3]spaceSlots
	// Per packet-number space: unresolved, lost or excluded outcomes; current-
	// path receipt-eligible outcomes that are not disposed; and PTO-retired
	// unresolved outcomes.
	pending, eligible, ptoPending [3]slotSet
	spans                         spanIndex
}

const (
	spanBlockSlots = 16
	spanBlocks     = maxRecoveryOutcomes / spanBlockSlots
)

// spanIndex maintains maximal runs of lost outcomes in ring order. Each run's
// duration is the send-time distance between its first and last endpoint;
// a max tree over fixed slot blocks finds the rightmost run whose duration
// exceeds a threshold supplied at query time.
type spanIndex struct {
	lost, endpoints, starts slotSet
	tree                    [2 * spanBlocks]time.Duration
}

func recoverySpace(space protocol.EncryptionLevel) int {
	switch space {
	case protocol.EncryptionInitial:
		return 0
	case protocol.EncryptionHandshake:
		return 1
	case protocol.Encryption0RTT, protocol.Encryption1RTT:
		return 2 // congestionKey folds 0-RTT into the application space
	}
	panic("invalid packet-number space")
}
