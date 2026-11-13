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
// The canonical allocation is never reused, so its pointer binds the epoch; the
// handle carries no second epoch value that could disagree with that identity.
// Retaining a copy can retain the canonical metadata after consumption; protocol
// retirement clears the metadata's packet reference independently of token copies.
// A registration shares a small GC allocation with at most seven neighbors, so a
// retained copy can also retain their metadata after their own consumption.
type LocalCompletionToken struct {
	registration *localRegistration
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

type localCompletionOwner struct {
	pending int // unconsumed tokens, including retired protocol packets
	live    int // unconsumed tokens whose packet is still protocol-owned
}

const localRegistrationChunkSize = 8

type localCompletions struct {
	owner            localCompletionOwner
	closed           bool
	nextRegistration uint8
	ecn              *localECNEvidence
	registrationHead *[localRegistrationChunkSize]localRegistration
}

// allocateRegistration issues each slot once. Full chunks have no allocator
// back-reference: only tokens, protocol packets and recovery evidence retain
// them. The owner retains at most one 448-byte partial chunk (on 64-bit hosts).
func (l *localCompletions) allocateRegistration() *localRegistration {
	if l.registrationHead == nil {
		l.registrationHead = new([localRegistrationChunkSize]localRegistration)
	}
	r := &l.registrationHead[l.nextRegistration]
	l.nextRegistration++
	if l.nextRegistration == localRegistrationChunkSize {
		l.registrationHead = nil
		l.nextRegistration = 0
	}
	return r
}

// localRegistrationEpoch binds one packet-number-space incarnation to its
// completion owner. It is immutable, issued once, and contains no pointer back
// to the packet-number space or its recovery history. Retained registrations
// keep this identity alive across Retry, packet-space disposal and late return.
type localRegistrationEpoch struct {
	owner *localCompletionOwner
}

type localRegistration struct {
	epoch         *localRegistrationEpoch
	packet        *packet // cleared at protocol retirement; never references native storage
	pn            protocol.PacketNumber
	registeredAt  monotime.Time
	eligibleAt    monotime.Time
	richOrdinal   uint64
	level         protocol.EncryptionLevel
	outcome       LocalPacketOutcome
	acked         bool
	earlyACK      bool
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
	h.local = &localCompletions{}
	for _, ps := range []*packetNumberSpace{h.initialPackets, h.handshakePackets, h.appDataPackets} {
		if ps != nil {
			ps.history.localPTOTracked = true
			ps.history.refillLocalPTO()
		}
	}
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

func (h *sentPacketHandler) validateLocalPacket(context PacketContext, t monotime.Time, pn protocol.PacketNumber, level protocol.EncryptionLevel, ecn protocol.ECN, size protocol.ByteCount) (*sentPacketHandler, *packetNumberSpace, error) {
	if h.local == nil || h.local.closed || (ecn != protocol.ECNNon && ecn != protocol.ECNUnsupported && (h.local.ecn == nil || level != protocol.Encryption1RTT || (ecn != protocol.ECT0 && ecn != protocol.ECT1))) {
		return nil, nil, errLocalPacketMode
	}
	if context.PathID != h.pathID {
		if path := h.path(context.PathID); path != nil && level == protocol.Encryption1RTT {
			return path.validateLocalPacket(context, t, pn, level, ecn, size)
		}
		return nil, nil, errLocalPacketMode
	}
	if level != protocol.EncryptionInitial && level != protocol.EncryptionHandshake && level != protocol.Encryption0RTT && level != protocol.Encryption1RTT {
		return nil, nil, errLocalPacketMode
	}
	ps := h.getPacketNumberSpace(level)
	if !h.validPacketContext(context, level) || ps == nil || t.IsZero() || size <= 0 || pn < 0 || (ps.history.highestPacketNumber != protocol.InvalidPacketNumber && pn != ps.history.highestPacketNumber+1) {
		return nil, nil, errLocalPacketMode
	}
	// Reserve against the shared history/tombstone budget before any accounting,
	// callback, or ownership transfer. A removed interior slot still counts once.
	if h.numTrackedPackets() >= protocol.MaxTrackedSentPackets {
		return nil, nil, ErrLocalCompletionCapacity
	}
	return h, ps, nil
}

func (h *sentPacketHandler) newLocalRegistration(ps *packetNumberSpace, pn protocol.PacketNumber, level protocol.EncryptionLevel, now monotime.Time) *localRegistration {
	if ps.localEpoch == nil {
		ps.localEpoch = &localRegistrationEpoch{owner: &h.local.owner}
	}
	r := h.local.allocateRegistration()
	r.epoch, r.pn, r.level, r.registeredAt = ps.localEpoch, pn, level, now
	h.local.owner.pending++
	return r
}

func (h *sentPacketHandler) finishLocalRegistration(ps *packetNumberSpace, pn protocol.PacketNumber, now monotime.Time) {
	h.settleLocalUndo(now)
	if ps.localUnregisteredPN == pn {
		ps.localUnregisteredPN = protocol.InvalidPacketNumber
	}
}

func (h *sentPacketHandler) registerLocalPacket(context PacketContext, t monotime.Time, pn, largestAcked protocol.PacketNumber, streams []StreamFrame, frames []Frame, level protocol.EncryptionLevel, ecn protocol.ECN, size protocol.ByteCount, mtu bool) (LocalCompletionToken, error) {
	h, ps, err := h.validateLocalPacket(context, t, pn, level, ecn, size)
	if err != nil {
		return LocalCompletionToken{}, err
	}
	r := h.newLocalRegistration(ps, pn, level, t)
	h.sentPacket(context, t, pn, largestAcked, streams, frames, level, ecn, size, mtu, false, r)
	h.finishLocalRegistration(ps, pn, t)
	return LocalCompletionToken{registration: r}, nil
}

func (h *sentPacketHandler) registerPreparedPacket(prepared *PacketPreparation, now monotime.Time, pn protocol.PacketNumber, ecn protocol.ECN) (LocalCompletionToken, error) {
	p := prepared.p
	h, ps, err := h.validateLocalPacket(p.Context, now, pn, protocol.Encryption1RTT, ecn, p.Length)
	if err != nil {
		return LocalCompletionToken{}, err
	}
	r := h.newLocalRegistration(ps, pn, protocol.Encryption1RTT, now)
	streams := prepared.streams
	prepared.p, prepared.streams = nil, nil
	// Capture ACK targets before the original accounting/logger callback point.
	targets := snapshotAckTargets(p.LargestAcked, p.Frames, protocol.Encryption1RTT)
	priorInFlight := h.accountSentPacket(ps, pn, p.Length)
	h.initializeSentPacket(p, r, now, protocol.Encryption1RTT, streams, targets)
	h.publishSentPacket(ps, p, pn, ecn, priorInFlight)
	h.finishLocalRegistration(ps, pn, now)
	return LocalCompletionToken{registration: r}, nil
}

func (h *sentPacketHandler) CompleteLocalPacket(token LocalCompletionToken, outcome LocalPacketOutcome, returnedAt monotime.Time) error {
	r := token.registration
	if r == nil || r.epoch == nil {
		return errLocalCompletionToken
	}
	owner := r.epoch.owner
	if h.local != nil && owner != &h.local.owner {
		for _, path := range h.paths {
			if path.local != nil && owner == &path.local.owner {
				return path.CompleteLocalPacket(token, outcome, returnedAt)
			}
		}
	}
	if h.local == nil || owner != &h.local.owner || r.outcome != localPacketPending {
		return errLocalCompletionToken
	}
	if outcome < LocalPacketAccepted || outcome > LocalPacketUnsubmitted || returnedAt.IsZero() || returnedAt.Before(r.registeredAt) {
		return errors.New("invalid local completion outcome or time")
	}
	// Completion can retire other local packets during its loss scan. Finalize
	// held undo only after that scan and every ECN transition in this operation.
	defer h.settleLocalUndo(returnedAt)
	h.local.owner.pending--
	if r.packet != nil {
		h.local.owner.live--
	}
	if ps := h.getPacketNumberSpace(r.level); ps != nil && ps.localEpoch == r.epoch {
		ps.history.completeLocal(r.pn)
	}
	r.outcome = outcome
	r.eligibleAt = returnedAt
	if r.packet != nil {
		if ps := h.getPacketNumberSpace(r.level); ps != nil && ps.localEpoch == r.epoch {
			ps.history.localPTOChanged(r.packet)
		}
	}
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
	h.retireAckOnly(returnedAt)
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
		if r.outcome == localPacketPending && r.packet != nil {
			h.local.owner.live--
		}
		r.packet = nil
		if h.local.ecn != nil {
			h.local.ecn.reconcileTerminal(h, r)
		}
	}
}

// qualifyPTO looks for a known-accepted packet no earlier PTO counted. Local
// rejection, queued expiry and uncertain submission never qualify an attempt.
func (h *sentPacketHandler) qualifyPTO(ps *packetNumberSpace) (accepted bool, tuple uint64) {
	for pn, p := range ps.history.Packets() {
		if pn >= h.nextQualifiedPN && p.Outstanding() && p.local != nil && p.local.outcome == LocalPacketAccepted {
			accepted, tuple, h.nextQualifiedPN = true, p.Context.TupleGeneration, pn+1
		}
	}
	return accepted, tuple
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
	n := h.local.owner.pending - h.appDataPackets.history.numUnresolvedLocal
	if h.initialPackets != nil {
		n -= h.initialPackets.history.numUnresolvedLocal
	}
	if h.handshakePackets != nil {
		n -= h.handshakePackets.history.numUnresolvedLocal
	}
	return n
}

func (h *sentPacketHandler) hasPendingLocal() bool {
	return h.local != nil && h.local.owner.live != 0
}

func (h *sentPacketHandler) localPTOTimeAndSpace() (monotime.Time, protocol.EncryptionLevel) {
	if h.handshakeConfirmed && h.initialPackets == nil && h.handshakePackets == nil && h.appDataPackets != nil {
		latest := h.appDataPackets.history.latestLocalPTO()
		if latest.IsZero() {
			return 0, 0
		}
		return latest.Add(h.getScaledPTO(true)), protocol.Encryption1RTT
	}
	var earliest monotime.Time
	var level protocol.EncryptionLevel
	for _, l := range []protocol.EncryptionLevel{protocol.EncryptionInitial, protocol.EncryptionHandshake, protocol.Encryption1RTT} {
		ps := h.getPacketNumberSpace(l)
		if ps == nil || (l == protocol.Encryption1RTT && !h.handshakeConfirmed) {
			continue
		}
		latest := ps.history.latestLocalPTO()
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
	h.local.registrationHead = nil
	h.local.nextRegistration = 0
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
			ps.history.localPTOChanged(p)
		}
	}
	h.initialPackets, h.handshakePackets = nil, nil
	h.appDataPackets = newPacketNumberSpace(0, true)
	h.lostPackets.Reset()
	h.ackCarriers.reset()
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
