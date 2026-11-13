package ackhandler

import (
	"container/heap"
	"maps"
	"math"
	"math/bits"
	"slices"
	"time"
	"unsafe"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

// deliverySampler is used only by the private rich dispatcher. Recovery owns
// frames and flight; this state owns only copied delivery evidence.
type deliverySampler struct {
	timingGeneration          uint64
	delivered, lost           uint64
	deliveredTime, sendOrigin monotime.Time
	outstanding               protocol.ByteCount
	minimumRTT                time.Duration
	retained                  map[congestionPacketKey]*retainedDelivery
	order                     retainedDeliveryHeap
	expiry                    retainedExpiryHeap
	covered                   *retainedDelivery // root of the (space, packet number) index
	evicted, expired, missing uint64
	limitedUntil              uint64
	limited, stop             congestion.SendLimitation
	applicationExhausted      bool
	evidenceLost              bool
	nextExpiry                monotime.Time
	originlessRegistrations   bool
	// timingFenced is set from a timing fence until a fresh origin. Restoring
	// that origin fences timing only; it never starts a new delivery generation.
	timingFenced bool
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
	packet             congestion.PacketInfo
	key                congestionPacketKey
	expires            monotime.Time
	index, expiryIndex int
	left, right        *retainedDelivery
	height             int8
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
	countNodes(2 * bits.Len(uint(len(d.sampler.order)))) // ordinal heap removal, charged at its sift bound
	heap.Remove(&d.sampler.order, r.index)
	d.sampler.expiry.remove(r.expiryIndex)
	d.sampler.covered = retainedDelete(d.sampler.covered, key)
	delete(d.sampler.retained, key)
	if len(d.sampler.retained) == 0 {
		d.sampler.nextExpiry = 0
	}
}

func (d *congestionDispatch) retire(key congestionPacketKey, now monotime.Time, pto time.Duration, reason deliveryRetirement) {
	p, ok := d.packets.take(key)
	if !ok {
		return
	}
	if !p.AckEliciting || p.PathProbe {
		return
	}
	d.expire(now)
	s := &d.sampler
	if _, retained := d.deliveryUse(); retained >= maxDeliveryRetained {
		s.evicted++
		s.evidenceLost = true
		if len(s.order) == 0 {
			// The ceiling is shared; another context's records are not ours to evict.
			d.recovery.missing(p.Ordinal)
			d.sampler.dispose(p)
			return
		}
		old := s.order[0].packet
		d.removeRetained(congestionKey(old.EncryptionLevel, old.PacketNumber), true)
	}
	if s.retained == nil {
		s.retained = make(map[congestionPacketKey]*retainedDelivery)
		// Contexts sharing the allowance grow on demand rather than each
		// reserving all of it.
		if d.root == nil {
			s.order = make(retainedDeliveryHeap, 0, maxDeliveryRetained)
			s.expiry = make(retainedExpiryHeap, 0, maxDeliveryRetained)
		}
	}
	d.reserveRetained()
	// Clamp before multiplication, including unusual restored PTO estimates.
	ttl := 3 * min(max(pto, 0), 10*time.Second)
	p.Retirement = congestion.DeliveryLost
	if reason == deliveryRetiredPTO {
		p.Retirement = congestion.DeliveryPTO
	}
	r := &retainedDelivery{packet: p, key: key, expires: now.Add(ttl)}
	s.retained[key] = r
	heap.Push(&s.order, r)
	s.expiry.push(r)
	s.covered = retainedInsert(s.covered, r)
	if s.nextExpiry.IsZero() || r.expires.Before(s.nextExpiry) {
		s.nextExpiry = r.expires
	}
}

// expire keeps the exposed deadline's existing behavior: a deadline left stale
// by ACK or disposal of the earliest record still fires, then advances to the
// earliest remaining expiry.
func (d *congestionDispatch) expire(now monotime.Time) {
	if d.sampler.nextExpiry.IsZero() || now.Before(d.sampler.nextExpiry) {
		return
	}
	prior := beginServiceWork(serviceRetainedExpiry)
	var next monotime.Time
	for {
		expires, ok := d.sampler.expiry.earliest()
		if !ok {
			break
		}
		countInspected()
		if expires.After(now) {
			countFailed()
			next = expires
			break
		}
		d.removeRetained(d.sampler.expiry[0].key, true)
		d.sampler.expired++
		d.sampler.evidenceLost = true
	}
	d.sampler.nextExpiry = next
	endServiceWork(prior)
}

// A rich root and its paths share one context's delivery allowance: the
// occupancy ceilings above, and backing storage within a quarter of them for
// allocation slack. Before a context grows a store past the allowance, its
// peers give back the capacity they hold beyond their records.
const (
	deliveryRecordAllowance   = maxDeliveryLive + maxDeliveryLive/4
	deliveryRetainedAllowance = maxDeliveryRetained + maxDeliveryRetained/4
)

// eachPeer visits the other contexts sharing d's allowance.
func (d *congestionDispatch) eachPeer(fn func(*congestionDispatch)) {
	if p := d.root.congestionEvents; p != nil && p != d {
		fn(p)
	}
	for _, path := range d.root.paths {
		if p := path.congestionEvents; p != nil && p != d {
			fn(p)
		}
	}
}

// reserveRecord makes room for one new live record. Live occupancy is already
// below the shared ceiling, so a compacted peer set always leaves room.
func (d *congestionDispatch) reserveRecord() {
	if d.root == nil || !d.packets.slabFull() {
		return
	}
	size := max(minDeliverySlots, 2*cap(d.packets.recs))
	peers := 0
	d.eachPeer(func(p *congestionDispatch) { peers += cap(p.packets.recs) })
	if size+peers > deliveryRecordAllowance {
		peers = 0
		d.eachPeer(func(p *congestionDispatch) {
			p.packets.compact()
			peers += cap(p.packets.recs)
		})
	}
	d.packets.growSlab(min(size, deliveryRecordAllowance-peers))
}

// reserveRetained makes room for one new retained record, as reserveRecord does.
func (d *congestionDispatch) reserveRetained() {
	s := &d.sampler
	if d.root == nil || len(s.order) < cap(s.order) {
		return
	}
	size := max(minDeliverySlots, 2*cap(s.order))
	peers := 0
	d.eachPeer(func(p *congestionDispatch) { peers += cap(p.sampler.order) })
	if size+peers > deliveryRetainedAllowance {
		peers = 0
		d.eachPeer(func(p *congestionDispatch) {
			p.sampler.compactRetained()
			peers += cap(p.sampler.order)
		})
	}
	size = min(size, deliveryRetainedAllowance-peers)
	order, expiry := make(retainedDeliveryHeap, len(s.order), size), make(retainedExpiryHeap, len(s.expiry), size)
	copy(order, s.order)
	copy(expiry, s.expiry)
	s.order, s.expiry = order, expiry
}

// compactRetained gives back retained capacity beyond the records. Records keep
// their heap positions; only the backing storage is replaced.
func (s *deliverySampler) compactRetained() {
	if len(s.retained) == 0 {
		s.retained, s.order, s.expiry = nil, nil, nil
		return
	}
	if cap(s.order) == len(s.order) {
		return
	}
	s.order, s.expiry = slices.Clone(s.order), slices.Clone(s.expiry)
	retained := make(map[congestionPacketKey]*retainedDelivery, len(s.retained))
	maps.Copy(retained, s.retained)
	s.retained = retained
}

// deliveryUse is the live and retained record count under this context's
// ceiling: its own, or the sum over a rich root and its paths.
func (d *congestionDispatch) deliveryUse() (live, retained int) {
	if d.root == nil {
		return d.packets.len(), len(d.sampler.retained)
	}
	add := func(p *congestionDispatch) {
		if p != nil {
			live += p.packets.len()
			retained += len(p.sampler.retained)
		}
	}
	add(d.root.congestionEvents)
	for _, path := range d.root.paths {
		add(path.congestionEvents)
	}
	return live, retained
}

// releasePeerScratch keeps the contexts' feedback buffers, together with one
// of the given size, within a single context's ceiling. A peer's buffer is idle
// outside its own synchronous feedback event.
func (d *congestionDispatch) releasePeerScratch(size int) {
	if d.root == nil {
		return
	}
	total := size
	visit := func(p *congestionDispatch, release bool) {
		if p == nil || p == d {
			return
		}
		if release {
			p.scratch, p.event.Acked, p.event.Lost = nil, nil, nil
		} else {
			total += cap(p.scratch)
		}
	}
	for _, release := range []bool{false, true} {
		if release && total <= maxDeliveryLive+maxDeliveryRetained {
			return
		}
		visit(d.root.congestionEvents, release)
		for _, path := range d.root.paths {
			visit(path.congestionEvents, release)
		}
	}
}

// DeliveryExpiry is independent of send permission and recovery's loss alarm.
// A root reports the earliest deadline of all its contexts.
func (h *sentPacketHandler) DeliveryExpiry() monotime.Time {
	var next monotime.Time
	if d := h.congestionEvents; d != nil {
		next = d.sampler.nextExpiry
	}
	for _, path := range h.paths {
		if d := path.congestionEvents; d != nil && !d.sampler.nextExpiry.IsZero() && (next.IsZero() || d.sampler.nextExpiry.Before(next)) {
			next = d.sampler.nextExpiry
		}
	}
	return next
}

func (h *sentPacketHandler) ExpireDelivery(now monotime.Time) {
	if d := h.congestionEvents; d != nil {
		d.expire(now)
	}
	for _, path := range h.paths {
		if d := path.congestionEvents; d != nil {
			d.expire(now)
		}
	}
}

// CloseDelivery closes this context's delivery state and, on a root, every path's.
func (h *sentPacketHandler) CloseDelivery() {
	for _, path := range h.paths {
		path.CloseDelivery()
	}
	if b, ok := h.congestion.(*congestion.BBRSender); ok {
		b.Close()
	}
	h.congestionEvents = nil
	if h.bbrECN != nil {
		h.bbrECN.closed = true
		h.bbrECN.ranges, h.bbrECN.scratch = nil, nil
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
		Live: d.packets.len(), Retained: len(s.retained), Outstanding: s.outstanding,
		RecordBytes: d.recovery.recordBytes() + uintptr(d.packets.len()+cap(d.scratch))*unsafe.Sizeof(congestion.PacketInfo{}) + uintptr(len(s.retained))*unsafe.Sizeof(retainedDelivery{}) + uintptr(cap(s.order)+cap(s.expiry))*unsafe.Sizeof((*retainedDelivery)(nil)),
		Evicted:     s.evicted, Expired: s.expired, Missing: s.missing, Stop: s.stop, Idle: h.DeliveryIdle(),
	}
}

// ObserveDeliveryStop records an opportunity's stop and any application
// exhaustion, which genuine idle requires, without establishing supply
// limitation: emission reports that once a supply gap costs sending time.
func (h *sentPacketHandler) ObserveDeliveryStop(reason congestion.SendLimitation) {
	d := h.congestionEvents
	if d == nil {
		return
	}
	d.sampler.stop = reason
	d.sampler.applicationExhausted = reason == congestion.SendApplicationLimited
}

func (h *sentPacketHandler) ObserveDeliveryLimitation(reason congestion.SendLimitation) {
	d := h.congestionEvents
	if d == nil {
		return
	}
	h.ObserveDeliveryStop(reason)
	s := &d.sampler
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

// activeLimitation is the marker a packet registered now carries, SendUnknown
// once delivery passes the boundary even though limited keeps the last reason.
func (s *deliverySampler) activeLimitation() congestion.SendLimitation {
	if s.limitedUntil != 0 && s.delivered <= s.limitedUntil {
		return s.limited
	}
	return congestion.SendUnknown
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
		Valid: info.Length > 0 && info.RegistrationValid && !s.sendOrigin.IsZero() && !s.deliveredTime.IsZero() && !info.SendTime.Before(s.sendOrigin),
	}
	info.Delivery.Limited = s.activeLimitation()
	s.outstanding += info.Length
	s.applicationExhausted = false
}

// contributesDelivery is shared by logical accounting and receipt-derived timing
// qualification. A receipt outside this numerator cannot supply its send bound.
func contributesDelivery(p congestion.PacketInfo, e *congestion.FeedbackEvent) bool {
	return p.AckEliciting && !p.PathProbe && p.PathGeneration == e.PathGeneration && p.SampleGeneration == e.SampleGeneration
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
		if p.AckEliciting && !p.PathProbe {
			s.outstanding -= p.Length
		}
		if !contributesDelivery(*p, e) {
			continue
		}
		s.delivered = addDelivery(s.delivered, uint64(p.Length))
		if !p.MTUProbe && !p.TimingExcluded && p.TimingGeneration == s.timingGeneration && (anchor == nil || p.Ordinal > anchor.Ordinal) {
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
