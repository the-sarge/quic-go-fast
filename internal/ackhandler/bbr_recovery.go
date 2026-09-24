package ackhandler

import (
	"maps"
	"time"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
)

const maxRecoveryOutcomes = 32768

type recoveryOutcomeState uint8

const (
	outcomeUnresolved recoveryOutcomeState = iota
	outcomeLost
	outcomeAcked
	outcomeExcluded // cannot prove congestion, but a valid receipt can exit recovery
	outcomeDisposed
)

type recoveryOutcome struct {
	key             congestionPacketKey
	level           protocol.EncryptionLevel
	ordinal         uint64
	sent            monotime.Time
	state           recoveryOutcomeState
	endpoint        bool
	receiptEligible bool
}

// recoveryEvidence is connection-owned. The ring includes every registration,
// even ACK-only packets and excluded probes. Dropping its oldest entry never
// creates a summary that could bridge unknown history.
type recoveryEvidence struct {
	outcomes        []recoveryOutcome
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

// currentPathRecoveryReceipt is the single admission rule for episode-exit
// evidence, independent of persistent-congestion endpoint eligibility.
func currentPathRecoveryReceipt(p congestion.PacketInfo, generation uint64) bool {
	return p.PathGeneration == generation && !p.PathProbe
}

func (r *recoveryEvidence) sent(p congestion.PacketInfo, generation uint64) {
	if r.outcomes == nil {
		r.outcomes = make([]recoveryOutcome, maxRecoveryOutcomes)
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
	o := recoveryOutcome{
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

func (r *recoveryEvidence) lost(key congestionPacketKey, congestionLoss, retained bool, boundary uint64) {
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
	if i, ok := r.keys[key]; ok {
		r.outcomes[i].state = outcomeDisposed
	}
}

func (r *recoveryEvidence) discardSpace(level protocol.EncryptionLevel) {
	for i := 0; i < r.count; i++ {
		o := &r.outcomes[(r.head+i)%maxRecoveryOutcomes]
		if o.level == level {
			r.missing(o.ordinal)
			o.state = outcomeDisposed
		}
	}
}

func (r *recoveryEvidence) reset() {
	// Connection-wide episode identities, like transmission ordinals, never
	// repeat; every path/Retry reset abandons the old evidence and RTT eligibility.
	*r = recoveryEvidence{episode: congestion.RecoveryEpisode{ID: r.episode.ID}}
}

func (r *recoveryEvidence) ack(ack *wire.AckFrame, level protocol.EncryptionLevel) {
	r.ackOrdinal = 0
	space := congestionKey(level, 0).space
	for i := 0; i < r.count; i++ {
		o := &r.outcomes[(r.head+i)%maxRecoveryOutcomes]
		if o.key.space == space && ack.AcksPacket(o.key.number) && o.state != outcomeDisposed && o.state != outcomeAcked {
			if o.receiptEligible {
				r.ackOrdinal = max(r.ackOrdinal, o.ordinal)
			}
			o.state = outcomeAcked
		}
	}
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
	var first *recoveryOutcome
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
func (r *recoveryEvidence) needsAckFeedback() bool {
	return r.unconfirmedLoss || (r.episode.Active && r.ackOrdinal > r.episode.Boundary)
}
