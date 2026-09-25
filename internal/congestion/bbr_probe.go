package congestion

import (
	"math"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

type bbrAckPhase uint8

const (
	bbrAcksInit bbrAckPhase = iota
	bbrAcksRefilling
	bbrAcksStarting
	bbrAcksFeedback
	bbrAcksStopping
)

type bbrBandwidth struct{ cycle, rate uint64 }

func (b *BBRSender) updateMaxBandwidth(s DeliverySample) {
	if s.BytesPerSecond == 0 || (s.BytesPerSecond < b.bandwidth && bbrLimited(s.Limited)) {
		return
	}
	slot := &b.bandwidthFilter[b.cycle%2]
	if slot.cycle != b.cycle {
		*slot = bbrBandwidth{cycle: b.cycle}
	}
	slot.rate = max(slot.rate, s.BytesPerSecond)
	b.bandwidth = 0
	for _, sample := range b.bandwidthFilter {
		if b.cycle-sample.cycle < 2 {
			b.bandwidth = max(b.bandwidth, sample.rate)
		}
	}
}

func (b *BBRSender) startProbeDown(now monotime.Time, delivered uint64) {
	b.phase = bbrDown
	b.lossRanges = b.lossRanges[:0]
	b.lossPending, b.lossOverflow = false, false
	b.lossBytes, b.lossFlight = 0, 0
	b.latestRate, b.latestVolume = 0, 0
	b.ackPhase = bbrAcksStopping
	b.cycleStamp = now
	b.probeWait = 2*time.Second + time.Duration(b.random(1000000000))
	b.roundsSinceProbe = uint64(b.random(2))
	b.nextRound = delivered
}

func (b *BBRSender) startProbeRefill(delivered uint64) {
	b.phase = bbrRefill
	b.ackPhase = bbrAcksRefilling
	b.previousProbePrecautionary = false
	b.probeUpRounds, b.probeUpAcked = 0, 0
	b.bandwidthShort = math.MaxUint64
	b.inflightShort = protocol.MaxByteCount
	b.nextRound = delivered
}

func (b *BBRSender) updateProbeCycle(e FeedbackEvent, anchor PacketInfo, roundStart bool) {
	if roundStart {
		b.roundsSinceProbe++
		if b.ackPhase == bbrAcksStarting {
			b.ackPhase = bbrAcksFeedback
		}
		if b.ackPhase == bbrAcksStopping {
			b.ackPhase = bbrAcksInit
			b.probeSample = false
			if !bbrLimited(e.Delivery.Limited) {
				b.cycle++
			}
			if b.previousProbePrecautionary && !b.previousProbeTooHigh {
				b.startProbeRefill(e.Delivery.Delivered)
				return
			}
		}
	}
	// Only current, safe delivery evidence can raise a previously learned cap.
	sampleLost := e.Delivery.Lost >= anchor.Delivery.Lost && e.Delivery.Lost-anchor.Delivery.Lost > uint64(max(0, anchor.Delivery.PostInFlight))/50
	if !sampleLost && b.inflightLong != protocol.MaxByteCount {
		b.inflightLong = max(b.inflightLong, anchor.Delivery.PostInFlight)
		if b.phase == bbrUp {
			b.raiseProbeInflight(e, roundStart)
		}
	}
	b.updateProbePhase(e, roundStart, true)
}

// ACK time and flight can drive phase decisions without a rate sample.
// Filter aging, packet rounds and model-bound growth remain sample-owned.
func (b *BBRSender) updateProbePhase(e FeedbackEvent, roundStart, sampleValid bool) {
	switch b.phase {
	case bbrStartup, bbrDrain:
		return
	case bbrDown, bbrCruise:
		target := min(b.bdp(1), b.window)
		rounds := min(uint64(63), max(uint64(1), (uint64(target)+uint64(b.size)-1)/uint64(b.size)))
		if b.roundsSinceProbe >= rounds || e.Time.Sub(b.cycleStamp) > b.probeWait {
			b.startProbeRefill(e.Delivery.Delivered)
		} else if b.phase == bbrDown && e.PostInFlight <= b.maxBandwidthInflight() && e.PostInFlight <= b.headroom() {
			b.phase = bbrCruise
		}
	case bbrRefill:
		if roundStart {
			b.phase = bbrUp
			b.ackPhase = bbrAcksStarting
			b.probeSample = true
			b.fullBandwidth, b.plateau = e.Delivery.BytesPerSecond, 0
			b.raiseProbeSlope()
			b.nextRound = e.Delivery.Delivered
		}
	case bbrUp:
		if b.previousProbeTooHigh && e.PostInFlight >= b.inflightLong {
			b.previousProbePrecautionary = true
		} else if b.windowLimited(e.PriorInFlight) && b.window >= b.inflightLong {
			if sampleValid {
				b.fullBandwidth = e.Delivery.BytesPerSecond
			}
			b.plateau = 0
			return
		} else if b.plateau < 3 {
			return
		}
		b.previousProbeTooHigh = false
		b.startProbeDown(e.Time, e.Delivery.Delivered)
	}
}

func (b *BBRSender) headroom() protocol.ByteCount {
	if b.inflightLong == protocol.MaxByteCount {
		return protocol.MaxByteCount
	}
	return max(4*b.size, b.inflightLong-max(b.size, bbrBytes(bbrScale(uint64(b.inflightLong), 15, 100))))
}

func (b *BBRSender) probingBandwidth() bool {
	return b.phase == bbrStartup || b.phase == bbrRefill || b.phase == bbrUp
}

// The sampler reports the final cumulative count for the event. Subtract
// later losses to recover the per-packet prefix, in recovery's ordinal order.
// deliverySampler.feedback counts exactly the same-path, same-sample-generation
// entries of FeedbackEvent.Lost; older sampling generations retain recovery
// authority but cannot contribute to this sample-local prefix.
func (b *BBRSender) probeLoss(e FeedbackEvent, p PacketInfo, remaining uint64) {
	if !b.probeSample || p.SampleGeneration != e.SampleGeneration || e.SampleGeneration != b.sampleGeneration || e.Delivery.Lost < remaining {
		return
	}
	lost := e.Delivery.Lost - remaining
	if lost < p.Delivery.Lost {
		return
	}
	lost -= p.Delivery.Lost
	flight := uint64(p.Delivery.PostInFlight)
	if lost <= flight/50 || lost < uint64(p.Length) || p.Delivery.PostInFlight < p.Length {
		return
	}
	previousFlight := flight - uint64(p.Length)
	previousLost := lost - uint64(p.Length)
	// Clamp a negative prefix when the threshold was crossed before this
	// packet. Full-width arithmetic avoids wrapping large byte counters.
	prefix := uint64(0)
	if previousLost <= previousFlight/50 {
		prefix = (previousFlight - previousLost*50) / 49
	}
	b.probeSample = false
	b.previousProbeTooHigh = true
	if !bbrLimited(p.Delivery.Limited) {
		b.inflightLong = max(bbrBytes(previousFlight+prefix), bbrBytes(bbrScale(uint64(min(b.bdp(1), b.window)), 7, 10)))
	}
	if b.phase == bbrUp {
		b.startProbeDown(max(e.Time, b.lastEvent), e.Delivery.Delivered)
	}
}

func (b *BBRSender) raiseProbeSlope() {
	b.probeUpPerIncrement = max(b.window/protocol.ByteCount(uint64(1)<<b.probeUpRounds), b.size)
	b.probeUpRounds = min(b.probeUpRounds+1, 30)
}

func (b *BBRSender) raiseProbeInflight(e FeedbackEvent, roundStart bool) {
	if !b.windowLimited(e.PriorInFlight) || b.window < b.inflightLong {
		return
	}
	b.probeUpAcked = bbrBytes(uint64(b.probeUpAcked) + uint64(bbrBytes(e.Delivery.Delivered-b.delivered)))
	delta := b.probeUpAcked / b.probeUpPerIncrement
	b.probeUpAcked %= b.probeUpPerIncrement
	b.inflightLong = bbrBytes(uint64(b.inflightLong) + bbrScale(uint64(delta), uint64(b.size), 1))
	if roundStart {
		b.raiseProbeSlope()
	}
}

// Ordinary emission stops when its remaining byte allowance cannot fit M.
// Bounds need not be packet-aligned, so exact byte occupancy is not required.
func (b *BBRSender) windowLimited(flight protocol.ByteCount) bool {
	return flight > b.window-b.size
}

func (b *BBRSender) maxBandwidthInflight() protocol.ByteCount {
	target := b.initialWindow
	if b.minimumRTT > 0 {
		target = bbrBytes(bbrScale(b.bandwidth, uint64(b.minimumRTT), uint64(time.Second)))
	}
	return b.quantize(target)
}
