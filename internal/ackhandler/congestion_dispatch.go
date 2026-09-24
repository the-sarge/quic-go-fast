package ackhandler

import (
	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

// congestionEventSink is private constructor plumbing, unavailable to ordinary
// connections until a complete controller is wired. It observes value records;
// recovery and the legacy Reno controller remain the only mutable authorities.
type congestionEventSink interface {
	Sent(congestion.SendEvent)
	Feedback(congestion.FeedbackEvent)
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
	ordinal          uint64
	pathGeneration   uint64
	sampleGeneration uint64
	sink             congestionEventSink
	packets          map[congestionPacketKey]congestion.PacketInfo
	event            congestion.FeedbackEvent
}

func (h *sentPacketHandler) captureCongestionSend(pn protocol.PacketNumber, p *packet, ecn protocol.ECN, prior protocol.ByteCount) {
	d := h.congestionEvents
	if d == nil {
		return
	}
	d.ordinal++
	info := congestion.PacketInfo{
		Space:            congestionKey(p.EncryptionLevel, pn).space,
		Ordinal:          d.ordinal,
		PathGeneration:   d.pathGeneration,
		SampleGeneration: d.sampleGeneration,
		PacketNumber:     pn,
		EncryptionLevel:  p.EncryptionLevel,
		SendTime:         p.SendTime,
		Length:           p.Length,
		AckEliciting:     p.IsAckEliciting(),
		InFlight:         p.includedInBytesInFlight,
		PathProbe:        p.isPathProbePacket,
		MTUProbe:         p.IsPathMTUProbePacket,
		ECN:              ecn,
	}

	d.packets[congestionKey(p.EncryptionLevel, pn)] = info
	d.sink.Sent(congestion.SendEvent{Packet: info, PriorInFlight: prior, PostInFlight: h.bytesInFlight})
}

func (h *sentPacketHandler) beginCongestionFeedback(now monotime.Time, level protocol.EncryptionLevel, prior protocol.ByteCount, acked []packetWithPacketNumber, largest protocol.PacketNumber) {
	d := h.congestionEvents
	if d == nil {
		return
	}
	d.event = congestion.FeedbackEvent{
		PathGeneration:   d.pathGeneration,
		SampleGeneration: d.sampleGeneration,
		Time:             now,
		Space:            congestionKey(level, 0).space,
		HasAck:           largest != protocol.InvalidPacketNumber,
		LargestAcked:     largest,
		PriorInFlight:    prior,
		Acked:            d.event.Acked[:0],
		Lost:             d.event.Lost[:0],
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
		d.event.Lost = append(d.event.Lost, info)
	}
	delete(d.packets, key)
}

func (h *sentPacketHandler) finishCongestionFeedback() {
	d := h.congestionEvents
	if d == nil {
		return
	}
	d.event.PostInFlight = h.bytesInFlight
	d.sink.Feedback(d.event)
	clear(d.event.Acked)
	clear(d.event.Lost)
	d.event.Acked = d.event.Acked[:0]
	d.event.Lost = d.event.Lost[:0]
}

// Disposal is not congestion loss or delivery. These records have exactly the
// live recovery lifetime; retained late-delivery evidence belongs to T2.
func (h *sentPacketHandler) discardCongestionPacket(level protocol.EncryptionLevel, pn protocol.PacketNumber) {
	if h.congestionEvents != nil {
		delete(h.congestionEvents.packets, congestionKey(level, pn))
	}
}

func (h *sentPacketHandler) discardCongestionSpace(level protocol.EncryptionLevel) {
	if d := h.congestionEvents; d != nil {
		for key, p := range d.packets {
			if p.EncryptionLevel == level {
				delete(d.packets, key)
			}
		}
	}
}

func (h *sentPacketHandler) resetCongestionCapture(pathChanged bool) {
	if d := h.congestionEvents; d != nil {
		d.sampleGeneration++
		if pathChanged {
			d.pathGeneration++
		}
		// Release retained scratch and map capacity at restart. Connection teardown
		// needs no independent cleanup: this state owns no resources or goroutines.
		d.packets = make(map[congestionPacketKey]congestion.PacketInfo)
		d.event = congestion.FeedbackEvent{}
	}
}
