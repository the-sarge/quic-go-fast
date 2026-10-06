package quic

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/quic-go/quic-go/internal/monotime"
)

// Measurement-only #734 hooks for the wake-path recorder (wakerec.go). They
// run only in the fixture's sender when the #715 timeline overlay is enabled,
// and record over the measured window read from the sender's -config file.
// The output files sit beside the timeline: send.wake.{trace,events,json}.
// Not a maintained hook.

func init() {
	output := wakeOutput()
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
	wakeStart(filepath.Join(filepath.Dir(output), "send.wake"), start, start+cfg.MeasureMS*1e6,
		func() int64 { return int64(monotime.Now()) })
}

// wakeOutput mirrors the #715 overlay's timelineOutput: the sender only, from
// TIMELINE_OUTPUT, which #715's run_case_x sets for "-timeline" variants. It
// does not need the overlay itself to be built in.
func wakeOutput() string {
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

func wakeArm(deadline, pacing monotime.Time, mode int64) {
	wakeArmed(int64(deadline), int64(pacing), mode)
}

func wakeWoke(c int64) { wakeWoken(c) }

func wakeOpp(now monotime.Time) { wakeOpportunity(int64(now)) }

// wakeExit records how an opportunity ended: the paced deadline the #715
// overlay treats as pending (0 otherwise), the stop reason, the #715 overlay's
// state class, and the pacing rate.
func wakeExit(r emissionResult, p *bbrSendPolicy) {
	if !wake.on.Load() {
		return
	}
	var d, rate int64
	if r.stop == emissionPaced && r.deadline != 0 && r.deadline != deadlineSendImmediately {
		d = int64(r.deadline)
	}
	if p != nil {
		rate = int64(p.rate)
	}
	class := int64(wsOther)
	switch {
	case r.err != nil:
	case r.progress && r.stop == emissionNoData && !r.supplyExhausted:
		class = wsImmediate
	case r.supplyExhausted || (r.stop == emissionNoData && !r.progress):
		class = wsApp
	case r.stop == emissionPaced:
		class = wsPaced
	case r.stop == emissionCongestionLimited:
		class = wsCwnd
	case r.stop == emissionQueueFull:
		class = wsCredit
	case r.stop == emissionHardBlocked:
		class = wsHard
	}
	wakeExited(d, int64(r.stop), class, rate)
}
