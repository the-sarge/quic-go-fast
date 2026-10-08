// Command synthetic runs D6's synthetic accounting cases (#740) under the
// memory sampler (mem_series.go, unchanged, built beside it), so the
// accounting rules are tested against real Linux /proc and runtime samples
// with known causes. Linux only; measurement-only, never a transport build.
//
// Every case keeps a 4 MiB live baseline and allocates short-lived garbage at
// a steady rate; the case adds one known effect:
//
//	none         the baseline
//	retained     8 MiB more live heap, retained for the whole run
//	spike        24 MiB live during warmup only, released at the measured start
//	stacks       64 goroutines parked in deep recursion (about 8 MiB of stack)
//	churn        the baseline live heap with four times the allocation rate
//	unexplained  8 MiB of anonymous memory mapped and touched outside the Go runtime
package main

import (
	"encoding/json"
	"flag"
	"os"
	"runtime"
	"sync"
	"syscall"
	"time"
)

var sink [][]byte

func churn(rate int, stop <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	per := rate / 1000
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			for n := 0; n < per; n += 4096 {
				b := make([]byte, 4096)
				b[0] = 1
				sink = append(sink[:0], b)
			}
		}
	}
}

func deep(n int, release <-chan struct{}) byte {
	var pad [1024]byte
	pad[n%1024] = byte(n)
	if n == 0 {
		<-release
		return pad[0]
	}
	return deep(n-1, release) + pad[n%1024]
}

func touch(b []byte) {
	for i := 0; i < len(b); i += 4096 {
		b[i] = 1
	}
}

func main() {
	which := flag.String("case", "none", "synthetic case")
	warmup := flag.Duration("warmup", 3*time.Second, "warmup")
	measure := flag.Duration("measure", 6*time.Second, "measured window")
	output := flag.String("output", "", "result JSON")
	flag.Parse()
	_ = memController("reno")
	start := time.Now()
	stopMem := startMemSeries()

	base := make([]byte, 4<<20)
	touch(base)
	rate := 50 << 20 // bytes per second of garbage
	if *which == "churn" {
		rate *= 4
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go churn(rate, stop, &wg)

	var extra []byte
	release := make(chan struct{})
	switch *which {
	case "retained":
		extra = make([]byte, 8<<20)
		touch(extra)
	case "spike":
		extra = make([]byte, 24<<20)
		touch(extra)
	case "stacks":
		for range 64 {
			go deep(120, release)
		}
	case "unexplained":
		m, err := syscall.Mmap(-1, 0, 8<<20, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
		if err != nil {
			panic(err)
		}
		touch(m)
	}
	time.Sleep(time.Until(start.Add(*warmup)))
	if *which == "spike" {
		extra = nil
	}
	time.Sleep(time.Until(start.Add(*warmup + *measure)))
	close(stop)
	wg.Wait()
	close(release)
	runtime.KeepAlive(base)
	runtime.KeepAlive(extra)
	stopMem()
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	data, _ := json.Marshal(map[string]any{"case": *which, "start_unix_ns": start.UnixNano(), "warmup_ms": warmup.Milliseconds(),
		"measure_ms": measure.Milliseconds(), "peak_rss_bytes": ru.Maxrss * 1024})
	if err := os.WriteFile(*output, data, 0o644); err != nil {
		panic(err)
	}
}
