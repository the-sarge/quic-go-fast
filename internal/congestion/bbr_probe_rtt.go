package congestion

import (
	"math"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

// The five-second scheduling filter and ten-second model filter are distinct.
// A saved pre-update cap prevents an expired, queued observation from making
// the very probe intended to drain that queue more permissive.
func (b *BBRSender) updateRTT(now monotime.Time, raw time.Duration) bool {
	expired := b.probeRTTMinimum > 0 && now.Sub(b.probeRTTStamp) > 5*time.Second
	if raw > 0 && (b.probeRTTMinimum == 0 || raw < b.probeRTTMinimum || expired) {
		b.probeRTTMinimum, b.probeRTTStamp = raw, now
	}
	if b.probeRTTMinimum > 0 && (b.minimumRTT == 0 || b.probeRTTMinimum < b.minimumRTT || now.Sub(b.minimumStamp) > 10*time.Second) {
		b.minimumRTT, b.minimumStamp = b.probeRTTMinimum, b.probeRTTStamp
	}
	return expired
}

func (b *BBRSender) probeRTTTarget() protocol.ByteCount {
	return max(4*b.size, b.bdpRatio(1, 2))
}

func (b *BBRSender) checkProbeRTT(e FeedbackEvent, expired bool, oldCap protocol.ByteCount) {
	if b.phase != bbrProbeRTT && expired && !b.idleRestart {
		b.probeRTTReturnStartup = b.phase == bbrStartup
		b.phase = bbrProbeRTT
		b.probeRTTSavedWindow = b.window
		b.probeRTTCap = oldCap
		b.ackPhase = bbrAcksStopping
		b.nextRound = e.Delivery.Delivered
		b.probeRTTHoldUntil, b.probeRTTRoundDone = 0, false
	}
	if e.Delivery.Delivered > b.delivered {
		b.idleRestart = false
	}
	if b.phase != bbrProbeRTT {
		return
	}
	b.boundWindow()
	if b.probeRTTHoldUntil == 0 {
		if e.PostInFlight <= b.window && e.PendingLocal >= 0 && e.PendingLocal <= b.window {
			b.probeRTTHoldUntil = e.Time.Add(200 * time.Millisecond)
			b.probeRTTOrdinal, b.probeRTTDelivered = b.sentOrdinal, e.Delivery.Delivered
			b.nextRound = e.Delivery.Delivered
		}
	} else {
		b.probeRTTRoundDone = b.probeRTTRoundDone || b.probeRTTRoundCompleted(e)
		b.finishProbeRTT(e.Time, e.Delivery.Delivered)
	}
}

// A registration after the hold boundary and its first ordinary receipt prove
// the round even if the optional rate interval/origin is unusable. The sampler
// freezes Delivered independently of that validity bit. Ordinal and time fence
// out earlier registrations, including equal-time packets before the hold.
func (b *BBRSender) probeRTTRoundCompleted(e FeedbackEvent) bool {
	if e.Delivery.Delivered <= b.probeRTTDelivered {
		return false
	}
	for _, p := range e.Acked {
		if p.RegistrationValid && p.AckEliciting && !p.MTUProbe && !p.PathProbe && p.Length > 0 &&
			p.PathGeneration == b.pathGeneration && p.SampleGeneration == b.sampleGeneration &&
			p.Ordinal > b.probeRTTOrdinal && p.Ordinal <= b.sentOrdinal &&
			p.SendTime >= b.probeRTTHoldUntil.Add(-200*time.Millisecond) &&
			p.Delivery.Delivered >= b.probeRTTDelivered && p.Delivery.Delivered < e.Delivery.Delivered {
			return true
		}
	}
	return false
}

// Both ACK and send-side restart use this gate. Reset/close discard state and
// never pass through successful probe completion.
func (b *BBRSender) finishProbeRTT(now monotime.Time, delivered uint64) {
	if b.phase != bbrProbeRTT || b.probeRTTHoldUntil == 0 || !b.probeRTTRoundDone || now <= b.probeRTTHoldUntil {
		return
	}
	b.probeRTTStamp = now
	// Exit discards the short-term model, including bounds learned while the
	// probe deliberately constrained flight. Restore through surviving caps.
	b.inflightShort, b.bandwidthShort = protocol.MaxByteCount, math.MaxUint64
	b.probeRTTCap = 0
	b.window = max(b.window, b.probeRTTSavedWindow)
	if b.probeRTTReturnStartup {
		b.phase = bbrStartup
	} else {
		b.startProbeDown(now, delivered)
		b.phase = bbrCruise
	}
	b.boundWindow()
	b.probeRTTSavedWindow = 0
	b.probeRTTHoldUntil, b.probeRTTRoundDone = 0, false
	b.probeRTTOrdinal, b.probeRTTDelivered = 0, 0
	b.rate = b.phaseRate()
}

func (b *BBRSender) InProbeRTT() bool { return !b.closed && b.phase == bbrProbeRTT }

// BeforeSend consumes recovery's genuine-idle observation before emission
// snapshots the pacing rate and window. It cannot infer idle from zero flight.
func (b *BBRSender) BeforeSend(now monotime.Time, idle bool) {
	if b.closed || !idle || now <= 0 || now < b.lastEvent {
		return
	}
	b.lastEvent = now
	b.idleRestart = true
	b.ce.roundDirty = true
	b.aggregationStart, b.aggregationDelivered = now, 0
	if b.phase == bbrProbeRTT {
		b.finishProbeRTT(now, b.delivered)
	} else if b.phase >= bbrDown {
		b.rate = b.capRate(max(1, min(b.bandwidth, b.bandwidthShort)))
	}
}
