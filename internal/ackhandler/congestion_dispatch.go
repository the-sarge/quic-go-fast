package ackhandler

import (
	"cmp"
	"slices"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
)

// congestionEventSink is private constructor plumbing, unavailable to ordinary
// connections until a complete controller is wired. It observes value records;
// recovery and the legacy Reno controller remain the only mutable authorities.
type congestionEventSink interface {
	Sent(congestion.SendEvent)
	Feedback(congestion.FeedbackEvent)
}

// EnableDeliverySampling is internal constructor plumbing for the private rich
// path. Public QUIC constructors never call it. Installation after registration
// is forbidden: a partial history cannot become authoritative evidence.
func EnableDeliverySampling(handler SentPacketHandler, sink interface {
	Sent(congestion.SendEvent)
	Feedback(congestion.FeedbackEvent)
}, pending func() protocol.ByteCount,
) {
	h, ok := handler.(*sentPacketHandler)
	if !ok || h.bytesSent != 0 || h.congestionEvents != nil || sink == nil || pending == nil {
		panic("invalid delivery sampler installation")
	}
	h.congestionEvents = &congestionDispatch{sink: sink, pending: pending, packets: make(map[congestionPacketKey]congestion.PacketInfo)}
}

type congestionPacketKey struct {
	space  protocol.EncryptionLevel
	number protocol.PacketNumber
}

func congestionKey(level protocol.EncryptionLevel, pn protocol.PacketNumber) congestionPacketKey {
	if level == protocol.Encryption0RTT {
		level = protocol.Encryption1RTT
	}
	return congestionPacketKey{space: level, number: pn}
}

type congestionDispatch struct {
	pending          func() protocol.ByteCount
	sampler          deliverySampler
	ordinal          uint64
	registrationTime monotime.Time
	pathGeneration   uint64
	sampleGeneration uint64
	sink             congestionEventSink
	packets          map[congestionPacketKey]congestion.PacketInfo
	event            congestion.FeedbackEvent
	scratch          []congestion.PacketInfo
}

func (h *sentPacketHandler) captureCongestionSend(pn protocol.PacketNumber, p *packet, ecn protocol.ECN, prior protocol.ByteCount) {
	d := h.congestionEvents
	if d == nil {
		return
	}
	d.ordinal++
	// 0-RTT shares the application space. Retain its actual unmarked
	// ordinal even after recovery disposal; it never adds testing marks.
	if h.bbrECN != nil && congestionKey(p.EncryptionLevel, pn).space == protocol.Encryption1RTT {
		h.bbrECN.sentPacket(pn, d.ordinal, d.pathGeneration, ecn)
	}
	d.expire(p.SendTime)
	registrationValid := !p.SendTime.IsZero() && !p.SendTime.Before(d.registrationTime)
	if registrationValid && p.IsAckEliciting() && !p.isPathProbePacket && d.pendingBytes() == 0 &&
		(d.sampler.sendOrigin.IsZero() || (prior == 0 && d.sampler.outstanding == 0)) {
		// A lifecycle fence leaves no usable current-generation origin. Records
		// sent while credit was pending have no origin either; fence those out
		// before establishing a baseline, without disposing their recovery.
		if !d.sampler.sendOrigin.IsZero() || d.sampler.originlessRegistrations {
			d.sampleGeneration++
		}
		d.sampler.sendOrigin, d.sampler.deliveredTime = p.SendTime, p.SendTime
		d.sampler.evidenceLost = false
		d.sampler.originlessRegistrations = false
	}
	info := congestion.PacketInfo{
		Space:             congestionKey(p.EncryptionLevel, pn).space,
		Ordinal:           d.ordinal,
		PathGeneration:    d.pathGeneration,
		SampleGeneration:  d.sampleGeneration,
		PacketNumber:      pn,
		EncryptionLevel:   p.EncryptionLevel,
		SendTime:          p.SendTime,
		RegistrationValid: registrationValid,
		Length:            p.Length,
		AckEliciting:      p.IsAckEliciting(),
		InFlight:          p.includedInBytesInFlight,
		PathProbe:         p.isPathProbePacket,
		MTUProbe:          p.IsPathMTUProbePacket,
		ECN:               ecn,
	}
	d.registrationTime = max(d.registrationTime, p.SendTime)

	if len(d.packets) < maxDeliveryLive {
		d.sampler.sent(&info, prior, h.bytesInFlight)
		d.packets[congestionKey(p.EncryptionLevel, pn)] = info
	} else {
		d.sampler.missing++ // Optional evidence never evicts mandatory recovery.
		d.sampler.evidenceLost = true
	}
	d.sink.Sent(congestion.SendEvent{Packet: info, PriorInFlight: prior, PostInFlight: h.bytesInFlight})
}

func (h *sentPacketHandler) beginCongestionFeedback(now monotime.Time, level protocol.EncryptionLevel, prior protocol.ByteCount, acked []packetWithPacketNumber, largest protocol.PacketNumber) {
	d := h.congestionEvents
	if d == nil {
		return
	}
	// ACK values grow from the front and losses from the back of one bounded
	// buffer. Registration cannot interleave this connection-owned event.
	needed := len(d.packets) + len(d.sampler.retained)
	if cap(d.scratch) < needed {
		d.scratch = make([]congestion.PacketInfo, min(maxDeliveryLive+maxDeliveryRetained, max(needed, 2*cap(d.scratch))))
	}
	d.scratch = d.scratch[:cap(d.scratch)]
	d.event = congestion.FeedbackEvent{
		PathGeneration:   d.pathGeneration,
		SampleGeneration: d.sampleGeneration,
		Time:             now,
		Space:            congestionKey(level, 0).space,
		HasAck:           largest != protocol.InvalidPacketNumber,
		LargestAcked:     largest,
		PriorInFlight:    prior,
		Acked:            d.scratch[:0],
		Lost:             d.scratch[len(d.scratch):],
	}

	for _, p := range acked {
		key := congestionKey(p.EncryptionLevel, p.PacketNumber)
		if info, ok := d.packets[key]; ok {
			d.event.Acked = append(d.event.Acked, info)
		}
		delete(d.packets, key)
	}
}

func (h *sentPacketHandler) captureCongestionLoss(level protocol.EncryptionLevel, pn protocol.PacketNumber, p *packet) {
	d := h.congestionEvents
	if d == nil {
		return
	}
	key := congestionKey(level, pn)
	if info, ok := d.packets[key]; ok && p.includedInBytesInFlight && !p.IsPathMTUProbePacket && !p.isPathProbePacket {
		start := len(d.scratch) - len(d.event.Lost) - 1
		d.scratch[start] = info
		d.event.Lost = d.scratch[start:]
	}
	d.retire(key, d.event.Time, h.rttStats.PTO(level == protocol.Encryption1RTT), deliveryRetiredLoss)
}

func (h *sentPacketHandler) finishCongestionFeedback() {
	d := h.congestionEvents
	if d == nil {
		return
	}
	d.event.PostInFlight = h.bytesInFlight
	if h.bbrECN != nil {
		d.event.ECN.Failed = h.bbrECN.state == ecnStateFailed || h.bbrECN.evidenceLost
	}
	slices.SortFunc(d.event.Lost, func(a, b congestion.PacketInfo) int { return cmp.Compare(a.Ordinal, b.Ordinal) })
	d.sampler.feedback(&d.event)
	// A timer scan may run early or against a stale loss deadline. Preserve
	// MTU retirement flight changes even though they are not congestion loss.
	if d.event.HasAck || len(d.event.Lost) > 0 || d.event.PriorInFlight != d.event.PostInFlight {
		d.sink.Feedback(d.event)
	}
	clear(d.event.Acked)
	clear(d.event.Lost)
	d.event.Acked = d.event.Acked[:0]
	d.event.Lost = d.event.Lost[:0]
}

// Disposal is not congestion loss or delivery.
func (h *sentPacketHandler) discardCongestionPacket(level protocol.EncryptionLevel, pn protocol.PacketNumber) {
	if h.congestionEvents != nil {
		d := h.congestionEvents
		key := congestionKey(level, pn)
		if p, ok := d.packets[key]; ok {
			d.sampler.dispose(p)
			delete(d.packets, key)
		}
	}
}

func (h *sentPacketHandler) discardCongestionSpace(level protocol.EncryptionLevel) {
	if d := h.congestionEvents; d != nil {
		for key, p := range d.packets {
			if p.EncryptionLevel == level {
				d.sampler.dispose(p)
				delete(d.packets, key)
			}
		}
		for key, r := range d.sampler.retained {
			if r.packet.EncryptionLevel == level {
				d.removeRetained(key, true)
			}
		}
		if level == protocol.Encryption0RTT {
			d.sampleGeneration++
			d.sampler.sendOrigin, d.sampler.deliveredTime = 0, 0
			d.sampler.originlessRegistrations = false
		}
	}
}

func (h *sentPacketHandler) resetCongestionCapture(pathChanged bool) {
	if d := h.congestionEvents; d != nil {
		d.sampleGeneration++
		if pathChanged {
			d.pathGeneration++
			if h.bbrECN != nil {
				h.bbrECN.resetPath(d.pathGeneration)
			}
		}
		// Release retained scratch and map capacity at restart. Connection teardown
		// needs no independent cleanup: this state owns no resources or goroutines.
		d.packets = make(map[congestionPacketKey]congestion.PacketInfo)
		d.event = congestion.FeedbackEvent{}
		d.scratch = nil
		d.sampler = deliverySampler{delivered: d.sampler.delivered, lost: d.sampler.lost}
	}
}

// Retained ACK discovery runs only after recovery has validated the decoded ACK.
// It visits bounded stored keys, never the packet numbers in an absent range.
func (h *sentPacketHandler) appendRetainedAck(ack *wire.AckFrame, level protocol.EncryptionLevel) {
	d := h.congestionEvents
	if d == nil {
		return
	}
	for key, r := range d.sampler.retained {
		if key.space == congestionKey(level, 0).space && ack.AcksPacket(key.number) {
			d.event.Acked = append(d.event.Acked, r.packet)
			d.removeRetained(key, false)
		}
	}
	slices.SortFunc(d.event.Acked, func(a, b congestion.PacketInfo) int { return cmp.Compare(a.Ordinal, b.Ordinal) })
}

func (h *sentPacketHandler) captureDeliveryRTT(p packetWithPacketNumber, now monotime.Time) {
	d := h.congestionEvents
	if d == nil || p.IsPathMTUProbePacket || p.isPathProbePacket || !now.After(p.SendTime) {
		return
	}
	for _, info := range d.event.Acked {
		if info.PacketNumber == p.PacketNumber && info.EncryptionLevel == p.EncryptionLevel && info.PathGeneration == d.pathGeneration && info.SampleGeneration == d.sampleGeneration && info.RegistrationValid {
			d.event.RawRTT = now.Sub(p.SendTime)
			return
		}
	}
}

func (h *sentPacketHandler) captureBBRECN(ack *wire.AckFrame, level protocol.EncryptionLevel) {
	if h.bbrECN != nil && h.congestionEvents != nil && level == protocol.Encryption1RTT {
		e := h.bbrECN.feedback(ack)
		h.congestionEvents.event.ECN = e
		h.congestionEvents.event.ECNChecked = e.Eligible
		h.congestionEvents.event.Congested = e.Eligible && e.Delta.CE > 0
	}
}
