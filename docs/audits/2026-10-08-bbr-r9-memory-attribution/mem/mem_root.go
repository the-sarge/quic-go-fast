package quic

import (
	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/congestion"
)

// Measurement-only #740 overlay: exported reads of the BBR structure accounting
// and the phase log for the fixture's memory sampler. Not a maintained hook.

func MemBBRNames() []string { return ackhandler.MemBBRNames[:] }

// MemBBR appends the latest published structure accounting to dst.
func MemBBR(dst []int64) []int64 {
	for i := range ackhandler.MemBBR {
		dst = append(dst, ackhandler.MemBBR[i].Load())
	}
	return dst
}

// MemPhases returns the phase transitions published so far.
func MemPhases() [][2]int64 {
	return congestion.MemPhaseLog[:congestion.MemPhaseCount.Load()]
}
