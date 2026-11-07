package ackhandler

import (
	"math/bits"

	"github.com/quic-go/quic-go/internal/protocol"
)

// localPacketRanks is only a projection of unqualified packet numbers. It does
// not own packets, token membership, history slots, or capacity accounting.
// Words represent aligned groups of 64 PNs. tree is their Fenwick population
// count, except for one word's pending population delta. Every prefix query
// includes that delta, so qualification and arbitrary distance queries remain
// exact without repeatedly updating the tree for consecutive changes in a word.
type localPacketRanks struct {
	base  protocol.PacketNumber
	words []uint64
	tree  []uint32 // one based; tree[0] is unused

	pendingWord  int
	pendingDelta int32
}

func (r *localPacketRanks) wordPrefix(n int) uint32 {
	end := n
	var count uint32
	for ; n > 0; n -= n & -n {
		count += r.tree[n]
	}
	if r.pendingDelta != 0 && r.pendingWord < end {
		return uint32(int64(count) + int64(r.pendingDelta))
	}
	return count
}

func (r *localPacketRanks) appendWord() {
	// The new node includes counts from earlier words. Commit their pending
	// delta first, so a later flush cannot add it to the new node a second time.
	r.flushPending()
	i := len(r.words) + 1
	// New Fenwick nodes cover earlier words too. Merely appending zero here
	// would lose their counts at each power-of-two boundary.
	count := r.wordPrefix(i-1) - r.wordPrefix(i-(i&-i))
	r.words = append(r.words, 0)
	if len(r.tree) == 0 {
		r.tree = append(r.tree, 0)
	}
	r.tree = append(r.tree, count)
}

func (r *localPacketRanks) replaceWord(i int, word uint64) {
	delta := bits.OnesCount64(word) - bits.OnesCount64(r.words[i])
	r.words[i] = word
	if delta == 0 {
		return
	}
	if r.pendingDelta != 0 && r.pendingWord != i {
		r.flushPending()
	}
	r.pendingWord = i
	r.pendingDelta += int32(delta)
}

func (r *localPacketRanks) flushPending() {
	if r.pendingDelta == 0 {
		return
	}
	for i := r.pendingWord + 1; i < len(r.tree); i += i & -i {
		r.tree[i] = uint32(int64(r.tree[i]) + int64(r.pendingDelta))
	}
	r.pendingDelta = 0
}

func (r *localPacketRanks) exclude(pn protocol.PacketNumber) {
	if len(r.words) == 0 {
		r.base = pn &^ 63
	}
	i := int((pn - r.base) / 64)
	if pn < r.base {
		panic("unqualified packet precedes retained rank base")
	}
	for len(r.words) <= i {
		r.appendWord()
	}
	r.replaceWord(i, r.words[i]|uint64(1)<<uint(pn&63))
}

func (r *localPacketRanks) qualify(pn protocol.PacketNumber) {
	if pn < r.base || len(r.words) == 0 {
		return
	}
	i := int((pn - r.base) / 64)
	if i >= len(r.words) {
		return
	}
	r.replaceWord(i, r.words[i]&^(uint64(1)<<uint(pn&63)))
}

// through counts unqualified numbers <= pn. Clipping here preserves queries
// outside the retained span without manufacturing evidence about old PNs.
func (r *localPacketRanks) through(pn protocol.PacketNumber) uint32 {
	if len(r.words) == 0 || pn < r.base {
		return 0
	}
	i := int((pn - r.base) / 64)
	if i >= len(r.words) {
		return r.wordPrefix(len(r.words))
	}
	mask := ^uint64(0) >> (63 - uint(pn&63))
	return r.wordPrefix(i) + uint32(bits.OnesCount64(r.words[i]&mask))
}

func (r *localPacketRanks) between(b, a protocol.PacketNumber) protocol.PacketNumber {
	if a <= b {
		return 0
	}
	return protocol.PacketNumber(r.through(a) - r.through(b))
}

func (r *localPacketRanks) rebuild() {
	// words already contains every change, including the pending delta.
	r.pendingWord, r.pendingDelta = 0, 0
	n := len(r.words) + 1
	if cap(r.tree) < n {
		r.tree = make([]uint32, n)
	} else {
		r.tree = r.tree[:n]
		clear(r.tree)
	}
	for i, word := range r.words {
		j := i + 1
		r.tree[j] += uint32(bits.OnesCount64(word))
		if parent := j + (j & -j); parent < len(r.tree) {
			r.tree[parent] += r.tree[j]
		}
	}
}

func (r *localPacketRanks) pruneBelow(pn protocol.PacketNumber) {
	if len(r.words) == 0 || pn <= r.base {
		return
	}
	n := int((pn - r.base) / 64)
	if n >= len(r.words) {
		r.reset()
		return
	}
	if n != 0 {
		// Move rank words, never physical history. This O(W) rebuild occurs
		// only when at least one complete 64-PN block leaves the prefix.
		copy(r.words, r.words[n:])
		clear(r.words[len(r.words)-n:])
		r.words = r.words[:len(r.words)-n]
		r.base += protocol.PacketNumber(n * 64)
		r.rebuild()
	}
	r.replaceWord(0, r.words[0]&(^uint64(0)<<uint(pn&63)))
}

func (r *localPacketRanks) reset() {
	clear(r.words)
	r.words = r.words[:0]
	r.tree = r.tree[:0]
	r.base = 0
	r.pendingWord, r.pendingDelta = 0, 0
}
