package ackhandler

import (
	"errors"
	"slices"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/qlogwriter"
)

// LocalSentPacketHandler is private construction plumbing for the inactive
// local-I/O route. All methods, including completion, run in the connection context.
// Callers reserve packet construction separately; refused registration transfers
// no frame ownership. SentPacket is forbidden on this route.
type LocalSentPacketHandler interface {
	SentPacketHandler
	RegisterLocalPacket(PacketContext, monotime.Time, protocol.PacketNumber, protocol.PacketNumber, []StreamFrame, []Frame, protocol.EncryptionLevel, protocol.ECN, protocol.ByteCount) (LocalCompletionToken, error)
	RegisterLocalMTUProbe(PacketContext, monotime.Time, protocol.PacketNumber, []Frame, protocol.ECN, protocol.ByteCount) (LocalCompletionToken, error)
	RegisterLocalPathProbe(PacketContext, monotime.Time, protocol.PacketNumber, []Frame, protocol.ByteCount) error
	CompleteLocalPacket(LocalCompletionToken, LocalPacketOutcome, monotime.Time) error
	CloseLocal(monotime.Time)
	CanRegisterLocalPackets(int) bool
}

// LocalCompletionToken identifies one registration, including its original epoch.
// A copy conveys no protocol or native-buffer ownership. It can be consumed once.
type LocalCompletionToken struct {
	owner     *localCompletionOwner
	id, epoch uint64
}

type LocalPacketOutcome uint8

const (
	localPacketPending LocalPacketOutcome = iota
	LocalPacketAccepted
	LocalPacketUncertain
	LocalPacketUnsubmitted
)

var (
	ErrLocalCompletionCapacity      = errors.New("local completion capacity exhausted")
	ErrLocalCompletionContradiction = errors.New("unsubmitted local packet was authenticated by peer")
	errLocalCompletionToken         = errors.New("invalid or consumed local completion token")
	errLocalPacketMode              = errors.New("unsupported local packet mode")
)

type localCompletionOwner struct{ next uint64 }

type localCompletions struct {
	owner     localCompletionOwner
	nextEpoch uint64
	records   map[uint64]*localRegistration
	closed    bool
	ecn       *localECNEvidence
}

type localRegistration struct {
	registeredAt  monotime.Time
	packet        *packet // cleared at protocol retirement; never references native storage
	pn            protocol.PacketNumber
	level         protocol.EncryptionLevel
	epoch         uint64
	outcome       LocalPacketOutcome
	eligibleAt    monotime.Time
	acked         bool
	earlyACK      bool
	richOrdinal   uint64
	timingMissing bool
}

// NewLocalSentPacketHandler admits the existing Reno and rich delivery modes.
// Local outcomes and authenticated ACKs are reconciled in the connection context.
func NewLocalSentPacketHandler(initialPN protocol.PacketNumber, size protocol.ByteCount, rtt *utils.RTTStats, stats *utils.ConnectionStats, validated, enableECN bool, ignore func(protocol.PathID, protocol.PacketNumber), perspective protocol.Perspective, qlogger qlogwriter.Recorder, logger utils.Logger) (LocalSentPacketHandler, error) {
	h := newSentPacketHandler(initialPN, size, rtt, stats, validated, enableECN, ignore, perspective, qlogger, logger, nil)
	return EnableLocalCompletions(h)
}

func EnableLocalCompletions(handler SentPacketHandler) (LocalSentPacketHandler, error) {
	h, ok := handler.(*sentPacketHandler)
	if !ok || h.local != nil || h.bytesSent != 0 || h.pathRoot != nil || len(h.paths) != 0 {
		return nil, errLocalPacketMode
	}
	h.local = &localCompletions{records: make(map[uint64]*localRegistration)}
	if h.congestionEvents != nil {
		h.congestionEvents.localRoute = true
	}
	if h.enableECN || h.bbrECN != nil {
		h.local.ecn = &localECNEvidence{watermark: protocol.InvalidPacketNumber}
		if h.bbrECN != nil {
			h.bbrECN.local = h.local.ecn
		} else {
			h.ecnTracker.(*ecnTracker).local = h.local.ecn
		}
	}
	return h, nil
}

func (h *sentPacketHandler) CanRegisterLocalPackets(count int) bool {
	return h.local != nil && !h.local.closed && count > 0 && count <= (protocol.MaxTrackedSentPackets-h.numTrackedPackets())/2
}

func (h *sentPacketHandler) RegisterLocalPacket(context PacketContext, t monotime.Time, pn, largestAcked protocol.PacketNumber, streams []StreamFrame, frames []Frame, level protocol.EncryptionLevel, ecn protocol.ECN, size protocol.ByteCount) (LocalCompletionToken, error) {
	return h.registerLocalPacket(context, t, pn, largestAcked, streams, frames, level, ecn, size, false)
}

// RegisterLocalMTUProbe keeps the probe's loss out of congestion control. Its
// other local lifecycle is the same as an ordinary registration's.
func (h *sentPacketHandler) RegisterLocalMTUProbe(context PacketContext, t monotime.Time, pn protocol.PacketNumber, frames []Frame, ecn protocol.ECN, size protocol.ByteCount) (LocalCompletionToken, error) {
	return h.registerLocalPacket(context, t, pn, protocol.InvalidPacketNumber, nil, frames, protocol.Encryption1RTT, ecn, size, true)
}

// RegisterLocalPathProbe needs no completion token: a path probe holds no flight,
// ECN mark or RTT sample, so no recovery consumer reads its transmission. Whether
// or not it left the host, its number never counts toward packet distance.
func (h *sentPacketHandler) RegisterLocalPathProbe(context PacketContext, t monotime.Time, pn protocol.PacketNumber, frames []Frame, size protocol.ByteCount) error {
	if h.local == nil || h.local.closed {
		return errLocalPacketMode
	}
	if context.PathID != h.pathID {
		if path := h.path(context.PathID); path != nil {
			return path.RegisterLocalPathProbe(context, t, pn, frames, size)
		}
		return errLocalPacketMode
	}
	ps := h.appDataPackets
	if !h.validPacketContext(context, protocol.Encryption1RTT) || t.IsZero() || size <= 0 || pn < 0 || (ps.history.highestPacketNumber != protocol.InvalidPacketNumber && pn != ps.history.highestPacketNumber+1) {
		return errLocalPacketMode
	}
	if h.numTrackedPackets() >= protocol.MaxTrackedSentPackets {
		return ErrLocalCompletionCapacity
	}
	h.sentPacket(context, t, pn, protocol.InvalidPacketNumber, nil, frames, protocol.Encryption1RTT, protocol.ECNNon, size, false, true, nil)
	ps.history.excludeFromDistance(pn)
	if ps.localUnregisteredPN == pn {
		ps.localUnregisteredPN = protocol.InvalidPacketNumber
	}
	return nil
}

func (h *sentPacketHandler) registerLocalPacket(context PacketContext, t monotime.Time, pn, largestAcked protocol.PacketNumber, streams []StreamFrame, frames []Frame, level protocol.EncryptionLevel, ecn protocol.ECN, size protocol.ByteCount, mtu bool) (LocalCompletionToken, error) {
	if h.local == nil || h.local.closed || (ecn != protocol.ECNNon && ecn != protocol.ECNUnsupported && (h.local.ecn == nil || level != protocol.Encryption1RTT || (ecn != protocol.ECT0 && ecn != protocol.ECT1))) {
		return LocalCompletionToken{}, errLocalPacketMode
	}
	if context.PathID != h.pathID {
		if path := h.path(context.PathID); path != nil && level == protocol.Encryption1RTT {
			return path.registerLocalPacket(context, t, pn, largestAcked, streams, frames, level, ecn, size, mtu)
		}
		return LocalCompletionToken{}, errLocalPacketMode
	}
	if level != protocol.EncryptionInitial && level != protocol.EncryptionHandshake && level != protocol.Encryption0RTT && level != protocol.Encryption1RTT {
		return LocalCompletionToken{}, errLocalPacketMode
	}
	ps := h.getPacketNumberSpace(level)
	if !h.validPacketContext(context, level) || ps == nil || t.IsZero() || size <= 0 || pn < 0 || (ps.history.highestPacketNumber != protocol.InvalidPacketNumber && pn != ps.history.highestPacketNumber+1) {
		return LocalCompletionToken{}, errLocalPacketMode
	}
	// Reserve against the shared history/tombstone budget before any accounting,
	// callback, or ownership transfer. A removed interior slot still counts once.
	if h.numTrackedPackets() >= protocol.MaxTrackedSentPackets {
		return LocalCompletionToken{}, ErrLocalCompletionCapacity
	}
	if ps.localEpoch == 0 {
		h.local.nextEpoch++
		ps.localEpoch = h.local.nextEpoch
	}
	h.local.owner.next++
	token := LocalCompletionToken{owner: &h.local.owner, id: h.local.owner.next, epoch: ps.localEpoch}
	r := &localRegistration{pn: pn, level: level, epoch: ps.localEpoch, registeredAt: t}
	h.local.records[token.id] = r
	h.sentPacket(context, t, pn, largestAcked, streams, frames, level, ecn, size, mtu, false, r)
	h.settleLocalUndo(t)
	if ps.localUnregisteredPN == pn {
		ps.localUnregisteredPN = protocol.InvalidPacketNumber
	}
	return token, nil
}

func (h *sentPacketHandler) CompleteLocalPacket(token LocalCompletionToken, outcome LocalPacketOutcome, returnedAt monotime.Time) error {
	if h.local != nil && token.owner != &h.local.owner {
		for _, path := range h.paths {
			if token.owner == &path.local.owner {
				return path.CompleteLocalPacket(token, outcome, returnedAt)
			}
		}
	}
	if h.local == nil || token.owner != &h.local.owner {
		return errLocalCompletionToken
	}
	r := h.local.records[token.id]
	if r == nil || r.epoch != token.epoch {
		return errLocalCompletionToken
	}
	if outcome < LocalPacketAccepted || outcome > LocalPacketUnsubmitted || returnedAt.IsZero() || returnedAt.Before(r.registeredAt) {
		return errors.New("invalid local completion outcome or time")
	}
	// Completion can retire other local packets during its loss scan. Finalize
	// held undo only after that scan and every ECN transition in this operation.
	defer h.settleLocalUndo(returnedAt)
	delete(h.local.records, token.id)
	r.outcome = outcome
	r.eligibleAt = returnedAt
	if r.acked && outcome == LocalPacketUnsubmitted {
		return ErrLocalCompletionContradiction
	}
	h.completeLocalCongestion(r)
	h.completeLocalECN(r, returnedAt)
	// ACK, key disposal, Retry and close have already settled protocol ownership.
	// Late facts release only the terminal identity, never replacement history.
	if r.packet == nil {
		return nil
	}
	if outcome == LocalPacketUnsubmitted {
		p := r.packet
		h.beginCongestionFeedback(returnedAt, r.level, h.bytesInFlight, nil, protocol.InvalidPacketNumber)
		h.discardCongestionPacket(r.level, r.pn)
		h.getPacketNumberSpace(r.level).history.DeclareLost(r.pn)
		h.removeFromBytesInFlight(p)
		if p.IsAckEliciting() {
			h.queueFramesForRetransmission(p)
		}
		r.packet = nil
		h.finishCongestionFeedback()
	} else {
		h.getPacketNumberSpace(r.level).history.qualifyLocal(r.pn)
		// A higher ACK may already have passed this packet while it was pending.
		h.beginCongestionFeedback(returnedAt, r.level, h.bytesInFlight, nil, protocol.InvalidPacketNumber)
		h.detectLostPackets(returnedAt, r.level)
		h.finishCongestionFeedback()
	}
	h.setLossDetectionTimer(returnedAt)
	return nil
}

func (h *sentPacketHandler) retireLocal(p *packet, acked bool) {
	if r := p.local; r != nil {
		if acked {
			if r.outcome == localPacketPending && !r.earlyACK {
				r.earlyACK = true
				if d := h.congestionEvents; d != nil {
					d.sampler.fenceTiming()
					d.advanceLocalQualification()
				}
			}
			h.getPacketNumberSpace(r.level).history.qualifyLocal(r.pn)
		}
		r.acked = r.acked || acked
		r.packet = nil
		if h.local.ecn != nil {
			h.local.ecn.reconcileTerminal(h, r)
		}
	}
}

func (p *packet) networkEligible() bool {
	return p.local == nil || p.local.outcome == LocalPacketAccepted || p.local.outcome == LocalPacketUncertain
}

func (p *packet) recoveryTime() monotime.Time {
	if p.local != nil {
		return p.local.eligibleAt
	}
	return p.SendTime
}

func (p *packet) timingEligible() bool {
	return p.local == nil || p.local.outcome == LocalPacketAccepted
}

// unresolvedLocalOutsideHistory counts only identities not already represented
// by a current history slot (including holes behind its retained prefix).
func (h *sentPacketHandler) unresolvedLocalOutsideHistory() int {
	if h.local == nil {
		return 0
	}
	n := 0
	for _, r := range h.local.records {
		ps := h.getPacketNumberSpace(r.level)
		if ps == nil || ps.localEpoch != r.epoch || ps.history.Len() == 0 || r.pn < ps.history.firstPacketNumber || r.pn > ps.history.highestPacketNumber {
			n++
		}
	}
	return n
}

func (h *sentPacketHandler) hasPendingLocal() bool {
	if h.local == nil {
		return false
	}
	for _, r := range h.local.records {
		if r.packet != nil {
			return true
		}
	}
	return false
}

func (h *sentPacketHandler) localPTOTimeAndSpace() (monotime.Time, protocol.EncryptionLevel) {
	var earliest monotime.Time
	var level protocol.EncryptionLevel
	for _, l := range []protocol.EncryptionLevel{protocol.EncryptionInitial, protocol.EncryptionHandshake, protocol.Encryption1RTT} {
		ps := h.getPacketNumberSpace(l)
		if ps == nil || (l == protocol.Encryption1RTT && !h.handshakeConfirmed) {
			continue
		}
		var latest monotime.Time
		for _, p := range ps.history.Packets() {
			if p.Outstanding() && p.networkEligible() && p.recoveryTime().After(latest) {
				latest = p.recoveryTime()
			}
		}
		if latest.IsZero() {
			continue
		}
		deadline := latest.Add(h.getScaledPTO(l == protocol.Encryption1RTT))
		if earliest.IsZero() || deadline.Before(earliest) {
			earliest, level = deadline, l
		}
	}
	return earliest, level
}

// CloseLocal terminates protocol ownership but keeps unresolved completion
// identities bounded and consumable by this same owner until actual return.
func (h *sentPacketHandler) CloseLocal(now monotime.Time) {
	if h.local == nil || h.local.closed {
		return
	}
	h.local.closed = true
	if h.local.ecn != nil {
		h.local.ecn.close()
	}
	for _, path := range h.paths {
		path.CloseLocal(now)
	}
	h.CloseDelivery()
	for _, ps := range []*packetNumberSpace{h.initialPackets, h.handshakePackets, h.appDataPackets} {
		if ps == nil {
			continue
		}
		for _, p := range ps.history.Packets() {
			h.retireLocal(p, false)
			h.removeFromBytesInFlight(p)
			p.Frames, p.StreamFrames = nil, nil
		}
	}
	h.initialPackets, h.handshakePackets = nil, nil
	h.appDataPackets = newPacketNumberSpace(0, true)
	h.lostPackets.Reset()
	h.ackCarriers = ackCarrierHistory{limit: h.ackCarriers.limit}
	h.numProbesToSend = 0
	h.ptoMode = SendNone
	h.congestion.OnPacketsDiscarded()
	h.setLossDetectionTimer(now)
}

// Local eligibility timestamps and loss declarations can arrive in either order.
// The existing receipt ceiling bounds this filter independently of packet history.
func (h *sentPacketHandler) expireLostPackets(cutoff monotime.Time) {
	if h.local == nil {
		h.lostPackets.DeleteBefore(cutoff)
		return
	}
	h.lostPackets.lostPackets = slices.DeleteFunc(h.lostPackets.lostPackets, func(p lostPacket) bool { return p.SendTime.Before(cutoff) })
}
