package quic

import (
	"context"
	"testing"

	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
)

func BenchmarkDatagramReceive(b *testing.B) {
	for _, mode := range []string{"SteadyDrain", "BurstDrain", "Overflow"} {
		b.Run(mode, func(b *testing.B) {
			logger := utils.DefaultLogger.WithPrefix("benchmark")
			logger.SetLogLevel(utils.LogLevelNothing)
			queue := newDatagramQueue(func() {}, logger)
			frame := &wire.DatagramFrame{Data: make([]byte, 1071)}
			ctx := context.Background()
			// Warm the admission/drain cases once; overflow remains full.
			for range maxDatagramRcvQueueLen {
				queue.HandleDatagramFrame(frame)
			}
			if mode != "Overflow" {
				for range maxDatagramRcvQueueLen {
					if _, err := queue.Receive(ctx); err != nil {
						b.Fatal(err)
					}
				}
			}
			b.ReportAllocs()
			switch mode {
			case "SteadyDrain":
				for b.Loop() {
					queue.HandleDatagramFrame(frame)
					if _, err := queue.Receive(ctx); err != nil {
						b.Fatal(err)
					}
				}
			case "BurstDrain":
				// One operation is a full 128-message burst and drain.
				for b.Loop() {
					for range maxDatagramRcvQueueLen {
						queue.HandleDatagramFrame(frame)
					}
					for range maxDatagramRcvQueueLen {
						if _, err := queue.Receive(ctx); err != nil {
							b.Fatal(err)
						}
					}
				}
			case "Overflow":
				for b.Loop() {
					queue.HandleDatagramFrame(frame)
				}
			}
		})
	}
}
