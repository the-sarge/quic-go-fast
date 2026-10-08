package main

// Measurement-only #740 overlay: a per-second memory accounting series.
//
// Every second (and once at stop) one JSON line is appended to $MEM_OUTPUT with
// resident memory from /proc/self/smaps_rollup and /proc/self/status, the Go
// runtime's disjoint /memory/classes quantities and GC context, the explicit
// BBR structure bytes, and the receive occupancy counters. Rows are written as
// they are taken, from preallocated buffers, so the sampler's own footprint is
// small and constant. The phase log is written once, at stop.
//
// The connection's public ConnectionStats packet counters (atomic in both
// revisions) are recorded for the comparability checks; the fixture registers
// its connection through memConn at the start of each session.
//
// $MEM_LOCAL_CONTROLLER, when set, selects this endpoint's congestion
// controller in place of the run configuration's (the receiver-controller arm).
// The shared run configuration, and so the peers' configuration check, is
// unchanged. Not a maintained hook.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"runtime/metrics"
	"strconv"
	"sync/atomic"
	"time"

	quic "github.com/quic-go/quic-go"
)

var memMetrics = []string{
	"/memory/classes/heap/objects:bytes", "/memory/classes/heap/unused:bytes", "/memory/classes/heap/free:bytes",
	"/memory/classes/heap/released:bytes", "/memory/classes/heap/stacks:bytes", "/memory/classes/os-stacks:bytes",
	"/memory/classes/metadata/mcache/free:bytes", "/memory/classes/metadata/mcache/inuse:bytes",
	"/memory/classes/metadata/mspan/free:bytes", "/memory/classes/metadata/mspan/inuse:bytes",
	"/memory/classes/metadata/other:bytes", "/memory/classes/other:bytes", "/memory/classes/profiling/buckets:bytes",
	"/memory/classes/total:bytes", "/gc/cycles/total:gc-cycles", "/gc/heap/goal:bytes", "/gc/heap/live:bytes",
	"/gc/heap/allocs:bytes", "/gc/scan/stack:bytes", "/sched/goroutines:goroutines",
}

// smaps_rollup and status fields, in kB.
var memRollup = []string{"Rss", "Pss", "Pss_Anon", "Pss_File", "Pss_Shmem", "Shared_Clean", "Shared_Dirty", "Private_Clean",
	"Private_Dirty", "Anonymous", "AnonHugePages", "Swap"}
var memStatus = []string{"VmHWM", "VmRSS", "RssAnon", "RssFile", "RssShmem"}

var memOverride string

var memConn atomic.Pointer[quic.Conn]

func memController(c string) string {
	if v := os.Getenv("MEM_LOCAL_CONTROLLER"); v != "" {
		memOverride = v
		return v
	}
	return c
}

// memFields copies the kB values of the named fields in a /proc file into dst.
func memFields(path string, buf []byte, names []string, dst []int64) {
	for i := range dst {
		dst[i] = -1
	}
	f, err := os.Open(path)
	if err != nil {
		return
	}
	n, _ := f.Read(buf)
	f.Close()
	for _, line := range bytes.Split(buf[:n], []byte{'\n'}) {
		colon := bytes.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		key := string(line[:colon])
		for i, name := range names {
			if key == name {
				v, err := strconv.ParseInt(string(bytes.TrimSuffix(bytes.TrimSpace(line[colon+1:]), []byte(" kB"))), 10, 64)
				if err == nil {
					dst[i] = v
				}
			}
		}
	}
}

func startMemSeries() func() {
	path := os.Getenv("MEM_OUTPUT")
	if path == "" {
		return func() {}
	}
	out, err := os.Create(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "[MEM-SERIES]", err)
		return func() {}
	}
	bbrNames := quic.MemBBRNames()
	columns := []string{"unix_ns"}
	for _, n := range memRollup {
		columns = append(columns, "rollup_"+n+"_kb")
	}
	for _, n := range memStatus {
		columns = append(columns, "status_"+n+"_kb")
	}
	columns = append(columns, memMetrics...)
	columns = append(columns, bbrNames...)
	columns = append(columns, "rx_len", "rx_max", "conn_received", "conn_read", "conn_occ_max",
		"stats_packets_sent", "stats_packets_received", "stats_packets_lost", "stats_bytes_sent")
	header, _ := json.Marshal(map[string]any{"columns": columns, "controller_override": memOverride, "pid": os.Getpid(),
		"interval_ms": 1000})
	out.Write(append(header, '\n'))

	samples := make([]metrics.Sample, len(memMetrics))
	for i, name := range memMetrics {
		samples[i].Name = name
	}
	buf := make([]byte, 8192)
	line := make([]byte, 0, 4096)
	row := make([]int64, 0, len(columns))
	rollup := make([]int64, len(memRollup))
	status := make([]int64, len(memStatus))
	sample := func(now time.Time) {
		memFields("/proc/self/smaps_rollup", buf, memRollup, rollup)
		memFields("/proc/self/status", buf, memStatus, status)
		metrics.Read(samples)
		row = append(row[:0], now.UnixNano())
		row = append(row, rollup...)
		row = append(row, status...)
		for _, s := range samples {
			if s.Value.Kind() == metrics.KindUint64 {
				row = append(row, int64(s.Value.Uint64()))
			} else {
				row = append(row, -1)
			}
		}
		row = quic.MemBBR(row)
		rxLen, rxMax, recv, read, occ := quic.DiagnosticOccupancy()
		row = append(row, rxLen, rxMax, recv, read, occ)
		if c := memConn.Load(); c != nil {
			st := c.ConnectionStats()
			row = append(row, int64(st.PacketsSent), int64(st.PacketsReceived), int64(st.PacketsLost), int64(st.BytesSent))
		} else {
			row = append(row, -1, -1, -1, -1)
		}
		line = append(line[:0], '[')
		for i, v := range row {
			if i > 0 {
				line = append(line, ',')
			}
			line = strconv.AppendInt(line, v, 10)
		}
		line = append(line, ']', '\n')
		out.Write(line)
	}

	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		sample(time.Now())
		for {
			select {
			case <-stop:
				sample(time.Now())
				return
			case now := <-tick.C:
				sample(now)
			}
		}
	}()
	return func() {
		close(stop)
		<-done
		tail, _ := json.Marshal(map[string]any{"phases": quic.MemPhases(), "controller_override": memOverride})
		out.Write(append(tail, '\n'))
		if err := out.Close(); err != nil {
			fmt.Fprintln(os.Stderr, "[MEM-SERIES]", err)
		}
	}
}
