package ackhandler

import (
	"time"
	"unsafe"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
)

const (
	maxRecoveryOutcomes = 32768
	recoverySlotMask    = maxRecoveryOutcomes - 1
)

type recoveryOutcomeState uint8

const (
	outcomeUnresolved recoveryOutcomeState = iota
	outcomeLost
	outcomeAcked
	outcomeExcluded // cannot prove congestion, but a valid receipt can exit recovery
	outcomeDisposed
)

type recoveryOutcome struct {
	number          protocol.PacketNumber
	ordinal         uint64
	sent            monotime.Time
	level, space    protocol.EncryptionLevel
	state           recoveryOutcomeState
	endpoint        bool
	receiptEligible bool
	ptoRetired      bool
}

// recoveryEvidence is connection-owned. The ring includes every registration,
// even ACK-only packets and excluded probes. Dropping its oldest entry never
// creates a summary that could bridge unknown history. Derived indexes let ACK
// service visit only the outcomes an ACK resolves or a query returns.
type recoveryEvidence struct {
	index           *recoveryIndex
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
	latestPackets   spacePackets
	boundaryPackets spacePackets
}

// spacePackets holds one packet number per space, each absent until noted.
type spacePackets struct {
	number [3]protocol.PacketNumber
	noted  [3]bool
}

func (w *spacePackets) note(space protocol.EncryptionLevel, pn protocol.PacketNumber) {
	s := recoverySpace(space)
	w.number[s], w.noted[s] = pn, true
}

func (w *spacePackets) get(space protocol.EncryptionLevel) (protocol.PacketNumber, bool) {
	s := recoverySpace(space)
	return w.number[s], w.noted[s]
}

// currentPathRecoveryReceipt is the single admission rule for episode-exit
// evidence, independent of persistent-congestion endpoint eligibility.
func currentPathRecoveryReceipt(p congestion.PacketInfo, generation uint64) bool {
	return p.PathGeneration == generation && !p.PathProbe
}

func pendingOutcome(s recoveryOutcomeState) bool {
	return s == outcomeUnresolved || s == outcomeLost || s == outcomeExcluded
}

// recordBytes accounts the outcome ring and every derived index. Map and
// allocator overhead remain outside this explicit structure accounting.
func (r *recoveryEvidence) recordBytes() uintptr {
	if r.index == nil {
		return 0
	}
	n := unsafe.Sizeof(recoveryIndex{}) + uintptr(cap(r.index.outcomes))*unsafe.Sizeof(recoveryOutcome{})
	for i := range r.index.spaces {
		n += uintptr(cap(r.index.spaces[i].slots)) * unsafe.Sizeof(uint16(0))
	}
	return n
}

func (r *recoveryEvidence) slot(l int) int    { return (r.head + l) & recoverySlotMask }
func (r *recoveryEvidence) logical(p int) int { return (p - r.head) & recoverySlotMask }

// sent reads the registration's record; the caller keeps ownership.
func (r *recoveryEvidence) sent(p *congestion.PacketInfo, generation uint64) {
	if r.index == nil {
		r.index = &recoveryIndex{outcomes: make([]recoveryOutcome, maxRecoveryOutcomes)}
	}
	x := r.index
	slot := r.slot(r.count)
	if r.count == maxRecoveryOutcomes {
		r.evicted++
		r.missing(x.outcomes[slot].ordinal)
		r.evict(slot)
		r.head = (r.head + 1) & recoverySlotMask
	} else {
		r.count++
	}
	key := congestionKey(p.EncryptionLevel, p.PacketNumber)
	o := recoveryOutcome{
		number: key.number, space: key.space, level: p.EncryptionLevel, ordinal: p.Ordinal, sent: p.SendTime,
		// Path/Retry reset clears the ledger; disposal prevents readmission.
		receiptEligible: currentPathRecoveryReceipt(*p, generation),
		endpoint:        r.measured && p.RegistrationValid && p.AckEliciting && !p.PathProbe && !p.MTUProbe,
	}
	if !p.RegistrationValid || p.PathProbe || p.MTUProbe {
		o.state = outcomeExcluded
	}
	x.outcomes[slot] = o
	s := recoverySpace(key.space)
	x.spaces[s].push(slot, key.number)
	x.pending[s].set(slot)
	x.eligible[s].assign(slot, o.receiptEligible)
	r.latestPackets.note(key.space, key.number)
}

// evict removes the oldest outcome while it still occupies logical position 0.
func (r *recoveryEvidence) evict(p int) {
	x := r.index
	o := &x.outcomes[p]
	s := recoverySpace(o.space)
	if o.state == outcomeLost {
		r.setLost(p, false)
	}
	x.pending[s].clear(p)
	x.eligible[s].clear(p)
	x.ptoPending[s].clear(p)
	x.spaces[s].popFront(x.outcomes)
}

func (r *recoveryEvidence) setState(p int, state recoveryOutcomeState) {
	x := r.index
	o := &x.outcomes[p]
	old := o.state
	if old == state {
		return
	}
	o.state = state
	s := recoverySpace(o.space)
	x.pending[s].assign(p, pendingOutcome(state))
	x.eligible[s].assign(p, o.receiptEligible && state != outcomeDisposed)
	x.ptoPending[s].assign(p, o.ptoRetired && state == outcomeUnresolved)
	if (old == outcomeLost) != (state == outcomeLost) {
		r.setLost(p, state == outcomeLost)
	}
}

// find relies on per-space packet-number monotonicity, which
// sentPacketHistory.checkSequentialPacketNumberUse enforces at registration.
func (r *recoveryEvidence) find(key congestionPacketKey) int {
	if r.index == nil {
		return -1
	}
	q := &r.index.spaces[recoverySpace(key.space)]
	if i := r.lowerBound(q, key.number); i < q.n {
		if p := q.at(i); r.index.outcomes[p].number == key.number {
			return p
		}
	}
	return -1
}

func (r *recoveryEvidence) lowerBound(q *spaceSlots, pn protocol.PacketNumber) int {
	if q.n == 0 || pn <= q.first {
		return 0
	}
	if pn > q.last {
		return q.n
	}
	// Entry i holds a number in [first+i, first+i+gaps], so the first entry at or
	// above pn lies in [d-gaps, min(n, d)] for d = pn-first. Without skipped
	// numbers the bracket is empty and the answer is d.
	d := int64(pn - q.first)
	lo, hi := int(max(0, d-q.gaps)), int(min(int64(q.n), d))
	for lo < hi {
		countNodes(1)
		mid := int(uint(lo+hi) >> 1)
		if r.index.outcomes[q.at(mid)].number < pn {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

func (r *recoveryEvidence) lost(key congestionPacketKey, congestionLoss, retained bool, boundary uint64) {
	i := r.find(key)
	known := i >= 0
	if known && r.index.outcomes[i].state == outcomeUnresolved {
		r.setState(i, outcomeLost)
		r.unconfirmedLoss = true // ACK-only losses can complete a classified span too.
	}
	if !congestionLoss {
		return
	}
	r.unconfirmedLoss = true
	if !r.episode.Active {
		r.episode = congestion.RecoveryEpisode{ID: r.episode.ID + 1, Boundary: boundary, Active: true, Entered: true, UndoPossible: true}
		r.members = make(map[uint64]struct{})
		r.boundaryPackets = r.latestPackets
	}
	// Loss ordering is a transport fact, independent of retained delivery
	// evidence. A per-space packet-number witness proves which side of the
	// frozen ordinal boundary this transmission occupies without guessing its
	// missing ordinal from the newest registration.
	if pn, ok := r.boundaryPackets.get(key.space); !ok || key.number > pn {
		r.episode.Boundary = boundary
		r.boundaryPackets = r.latestPackets
	}
	if !known || !retained {
		r.invalidateUndo()
		return
	}
	ordinal := r.index.outcomes[i].ordinal
	if r.episode.UndoPossible {
		if len(r.members) == maxDeliveryRetained {
			r.invalidateUndo()
		} else {
			r.members[ordinal] = struct{}{}
		}
	}
}

func (r *recoveryEvidence) invalidateUndo() {
	r.episode.UndoPossible = false
	r.members = nil
}

func (r *recoveryEvidence) missing(ordinal uint64) {
	if _, ok := r.members[ordinal]; ok || (r.episode.ID != 0 && ordinal == r.episode.Boundary) {
		r.invalidateUndo()
	}
}

func (r *recoveryEvidence) discard(key congestionPacketKey) {
	if i := r.find(key); i >= 0 {
		r.setState(i, outcomeDisposed)
	}
}

// PTO extraction transfers frame/flight ownership without declaring a loss.
// This fact survives optional delivery retention and expiry.
func (r *recoveryEvidence) retirePTO(key congestionPacketKey) {
	if i := r.find(key); i >= 0 && r.index.outcomes[i].state == outcomeUnresolved {
		o := &r.index.outcomes[i]
		o.ptoRetired = true
		r.index.ptoPending[recoverySpace(o.space)].set(i)
	}
}

// confirmPTO changes only persistent-span evidence. Ordinary loss callbacks,
// delivery loss totals and episode membership belong to lost, not this path.
//
// Only valid, non-probe registrations start unresolved, and dispatch admits a
// valid registration only when its send time is not before any earlier one.
// Packet numbers also rise within a space, so eligible PTO outcomes form a
// prefix of the space's PTO-retired backlog: the first ineligible one ends it.
func (r *recoveryEvidence) confirmPTO(space protocol.EncryptionLevel, witness protocol.PacketNumber, cutoff monotime.Time) {
	if witness == protocol.InvalidPacketNumber || r.index == nil {
		return
	}
	prior := beginServiceWork(servicePTOConfirmation)
	x := r.index
	set := &x.ptoPending[recoverySpace(space)]
	for l := r.nextL(set, 0, r.count-1, false); l >= 0; l = r.nextL(set, l+1, r.count-1, false) {
		p := r.slot(l)
		o := &x.outcomes[p]
		countInspected()
		if o.number >= witness || o.sent.After(cutoff) {
			countFailed()
			break
		}
		r.setState(p, outcomeLost)
		r.unconfirmedLoss = true
	}
	endServiceWork(prior)
}

func (r *recoveryEvidence) discardSpace(level protocol.EncryptionLevel) {
	if r.index == nil {
		return
	}
	q := &r.index.spaces[recoverySpace(congestionKey(level, 0).space)]
	for i := range q.n {
		p := q.at(i)
		if o := &r.index.outcomes[p]; o.level == level {
			r.missing(o.ordinal)
			r.setState(p, outcomeDisposed)
		}
	}
}

func (r *recoveryEvidence) reset() {
	// Connection-wide episode identities, like transmission ordinals, never
	// repeat; every path/Retry reset abandons the old evidence and RTT eligibility.
	*r = recoveryEvidence{episode: congestion.RecoveryEpisode{ID: r.episode.ID}}
}

// ack resolves only covered outcomes that are not yet ACKed or disposed. The
// returned witness remains the greatest covered, receipt-eligible registration,
// including one already ACKed, so duplicate ACKs can still confirm PTO outcomes.
func (r *recoveryEvidence) ack(ack *wire.AckFrame, level protocol.EncryptionLevel) protocol.PacketNumber {
	r.ackOrdinal = 0
	witness := protocol.InvalidPacketNumber
	if r.index == nil {
		return witness
	}
	x := r.index
	s := recoverySpace(congestionKey(level, 0).space)
	q := &x.spaces[s]
	for _, rng := range ack.AckRanges {
		prior := beginServiceWork(serviceAckTransitions)
		i, j := r.lowerBound(q, rng.Smallest), r.lowerBound(q, rng.Largest+1)
		if i >= j {
			endServiceWork(prior)
			continue
		}
		a, b := r.logical(q.at(i)), r.logical(q.at(j-1))
		for l := r.nextL(&x.pending[s], a, b, false); l >= 0; l = r.nextL(&x.pending[s], l+1, b, false) {
			p := r.slot(l)
			countInspected()
			if o := &x.outcomes[p]; o.receiptEligible {
				r.ackOrdinal = max(r.ackOrdinal, o.ordinal)
			}
			r.setState(p, outcomeAcked)
		}
		endServiceWork(prior)
		prior = beginServiceWork(serviceAckWitness)
		if l := r.prevL(&x.eligible[s], a, b, false); l >= 0 {
			countInspected()
			witness = max(witness, x.outcomes[r.slot(l)].number)
		}
		endServiceWork(prior)
	}
	return witness
}

func (r *recoveryEvidence) feedback(e *congestion.FeedbackEvent, pto time.Duration) {
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
	e.PersistentCongestion = r.persistentSpan(3 * pto)
	if e.PersistentCongestion.EndOrdinal != 0 {
		r.reported = e.PersistentCongestion.EndOrdinal
		r.invalidateUndo()
		r.episode.UndoEligible = false
	}
}

// persistentSpan selects the latest endpoint, after the last report, that is
// more than threshold after the first endpoint of its contiguous lost run.
// Endpoint ordinals and send times both rise in ring order (see confirmPTO), so
// a run's latest endpoint qualifies whenever any of its endpoints does, and
// the latest qualifying endpoint lies in the rightmost run whose
// first-to-last endpoint duration exceeds threshold. The threshold applies the
// current PTO at every query; no qualification is cached.
func (r *recoveryEvidence) persistentSpan(threshold time.Duration) congestion.PersistentCongestion {
	if r.index == nil || r.count == 0 {
		return congestion.PersistentCongestion{}
	}
	prior := beginServiceWork(servicePersistentSpan)
	defer endServiceWork(prior)
	l := r.rightmostRun(threshold)
	if l < 0 {
		return congestion.PersistentCongestion{}
	}
	first, last, _ := r.runFrom(l)
	start, end := &r.index.outcomes[r.slot(first)], &r.index.outcomes[r.slot(last)]
	countInspected()
	if end.ordinal <= r.reported {
		countFailed()
		return congestion.PersistentCongestion{}
	}
	return congestion.PersistentCongestion{StartOrdinal: start.ordinal, EndOrdinal: end.ordinal}
}

// ACK-only or duplicate receipts can confirm losses accumulated by a timer.
// Empty events without new recovery evidence retain the existing suppression.
func (r *recoveryEvidence) needsAckFeedback() bool {
	return r.unconfirmedLoss || (r.episode.Active && r.ackOrdinal > r.episode.Boundary)
}

// nextL and prevL search logical positions [a, b] of the ring for a slot in
// (or, with clear, absent from) set, returning a logical position or -1.
func (r *recoveryEvidence) nextL(set *slotSet, a, b int, clear bool) int {
	if a > b {
		return -1
	}
	pa, pb := r.slot(a), r.slot(b)
	if pa <= pb {
		return r.logicalOrNone(set.next(pa, pb, clear))
	}
	if p := set.next(pa, recoverySlotMask, clear); p >= 0 {
		return r.logical(p)
	}
	return r.logicalOrNone(set.next(0, pb, clear))
}

func (r *recoveryEvidence) prevL(set *slotSet, a, b int, clear bool) int {
	if a > b {
		return -1
	}
	pa, pb := r.slot(a), r.slot(b)
	if pa <= pb {
		return r.logicalOrNone(set.prev(pa, pb, clear))
	}
	if p := set.prev(0, pb, clear); p >= 0 {
		return r.logical(p)
	}
	return r.logicalOrNone(set.prev(pa, recoverySlotMask, clear))
}

func (r *recoveryEvidence) logicalOrNone(p int) int {
	if p < 0 {
		return -1
	}
	return r.logical(p)
}

func (r *recoveryEvidence) runStart(p int) bool {
	lost := &r.index.spans.lost
	if !lost.has(p) {
		return false
	}
	l := r.logical(p)
	return l == 0 || !lost.has(r.slot(l-1))
}

// runFrom returns the logical first and last endpoints of the lost run that
// starts at logical position l, and their send-time distance. A run without two
// endpoints has no qualifying distance.
func (r *recoveryEvidence) runFrom(l int) (first, last int, d time.Duration) {
	sp := &r.index.spans
	end := r.nextL(&sp.lost, l, r.count-1, true)
	if end < 0 {
		end = r.count
	}
	end--
	if first = r.nextL(&sp.endpoints, l, end, false); first < 0 {
		return -1, -1, 0
	}
	last = r.prevL(&sp.endpoints, first, end, false)
	if last == first {
		return first, last, 0
	}
	return first, last, r.index.outcomes[r.slot(last)].sent.Sub(r.index.outcomes[r.slot(first)].sent)
}

// setLost maintains run membership, run starts and the block maxima of every
// run whose extent changes: the run on the left, this slot and the next slot.
func (r *recoveryEvidence) setLost(p int, lost bool) {
	sp := &r.index.spans
	sp.lost.assign(p, lost)
	sp.endpoints.assign(p, lost && r.index.outcomes[p].endpoint)
	sp.starts.assign(p, r.runStart(p))
	l := r.logical(p)
	blocks := [3]int{p / spanBlockSlots, -1, -1}
	if l+1 < r.count {
		next := r.slot(l + 1)
		sp.starts.assign(next, r.runStart(next))
		blocks[1] = next / spanBlockSlots
	}
	if l > 0 && sp.lost.has(r.slot(l-1)) {
		start := r.prevL(&sp.lost, 0, l-1, true) + 1
		blocks[2] = r.slot(start) / spanBlockSlots
	}
	for i, b := range blocks {
		if b >= 0 && (i == 0 || b != blocks[0]) && (i < 2 || b != blocks[1]) {
			r.refreshBlock(b)
		}
	}
}

func (r *recoveryEvidence) refreshBlock(b int) {
	sp := &r.index.spans
	var best time.Duration
	lo, hi := b*spanBlockSlots, b*spanBlockSlots+spanBlockSlots-1
	for p := sp.starts.nextSet(lo, hi); p >= 0; p = sp.starts.nextSet(p+1, hi) {
		_, _, d := r.runFrom(r.logical(p))
		best = max(best, d)
	}
	i := spanBlocks + b
	sp.tree[i] = best
	for countNodes(1); i > 1; {
		i >>= 1
		v := max(sp.tree[2*i], sp.tree[2*i+1])
		if sp.tree[i] == v {
			break
		}
		sp.tree[i] = v
		countNodes(1)
	}
}

// rightmostRun returns the logical start of the latest run whose duration
// exceeds threshold, or -1. Logical order is [head, end) then [0, head).
func (r *recoveryEvidence) rightmostRun(threshold time.Duration) int {
	if r.head == 0 {
		return r.rightmostRunIn(0, r.count-1, threshold)
	}
	if l := r.rightmostRunIn(0, r.head-1, threshold); l >= 0 {
		return l
	}
	return r.rightmostRunIn(r.head, recoverySlotMask, threshold)
}

func (r *recoveryEvidence) rightmostRunIn(lo, hi int, threshold time.Duration) int {
	sp := &r.index.spans
	bl, bh := lo/spanBlockSlots, hi/spanBlockSlots
	if l := r.scanBlock(max(lo, bh*spanBlockSlots), hi, threshold); l >= 0 || bl == bh {
		return l
	}
	for top := bh - 1; top > bl; {
		b := sp.rightmostBlock(1, 0, spanBlocks-1, bl+1, top, threshold)
		if b < 0 {
			break
		}
		if l := r.scanBlock(b*spanBlockSlots, b*spanBlockSlots+spanBlockSlots-1, threshold); l >= 0 {
			return l
		}
		top = b - 1
	}
	return r.scanBlock(lo, bl*spanBlockSlots+spanBlockSlots-1, threshold)
}

// scanBlock evaluates run starts in physical slots [lo, hi] of one block,
// latest first. The block maximum covers every start in the block.
func (r *recoveryEvidence) scanBlock(lo, hi int, threshold time.Duration) int {
	sp := &r.index.spans
	countNodes(1)
	if sp.tree[spanBlocks+lo/spanBlockSlots] <= threshold {
		return -1
	}
	for p := sp.starts.prevSet(lo, hi); p >= 0; p = sp.starts.prevSet(lo, p-1) {
		countInspected()
		l := r.logical(p)
		if _, _, d := r.runFrom(l); d > threshold {
			return l
		}
		countFailed()
	}
	return -1
}

func (sp *spanIndex) rightmostBlock(node, nl, nr, a, b int, threshold time.Duration) int {
	if nr < a || nl > b {
		return -1
	}
	countNodes(1)
	if sp.tree[node] <= threshold {
		return -1
	}
	if nl == nr {
		return nl
	}
	mid := (nl + nr) / 2
	if x := sp.rightmostBlock(2*node+1, mid+1, nr, a, b, threshold); x >= 0 {
		return x
	}
	return sp.rightmostBlock(2*node, nl, mid, a, b, threshold)
}
