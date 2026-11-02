package ackhandler

import (
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

type localRichRecorder struct {
	recorder   *congestionRecorder
	controller *congestion.BBRSender
}

func (r *localRichRecorder) Sent(e congestion.SendEvent) {
	r.recorder.Sent(e)
	r.controller.Sent(e)
}

func (r *localRichRecorder) Feedback(e congestion.FeedbackEvent) {
	r.recorder.Feedback(e)
	r.controller.Feedback(e)
}

func newLocalRichHandler(t *testing.T) (*sentPacketHandler, *congestionRecorder) {
	t.Helper()
	h := newLocalTestHandler(t).(*sentPacketHandler)
	r := &congestionRecorder{}
	b := EnableBBR(h, 1200, func() protocol.ByteCount { return 0 })
	h.congestionEvents.sink = &localRichRecorder{recorder: r, controller: b}
	return h, r
}

func localRichPacket(t *testing.T, h *sentPacketHandler, now monotime.Time) (protocol.PacketNumber, LocalCompletionToken) {
	t.Helper()
	return registerLocalTestPacket(t, h, now, protocol.EncryptionInitial, []Frame{{Frame: &wire.PingFrame{}}})
}

func TestLocalRichOrderedCompletion(t *testing.T) {
	h, r := newLocalRichHandler(t)
	now := monotime.Now()
	a, ta := localRichPacket(t, h, now)
	b, tb := localRichPacket(t, h, now.Add(time.Millisecond))
	require.NoError(t, h.CompleteLocalPacket(tb, LocalPacketAccepted, now.Add(3*time.Millisecond)))
	require.NoError(t, h.CompleteLocalPacket(ta, LocalPacketAccepted, now.Add(2*time.Millisecond)))
	acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(10*time.Millisecond), a, b)
	require.Len(t, r.sends, 2, "completion must not register again")
	e := r.feedback[len(r.feedback)-1]
	require.Len(t, e.Acked, 2)
	require.Equal(t, r.sends[0].Packet.SendTime, e.Acked[0].SendTime)
	require.Equal(t, r.sends[1].Packet.SendTime, e.Acked[1].SendTime)
	require.Equal(t, uint64(2400), e.Delivery.Delivered)
	require.True(t, e.Delivery.Valid)
	require.Equal(t, uint64(2), e.Acked[1].Ordinal)
	t.Run("backward endpoints and fresh segment", func(t *testing.T) {
		h, r, now := measuredLocalRichHandler(t)
		_, prefix := localRichPacket(t, h, now)
		_, backward := localRichPacket(t, h, now.Add(time.Millisecond))
		_, stillBackward := localRichPacket(t, h, now.Add(2*time.Millisecond))
		require.NoError(t, h.CompleteLocalPacket(backward, LocalPacketAccepted, now.Add(20*time.Millisecond)))
		require.NoError(t, h.CompleteLocalPacket(stillBackward, LocalPacketAccepted, now.Add(30*time.Millisecond)))
		require.NoError(t, h.CompleteLocalPacket(prefix, LocalPacketAccepted, now.Add(100*time.Millisecond)))
		_, c := localRichPacket(t, h, now.Add(110*time.Millisecond))
		_, d := localRichPacket(t, h, now.Add(210*time.Millisecond))
		require.NoError(t, h.CompleteLocalPacket(c, LocalPacketAccepted, now.Add(120*time.Millisecond)))
		require.NoError(t, h.CompleteLocalPacket(d, LocalPacketAccepted, now.Add(220*time.Millisecond)))
		for range 5 {
			require.True(t, h.QueueProbePacketAt(protocol.EncryptionInitial, now.Add(230*time.Millisecond)))
		}
		witness, wt := localRichPacket(t, h, now.Add(300*time.Millisecond))
		require.NoError(t, h.CompleteLocalPacket(wt, LocalPacketAccepted, now.Add(301*time.Millisecond)))
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(310*time.Millisecond), witness)
		e := r.feedback[len(r.feedback)-1]
		require.Equal(t, uint64(5), e.PersistentCongestion.StartOrdinal)
		require.Equal(t, uint64(6), e.PersistentCongestion.EndOrdinal)
		require.True(t, e.Delivery.Valid)
	})
	t.Run("evicted facts stay absent", func(t *testing.T) {
		h, r := newLocalRichHandler(t)
		now := monotime.Now()
		pn, old := localRichPacket(t, h, now)
		for i := range maxRecoveryOutcomes {
			at := now.Add(time.Duration(i+2) * time.Millisecond)
			pn, tok := registerLocalTestPacket(t, h, at, protocol.EncryptionHandshake, []Frame{{Frame: &wire.PingFrame{}}})
			require.NoError(t, h.CompleteLocalPacket(tok, LocalPacketAccepted, at))
			acknowledgeRecoveryPacket(t, h, protocol.EncryptionHandshake, at.Add(time.Microsecond), pn)
			r.sends, r.feedback = nil, nil
		}
		_, fresh := localRichPacket(t, h, now.Add(40*time.Second))
		require.NoError(t, h.CompleteLocalPacket(fresh, LocalPacketAccepted, now.Add(41*time.Second)))
		r.sends, r.feedback = nil, nil
		before := h.DeliveryStats()
		require.Positive(t, before.OutcomeEvicted)
		require.NoError(t, h.CompleteLocalPacket(old, LocalPacketAccepted, now.Add(time.Second)))
		require.Equal(t, before, h.DeliveryStats())
		require.Empty(t, r.sends)
		require.Empty(t, r.feedback)
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(time.Minute), pn)
		require.False(t, r.feedback[len(r.feedback)-1].Delivery.Valid, "missing outcome cannot recover old timing")
		require.Equal(t, uint64((maxRecoveryOutcomes+1)*1200), r.feedback[len(r.feedback)-1].Delivery.Delivered, "timing eviction cannot lose logical delivery")
	})
}

func TestLocalRichTimingFence(t *testing.T) {
	for _, mode := range []string{"early ACK", "uncertain", "unsubmitted"} {
		t.Run(mode, func(t *testing.T) {
			h, r := newLocalRichHandler(t)
			now := monotime.Now()
			a, ta := localRichPacket(t, h, now)
			b, tb := localRichPacket(t, h, now.Add(time.Millisecond))
			require.NoError(t, h.CompleteLocalPacket(tb, LocalPacketAccepted, now.Add(2*time.Millisecond)))
			if mode == "early ACK" {
				acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(3*time.Millisecond), a)
				e := r.feedback[len(r.feedback)-1]
				require.Equal(t, uint64(1200), e.Delivery.Delivered)
				require.False(t, e.Delivery.Valid)
				require.Zero(t, e.RawRTT)
				require.NoError(t, h.CompleteLocalPacket(ta, LocalPacketAccepted, now.Add(4*time.Millisecond)))
			} else {
				outcome := LocalPacketUncertain
				if mode == "unsubmitted" {
					outcome = LocalPacketUnsubmitted
				}
				require.NoError(t, h.CompleteLocalPacket(ta, outcome, now.Add(3*time.Millisecond)))
				if mode == "uncertain" {
					acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(4*time.Millisecond), a)
					require.False(t, r.feedback[len(r.feedback)-1].Delivery.Valid)
					require.Zero(t, r.feedback[len(r.feedback)-1].RawRTT)
				}
			}
			acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(5*time.Millisecond), b)
			e := r.feedback[len(r.feedback)-1]
			require.False(t, e.Delivery.Valid, "a pre-fence snapshot cannot bridge excluded timing")
			delivered := uint64(2400)
			if mode == "unsubmitted" {
				delivered = 1200
			}
			require.Equal(t, delivered, e.Delivery.Delivered)
			require.Zero(t, e.Delivery.Lost, "local disposal is not network loss")
			c, tc := localRichPacket(t, h, now.Add(6*time.Millisecond))
			require.NoError(t, h.CompleteLocalPacket(tc, LocalPacketAccepted, now.Add(7*time.Millisecond)))
			acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(16*time.Millisecond), c)
			e = r.feedback[len(r.feedback)-1]
			require.True(t, e.Delivery.Valid, "fresh accepted anchor restores sampling")
			require.Equal(t, delivered+1200, e.Delivery.Delivered)
			require.Equal(t, now.Add(6*time.Millisecond), e.Acked[0].SendTime)
			require.Equal(t, 10*time.Millisecond, e.RawRTT, "accepted samples retain registration provenance")
			acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(17*time.Millisecond), c)
			require.Equal(t, delivered+1200, h.congestionEvents.sampler.delivered, "duplicate ACK is not delivery")
		})
	}
	t.Run("late uncertain receipt fences newer snapshots", func(t *testing.T) {
		h, r := newLocalRichHandler(t)
		now := monotime.Now()
		a, ta := localRichPacket(t, h, now)
		require.NoError(t, h.CompleteLocalPacket(ta, LocalPacketUncertain, now.Add(time.Millisecond)))
		b, tb := localRichPacket(t, h, now.Add(2*time.Millisecond))
		require.NoError(t, h.CompleteLocalPacket(tb, LocalPacketAccepted, now.Add(3*time.Millisecond)))
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(4*time.Millisecond), a)
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(5*time.Millisecond), b)
		require.False(t, r.feedback[len(r.feedback)-1].Delivery.Valid, "uncertain delivery cannot enter a later rate numerator")
		c, tc := localRichPacket(t, h, now.Add(6*time.Millisecond))
		require.NoError(t, h.CompleteLocalPacket(tc, LocalPacketAccepted, now.Add(7*time.Millisecond)))
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(16*time.Millisecond), c)
		require.True(t, r.feedback[len(r.feedback)-1].Delivery.Valid)
		require.Equal(t, uint64(3600), r.feedback[len(r.feedback)-1].Delivery.Delivered)
	})
	for _, lifecycle := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending credit after timing fence", true: "pending credit after 0-RTT disposal"}[lifecycle], func(t *testing.T) {
			h, r := newLocalRichHandler(t)
			var pending protocol.ByteCount
			h.congestionEvents.pending = func() protocol.ByteCount { return pending }
			now := monotime.Now()
			level := protocol.EncryptionInitial
			if lifecycle {
				level = protocol.Encryption0RTT
			}
			a, ta := registerLocalTestPacket(t, h, now, level, []Frame{{Frame: &wire.PingFrame{}}})
			if lifecycle {
				h.DropPackets(level, now.Add(time.Millisecond))
			} else {
				require.NoError(t, h.CompleteLocalPacket(ta, LocalPacketUncertain, now.Add(time.Millisecond)))
			}
			generation := h.congestionEvents.sampleGeneration
			pending = 1200
			b, tb := localRichPacket(t, h, now.Add(2*time.Millisecond))
			require.NoError(t, h.CompleteLocalPacket(tb, LocalPacketAccepted, now.Add(3*time.Millisecond)))
			pending = 0
			c, tc := localRichPacket(t, h, now.Add(4*time.Millisecond))
			require.NoError(t, h.CompleteLocalPacket(tc, LocalPacketAccepted, now.Add(5*time.Millisecond)))
			require.Equal(t, generation, r.sends[2].Packet.SampleGeneration, "origin restoration is timing-only")
			if !lifecycle {
				acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(6*time.Millisecond), a)
			}
			acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(7*time.Millisecond), b)
			acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(16*time.Millisecond), c)
			e := r.feedback[len(r.feedback)-1]
			delivered := uint64(3600)
			if lifecycle {
				delivered = 2400
			}
			require.Equal(t, delivered, e.Delivery.Delivered)
			require.Zero(t, e.Delivery.Lost)
			require.False(t, e.Delivery.Valid)
			fresh, ft := localRichPacket(t, h, now.Add(20*time.Millisecond))
			require.NoError(t, h.CompleteLocalPacket(ft, LocalPacketAccepted, now.Add(21*time.Millisecond)))
			acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(30*time.Millisecond), fresh)
			require.True(t, r.feedback[len(r.feedback)-1].Delivery.Valid)
			require.Equal(t, delivered+1200, r.feedback[len(r.feedback)-1].Delivery.Delivered)
		})
	}

	t.Run("older accepted receipt preserves the send-time bound", func(t *testing.T) {
		for _, retained := range []bool{false, true} {
			t.Run(map[bool]string{false: "live", true: "PTO retained"}[retained], func(t *testing.T) {
				h, r := newLocalRichHandler(t)
				now := monotime.Now()
				a, ta := localRichPacket(t, h, now)
				b, tb := localRichPacket(t, h, now.Add(time.Millisecond))
				require.NoError(t, h.CompleteLocalPacket(tb, LocalPacketAccepted, now.Add(2*time.Millisecond)))
				require.NoError(t, h.CompleteLocalPacket(ta, LocalPacketUncertain, now.Add(3*time.Millisecond)))
				acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(4*time.Millisecond), a)
				if retained {
					require.True(t, h.QueueProbePacketAt(protocol.EncryptionInitial, now.Add(5*time.Millisecond)))
				}
				c, tc := localRichPacket(t, h, now.Add(100*time.Millisecond))
				require.NoError(t, h.CompleteLocalPacket(tc, LocalPacketAccepted, now.Add(100*time.Millisecond+100*time.Microsecond)))
				acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(100*time.Millisecond+200*time.Microsecond), b)
				acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(101*time.Millisecond), c)
				e := r.feedback[len(r.feedback)-1]
				require.Equal(t, uint64(3600), e.Delivery.Delivered)
				require.Zero(t, e.Delivery.Lost)
				require.False(t, e.Delivery.Valid, "older delivery must not enter a numerator without its send-time bound")
				fresh, ft := localRichPacket(t, h, now.Add(110*time.Millisecond))
				require.NoError(t, h.CompleteLocalPacket(ft, LocalPacketAccepted, now.Add(111*time.Millisecond)))
				acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(120*time.Millisecond), fresh)
				require.True(t, r.feedback[len(r.feedback)-1].Delivery.Valid)
				require.Equal(t, uint64(4800), r.feedback[len(r.feedback)-1].Delivery.Delivered)
			})
		}
	})

	t.Run("accepted pipeline recovers without draining", func(t *testing.T) {
		h, r := newLocalRichHandler(t)
		now := monotime.Now()
		a, ta := localRichPacket(t, h, now)
		previous, tb := localRichPacket(t, h, now.Add(time.Millisecond))
		require.NoError(t, h.CompleteLocalPacket(tb, LocalPacketAccepted, now.Add(2*time.Millisecond)))
		require.NoError(t, h.CompleteLocalPacket(ta, LocalPacketUncertain, now.Add(3*time.Millisecond)))
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(4*time.Millisecond), a)
		for i := range 8 {
			at := now.Add(time.Duration(10+i*10) * time.Millisecond)
			next, tok := localRichPacket(t, h, at)
			require.NoError(t, h.CompleteLocalPacket(tok, LocalPacketAccepted, at.Add(time.Millisecond)))
			acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, at.Add(2*time.Millisecond), previous)
			e := r.feedback[len(r.feedback)-1]
			require.Equal(t, uint64(i+2)*1200, e.Delivery.Delivered)
			require.Zero(t, e.Delivery.Lost)
			if i >= 2 {
				require.True(t, e.Delivery.Valid, "accepted successors covered by the retained send-time bound recover sampling")
				require.Positive(t, e.RawRTT)
			}
			previous = next
		}
	})

	t.Run("accepted ACK-only receipt cannot seed a partial baseline", func(t *testing.T) {
		h, r := newLocalRichHandler(t)
		now := monotime.Now()
		ao := h.PopPacketNumber(protocol.EncryptionInitial)
		aot, err := h.RegisterLocalPacket(PacketContext{}, now, ao, 0, nil, nil, protocol.EncryptionInitial, protocol.ECNNon, 1200)
		require.NoError(t, err)
		require.NoError(t, h.CompleteLocalPacket(aot, LocalPacketAccepted, now.Add(time.Millisecond)))
		_, bt := localRichPacket(t, h, now.Add(2*time.Millisecond))
		require.NoError(t, h.CompleteLocalPacket(bt, LocalPacketUncertain, now.Add(5*time.Millisecond)))
		generation := h.congestionEvents.sampler.timingGeneration
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(5*time.Millisecond+time.Microsecond), ao)
		require.Equal(t, generation, h.congestionEvents.sampler.timingGeneration, "a known accepted receipt outside the numerator cannot retain a stale send bound")
		require.Zero(t, h.congestionEvents.sampler.delivered)
		c, ct := localRichPacket(t, h, now.Add(6*time.Millisecond))
		require.NoError(t, h.CompleteLocalPacket(ct, LocalPacketAccepted, now.Add(7*time.Millisecond)))
		snapshot := r.sends[len(r.sends)-1].Packet.Delivery
		require.True(t, snapshot.Valid)
		require.Equal(t, now.Add(6*time.Millisecond), snapshot.DeliveredTime)
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(16*time.Millisecond), c)
		e := r.feedback[len(r.feedback)-1]
		require.True(t, e.Delivery.Valid)
		require.Equal(t, 10*time.Millisecond, e.Delivery.Interval)
		require.Equal(t, uint64(1200), e.Delivery.Delivered)
		require.Equal(t, uint64(120000), e.Delivery.BytesPerSecond)
		fresh, ft := localRichPacket(t, h, now.Add(20*time.Millisecond))
		require.NoError(t, h.CompleteLocalPacket(ft, LocalPacketAccepted, now.Add(21*time.Millisecond)))
		acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(30*time.Millisecond), fresh)
		require.True(t, r.feedback[len(r.feedback)-1].Delivery.Valid)
		require.Equal(t, uint64(2400), r.feedback[len(r.feedback)-1].Delivery.Delivered)
		for _, sent := range r.sends {
			if sent.Packet.Delivery.Valid {
				require.False(t, sent.Packet.Delivery.DeliveredTime.IsZero(), "valid snapshots require a complete timing baseline")
			}
		}
	})

	for _, lifecycle := range []string{"Retry", "Initial drop", "Handshake drop", "0-RTT drop", "close"} {
		t.Run(lifecycle, func(t *testing.T) {
			h, r := newLocalRichHandler(t)
			now := monotime.Now()
			level := protocol.EncryptionInitial
			switch lifecycle {
			case "Handshake drop":
				level = protocol.EncryptionHandshake
			case "0-RTT drop":
				level = protocol.Encryption0RTT
			}
			_, tok := registerLocalTestPacket(t, h, now, level, []Frame{{Frame: &wire.PingFrame{}}})
			switch lifecycle {
			case "Retry":
				h.ResetForRetry(now.Add(time.Millisecond))
			case "close":
				h.CloseLocal(now.Add(time.Millisecond))
			default:
				h.DropPackets(level, now.Add(time.Millisecond))
			}
			before := h.DeliveryStats()
			require.NoError(t, h.CompleteLocalPacket(tok, LocalPacketUncertain, now.Add(2*time.Millisecond)))
			require.Equal(t, before, h.DeliveryStats(), "late result cannot mutate replacement evidence")
			require.Error(t, h.CompleteLocalPacket(tok, LocalPacketAccepted, now.Add(3*time.Millisecond)))
			if lifecycle == "close" {
				require.Zero(t, h.DeliveryStats().OutcomeEntries)
				return
			}
			nextLevel := protocol.EncryptionInitial
			if lifecycle == "Initial drop" {
				nextLevel = protocol.EncryptionHandshake
			}
			if lifecycle == "0-RTT drop" {
				nextLevel = protocol.Encryption1RTT
			}
			pn, next := registerLocalTestPacket(t, h, now.Add(10*time.Millisecond), nextLevel, []Frame{{Frame: &wire.PingFrame{}}})
			require.NoError(t, h.CompleteLocalPacket(next, LocalPacketAccepted, now.Add(11*time.Millisecond)))
			acknowledgeRecoveryPacket(t, h, nextLevel, now.Add(20*time.Millisecond), pn)
			require.True(t, r.feedback[len(r.feedback)-1].Delivery.Valid, "fresh lifecycle anchor remains usable")
		})
	}
}

func measuredLocalRichHandler(t *testing.T) (*sentPacketHandler, *congestionRecorder, monotime.Time) {
	t.Helper()
	h, r := newLocalRichHandler(t)
	now := monotime.Now()
	pn, tok := localRichPacket(t, h, now)
	require.NoError(t, h.CompleteLocalPacket(tok, LocalPacketAccepted, now.Add(time.Millisecond)))
	acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(10*time.Millisecond), pn)
	return h, r, now.Add(20 * time.Millisecond)
}

func TestLocalRichRecoveryQualification(t *testing.T) {
	for _, outcome := range []LocalPacketOutcome{LocalPacketAccepted, LocalPacketUncertain, LocalPacketUnsubmitted} {
		t.Run("resolved prefix "+string(rune('0'+outcome)), func(t *testing.T) {
			h, r, now := measuredLocalRichHandler(t)
			_, prefix := localRichPacket(t, h, now)
			_, a := localRichPacket(t, h, now.Add(20*time.Millisecond))
			_, b := localRichPacket(t, h, now.Add(120*time.Millisecond))
			require.NoError(t, h.CompleteLocalPacket(b, LocalPacketAccepted, now.Add(130*time.Millisecond)))
			require.NoError(t, h.CompleteLocalPacket(a, LocalPacketAccepted, now.Add(30*time.Millisecond)))
			require.True(t, h.QueueProbePacketAt(protocol.EncryptionInitial, now.Add(140*time.Millisecond)))
			require.True(t, h.QueueProbePacketAt(protocol.EncryptionInitial, now.Add(140*time.Millisecond)))
			require.False(t, h.QueueProbePacketAt(protocol.EncryptionInitial, now.Add(140*time.Millisecond)), "pending prefix stays owned")
			witness, wt := localRichPacket(t, h, now.Add(230*time.Millisecond))
			require.NoError(t, h.CompleteLocalPacket(wt, LocalPacketAccepted, now.Add(231*time.Millisecond)))
			acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(240*time.Millisecond), witness)
			require.Zero(t, r.feedback[len(r.feedback)-1].PersistentCongestion.EndOrdinal, "pending prefix cannot certify later timing spans")
			require.NoError(t, h.CompleteLocalPacket(prefix, outcome, now.Add(10*time.Millisecond)))
			acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(241*time.Millisecond), witness)
			e := r.feedback[len(r.feedback)-1]
			require.Equal(t, uint64(4), e.PersistentCongestion.EndOrdinal, "resolved suffix must become usable")
			first := uint64(3)
			if outcome == LocalPacketAccepted {
				first = 2
			}
			require.Equal(t, first, e.PersistentCongestion.StartOrdinal)
			lost := uint64(1200) // the resolved prefix is ordinary packet-threshold loss
			if outcome == LocalPacketUnsubmitted {
				lost = 0
			}
			require.Equal(t, lost, e.Delivery.Lost, "PTO confirmation adds no delivery loss")
			// The restart itself advances logical identity; origin restoration
			// after pending credit must retain deliveries from that new epoch.
			var pending protocol.ByteCount = 1200
			h.congestionEvents.pending = func() protocol.ByteCount { return pending }
			generation := h.congestionEvents.sampleGeneration
			originless, ot := localRichPacket(t, h, now.Add(250*time.Millisecond))
			require.NoError(t, h.CompleteLocalPacket(ot, LocalPacketAccepted, now.Add(251*time.Millisecond)))
			pending = 0
			fresh, ft := localRichPacket(t, h, now.Add(260*time.Millisecond))
			require.NoError(t, h.CompleteLocalPacket(ft, LocalPacketAccepted, now.Add(261*time.Millisecond)))
			require.Equal(t, generation, r.sends[len(r.sends)-1].Packet.SampleGeneration)
			acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(270*time.Millisecond), originless, fresh)
			require.Equal(t, e.Delivery.Delivered+2400, r.feedback[len(r.feedback)-1].Delivery.Delivered)
			require.False(t, r.feedback[len(r.feedback)-1].Delivery.Valid)
		})
	}
	for _, outcome := range []LocalPacketOutcome{localPacketPending, LocalPacketUncertain, LocalPacketUnsubmitted} {
		t.Run("excluded middle "+string(rune('0'+outcome)), func(t *testing.T) {
			h, r, now := measuredLocalRichHandler(t)
			_, a := localRichPacket(t, h, now)
			_, middle := localRichPacket(t, h, now.Add(time.Millisecond))
			_, b := localRichPacket(t, h, now.Add(100*time.Millisecond))
			require.NoError(t, h.CompleteLocalPacket(a, LocalPacketAccepted, now.Add(2*time.Millisecond)))
			if outcome != localPacketPending {
				require.NoError(t, h.CompleteLocalPacket(middle, outcome, now.Add(3*time.Millisecond)))
			}
			require.NoError(t, h.CompleteLocalPacket(b, LocalPacketAccepted, now.Add(101*time.Millisecond)))
			witness, wt := localRichPacket(t, h, now.Add(200*time.Millisecond))
			require.NoError(t, h.CompleteLocalPacket(wt, LocalPacketAccepted, now.Add(201*time.Millisecond)))
			acknowledgeRecoveryPacket(t, h, protocol.EncryptionInitial, now.Add(210*time.Millisecond), witness)
			e := r.feedback[len(r.feedback)-1]
			require.Zero(t, e.PersistentCongestion.EndOrdinal)
			require.NotEmpty(t, e.Lost, "ordinary loss must survive timing exclusion")
		})
	}
}
