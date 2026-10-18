package ackhandler

import (
	"strings"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

func newBBRECNTestHandler() (*sentPacketHandler, *congestionRecorder) {
	r := &congestionRecorder{}
	h := newCongestionTestHandler(r)
	EnableBBRECN(h, func(bool) (uint64, bool, bool) { return h.congestionEvents.pathGeneration, true, true })
	return h, r
}

func sendBBRECNPacket(h *sentPacketHandler, mark protocol.ECN, pathProbe bool) protocol.PacketNumber {
	pn := h.PopPacketNumber(protocol.Encryption1RTT)
	h.SentPacket(monotime.Now(), pn, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, mark, 1200, false, pathProbe)
	return pn
}

func ackBBRECN(t *testing.T, h *sentPacketHandler, ranges []wire.AckRange, ect0, ce uint64) {
	t.Helper()
	_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ranges, ECT0: ect0, ECNCE: ce}, protocol.Encryption1RTT, monotime.Now().Add(time.Millisecond))
	require.NoError(t, err)
}

func TestBBRECNActualMarkingHoles(t *testing.T) {
	for _, lifecycle := range []string{"ACK", "0-RTT rejection", "Retry"} {
		t.Run("known unmarked 0-RTT/"+lifecycle, func(t *testing.T) {
			h, r := newBBRECNTestHandler()
			now := monotime.Now()
			sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1000)
			early := sendCongestionTestPacket(h, now, protocol.Encryption0RTT, 1000)
			switch lifecycle {
			case "0-RTT rejection":
				h.DropPackets(protocol.Encryption0RTT, now.Add(time.Millisecond))
			case "Retry":
				h.ResetForRetry(now.Add(time.Millisecond))
			}
			// Recovery and delivery disposal don't erase actual unmarked facts.
			if lifecycle == "Retry" {
				sendBBRECNPacket(h, protocol.ECNNon, false)
			}
			ackBBRECN(t, h, ackRanges(early), 0, 0)
			require.False(t, r.feedback[len(r.feedback)-1].ECN.Eligible)
			require.False(t, r.feedback[len(r.feedback)-1].ECN.Failed)
			require.Equal(t, protocol.ECT0, h.ECNMode(true))
			fresh := sendBBRECNPacket(h, h.ECNMode(true), false)
			ackBBRECN(t, h, ackRanges(fresh), 1, 0)
			require.True(t, r.feedback[len(r.feedback)-1].ECN.Eligible)
		})
	}

	for _, pathProbe := range []bool{false, true} {
		t.Run(map[bool]string{false: "coalesced hole", true: "path probe hole"}[pathProbe], func(t *testing.T) {
			h, r := newBBRECNTestHandler()
			first := sendBBRECNPacket(h, h.ECNMode(true), false)
			hole := sendBBRECNPacket(h, protocol.ECNNon, pathProbe)
			skipped := h.PopPacketNumber(protocol.Encryption1RTT)
			h.appDataPackets.history.SkippedPacket(skipped)
			last := sendBBRECNPacket(h, h.ECNMode(true), false)
			ackBBRECN(t, h, ackRanges(first, hole, last), 2, 0)
			require.Equal(t, protocol.ECT0, h.ECNMode(true))
			require.True(t, r.feedback[len(r.feedback)-1].ECN.Eligible)
			require.Equal(t, congestion.ECNCounts{ECT0: 2}, r.feedback[len(r.feedback)-1].ECN.Delta)
			require.Equal(t, uint64(3), r.feedback[len(r.feedback)-1].ECN.Ordinal)
		})
	}
}

func TestBBRECNAdvancingLateOnlyFeedback(t *testing.T) {
	h, r := newBBRECNTestHandler()
	first := sendBBRECNPacket(h, h.ECNMode(true), false)
	ackBBRECN(t, h, ackRanges(first), 1, 0)
	late := sendBBRECNPacket(h, h.ECNMode(true), false)
	require.True(t, h.QueueProbePacket(protocol.Encryption1RTT))
	h.ExpireDelivery(monotime.Now().Add(time.Minute))
	ackBBRECN(t, h, ackRanges(late), 1, 1)
	e := r.feedback[len(r.feedback)-1]
	require.Empty(t, e.Acked)
	require.True(t, e.ECN.Eligible)
	require.Equal(t, uint64(1), e.ECN.Delta.CE)
	require.Equal(t, uint64(2), e.ECN.Ordinal)
	ackBBRECN(t, h, ackRanges(late), 1, 1)
	require.False(t, r.feedback[len(r.feedback)-1].ECN.Eligible)
}

func TestBBRECNReorderedAndInvalidCounters(t *testing.T) {
	for _, name := range []string{"decreased", "missing", "excess CE", "remarked", "skipped", "reordered"} {
		t.Run(name, func(t *testing.T) {
			h, r := newBBRECNTestHandler()
			first := sendBBRECNPacket(h, h.ECNMode(true), false)
			ackBBRECN(t, h, ackRanges(first), 1, 0)
			next := sendBBRECNPacket(h, h.ECNMode(true), false)
			switch name {
			case "decreased", "missing":
				ackBBRECN(t, h, ackRanges(next), 0, 0)
			case "excess CE":
				ackBBRECN(t, h, ackRanges(next), 1, 2)
			case "remarked":
				_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(next), ECT0: 1, ECT1: 1}, protocol.Encryption1RTT, monotime.Now())
				require.NoError(t, err)
			case "skipped":
				// A forgotten unsent number must not acquire marking or ordinal authority.
				gap := h.PopPacketNumber(protocol.Encryption1RTT)
				h.appDataPackets.history.SkippedPacket(gap)
				sendBBRECNPacket(h, protocol.ECNNon, false)
				_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(gap), ECT0: 2}, protocol.Encryption1RTT, monotime.Now())
				var transportErr *qerr.TransportError
				require.ErrorAs(t, err, &transportErr)
				require.Equal(t, qerr.ProtocolViolation, transportErr.ErrorCode)
				require.Equal(t, protocol.ECT0, h.ECNMode(true))
				require.Len(t, r.feedback, 1, "rejected ACK cannot reach the validator")
				return
			case "reordered":
				ackBBRECN(t, h, ackRanges(first), 0, 0)
				require.Equal(t, protocol.ECT0, h.ECNMode(true))
				ackBBRECN(t, h, ackRanges(next), 2, 0)
				require.True(t, r.feedback[len(r.feedback)-1].ECN.Eligible)
				return
			}
			require.Equal(t, protocol.ECNNon, h.ECNMode(true))
			require.False(t, r.feedback[len(r.feedback)-1].ECN.Eligible)
		})
	}
}

func TestBBRECNRangeBudgetFallback(t *testing.T) {
	t.Run("compress ACKed suffix behind unresolved loss", func(t *testing.T) {
		h, r := newBBRECNTestHandler()
		first := sendBBRECNPacket(h, h.ECNMode(true), false)
		ackBBRECN(t, h, ackRanges(first), 1, 0)
		sendBBRECNPacket(h, h.ECNMode(true), false) // unresolved marked packet 1
		for pn := protocol.PacketNumber(2); pn <= 4200; pn++ {
			h.SentPacket(monotime.Now(), pn, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, h.ECNMode(true), 1200, false, false)
			// The peer keeps reporting packet 0, so the hole stays inside every
			// reported span and is never retired.
			ackBBRECN(t, h, []wire.AckRange{{Smallest: 2, Largest: pn}, {Smallest: 0, Largest: 0}}, uint64(pn), 0)
		}
		require.Equal(t, protocol.ECT0, h.ECNMode(true))
		require.Len(t, h.bbrECN.ranges, 2, "one unresolved hole and one affine ACKed suffix")
		e := r.feedback[len(r.feedback)-1].ECN
		require.True(t, e.Eligible)
		t.Logf("equivalent suffix: ranges=%d marking=%v eligible=%v accepted=%+v", len(h.bbrECN.ranges), h.ECNMode(true), e.Eligible, e.Accepted)
	})

	t.Run("insertion", func(t *testing.T) {
		h, r := newBBRECNTestHandler()
		first := sendBBRECNPacket(h, h.ECNMode(true), false)
		ackBBRECN(t, h, ackRanges(first), 1, 0)
		for i := range 4100 {
			mark := protocol.ECNNon
			if i%2 == 0 {
				mark = h.ECNMode(true)
			}
			sendBBRECNPacket(h, mark, false)
		}
		require.Equal(t, protocol.ECNNon, h.ECNMode(true))
		require.LessOrEqual(t, len(h.bbrECN.ranges), 4096)
		last := h.appDataPackets.largestSent
		ackBBRECN(t, h, ackRanges(last), 1, 1)
		require.False(t, r.feedback[len(r.feedback)-1].ECN.Eligible)
		require.Equal(t, congestion.ECNCounts{ECT0: 1}, r.feedback[len(r.feedback)-1].ECN.Accepted)
	})
	t.Run("ACK splits", func(t *testing.T) {
		h, _ := newBBRECNTestHandler()
		first := sendBBRECNPacket(h, h.ECNMode(true), false)
		ackBBRECN(t, h, ackRanges(first), 1, 0)
		var ranges []wire.AckRange
		// Bypass random skipping, but use real recovery registration for one
		// contiguous wire packet-number run and one sparse decoded ACK.
		for pn := protocol.PacketNumber(1); pn <= 8200; pn++ {
			h.SentPacket(monotime.Now(), pn, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, protocol.ECT0, 1200, false, false)
			if pn%2 == 0 {
				ranges = append([]wire.AckRange{{Smallest: pn, Largest: pn}}, ranges...)
			}
		}
		ackBBRECN(t, h, ranges, 4101, 0)
		require.Equal(t, protocol.ECNNon, h.ECNMode(true))
		require.LessOrEqual(t, len(h.bbrECN.ranges), 4096)
	})
	t.Run("accounted prefix compacts", func(t *testing.T) {
		h, _ := newBBRECNTestHandler()
		for i := range 4200 {
			pn := sendBBRECNPacket(h, h.ECNMode(true), false)
			ackBBRECN(t, h, ackRanges(pn), uint64(i+1), 0)
		}
		require.Equal(t, protocol.ECT0, h.ECNMode(true))
		require.LessOrEqual(t, len(h.bbrECN.ranges), 1)
	})
}

func TestBBRECNDistinctACKedSuffixPinsRangeBudget(t *testing.T) {
	h, r := newBBRECNTestHandler()
	first := sendBBRECNPacket(h, h.ECNMode(true), false)
	ackBBRECN(t, h, ackRanges(first), 1, 0)
	pinned := sendBBRECNPacket(h, h.ECNMode(true), false)
	require.Equal(t, protocol.PacketNumber(1), pinned)
	ect0 := uint64(1)
	// Register contiguous packet numbers directly to exclude random skips.
	// Alternating actual codepoints keep the ACKed suffix distinct. The peer
	// keeps reporting packet 0, so the hole stays inside every reported span:
	// retirement (#803) cannot free these records, and the budget still binds.
	for pn := protocol.PacketNumber(2); pn <= maxECNMarkRanges; pn++ {
		mark := protocol.ECNNon
		if pn%2 != 0 {
			mark = h.ECNMode(true)
			ect0++
		}
		h.SentPacket(monotime.Now(), pn, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, mark, 1200, false, false)
		ackBBRECN(t, h, []wire.AckRange{{Smallest: 2, Largest: pn}, {Smallest: 0, Largest: 0}}, ect0, 0)
	}
	require.Len(t, h.bbrECN.ranges, maxECNMarkRanges)
	require.False(t, h.bbrECN.ranges[0].acked)
	require.Equal(t, pinned, h.bbrECN.ranges[0].first)
	for _, entry := range h.bbrECN.ranges[1:] {
		require.True(t, entry.acked)
	}
	before := r.feedback[len(r.feedback)-1].ECN
	require.True(t, before.Eligible)
	require.Equal(t, protocol.ECT0, h.ECNMode(true))
	t.Logf("distinct suffix at capacity: ranges=%d marking=%v eligible=%v accepted=%+v", len(h.bbrECN.ranges), h.ECNMode(true), before.Eligible, before.Accepted)

	const overflow = protocol.PacketNumber(maxECNMarkRanges + 1)
	h.SentPacket(monotime.Now(), overflow, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, h.ECNMode(true), 1200, false, false)
	require.Equal(t, protocol.ECNNon, h.ECNMode(true))
	require.True(t, h.bbrECN.evidenceLost)
	require.False(t, h.bbrECN.counterFailed)
	require.Len(t, h.bbrECN.ranges, maxECNMarkRanges)
	// An advancing ACK also reports the old hole, but lost ledger evidence
	// prevents this from manufacturing fresh eligibility or accepted counters.
	ackBBRECN(t, h, ackRanges(pinned, overflow), ect0+2, 0)
	after := r.feedback[len(r.feedback)-1].ECN
	require.Len(t, h.bbrECN.ranges, maxECNMarkRanges)
	require.LessOrEqual(t, cap(h.bbrECN.ranges), maxECNMarkRanges)
	require.Equal(t, protocol.ECNNon, h.ECNMode(true))
	require.True(t, after.Failed)
	require.False(t, after.Eligible)
	require.Equal(t, before.Accepted, after.Accepted)
	require.Equal(t, before.Watermark, after.Watermark)
	require.Zero(t, after.Ordinal)
	t.Logf("distinct suffix after overflow and late ACK: ranges=%d marking=%v eligible=%v accepted=%+v failed=%v", len(h.bbrECN.ranges), h.ECNMode(true), after.Eligible, after.Accepted, after.Failed)
}

// ordinaryLossPath sends blocks of contiguous packet numbers at the sender's
// current marking and acknowledges all but each block's leading lost packets.
// Every ACK repeats the previous block's range, as a peer does, so each hole
// lies inside the acknowledged span and real loss detection declares it lost.
type ordinaryLossPath struct {
	h    *sentPacketHandler
	next protocol.PacketNumber
	prev wire.AckRange
	ect0 uint64 // the peer's cumulative ECT(0) count
}

const ordinaryLossBlock = 64

func newOrdinaryLossPath(t *testing.T, h *sentPacketHandler) *ordinaryLossPath {
	t.Helper()
	// Register packet numbers directly to exclude random skips.
	h.SentPacket(monotime.Now(), 0, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, h.ECNMode(true), 1200, false, false)
	ackBBRECN(t, h, ackRanges(0), 1, 0)
	require.Equal(t, protocol.ECT0, h.ECNMode(true))
	return &ordinaryLossPath{h: h, next: 1, prev: wire.AckRange{Smallest: 0, Largest: 0}, ect0: 1}
}

func (p *ordinaryLossPath) lossEvent(t *testing.T, lost protocol.PacketNumber) {
	t.Helper()
	first := p.next
	for pn := first; pn < first+ordinaryLossBlock; pn++ {
		mark := p.h.ECNMode(true)
		p.h.SentPacket(monotime.Now(), pn, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, mark, 1200, false, false)
		if pn >= first+lost && mark == protocol.ECT0 {
			p.ect0++
		}
	}
	p.next = first + ordinaryLossBlock
	acked := wire.AckRange{Smallest: first + lost, Largest: p.next - 1}
	ackBBRECN(t, p.h, []wire.AckRange{acked, p.prev}, p.ect0, 0)
	p.prev = acked
}

// TestBBRECNOrdinaryLossKeepsECN was TestBBRECNOrdinaryLossExhaustsRangeBudget,
// which characterized issue #793: a really lost packet is never ACKed, so its
// record pinned prefix compaction, every loss event added a hole and an ACKed
// suffix, and ordinary loss alone exhausted the range budget. With #803's
// retirement, each hole retires once an accepted ACK's reported span starts
// above it, so the ledger stays small and marking stays ECT(0) through twice
// the loss events that used to exhaust it. The legacy validator keeps no
// per-packet ledger and stays ECN-capable under the same pattern.
func TestBBRECNOrdinaryLossKeepsECN(t *testing.T) {
	for _, tc := range []struct {
		name string
		lost protocol.PacketNumber // leading packets of each block that are never ACKed
	}{
		{name: "isolated loss", lost: 1},
		{name: "paired loss", lost: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, r := newBBRECNTestHandler()
			p := newOrdinaryLossPath(t, h)
			// Twice the 2,048 loss events that exhausted the ledger before #803.
			const events = maxECNMarkRanges
			for event := 1; event <= events; event++ {
				p.lossEvent(t, tc.lost)
				require.Equal(t, protocol.ECT0, h.ECNMode(true), "loss event %d", event)
				// The previous hole lay below this ACK's span and retired; this
				// block's hole lies inside it and stays with its ACKed suffix.
				require.Len(t, h.bbrECN.ranges, 2, "loss event %d", event)
				require.False(t, h.bbrECN.ranges[0].acked)
				require.Equal(t, p.prev.Smallest-tc.lost, h.bbrECN.ranges[0].first)
			}
			var declaredLost int
			for _, e := range r.feedback {
				declaredLost += len(e.Lost)
			}
			require.Equal(t, events*int(tc.lost), declaredLost, "recovery declared every hole lost")
			require.False(t, h.bbrECN.evidenceLost)
			require.False(t, h.bbrECN.counterFailed)
			require.True(t, strings.HasPrefix(h.BBRDiagnostics(), "ecn=capable "), h.BBRDiagnostics())
			last := r.feedback[len(r.feedback)-1].ECN
			require.True(t, last.Eligible)
			require.Equal(t, congestion.ECNCounts{ECT0: p.ect0}, last.Accepted)
			t.Logf("%s: ECN stayed ECT(0) through %d loss events (%d lost packets); ranges=%d accepted=%+v", tc.name, events, events*int(tc.lost), len(h.bbrECN.ranges), last.Accepted)
		})
	}

	t.Run("legacy validator", func(t *testing.T) {
		h := NewSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, true, nil, protocol.PerspectiveServer, nil, utils.DefaultLogger).(*sentPacketHandler)
		p := newOrdinaryLossPath(t, h)
		// Twice the loss events that exhausted the BBR ledger before #803.
		for range maxECNMarkRanges {
			p.lossEvent(t, 1)
		}
		require.False(t, h.appDataPackets.history.HasOutstandingPackets(), "recovery declared every hole lost")
		require.Nil(t, h.bbrECN)
		require.Equal(t, protocol.ECT0, h.ECNMode(true))
	})
}

// sendBBRECNAt registers pn directly, excluding random packet-number skips.
func sendBBRECNAt(h *sentPacketHandler, pn protocol.PacketNumber, mark protocol.ECN) {
	h.SentPacket(monotime.Now(), pn, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, mark, 1200, false, false)
}

// newCapableBBRECNHandler validates ECN with packet 0. Its 100 ms RTT sample
// keeps packets that a test sends and acknowledges within a few milliseconds
// clear of the time threshold, so only the packet threshold declares loss.
func newCapableBBRECNHandler(t *testing.T) (*sentPacketHandler, *congestionRecorder) {
	t.Helper()
	h, r := newBBRECNTestHandler()
	now := monotime.Now()
	h.SentPacket(now, 0, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, h.ECNMode(true), 1200, false, false)
	_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(0), ECT0: 1}, protocol.Encryption1RTT, now.Add(100*time.Millisecond))
	require.NoError(t, err)
	require.Equal(t, ecnStateCapable, h.bbrECN.state)
	require.Empty(t, h.bbrECN.ranges)
	return h, r
}

// TestBBRECNRetiresRecordsBelowReportedSpan pins #803's retirement rule. Once
// ECN is capable, an accepted advancing ACK retires the records that end below
// its smallest acknowledged packet number. Holes inside its span and records
// above it stay, and recovery's own accounting is untouched. Deferred and
// failed ACKs retire nothing, and nothing retires while validation is pending,
// so late probe feedback can still establish capability.
func TestBBRECNRetiresRecordsBelowReportedSpan(t *testing.T) {
	// Packet numbers registered from 0 on one path have ordinal pn+1.
	ect0 := func(first, last protocol.PacketNumber, acked bool) ecnMarkRange {
		return ecnMarkRange{first: first, last: last, ordinal: uint64(first) + 1, mark: protocol.ECT0, acked: acked}
	}

	t.Run("below the span retires; the span and the tail stay", func(t *testing.T) {
		h, r := newCapableBBRECNHandler(t)
		for pn := protocol.PacketNumber(1); pn <= 6; pn++ {
			sendBBRECNAt(h, pn, protocol.ECT0)
		}
		// 1 and 2 lie below the span, 4 is a hole inside it and 6 lies above it.
		ackBBRECN(t, h, []wire.AckRange{{Smallest: 5, Largest: 5}, {Smallest: 3, Largest: 3}}, 3, 0)
		e := r.feedback[len(r.feedback)-1].ECN
		require.True(t, e.Eligible)
		require.Equal(t, uint64(6), e.Ordinal)
		require.Equal(t, []ecnMarkRange{ect0(4, 4, false), ect0(5, 5, true), ect0(6, 6, false)}, h.bbrECN.ranges)
		// Only accounted marks (0 and 3) become offsets; the ordinal boundary
		// passes over the retired 1 and 2 to packet 3.
		require.Equal(t, congestion.ECNCounts{ECT0: 2}, h.bbrECN.compacted)
		require.Equal(t, uint64(4), h.bbrECN.compactedOrdinal)
		require.Equal(t, protocol.ECT0, h.ECNMode(true))
	})

	t.Run("recovery still owns retired packets in flight", func(t *testing.T) {
		h, _ := newCapableBBRECNHandler(t)
		for pn := protocol.PacketNumber(1); pn <= 3; pn++ {
			sendBBRECNAt(h, pn, protocol.ECT0)
		}
		ackBBRECN(t, h, ackRanges(3), 2, 0)
		require.Empty(t, h.bbrECN.ranges, "1 and 2 retired below the span; 3 compacted")
		pn, _ := h.appDataPackets.history.FirstOutstanding()
		require.Equal(t, protocol.PacketNumber(1), pn, "below the packet threshold, so not declared lost")
		require.Equal(t, 2, h.appDataPackets.history.NumOutstanding())
	})

	t.Run("deferred and failed ACKs retire nothing", func(t *testing.T) {
		h, r := newCapableBBRECNHandler(t)
		for pn := protocol.PacketNumber(1); pn <= 4; pn++ {
			sendBBRECNAt(h, pn, protocol.ECT0)
		}
		ackBBRECN(t, h, []wire.AckRange{{Smallest: 4, Largest: 4}, {Smallest: 2, Largest: 2}}, 3, 0)
		kept := []ecnMarkRange{ect0(3, 3, false), ect0(4, 4, true)}
		require.Equal(t, kept, h.bbrECN.ranges)
		// The hole at 3 lies below this span, but the ACK does not advance.
		ackBBRECN(t, h, ackRanges(4), 3, 0)
		require.True(t, r.feedback[len(r.feedback)-1].ECN.Deferred)
		require.Equal(t, kept, h.bbrECN.ranges)
		sendBBRECNAt(h, 5, protocol.ECT0)
		ackBBRECN(t, h, ackRanges(5), 2, 0) // decreased counters
		require.True(t, r.feedback[len(r.feedback)-1].ECN.Failed)
		require.True(t, h.bbrECN.counterFailed)
		require.Equal(t, append(kept, ect0(5, 5, false)), h.bbrECN.ranges)
	})

	for _, tc := range []struct {
		name         string
		revalidation bool
		ceFirst      bool
	}{
		{name: "pending validation keeps unresolved probes"},
		{name: "pending validation keeps probes after a CE-first report", ceFirst: true},
		{name: "pending revalidation keeps unresolved probes", revalidation: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var h *sentPacketHandler
			var accepted uint64
			b := protocol.PacketNumber(0)
			if tc.revalidation {
				h, _ = newCapableBBRECNHandler(t)
				h.MigratedPath(monotime.Now(), 1200)
				accepted, b = 1, 1
			} else {
				h, _ = newBBRECNTestHandler()
			}
			// Ten probes, then Unknown sends an unmarked packet.
			for pn := b; pn < b+numECNTestingPackets; pn++ {
				sendBBRECNAt(h, pn, h.ECNMode(true))
			}
			require.Equal(t, ecnStateUnknown, h.bbrECN.state)
			unmarked := b + numECNTestingPackets
			sendBBRECNAt(h, unmarked, h.ECNMode(true))
			require.Equal(t, protocol.ECNNon, h.ECNMode(true))
			// The first advancing report starts above most probes.
			if tc.ceFirst {
				ackBBRECN(t, h, ackRanges(unmarked-1), accepted, 1)
			} else {
				ackBBRECN(t, h, ackRanges(unmarked), accepted, 0)
			}
			require.Equal(t, ecnStateUnknown, h.bbrECN.state)
			require.NotEmpty(t, h.bbrECN.ranges, "unresolved probes are not retired")
			require.Equal(t, b, h.bbrECN.ranges[0].first, "unresolved probes are not retired")
			// Late feedback for a probe establishes capability.
			next := unmarked + 1
			sendBBRECNAt(h, next, h.ECNMode(true))
			if tc.ceFirst {
				ackBBRECN(t, h, []wire.AckRange{{Smallest: unmarked - 2, Largest: next}}, accepted+1, 1)
			} else {
				ackBBRECN(t, h, []wire.AckRange{{Smallest: b, Largest: next}}, accepted+numECNTestingPackets, 0)
			}
			require.Equal(t, ecnStateCapable, h.bbrECN.state)
			require.Equal(t, protocol.ECT0, h.ECNMode(true))
			ect0, ce := accepted+numECNTestingPackets, uint64(0)
			if tc.ceFirst {
				// The establishing ACK is compacted before its own transition,
				// so the probes below its span stay for one more ACK.
				require.NotEmpty(t, h.bbrECN.ranges)
				require.Equal(t, b, h.bbrECN.ranges[0].first)
				ect0, ce = accepted+1, 1
			}
			// The next accepted advancing ACK on the capable path retires any
			// record left below its span.
			last := next + 1
			sendBBRECNAt(h, last, h.ECNMode(true))
			ackBBRECN(t, h, ackRanges(last), ect0+1, ce)
			require.Empty(t, h.bbrECN.ranges)
		})
	}

	t.Run("the post-reset drain retires nothing", func(t *testing.T) {
		h, r := newCapableBBRECNHandler(t)
		drained := false
		h.bbrECN.path = func(bool) (uint64, bool, bool) { return h.congestionEvents.pathGeneration, drained, true }
		sendBBRECNAt(h, 1, protocol.ECT0)
		sendBBRECNAt(h, 2, protocol.ECT0)
		h.MigratedPath(monotime.Now(), 1200)
		require.Equal(t, protocol.ECNNon, h.ECNMode(true), "draining the old generation")
		sendBBRECNAt(h, 3, protocol.ECNNon)
		// An accepted advancing ACK whose span starts above the old records.
		ackBBRECN(t, h, ackRanges(3), 1, 0)
		e := r.feedback[len(r.feedback)-1].ECN
		require.False(t, e.Deferred)
		require.False(t, e.Failed)
		require.Equal(t, protocol.PacketNumber(3), e.Watermark)
		require.True(t, h.bbrECN.draining)
		require.NotEmpty(t, h.bbrECN.ranges)
		require.Equal(t, ect0(1, 2, false), h.bbrECN.ranges[0], "old marking stays through the drain")
	})
}

// TestBBRECNLateACKOfRetiredRecord shows what a later receipt of a packet whose
// ledger record retired (#803) does. It adds no newly acknowledged mark, so the
// peer's counter surplus is consumed once by the first accepted advancing
// report, which loosens the too-few-counts check by that packet in that report
// only. A late CE still counts as congestion through the advancing anchor.
func TestBBRECNLateACKOfRetiredRecord(t *testing.T) {
	setup := func(t *testing.T) (*sentPacketHandler, *congestionRecorder) {
		h, r := newCapableBBRECNHandler(t)
		for pn := protocol.PacketNumber(1); pn <= 5; pn++ {
			sendBBRECNAt(h, pn, protocol.ECT0)
		}
		// Packet 1 lies below the span and retires.
		ackBBRECN(t, h, ackRanges(2, 3, 4), 4, 0)
		require.Equal(t, []ecnMarkRange{{first: 5, last: 5, ordinal: 6, mark: protocol.ECT0}}, h.bbrECN.ranges)
		require.Equal(t, congestion.ECNCounts{ECT0: 4}, h.bbrECN.compacted)
		return h, r
	}
	late := []wire.AckRange{{Smallest: 1, Largest: 5}}

	t.Run("ECT(0)", func(t *testing.T) {
		h, r := setup(t)
		ackBBRECN(t, h, late, 6, 0)
		e := r.feedback[len(r.feedback)-1]
		require.True(t, e.ECN.Eligible)
		require.Equal(t, congestion.ECNCounts{ECT0: 2}, e.ECN.Delta, "one newly acknowledged mark plus the retired packet's receipt")
		require.Equal(t, uint64(6), e.ECN.Ordinal)
		require.False(t, e.Congested)
		require.Equal(t, protocol.ECT0, h.ECNMode(true))
	})

	t.Run("CE", func(t *testing.T) {
		h, r := setup(t)
		ackBBRECN(t, h, late, 5, 1)
		e := r.feedback[len(r.feedback)-1]
		require.True(t, e.ECN.Eligible)
		require.Equal(t, congestion.ECNCounts{ECT0: 1, CE: 1}, e.ECN.Delta)
		require.Equal(t, uint64(6), e.ECN.Ordinal, "attributed to the advancing anchor")
		require.True(t, e.Congested)
	})

	t.Run("the check loosens by the retired packet", func(t *testing.T) {
		// The counts cover one of the two acknowledged packets. With packet 1
		// still recorded this fails; retired, only packet 5 is newly marked.
		h, r := setup(t)
		ackBBRECN(t, h, late, 5, 0)
		e := r.feedback[len(r.feedback)-1].ECN
		require.True(t, e.Eligible)
		require.False(t, e.Failed)
		require.Equal(t, congestion.ECNCounts{ECT0: 1}, e.Delta)
	})

	t.Run("and by no more", func(t *testing.T) {
		h, r := setup(t)
		sendBBRECNAt(h, 6, protocol.ECT0)
		ackBBRECN(t, h, []wire.AckRange{{Smallest: 1, Largest: 6}}, 5, 0)
		require.True(t, r.feedback[len(r.feedback)-1].ECN.Failed)
		require.True(t, h.bbrECN.counterFailed)
		require.Equal(t, protocol.ECNNon, h.ECNMode(true))
	})

	t.Run("a deferred receipt is consumed once", func(t *testing.T) {
		h, r := setup(t)
		ackBBRECN(t, h, ackRanges(1, 2, 3, 4), 5, 0)
		e := r.feedback[len(r.feedback)-1].ECN
		require.True(t, e.Deferred)
		require.Equal(t, congestion.ECNCounts{ECT0: 4}, e.Accepted)
		require.Equal(t, []ecnMarkRange{{first: 5, last: 5, ordinal: 6, mark: protocol.ECT0}}, h.bbrECN.ranges)
		// The next advancing report omits packet 1 but carries its count.
		ackBBRECN(t, h, ackRanges(5), 6, 0)
		e = r.feedback[len(r.feedback)-1].ECN
		require.True(t, e.Eligible)
		require.Equal(t, congestion.ECNCounts{ECT0: 2}, e.Delta)
		require.Equal(t, uint64(6), e.Ordinal)
		ackBBRECN(t, h, ackRanges(5), 6, 0)
		e = r.feedback[len(r.feedback)-1].ECN
		require.True(t, e.Deferred)
		require.Equal(t, congestion.ECNCounts{ECT0: 6}, e.Accepted)
	})
}

func TestBBRECNTestingVersusCapableCE(t *testing.T) {
	for _, name := range []string{"testing all CE", "testing loss and CE", "testing all lost", "capable all CE"} {
		t.Run(name, func(t *testing.T) {
			h, r := newBBRECNTestHandler()
			if name == "capable all CE" {
				pn := sendBBRECNPacket(h, h.ECNMode(true), false)
				ackBBRECN(t, h, ackRanges(pn), 1, 0)
				pn = sendBBRECNPacket(h, h.ECNMode(true), false)
				ackBBRECN(t, h, ackRanges(pn), 1, 1)
				require.Equal(t, protocol.ECT0, h.ECNMode(true))
				require.True(t, r.feedback[len(r.feedback)-1].Congested)
				return
			}
			var pns []protocol.PacketNumber
			for range 10 {
				pns = append(pns, sendBBRECNPacket(h, h.ECNMode(true), false))
			}
			if name == "testing all CE" {
				ackBBRECN(t, h, ackRanges(pns...), 0, 10)
			} else {
				// An ACK-only unmarked packet anchors time-loss detection without
				// manufacturing ECT receipts for the missing test transmissions.
				anchor := h.PopPacketNumber(protocol.Encryption1RTT)
				h.SentPacket(monotime.Now(), anchor, protocol.InvalidPacketNumber, nil, nil, protocol.Encryption1RTT, protocol.ECNNon, 50, false, false)
				ranges := ackRanges(anchor)
				ce := uint64(0)
				if name == "testing loss and CE" {
					ranges = ackRanges(pns[9], anchor)
					ce = 1
				}
				ackBBRECN(t, h, ranges, 0, ce)
				_, err := h.OnLossDetectionTimeout(h.GetLossDetectionTimeout().Add(time.Second))
				require.NoError(t, err)
			}
			require.Equal(t, protocol.ECNNon, h.ECNMode(true))
			require.True(t, h.bbrECN.state == ecnStateFailed)
			require.False(t, r.feedback[len(r.feedback)-1].Congested)
		})
	}
}

func TestBBRECNMigrationCounterFence(t *testing.T) {
	for _, capable := range []bool{true, false} {
		t.Run(map[bool]string{true: "qualified path", false: "unqualified path"}[capable], func(t *testing.T) {
			h, r := newBBRECNTestHandler()
			wantUnmarked := protocol.ECNNon
			if !capable {
				wantUnmarked = protocol.ECNUnsupported
			}
			drained := false
			h.bbrECN.path = func(bool) (uint64, bool, bool) { return h.congestionEvents.pathGeneration, drained, capable }
			// Initial validation doesn't require an old-generation drain.
			h.bbrECN.path = func(bool) (uint64, bool, bool) { return h.congestionEvents.pathGeneration, drained, true }
			first := sendBBRECNPacket(h, h.ECNMode(true), false)
			ackBBRECN(t, h, ackRanges(first), 1, 0)
			old := sendBBRECNPacket(h, h.ECNMode(true), false)
			h.MigratedPath(monotime.Now(), 1200)
			h.bbrECN.path = func(bool) (uint64, bool, bool) { return h.congestionEvents.pathGeneration, drained, capable }
			require.Equal(t, wantUnmarked, h.ECNMode(true))
			ackBBRECN(t, h, ackRanges(old), 1, 1)
			require.False(t, r.feedback[len(r.feedback)-1].ECN.Eligible)
			require.Equal(t, wantUnmarked, h.ECNMode(true))
			drained = true
			if !capable {
				require.Equal(t, wantUnmarked, h.ECNMode(true))
				return
			}
			require.Equal(t, protocol.ECT0, h.ECNMode(true))
			fresh := sendBBRECNPacket(h, h.ECNMode(true), false)
			ackBBRECN(t, h, ackRanges(fresh), 2, 1)
			e := r.feedback[len(r.feedback)-1].ECN
			require.True(t, e.Eligible)
			require.Equal(t, uint64(1), e.PathGeneration)
			require.Equal(t, congestion.ECNCounts{ECT0: 1}, e.Delta)
			require.Equal(t, congestion.ECNCounts{ECT0: 2, CE: 1}, e.Accepted)
		})
	}
}

func TestBBRECNMissingOldMarkedPacket(t *testing.T) {
	h, r := newBBRECNTestHandler()
	old := sendBBRECNPacket(h, h.ECNMode(true), false)
	h.MigratedPath(monotime.Now(), 1200)
	h.ExpireDelivery(monotime.Now().Add(time.Minute))
	require.Equal(t, protocol.ECNNon, h.ECNMode(true))
	fresh := sendBBRECNPacket(h, h.ECNMode(true), false)
	ackBBRECN(t, h, ackRanges(fresh), 0, 0)
	require.False(t, r.feedback[len(r.feedback)-1].ECN.Eligible)
	require.Equal(t, protocol.ECNNon, h.ECNMode(true))
	// A nonadvancing count-only report deliberately cannot establish the fence.
	ackBBRECN(t, h, ackRanges(old), 1, 0)
	require.Equal(t, protocol.ECNNon, h.ECNMode(true))
}

func TestBBRECNRepeatedMigration(t *testing.T) {
	for _, duringDrain := range []bool{false, true} {
		t.Run(map[bool]string{false: "invalid counters before reset", true: "invalid counters during drain"}[duringDrain], func(t *testing.T) {
			h, r := newBBRECNTestHandler()
			first := sendBBRECNPacket(h, h.ECNMode(true), false)
			ackBBRECN(t, h, ackRanges(first), 1, 0)
			drained := !duringDrain
			h.bbrECN.path = func(bool) (uint64, bool, bool) { return h.congestionEvents.pathGeneration, drained, true }
			if duringDrain {
				h.MigratedPath(monotime.Now(), 1200)
			}
			next := sendBBRECNPacket(h, protocol.ECNNon, false)
			ackBBRECN(t, h, ackRanges(next), 0, 0)
			require.True(t, r.feedback[len(r.feedback)-1].ECN.Failed)
			if !duringDrain {
				h.MigratedPath(monotime.Now(), 1200)
			}
			drained = true
			require.Equal(t, protocol.ECNNon, h.ECNMode(true), "a previously met fence cannot erase an invalid report")
			h.MigratedPath(monotime.Now(), 1200)
			require.Equal(t, protocol.ECNNon, h.ECNMode(true))
		})
	}

	t.Run("fresh tests forget old loss flags", func(t *testing.T) {
		h, _ := newBBRECNTestHandler()
		var old []protocol.PacketNumber
		for range 10 {
			old = append(old, sendBBRECNPacket(h, h.ECNMode(true), false))
		}
		anchor := sendBBRECNPacket(h, protocol.ECNNon, false)
		ackBBRECN(t, h, ackRanges(old[9], anchor), 0, 1)
		_, err := h.OnLossDetectionTimeout(h.GetLossDetectionTimeout().Add(time.Second))
		require.NoError(t, err)
		h.MigratedPath(monotime.Now(), 1200)
		next := sendBBRECNPacket(h, h.ECNMode(true), false)
		ackBBRECN(t, h, ackRanges(append(old, next)...), 9, 1)
		require.Equal(t, protocol.ECT0, h.ECNMode(true))
		for range 10 {
			sendBBRECNPacket(h, h.ECNMode(true), false)
		}
		last := sendBBRECNPacket(h, protocol.ECNNon, false)
		ackBBRECN(t, h, ackRanges(last), 9, 1)
		_, err = h.OnLossDetectionTimeout(h.GetLossDetectionTimeout().Add(time.Second))
		require.NoError(t, err)
		require.Equal(t, ecnStateFailed, h.bbrECN.state)
	})

	t.Run("unresolved marking survives repeated resets", func(t *testing.T) {
		h, r := newBBRECNTestHandler()
		old := sendBBRECNPacket(h, h.ECNMode(true), false)
		h.MigratedPath(monotime.Now(), 1200)
		h.MigratedPath(monotime.Now(), 1200)
		require.Equal(t, protocol.ECNNon, h.ECNMode(true))
		ackBBRECN(t, h, ackRanges(old), 1, 0)
		require.False(t, r.feedback[len(r.feedback)-1].ECN.Eligible)
		require.Equal(t, protocol.ECT0, h.ECNMode(true))
		// The fresh epoch gets ten tests, independent of the old count offsets.
		for range 10 {
			require.Equal(t, protocol.ECT0, h.ECNMode(true))
			sendBBRECNPacket(h, h.ECNMode(true), false)
		}
		require.Equal(t, protocol.ECNNon, h.ECNMode(true))
	})
	t.Run("no marked history waits only for old local debt", func(t *testing.T) {
		h, _ := newBBRECNTestHandler()
		drained := false
		h.bbrECN.path = func(bool) (uint64, bool, bool) { return h.congestionEvents.pathGeneration, drained, true }
		h.MigratedPath(monotime.Now(), 1200)
		require.Equal(t, protocol.ECNNon, h.ECNMode(true))
		drained = true
		require.Equal(t, protocol.ECT0, h.ECNMode(true))
	})
	t.Run("close retires the private ledger", func(t *testing.T) {
		h, _ := newBBRECNTestHandler()
		sendBBRECNPacket(h, h.ECNMode(true), false)
		h.CloseDelivery()
		require.Equal(t, protocol.ECNNon, h.ECNMode(true))
		require.Empty(t, h.bbrECN.ranges)
	})
}

func TestLegacyECNDispatchPreserved(t *testing.T) {
	for _, level := range []protocol.EncryptionLevel{protocol.EncryptionInitial, protocol.EncryptionHandshake} {
		t.Run(level.String()+" duplicate ACK", func(t *testing.T) {
			h, r := newBBRECNTestHandler()
			now := monotime.Now()
			pn := sendCongestionTestPacket(h, now, level, 1000)
			ack := &wire.AckFrame{AckRanges: ackRanges(pn)}
			_, err := h.ReceivedAck(ack, level, now.Add(time.Millisecond))
			require.NoError(t, err)
			require.Len(t, r.feedback, 1)
			_, err = h.ReceivedAck(ack, level, now.Add(2*time.Millisecond))
			require.NoError(t, err)
			require.Len(t, r.feedback, 1)
		})
	}

	h := NewSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, true, nil, protocol.PerspectiveServer, nil, utils.DefaultLogger).(*sentPacketHandler)
	first := sendBBRECNPacket(h, h.ECNMode(true), false)
	ackBBRECN(t, h, ackRanges(first), 1, 0)
	require.Nil(t, h.bbrECN)
	late := sendBBRECNPacket(h, h.ECNMode(true), false)
	require.True(t, h.QueueProbePacket(protocol.Encryption1RTT))
	// Legacy recovery returns before inspecting a late-only counter decrease.
	ackBBRECN(t, h, ackRanges(late), 0, 0)
	require.Equal(t, protocol.ECT0, h.ECNMode(true))
}
