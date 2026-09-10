//go:build emission_experiment

package quic

import (
	"fmt"
	"os"
	"testing"
	"time"
	"unsafe"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

// This opt-in diagnostic compares identical fixture overhead on the base and
// candidate. The production packer/recovery and queue still process each packet.
func TestEmissionFocusedAllocations(t *testing.T) {
	if os.Getenv("EMISSION_EXPERIMENT") != "1" {
		t.Skip("explicit E1 capture only")
	}
	for _, gso := range []bool{false, true} {
		t.Run(fmt.Sprintf("gso=%t", gso), func(t *testing.T) {
			c := newEmissionTestConnection(t, gso).conn
			// Keep synthetic server anti-amplification credit outside this
			// established-send allocation measurement, including full batches.
			c.sentPacketHandler.ReceivedBytes(1<<30, monotime.Now())
			q := c.emission.queue.(*sendQueue)
			frame := &wire.DatagramFrame{DataLenPresent: true, Data: make([]byte, 1071)}
			packets := 1
			if gso {
				packets = 3
				frame.Data = make([]byte, 1200)
			}
			now := monotime.Now()
			allocs := testing.AllocsPerRun(1000, func() {
				if gso {
					_, pnLen := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
					frame.Data = frame.Data[:1200-int(wire.ShortHeaderLen(c.connIDManager.Get(), pnLen))-7-3]
				}
				for range packets {
					require.NoError(t, c.datagramQueue.Add(frame))
				}
				require.NoError(t, c.sendPackets(now))
				require.Len(t, q.queue, 1)
				entry := <-q.queue
				now = now.Add(time.Second)
				data := entry.buf.Data
				if gso {
					require.Equal(t, packets*1200, len(data))
				}
				for range packets {
					size := len(data)
					if gso {
						size = 1200
					}
					_, pn, _, _, err := wire.ParseShortHeader(data[:size], c.connIDManager.Get().Len())
					require.NoError(t, err)
					_, err = c.sentPacketHandler.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: pn, Largest: pn}}}, protocol.Encryption1RTT, now)
					require.NoError(t, err)
					data = data[size:]
				}
				entry.buf.Release()
			})
			fmt.Printf("E1_ALLOC gso=%t packets_per_batch=%d allocs_per_batch=%.6f conn_bytes=%d\n", gso, packets, allocs, unsafe.Sizeof(Conn{}))
		})
	}
}

// Preserve the historical outcome/allocation fixture entrypoint.
func (c *Conn) sendPackets(now monotime.Time) error { return c.emitPackets(now).err }

func (c *Conn) emitPackets(now monotime.Time) emissionResult {
	result := c.emission.finish(c.emission.sendAny(now, c.handshakeConfirmed))
	c.pacingDeadline = result.deadline
	if result.retry {
		c.scheduleSending()
	}
	return result
}
