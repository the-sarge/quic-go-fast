//go:build linux || windows

package quic

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
