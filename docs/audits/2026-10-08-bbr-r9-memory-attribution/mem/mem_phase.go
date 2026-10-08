package congestion

import (
	"sync"
	"sync/atomic"
	"time"
)

// Measurement-only #740 overlay: BBR phase transitions observed after each
// feedback event, [unix ns, phase], with the semantics of the counting
// overlay's c4Phase (#711). The log is a fixed array in BSS, so only the pages
// it writes become resident; nothing grows or is marshalled while the
// connection runs. The fixture runs one connection per endpoint; tests run
// many, so the last phase is atomic and rows are appended under a lock taken
// only on a phase change. The count is published after each row. Not a
// maintained hook.

var MemPhaseLog [1 << 16][2]int64

var MemPhaseCount atomic.Int64

var (
	memLastPhase atomic.Int64
	memPhaseMu   sync.Mutex
)

func init() { memLastPhase.Store(-1) }

func memPhase(p bbrPhase) {
	if memLastPhase.Load() == int64(p) {
		return
	}
	memPhaseMu.Lock()
	defer memPhaseMu.Unlock()
	n := MemPhaseCount.Load()
	if memLastPhase.Swap(int64(p)) != int64(p) && n < int64(len(MemPhaseLog)) {
		MemPhaseLog[n] = [2]int64{time.Now().UnixNano(), int64(p)}
		MemPhaseCount.Store(n + 1)
	}
}
