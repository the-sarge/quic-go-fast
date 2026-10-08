package quic

// Measurement-only #740 overlay for the frozen Reno reference, which never
// selects BBR here: the same exported reads as mem_root.go, with no BBR
// structure to account. Not a maintained hook.

var memBBRNames = []string{"ring_bytes", "ring_entries", "ring_count", "ring_evicted", "delivery_slot_bytes",
	"delivery_record_bytes", "delivery_free_bytes", "delivery_live", "scratch_bytes", "retained", "retained_bytes", "updates"}

func MemBBRNames() []string { return memBBRNames }

var memBBRZero = make([]int64, len(memBBRNames))

func MemBBR(dst []int64) []int64 { return append(dst, memBBRZero...) }

func MemPhases() [][2]int64 { return nil }
