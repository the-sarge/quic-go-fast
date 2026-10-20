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
	if cap(b.Data) == readSlotBufferSize {
		readSlotBufferPool.Put(b)
		return
	}
	panic("putPacketBuffer called with packet of wrong size!")
}

// readSlotBufferSize is the capacity of a read slot offered to a batch
// adapter that opted into more than one (#857). It is one byte larger than the
// packet ceiling, so a full slot means the datagram was truncated.
const readSlotBufferSize = protocol.MaxPacketBufferSize + 1

var bufferPool, largeBufferPool, coalescedBufferPool, readSlotBufferPool sync.Pool

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
// These buffers back coalesced reads, which stay with their reader while the
// datagrams are copied out. Managed readers also hand one out as a packet:
// a datagram above the large tier, and, on Darwin, FreeBSD and Linux without
// GRO, every datagram.
func getCoalescedPacketBuffer() *packetBuffer {
	buf := coalescedBufferPool.Get().(*packetBuffer)
	buf.refCount = 1
	buf.Data = buf.Data[:0]
	return buf
}

// getReadSlotBuffer returns a buffer from the read-slot tier. A datagram read
// into it is delivered in it, and its release returns it to this tier.
func getReadSlotBuffer() *packetBuffer {
	buf := readSlotBufferPool.Get().(*packetBuffer)
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
	readSlotBufferPool.New = func() any {
		return &packetBuffer{Data: make([]byte, 0, readSlotBufferSize)}
	}
}
