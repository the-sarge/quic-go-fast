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
	UnixNS int64  `json:"unix_ns"`
	Kind   string `json:"kind"`
	Event  any    `json:"event"`
}
type metricSnapshot struct {
	CongestionWindow                                 int
	MinRTT, SmoothedRTT, LatestRTT                   time.Duration
	MaxReportedFlightBytes, MaxReportedFlightPackets int
	FlowControlBlockedFrames, LostPackets            uint64
}
type trace struct {
	mu                       sync.Mutex
	SentECN                  map[qlog.ECN]uint64 `json:"sent_ecn"`
	ReceivedECN              map[qlog.ECN]uint64 `json:"received_ecn"`
	Events                   []traceRecord       `json:"events"`
	lastMetrics              time.Time
	metrics                  metricSnapshot
	FlowControlBlockedFrames uint64 `json:"flow_control_blocked_frames"`
	LostPackets              uint64 `json:"lost_packets"`
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
		t.countFrames(e.Frames)
	case *qlog.PacketSent:
		t.SentECN[e.ECN]++
		t.countFrames(e.Frames)
	case qlog.PacketReceived:
		t.ReceivedECN[e.ECN]++
	case *qlog.PacketReceived:
		t.ReceivedECN[e.ECN]++
	case qlog.PacketLost, *qlog.PacketLost:
		t.LostPackets++
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
		keep = t.observeMetrics(e, now)
	case *qlog.MetricsUpdated:
		keep = t.observeMetrics(*e, now)
	}
	if keep != nil && len(t.Events) < 1024 {
		t.Events = append(t.Events, traceRecord{now.UnixNano(), event.Name(), keep})
	}
}

func (t *trace) countFrames(frames []qlog.Frame) {
	for _, f := range frames {
		switch f.Frame.(type) {
		case *qlog.DataBlockedFrame, *qlog.StreamDataBlockedFrame:
			t.FlowControlBlockedFrames++
		}
	}
}
func (t *trace) observeMetrics(e qlog.MetricsUpdated, now time.Time) any {
	// These qlog fields are sparse updates. A zero means omitted, so retain known
	// positive window/RTT values. Flight fields report an interval peak, not a
	// claim that an omitted zero means the connection currently has no flight.
	if e.CongestionWindow > 0 {
		t.metrics.CongestionWindow = e.CongestionWindow
	}
	if e.MinRTT > 0 {
		t.metrics.MinRTT = e.MinRTT
	}
	if e.SmoothedRTT > 0 {
		t.metrics.SmoothedRTT = e.SmoothedRTT
	}
	if e.LatestRTT > 0 {
		t.metrics.LatestRTT = e.LatestRTT
	}
	t.metrics.MaxReportedFlightBytes = max(t.metrics.MaxReportedFlightBytes, e.BytesInFlight)
	t.metrics.MaxReportedFlightPackets = max(t.metrics.MaxReportedFlightPackets, e.PacketsInFlight)
	if now.Sub(t.lastMetrics) < time.Second {
		return nil
	}
	t.lastMetrics = now
	t.metrics.FlowControlBlockedFrames = t.FlowControlBlockedFrames
	t.metrics.LostPackets = t.LostPackets
	snapshot := t.metrics
	t.metrics.MaxReportedFlightBytes = 0
	t.metrics.MaxReportedFlightPackets = 0
	return snapshot
}
