package main

import "testing"

func TestDuplexActivityKeepsDirectionEvidence(t *testing.T) {
	a := &flowActivity{}
	a.record(0, false)
	a.record(0, true)
	before := a.snapshot()
	// Reverse response is causally later, but both flows were already active.
	if before.FocalPackets != 1 || before.CompetitorPackets != 1 || before.DirectionCompetitor[1] != 0 {
		t.Fatal(before)
	}
	a.record(1, true)
	a.record(1, false)
	after := a.snapshot()
	if after.FocalPackets != 2 || after.CompetitorPackets != 2 || after.DirectionCompetitor != [2]uint64{1, 1} {
		t.Fatal(after)
	}
}

func TestActivityCollectorJoinsAndIncludesFinalCounts(t *testing.T) {
	a := &flowActivity{}
	done := make(chan struct{})
	result := make(chan []activitySample, 1)
	go func() { result <- collectActivity(a, done) }()
	a.record(0, true)
	a.record(1, true)
	close(done)
	samples := <-result
	if len(samples) < 2 || samples[len(samples)-1].CompetitorPackets != 2 {
		t.Fatal(samples)
	}
}
