//go:build linux && queue_experiment

package quic

// Experimental measurement aid, excluded from normal builds and tests.
// This models arrival and receiver schedules; it is not a network benchmark.

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
)

type pacedHistogram struct {
	Buckets [100001]uint64 // microseconds; final bucket includes overflow
	Count   uint64
	SumNS   uint64
	MaxNS   int64
}

func (h *pacedHistogram) add(ns int64) {
	if ns < 0 {
		panic("negative monotonic duration")
	}
	h.Buckets[min(ns/1000, int64(len(h.Buckets)-1))]++
	h.Count++
	h.SumNS += uint64(ns)
	h.MaxNS = max(h.MaxNS, ns)
}

func (h *pacedHistogram) summary() map[string]float64 {
	result := map[string]float64{"count": float64(h.Count), "max_us": float64(h.MaxNS) / 1000, "overflow_count": float64(h.Buckets[len(h.Buckets)-1])}
	if h.Count == 0 {
		return result
	}
	result["mean_us"] = float64(h.SumNS) / float64(h.Count) / 1000
	for name, pct := range map[string]uint64{"p50_us_upper": 50, "p95_us_upper": 95, "p99_us_upper": 99} {
		threshold := (h.Count*pct + 99) / 100
		var count uint64
		for i, n := range h.Buckets {
			count += n
			if count >= threshold {
				result[name] = float64(i + 1)
				break
			}
		}
	}
	return result
}

func pacedCPUSeconds() float64 {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		panic(err)
	}
	return float64(usage.Utime.Sec+usage.Stime.Sec) + float64(usage.Utime.Usec+usage.Stime.Usec)/1e6
}

func pacedEnvInt(t *testing.T, name string) int {
	t.Helper()
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil || value <= 0 {
		t.Fatalf("%s must be a positive integer", name)
	}
	return value
}

func TestDatagramReceivePacedExperiment(t *testing.T) {
	if os.Getenv("QUEUE_EXPERIMENT") != "1" {
		t.Skip("explicit experimental invocation required")
	}
	rate := pacedEnvInt(t, "QUEUE_RATE")
	burst := pacedEnvInt(t, "QUEUE_BURST")
	duration := time.Duration(pacedEnvInt(t, "QUEUE_DURATION_MS")) * time.Millisecond
	stall := os.Getenv("QUEUE_STALL") == "1"
	total := int64(rate) * duration.Nanoseconds() / int64(time.Second)
	if total == 0 || total%int64(burst) != 0 {
		t.Fatal("duration must contain a whole number of bursts")
	}
	interval := time.Duration(int64(burst) * int64(time.Second) / int64(rate))
	if interval <= 0 {
		t.Fatal("burst interval must be positive")
	}

	logger := utils.DefaultLogger.WithPrefix("paced-experiment")
	logger.SetLogLevel(utils.LogLevelNothing)
	queue := newDatagramQueue(func() {}, logger)
	frame := &wire.DatagramFrame{Data: make([]byte, 1071)}
	frame.Data[len(frame.Data)-1] = 0xa5
	ctx := context.Background()
	for range maxDatagramRcvQueueLen {
		queue.HandleDatagramFrame(frame)
	}
	for range maxDatagramRcvQueueLen {
		if _, err := queue.Receive(ctx); err != nil {
			t.Fatal(err)
		}
	}

	latency, lateness, pauseLengths := new(pacedHistogram), new(pacedHistogram), new(pacedHistogram)
	external := prepareExternalPacer(t, duration)
	finished := errors.New("paced producer finished")
	ready, done, begin := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var epoch time.Time
	var delivered, lastSequence int64
	var receiverError error
	go func() {
		defer close(done)
		close(ready)
		<-begin
		nextPause := epoch.Add(10 * time.Millisecond)
		for {
			if stall && time.Now().Before(epoch.Add(duration)) && !time.Now().Before(nextPause) {
				pauseStart := time.Now()
				time.Sleep(time.Millisecond)
				pauseEnd := time.Now()
				pauseLengths.add(pauseEnd.Sub(pauseStart).Nanoseconds())
				// Skip missed pause slots; do not manufacture consecutive pauses.
				nextPause = epoch.Add((pauseEnd.Sub(epoch)/(10*time.Millisecond) + 1) * 10 * time.Millisecond)
			}
			data, err := queue.Receive(ctx)
			if err != nil {
				receiverError = err
				return
			}
			now := time.Since(epoch).Nanoseconds()
			if len(data) != 1071 || data[len(data)-1] != 0xa5 {
				receiverError = errors.New("payload integrity failure")
				return
			}
			sequence := int64(binary.LittleEndian.Uint64(data[:8]))
			if sequence <= lastSequence {
				receiverError = errors.New("FIFO or duplicate delivery failure")
				return
			}
			lastSequence = sequence
			latency.add(now - int64(binary.LittleEndian.Uint64(data[8:16])))
			delivered++
		}
	}()
	<-ready
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	cpuStart := pacedCPUSeconds()
	epoch = time.Now().Add(time.Millisecond)
	if external != nil {
		epoch = external.epoch
	}
	close(begin)
	var offered, skippedBursts int64
	bursts := total / int64(burst)
	for slot := int64(0); slot < bursts || external != nil; slot++ {
		if external != nil {
			var more bool
			slot, more = external.next(t, bursts)
			if !more {
				skippedBursts = external.generatorSkipped + external.ipcSkipped + external.expired
				break
			}
		}
		deadline := epoch.Add(time.Duration(slot * int64(burst) * int64(time.Second) / int64(rate)))
		// Deliberately busy pace on one P to avoid Linux timer granularity
		// turning fine-grained arrivals into accidental millisecond bursts.
		for external == nil && time.Now().Before(deadline) {
		}
		now := time.Now()
		if external != nil && (now.Sub(deadline) >= interval || !now.Before(epoch.Add(duration))) {
			external.expired++
			continue
		}
		if !now.Before(epoch.Add(duration)) {
			skippedBursts += bursts - slot
			break
		}
		if now.Sub(deadline) >= interval {
			latestSlot := min(int64(now.Sub(epoch))*int64(rate)/(int64(burst)*int64(time.Second)), bursts-1)
			skippedBursts += latestSlot - slot
			slot = latestSlot
			deadline = epoch.Add(time.Duration(slot * int64(burst) * int64(time.Second) / int64(rate)))
		}
		lateness.add(now.Sub(deadline).Nanoseconds())
		for range burst {
			offered++
			binary.LittleEndian.PutUint64(frame.Data[:8], uint64(offered))
			binary.LittleEndian.PutUint64(frame.Data[8:16], uint64(time.Since(epoch).Nanoseconds()))
			queue.HandleDatagramFrame(frame)
		}
	}
	for time.Now().Before(epoch.Add(duration)) {
	}
	producerElapsed := time.Since(epoch)
	queue.CloseWithError(finished)
	<-done
	elapsed := time.Since(epoch)
	cpuSeconds := pacedCPUSeconds() - cpuStart
	runtime.ReadMemStats(&after)
	if receiverError != finished || delivered <= 0 || delivered > offered || lastSequence > offered || offered+skippedBursts*int64(burst) != total {
		t.Fatalf("invalid accounting: offered=%d delivered=%d skipped=%d planned=%d last=%d error=%v", offered, delivered, skippedBursts*int64(burst), total, lastSequence, receiverError)
	}
	result := map[string]any{
		"rate": rate, "burst": burst, "stall": stall, "duration_seconds": duration.Seconds(),
		"planned": total, "offered": offered, "generator_skipped": skippedBursts * int64(burst),
		"delivered": delivered, "queue_drops": offered - delivered,
		"offered_per_second":       float64(offered) / producerElapsed.Seconds(),
		"delivered_per_second":     float64(delivered) / elapsed.Seconds(),
		"producer_elapsed_seconds": producerElapsed.Seconds(), "elapsed_seconds": elapsed.Seconds(),
		"allocated_bytes": after.TotalAlloc - before.TotalAlloc, "allocations": after.Mallocs - before.Mallocs,
		"gc_cycles": after.NumGC - before.NumGC, "gc_pause_ns": after.PauseTotalNs - before.PauseTotalNs,
		"process_cpu_seconds": cpuSeconds,
		"queue_latency":       latency.summary(), "arrival_lateness": lateness.summary(), "receiver_pause": pauseLengths.summary(),
	}
	result["pacing_mode"] = "internal"
	if external != nil {
		result["pacing_mode"] = "external"
		result["external_generator_skipped"] = external.generatorSkipped * int64(burst)
		result["external_ipc_skipped"] = external.ipcSkipped * int64(burst)
		result["external_expired"] = external.expired * int64(burst)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("QUEUE_RESULT %s\n", encoded)
}
