package ackhandler

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

func TestAdaptiveLossPacketThreshold(t *testing.T) {
	for _, controller := range []string{"Reno", "BBR"} {
		t.Run(controller, func(t *testing.T) {
			f := newSpuriousLossFixture(false)
			if controller == "BBR" {
				EnableBBR(f.h, 1200, func() protocol.ByteCount { return 0 })
			}
			pns := f.loseFirstTwo(t)
			f.ack(t, 105*time.Millisecond, pns...)
			var fresh []protocol.PacketNumber
			for range 5 {
				fresh = append(fresh, f.send(110*time.Millisecond, protocol.Encryption1RTT))
			}
			f.ack(t, 210*time.Millisecond, fresh[4])
			require.Equal(t, pns[:2], f.packets.Lost, "proof suppresses packet-distance loss for the fresh flight")
			deadline := f.h.GetLossDetectionTimeout()
			require.Equal(t, f.start.Add(222500*time.Microsecond), deadline)
			_, err := f.h.OnLossDetectionTimeout(deadline)
			require.NoError(t, err)
			require.Equal(t, append(slices.Clone(pns[:2]), fresh[:4]...), f.packets.Lost)
			require.Zero(t, f.h.bytesInFlight)
			_, err = f.h.OnLossDetectionTimeout(deadline.Add(time.Millisecond))
			require.NoError(t, err)
			require.Len(t, f.packets.Lost, 6, "timer retirement is exactly once")
		})
	}
}

func TestAdaptiveLossGrowthAndCap(t *testing.T) {
	for _, controller := range []string{"Reno", "BBR"} {
		t.Run(controller, func(t *testing.T) {
			f := newSpuriousLossFixture(false)
			if controller == "BBR" {
				EnableBBR(f.h, 1200, func() protocol.ByteCount { return 0 })
			}
			// Each new flight follows the preceding increase. The literal deadlines
			// exercise 9/8 -> 5/4 -> 3/2 -> 2, then the unchanged 2 RTT cap.
			for step, deadlineAfterSend := range []time.Duration{1750 * time.Millisecond, 2 * time.Second, 2500 * time.Millisecond, 2500 * time.Millisecond} {
				t.Run(fmt.Sprintf("step_%d", step), func(t *testing.T) {
					origin := time.Duration(step) * 4 * time.Second
					var first, later []protocol.PacketNumber
					for range 3 {
						first = append(first, f.send(origin, protocol.Encryption1RTT))
					}
					for range 3 {
						later = append(later, f.send(origin+500*time.Millisecond, protocol.Encryption1RTT))
					}
					f.ack(t, origin+1500*time.Millisecond, later[2])
					proofAt := origin + 1562500*time.Microsecond
					if step == 3 {
						_, err := f.h.OnLossDetectionTimeout(f.start.Add(origin + 2*time.Second))
						require.NoError(t, err)
						proofAt = origin + 2062500*time.Microsecond
					}
					f.ack(t, proofAt, first...)
					deadline := f.start.Add(origin + deadlineAfterSend)
					require.Equal(t, deadline, f.h.GetLossDetectionTimeout())
					_, err := f.h.OnLossDetectionTimeout(deadline.Add(-time.Nanosecond))
					require.NoError(t, err)
					require.NotContains(t, f.packets.Lost, later[0])
					_, err = f.h.OnLossDetectionTimeout(deadline)
					require.NoError(t, err)
					require.Contains(t, f.packets.Lost, later[0])
					require.Contains(t, f.packets.Lost, later[1])
					f.ack(t, origin+3*time.Second, later...)
				})
			}
		})
	}
}

func TestAdaptiveLossOldReceiptAfterFreshSend(t *testing.T) {
	f := newSpuriousLossFixture(false)
	var first, later []protocol.PacketNumber
	for range 3 {
		first = append(first, f.send(0, protocol.Encryption1RTT))
	}
	for range 3 {
		later = append(later, f.send(500*time.Millisecond, protocol.Encryption1RTT))
	}
	f.ack(t, 1500*time.Millisecond, later[2])
	f.ack(t, 1562500*time.Microsecond, first[0])
	f.ack(t, 1563*time.Millisecond, first[1]) // No new send: no further increase.
	f.send(1564*time.Millisecond, protocol.Encryption1RTT)
	f.ack(t, 1565*time.Millisecond, first[2]) // New send cannot qualify this old loss.
	require.Equal(t, f.start.Add(1750*time.Millisecond), f.h.GetLossDetectionTimeout())
}

func TestAdaptiveLossSpacesAndMigration(t *testing.T) {
	for _, controller := range []string{"Reno", "BBR"} {
		for _, level := range []protocol.EncryptionLevel{protocol.EncryptionInitial, protocol.EncryptionHandshake, protocol.Encryption1RTT} {
			for _, state := range []string{"default", "trained", "migrated"} {
				t.Run(fmt.Sprintf("%s/%s/%s", controller, level, state), func(t *testing.T) {
					f := newSpuriousLossFixture(false)
					if controller == "BBR" {
						EnableBBR(f.h, 1200, func() protocol.ByteCount { return 0 })
					}
					if state != "default" {
						pns := f.loseFirstTwo(t)
						f.ack(t, 120*time.Millisecond, pns...)
					}
					if state == "migrated" {
						f.h.MigratedPath(f.start.Add(200*time.Millisecond), 1200)
					}
					f.packets.Reset()
					var fresh []protocol.PacketNumber
					for range 5 {
						fresh = append(fresh, f.send(300*time.Millisecond, level))
					}
					ackLevel := level
					if level == protocol.Encryption0RTT {
						ackLevel = protocol.Encryption1RTT
					}
					acknowledgeRecoveryPacket(t, f.h, ackLevel, f.start.Add(400*time.Millisecond), fresh[4])
					if state == "trained" && (level == protocol.Encryption0RTT || level == protocol.Encryption1RTT) {
						require.Empty(t, f.packets.Lost)
						require.Equal(t, f.start.Add(425*time.Millisecond), f.h.GetLossDetectionTimeout())
					} else {
						require.Equal(t, fresh[:2], f.packets.Lost)
						require.Equal(t, f.start.Add(412500*time.Microsecond), f.h.GetLossDetectionTimeout())
					}
				})
			}
		}
	}
}

func TestAdaptiveLossSharedApplicationSpace(t *testing.T) {
	f := newSpuriousLossFixture(false)
	var early, later []protocol.PacketNumber
	for range 3 {
		early = append(early, f.send(0, protocol.Encryption0RTT))
	}
	for range 3 {
		later = append(later, f.send(500*time.Millisecond, protocol.Encryption1RTT))
	}
	f.ack(t, 1500*time.Millisecond, later[2])
	f.ack(t, 1562500*time.Microsecond, early...)
	require.Equal(t, f.start.Add(1750*time.Millisecond), f.h.GetLossDetectionTimeout())
}

func TestAdaptiveLossTimingBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name                              string
		rtt, received, ackDelay, deadline time.Duration
	}{
		{"equality", time.Second, 1125 * time.Millisecond, 0, 1125 * time.Millisecond},
		{"strict excess", time.Second, 1125*time.Millisecond + time.Nanosecond, 0, 1250 * time.Millisecond},
		{"subtract delay", time.Second, 1150 * time.Millisecond, 25 * time.Millisecond, 1125 * time.Millisecond},
		{"clamp delay", time.Second, 1150*time.Millisecond + time.Nanosecond, time.Second, 1250 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSpuriousLossFixture(false)
			f.h.rttStats.SetMaxAckDelay(25 * time.Millisecond)
			var pns []protocol.PacketNumber
			for range 5 {
				pns = append(pns, f.send(0, protocol.Encryption1RTT))
			}
			f.ack(t, tc.rtt, pns[4])
			_, err := f.h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pns[0], pns[1]), DelayTime: tc.ackDelay}, protocol.Encryption1RTT, f.start.Add(tc.received))
			require.NoError(t, err)
			require.Equal(t, f.start.Add(tc.deadline), f.h.GetLossDetectionTimeout())
		})
	}
	for _, tc := range []struct{ proof, deadline time.Duration }{
		{time.Millisecond, 3125 * time.Microsecond},
		{time.Millisecond + time.Nanosecond, 3250 * time.Microsecond},
	} {
		t.Run("granularity/"+tc.proof.String(), func(t *testing.T) {
			f := newSpuriousLossFixture(false)
			var pns []protocol.PacketNumber
			for range 5 {
				pns = append(pns, f.send(0, protocol.Encryption1RTT))
			}
			f.ack(t, time.Microsecond, pns[4])
			f.ack(t, tc.proof, pns...)
			var fresh []protocol.PacketNumber
			for range 5 {
				fresh = append(fresh, f.send(2*time.Millisecond, protocol.Encryption1RTT))
			}
			f.ack(t, 3*time.Millisecond, fresh[4])
			require.Equal(t, f.start.Add(tc.deadline), f.h.GetLossDetectionTimeout())
		})
	}
}

func TestAdaptiveLossUsesCurrentRTT(t *testing.T) {
	f := newSpuriousLossFixture(false)
	pns := f.loseFirstTwo(t)
	var fresh []protocol.PacketNumber
	for range 5 {
		fresh = append(fresh, f.send(110*time.Millisecond, protocol.Encryption1RTT))
	}
	f.ack(t, 1110*time.Millisecond, append(slices.Clone(pns), fresh[4])...)
	require.Equal(t, time.Second, f.h.rttStats.LatestRTT())
	require.Equal(t, f.start.Add(1235*time.Millisecond), f.h.GetLossDetectionTimeout(), "current RTT makes old reordering insufficient for growth")
	require.Equal(t, pns[:2], f.packets.Lost)
}

func TestAdaptiveLossUnavailableEvidence(t *testing.T) {
	for _, kind := range []string{"expired", "evicted", "unsent ACK", "skipped ACK"} {
		t.Run(kind, func(t *testing.T) {
			f := newSpuriousLossFixture(false)
			var pns []protocol.PacketNumber
			count := 5
			if kind == "evicted" {
				count = 80
			}
			for range count {
				pns = append(pns, f.send(0, protocol.Encryption1RTT))
			}
			f.ack(t, 100*time.Millisecond, pns[len(pns)-1])
			proofAt := 120 * time.Millisecond
			if kind == "expired" {
				proofAt = 3*f.h.rttStats.PTO(false) + time.Nanosecond
			}
			if kind == "unsent ACK" || kind == "skipped ACK" {
				invalid := pns[len(pns)-1] + 1
				if kind == "skipped ACK" {
					previous := pns[len(pns)-1]
					for {
						pn := f.send(110*time.Millisecond, protocol.Encryption1RTT)
						if pn > previous+1 {
							invalid = pn - 1
							break
						}
						require.Less(t, pn, protocol.PacketNumber(1e6))
						previous = pn
					}
				}
				_, err := f.h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pns[0], invalid)}, protocol.Encryption1RTT, f.start.Add(proofAt))
				require.Error(t, err)
			} else {
				f.ack(t, proofAt, pns[0])
			}
			var fresh []protocol.PacketNumber
			for range 5 {
				fresh = append(fresh, f.send(proofAt+10*time.Millisecond, protocol.Encryption1RTT))
			}
			f.ack(t, proofAt+110*time.Millisecond, fresh[4])
			require.Contains(t, f.packets.Lost, fresh[0], "unavailable or rejected evidence cannot disable packet-distance loss")
			require.Contains(t, f.packets.Lost, fresh[1])
			require.Equal(t, f.start.Add(proofAt+122500*time.Microsecond), f.h.GetLossDetectionTimeout(), "unavailable evidence cannot grow the time threshold")
		})
	}
}

type adaptiveFeedbackRecorder struct {
	sink congestionEventSink
	congestionRecorder
}

func (r *adaptiveFeedbackRecorder) Sent(e congestion.SendEvent) {
	if r.sink != nil {
		r.sink.Sent(e)
	}
	r.congestionRecorder.Sent(e)
}

func (r *adaptiveFeedbackRecorder) Feedback(e congestion.FeedbackEvent) {
	if r.sink != nil {
		r.sink.Feedback(e)
	}
	r.congestionRecorder.Feedback(e)
}

func TestAdaptiveLossBeforeBothClassifiers(t *testing.T) {
	for _, controller := range []string{"Reno", "BBR"} {
		for _, lateOnly := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/late_only=%v", controller, lateOnly), func(t *testing.T) {
				f := newSpuriousLossFixture(false)
				r := &adaptiveFeedbackRecorder{}
				if controller == "BBR" {
					EnableBBR(f.h, 1200, func() protocol.ByteCount { return 0 })
					r.sink = f.h.congestionEvents.sink
					f.h.congestionEvents.sink = r
				} else {
					EnableDeliverySampling(f.h, r, func() protocol.ByteCount { return 0 })
				}
				baseline := f.send(0, protocol.EncryptionInitial)
				acknowledgeRecoveryPacket(t, f.h, protocol.EncryptionInitial, f.start.Add(time.Second), baseline)
				f.send(2*time.Second, protocol.Encryption1RTT)
				f.send(12*time.Second, protocol.Encryption1RTT)
				for range 2 {
					require.True(t, f.h.QueueProbePacketAt(protocol.Encryption1RTT, f.start.Add(12*time.Second)))
				}
				for _, e := range r.feedback {
					require.Zero(t, e.PersistentCongestion.EndOrdinal, "PTO alone cannot establish loss")
				}
				var seed []protocol.PacketNumber
				for range 5 {
					seed = append(seed, f.send(12010*time.Millisecond, protocol.Encryption1RTT))
				}
				seedAck := 12020 * time.Millisecond
				witness := seed[4]
				if lateOnly {
					seedAck = 13010 * time.Millisecond
				}
				f.ack(t, seedAck, seed[4])
				if !lateOnly {
					witness = f.send(12200*time.Millisecond, protocol.Encryption1RTT)
				}
				f.ack(t, 13200*time.Millisecond, seed[0], witness)
				e := r.feedback[len(r.feedback)-1]
				require.Equal(t, !lateOnly, e.RTTUpdated)
				require.Equal(t, time.Second, f.h.rttStats.LatestRTT())
				require.Empty(t, e.Lost, "ordinary originals are younger than the adapted delay")
				require.Zero(t, e.PersistentCongestion.EndOrdinal, "PTO endpoints must not be classified using the old delay")
				require.Equal(t, f.start.Add(13260*time.Millisecond), f.h.GetLossDetectionTimeout())
				// At 1.25 RTT, the last PTO-retired endpoint qualifies. A duplicate
				// receipt supplies a witness without a new RTT or ordinary loss.
				f.ack(t, 13250*time.Millisecond, witness)
				e = r.feedback[len(r.feedback)-1]
				require.Equal(t, uint64(2), e.PersistentCongestion.StartOrdinal)
				require.Equal(t, uint64(3), e.PersistentCongestion.EndOrdinal)
				require.Empty(t, e.Lost)
				require.False(t, e.RTTUpdated)
				count := len(r.feedback)
				f.ack(t, 13251*time.Millisecond, witness)
				require.Len(t, r.feedback, count, "persistent restart is deduplicated")
			})
		}
	}
}

func TestAdaptiveLossLateOnlyGenuineLoss(t *testing.T) {
	for _, controller := range []string{"Reno", "BBR"} {
		t.Run(controller, func(t *testing.T) {
			f := newSpuriousLossFixture(false)
			r := &adaptiveFeedbackRecorder{}
			if controller == "BBR" {
				EnableBBR(f.h, 1200, func() protocol.ByteCount { return 0 })
				r.sink = f.h.congestionEvents.sink
				f.h.congestionEvents.sink = r
			} else {
				EnableDeliverySampling(f.h, r, func() protocol.ByteCount { return 0 })
			}
			var first, later []protocol.PacketNumber
			for range 3 {
				first = append(first, f.send(0, protocol.Encryption1RTT))
			}
			for range 3 {
				later = append(later, f.send(500*time.Millisecond, protocol.Encryption1RTT))
			}
			f.ack(t, 1500*time.Millisecond, later[2])
			rtt := f.h.rttStats.LatestRTT()
			f.ack(t, 2*time.Second, first...)
			e := r.feedback[len(r.feedback)-1]
			require.False(t, e.RTTUpdated)
			require.Equal(t, rtt, f.h.rttStats.LatestRTT())
			require.Len(t, e.Lost, 2, "late-only adaptation must still emit genuine-loss feedback")
			require.Equal(t, append(slices.Clone(first), later[:2]...), f.packets.Lost)
			require.Zero(t, f.h.bytesInFlight)
			if controller == "Reno" {
				require.EqualValues(t, 26880, f.h.congestion.GetCongestionWindow(), "genuine losses prevent old receipts from undoing the cutback")
			}
			count := len(r.feedback)
			f.ack(t, 2001*time.Millisecond, first...)
			require.Len(t, r.feedback, count)
			require.Len(t, f.packets.Lost, 5)
		})
	}
}

func TestAdaptiveLossLateOnlyDeadline(t *testing.T) {
	for _, controller := range []string{"Reno", "BBR"} {
		t.Run(controller, func(t *testing.T) {
			f := newSpuriousLossFixture(false)
			if controller == "BBR" {
				EnableBBR(f.h, 1200, func() protocol.ByteCount { return 0 })
			}
			var first, later []protocol.PacketNumber
			for range 3 {
				first = append(first, f.send(0, protocol.Encryption1RTT))
			}
			for range 3 {
				later = append(later, f.send(500*time.Millisecond, protocol.Encryption1RTT))
			}
			f.ack(t, 1500*time.Millisecond, later[2])
			require.Equal(t, first, f.packets.Lost)
			oldDeadline := f.start.Add(1625 * time.Millisecond)
			require.Equal(t, oldDeadline, f.h.GetLossDetectionTimeout())
			rtt, latest := f.h.rttStats.SmoothedRTT(), f.h.rttStats.LatestRTT()
			f.ack(t, 1562500*time.Microsecond, first[0], first[1])
			deadline := f.start.Add(1750 * time.Millisecond)
			require.Equal(t, deadline, f.h.GetLossDetectionTimeout(), "several receipts increase the threshold only once")
			f.ack(t, 1563*time.Millisecond, first[2])
			f.ack(t, 1564*time.Millisecond, first...)
			require.Equal(t, deadline, f.h.GetLossDetectionTimeout(), "old receipts and duplicates cannot increase it again")
			require.Equal(t, rtt, f.h.rttStats.SmoothedRTT())
			require.Equal(t, latest, f.h.rttStats.LatestRTT())
			_, err := f.h.OnLossDetectionTimeout(oldDeadline)
			require.NoError(t, err)
			require.Equal(t, first, f.packets.Lost)
			_, err = f.h.OnLossDetectionTimeout(deadline)
			require.NoError(t, err)
			require.Equal(t, append(slices.Clone(first), later[:2]...), f.packets.Lost)
			require.Zero(t, f.h.bytesInFlight)
		})
	}
}
