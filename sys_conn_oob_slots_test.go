//go:build darwin || linux || freebsd

package quic

import (
	"bytes"
	"net"
	"slices"
	"sync"
	"testing"

	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
)

// newSlotReader returns a reader offering n read slots to read, as an opted-in
// Darwin batch adapter is offered them (#857). The slot logic is shared, so it
// is tested on every platform with a batched reader.
func newSlotReader(t *testing.T, n int, read lifetimeBatchReader) *oobConn {
	t.Helper()
	c, err := newConn(newUDPConnLocalhost(t), true, false)
	require.NoError(t, err)
	c.setReadSlots(n, true)
	c.batchConn = read
	t.Cleanup(c.releaseReadBuffers)
	return c
}

func TestReadSlotsDeliverABatchFromItsSlots(t *testing.T) {
	calls := 0
	var offered []*packetBuffer
	var c *oobConn
	c = newSlotReader(t, 4, func(ms []ipv4.Message) (int, error) {
		calls++
		require.Len(t, ms, 4)
		offered = slices.Clone(c.buffers)
		for i := range ms {
			require.Len(t, ms[i].Buffers[0], protocol.MaxPacketBufferSize+1)
		}
		for i := range 3 {
			ms[i].N = copy(ms[i].Buffers[0], []byte{byte(i)})
		}
		return 3, nil
	})
	for i := range 3 {
		p, err := c.ReadPacket()
		require.NoError(t, err)
		require.Equal(t, []byte{byte(i)}, p.data)
		require.Same(t, offered[i], p.buffer, "a datagram is delivered in its slot")
		// The connection's release returns the slot to the slot tier.
		require.NotPanics(t, p.buffer.Release)
	}
	require.Equal(t, 1, calls)
}

func TestReadSlotsDeliverACeilingSizeDatagramWhole(t *testing.T) {
	ceiling := bytes.Repeat([]byte{'c'}, protocol.MaxPacketBufferSize)
	c := newSlotReader(t, 2, func(ms []ipv4.Message) (int, error) {
		ms[0].N = copy(ms[0].Buffers[0], ceiling)
		return 1, nil
	})
	p, err := c.ReadPacket()
	require.NoError(t, err)
	defer p.buffer.Release()
	require.Equal(t, ceiling, p.data)
}

// A full slot means the datagram was truncated. The reader drops it, and any
// other invalid datagram, and continues with the rest of the batch.
func TestReadSlotsDropTruncatedDatagrams(t *testing.T) {
	var offered []*packetBuffer
	var c *oobConn
	c = newSlotReader(t, 4, func(ms []ipv4.Message) (int, error) {
		offered = slices.Clone(c.buffers)
		ms[0].N = copy(ms[0].Buffers[0], bytes.Repeat([]byte{'f'}, protocol.MaxPacketBufferSize+1))
		ms[1].N = copy(ms[1].Buffers[0], "flagged")
		ms[1].Flags = unix.MSG_TRUNC
		ms[2].N = copy(ms[2].Buffers[0], "bad ancillary")
		ms[2].NN = copy(ms[2].OOB, []byte{1})
		ms[3].N = copy(ms[3].Buffers[0], "next")
		return 4, nil
	})
	p, err := c.ReadPacket()
	require.NoError(t, err)
	defer p.buffer.Release()
	require.Equal(t, "next", string(p.data))
	for _, dropped := range offered[:3] {
		require.Zero(t, dropped.refCount, "a dropped datagram's slot is released")
	}
}

// A failed batch discards its data but keeps the slots for the next read.
func TestReadSlotsFailedBatchKeepsSlots(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result func([]ipv4.Message) (int, error)
	}{
		{"error", func(ms []ipv4.Message) (int, error) {
			ms[0].N = copy(ms[0].Buffers[0], "discarded")
			return 1, net.ErrClosed
		}},
		// An adapter that reports more datagrams than it was offered slots
		// for fails the read.
		{"more_than_offered", func(ms []ipv4.Message) (int, error) { return len(ms) + 1, nil }},
		{"negative", func([]ipv4.Message) (int, error) { return -1, nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			var first, second []*packetBuffer
			var c *oobConn
			c = newSlotReader(t, 3, func(ms []ipv4.Message) (int, error) {
				calls++
				if calls == 1 {
					first = slices.Clone(c.buffers)
					return tc.result(ms)
				}
				second = slices.Clone(c.buffers)
				ms[0].N = copy(ms[0].Buffers[0], "retry")
				return 1, nil
			})
			_, err := c.ReadPacket()
			require.Error(t, err)
			p, err := c.ReadPacket()
			require.NoError(t, err)
			defer p.buffer.Release()
			require.Equal(t, "retry", string(p.data))
			require.Equal(t, first, second, "the failed batch's slots are offered again")
			require.Same(t, first[0], p.buffer)
		})
	}
}

// When the reader stops, it releases the slots it still holds exactly once,
// while connections release the datagrams already delivered.
func TestReadSlotsReaderStopReleasesSlotsOnce(t *testing.T) {
	t.Run("concurrent_release", func(t *testing.T) {
		var mx sync.Mutex
		var seen []*packetBuffer
		calls := 0
		var c *oobConn
		c = newSlotReader(t, 4, func(ms []ipv4.Message) (int, error) {
			calls++
			mx.Lock()
			seen = append(seen, c.buffers...)
			mx.Unlock()
			if calls == 20 {
				return 0, net.ErrClosed
			}
			// Short batches leave slots held between reads.
			n := 1 + calls%len(ms)
			for i := range n {
				ms[i].N = copy(ms[i].Buffers[0], []byte{byte(calls), byte(i)})
			}
			return n, nil
		})
		delivered := make(chan receivedPacket)
		var wg sync.WaitGroup
		wg.Go(func() {
			for p := range delivered {
				p.buffer.Release()
			}
		})
		func() {
			defer close(delivered)
			defer c.releaseReadBuffers()
			for {
				p, err := c.ReadPacket()
				if err != nil {
					return
				}
				delivered <- p
			}
		}()
		wg.Wait()
		c.releaseReadBuffers()
		for _, b := range c.buffers {
			require.Nil(t, b)
		}
		for _, b := range seen {
			require.Zero(t, b.refCount, "every slot is released exactly once")
		}
	})

	t.Run("listener", func(t *testing.T) {
		var held []*packetBuffer
		var c *oobConn
		c = newSlotReader(t, 4, func(ms []ipv4.Message) (int, error) {
			held = slices.Clone(c.buffers)
			return 0, net.ErrClosed
		})
		(&Transport{}).listen(c)
		require.Len(t, held, 4)
		for i, b := range c.buffers {
			require.Nil(t, b)
			require.Zero(t, held[i].refCount)
		}
	})
}

// slotDeclaringConn is a batch adapter that declares slots. Its ReadBatch
// fills one message per call through ReadMsgUDP, which is valid with any
// number of slots, and records what it was offered.
type slotDeclaringConn struct {
	*net.UDPConn
	slots int

	mx      sync.Mutex
	offered int
	slotLen int
}

func (c *slotDeclaringConn) ReadBatchSlotsV1() int { return c.slots }

func (c *slotDeclaringConn) ReadBatch(ms []ipv4.Message, _ int) (int, error) {
	c.mx.Lock()
	c.offered, c.slotLen = len(ms), len(ms[0].Buffers[0])
	c.mx.Unlock()
	m := &ms[0]
	n, nn, flags, addr, err := c.ReadMsgUDP(m.Buffers[0], m.OOB)
	m.N, m.NN, m.Flags, m.Addr = n, nn, flags, addr
	if err != nil {
		return 0, err
	}
	return 1, nil
}

func (c *slotDeclaringConn) lastOffer() (slots, slotLen int) {
	c.mx.Lock()
	defer c.mx.Unlock()
	return c.offered, c.slotLen
}

// readThroughSlotDeclaringConn reads one datagram through a reader built on
// c and returns what c was offered and the delivered buffer's capacity.
func readThroughSlotDeclaringConn(t *testing.T, c *slotDeclaringConn) (slots, slotLen, deliveredCap int) {
	t.Helper()
	reader, err := newConn(c, true, false)
	require.NoError(t, err)
	t.Cleanup(reader.releaseReadBuffers)
	sender := newUDPConnLocalhost(t)
	_, err = sender.WriteTo([]byte("datagram"), c.LocalAddr())
	require.NoError(t, err)
	p, err := reader.ReadPacket()
	require.NoError(t, err)
	defer p.buffer.Release()
	require.Equal(t, "datagram", string(p.data))
	slots, slotLen = c.lastOffer()
	return slots, slotLen, cap(p.buffer.Data)
}
