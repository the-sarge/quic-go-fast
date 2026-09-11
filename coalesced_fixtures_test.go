package quic

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// newCoalescedReadFixture fills a coalesced slab as if a single coalesced
// socket read delivered the given segments and splits it into per-datagram
// views. All segments must be segmentSize bytes long, except that the final
// segment may be shorter (the short tail a kernel-coalesced read permits).
// It is shared by the coalesced-storage contract tests and by the platform
// producer slices' (Linux GRO, Windows URO) routing closure tests.
func newCoalescedReadFixture(t *testing.T, segmentSize int, segments ...[]byte) (*coalescedSlab, []*packetBuffer) {
	t.Helper()
	require.NotEmpty(t, segments, "a coalesced read delivers at least one segment")
	slab := getCoalescedSlab()
	data := slab.buf.Data
	for i, seg := range segments {
		require.NotEmpty(t, seg)
		if i < len(segments)-1 {
			require.Len(t, seg, segmentSize, "only the final segment of a coalesced read may be short")
		} else {
			require.LessOrEqual(t, len(seg), segmentSize)
		}
		data = append(data, seg...)
	}
	slab.buf.Data = data
	views := slab.split(segmentSize)
	require.Len(t, views, len(segments))
	for i, v := range views {
		require.Equal(t, segments[i], v.Data)
	}
	return slab, views
}

// testCoalescedSegments builds n distinguishable segments of the given size.
func testCoalescedSegments(n, size int) [][]byte {
	segs := make([][]byte, n)
	for i := range segs {
		seg := make([]byte, size)
		for j := range seg {
			seg[j] = byte(i + 1)
		}
		segs[i] = seg
	}
	return segs
}
