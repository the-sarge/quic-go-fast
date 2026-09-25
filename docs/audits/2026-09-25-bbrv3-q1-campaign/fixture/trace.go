package main

import (
	"context"
	"sync"
	"time"

	quic "github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/qlogwriter"
)

type traceRecord struct {
	UnixNS int64 `json:"unix_ns"`
	Event  any   `json:"event"`
}
type trace struct {
	mu          sync.Mutex
	SentECN     map[qlog.ECN]uint64 `json:"sent_ecn"`
	ReceivedECN map[qlog.ECN]uint64 `json:"received_ecn"`
	Events      []traceRecord       `json:"events"`
	lastMetrics time.Time
}

func newTrace() *trace {
	return &trace{SentECN: make(map[qlog.ECN]uint64), ReceivedECN: make(map[qlog.ECN]uint64)}
}
func (t *trace) factory(context.Context, bool, quic.ConnectionID) qlogwriter.Trace { return t }
func (t *trace) AddProducer() qlogwriter.Recorder                                  { return t }
func (t *trace) SupportsSchemas(string) bool                                       { return true }
func (t *trace) Close() error                                                      { return nil }
func (t *trace) RecordEvent(event qlogwriter.Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	var keep any
	switch e := event.(type) {
	case qlog.PacketSent:
		t.SentECN[e.ECN]++
	case *qlog.PacketSent:
		t.SentECN[e.ECN]++
	case qlog.PacketReceived:
		t.ReceivedECN[e.ECN]++
	case *qlog.PacketReceived:
		t.ReceivedECN[e.ECN]++
	case qlog.ECNStateUpdated:
		keep = e
	case *qlog.ECNStateUpdated:
		keep = *e
	case qlog.DebugEvent:
		if e.EventName == "congestion_control" {
			keep = e
		}
	case *qlog.DebugEvent:
		if e.EventName == "congestion_control" {
			keep = *e
		}
	case qlog.MetricsUpdated:
		if now.Sub(t.lastMetrics) >= time.Second {
			keep = e
			t.lastMetrics = now
		}
	case *qlog.MetricsUpdated:
		if now.Sub(t.lastMetrics) >= time.Second {
			keep = *e
			t.lastMetrics = now
		}
	}
	if keep != nil && len(t.Events) < 1024 {
		t.Events = append(t.Events, traceRecord{now.UnixNano(), keep})
	}
}
