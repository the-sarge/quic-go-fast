package ackhandler

import (
	"container/heap"
	"math"
	"math/bits"
	"time"
	"unsafe"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

// deliverySampler is used only by the private rich dispatcher. Recovery owns
// frames and flight; this state owns only copied delivery evidence.
type deliverySampler struct {
	delivered, lost           uint64
	deliveredTime, sendOrigin monotime.Time
	outstanding               protocol.ByteCount
	minimumRTT                time.Duration
	retained                  map[congestionPacketKey]*retainedDelivery
	order                     retainedDeliveryHeap
	evicted, expired, missing uint64
	limitedUntil              uint64
	limited, stop             congestion.SendLimitation
	applicationExhausted      bool
	evidenceLost              bool
	nextExpiry                monotime.Time
	originlessRegistrations   bool
}

const (
	maxDeliveryLive     = 25000
	maxDeliveryRetained = 4096
)

type deliveryRetirement uint8

const (
	deliveryRetiredLoss deliveryRetirement = iota
	deliveryRetiredPTO
)

type retainedDelivery struct {
	packet  congestion.PacketInfo
	expires monotime.Time
	index   int
}
type retainedDeliveryHeap []*retainedDelivery

func (q retainedDeliveryHeap) Len() int           { return len(q) }
func (q retainedDeliveryHeap) Less(i, j int) bool { return q[i].packet.Ordinal < q[j].packet.Ordinal }

func (q retainedDeliveryHeap) Swap(i, j int) {
	q[i], q[j] = q[j], q[i]
	q[i].index = i
	q[j].index = j
}

func (q *retainedDeliveryHeap) Push(v any) {
	r := v.(*retainedDelivery)
	r.index = len(*q)
	*q = append(*q, r)
}

func (q *retainedDeliveryHeap) Pop() any {
	a := *q
	r := a[len(a)-1]
	a[len(a)-1] = nil
	*q = a[:len(a)-1]
	return r
}

func (s *deliverySampler) dispose(p congestion.PacketInfo) {
	if p.AckEliciting && !p.PathProbe {
		s.outstanding -= p.Length
	}
}

func (d *congestionDispatch) removeRetained(key congestionPacketKey, dispose bool) {
	r := d.sampler.retained[key]
	if r == nil {
		return
	}
	if dispose {
		d.recovery.missing(r.packet.Ordinal)
		d.sampler.dispose(r.packet)
	}
	heap.Remove(&d.sampler.order, r.index)
	delete(d.sampler.retained, key)
	if len(d.sampler.retained) == 0 {
		d.sampler.nextExpiry = 0
	}
}

func (d *congestionDispatch) retire(key congestionPacketKey, now monotime.Time, pto time.Duration, reason deliveryRetirement) {
	p, ok := d.packets[key]
	if !ok {
		return
	}
	delete(d.packets, key)
	if !p.AckEliciting || p.PathProbe {
		return
	}
	d.expire(now)
	s := &d.sampler
	if len(s.order) == maxDeliveryRetained {
		old := s.order[0].packet
		d.removeRetained(congestionKey(old.EncryptionLevel, old.PacketNumber), true)
		s.evicted++
		s.evidenceLost = true
	}
	if s.retained == nil {
		s.retained = make(map[congestionPacketKey]*retainedDelivery)
		s.order = make(retainedDeliveryHeap, 0, maxDeliveryRetained)
	}
	// Clamp before multiplication, including unusual restored PTO estimates.
	ttl := 3 * min(max(pto, 0), 10*time.Second)
	p.Retirement = congestion.DeliveryLost
	if reason == deliveryRetiredPTO {
		p.Retirement = congestion.DeliveryPTO
	}
	r := &retainedDelivery{packet: p, expires: now.Add(ttl)}
	s.retained[key] = r
	heap.Push(&s.order, r)
	if s.nextExpiry.IsZero() || r.expires.Before(s.nextExpiry) {
		s.nextExpiry = r.expires
	}
}

func (d *congestionDispatch) expire(now monotime.Time) {
	if d.sampler.nextExpiry.IsZero() || now.Before(d.sampler.nextExpiry) {
		return
	}
	var next monotime.Time
	for key, r := range d.sampler.retained {
		if !r.expires.After(now) {
			d.removeRetained(key, true)
			d.sampler.expired++
			d.sampler.evidenceLost = true
		} else if next.IsZero() || r.expires.Before(next) {
			next = r.expires
		}
	}
	d.sampler.nextExpiry = next
}

// DeliveryExpiry is independent of send permission and recovery's loss alarm.
func (h *sentPacketHandler) DeliveryExpiry() monotime.Time {
	if d := h.congestionEvents; d != nil {
		return d.sampler.nextExpiry
	}
	return 0
}

func (h *sentPacketHandler) ExpireDelivery(now monotime.Time) {
	if d := h.congestionEvents; d != nil {
		d.expire(now)
	}
}

func (h *sentPacketHandler) CloseDelivery() {
	if b, ok := h.congestion.(*congestion.BBRSender); ok {
		b.Close()
	}
	h.congestionEvents = nil
	if h.bbrECN != nil {
		h.bbrECN.closed = true
		h.bbrECN.ranges = nil
		h.bbrECN.path = nil
	}
}

func (d *congestionDispatch) pendingBytes() protocol.ByteCount {
	if d.pending == nil {
		return -1 // unavailable authority cannot establish zero pending work
	}
	return d.pending()
}

func (h *sentPacketHandler) DeliveryIdle() bool {
	d := h.congestionEvents
	return d != nil && d.sampler.applicationExhausted && !d.sampler.evidenceLost && h.bytesInFlight == 0 && d.sampler.outstanding == 0 && d.pendingBytes() == 0
}

func (h *sentPacketHandler) DeliveryStats() congestion.DeliveryStats {
	d := h.congestionEvents
	if d == nil {
		return congestion.DeliveryStats{}
	}
	s := &d.sampler
	return congestion.DeliveryStats{
		OutcomeEntries: d.recovery.count, OutcomeEvicted: d.recovery.evicted,
		Live: len(d.packets), Retained: len(s.retained), Outstanding: s.outstanding,
		RecordBytes: uintptr(cap(d.recovery.outcomes))*unsafe.Sizeof(recoveryOutcome{}) + uintptr(len(d.packets)+cap(d.scratch))*unsafe.Sizeof(congestion.PacketInfo{}) + uintptr(len(s.retained))*unsafe.Sizeof(retainedDelivery{}) + uintptr(cap(s.order))*unsafe.Sizeof((*retainedDelivery)(nil)),
		Evicted:     s.evicted, Expired: s.expired, Missing: s.missing, Stop: s.stop, Idle: h.DeliveryIdle(),
	}
}

func (h *sentPacketHandler) ObserveDeliveryLimitation(reason congestion.SendLimitation) {
	d := h.congestionEvents
	if d == nil {
		return
	}
	s := &d.sampler
	s.stop = reason
	s.applicationExhausted = reason == congestion.SendApplicationLimited
	if reason == congestion.SendApplicationLimited || reason == congestion.SendFlowControlLimited || reason == congestion.SendProbeRTTLimited {
		s.markLimited(reason)
	}
	if b, ok := h.congestion.(*congestion.BBRSender); ok {
		b.ObserveLimitation(reason)
	}
}

// Intentional probe limitation does not replace the observed supply reason;
// in particular it cannot manufacture or erase genuine application idle.
func (s *deliverySampler) markLimited(reason congestion.SendLimitation) {
	s.limitedUntil = addDelivery(s.delivered, uint64(max(s.outstanding, 1)))
	s.limited = reason
}

func addDelivery(a, b uint64) uint64 {
	if b > math.MaxUint64-a {
		return math.MaxUint64
	}
	return a + b
}

func deliveryRate(n uint64, interval time.Duration) uint64 {
	hi, lo := bits.Mul64(n, uint64(time.Second))
	if hi >= uint64(interval) {
		return math.MaxUint64
	}
	rate, _ := bits.Div64(hi, lo, uint64(interval))
	return rate
}

func (s *deliverySampler) sent(info *congestion.PacketInfo, prior, post protocol.ByteCount) {
	if !info.AckEliciting || info.PathProbe {
		return
	}
	if s.sendOrigin.IsZero() {
		s.originlessRegistrations = true
	}
	info.Delivery = congestion.DeliverySnapshot{
		Delivered: s.delivered, Lost: s.lost, DeliveredTime: s.deliveredTime, SendOrigin: s.sendOrigin,
		PriorInFlight: prior, PostInFlight: post, Outstanding: s.outstanding,
		Valid: info.Length > 0 && info.RegistrationValid && !s.sendOrigin.IsZero() && !info.SendTime.Before(s.sendOrigin),
	}
	if s.limitedUntil != 0 && s.delivered <= s.limitedUntil {
		info.Delivery.Limited = s.limited
	}
	s.outstanding += info.Length
	s.applicationExhausted = false
}

func (s *deliverySampler) feedback(e *congestion.FeedbackEvent) {
	if e.Time.Before(s.deliveredTime) {
		e.RawRTT = 0
	}
	if e.RawRTT > 0 && (s.minimumRTT == 0 || e.RawRTT < s.minimumRTT) {
		s.minimumRTT = e.RawRTT
	}
	for _, p := range e.Lost {
		if p.PathGeneration == e.PathGeneration && p.SampleGeneration == e.SampleGeneration {
			s.lost = addDelivery(s.lost, uint64(p.Length))
		}
	}
	var anchor *congestion.PacketInfo
	deliveredBefore := s.delivered
	for i := range e.Acked {
		p := &e.Acked[i]
		if !p.AckEliciting || p.PathProbe {
			continue
		}
		s.outstanding -= p.Length
		if p.PathGeneration != e.PathGeneration || p.SampleGeneration != e.SampleGeneration {
			continue
		}
		s.delivered = addDelivery(s.delivered, uint64(p.Length))
		if !p.MTUProbe && (anchor == nil || p.Ordinal > anchor.Ordinal) {
			anchor = p
		}
	}
	e.Delivery = congestion.DeliverySample{Delivered: s.delivered, Lost: s.lost, RawRTT: e.RawRTT}
	if anchor == nil {
		if s.delivered != deliveredBefore {
			s.deliveredTime = max(s.deliveredTime, e.Time)
		}
		return
	}
	e.Delivery.Ordinal = anchor.Ordinal
	d := anchor.Delivery
	interval := max(anchor.SendTime.Sub(d.SendOrigin), e.Time.Sub(d.DeliveredTime))
	e.Delivery.Interval, e.Delivery.Limited = interval, d.Limited
	if d.Valid && interval > 0 && interval >= s.minimumRTT && e.Time.After(anchor.SendTime) && !e.Time.Before(s.deliveredTime) && s.delivered >= d.Delivered && s.delivered < math.MaxUint64 {
		e.Delivery.BytesPerSecond = deliveryRate(s.delivered-d.Delivered, interval)
		e.Delivery.Valid = true
	}
	s.deliveredTime = max(s.deliveredTime, e.Time)
	if d.Valid {
		s.sendOrigin = max(s.sendOrigin, anchor.SendTime)
	}
}
