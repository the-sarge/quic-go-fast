package quic

import (
	"bytes"
	"testing"

	"github.com/quic-go/quic-go/internal/mocks"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/quic-go/quic-go/testutils/events"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// releaseUnprocessedQueueAtCleanup releases whatever the test left queued.
func releaseUnprocessedQueueAtCleanup(t *testing.T, tc *testConnection) {
	t.Helper()
	t.Cleanup(func() {
		for !tc.conn.receivedPackets.Empty() {
			tc.conn.receivedPackets.PopFront().buffer.Release()
		}
	})
}

// confirmHandshakeForQueue confirms the handshake through the run loop's
// confirmation step, the one both perspectives reach.
func confirmHandshakeForQueue(t *testing.T, cs *mocks.MockCryptoSetup, tc *testConnection) {
	t.Helper()
	cs.EXPECT().DiscardInitialKeys().AnyTimes()
	cs.EXPECT().SetHandshakeConfirmed()
	require.NoError(t, tc.conn.handleHandshakeConfirmed(monotime.Now()))
}

func queueTestPacket(t *testing.T, tc *testConnection, n int) {
	t.Helper()
	for range n {
		tc.conn.handlePacket(lifetimePacket(t, tc, []byte("foobar")))
	}
}

// The queue holds 256 packets until the handshake is confirmed and 1,024
// after, on both perspectives. Code compiled against upstream quic-go finds
// the drop count by interface assertion.
func TestConnectionUnprocessedQueueDepth(t *testing.T) {
	for _, perspective := range []protocol.Perspective{protocol.PerspectiveServer, protocol.PerspectiveClient} {
		t.Run(perspective.String(), func(t *testing.T) {
			ctrl := gomock.NewController(t)
			cs := mocks.NewMockCryptoSetup(ctrl)
			var eventRecorder events.Recorder
			opts := []testConnectionOpt{connectionOptCryptoSetup(cs), connectionOptTracer(&eventRecorder)}
			var tc *testConnection
			if perspective == protocol.PerspectiveServer {
				tc = newServerTestConnection(t, ctrl, nil, false, opts...)
			} else {
				tc = newClientTestConnection(t, ctrl, nil, false, opts...)
			}
			releaseUnprocessedQueueAtCleanup(t, tc)
			drops, ok := any(tc.conn).(interface{ UnprocessedPacketDropsV1() uint64 })
			require.True(t, ok)
			require.Zero(t, drops.UnprocessedPacketDropsV1())

			queueTestPacket(t, tc, protocol.MaxConnUnprocessedPackets)
			p := lifetimePacket(t, tc, []byte("foobar"))
			tc.conn.handlePacket(p)
			require.Equal(t, protocol.MaxConnUnprocessedPackets, tc.conn.receivedPackets.Len())
			require.Zero(t, p.buffer.refCount, "a dropped packet must be released")
			require.EqualValues(t, 1, drops.UnprocessedPacketDropsV1())

			confirmHandshakeForQueue(t, cs, tc)
			queueTestPacket(t, tc, protocol.MaxConnUnprocessedPacketsConfirmed-protocol.MaxConnUnprocessedPackets)
			require.Equal(t, protocol.MaxConnUnprocessedPacketsConfirmed, tc.conn.receivedPackets.Len())
			p = lifetimePacket(t, tc, []byte("foobar"))
			tc.conn.handlePacket(p)
			require.Equal(t, protocol.MaxConnUnprocessedPacketsConfirmed, tc.conn.receivedPackets.Len())
			require.Zero(t, p.buffer.refCount, "a dropped packet must be released")
			require.EqualValues(t, 2, drops.UnprocessedPacketDropsV1())

			dropped := qlog.PacketDropped{Raw: qlog.RawInfo{Length: 6}, Trigger: qlog.PacketDropDOSPrevention}
			require.Equal(t, []qlogwriter.Event{dropped, dropped}, eventRecorder.Events(qlog.PacketDropped{}))
		})
	}
}

// Packets released because the connection has closed are not counted, and
// the count keeps its value after close.
func TestConnectionUnprocessedPacketDropsExcludeClosed(t *testing.T) {
	tc := newServerTestConnection(t, nil, nil, false)
	releaseUnprocessedQueueAtCleanup(t, tc)
	queueTestPacket(t, tc, protocol.MaxConnUnprocessedPackets+1)
	require.EqualValues(t, 1, tc.conn.UnprocessedPacketDropsV1())

	tc.conn.closePacketAdmission()
	p := lifetimePacket(t, tc, []byte("foobar"))
	tc.conn.handlePacket(p)
	require.Zero(t, p.buffer.refCount, "a packet arriving after close must be released")
	require.EqualValues(t, 1, tc.conn.UnprocessedPacketDropsV1())
}

// A refusal by the coalesced retained-bytes budget is a drop at this queue
// and is counted.
func TestConnectionUnprocessedPacketDropsCountRetentionRefusal(t *testing.T) {
	tc := newServerTestConnection(t, nil, nil, false)
	releaseUnprocessedQueueAtCleanup(t, tc)
	tc.conn.coalescedRetention.retained.Store(protocol.MaxConnRetainedCoalescedBytes - protocol.MaxCoalescedPacketBufferSize + 1)
	slab, views := newCoalescedReadFixture(t, 30000, testCoalescedSegments(1, 30000)...)
	tc.conn.handlePacket(receivedPacket{data: views[0].Data, buffer: views[0], rcvTime: monotime.Now()})
	require.True(t, slab.released(), "a refused view must be released")
	require.Zero(t, tc.conn.receivedPackets.Len())
	require.EqualValues(t, 1, tc.conn.UnprocessedPacketDropsV1())
}

// The slots beyond 256 hold only packet-sized (1,452-byte) buffers, so they
// add at most 768 × 1,452 bytes per connection whatever the reader. A packet
// in a larger buffer that fits is copied down; one that does not fit is
// dropped and counted. The first 256 slots keep every packet as delivered.
func TestConnectionUnprocessedQueueConfirmedSlotsHoldPacketSizedBuffers(t *testing.T) {
	ctrl := gomock.NewController(t)
	cs := mocks.NewMockCryptoSetup(ctrl)
	tc := newServerTestConnection(t, ctrl, nil, false, connectionOptCryptoSetup(cs))
	releaseUnprocessedQueueAtCleanup(t, tc)

	small := bytes.Repeat([]byte{'s'}, 100)
	large := bytes.Repeat([]byte{'l'}, protocol.MaxPacketBufferSize+1)
	packetIn := func(buffer *packetBuffer, data []byte) receivedPacket {
		buffer.Data = append(buffer.Data, data...)
		return receivedPacket{buffer: buffer, data: buffer.Data, remoteAddr: tc.remoteAddr, rcvTime: monotime.Now()}
	}

	// Below 256, a managed reader's 64 KiB buffer and a large-tier buffer
	// are queued as delivered.
	coalescedBelow := getCoalescedPacketBuffer()
	largeBelow := getLargePacketBuffer()
	tc.conn.handlePacket(packetIn(coalescedBelow, small))
	tc.conn.handlePacket(packetIn(largeBelow, large))
	queueTestPacket(t, tc, protocol.MaxConnUnprocessedPackets-2)
	confirmHandshakeForQueue(t, cs, tc)

	// Beyond 256, a small packet in a 64 KiB buffer is copied down.
	coalescedAbove := getCoalescedPacketBuffer()
	tc.conn.handlePacket(packetIn(coalescedAbove, small))
	require.Zero(t, coalescedAbove.refCount, "the 64 KiB buffer must be released after the copy")
	// A small slab view is copied down too.
	smallSlab, smallViews := newCoalescedReadFixture(t, len(small), small)
	tc.conn.handlePacket(receivedPacket{data: smallViews[0].Data, buffer: smallViews[0], rcvTime: monotime.Now()})
	require.True(t, smallSlab.released(), "the queue must not pin the slab")
	require.Equal(t, protocol.MaxConnUnprocessedPackets+2, tc.conn.receivedPackets.Len())

	// A packet larger than the packet-sized tier is dropped, whether it is
	// in a large-tier buffer or an oversized slab view, which is never
	// charged against the retained-bytes budget.
	largeAbove := getLargePacketBuffer()
	tc.conn.handlePacket(packetIn(largeAbove, large))
	require.Zero(t, largeAbove.refCount, "a dropped packet must be released")
	bigSlab, bigViews := newCoalescedReadFixture(t, 30000, testCoalescedSegments(1, 30000)...)
	tc.conn.handlePacket(receivedPacket{data: bigViews[0].Data, buffer: bigViews[0], rcvTime: monotime.Now()})
	require.True(t, bigSlab.released(), "a dropped view must be released")
	require.Zero(t, tc.conn.coalescedRetention.retained.Load())
	require.Equal(t, protocol.MaxConnUnprocessedPackets+2, tc.conn.receivedPackets.Len())
	require.EqualValues(t, 2, tc.conn.UnprocessedPacketDropsV1())

	var queued []receivedPacket
	for !tc.conn.receivedPackets.Empty() {
		queued = append(queued, tc.conn.receivedPackets.PopFront())
	}
	defer func() {
		for _, p := range queued {
			p.buffer.Release()
		}
	}()
	require.Same(t, coalescedBelow, queued[0].buffer)
	require.Same(t, largeBelow, queued[1].buffer)
	for _, p := range queued[protocol.MaxConnUnprocessedPackets:] {
		require.Nil(t, p.buffer.slab)
		require.Equal(t, protocol.MaxPacketBufferSize, cap(p.buffer.Data))
		require.Equal(t, small, p.data)
	}
}

// A read slot is one byte larger than a packet-sized buffer. Beyond 256 it is
// queued as delivered, without a copy (#857).
func TestConnectionUnprocessedQueueKeepsReadSlotsBeyond256(t *testing.T) {
	ctrl := gomock.NewController(t)
	cs := mocks.NewMockCryptoSetup(ctrl)
	tc := newServerTestConnection(t, ctrl, nil, false, connectionOptCryptoSetup(cs))
	releaseUnprocessedQueueAtCleanup(t, tc)
	queueTestPacket(t, tc, protocol.MaxConnUnprocessedPackets)
	confirmHandshakeForQueue(t, cs, tc)

	slot := getReadSlotBuffer()
	slot.Data = append(slot.Data, bytes.Repeat([]byte{'s'}, 100)...)
	tc.conn.handlePacket(receivedPacket{buffer: slot, data: slot.Data, remoteAddr: tc.remoteAddr, rcvTime: monotime.Now()})
	require.Equal(t, protocol.MaxConnUnprocessedPackets+1, tc.conn.receivedPackets.Len())
	require.Equal(t, 1, slot.refCount, "the queue holds the slot")
	var last receivedPacket
	for !tc.conn.receivedPackets.Empty() {
		last = tc.conn.receivedPackets.PopFront()
		if last.buffer != slot {
			last.buffer.Release()
		}
	}
	require.Same(t, slot, last.buffer)
	last.buffer.Release()
}
