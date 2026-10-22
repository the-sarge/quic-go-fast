package ackhandler

import (
	"fmt"
	"testing"
	"time"
	"unsafe"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

func newOrderingHandler(ecn bool, perspective protocol.Perspective) *sentPacketHandler {
	h := NewSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, ecn, nil, perspective, nil, utils.DefaultLogger).(*sentPacketHandler)
	// Use deterministic application numbering; the skip case installs its own generator.
	h.appDataPackets.pns = newSequentialPacketNumberGenerator(0)
	return h
}

func orderingSend(t *testing.T, h *sentPacketHandler, at monotime.Time, level protocol.EncryptionLevel, ack, mtu, path bool) protocol.PacketNumber {
	t.Helper()
	pn := h.PopPacketNumber(level)
	var frames []Frame
	if ack {
		frames = []Frame{{Frame: &wire.PingFrame{}}}
	}
	mark := protocol.ECNNon
	if h.enableECN && level == protocol.Encryption1RTT && !path {
		mark = h.ECNMode(true)
	}
	h.SentPacket(at, pn, protocol.InvalidPacketNumber, nil, frames, level, mark, 1200, mtu, path)
	return pn
}

func orderingAck(t *testing.T, h *sentPacketHandler, at monotime.Time, level protocol.EncryptionLevel, first, last protocol.PacketNumber, ect, ce uint64) {
	t.Helper()
	_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: first, Largest: last}}, ECT0: ect, ECNCE: ce}, level, at)
	require.NoError(t, err)
}

func orderingHandshakeCut(t *testing.T, h *sentPacketHandler, at monotime.Time) {
	t.Helper()
	for range 4 {
		orderingSend(t, h, at, protocol.EncryptionHandshake, true, false, false)
	}
	orderingAck(t, h, at.Add(20*time.Millisecond), protocol.EncryptionHandshake, 1, 3, 0, 0)
}

func orderingECNCapable(t *testing.T) (*sentPacketHandler, monotime.Time) {
	t.Helper()
	h := newOrderingHandler(true, protocol.PerspectiveServer)
	at := monotime.Time(time.Second)
	for range 10 {
		orderingSend(t, h, at, protocol.Encryption1RTT, true, false, false)
	}
	orderingAck(t, h, at.Add(20*time.Millisecond), protocol.Encryption1RTT, 0, 9, 10, 0)
	require.Equal(t, ecnStateCapable, h.ecnTracker.(*ecnTracker).state)
	return h, at.Add(30 * time.Millisecond)
}

func TestRecoveryOrderingECNRetiredAnchor(t *testing.T) {
	for _, before := range []bool{false, true} {
		for _, retired := range []bool{false, true} {
			t.Run(fmt.Sprintf("before=%t/retired=%t", before, retired), func(t *testing.T) {
				h, at := orderingECNCapable(t)
				if !before {
					orderingHandshakeCut(t, h, at)
					at = at.Add(30 * time.Millisecond)
				}
				lower := orderingSend(t, h, at, protocol.Encryption1RTT, false, false, false)
				anchor := orderingSend(t, h, at, protocol.Encryption1RTT, true, false, false)
				require.Equal(t, protocol.PacketNumber(10), lower)
				require.Equal(t, protocol.PacketNumber(11), anchor)
				if before {
					orderingHandshakeCut(t, h, at)
					at = at.Add(30 * time.Millisecond)
				}
				if retired {
					require.True(t, h.QueueProbePacketAt(protocol.Encryption1RTT, at))
				}
				var foundLower, foundAnchor bool
				for pn := range h.appDataPackets.history.Packets() {
					foundLower = foundLower || pn == lower
					foundAnchor = foundAnchor || pn == anchor
				}
				require.True(t, foundLower)
				require.Equal(t, !retired, foundAnchor)
				require.Equal(t, protocol.PacketNumber(9), h.appDataPackets.largestAcked)
				window := h.congestion.GetCongestionWindow()
				orderingAck(t, h, at.Add(20*time.Millisecond), protocol.Encryption1RTT, lower, anchor, 11, 1)
				want := window
				if !before {
					want = window * 7 / 10
				}
				require.Equal(t, want, h.congestion.GetCongestionWindow())
				tracker := h.ecnTracker.(*ecnTracker)
				require.Equal(t, ecnStateCapable, tracker.state)
				require.Equal(t, int64(1), tracker.numAckedECNCE)
				require.Equal(t, int64(11), tracker.numAckedECT0)
				orderingAck(t, h, at.Add(21*time.Millisecond), protocol.Encryption1RTT, lower, anchor, 11, 1)
				orderingAck(t, h, at.Add(22*time.Millisecond), protocol.Encryption1RTT, 0, 8, 8, 0)
				require.Equal(t, want, h.congestion.GetCongestionWindow(), "duplicate/reordered ACK cannot cut again")
			})
		}
	}
}

func TestRecoveryOrderingMixedCEAndLoss(t *testing.T) {
	h, at := orderingECNCapable(t)
	for range 4 {
		orderingSend(t, h, at, protocol.Encryption1RTT, true, false, false)
	}
	before := h.congestion.GetCongestionWindow()
	orderingAck(t, h, at.Add(20*time.Millisecond), protocol.Encryption1RTT, 13, 13, 10, 1)
	require.Equal(t, before*7/10, h.congestion.GetCongestionWindow())
	require.Equal(t, ecnStateCapable, h.ecnTracker.(*ecnTracker).state)
}

func TestRecoveryOrderingHandshakeApplication(t *testing.T) {
	h := newOrderingHandler(false, protocol.PerspectiveServer)
	at := monotime.Time(time.Second)
	for range 7 {
		orderingSend(t, h, at, protocol.EncryptionHandshake, true, false, false)
	}
	orderingSend(t, h, at, protocol.Encryption1RTT, true, false, false)
	orderingAck(t, h, at.Add(20*time.Millisecond), protocol.EncryptionHandshake, 6, 6, 0, 0)
	require.Equal(t, protocol.ByteCount(26880), h.congestion.GetCongestionWindow())
	require.True(t, h.congestion.InRecovery())
	orderingSend(t, h, at.Add(30*time.Millisecond), protocol.Encryption1RTT, true, false, false)
	orderingAck(t, h, at.Add(50*time.Millisecond), protocol.Encryption1RTT, 1, 1, 0, 0)
	require.False(t, h.congestion.InRecovery())
	for range 4 {
		orderingSend(t, h, at.Add(60*time.Millisecond), protocol.Encryption1RTT, true, false, false)
	}
	orderingAck(t, h, at.Add(80*time.Millisecond), protocol.Encryption1RTT, 3, 5, 0, 0)
	require.Equal(t, protocol.ByteCount(18816), h.congestion.GetCongestionWindow())
}

func TestRecoveryOrderingLifecycle(t *testing.T) {
	for _, level := range []protocol.EncryptionLevel{protocol.EncryptionInitial, protocol.EncryptionHandshake} {
		t.Run("discard_"+level.String(), func(t *testing.T) {
			h := newOrderingHandler(false, protocol.PerspectiveServer)
			at := monotime.Time(time.Second)
			orderingSend(t, h, at, level, true, false, false)
			h.DropPackets(level, at.Add(time.Millisecond))
			require.Zero(t, h.bytesInFlight)
			require.Zero(t, h.connStats.PacketsLost.Load())
			require.Equal(t, protocol.ByteCount(38400), h.congestion.GetCongestionWindow())
			// New registrations may reuse pooled records but not disposed identities.
			pn := orderingSend(t, h, at.Add(2*time.Millisecond), protocol.Encryption1RTT, true, false, false)
			orderingAck(t, h, at.Add(20*time.Millisecond), protocol.Encryption1RTT, pn, pn, 0, 0)
			require.False(t, h.congestion.InRecovery())
			require.Zero(t, h.bytesInFlight)
		})
	}

	t.Run("Retry_and_zero_to_one_rtt", func(t *testing.T) {
		h := newOrderingHandler(false, protocol.PerspectiveClient)
		at := monotime.Time(time.Second)
		orderingSend(t, h, at, protocol.EncryptionInitial, true, false, false)
		orderingSend(t, h, at, protocol.Encryption0RTT, true, false, false)
		h.ResetForRetry(at.Add(20 * time.Millisecond))
		require.Equal(t, protocol.PacketNumber(1), orderingSend(t, h, at.Add(30*time.Millisecond), protocol.EncryptionInitial, true, false, false))
		require.Equal(t, protocol.PacketNumber(1), orderingSend(t, h, at.Add(30*time.Millisecond), protocol.Encryption0RTT, true, false, false))
		h.DropPackets(protocol.Encryption0RTT, at.Add(40*time.Millisecond))
		h.DropPackets(protocol.EncryptionInitial, at.Add(40*time.Millisecond))
		require.Equal(t, protocol.PacketNumber(2), orderingSend(t, h, at.Add(50*time.Millisecond), protocol.Encryption1RTT, true, false, false))
		orderingAck(t, h, at.Add(70*time.Millisecond), protocol.Encryption1RTT, 2, 2, 0, 0)
		require.False(t, h.congestion.InRecovery())
	})
	t.Run("migration_keeps_wire_numbers_resets_controller", func(t *testing.T) {
		h := newOrderingHandler(false, protocol.PerspectiveClient)
		at := monotime.Time(time.Second)
		orderingSend(t, h, at, protocol.Encryption1RTT, true, false, false)
		h.MigratedPath(at.Add(20*time.Millisecond), 1200)
		orderingAck(t, h, at.Add(21*time.Millisecond), protocol.Encryption1RTT, 0, 0, 0, 0)
		require.False(t, h.congestion.InRecovery(), "disposed old-path receipts cannot reach the fresh sender")
		require.Equal(t, protocol.ByteCount(38400), h.congestion.GetCongestionWindow())
		require.Equal(t, protocol.PacketNumber(1), orderingSend(t, h, at.Add(30*time.Millisecond), protocol.Encryption1RTT, true, false, false))
		orderingAck(t, h, at.Add(50*time.Millisecond), protocol.Encryption1RTT, 1, 1, 0, 0)
		require.False(t, h.congestion.InRecovery())
	})
	t.Run("intentional_skip_remains_validated", func(t *testing.T) {
		h := newOrderingHandler(false, protocol.PerspectiveServer)
		h.appDataPackets.pns = newSkippingPacketNumberGenerator(0, 256, 1024)
		h.appDataPackets.pns.(*skippingPacketNumberGenerator).nextToSkip = 2
		at := monotime.Time(time.Second)
		for _, want := range []protocol.PacketNumber{0, 1, 3} {
			require.Equal(t, want, orderingSend(t, h, at, protocol.Encryption1RTT, true, false, false))
		}
		_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: 0, Largest: 3}}}, protocol.Encryption1RTT, at.Add(20*time.Millisecond))
		require.ErrorContains(t, err, "skipped packet number")
		orderingAck(t, h, at.Add(20*time.Millisecond), protocol.Encryption1RTT, 3, 3, 0, 0)
		require.Zero(t, h.connStats.PacketsLost.Load(), "skipped number cannot count toward threshold")
	})
	t.Run("MTU_loss_excluded", func(t *testing.T) {
		h := newOrderingHandler(false, protocol.PerspectiveServer)
		at := monotime.Time(time.Second)
		window := h.congestion.GetCongestionWindow()
		orderingSend(t, h, at, protocol.Encryption1RTT, true, true, false)
		for range 3 {
			orderingSend(t, h, at, protocol.Encryption1RTT, true, false, false)
		}
		orderingAck(t, h, at.Add(20*time.Millisecond), protocol.Encryption1RTT, 3, 3, 0, 0)
		require.Equal(t, window, h.congestion.GetCongestionWindow())
		require.Zero(t, h.connStats.PacketsLost.Load())
		require.False(t, h.congestion.InRecovery())
	})
	t.Run("MTU_ACK_crosses", func(t *testing.T) {
		h := newOrderingHandler(false, protocol.PerspectiveServer)
		at := monotime.Time(time.Second)
		orderingHandshakeCut(t, h, at)
		window := h.congestion.GetCongestionWindow()
		pn := orderingSend(t, h, at.Add(30*time.Millisecond), protocol.Encryption1RTT, true, true, false)
		orderingAck(t, h, at.Add(50*time.Millisecond), protocol.Encryption1RTT, pn, pn, 0, 0)
		require.False(t, h.congestion.InRecovery())
		require.Equal(t, window, h.congestion.GetCongestionWindow())
	})
	t.Run("path_probe_and_ACK_only_do_not_exit_recovery", func(t *testing.T) {
		h := newOrderingHandler(false, protocol.PerspectiveServer)
		at := monotime.Time(time.Second)
		orderingHandshakeCut(t, h, at)
		flight := h.bytesInFlight
		lower := orderingSend(t, h, at.Add(30*time.Millisecond), protocol.Encryption1RTT, false, false, false)
		upper := orderingSend(t, h, at.Add(30*time.Millisecond), protocol.Encryption1RTT, true, false, true)
		require.Equal(t, flight, h.bytesInFlight, "excluded registrations cannot charge flight")
		orderingAck(t, h, at.Add(50*time.Millisecond), protocol.Encryption1RTT, lower, upper, 0, 0)
		require.True(t, h.congestion.InRecovery())
	})
}

func TestRecoveryOrderingLayout(t *testing.T) {
	c := congestion.NewCubicSender(congestion.DefaultClock{}, utils.NewRTTStats(), &utils.ConnectionStats{}, 1200, true, nil)
	t.Logf("packet=%d cubicSender=%d HybridSlowStart=%d bytes", unsafe.Sizeof(packet{}), unsafe.Sizeof(*c), unsafe.Sizeof(congestion.HybridSlowStart{}))
}
