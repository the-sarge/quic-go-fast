package quic

import (
	"fmt"
	"slices"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/internal/wire"
)

type connRunnerCallbacks struct {
	AddConnectionID     func(protocol.ConnectionID)
	AddPathConnectionID func(protocol.ConnectionID, protocol.PathID) bool
	RemoveConnectionID  func(protocol.ConnectionID)
	ReplaceWithClosed   func([]protocol.ConnectionID, []byte, time.Duration)
	// ReplaceWithClosedPaths is set on every transport that can route path CIDs.
	ReplaceWithClosedPaths func([]protocol.ConnectionID, *closedPaths, bool, time.Duration)
}

// The memory address of the Transport is used as the key.
type connRunners map[connRunner]connRunnerCallbacks

func (cr connRunners) AddConnectionID(id protocol.ConnectionID) {
	for _, c := range cr {
		c.AddConnectionID(id)
	}
}

func (cr connRunners) RemoveConnectionID(id protocol.ConnectionID) {
	for _, c := range cr {
		c.RemoveConnectionID(id)
	}
}

func (cr connRunners) ReplaceWithClosed(ids []protocol.ConnectionID, b []byte, expiry time.Duration) {
	for _, c := range cr {
		c.ReplaceWithClosed(ids, b, expiry)
	}
}

type connIDToRetire struct {
	t      monotime.Time
	connID protocol.ConnectionID
}

type connIDGenerator struct {
	pathCIDs    map[protocol.PathID]*issuedPathCIDs
	generator   ConnectionIDGenerator
	highestSeq  uint64
	connRunners connRunners
	ownRunner   connRunner // the runner of a member whose transport is nil

	activeSrcConnIDs        map[uint64]protocol.ConnectionID
	connIDsToRetire         []connIDToRetire       // sorted by t
	initialClientDestConnID *protocol.ConnectionID // nil for the client

	statelessResetter *statelessResetter

	queueControlFrame func(wire.Frame)
}

func newConnIDGenerator(
	runner connRunner,
	initialConnectionID protocol.ConnectionID,
	initialClientDestConnID *protocol.ConnectionID, // nil for the client
	statelessResetter *statelessResetter,
	callbacks connRunnerCallbacks,
	queueControlFrame func(wire.Frame),
	generator ConnectionIDGenerator,
) *connIDGenerator {
	m := &connIDGenerator{
		generator:         generator,
		activeSrcConnIDs:  make(map[uint64]protocol.ConnectionID),
		statelessResetter: statelessResetter,
		connRunners:       map[connRunner]connRunnerCallbacks{runner: callbacks},
		ownRunner:         runner,
		queueControlFrame: queueControlFrame,
	}
	m.activeSrcConnIDs[0] = initialConnectionID
	m.initialClientDestConnID = initialClientDestConnID
	return m
}

func (m *connIDGenerator) SetMaxActiveConnIDs(limit uint64) error {
	if m.pathCIDs != nil {
		return m.issuePathCIDs(0, min(limit, 2))
	}
	if m.generator.ConnectionIDLen() == 0 {
		return nil
	}
	// The active_connection_id_limit transport parameter is the number of
	// connection IDs the peer will store. This limit includes the connection ID
	// used during the handshake, and the one sent in the preferred_address
	// transport parameter.
	// We currently don't send the preferred_address transport parameter,
	// so we can issue (limit - 1) connection IDs.
	for i := uint64(len(m.activeSrcConnIDs)); i < min(limit, protocol.MaxIssuedConnectionIDs); i++ {
		if err := m.issueNewConnID(); err != nil {
			return err
		}
	}
	return nil
}

func (m *connIDGenerator) Retire(seq uint64, sentWithDestConnID protocol.ConnectionID, expiry monotime.Time) error {
	if m.pathCIDs != nil {
		return m.retirePathCID(&wire.PathRetireConnectionIDFrame{SequenceNumber: seq}, sentWithDestConnID, expiry)
	}
	if seq > m.highestSeq {
		return &qerr.TransportError{
			ErrorCode:    qerr.ProtocolViolation,
			ErrorMessage: fmt.Sprintf("retired connection ID %d (highest issued: %d)", seq, m.highestSeq),
		}
	}
	connID, ok := m.activeSrcConnIDs[seq]
	// We might already have deleted this connection ID, if this is a duplicate frame.
	if !ok {
		return nil
	}
	if connID == sentWithDestConnID {
		return &qerr.TransportError{
			ErrorCode:    qerr.ProtocolViolation,
			ErrorMessage: fmt.Sprintf("retired connection ID %d (%s), which was used as the Destination Connection ID on this packet", seq, connID),
		}
	}
	m.queueConnIDForRetiring(connID, expiry)

	delete(m.activeSrcConnIDs, seq)
	// Don't issue a replacement for the initial connection ID.
	if seq == 0 {
		return nil
	}
	return m.issueNewConnID()
}

func (m *connIDGenerator) queueConnIDForRetiring(connID protocol.ConnectionID, expiry monotime.Time) {
	idx := slices.IndexFunc(m.connIDsToRetire, func(c connIDToRetire) bool {
		return c.t.After(expiry)
	})
	if idx == -1 {
		idx = len(m.connIDsToRetire)
	}
	m.connIDsToRetire = slices.Insert(m.connIDsToRetire, idx, connIDToRetire{t: expiry, connID: connID})
}

func (m *connIDGenerator) issueNewConnID() error {
	connID, err := m.generator.GenerateConnectionID()
	if err != nil {
		return err
	}
	m.activeSrcConnIDs[m.highestSeq+1] = connID
	m.connRunners.AddConnectionID(connID)
	m.queueControlFrame(&wire.NewConnectionIDFrame{
		SequenceNumber:      m.highestSeq + 1,
		ConnectionID:        connID,
		StatelessResetToken: m.statelessResetter.GetStatelessResetToken(connID),
	})
	m.highestSeq++
	return nil
}

func (m *connIDGenerator) SetHandshakeComplete(connIDExpiry monotime.Time) {
	if m.initialClientDestConnID != nil {
		m.queueConnIDForRetiring(*m.initialClientDestConnID, connIDExpiry)
		m.initialClientDestConnID = nil
	}
}

func (m *connIDGenerator) RemoveRetiredConnIDs(now monotime.Time) {
	if len(m.connIDsToRetire) == 0 {
		return
	}
	for _, c := range m.connIDsToRetire {
		if c.t.After(now) {
			break
		}
		m.connRunners.RemoveConnectionID(c.connID)
		m.connIDsToRetire = m.connIDsToRetire[1:]
	}
}

func (m *connIDGenerator) RemoveAll() {
	for _, cid := range m.extraPathCIDs() {
		m.connRunners.RemoveConnectionID(cid)
	}
	if m.initialClientDestConnID != nil {
		m.connRunners.RemoveConnectionID(*m.initialClientDestConnID)
	}
	for _, connID := range m.activeSrcConnIDs {
		m.connRunners.RemoveConnectionID(connID)
	}
	for _, c := range m.connIDsToRetire {
		m.connRunners.RemoveConnectionID(c.connID)
	}
}

func (m *connIDGenerator) ReplaceWithClosed(connClose []byte, expiry time.Duration) {
	m.connRunners.ReplaceWithClosed(m.closingConnIDs(), connClose, expiry)
}

// ReplaceWithClosedPaths installs one cached close on every attached transport.
func (m *connIDGenerator) ReplaceWithClosedPaths(closed *closedPaths, expiry time.Duration) {
	connIDs := m.closingConnIDs()
	for runner, callbacks := range m.connRunners {
		if callbacks.ReplaceWithClosedPaths == nil {
			callbacks.ReplaceWithClosed(connIDs, nil, expiry)
			continue
		}
		callbacks.ReplaceWithClosedPaths(connIDs, closed, runner == m.ownRunner, expiry)
	}
}

func (m *connIDGenerator) closingConnIDs() []protocol.ConnectionID {
	connIDs := make([]protocol.ConnectionID, 0, len(m.activeSrcConnIDs)+len(m.connIDsToRetire)+1)
	if m.initialClientDestConnID != nil {
		connIDs = append(connIDs, *m.initialClientDestConnID)
	}
	for _, connID := range m.activeSrcConnIDs {
		connIDs = append(connIDs, connID)
	}
	for _, c := range m.connIDsToRetire {
		connIDs = append(connIDs, c.connID)
	}
	return append(connIDs, m.extraPathCIDs()...)
}

// addPathRunner attaches a transport under private multipath, retaining each issued
// CID's path binding. A refusal leaves the transport and this generator untouched.
func (m *connIDGenerator) addPathRunner(runner *packetHandlerMap, r connRunnerCallbacks, tokens []protocol.StatelessResetToken, handler packetHandler) error {
	var routes []pathRouteEntry
	for id, set := range m.pathCIDs {
		for _, cid := range set.active {
			routes = append(routes, pathRouteEntry{id: cid, path: id})
		}
		for _, entry := range set.retiring {
			routes = append(routes, pathRouteEntry{id: entry.connID, path: id})
		}
	}
	if err := runner.attachRoutes(routes, tokens, handler); err != nil {
		return err
	}
	m.connRunners[runner] = r
	return nil
}

// routedCIDs lists every CID a runner of this connection holds.
func (m *connIDGenerator) routedCIDs() []protocol.ConnectionID {
	ids := m.extraPathCIDs()
	for _, cid := range m.activeSrcConnIDs {
		ids = append(ids, cid)
	}
	for _, entry := range m.connIDsToRetire {
		ids = append(ids, entry.connID)
	}
	if m.initialClientDestConnID != nil {
		ids = append(ids, *m.initialClientDestConnID)
	}
	return ids
}

func (m *connIDGenerator) AddConnRunner(runner connRunner, r connRunnerCallbacks) error {
	if m.pathCIDs != nil {
		return fmt.Errorf("legacy migration is unavailable with private multipath credit")
	}
	// The transport might have already been added earlier.
	// This happens if the application migrates back to and old path.
	if _, ok := m.connRunners[runner]; ok {
		return nil
	}
	m.connRunners[runner] = r
	if m.initialClientDestConnID != nil {
		r.AddConnectionID(*m.initialClientDestConnID)
	}
	for _, connID := range m.activeSrcConnIDs {
		r.AddConnectionID(connID)
	}
	return nil
}
