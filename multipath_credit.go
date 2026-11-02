package quic

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
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
	peerCIDLimit uint64
	terminals    map[protocol.PathID]*pathTerminal
	paths        map[protocol.PathID]bool // true only for charged live/validating slots
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
// neither call it nor advertise multipath. R9 owns consumed-path admission.
func (c *Conn) enableMultipathCredit(capacity int, peerMax protocol.PathID, peerCIDLimit uint64) (_ *wire.TransportParameters, err error) {
	if c.pathCredit != nil || !c.handshakeConfirmed || c.version != protocol.Version1 || capacity < 1 || capacity > 2 || peerCIDLimit < 2 || c.emission.bbr != nil || c.connIDManager.closed || c.connIDManager.activeConnectionID.Len() == 0 || c.srcConnIDLen == 0 || c.connIDGenerator.generator.ConnectionIDLen() != c.srcConnIDLen || c.pathManager != nil || c.pathManagerOutgoing.Load() != nil || len(c.connIDManager.pathProbing) != 0 || uint64(len(c.connIDGenerator.activeSrcConnIDs)) > min(peerCIDLimit, 2) || len(c.connIDGenerator.activeSrcConnIDs)+len(c.connIDGenerator.connIDsToRetire) > maxPathCIDRecords {
		return nil, fmt.Errorf("invalid private multipath credit activation")
	}
	p := &pathCreditRegistry{conn: c, capacity: capacity, localMax: protocol.PathID(capacity - 1), peerMax: peerMax, peerCIDLimit: peerCIDLimit, paths: map[protocol.PathID]bool{0: true}, terminals: make(map[protocol.PathID]*pathTerminal)}
	c.pathCredit = p
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
	if p.localMax == 0 || p.peerMax == 0 || len(p.paths) >= p.capacity || p.terminals[1] != nil {
		return nil
	}
	p.paths[1] = false
	p.conn.connIDGenerator.pathCIDs[1] = &issuedPathCIDs{active: make(map[uint64]protocol.ConnectionID)}
	return p.conn.connIDGenerator.issuePathCIDs(1, min(p.peerCIDLimit, 2))
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
	if id == 0 {
		return &qerr.TransportError{ErrorCode: qerr.InternalError, ErrorMessage: "last usable path abandoned"}
	}
	terminal := p.terminals[id]
	if terminal == nil {
		terminal = p.newTerminal()
		p.terminals[id] = terminal
		p.conn.queueControlFrame(&wire.PathAbandonFrame{PathID: id})
	}
	if !terminal.expired && !terminal.received {
		terminal.received = true
		terminal.expiry = pathDeadline(now, threePathPTO(p.conn.observedRTT().PTO(true)))
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
	p.conn.connIDManager.expirePathCIDs(now)
	p.conn.connIDGenerator.expirePathCIDs(now)
	for id, terminal := range p.terminals {
		if terminal.expired {
			continue
		}
		if !terminal.received {
			if accepted := terminal.receipt.acceptedAt(); accepted != 0 && terminal.unanswered == 0 {
				// Snapshot policy once, when acceptance first becomes known to Conn.
				terminal.unanswered = pathDeadline(accepted, max(30*time.Second, threePathPTO(p.conn.observedRTT().PTO(true))))
			}
			if terminal.unanswered != 0 && !terminal.unanswered.After(now) {
				return &qerr.TransportError{ErrorCode: qerr.InternalError, ErrorMessage: "unanswered path abandonment"}
			}
		}
		if !terminal.expired && terminal.expiry != 0 && !terminal.expiry.After(now) {
			p.conn.connIDGenerator.releasePathCIDs(id)
			p.conn.connIDManager.releasePathCIDs(id)
			terminal.expired = true
			terminal.receipt.seal()
		}
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
	for _, terminal := range p.terminals {
		if !terminal.received && terminal.unanswered != 0 && (next == 0 || terminal.unanswered < next) {
			next = terminal.unanswered
		}
		if !terminal.expired && terminal.expiry != 0 && (next == 0 || terminal.expiry < next) {
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
	delete(m.pathCIDs, id)
}

func (p *pathCreditRegistry) receiveCID(f *wire.PathNewConnectionIDFrame, now monotime.Time) error {
	if err := p.validateID(f.PathID); err != nil {
		return err
	}
	if p.terminals[f.PathID] != nil {
		return nil
	}
	err := p.conn.connIDManager.addPathCID(f, pathDeadline(now, threePathPTO(p.conn.observedRTT().PTO(false))))
	return p.resourceResult(f.PathID, err)
}

func (p *pathCreditRegistry) resourceResult(id protocol.PathID, err error) error {
	if !errors.Is(err, errPathCIDCapacity) {
		return err
	}
	if id == 0 {
		return &qerr.TransportError{ErrorCode: qerr.InternalError, ErrorMessage: err.Error() + " on last usable path"}
	}
	if p.terminals[id] == nil {
		p.terminals[id] = p.newTerminal()
		if set := p.conn.connIDManager.pathCIDs[id]; set != nil {
			set.terminal = true
		}
		if set := p.conn.connIDGenerator.pathCIDs[id]; set != nil {
			set.terminal = true
		}
		p.conn.queueControlFrame(&wire.PathAbandonFrame{PathID: id, ErrorCode: 0x3e75})
	}
	return nil
}

func (p *pathCreditRegistry) retireCID(f *wire.PathRetireConnectionIDFrame, dest protocol.ConnectionID, now monotime.Time) error {
	if err := p.validateID(f.PathID); err != nil {
		return err
	}
	if p.terminals[f.PathID] != nil {
		return nil
	}
	return p.resourceResult(f.PathID, p.conn.connIDGenerator.retirePathCID(f, dest, pathDeadline(now, threePathPTO(p.conn.observedRTT().PTO(false)))))
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
