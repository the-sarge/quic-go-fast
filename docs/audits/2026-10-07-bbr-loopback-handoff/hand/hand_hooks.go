package quic

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/quic-go/quic-go/internal/monotime"
)

// Measurement-only #738 hooks for the hand-off recorder (handrec.go). Like
// #734's wake hooks they run only in the fixture's sender, when #715's
// run_case_x sets TIMELINE_OUTPUT for a "-timeline" variant, and record over
// the measured window read from the sender's -config file. Output:
// send.hand.json beside the timeline. Not a maintained hook.

func init() {
	output := handOutput()
	if output == "" {
		return
	}
	var path string
	for i, a := range os.Args {
		if (a == "-config" || a == "--config") && i+1 < len(os.Args) {
			path = os.Args[i+1]
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var cfg struct {
		StartUnixNS int64 `json:"start_unix_ns"`
		WarmupMS    int64 `json:"warmup_ms"`
		MeasureMS   int64 `json:"measure_ms"`
	}
	if json.Unmarshal(b, &cfg) != nil || cfg.MeasureMS == 0 {
		return
	}
	start := cfg.StartUnixNS + cfg.WarmupMS*1e6
	handStart(filepath.Join(filepath.Dir(output), "send.hand"), start, start+cfg.MeasureMS*1e6)
}

// handOutput is #734's wakeOutput: the sender only, from TIMELINE_OUTPUT.
func handOutput() string {
	for i, a := range os.Args {
		if (a == "-role" || a == "--role") && i+1 < len(os.Args) && os.Args[i+1] == "send" {
			return os.Getenv("TIMELINE_OUTPUT")
		}
		if a == "-role=send" || a == "--role=send" {
			return os.Getenv("TIMELINE_OUTPUT")
		}
	}
	return ""
}

func handOpp(now monotime.Time) { handOpportunity(int64(now)) }

// handExit classifies how an opportunity ended, as #734's wakeExit does, and
// splits its "credit" class into a local credit refusal (the result waits on
// the credit's channel) and a full send queue.
func handExit(r emissionResult, e *packetEmission) {
	if !hand.on.Load() {
		return
	}
	var d, rate int64
	if r.stop == emissionPaced && r.deadline != 0 && r.deadline != deadlineSendImmediately {
		d = int64(r.deadline)
	}
	if e.bbr != nil {
		rate = int64(e.bbr.rate)
	}
	class := hcOther
	switch {
	case r.err != nil:
	case r.progress && r.stop == emissionNoData && !r.supplyExhausted:
		class = hcRunning // an immediate continuation
	case r.supplyExhausted || (r.stop == emissionNoData && !r.progress):
		class = hcApp
	case r.stop == emissionPaced:
		class = hcPaced
	case r.stop == emissionCongestionLimited:
		class = hcCwnd
	case r.stop == emissionQueueFull:
		class = hcQueue
		if e.bbr != nil && r.available == (<-chan struct{})(e.bbr.credit.available) {
			class = hcCredit
		}
	case r.stop == emissionHardBlocked:
		class = hcHard
	}
	handExited(class, r.progress, d, rate)
}
