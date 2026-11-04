//go:build darwin || linux || freebsd

package quic

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

// replacementPeerCID is the peer's connection ID on id. Each is distinct, because
// the CID owner refuses one reused across paths.
func replacementPeerCID(id protocol.PathID) protocol.ConnectionID {
	cid := make([]byte, admissionPeerCID.Len())
	cid[0], cid[1] = 0xbe, byte(id)
	return protocol.ParseConnectionID(cid)
}

// peerGrant is the peer's half of a replacement: it retires its view of old, then
// grants and supplies a CID for id, so the local window can issue and open.
func (f *reliableFixture) peerGrant(t *testing.T, old, id protocol.PathID) {
	t.Helper()
	seal, err := f.peer.Get1RTTSealer()
	require.NoError(t, err)
	require.NoError(t, handshake.RetireShortHeaderPath(seal, old))
	if old > 1 { // an earlier replacement's peer view is long gone
		require.NoError(t, handshake.RemoveShortHeaderPath(seal, old-1))
	}
	require.NoError(t, handshake.AdmitShortHeaderPath(seal, id, utils.NewRTTStats()))
	require.NoError(t, creditIngress(t, f.tc, monotime.Now(),
		&wire.MaxPathIDFrame{MaximumPathID: id},
		&wire.PathNewConnectionIDFrame{PathID: id, ConnectionID: replacementPeerCID(id), StatelessResetToken: protocol.StatelessResetToken{0xaa, byte(id)}},
	))
}

// openValidated opens the issued ID on a tuple already validated, which activates
// at once, and makes it the private primary.
func (f *reliableFixture) openValidated(t *testing.T, id protocol.PathID) {
	t.Helper()
	f.c.pathCredit.recordValidated(admissionTupleA)
	errs := f.openAsync(context.Background(), nil, admissionTupleA)
	f.pass(t)
	require.NoError(t, <-errs)
	require.True(t, f.c.pathCredit.members[id].live)
	f.c.pathCredit.selectPrimaryPrivate(id)
}

// finishRetirement sends the local retirement of id through the fence, answers it
// and lets its retention elapse, so every charge it holds is reclaimed.
func (f *reliableFixture) finishRetirement(t *testing.T, id protocol.PathID) {
	t.Helper()
	f.c.pathCredit.decline(id, 0, ErrPathClosed)
	f.pass(t)
	f.pass(t)
	require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathAbandonFrame{PathID: id}))
	time.Sleep(time.Until(f.c.pathCredit.terminals[id].expiry.ToTime()) + time.Millisecond)
	f.pass(t)
	f.pass(t)
}

func replacementMaxPathIDs(writes []reliableWrite) []protocol.PathID {
	var ids []protocol.PathID
	for _, write := range writes {
		for _, frame := range write.frames {
			if grant, ok := frame.(*wire.MaxPathIDFrame); ok {
				ids = append(ids, grant.MaximumPathID)
			}
		}
	}
	return ids
}

// TestPathReplacementAfterCutoff follows the plan's replacement table: no grant
// while the old path's submission is claimed, one at the signal, and the new ID's
// window only once the peer's maximum allows it.
func TestPathReplacementAfterCutoff(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveClient)
		f.c.pathCredit.selectPrimaryPrivate(1)
		f.socket.hold = nativeProbeHoldAll
		f.write(t, 4000)
		f.pass(t)
		require.NotEmpty(t, f.socket.writes, "the first submission is claimed")
		f.c.pathCredit.decline(1, 0, ErrPathClosed)
		terminal := f.c.pathCredit.terminals[1]
		f.pass(t)
		require.False(t, terminal.signaled, "the claimed submission holds the cutoff")
		require.Equal(t, protocol.PathID(1), f.c.pathCredit.localMax, "no grant before the fence")

		f.socket.hold = nil
		f.socket.release()
		synctest.Wait() // the endpoint's held writes finish before the test touches the log
		f.socket.writes = nil
		f.pass(t)
		f.pass(t)
		require.True(t, terminal.signaled)
		require.Equal(t, protocol.PathID(2), f.c.pathCredit.localMax, "one step at the transition")
		writes := f.parseWrites(t, 0)
		require.Equal(t, []protocol.PathID{2}, replacementMaxPathIDs(writes))
		_, charged := f.c.pathCredit.paths[1]
		require.False(t, charged, "the fenced path left the live slot")
		_, issued := f.c.pathCredit.paths[2]
		require.False(t, issued, "ID 2's CIDs wait for the peer's maximum")

		f.peerGrant(t, 1, 2)
		f.pass(t)
		require.NotNil(t, f.c.connIDGenerator.pathCIDs[2], "both maximums now allow the window")
		f.socket.writes = nil
		f.openValidated(t, 2)
		f.write(t, 1000)
		f.pass(t)
		require.NotZero(t, reliableBytes(f.parseWrites(t, 2)), "the replacement carries traffic")
	})
}

// TestPathReplacementRemoteAbandonDuringReplenishment abandons the granted unused
// ID. It is never reused, and its terminal metadata takes a unit like any other.
func TestPathReplacementRemoteAbandonDuringReplenishment(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveClient)
		f.finishRetirement(t, 1)
		require.Zero(t, f.c.pathCredit.historyUnits)
		f.peerGrant(t, 1, 2)
		f.pass(t)
		require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathAbandonFrame{PathID: 2}))
		f.pass(t)
		f.pass(t)
		require.Equal(t, 1, f.c.pathCredit.historyUnits, "unused terminal metadata is charged")
		require.Equal(t, protocol.PathID(3), f.c.pathCredit.localMax, "the next ID, never the old one")
		require.False(t, f.c.pathCredit.consume(2))
	})
}

// TestPathReplacementOldAckAfterReplacement delivers late ACKs for the retired path
// while its evidence is retained and after, with the replacement live.
func TestPathReplacementOldAckAfterReplacement(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveClient)
		f.c.pathCredit.selectPrimaryPrivate(1)
		f.write(t, 1000)
		f.pass(t)
		old := f.parseWrites(t, 1)
		require.NotEmpty(t, old)
		f.socket.writes = nil
		f.c.pathCredit.decline(1, 0, ErrPathClosed)
		f.pass(t)
		f.pass(t)
		first := reliableBytes(f.parseWrites(t, 0))
		require.Equal(t, 1000, first, "the accepted bytes returned to their logical owner")

		f.peerGrant(t, 1, 2)
		f.pass(t)
		f.socket.writes = nil
		f.openValidated(t, 2)
		lateAck := func() *wire.AckFrame {
			return &wire.AckFrame{HasPathID: true, PathID: 1, AckRanges: []wire.AckRange{{Smallest: old[0].pn, Largest: old[len(old)-1].pn}}}
		}
		require.NoError(t, creditIngress(t, f.tc, monotime.Now(), lateAck()))
		f.pass(t)
		require.Zero(t, reliableBytes(f.parseWrites(t, 2)), "the late ACK retires nothing twice and retransmits nothing")

		// A retained context can leave three receive spaces owing an ACK at once, more
		// than one carrier records.
		for _, id := range []protocol.PathID{0, 1, 2} {
			require.NoError(t, f.c.receivedPacketHandler.ReceivedPacketOnPath(id, 100, protocol.ECNNon, monotime.Now(), true))
			require.NoError(t, f.c.receivedPacketHandler.ReceivedPacketOnPath(id, 101, protocol.ECNNon, monotime.Now(), true))
		}
		f.write(t, 500)
		f.pass(t)

		require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathAbandonFrame{PathID: 1}))
		time.Sleep(time.Until(f.c.pathCredit.terminals[1].expiry.ToTime()) + time.Millisecond)
		f.pass(t)
		f.pass(t)
		require.Nil(t, f.recovery(1), "reclamation removed the retired context")
		require.NoError(t, creditIngress(t, f.tc, monotime.Now(), lateAck()), "a later ACK for a reclaimed ID is ignored")
		above := &wire.AckFrame{HasPathID: true, PathID: 3, AckRanges: []wire.AckRange{{Smallest: 0, Largest: 0}}}
		require.Error(t, creditIngress(t, f.tc, monotime.Now(), above), "an ID above the admitted range keeps its violation")
	})
}

// TestPathReplacementRetainedCapBlocksOpening fills the pool: the grant is withheld,
// the request waits within its context, and one reclaimed unit restores admission.
func TestPathReplacementRetainedCapBlocksOpening(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveClient)
		f.c.pathCredit.historyUnits = maxPathHistoryUnits // three other retained records
		f.c.pathCredit.decline(1, 0, ErrPathClosed)
		f.pass(t)
		f.pass(t)
		require.Equal(t, protocol.PathID(1), f.c.pathCredit.localMax, "a full pool withholds the grant")
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		errs := f.openAsync(ctx, nil, admissionTupleB)
		f.pass(t)
		time.Sleep(2 * time.Second)
		f.pass(t)
		require.ErrorIs(t, <-errs, context.DeadlineExceeded)
		require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathAbandonFrame{PathID: 1}))
		time.Sleep(time.Until(f.c.pathCredit.terminals[1].expiry.ToTime()) + time.Millisecond)
		require.NoError(t, f.c.emission.consumeLocal())
		require.NoError(t, f.c.pathCredit.expire(monotime.Now()))
		require.Equal(t, maxPathHistoryUnits-1, f.c.pathCredit.historyUnits)
		require.Equal(t, protocol.PathID(2), f.c.pathCredit.localMax, "the pass that frees a unit restores the grant")
	})
}

// TestPathReplacementMissingReciprocal keeps the escalation while replacement goes
// ahead: the slot leaves at the signal, the obligation does not.
func TestPathReplacementMissingReciprocal(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveClient)
		f.c.pathCredit.decline(1, 0, ErrPathClosed)
		f.pass(t)
		f.pass(t)
		terminal := f.c.pathCredit.terminals[1]
		require.NotZero(t, terminal.receipt.acceptedAt())
		require.Equal(t, protocol.PathID(2), f.c.pathCredit.localMax, "the grant does not wait for the reciprocal frame")
		require.NoError(t, f.c.pathCredit.expire(monotime.Now()))
		require.NotZero(t, terminal.unanswered)
		time.Sleep(time.Until(terminal.unanswered.ToTime()) + time.Millisecond)
		err := f.c.pathCredit.expire(monotime.Now())
		var transportErr *qerr.TransportError
		require.ErrorAs(t, err, &transportErr)
		require.Equal(t, qerr.InternalError, transportErr.ErrorCode)
	})
}

// TestPathZeroRetirementSurvivor retires path zero under claimed traffic. The cutoff,
// reciprocal signal and rescheduled reliable bytes all follow the survivor.
func TestPathZeroRetirementSurvivor(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveClient)
		f.socket.hold = nativeProbeHoldAll
		f.write(t, 4000)
		f.pass(t)
		claimed := len(f.socket.writes)
		require.NotZero(t, claimed)
		require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathAbandonFrame{PathID: 0}))
		terminal := f.c.pathCredit.terminals[0]
		require.NotNil(t, terminal, "a survivor makes this a retirement, not a close")
		f.pass(t)
		require.False(t, terminal.signaled, "ordinary path-zero submissions hold the fence")
		require.Len(t, f.socket.writes, claimed, "queued path-zero requests are revoked")

		f.socket.hold = nil
		f.socket.release()
		f.pass(t)
		f.pass(t)
		f.pass(t)
		require.True(t, terminal.signaled)
		writes := f.parseWrites(t, 1)
		abandon := false
		for _, write := range writes {
			for _, frame := range write.frames {
				if signal, ok := frame.(*wire.PathAbandonFrame); ok && signal.PathID == 0 {
					abandon = true
				}
			}
		}
		require.True(t, abandon, "the reciprocal signal leaves on the survivor")
		require.Equal(t, 4000, reliableBytes(writes), "revoked and accepted bytes alike returned to their owner and left on the survivor")
		require.Equal(t, protocol.PathID(1), f.c.pathCredit.primary)
	})
}

// TestPathZeroRetirementObservesSurvivorRTT reads through a controller created before
// retirement, which holds the stable observation rather than an estimator.
func TestPathZeroRetirementObservesSurvivorRTT(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveClient)
		reader := f.c.flowControlRTT()
		survivor := f.c.pathCredit.rtt[1]
		survivor.UpdateRTT(77*time.Millisecond, 0)
		require.NotEqual(t, survivor.SmoothedRTT(), reader.SmoothedRTT(), "zero's own observation while live")
		require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathAbandonFrame{PathID: 0}))
		require.Equal(t, survivor.SmoothedRTT(), reader.SmoothedRTT(), "the existing reader follows the surviving primary")
		survivor.UpdateRTT(99*time.Millisecond, 0)
		require.Equal(t, survivor.SmoothedRTT(), reader.SmoothedRTT())
	})
}

// TestPathZeroRetirementLastPath keeps prompt closure on whichever path is last.
func TestPathZeroRetirementLastPath(t *testing.T) {
	t.Run("zero with only a validating path", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveServer)
			f.supplyPeerCIDs(t)
			require.True(t, f.receive(t, 1, admissionTupleA, 1200, &wire.PingFrame{}))
			err := creditIngress(t, f.tc, monotime.Now(), &wire.PathAbandonFrame{PathID: 0})
			var transportErr *qerr.TransportError
			require.ErrorAs(t, err, &transportErr)
			require.Equal(t, qerr.InternalError, transportErr.ErrorCode)
		})
	})
	t.Run("the survivor after zero retired", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newReliableFixture(t, protocol.PerspectiveClient)
			require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathAbandonFrame{PathID: 0}))
			f.pass(t)
			err := creditIngress(t, f.tc, monotime.Now(), &wire.PathAbandonFrame{PathID: 1})
			var transportErr *qerr.TransportError
			require.ErrorAs(t, err, &transportErr)
			require.Equal(t, qerr.InternalError, transportErr.ErrorCode)
		})
	})
}

// TestPathZeroNoEligibleSurvivor removes the survivor's last usable CID. Emission
// must report blocked: a nil route would seal on the retired path.
func TestPathZeroNoEligibleSurvivor(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveClient)
		require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathAbandonFrame{PathID: 0}))
		f.pass(t)
		f.pass(t)
		f.c.peerParams.MaxDatagramFrameSize = wire.MaxDatagramSize
		f.setPathMTU(1, 1400)
		f.c.emission.selectRoute(monotime.Now())
		limit := estimateMaxPayloadSize(1400)
		require.NoError(t, f.c.SendDatagram(make([]byte, limit)), "zero's smaller estimate retired with it")
		var tooLarge *DatagramTooLargeError
		require.ErrorAs(t, f.c.SendDatagram(make([]byte, limit+1)), &tooLarge)

		f.c.connIDManager.pathCIDs[1].records = nil
		f.socket.writes = nil
		f.write(t, 1000)
		result := f.c.triggerSending(monotime.Now())
		require.NoError(t, result.err)
		require.Equal(t, blockModeHardBlocked, result.blocked)
		synctest.Wait()
		require.Empty(t, f.socket.writes)
		require.Nil(t, f.c.datagramQueue.Peek(), "nothing can carry the queued DATAGRAM")
		require.EqualValues(t, 1, f.c.datagramQueue.sendDrops.Load())
	})
}

// TestPathZeroCloseAfterRetirement closes with zero retired and with it reclaimed.
// The emission close owner still refuses for zero, without reading released CID state;
// handleCloseError closes via survivors instead.
func TestPathZeroCloseAfterRetirement(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveClient)
		require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathAbandonFrame{PathID: 0}))
		f.pass(t)
		f.pass(t)
		_, err := f.c.emission.close(&qerr.TransportError{ErrorCode: qerr.NoError})
		require.Error(t, err, "retired, before reclamation")
		time.Sleep(time.Until(f.c.pathCredit.terminals[0].expiry.ToTime()) + time.Millisecond)
		f.pass(t)
		f.pass(t)
		require.Nil(t, f.c.connIDManager.pathCIDs[0])
		_, err = f.c.emission.close(&qerr.TransportError{ErrorCode: qerr.NoError})
		require.Error(t, err, "reclaimed")
	})
}

// TestPathReplacementReclamationAwaitsResult publishes a result after the connection
// consumed its results and before retention's pass reads the fence. The fence is
// resolved, but the Accepted packet's frames still need the terminal's final drain,
// and an early ACK's token still needs its context.
func TestPathReplacementReclamationAwaitsResult(t *testing.T) {
	for name, earlyACK := range map[string]bool{"accepted": false, "early ACK": true} {
		t.Run(name, func(t *testing.T) {
			nativeOrdinaryTest(t, func(t *testing.T) {
				f := newReliableFixture(t, protocol.PerspectiveClient)
				f.c.pathCredit.selectPrimaryPrivate(1)
				f.socket.writes = nil
				f.write(t, 1000)
				f.admissionFixture.pass(t) // the result stays unconsumed
				if earlyACK {
					pn := f.parseWrites(t, 1)[0].pn
					require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.AckFrame{HasPathID: true, PathID: 1, AckRanges: []wire.AckRange{{Smallest: pn, Largest: pn}}}))
				}
				require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathAbandonFrame{PathID: 1}))
				terminal := f.c.pathCredit.terminals[1]
				require.NoError(t, f.c.pathCredit.expire(monotime.Now()))
				require.True(t, terminal.signaled, "the endpoint reports the fence resolved")
				time.Sleep(time.Until(terminal.expiry.ToTime()) + time.Millisecond)
				require.NoError(t, f.c.pathCredit.expire(monotime.Now()))
				require.NotNil(t, f.c.pathCredit.members[1], "an unconsumed result holds reclamation")
				require.NotNil(t, f.recovery(1))
				next := f.c.pathCredit.nextExpiry()
				require.True(t, next == 0 || next.After(monotime.Now()), "a pending result arms no elapsed timer")

				f.socket.writes = nil
				require.NoError(t, f.c.emission.consumeLocal(), "the token still finds its context")
				require.NoError(t, f.c.pathCredit.expire(monotime.Now()))
				require.Nil(t, f.c.pathCredit.members[1], "the final drain lets it reclaim")
				f.pass(t)
				want := 1000
				if earlyACK {
					want = 0
				}
				require.Equal(t, want, reliableBytes(f.parseWrites(t, 0)), "drained frames aren't stranded, and ACKed ones aren't resent")
			})
		})
	}
}

// TestPathReplacementTerminalWithoutUnit lets zero's retention elapse with the pool
// full. Retention never waits on capacity, and no unheld unit is released.
func TestPathReplacementTerminalWithoutUnit(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveClient)
		f.c.pathCredit.historyUnits = maxPathHistoryUnits
		require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathAbandonFrame{PathID: 0}))
		f.pass(t)
		f.pass(t)
		terminal := f.c.pathCredit.terminals[0]
		require.True(t, terminal.signaled)
		_, charged := f.c.pathCredit.paths[0]
		require.True(t, charged, "without a unit the slot stays charged")
		time.Sleep(time.Until(terminal.expiry.ToTime()) + time.Millisecond)
		f.pass(t)
		f.pass(t)
		_, charged = f.c.pathCredit.paths[0]
		require.False(t, charged, "reclamation freed the slot")
		require.Equal(t, maxPathHistoryUnits, f.c.pathCredit.historyUnits, "and released no unheld unit")
	})
}

// TestPathReplacementRecordsEveryReceipt packs three abandonments together. A slot
// overflow would lose a first acceptance, and the unanswered timer with it.
func TestPathReplacementRecordsEveryReceipt(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveClient)
		var frames []ackhandler.Frame
		for _, id := range []protocol.PathID{1, 2, 3} {
			f.c.pathCredit.terminals[id] = f.c.pathCredit.newTerminal()
			frames = append(frames, ackhandler.Frame{Frame: &wire.PathAbandonFrame{PathID: id}})
		}
		f.c.emission.captureTerminalReceipts(frames)
		f.c.emission.captureTerminalReceipts(frames)
		receipts := 0
		for _, receipt := range f.c.emission.terminalReceipts {
			if receipt != nil {
				receipts++
			}
		}
		require.Equal(t, 3, receipts, "each one is recorded, without duplicates")
	})
}

// TestPathReplacementChurnStaysBounded repeats replacement. IDs keep rising, frames
// for reclaimed IDs are ignored, and terminal state does not grow.
func TestPathReplacementChurnStaysBounded(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveClient)
		for id := protocol.PathID(1); id <= 6; id++ {
			f.finishRetirement(t, id)
			require.Empty(t, f.c.pathCredit.terminals, "a reclaimed ID keeps no record")
			require.Zero(t, f.c.pathCredit.historyUnits)
			require.Equal(t, id+1, f.c.pathCredit.localMax)
			now := monotime.Now()
			require.NoError(t, creditIngress(t, f.tc, now,
				&wire.PathAbandonFrame{PathID: id},
				&wire.PathRetireConnectionIDFrame{PathID: id, SequenceNumber: 0},
				&wire.PathNewConnectionIDFrame{PathID: id, SequenceNumber: 9, ConnectionID: replacementPeerCID(0xf0 + id), StatelessResetToken: protocol.StatelessResetToken{byte(id)}},
			), "stale frames for a reclaimed ID are ignored")
			require.Nil(t, f.c.connIDManager.pathCIDs[id], "and recreate nothing")
			f.peerGrant(t, id, id+1)
			f.pass(t)
			f.openValidated(t, id+1)
		}
		require.Len(t, f.c.pathCredit.members, 2)
	})
}

// TestPathReplacementSharedCharges draws from the pool as R13's old generations will.
// Generations and retired paths compete, and a loser refuses rather than evicts. A
// transport keeps its attachment charge past its last member, until R13 can detach it.
func TestPathReplacementSharedCharges(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveClient)
		p := f.c.pathCredit
		require.Equal(t, 1, p.historyUnits, "the live path holds its own unit")
		require.True(t, p.reserveHistory())
		require.True(t, p.reserveHistory())
		p.decline(1, 0, ErrPathClosed)
		f.pass(t)
		f.pass(t)
		require.Equal(t, protocol.PathID(2), p.localMax, "one unit remains for the replacement")
		f.peerGrant(t, 1, 2)
		f.pass(t)
		require.True(t, p.reserveHistory(), "an old generation takes the last unit")
		require.False(t, p.reserveHistory())
		errs := f.openAsync(context.Background(), nil, admissionTupleB)
		f.pass(t)
		require.ErrorIs(t, <-errs, ErrMultipathResourceLimitV1, "the loser refuses and evicts nothing")
		require.Equal(t, maxPathHistoryUnits, p.historyUnits)
		_, charged := p.paths[2]
		require.True(t, charged, "the ID stays unused and issued")

		departed := &Transport{}
		p.attachments[departed] = 0
		for len(p.attachments) < maxPathAttachments {
			p.attachments[&Transport{}] = 0
		}
		p.releaseHistory()
		_, err := p.admitMember(2, &Transport{}, monotime.Now())
		require.ErrorIs(t, err, ErrMultipathResourceLimitV1, "zero references still hold the attachment charge")
		_, attached := p.attachments[departed]
		require.True(t, attached)
	})
}
