package quic

import (
	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

// These observations never move delivery/model mutation onto the worker.
type deliveryLifecycle interface {
	DeliveryExpiry() monotime.Time
	ExpireDelivery(monotime.Time)
	CloseDelivery()
}

func (e *packetEmission) deliveryPendingBytes() protocol.ByteCount {
	if e.bbr == nil {
		return -1 // legacy queues expose no byte-credit authority
	}
	return e.currentPendingBytes(e.bbr)
}

// currentPendingBytes is one policy's current-generation local work.
func (e *packetEmission) currentPendingBytes(p *bbrSendPolicy) protocol.ByteCount {
	c := p.credit
	c.mu.Lock()
	defer c.mu.Unlock()
	n := c.current
	// A construction reservation is allowance for this registration, not prior
	// local work, and only the credit that granted it excludes it. Earlier
	// registrations in the same batch remain unresolved.
	if r := e.reservation; r != nil && r.owner == c && r.generation == c.generation {
		n -= r.bytes
	}
	return n
}

// bbrPendingBytes is one BBR policy's local work, the sampler's pending-local
// authority. The current construction reservation is capacity for the packet
// being registered, not older queued work. It stays charged for admission but
// is excluded, from the credit that granted it only, when asking whether a
// delivery sampling epoch can begin.
func (e *packetEmission) bbrPendingBytes(p *bbrSendPolicy) protocol.ByteCount {
	c := p.credit
	c.mu.Lock()
	defer c.mu.Unlock()
	pending := c.pending
	if r := e.reservation; r != nil && r.owner == c {
		pending -= r.bytes
	}
	return pending
}

// deliveryObserver is recovery's private sampler surface for emission stops.
type deliveryObserver interface {
	ObserveDeliveryStop(congestion.SendLimitation)
	ObserveDeliveryLimitation(congestion.SendLimitation)
}

// Only BBR's sendBounded reports opportunities here, so e.bbr is set. The
// opportunity's route owns the stop: its recovery samples it, and its policy
// holds the supply gap.
func (e *packetEmission) observeDeliveryResult(route *pathRoute, result emissionResult, now monotime.Time) {
	recovery := *e.recovery
	if route != nil {
		recovery = route.recovery
	}
	h, ok := recovery.(deliveryObserver)
	if !ok {
		return
	}
	p := e.policyOn(route)
	reason := congestion.SendUnknown
	switch result.stop {
	case emissionQueueFull:
		reason = congestion.SendLocalLimited
	case emissionHardBlocked, emissionProbePending:
		reason = congestion.SendRecoveryLimited
	case emissionCongestionLimited:
		reason = congestion.SendCongestionLimited
	case emissionPaced:
		reason = congestion.SendPacingLimited
	case emissionReceivePending:
		reason = congestion.SendReceiveYield
	case emissionNoData:
		if result.supplyExhausted {
			reason = e.packer.deliveryLimitation()
		}
	case emissionSendAny, emissionProbeSent:
	}
	if reason == congestion.SendApplicationLimited {
		if policy, ok := e.policy.(interface{ emissionStreamOpenBlocked() bool }); !ok {
			reason = congestion.SendUnknown
		} else if policy.emissionStreamOpenBlocked() {
			reason = congestion.SendFlowControlLimited
		}
	}
	if reason != congestion.SendApplicationLimited && reason != congestion.SendFlowControlLimited {
		// Any other stop, or packed data, ends a gap that never cost sending time.
		p.supplyGap = congestion.SendUnknown
		h.ObserveDeliveryStop(reason)
		return
	}
	p.supplyGap = reason
	if e.supplyGapCostsSending(p, now) {
		h.ObserveDeliveryLimitation(reason)
		return
	}
	h.ObserveDeliveryStop(reason)
}

// confirmSupplyGap runs before ordinary packing: draft-06 checks for
// application limitation when the connection decides to send more data, so a
// gap that has cost sending time marks the data that ends it.
func (e *packetEmission) confirmSupplyGap(now monotime.Time) {
	p := e.routePolicy()
	if p.supplyGap == congestion.SendUnknown || !e.supplyGapCostsSending(p, now) {
		return
	}
	if h, ok := e.selected().(deliveryObserver); ok {
		h.ObserveDeliveryLimitation(p.supplyGap)
	}
}

// Exhaustion becomes supply limitation only once the gap costs sending time.
// Until the credit last spent has had time to refill, data that returns is sent
// within credit still accumulating; SendStream.Write, for one, returns with the
// last packet of a write still buffered. Pending local work is draft-06's
// C.pending_transmissions: those bytes still reach the path.
func (e *packetEmission) supplyGapCostsSending(p *bbrSendPolicy, now monotime.Time) bool {
	return !now.Before(p.refilledAt) && e.currentPendingBytes(p) == 0
}

func (c *Conn) emissionStreamOpenBlocked() bool {
	m := c.streamsMap
	m.mutex.Lock()
	reset, bidi, uni := m.reset, m.outgoingBidiStreams, m.outgoingUniStreams
	m.mutex.Unlock()
	return reset || bidi.deliveryOpenPending() || uni.deliveryOpenPending()
}

// The opener owner remains authoritative even after STREAMS_BLOCKED is sent.
// A snapshot of waiters is conservative while an unblocked goroutine resumes.
func (m *outgoingStreamsMap[T]) deliveryOpenPending() bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.closeErr == nil && len(m.openQueue) > 0
}

func (p *packetPacker) deliveryLimitation() congestion.SendLimitation {
	if p.retransmissionQueue.HasData(protocol.EncryptionInitial) || p.retransmissionQueue.HasData(protocol.EncryptionHandshake) || p.retransmissionQueue.HasData(protocol.Encryption1RTT) || p.initialStream.HasData() || p.handshakeStream.HasData() || (p.datagramQueue != nil && p.datagramQueue.Peek() != nil) {
		return congestion.SendUnknown
	}
	if f, ok := p.framer.(*framer); ok {
		return f.deliveryLimitation()
	}
	return congestion.SendUnknown
}

// The optional registry follows existing stream completion and 0-RTT cleanup.
// Stream owners supply current facts under their locks; no flow credit is copied.
func (f *framer) enableDeliveryObservations() {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.deliveryStreams = make(map[protocol.StreamID]streamFrameGetter)
	for id, str := range f.activeStreams {
		f.deliveryStreams[id] = str.streamFrameGetter
	}
}

func (f *framer) deliveryLimitation() congestion.SendLimitation {
	f.controlFrameMutex.Lock()
	defer f.controlFrameMutex.Unlock()
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if f.deliveryStreams == nil || len(f.controlFrames) > 0 || len(f.streamsWithControlFrames) > 0 || len(f.pathResponses) > 0 || len(f.retransmissionStreams) > 0 {
		return congestion.SendUnknown
	}
	reason := congestion.SendApplicationLimited
	for _, str := range f.deliveryStreams {
		s, ok := str.(interface {
			deliveryLimitation() congestion.SendLimitation
		})
		if !ok {
			return congestion.SendUnknown
		}
		// Only application and flow-control exhaustion establish supply limits.
		//nolint:exhaustive
		switch s.deliveryLimitation() {
		case congestion.SendApplicationLimited:
		case congestion.SendFlowControlLimited:
			reason = congestion.SendFlowControlLimited
		default:
			return congestion.SendUnknown
		}
	}
	return reason
}

func (s *SendStream) deliveryLimitation() congestion.SendLimitation {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.shutdownErr != nil || s.writeLimited || (s.resetErr != nil && s.writeOffset >= s.reliableOffset()) {
		return congestion.SendApplicationLimited
	}
	if len(s.dataForWriting) == 0 && s.nextFrame == nil {
		if s.finishedWriting && !s.finSent {
			return congestion.SendUnknown
		}
		return congestion.SendApplicationLimited
	}
	if !s.nextFrameReserved && s.flowController.SendWindowSize() == 0 {
		return congestion.SendFlowControlLimited
	}
	return congestion.SendUnknown
}
