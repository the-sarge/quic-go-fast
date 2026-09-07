//go:build queue_lifetime_experiment

package quic

import (
	"testing"

	"github.com/quic-go/quic-go/internal/protocol"
)

// The fixture substitutes only socket I/O; the real queue owns buffer release.
type lifetimeBenchmarkConn struct{ sendConn }

func (lifetimeBenchmarkConn) Write([]byte, uint16, protocol.ECN) error { return nil }

func BenchmarkQueueLifetime(b *testing.B) {
	for _, pressure := range []bool{false, true} {
		name := "HandoffDrain"
		if pressure {
			name = "CapacityPressure"
		}
		b.Run(name, func(b *testing.B) {
			q := newSendQueue(lifetimeBenchmarkConn{}, nil)
			result := make(chan error, 1)
			go func() { result <- q.Run() }()
			b.ReportAllocs()
			for b.Loop() {
				for q.WouldBlock() {
					<-q.Available()
				}
				q.Send(getPacketBuffer(), 0, protocol.ECNNon, sendMetadata{})
				if !pressure {
					<-q.Available()
				}
			}
			q.Close()
			if err := <-result; err != nil {
				b.Fatal(err)
			}
		})
	}
}
