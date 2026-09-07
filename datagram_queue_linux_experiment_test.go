//go:build linux && queue_experiment

package quic

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
)

// BenchmarkDatagramReceiveConcurrent is an experimental unpaced producer/receiver
// workload. The default ns/op and allocation metrics are per offered datagram;
// delivered/s, ns/delivered, and drop-% describe useful work under overload.
func BenchmarkDatagramReceiveConcurrent(b *testing.B) {
	if os.Getenv("QUEUE_EXPERIMENT") != "1" {
		b.Skip("set QUEUE_EXPERIMENT=1 to run this archived experiment")
	}
	logger := utils.DefaultLogger.WithPrefix("benchmark")
	logger.SetLogLevel(utils.LogLevelNothing)
	queue := newDatagramQueue(func() {}, logger)
	frame := &wire.DatagramFrame{Data: make([]byte, 1071)}
	ctx := context.Background()
	for range maxDatagramRcvQueueLen {
		queue.HandleDatagramFrame(frame)
	}
	for range maxDatagramRcvQueueLen {
		if _, err := queue.Receive(ctx); err != nil {
			b.Fatal(err)
		}
	}

	finished := errors.New("benchmark producer finished")
	done := make(chan struct{})
	var delivered int
	var receiveErr error
	go func() {
		defer close(done)
		for {
			data, err := queue.Receive(ctx)
			if err != nil {
				receiveErr = err
				return
			}
			if len(data) != len(frame.Data) {
				receiveErr = errors.New("unexpected datagram length")
				return
			}
			delivered++
		}
	}()

	b.ReportAllocs()
	for b.Loop() {
		queue.HandleDatagramFrame(frame)
	}
	// B.Loop stops its timer. Include draining the remaining admitted datagrams
	// and joining the receiver so delivered work matches the measured interval.
	b.StartTimer()
	queue.CloseWithError(finished)
	<-done
	b.StopTimer()
	if receiveErr != finished || delivered == 0 || delivered > b.N {
		b.Fatalf("invalid delivery accounting: offered=%d delivered=%d error=%v", b.N, delivered, receiveErr)
	}
	b.ReportMetric(float64(delivered), "delivered")
	b.ReportMetric(float64(b.N-delivered), "drops")
	b.ReportMetric(100*float64(b.N-delivered)/float64(b.N), "drop-%")
	b.ReportMetric(float64(delivered)/b.Elapsed().Seconds(), "delivered/s")
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(delivered), "ns/delivered")
}

// BenchmarkDatagramReceivePartialRefill keeps 96 entries live between operations.
// Each operation drains and refills 32 messages, never emptying the queue.
func BenchmarkDatagramReceivePartialRefill(b *testing.B) {
	if os.Getenv("QUEUE_EXPERIMENT") != "1" {
		b.Skip("set QUEUE_EXPERIMENT=1 to run this archived experiment")
	}
	logger := utils.DefaultLogger.WithPrefix("benchmark")
	logger.SetLogLevel(utils.LogLevelNothing)
	queue := newDatagramQueue(func() {}, logger)
	frame := &wire.DatagramFrame{Data: make([]byte, 1071)}
	ctx := context.Background()
	for range 96 {
		queue.HandleDatagramFrame(frame)
	}
	b.ReportAllocs()
	for b.Loop() {
		for range 32 {
			if _, err := queue.Receive(ctx); err != nil {
				b.Fatal(err)
			}
		}
		for range 32 {
			queue.HandleDatagramFrame(frame)
		}
	}
}
