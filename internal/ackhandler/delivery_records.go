package ackhandler

import (
	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/protocol"
)

// deliveryRecords holds the dispatch's live delivery records with map
// semantics. A PacketInfo is larger than Go's 128-byte inline map element, so a
// map allocates one object per insert; here records sit in a dense slab reused
// through a free list. The index is an open-addressing table of slab indices:
// Fibonacci hashing spreads consecutive packet numbers, and linear probing with
// backward-shift deletion leaves no tombstones. The caller's live limit bounds
// the slab; the table stays at or below 7/8 load.
type deliveryRecords struct {
	slots []deliverySlot // power-of-two length
	shift uint8          // 64 - log2(len(slots))
	recs  []deliveryRecord
	free  []int32
	n     int
}

type deliverySlot struct {
	key uint64 // packed key; zero marks an empty slot
	rec int32
}

type deliveryRecord struct {
	key  uint64 // zero while the record is free
	info congestion.PacketInfo
}

const minDeliverySlots = 16

var deliverySpaces = [...]protocol.EncryptionLevel{protocol.EncryptionInitial, protocol.EncryptionHandshake, protocol.Encryption1RTT}

// packDelivery is nonzero for every valid key: the space index occupies the low
// two bits, offset by one, under a packet number below 2^62.
func packDelivery(k congestionPacketKey) uint64 {
	return uint64(k.number)<<2 | uint64(recoverySpace(k.space)+1)
}

func unpackDelivery(v uint64) congestionPacketKey {
	return congestionPacketKey{space: deliverySpaces[v&3-1], number: protocol.PacketNumber(v >> 2)}
}

func (t *deliveryRecords) home(key uint64) int {
	return int((key * 0x9e3779b97f4a7c15) >> t.shift)
}

func (t *deliveryRecords) find(key uint64) int {
	if t.n == 0 {
		return -1
	}
	mask := len(t.slots) - 1
	for i := t.home(key); ; i = (i + 1) & mask {
		switch t.slots[i].key {
		case key:
			return i
		case 0:
			return -1
		}
	}
}

func (t *deliveryRecords) len() int { return t.n }

func (t *deliveryRecords) get(k congestionPacketKey) (congestion.PacketInfo, bool) {
	if i := t.find(packDelivery(k)); i >= 0 {
		return t.recs[t.slots[i].rec].info, true
	}
	return congestion.PacketInfo{}, false
}

func (t *deliveryRecords) has(k congestionPacketKey) bool { return t.find(packDelivery(k)) >= 0 }

// set inserts or replaces the record for k.
func (t *deliveryRecords) set(k congestionPacketKey, info congestion.PacketInfo) {
	key := packDelivery(k)
	if i := t.find(key); i >= 0 {
		t.recs[t.slots[i].rec].info = info
		return
	}
	if (t.n+1)*8 > len(t.slots)*7 {
		t.grow()
	}
	var r int32
	if n := len(t.free); n > 0 {
		r, t.free = t.free[n-1], t.free[:n-1]
		t.recs[r] = deliveryRecord{key: key, info: info}
	} else {
		r = int32(len(t.recs))
		t.recs = append(t.recs, deliveryRecord{key: key, info: info})
	}
	t.place(key, r)
	t.n++
}

func (t *deliveryRecords) place(key uint64, r int32) {
	mask := len(t.slots) - 1
	i := t.home(key)
	for t.slots[i].key != 0 {
		i = (i + 1) & mask
	}
	t.slots[i] = deliverySlot{key: key, rec: r}
}

func (t *deliveryRecords) grow() {
	size := max(minDeliverySlots, 2*len(t.slots))
	shift := 64
	for s := size; s > 1; s >>= 1 {
		shift--
	}
	t.slots, t.shift = make([]deliverySlot, size), uint8(shift)
	for r := range t.recs {
		if key := t.recs[r].key; key != 0 {
			t.place(key, int32(r))
		}
	}
}

// delete removes k and reports whether it was present.
func (t *deliveryRecords) delete(k congestionPacketKey) bool {
	_, ok := t.take(k)
	return ok
}

// take removes k and returns its record, as a map lookup followed by delete.
func (t *deliveryRecords) take(k congestionPacketKey) (congestion.PacketInfo, bool) {
	i := t.find(packDelivery(k))
	if i < 0 {
		return congestion.PacketInfo{}, false
	}
	r := t.slots[i].rec
	info := t.recs[r].info
	t.recs[r] = deliveryRecord{}
	t.remove(i, r)
	return info, true
}

// takeAppend removes k and appends its record to dst, moving it once. Only the
// free marker is cleared: a free record is overwritten whole before it is read,
// and PacketInfo holds no pointers to retain.
func (t *deliveryRecords) takeAppend(k congestionPacketKey, dst []congestion.PacketInfo) ([]congestion.PacketInfo, bool) {
	i := t.find(packDelivery(k))
	if i < 0 {
		return dst, false
	}
	r := t.slots[i].rec
	dst = append(dst, t.recs[r].info)
	t.recs[r].key = 0
	t.remove(i, r)
	return dst, true
}

// remove frees record r and empties slot i.
func (t *deliveryRecords) remove(i int, r int32) {
	t.free = append(t.free, r)
	t.n--
	// Backward-shift deletion: move each later entry of the probe run whose home
	// does not lie cyclically in (i, j] into the hole.
	mask := len(t.slots) - 1
	for j := (i + 1) & mask; t.slots[j].key != 0; j = (j + 1) & mask {
		h := t.home(t.slots[j].key)
		if (i < j && i < h && h <= j) || (j < i && (i < h || h <= j)) {
			continue
		}
		t.slots[i] = t.slots[j]
		i = j
	}
	t.slots[i] = deliverySlot{}
}

// each visits every record once. fn may delete the record it is given.
func (t *deliveryRecords) each(fn func(congestionPacketKey, congestion.PacketInfo)) {
	for r := range t.recs {
		if key := t.recs[r].key; key != 0 {
			fn(unpackDelivery(key), t.recs[r].info)
		}
	}
}
