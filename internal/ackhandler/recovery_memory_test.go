package ackhandler

import (
	"encoding/json"
	"os"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
)

// The named C2 memory observable: live heap retained by the recovery ledger and
// by retained delivery evidence at matched, full occupancy, measured for the
// frozen reference and the candidate in one process. Explicit structure
// accounting is reported beside it. Peak RSS is an endpoint quantity and is not
// measured here. Set BBR_MEMORY_OUT to run and record the observation.

type memoryObservation struct {
	Structure  string
	Occupancy  int
	Frozen     memoryAccount
	Candidate  memoryAccount
	LiveChange float64 // candidate live heap relative to frozen
}

type memoryAccount struct {
	LiveHeap  uint64            // forced-GC heap delta while the structure is reachable
	Accounted map[string]uint64 // explicit structure accounting
}

func liveHeap(build func() any) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&before)
	v := build()
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(v)
	return after.HeapAlloc - before.HeapAlloc
}

// ledgerRegistrations reaches steady state: the ring is full and has wrapped
// twice, so the frozen key map carries its steady-state insert/delete history.
const ledgerRegistrations = 3 * maxRecoveryOutcomes

func memoryInfo(pn protocol.PacketNumber) congestion.PacketInfo {
	return congestion.PacketInfo{
		Space: protocol.Encryption1RTT, EncryptionLevel: protocol.Encryption1RTT, PacketNumber: pn, Ordinal: uint64(pn) + 1,
		SendTime: workBaseTime.Add(time.Duration(pn) * time.Microsecond), RegistrationValid: true, AckEliciting: true, InFlight: true, Length: 1200,
	}
}

var workBaseTime = monotime.Time(time.Hour)

func TestRecoveryMemoryObservable(t *testing.T) {
	out := os.Getenv("BBR_MEMORY_OUT")
	if out == "" {
		t.Skip("set BBR_MEMORY_OUT to record the memory observable")
	}
	var observations []memoryObservation

	var frozenLedger *frozenRecovery
	frozenLive := liveHeap(func() any {
		frozenLedger = &frozenRecovery{measured: true}
		for pn := range protocol.PacketNumber(ledgerRegistrations) {
			frozenLedger.sent(memoryInfo(pn), 0)
		}
		return frozenLedger
	})
	var candidateLedger *recoveryEvidence
	candidateLive := liveHeap(func() any {
		candidateLedger = &recoveryEvidence{measured: true}
		for pn := range protocol.PacketNumber(ledgerRegistrations) {
			candidateLedger.sent(memoryInfo(pn), 0)
		}
		return candidateLedger
	})
	require.Equal(t, maxRecoveryOutcomes, frozenLedger.count)
	require.Equal(t, maxRecoveryOutcomes, candidateLedger.count)
	frozenRing := uint64(cap(frozenLedger.outcomes)) * uint64(unsafe.Sizeof(frozenOutcome{}))
	x := candidateLedger.index
	var deques uint64
	for i := range x.spaces {
		deques += uint64(cap(x.spaces[i].slots)) * 2
	}
	observations = append(observations, memoryObservation{
		Structure: "recovery ledger", Occupancy: maxRecoveryOutcomes,
		Frozen: memoryAccount{LiveHeap: frozenLive, Accounted: map[string]uint64{
			"outcome ring":                    frozenRing,
			"key map (live heap minus ring)":  frozenLive - frozenRing,
			"DeliveryStats.RecordBytes share": frozenRing,
		}},
		Candidate: memoryAccount{LiveHeap: candidateLive, Accounted: map[string]uint64{
			"outcome ring":                    uint64(cap(x.outcomes)) * uint64(unsafe.Sizeof(recoveryOutcome{})),
			"slot sets":                       uint64(unsafe.Sizeof(x.pending) + unsafe.Sizeof(x.eligible) + unsafe.Sizeof(x.ptoPending) + 3*unsafe.Sizeof(slotSet{})),
			"run duration tree":               uint64(unsafe.Sizeof(x.spans.tree)),
			"per-space slot lists":            deques,
			"DeliveryStats.RecordBytes share": uint64(candidateLedger.recordBytes()),
		}},
		LiveChange: float64(candidateLive)/float64(frozenLive) - 1,
	})

	pto := 10 * time.Second
	var frozenEvidence *frozenDispatch
	frozenEvidenceLive := liveHeap(func() any {
		d := newFrozenDispatch()
		for pn := range protocol.PacketNumber(maxDeliveryRetained) {
			d.packets[congestionKey(protocol.Encryption1RTT, pn)] = memoryInfo(pn)
		}
		for pn := range protocol.PacketNumber(maxDeliveryRetained) {
			d.retire(congestionKey(protocol.Encryption1RTT, pn), workBaseTime, pto, deliveryRetiredLoss)
		}
		d.packets = nil
		frozenEvidence = d
		return d
	})
	var candidateRetained *congestionDispatch
	candidateRetainedLive := liveHeap(func() any {
		d := &congestionDispatch{}
		for pn := range protocol.PacketNumber(maxDeliveryRetained) {
			d.packets.set(congestionKey(protocol.Encryption1RTT, pn), memoryInfo(pn))
		}
		for pn := range protocol.PacketNumber(maxDeliveryRetained) {
			d.retire(congestionKey(protocol.Encryption1RTT, pn), workBaseTime, pto, deliveryRetiredLoss)
		}
		d.packets = deliveryRecords{}
		candidateRetained = d
		return d
	})
	require.Len(t, frozenEvidence.sampler.retained, maxDeliveryRetained)
	require.Len(t, candidateRetained.sampler.retained, maxDeliveryRetained)
	s := &candidateRetained.sampler
	observations = append(observations, memoryObservation{
		Structure: "retained delivery evidence", Occupancy: maxDeliveryRetained,
		Frozen: memoryAccount{LiveHeap: frozenEvidenceLive, Accounted: map[string]uint64{
			"records":      uint64(len(frozenEvidence.sampler.retained)) * uint64(unsafe.Sizeof(frozenRetained{})),
			"ordinal heap": uint64(cap(frozenEvidence.sampler.order)) * 8,
		}},
		Candidate: memoryAccount{LiveHeap: candidateRetainedLive, Accounted: map[string]uint64{
			"records (with index links)": uint64(len(s.retained)) * uint64(unsafe.Sizeof(retainedDelivery{})),
			"ordinal heap":               uint64(cap(s.order)) * 8,
			"expiry heap":                uint64(cap(s.expiry)) * 8,
		}},
		LiveChange: float64(candidateRetainedLive)/float64(frozenEvidenceLive) - 1,
	})
	runtime.KeepAlive(frozenLedger)
	runtime.KeepAlive(candidateLedger)
	runtime.KeepAlive(frozenEvidence)
	runtime.KeepAlive(candidateRetained)

	for _, o := range observations {
		t.Logf("%s at %d: frozen live %d B, candidate live %d B (%+.1f%%); accounted frozen %v candidate %v",
			o.Structure, o.Occupancy, o.Frozen.LiveHeap, o.Candidate.LiveHeap, 100*o.LiveChange, o.Frozen.Accounted, o.Candidate.Accounted)
	}
	data, err := json.MarshalIndent(observations, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(out, data, 0o644))
}
