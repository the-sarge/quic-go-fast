package ackhandler

import (
	"fmt"
	"slices"
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

// newAckOnlyTestHandler returns a confirmed server handler on one path.
func newAckOnlyTestHandler(t *testing.T, local, ecn bool) *sentPacketHandler {
	t.Helper()
	var h *sentPacketHandler
	if local {
		handler, err := NewLocalSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, ecn, nil, protocol.PerspectiveServer, nil, utils.DefaultLogger)
		require.NoError(t, err)
		h = handler.(*sentPacketHandler)
	} else {
		h = NewSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, ecn, nil, protocol.PerspectiveServer, nil, utils.DefaultLogger).(*sentPacketHandler)
	}
	now := monotime.Time(time.Second)
	h.DropPackets(protocol.EncryptionInitial, now)
	h.DropPackets(protocol.EncryptionHandshake, now)
	h.appDataPackets.pns = newSequentialPacketNumberGenerator(0)
	return h
}

// ackOnlyFrames is a packet whose only frame acknowledges received on path id.
func ackOnlyFrames(id protocol.PathID, received protocol.PacketNumber) []Frame {
	return []Frame{{Frame: &wire.AckFrame{HasPathID: id != 0, PathID: id, AckRanges: ackRanges(received)}}}
}

// sendUnregistered records a packet on path id without a local completion registration.
func sendUnregistered(h *sentPacketHandler, id protocol.PathID, now monotime.Time, frames []Frame, ecn protocol.ECN) protocol.PacketNumber {
	pn := h.RecoveryOnPath(id).PopPacketNumber(protocol.Encryption1RTT)
	h.SentPacketWithContext(PacketContext{PathID: id}, now, pn, protocol.InvalidPacketNumber, nil, frames, protocol.Encryption1RTT, ecn, 50, false, false)
	return pn
}

// registerLocal registers a local packet on path id, leaving its completion pending.
func registerLocal(t *testing.T, h *sentPacketHandler, id protocol.PathID, now monotime.Time, frames []Frame, ecn protocol.ECN) (protocol.PacketNumber, LocalCompletionToken) {
	t.Helper()
	pn := h.RecoveryOnPath(id).PopPacketNumber(protocol.Encryption1RTT)
	token, err := h.RegisterLocalPacket(PacketContext{PathID: id}, now, pn, protocol.InvalidPacketNumber, nil, frames, protocol.Encryption1RTT, ecn, 50)
	require.NoError(t, err)
	return pn, token
}

// sendAccepted registers a local packet and completes it as accepted.
func sendAccepted(t *testing.T, h *sentPacketHandler, id protocol.PathID, now monotime.Time, frames []Frame, ecn protocol.ECN) protocol.PacketNumber {
	t.Helper()
	pn, token := registerLocal(t, h, id, now, frames, ecn)
	require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now))
	return pn
}

// historyPNs lists the packet numbers of the records in a context's application history.
func historyPNs(h *sentPacketHandler) []protocol.PacketNumber {
	var pns []protocol.PacketNumber
	for pn := range h.appDataPackets.history.Packets() {
		pns = append(pns, pn)
	}
	return pns
}

func pnRange(lo, hi protocol.PacketNumber) []protocol.PacketNumber {
	var pns []protocol.PacketNumber
	for pn := lo; pn <= hi; pn++ {
		pns = append(pns, pn)
	}
	return pns
}

func TestAckOnlyRetirementSustainedTwoPaths(t *testing.T) {
	for _, local := range []bool{false, true} {
		t.Run(fmt.Sprintf("local=%t", local), func(t *testing.T) {
			h := newAckOnlyTestHandler(t, local, false)
			require.NoError(t, h.AddPath(17, 1200))
			now := monotime.Time(2 * time.Second)
			paths := []protocol.PathID{0, 17}
			// No peer acknowledgment ever arrives, and nothing is ack-eliciting.
			for n := range protocol.MaxTrackedSentPackets + 1000 {
				id := paths[n%2]
				path := h.path(id)
				require.NotEqual(t, SendNone, path.SendMode(now), "packet %d", n)
				if local {
					sendAccepted(t, h, id, now, ackOnlyFrames(id, protocol.PacketNumber(n)), protocol.ECNNon)
				} else {
					sendUnregistered(h, id, now, ackOnlyFrames(id, protocol.PacketNumber(n)), protocol.ECNNon)
				}
				now = now.Add(time.Microsecond)
			}
			for _, id := range paths {
				path := h.path(id)
				require.Len(t, historyPNs(path), protocol.MaxNonAckElicitingAcks, "path %d keeps only the newest completed records", id)
				require.NotEqual(t, SendNone, path.SendMode(now))
			}
			require.LessOrEqual(t, h.numTrackedPackets(), 2*(protocol.MaxNonAckElicitingAcks+maxSkippedPackets), "the shared span shrinks")
		})
	}
}

func TestAckOnlyRetirementPendingCompletion(t *testing.T) {
	h := newAckOnlyTestHandler(t, true, false)
	now := monotime.Time(2 * time.Second)
	pending, token := registerLocal(t, h, 0, now, ackOnlyFrames(0, 0), protocol.ECNNon)
	for n := range 30 {
		sendAccepted(t, h, 0, now, ackOnlyFrames(0, protocol.PacketNumber(n+1)), protocol.ECNNon)
	}
	require.Equal(t, append([]protocol.PacketNumber{pending}, pnRange(12, 30)...), historyPNs(h), "the pending record stays and pins the span")
	require.Equal(t, 31, h.appDataPackets.history.Len())

	require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now.Add(time.Millisecond)), "the pending record still resolves through its token")
	require.Equal(t, pnRange(12, 30), historyPNs(h), "once completed, it is the oldest completed record")
	require.Equal(t, protocol.MaxNonAckElicitingAcks, h.appDataPackets.history.Len())
	require.Zero(t, h.local.owner.pending)
	require.Error(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now.Add(2*time.Millisecond)))
}

func TestAckOnlyRetirementLateACK(t *testing.T) {
	newFixture := func(t *testing.T) (*sentPacketHandler, *ReceivedPacketHandler, monotime.Time) {
		t.Helper()
		h := newAckOnlyTestHandler(t, false, false)
		// Skip 0, as PopPacketNumber would.
		h.appDataPackets.history.SkippedPacket(0)
		h.appDataPackets.pns = newSequentialPacketNumberGenerator(1)
		received := NewReceivedPacketHandler(utils.DefaultLogger)
		h.ignorePacketsBelow = received.IgnorePacketsBelowOnPath
		now := monotime.Time(2 * time.Second)
		require.NoError(t, received.ReceivedPacketOnPath(0, 9, protocol.ECNNon, now, true))
		return h, received, now
	}

	t.Run("retained carrier", func(t *testing.T) {
		h, received, now := newFixture(t)
		first := sendUnregistered(h, 0, now, ackOnlyFrames(0, 9), protocol.ECNNon)
		for range protocol.MaxNonAckElicitingAcks {
			sendUnregistered(h, 0, now, nil, protocol.ECNNon)
		}
		require.NotContains(t, historyPNs(h), first, "the oldest completed record is retired")
		require.NotNil(t, received.GetAckFrame(protocol.Encryption1RTT, now, false))
		history := historyPNs(h)

		receipt, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(first)}, protocol.Encryption1RTT, now.Add(time.Millisecond))
		require.NoError(t, err, "an ACK covering a retired number is ignored")
		require.False(t, receipt.Valid)
		require.Equal(t, history, historyPNs(h))
		require.Nil(t, received.GetAckFrame(protocol.Encryption1RTT, now, false), "the retained carrier prunes its target")

		_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(0)}, protocol.Encryption1RTT, now.Add(2*time.Millisecond))
		var transportErr *qerr.TransportError
		require.ErrorAs(t, err, &transportErr, "an ACK covering a skipped number still fails")
		require.Equal(t, qerr.ProtocolViolation, transportErr.ErrorCode)
	})

	t.Run("evicted carrier", func(t *testing.T) {
		h, received, now := newFixture(t)
		first := sendUnregistered(h, 0, now, ackOnlyFrames(0, 9), protocol.ECNNon)
		// Retire as many later carriers as the store holds, so the first is evicted.
		for range protocol.MaxNonAckElicitingAcks + maxRetiredAckCarriers {
			sendUnregistered(h, 0, now, ackOnlyFrames(0, 9), protocol.ECNNon)
		}
		require.Len(t, h.ackCarriers.carriers, maxRetiredAckCarriers)
		_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(first)}, protocol.Encryption1RTT, now.Add(time.Millisecond))
		require.NoError(t, err)
		require.NotNil(t, received.GetAckFrame(protocol.Encryption1RTT, now, false), "the capped receive history keeps the target")
	})
}

func TestAckOnlyRetirementWaitsForAckEliciting(t *testing.T) {
	h := newAckOnlyTestHandler(t, false, false)
	now := monotime.Time(2 * time.Second)
	var tracker packetTracker
	ping := sendUnregistered(h, 0, now, []Frame{tracker.NewPingFrame(0)}, protocol.ECNNon)
	for n := range 30 {
		sendUnregistered(h, 0, now, ackOnlyFrames(0, protocol.PacketNumber(n)), protocol.ECNNon)
	}
	require.Equal(t, pnRange(0, 30), historyPNs(h), "nothing retires while an ack-eliciting record remains")

	_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(ping)}, protocol.Encryption1RTT, now.Add(10*time.Millisecond))
	require.NoError(t, err)
	require.Equal(t, []protocol.PacketNumber{ping}, tracker.Acked)
	require.Equal(t, pnRange(12, 30), historyPNs(h), "removing the last ack-eliciting record runs retirement")
}

// localECNLedger projects local ECN evidence onto its comparable facts.
type localECNLedger struct {
	possible, confirmed, reported, newly congestion.ECNCounts
	frontier, watermark                  protocol.PacketNumber
	unresolved, testingPending           int
	pending, failed, closed              bool
	constraints                          []localECNConstraint
	records                              []localECNRecord
}

func snapshotLocalECN(e *localECNEvidence) localECNLedger {
	l := localECNLedger{
		possible: e.possible, confirmed: e.confirmed, reported: e.reported, newly: e.newly,
		frontier: e.frontier, watermark: e.watermark,
		unresolved: e.unresolved, testingPending: e.testingPending,
		pending: e.pending, failed: e.failed, closed: e.closed,
		constraints: slices.Clone(e.constraints),
	}
	for i := range e.records.len() {
		r := *e.records.at(i)
		r.registration = nil // identity only; its packet reference is cleared at retirement
		l.records = append(l.records, r)
	}
	return l
}

func TestAckOnlyRetirementECN(t *testing.T) {
	t.Run("retired accepted or unregistered record", func(t *testing.T) {
		for _, mode := range []string{"unregistered", "local Reno", "local BBR"} {
			t.Run(mode, func(t *testing.T) {
				var h *sentPacketHandler
				switch mode {
				case "unregistered":
					h = newAckOnlyTestHandler(t, false, true)
				default:
					h, _ = newLocalECNTestHandler(t, mode == "local BBR")
				}
				now := monotime.Now()
				send := func(frames []Frame) protocol.PacketNumber {
					if h.local == nil {
						return sendUnregistered(h, 0, now, frames, h.ECNMode(true))
					}
					return sendAccepted(t, h, 0, now, frames, h.ECNMode(true))
				}
				send([]Frame{{Frame: &wire.PingFrame{}}})
				require.Equal(t, protocol.ECT0, h.ECNMode(true), "the next ACK-only record is ECN-marked")
				marked := send(nil)
				for range 25 {
					send(nil)
				}
				ecnMode := h.ECNMode(true)
				var tracker ecnTracker
				if e, ok := h.ecnTracker.(*ecnTracker); ok {
					tracker = *e
				}
				var bbrLedger bbrECNTracker
				if h.bbrECN != nil {
					bbrLedger = *h.bbrECN
					bbrLedger.ranges = slices.Clone(h.bbrECN.ranges)
				}
				var local localECNLedger
				if h.local != nil {
					local = snapshotLocalECN(h.local.ecn)
				}

				// A PTO extracts the only ack-eliciting record.
				require.True(t, h.QueueProbePacketAt(protocol.Encryption1RTT, now.Add(time.Second)))
				require.NotContains(t, historyPNs(h), marked, "the marked record is retired")
				require.Len(t, historyPNs(h), protocol.MaxNonAckElicitingAcks)

				require.Equal(t, ecnMode, h.ECNMode(true))
				if e, ok := h.ecnTracker.(*ecnTracker); ok {
					require.Equal(t, tracker, *e, "ECN validation is unchanged")
				}
				if h.bbrECN != nil {
					require.Equal(t, bbrLedger.ranges, h.bbrECN.ranges, "the BBR ledger is unchanged")
					require.Equal(t, bbrLedger.state, h.bbrECN.state)
					require.Equal(t, bbrLedger.testing, h.bbrECN.testing)
					require.Equal(t, bbrLedger.testingLost, h.bbrECN.testingLost)
					require.Equal(t, bbrLedger.evidenceLost, h.bbrECN.evidenceLost)
					require.Equal(t, bbrLedger.sent, h.bbrECN.sent)
					require.Equal(t, bbrLedger.accepted, h.bbrECN.accepted)
				}
				if h.local != nil {
					require.Equal(t, local, snapshotLocalECN(h.local.ecn), "the local ledger is unchanged")
				}
			})
		}
	})

	t.Run("uncertain record", func(t *testing.T) {
		for _, bbr := range []bool{false, true} {
			t.Run(fmt.Sprintf("BBR=%t", bbr), func(t *testing.T) {
				h, _ := newLocalECNTestHandler(t, bbr)
				now := monotime.Now()
				a, ta := localECNPacket(t, h, now)
				require.NoError(t, h.CompleteLocalPacket(ta, LocalPacketAccepted, now.Add(time.Millisecond)))
				ackBBRECNAt(t, h, now.Add(2*time.Millisecond), ackRanges(a), 1, 0)
				require.Equal(t, protocol.ECT0, h.ECNMode(true))

				uncertain, token := registerLocal(t, h, 0, now.Add(3*time.Millisecond), nil, h.ECNMode(true))
				require.NoError(t, h.CompleteLocalPacket(token, LocalPacketUncertain, now.Add(4*time.Millisecond)))
				for range 25 {
					sendAccepted(t, h, 0, now.Add(5*time.Millisecond), nil, h.ECNMode(true))
				}
				pns := historyPNs(h)
				require.Equal(t, uncertain, pns[0], "the uncertain record is not retired")
				require.Len(t, pns, protocol.MaxNonAckElicitingAcks+1)
				require.Equal(t, protocol.ECT0, h.ECNMode(true), "local ECN stays capable")
			})
		}
	})
}

func TestAckOnlyRetirementBBRDisposal(t *testing.T) {
	h := newAckOnlyTestHandler(t, false, false)
	EnableBBR(h, 1200, func() protocol.ByteCount { return 0 })
	now := monotime.Time(2 * time.Second)
	first := sendUnregistered(h, 0, now, ackOnlyFrames(0, 0), protocol.ECNNon)
	r := &h.congestionEvents.recovery
	i := r.find(congestionKey(protocol.Encryption1RTT, first))
	require.GreaterOrEqual(t, i, 0)
	require.Equal(t, outcomeUnresolved, r.index.outcomes[i].state)
	for n := range protocol.MaxNonAckElicitingAcks {
		sendUnregistered(h, 0, now, ackOnlyFrames(0, protocol.PacketNumber(n+1)), protocol.ECNNon)
	}
	require.NotContains(t, historyPNs(h), first)
	require.Equal(t, outcomeDisposed, r.index.outcomes[i].state, "retirement disposes of the outcome")
	require.False(t, r.unconfirmedLoss)
	require.Equal(t, slotSet{}, r.index.spans.lost, "the lost-run index is unchanged")
	require.False(t, h.congestionEvents.packets.has(congestionKey(protocol.Encryption1RTT, first)), "the delivery record is disposed of")
}

func TestAckOnlyRetirementPeriodicPING(t *testing.T) {
	// ACK-only 0–18, PING 19 and ACK-only 20–38, as the packer's periodic PING sends them.
	newSequence := func(t *testing.T) (*sentPacketHandler, *packetTracker, monotime.Time, protocol.ByteCount) {
		t.Helper()
		h := newAckOnlyTestHandler(t, false, false)
		now := monotime.Time(2 * time.Second)
		tracker := &packetTracker{}
		for n := range protocol.PacketNumber(39) {
			frames := ackOnlyFrames(0, n)
			if n == 19 {
				frames = append(frames, tracker.NewPingFrame(n))
			}
			sendUnregistered(h, 0, now, frames, protocol.ECNNon)
		}
		require.Equal(t, pnRange(0, 38), historyPNs(h))
		return h, tracker, now, h.congestion.GetCongestionWindow()
	}
	unchanged := func(t *testing.T, h *sentPacketHandler, window protocol.ByteCount) {
		t.Helper()
		require.False(t, h.congestion.InRecovery(), "no congestion event")
		require.GreaterOrEqual(t, h.congestion.GetCongestionWindow(), window)
		require.Empty(t, h.lostPackets.lostPackets, "no spurious-loss evidence")
		require.False(t, h.packetThresholdDisabled)
		require.Equal(t, uint8(defaultTimeThresholdEighths), h.timeThresholdEighths)
	}

	t.Run("selective ACK of the PING", func(t *testing.T) {
		h, tracker, now, window := newSequence(t)
		_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(19)}, protocol.Encryption1RTT, now.Add(100*time.Millisecond))
		require.NoError(t, err)
		require.Equal(t, []protocol.PacketNumber{19}, tracker.Acked)
		// The packet threshold declares 0–16 lost; 17 and 18 are retired.
		require.Equal(t, pnRange(20, 38), historyPNs(h))
		unchanged(t, h, window)

		receipt, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(17)}, protocol.Encryption1RTT, now.Add(110*time.Millisecond))
		require.NoError(t, err, "a later ACK of a retired number is ignored")
		require.False(t, receipt.Valid)
		require.Equal(t, pnRange(20, 38), historyPNs(h))
		unchanged(t, h, window)
	})

	t.Run("PTO extracts the PING", func(t *testing.T) {
		h, tracker, _, window := newSequence(t)
		alarm := h.GetLossDetectionTimeout()
		outcome, err := h.OnLossDetectionTimeout(alarm)
		require.NoError(t, err)
		require.True(t, outcome.PTO)
		require.True(t, h.QueueProbePacketAt(protocol.Encryption1RTT, alarm))
		require.Equal(t, []protocol.PacketNumber{19}, tracker.Lost, "the PING's frames return for retransmission")
		require.Equal(t, pnRange(20, 38), historyPNs(h))
		unchanged(t, h, window)

		receipt, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(5)}, protocol.Encryption1RTT, alarm.Add(time.Millisecond))
		require.NoError(t, err, "a later ACK of a retired number is ignored")
		require.False(t, receipt.Valid)
		require.Equal(t, pnRange(20, 38), historyPNs(h))
		unchanged(t, h, window)
	})
}

func TestAckOnlyRetirementPathRemoval(t *testing.T) {
	h := newAckOnlyTestHandler(t, false, false)
	require.NoError(t, h.AddPath(17, 1200))
	path := h.path(17)
	received := NewReceivedPacketHandler(utils.DefaultLogger)
	require.NoError(t, received.AddPath(17))
	path.ignorePacketsBelow = received.IgnorePacketsBelowOnPath
	now := monotime.Time(2 * time.Second)
	require.NoError(t, received.ReceivedPacketOnPath(17, 9, protocol.ECNNon, now, true))
	for range protocol.MaxNonAckElicitingAcks + 3 {
		sendUnregistered(h, 17, now, ackOnlyFrames(17, 9), protocol.ECNNon)
	}
	require.Len(t, path.ackCarriers.carriers, 3, "the retired records' carriers are retained")
	require.Len(t, historyPNs(path), protocol.MaxNonAckElicitingAcks)

	h.RetirePath(17)
	require.True(t, h.PathQuiescent(17))
	h.RemovePath(17)
	require.Nil(t, h.RecoveryOnPath(17))
	require.Zero(t, h.numTrackedPackets(), "the removed path's span no longer counts")
	_, err := h.ReceivedAck(&wire.AckFrame{HasPathID: true, PathID: 17, AckRanges: ackRanges(0)}, protocol.Encryption1RTT, now.Add(time.Millisecond))
	require.NoError(t, err, "a later ACK for the removed path is ignored")
	require.NotNil(t, received.GetPathAckFrame(17, now, false), "a removed carrier prunes nothing")
}
