package ackhandler

import (
	"sync/atomic"
	"unsafe"

	"github.com/quic-go/quic-go/internal/congestion"
)

// Measurement-only #740 overlay: the explicit bytes of the two BBR bookkeeping
// structures (the outcome ring and the delivery records) and the feedback
// scratch buffer, published from the connection goroutine on the first and then
// every 256th registration. It is copied into exported build trees and is never
// part of a tracked transport source.

const (
	MemRingBytes           = iota // recoveryEvidence.recordBytes(): ring, indexes and slot lists
	MemRingEntries                // cap(outcomes)
	MemRingCount                  // occupied ring entries
	MemRingEvicted                // ring evictions so far
	MemDeliverySlotBytes          // len(slots) * sizeof(deliverySlot)
	MemDeliveryRecordBytes        // cap(recs) * sizeof(deliveryRecord)
	MemDeliveryFreeBytes          // cap(free) * 4
	MemDeliveryLive               // live delivery records
	MemScratchBytes               // cap(scratch) * sizeof(PacketInfo)
	MemRetained                   // retained deliveries (map entries)
	MemRetainedBytes              // len(retained) * sizeof(retainedDelivery), map overhead excluded
	MemUpdates                    // publications so far
	MemBBRColumns
)

var MemBBRNames = [MemBBRColumns]string{"ring_bytes", "ring_entries", "ring_count", "ring_evicted", "delivery_slot_bytes",
	"delivery_record_bytes", "delivery_free_bytes", "delivery_live", "scratch_bytes", "retained", "retained_bytes", "updates"}

var MemBBR [MemBBRColumns]atomic.Int64

var memNotes atomic.Uint64 // per process; the fixture runs one connection per endpoint, tests run many

func memNote(d *congestionDispatch) {
	if memNotes.Add(1)&255 != 1 {
		return
	}
	var ringEntries int
	if d.recovery.index != nil {
		ringEntries = cap(d.recovery.index.outcomes)
	}
	MemBBR[MemRingBytes].Store(int64(d.recovery.recordBytes()))
	MemBBR[MemRingEntries].Store(int64(ringEntries))
	MemBBR[MemRingCount].Store(int64(d.recovery.count))
	MemBBR[MemRingEvicted].Store(int64(d.recovery.evicted))
	MemBBR[MemDeliverySlotBytes].Store(int64(uintptr(len(d.packets.slots)) * unsafe.Sizeof(deliverySlot{})))
	MemBBR[MemDeliveryRecordBytes].Store(int64(uintptr(cap(d.packets.recs)) * unsafe.Sizeof(deliveryRecord{})))
	MemBBR[MemDeliveryFreeBytes].Store(int64(cap(d.packets.free) * 4))
	MemBBR[MemDeliveryLive].Store(int64(d.packets.n))
	MemBBR[MemScratchBytes].Store(int64(uintptr(cap(d.scratch)) * unsafe.Sizeof(congestion.PacketInfo{})))
	MemBBR[MemRetained].Store(int64(len(d.sampler.retained)))
	MemBBR[MemRetainedBytes].Store(int64(uintptr(len(d.sampler.retained)) * unsafe.Sizeof(retainedDelivery{})))
	MemBBR[MemUpdates].Add(1)
}
