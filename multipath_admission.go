package quic

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
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
	// established accounts for sends to a validated tuple, whose amplification
	// limit is lifted, and revokes them at retirement.
	established path
}

type pathCandidate struct {
	probe     path
	addr      net.Addr
	info      packetInfo
	transport *Transport
	connID    protocol.ConnectionID
	request   *pathOpenRequest
	retired   bool // the slot stays charged until its submissions fence
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
	canceled  atomic.Bool
	accepted  bool
	id        protocol.PathID
}

func (r *pathOpenRequest) finish(err error) {
	select {
	case r.done <- err:
	default:
	}
}

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
	p.members = map[protocol.PathID]*pathMember{0: {id: 0, live: true, addr: addr, established: path{validated: true, mtuValidated: true}}}
	p.validated = []net.Addr{addr}
	p.rtt = make(map[protocol.PathID]*utils.RTTStats)
	p.attachments = make(map[*Transport]int)
}

func (p *pathCreditRegistry) hasAdmission() bool { return p.members != nil }

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
func (p *pathCreditRegistry) admitMember(id protocol.PathID, target *Transport) (*pathMember, error) {
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
	member := &pathMember{id: id, established: path{validated: true, mtuValidated: true}}
	p.members[id] = member
	return member, nil
}

// admitIngress is reached only by an authenticated packet. A declined path keeps
// terminal metadata alone, and a sparse or unissued ID has no keys to get here.
func (p *pathCreditRegistry) admitIngress(id protocol.PathID, _ monotime.Time) {
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
	if _, err := p.admitMember(id, nil); err != nil {
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

func (p *pathCreditRegistry) activate(m *pathMember, addr net.Addr, info packetInfo, target *Transport) {
	m.live, m.addr, m.info, m.transport = true, addr, info, target
	p.recordValidated(addr)
}

func (p *pathCreditRegistry) newCandidate(addr net.Addr, info packetInfo, target *Transport, connID protocol.ConnectionID, deadline monotime.Time, now monotime.Time) *pathCandidate {
	base := max(2*utils.DefaultInitialRTT, p.conn.observedRTT().PTO(true))
	candidate := &pathCandidate{addr: addr, info: info, transport: target, connID: connID}
	candidate.probe = path{
		addr: addr, lastPacketTime: now,
		baseProbeInterval: base, probeInterval: base,
		nextProbeTime: now, validationDeadline: deadline,
	}
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
		if !candidate.retired && addrsEqual(candidate.addr, addr) {
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
		if !m.live {
			p.activate(m, addr, info, nil)
		}
		answer()
		return
	}
	connID, ok := p.conn.connIDManager.pathDestination(id, m.live)
	if !ok {
		return // the peer must retry once a CID is available
	}
	m.candidate = p.newCandidate(addr, info, nil, connID, pathDeadline(now, pathTimeout), now)
	m.candidate.probe.bytesReceived = size
	answer()
	p.conn.scheduleSending()
}

// pathResponse matches only challenges committed on the arrival path. The data is
// unguessable, so a response elsewhere is a peer that has not followed the path.
func (p *pathCreditRegistry) pathResponse(f *wire.PathResponseFrame, ingress protocol.PathID, now monotime.Time) {
	m := p.members[ingress]
	if m == nil || m.candidate == nil || m.candidate.retired || p.terminals[ingress] != nil {
		return
	}
	candidate := m.candidate
	if !candidate.probe.matchResponse(f.Data, now) {
		return
	}
	p.conn.scheduleSending()
	if !candidate.probe.mtuValidated {
		return
	}
	if m.live {
		p.recordValidated(candidate.addr)
	} else {
		p.activate(m, candidate.addr, candidate.info, candidate.transport)
	}
	m.candidate = nil
	if request := candidate.request; request != nil {
		request.finish(nil)
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
		candidate := m.candidate
		if candidate == nil {
			continue
		}
		if candidate.retired {
			if !p.probesPending(candidate) {
				m.candidate = nil
			}
			continue
		}
		if now.Before(candidate.probe.validationDeadline) {
			continue
		}
		if m.live {
			candidate.probe.retire()
			candidate.retired = true
			if !p.probesPending(candidate) {
				m.candidate = nil
			}
			continue
		}
		p.decline(id, 0, errPathValidationTimeout)
	}
	for id, terminal := range p.terminals {
		p.signalTerminal(id, terminal)
	}
	return nil
}

func (p *pathCreditRegistry) probesPending(candidate *pathCandidate) bool {
	return p.conn.emission.local.probesPending(candidate.transport, &candidate.probe.probeEpoch)
}

// serviceRequest handles the local operation. Before an ID is committed, a
// cancellation only releases the operation slot.
func (p *pathCreditRegistry) serviceRequest(now monotime.Time) error {
	request := p.conn.pathOpen.Load()
	if request == nil {
		return nil
	}
	if request.accepted {
		if !request.canceled.Load() {
			return nil
		}
		if m := p.members[request.id]; m != nil && m.candidate != nil && !m.candidate.retired && p.terminals[request.id] == nil {
			p.decline(request.id, 0, context.Canceled)
		}
		if terminal := p.terminals[request.id]; terminal == nil || terminal.signaled {
			p.conn.pathOpen.CompareAndSwap(request, nil)
		}
		return nil
	}
	if request.canceled.Load() {
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
	m, err := p.admitMember(id, request.transport)
	if err != nil {
		return p.refuseRequest(request, err)
	}
	request.accepted, request.id = true, id
	m.transport = request.transport
	if request.transport != nil {
		p.attachments[request.transport]++
		runner := (*packetHandlerMap)(request.transport)
		if err := p.conn.connIDGenerator.addPathRunner(runner, connectionRunnerCallbacks(runner, p.conn)); err != nil {
			return &qerr.TransportError{ErrorCode: qerr.InternalError, ErrorMessage: err.Error()}
		}
	}
	if request.transport == nil && p.tupleValidated(request.addr) {
		p.activate(m, request.addr, packetInfo{}, nil)
		m.announce = true
		request.finish(nil)
		p.conn.pathOpen.CompareAndSwap(request, nil)
		return nil
	}
	deadline := pathDeadline(now, pathTimeout)
	if d, ok := request.ctx.Deadline(); ok {
		if limit := now.Add(time.Until(d)); limit.Before(deadline) {
			deadline = limit
		}
	}
	m.candidate = p.newCandidate(request.addr, packetInfo{}, request.transport, connID, deadline, now)
	m.candidate.probe.unlimited = true
	m.candidate.request = request
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
	candidate := m.candidate
	if candidate == nil {
		return
	}
	candidate.probe.retire()
	candidate.retired = true
	if request := candidate.request; request != nil {
		candidate.request = nil
		request.finish(err) // a canceled caller has already returned
		request.canceled.Store(true)
	}
}

// signalTerminal queues the abandonment once no submission can follow it. Keys
// stop sealing at the same boundary, and open until retention ends.
func (p *pathCreditRegistry) signalTerminal(id protocol.PathID, terminal *pathTerminal) {
	if terminal.signaled {
		return
	}
	if m := p.members[id]; m != nil {
		if m.candidate != nil && p.probesPending(m.candidate) {
			return
		}
		if p.conn.emission.local.probesPending(m.transport, &m.established.probeEpoch) {
			return
		}
		m.candidate = nil
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
	for id, m := range p.members {
		candidate := m.candidate
		if candidate == nil || candidate.retired || p.terminals[id] != nil {
			continue
		}
		if next == 0 || candidate.probe.validationDeadline < next {
			next = candidate.probe.validationDeadline
		}
		// A due probe that couldn't be sent waits for the next wake, not a spin.
		if t := candidate.probe.nextProbeTime; candidate.probe.canProbe(t) && t.After(p.now) && t < next {
			next = t
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
	if candidate := m.candidate; candidate != nil && !candidate.retired {
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
	if !p.tupleValidated(addr) {
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
		if m.candidate != nil {
			m.candidate.probe.retire()
			m.candidate.retired = true
		}
	}
}
