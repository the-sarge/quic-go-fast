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

func (b *BBRSender) checkProbeRTT(e FeedbackEvent, expired bool, oldCap protocol.ByteCount, roundStart bool) {
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
			b.nextRound = e.Delivery.Delivered
		}
	} else {
		b.probeRTTRoundDone = b.probeRTTRoundDone || roundStart
		b.finishProbeRTT(e.Time, e.Delivery.Delivered)
	}
}

// Both ACK and send-side restart use this gate. Reset/close discard state and
// never pass through successful probe completion.
func (b *BBRSender) finishProbeRTT(now monotime.Time, delivered uint64) {
	if b.phase != bbrProbeRTT || b.probeRTTHoldUntil == 0 || !b.probeRTTRoundDone || now <= b.probeRTTHoldUntil {
		return
	}
	b.probeRTTStamp = now
	b.window = max(b.window, b.probeRTTSavedWindow)
	if b.probeRTTReturnStartup {
		b.phase = bbrStartup
	} else {
		b.startProbeDown(now, delivered)
		b.phase = bbrCruise
	}
	// Restore through the bounds that currently apply before resetting the
	// short-term model for the next bandwidth cycle.
	b.boundWindow()
	b.inflightShort, b.bandwidthShort = protocol.MaxByteCount, math.MaxUint64
	b.probeRTTCap, b.probeRTTSavedWindow = 0, 0
	b.probeRTTHoldUntil, b.probeRTTRoundDone = 0, false
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
	b.aggregationStart, b.aggregationDelivered = now, 0
	if b.phase == bbrProbeRTT {
		b.finishProbeRTT(now, b.delivered)
	} else if b.phase >= bbrDown {
		b.rate = max(1, min(b.bandwidth, b.bandwidthShort))
	}
}
