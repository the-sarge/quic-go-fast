package ackhandler

import (
	"errors"
	"fmt"
	"time"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/qlogwriter"
)

const (
	// Extra RTT allowance for time-based loss, in eighths of an RTT.
	defaultTimeThresholdEighths = 1
	maxTimeThresholdEighths     = 8
	// Maximum reordering in packets before packet threshold loss detection considers a packet lost.
	packetThreshold = 3
	// Before validating the client's address, the server won't send more than 3x bytes than it received.
	amplificationFactor = 3
	// We use Retry packets to derive an RTT estimate. Make sure we don't set the RTT to a super low value yet.
	minRTTAfterRetry = 5 * time.Millisecond
	// The PTO duration uses exponential backoff, but is truncated to a maximum value, as allowed by RFC 8961, section 4.4.
	maxPTODuration = 60 * time.Second
)

// Path probe packets are declared lost after this time.
const pathProbePacketLossTimeout = time.Second

// Bound peer-triggered probe state independently of ordinary packet recovery.
const maxOutstandingPathProbes = 32

type packetNumberSpace struct {
	history sentPacketHistory
	pns     packetNumberGenerator

	lossTime                   monotime.Time
	lastAckElicitingPacketTime monotime.Time

	largestAcked protocol.PacketNumber
	largestSent  protocol.PacketNumber
}

func newPacketNumberSpace(initialPN protocol.PacketNumber, isAppData bool) *packetNumberSpace {
	var pns packetNumberGenerator
	if isAppData {
		pns = newSkippingPacketNumberGenerator(initialPN, protocol.SkipPacketInitialPeriod, protocol.SkipPacketMaxPeriod)
	} else {
		pns = newSequentialPacketNumberGenerator(initialPN)
	}
	return &packetNumberSpace{
		history:      *newSentPacketHistory(isAppData),
		pns:          pns,
		largestSent:  protocol.InvalidPacketNumber,
		largestAcked: protocol.InvalidPacketNumber,
	}
}

type alarmTimer struct {
	Time            monotime.Time
	TimerType       qlog.TimerType
	EncryptionLevel protocol.EncryptionLevel
}

type sentPacketHandler struct {
	initialPackets          *packetNumberSpace
	handshakePackets        *packetNumberSpace
	appDataPackets          *packetNumberSpace
	lostPackets             lostPacketTracker // only for application-data packet number space
	packetThresholdDisabled bool              // application space, after surviving spurious-loss proof
	timeThresholdEighths    uint8             // extra application RTT allowance, in eighths
	minPNToRelaxReordering  protocol.PacketNumber
	// send time of the largest acknowledged packet, across all packet number spaces
	largestAckedTime monotime.Time

	// Do we know that the peer completed address validation yet?
	// Always true for the server.
	peerCompletedAddressValidation bool
	bytesReceived                  protocol.ByteCount
	bytesSent                      protocol.ByteCount
	// Have we validated the peer's address yet?
	// Always true for the client.
	peerAddressValidated bool

	handshakeConfirmed bool

	ignorePacketsBelow func(protocol.PacketNumber)

	ackedPackets []packetWithPacketNumber // to avoid allocations in detectAndRemoveAckedPackets

	bytesInFlight protocol.ByteCount

	congestion       congestion.SendAlgorithmWithDebugInfos
	congestionEvents *congestionDispatch
	rttStats         *utils.RTTStats
	connStats        *utils.ConnectionStats

	// The number of times a PTO has been sent without receiving an ack.
	ptoCount uint32
	ptoMode  SendMode
	// The number of PTO probe packets that should be sent.
	// Only applies to the application-data packet number space.
	numProbesToSend int

	// The alarm timeout
	alarm alarmTimer

	bbrECN     *bbrECNTracker
	enableECN  bool
	ecnTracker ecnHandler

	perspective protocol.Perspective

	qlogger     qlogwriter.Recorder
	lastMetrics qlog.MetricsUpdated
	logger      utils.Logger
}

var _ SentPacketHandler = &sentPacketHandler{}

// clientAddressValidated indicates whether the address was validated beforehand by an address validation token.
// If the address was validated, the amplification limit doesn't apply. It has no effect for a client.
func NewSentPacketHandler(
	initialPN protocol.PacketNumber,
	initialMaxDatagramSize protocol.ByteCount,
	rttStats *utils.RTTStats,
	connStats *utils.ConnectionStats,
	clientAddressValidated bool,
	enableECN bool,
	ignorePacketsBelow func(protocol.PacketNumber),
	pers protocol.Perspective,
	qlogger qlogwriter.Recorder,
	logger utils.Logger,
) SentPacketHandler {
	return newSentPacketHandler(initialPN, initialMaxDatagramSize, rttStats, connStats, clientAddressValidated, enableECN, ignorePacketsBelow, pers, qlogger, logger, nil)
}

func newSentPacketHandler(
	initialPN protocol.PacketNumber,
	initialMaxDatagramSize protocol.ByteCount,
	rttStats *utils.RTTStats,
	connStats *utils.ConnectionStats,
	clientAddressValidated bool,
	enableECN bool,
	ignorePacketsBelow func(protocol.PacketNumber),
	pers protocol.Perspective,
	qlogger qlogwriter.Recorder,
	logger utils.Logger,
	sink congestionEventSink,
) *sentPacketHandler {
	sender := congestion.NewCubicSender(
		congestion.DefaultClock{},
		rttStats,
		connStats,
		initialMaxDatagramSize,
		true, // use Reno
		qlogger,
	)

	h := &sentPacketHandler{
		peerCompletedAddressValidation: pers == protocol.PerspectiveServer,
		peerAddressValidated:           pers == protocol.PerspectiveClient || clientAddressValidated,
		initialPackets:                 newPacketNumberSpace(initialPN, false),
		handshakePackets:               newPacketNumberSpace(0, false),
		appDataPackets:                 newPacketNumberSpace(0, true),
		lostPackets:                    *newLostPacketTracker(64),
		timeThresholdEighths:           defaultTimeThresholdEighths,
		minPNToRelaxReordering:         protocol.InvalidPacketNumber,
		rttStats:                       rttStats,
		connStats:                      connStats,
		congestion:                     sender,
		ignorePacketsBelow:             ignorePacketsBelow,
		perspective:                    pers,
		qlogger:                        qlogger,
		logger:                         logger,
	}
	if sink != nil {
		h.congestionEvents = &congestionDispatch{sink: sink}
	}
	if enableECN {
		h.enableECN = true
		h.ecnTracker = newECNTracker(logger, qlogger)
	}
	return h
}

func (h *sentPacketHandler) removeFromBytesInFlight(p *packet) {
	if p.includedInBytesInFlight {
		if p.Length > h.bytesInFlight {
			panic("negative bytes_in_flight")
		}
		h.bytesInFlight -= p.Length
		p.includedInBytesInFlight = false
	}
}

func (h *sentPacketHandler) DropPackets(encLevel protocol.EncryptionLevel, now monotime.Time) {
	h.congestion.OnPacketsDiscarded()
	h.discardCongestionSpace(encLevel)
	// The server won't await address validation after the handshake is confirmed.
	// This applies even if we didn't receive an ACK for a Handshake packet.
	if h.perspective == protocol.PerspectiveClient && encLevel == protocol.EncryptionHandshake {
		h.peerCompletedAddressValidation = true
	}
	// remove outstanding packets from bytes_in_flight
	if encLevel == protocol.EncryptionInitial || encLevel == protocol.EncryptionHandshake {
		pnSpace := h.getPacketNumberSpace(encLevel)
		// We might already have dropped this packet number space.
		if pnSpace == nil {
			return
		}
		for _, p := range pnSpace.history.Packets() {
			h.removeFromBytesInFlight(p)
		}
	}
	// drop the packet history
	//nolint:exhaustive // Not every packet number space can be dropped.
	switch encLevel {
	case protocol.EncryptionInitial:
		h.initialPackets = nil
	case protocol.EncryptionHandshake:
		// Dropping the handshake packet number space means that the handshake is confirmed,
		// see section 4.9.2 of RFC 9001.
		h.handshakeConfirmed = true
		h.handshakePackets = nil
	case protocol.Encryption0RTT:
		// This function is only called when 0-RTT is rejected,
		// and not when the client drops 0-RTT keys when the handshake completes.
		// When 0-RTT is rejected, all application data sent so far becomes invalid.
		// Delete the packets from the history and remove them from bytes_in_flight.
		h.lostPackets.Reset()
		for pn, p := range h.appDataPackets.history.Packets() {
			if p.EncryptionLevel != protocol.Encryption0RTT {
				break
			}
			h.removeFromBytesInFlight(p)
			h.appDataPackets.history.Remove(pn)
		}
	default:
		panic(fmt.Sprintf("Cannot drop keys for encryption level %s", encLevel))
	}
	if h.qlogger != nil && h.ptoCount != 0 {
		h.qlogger.RecordEvent(qlog.PTOCountUpdated{PTOCount: 0})
	}
	h.ptoCount = 0
	h.numProbesToSend = 0
	h.ptoMode = SendNone
	h.setLossDetectionTimer(now)
}

func (h *sentPacketHandler) ReceivedBytes(n protocol.ByteCount, t monotime.Time) {
	h.connStats.BytesReceived.Add(uint64(n))
	wasAmplificationLimit := h.isAmplificationLimited()
	h.bytesReceived += n
	if wasAmplificationLimit && !h.isAmplificationLimited() {
		h.setLossDetectionTimer(t)
	}
}

func (h *sentPacketHandler) ReceivedPacket(l protocol.EncryptionLevel, t monotime.Time) {
	h.connStats.PacketsReceived.Add(1)
	if h.perspective == protocol.PerspectiveServer && l == protocol.EncryptionHandshake && !h.peerAddressValidated {
		h.peerAddressValidated = true
		h.setLossDetectionTimer(t)
	}
}

func (h *sentPacketHandler) packetsInFlight() int {
	packetsInFlight := h.appDataPackets.history.NumOutstanding()
	if h.handshakePackets != nil {
		packetsInFlight += h.handshakePackets.history.NumOutstanding()
	}
	if h.initialPackets != nil {
		packetsInFlight += h.initialPackets.history.NumOutstanding()
	}
	return packetsInFlight
}

func (h *sentPacketHandler) SentPacket(
	t monotime.Time,
	pn, largestAcked protocol.PacketNumber,
	streamFrames []StreamFrame,
	frames []Frame,
	encLevel protocol.EncryptionLevel,
	ecn protocol.ECN,
	size protocol.ByteCount,
	isPathMTUProbePacket bool,
	isPathProbePacket bool,
) {
	priorInFlight := h.bytesInFlight
	h.bytesSent += size
	h.connStats.BytesSent.Add(uint64(size))
	h.connStats.PacketsSent.Add(1)

	pnSpace := h.getPacketNumberSpace(encLevel)
	if h.logger.Debug() && (pnSpace.history.HasOutstandingPackets() || pnSpace.history.HasOutstandingPathProbes()) {
		for p := max(0, pnSpace.largestSent+1); p < pn; p++ {
			h.logger.Debugf("Skipping packet number %d", p)
		}
	}

	pnSpace.largestSent = pn
	if pnSpace == h.appDataPackets && h.minPNToRelaxReordering == protocol.InvalidPacketNumber {
		h.minPNToRelaxReordering = pn
	}

	p := getPacket()
	p.SendTime = t
	p.EncryptionLevel = encLevel
	p.Length = size
	p.Frames = frames
	p.LargestAcked = largestAcked
	p.StreamFrames = streamFrames
	p.IsPathMTUProbePacket = isPathMTUProbePacket
	p.isPathProbePacket = isPathProbePacket
	isAckEliciting := p.IsAckEliciting()

	if isPathProbePacket {
		h.congestion.OnPacketSent(t, h.bytesInFlight, congestion.NewPacketKey(encLevel, pn), size, isAckEliciting, true)
		h.captureCongestionSend(pn, p, ecn, priorInFlight)
		pnSpace.history.SentPathProbePacket(pn, p)
		h.setLossDetectionTimer(t)
		return
	}
	if isAckEliciting {
		pnSpace.lastAckElicitingPacketTime = t
		h.bytesInFlight += size
		p.includedInBytesInFlight = true
		if h.numProbesToSend > 0 {
			h.numProbesToSend--
		}
	}
	h.congestion.OnPacketSent(t, h.bytesInFlight, congestion.NewPacketKey(encLevel, pn), size, isAckEliciting, false)
	h.captureCongestionSend(pn, p, ecn, priorInFlight)

	if encLevel == protocol.Encryption1RTT && h.ecnTracker != nil {
		h.ecnTracker.SentPacket(pn, ecn)
	}

	pnSpace.history.SentPacket(pn, p)
	if !isAckEliciting {
		if !h.peerCompletedAddressValidation {
			h.setLossDetectionTimer(t)
		}
		return
	}
	if h.qlogger != nil {
		h.qlogMetricsUpdated()
	}
	h.setLossDetectionTimer(t)
}

func (h *sentPacketHandler) qlogMetricsUpdated() {
	var metricsUpdatedEvent qlog.MetricsUpdated
	var updated bool
	if h.rttStats.HasMeasurement() {
		if h.lastMetrics.MinRTT != h.rttStats.MinRTT() {
			metricsUpdatedEvent.MinRTT = h.rttStats.MinRTT()
			h.lastMetrics.MinRTT = metricsUpdatedEvent.MinRTT
			updated = true
		}
		if h.lastMetrics.SmoothedRTT != h.rttStats.SmoothedRTT() {
			metricsUpdatedEvent.SmoothedRTT = h.rttStats.SmoothedRTT()
			h.lastMetrics.SmoothedRTT = metricsUpdatedEvent.SmoothedRTT
			updated = true
		}
		if h.lastMetrics.LatestRTT != h.rttStats.LatestRTT() {
			metricsUpdatedEvent.LatestRTT = h.rttStats.LatestRTT()
			h.lastMetrics.LatestRTT = metricsUpdatedEvent.LatestRTT
			updated = true
		}
		if h.lastMetrics.RTTVariance != h.rttStats.MeanDeviation() {
			metricsUpdatedEvent.RTTVariance = h.rttStats.MeanDeviation()
			h.lastMetrics.RTTVariance = metricsUpdatedEvent.RTTVariance
			updated = true
		}
	}
	if cwnd := h.congestion.GetCongestionWindow(); h.lastMetrics.CongestionWindow != int(cwnd) {
		metricsUpdatedEvent.CongestionWindow = int(cwnd)
		h.lastMetrics.CongestionWindow = metricsUpdatedEvent.CongestionWindow
		updated = true
	}
	if h.lastMetrics.BytesInFlight != int(h.bytesInFlight) {
		metricsUpdatedEvent.BytesInFlight = int(h.bytesInFlight)
		h.lastMetrics.BytesInFlight = metricsUpdatedEvent.BytesInFlight
		updated = true
	}
	packetsInFlight := h.packetsInFlight()
	if h.lastMetrics.PacketsInFlight != packetsInFlight {
		metricsUpdatedEvent.PacketsInFlight = packetsInFlight
		h.lastMetrics.PacketsInFlight = metricsUpdatedEvent.PacketsInFlight
		updated = true
	}
	if updated {
		h.qlogger.RecordEvent(metricsUpdatedEvent)
	}
}

func (h *sentPacketHandler) getPacketNumberSpace(encLevel protocol.EncryptionLevel) *packetNumberSpace {
	switch encLevel {
	case protocol.EncryptionInitial:
		return h.initialPackets
	case protocol.EncryptionHandshake:
		return h.handshakePackets
	case protocol.Encryption0RTT, protocol.Encryption1RTT:
		return h.appDataPackets
	default:
		panic("invalid packet number space")
	}
}

func (h *sentPacketHandler) ReceivedAck(ack *wire.AckFrame, encLevel protocol.EncryptionLevel, rcvTime monotime.Time) (bool /* contained 1-RTT packet */, error) {
	pnSpace := h.getPacketNumberSpace(encLevel)

	largestAcked := ack.LargestAcked()
	if largestAcked > pnSpace.largestSent {
		return false, &qerr.TransportError{
			ErrorCode:    qerr.ProtocolViolation,
			ErrorMessage: "received ACK for an unsent packet",
		}
	}

	// Servers complete address validation when a protected packet is received.
	if h.perspective == protocol.PerspectiveClient && !h.peerCompletedAddressValidation &&
		(encLevel == protocol.EncryptionHandshake || encLevel == protocol.Encryption1RTT) {
		h.peerCompletedAddressValidation = true
		h.logger.Debugf("Peer doesn't await address validation any longer.")
		// Make sure that the timer is reset, even if this ACK doesn't acknowledge any (ack-eliciting) packets.
		h.setLossDetectionTimer(rcvTime)
	}

	priorInFlight := h.bytesInFlight
	ackedPackets, hasAckEliciting, err := h.detectAndRemoveAckedPackets(ack, encLevel)
	if err != nil {
		return false, err
	}
	if h.congestionEvents != nil {
		h.ExpireDelivery(rcvTime)
	}
	if len(ackedPackets) == 0 {
		// Admission above must precede both histories' retained receipt processing.
		h.beginCongestionFeedback(rcvTime, encLevel, priorInFlight, nil, largestAcked)
		witness := h.appendRetainedAck(ack, encLevel)
		var spuriousLosses []lostPacket
		var timeThresholdIncreased bool
		if encLevel == protocol.Encryption1RTT {
			spuriousLosses, timeThresholdIncreased = h.detectSpuriousLosses(ack, rcvTime)
		}
		h.confirmPTOOutcomes(encLevel, witness, rcvTime)
		if timeThresholdIncreased {
			h.detectLostPackets(rcvTime, encLevel)
			h.setLossDetectionTimer(rcvTime)
		}
		if encLevel == protocol.Encryption1RTT {
			h.finishSpuriousLosses(spuriousLosses, rcvTime)
		}
		if encLevel == protocol.Encryption1RTT && h.qlogger != nil {
			h.qlogMetricsUpdated()
		}
		if h.congestionEvents != nil {
			h.captureBBRECN(ack, encLevel)
			if len(h.congestionEvents.event.Acked) > 0 || len(h.congestionEvents.event.Lost) > 0 || priorInFlight != h.bytesInFlight || h.congestionEvents.recovery.needsAckFeedback() || (h.bbrECN != nil && encLevel == protocol.Encryption1RTT) {
				h.finishCongestionFeedback()
			}
		}
		return false, nil
	}
	h.beginCongestionFeedback(rcvTime, encLevel, priorInFlight, ackedPackets, largestAcked)
	witness := h.appendRetainedAck(ack, encLevel)
	h.captureBBRECN(ack, encLevel)
	// update the RTT, if:
	// * the largest acked is newly acknowledged, AND
	// * at least one new ack-eliciting packet was acknowledged
	if len(ackedPackets) > 0 {
		if p := ackedPackets[len(ackedPackets)-1]; p.PacketNumber == ack.LargestAcked() && !p.isPathProbePacket && hasAckEliciting {
			if h.congestionEvents != nil {
				h.congestionEvents.event.RTTEligible = true
			}
			// don't use the ack delay for Initial and Handshake packets
			var ackDelay time.Duration
			if encLevel == protocol.Encryption1RTT {
				ackDelay = min(ack.DelayTime, h.rttStats.MaxAckDelay())
			}
			if h.largestAckedTime.IsZero() || !p.SendTime.Before(h.largestAckedTime) {
				h.rttStats.UpdateRTT(rcvTime.Sub(p.SendTime), ackDelay)
				if h.congestionEvents != nil {
					h.congestionEvents.event.RTTUpdated = rcvTime.After(p.SendTime)
					h.captureDeliveryRTT(p, rcvTime)
				}
				if h.logger.Debug() {
					h.logger.Debugf("\tupdated RTT: %s (σ: %s)", h.rttStats.SmoothedRTT(), h.rttStats.MeanDeviation())
				}
				h.largestAckedTime = p.SendTime
			}
			h.congestion.MaybeExitSlowStart()
		}
	}

	// Only inform the ECN tracker about new 1-RTT ACKs if the ACK increases the largest acked.
	if encLevel == protocol.Encryption1RTT && h.ecnTracker != nil && largestAcked > pnSpace.largestAcked {
		congested := h.ecnTracker.HandleNewlyAcked(ackedPackets, int64(ack.ECT0), int64(ack.ECT1), int64(ack.ECNCE))
		if h.congestionEvents != nil {
			h.congestionEvents.event.ECNChecked = true
			h.congestionEvents.event.Congested = congested
		}
		if congested {
			h.congestion.OnCongestionEvent(congestion.NewPacketKey(encLevel, largestAcked), 0, priorInFlight)
		}
	}

	pnSpace.largestAcked = max(pnSpace.largestAcked, largestAcked)

	// Adapt before either classifier, but deliver Reno undo receipts only after
	// current-ACK CE and genuine losses have invalidated or superseded old state.
	var spuriousLosses []lostPacket
	if encLevel == protocol.Encryption1RTT {
		spuriousLosses, _ = h.detectSpuriousLosses(ack, rcvTime)
	}
	h.confirmPTOOutcomes(encLevel, witness, rcvTime)
	h.detectLostPackets(rcvTime, encLevel)
	if encLevel == protocol.Encryption1RTT {
		h.detectLostPathProbes(rcvTime)
	}
	var acked1RTTPacket bool
	for _, p := range ackedPackets {
		if p.includedInBytesInFlight {
			h.congestion.OnPacketAcked(congestion.NewPacketKey(p.EncryptionLevel, p.PacketNumber), p.Length, priorInFlight, rcvTime)
		}
		if p.EncryptionLevel == protocol.Encryption1RTT {
			acked1RTTPacket = true
		}
		h.removeFromBytesInFlight(p.packet)
		if !p.isPathProbePacket {
			putPacket(p.packet)
		}
	}

	if encLevel == protocol.Encryption1RTT {
		h.finishSpuriousLosses(spuriousLosses, rcvTime)
	}

	// After this point, we must not use ackedPackets any longer!
	// We've already returned the buffers.
	ackedPackets = nil    //nolint:ineffassign // This is just to be on the safe side.
	clear(h.ackedPackets) // make sure the memory is released
	h.ackedPackets = h.ackedPackets[:0]

	// Reset the pto_count unless the client is unsure if the server has validated the client's address.
	if h.peerCompletedAddressValidation {
		if h.qlogger != nil && h.ptoCount != 0 {
			h.qlogger.RecordEvent(qlog.PTOCountUpdated{PTOCount: 0})
		}
		h.ptoCount = 0
	}
	h.numProbesToSend = 0

	if h.qlogger != nil {
		h.qlogMetricsUpdated()
	}

	h.setLossDetectionTimer(rcvTime)
	h.finishCongestionFeedback()
	return acked1RTTPacket, nil
}

// detectSpuriousLosses consumes metadata only, after application ACK admission.
// The returned batch contains only unexpired receipts and lives for this ACK.
func (h *sentPacketHandler) detectSpuriousLosses(ack *wire.AckFrame, rcvTime monotime.Time) (_ []lostPacket, timeThresholdIncreased bool) {
	ackTime := rcvTime.Add(-min(ack.DelayTime, h.rttStats.MaxAckDelay()))
	cutoff := rcvTime.Add(-3 * h.rttStats.PTO(false))
	var maxTimeReordering time.Duration
	ackRangeIdx := len(ack.AckRanges) - 1
	var spuriousLosses []lostPacket
	for pn, sendTime := range h.lostPackets.All() {
		ackRange := ack.AckRanges[ackRangeIdx]
		for pn > ackRange.Largest && ackRangeIdx > 0 {
			ackRangeIdx--
			ackRange = ack.AckRanges[ackRangeIdx]
		}
		if pn < ackRange.Smallest {
			continue
		}
		if pn <= ackRange.Largest {
			packetReordering := h.appDataPackets.history.Difference(ack.LargestAcked(), pn)
			timeReordering := ackTime.Sub(sendTime)

			if h.qlogger != nil {
				h.qlogger.RecordEvent(qlog.SpuriousLoss{
					EncryptionLevel:  protocol.Encryption1RTT,
					PacketNumber:     pn,
					PacketReordering: uint64(packetReordering),
					TimeReordering:   timeReordering,
				})
			}
			spuriousLosses = append(spuriousLosses, lostPacket{PacketNumber: pn, SendTime: sendTime})
			// Diagnostics keep their consumption-before-cleanup order. Only
			// surviving evidence may train policy or restore congestion state.
			if !sendTime.Before(cutoff) {
				h.packetThresholdDisabled = true
				if h.minPNToRelaxReordering != protocol.InvalidPacketNumber && pn >= h.minPNToRelaxReordering {
					maxTimeReordering = max(maxTimeReordering, timeReordering)
				}
			}
		}
	}
	unexpired := spuriousLosses[:0]
	for _, p := range spuriousLosses {
		h.lostPackets.Delete(p.PacketNumber)
		if !p.SendTime.Before(cutoff) {
			unexpired = append(unexpired, p)
		}
	}
	if maxTimeReordering > h.lossDelay(protocol.Encryption1RTT) && h.timeThresholdEighths < maxTimeThresholdEighths {
		h.timeThresholdEighths = min(2*h.timeThresholdEighths, maxTimeThresholdEighths)
		// The next application registration establishes a new boundary. Merely
		// sending again cannot make an older retained loss eligible for growth.
		h.minPNToRelaxReordering = protocol.InvalidPacketNumber
		return unexpired, true
	}
	return unexpired, false
}

func (h *sentPacketHandler) finishSpuriousLosses(packets []lostPacket, now monotime.Time) {
	for _, p := range packets {
		h.congestion.OnSpuriousLoss(congestion.NewPacketKey(protocol.Encryption1RTT, p.PacketNumber))
	}
	// Include any losses declared by this ACK in the existing strict send-age
	// cleanup. Diagnostics consume matching records before cleanup on both paths.
	h.lostPackets.DeleteBefore(now.Add(-3 * h.rttStats.PTO(false)))
}

// Packets are returned in ascending packet number order.
func (h *sentPacketHandler) detectAndRemoveAckedPackets(
	ack *wire.AckFrame,
	encLevel protocol.EncryptionLevel,
) (_ []packetWithPacketNumber, hasAckEliciting bool, _ error) {
	if len(h.ackedPackets) > 0 {
		return nil, false, errors.New("ackhandler BUG: ackedPackets slice not empty")
	}

	pnSpace := h.getPacketNumberSpace(encLevel)

	if encLevel == protocol.Encryption1RTT {
		for p := range pnSpace.history.SkippedPackets() {
			if ack.AcksPacket(p) {
				return nil, false, &qerr.TransportError{
					ErrorCode:    qerr.ProtocolViolation,
					ErrorMessage: fmt.Sprintf("received an ACK for skipped packet number: %d (%s)", p, encLevel),
				}
			}
		}
	}

	var ackRangeIndex int
	lowestAcked := ack.LowestAcked()
	largestAcked := ack.LargestAcked()
	for pn, p := range pnSpace.history.Packets() {
		// ignore packets below the lowest acked
		if pn < lowestAcked {
			continue
		}
		if pn > largestAcked {
			break
		}

		if ack.HasMissingRanges() {
			ackRange := ack.AckRanges[len(ack.AckRanges)-1-ackRangeIndex]

			for pn > ackRange.Largest && ackRangeIndex < len(ack.AckRanges)-1 {
				ackRangeIndex++
				ackRange = ack.AckRanges[len(ack.AckRanges)-1-ackRangeIndex]
			}

			if pn < ackRange.Smallest { // packet not contained in ACK range
				continue
			}
			if pn > ackRange.Largest {
				return nil, false, fmt.Errorf("BUG: ackhandler would have acked wrong packet %d, while evaluating range %d -> %d", pn, ackRange.Smallest, ackRange.Largest)
			}
		}
		if p.isPathProbePacket {
			probePacket := pnSpace.history.RemovePathProbe(pn)
			// the probe packet might already have been declared lost
			if probePacket != nil {
				h.ackedPackets = append(h.ackedPackets, packetWithPacketNumber{PacketNumber: pn, packet: probePacket})
			}
			continue
		}
		if p.IsAckEliciting() {
			hasAckEliciting = true
		}
		h.ackedPackets = append(h.ackedPackets, packetWithPacketNumber{PacketNumber: pn, packet: p})
	}
	if h.logger.Debug() && len(h.ackedPackets) > 0 {
		pns := make([]protocol.PacketNumber, len(h.ackedPackets))
		for i, p := range h.ackedPackets {
			pns[i] = p.PacketNumber
		}
		h.logger.Debugf("\tnewly acked packets (%d): %d", len(pns), pns)
	}

	for _, p := range h.ackedPackets {
		if p.LargestAcked != protocol.InvalidPacketNumber && encLevel == protocol.Encryption1RTT && h.ignorePacketsBelow != nil {
			h.ignorePacketsBelow(p.LargestAcked + 1)
		}

		for _, f := range p.Frames {
			if f.Handler != nil {
				f.Handler.OnAcked(f.Frame)
			}
		}
		for _, f := range p.StreamFrames {
			if f.Handler != nil {
				f.Handler.OnAcked(f.Frame)
			}
		}
		if !p.isPathProbePacket {
			if err := pnSpace.history.Remove(p.PacketNumber); err != nil {
				return nil, false, err
			}
		}
	}
	// TODO: add support for the transport:packets_acked qlog event
	return h.ackedPackets, hasAckEliciting, nil
}

func (h *sentPacketHandler) getLossTimeAndSpace() (monotime.Time, protocol.EncryptionLevel) {
	var encLevel protocol.EncryptionLevel
	var lossTime monotime.Time

	if h.initialPackets != nil {
		lossTime = h.initialPackets.lossTime
		encLevel = protocol.EncryptionInitial
	}
	if h.handshakePackets != nil && (lossTime.IsZero() || (!h.handshakePackets.lossTime.IsZero() && h.handshakePackets.lossTime.Before(lossTime))) {
		lossTime = h.handshakePackets.lossTime
		encLevel = protocol.EncryptionHandshake
	}
	if lossTime.IsZero() || (!h.appDataPackets.lossTime.IsZero() && h.appDataPackets.lossTime.Before(lossTime)) {
		lossTime = h.appDataPackets.lossTime
		encLevel = protocol.Encryption1RTT
	}
	return lossTime, encLevel
}

func (h *sentPacketHandler) getScaledPTO(includeMaxAckDelay bool) time.Duration {
	pto := h.rttStats.PTO(includeMaxAckDelay) << h.ptoCount
	if pto > maxPTODuration || pto <= 0 {
		return maxPTODuration
	}
	return pto
}

// same logic as getLossTimeAndSpace, but for lastAckElicitingPacketTime instead of lossTime
func (h *sentPacketHandler) getPTOTimeAndSpace(now monotime.Time) (pto monotime.Time, encLevel protocol.EncryptionLevel) {
	// We only send application data probe packets once the handshake is confirmed,
	// because before that, we don't have the keys to decrypt ACKs sent in 1-RTT packets.
	if !h.handshakeConfirmed && !h.hasOutstandingCryptoPackets() {
		if h.peerCompletedAddressValidation {
			return
		}
		t := now.Add(h.getScaledPTO(false))
		if h.initialPackets != nil {
			return t, protocol.EncryptionInitial
		}
		return t, protocol.EncryptionHandshake
	}

	if h.initialPackets != nil && h.initialPackets.history.HasOutstandingPackets() &&
		!h.initialPackets.lastAckElicitingPacketTime.IsZero() {
		encLevel = protocol.EncryptionInitial
		if t := h.initialPackets.lastAckElicitingPacketTime; !t.IsZero() {
			pto = t.Add(h.getScaledPTO(false))
		}
	}
	if h.handshakePackets != nil && h.handshakePackets.history.HasOutstandingPackets() &&
		!h.handshakePackets.lastAckElicitingPacketTime.IsZero() {
		t := h.handshakePackets.lastAckElicitingPacketTime.Add(h.getScaledPTO(false))
		if pto.IsZero() || (!t.IsZero() && t.Before(pto)) {
			pto = t
			encLevel = protocol.EncryptionHandshake
		}
	}
	if h.handshakeConfirmed && h.appDataPackets.history.HasOutstandingPackets() &&
		!h.appDataPackets.lastAckElicitingPacketTime.IsZero() {
		t := h.appDataPackets.lastAckElicitingPacketTime.Add(h.getScaledPTO(true))
		if pto.IsZero() || (!t.IsZero() && t.Before(pto)) {
			pto = t
			encLevel = protocol.Encryption1RTT
		}
	}
	return pto, encLevel
}

func (h *sentPacketHandler) hasOutstandingCryptoPackets() bool {
	if h.initialPackets != nil && h.initialPackets.history.HasOutstandingPackets() {
		return true
	}
	if h.handshakePackets != nil && h.handshakePackets.history.HasOutstandingPackets() {
		return true
	}
	return false
}

func (h *sentPacketHandler) setLossDetectionTimer(now monotime.Time) {
	oldAlarm := h.alarm // only needed in case tracing is enabled
	newAlarm := h.lossDetectionTime(now)
	h.alarm = newAlarm

	hasAlarm := !newAlarm.Time.IsZero()
	if !hasAlarm && !oldAlarm.Time.IsZero() {
		h.logger.Debugf("Canceling loss detection timer.")
		if h.qlogger != nil {
			h.qlogger.RecordEvent(qlog.LossTimerUpdated{
				Type: qlog.LossTimerUpdateTypeCancelled,
			})
		}
	}

	if h.qlogger != nil && hasAlarm && newAlarm != oldAlarm {
		h.qlogger.RecordEvent(qlog.LossTimerUpdated{
			Type:      qlog.LossTimerUpdateTypeSet,
			TimerType: newAlarm.TimerType,
			EncLevel:  newAlarm.EncryptionLevel,
			Time:      newAlarm.Time.ToTime(),
		})
	}
}

func (h *sentPacketHandler) lossDetectionTime(now monotime.Time) alarmTimer {
	// cancel the alarm if no packets are outstanding
	if h.peerCompletedAddressValidation && !h.hasOutstandingCryptoPackets() &&
		!h.appDataPackets.history.HasOutstandingPackets() && !h.appDataPackets.history.HasOutstandingPathProbes() {
		return alarmTimer{}
	}

	// cancel the alarm if amplification limited
	if h.isAmplificationLimited() {
		return alarmTimer{}
	}

	var pathProbeLossTime monotime.Time
	if h.appDataPackets.history.HasOutstandingPathProbes() {
		if _, p := h.appDataPackets.history.FirstOutstandingPathProbe(); p != nil {
			pathProbeLossTime = p.SendTime.Add(pathProbePacketLossTimeout)
		}
	}

	// early retransmit timer or time loss detection
	lossTime, encLevel := h.getLossTimeAndSpace()
	if !lossTime.IsZero() && (pathProbeLossTime.IsZero() || lossTime.Before(pathProbeLossTime)) {
		return alarmTimer{
			Time:            lossTime,
			TimerType:       qlog.TimerTypeACK,
			EncryptionLevel: encLevel,
		}
	}
	ptoTime, encLevel := h.getPTOTimeAndSpace(now)
	if !ptoTime.IsZero() && (pathProbeLossTime.IsZero() || ptoTime.Before(pathProbeLossTime)) {
		return alarmTimer{
			Time:            ptoTime,
			TimerType:       qlog.TimerTypePTO,
			EncryptionLevel: encLevel,
		}
	}
	if !pathProbeLossTime.IsZero() {
		return alarmTimer{
			Time:            pathProbeLossTime,
			TimerType:       qlog.TimerTypePathProbe,
			EncryptionLevel: protocol.Encryption1RTT,
		}
	}
	return alarmTimer{}
}

func (h *sentPacketHandler) detectLostPathProbes(now monotime.Time) {
	if !h.appDataPackets.history.HasOutstandingPathProbes() {
		return
	}
	lossTime := now.Add(-pathProbePacketLossTimeout)
	for pn, p := range h.appDataPackets.history.PathProbes() {
		if p.SendTime.After(lossTime) {
			continue
		}
		h.appDataPackets.history.RemovePathProbe(pn)
		for _, f := range p.Frames {
			f.Handler.OnLost(f.Frame)
		}
		h.discardCongestionPacket(protocol.Encryption1RTT, pn)
	}
}

func (h *sentPacketHandler) lossDelay(level protocol.EncryptionLevel) time.Duration {
	eighths := uint8(defaultTimeThresholdEighths)
	if level == protocol.Encryption0RTT || level == protocol.Encryption1RTT {
		eighths = h.timeThresholdEighths
	}
	maxRTT := float64(max(h.rttStats.LatestRTT(), h.rttStats.SmoothedRTT()))
	lossDelay := time.Duration((float64(8+eighths) / 8) * maxRTT)

	// Minimum time of granularity before packets are deemed lost.
	return max(lossDelay, protocol.TimerGranularity)
}

func (h *sentPacketHandler) detectLostPackets(now monotime.Time, encLevel protocol.EncryptionLevel) {
	pnSpace := h.getPacketNumberSpace(encLevel)
	pnSpace.lossTime = 0
	lossDelay := h.lossDelay(encLevel)

	// Packets sent before this time are deemed lost.
	lostSendTime := now.Add(-lossDelay)

	priorInFlight := h.bytesInFlight
	for pn, p := range pnSpace.history.Packets() {
		if pn > pnSpace.largestAcked {
			break
		}

		var packetLost bool
		if !p.SendTime.After(lostSendTime) {
			packetLost = true
			if !p.isPathProbePacket && p.IsAckEliciting() {
				if h.logger.Debug() {
					h.logger.Debugf("\tlost packet %d (time threshold)", pn)
				}
				if h.qlogger != nil {
					h.qlogger.RecordEvent(qlog.PacketLost{
						Header: qlog.PacketHeader{
							PacketType:   qlog.EncryptionLevelToPacketType(p.EncryptionLevel),
							PacketNumber: pn,
						},
						Trigger: qlog.PacketLossTimeThreshold,
					})
				}
			}
		} else if (pnSpace != h.appDataPackets || !h.packetThresholdDisabled) && pnSpace.history.Difference(pnSpace.largestAcked, pn) >= packetThreshold {
			packetLost = true
			if !p.isPathProbePacket && p.IsAckEliciting() {
				if h.logger.Debug() {
					h.logger.Debugf("\tlost packet %d (reordering threshold)", pn)
				}
				if h.qlogger != nil {
					h.qlogger.RecordEvent(qlog.PacketLost{
						Header: qlog.PacketHeader{
							PacketType:   qlog.EncryptionLevelToPacketType(p.EncryptionLevel),
							PacketNumber: pn,
						},
						Trigger: qlog.PacketLossReorderingThreshold,
					})
				}
			}
		} else if pnSpace.lossTime.IsZero() {
			// Note: This conditional is only entered once per call
			lossTime := p.SendTime.Add(lossDelay)
			if h.logger.Debug() {
				h.logger.Debugf("\tsetting loss timer for packet %d (%s) to %s (in %s)", pn, encLevel, lossDelay, lossTime)
			}
			pnSpace.lossTime = lossTime
		}
		if packetLost {
			h.captureCongestionLoss(encLevel, pn, p)
			if p.Outstanding() && (encLevel == protocol.Encryption0RTT || encLevel == protocol.Encryption1RTT) {
				h.lostPackets.Add(pn, p.SendTime)
			}
			pnSpace.history.DeclareLost(pn)
			if !p.isPathProbePacket && p.IsAckEliciting() {
				// the bytes in flight need to be reduced no matter if the frames in this packet will be retransmitted
				h.removeFromBytesInFlight(p)
				h.queueFramesForRetransmission(p)
				if !p.IsPathMTUProbePacket {
					h.congestion.OnCongestionEvent(congestion.NewPacketKey(p.EncryptionLevel, pn), p.Length, priorInFlight)
				}
				if encLevel == protocol.Encryption1RTT && h.bbrECN != nil {
					h.bbrECN.lostPacket(pn)
				}
				if encLevel == protocol.Encryption1RTT && h.ecnTracker != nil {
					h.ecnTracker.LostPacket(pn)
				}
			}
		}
	}
}

func (h *sentPacketHandler) OnLossDetectionTimeout(now monotime.Time) (TimeoutOutcome, error) {
	defer h.setLossDetectionTimer(now)

	if h.handshakeConfirmed {
		h.detectLostPathProbes(now)
	}

	earliestLossTime, encLevel := h.getLossTimeAndSpace()
	if !earliestLossTime.IsZero() {
		if h.logger.Debug() {
			h.logger.Debugf("Loss detection alarm fired in loss timer mode. Loss time: %s", earliestLossTime)
		}
		if h.qlogger != nil {
			h.qlogger.RecordEvent(qlog.LossTimerUpdated{
				Type:      qlog.LossTimerUpdateTypeExpired,
				TimerType: qlog.TimerTypeACK,
				EncLevel:  encLevel,
			})
		}
		// Early retransmit or time loss detection
		h.beginCongestionFeedback(now, encLevel, h.bytesInFlight, nil, protocol.InvalidPacketNumber)
		h.detectLostPackets(now, encLevel)
		h.finishCongestionFeedback()
		return TimeoutOutcome{}, nil
	}

	// PTO
	// When all outstanding are acknowledged, the alarm is canceled in setLossDetectionTimer.
	// However, there's no way to reset the timer in the connection.
	// When OnLossDetectionTimeout is called, we therefore need to make sure that there are
	// actually packets outstanding.
	if h.bytesInFlight == 0 && !h.peerCompletedAddressValidation {
		// A client with nothing in flight sends one probe, so that the server isn't
		// stuck at its amplification limit. This branch emits no qlog event.
		h.ptoCount++
		h.numProbesToSend++
		switch {
		case h.initialPackets != nil:
			h.ptoMode = SendPTOInitial
			return TimeoutOutcome{PTO: true, EncLevel: protocol.EncryptionInitial}, nil
		case h.handshakePackets != nil:
			h.ptoMode = SendPTOHandshake
			return TimeoutOutcome{PTO: true, EncLevel: protocol.EncryptionHandshake}, nil
		}
		return TimeoutOutcome{}, errors.New("sentPacketHandler BUG: PTO fired, but bytes_in_flight is 0 and Initial and Handshake already dropped")
	}

	ptoTime, encLevel := h.getPTOTimeAndSpace(now)
	if ptoTime.IsZero() {
		return TimeoutOutcome{}, nil
	}
	ps := h.getPacketNumberSpace(encLevel)
	if !ps.history.HasOutstandingPackets() && !ps.history.HasOutstandingPathProbes() && !h.peerCompletedAddressValidation {
		return TimeoutOutcome{}, nil
	}
	h.ptoCount++
	if h.logger.Debug() {
		h.logger.Debugf("Loss detection alarm for %s fired in PTO mode. PTO count: %d", encLevel, h.ptoCount)
	}
	if h.qlogger != nil {
		h.qlogger.RecordEvent(qlog.LossTimerUpdated{
			Type:      qlog.LossTimerUpdateTypeExpired,
			TimerType: qlog.TimerTypePTO,
			EncLevel:  encLevel,
		})
		h.qlogger.RecordEvent(qlog.PTOCountUpdated{PTOCount: h.ptoCount})
	}
	h.numProbesToSend += 2
	//nolint:exhaustive // We never arm a PTO timer for 0-RTT packets.
	switch encLevel {
	case protocol.EncryptionInitial:
		h.ptoMode = SendPTOInitial
	case protocol.EncryptionHandshake:
		h.ptoMode = SendPTOHandshake
	case protocol.Encryption1RTT:
		// skip a packet number in order to elicit an immediate ACK
		pn := h.PopPacketNumber(protocol.Encryption1RTT)
		h.getPacketNumberSpace(protocol.Encryption1RTT).history.SkippedPacket(pn)
		h.ptoMode = SendPTOAppData
	default:
		return TimeoutOutcome{}, fmt.Errorf("PTO timer in unexpected encryption level: %s", encLevel)
	}
	return TimeoutOutcome{PTO: true, EncLevel: encLevel}, nil
}

func (h *sentPacketHandler) GetLossDetectionTimeout() monotime.Time {
	return h.alarm.Time
}

func (h *sentPacketHandler) ECNMode(isShortHeaderPacket bool) protocol.ECN {
	if h.bbrECN != nil {
		return h.bbrECN.mode(isShortHeaderPacket)
	}
	if !h.enableECN {
		return protocol.ECNUnsupported
	}
	if !isShortHeaderPacket {
		return protocol.ECNNon
	}
	return h.ecnTracker.Mode()
}

func (h *sentPacketHandler) PeekPacketNumber(encLevel protocol.EncryptionLevel) (protocol.PacketNumber, protocol.PacketNumberLen) {
	pnSpace := h.getPacketNumberSpace(encLevel)
	pn := pnSpace.pns.Peek()
	// See section 17.1 of RFC 9000.
	return pn, protocol.PacketNumberLengthForHeader(pn, pnSpace.largestAcked)
}

func (h *sentPacketHandler) PopPacketNumber(encLevel protocol.EncryptionLevel) protocol.PacketNumber {
	pnSpace := h.getPacketNumberSpace(encLevel)
	skipped, pn := pnSpace.pns.Pop()
	if skipped {
		skippedPN := pn - 1
		pnSpace.history.SkippedPacket(skippedPN)
		if h.logger.Debug() {
			h.logger.Debugf("Skipping packet number %d", skippedPN)
		}
	}
	return pn
}

func (h *sentPacketHandler) SendMode(now monotime.Time) SendMode {
	return h.sendMode(now, true)
}

// SendAllowance supplies the private emission pacer without applying Reno's
// rate/burst adjustment twice. All recovery, probe and amplification gates stay
// authoritative, and the caller must fit whole datagrams within the allowance.
func (h *sentPacketHandler) SendAllowance(now monotime.Time) (SendMode, protocol.ByteCount) {
	allowance := max(0, h.congestion.GetCongestionWindow()-h.bytesInFlight)
	if !h.peerAddressValidated {
		allowance = min(allowance, max(0, amplificationFactor*h.bytesReceived-h.bytesSent))
	}
	return h.sendMode(now, false), allowance
}

func (h *sentPacketHandler) numTrackedPackets() int {
	numTrackedPackets := h.appDataPackets.history.Len()
	if h.initialPackets != nil {
		numTrackedPackets += h.initialPackets.history.Len()
	}
	if h.handshakePackets != nil {
		numTrackedPackets += h.handshakePackets.history.Len()
	}
	return numTrackedPackets
}

func (h *sentPacketHandler) CanSendPathProbe() bool {
	if h.appDataPackets.history.pathProbePackets.Len() >= maxOutstandingPathProbes {
		return false
	}
	// Popping the next packet number can also insert a skipped-number marker.
	additional := 1
	if h.appDataPackets.history.Len() > 0 {
		additional = int(h.appDataPackets.pns.Peek() - h.appDataPackets.history.highestPacketNumber)
	}
	return h.numTrackedPackets()+additional <= protocol.MaxTrackedSentPackets
}

func (h *sentPacketHandler) sendMode(now monotime.Time, paced bool) SendMode {
	numTrackedPackets := h.numTrackedPackets()

	if h.isAmplificationLimited() {
		h.logger.Debugf("Amplification window limited. Received %d bytes, already sent out %d bytes", h.bytesReceived, h.bytesSent)
		return SendNone
	}
	// Don't send any packets if we're keeping track of the maximum number of packets.
	// Note that since MaxOutstandingSentPackets is smaller than MaxTrackedSentPackets,
	// we will stop sending out new data when reaching MaxOutstandingSentPackets,
	// but still allow sending of retransmissions and ACKs.
	if numTrackedPackets >= protocol.MaxTrackedSentPackets {
		if h.logger.Debug() {
			h.logger.Debugf("Limited by the number of tracked packets: tracking %d packets, maximum %d", numTrackedPackets, protocol.MaxTrackedSentPackets)
		}
		return SendNone
	}
	if h.numProbesToSend > 0 {
		return h.ptoMode
	}
	// Only send ACKs if we're congestion limited.
	if !h.congestion.CanSend(h.bytesInFlight) {
		if h.logger.Debug() {
			h.logger.Debugf("Congestion limited: bytes in flight %d, window %d", h.bytesInFlight, h.congestion.GetCongestionWindow())
		}
		return SendAck
	}
	if numTrackedPackets >= protocol.MaxOutstandingSentPackets {
		if h.logger.Debug() {
			h.logger.Debugf("Max outstanding limited: tracking %d packets, maximum: %d", numTrackedPackets, protocol.MaxOutstandingSentPackets)
		}
		return SendAck
	}
	if paced && !h.congestion.HasPacingBudget(now) {
		return SendPacingLimited
	}
	return SendAny
}

func (h *sentPacketHandler) TimeUntilSend() monotime.Time {
	return h.congestion.TimeUntilSend(h.bytesInFlight)
}

func (h *sentPacketHandler) SetMaxDatagramSize(s protocol.ByteCount) {
	h.congestion.SetMaxDatagramSize(s)
}

func (h *sentPacketHandler) isAmplificationLimited() bool {
	if h.peerAddressValidated {
		return false
	}
	return h.bytesSent >= amplificationFactor*h.bytesReceived
}

func (h *sentPacketHandler) QueueProbePacket(encLevel protocol.EncryptionLevel) bool {
	return h.QueueProbePacketAt(encLevel, monotime.Now())
}

func (h *sentPacketHandler) QueueProbePacketAt(encLevel protocol.EncryptionLevel, now monotime.Time) bool {
	pnSpace := h.getPacketNumberSpace(encLevel)
	pn, p := pnSpace.history.FirstOutstanding()
	if p == nil {
		return false
	}
	if d := h.congestionEvents; d != nil {
		d.recovery.retirePTO(congestionKey(encLevel, pn))
		d.retire(congestionKey(encLevel, pn), now, h.rttStats.PTO(encLevel == protocol.Encryption1RTT), deliveryRetiredPTO)
	}
	// TODO: don't declare the packet lost here.
	// Keep track of acknowledged frames instead.
	// Call DeclareLost before queueFramesForRetransmission, which clears the packet's frames.
	pnSpace.history.DeclareLost(pn)
	h.removeFromBytesInFlight(p)
	h.queueFramesForRetransmission(p)
	return true
}

func (h *sentPacketHandler) queueFramesForRetransmission(p *packet) {
	if len(p.Frames) == 0 && len(p.StreamFrames) == 0 {
		panic("no frames")
	}
	for _, f := range p.Frames {
		if f.Handler != nil {
			f.Handler.OnLost(f.Frame)
		}
	}
	for _, f := range p.StreamFrames {
		if f.Handler != nil {
			f.Handler.OnLost(f.Frame)
		}
	}
	p.StreamFrames = nil
	p.Frames = nil
}

func (h *sentPacketHandler) ResetForRetry(now monotime.Time) {
	h.congestion.OnPacketsDiscarded()
	h.lostPackets.Reset()
	h.resetCongestionCapture(false)
	h.bytesInFlight = 0
	var firstPacketSendTime monotime.Time
	for _, p := range h.initialPackets.history.Packets() {
		if firstPacketSendTime.IsZero() {
			firstPacketSendTime = p.SendTime
		}
		if p.IsAckEliciting() {
			h.queueFramesForRetransmission(p)
		}
	}
	// All application data packets sent at this point are 0-RTT packets.
	// In the case of a Retry, we can assume that the server dropped all of them.
	for _, p := range h.appDataPackets.history.Packets() {
		if p.IsAckEliciting() {
			h.queueFramesForRetransmission(p)
		}
	}

	// Only use the Retry to estimate the RTT if we didn't send any retransmission for the Initial.
	// Otherwise, we don't know which Initial the Retry was sent in response to.
	if h.ptoCount == 0 {
		// Don't set the RTT to a value lower than 5ms here.
		h.rttStats.UpdateRTT(max(minRTTAfterRetry, now.Sub(firstPacketSendTime)), 0)
		if h.logger.Debug() {
			h.logger.Debugf("\tupdated RTT: %s (σ: %s)", h.rttStats.SmoothedRTT(), h.rttStats.MeanDeviation())
		}
		if h.qlogger != nil {
			h.qlogMetricsUpdated()
		}
	}
	h.initialPackets = newPacketNumberSpace(h.initialPackets.pns.Peek(), false)
	h.appDataPackets = newPacketNumberSpace(h.appDataPackets.pns.Peek(), true)
	oldAlarm := h.alarm
	h.alarm = alarmTimer{}
	if h.qlogger != nil {
		h.qlogger.RecordEvent(qlog.PTOCountUpdated{PTOCount: 0})
		if !oldAlarm.Time.IsZero() {
			h.qlogger.RecordEvent(qlog.LossTimerUpdated{
				Type: qlog.LossTimerUpdateTypeCancelled,
			})
		}
	}
	h.ptoCount = 0
}

func (h *sentPacketHandler) MigratedPath(now monotime.Time, initialMaxDatagramSize protocol.ByteCount) {
	h.resetCongestionCapture(true)
	h.rttStats.ResetForPathMigration()
	h.packetThresholdDisabled = false
	h.timeThresholdEighths = defaultTimeThresholdEighths
	h.minPNToRelaxReordering = protocol.InvalidPacketNumber
	h.lostPackets.Reset()
	for pn, p := range h.appDataPackets.history.Packets() {
		h.appDataPackets.history.DeclareLost(pn)
		if !p.isPathProbePacket {
			h.removeFromBytesInFlight(p)
			if p.IsAckEliciting() {
				h.queueFramesForRetransmission(p)
			}
		}
	}
	for pn := range h.appDataPackets.history.PathProbes() {
		h.appDataPackets.history.RemovePathProbe(pn)
	}
	if b, ok := h.congestion.(*congestion.BBRSender); ok {
		b.SetMaxDatagramSize(initialMaxDatagramSize)
		b.Reset(h.congestionEvents.pathGeneration, h.congestionEvents.sampleGeneration, h.congestionEvents.sampler.delivered)
	} else {
		h.congestion = congestion.NewCubicSender(
			congestion.DefaultClock{},
			h.rttStats,
			h.connStats,
			initialMaxDatagramSize,
			true, // use Reno
			h.qlogger,
		)
	}
	h.setLossDetectionTimer(now)
}
