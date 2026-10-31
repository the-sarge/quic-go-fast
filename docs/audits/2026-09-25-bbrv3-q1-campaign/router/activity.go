package main

import (
	"sync"
	"time"
)

// A flow is duplex. One synchronized snapshot owns both directions, so receipt
// consumers never sum counters observed at different instants or require an ACK
// before the packet causing it can traverse the modeled path.
type activitySample struct {
	UnixNS                              int64
	FocalPackets, CompetitorPackets     uint64
	DirectionFocal, DirectionCompetitor [2]uint64
}

type flowActivity struct {
	mu                sync.Mutex
	focal, competitor [2]uint64
}

func (a *flowActivity) record(direction int, competitor bool) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if competitor {
		a.competitor[direction]++
	} else {
		a.focal[direction]++
	}
}

func (a *flowActivity) snapshot() activitySample {
	a.mu.Lock()
	defer a.mu.Unlock()
	return activitySample{time.Now().UnixNano(), a.focal[0] + a.focal[1], a.competitor[0] + a.competitor[1], a.focal, a.competitor}
}

// done is closed only after both forwarding owners have joined. The final
// sample therefore includes every admitted packet, independently of ctx order.
func collectActivity(a *flowActivity, done <-chan struct{}) []activitySample {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	samples := []activitySample{a.snapshot()}
	for {
		select {
		case <-done:
			return append(samples, a.snapshot())
		case <-ticker.C:
			samples = append(samples, a.snapshot())
		}
	}
}
