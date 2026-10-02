package congestion

import (
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
)

// Focused oracles for the four draft-06 conformance items of the BBRv3
// correction choice. Each oracle failed on the C1-C3 service layer before its
// conformance change; each control passes on both.

// startupLossExit drives a qualifying Startup-loss exit: six discontiguous
// losses in one recovery episode, then a loss-round boundary ACK with exitRTT.
func startupLossExit(t *testing.T, rate uint64, rtt, exitRTT time.Duration) *BBRSender {
	t.Helper()
	b := NewBBRSender(1200)
	feedbackRoundRTT(b, 1, 1200, rate, SendApplicationLimited, 60000, rtt)
	var lost []PacketInfo
	for i := range 6 {
		lost = append(lost, PacketInfo{Space: protocol.Encryption1RTT, PacketNumber: protocol.PacketNumber(2 * i), Ordinal: uint64(i + 2), Length: 1200, AckEliciting: true, RegistrationValid: true, Delivery: DeliverySnapshot{Valid: true, PostInFlight: 60000}})
	}
	b.Feedback(FeedbackEvent{Time: monotime.Time(2100 * time.Millisecond), Lost: lost, PriorInFlight: 60000, PostInFlight: 50000, Delivery: DeliverySample{Delivered: 1200, Lost: 7200}, RecoveryEpisode: RecoveryEpisode{ID: 1, Entered: true, Active: true, UndoPossible: true, Boundary: 7}})
	require.True(t, b.InSlowStart())
	feedbackRoundRTT(b, 20, 2400, rate, SendApplicationLimited, 20000, exitRTT)
	require.False(t, b.InSlowStart(), "qualifying Startup loss")
	return b
}

func spuriousUndo(b *BBRSender, now monotime.Time, all bool) {
	b.Feedback(FeedbackEvent{Time: now, RecoveryEpisode: RecoveryEpisode{ID: 1, Exited: true, UndoEligible: all}})
}

// Item 1: HandleSpuriousLossDetection returns a loss-driven Startup or Up exit
// to its probing state, not only its numeric bounds.
func TestBBRUndoRestoresLossDrivenPhase(t *testing.T) {
	startupRate := bbrScale(100000, 2772588722, 1000000000)
	for _, all := range []bool{true, false} {
		name := map[bool]string{true: "all-spurious", false: "mixed control"}[all]
		t.Run("Startup "+name, func(t *testing.T) {
			b := startupLossExit(t, 100000, 100*time.Millisecond, 100*time.Millisecond)
			require.Equal(t, bbrDrain, b.phase)
			spuriousUndo(b, monotime.Time(3950*time.Millisecond), all)
			if all {
				require.Equal(t, bbrStartup, b.phase, "undo returns to Startup")
				require.Equal(t, startupRate, b.PacingRate(), "Startup gain is the next allowance")
				require.Zero(t, b.plateau)
				feedbackRound(b, 21, 3600, 100000, SendApplicationLimited, 20000)
				require.True(t, b.InSlowStart(), "a restored Startup continues probing")
			} else {
				require.Equal(t, bbrDrain, b.phase)
				require.EqualValues(t, 50000, b.PacingRate())
			}
		})
		t.Run("Up "+name, func(t *testing.T) {
			x := newBBRProbeTrace()
			x.up(t)
			x.b.Feedback(FeedbackEvent{Time: x.now, RecoveryEpisode: RecoveryEpisode{ID: 1, Boundary: x.ordinal, Entered: true, Active: true, UndoPossible: true}})
			x.lose(20000, SendUnknown)
			require.Equal(t, bbrDown, x.b.phase, "loss ends the bandwidth probe")
			spuriousUndo(x.b, x.now.Add(time.Millisecond), all)
			if all {
				require.Equal(t, bbrRefill, x.b.phase, "undo restarts the probe at Refill")
				require.EqualValues(t, 100000, x.b.PacingRate())
				x.ack(100000, 9000, SendUnknown)
				require.Equal(t, bbrUp, x.b.phase, "Refill lasts one packet-timed round")
				require.EqualValues(t, 125000, x.b.PacingRate())
			} else {
				require.Equal(t, bbrDown, x.b.phase)
				require.EqualValues(t, 90000, x.b.PacingRate())
			}
		})
	}

	t.Run("control: plateau exit is not loss-driven", func(t *testing.T) {
		b := NewBBRSender(1200)
		b.Feedback(FeedbackEvent{Time: monotime.Time(1900 * time.Millisecond), RecoveryEpisode: RecoveryEpisode{ID: 1, Entered: true, Active: true, UndoPossible: true}})
		for i := uint64(1); i <= 4; i++ {
			feedbackRound(b, i, i*1200, 100000, SendUnknown, 20000)
		}
		require.Equal(t, bbrDrain, b.phase)
		spuriousUndo(b, monotime.Time(2500*time.Millisecond), true)
		require.Equal(t, bbrDrain, b.phase)
	})

	t.Run("control: Cruise undo keeps Cruise", func(t *testing.T) {
		x := newBBRProbeTrace()
		x.cruise()
		x.b.Feedback(FeedbackEvent{Time: x.now, RecoveryEpisode: RecoveryEpisode{ID: 1, Boundary: x.ordinal, Entered: true, Active: true, UndoPossible: true}})
		x.lose(20000, SendUnknown)
		x.ack(20000, 9000, SendUnknown)
		spuriousUndo(x.b, x.now.Add(time.Millisecond), true)
		require.Equal(t, bbrCruise, x.b.phase)
	})

	for _, all := range []bool{true, false} {
		t.Run(map[bool]string{true: "ProbeRTT keeps measuring, then returns to Startup", false: "control: mixed ProbeRTT exit returns to Cruise"}[all], func(t *testing.T) {
			b := startupLossExit(t, 100000, 100*time.Millisecond, 100*time.Millisecond)
			feedbackRound(b, 52, 3600, 100000, SendUnknown, 0)
			require.True(t, b.InProbeRTT())
			spuriousUndo(b, monotime.Time(7150*time.Millisecond), all)
			require.True(t, b.InProbeRTT(), "undo does not complete a measurement")
			feedbackRound(b, 55, 4800, 100000, SendUnknown, 0)
			require.False(t, b.InProbeRTT())
			require.Equal(t, all, b.InSlowStart())
		})
	}

	t.Run("control: Up undo during ProbeRTT keeps measuring, then Cruise", func(t *testing.T) {
		x := newBBRProbeTrace()
		x.up(t)
		x.b.Feedback(FeedbackEvent{Time: x.now, RecoveryEpisode: RecoveryEpisode{ID: 1, Boundary: x.ordinal, Entered: true, Active: true, UndoPossible: true}})
		x.lose(20000, SendUnknown)
		probeRTTAck(x, 6*time.Second, 100*time.Millisecond, 100000, 0)
		require.True(t, x.b.InProbeRTT())
		spuriousUndo(x.b, x.now.Add(time.Millisecond), true)
		require.True(t, x.b.InProbeRTT(), "the draft restarts no probe from ProbeRTT")
		probeRTTAck(x, 201*time.Millisecond, 100*time.Millisecond, 100000, 0)
		require.Equal(t, bbrCruise, x.b.phase)
	})

	t.Run("control: CE cap blocks Startup restoration", func(t *testing.T) {
		b := startupLossExit(t, 100000, 100*time.Millisecond, 100*time.Millisecond)
		b.Feedback(FeedbackEvent{Time: monotime.Time(3910 * time.Millisecond), HasAck: true, PriorInFlight: 20000, PostInFlight: 20000, Delivery: DeliverySample{Delivered: 2400}, ECN: ECNResult{Eligible: true, Ordinal: b.sentOrdinal, Delta: ECNCounts{CE: 1}}})
		require.True(t, b.ce.active)
		spuriousUndo(b, monotime.Time(3950*time.Millisecond), true)
		require.Equal(t, bbrDrain, b.phase, "a CE response owns the Startup exit")
	})
	t.Run("control: CE cap blocks Refill restoration", func(t *testing.T) {
		x := newBBRProbeTrace()
		x.up(t)
		x.b.Feedback(FeedbackEvent{Time: x.now, RecoveryEpisode: RecoveryEpisode{ID: 1, Boundary: x.ordinal, Entered: true, Active: true, UndoPossible: true}})
		x.lose(20000, SendUnknown)
		x.b.Feedback(ceEvent(x, x.ordinal, 1))
		spuriousUndo(x.b, x.now.Add(time.Millisecond), true)
		require.Equal(t, bbrDown, x.b.phase, "no new Refill while CE caps are active")
	})
}

// Item 2: CheckStartupDone, CheckDrainDone and UpdateProbeBWCyclePhase precede
// UpdateMinRTT; UpdateMinRTT then CheckProbeRTT keep their order and saved cap.
func TestBBRPhaseDecisionsUsePreUpdateMinRTT(t *testing.T) {
	for _, rtt := range []time.Duration{50 * time.Millisecond, 100 * time.Millisecond} {
		control := rtt == 100*time.Millisecond
		name := map[bool]string{false: "lower RTT", true: "control: unchanged RTT"}[control]
		t.Run("Drain "+name, func(t *testing.T) {
			x := newBBRProbeTrace()
			for range 4 {
				x.ack(100000, 20000, SendUnknown)
			}
			require.Equal(t, bbrDrain, x.b.phase)
			probeRTTAck(x, 100*time.Millisecond, rtt, 100000, 7500)
			require.Equal(t, bbrCruise, x.b.phase, "the old 10000-byte target admits 7500 bytes")
			require.EqualValues(t, 100000, x.b.PacingRate())
			require.Equal(t, rtt, x.b.minimumRTT, "the same ACK still updates the minimum")
		})
		t.Run("Drain rejected rate "+name, func(t *testing.T) {
			x := newBBRProbeTrace()
			for range 4 {
				x.ack(100000, 20000, SendUnknown)
			}
			e := probeRTTEvent(x, 100*time.Millisecond, rtt, 100000, 7500)
			e.Delivery.Valid, e.Delivery.BytesPerSecond = false, 0
			x.b.Feedback(e)
			require.Equal(t, bbrCruise, x.b.phase, "the unsampled path decides on the old minimum too")
			require.Equal(t, rtt, x.b.minimumRTT)
		})
		t.Run("ProbeBW Down "+name, func(t *testing.T) {
			x := newBBRProbeTrace()
			x.up(t)
			for range 3 {
				x.ack(100000, 9000, SendUnknown)
			}
			require.Equal(t, bbrDown, x.b.phase)
			probeRTTAck(x, 100*time.Millisecond, rtt, 100000, 7500)
			require.Equal(t, bbrCruise, x.b.phase, "the old max-bandwidth BDP admits 7500 bytes")
		})
	}

	for _, exitRTT := range []time.Duration{50 * time.Millisecond, 100 * time.Millisecond} {
		t.Run(map[bool]string{true: "Startup loss lower RTT", false: "control: Startup loss unchanged RTT"}[exitRTT < 100*time.Millisecond], func(t *testing.T) {
			b := startupLossExit(t, 1000000, 100*time.Millisecond, exitRTT)
			require.EqualValues(t, 100000, b.inflightLong, "learned before this ACK's minimum update")
			require.Equal(t, exitRTT, b.minimumRTT)
		})
	}

	t.Run("control: a stale-delivered ACK still updates the minimum", func(t *testing.T) {
		x := newBBRProbeTrace()
		x.cruise()
		e := probeRTTEvent(x, 100*time.Millisecond, 50*time.Millisecond, 100000, 9000)
		e.Delivery.Delivered = 0
		x.b.Feedback(e)
		require.Equal(t, 50*time.Millisecond, x.b.minimumRTT)
	})

	t.Run("Drain expired increasing minimum", func(t *testing.T) {
		x := newBBRProbeTrace()
		for range 4 {
			x.ack(100000, 20000, SendUnknown)
		}
		require.Equal(t, bbrDrain, x.b.phase)
		// An idle restart suppresses ProbeRTT entry while the five-second filter
		// takes 200ms; the ten-second minimum still holds 100ms.
		x.b.BeforeSend(x.now.Add(4900*time.Millisecond), true)
		probeRTTAck(x, 4900*time.Millisecond, 200*time.Millisecond, 100000, 30000)
		require.Equal(t, bbrDrain, x.b.phase)
		require.Equal(t, 100*time.Millisecond, x.b.minimumRTT)
		require.Equal(t, 200*time.Millisecond, x.b.probeRTTMinimum)
		probeRTTAck(x, 4900*time.Millisecond, 200*time.Millisecond, 100000, 15000)
		require.Equal(t, 200*time.Millisecond, x.b.minimumRTT, "the expired minimum is replaced on this ACK")
		require.False(t, x.b.InProbeRTT())
		require.Equal(t, bbrDrain, x.b.phase, "15000 bytes exceeds the old 10000-byte target")
	})

	for _, tc := range []struct {
		name     string
		elapsed  time.Duration
		rtt      time.Duration
		rejected bool
	}{
		{"lower RTT", 6 * time.Second, 50 * time.Millisecond, false},
		{"expired higher minimum", 11 * time.Second, time.Second, false},
		{"rejected rate", 6 * time.Second, 50 * time.Millisecond, true},
	} {
		t.Run("control: same-ACK ProbeRTT entry, "+tc.name, func(t *testing.T) {
			x := newBBRProbeTrace()
			x.cruise()
			e := probeRTTEvent(x, tc.elapsed, tc.rtt, 100000, 9000)
			if tc.rejected {
				e.Delivery.Valid, e.Delivery.BytesPerSecond = false, 0
			}
			x.b.Feedback(e)
			require.True(t, x.b.InProbeRTT(), "the expiry result reaches the ProbeRTT check")
			require.Equal(t, tc.rtt, x.b.minimumRTT)
			require.EqualValues(t, max(4800, min(5000, x.b.probeRTTTarget())), x.b.probeRTTCap, "saved pre-update cap, lowered only by a smaller new target")
		})
	}
}

// Item 3: UpdateRound precedes the positive-rate test. Rate rejection does not
// withhold an otherwise valid delivered-at-send round boundary.
func TestBBRPacketRoundsIgnoreRateValidity(t *testing.T) {
	type variant struct {
		name    string
		advance bool
		mutate  func(*FeedbackEvent)
	}
	variants := []variant{
		{"rejected rate", true, func(e *FeedbackEvent) { e.Delivery.Valid, e.Delivery.BytesPerSecond = false, 0 }},
		{"rejected interval", true, func(e *FeedbackEvent) { e.Delivery.Interval, e.Delivery.BytesPerSecond = 0, 0 }},
		{"control: valid rate", true, func(*FeedbackEvent) {}},
		{"control: missing history", false, func(e *FeedbackEvent) {
			e.Delivery.Valid, e.Delivery.BytesPerSecond = false, 0
			e.Acked[0].Delivery.Valid = false
		}},
		{"control: absent anchor", false, func(e *FeedbackEvent) {
			e.Delivery.Valid, e.Delivery.BytesPerSecond = false, 0
			e.Acked = nil
		}},
		{"control: invalid clock", false, func(e *FeedbackEvent) { e.Time = 0 }},
	}
	for _, v := range variants {
		t.Run("Drain "+v.name, func(t *testing.T) {
			x := newBBRProbeTrace()
			for range 4 {
				x.ack(100000, 20000, SendUnknown)
			}
			require.Equal(t, bbrDrain, x.b.phase)
			round := x.b.round
			for range 4 {
				e := probeRTTEvent(x, 100*time.Millisecond, 100*time.Millisecond, 100000, 20000)
				v.mutate(&e)
				x.b.Feedback(e)
			}
			if v.advance {
				require.Equal(t, round+4, x.b.round)
				require.Equal(t, bbrDown, x.b.phase, "four completed Drain rounds")
			} else {
				require.Equal(t, round, x.b.round, "no invented round evidence")
				require.Equal(t, bbrDrain, x.b.phase)
			}
		})
		t.Run("Refill "+v.name, func(t *testing.T) {
			x := newBBRProbeTrace()
			x.cruise()
			for range 12 {
				if x.b.phase == bbrRefill {
					break
				}
				x.ack(100000, 9000, SendUnknown)
			}
			require.Equal(t, bbrRefill, x.b.phase)
			round := x.b.round
			e := probeRTTEvent(x, 100*time.Millisecond, 100*time.Millisecond, 100000, 9000)
			v.mutate(&e)
			x.b.Feedback(e)
			if v.advance {
				require.Equal(t, round+1, x.b.round)
				require.Equal(t, bbrUp, x.b.phase, "Refill ends at the round boundary")
				require.Equal(t, bbrAcksStarting, x.b.ackPhase)
				require.Equal(t, e.Delivery.BytesPerSecond, x.b.fullBandwidth, "a rejected rate seeds no plateau baseline")
			} else {
				require.Equal(t, round, x.b.round)
				require.Equal(t, bbrRefill, x.b.phase)
			}
		})
	}

	for _, valid := range []bool{true, false} {
		t.Run(map[bool]string{true: "control: valid Up round raises the slope and grows the bound", false: "rejected Up round raises the slope, not the bound"}[valid], func(t *testing.T) {
			x := newBBRProbeTrace()
			x.up(t)
			x.b.inflightLong = x.b.window
			bound, rounds, acked := x.b.inflightLong, x.b.probeUpRounds, x.b.probeUpAcked
			e := probeRTTEvent(x, 100*time.Millisecond, 100*time.Millisecond, 100000, x.b.window)
			e.Acked[0].Delivery.PostInFlight = bound
			if !valid {
				e.Delivery.Valid, e.Delivery.BytesPerSecond = false, 0
			}
			x.b.Feedback(e)
			require.Equal(t, bbrUp, x.b.phase)
			require.Equal(t, rounds+1, x.b.probeUpRounds, "RaiseInflightLongtermSlope follows round_start")
			require.Equal(t, bound, x.b.inflightLong, "one ACK is below one growth increment")
			if valid {
				require.Equal(t, acked+1200, x.b.probeUpAcked, "acknowledged growth accumulates")
			} else {
				require.Equal(t, acked, x.b.probeUpAcked, "long-term bound growth stays sample-owned")
			}
		})
	}
	t.Run("rejected rate leaves the safe-flight bound raise", func(t *testing.T) {
		x := newBBRProbeTrace()
		x.up(t)
		for range 3 {
			x.ack(100000, 9000, SendUnknown)
		}
		x.b.inflightLong = 12000
		e := probeRTTEvent(x, 100*time.Millisecond, 100*time.Millisecond, 100000, 20000)
		e.Delivery.Valid, e.Delivery.BytesPerSecond = false, 0
		x.b.Feedback(e)
		require.EqualValues(t, 12000, x.b.inflightLong)
	})

	t.Run("rejected rate leaves rate-owned model state", func(t *testing.T) {
		x := newBBRProbeTrace()
		x.up(t)
		for range 3 {
			x.ack(100000, 9000, SendUnknown)
		}
		require.Equal(t, bbrDown, x.b.phase)
		require.Equal(t, bbrAcksStopping, x.b.ackPhase)
		bandwidth, cycle, volume := x.b.bandwidth, x.b.cycle, x.b.latestVolume
		e := probeRTTEvent(x, 100*time.Millisecond, 100*time.Millisecond, 1, 20000)
		e.Delivery.Valid = false
		x.b.Feedback(e)
		require.Equal(t, bandwidth, x.b.bandwidth, "no bandwidth sample")
		require.Equal(t, volume, x.b.latestVolume, "loss-round signals stay sample-owned")
		require.Equal(t, bbrAcksInit, x.b.ackPhase, "the stopping round ends at its boundary")
		require.Equal(t, cycle+1, x.b.cycle, "AdvanceMaxBwFilter follows the round")
	})
}

// Item 4: CheckStartupHighLoss learns max(BBR.bdp, inflight_latest), not the
// quantized output target.
func TestBBRStartupLossLearnsUnquantizedCapacity(t *testing.T) {
	for _, tc := range []struct {
		name              string
		rate              uint64
		rtt               time.Duration
		learned, headroom protocol.ByteCount
	}{
		// 2Q is 131072 at Startup's 277MB/s rate; BDP is 100000.
		{"two quanta exceed BDP", 100000000, time.Millisecond, 100000, 85000},
		// The learned cap differs, but the four-M output floor masks headroom.
		{"output floor masks the difference", 20000, 100 * time.Millisecond, 2000, 4800},
		{"control: high BDP", 1000000, 100 * time.Millisecond, 100000, 85000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := startupLossExit(t, tc.rate, tc.rtt, tc.rtt)
			require.Equal(t, tc.learned, b.inflightLong)
			require.Equal(t, tc.headroom, b.headroom(), "later Cruise allowance")
		})
	}

	t.Run("control: latest volume above BDP", func(t *testing.T) {
		b := NewBBRSender(1200)
		feedbackRound(b, 1, 1200, 20000, SendApplicationLimited, 60000)
		var lost []PacketInfo
		for i := range 6 {
			lost = append(lost, PacketInfo{Space: protocol.Encryption1RTT, PacketNumber: protocol.PacketNumber(2 * i), Ordinal: uint64(i + 2), Length: 1200, AckEliciting: true, RegistrationValid: true, Delivery: DeliverySnapshot{Valid: true, PostInFlight: 60000}})
		}
		b.Feedback(FeedbackEvent{Time: monotime.Time(2100 * time.Millisecond), Lost: lost, PriorInFlight: 60000, PostInFlight: 50000, Delivery: DeliverySample{Delivered: 1200, Lost: 7200}, RecoveryEpisode: RecoveryEpisode{ID: 1, Entered: true, Active: true, Boundary: 7}})
		// One ACK delivers 6000 bytes beyond its anchor's delivered-at-send.
		p := PacketInfo{Ordinal: 20, Length: 1200, AckEliciting: true, RegistrationValid: true, SendTime: monotime.Time(3800 * time.Millisecond), Delivery: DeliverySnapshot{Delivered: 1200, Valid: true}}
		b.Sent(SendEvent{Packet: p})
		b.Feedback(FeedbackEvent{Time: monotime.Time(3900 * time.Millisecond), HasAck: true, RawRTT: 100 * time.Millisecond, PostInFlight: 20000, Acked: []PacketInfo{p}, Delivery: DeliverySample{Delivered: 7200, Ordinal: 20, BytesPerSecond: 20000, Interval: 100 * time.Millisecond, Limited: SendApplicationLimited, Valid: true}})
		require.False(t, b.InSlowStart())
		require.EqualValues(t, 6000, b.inflightLong, "inflight_latest exceeds the 2000-byte BDP")
	})
}
