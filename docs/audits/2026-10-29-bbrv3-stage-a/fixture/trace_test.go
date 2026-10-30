package main

import (
	"encoding/json"
	"github.com/quic-go/quic-go/qlog"
	"testing"
	"time"
)

func TestSparseTransportMetricsRetainKnownWindow(t *testing.T) {
	tr := newTrace()
	tr.RecordEvent(qlog.MetricsUpdated{CongestionWindow: 64000, SmoothedRTT: 20 * time.Millisecond})
	tr.lastMetrics = time.Now().Add(-2 * time.Second)
	tr.RecordEvent(qlog.MetricsUpdated{BytesInFlight: 1200})
	b, err := json.Marshal(tr.Events[len(tr.Events)-1].Event)
	if err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err = json.Unmarshal(b, &event); err != nil {
		t.Fatal(err)
	}
	if event["CongestionWindow"] != float64(64000) || event["SmoothedRTT"] != float64(20000000) {
		t.Fatalf("sparse update lost known metrics: %s", b)
	}
}
