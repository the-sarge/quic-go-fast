package main

// Measurement-only overlay for the #710 attribution: a 10 ms runtime and
// transport occupancy series plus optional per-second heap profiles.

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/metrics"
	"runtime/pprof"
	"time"

	quic "github.com/quic-go/quic-go"
)

var (
	seriesOutput  = flag.String("series-output", "", "local diagnostic: 10 ms runtime/occupancy series JSON")
	heapSeriesDir = flag.String("heap-series-dir", "", "local diagnostic: heap profile every second, no forced GC")
)

var seriesMetrics = []string{
	"/memory/classes/heap/objects:bytes", "/gc/heap/live:bytes", "/gc/heap/goal:bytes", "/gc/heap/allocs:bytes",
	"/gc/cycles/total:gc-cycles", "/memory/classes/total:bytes", "/memory/classes/heap/released:bytes",
	"/memory/classes/heap/free:bytes", "/memory/classes/heap/unused:bytes",
}

type diagSeries struct {
	Metrics    []string   `json:"metrics"`
	Columns    []string   `json:"columns"`
	Samples    [][]int64  `json:"samples"`
	HeapSeries [][2]int64 `json:"heap_series,omitempty"` // [index, unix ns]
	HeapErrors []string   `json:"heap_errors,omitempty"`
}

func startDiagSeries() func() {
	if *seriesOutput == "" && *heapSeriesDir == "" {
		return func() {}
	}
	stop, done := make(chan struct{}), make(chan struct{})
	out := diagSeries{Metrics: seriesMetrics, Columns: append([]string{"unix_ns", "max_rss"}, append(append([]string{}, seriesMetrics...), "rx_len", "rx_max", "conn_received", "conn_read", "conn_occ_max")...)}
	out.Samples = make([][]int64, 0, 8192)
	go func() {
		defer close(done)
		samples := make([]metrics.Sample, len(seriesMetrics))
		for i, name := range seriesMetrics {
			samples[i].Name = name
		}
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		var heapTick <-chan time.Time
		if *heapSeriesDir != "" {
			t := time.NewTicker(time.Second)
			defer t.Stop()
			heapTick = t.C
		}
		for {
			select {
			case <-stop:
				return
			case <-heapTick:
				i := len(out.HeapSeries)
				f, err := os.Create(filepath.Join(*heapSeriesDir, fmt.Sprintf("heap-%03d.pprof", i)))
				if err == nil {
					err = pprof.Lookup("heap").WriteTo(f, 0)
					f.Close()
				}
				if err != nil {
					out.HeapErrors = append(out.HeapErrors, err.Error())
				}
				out.HeapSeries = append(out.HeapSeries, [2]int64{int64(i), time.Now().UnixNano()})
			case now := <-tick.C:
				metrics.Read(samples)
				row := make([]int64, 0, len(out.Columns))
				row = append(row, now.UnixNano(), int64(usage().PeakRSSBytes))
				for _, s := range samples {
					row = append(row, int64(s.Value.Uint64()))
				}
				rxLen, rxMax, recv, read, occ := quic.DiagnosticOccupancy()
				out.Samples = append(out.Samples, append(row, rxLen, rxMax, recv, read, occ))
			}
		}
	}()
	return func() {
		close(stop)
		<-done
		if *seriesOutput == "" {
			return
		}
		data, err := json.Marshal(out)
		if err == nil {
			err = os.WriteFile(*seriesOutput, data, 0o644)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "[BBR-SERIES]", err)
		}
	}
}
