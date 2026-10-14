package ackhandler

// The frozen reference below is copied from component
// e4f322cbbfd4225a4b714e08ec19c958cccadcb0 (internal/ackhandler/bbr_recovery.go,
// delivery_sampler.go and the retained paths of congestion_dispatch.go) with
// renamed types. It owns its own state and shares no storage with the
// candidate; only stateless constants and arithmetic helpers are shared.
// markLimited is omitted: the reference drives no limitation observations.

import (
	"cmp"
	"container/heap"
	"maps"
	"math"
	"slices"
	"time"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
)

type frozenOutcome struct {
	key             congestionPacketKey
	level           protocol.EncryptionLevel
	ordinal         uint64
	sent            monotime.Time
	state           recoveryOutcomeState
	endpoint        bool
	receiptEligible bool
	ptoRetired      bool
}

// frozenRecovery is connection-owned. The ring includes every registration,
// even ACK-only packets and excluded probes. Dropping its oldest entry never
// creates a summary that could bridge unknown history.
type frozenRecovery struct {
	outcomes        []frozenOutcome
	keys            map[congestionPacketKey]int
	head, count     int
	evicted         uint64
	measured        bool
	reported        uint64
	episode         congestion.RecoveryEpisode
	members         map[uint64]struct{}
	ackOrdinal      uint64
	unconfirmedLoss bool
	// Packet numbers are monotonic within each of the three spaces. These
	// boundary witnesses survive optional sampler and outcome-ring eviction.
	latestPackets   map[protocol.EncryptionLevel]protocol.PacketNumber
	boundaryPackets map[protocol.EncryptionLevel]protocol.PacketNumber
}

func (r *frozenRecovery) sent(p congestion.PacketInfo, generation uint64) {
	if r.outcomes == nil {
		r.outcomes = make([]frozenOutcome, maxRecoveryOutcomes)
		r.keys = make(map[congestionPacketKey]int)
		r.latestPackets = make(map[protocol.EncryptionLevel]protocol.PacketNumber, 3)
	}
	index := (r.head + r.count) % maxRecoveryOutcomes
	if r.count == maxRecoveryOutcomes {
		r.evicted++
		r.missing(r.outcomes[index].ordinal)
		delete(r.keys, r.outcomes[index].key)
		r.head = (r.head + 1) % maxRecoveryOutcomes
	} else {
		r.count++
	}
	o := frozenOutcome{
		key: congestionKey(p.EncryptionLevel, p.PacketNumber), level: p.EncryptionLevel, ordinal: p.Ordinal, sent: p.SendTime,
		// Path/Retry reset clears the ledger; disposal prevents readmission.
		receiptEligible: currentPathRecoveryReceipt(p, generation),
		endpoint:        r.measured && p.RegistrationValid && p.AckEliciting && !p.PathProbe && !p.MTUProbe,
	}
	if !p.RegistrationValid || p.PathProbe || p.MTUProbe {
		o.state = outcomeExcluded
	}
	r.outcomes[index] = o
	r.keys[o.key] = index
	r.latestPackets[o.key.space] = o.key.number
}

func (r *frozenRecovery) lost(key congestionPacketKey, congestionLoss, retained bool, boundary uint64) {
	i, known := r.keys[key]
	if known && r.outcomes[i].state == outcomeUnresolved {
		r.outcomes[i].state = outcomeLost
		r.unconfirmedLoss = true // ACK-only losses can complete a classified span too.
	}
	if !congestionLoss {
		return
	}
	r.unconfirmedLoss = true
	if !r.episode.Active {
		r.episode = congestion.RecoveryEpisode{ID: r.episode.ID + 1, Boundary: boundary, Active: true, Entered: true, UndoPossible: true}
		r.members = make(map[uint64]struct{})
		r.boundaryPackets = maps.Clone(r.latestPackets)
	}
	// Loss ordering is a transport fact, independent of retained delivery
	// evidence. A per-space packet-number witness proves which side of the
	// frozen ordinal boundary this transmission occupies without guessing its
	// missing ordinal from the newest registration.
	if pn, ok := r.boundaryPackets[key.space]; !ok || key.number > pn {
		r.episode.Boundary = boundary
		r.boundaryPackets = maps.Clone(r.latestPackets)
	}
	if !known || !retained {
		r.invalidateUndo()
		return
	}
	ordinal := r.outcomes[i].ordinal
	if r.episode.UndoPossible {
		if len(r.members) == maxDeliveryRetained {
			r.invalidateUndo()
		} else {
			r.members[ordinal] = struct{}{}
		}
	}
}

func (r *frozenRecovery) invalidateUndo() {
	r.episode.UndoPossible = false
	r.members = nil
}

func (r *frozenRecovery) missing(ordinal uint64) {
	if _, ok := r.members[ordinal]; ok || (r.episode.ID != 0 && ordinal == r.episode.Boundary) {
		r.invalidateUndo()
	}
}

func (r *frozenRecovery) discard(key congestionPacketKey) {
	if i, ok := r.keys[key]; ok {
		r.outcomes[i].state = outcomeDisposed
	}
}

// PTO extraction transfers frame/flight ownership without declaring a loss.
// This fact survives optional delivery retention and expiry.
func (r *frozenRecovery) retirePTO(key congestionPacketKey) {
	if i, ok := r.keys[key]; ok && r.outcomes[i].state == outcomeUnresolved {
		r.outcomes[i].ptoRetired = true
	}
}

// confirmPTO changes only persistent-span evidence. Ordinary loss callbacks,
// delivery loss totals and episode membership belong to lost, not this path.
func (r *frozenRecovery) confirmPTO(space protocol.EncryptionLevel, witness protocol.PacketNumber, cutoff monotime.Time) {
	if witness == protocol.InvalidPacketNumber {
		return
	}
	for i := 0; i < r.count; i++ {
		o := &r.outcomes[(r.head+i)%maxRecoveryOutcomes]
		if o.key.space == space && o.key.number < witness && o.ptoRetired && o.state == outcomeUnresolved && !o.sent.After(cutoff) {
			o.state = outcomeLost
			r.unconfirmedLoss = true
		}
	}
}

func (r *frozenRecovery) discardSpace(level protocol.EncryptionLevel) {
	for i := 0; i < r.count; i++ {
		o := &r.outcomes[(r.head+i)%maxRecoveryOutcomes]
		if o.level == level {
			r.missing(o.ordinal)
			o.state = outcomeDisposed
		}
	}
}

func (r *frozenRecovery) reset() {
	// Connection-wide episode identities, like transmission ordinals, never
	// repeat; every path/Retry reset abandons the old evidence and RTT eligibility.
	*r = frozenRecovery{episode: congestion.RecoveryEpisode{ID: r.episode.ID}}
}

func (r *frozenRecovery) ack(ack *wire.AckFrame, level protocol.EncryptionLevel) protocol.PacketNumber {
	r.ackOrdinal = 0
	space := congestionKey(level, 0).space
	witness := protocol.InvalidPacketNumber
	for i := 0; i < r.count; i++ {
		o := &r.outcomes[(r.head+i)%maxRecoveryOutcomes]
		if o.key.space == space && ack.AcksPacket(o.key.number) && o.state != outcomeDisposed {
			if o.receiptEligible {
				// Duplicate receipts may confirm PTO outcomes after an earlier
				// ACK arrived before the time threshold. They add no RTT progress.
				witness = max(witness, o.key.number)
				if o.state != outcomeAcked {
					r.ackOrdinal = max(r.ackOrdinal, o.ordinal)
				}
			}
			o.state = outcomeAcked
		}
	}
	return witness
}

func (r *frozenRecovery) feedback(e *congestion.FeedbackEvent, pto time.Duration) {
	for _, p := range e.Acked {
		// Retained current-path receipt metadata remains a valid ordinal witness
		// even after the independent persistent-congestion ring evicted it.
		if currentPathRecoveryReceipt(p, e.PathGeneration) {
			r.ackOrdinal = max(r.ackOrdinal, p.Ordinal)
			delete(r.members, p.Ordinal)
		}
	}
	if r.episode.Active && r.ackOrdinal > r.episode.Boundary {
		r.episode.Active = false
		r.episode.Exited = true
	}
	// An active episode can still acquire losses from unresolved transmissions.
	// Publish its one-shot repair only after the boundary has been crossed.
	if !r.episode.Active && r.episode.UndoPossible && len(r.members) == 0 {
		r.episode.UndoEligible = true
		r.episode.UndoPossible = false
	}
	defer func() {
		r.episode.Pending = len(r.members)
		e.RecoveryEpisode = r.episode
		r.ackOrdinal = 0
		r.episode.Entered, r.episode.Exited, r.episode.UndoEligible = false, false, false
	}()

	if e.RTTUpdated {
		r.measured = true
	}
	if !e.HasAck {
		return
	}
	r.unconfirmedLoss = false
	// RFC 9002 section 7.6 includes max_ack_delay in every space, with no
	// exponential PTO backoff. Avoid overflow for unusual restored estimates.
	if pto <= 0 || pto > time.Duration(1<<63-1)/3 {
		return
	}
	var first *frozenOutcome
	for i := 0; i < r.count; i++ {
		o := &r.outcomes[(r.head+i)%maxRecoveryOutcomes]
		if o.state != outcomeLost {
			first = nil
			continue
		}
		if !o.endpoint {
			continue
		}
		if first == nil {
			first = o
			continue
		}
		if o.ordinal > r.reported && o.sent.Sub(first.sent) > 3*pto {
			e.PersistentCongestion = congestion.PersistentCongestion{StartOrdinal: first.ordinal, EndOrdinal: o.ordinal}
		}
	}
	if e.PersistentCongestion.EndOrdinal != 0 {
		r.reported = e.PersistentCongestion.EndOrdinal
		r.invalidateUndo()
		r.episode.UndoEligible = false
	}
}

// ACK-only or duplicate receipts can confirm losses accumulated by a timer.
// Empty events without new recovery evidence retain the existing suppression.
func (r *frozenRecovery) needsAckFeedback() bool {
	return r.unconfirmedLoss || (r.episode.Active && r.ackOrdinal > r.episode.Boundary)
}

type frozenSampler struct {
	delivered, lost           uint64
	deliveredTime, sendOrigin monotime.Time
	outstanding               protocol.ByteCount
	minimumRTT                time.Duration
	retained                  map[congestionPacketKey]*frozenRetained
	order                     frozenRetainedHeap
	evicted, expired, missing uint64
	limitedUntil              uint64
	limited, stop             congestion.SendLimitation
	applicationExhausted      bool
	evidenceLost              bool
	nextExpiry                monotime.Time
	originlessRegistrations   bool
}

type frozenRetained struct {
	packet  congestion.PacketInfo
	expires monotime.Time
	index   int
}

type frozenRetainedHeap []*frozenRetained

func (q frozenRetainedHeap) Len() int           { return len(q) }
func (q frozenRetainedHeap) Less(i, j int) bool { return q[i].packet.Ordinal < q[j].packet.Ordinal }

func (q frozenRetainedHeap) Swap(i, j int) {
	q[i], q[j] = q[j], q[i]
	q[i].index = i
	q[j].index = j
}

func (q *frozenRetainedHeap) Push(v any) {
	r := v.(*frozenRetained)
	r.index = len(*q)
	*q = append(*q, r)
}

func (q *frozenRetainedHeap) Pop() any {
	a := *q
	r := a[len(a)-1]
	a[len(a)-1] = nil
	*q = a[:len(a)-1]
	return r
}

func (s *frozenSampler) dispose(p congestion.PacketInfo) {
	if p.AckEliciting && !p.PathProbe {
		s.outstanding -= p.Length
	}
}

func (d *frozenDispatch) removeRetained(key congestionPacketKey, dispose bool) {
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

func (d *frozenDispatch) retire(key congestionPacketKey, now monotime.Time, pto time.Duration, reason deliveryRetirement) {
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
		s.retained = make(map[congestionPacketKey]*frozenRetained)
		s.order = make(frozenRetainedHeap, 0, maxDeliveryRetained)
	}
	// Clamp before multiplication, including unusual restored PTO estimates.
	ttl := 3 * min(max(pto, 0), 10*time.Second)
	p.Retirement = congestion.DeliveryLost
	if reason == deliveryRetiredPTO {
		p.Retirement = congestion.DeliveryPTO
	}
	r := &frozenRetained{packet: p, expires: now.Add(ttl)}
	s.retained[key] = r
	heap.Push(&s.order, r)
	if s.nextExpiry.IsZero() || r.expires.Before(s.nextExpiry) {
		s.nextExpiry = r.expires
	}
}

func (d *frozenDispatch) expire(now monotime.Time) {
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

func (s *frozenSampler) sent(info *congestion.PacketInfo, prior, post protocol.ByteCount) {
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

func (s *frozenSampler) feedback(e *congestion.FeedbackEvent) {
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

// frozenDispatch holds the frozen dispatch state that the changed service
// paths read or write. Its methods copy the frozen handler hooks, with the
// handler's RTT-derived inputs supplied explicitly by the caller.
type frozenDispatch struct {
	sampler          frozenSampler
	recovery         frozenRecovery
	ordinal          uint64
	registrationTime monotime.Time
	pathGeneration   uint64
	sampleGeneration uint64
	packets          map[congestionPacketKey]congestion.PacketInfo
	event            congestion.FeedbackEvent
}

func newFrozenDispatch() *frozenDispatch {
	return &frozenDispatch{packets: make(map[congestionPacketKey]congestion.PacketInfo)}
}

// send copies captureCongestionSend without ECN or BBR-specific marking.
func (d *frozenDispatch) send(pn protocol.PacketNumber, p *packet, prior, post, pending protocol.ByteCount) congestion.PacketInfo {
	d.ordinal++
	d.expire(p.SendTime)
	registrationValid := !p.SendTime.IsZero() && !p.SendTime.Before(d.registrationTime)
	if registrationValid && p.IsAckEliciting() && !p.isPathProbePacket && pending == 0 &&
		(d.sampler.sendOrigin.IsZero() || (prior == 0 && d.sampler.outstanding == 0)) {
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
		ECN:               protocol.ECNNon,
	}
	d.recovery.sent(info, d.pathGeneration)
	d.registrationTime = max(d.registrationTime, p.SendTime)
	if len(d.packets) < maxDeliveryLive {
		d.sampler.sent(&info, prior, post)
		d.packets[congestionKey(p.EncryptionLevel, pn)] = info
	} else {
		d.sampler.missing++
		d.sampler.evidenceLost = true
	}
	return info
}

func (d *frozenDispatch) begin(now monotime.Time, level protocol.EncryptionLevel, prior protocol.ByteCount, acked []packetWithPacketNumber, largest protocol.PacketNumber) {
	d.event = congestion.FeedbackEvent{
		PathGeneration:   d.pathGeneration,
		SampleGeneration: d.sampleGeneration,
		Time:             now,
		Space:            congestionKey(level, 0).space,
		HasAck:           largest != protocol.InvalidPacketNumber,
		LargestAcked:     largest,
		PriorInFlight:    prior,
	}
	for _, p := range acked {
		key := congestionKey(p.EncryptionLevel, p.PacketNumber)
		if info, ok := d.packets[key]; ok {
			d.event.Acked = append(d.event.Acked, info)
		}
		delete(d.packets, key)
	}
}

// loss copies captureCongestionLoss with the level's PTO supplied explicitly.
func (d *frozenDispatch) loss(level protocol.EncryptionLevel, pn protocol.PacketNumber, p *packet, pto time.Duration) {
	key := congestionKey(level, pn)
	_, retained := d.packets[key]
	d.recovery.lost(key, p.includedInBytesInFlight && !p.IsPathMTUProbePacket && !p.isPathProbePacket, retained, d.ordinal)
	if info, ok := d.packets[key]; ok && p.includedInBytesInFlight && !p.IsPathMTUProbePacket && !p.isPathProbePacket {
		d.event.Lost = append(d.event.Lost, info)
	}
	d.retire(key, d.event.Time, pto, deliveryRetiredLoss)
}

func (d *frozenDispatch) appendRetainedAck(ack *wire.AckFrame, level protocol.EncryptionLevel) protocol.PacketNumber {
	witness := d.recovery.ack(ack, level)
	for key, r := range d.sampler.retained {
		if key.space == congestionKey(level, 0).space && ack.AcksPacket(key.number) {
			d.event.Acked = append(d.event.Acked, r.packet)
			d.removeRetained(key, false)
		}
	}
	slices.SortFunc(d.event.Acked, func(a, b congestion.PacketInfo) int { return cmp.Compare(a.Ordinal, b.Ordinal) })
	for _, p := range d.event.Acked {
		if currentPathRecoveryReceipt(p, d.pathGeneration) {
			witness = max(witness, p.PacketNumber)
		}
	}
	return witness
}

// finish copies the controller-independent part of finishCongestionFeedback.
func (d *frozenDispatch) finish(smoothed time.Duration, post, pending protocol.ByteCount, pto time.Duration) (congestion.FeedbackEvent, bool) {
	d.event.SmoothedRTT = smoothed
	d.event.PostInFlight = post
	d.event.PendingLocal = pending
	slices.SortFunc(d.event.Lost, func(a, b congestion.PacketInfo) int { return cmp.Compare(a.Ordinal, b.Ordinal) })
	d.sampler.feedback(&d.event)
	d.recovery.feedback(&d.event, pto)
	emitted := d.event.HasAck || len(d.event.Lost) > 0 || d.event.PriorInFlight != d.event.PostInFlight
	e := d.event
	d.event = congestion.FeedbackEvent{}
	return e, emitted
}

func (d *frozenDispatch) discardPacket(level protocol.EncryptionLevel, pn protocol.PacketNumber) {
	key := congestionKey(level, pn)
	d.recovery.discard(key)
	if p, ok := d.packets[key]; ok {
		d.sampler.dispose(p)
		delete(d.packets, key)
	}
}

func (d *frozenDispatch) discardSpace(level protocol.EncryptionLevel) {
	d.recovery.discardSpace(level)
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

func (d *frozenDispatch) resetCapture(pathChanged bool) {
	d.sampleGeneration++
	if pathChanged {
		d.pathGeneration++
	}
	d.recovery.reset()
	d.packets = make(map[congestionPacketKey]congestion.PacketInfo)
	d.event = congestion.FeedbackEvent{}
	d.sampler = frozenSampler{delivered: d.sampler.delivered, lost: d.sampler.lost}
}
