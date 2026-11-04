package quic

import (
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

var errPathValidationTimeout = errors.New("quic: path validation deadline exceeded")

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
	established path
	route       pathRoute
}

type pathCandidate struct {
	probe     path
	addr      net.Addr
	info      packetInfo
	transport *Transport
	connID    protocol.ConnectionID
	request   *pathOpenRequest
}

type pathFence struct {
	transport *Transport
	epoch     *responseEpoch
}

type pathResponseDue struct {
	data [8]byte
	addr net.Addr
	info packetInfo
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
}

const (
	pathOpenPending int32 = iota
	pathOpenCanceled
	pathOpenFinished
)

// finish publishes the connection's result unless cancellation already won. A
// false return means the caller was told it canceled, so nothing may activate.
func (r *pathOpenRequest) finish(err error) bool {
	if !r.outcome.CompareAndSwap(pathOpenPending, pathOpenFinished) {
		return false
	}
	r.done <- err
	return true
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
	zero := &pathMember{id: 0, live: true, addr: addr, established: path{validated: true, mtuValidated: true}}
	zero.fences = []pathFence{{epoch: &zero.established.probeEpoch}}
	p.members = map[protocol.PathID]*pathMember{0: zero}
	p.validated = []net.Addr{addr}
	p.rtt = make(map[protocol.PathID]*utils.RTTStats)
	p.attachments = make(map[*Transport]int)
}

func (p *pathCreditRegistry) hasAdmission() bool { return p.members != nil }

// recoveryOn resolves a path's own recovery, which admission has already installed.
func (p *pathCreditRegistry) recoveryOn(id protocol.PathID) ackhandler.SentPacketHandler {
	return p.conn.sentPacketHandler.(interface {
		RecoveryOnPath(protocol.PathID) ackhandler.SentPacketHandler
	}).RecoveryOnPath(id)
}

// selectRoute reads each eligible path into an immutable view and asks the
// scheduler. Congestion, pacing and local capacity are deliberately not inputs.
func (p *pathCreditRegistry) selectRoute(now monotime.Time) *pathRoute {
	if !p.hasAdmission() || p.closed {
		return nil
	}
	var storage [2]pathView
	views := append(storage[:0], pathView{probing: p.conn.sentPacketHandler.SendMode(now) == ackhandler.SendPTOAppData})
	for id := protocol.PathID(1); id <= p.localMax; id++ {
		if m := p.members[id]; m != nil && p.eligible(m) {
			views = append(views, pathView{id: id, probing: p.recoveryOn(id).SendMode(now) == ackhandler.SendPTOAppData})
		}
	}
	id := selectPath(views, p.primary)
	if id == 0 {
		return nil
	}
	m := p.members[id]
	connID, _ := p.conn.connIDManager.pathDestination(id, false)
	m.route.context, m.route.connID, m.route.recovery = ackhandler.PacketContext{PathID: id}, connID, p.recoveryOn(id)
	m.route.addr, m.route.info, m.route.transport, m.route.epoch = m.addr, m.info, m.transport, &m.established.probeEpoch
	return &m.route
}

// eligible never reads congestion or capacity. A quarantined endpoint affects its
// paths alone, and neither signals abandonment nor promotes anything.
func (p *pathCreditRegistry) eligible(m *pathMember) bool {
	if !m.live || p.terminals[m.id] != nil || !p.conn.emission.local.routeUsable(m.transport) {
		return false
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

// largestPTO is the largest measured PTO over path zero's observation and members.
// An unmeasured path contributes nothing, so its default cannot inflate retention.
func (p *pathCreditRegistry) largestPTO(includeMaxAckDelay bool) time.Duration {
	largest := p.conn.observedRTT().PTO(includeMaxAckDelay)
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
	if p.historyUnits >= maxPathHistoryUnits {
		return nil, fmt.Errorf("%w: retained path history is full", ErrMultipathResourceLimitV1)
	}
	if target != nil && p.attachments[target] == 0 && len(p.attachments)+1 > maxPathAttachments {
		return nil, fmt.Errorf("%w: transport attachments are full", ErrMultipathResourceLimitV1)
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
	p.historyUnits++
	member := &pathMember{id: id, transport: target, deadline: pathDeadline(now, pathTimeout), established: path{validated: true, mtuValidated: true}}
	member.fences = []pathFence{{transport: target, epoch: &member.established.probeEpoch}}
	p.members[id] = member
	return member, nil
}

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
	p.recordValidated(addr)
	return true
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
func (p *pathCreditRegistry) receivedTuple(id protocol.PathID, addr net.Addr, info packetInfo, size protocol.ByteCount, challenge *wire.PathChallengeFrame, now monotime.Time) {
	m := p.members[id]
	if m == nil || p.closed || p.terminals[id] != nil {
		return
	}
	answer := func() {
		if challenge != nil {
			m.response = &pathResponseDue{data: challenge.Data, addr: addr, info: info}
			p.conn.scheduleSending()
		}
	}
	if m.live && addrsEqual(m.addr, addr) {
		answer()
		return
	}
	if candidate := m.candidate; candidate != nil {
		if addrsEqual(candidate.addr, addr) {
			candidate.probe.bytesReceived += size
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
	p.newCandidate(m, addr, info, nil, connID, deadline, now).probe.bytesReceived = size
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
	if !candidate.probe.matchResponse(f.Data, now) {
		return
	}
	p.conn.scheduleSending()
	if !candidate.probe.mtuValidated {
		return
	}
	request := candidate.request
	if request != nil && !request.finish(nil) {
		return // cancellation won; the next service closes this initiation
	}
	if m.live {
		p.recordValidated(candidate.addr)
	} else {
		p.activate(m, candidate.addr, candidate.info, candidate.transport, now)
	}
	p.retireCandidate(m)
	if request != nil {
		p.conn.pathOpen.CompareAndSwap(request, nil)
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
			if id != 0 {
				p.rescheduleUnusable(id, now)
			}
			continue
		}
		if m.live && id != 0 && !p.conn.emission.local.routeUsable(m.transport) {
			p.rescheduleUnusable(id, now)
		}
		if !m.live {
			if !now.Before(m.deadline) {
				p.decline(id, 0, errPathValidationTimeout)
			}
			continue
		}
		if m.candidate != nil && !now.Before(m.candidate.probe.validationDeadline) {
			p.retireCandidate(m)
		}
		p.pruneFences(m)
	}
	for id, terminal := range p.terminals {
		p.signalTerminal(id, terminal)
	}
	return nil
}

// retireCandidate revokes the candidate's queued probes. Its fence entry keeps
// target and epoch for whatever is already claimed.
func (p *pathCreditRegistry) retireCandidate(m *pathMember) {
	if m.candidate != nil {
		m.candidate.probe.retire()
		m.candidate = nil
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
	request := p.conn.pathOpen.Load()
	if request == nil {
		return nil
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
	if request.transport != nil && request.transport.responses == nil {
		return p.refuseRequest(request, ErrMultipathUnsupportedV1)
	}
	const id protocol.PathID = 1 // the one initial window; replacement credit is R12's
	if charged, issued := p.paths[id]; !issued || charged || p.terminals[id] != nil {
		return nil // no credit yet; the caller's context bounds this wait
	}
	connID, ok := p.conn.connIDManager.pathDestination(id, false)
	if !ok {
		return nil // no peer CID yet; a later control frame wakes this pass
	}
	m, err := p.admitMember(id, request.transport, now)
	if err != nil {
		return p.refuseRequest(request, err)
	}
	request.accepted, request.id = true, id
	if request.transport != nil {
		p.attachments[request.transport]++
		runner := (*packetHandlerMap)(request.transport)
		if err := p.conn.connIDGenerator.addPathRunner(runner, connectionRunnerCallbacks(runner, p.conn)); err != nil {
			return &qerr.TransportError{ErrorCode: qerr.InternalError, ErrorMessage: err.Error()}
		}
	}
	if request.transport == nil && p.tupleValidated(request.addr) {
		if request.finish(nil) {
			p.activate(m, request.addr, packetInfo{}, nil, now)
			m.announce = true
			p.conn.pathOpen.CompareAndSwap(request, nil)
		}
		return nil
	}
	if d, ok := request.ctx.Deadline(); ok {
		if limit := now.Add(time.Until(d)); limit.Before(m.deadline) {
			m.deadline = limit
		}
	}
	candidate := p.newCandidate(m, request.addr, packetInfo{}, request.transport, connID, m.deadline, now)
	candidate.probe.unlimited = true
	candidate.request = request
	return nil
}

func (p *pathCreditRegistry) refuseRequest(request *pathOpenRequest, err error) error {
	request.finish(err)
	p.conn.pathOpen.CompareAndSwap(request, nil)
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
	}
	terminal.signaled = true
	p.conn.queueControlFrame(&wire.PathAbandonFrame{PathID: id, ErrorCode: qerr.TransportErrorCode(terminal.code)})
	p.conn.scheduleSending()
}

// releaseMember ends a retained path's charges when its retention expires.
func (p *pathCreditRegistry) releaseMember(id protocol.PathID) {
	if !p.hasAdmission() {
		return
	}
	if sealer, err := p.conn.emission.packer.cryptoSetup.Get1RTTSealer(); err == nil {
		_ = handshake.RemoveShortHeaderPath(sealer, id)
	}
	m := p.members[id]
	if m == nil {
		return
	}
	delete(p.members, id)
	p.historyUnits--
	if m.transport != nil {
		if p.attachments[m.transport]--; p.attachments[m.transport] == 0 {
			delete(p.attachments, m.transport)
		}
	}
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
		if candidate := m.candidate; candidate != nil {
			take(candidate.probe.validationDeadline)
			// A due probe that couldn't be sent waits for the next wake, not a spin.
			if t := candidate.probe.nextProbeTime; candidate.probe.canProbe(t) && t.After(p.now) {
				take(t)
			}
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
	for id := protocol.PathID(0); id <= p.localMax; id++ {
		m := p.members[id]
		if m == nil || p.terminals[id] != nil {
			continue
		}
		if intent := p.memberProbe(m, now); intent != nil {
			return intent
		}
	}
	return nil
}

func (p *pathCreditRegistry) memberProbe(m *pathMember, now monotime.Time) *pathProbeIntent {
	context := ackhandler.PacketContext{PathID: m.id}
	if m.id == 0 {
		context = p.conn.emission.packer.pathZeroContext()
	}
	if candidate := m.candidate; candidate != nil {
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
		answered := m.response != nil && addrsEqual(m.response.addr, candidate.addr) && probe.sendAllowance() >= minPathProbeAllowance
		if answered {
			frames = append(frames, ackhandler.Frame{Frame: &wire.PathResponseFrame{Data: m.response.data}, Handler: emptyHandler{}})
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
	if m.response == nil && !m.announce {
		return nil
	}
	connID, ok := p.conn.connIDManager.pathDestination(m.id, false)
	if !ok {
		return nil
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
		probe: &m.established, addr: addr, info: info,
		sent: func() {
			p.conn.connIDManager.notePathProbe(m.id, connID)
			m.response, m.announce = nil, false
		},
		refused: func(monotime.Time) { m.response, m.announce = nil, false },
	}
}

// closeAdmission ends the local operation and revokes every queued probe. The
// endpoint's detach owns anything already claimed.
func (p *pathCreditRegistry) closeAdmission() {
	if request := p.conn.pathOpen.Load(); request != nil {
		request.finish(net.ErrClosed)
		p.conn.pathOpen.CompareAndSwap(request, nil)
	}
	for _, m := range p.members {
		m.established.retire()
		p.retireCandidate(m)
	}
}
