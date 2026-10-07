package congestion

import "time"

// Measurement-only #715 overlay: the BBR model's state after each feedback
// event, at most one row per 10 ms plus every phase change. Owned by the
// connection goroutine; written out by the root package's emission overlay.
// Not a maintained hook.
var TimelineModelColumns = []string{"unix_ns", "phase", "bandwidth", "rate", "window", "inflight_long", "inflight_short",
	"bandwidth_short", "min_rtt_ns", "prior_in_flight", "round", "size"}

var TimelineModel [][12]int64

// TimelineEnabled is set by the root overlay when an output path exists.
var TimelineEnabled bool

var timelineLastNS int64
var timelineLastPhase = -1

func clampInt64(v uint64) int64 {
	if v > 1<<62 {
		return 1 << 62
	}
	return int64(v)
}

func timelineFeedback(b *BBRSender, priorInFlight int64) {
	if !TimelineEnabled || len(TimelineModel) >= 1<<17 {
		return
	}
	now := time.Now().UnixNano()
	if now-timelineLastNS < int64(10*time.Millisecond) && int(b.phase) == timelineLastPhase {
		return
	}
	timelineLastNS, timelineLastPhase = now, int(b.phase)
	TimelineModel = append(TimelineModel, [12]int64{now, int64(b.phase), clampInt64(b.bandwidth), clampInt64(b.rate), int64(b.window),
		int64(b.inflightLong), int64(b.inflightShort), clampInt64(b.bandwidthShort), int64(b.minimumRTT), priorInFlight,
		clampInt64(b.round), int64(b.size)})
}
