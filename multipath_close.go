package quic

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/qlog"
)

// observe records one path transition as copied facts.
func (p *pathCreditRegistry) observe(m *pathMember, state string) {
	if p.conn.qlogger != nil && m != nil {
		p.conn.qlogger.RecordEvent(qlog.PathLifecycleUpdated{Path: qlog.PathIdentity{PathID: uint64(m.id), TupleGeneration: m.tuple}, State: state})
	}
}

// qlogZero names path zero for its MTU owner, on the connection goroutine.
func (p *pathCreditRegistry) qlogZero() *qlog.PathIdentity { return p.qlogIngress(0) }

// qlogIngress names the path a packet arrived on. A nil registry is the ordinary
// connection, and a path whose member has gone reports its ID alone.
func (p *pathCreditRegistry) qlogIngress(id protocol.PathID) *qlog.PathIdentity {
	if p == nil || !p.hasAdmission() {
		return nil
	}
	identity := &qlog.PathIdentity{PathID: uint64(id)}
	if m := p.members[id]; m != nil {
		identity.TupleGeneration = m.tuple
	}
	return identity
}

var errNoClosePath = errors.New("quic: no path can carry the close")

// closeResponse is one path's retained CONNECTION_CLOSE, with the identity it was
// sealed under. Its tuple and transport are that path's own, not a trigger's.
type closeResponse struct {
	path      protocol.PathID
	packet    []byte
	addr      net.Addr
	info      packetInfo
	transport *Transport // nil is the connection's own transport
	enqueue   atomic.Pointer[func(*closeResponse)]
}

// closedPaths is a multipath connection's cached close, shared by the closed handler
// on every transport. One backoff counter bounds the replies across all of them.
type closedPaths struct {
	counter   atomic.Uint32
	responses []*closeResponse
	logger    utils.Logger
}

// bind publishes how a response reaches its transport's socket. A trigger that
// arrives before its response's transport is bound is dropped, as a full queue is.
func (s *closedPaths) bind(t *Transport, own bool, enqueue func(*closeResponse)) {
	for _, response := range s.responses {
		if response.transport == t || (own && response.transport == nil) {
			response.enqueue.Store(&enqueue)
		}
	}
}

// respond answers a trigger through its path's retained response. Any other path,
// abandoned or still validating, has none, and the lowest retained path answers.
func (s *closedPaths) respond(path protocol.PathID) {
	if len(s.responses) == 0 {
		return
	}
	if !closeBackoffAllows(s.counter.Add(1)) {
		return
	}
	response := s.responses[0]
	for _, candidate := range s.responses {
		if candidate.path == path {
			response = candidate
			break
		}
	}
	if enqueue := response.enqueue.Load(); enqueue != nil {
		s.logger.Debugf("Retransmitting CONNECTION_CLOSE on path %d.", response.path)
		(*enqueue)(response)
	}
}

// pathFailures holds the first endpoint failure reported to the connection. There are
// two producers, and neither decides: the sender's fault reading on the connection
// goroutine, and a transport's read failure on its own. The endpoints themselves say
// which paths it stopped, so repeats per connection ID or per transport change nothing.
type pathFailures struct {
	mu     sync.Mutex
	first  error
	sealed bool
}

// note records a cause unless the connection has ended, which it reports.
func (f *pathFailures) note(cause error) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sealed {
		return false
	}
	if f.first == nil {
		f.first = cause
	}
	return true
}

func (f *pathFailures) seal() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sealed = true
}

// closeContexts lists the paths a close can use, in path ID order. A terminal or
// validating path has none of the required authority.
func (p *pathCreditRegistry) closeContexts(buf []*pathMember) []*pathMember {
	contexts := buf[:0]
	var ordered [maxPathHistoryUnits + 1]*pathMember
	for _, m := range p.orderedMembers(ordered[:0]) {
		if p.eligible(m) {
			contexts = append(contexts, m)
		}
	}
	return contexts
}

// closeRoute is a nonzero path's identity at close. Zero's stays with the packer.
func (p *pathCreditRegistry) closeRoute(m *pathMember) *pathRoute {
	connID, _ := p.conn.connIDManager.pathDestination(m.id, false)
	route := &m.route
	route.context, route.connID, route.recovery = ackhandler.PacketContext{PathID: m.id, TupleGeneration: m.tuple}, connID, p.recoveryOn(m.id)
	route.addr, route.info, route.transport = m.addr, m.info, m.transport
	return route
}

func pathCloseFrame(cause error) *wire.ConnectionCloseFrame {
	if transportErr, ok := errors.AsType[*qerr.TransportError](cause); ok {
		frame := &wire.ConnectionCloseFrame{ErrorCode: uint64(transportErr.ErrorCode), FrameType: transportErr.FrameType}
		if !transportErr.ErrorCode.IsCryptoError() {
			frame.ReasonPhrase = transportErr.ErrorMessage
		}
		return frame
	}
	if applicationErr, ok := errors.AsType[*qerr.ApplicationError](cause); ok {
		return &wire.ConnectionCloseFrame{IsApplicationError: true, ErrorCode: uint64(applicationErr.ErrorCode), ReasonPhrase: applicationErr.ErrorMessage}
	}
	return &wire.ConnectionCloseFrame{ErrorCode: uint64(qerr.InternalError), ReasonPhrase: cause.Error()}
}

// closePaths submits the close once on every usable path. Only a context that took
// its packet is retained, and failure on one path costs no other. The outcome is an
// error only when no path took one.
func (e *packetEmission) closePaths(cause error) (*closedPaths, error) {
	closed := &closedPaths{logger: e.pathCredit.conn.logger}
	var failure error
	var storage [maxPathHistoryUnits + 1]*pathMember
	for _, m := range e.pathCredit.closeContexts(storage[:0]) {
		packet, err := e.closeOn(m, cause)
		if err != nil {
			failure = errors.Join(failure, err)
			continue
		}
		closed.responses = append(closed.responses, &closeResponse{path: m.id, packet: packet, addr: m.addr, info: m.info, transport: m.transport})
	}
	if len(closed.responses) != 0 {
		return closed, nil
	}
	if failure == nil {
		failure = errNoClosePath
	}
	return closed, failure
}

func (e *packetEmission) closeOn(m *pathMember, cause error) ([]byte, error) {
	if m.id == 0 {
		return e.close(cause)
	}
	route := e.pathCredit.closeRoute(m)
	packet, buffer, err := e.packer.PackPathClose(route, pathCloseFrame(cause), route.mtu.CurrentSize(), e.version)
	if err != nil {
		return nil, fmt.Errorf("path %d close: %w", m.id, err)
	}
	ecn := route.recovery.ECNMode(true)
	e.policy.logShortHeaderPacket(packet, ecn, buffer.Len())
	retained := bytes.Clone(buffer.Data)
	if err := e.local.sendPathClose(buffer, ecn, route); err != nil {
		return nil, err
	}
	return retained, nil
}
