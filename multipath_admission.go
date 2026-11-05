package quic

import (
	"cmp"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"slices"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/qlogwriter"
)

const (
	maxPathHistoryUnits = 4
	maxPathAttachments  = 8
	// minPathProbeAllowance fits a short header with the longest connection ID,
	// both validation frames and the AEAD tag, so a selected probe always packs.
	minPathProbeAllowance protocol.ByteCount = 64
	// pathResourceLimitReached is the draft's PATH_RESOURCE_LIMIT_REACHED.
	pathResourceLimitReached = 0x3e75
)

var (
	errPathValidationTimeout = errors.New("quic: path validation deadline exceeded")
	errPathZeroRetired       = errors.New("quic: path zero is retired")
)

// pathMember is one consumed or established protocol path. Only the connection
// goroutine touches it. A candidate is its one validating tuple.
type pathMember struct {
	id        protocol.PathID
	live      bool
	addr      net.Addr
	info      packetInfo
	transport *Transport // nil is the connection's own transport
	candidate *pathCandidate
	response  *pathResponseDue
	announce  bool
	deadline  monotime.Time // absolute, set at commitment; only a validating path uses it
	// fences lists every probe epoch this path has created. An entry leaves only
	// once it is revoked and resolved, so the abandonment signal and the release of
	// this member can never outrun a submission.
	fences []pathFence
	// established accounts for sends to a validated tuple, whose amplification
	// limit is lifted, and revokes them at retirement.
	established *path
	route       pathRoute
	// standby is the last validated tuple that isn't current, with the path-local CID
	// that validated it.
	standby *pathStandby
	// largestNonProbing is the highest non-probing packet number on this path, which
	// is the only packet a server's cutover may follow. RFC 9000 section 9.3.
	largestNonProbing protocol.PacketNumber
	// announcing owes the new tuple a non-probing packet, until one is acknowledged.
	announcing bool
	announceAt monotime.Time
	announces  int
	// pmtu is the size this path's congestion accounting already uses.
	pmtu protocol.ByteCount
	// statusSeq numbers this path's next PATH_STATUS frame.
	statusSeq uint64
	// lastOutcome is the snapshot's category of the latest local result.
	lastOutcome string
	pathPolicy
}

type pathCandidate struct {
	probe     path
	addr      net.Addr
	info      packetInfo
	transport *Transport
	connID    protocol.ConnectionID
	request   *pathOpenRequest
	// migrating holds a history unit and an attachment reference until commit or
	// release. fencing means it validated and the old tuple's fence is resolving.
	migrating, fencing bool
	keepCID            bool // the CID stays with the tuple's standby at retirement
	// movePN is the non-probing packet from this tuple that could move the path, while
	// it stays the path's highest. Valid only if moves is set.
	moves  bool
	movePN protocol.PacketNumber
}

type pathStandby struct {
	addr   net.Addr
	info   packetInfo
	connID protocol.ConnectionID
}

type pathFence struct {
	transport *Transport
	epoch     *responseEpoch
}

type pathResponseDue struct {
	data [8]byte
	addr net.Addr
	info packetInfo
	// transport delivered the challenge, and the response has to leave through it.
	transport *Transport
}

// pathOpenRequest crosses from the caller to the connection goroutine. It is
// the connection's one local operation, until any cutoff it caused has fenced.
type pathOpenRequest struct {
	ctx       context.Context
	transport *Transport
	addr      net.Addr
	done      chan error
	outcome   atomic.Int32 // one transition decides what the caller observes
	accepted  bool
	id        protocol.PathID
	migrate   bool          // id names the path to move to transport
	retained  *pathRetained // an aborted migration's charges, which hold the operation
}

const (
	pathOpenPending int32 = iota
	pathOpenCanceled
	pathOpenFinished
)

// finish publishes the connection's result unless cancellation already won. A
// false return means the caller was told it canceled, so nothing may activate.
func (r *pathOpenRequest) finish(err error) bool {
	if !r.claim() {
		return false
	}
	r.done <- err
	return true
}

// claim decides the outcome without telling the caller, who learns only once the work
// the outcome promises is complete. A caller that lost the race waits for the send.
func (r *pathOpenRequest) claim() bool {
	return r.outcome.CompareAndSwap(pathOpenPending, pathOpenFinished)
}

func (r *pathOpenRequest) canceled() bool { return r.outcome.Load() == pathOpenCanceled }

// pathProbeIntent is one emission's frames, selected before any commitment.
type pathProbeIntent struct {
	context   ackhandler.PacketContext
	transport *Transport
	connID    protocol.ConnectionID
	frames    []ackhandler.Frame
	probe     *path
	addr      net.Addr
	info      packetInfo
	sent      func()
	refused   func(monotime.Time)
}

func (p *pathCreditRegistry) enableAdmission() {
	if p.conn.emission.local == nil {
		return
	}
	addr := p.conn.RemoteAddr()
	zero := &pathMember{id: 0, live: true, addr: addr, info: p.conn.emission.local.routeInfo(), established: &path{validated: true, mtuValidated: true}}
	zero.tuple = p.conn.emission.packer.pathZeroContext().TupleGeneration
	zero.livenessAt = monotime.Now()
	zero.fences = []pathFence{{epoch: &zero.established.probeEpoch}}
	p.members = map[protocol.PathID]*pathMember{0: zero}
	p.conn.emission.packer.holdDatagrams = true
	p.validated = []net.Addr{addr}
	p.rtt = map[protocol.PathID]*utils.RTTStats{0: p.conn.rttStats}
	p.attachments = map[*Transport]*pathAttachment{nil: {refs: 1, detached: make(chan struct{})}}
	p.handshakeAddr = addr
	if runner, ok := p.conn.connIDGenerator.ownRunner.(*packetHandlerMap); ok {
		p.origin = (*Transport)(runner)
	}
	p.fanOutResetTokens()
	if p.conn.perspective == protocol.PerspectiveClient {
		p.conn.failureOwner.Store(p)
	}
	if recovery, ok := p.conn.sentPacketHandler.(interface{ EnablePathPolicy() }); ok {
		recovery.EnablePathPolicy()
	}
}

func (p *pathCreditRegistry) hasAdmission() bool { return p.members != nil }

// sentPathOwner is the part of recovery's root that a path's lifecycle uses.
type sentPathOwner interface {
	RetirePath(protocol.PathID)
	PathQuiescent(protocol.PathID) bool
	RemovePath(protocol.PathID)
}

func (p *pathCreditRegistry) sentPaths() sentPathOwner {
	return p.conn.sentPacketHandler.(sentPathOwner)
}

// zeroRetired reports that path zero has been cut off, from then on for good.
func (p *pathCreditRegistry) zeroRetired() bool { return p.terminals[0] != nil || p.released(0) }

// orderedMembers fills buf in ID order. Membership is small and IDs are not dense,
// so this scans members rather than every granted ID.
func (p *pathCreditRegistry) orderedMembers(buf []*pathMember) []*pathMember {
	for _, m := range p.members {
		buf = append(buf, m)
	}
	slices.SortFunc(buf, func(a, b *pathMember) int { return cmp.Compare(a.id, b.id) })
	return buf
}

// recoveryOn resolves a path's own recovery, which admission has already installed.
func (p *pathCreditRegistry) recoveryOn(id protocol.PathID) ackhandler.SentPacketHandler {
	return p.conn.sentPacketHandler.(interface {
		RecoveryOnPath(protocol.PathID) ackhandler.SentPacketHandler
	}).RecoveryOnPath(id)
}

// selectRoute reads each eligible path into an immutable view and asks the
// scheduler. Congestion, pacing and local capacity never choose the primary. They
// only tell whether a path the next DATAGRAM needs could carry it now.
func (p *pathCreditRegistry) selectRoute(now monotime.Time) (route, steerTo *pathRoute, steering, held bool) {
	if !p.hasAdmission() || p.closed {
		return nil, nil, false, false
	}
	var storage [2]pathView
	views := storage[:0]
	limit := protocol.MaxByteCount
	var ordered [maxPathHistoryUnits + 1]*pathMember
	for _, m := range p.orderedMembers(ordered[:0]) {
		id := m.id
		if !p.primaryEligible(m) {
			continue // a suspect path's only probes are its revalidation challenges
		}
		var view pathView
		var size protocol.ByteCount
		if id == 0 {
			view = pathViewFor(id, p.conn.sentPacketHandler.SendMode(now))
			size = datagramPayloadLimit(p.conn.maxPacketSize(), p.conn.connIDManager.Get())
		} else {
			connID := p.refreshRoute(m)
			view = pathViewFor(id, m.route.recovery.SendMode(now))
			size = datagramPayloadLimit(protocol.ByteCount(p.pmtuObservation(id)), connID)
		}
		limit = min(limit, size)
		views = append(views, view)
	}
	if limit != protocol.MaxByteCount {
		p.conn.pathPayloadLimit.Store(uint32(limit))
		if !p.zeroCarries() {
			// SendDatagram also bounds by path zero's estimate, which no longer applies.
			p.conn.maxPayloadSizeEstimate.Store(uint32(limit))
		}
	}
	if p.primary == noPrimary {
		// The hold is no eligible carrier, and a frame must not wait out revalidation.
		p.placeDatagram(nil)
	} else {
		p.placeDatagram(views)
	}
	// A fencing path counted for placement above, so a fence drops nothing, and it
	// keeps the primary, but nothing may pack against its revoked epoch.
	views = slices.DeleteFunc(views, func(view pathView) bool { return p.members[view.id].fencing() })
	chosen, steer, steering, held := selectPath(views, p.primary)
	if held {
		return p.heldRoute(), nil, false, true
	}
	return p.routeOf(chosen), p.routeOf(steer), steering, false
}

// refreshRoute brings a nonzero member's route up to its current tuple.
func (p *pathCreditRegistry) refreshRoute(m *pathMember) protocol.ConnectionID {
	connID, _ := p.conn.connIDManager.pathDestination(m.id, false)
	m.route.context, m.route.connID, m.route.recovery = ackhandler.PacketContext{PathID: m.id, TupleGeneration: m.tuple}, connID, p.recoveryOn(m.id)
	m.route.addr, m.route.info, m.route.transport, m.route.epoch = m.addr, m.info, m.transport, &m.established.probeEpoch
	return connID
}

// routeOf is a selected path's route. Path zero's is nil.
func (p *pathCreditRegistry) routeOf(id protocol.PathID) *pathRoute {
	if id == 0 {
		return nil
	}
	return &p.members[id].route
}

// datagramPayloadLimit is a payload this path can always carry in a fresh packet. It
// reserves the longest packet number, since one can grow while a message is queued,
// and never exceeds the estimate an ordinary connection admits at.
func datagramPayloadLimit(mtu protocol.ByteCount, connID protocol.ConnectionID) protocol.ByteCount {
	const tag, frameHeader = 16, 1 + 2 // type and a two-byte length
	return min(estimateMaxPayloadSize(mtu), mtu-wire.ShortHeaderLen(connID, protocol.PacketNumberLen4)-tag-frameHeader)
}

func pathViewFor(id protocol.PathID, mode ackhandler.SendMode) pathView {
	return pathView{
		id: id, fits: true,
		probing: mode == ackhandler.SendPTOAppData,
		ready:   mode == ackhandler.SendAny || mode == ackhandler.SendPacingLimited,
	}
}

// placeDatagram marks which views a fresh packet could carry the next DATAGRAM on.
// It is the one drop owner: a frame that fits no eligible path leaves at once, and
// the frame behind it is considered in turn. Frames behind the next stay unread
// until they reach the front.
func (p *pathCreditRegistry) placeDatagram(views []pathView) {
	queue := p.conn.datagramQueue
	if queue == nil {
		return
	}
	packer := p.conn.emission.packer
	for {
		f := queue.Peek()
		if f == nil {
			return
		}
		size, fits := f.Length(p.conn.version), false
		for i := range views {
			view := &views[i]
			if view.room == 0 {
				var route *pathRoute
				limit := p.conn.maxPacketSize()
				if view.id != 0 {
					route = &p.members[view.id].route
					limit = route.mtu.CurrentSize()
				}
				room, ok := packer.datagramRoom(route, limit)
				if !ok {
					return // no 1-RTT keys yet, so nothing can be judged
				}
				view.room = room
			}
			view.fits = size <= view.room
			fits = fits || view.fits
		}
		if fits {
			return
		}
		queue.dropUnfit()
	}
}

// pmtuObservation is a path's current UDP payload limit as a value, zero when it
// has none yet. Path zero's stays the connection's first-path finder.
func (p *pathCreditRegistry) pmtuObservation(id protocol.PathID) uint32 {
	var finder *mtuFinder
	if id == 0 {
		finder = p.conn.mtuDiscoverer
	} else if m := p.members[id]; m != nil {
		finder = m.route.mtu
	}
	if finder == nil {
		return 0
	}
	return uint32(finder.CurrentSize())
}

// notePMTU applies a size an acknowledged probe raised to that path's congestion
// accounting. Path zero's finder and PathMTUSettledV1 stay with the connection.
func (p *pathCreditRegistry) notePMTU() {
	for id, m := range p.members {
		if m.route.mtu == nil || m.route.mtu.CurrentSize() <= m.pmtu {
			continue
		}
		m.pmtu = m.route.mtu.CurrentSize()
		if recovery := p.recoveryOn(id); recovery != nil {
			recovery.SetMaxDatagramSize(m.pmtu)
		}
	}
}

// eligible never reads congestion or capacity. A quarantined endpoint affects its
// paths alone, path zero included, and neither signals abandonment nor promotes anything.
func (p *pathCreditRegistry) eligible(m *pathMember) bool {
	if !m.live || p.terminals[m.id] != nil || !p.conn.emission.local.routeUsable(m.transport) {
		return false
	}
	if m.id == 0 {
		return true // path zero's route is the connection's own
	}
	_, ok := p.conn.connIDManager.pathDestination(m.id, false)
	return ok
}

// rescheduleUnusable returns a path's outstanding reliable work to its logical
// owners, once no packet on it can reach the peer. Retained evidence stays, so a
// late ACK retires nothing twice. A packet still pending locally waits for its result.
func (p *pathCreditRegistry) rescheduleUnusable(id protocol.PathID, now monotime.Time) {
	recovery := p.recoveryOn(id)
	if recovery == nil {
		return
	}
	queue := recovery.(interface {
		QueueProbePacketAt(protocol.EncryptionLevel, monotime.Time) bool
	})
	for queue.QueueProbePacketAt(protocol.Encryption1RTT, now) { //nolint:revive // each call consumes one packet
	}
}

// largestPTO is the largest measured PTO over path zero, the observation and members.
// An unmeasured path contributes nothing, so its default cannot inflate retention.
func (p *pathCreditRegistry) largestPTO(includeMaxAckDelay bool) time.Duration {
	// Path zero stays in, though the observation may have moved to the primary.
	largest := max(p.conn.observedRTT().PTO(includeMaxAckDelay), p.conn.rttStats.PTO(includeMaxAckDelay))
	for id := range p.members {
		if rtt := p.rtt[id]; rtt != nil && rtt.HasMeasurement() {
			largest = max(largest, rtt.PTO(includeMaxAckDelay))
		}
	}
	return largest
}

// admitIssuedKeys runs before the window's CIDs are published, so the first
// authenticated packet finds its keys and no packet ever allocates them.
func (p *pathCreditRegistry) admitIssuedKeys(id protocol.PathID) error {
	if !p.hasAdmission() {
		return nil
	}
	sealer, err := p.conn.emission.packer.cryptoSetup.Get1RTTSealer()
	if err != nil {
		return err
	}
	rtt := utils.NewRTTStats()
	if err := handshake.AdmitShortHeaderPath(sealer, id, rtt); err != nil {
		return err
	}
	p.rtt[id] = rtt
	return nil
}

// admitMember commits an ID after authentication or a local decision. Every
// capacity is reserved before recovery, routes or submission exist.
func (p *pathCreditRegistry) admitMember(id protocol.PathID, target *Transport, now monotime.Time) (*pathMember, error) {
	if target == nil && p.attachments[nil] == nil {
		return nil, fmt.Errorf("%w: the connection has left its own transport", ErrMultipathInvalidConfigV1)
	}
	if p.historyUnits >= maxPathHistoryUnits {
		return nil, fmt.Errorf("%w: retained path history is full", ErrMultipathResourceLimitV1)
	}
	recovery, ok := p.conn.sentPacketHandler.(interface {
		AddPathWithRTT(protocol.PathID, protocol.ByteCount, *utils.RTTStats) error
	})
	if !ok {
		return nil, ErrMultipathUnsupportedV1
	}
	if err := recovery.AddPathWithRTT(id, protocol.MinInitialPacketSize, p.rtt[id]); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMultipathResourceLimitV1, err)
	}
	if err := p.conn.receivedPacketHandler.AddPath(id); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMultipathResourceLimitV1, err)
	}
	if !p.consume(id) {
		return nil, fmt.Errorf("%w: path ID %d is not available", ErrMultipathResourceLimitV1, id)
	}
	p.reserveHistory()
	if target == nil {
		p.attachments[nil].refs++ // a target's reference came with its attachment
	}
	member := &pathMember{id: id, transport: target, deadline: pathDeadline(now, pathTimeout), established: &path{validated: true, mtuValidated: true}}
	member.fences = []pathFence{{transport: target, epoch: &member.established.probeEpoch}}
	p.members[id] = member
	p.observe(member, "admitted")
	return member, nil
}

// reserveHistory takes one retained-history unit, which commitment, a pending
// terminal and R13's old generations all draw from.
func (p *pathCreditRegistry) reserveHistory() bool {
	if p.historyUnits >= maxPathHistoryUnits {
		return false
	}
	p.historyUnits++
	return true
}

func (p *pathCreditRegistry) releaseHistory() { p.historyUnits-- }

// admitIngress is reached only by an authenticated packet. A declined path keeps
// terminal metadata alone, and a sparse or unissued ID has no keys to get here.
func (p *pathCreditRegistry) admitIngress(id protocol.PathID, now monotime.Time) {
	if !p.hasAdmission() {
		return
	}
	if charged, issued := p.paths[id]; !issued || charged || p.terminals[id] != nil {
		return
	}
	if p.conn.perspective == protocol.PerspectiveClient {
		p.decline(id, 0, ErrPathClosed)
		return
	}
	if _, err := p.admitMember(id, nil, now); err != nil {
		p.decline(id, pathResourceLimitReached, err)
	}
}

func (p *pathCreditRegistry) tupleValidated(addr net.Addr) bool {
	for _, validated := range p.validated {
		if addrsEqual(validated, addr) {
			return true
		}
	}
	return false
}

func (p *pathCreditRegistry) recordValidated(addr net.Addr) {
	if p.tupleValidated(addr) {
		return
	}
	if len(p.validated) == maxPathAttachments {
		copy(p.validated, p.validated[1:])
		p.validated = p.validated[:maxPathAttachments-1]
	}
	p.validated = append(p.validated, addr)
}

// activate is the one route to a live path. Packets are handled before expiry, so
// the committed deadline is enforced here for every caller.
func (p *pathCreditRegistry) activate(m *pathMember, addr net.Addr, info packetInfo, target *Transport, now monotime.Time) bool {
	if !now.Before(m.deadline) {
		return false
	}
	m.live, m.addr, m.info, m.transport = true, addr, info, target
	m.backup, m.livenessAt = true, now
	p.observe(m, "live")
	p.startPMTU(m, target, now)
	p.recordValidated(addr)
	return true
}

// startPMTU gives a path that just went live its own finder, at the size its
// validation proved. It reports to no first-path signal, and qlog names its path.
func (p *pathCreditRegistry) startPMTU(m *pathMember, target *Transport, now monotime.Time) {
	m.pmtu = protocol.MinInitialPacketSize
	var recorder qlogwriter.Recorder
	if p.conn.qlogger != nil {
		recorder = pathMTURecorder{Recorder: p.conn.qlogger, member: m}
	}
	m.route.mtu = newMTUDiscoverer(p.rtt[m.id], m.pmtu, p.conn.peerMaxPacketSize(), recorder)
	if p.conn.config.DisablePathMTUDiscovery {
		return
	}
	capabilities := p.conn.conn.capabilities()
	if target != nil {
		capabilities = target.conn.capabilities()
	}
	if capabilities.DF {
		m.route.mtu.Start(now)
	}
}

func (p *pathCreditRegistry) newCandidate(m *pathMember, addr net.Addr, info packetInfo, target *Transport, connID protocol.ConnectionID, deadline monotime.Time, now monotime.Time) *pathCandidate {
	base := max(2*utils.DefaultInitialRTT, p.conn.observedRTT().PTO(true))
	candidate := &pathCandidate{addr: addr, info: info, transport: target, connID: connID}
	candidate.probe = path{
		addr: addr, lastPacketTime: now,
		baseProbeInterval: base, probeInterval: base,
		nextProbeTime: now, validationDeadline: deadline,
	}
	m.fences = append(m.fences, pathFence{transport: target, epoch: &candidate.probe.probeEpoch})
	m.candidate = candidate
	return candidate
}

// receivedTuple accounts one authenticated packet to its tuple. Additional tuples
// get no candidate or borrowed budget, and traffic never moves a deadline.
func (p *pathCreditRegistry) receivedTuple(id protocol.PathID, pn protocol.PacketNumber, via *Transport, addr net.Addr, info packetInfo, size protocol.ByteCount, challenge *wire.PathChallengeFrame, nonProbing bool, now monotime.Time) {
	m := p.members[id]
	if m == nil || p.closed || p.terminals[id] != nil {
		return
	}
	via = p.attachmentKey(via)
	moving := nonProbing && pn >= m.largestNonProbing
	if moving {
		m.largestNonProbing = pn
	}
	answer := func() {
		if challenge != nil {
			m.response = &pathResponseDue{data: challenge.Data, addr: addr, info: info, transport: via}
			p.conn.scheduleSending()
		}
	}
	if m.live && via == m.transport && addrsEqual(m.addr, addr) {
		answer()
		return
	}
	if candidate := m.candidate; candidate != nil {
		if via == candidate.transport && addrsEqual(candidate.addr, addr) {
			candidate.probe.bytesReceived += size
			if moving {
				candidate.moves, candidate.movePN = true, pn
			}
			answer()
			p.conn.scheduleSending()
		}
		return
	}
	if p.conn.perspective == protocol.PerspectiveClient {
		return // this profile's server never moves a path
	}
	if p.tupleValidated(addr) {
		if m.live || p.activate(m, addr, info, nil, now) {
			answer()
		}
		if moving {
			p.beginIncomingCutover(m, addr, info, pn, now)
		}
		return
	}
	if p.pruneFences(m) {
		return // a retired candidate's slot stays charged until its submissions fence
	}
	connID, ok := p.conn.connIDManager.pathDestination(id, m.live)
	if !ok {
		return // the peer must retry once a CID is available; the deadline keeps running
	}
	deadline := m.deadline
	if m.live {
		deadline = pathDeadline(now, pathTimeout)
	}
	candidate := p.newCandidate(m, addr, info, nil, connID, deadline, now)
	candidate.probe.bytesReceived = size
	candidate.moves, candidate.movePN = moving, pn
	answer()
	p.conn.scheduleSending()
}

// pathResponse matches only challenges committed on the arrival path. The data is
// unguessable, so a response elsewhere is a peer that has not followed the path.
// Packets are handled before expiry, so eligibility is checked here as well.
func (p *pathCreditRegistry) pathResponse(f *wire.PathResponseFrame, ingress protocol.PathID, now monotime.Time) {
	m := p.members[ingress]
	if m == nil || m.candidate == nil || p.terminals[ingress] != nil {
		return
	}
	candidate := m.candidate
	if !now.Before(candidate.probe.validationDeadline) || (candidate.request != nil && candidate.request.canceled()) {
		return
	}
	if p.attemptTargetFailed(m) {
		return // this pass's service ends the attempt, so no outcome is claimed here
	}
	deadline := candidate.probe.validationDeadline
	if !candidate.probe.matchResponse(f.Data, now) {
		return
	}
	p.conn.scheduleSending()
	if m.live && candidate.transport == m.transport && addrsEqual(candidate.addr, m.addr) {
		p.noteLiveness(m, now)
		return
	}
	if !candidate.probe.mtuValidated {
		return
	}
	if candidate.migrating {
		// The commit waits for the old tuple's fence, on the next service, and the
		// attempt's deadline still bounds that wait.
		candidate.fencing, candidate.probe.validationDeadline = true, deadline
		m.established.retire()
		return
	}
	request := candidate.request
	if request != nil && !request.claim() {
		return // cancellation won; the next service closes this initiation
	}
	if m.live {
		p.recordValidated(candidate.addr)
		p.keepStandby(m, candidate)
	} else {
		p.activate(m, candidate.addr, candidate.info, candidate.transport, now)
	}
	moving := m.live && candidate.moves && candidate.movePN == m.largestNonProbing
	addr, info, movePN := candidate.addr, candidate.info, candidate.movePN
	p.retireCandidate(m)
	if request != nil {
		p.conn.pathOpen.CompareAndSwap(request, nil)
		request.done <- nil // success means the path is usable, and the next operation can start
	}
	if moving {
		p.beginIncomingCutover(m, addr, info, movePN, now)
	}
}

// service advances requests, deadlines and fences. Fence reads never wait.
func (p *pathCreditRegistry) service(now monotime.Time) error {
	if !p.hasAdmission() {
		return nil
	}
	p.now = now
	if err := p.serviceRequest(now); err != nil {
		return err
	}
	for id, m := range p.members {
		if p.terminals[id] != nil {
			p.rescheduleUnusable(id, now)
			continue
		}
		if m.live && !p.conn.emission.local.routeUsable(m.transport) {
			p.rescheduleUnusable(id, now)
		}
		if m.live && m.suspect {
			p.rescheduleUnusable(id, now)
		}
		if !m.live {
			if p.attemptTargetFailed(m) {
				p.decline(id, 0, errPathTargetFailed)
			} else if !now.Before(m.deadline) {
				p.decline(id, 0, errPathValidationTimeout)
			}
			continue
		}
		if p.attemptTargetFailed(m) {
			m.candidate.request.finish(errPathTargetFailed)
			p.retireCandidate(m)
			p.conn.scheduleSending()
		}
		if candidate := m.candidate; candidate != nil && !now.Before(candidate.probe.validationDeadline) {
			if candidate.request != nil {
				candidate.request.finish(errPathValidationTimeout)
			}
			p.retireCandidate(m)
		}
		p.serviceCutover(m, now)
		if m.suspect {
			p.startRevalidation(m, now)
		}
		p.pruneFences(m)
	}
	p.reselect(now)
	for id, terminal := range p.terminals {
		p.signalTerminal(id, terminal)
	}
	p.serviceRetained(now)
	p.serviceDetachments()
	err := p.replenish()
	p.publishPaths()
	return err
}

// retireCandidate revokes the candidate's queued probes. Its fence entry keeps
// target and epoch for whatever is already claimed.
func (p *pathCreditRegistry) retireCandidate(m *pathMember) {
	candidate := m.candidate
	if candidate == nil {
		return
	}
	candidate.probe.retire()
	m.candidate = nil
	if p.migrationWaiting(m.id) {
		p.conn.scheduleSending() // it takes this slot on its next pass
	}
	expiry := pathDeadline(monotime.Now(), threePathPTO(p.largestPTO(false)))
	if !candidate.keepCID && candidate.connID.Len() != 0 {
		p.conn.connIDManager.retirePathDestination(m.id, candidate.connID, expiry)
	}
	if !candidate.migrating {
		return
	}
	entry := p.retain(candidate.transport, 0, &candidate.probe.probeEpoch)
	if candidate.request != nil {
		candidate.request.retained = entry
	}
	if candidate.fencing && m.live && p.terminals[m.id] == nil {
		// The old tuple stays selected, on an epoch nothing has revoked.
		m.established = &path{validated: true, mtuValidated: true}
		m.fences = append(m.fences, pathFence{transport: m.transport, epoch: &m.established.probeEpoch})
		p.conn.scheduleSending()
	}
}

// pruneFences drops resolved revoked entries, and reports whether one remains.
func (p *pathCreditRegistry) pruneFences(m *pathMember) bool {
	busy := false
	m.fences = slices.DeleteFunc(m.fences, func(fence pathFence) bool {
		if !fence.epoch.revoked.Load() {
			return false
		}
		if p.conn.emission.local.probesPending(fence.transport, fence.epoch) {
			busy = true
			return false
		}
		return true
	})
	return busy
}

// serviceRequest handles the local operation. Before an ID is committed, a
// cancellation only releases the operation slot.
func (p *pathCreditRegistry) serviceRequest(now monotime.Time) error {
	p.serviceCommands(now)
	request := p.conn.pathOpen.Load()
	if request == nil {
		return nil
	}
	if request.migrate {
		return p.serviceMigration(request, now)
	}
	if request.accepted {
		if request.outcome.Load() == pathOpenPending {
			return nil
		}
		if m := p.members[request.id]; m != nil && !m.live && p.terminals[request.id] == nil {
			p.decline(request.id, 0, context.Canceled)
		}
		if terminal := p.terminals[request.id]; terminal == nil || terminal.signaled {
			p.conn.pathOpen.CompareAndSwap(request, nil)
		}
		return nil
	}
	if request.canceled() {
		p.conn.pathOpen.CompareAndSwap(request, nil)
		return nil
	}
	if p.conn.perspective != protocol.PerspectiveClient {
		return p.refuseRequest(request, ErrMultipathUnsupportedV1)
	}
	if !p.conn.handshakeConfirmed {
		return nil // recovery refuses a new path before confirmation; the context bounds this wait
	}
	if request.transport != nil && request.transport.responses == nil {
		return p.refuseRequest(request, ErrMultipathUnsupportedV1)
	}
	target := p.attachmentKey(request.transport)
	if p.targetFailed(target) {
		return p.refuseRequest(request, errPathTargetFailed)
	}
	if p.migrationForbidden(request.addr) {
		return p.refuseRequest(request, fmt.Errorf("%w: the peer disabled migration to its handshake address", ErrMultipathUnsupportedV1))
	}
	id, ok := p.unusedID()
	if !ok {
		return nil // no credit yet; the caller's context bounds this wait
	}
	connID, ok := p.conn.connIDManager.pathDestination(id, false)
	if !ok {
		return nil // no peer CID yet; a later control frame wakes this pass
	}
	if p.historyUnits >= maxPathHistoryUnits {
		return p.refuseRequest(request, fmt.Errorf("%w: retained path history is full", ErrMultipathResourceLimitV1))
	}
	if target != nil {
		// The ID stays unconsumed unless the target can route.
		if err := p.attach(target); err != nil {
			return p.refuseRequest(request, err)
		}
	}
	m, err := p.admitMember(id, target, now)
	if err != nil {
		if target != nil {
			p.unref(target)
		}
		return p.refuseRequest(request, err)
	}
	request.accepted, request.id = true, id
	if target == nil && p.tupleValidated(request.addr) {
		if request.claim() {
			p.activate(m, request.addr, packetInfo{}, nil, now)
			m.announce = true
			p.conn.pathOpen.CompareAndSwap(request, nil)
			request.done <- nil // success means the path is usable, and the next operation can start
		}
		return nil
	}
	if d, ok := request.ctx.Deadline(); ok {
		if limit := now.Add(time.Until(d)); limit.Before(m.deadline) {
			m.deadline = limit
		}
	}
	candidate := p.newCandidate(m, request.addr, packetInfo{}, target, connID, m.deadline, now)
	candidate.keepCID = true // an initiation's CID is the path's own
	candidate.probe.unlimited = true
	candidate.request = request
	return nil
}

// unusedID finds the one issued ID nothing has consumed yet.
func (p *pathCreditRegistry) unusedID() (protocol.PathID, bool) {
	for id, charged := range p.paths {
		if !charged && p.terminals[id] == nil {
			return id, true
		}
	}
	return 0, false
}

// refuseRequest frees the operation first, so a caller that retries at once doesn't
// meet its own refused request.
func (p *pathCreditRegistry) refuseRequest(request *pathOpenRequest, err error) error {
	p.conn.pathOpen.CompareAndSwap(request, nil)
	request.finish(err)
	return nil
}

// retireMember is the submission cutoff. It revokes future submissions at once;
// signalTerminal waits for the ones already started.
func (p *pathCreditRegistry) retireMember(id protocol.PathID, err error) {
	m := p.members[id]
	if m == nil {
		return
	}
	m.response, m.announce = nil, false
	m.established.retire()
	if candidate := m.candidate; candidate != nil {
		if request := candidate.request; request != nil {
			request.finish(err) // a canceled caller has already returned
		}
		p.retireCandidate(m)
	}
}

// signalTerminal queues the abandonment once no submission can follow it. Keys
// stop sealing at the same boundary, and open until retention ends.
func (p *pathCreditRegistry) signalTerminal(id protocol.PathID, terminal *pathTerminal) {
	if terminal.signaled {
		return
	}
	if m := p.members[id]; m != nil {
		p.pruneFences(m)
		if len(m.fences) != 0 {
			return // every entry is revoked here, so each one is an unresolved claim
		}
	}
	if p.hasAdmission() {
		if sealer, err := p.conn.emission.packer.cryptoSetup.Get1RTTSealer(); err == nil {
			_ = handshake.RetireShortHeaderPath(sealer, id)
		}
		p.sentPaths().RetirePath(id)
	}
	terminal.signaled = true
	p.conn.queueControlFrame(&wire.PathAbandonFrame{PathID: id, ErrorCode: qerr.TransportErrorCode(terminal.code)})
	p.conn.scheduleSending()
}

// releaseMember ends a retained path's keys, contexts and reference at reclamation.
// Path zero's keys stay, because the legacy accessors read their entry. A transport's
// attachment stays charged at zero references until R13 can detach it, since its
// runner and the sender's target live on.
func (p *pathCreditRegistry) releaseMember(id protocol.PathID) {
	if !p.hasAdmission() {
		return
	}
	delete(p.rtt, id)
	if id != 0 {
		if sealer, err := p.conn.emission.packer.cryptoSetup.Get1RTTSealer(); err == nil {
			_ = handshake.RemoveShortHeaderPath(sealer, id)
		}
		p.conn.receivedPacketHandler.RemovePath(id)
	}
	if p.members[id] != nil {
		p.sentPaths().RemovePath(id)
	}
	m := p.members[id]
	if m == nil {
		return
	}
	delete(p.members, id)
	p.unref(m.transport)
}

func (p *pathCreditRegistry) nextAdmissionExpiry() monotime.Time {
	var next monotime.Time
	take := func(t monotime.Time) {
		if t != 0 && (next == 0 || t < next) {
			next = t
		}
	}
	for id, m := range p.members {
		if p.terminals[id] != nil {
			continue
		}
		if !m.live {
			take(m.deadline)
		}
		// A due one waits for its next pass, so a blocked attempt can't spin this timer.
		if m.announcing && m.announceAt.After(p.now) {
			take(m.announceAt)
		}
		if candidate := m.candidate; candidate != nil {
			take(candidate.probe.validationDeadline)
			// A due probe that couldn't be sent waits for the next wake, not a spin.
			if t := candidate.probe.nextProbeTime; candidate.probe.canProbe(t) && t.After(p.now) {
				take(t)
			}
		}
	}
	for _, entry := range p.retained {
		// An elapsed entry waits on its fence, which wakes through endpoint completion.
		if entry.expiry.After(p.now) {
			take(entry.expiry)
		}
	}
	return next
}

// nextProbe selects one path's frames, lowest path first. A tuple short of its
// allowance selects nothing, so it never blocks the next path's probe.
func (p *pathCreditRegistry) nextProbe(now monotime.Time) *pathProbeIntent {
	if !p.hasAdmission() || p.closed || !p.conn.sentPacketHandler.CanSendPathProbe() {
		return nil
	}
	var ordered [maxPathHistoryUnits + 1]*pathMember
	for _, m := range p.orderedMembers(ordered[:0]) {
		if p.terminals[m.id] != nil {
			continue
		}
		if intent := p.memberProbe(m, now); intent != nil {
			return intent
		}
	}
	return nil
}

func (p *pathCreditRegistry) memberProbe(m *pathMember, now monotime.Time) *pathProbeIntent {
	context := ackhandler.PacketContext{PathID: m.id, TupleGeneration: m.tuple}
	if m.id == 0 {
		context = p.conn.emission.packer.pathZeroContext()
	}
	if m.response != nil && m.response.transport != m.transport && (m.candidate == nil || m.response.transport != m.candidate.transport) {
		m.response = nil // that tuple is gone, and the peer retries on the current one
	}
	// Each intent is gated on its own endpoint before packing, so nothing spends a packet
	// number toward a failed one, and a probe toward a healthy target still goes.
	usable := p.conn.emission.local.routeUsable
	if candidate := m.candidate; candidate != nil && usable(candidate.transport) {
		probe := &candidate.probe
		var frames []ackhandler.Frame
		var data [8]byte
		challenged := probe.canProbe(now)
		if !probe.unlimited && probe.sendAllowance() < minPathProbeAllowance {
			challenged = false
		}
		if challenged {
			_, _ = rand.Read(data[:])
			frames = append(frames, ackhandler.Frame{Frame: &wire.PathChallengeFrame{Data: data}, Handler: emptyHandler{}})
		}
		answered := m.response != nil && m.response.transport == candidate.transport && addrsEqual(m.response.addr, candidate.addr) && probe.sendAllowance() >= minPathProbeAllowance
		if answered {
			frames = append(frames, ackhandler.Frame{Frame: &wire.PathResponseFrame{Data: m.response.data}, Handler: emptyHandler{}})
		}
		if len(frames) != 0 && !p.conn.connIDManager.pathDestinationUsable(m.id, candidate.connID) {
			// The peer retired this CID since it was chosen.
			if connID, ok := p.conn.connIDManager.pathDestination(m.id, candidate.transport != m.transport || !addrsEqual(candidate.addr, m.addr)); ok {
				candidate.connID = connID
			}
		}
		if len(frames) != 0 && p.conn.connIDManager.pathDestinationUsable(m.id, candidate.connID) {
			connID := candidate.connID
			return &pathProbeIntent{
				context: context, transport: candidate.transport, connID: candidate.connID, frames: frames,
				probe: probe, addr: candidate.addr, info: candidate.info,
				sent: func() {
					p.conn.connIDManager.notePathProbe(m.id, connID)
					if answered {
						m.response = nil
					}
				},
				refused: func(now monotime.Time) {
					if challenged {
						probe.commitChallenge(data, 0, now)
					}
					if answered {
						m.response = nil // the peer retries; a retained response would spin
					}
				},
			}
		}
	}
	if (m.response == nil && !m.announce) || m.fencing() {
		return nil
	}
	if m.response != nil && m.response.transport != m.transport {
		return nil // only its own tuple may answer; the candidate retries, or the peer does
	}
	if !usable(m.transport) {
		m.response, m.announce = nil, false // the peer retries on a path that can answer
		return nil
	}
	connID, ok := p.conn.connIDManager.pathDestination(m.id, false)
	if !ok {
		return nil
	}
	if m.response != nil && !addrsEqual(m.response.addr, m.addr) {
		// A validated tuple that isn't current answers on its own path-local CID.
		if connID, ok = p.standbyCID(m, m.response.addr, m.response.info); !ok {
			m.response = nil
			return nil
		}
	}
	var frames []ackhandler.Frame
	addr, info := m.addr, m.info
	if m.announce {
		frames = append(frames, ackhandler.Frame{Frame: &wire.PingFrame{}, Handler: emptyHandler{}})
	}
	if m.response != nil {
		frames = append(frames, ackhandler.Frame{Frame: &wire.PathResponseFrame{Data: m.response.data}, Handler: emptyHandler{}})
		addr, info = m.response.addr, m.response.info
	}
	if !addrsEqual(addr, m.addr) && !p.tupleValidated(addr) {
		return nil // no accounting owner for this tuple; the peer must retry
	}
	return &pathProbeIntent{
		context: context, transport: m.transport, connID: connID, frames: frames,
		probe: m.established, addr: addr, info: info,
		sent: func() {
			p.conn.connIDManager.notePathProbe(m.id, connID)
			m.response, m.announce = nil, false
		},
		refused: func(monotime.Time) { m.response, m.announce = nil, false },
	}
}

// endOperation gives the pending local operation the connection's terminal error, the one
// its streams and context report.
func (p *pathCreditRegistry) endOperation(cause error) {
	if request := p.conn.pathOpen.Load(); request != nil {
		p.conn.pathOpen.CompareAndSwap(request, nil)
		request.finish(cause)
	}
}

// closeAdmission revokes every queued probe. The endpoint's detach owns anything already
// claimed.
func (p *pathCreditRegistry) closeAdmission() {
	for _, m := range p.members {
		m.established.retire()
		p.retireCandidate(m)
	}
}

// setLocalUsePrivate is the private driver behind SetLocalUseV1. It overrides
// the demotion of a recovered path.
func (p *pathCreditRegistry) setLocalUsePrivate(id protocol.PathID, backup bool, now monotime.Time) error {
	if err := p.validateID(id); err != nil {
		return err
	}
	m := p.members[id]
	if m == nil || p.terminals[id] != nil {
		return ErrPathClosed
	}
	if !m.live {
		return ErrPathNotValidated
	}
	m.backup = backup
	p.reselect(now)
	p.conn.scheduleSending()
	return nil
}
