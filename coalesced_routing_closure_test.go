package quic

import (
	"bytes"
	"sync"
	"testing"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/internal/utils"

	"github.com/stretchr/testify/require"
)

// Routing-closure tests for coalesced reads (Slice G2): sibling segment
// views of one slab can carry different connection IDs and reach different
// terminal routing outcomes — established connections, server admission,
// unknown-CID handling, rejection, and queue overflow — that release
// concurrently. Every view must reach exactly one outcome and the slab must
// recycle exactly once, after its last view releases. Run under -race.

func longHeaderPacketWithCID(t *testing.T, size int, cid []byte) []byte {
	t.Helper()
	require.GreaterOrEqual(t, size, 7+len(cid))
	b := make([]byte, size)
	b[0] = 0xc0 // long header, fixed bit
	copy(b[1:5], []byte{0, 0, 0, 1})
	b[5] = byte(len(cid))
	copy(b[6:], cid)
	b[6+len(cid)] = 0 // src conn ID length
	return b
}

func shortHeaderPacketWithCID(t *testing.T, size int, cid []byte) []byte {
	t.Helper()
	require.GreaterOrEqual(t, size, 1+len(cid))
	b := make([]byte, size)
	b[0] = 0x40 // short header, fixed bit
	copy(b[1:], cid)
	return b
}

// releasingPacketHandler releases every received packet's buffer on a
// goroutine gated by start, so sibling views release concurrently.
type releasingPacketHandler struct {
	start <-chan struct{}
	wg    *sync.WaitGroup
	data  [][]byte // packet contents recorded at dispatch, before release
}

func (h *releasingPacketHandler) handlePacket(p receivedPacket) {
	h.data = append(h.data, bytes.Clone(p.data))
	h.wg.Go(func() {
		<-h.start
		p.buffer.Release()
	})
}

func (h *releasingPacketHandler) destroy(error)                                   {}
func (h *releasingPacketHandler) closeWithTransportError(qerr.TransportErrorCode) {}

// class: sibling views routed to two established connections that release
// concurrently — the slab must recycle exactly once.
func TestCoalescedRoutingMixedConnectionIDs(t *testing.T) {
	cidA, cidB := []byte{1, 1, 1, 1, 1, 1, 1, 1}, []byte{2, 2, 2, 2, 2, 2, 2, 2}
	pktA := longHeaderPacketWithCID(t, 1200, cidA)
	pktB := longHeaderPacketWithCID(t, 1200, cidB)
	slab, views := newCoalescedReadFixture(t, 1200, pktA, pktB)

	start := make(chan struct{})
	var wg sync.WaitGroup
	hA := &releasingPacketHandler{start: start, wg: &wg}
	hB := &releasingPacketHandler{start: start, wg: &wg}
	tr := &Transport{
		handlers: map[protocol.ConnectionID]packetHandler{
			protocol.ParseConnectionID(cidA): hA,
			protocol.ParseConnectionID(cidB): hB,
		},
		logger: utils.DefaultLogger,
	}

	tr.handlePacket(fixturePacket(views[0]))
	tr.handlePacket(fixturePacket(views[1]))
	require.False(t, slab.released(), "views still hold the slab before release")
	close(start)
	wg.Wait()
	require.True(t, slab.released(), "slab must recycle exactly once after concurrent sibling release")
	require.Equal(t, [][]byte{pktA}, hA.data)
	require.Equal(t, [][]byte{pktB}, hB.data)
}

// class: one sibling reaches server admission (an Initial-path hold), the
// other an established connection — plus the short-header-remainder rule:
// each view is parsed as its own datagram, so a short-header sibling consumes
// only its own segment.
func TestCoalescedRoutingInitialAndShortHeaderSiblings(t *testing.T) {
	cid := []byte{3, 3, 3, 3, 3, 3, 3, 3}
	pktInitial := longHeaderPacketWithCID(t, 1200, []byte{9, 9, 9, 9, 9, 9, 9, 9})
	pktShort := shortHeaderPacketWithCID(t, 1200, cid)
	slab, views := newCoalescedReadFixture(t, 1200, pktInitial, pktShort)

	start := make(chan struct{})
	close(start)
	var wg sync.WaitGroup
	h := &releasingPacketHandler{start: start, wg: &wg}
	srv := &baseServer{
		receivedPackets: make(chan receivedPacket, 8),
		errorChan:       make(chan struct{}),
		logger:          utils.DefaultLogger,
	}
	tr := &Transport{
		handlers:  map[protocol.ConnectionID]packetHandler{protocol.ParseConnectionID(cid): h},
		connIDLen: 8,
		server:    srv,
		logger:    utils.DefaultLogger,
	}

	tr.handlePacket(fixturePacket(views[0]))
	tr.handlePacket(fixturePacket(views[1]))
	wg.Wait()

	q := <-srv.receivedPackets
	require.Nil(t, q.buffer.slab, "the server-queued sibling must be copied out of the slab")
	require.True(t, slab.released(), "queue copy and connection release must free the slab")
	require.Equal(t, pktInitial, q.data, "the admission copy is exactly its own segment")
	require.Equal(t, [][]byte{pktShort}, h.data, "a short-header view consumes only its own segment")
	q.buffer.Release()
}

// class: sibling views with unknown connection IDs — one queued for a
// stateless reset, one dropped without a reset key — each reach exactly one
// terminal outcome and the slab recycles once.
func TestCoalescedRoutingUnknownCIDStatelessReset(t *testing.T) {
	t.Run("reset queued", func(t *testing.T) {
		pkt := shortHeaderPacketWithCID(t, 1200, []byte{7, 7, 7, 7, 7, 7, 7, 7})
		slab, views := newCoalescedReadFixture(t, 1200, pkt, pkt)
		tr := &Transport{
			handlers:            map[protocol.ConnectionID]packetHandler{},
			connIDLen:           8,
			StatelessResetKey:   &StatelessResetKey{},
			statelessResetQueue: make(chan receivedPacket, 4),
			logger:              utils.DefaultLogger,
		}
		tr.handlePacket(fixturePacket(views[0]))
		q := <-tr.statelessResetQueue
		require.Nil(t, q.buffer.slab, "the reset-queued sibling must be copied out of the slab")
		views[1].Release()
		require.True(t, slab.released())
		q.buffer.Release()
	})
	t.Run("dropped without reset key", func(t *testing.T) {
		pkt := shortHeaderPacketWithCID(t, 1200, []byte{7, 7, 7, 7, 7, 7, 7, 7})
		slab, views := newCoalescedReadFixture(t, 1200, pkt, pkt)
		tr := &Transport{
			handlers:  map[protocol.ConnectionID]packetHandler{},
			connIDLen: 8,
			logger:    utils.DefaultLogger,
		}
		tr.handlePacket(fixturePacket(views[0]))
		require.False(t, slab.released(), "the sibling still holds the slab")
		views[1].Release()
		require.True(t, slab.released(), "the dropped view must have released exactly once")
	})
}

// class: a sibling rejected at admission while another sibling is queued —
// here the queued sibling is oversized, so it kept the slab under a budget
// charge; the rejection must not free the slab out from under it.
func TestCoalescedRoutingRejectedSiblingKeepsSlabForQueuedView(t *testing.T) {
	const segSize = protocol.MaxLargePacketBufferSize + 1000
	segs := testCoalescedSegments(2, segSize)
	slab, views := newCoalescedReadFixture(t, segSize, segs...)
	srv := &baseServer{
		receivedPackets: make(chan receivedPacket, 8),
		errorChan:       make(chan struct{}),
		logger:          utils.DefaultLogger,
	}

	srv.handlePacket(fixturePacket(views[0]))
	require.EqualValues(t, protocol.MaxCoalescedPacketBufferSize, srv.coalescedRetention.retained.Load(),
		"an oversized queued view keeps its slab under a budget charge")

	close(srv.errorChan) // the server begins rejecting at admission
	srv.handlePacket(fixturePacket(views[1]))
	require.False(t, slab.released(), "rejecting a sibling must not free the queued view's slab")

	q := <-srv.receivedPackets
	require.Equal(t, segs[0], q.data)
	q.buffer.Release()
	require.True(t, slab.released())
	require.Zero(t, srv.coalescedRetention.retained.Load(), "release must refund the budget")
}

// class: queue overflow drops a view while its sibling is still in flight —
// the drop releases exactly one hold and the slab survives for the sibling.
func TestCoalescedRoutingQueueOverflowWithSiblingInFlight(t *testing.T) {
	pkt := longHeaderPacketWithCID(t, 1200, []byte{5, 5, 5, 5, 5, 5, 5, 5})
	slab, views := newCoalescedReadFixture(t, 1200, pkt, pkt)
	srv := &baseServer{
		receivedPackets: make(chan receivedPacket, 1),
		errorChan:       make(chan struct{}),
		logger:          utils.DefaultLogger,
	}
	// fill the queue with an ordinary packet
	full := getPacketBuffer()
	full.Data = append(full.Data, 0x40)
	srv.handlePacket(receivedPacket{data: full.Data, buffer: full})

	srv.handlePacket(fixturePacket(views[0])) // overflows: dropped and released
	require.False(t, slab.released(), "the in-flight sibling still holds the slab")
	views[1].Release()
	require.True(t, slab.released())

	q := <-srv.receivedPackets
	q.buffer.Release()
}

// class: the short tail segment of a coalesced read routes and releases like
// any full-size sibling.
func TestCoalescedRoutingShortTailSegment(t *testing.T) {
	cidA, cidB := []byte{1, 2, 3, 4, 5, 6, 7, 8}, []byte{8, 7, 6, 5, 4, 3, 2, 1}
	pktA := longHeaderPacketWithCID(t, 1200, cidA)
	tail := longHeaderPacketWithCID(t, 300, cidB)
	slab, views := newCoalescedReadFixture(t, 1200, pktA, tail)

	start := make(chan struct{})
	close(start)
	var wg sync.WaitGroup
	hA := &releasingPacketHandler{start: start, wg: &wg}
	hB := &releasingPacketHandler{start: start, wg: &wg}
	tr := &Transport{
		handlers: map[protocol.ConnectionID]packetHandler{
			protocol.ParseConnectionID(cidA): hA,
			protocol.ParseConnectionID(cidB): hB,
		},
		logger: utils.DefaultLogger,
	}
	tr.handlePacket(fixturePacket(views[0]))
	tr.handlePacket(fixturePacket(views[1]))
	wg.Wait()
	require.True(t, slab.released())
	require.Equal(t, [][]byte{tail}, hB.data, "the tail view carries exactly the tail bytes")
}
