package main

import (
	"testing"
	"time"
)

func TestGridCastCompetitorContract(t *testing.T) {
	r := Run{ID: "gate", Controller: "reno", Workload: "stream", StartUnixNS: time.Now().UnixNano(), WarmupMS: 60000, MeasureMS: 60000, PayloadBytes: 16384, GridCastGate: true}
	if err := r.validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Run){func(r *Run) { r.Controller = "bbrv3" }, func(r *Run) { r.WarmupMS = 0 }, func(r *Run) { r.MeasureMS = 300000 }, func(r *Run) { r.BulkPauseEndMS = 270000 }, func(r *Run) { r.Workload = "datagram" }} {
		changed := r
		mutate(&changed)
		if changed.validate() == nil {
			t.Fatal("accepted different competitor contract")
		}
	}
}
