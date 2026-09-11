package quic

import (
	"fmt"
	"sync/atomic"

	"github.com/quic-go/quic-go/internal/protocol"
)

// A coalescedSlab is the backing storage for the segments of a single
// coalesced socket read (Linux UDP_GRO, Windows URO). Sibling segments of one
// read can carry different connection IDs and route to owners that release
// concurrently, so the slab counts its live views atomically — the one
// exception to the non-atomic incoming-storage contract, authorized by the
// 2026-09-11 amendment to ADR 0005 and confined to exactly this use.
//
// A producer obtains a slab with getCoalescedSlab, fills buf.Data with the
// bytes of one coalesced read, and calls split exactly once. The slab is
// returned to the coalesced buffer tier when the last view releases.
type coalescedSlab struct {
	buf *packetBuffer

	// views counts the live per-datagram views into buf. The view whose
	// release drops it to zero recycles the slab.
	views atomic.Int32
}

// coalescedRetentionBudget bounds the coalesced-slab bytes a single
// connection's retention queues may pin through oversized views, enforcing
// protocol.MaxConnRetainedCoalescedBytes alongside the queues' entry counts.
// Charges and releases can come from different goroutines, so the counter is
// atomic.
type coalescedRetentionBudget struct {
	retained atomic.Int64
}

// tryCharge reserves n bytes of the budget,
// reporting false when the reservation would exceed it.
func (b *coalescedRetentionBudget) tryCharge(n int64) bool {
	for {
		cur := b.retained.Load()
		if cur+n > protocol.MaxConnRetainedCoalescedBytes {
			return false
		}
		if b.retained.CompareAndSwap(cur, cur+n) {
			return true
		}
	}
}

func (b *coalescedRetentionBudget) uncharge(n int64) {
	if b.retained.Add(-n) < 0 {
		panic("negative coalesced retention budget")
	}
}

func getCoalescedSlab() *coalescedSlab {
	return &coalescedSlab{buf: getCoalescedPacketBuffer()}
}

// split partitions the filled slab into per-datagram packetBuffer views of
// segmentSize bytes each; the final view keeps the short tail when the filled
// length is not a multiple of segmentSize. Each view carries its own ordinary
// non-atomic refCount and releases through the slab hook in putBack. split
// must be called exactly once per slab, after the fill and before any view is
// handed off.
func (s *coalescedSlab) split(segmentSize int) []*packetBuffer {
	if segmentSize <= 0 {
		panic(fmt.Sprintf("coalescedSlab.split: invalid segment size %d", segmentSize))
	}
	data := s.buf.Data
	if len(data) == 0 {
		panic("coalescedSlab.split: empty slab")
	}
	views := make([]*packetBuffer, 0, (len(data)+segmentSize-1)/segmentSize)
	for off := 0; off < len(data); off += segmentSize {
		end := min(off+segmentSize, len(data))
		views = append(views, &packetBuffer{
			Data:     data[off:end:end],
			refCount: 1,
			slab:     s,
		})
	}
	s.views.Store(int32(len(views)))
	return views
}

// released reports whether the slab's storage has been recycled.
// It is only meaningful once all releases have completed.
func (s *coalescedSlab) released() bool { return s.buf == nil }

// decrement records the release of one view and recycles the slab's storage
// into the coalesced buffer tier when the last view is gone.
func (s *coalescedSlab) decrement() {
	n := s.views.Add(-1)
	if n < 0 {
		panic("coalescedSlab released more often than its view count")
	}
	if n == 0 {
		buf := s.buf
		s.buf = nil
		buf.Release()
	}
}

// retainForRetentionQueue makes p safe to hold in a count-bounded retention
// queue past the routing-and-processing pass. A packet that is not backed by
// a coalesced slab is returned unchanged. A slab-backed view is copied into
// the smallest existing buffer tier that fits, and the queue's hold on the
// view is released, so a lone retained segment cannot pin its whole slab. A
// view larger than the large tier keeps its slab and is charged at full slab
// size against the connection's retained-bytes budget; retainForRetentionQueue
// reports false when that budget is exhausted, and the caller must drop the
// packet instead of queueing it.
func retainForRetentionQueue(p receivedPacket, budget *coalescedRetentionBudget) (receivedPacket, bool) {
	view := p.buffer
	if view == nil || view.slab == nil {
		return p, true
	}
	var buf *packetBuffer
	switch size := len(p.data); {
	case size <= protocol.MaxPacketBufferSize:
		buf = getPacketBuffer()
	case size <= protocol.MaxLargePacketBufferSize:
		buf = getLargePacketBuffer()
	default:
		if view.retentionCharge == budget {
			// already charged to this owner when it entered an earlier
			// retention queue
			return p, true
		}
		if !budget.tryCharge(protocol.MaxCoalescedPacketBufferSize) {
			// refused; a charge from a previous owner stays in place until
			// the caller disposes of the packet
			return p, false
		}
		if prev := view.retentionCharge; prev != nil {
			// admission by a new owner moves the charge: the destination is
			// reserved above before the source is refunded
			prev.uncharge(protocol.MaxCoalescedPacketBufferSize)
		}
		view.retentionCharge = budget
		return p, true
	}
	buf.Data = buf.Data[:len(p.data)]
	copy(buf.Data, p.data)
	p.buffer = buf
	p.data = buf.Data
	// release the queue's hold on the view; other packets parsed from the
	// same datagram may still hold it
	view.Decrement()
	view.MaybeRelease()
	return p, true
}

// releaseSlabView is the slab hook on the packetBuffer release path: instead
// of returning the view's Data to a pool, it hands the release to the slab's
// atomic count. The view itself is not pooled.
func (b *packetBuffer) releaseSlabView() {
	if b.retentionCharge != nil {
		b.retentionCharge.uncharge(protocol.MaxCoalescedPacketBufferSize)
		b.retentionCharge = nil
	}
	slab := b.slab
	b.slab = nil
	slab.decrement()
}
