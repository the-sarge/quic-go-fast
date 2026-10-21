//go:build bbrworkcount

package congestion

import "time"

// Demonstration-only C4 engagement counters, owned by the connection goroutine.
var C4WorkNames = [...]string{
	"undo_restore_startup", "undo_return_startup_after_probertt", "undo_restart_refill",
	"min_rtt_lowered", "round_start_rejected_rate",
	"startup_loss_exit", "startup_loss_exit_quantization_differs",
	"undo_exited_restore_eligible", "loss_exit_noted",
}

var C4Work [len(C4WorkNames)]uint64

func c4Count(i int) { C4Work[i]++ }

// Phase transitions observed after each feedback event: [unix ns, phase].
// Bounded; the demonstration correlates them with relay queue samples.
var C4Phases [][2]int64

var c4LastPhase = -1

func c4Phase(p bbrPhase) {
	if int(p) != c4LastPhase && len(C4Phases) < 1<<16 {
		c4LastPhase = int(p)
		C4Phases = append(C4Phases, [2]int64{time.Now().UnixNano(), int64(p)})
	}
}
