package quic

import (
	"sync"
	"testing"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/qlog"

	"github.com/stretchr/testify/require"
)

// Closure class 1: all views released sequentially → the slab is recycled
// exactly once, after the last view releases.
func TestCoalescedSlabSequentialRelease(t *testing.T) {
	slab, views := newCoalescedReadFixture(t, 1200, testCoalescedSegments(3, 1200)...)
	for i, v := range views {
		require.False(t, slab.released(), "slab recycled before view %d released", i)
		v.Release()
	}
	require.True(t, slab.released(), "slab not recycled after the last view released")
}

// Closure class 2: views released concurrently from distinct goroutines →
// the slab is recycled exactly once, race-clean under the race detector.
func TestCoalescedSlabConcurrentRelease(t *testing.T) {
	slab, views := newCoalescedReadFixture(t, 1200, testCoalescedSegments(8, 1200)...)
	var wg sync.WaitGroup
	wg.Add(len(views))
	start := make(chan struct{})
	for _, v := range views {
		go func() {
			defer wg.Done()
			<-start
			v.Release()
		}()
	}
	close(start)
	wg.Wait()
	require.True(t, slab.released(), "slab not recycled after all views released")
}

// Closure class 3: a view entering a retention queue is copied into the
// smallest fitting existing tier: the copy is independent of the slab's
// bytes, and the queue's hold on the view is released so the slab count
// decrements.
func TestCoalescedSlabRetentionCopy(t *testing.T) {
	segments := testCoalescedSegments(2, 1200)
	slab, views := newCoalescedReadFixture(t, 1200, segments...)
	var budget coalescedRetentionBudget
	p := receivedPacket{buffer: views[0], data: views[0].Data}
	retained, ok := retainForRetentionQueue(p, &budget)
	require.True(t, ok)
	require.NotSame(t, views[0], retained.buffer)
	require.Nil(t, retained.buffer.slab, "retained copy must not be slab-backed")
	require.Equal(t, protocol.MaxPacketBufferSize, cap(retained.buffer.Data), "copy must use the smallest fitting tier")
	require.Equal(t, segments[0], retained.data)

	// the queue's hold on the view was released: only views[1] keeps the slab alive
	require.False(t, slab.released())
	// the copy is independent: mutating the slab's bytes must not change it
	slab.buf.Data[0] ^= 0xff
	require.Equal(t, segments[0], retained.data)

	views[1].Release()
	require.True(t, slab.released(), "slab still pinned after retention copy and sibling release")
	retained.buffer.Release()
}

// A packet that is not slab-backed passes through retention untouched: the
// wiring in the connection's queues is dormant until a producer exists.
func TestCoalescedRetentionNonSlabPassthrough(t *testing.T) {
	buf := getPacketBuffer()
	buf.Data = append(buf.Data, []byte("foobar")...)
	var budget coalescedRetentionBudget
	p := receivedPacket{buffer: buf, data: buf.Data}
	retained, ok := retainForRetentionQueue(p, &budget)
	require.True(t, ok)
	require.Same(t, buf, retained.buffer)
	require.Equal(t, p.data, retained.data)
	buf.Release()
}

// Closure class 4: a view larger than the large tier keeps its slab when
// retained, is charged at full slab size against the per-connection
// retained-bytes budget, and releases both slab and charge on queue eviction.
func TestCoalescedSlabOversizedRetention(t *testing.T) {
	slab, views := newCoalescedReadFixture(t, 30000, testCoalescedSegments(1, 30000)...)
	var budget coalescedRetentionBudget
	p := receivedPacket{buffer: views[0], data: views[0].Data}
	retained, ok := retainForRetentionQueue(p, &budget)
	require.True(t, ok)
	require.Same(t, views[0], retained.buffer, "oversized view keeps its slab")
	require.Equal(t, int64(protocol.MaxCoalescedPacketBufferSize), budget.retained.Load(), "charged at full slab size")
	require.False(t, slab.released())

	// entering a second retention queue must not charge again
	retained2, ok := retainForRetentionQueue(retained, &budget)
	require.True(t, ok)
	require.Same(t, views[0], retained2.buffer)
	require.Equal(t, int64(protocol.MaxCoalescedPacketBufferSize), budget.retained.Load())

	// queue eviction releases the view: slab recycled, budget released
	retained2.buffer.Release()
	require.True(t, slab.released())
	require.Zero(t, budget.retained.Load())
}

// Budget exhaustion refuses queue entry; the caller drops the packet.
func TestCoalescedSlabRetentionBudgetExhausted(t *testing.T) {
	slab, views := newCoalescedReadFixture(t, 30000, testCoalescedSegments(1, 30000)...)
	var budget coalescedRetentionBudget
	budget.retained.Store(protocol.MaxConnRetainedCoalescedBytes - protocol.MaxCoalescedPacketBufferSize + 1)
	p := receivedPacket{buffer: views[0], data: views[0].Data}
	_, ok := retainForRetentionQueue(p, &budget)
	require.False(t, ok, "an exhausted budget must refuse queue entry")
	require.False(t, slab.released())
	views[0].Release() // the caller drops the packet
	require.True(t, slab.released())
}

// The unprocessed queue (handlePacket) holds retention copies, not slab
// views: a queued segment must not pin its slab. Dormant at runtime until a
// producer creates slab views.
func TestCoalescedRetentionUnprocessedQueueWiring(t *testing.T) {
	tc := newServerTestConnection(t, nil, nil, false)
	slab, views := newCoalescedReadFixture(t, 1200, testCoalescedSegments(2, 1200)...)
	tc.conn.handlePacket(receivedPacket{data: views[0].Data, buffer: views[0], rcvTime: monotime.Now()})
	require.Zero(t, tc.conn.coalescedRetention.retained.Load(), "in-tier copy must not charge the budget")
	views[1].Release()
	require.True(t, slab.released(), "unprocessed queue pinned the slab")
}

// The undecryptable queue holds retention copies, not slab views.
func TestCoalescedRetentionUndecryptableQueueWiring(t *testing.T) {
	tc := newServerTestConnection(t, nil, nil, false)
	slab, views := newCoalescedReadFixture(t, 1200, testCoalescedSegments(2, 1200)...)
	var checksum qlog.DatagramPayloadChecksum
	queued := tc.conn.tryQueueingUndecryptablePacket(
		receivedPacket{data: views[0].Data, buffer: views[0], rcvTime: monotime.Now()},
		qlog.PacketTypeInitial, checksum,
	)
	require.True(t, queued)
	require.Zero(t, tc.conn.coalescedRetention.retained.Load(), "in-tier copy must not charge the budget")
	views[1].Release()
	require.True(t, slab.released(), "undecryptable queue pinned the slab")
}

// Closure class 5: double release of one view panics, preserving the
// existing packetBuffer release contract.
func TestCoalescedSlabViewDoubleRelease(t *testing.T) {
	slab, views := newCoalescedReadFixture(t, 1200, testCoalescedSegments(2, 1200)...)
	views[0].Release()
	require.Panics(t, func() { views[0].Release() })
	require.False(t, slab.released())
	views[1].Release()
	require.True(t, slab.released())
}
