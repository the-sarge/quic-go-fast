package congestion

import (
	"math"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
)

// CE bounds describe effective output, independently of bandwidth knowledge and
// loss bounds. Only the connection-owned reducer consumes validated T4 facts.
type bbrCE struct {
	failed                               bool
	failureBoundary, feedbackOrdinal     uint64
	roundStarted, roundDirty, cleanRound bool
	roundDelivered, roundOrdinal         uint64

	active         bool
	flight         protocol.ByteCount
	rate, boundary uint64
}

func (b *BBRSender) effectiveRate() uint64 {
	return max(1, b.rate/100*99+b.rate%100*99/100)
}

func (b *BBRSender) capRate(rate uint64) uint64 {
	if !b.ce.active {
		return rate
	}
	// PacingRate is before emission's 1% margin. Convert the effective CE cap
	// back with a ceiling so emission applies that margin exactly once.
	nominal := bbrScale(b.ce.rate, 100, 99)
	if b.ce.rate%99 != 0 && nominal < math.MaxUint64 {
		nominal++
	}
	return min(rate, nominal)
}

func (b *BBRSender) finishCE(e FeedbackEvent, rate uint64, window protocol.ByteCount, phase bbrPhase, probeHolding bool) {
	c := &b.ce
	if e.ECN.Eligible && e.ECN.PathGeneration == b.pathGeneration && e.ECN.Ordinal > 0 && e.ECN.Ordinal <= b.sentOrdinal && e.ECN.Delta.CE > 0 && e.ECN.Ordinal > c.boundary {
		c.active = true
		b.probeRTTReturnStartup = false
		c.boundary = b.sentOrdinal
		c.roundStarted, c.cleanRound, c.roundDirty = false, false, true
		c.flight = max(4*b.size, min(window, max(e.PriorInFlight, 4*b.size))/2)
		interval := b.ceInterval(e)
		c.rate = min(rate, max(bbrScale(uint64(b.size), uint64(time.Second), uint64(interval)), rate/2))
		if b.phase != bbrProbeRTT && (phase == bbrStartup || b.phase == bbrStartup) {
			b.phase = bbrDrain
			b.drainRound = b.round
			b.plateau = 3
		}
		if b.phase != bbrProbeRTT && (phase == bbrRefill || phase == bbrUp || b.phase == bbrRefill || b.phase == bbrUp) {
			b.startProbeDown(e.Time, e.Delivery.Delivered)
		}
	}
	b.recoverCE(e, phase)
	if c.active {
		b.boundWindow()
		// A hold started by this ACK must satisfy the final, CE-composed target.
		// An already-running hold remains a valid measurement under stricter caps.
		if !probeHolding && b.InProbeRTT() && (e.PostInFlight > b.window || e.PendingLocal < 0 || e.PendingLocal > b.window) {
			b.probeRTTHoldUntil, b.probeRTTRoundDone = 0, false
		}
		b.rate = b.phaseRate()
	}
}

// No pre-response or pre-failure round can release a cap. Loss-only events
// participate in the latch even though they cannot complete a packet round.
func (b *BBRSender) recoverCE(e FeedbackEvent, phase bbrPhase) {
	c := &b.ce
	if e.ECN.Failed && !c.failed {
		c.failed = true
		c.failureBoundary = b.sentOrdinal
		c.roundStarted, c.cleanRound = false, false
	}
	valid := e.ECN.Eligible && e.ECN.PathGeneration == b.pathGeneration && e.ECN.Ordinal > 0 && e.ECN.Ordinal <= b.sentOrdinal
	advancing := valid && e.ECN.Ordinal > c.feedbackOrdinal
	if advancing {
		c.feedbackOrdinal = e.ECN.Ordinal
	}
	if !c.active {
		return
	}
	dirty := bbrLimited(e.Delivery.Limited) || phase == bbrProbeRTT || b.phase == bbrProbeRTT || (valid && e.ECN.Delta.CE > 0)
	for _, p := range e.Lost {
		if p.AckEliciting && !p.MTUProbe && !p.PathProbe && p.Length > 0 && p.PathGeneration == b.pathGeneration && p.SampleGeneration >= b.modelSampleFloor {
			dirty = true
		}
	}
	if e.HasAck && !c.failed && !advancing {
		dirty = true
	}
	if e.SampleGeneration != b.sampleGeneration || e.Time <= 0 || e.Time < b.lastEvent {
		dirty = true
	}
	c.roundDirty = c.roundDirty || dirty
	if !e.HasAck || e.SampleGeneration != b.sampleGeneration || e.Time <= 0 || e.Time < b.lastEvent {
		return
	}
	var anchor *PacketInfo
	for i := range e.Acked {
		p := &e.Acked[i]
		if p.RegistrationValid && p.AckEliciting && !p.PathProbe && !p.MTUProbe && p.Length > 0 && p.PathGeneration == b.pathGeneration && p.SampleGeneration == b.sampleGeneration && p.Ordinal > max(c.boundary, c.failureBoundary) && p.Ordinal <= b.sentOrdinal && p.Delivery.Delivered < e.Delivery.Delivered && (!c.failed || p.ECN == protocol.ECNNon) {
			if anchor == nil || p.Ordinal > anchor.Ordinal {
				anchor = p
			}
		}
	}
	if anchor == nil {
		return
	}
	if bbrLimited(anchor.Delivery.Limited) {
		dirty = true
		c.roundDirty = true
	}
	if !c.roundStarted {
		c.roundStarted = true
		c.roundDelivered, c.roundOrdinal = e.Delivery.Delivered, b.sentOrdinal
		c.roundDirty = dirty
		return
	}
	if anchor.Ordinal <= c.roundOrdinal || anchor.Delivery.Delivered < c.roundDelivered {
		return
	}
	if c.roundDirty {
		c.cleanRound = false
	} else if !c.cleanRound {
		c.cleanRound = true
	} else {
		c.flight = bbrBytes(uint64(c.flight) + uint64(max(b.size, c.flight/16)))
		step := max(bbrScale(uint64(b.size), uint64(time.Second), uint64(b.ceInterval(e))), c.rate/16)
		c.rate += min(step, math.MaxUint64-c.rate)
		rate := min(b.bandwidth, b.bandwidthShort)
		effective := rate/100*99 + rate%100*99/100
		// Cruise's normal window target, including current loss bounds and ACK
		// aggregation, is independent of the capped current window.
		quantum := max(2*b.size, protocol.ByteCount(min(uint64(65536), effective/1000)))
		target := max(4*b.size, 2*quantum, bbrBytes(uint64(b.bdp(2))+uint64(b.aggregationMaximum())))
		target = min(target, b.inflightShort, b.inflightLong, b.headroom(), b.size*protocol.MaxCongestionWindowPackets)
		target = max(4*b.size, target)
		if c.flight >= target && c.rate >= effective {
			c.active = false
			b.startProbeDown(e.Time, e.Delivery.Delivered)
			b.phase = bbrCruise
			b.boundWindow()
			b.rate = b.phaseRate()
		}
	}
	c.roundDelivered, c.roundOrdinal = e.Delivery.Delivered, b.sentOrdinal
	c.roundDirty = dirty
}

func (b *BBRSender) ceInterval(e FeedbackEvent) time.Duration {
	interval := b.minimumRTT
	if interval == 0 {
		interval = e.SmoothedRTT
	}
	return max(interval, time.Millisecond)
}
