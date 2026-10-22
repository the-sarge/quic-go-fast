package ackhandler

import (
	"slices"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/qlog"
	"github.com/stretchr/testify/require"
)

func TestRenoUndoCompleteLoss(t *testing.T) {
	for _, count := range []int{1, 2} {
		t.Run(map[int]string{1: "one", 2: "all"}[count], func(t *testing.T) {
			f := newSpuriousLossFixture(false)
			pns := make([]protocol.PacketNumber, count+3)
			for i := range pns {
				pns[i] = f.send(time.Duration(i)*time.Millisecond, protocol.Encryption1RTT)
			}
			require.EqualValues(t, 38400, f.h.congestion.GetCongestionWindow())
			f.ack(t, 100*time.Millisecond, pns[len(pns)-1])
			require.Equal(t, pns[:count], f.packets.Lost)
			require.EqualValues(t, 26880, f.h.congestion.GetCongestionWindow())
			require.False(t, f.h.congestion.InSlowStart())
			flight := f.h.bytesInFlight
			f.events.Clear()
			f.ack(t, 105*time.Millisecond, pns[:count]...)
			require.EqualValues(t, 38400, f.h.congestion.GetCongestionWindow())
			require.True(t, f.h.congestion.InSlowStart())
			require.False(t, f.h.congestion.InRecovery())
			require.Equal(t, flight, f.h.bytesInFlight)
			require.Equal(t, pns[:count], f.packets.Lost)
			require.Equal(t, []protocol.PacketNumber{pns[len(pns)-1]}, f.packets.Acked)
			metrics := f.events.Events(qlog.MetricsUpdated{})
			require.Len(t, metrics, 1)
			require.Equal(t, 38400, metrics[0].(qlog.MetricsUpdated).CongestionWindow)
		})
	}
}

func TestRenoUndoExpiredReceipt(t *testing.T) {
	for _, extra := range []time.Duration{0, time.Nanosecond} {
		t.Run(extra.String(), func(t *testing.T) {
			f := newSpuriousLossFixture(false)
			pns := f.loseFirstTwo(t)
			// No intervening ACK has cleaned the history; age still fences undo.
			lifetime := 3 * f.h.rttStats.PTO(false)
			f.ack(t, lifetime+extra, pns[0], pns[1])
			want := protocol.ByteCount(26880)
			if extra == 0 {
				want = 38400 // exact equality survives the strict cleanup cutoff
			}
			require.Equal(t, want, f.h.congestion.GetCongestionWindow())
		})
	}
}

func TestRenoUndoKeyDiscard(t *testing.T) {
	for _, level := range []protocol.EncryptionLevel{protocol.EncryptionInitial, protocol.EncryptionHandshake, protocol.Encryption0RTT} {
		t.Run(level.String(), func(t *testing.T) {
			f := newSpuriousLossFixture(false)
			pns := f.loseFirstTwo(t)
			f.h.DropPackets(level, f.start.Add(101*time.Millisecond))
			f.ack(t, 105*time.Millisecond, pns[0], pns[1])
			require.EqualValues(t, 26880, f.h.congestion.GetCongestionWindow())
		})
	}
}

func TestRenoUndoIncompleteEvidence(t *testing.T) {
	t.Run("genuine_and_duplicate", func(t *testing.T) {
		f := newSpuriousLossFixture(false)
		pns := f.loseFirstTwo(t)
		f.ack(t, 105*time.Millisecond, pns[0])
		f.ack(t, 106*time.Millisecond, pns[0], pns[4])
		require.EqualValues(t, 26880, f.h.congestion.GetCongestionWindow())
		f.ack(t, 107*time.Millisecond, pns[0], pns[1])
		require.EqualValues(t, 38400, f.h.congestion.GetCongestionWindow())
	})
	t.Run("77_losses_64_receipts", func(t *testing.T) {
		f := newSpuriousLossFixture(false)
		// Deterministic numbering retains the frozen 77-loss setup.
		f.h.appDataPackets.pns = newSequentialPacketNumberGenerator(0)
		pns := make([]protocol.PacketNumber, 80)
		for i := range pns {
			pns[i] = f.send(0, protocol.Encryption1RTT)
		}
		f.ack(t, 100*time.Millisecond, pns[79])
		require.Len(t, f.packets.Lost, 77)
		f.ack(t, 105*time.Millisecond, pns...)
		require.Len(t, f.events.Events(qlog.SpuriousLoss{}), 64)
		require.EqualValues(t, 26880, f.h.congestion.GetCongestionWindow())
	})
	t.Run("initial_or_handshake_is_not_application", func(t *testing.T) {
		for _, level := range []protocol.EncryptionLevel{protocol.EncryptionInitial, protocol.EncryptionHandshake} {
			h := newOrderingHandler(false, protocol.PerspectiveServer)
			at := monotime.Time(time.Second)
			for range 4 {
				orderingSend(t, h, at, level, true, false, false)
				orderingSend(t, h, at, protocol.Encryption1RTT, true, false, false)
			}
			orderingAck(t, h, at.Add(20*time.Millisecond), level, 1, 3, 0, 0)
			orderingAck(t, h, at.Add(20*time.Millisecond), protocol.Encryption1RTT, 1, 3, 0, 0)
			orderingAck(t, h, at.Add(21*time.Millisecond), protocol.Encryption1RTT, 0, 3, 0, 0)
			require.EqualValues(t, 26880, h.congestion.GetCongestionWindow(), "%s loss is still genuine", level)
		}
	})
}

func TestRenoUndoNewLossBeforeOldProof(t *testing.T) {
	f := newSpuriousLossFixture(false)
	pns := f.loseFirstTwo(t)
	newer := make([]protocol.PacketNumber, 4)
	for i := range newer {
		newer[i] = f.send(110*time.Millisecond, protocol.Encryption1RTT)
	}
	// ACK all surviving old packets, prove the old losses, and declare a
	// genuine new loss in the same admitted ACK.
	f.ack(t, 210*time.Millisecond, append(slices.Clone(pns), newer[1:]...)...)
	require.EqualValues(t, 18816, f.h.congestion.GetCongestionWindow())
	f.ack(t, 211*time.Millisecond, pns...)
	require.EqualValues(t, 18816, f.h.congestion.GetCongestionWindow())
	f.ack(t, 212*time.Millisecond, newer[0])
	require.EqualValues(t, 26880, f.h.congestion.GetCongestionWindow(), "only the latest cutback can be undone")
}

func TestRenoUndoValidatedCE(t *testing.T) {
	for _, when := range []string{"before", "with", "after"} {
		t.Run(when, func(t *testing.T) {
			h, at := orderingECNCapable(t)
			for range 5 {
				orderingSend(t, h, at, protocol.Encryption1RTT, true, false, false)
			}
			orderingAck(t, h, at.Add(20*time.Millisecond), protocol.Encryption1RTT, 14, 14, 11, 0)
			require.EqualValues(t, 26880, h.congestion.GetCongestionWindow())
			proof := &wire.AckFrame{AckRanges: ackRanges(10, 11, 14)}
			if when == "after" {
				_, err := h.ReceivedAck(proof, protocol.Encryption1RTT, at.Add(21*time.Millisecond))
				require.NoError(t, err)
				require.EqualValues(t, 38400, h.congestion.GetCongestionWindow())
			}
			orderingSend(t, h, at.Add(22*time.Millisecond), protocol.Encryption1RTT, true, false, false)
			// Acknowledge every live packet to isolate the CE cut from loss.
			ce := &wire.AckFrame{AckRanges: ackRanges(12, 13, 14, 15), ECT0: 13, ECNCE: 1}
			if when == "with" {
				ce.AckRanges = ackRanges(10, 11, 12, 13, 14, 15)
				ce.ECT0 = 15
			}
			_, err := h.ReceivedAck(ce, protocol.Encryption1RTT, at.Add(42*time.Millisecond))
			require.NoError(t, err)
			require.Equal(t, ecnStateCapable, h.ecnTracker.(*ecnTracker).state)
			want := protocol.ByteCount(18816)
			if when == "after" {
				want = 26880
			}
			require.Equal(t, want, h.congestion.GetCongestionWindow())
			_, err = h.ReceivedAck(proof, protocol.Encryption1RTT, at.Add(43*time.Millisecond))
			require.NoError(t, err)
			_, err = h.ReceivedAck(ce, protocol.Encryption1RTT, at.Add(44*time.Millisecond))
			require.NoError(t, err)
			require.Equal(t, want, h.congestion.GetCongestionWindow())
		})
	}
}

func TestRenoUndoSuppressedCERetiredAnchor(t *testing.T) {
	for _, retired := range []bool{false, true} {
		t.Run(map[bool]string{false: "present", true: "retired"}[retired], func(t *testing.T) {
			h, at := orderingECNCapable(t)
			for range 6 {
				orderingSend(t, h, at, protocol.Encryption1RTT, true, false, false)
			}
			lower := orderingSend(t, h, at, protocol.Encryption1RTT, false, false, false)
			anchor := orderingSend(t, h, at, protocol.Encryption1RTT, true, false, false)
			require.EqualValues(t, 16, lower)
			require.EqualValues(t, 17, anchor)
			orderingAck(t, h, at.Add(20*time.Millisecond), protocol.Encryption1RTT, 13, 13, 11, 0)
			require.EqualValues(t, 26880, h.congestion.GetCongestionWindow())
			if retired {
				// Retire eligible packets through the existing PTO entrypoint;
				// the lower ACK-only record remains live for legacy ECN admission.
				for range 5 {
					require.True(t, h.QueueProbePacketAt(protocol.Encryption1RTT, at.Add(21*time.Millisecond)))
				}
			}
			var foundAnchor bool
			for pn := range h.appDataPackets.history.Packets() {
				foundAnchor = foundAnchor || pn == anchor
			}
			require.Equal(t, !retired, foundAnchor)
			ack := &wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: 10, Largest: anchor}}, ECT0: 17, ECNCE: 1}
			_, err := h.ReceivedAck(ack, protocol.Encryption1RTT, at.Add(22*time.Millisecond))
			require.NoError(t, err)
			require.Equal(t, ecnStateCapable, h.ecnTracker.(*ecnTracker).state)
			require.Equal(t, int64(1), h.ecnTracker.(*ecnTracker).numAckedECNCE)
			require.EqualValues(t, 26880, h.congestion.GetCongestionWindow(), "suppressed CE still invalidates undo")
			_, err = h.ReceivedAck(ack, protocol.Encryption1RTT, at.Add(23*time.Millisecond))
			require.NoError(t, err)
			orderingAck(t, h, at.Add(24*time.Millisecond), protocol.Encryption1RTT, 10, 10, 0, 0)
			require.EqualValues(t, 26880, h.congestion.GetCongestionWindow())
		})
	}
}

func TestRenoUndoAfterRecoveryExit(t *testing.T) {
	f := newSpuriousLossFixture(false)
	pns := f.loseFirstTwo(t)
	f.ack(t, 105*time.Millisecond, pns[2], pns[3])
	fresh := f.send(110*time.Millisecond, protocol.Encryption1RTT)
	f.ack(t, 210*time.Millisecond, fresh)
	require.False(t, f.h.congestion.InRecovery())
	f.ack(t, 211*time.Millisecond, pns[0], pns[1])
	require.EqualValues(t, 38400, f.h.congestion.GetCongestionWindow())
	f.ack(t, 212*time.Millisecond, pns[4])
	require.False(t, f.h.congestion.InRecovery())
}

func TestRenoUndoDoesNotShrinkGrownWindow(t *testing.T) {
	f := newSpuriousLossFixture(false)
	pns := f.loseFirstTwo(t)
	f.ack(t, 105*time.Millisecond, pns[2], pns[3])
	at := 110 * time.Millisecond
	for f.h.congestion.GetCongestionWindow() <= 38400 {
		var batch []protocol.PacketNumber
		for f.h.congestion.CanSend(f.h.bytesInFlight) {
			batch = append(batch, f.send(at, protocol.Encryption1RTT))
		}
		f.ack(t, at+time.Millisecond, batch...)
		at += 2 * time.Millisecond
	}
	require.EqualValues(t, 38880, f.h.congestion.GetCongestionWindow())
	require.LessOrEqual(t, at, 3*f.h.rttStats.PTO(false), "old evidence remains unexpired")
	f.events.Clear()
	f.ack(t, at, pns[0], pns[1])
	require.Len(t, f.events.Events(qlog.SpuriousLoss{}), 2)
	require.EqualValues(t, 38880, f.h.congestion.GetCongestionWindow())
	require.False(t, f.h.congestion.InRecovery())
}

func TestRenoUndoLifecycle(t *testing.T) {
	for _, lifecycle := range []string{"Retry", "rejected_0RTT", "migration"} {
		t.Run(lifecycle, func(t *testing.T) {
			f := newSpuriousLossFixture(false)
			f.send(0, protocol.EncryptionInitial)
			pns := make([]protocol.PacketNumber, 4)
			for i := range pns {
				pns[i] = f.send(time.Duration(i)*time.Millisecond, protocol.Encryption0RTT)
			}
			f.ack(t, 100*time.Millisecond, pns[3])
			require.EqualValues(t, 26880, f.h.congestion.GetCongestionWindow())
			switch lifecycle {
			case "Retry":
				f.h.ResetForRetry(f.start.Add(101 * time.Millisecond))
			case "rejected_0RTT":
				f.h.DropPackets(protocol.Encryption0RTT, f.start.Add(101*time.Millisecond))
			case "migration":
				f.h.MigratedPath(f.start.Add(101*time.Millisecond), 1200)
			}
			fresh := f.send(110*time.Millisecond, protocol.Encryption1RTT)
			f.events.Clear()
			f.ack(t, 210*time.Millisecond, pns[0], fresh)
			require.Empty(t, f.events.Events(qlog.SpuriousLoss{}), "disposed receipts cannot reappear")
			want := protocol.ByteCount(26880)
			if lifecycle == "migration" {
				want = 38400
			}
			require.Equal(t, want, f.h.congestion.GetCongestionWindow())
		})
	}
}
