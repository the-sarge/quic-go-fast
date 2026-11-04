package quic

import (
	"math"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
)

const (
	// suspectPTOAttempts is how many consecutive qualified PTO attempts make a
	// path suspect.
	suspectPTOAttempts = 3
	// minBackupFreshness is the shortest time liveness evidence stays fresh enough
	// to promote a path without revalidating it.
	minBackupFreshness = 10 * time.Second
	// noPrimary names no path, which clears the designated primary.
	noPrimary protocol.PathID = math.MaxUint32
)

// pathPolicy is one member's primary/backup state. Only the connection goroutine
// touches it, and the scheduler sees just the views built from it.
type pathPolicy struct {
	tuple      uint64 // the current tuple's generation
	backup     bool   // the application's or the policy's local use
	suspect    bool
	attempts   int // consecutive qualified PTO attempts; not recovery's ptoCount
	livenessAt monotime.Time

	peerBackup bool // advisory, and only ever a tie-break under local use
	peerSeq    uint64
	peerSet    bool
}

// primaryEligible means live, validated and authorized, neither suspect nor
// retiring. Pacing, congestion and local capacity are not inputs.
func (p *pathCreditRegistry) primaryEligible(m *pathMember) bool {
	return p.eligible(m) && !m.suspect
}

func (p *pathCreditRegistry) rttOf(id protocol.PathID) *utils.RTTStats {
	if id == 0 {
		return p.conn.rttStats
	}
	return p.rtt[id]
}

// fresh reports whether a path's liveness evidence still allows promotion.
func (p *pathCreditRegistry) fresh(m *pathMember, now monotime.Time) bool {
	window := minBackupFreshness
	if rtt := p.rttOf(m.id); rtt != nil {
		window = max(window, threePathPTO(rtt.PTO(true)))
	}
	return now.Sub(m.livenessAt) <= window
}

// notePTO counts one PTO expiration toward suspicion, only if the socket is known
// to have accepted a packet on the current tuple that no earlier attempt counted.
func (p *pathCreditRegistry) notePTO(outcome ackhandler.TimeoutOutcome, now monotime.Time) {
	if !p.hasAdmission() || !outcome.PTO || outcome.EncLevel != protocol.Encryption1RTT || !outcome.Accepted {
		return
	}
	m := p.members[outcome.PathID]
	if m == nil || !m.live || m.suspect || p.terminals[m.id] != nil || outcome.TupleGeneration != m.tuple {
		return
	}
	if m.attempts++; m.attempts >= suspectPTOAttempts {
		p.markSuspect(m, now)
	}
}

// noteAck applies an ACK to the path whose packets it acknowledges, whichever path
// carried it. Traffic from an old tuple says nothing about the current one.
func (p *pathCreditRegistry) noteAck(frame *wire.AckFrame, now monotime.Time) {
	if !p.hasAdmission() {
		return
	}
	var target protocol.PathID
	if frame.HasPathID {
		target = frame.PathID
	}
	m := p.members[target]
	recovery := p.recoveryOn(target)
	if m == nil || recovery == nil {
		return
	}
	acked, ok := recovery.(interface{ TakeAckedTuple() (uint64, bool) })
	if !ok {
		return
	}
	if tuple, live := acked.TakeAckedTuple(); live && tuple == m.tuple && m.live && p.terminals[target] == nil {
		p.noteLiveness(m, now)
	}
}

// noteLiveness is current-tuple evidence. A suspect path that recovers becomes a
// backup before reselection, so a promoted primary keeps its place.
func (p *pathCreditRegistry) noteLiveness(m *pathMember, now monotime.Time) {
	m.attempts, m.livenessAt = 0, now
	if candidate := m.candidate; candidate != nil && addrsEqual(candidate.addr, m.addr) {
		p.retireCandidate(m)
	}
	if m.suspect {
		m.suspect, m.backup = false, true
		p.conn.scheduleSending()
	}
	p.reselect(now)
}

// markSuspect stops ordinary scheduling on a path. It spends no wire ID and sends
// no PATH_ABANDON. Its reliable work returns to the logical owners while the retained
// evidence stays, and bounded revalidation looks for recovery.
func (p *pathCreditRegistry) markSuspect(m *pathMember, now monotime.Time) {
	m.suspect, m.attempts = true, 0
	p.rescheduleUnusable(m.id, now)
	p.startRevalidation(m, now)
	p.reselect(now)
	p.conn.scheduleSending()
}

// startRevalidation challenges a path's current tuple, with one candidate at a time
// and the candidate's own challenge cap and deadline as the bound. A response is the
// only way this ends in liveness, and the candidate's fence covers its submissions.
func (p *pathCreditRegistry) startRevalidation(m *pathMember, now monotime.Time) {
	if m.candidate != nil || p.terminals[m.id] != nil || !m.live || p.pruneFences(m) {
		return
	}
	connID, ok := p.conn.connIDManager.pathDestination(m.id, false)
	if !ok {
		return // a later control frame wakes the next pass
	}
	candidate := p.newCandidate(m, m.addr, m.info, m.transport, connID, pathDeadline(now, pathTimeout), now)
	candidate.probe.unlimited = true
	p.conn.scheduleSending()
}

// reselect is the one owner of the designated primary. A healthy current primary
// stays among equally preferred paths. Otherwise the choice is a fresh eligible path,
// active before backup, with the peer's advisory status and then the lowest ID as
// tie-breaks. A stale path is revalidated, not promoted.
func (p *pathCreditRegistry) reselect(now monotime.Time) {
	if !p.hasAdmission() || p.closed {
		return
	}
	current := p.members[p.primary]
	if current != nil && !p.primaryEligible(current) {
		current = nil
	}
	var best *pathMember
	for id := protocol.PathID(0); id <= p.localMax; id++ {
		m := p.members[id]
		if m == nil || m == current || !p.primaryEligible(m) {
			continue
		}
		if current != nil && (m.backup || !current.backup) {
			continue // no more preferred than the healthy primary
		}
		if !p.fresh(m, now) {
			p.startRevalidation(m, now)
			continue
		}
		if best == nil || (best.backup && !m.backup) || (best.backup == m.backup && best.peerBackup && !m.peerBackup) {
			best = m
		}
	}
	switch {
	case best != nil:
		p.primary = best.id
	case current == nil:
		p.primary = noPrimary
	}
	p.syncObservation()
}

// syncObservation keeps the legacy RTT observation on path zero while it is primary
// eligible, then on the designated primary, and retains the last source with neither.
func (p *pathCreditRegistry) syncObservation() {
	if p.conn.rttObservation == nil {
		return
	}
	if zero := p.members[0]; zero != nil && p.primaryEligible(zero) {
		p.conn.rttObservation.observe(p.conn.rttStats)
		return
	}
	if p.members[p.primary] != nil {
		if rtt := p.rttOf(p.primary); rtt != nil {
			p.conn.rttObservation.observe(rtt)
		}
	}
}

// heldRoute carries ACKs while nothing may carry ordinary work: the lowest path
// that isn't suspect, otherwise path zero.
func (p *pathCreditRegistry) heldRoute() *pathRoute {
	for id := protocol.PathID(0); id <= p.localMax; id++ {
		if m := p.members[id]; m != nil && !m.suspect && p.eligible(m) {
			return p.routeOf(id)
		}
	}
	return nil
}

// peerStatus records the peer's advisory preference. The newest sequence number
// wins, and it can't override local use or validation, nor move a healthy primary.
func (p *pathCreditRegistry) peerStatus(f *wire.PathStatusFrame) error {
	if err := p.validateID(f.PathID); err != nil {
		return err
	}
	m := p.members[f.PathID]
	if m == nil || p.terminals[f.PathID] != nil || (m.peerSet && f.SequenceNumber <= m.peerSeq) {
		return nil
	}
	m.peerBackup, m.peerSeq, m.peerSet = f.Backup, f.SequenceNumber, true
	return nil
}
