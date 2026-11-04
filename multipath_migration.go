package quic

import (
	"errors"
	"fmt"
	"net"
	"slices"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
)

// maxPathAnnounceBackoff caps the doubling of a cutover's announcement retries.
const maxPathAnnounceBackoff = 6

var errPathCandidateCIDRetired = errors.New("quic: the peer retired the migration's connection ID")

// pathAttachment charges one transport's routing to this connection. References are
// members, candidates and retained entries. At zero it can detach.
type pathAttachment struct {
	refs     int
	detached chan struct{} // closed at publication
}

// pathRetained holds one history unit and one attachment reference until its time has
// passed and its epoch has resolved. A cutover's old tuple has a retention time and an
// already resolved fence, and an aborted attempt the reverse. Nothing frees an older
// entry, because each one owns its own charges.
type pathRetained struct {
	transport *Transport
	expiry    monotime.Time
	epoch     *responseEpoch
	released  bool
}

func (m *pathMember) fencing() bool { return m.candidate != nil && m.candidate.fencing }

func (p *pathCreditRegistry) zeroFencing() bool {
	zero := p.members[0]
	return zero != nil && zero.fencing()
}

// attachmentKey is nil for the connection's own transport, which every member and
// the sender call nil, until it has detached and can only return as any other target.
func (p *pathCreditRegistry) attachmentKey(t *Transport) *Transport {
	if t == p.origin && !p.originDetached {
		return nil
	}
	return t
}

// fanOutResetTokens makes tokens follow every attached transport, so a reset that
// arrives on any of them is detected and none survives on a detached one.
func (p *pathCreditRegistry) fanOutResetTokens() {
	c := p.conn
	c.connIDManager.addStatelessResetToken = func(token protocol.StatelessResetToken) {
		for runner := range c.connIDGenerator.connRunners {
			runner.AddResetToken(token, c)
		}
	}
	c.connIDManager.removeStatelessResetToken = func(token protocol.StatelessResetToken) {
		for runner := range c.connIDGenerator.connRunners {
			runner.RemoveResetToken(token)
		}
	}
}

// attach takes a reference on target, registering its routes on the first one. A
// refusal costs nothing, and leaves the connection and the target as they were.
func (p *pathCreditRegistry) attach(target *Transport) error {
	if attachment := p.attachments[target]; attachment != nil {
		attachment.refs++
		return nil
	}
	if len(p.attachments) >= maxPathAttachments {
		return fmt.Errorf("%w: transport attachments are full", ErrMultipathResourceLimitV1)
	}
	runner := (*packetHandlerMap)(target)
	if err := p.conn.connIDGenerator.addPathRunner(runner, connectionRunnerCallbacks(runner, p.conn), p.conn.connIDManager.armedResetTokens(), p.conn); err != nil {
		return err
	}
	p.attachMu.Lock()
	p.attachments[target] = &pathAttachment{refs: 1, detached: make(chan struct{})}
	p.attachMu.Unlock()
	return nil
}

func (p *pathCreditRegistry) unref(target *Transport) {
	if attachment := p.attachments[target]; attachment != nil {
		attachment.refs--
	}
}

// retain moves one history unit and one attachment reference into the list.
func (p *pathCreditRegistry) retain(target *Transport, expiry monotime.Time, epoch *responseEpoch) *pathRetained {
	entry := &pathRetained{transport: target, expiry: expiry, epoch: epoch}
	p.retained = append(p.retained, entry)
	return entry
}

func (p *pathCreditRegistry) serviceRetained(now monotime.Time) {
	p.retained = slices.DeleteFunc(p.retained, func(entry *pathRetained) bool {
		if entry.expiry.After(now) || (entry.epoch != nil && p.conn.emission.local.probesPending(entry.transport, entry.epoch)) {
			return false
		}
		entry.released = true
		p.releaseHistory()
		p.unref(entry.transport)
		return true
	})
}

// serviceDetachments retries each unreferenced transport on every pass. Fence and
// result reads never wait, and an unfinished one wakes through endpoint completion.
func (p *pathCreditRegistry) serviceDetachments() {
	for target, attachment := range p.attachments {
		if attachment.refs == 0 {
			p.detach(target, attachment)
		}
	}
}

// detach publishes only once this transport can neither destroy nor send for this
// connection. When close or a captured reset wins, the connection's own termination
// is the result, and nothing claims safety.
func (p *pathCreditRegistry) detach(key *Transport, attachment *pathAttachment) {
	target := key
	if target == nil {
		target = p.origin
	}
	if target == nil || !p.conn.emission.local.detachReady(key) {
		return
	}
	runner := (*packetHandlerMap)(target)
	generator := p.conn.connIDGenerator
	if !runner.detachRoutes(p.conn, generator.routedCIDs(), p.conn.connIDManager.armedResetTokens()) {
		return
	}
	delete(generator.connRunners, runner)
	if generator.ownRunner == connRunner(runner) {
		generator.ownRunner = nil
	}
	p.conn.emission.local.detachTransport(key)
	p.attachMu.Lock()
	delete(p.attachments, key)
	if key == nil {
		p.originDetached = true
	}
	p.attachMu.Unlock()
	close(attachment.detached)
}

// migrationForbidden applies the peer's disable_active_migration, which covers its
// handshake address and leaves other admitted addresses allowed.
func (p *pathCreditRegistry) migrationForbidden(addr net.Addr) bool {
	return p.conn.peerParams != nil && p.conn.peerParams.DisableActiveMigration && addrsEqual(addr, p.handshakeAddr)
}

// migrationWaiting keeps revalidation from renewing ahead of a waiting migration.
func (p *pathCreditRegistry) migrationWaiting(id protocol.PathID) bool {
	request := p.conn.pathOpen.Load()
	return request != nil && request.migrate && !request.accepted && request.id == id && request.outcome.Load() == pathOpenPending
}

// serviceMigration advances the local operation. Reservations follow the plan's
// order, and everything before the attachment leaves the path untouched.
func (p *pathCreditRegistry) serviceMigration(request *pathOpenRequest, now monotime.Time) error {
	m := p.members[request.id]
	if request.accepted {
		if request.outcome.Load() == pathOpenPending {
			return nil
		}
		if m != nil && m.candidate != nil && m.candidate.request == request {
			p.retireCandidate(m)
		}
		if request.retained == nil || request.retained.released {
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
		return nil // no path moves before confirmation; the context bounds this wait
	}
	if m == nil || !m.live || p.terminals[request.id] != nil {
		return p.refuseRequest(request, ErrPathClosed)
	}
	target := p.attachmentKey(request.transport)
	if target == m.transport {
		return p.refuseRequest(request, nil) // already there
	}
	if p.migrationForbidden(m.addr) {
		return p.refuseRequest(request, fmt.Errorf("%w: the peer disabled migration to its handshake address", ErrMultipathUnsupportedV1))
	}
	if m.candidate != nil || p.pruneFences(m) {
		return nil // an admitted candidate keeps its slot; the context bounds this wait
	}
	if p.historyUnits >= maxPathHistoryUnits {
		return p.refuseRequest(request, fmt.Errorf("%w: retained path history is full", ErrMultipathResourceLimitV1))
	}
	connID, ok := p.conn.connIDManager.pathDestination(m.id, true)
	if !ok {
		return nil // no spare CID yet; a later control frame wakes this pass
	}
	if err := p.attach(target); err != nil {
		return p.refuseRequest(request, err)
	}
	p.reserveHistory()
	request.accepted = true
	deadline := pathDeadline(now, pathTimeout)
	if d, ok := request.ctx.Deadline(); ok {
		if limit := now.Add(time.Until(d)); limit.Before(deadline) {
			deadline = limit
		}
	}
	candidate := p.newCandidate(m, m.addr, packetInfo{}, target, connID, deadline, now)
	candidate.probe.unlimited = true
	candidate.request, candidate.migrating = request, true
	p.conn.scheduleSending()
	return nil
}

// keepStandby records a validated extra tuple, retiring the CID of the one it replaces.
func (p *pathCreditRegistry) keepStandby(m *pathMember, candidate *pathCandidate) {
	if m.standby != nil && m.standby.connID != candidate.connID {
		p.conn.connIDManager.retirePathDestination(m.id, m.standby.connID, pathDeadline(monotime.Now(), threePathPTO(p.largestPTO(false))))
	}
	m.standby = &pathStandby{addr: candidate.addr, info: candidate.info, connID: candidate.connID}
	candidate.keepCID = true
}

// standbyCID is a validated tuple's path-local CID. Validation is connection-wide and
// grants none, so a tuple validated elsewhere takes a fresh one here.
func (p *pathCreditRegistry) standbyCID(m *pathMember, addr net.Addr, info packetInfo) (protocol.ConnectionID, bool) {
	manager := p.conn.connIDManager
	if m.standby != nil && addrsEqual(m.standby.addr, addr) && manager.pathDestinationUsable(m.id, m.standby.connID) {
		return m.standby.connID, true
	}
	connID, ok := manager.pathDestination(m.id, true)
	if !ok {
		return protocol.ConnectionID{}, false
	}
	if m.standby != nil {
		manager.retirePathDestination(m.id, m.standby.connID, pathDeadline(monotime.Now(), threePathPTO(p.largestPTO(false))))
	}
	m.standby = &pathStandby{addr: addr, info: info, connID: connID}
	return connID, true
}

// beginIncomingCutover follows a server's peer to a validated tuple. It only ever
// takes a free slot and a free unit, or leaves the peer's next packet to try again.
func (p *pathCreditRegistry) beginIncomingCutover(m *pathMember, addr net.Addr, info packetInfo, pn protocol.PacketNumber, now monotime.Time) {
	if p.conn.perspective != protocol.PerspectiveServer || !m.live || m.candidate != nil || p.terminals[m.id] != nil || p.pruneFences(m) || addrsEqual(addr, m.addr) {
		return
	}
	connID, ok := p.standbyCID(m, addr, info)
	if !ok || !p.reserveHistory() {
		return
	}
	p.attachments[m.transport].refs++
	candidate := p.newCandidate(m, addr, info, m.transport, connID, pathDeadline(now, pathTimeout), now)
	candidate.probe.validated, candidate.probe.mtuValidated, candidate.probe.unlimited = true, true, true
	candidate.migrating, candidate.fencing, candidate.keepCID = true, true, true
	candidate.moves, candidate.movePN = true, pn
	m.standby = nil
	m.established.retire()
	p.conn.scheduleSending()
}

// serviceCutover commits once every revoked fence on the member has resolved. The
// outcome swap decides between cancellation and a commit the caller can't undo.
func (p *pathCreditRegistry) serviceCutover(m *pathMember, now monotime.Time) {
	candidate := m.candidate
	if candidate == nil || !candidate.fencing || p.pruneFences(m) {
		return
	}
	request := candidate.request
	if request == nil && candidate.movePN != m.largestNonProbing {
		// The commit is the one place a server's move takes effect, so this is what
		// holds it to the highest non-probing packet, whenever a newer one arrived.
		candidate.keepCID = false
		p.retireCandidate(m)
		return
	}
	if !p.conn.connIDManager.pathDestinationUsable(m.id, candidate.connID) {
		// The peer retired it during the fence, so this attempt has nothing to cut over to.
		if request != nil {
			request.finish(errPathCandidateCIDRetired)
		}
		p.retireCandidate(m)
		return
	}
	if request != nil && !request.finish(nil) {
		p.retireCandidate(m)
		return
	}
	p.commitTuple(m, candidate, now)
	if request != nil {
		p.conn.pathOpen.CompareAndSwap(request, nil)
	}
}

// commitTuple is the one cutover. The largest PTO is read before the RTT resets, since
// the moving path can be its source. The old tuple's charges become a retained entry,
// and the attempt's target reference becomes the member's.
func (p *pathCreditRegistry) commitTuple(m *pathMember, candidate *pathCandidate, now monotime.Time) {
	retention := threePathPTO(p.largestPTO(true))
	expiry := pathDeadline(now, retention)
	c := p.conn
	c.emission.leaveRoute()
	p.retain(m.transport, expiry, nil)
	moved := candidate.transport != m.transport
	m.candidate, m.standby = nil, nil
	m.established = &candidate.probe
	m.established.clearChallenges()
	m.transport, m.addr, m.info = candidate.transport, candidate.addr, candidate.info
	c.connIDManager.activatePathDestination(m.id, candidate.connID, pathDeadline(now, threePathPTO(p.largestPTO(false))))
	recovery := p.recoveryOn(m.id)
	if m.id == 0 {
		c.pathGeneration++
		m.tuple = c.pathGeneration
		c.emission.resetLocalPath(c.pathGeneration, now)
		c.resetPathPacketSize(now)
		if moved {
			c.emission.moveLocalZero(m.transport, m.addr, m.info, newSendConn(p.transportConn(m.transport), m.addr, m.info, utils.DefaultLogger))
		} else {
			c.emission.rebindLocalZero(m.addr, m.info)
		}
		c.startMTUDiscovery(now)
	} else {
		m.tuple++
		recovery.MigratedPath(now, protocol.MinInitialPacketSize)
		c.emission.local.resetWriteState(&m.route)
		p.startPMTU(m, m.transport, now)
	}
	if ecn, ok := recovery.(interface{ RestartTupleECN() }); ok {
		ecn.RestartTupleECN()
	}
	p.recordValidated(m.addr)
	p.observe(m, "migrated")
	p.noteLiveness(m, now)
	if c.perspective == protocol.PerspectiveClient {
		m.announcing, m.announces, m.announceAt = true, 0, now
	}
	c.scheduleSending()
}

// transportConn is the socket a path-zero move publishes.
func (p *pathCreditRegistry) transportConn(target *Transport) rawConn {
	if target == nil {
		return p.origin.conn
	}
	return target.conn
}

// nextAnnouncement is a member owing its new tuple a non-probing packet now. An idle
// backup has no other traffic there, and another path's can't substitute.
func (p *pathCreditRegistry) nextAnnouncement(now monotime.Time) *pathMember {
	var ordered [maxPathHistoryUnits + 1]*pathMember
	for _, m := range p.orderedMembers(ordered[:0]) {
		if m.announcing && !m.announceAt.After(now) && !m.fencing() && p.primaryEligible(m) {
			return m
		}
	}
	return nil
}

// noteAnnounced schedules the retry, for an attempt sent or one the endpoint couldn't
// take. Only a current-tuple acknowledgment or the path's end discharges the obligation,
// since a local refusal leaves nothing outstanding for recovery's PTO to probe.
func (p *pathCreditRegistry) noteAnnounced(m *pathMember, now monotime.Time) {
	m.announces = min(m.announces+1, maxPathAnnounceBackoff)
	m.announceAt = pathDeadline(now, p.rttOf(m.id).PTO(true)<<m.announces)
}
