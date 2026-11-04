package quic

import (
	"errors"
	"fmt"
	"math"
	"net"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
)

// pathCreditRegistry belongs exclusively to the connection goroutine. An issued
// ID has no protection, recovery or data-sending authority until admission.
type pathCreditRegistry struct {
	conn         *Conn
	closed       bool
	capacity     int
	localMax     protocol.PathID
	peerMax      protocol.PathID
	nextID       protocol.PathID // lowest ID never issued or abandoned; IDs only rise
	peerCIDLimit uint64
	terminals    map[protocol.PathID]*pathTerminal
	paths        map[protocol.PathID]bool // true only for charged live/validating slots

	// Admission state exists only on the private endpoint route.
	members      map[protocol.PathID]*pathMember
	validated    []net.Addr
	rtt          map[protocol.PathID]*utils.RTTStats
	historyUnits int
	attachments  map[*Transport]int
	now          monotime.Time
	primary      protocol.PathID
}

type issuedPathCIDs struct {
	next     uint64
	active   map[uint64]protocol.ConnectionID
	retiring []connIDToRetire
	terminal bool
}

func connectionRunnerCallbacks(runner connRunner, c *Conn) connRunnerCallbacks {
	callbacks := connRunnerCallbacks{
		AddConnectionID:    func(id protocol.ConnectionID) { runner.Add(id, c) },
		RemoveConnectionID: runner.Remove,
		ReplaceWithClosed:  runner.ReplaceWithClosed,
	}
	if paths, ok := runner.(interface {
		AddPath(protocol.ConnectionID, protocol.PathID, packetHandler) bool
	}); ok {
		callbacks.AddPathConnectionID = func(id protocol.ConnectionID, path protocol.PathID) bool { return paths.AddPath(id, path, c) }
	}
	return callbacks
}

// enableMultipathCredit is private scenario activation. Production constructors
// neither call it nor advertise multipath. A connection already on the private
// endpoint route gains consumed-path admission; any other keeps unused-ID credit.
func (c *Conn) enableMultipathCredit(capacity int, peerMax protocol.PathID, peerCIDLimit uint64) (_ *wire.TransportParameters, err error) {
	if c.pathCredit != nil || !c.handshakeConfirmed || c.version != protocol.Version1 || capacity < 1 || capacity > 2 || peerCIDLimit < 2 || c.emission.bbr != nil || c.connIDManager.closed || c.connIDManager.activeConnectionID.Len() == 0 || c.srcConnIDLen == 0 || c.connIDGenerator.generator.ConnectionIDLen() != c.srcConnIDLen || c.pathManager != nil || c.pathManagerOutgoing.Load() != nil || len(c.connIDManager.pathProbing) != 0 || uint64(len(c.connIDGenerator.activeSrcConnIDs)) > min(peerCIDLimit, 2) || len(c.connIDGenerator.activeSrcConnIDs)+len(c.connIDGenerator.connIDsToRetire) > maxPathCIDRecords {
		return nil, fmt.Errorf("invalid private multipath credit activation")
	}
	p := &pathCreditRegistry{conn: c, capacity: capacity, localMax: protocol.PathID(capacity - 1), peerMax: peerMax, nextID: 1, peerCIDLimit: peerCIDLimit, paths: map[protocol.PathID]bool{0: true}, terminals: make(map[protocol.PathID]*pathTerminal)}
	c.pathCredit = p
	p.enableAdmission()
	defer func() {
		if err != nil {
			// Once publication begins, rollback would erase routing obligations.
			// Fence the private activation and let ordinary close own cleanup.
			p.close()
			c.closeLocal(&qerr.TransportError{ErrorCode: qerr.InternalError, ErrorMessage: err.Error()})
		}
	}()
	c.emission.pathCredit = p
	c.connIDManager.enablePathCIDs()
	c.connIDGenerator.pathCIDs = map[protocol.PathID]*issuedPathCIDs{0: {next: c.connIDGenerator.highestSeq + 1, active: c.connIDGenerator.activeSrcConnIDs, retiring: c.connIDGenerator.connIDsToRetire}}
	c.connIDGenerator.connIDsToRetire = nil
	if err := c.connIDGenerator.issuePathCIDs(0, min(peerCIDLimit, 2)); err != nil {
		return nil, err
	}
	if err := p.issueWindow(); err != nil {
		return nil, err
	}
	c.frameParser.EnableMultipath()
	localMax := p.localMax
	return &wire.TransportParameters{InitialMaxPathID: &localMax, ActiveConnectionIDLimit: 4}, nil
}

func (p *pathCreditRegistry) issueWindow() error {
	id := p.nextID
	if id > p.localMax || id > p.peerMax || len(p.paths) >= p.capacity {
		return nil
	}
	if err := p.admitIssuedKeys(id); err != nil {
		return err
	}
	p.nextID = id + 1
	p.paths[id] = false
	p.conn.connIDGenerator.pathCIDs[id] = &issuedPathCIDs{active: make(map[uint64]protocol.ConnectionID)}
	return p.conn.connIDGenerator.issuePathCIDs(id, min(p.peerCIDLimit, 2))
}

// replenish moves each fenced terminal out of its live slot once it holds a history
// unit, then raises the grant one step. Withholding the grant while the pool is full
// is what blocks a replacement: it never reaches a decline the registry could foresee.
func (p *pathCreditRegistry) replenish() error {
	for id, terminal := range p.terminals {
		if terminal.signaled && !terminal.slotFree {
			if !terminal.unit {
				if !p.reserveHistory() {
					continue
				}
				terminal.unit = true
			}
			delete(p.paths, id)
			terminal.slotFree = true
		}
	}
	if len(p.paths) < p.capacity && p.localMax < p.nextID && p.historyUnits < maxPathHistoryUnits {
		p.localMax++
		p.conn.queueControlFrame(&wire.MaxPathIDFrame{MaximumPathID: p.localMax})
		p.conn.scheduleSending()
	}
	return p.issueWindow()
}

// released reports an ID whose terminal was reclaimed. IDs only rise, so it needs no
// record, and only a frame's sender can still name it.
func (p *pathCreditRegistry) released(id protocol.PathID) bool {
	if !p.hasAdmission() || id >= p.nextID || p.terminals[id] != nil {
		return false
	}
	_, charged := p.paths[id]
	return !charged
}

func (m *connIDGenerator) issuePathCIDs(id protocol.PathID, target uint64) error {
	set := m.pathCIDs[id]
	for uint64(len(set.active)) < target {
		if len(set.active)+len(set.retiring) >= maxPathCIDRecords {
			return errPathCIDCapacity
		}
		cid, err := m.generator.GenerateConnectionID()
		if err != nil {
			return err
		}
		var added []connRunnerCallbacks
		for _, callbacks := range m.connRunners {
			if callbacks.AddPathConnectionID == nil || !callbacks.AddPathConnectionID(cid, id) {
				for _, previous := range added {
					previous.RemoveConnectionID(cid)
				}
				return fmt.Errorf("cannot register path %d connection ID", id)
			}
			added = append(added, callbacks)
		}
		seq := set.next
		set.next++
		set.active[seq] = cid
		m.queueControlFrame(&wire.PathNewConnectionIDFrame{PathID: id, SequenceNumber: seq, ConnectionID: cid, StatelessResetToken: m.statelessResetter.GetStatelessResetToken(cid)})
	}
	return nil
}

// consume charges the initial issued window before an initiation can proceed.
// A consumed ID never returns to the unused window, even after failure.
func (p *pathCreditRegistry) consume(id protocol.PathID) bool {
	charged, ok := p.paths[id]
	if !ok || charged || p.terminals[id] != nil {
		return false
	}
	p.paths[id] = true
	return true
}

func (p *pathCreditRegistry) validateID(id protocol.PathID) error {
	if id > p.localMax {
		return &qerr.TransportError{ErrorCode: qerr.ProtocolViolation, ErrorMessage: "path ID exceeds granted credit"}
	}
	return nil
}

type pathTerminal struct {
	expiry     monotime.Time
	expired    bool
	received   bool
	unanswered monotime.Time
	receipt    *pathAbandonReceipt
	code       uint64
	signaled   bool // PATH_ABANDON is queued; the submission fence has resolved
	unit       bool // holds a retained-history unit
	slotFree   bool // has left the live slot
	pending    bool // reclamation waits for a local result, which wakes the connection
}

func threePathPTO(pto time.Duration) time.Duration {
	if pto > math.MaxInt64/3 {
		return time.Duration(math.MaxInt64)
	}
	return 3 * pto
}

func (p *pathCreditRegistry) abandon(id protocol.PathID, now monotime.Time) error {
	if err := p.validateID(id); err != nil {
		return err
	}
	if p.released(id) {
		return nil
	}
	terminal := p.terminals[id]
	if terminal == nil {
		if p.isLastPath(id) {
			return &qerr.TransportError{ErrorCode: qerr.InternalError, ErrorMessage: "last usable path abandoned"}
		}
		terminal = p.beginTerminal(id, 0, ErrPathClosed)
	}
	if !terminal.expired && !terminal.received {
		terminal.received = true
		terminal.expiry = pathDeadline(now, threePathPTO(p.largestPTO(true)))
		if set := p.conn.connIDManager.pathCIDs[id]; set != nil {
			set.terminal = true
		}
		if set := p.conn.connIDGenerator.pathCIDs[id]; set != nil {
			set.terminal = true
		}
	}
	return nil
}

func (p *pathCreditRegistry) expire(now monotime.Time) error {
	if p.closed {
		return nil
	}
	if err := p.service(now); err != nil {
		return err
	}
	p.conn.connIDManager.expirePathCIDs(now)
	p.conn.connIDGenerator.expirePathCIDs(now)
	for id, terminal := range p.terminals {
		if terminal.expired {
			continue
		}
		if !terminal.received {
			if accepted := terminal.receipt.acceptedAt(); accepted != 0 && terminal.unanswered == 0 {
				// Snapshot policy once, when acceptance first becomes known to Conn.
				terminal.unanswered = pathDeadline(accepted, max(30*time.Second, threePathPTO(p.largestPTO(true))))
			}
			if terminal.unanswered != 0 && !terminal.unanswered.After(now) {
				return &qerr.TransportError{ErrorCode: qerr.InternalError, ErrorMessage: "unanswered path abandonment"}
			}
		}
		// Retention alone releases nothing: the signal proves the fence resolved.
		if !terminal.expired && terminal.signaled && terminal.expiry != 0 && !terminal.expiry.After(now) {
			p.reclaim(id, terminal, now)
		}
	}
	if p.hasAdmission() {
		return p.replenish() // reclamation can free the unit or slot a grant waits for
	}
	return nil
}

func (p *pathCreditRegistry) nextExpiry() monotime.Time {
	if p.closed {
		return 0
	}
	var next monotime.Time
	take := func(t monotime.Time) {
		if t != 0 && (next == 0 || t < next) {
			next = t
		}
	}
	for _, set := range p.conn.connIDGenerator.pathCIDs {
		if !set.terminal {
			for _, entry := range set.retiring {
				take(entry.t)
			}
		}
	}
	for _, set := range p.conn.connIDManager.pathCIDs {
		if !set.terminal {
			for _, entry := range set.records {
				if entry.retired {
					take(entry.expiry)
				}
			}
		}
	}
	take(p.nextAdmissionExpiry())
	for _, terminal := range p.terminals {
		if !terminal.received && terminal.unanswered != 0 && (next == 0 || terminal.unanswered < next) {
			next = terminal.unanswered
		}
		// An unresolved fence wakes through endpoint completion, not an elapsed timer.
		if !terminal.expired && !terminal.pending && terminal.signaled && terminal.expiry != 0 && (next == 0 || terminal.expiry < next) {
			next = terminal.expiry
		}
	}
	return next
}

func (m *connIDGenerator) releasePathCIDs(id protocol.PathID) {
	set := m.pathCIDs[id]
	if set == nil {
		return
	}
	for _, cid := range set.active {
		m.connRunners.RemoveConnectionID(cid)
	}
	for _, retired := range set.retiring {
		m.connRunners.RemoveConnectionID(retired.connID)
	}
	// Path zero's set aliases the legacy map, which close would otherwise re-register.
	clear(set.active)
	delete(m.pathCIDs, id)
}

func (p *pathCreditRegistry) receiveCID(f *wire.PathNewConnectionIDFrame, now monotime.Time) error {
	if err := p.validateID(f.PathID); err != nil {
		return err
	}
	if p.terminals[f.PathID] != nil || p.released(f.PathID) {
		return nil
	}
	err := p.conn.connIDManager.addPathCID(f, pathDeadline(now, threePathPTO(p.largestPTO(false))))
	return p.resourceResult(f.PathID, err)
}

func (p *pathCreditRegistry) resourceResult(id protocol.PathID, err error) error {
	if !errors.Is(err, errPathCIDCapacity) {
		return err
	}
	if p.isLastPath(id) {
		return &qerr.TransportError{ErrorCode: qerr.InternalError, ErrorMessage: err.Error() + " on last usable path"}
	}
	p.decline(id, pathResourceLimitReached, ErrMultipathResourceLimitV1)
	return nil
}

// decline closes id through the one terminal owner. Its signal follows any
// outstanding submission fence, and unanswered abandonment keeps its obligations.
func (p *pathCreditRegistry) decline(id protocol.PathID, code uint64, err error) {
	if p.terminals[id] != nil {
		return
	}
	if set := p.conn.connIDManager.pathCIDs[id]; set != nil {
		set.terminal = true
	}
	if set := p.conn.connIDGenerator.pathCIDs[id]; set != nil {
		set.terminal = true
	}
	p.beginTerminal(id, code, err)
}

// beginTerminal is the one entry into retirement. A committed path already holds its
// history unit; path zero and an unused ID reserve one before leaving their slot.
func (p *pathCreditRegistry) beginTerminal(id protocol.PathID, code uint64, err error) *pathTerminal {
	terminal := p.newTerminal()
	terminal.code = code
	terminal.unit = id != 0 && p.members[id] != nil
	p.terminals[id] = terminal
	p.nextID = max(p.nextID, id+1)
	p.retireMember(id, err)
	p.moveObservation(id)
	p.signalTerminal(id, terminal)
	return terminal
}

// isLastPath reports that retiring id would leave no path able to carry the
// connection. An ID that was never live can't be the last.
func (p *pathCreditRegistry) isLastPath(id protocol.PathID) bool {
	if m := p.members[id]; id != 0 && (m == nil || !m.live) {
		return false
	}
	for other, m := range p.members {
		if other != id && p.eligible(m) {
			return false
		}
	}
	return true
}

// reclaim ends a retained path once nothing can still need it. An Accepted result the
// connection hasn't consumed leaves frames only this terminal can drain, so the
// endpoint's resolved fence isn't enough. Its completion wakes the next attempt.
func (p *pathCreditRegistry) reclaim(id protocol.PathID, terminal *pathTerminal, now monotime.Time) {
	if p.hasAdmission() && p.members[id] != nil {
		p.rescheduleUnusable(id, now)
		if !p.sentPaths().PathQuiescent(id) {
			terminal.pending = true
			return
		}
	}
	terminal.pending = false
	p.conn.connIDGenerator.releasePathCIDs(id)
	p.conn.connIDManager.releasePathCIDs(id)
	terminal.expired = true
	terminal.receipt.seal()
	p.releaseMember(id)
	if !p.hasAdmission() {
		return // the credit-only fixture has one window and keeps its record
	}
	if terminal.unit {
		p.releaseHistory()
	}
	delete(p.paths, id)
	delete(p.terminals, id)
}

func (p *pathCreditRegistry) retireCID(f *wire.PathRetireConnectionIDFrame, dest protocol.ConnectionID, now monotime.Time) error {
	if err := p.validateID(f.PathID); err != nil {
		return err
	}
	if p.terminals[f.PathID] != nil || p.released(f.PathID) {
		return nil
	}
	return p.resourceResult(f.PathID, p.conn.connIDGenerator.retirePathCID(f, dest, pathDeadline(now, threePathPTO(p.largestPTO(false)))))
}

func (p *pathCreditRegistry) cidsBlocked(f *wire.PathCIDsBlockedFrame) error {
	if err := p.validateID(f.PathID); err != nil {
		return err
	}
	set := p.conn.connIDGenerator.pathCIDs[f.PathID]
	if p.terminals[f.PathID] != nil || set == nil || f.NextSequenceNumber < set.next {
		return nil
	}
	if f.NextSequenceNumber > set.next {
		return &qerr.TransportError{ErrorCode: qerr.ProtocolViolation, ErrorMessage: "CID request exceeds issued sequence"}
	}
	if uint64(len(set.active)) >= min(p.peerCIDLimit, 4) {
		return nil // wait for retirement; peer credit is separate from local retention
	}
	return p.resourceResult(f.PathID, p.conn.connIDGenerator.issuePathCIDs(f.PathID, uint64(len(set.active))+1))
}
