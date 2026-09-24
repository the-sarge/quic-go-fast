package ackhandler

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

func TestBBRPTOConfirmedSmallFlight(t *testing.T) {
	h, r, now := measuredRecoveryHandler(t)
	sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1200)
	var newest []protocol.PacketNumber
	for range 4 {
		deadline := h.GetLossDetectionTimeout()
		require.NoError(t, h.OnLossDetectionTimeout(deadline))
		for h.initialPackets.history.HasOutstandingPackets() {
			require.True(t, h.QueueProbePacketAt(protocol.EncryptionInitial, deadline))
		}
		newest = newest[:0]
		for range 2 {
			newest = append(newest, sendCongestionTestPacket(h, deadline, protocol.EncryptionInitial, 1200))
		}
	}
	for _, e := range r.feedback {
		require.Zero(t, e.PersistentCongestion.EndOrdinal)
		require.Empty(t, e.Lost)
	}
	acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, h.initialPackets.lastAckElicitingPacketTime.Add(10*time.Millisecond), newest...)
	e := r.feedback[len(r.feedback)-1]
	require.Equal(t, uint64(2), e.PersistentCongestion.StartOrdinal)
	require.Equal(t, uint64(8), e.PersistentCongestion.EndOrdinal)
	require.Equal(t, uint64(5), h.DeliveryStats().Expired)
	require.Equal(t, 2, h.DeliveryStats().Retained)
	require.Empty(t, e.Lost)
	require.Zero(t, e.Delivery.Lost)
	require.Zero(t, e.RecoveryEpisode.ID)
}

// ptoPair registers measured endpoints one second apart and transfers their
// flight/frame ownership through the actual PTO extraction entrypoint.
func ptoPair(t *testing.T, h *sentPacketHandler, now monotime.Time, level protocol.EncryptionLevel) (protocol.PacketNumber, protocol.PacketNumber) {
	t.Helper()
	first := sendCongestionTestPacket(h, now, level, 1200)
	last := sendCongestionTestPacket(h, now.Add(time.Second), level, 1200)
	require.True(t, h.QueueProbePacketAt(level, now.Add(time.Second)))
	require.True(t, h.QueueProbePacketAt(level, now.Add(time.Second)))
	return first, last
}

func TestBBRPTOConfirmationAdmission(t *testing.T) {
	for _, kind := range []string{"ordinary", "ACK-only", "MTU", "alternate path", "wrong space", "unsent", "skipped"} {
		t.Run(kind, func(t *testing.T) {
			h, r, now := measuredRecoveryHandler(t)
			ptoPair(t, h, now, protocol.Encryption1RTT)
			level := protocol.Encryption1RTT
			if kind == "wrong space" {
				level = protocol.EncryptionHandshake
			}
			pn := h.PopPacketNumber(level)
			frames := []Frame{{Frame: &wire.PingFrame{}}}
			if kind == "ACK-only" {
				frames = nil
			}
			h.SentPacket(now.Add(2*time.Second), pn, protocol.InvalidPacketNumber, nil, frames, level, protocol.ECNNon, 1200, kind == "MTU", kind == "alternate path")
			if kind == "unsent" {
				pn += 100
			} else if kind == "skipped" {
				previous := pn
				for {
					next := sendCongestionTestPacket(h, now.Add(2*time.Second), level, 1200)
					if next > previous+1 {
						pn = next - 1
						break
					}
					require.Less(t, next, protocol.PacketNumber(1e6))
					previous = next
				}
			}
			_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pn)}, level, now.Add(2010*time.Millisecond))
			if kind == "unsent" || kind == "skipped" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			want := kind == "ordinary" || kind == "ACK-only" || kind == "MTU"
			require.Equal(t, want, r.feedback[len(r.feedback)-1].PersistentCongestion.EndOrdinal != 0)
		})
	}
	t.Run("duplicate witness at time threshold", func(t *testing.T) {
		h, r, now := measuredRecoveryHandler(t)
		sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1200)
		sendCongestionTestPacket(h, now.Add(100*time.Millisecond), protocol.EncryptionInitial, 1200)
		require.True(t, h.QueueProbePacketAt(protocol.EncryptionInitial, now.Add(100*time.Millisecond)))
		require.True(t, h.QueueProbePacketAt(protocol.EncryptionInitial, now.Add(100*time.Millisecond)))
		witness := sendCongestionTestPacket(h, now.Add(101*time.Millisecond), protocol.EncryptionInitial, 1200)
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(111*time.Millisecond), witness)
		require.Zero(t, r.feedback[len(r.feedback)-1].PersistentCongestion.EndOrdinal)
		count := len(r.feedback)
		// Latest and smoothed RTT are 10ms: the existing 9/8 threshold is 11.25ms.
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(111250*time.Microsecond-time.Nanosecond), witness)
		require.Len(t, r.feedback, count)
		// A duplicate of the earlier baseline receipt has no higher packet number.
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(111250*time.Microsecond), 0)
		require.Len(t, r.feedback, count)
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(111250*time.Microsecond), witness)
		require.Len(t, r.feedback, count+1)
		e := r.feedback[len(r.feedback)-1]
		require.Equal(t, uint64(3), e.PersistentCongestion.EndOrdinal)
		require.Empty(t, e.Acked)
		require.Empty(t, e.Lost)
		require.False(t, e.RTTUpdated)
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(120*time.Millisecond), witness)
		require.Len(t, r.feedback, count+1)
	})
	t.Run("classification uses updated RTT", func(t *testing.T) {
		h, r, now := measuredRecoveryHandler(t)
		sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1200)
		sendCongestionTestPacket(h, now.Add(10*time.Second), protocol.EncryptionInitial, 1200)
		for range 2 {
			require.True(t, h.QueueProbePacketAt(protocol.EncryptionInitial, now.Add(10*time.Second)))
		}
		witness := sendCongestionTestPacket(h, now.Add(10*time.Second), protocol.EncryptionInitial, 1200)
		// A fresh 1s RTT raises loss delay to 1.125s; the last original is only 1s old.
		// The 10s span exceeds the updated persistent duration, so stale RTT would report it.
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(11*time.Second), witness)
		require.True(t, r.feedback[len(r.feedback)-1].RTTUpdated)
		require.Zero(t, r.feedback[len(r.feedback)-1].PersistentCongestion.EndOrdinal)
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(11125*time.Millisecond), witness)
		require.Equal(t, uint64(3), r.feedback[len(r.feedback)-1].PersistentCongestion.EndOrdinal)
	})
}

func TestBBRPTOReceiptOrdering(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(fmt.Sprintf("same-event original expired=%v", expired), func(t *testing.T) {
			h, r, now := measuredRecoveryHandler(t)
			first, _ := ptoPair(t, h, now, protocol.EncryptionInitial)
			at := now.Add(1010 * time.Millisecond)
			if expired {
				at = now.Add(2 * time.Second)
			}
			witness := sendCongestionTestPacket(h, at, protocol.EncryptionInitial, 1200)
			acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, at.Add(10*time.Millisecond), first, witness)
			e := r.feedback[len(r.feedback)-1]
			require.Zero(t, e.PersistentCongestion.EndOrdinal)
			require.Zero(t, e.Delivery.Lost)
			wantDelivered := uint64(3600)
			if expired {
				wantDelivered = 2400
			}
			require.Equal(t, wantDelivered, e.Delivery.Delivered)
			if !expired {
				require.Equal(t, congestion.DeliveryPTO, e.Acked[0].Retirement)
			}
			count := len(r.feedback)
			acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, at.Add(20*time.Millisecond), first, witness)
			require.Len(t, r.feedback, count)
		})
	}
	t.Run("late original breaks a subsequent extension", func(t *testing.T) {
		h, r, now := measuredRecoveryHandler(t)
		sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1200)
		middle := sendCongestionTestPacket(h, now.Add(100*time.Millisecond), protocol.EncryptionInitial, 1200)
		sendCongestionTestPacket(h, now.Add(200*time.Millisecond), protocol.EncryptionInitial, 1200)
		for range 3 {
			require.True(t, h.QueueProbePacketAt(protocol.EncryptionInitial, now.Add(200*time.Millisecond)))
		}
		witness := sendCongestionTestPacket(h, now.Add(201*time.Millisecond), protocol.EncryptionInitial, 1200)
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(211*time.Millisecond), witness)
		require.Equal(t, uint64(3), r.feedback[len(r.feedback)-1].PersistentCongestion.EndOrdinal)
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(211100*time.Microsecond), middle)
		e := r.feedback[len(r.feedback)-1]
		require.Equal(t, uint64(3600), e.Delivery.Delivered)
		require.Equal(t, congestion.DeliveryPTO, e.Acked[0].Retirement)
		require.Zero(t, e.PersistentCongestion.EndOrdinal)
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(211250*time.Microsecond), witness)
		require.Zero(t, r.feedback[len(r.feedback)-1].PersistentCongestion.EndOrdinal)
		reports := 0
		for _, event := range r.feedback {
			if event.PersistentCongestion.EndOrdinal != 0 {
				reports++
			}
		}
		require.Equal(t, 1, reports, "the earlier report remains historical; receipt prevents extension")
	})
}

func TestBBRPTOEvidenceBounds(t *testing.T) {
	for _, mode := range []string{"optional live full", "retained eviction", "outcome eviction", "surviving suffix", "unresolved gap", "MTU gap", "alternate-path gap"} {
		t.Run(mode, func(t *testing.T) {
			h, r, now := measuredRecoveryHandler(t)
			if mode == "optional live full" {
				for range 25000 {
					pn := h.PopPacketNumber(protocol.EncryptionHandshake)
					h.SentPacket(now, pn, protocol.InvalidPacketNumber, nil, nil, protocol.EncryptionHandshake, protocol.ECNNon, 40, false, false)
				}
			}
			sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1200)
			if strings.HasSuffix(mode, "gap") {
				pn := h.PopPacketNumber(protocol.EncryptionHandshake)
				h.SentPacket(now.Add(500*time.Millisecond), pn, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.PingFrame{}}}, protocol.EncryptionHandshake, protocol.ECNNon, 1200, mode == "MTU gap", mode == "alternate-path gap")
			}
			sendCongestionTestPacket(h, now.Add(time.Second), protocol.EncryptionInitial, 1200)
			for range 2 {
				require.True(t, h.QueueProbePacketAt(protocol.EncryptionInitial, now.Add(time.Second)))
			}
			if mode == "retained eviction" {
				for range 4096 {
					sendCongestionTestPacket(h, now.Add(time.Second), protocol.EncryptionHandshake, 1200)
					require.True(t, h.QueueProbePacketAt(protocol.EncryptionHandshake, now.Add(time.Second)))
				}
				require.Equal(t, uint64(2), h.DeliveryStats().Evicted)
				require.Equal(t, 4096, h.DeliveryStats().Retained)
			}
			if mode == "outcome eviction" || mode == "surviving suffix" {
				for range 32768 {
					pn := h.PopPacketNumber(protocol.EncryptionHandshake)
					h.SentPacket(now.Add(time.Second), pn, protocol.InvalidPacketNumber, nil, nil, protocol.EncryptionHandshake, protocol.ECNNon, 40, false, false)
				}
				require.Equal(t, 32768, h.DeliveryStats().OutcomeEntries)
				require.Positive(t, h.DeliveryStats().OutcomeEvicted)
				if mode == "surviving suffix" {
					ptoPair(t, h, now.Add(time.Second), protocol.EncryptionInitial)
				}
			}
			at := now.Add(3 * time.Second)
			witness := sendCongestionTestPacket(h, at, protocol.EncryptionInitial, 1200)
			acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, at.Add(10*time.Millisecond), witness)
			e := r.feedback[len(r.feedback)-1]
			want := mode == "optional live full" || mode == "retained eviction" || mode == "surviving suffix"
			require.Equal(t, want, e.PersistentCongestion.EndOrdinal != 0)
			require.Empty(t, e.Lost)
			require.Zero(t, e.Delivery.Lost)
			if mode == "optional live full" {
				require.Positive(t, h.DeliveryStats().Missing)
				require.Empty(t, e.Acked)
			}
		})
	}
}

func TestBBRPTOConfirmationLifecycle(t *testing.T) {
	for _, mode := range []string{"Retry", "path", "close", "rejected 0-RTT", "preserve 1-RTT", "shared 0-RTT identity", "key discard", "unmeasured"} {
		t.Run(mode, func(t *testing.T) {
			h, r, now := measuredRecoveryHandler(t)
			level := protocol.Encryption1RTT
			switch mode {
			case "Retry", "key discard":
				level = protocol.EncryptionInitial
			case "rejected 0-RTT", "shared 0-RTT identity":
				level = protocol.Encryption0RTT
			case "unmeasured":
				r = &congestionRecorder{}
				h = newCongestionTestHandler(r)
			}
			ptoPair(t, h, now, level)
			if level == protocol.Encryption0RTT {
				level = protocol.Encryption1RTT
			}
			witness := sendCongestionTestPacket(h, now.Add(2*time.Second), level, 1200)
			switch mode {
			case "Retry":
				h.ResetForRetry(now.Add(2 * time.Second))
			case "path":
				h.MigratedPath(now.Add(2*time.Second), 1200)
			case "close":
				h.CloseDelivery()
			case "rejected 0-RTT", "preserve 1-RTT":
				h.DropPackets(protocol.Encryption0RTT, now.Add(2*time.Second))
			case "key discard":
				h.DropPackets(protocol.EncryptionInitial, now.Add(2*time.Second))
				// The transport does not dispatch ACKs into a discarded space.
				// A receipt in another live space must not revive its endpoints.
				level = protocol.EncryptionHandshake
				witness = sendCongestionTestPacket(h, now.Add(2*time.Second), level, 1200)
			}
			_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(witness)}, level, now.Add(2010*time.Millisecond))
			if mode == "Retry" {
				require.Error(t, err, "old packet number is no longer registered after Retry")
			} else {
				require.NoError(t, err)
			}
			want := mode == "preserve 1-RTT" || mode == "shared 0-RTT identity"
			if len(r.feedback) > 0 {
				require.Equal(t, want, r.feedback[len(r.feedback)-1].PersistentCongestion.EndOrdinal != 0)
			} else {
				require.False(t, want)
			}
		})
	}
	t.Run("each space needs its own receipt", func(t *testing.T) {
		h, r, now := measuredRecoveryHandler(t)
		sendCongestionTestPacket(h, now, protocol.EncryptionInitial, 1200)
		sendCongestionTestPacket(h, now.Add(time.Second), protocol.EncryptionHandshake, 1200)
		require.True(t, h.QueueProbePacketAt(protocol.EncryptionInitial, now.Add(time.Second)))
		require.True(t, h.QueueProbePacketAt(protocol.EncryptionHandshake, now.Add(time.Second)))
		initial := sendCongestionTestPacket(h, now.Add(2*time.Second), protocol.EncryptionInitial, 1200)
		handshake := sendCongestionTestPacket(h, now.Add(2*time.Second), protocol.EncryptionHandshake, 1200)
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(2010*time.Millisecond), initial)
		require.Zero(t, r.feedback[len(r.feedback)-1].PersistentCongestion.EndOrdinal)
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionHandshake, now.Add(2020*time.Millisecond), handshake)
		e := r.feedback[len(r.feedback)-1]
		require.Equal(t, uint64(2), e.PersistentCongestion.StartOrdinal)
		require.Equal(t, uint64(3), e.PersistentCongestion.EndOrdinal)
	})
}

func TestBBRPTOAccountingPreservation(t *testing.T) {
	for _, payload := range []string{"stream", "DATAGRAM", "mixed"} {
		t.Run(payload, func(t *testing.T) {
			h, r, now := measuredRecoveryHandler(t)
			var order []string
			h.congestion = &congestionLegacyTap{SendAlgorithmWithDebugInfos: h.congestion, order: &order}
			lost, acked := 0, 0
			var originals []protocol.PacketNumber
			for _, at := range []monotime.Time{now, now.Add(time.Second)} {
				var streams []StreamFrame
				var frames []Frame
				if payload != "DATAGRAM" {
					streams = []StreamFrame{{Frame: &wire.StreamFrame{StreamID: 0, Data: []byte("stream")}, Handler: &customFrameHandler{
						onLost: func(wire.Frame) { lost++ }, onAcked: func(wire.Frame) { acked++ },
					}}}
				}
				if payload != "stream" {
					// Like real packing, application DATAGRAM has no retransmission handler.
					frames = []Frame{{Frame: &wire.DatagramFrame{Data: []byte("unreliable")}}}
				}
				pn := h.PopPacketNumber(protocol.Encryption1RTT)
				originals = append(originals, pn)
				h.SentPacket(at, pn, protocol.InvalidPacketNumber, streams, frames, protocol.Encryption1RTT, protocol.ECNNon, 1200, false, false)
			}
			for range 2 {
				require.True(t, h.QueueProbePacketAt(protocol.Encryption1RTT, now.Add(time.Second)))
			}
			wantCallbacks := 2
			if payload == "DATAGRAM" {
				wantCallbacks = 0
			}
			require.Equal(t, wantCallbacks, lost)
			require.Zero(t, h.bytesInFlight)
			witness := sendCongestionTestPacket(h, now.Add(1010*time.Millisecond), protocol.Encryption1RTT, 1200)
			order = nil
			acknowledgeRecoveryPacket(t, h, protocol.Encryption1RTT, now.Add(1020*time.Millisecond), witness)
			e := r.feedback[len(r.feedback)-1]
			require.NotZero(t, e.PersistentCongestion.EndOrdinal)
			require.Equal(t, protocol.ByteCount(1200), e.PriorInFlight)
			require.Zero(t, e.PostInFlight)
			require.Empty(t, e.Lost)
			require.Zero(t, e.Delivery.Lost)
			require.Zero(t, e.RecoveryEpisode.ID)
			require.False(t, e.RecoveryEpisode.UndoPossible)
			for _, event := range order {
				require.NotContains(t, event, "congestion")
			}
			for pn := range h.lostPackets.All() {
				t.Fatalf("PTO confirmation entered ordinary lost-packet tracking: %d", pn)
			}
			order = nil
			acknowledgeRecoveryPacket(t, h, protocol.Encryption1RTT, now.Add(1021*time.Millisecond), originals...)
			late := r.feedback[len(r.feedback)-1]
			require.Equal(t, uint64(4800), late.Delivery.Delivered)
			require.Zero(t, late.Delivery.Lost)
			require.Empty(t, order)
			require.Equal(t, wantCallbacks, lost)
			require.Zero(t, acked, "PTO already transferred frame ownership")
			require.Zero(t, h.bytesInFlight)
			count := len(r.feedback)
			acknowledgeRecoveryPacket(t, h, protocol.Encryption1RTT, now.Add(1022*time.Millisecond), originals...)
			require.Len(t, r.feedback, count)
		})
	}
}
