package quic

import (
	"net"
	"testing"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"

	"github.com/stretchr/testify/require"
)

// Every queue that holds a receivedPacket past the routing-and-processing
// pass must pass it through retainForRetentionQueue first (datapath offload
// plan, 2026-09-11): a queued slab view is copied into an ordinary tier so it
// cannot pin its 64 KiB slab, and an oversized view is charged against the
// owner's retained-bytes budget. G1 wired the per-connection queues; these
// tests cover the transport- and server-owned holds that a Linux GRO producer
// reaches.

// a minimal long-header packet: type byte, version, 8-byte dest connection
// ID, empty source connection ID, padded to the requested size
func testLongHeaderPacketBytes(t *testing.T, size int) []byte {
	t.Helper()
	require.GreaterOrEqual(t, size, 15)
	b := make([]byte, size)
	b[0] = 0xc0 // long header, fixed bit
	copy(b[1:5], []byte{0, 0, 0, 1})
	b[5] = 8 // dest conn ID length
	copy(b[6:14], []byte{1, 2, 3, 4, 5, 6, 7, 8})
	b[14] = 0 // src conn ID length
	return b
}

func testShortHeaderPacketBytes(t *testing.T, size int) []byte {
	t.Helper()
	require.GreaterOrEqual(t, size, 1)
	b := make([]byte, size)
	b[0] = 0x40 // short header, fixed bit
	return b
}

func fixturePacket(view *packetBuffer) receivedPacket {
	return receivedPacket{
		remoteAddr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4242},
		data:       view.Data,
		buffer:     view,
	}
}

func TestServerQueueDoesNotPinCoalescedSlab(t *testing.T) {
	s := &baseServer{
		receivedPackets: make(chan receivedPacket, protocol.MaxServerUnprocessedPackets),
		errorChan:       make(chan struct{}),
		logger:          utils.DefaultLogger,
	}
	pkt := testLongHeaderPacketBytes(t, 1200)
	slab, views := newCoalescedReadFixture(t, 1200, pkt, pkt)

	s.handlePacket(fixturePacket(views[0]))

	q := <-s.receivedPackets
	require.Nil(t, q.buffer.slab, "a queued packet must not stay a slab view")
	views[1].Release()
	require.True(t, slab.released(), "the server queue must not pin the slab")
	require.Equal(t, pkt, q.data, "the queued copy must survive slab recycling")
	q.buffer.Release()
}

func TestServerQueueDropsOversizedViewWhenBudgetExhausted(t *testing.T) {
	s := &baseServer{
		receivedPackets: make(chan receivedPacket, protocol.MaxServerUnprocessedPackets),
		errorChan:       make(chan struct{}),
		logger:          utils.DefaultLogger,
	}
	require.True(t, s.coalescedRetention.tryCharge(protocol.MaxConnRetainedCoalescedBytes))

	pkt := testLongHeaderPacketBytes(t, protocol.MaxLargePacketBufferSize+1)
	slab, views := newCoalescedReadFixture(t, len(pkt), pkt, pkt)

	s.handlePacket(fixturePacket(views[0]))

	require.Empty(t, s.receivedPackets, "an unbudgetable oversized view must be dropped, not queued")
	views[1].Release()
	require.True(t, slab.released(), "the dropped view's slab hold must be released")
}

func TestZeroRTTQueueDoesNotPinCoalescedSlab(t *testing.T) {
	s := &baseServer{
		tr:            (*packetHandlerMap)(&Transport{}),
		zeroRTTQueues: map[protocol.ConnectionID]*zeroRTTQueue{},
		logger:        utils.DefaultLogger,
	}
	pkt := testLongHeaderPacketBytes(t, 1200)
	slab, views := newCoalescedReadFixture(t, 1200, pkt, pkt)

	require.True(t, s.handle0RTTPacket(fixturePacket(views[0])))

	require.Len(t, s.zeroRTTQueues, 1)
	for _, q := range s.zeroRTTQueues {
		require.Len(t, q.packets, 1)
		require.Nil(t, q.packets[0].buffer.slab, "a queued 0-RTT packet must not stay a slab view")
		require.Equal(t, pkt, q.packets[0].data)
	}
	views[1].Release()
	require.True(t, slab.released(), "the 0-RTT queue must not pin the slab")
}

func TestVersionNegotiationQueueDoesNotPinCoalescedSlab(t *testing.T) {
	s := &baseServer{
		versionNegotiationQueue: make(chan receivedPacket, 4),
		logger:                  utils.DefaultLogger,
	}
	pkt := testLongHeaderPacketBytes(t, 1300)
	slab, views := newCoalescedReadFixture(t, 1300, pkt, pkt)

	require.True(t, s.enqueueVersionNegotiationPacket(fixturePacket(views[0])))

	q := <-s.versionNegotiationQueue
	require.Nil(t, q.buffer.slab, "a queued VN packet must not stay a slab view")
	views[1].Release()
	require.True(t, slab.released(), "the version-negotiation queue must not pin the slab")
	require.Equal(t, pkt, q.data)
	q.buffer.Release()
}

func TestNonQUICQueueDoesNotPinCoalescedSlab(t *testing.T) {
	tr := &Transport{nonQUICPackets: make(chan receivedPacket, maxQueuedNonQUICPackets)}
	tr.readingNonQUICPackets.Store(true)

	payload := make([]byte, 1000)
	payload[0] = 0x01 // not a QUIC packet
	slab, views := newCoalescedReadFixture(t, 1000, payload, payload)

	tr.handleNonQUICPacket(fixturePacket(views[0]))

	q := <-tr.nonQUICPackets
	require.Nil(t, q.buffer.slab, "a queued non-QUIC packet must not stay a slab view")
	views[1].Release()
	require.True(t, slab.released(), "the non-QUIC queue must not pin the slab")
	require.Equal(t, payload, q.data)
	q.buffer.Release()
}

// An oversized non-QUIC view cannot be copied into an ordinary tier; it keeps
// its slab and is charged against the transport's retained-bytes budget,
// refunded when the consumer releases it.
func TestNonQUICQueueChargesOversizedView(t *testing.T) {
	tr := &Transport{nonQUICPackets: make(chan receivedPacket, maxQueuedNonQUICPackets)}
	tr.readingNonQUICPackets.Store(true)

	payload := make([]byte, protocol.MaxLargePacketBufferSize+1)
	payload[0] = 0x01
	slab, views := newCoalescedReadFixture(t, len(payload), payload)
	_ = slab

	tr.handleNonQUICPacket(fixturePacket(views[0]))

	q := <-tr.nonQUICPackets
	require.NotNil(t, q.buffer.slab, "an oversized view keeps its slab")
	require.EqualValues(t, protocol.MaxCoalescedPacketBufferSize, tr.coalescedRetention.retained.Load(),
		"an oversized view is charged at full slab size")
	q.buffer.Release()
	require.Zero(t, tr.coalescedRetention.retained.Load(), "release must refund the budget")
}

func TestStatelessResetQueueDoesNotPinCoalescedSlab(t *testing.T) {
	tr := &Transport{
		statelessResetQueue: make(chan receivedPacket, 4),
		StatelessResetKey:   &StatelessResetKey{},
	}
	pkt := testShortHeaderPacketBytes(t, 100)
	slab, views := newCoalescedReadFixture(t, 100, pkt, pkt)

	require.True(t, tr.maybeSendStatelessReset(fixturePacket(views[0])))

	q := <-tr.statelessResetQueue
	require.Nil(t, q.buffer.slab, "a queued stateless-reset trigger must not stay a slab view")
	views[1].Release()
	require.True(t, slab.released(), "the stateless-reset queue must not pin the slab")
	q.buffer.Release()
}
