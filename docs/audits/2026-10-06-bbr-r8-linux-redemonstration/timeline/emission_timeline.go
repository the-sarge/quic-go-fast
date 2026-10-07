package quic

import (
	"encoding/json"
	"os"
	"time"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
)

// Measurement-only #715 overlay: how each BBR send opportunity ends, how long
// the sender stays in each resulting state, pacing lateness, and bytes handed
// off, in 10 ms buckets. Owned by the connection goroutine, which also writes
// the JSON snapshot (with the model rows) every 500 ms. Not a maintained hook.

// Bucket columns. State times are nanoseconds; the state between two
// opportunities is the stop reason of the earlier one.
var timelineEmitColumns = []string{"emit_ns", "immediate_ns", "paced_ns", "cwnd_ns", "credit_ns", "hard_ns", "app_ns", "other_ns",
	"bytes", "packets", "late_sum_ns", "late_count", "late_max_ns", "opportunities"}

const (
	tlEmit = iota
	tlImmediate
	tlPaced
	tlCwnd
	tlCredit
	tlHard
	tlApp
	tlOther
	tlBytes
	tlPackets
	tlLateSum
	tlLateCount
	tlLateMax
	tlOpportunities
	tlColumns
)

const timelineBucketNS = int64(10 * time.Millisecond)

var timeline = struct {
	output     string
	baseNS     int64
	buckets    [][tlColumns]int64
	lastExitNS int64
	lastState  int
	pending    monotime.Time
	nextDumpNS int64
	entryNS    int64
	dumpErrors int
}{output: timelineOutput(), lastState: -1}

// timelineOutput enables the overlay only in the fixture's sender process, so
// one environment variable can serve both endpoints.
func timelineOutput() string {
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

func init() {
	congestion.TimelineEnabled = timeline.output != ""
}

func timelineBucket(ns int64) *[tlColumns]int64 {
	i := (ns - timeline.baseNS) / timelineBucketNS
	if i < 0 || i >= 1<<14 {
		return nil
	}
	for int64(len(timeline.buckets)) <= i {
		timeline.buckets = append(timeline.buckets, [tlColumns]int64{})
	}
	return &timeline.buckets[i]
}

// timelineSpan adds the interval [from, to) to column c, split across buckets.
func timelineSpan(c int, from, to int64) {
	for from < to {
		end := min(to, timeline.baseNS+((from-timeline.baseNS)/timelineBucketNS+1)*timelineBucketNS)
		if b := timelineBucket(from); b != nil {
			b[c] += end - from
		}
		from = end
	}
}

func timelineEnter(now monotime.Time) {
	if timeline.output == "" {
		return
	}
	// Spans use the wall clock at entry; lateness uses the pacer's own clock
	// (the opportunity's now) against the deadline it returned.
	ns := time.Now().UnixNano()
	if timeline.baseNS == 0 {
		timeline.baseNS = ns - ns%timelineBucketNS
	}
	if timeline.lastState >= 0 {
		timelineSpan(timeline.lastState, timeline.lastExitNS, ns)
	}
	if timeline.pending != 0 && !now.Before(timeline.pending) {
		late := int64(now.Sub(timeline.pending))
		if b := timelineBucket(ns); b != nil {
			b[tlLateSum] += late
			b[tlLateCount]++
			b[tlLateMax] = max(b[tlLateMax], late)
		}
		timeline.pending = 0
	}
	timeline.entryNS = ns
}

func timelineExit(r emissionResult) {
	if timeline.output == "" || timeline.baseNS == 0 {
		return
	}
	ns := time.Now().UnixNano()
	timelineSpan(tlEmit, timeline.entryNS, ns)
	if b := timelineBucket(timeline.entryNS); b != nil {
		b[tlOpportunities]++
	}
	state := tlOther
	switch {
	case r.err != nil:
		state = tlOther
	case r.progress && r.stop == emissionNoData && !r.supplyExhausted:
		state = tlImmediate
	case r.supplyExhausted || (r.stop == emissionNoData && !r.progress):
		state = tlApp
	case r.stop == emissionPaced:
		state = tlPaced
	case r.stop == emissionCongestionLimited:
		state = tlCwnd
	case r.stop == emissionQueueFull:
		state = tlCredit
	case r.stop == emissionHardBlocked:
		state = tlHard
	}
	if r.stop == emissionPaced && r.deadline != 0 && r.deadline != deadlineSendImmediately {
		timeline.pending = r.deadline
	} else {
		timeline.pending = 0
	}
	timeline.lastState, timeline.lastExitNS = state, ns
	if ns >= timeline.nextDumpNS {
		timeline.nextDumpNS = ns + int64(500*time.Millisecond)
		timelineDump(ns)
	}
}

func timelineSent(bytes, gso int) {
	if timeline.output == "" || timeline.baseNS == 0 {
		return
	}
	b := timelineBucket(time.Now().UnixNano())
	if b == nil {
		return
	}
	b[tlBytes] += int64(bytes)
	if gso > 0 {
		b[tlPackets] += int64((bytes + gso - 1) / gso)
	} else {
		b[tlPackets]++
	}
}

func timelineDump(ns int64) {
	data, err := json.Marshal(map[string]any{
		"dumped_unix_ns": ns, "base_unix_ns": timeline.baseNS, "bucket_ns": timelineBucketNS,
		"emit_columns": timelineEmitColumns, "emit": timeline.buckets,
		"model_columns": congestion.TimelineModelColumns, "model": congestion.TimelineModel,
		"dump_errors": timeline.dumpErrors,
	})
	if err == nil {
		tmp := timeline.output + ".tmp"
		if err = os.WriteFile(tmp, data, 0o644); err == nil {
			err = os.Rename(tmp, timeline.output)
		}
	}
	if err != nil {
		timeline.dumpErrors++
	}
}
