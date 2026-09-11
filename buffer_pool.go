package quic

import (
	"sync"

	"github.com/quic-go/quic-go/internal/protocol"
)

type packetBuffer struct {
	Data []byte

	// refCount counts live packet views and active parsing holds on Data.
	// It doesn't support concurrent use.
	refCount int

	// slab, when non-nil, marks this buffer as a per-datagram view into a
	// coalesced slab (ADR 0005, amendment 2026-09-11). Releasing the view
	// decrements the slab's atomic sibling count instead of returning Data
	// to a pool. refCount above keeps its non-atomic single-owner contract.
	slab *coalescedSlab
	// retentionCharge, when non-nil, is the per-connection budget charged
	// for holding this oversized slab view in a retention queue. It is
	// released when the view releases. Only set on slab views.
	retentionCharge *coalescedRetentionBudget
}

// Split increases the refCount.
// It must be called when a packet buffer is used for more than one packet,
// e.g. when splitting coalesced packets.
func (b *packetBuffer) Split() {
	b.refCount++
}

// Decrement decrements the reference counter.
// It doesn't put the buffer back into the pool.
func (b *packetBuffer) Decrement() {
	b.refCount--
	if b.refCount < 0 {
		panic("negative packetBuffer refCount")
	}
}

// MaybeRelease puts the packet buffer back into the pool,
// if the reference counter already reached 0.
func (b *packetBuffer) MaybeRelease() {
	// only put the packetBuffer back if it's not used any more
	if b.refCount == 0 {
		b.putBack()
	}
}

// Release puts back the packet buffer into the pool.
// It should be called when processing is definitely finished.
func (b *packetBuffer) Release() {
	b.Decrement()
	if b.refCount != 0 {
		panic("packetBuffer refCount not zero")
	}
	b.putBack()
}

// Len returns the length of Data
func (b *packetBuffer) Len() protocol.ByteCount { return protocol.ByteCount(len(b.Data)) }
func (b *packetBuffer) Cap() protocol.ByteCount { return protocol.ByteCount(cap(b.Data)) }

func (b *packetBuffer) putBack() {
	if b.slab != nil {
		b.releaseSlabView()
		return
	}
	if cap(b.Data) == protocol.MaxPacketBufferSize {
		bufferPool.Put(b)
		return
	}
	if cap(b.Data) == protocol.MaxLargePacketBufferSize {
		largeBufferPool.Put(b)
		return
	}
	if cap(b.Data) == protocol.MaxCoalescedPacketBufferSize {
		coalescedBufferPool.Put(b)
		return
	}
	panic("putPacketBuffer called with packet of wrong size!")
}

var bufferPool, largeBufferPool, coalescedBufferPool sync.Pool

func getPacketBuffer() *packetBuffer {
	buf := bufferPool.Get().(*packetBuffer)
	buf.refCount = 1
	buf.Data = buf.Data[:0]
	return buf
}

func getLargePacketBuffer() *packetBuffer {
	buf := largeBufferPool.Get().(*packetBuffer)
	buf.refCount = 1
	buf.Data = buf.Data[:0]
	return buf
}

// getCoalescedPacketBuffer returns a buffer from the coalesced tier.
// These buffers only back coalesced slabs; they are never handed out as
// ordinary packet buffers and never retained by the standard tiers.
func getCoalescedPacketBuffer() *packetBuffer {
	buf := coalescedBufferPool.Get().(*packetBuffer)
	buf.refCount = 1
	buf.Data = buf.Data[:0]
	return buf
}

func init() {
	bufferPool.New = func() any {
		return &packetBuffer{Data: make([]byte, 0, protocol.MaxPacketBufferSize)}
	}
	largeBufferPool.New = func() any {
		return &packetBuffer{Data: make([]byte, 0, protocol.MaxLargePacketBufferSize)}
	}
	coalescedBufferPool.New = func() any {
		return &packetBuffer{Data: make([]byte, 0, protocol.MaxCoalescedPacketBufferSize)}
	}
}
