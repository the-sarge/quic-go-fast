package congestion

import (
	"cmp"
	"math"
	"math/bits"
	"slices"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
)

type bbrPhase uint8

const (
	bbrStartup bbrPhase = iota
	bbrDrain
	bbrDown
	bbrCruise
	bbrRefill
	bbrUp
	bbrProbeRTT
)

// BBRSender owns the private BBR model on the connection goroutine. Recovery
// owns the input facts; emission owns pacing debt and applies the 1% margin.
// No public connection selects this incomplete controller.
type BBRSender struct {
	ce bbrCE

	sentOrdinal, probeRTTOrdinal, probeRTTDelivered uint64

	idleRestart bool

	probeRTTHoldUntil monotime.Time
	probeRTTRoundDone bool

	minimumStamp, probeRTTStamp      monotime.Time
	probeRTTMinimum                  time.Duration
	probeRTTCap, probeRTTSavedWindow protocol.ByteCount
	probeRTTReturnStartup            bool

	windowUsedInRound, windowUsedLastRound           bool
	previousProbeTooHigh, previousProbePrecautionary bool
	probeUpRounds                                    uint8
	probeUpAcked, probeUpPerIncrement                protocol.ByteCount
	ackPhase                                         bbrAckPhase
	cycle                                            uint64
	bandwidthFilter                                  [2]bbrBandwidth
	probeSample                                      bool

	random           func(int32) int32
	cycleStamp       monotime.Time
	probeWait        time.Duration
	roundsSinceProbe uint64

	size, initialWindow, window                           protocol.ByteCount
	phase                                                 bbrPhase
	rate, bandwidth, fullBandwidth                        uint64
	deliveryBase, delivered, nextRound, round, drainRound uint64
	plateau                                               uint8
	minimumRTT                                            time.Duration
	lastEvent                                             monotime.Time
	lossRanges                                            []bbrLossRange
	lossBytes, lossFlight                                 protocol.ByteCount
	lossRoundDelivered                                    uint64
	lossPending, lossOverflow, recoveryStarted            bool
	recoveryDelivered                                     uint64
	inflightLong, inflightShort                           protocol.ByteCount
	bandwidthShort                                        uint64
	latestRate, latestVolume                              uint64
	aggregationStart                                      monotime.Time
	aggregationDelivered                                  uint64
	aggregation                                           [10]bbrAggregation
	pathGeneration, sampleGeneration, modelSampleFloor    uint64
	closed                                                bool
	recoveryBoundary                                      uint64
	recoveryEligible                                      bool
}

func NewBBRSender(size protocol.ByteCount) *BBRSender {
	if size <= 0 || size > protocol.MaxPacketBufferSize {
		panic("invalid BBR packet size")
	}
	w := min(10*size, max(14720, 2*size))
	return &BBRSender{random: (&utils.Rand{}).Int31n, size: size, initialWindow: w, window: w, rate: bbrScale(uint64(w)*10, 2772588722, 1000000000), inflightLong: protocol.MaxByteCount, inflightShort: protocol.MaxByteCount, bandwidthShort: math.MaxUint64}
}

// bbrScale floors a full-width product, saturating before any signed conversion.
func bbrScale(a, b, d uint64) uint64 {
	hi, lo := bits.Mul64(a, b)
	if hi >= d {
		return math.MaxUint64
	}
	q, _ := bits.Div64(hi, lo, d)
	return q
}

func bbrBytes(n uint64) protocol.ByteCount {
	return protocol.ByteCount(min(n, uint64(protocol.MaxByteCount)))
}
func (b *BBRSender) GetCongestionWindow() protocol.ByteCount { return b.window }
func (b *BBRSender) CanSend(flight protocol.ByteCount) bool  { return flight < b.window }
func (b *BBRSender) InSlowStart() bool                       { return !b.closed && b.phase == bbrStartup }
func (b *BBRSender) PacingRate() uint64                      { return b.rate }
func (b *BBRSender) Sent(e SendEvent) {
	p := e.Packet
	if b.closed || p.PathGeneration != b.pathGeneration || p.SampleGeneration < b.sampleGeneration || !p.RegistrationValid {
		return
	}
	b.sentOrdinal = max(b.sentOrdinal, p.Ordinal)
	b.advanceSampling(p.SampleGeneration, p.Delivery.Delivered)
	b.delivered = max(b.delivered, p.Delivery.Delivered)
	if b.ce.active && bbrLimited(p.Delivery.Limited) {
		b.ce.roundDirty = true
	}
	if p.AckEliciting && !p.PathProbe && !p.MTUProbe {
		b.windowUsedInRound = b.windowUsedInRound || b.windowLimited(e.PostInFlight)
	}
}

// Sampling fences retire optional measurements, not current-model recovery.
// Both registration and feedback can be the first event after such a fence.
func (b *BBRSender) advanceSampling(generation, delivered uint64) {
	if generation > b.sampleGeneration {
		b.sampleGeneration = generation
		b.ce.roundStarted, b.ce.cleanRound = false, false
		b.windowUsedInRound, b.windowUsedLastRound = false, false
		b.nextRound = delivered
	}
}

func (b *BBRSender) Feedback(e FeedbackEvent) {
	if b.closed || e.PathGeneration != b.pathGeneration || e.SampleGeneration < b.modelSampleFloor {
		return
	}
	preRate, preWindow, prePhase := b.effectiveRate(), b.window, b.phase
	defer b.finishCE(e, preRate, preWindow, prePhase, b.probeRTTHoldUntil != 0)
	if b.InProbeRTT() {
		e.Delivery.Limited = SendProbeRTTLimited
	}
	b.advanceSampling(e.SampleGeneration, e.Delivery.Delivered)
	clockValid := e.Time > 0 && e.Time >= b.lastEvent
	// Recovery facts arrive in logical processing order even when a queued ACK
	// carries an earlier receive timestamp than a timer event.
	b.noteLoss(e)
	if e.SampleGeneration != b.sampleGeneration {
		return // Current-model recovery remains authoritative; stale samples do not.
	}
	if clockValid {
		b.lastEvent = e.Time
	} else {
		e.RawRTT = 0
		e.Delivery.Valid = false
	}
	oldProbeCap := b.probeRTTTarget()
	expired := false
	if clockValid && e.HasAck {
		expired = b.updateRTT(e.Time, e.RawRTT)
	}
	s := e.Delivery
	if !e.HasAck || s.Delivered < b.delivered {
		return
	}
	if clockValid {
		b.windowUsedInRound = b.windowUsedInRound || b.windowLimited(e.PriorInFlight)
	}
	var anchor *PacketInfo
	for i := range e.Acked {
		if e.Acked[i].Ordinal == s.Ordinal {
			anchor = &e.Acked[i]
			break
		}
	}
	if anchor == nil || !anchor.Delivery.Valid || anchor.Delivery.Delivered > s.Delivered || anchor.PathGeneration != b.pathGeneration || anchor.SampleGeneration != b.sampleGeneration || anchor.MTUProbe || anchor.PathProbe || !s.Valid || s.Interval <= 0 {
		b.checkDrainDone(e)
		if clockValid && s.Delivered > b.delivered && b.phase >= bbrDown {
			b.updateProbePhase(e, false, false)
		}
		if clockValid {
			b.checkProbeRTT(e, expired, oldProbeCap)
		}
		b.applyACK(e, clockValid)
		return
	}
	roundStart := anchor.Delivery.Delivered >= b.nextRound
	if roundStart {
		// Consumers on the boundary ACK still see the just-completed round.
		// A later complete round replaces it, including a round with no usage.
		b.windowUsedLastRound, b.windowUsedInRound = b.windowUsedInRound, false
		b.round++
		b.roundsSinceProbe++
		if b.InProbeRTT() {
			b.probeSample = false
			b.ackPhase = bbrAcksInit
		}
		b.nextRound = s.Delivered
	}
	b.updateMaxBandwidth(s)
	volume := s.Delivered - anchor.Delivery.Delivered
	b.latestRate = max(b.latestRate, s.BytesPerSecond)
	b.latestVolume = max(b.latestVolume, volume)
	if b.recoveryStarted && anchor.Ordinal > b.recoveryBoundary && anchor.Delivery.Delivered >= b.recoveryDelivered {
		b.recoveryEligible = true
	}
	lossRoundStart := anchor.Delivery.Delivered >= b.lossRoundDelivered
	if lossRoundStart {
		if b.lossPending && b.phase == bbrStartup && b.recoveryEligible && !b.lossOverflow && b.discontiguousLosses() >= 6 && uint64(b.lossBytes) > uint64(b.lossFlight)/50 {
			b.inflightLong = max(b.inflight(1), bbrBytes(b.latestVolume))
			b.phase = bbrDrain
			b.drainRound = b.round
		} else if b.lossPending && !b.probingBandwidth() {
			if b.bandwidthShort == math.MaxUint64 {
				b.bandwidthShort = b.bandwidth
			}
			if b.inflightShort == protocol.MaxByteCount {
				b.inflightShort = b.window
			}
			b.bandwidthShort = max(b.latestRate, bbrScale(b.bandwidthShort, 7, 10))
			b.inflightShort = max(bbrBytes(b.latestVolume), bbrBytes(bbrScale(uint64(b.inflightShort), 7, 10)))
		}
		b.lossRanges = b.lossRanges[:0]
		b.lossBytes, b.lossFlight = 0, 0
		b.lossPending, b.lossOverflow = false, false
		if !b.recoveryStarted {
			b.recoveryEligible = false
		}
		b.lossRoundDelivered = s.Delivered
	}

	if (b.phase == bbrStartup || b.phase == bbrUp) && roundStart && !bbrLimited(s.Limited) {
		if b.fullBandwidth == 0 || bbrScale(s.BytesPerSecond, 4, 5) >= b.fullBandwidth {
			b.fullBandwidth = s.BytesPerSecond
			b.plateau = 0
		} else {
			b.plateau = min(b.plateau+1, 3)
		}
		if b.plateau >= 3 && b.phase == bbrStartup {
			b.phase = bbrDrain
			b.drainRound = b.round
		}
	}
	b.checkDrainDone(e)
	if b.phase != bbrStartup && b.phase != bbrProbeRTT {
		b.updateProbeCycle(e, *anchor, roundStart)
	}
	if lossRoundStart {
		// Seed the next loss round after a phase entry has reset its signals.
		b.latestRate, b.latestVolume = s.BytesPerSecond, volume
	}
	if clockValid {
		b.checkProbeRTT(e, expired, oldProbeCap)
	}
	b.applyACK(e, clockValid)
}

func (b *BBRSender) checkDrainDone(e FeedbackEvent) {
	if b.phase == bbrDrain && (e.PostInFlight <= b.inflight(1) || b.round > b.drainRound+3) {
		b.startProbeDown(e.Time, e.Delivery.Delivered)
	}
}

func (b *BBRSender) applyACK(e FeedbackEvent, clockValid bool) {
	s := e.Delivery
	acked := bbrBytes(s.Delivered - b.delivered)
	b.delivered = s.Delivered
	extra := b.aggregationMaximum()
	if clockValid {
		extra = b.updateAggregation(e.Time, uint64(acked))
	}
	modelTarget := b.bdp(2)
	if b.phase == bbrUp {
		modelTarget = b.bdpRatio(9, 4)
	}
	target := b.quantize(bbrBytes(uint64(modelTarget) + uint64(extra)))
	maxWindow := b.size * protocol.MaxCongestionWindowPackets
	grown := b.window + min(acked, maxWindow-b.window)
	if b.phase != bbrStartup {
		b.window = min(grown, target)
	} else if b.window < target || b.delivered-b.deliveryBase < uint64(b.initialWindow) {
		b.window = grown
	}
	b.boundWindow()

	b.rate = b.phaseRate()
}

// Apply the same current caps after ACK growth, restoration and size changes.
func (b *BBRSender) boundWindow() {
	cap := b.inflightShort
	if b.phase >= bbrDown {
		cap = min(cap, b.inflightLong)
	}
	if b.phase == bbrCruise || b.phase == bbrProbeRTT {
		cap = min(cap, b.headroom())
	}
	if b.phase == bbrProbeRTT {
		// The current packet-size floor belongs to output, not historical cap
		// evidence. A temporary floor increase must not enlarge that evidence.
		b.probeRTTCap = min(b.probeRTTCap, b.probeRTTTarget())
		cap = min(cap, b.probeRTTCap)
	}
	if b.ce.active {
		cap = min(cap, b.ce.flight)
	}
	b.window = min(b.size*protocol.MaxCongestionWindowPackets, max(4*b.size, min(b.window, cap)))
}

func (b *BBRSender) phaseRate() uint64 {
	return b.capRate(b.modelPhaseRate())
}

// Both phase output and CE release use the same unmeasured-rate fallback.
func (b *BBRSender) modelBandwidth() uint64 {
	rate := b.bandwidth
	if rate == 0 {
		rate = uint64(b.initialWindow) * 10
	}
	return min(rate, b.bandwidthShort)
}

func (b *BBRSender) modelPhaseRate() uint64 {
	modelRate := b.modelBandwidth()
	rate := bbrScale(modelRate, 2772588722, 1000000000) // floor(4*ln(2) at 1e-9 precision)
	if b.phase == bbrDrain {
		rate = modelRate / 2
	}
	switch b.phase {
	case bbrStartup, bbrDrain:
	case bbrDown:
		rate = bbrScale(modelRate, 9, 10)
	case bbrCruise, bbrRefill, bbrProbeRTT:
		rate = modelRate
	case bbrUp:
		rate = bbrScale(modelRate, 5, 4)
	}
	if b.phase == bbrStartup {
		return max(b.rate, rate)
	} else {
		return max(1, rate)
	}
}

func bbrLimited(reason SendLimitation) bool {
	return reason == SendApplicationLimited || reason == SendFlowControlLimited || reason == SendProbeRTTLimited
}

func (b *BBRSender) bdp(gain uint64) protocol.ByteCount { return b.bdpRatio(gain, 1) }

func (b *BBRSender) bdpRatio(numerator, denominator uint64) protocol.ByteCount {
	if b.minimumRTT == 0 {
		return b.initialWindow
	}
	return bbrBytes(bbrScale(bbrScale(min(b.bandwidth, b.bandwidthShort), uint64(b.minimumRTT), uint64(time.Second)), numerator, denominator))
}

func (b *BBRSender) inflight(gain uint64) protocol.ByteCount {
	return b.quantize(b.bdp(gain))
}

func (b *BBRSender) quantize(target protocol.ByteCount) protocol.ByteCount {
	target = max(2*b.quantum(), 4*b.size, target)
	if b.phase == bbrUp {
		target = bbrBytes(uint64(target) + uint64(2*b.size))
	}
	return target
}

func (b *BBRSender) quantum() protocol.ByteCount {
	rate := b.phaseRate()
	rate = max(1, rate/100*99+rate%100*99/100)
	return max(2*b.size, protocol.ByteCount(min(uint64(65536), rate/1000)))
}

type bbrLossRange struct {
	space       protocol.EncryptionLevel
	first, last protocol.PacketNumber
}

func (b *BBRSender) noteLoss(e FeedbackEvent) {
	if e.RecoveryEpisode.Exited {
		b.recoveryStarted = false
		b.recoveryEligible = true
	}
	if e.RecoveryEpisode.Entered {
		b.recoveryStarted = true
		b.recoveryDelivered = e.Delivery.Delivered
		b.recoveryBoundary = e.RecoveryEpisode.Boundary
		b.recoveryEligible = false
	}
	var remaining uint64
	for _, p := range e.Lost {
		if p.SampleGeneration == e.SampleGeneration && p.PathGeneration == e.PathGeneration {
			remaining += uint64(max(0, p.Length))
		}
	}
	for _, p := range e.Lost {
		if p.SampleGeneration == e.SampleGeneration && p.PathGeneration == e.PathGeneration {
			remaining -= uint64(max(0, p.Length))
		}

		if !p.AckEliciting || p.PathProbe || p.MTUProbe || p.Delivery.PostInFlight <= 0 || p.Length <= 0 || p.PathGeneration != e.PathGeneration || p.SampleGeneration < b.modelSampleFloor {
			continue
		}
		if !b.lossPending {
			b.lossRoundDelivered = e.Delivery.Delivered
			b.lossPending = true
		}
		b.lossBytes = bbrBytes(min(uint64(protocol.MaxByteCount), uint64(b.lossBytes)+uint64(p.Length)))
		b.lossFlight = max(b.lossFlight, p.Delivery.PostInFlight)
		b.probeLoss(e, p, remaining)
		if len(b.lossRanges) < 25000 {
			b.lossRanges = append(b.lossRanges, bbrLossRange{p.Space, p.PacketNumber, p.PacketNumber})
		} else {
			b.lossOverflow = true
		}
	}
}

// Merge after the complete round so later losses filling a gap do not create
// a false six-range signal. Overflow conservatively disables this signal.
func (b *BBRSender) discontiguousLosses() int {
	slices.SortFunc(b.lossRanges, func(a, c bbrLossRange) int {
		if n := cmp.Compare(a.space, c.space); n != 0 {
			return n
		}
		return cmp.Compare(a.first, c.first)
	})
	count := 0
	var previous bbrLossRange
	for _, r := range b.lossRanges {
		if count == 0 || r.space != previous.space || r.first > previous.last+1 {
			count++
			previous = r
		} else {
			previous.last = max(previous.last, r.last)
		}
	}
	return count
}

type bbrAggregation struct {
	round uint64
	bytes protocol.ByteCount
}

func (b *BBRSender) updateAggregation(now monotime.Time, acked uint64) protocol.ByteCount {
	expected := bbrScale(min(b.bandwidth, b.bandwidthShort), uint64(now.Sub(b.aggregationStart)), uint64(time.Second))
	if b.aggregationStart == 0 || b.aggregationDelivered <= expected {
		b.aggregationStart = now
		b.aggregationDelivered = 0
		expected = 0
	}
	b.aggregationDelivered = min(uint64(protocol.MaxByteCount), b.aggregationDelivered+min(acked, uint64(protocol.MaxByteCount)))
	extra := min(b.window, bbrBytes(b.aggregationDelivered-expected))
	slot := &b.aggregation[b.round%10]
	if slot.round != b.round {
		*slot = bbrAggregation{round: b.round}
	}
	slot.bytes = max(slot.bytes, extra)
	return b.aggregationMaximum()
}

func (b *BBRSender) aggregationMaximum() protocol.ByteCount {
	var extra protocol.ByteCount
	window := uint64(10)
	if b.phase == bbrStartup {
		window = 1
	}
	for _, v := range b.aggregation {
		if b.round-v.round < window {
			extra = max(extra, v.bytes)
		}
	}
	return extra
}

// Reset is called only by recovery's lifecycle owner, never inferred from an ACK.
func (b *BBRSender) Reset(path, sample, delivered uint64) {
	if b.closed {
		return
	}
	size, random := b.size, b.random
	*b = *NewBBRSender(size)
	b.random = random
	b.pathGeneration, b.sampleGeneration, b.modelSampleFloor = path, sample, sample
	b.delivered, b.deliveryBase, b.nextRound = delivered, delivered, delivered
}
func (b *BBRSender) Close() { *b = BBRSender{closed: true} }
func (b *BBRSender) SetMaxDatagramSize(size protocol.ByteCount) {
	if b.closed {
		return
	}
	fresh := NewBBRSender(size)
	b.size, b.initialWindow = size, fresh.initialWindow
	if b.InProbeRTT() || b.ce.active {
		b.boundWindow()
	} else {
		// Emission refreshes this value on every opportunity. Preserve ACK-owned
		// cap timing outside ProbeRTT, including a refresh of an unchanged size.
		b.window = min(size*protocol.MaxCongestionWindowPackets, max(4*size, b.window))
	}
}

// Legacy callbacks are deliberately inert. A logical rich feedback event is the
// sole model update; emission's private bounded pacer owns every pacing debit.
func (b *BBRSender) TimeUntilSend(protocol.ByteCount) monotime.Time { return 0 }
func (b *BBRSender) HasPacingBudget(monotime.Time) bool             { return !b.closed }
func (b *BBRSender) OnPacketSent(monotime.Time, protocol.ByteCount, protocol.PacketNumber, protocol.ByteCount, bool) {
}
func (b *BBRSender) MaybeExitSlowStart() {}
func (b *BBRSender) OnPacketAcked(protocol.PacketNumber, protocol.ByteCount, protocol.ByteCount, monotime.Time) {
}

func (b *BBRSender) OnCongestionEvent(protocol.PacketNumber, protocol.ByteCount, protocol.ByteCount) {
}
func (b *BBRSender) OnRetransmissionTimeout(bool) {}
func (b *BBRSender) InRecovery() bool             { return b.recoveryStarted && !b.closed }

var _ SendAlgorithmWithDebugInfos = (*BBRSender)(nil)
