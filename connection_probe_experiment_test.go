//go:build emission_experiment

package quic

import (
	"fmt"
	"os"
	"testing"
	"time"
	"unsafe"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

// Opt-in, identical-source allocation observations for the migrated probe paths.
// Path construction is measured separately from transport setup and syscall I/O.
func TestEmissionProbeAllocations(t *testing.T) {
	if os.Getenv("EMISSION_EXPERIMENT") != "1" {
		t.Skip("explicit probe capture only")
	}
	t.Run("path-construction-registration", func(t *testing.T) {
		c := newEmissionTestConnection(t, false).conn
		now := monotime.Now()
		frames := []ackhandler.Frame{{Frame: &wire.PathChallengeFrame{Data: [8]byte{1}}}}
		allocs := testing.AllocsPerRun(1000, func() {
			p, buf, err := c.packer.PackPathProbePacket(c.connIDManager.Get(), frames, c.version)
			require.NoError(t, err)
			c.emission.registerPacket(p, protocol.ECNNon, now)
			_, err = c.sentPacketHandler.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: p.PacketNumber, Largest: p.PacketNumber}}}, protocol.Encryption1RTT, now.Add(time.Millisecond))
			require.NoError(t, err)
			now = now.Add(time.Second)
			buf.Release()
		})
		fmt.Printf("path-construction-registration allocs=%g conn-bytes=%d\n", allocs, unsafe.Sizeof(*c))
	})
	t.Run("queued-MTU", func(t *testing.T) {
		c := newEmissionTestConnection(t, false).conn
		q := c.sendQueue.(*sendQueue)
		now := monotime.Now()
		c.mtuDiscoverer = newMTUDiscoverer(c.rttStats, 1200, 1400, nil)
		c.sentPacketHandler.ReceivedBytes(1<<30, now)
		allocs := testing.AllocsPerRun(1000, func() {
			now = now.Add(time.Second)
			c.mtuDiscoverer.Reset(now.Add(-time.Hour), 1200, 1400)
			require.NoError(t, c.emitPackets(now).err)
			entry := <-q.queue
			_, pn, _, _, err := wire.ParseShortHeader(entry.buf.Data, c.connIDManager.Get().Len())
			require.NoError(t, err)
			_, err = c.sentPacketHandler.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: pn, Largest: pn}}}, protocol.Encryption1RTT, now.Add(time.Millisecond))
			require.NoError(t, err)
			entry.buf.Release()
		})
		fmt.Printf("queued-MTU allocs=%g\n", allocs)
	})
}
