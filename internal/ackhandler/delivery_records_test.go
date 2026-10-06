package ackhandler

import (
	"math/rand/v2"
	"runtime"
	"testing"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
)

// asMap is the oracle view of the records: the map the dispatch used to hold.
func (t *deliveryRecords) asMap() map[congestionPacketKey]congestion.PacketInfo {
	m := make(map[congestionPacketKey]congestion.PacketInfo, t.n)
	t.each(func(k congestionPacketKey, p congestion.PacketInfo) { m[k] = p })
	return m
}

// checkDeliveryRecords recomputes the table's invariants: every slot reaches its
// record from its home without crossing an empty slot, slab and table agree, and
// load stays at or below 7/8.
func checkDeliveryRecords(t testing.TB, r *deliveryRecords) {
	t.Helper()
	live := 0
	for i, s := range r.slots {
		if s.key == 0 {
			continue
		}
		live++
		require.Equal(t, s.key, r.recs[s.rec].key, "slot %d record", i)
		mask := len(r.slots) - 1
		for j := r.home(s.key); j != i; j = (j + 1) & mask {
			require.NotZero(t, r.slots[j].key, "probe run from home to slot %d is broken", i)
		}
	}
	require.Equal(t, r.n, live)
	free := 0
	for _, rec := range r.recs {
		if rec.key == 0 {
			free++
		}
	}
	require.Equal(t, len(r.free), free)
	require.LessOrEqual(t, r.n*8, len(r.slots)*7)
}

func deliveryInfo(k congestionPacketKey, ordinal uint64) congestion.PacketInfo {
	return congestion.PacketInfo{Space: k.space, EncryptionLevel: k.space, PacketNumber: k.number, Ordinal: ordinal, Length: protocol.ByteCount(ordinal%1400 + 1), AckEliciting: ordinal%5 != 0}
}

// The records behave as the map they replace under the dispatch's operations:
// monotonic inserts per space with skips, lookups of present and absent keys,
// ACK-range and loss deletes, deletes during a space-discard iteration and
// resets, including packet numbers at both ends of the valid range.
func TestDeliveryRecordsMapEquivalence(t *testing.T) {
	seeds := 400
	if testing.Short() {
		seeds = 40
	}
	var ops, maxLive int
	for seed := range seeds {
		rng := rand.New(rand.NewPCG(uint64(seed), 0x714))
		// #735 lead 3 draws from its own stream, so the histories stay #714's.
		pick := rand.New(rand.NewPCG(uint64(seed), 0x735))
		var got deliveryRecords
		want := map[congestionPacketKey]congestion.PacketInfo{}
		next := [3]protocol.PacketNumber{0, 0, protocol.PacketNumber(rng.IntN(3)) * (1<<62 - 1<<20)}
		if seed%11 == 0 {
			next[2] = 1<<62 - 3000 // reach the top of the packet-number range
		}
		var ordinal uint64
		live := func() []congestionPacketKey {
			keys := make([]congestionPacketKey, 0, len(want))
			for k := range want {
				keys = append(keys, k)
			}
			return keys
		}
		steps := 200 + rng.IntN(3000)
		// The insert share varies by seed, so some histories grow large tables.
		ins := 55 + seed%5*10
		rest := 100 - ins
		del, look, discard := ins+rest*30/45, ins+rest*40/45, ins+rest*43/45
		for range steps {
			ops++
			switch x := rng.IntN(100); {
			case x < ins: // registrations, occasionally skipping numbers
				s := 2
				if rng.IntN(10) == 0 {
					s = rng.IntN(3)
				}
				if next[s] >= 1<<62-1 {
					continue
				}
				if rng.IntN(20) == 0 {
					next[s] += protocol.PacketNumber(1 + rng.IntN(3))
				}
				k := congestionPacketKey{space: deliverySpaces[s], number: next[s]}
				next[s]++
				ordinal++
				got.set(k, deliveryInfo(k, ordinal))
				want[k] = deliveryInfo(k, ordinal)
			case x < del: // an ACK range or loss: delete a run of keys, present or not
				s := deliverySpaces[2]
				if rng.IntN(10) == 0 {
					s = deliverySpaces[rng.IntN(3)]
				}
				hi := next[recoverySpace(s)] - protocol.PacketNumber(rng.IntN(64))
				lo := hi - protocol.PacketNumber(rng.IntN(32))
				for pn := lo; pn <= hi; pn++ {
					k := congestionPacketKey{space: s, number: pn}
					w, ok := want[k]
					if pick.IntN(2) == 0 {
						// #735 lead 3: the ACK path moves the record into the event list.
						dst := []congestion.PacketInfo{{Ordinal: 1}}
						dst, gok := got.takeAppend(k, dst)
						require.Equal(t, ok, gok)
						if ok {
							require.Equal(t, []congestion.PacketInfo{{Ordinal: 1}, w}, dst)
						} else {
							require.Len(t, dst, 1)
						}
					} else {
						require.Equal(t, ok, got.delete(k))
					}
					delete(want, k)
				}
			case x < look: // lookups
				for range 8 {
					k := congestionPacketKey{space: deliverySpaces[rng.IntN(3)], number: protocol.PacketNumber(rng.Int64N(int64(next[2] + 2)))}
					if keys := live(); len(keys) > 0 && rng.IntN(2) == 0 {
						k = keys[rng.IntN(len(keys))]
					}
					w, wok := want[k]
					g, gok := got.get(k)
					require.Equal(t, wok, gok)
					require.Equal(t, w, g)
					require.Equal(t, wok, got.has(k))
				}
			case x < discard: // space discard: delete during iteration
				s := deliverySpaces[rng.IntN(2)]
				got.each(func(k congestionPacketKey, p congestion.PacketInfo) {
					if p.EncryptionLevel == s {
						require.True(t, got.delete(k))
					}
				})
				for k, p := range want {
					if p.EncryptionLevel == s {
						delete(want, k)
					}
				}
			default: // path or Retry reset
				got = deliveryRecords{}
				want = map[congestionPacketKey]congestion.PacketInfo{}
			}
			require.Equal(t, len(want), got.len())
			maxLive = max(maxLive, got.len())
			if rng.IntN(50) == 0 {
				checkDeliveryRecords(t, &got)
				require.Equal(t, want, got.asMap())
			}
		}
		checkDeliveryRecords(t, &got)
		require.Equal(t, want, got.asMap())
	}
	t.Logf("seeds=%d ops=%d max-live=%d", seeds, ops, maxLive)
}

// At the dispatch's live limit the table holds 32,768 slots and the slab at most
// the limit's records; draining and refilling reuses both without growth.
func TestDeliveryRecordsBound(t *testing.T) {
	var r deliveryRecords
	for pn := range protocol.PacketNumber(maxDeliveryLive) {
		k := congestionKey(protocol.Encryption1RTT, pn)
		r.set(k, deliveryInfo(k, uint64(pn)+1))
	}
	checkDeliveryRecords(t, &r)
	require.Len(t, r.slots, 32768)
	require.Len(t, r.recs, maxDeliveryLive)
	slots, recs := &r.slots[0], &r.recs[0]
	for round := range 3 {
		base := protocol.PacketNumber(maxDeliveryLive * (round + 1))
		for pn := range protocol.PacketNumber(maxDeliveryLive) {
			require.True(t, r.delete(congestionKey(protocol.Encryption1RTT, base-maxDeliveryLive+pn)))
			k := congestionKey(protocol.Encryption1RTT, base+pn)
			r.set(k, deliveryInfo(k, uint64(base+pn)+1))
		}
		checkDeliveryRecords(t, &r)
		require.Equal(t, maxDeliveryLive, r.len())
	}
	require.Same(t, slots, &r.slots[0], "steady occupancy never regrows the table")
	require.Same(t, recs, &r.recs[0], "steady occupancy reuses the slab")
	allocs := testing.AllocsPerRun(100, func() {
		k := congestionKey(protocol.Encryption1RTT, 10*maxDeliveryLive)
		r.set(k, deliveryInfo(k, 1))
		r.delete(k)
	})
	require.Zero(t, allocs)
}

func deliveryLiveHeap(build func() any) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	v := build()
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(v)
	return after.HeapAlloc - before.HeapAlloc
}

// Reported, not asserted: live heap of the replaced map and of the records at a
// typical S5 window and at the live limit, after steady insert/delete churn.
func TestDeliveryRecordsMemoryReport(t *testing.T) {
	for _, live := range []int{2000, maxDeliveryLive} {
		mapHeap := deliveryLiveHeap(func() any {
			m := map[congestionPacketKey]congestion.PacketInfo{}
			for pn := range protocol.PacketNumber(3 * live) {
				k := congestionKey(protocol.Encryption1RTT, pn)
				m[k] = deliveryInfo(k, 1)
				if pn >= protocol.PacketNumber(live) {
					delete(m, congestionKey(protocol.Encryption1RTT, pn-protocol.PacketNumber(live)))
				}
			}
			return m
		})
		tableHeap := deliveryLiveHeap(func() any {
			r := &deliveryRecords{}
			for pn := range protocol.PacketNumber(3 * live) {
				k := congestionKey(protocol.Encryption1RTT, pn)
				r.set(k, deliveryInfo(k, 1))
				if pn >= protocol.PacketNumber(live) {
					r.delete(congestionKey(protocol.Encryption1RTT, pn-protocol.PacketNumber(live)))
				}
			}
			return r
		})
		t.Logf("live=%d map=%d B records=%d B (%.2fx)", live, mapHeap, tableHeap, float64(tableHeap)/float64(mapHeap))
	}
}
