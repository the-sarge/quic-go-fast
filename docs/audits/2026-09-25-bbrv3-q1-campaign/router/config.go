package main

import (
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"time"
)

type side struct {
	Interface  string
	PeerMAC    string
	Focal      string
	Competitor string
}
type config struct {
	A, B            side
	Scenario        string
	StartUnixNS     int64
	MeasureSeconds  int
	LifetimeSeconds int // GridCast uses the owner clock, with no export tail.
	AttemptClock    string
	Phase           int
	Seed            uint64
}

func validateConfig(c config, now time.Time) error {
	gate := c.Scenario == "gridcast-loss" || c.Scenario == "gridcast-coexistence"
	limit := 300
	if gate {
		limit = 600
	}
	if c.MeasureSeconds < 1 || c.MeasureSeconds > limit || time.Unix(0, c.StartUnixNS).Sub(now) > 2*time.Minute {
		return fmt.Errorf("invalid bounded configuration")
	}
	if gate {
		if c.LifetimeSeconds != 600 || c.MeasureSeconds != 600 || c.Phase != 0 || c.StartUnixNS != 0 || !filepath.IsAbs(c.AttemptClock) {
			return fmt.Errorf("GridCast requires an absolute owner clock, fixed 600 seconds, phase/start zero")
		}
	} else if c.LifetimeSeconds != 0 || c.AttemptClock != "" {
		return fmt.Errorf("lifetime override is only for GridCast profiles")
	}
	return nil
}

// A shared Mac address cannot distinguish flows; the distinct Linux IP does.
func isCompetitor(src, dst, focalSrc, focalDst, competitorSrc, competitorDst net.IP) bool {
	return (competitorSrc != nil && !competitorSrc.Equal(focalSrc) && src.Equal(competitorSrc)) || (competitorDst != nil && !competitorDst.Equal(focalDst) && dst.Equal(competitorDst))
}

type attemptClock struct {
	StartMonotonicNS    int64 `json:"start_monotonic_ns"`
	DeadlineMonotonicNS int64 `json:"deadline_monotonic_ns"`
	StartUnixNS         int64 `json:"start_unix_ns"`
	DeadlineUnixNS      int64 `json:"deadline_unix_ns"`
}

func parseAttemptClock(data []byte) (attemptClock, error) {
	var c attemptClock
	if err := json.Unmarshal(data, &c); err != nil {
		return c, err
	}
	if c.StartMonotonicNS <= 0 || c.StartUnixNS <= 0 || c.DeadlineMonotonicNS-c.StartMonotonicNS != int64(600*time.Second) || c.DeadlineUnixNS-c.StartUnixNS != int64(600*time.Second) {
		return c, fmt.Errorf("invalid sender-owned attempt clock")
	}
	return c, nil
}

// Cancellation is not a failed socket submission. Keep historical receipts
// unchanged; only the sender-deadline gate separates intentional discards.
func cancellationCounts(gate bool) (sendErrors, discards uint64) {
	if gate {
		return 0, 1
	}
	return 1, 0
}
