package ackhandler

import (
	"cmp"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/stretchr/testify/require"
)

// #735 lead 2: tracking order while an ACK's records are appended, and sorting
// only when it broke, yields exactly the always-sorted list. Histories mix
// in-order live records with retained discoveries that precede them.
func TestFeedbackAckedOrderMatchesSort(t *testing.T) {
	rng := rand.New(rand.NewPCG(735, 2))
	var sorted, unsorted int
	d := &congestionDispatch{}
	for range 20000 {
		d.event.Acked = d.event.Acked[:0]
		d.ackedUnordered = false
		// Unique ordinals: a live run in send order, then retained records drawn
		// from anywhere, as appendRetainedAck discovers them.
		base := uint64(rng.IntN(1 << 20))
		ords := make([]uint64, 0, 64)
		seen := map[uint64]bool{}
		for i := range rng.IntN(32) {
			o := base + uint64(i)*uint64(1+rng.IntN(3))
			if !seen[o] {
				seen[o] = true
				ords = append(ords, o)
			}
		}
		for range rng.IntN(4) {
			if rng.IntN(3) == 0 {
				continue
			}
			o := uint64(rng.IntN(1 << 21))
			if !seen[o] {
				seen[o] = true
				ords = append(ords, o)
			}
		}
		want := make([]congestion.PacketInfo, 0, len(ords))
		for _, o := range ords {
			p := congestion.PacketInfo{Ordinal: o, Length: 1200}
			d.appendAcked(p)
			want = append(want, p)
		}
		slices.SortFunc(want, func(a, b congestion.PacketInfo) int { return cmp.Compare(a.Ordinal, b.Ordinal) })
		if d.ackedUnordered {
			unsorted++
		} else {
			sorted++
		}
		d.sortAcked()
		require.Equal(t, want, d.event.Acked)
	}
	require.Positive(t, sorted)
	require.Positive(t, unsorted)
	t.Logf("in order %d, needed sorting %d", sorted, unsorted)
}
