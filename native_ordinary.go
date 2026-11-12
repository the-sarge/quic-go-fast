//go:build darwin || linux || freebsd

package quic

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"runtime"
	"slices"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

var nextNativeOrdinary atomic.Uint64

var errLocalPacketRoute = errors.New("quic: operation excluded from private ordinary endpoint route")

// Validation probes use the connection-control lane, so ordinary backlog can't
// starve them.
const nativeProbeLane = 2

type ordinaryLocalPacket struct {
	token    ackhandler.LocalCompletionToken
	datagram bool
	mtu      *mtuFinderAckHandler
}

type ordinaryLocalState uint8

const (
	ordinaryFree ordinaryLocalState = iota
	ordinaryBuilding
	ordinaryStaged
	ordinaryEndpointOwned
	ordinaryTerminalProcessing
)

// ordinaryLocalMember maps one native member to its ordered registration span.
// It stays on the connection after payload ownership transfers to the endpoint.
type ordinaryLocalMember struct {
	end       int
	datagrams int
	metadata  sendMetadata
	bytes     protocol.ByteCount
}

type ordinaryLocalRecord struct {
	state       ordinaryLocalState
	participant *endpointParticipant
	request     endpointRequest
	offered     bool
	deadline    monotime.Time
	terminal    endpointResult
	endpoint    *nativeEndpoint
	identity    endpointIdentity
	packets     []ordinaryLocalPacket
	count       int
	datagrams   int
	members     []ordinaryLocalMember
	backing     protocol.ByteCount
}

type nativeOrdinarySender struct {
	endpoint    *nativeEndpoint
	participant *endpointParticipant
	recovery    ackhandler.LocalSentPacketHandler
	connection  *Conn
	destination netip.AddrPort
	info        packetInfo
	oob         []byte
	writeState  *endpointWriteState // the current path's; each conn owns its own
	nativeGSO   bool
	generation  uint64
	// switchGeneration is the generation the latest path switch began. A result
	// below it belongs to a path the connection left, even on an endpoint it has
	// since returned to.
	switchGeneration uint64
	records          [4]ordinaryLocalRecord
	pending          *ordinaryLocalRecord
	err              error
	drained, closed  bool
	droppedDatagrams uint64
	// targets holds at most one participant per other endpoint a client probed or
	// left. They share the primary's wake channel, and each pass of the connection
	// loop reconciles all of them.
	targets []*endpointParticipant
	// route is the selected nonzero path; nil submits on path zero's fields above.
	route *pathRoute
	// origin is where a member with a nil transport sends: the connection's own
	// endpoint, which path zero's fields stop naming once path zero moves.
	origin *endpointParticipant
	// faultHanded means the registry holds an endpoint fault. The fault channel then
	// goes quiet, and every later fault wakes the connection through completed.
	faultHanded       bool
	sequence          uint64
	deferred          []*ordinaryLocalRecord
	consuming         bool
	processing        bool
	protectionScratch *handshake.PacketProtectionScratch
}

type nativeOrdinaryConn struct {
	sendConn
	sender     *nativeOrdinarySender
	writeState endpointWriteState
}

func (c *Conn) useNativeOrdinary(endpoint *nativeEndpoint, destination netip.AddrPort, info packetInfo) error {
	if endpoint == nil || !destination.IsValid() || c.emission.local != nil || c.sentFirstPacket || c.emission.pathCredit != nil || c.emission.authority != nil || c.emission.sendQoS != nil {
		return errLocalPacketRoute
	}
	if socket, ok := c.conn.(*sconn); ok && socket.external != nil {
		return errLocalPacketRoute
	}
	participant, err := endpoint.attach(nextNativeOrdinary.Add(1))
	if err != nil {
		return err
	}
	handler, err := ackhandler.EnableLocalCompletions(c.sentPacketHandler)
	if err != nil {
		endpoint.detach(participant)
		return err
	}
	queue := &nativeOrdinarySender{
		endpoint: endpoint, participant: participant, recovery: handler, connection: c,
		destination: destination, info: info, oob: info.OOB(), nativeGSO: runtime.GOOS == "linux", generation: 1,
	}
	queue.origin = participant
	c.emission.local, c.emission.queue = queue, queue
	conn := &nativeOrdinaryConn{sendConn: c.conn, sender: queue}
	queue.writeState = &conn.writeState
	c.conn = conn
	return nil
}

// drain moves terminal disposal to the endpoint. Queued requests keep their
// deadlines, because the baseline sends them ahead of a CONNECTION_CLOSE. It
// never waits for native I/O.
func (e *nativeEndpoint) drain(p *endpointParticipant) {
	e.do(func(s *endpointState) {
		if !s.valid(p) || p.draining {
			return
		}
		p.draining = true
		s.withdraw(p)
	})
}

// depositClose takes ownership of a draining participant's close packet on
// success. It is admitted on the connection-control lane once the participant's
// queued and current requests resolve, or released at deposit+B.
func (e *nativeEndpoint) depositClose(p *endpointParticipant, request endpointRequest) error {
	return e.depositCloseUpTo(p, request, 1)
}

// depositCloseUpTo holds one close per path that uses this endpoint. They go out in
// deposit order, each under its own deadline.
func (e *nativeEndpoint) depositCloseUpTo(p *endpointParticipant, request endpointRequest, limit int) error {
	err := net.ErrClosed
	e.do(func(s *endpointState) {
		if s.sealed || s.faulted || !s.valid(p) || !p.draining || len(p.closings) >= limit {
			return
		}
		err = errEndpointRequest
		size, valid := request.checkedSize(p.class)
		if request.identity.connection != p.id || !valid || len(request.members) != 1 || request.members[0].buffer == nil || size > 64<<10 {
			return
		}
		if len(p.closings) == 0 {
			s.closers = append(s.closers, p)
		}
		p.closings = append(p.closings, endpointClosing{request: request.snapshot(), size: size, deadline: time.Now().Add(s.budget)})
		p.closeDeposited = true
		err = nil
	})
	return err
}

func (e *nativeEndpoint) cancelGeneration(p *endpointParticipant, generation uint64) {
	e.do(func(s *endpointState) {
		for index := range s.records {
			record := &s.records[index]
			if record.used && !record.reserved && !record.terminal && record.participant == p && record.request.identity.generation == generation {
				record.revoked = true
				if s.current != nil && s.current.index == index {
					s.current.canceled.Store(true)
				}
			}
		}
	})
}

func (s *nativeOrdinarySender) WouldBlock() bool {
	blocked, _ := s.wouldBlock()
	return blocked
}

// wouldBlock reports the actual finish action even if a callback reuses a record.
func (s *nativeOrdinarySender) wouldBlock() (blocked, finished bool) {
	if s.drained || s.closed || s.err != nil {
		return true, false
	}
	if !s.recovery.CanRegisterLocalPackets(1) || !s.bufferRoom(s.route, 0) {
		finished = s.pending != nil
		s.finish()
		return true, finished
	}
	if s.pending != nil {
		identity := s.constructionIdentity(s.route)
		identity.registration = s.pending.identity.registration
		if identity == s.pending.identity && (len(s.pending.members) == 0 || s.pending.request.epoch == s.constructionEpoch()) {
			return false, false
		}
		s.finish()
		finished = true
	}
	s.pending = s.reserve(s.route)
	return s.pending == nil, finished
}

// reserve holds connection-local capacity before destructive packing. It never
// advertises endpoint demand; only a frozen Staged head can do that.
func (s *nativeOrdinarySender) reserve(route *pathRoute) *ordinaryLocalRecord {
	packets := 1
	if !s.connection.handshakeConfirmed {
		packets = 3
	}
	if !s.recovery.CanRegisterLocalPackets(packets) {
		return nil
	}
	participant := s.participantOf(route)
	if participant == nil || s.routeSaturated(participant.endpoint) || !s.bufferRoom(route, 0) {
		return nil
	}
	for i := range s.records {
		record := &s.records[i]
		if record.state != ordinaryFree {
			continue
		}
		s.sequence++
		identity := s.constructionIdentity(route)
		identity.registration = s.sequence
		*record = ordinaryLocalRecord{
			state: ordinaryBuilding, endpoint: participant.endpoint, participant: participant, identity: identity, deadline: monotime.Now().Add(participant.endpoint.budget),
			packets: record.packets, members: record.members, request: endpointRequest{members: record.request.members},
		}
		return record
	}
	return nil
}

func (s *nativeOrdinarySender) Available() <-chan struct{} { return s.participant.completed }
func (s *nativeOrdinarySender) completed() <-chan struct{} { return s.participant.completed }
func (s *nativeOrdinarySender) Run() error                 { return nil }

// faulted stays ready once its endpoint has faulted, so a connection that continues
// can't keep it in the select.
func (s *nativeOrdinarySender) faulted() <-chan struct{} {
	if s.faultHanded {
		return nil
	}
	return s.endpoint.fault
}

func (s *nativeOrdinarySender) participantOf(route *pathRoute) *endpointParticipant {
	if route == nil {
		return s.participant
	}
	if route.transport == nil {
		return s.origin
	}
	endpoint := nativeTransportEndpoint(route.transport)
	if endpoint == nil {
		return nil
	}
	return s.probeParticipant(endpoint)
}

// routeSaturated keeps one of the connection's records free for another endpoint, so
// a slow socket can't hold back a path on an independent one. The total is unchanged,
// and a connection without path admission keeps every record.
func (s *nativeOrdinarySender) routeSaturated(endpoint *nativeEndpoint) bool {
	if credit := s.connection.pathCredit; len(s.targets) == 0 || credit == nil || !credit.hasAdmission() {
		return false
	}
	held := 0
	for index := range s.records {
		if s.records[index].state != ordinaryFree && s.records[index].endpoint == endpoint {
			held++
		}
	}
	return held >= len(s.records)-1
}

// useRoute changes only the current local builder. Frozen staged requests retain
// their route, identity, demand and payload until admission or local settlement.
func (s *nativeOrdinarySender) useRoute(route *pathRoute) {
	s.finish()
	s.route = route
}

func (s *nativeOrdinarySender) steer(route *pathRoute) bool {
	if s.drained || s.closed || s.err != nil || !s.bufferRoom(route, 0) {
		return false
	}
	if s.route == route && s.pending != nil {
		return !s.WouldBlock()
	}
	s.finish()
	record := s.reserve(route)
	if record == nil {
		return false
	}
	s.route, s.pending = route, record
	return true
}

// routeUsable reports whether a path's endpoint can still take its packets. A
// fault or close quarantines that endpoint and its paths; other paths continue. No
// endpoint is special, and a nil target is the connection's own.
func (s *nativeOrdinarySender) routeUsable(target *Transport) bool {
	if target == nil {
		return s.origin != nil && !s.origin.endpoint.quarantined()
	}
	endpoint := nativeTransportEndpoint(target)
	return endpoint != nil && !endpoint.quarantined()
}

func (s *nativeOrdinarySender) register(token ackhandler.LocalCompletionToken, datagram bool) {
	if s.pending == nil || s.pending.count-s.pending.memberEnd() == maxGSOSegments+3 {
		panic("local packet registration without reserved construction capacity")
	}
	s.pending.packets = append(s.pending.packets, ordinaryLocalPacket{token: token, datagram: datagram})
	s.pending.count++
}

func (s *nativeOrdinarySender) registerMTUProbe(handler *mtuFinderAckHandler) {
	s.pending.packets[s.pending.count-1].mtu = handler
}

func (s *nativeOrdinarySender) Send(buffer *packetBuffer, segment uint16, ecn protocol.ECN, metadata sendMetadata) {
	s.sendProtected(buffer, segment, ecn, metadata, nil)
}

func (s *nativeOrdinarySender) sendProtected(buffer *packetBuffer, segment uint16, ecn protocol.ECN, metadata sendMetadata, protection *nativeProtectionPlan) {
	record := s.pending
	if record == nil {
		panic("local packet handoff without reserved construction capacity")
	}
	destination, base, writeState := s.destination, s.oob, s.writeState
	nativeGSO := s.nativeGSO
	epoch := s.constructionEpoch()
	routed := true
	if route := s.route; route != nil {
		destination, routed = nativeDestination(route.addr)
		base, writeState = route.info.OOB(), &route.writeState
	} else if conn, ok := s.connection.conn.(*nativeOrdinaryConn); ok {
		// Packing geometry does not grant wire GSO. The existing connection
		// capability owner also governs segmented buffers sent as scalar calls.
		nativeGSO = nativeGSO && conn.sendConn.capabilities().GSO
	}
	member := endpointMember{buffer: buffer, protection: protection, oob: record.packetOOB(base, destination, ecn), gsoSize: segment, nativeGSO: nativeGSO, writeState: writeState, credit: metadata.credit}
	if len(record.members) == 0 {
		record.request = endpointRequest{identity: record.identity, destination: destination, deadline: record.deadline.ToTime(), epoch: epoch, members: record.request.members}
	}
	datagrams := member.datagrams()
	if metadata.shortSoftwareDatagramGroup && !member.nativeGSO && datagrams > 1 {
		record.request.shortSoftwareDatagramGroup = true
	}
	packets := record.count - record.memberEnd()
	record.request.members = append(record.request.members, member)
	record.members = append(record.members, ordinaryLocalMember{end: record.count, datagrams: datagrams, metadata: metadata, bytes: buffer.Len()})
	record.backing += buffer.Cap()
	record.datagrams += datagrams
	if !routed || record.request.destination != destination || record.request.epoch != epoch || (segment != 0 && packets != datagrams) {
		s.err = errors.Join(s.err, errEndpointRequest)
		s.settleLocal(record, errEndpointRequest)
		return
	}
	if metadata.handshake || metadata.mtuProbe || len(metadata.terminalReceipts) != 0 {
		s.finish()
	}
}

func (s *nativeOrdinarySender) packetOOB(ecn protocol.ECN) []byte {
	return packetOOB(s.oob, s.destination, ecn)
}

func (r *ordinaryLocalRecord) packetOOB(base []byte, destination netip.AddrPort, ecn protocol.ECN) []byte {
	if r.state != ordinaryBuilding || len(r.request.members) == 0 {
		return packetOOB(base, destination, ecn)
	}
	previous := r.request.members[len(r.request.members)-1].oob
	if len(previous) == 0 {
		return packetOOB(base, destination, ecn)
	}
	space := 0
	if ecn != protocol.ECNUnsupported {
		if destination.Addr().Unmap().Is4() {
			space = unix.CmsgSpace(ecnIPv4DataLen)
		} else {
			space = unix.CmsgSpace(4)
		}
	}
	var scratch [oobBufferSize]byte
	if space > len(scratch) || len(base) > len(scratch)-space {
		return packetOOB(base, destination, ecn)
	}
	candidate := append(scratch[:0], base...)
	if ecn != protocol.ECNUnsupported {
		if destination.Addr().Unmap().Is4() {
			candidate = appendIPv4ECNMsg(candidate, ecn)
		} else {
			candidate = appendIPv6ECNMsg(candidate, ecn)
		}
	}
	// Only the current Building record owns this value. It stays immutable;
	// endpoint admission still takes its independent snapshot.
	if bytes.Equal(candidate, previous) {
		return previous
	}
	return packetOOB(base, destination, ecn)
}

func packetOOB(base []byte, destination netip.AddrPort, ecn protocol.ECN) []byte {
	oob := append([]byte(nil), base...)
	if ecn != protocol.ECNUnsupported {
		if destination.Addr().Unmap().Is4() {
			oob = appendIPv4ECNMsg(oob, ecn)
		} else {
			oob = appendIPv6ECNMsg(oob, ecn)
		}
	}
	return oob
}

// drain ends local admission while recovery stays open. CloseLocal replaces the
// packet-number spaces, and the close packet is packed after this returns.
func (s *nativeOrdinarySender) drain() {
	if s.drained || s.closed {
		return
	}
	s.drained = true
	s.cancelLocal(nil)
	s.endpoint.drain(s.participant)
	for _, target := range s.targets {
		target.endpoint.drain(target)
	}
}

// sendClose consumes the buffer. The endpoint holds it as this connection's close
// obligation, so the caller never waits for native I/O.
func (s *nativeOrdinarySender) sendClose(buffer *packetBuffer, ecn protocol.ECN) error {
	s.drain()
	limit := 1
	if credit := s.connection.pathCredit; credit != nil && credit.hasAdmission() {
		limit = pathCloseLimit
	}
	err := s.endpoint.depositCloseUpTo(s.participant, endpointRequest{
		identity:    endpointIdentity{connection: s.participant.id, generation: s.generation, pathGeneration: s.connection.pathGeneration, tuple: s.connection.pathGeneration},
		destination: s.destination, members: []endpointMember{{buffer: buffer, oob: s.packetOOB(ecn), writeState: s.writeState}},
	}, limit)
	if err != nil {
		buffer.Release()
	}
	return err
}

// pathCloseLimit is the two live paths a connection can hold.
const pathCloseLimit = 2

// sendPathClose is sendClose for a nonzero path, through that path's own endpoint.
// A target attached only now joins the drain before it holds the close.
func (s *nativeOrdinarySender) sendPathClose(buffer *packetBuffer, ecn protocol.ECN, route *pathRoute) error {
	s.drain()
	participant := s.origin
	if route.transport != nil {
		participant = s.probeParticipant(nativeTransportEndpoint(route.transport))
	}
	destination, ok := nativeDestination(route.addr)
	if participant == nil || !ok {
		buffer.Release()
		return errLocalPacketRoute
	}
	participant.endpoint.drain(participant)
	err := participant.endpoint.depositCloseUpTo(participant, endpointRequest{
		identity:    endpointIdentity{connection: participant.id, generation: s.generation, pathGeneration: route.context.TupleGeneration, tuple: route.context.TupleGeneration},
		destination: destination, members: []endpointMember{{buffer: buffer, oob: packetOOB(route.info.OOB(), destination, ecn), writeState: &route.writeState}},
	}, pathCloseLimit)
	if err != nil {
		buffer.Release()
	}
	return err
}

// routeInfo is the local address and interface path zero sends from.
func (s *nativeOrdinarySender) routeInfo() packetInfo { return s.info }

func (s *nativeOrdinarySender) SendProbe(*packetBuffer, net.Addr, packetInfo) error {
	return errLocalPacketRoute
}

// probeTargetReady refuses an ungranted target, whose direct write would block
// the connection loop. A target that can't take a participant only costs this
// attempt; the existing probe timer retries.
func (s *nativeOrdinarySender) probeTargetReady(target *Transport) (bool, error) {
	endpoint := nativeTransportEndpoint(target)
	if endpoint == nil {
		return false, errLocalPacketRoute
	}
	if s.closed {
		return false, nil
	}
	return s.probeParticipant(endpoint) != nil, nil
}

func (s *nativeOrdinarySender) probeParticipant(endpoint *nativeEndpoint) *endpointParticipant {
	if endpoint == s.endpoint {
		return s.participant
	}
	for _, target := range s.targets {
		if target.endpoint == endpoint {
			return target
		}
	}
	target, err := endpoint.attachShared(s.participant.id, s.participant.completed)
	if err != nil {
		return nil
	}
	s.targets = append(s.targets, target)
	return target
}

// reserveTarget takes this connection's participant on a target before its routes exist,
// and classifies a refusal. A later probe or send reuses the participant.
func (s *nativeOrdinarySender) reserveTarget(target *Transport) error {
	if s.transportParticipant(target) != nil {
		return nil
	}
	participant, err := nativeTransportEndpoint(target).attachShared(s.participant.id, s.participant.completed)
	if errors.Is(err, errEndpointCapacity) {
		return fmt.Errorf("%w: %w", ErrMultipathResourceLimitV1, err)
	}
	if err != nil {
		return err
	}
	s.targets = append(s.targets, participant)
	return nil
}

// sendProbe consumes the buffer. All commitments precede it, so refusal keeps
// every charge and leaves the next attempt to the validation budget. Results stay
// best effort; the endpoint fault channel reports endpoint-wide failure.
func (s *nativeOrdinarySender) sendProbe(buffer *packetBuffer, target *Transport, address net.Addr, info packetInfo, epoch *responseEpoch) error {
	participant := s.origin
	if target != nil {
		participant = s.probeParticipant(nativeTransportEndpoint(target))
	}
	destination, ok := nativeDestination(address)
	if participant == nil || !ok {
		buffer.Release()
		return errLocalPacketRoute
	}
	err := participant.endpoint.offer(participant, nativeProbeLane, endpointRequest{
		identity:    endpointIdentity{connection: participant.id, generation: s.generation, pathGeneration: s.connection.pathGeneration, tuple: s.connection.pathGeneration},
		destination: destination, members: []endpointMember{{buffer: buffer, oob: info.OOB()}}, epoch: epoch,
	})
	if err == nil {
		return nil
	}
	buffer.Release()
	participant.endpoint.cancelReadiness(participant, nativeProbeLane)
	if errors.Is(err, errEndpointCapacity) || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

// probesPending reports whether a probe under epoch can still reach the socket.
// It reads the endpoint's records and never waits for a native return.
func (s *nativeOrdinarySender) probesPending(target *Transport, epoch *responseEpoch) bool {
	// Retiring a tuple first settles its unadmitted work. Recovery still owns the
	// old token context here, before the registry can reclaim it at the native fence.
	s.expireLocal()
	for i := range s.records {
		r := &s.records[i]
		if r.state == ordinaryStaged && r.request.epoch == epoch {
			return true
		}
	}

	var endpoint *nativeEndpoint
	if target != nil {
		endpoint = nativeTransportEndpoint(target)
	} else if s.origin != nil {
		endpoint = s.origin.endpoint
	}
	return endpoint != nil && endpoint.epochPending(epoch)
}

// epochPending reports an unresolved request under epoch. A claimed request stays
// unresolved until actual native return.
func (e *nativeEndpoint) epochPending(epoch *responseEpoch) bool {
	pending := false
	e.do(func(s *endpointState) {
		for index := range s.records {
			record := &s.records[index]
			if record.used && !record.reserved && !record.terminal && record.request.epoch == epoch {
				pending = true
				return
			}
		}
	})
	return pending
}

func (e *nativeEndpoint) quarantined() bool {
	quarantined := true
	e.do(func(s *endpointState) { quarantined = s.faulted || s.sealed })
	return quarantined
}

func nativeTransportEndpoint(target *Transport) *nativeEndpoint {
	if target == nil || target.responses == nil {
		return nil
	}
	endpoint, _ := target.responses.backend.(*nativeEndpoint)
	return endpoint
}

func nativeDestination(address net.Addr) (netip.AddrPort, bool) {
	udp, ok := address.(*net.UDPAddr)
	if !ok || udp == nil || !udp.AddrPort().IsValid() {
		return netip.AddrPort{}, false
	}
	destination := udp.AddrPort()
	return netip.AddrPortFrom(destination.Addr().Unmap(), destination.Port()), true
}

// completePacket tells the MTU owner about a local result before recovery runs,
// because recovery's own callback can't distinguish it from network loss. A size
// rejection stays PMTU feedback through that callback.
func (s *nativeOrdinarySender) completePacket(packet ordinaryLocalPacket, outcome ackhandler.LocalPacketOutcome, returnedAt monotime.Time, sizeRejected bool) {
	if packet.mtu != nil {
		if outcome == ackhandler.LocalPacketUncertain {
			packet.mtu.markLocalUncertain()
		} else if outcome == ackhandler.LocalPacketUnsubmitted && !sizeRejected {
			packet.mtu.disposeLocal()
		}
	}
	if err := s.recovery.CompleteLocalPacket(packet.token, outcome, returnedAt); err != nil {
		s.err = errors.Join(s.err, err)
	}
}

func (s *nativeOrdinarySender) finish() {
	if s.pending == nil {
		return
	}
	record := s.pending
	s.pending = nil
	// A failed buffer can have registered packets without a native member.
	// Keep that suffix in the sidecar until earlier member facts are applied.
	if len(record.members) == 0 {
		s.settleLocal(record, nil)
		return
	}
	record.state = ordinaryStaged
	s.serviceParticipant(record.participant)
}

func (s *nativeOrdinarySender) consume() error {
	if s.consuming {
		return s.err
	}
	// Consume the shared hint before draining every participant. A concurrent later
	// publication then leaves a new hint; empty drains never re-signal themselves.
	select {
	case <-s.participant.completed:
	default:
	}
	s.consuming = true
	defer func() { s.consuming = false }()
	s.expireLocal()
	s.progress(s.participant)
	for _, target := range s.targets {
		s.progress(target)
	}
	s.consumeDeferred()
	if !s.admitted() {
		select {
		case <-s.endpoint.fault:
			s.err = errors.Join(s.err, errEndpointFault)
		default:
		}
		return s.err
	}
	s.reconcileFailure(s.participant)
	for _, target := range s.targets {
		s.reconcileFailure(target)
	}
	if err := s.connection.pathCredit.failed(); err != nil && s.err == nil {
		s.err = err
	}
	return s.err
}

func (s *nativeOrdinarySender) admitted() bool {
	credit := s.connection.pathCredit
	return credit != nil && credit.hasAdmission()
}

// reconcileFailure hands a participant's endpoint fault to the registry, and settles
// what its retirement left. Sealing revokes a spared connection's participants and the
// endpoint discards their later results, but retirement proves no request or native
// borrow remains. A missing result doesn't prove nothing was sent, so each record left
// settles as uncertain, and the registry's drain then returns its work.
func (s *nativeOrdinarySender) reconcileFailure(participant *endpointParticipant) {
	select {
	case <-participant.endpoint.fault:
		s.connection.pathCredit.failures.note(errEndpointFault)
		s.faultHanded = true
	default:
	}
	select {
	case <-participant.retired:
	default:
		return
	}
	for index := range s.records {
		record := &s.records[index]
		if record.state != ordinaryEndpointOwned || record.endpoint != participant.endpoint {
			continue
		}
		for i, packet := range record.packets[:record.count] {
			outcome := ackhandler.LocalPacketUncertain
			if i >= record.memberEnd() {
				outcome = ackhandler.LocalPacketUnsubmitted
				if packet.datagram {
					s.droppedDatagrams++
				}
			}
			s.completePacket(packet, outcome, monotime.Now(), false)
		}
		s.connection.pathCredit.noteOutcome(protocol.PathID(record.identity.path), record.identity.tuple, 0, record.datagrams, record.datagrams)
		s.freeRecord(record)
	}
}

// consumeParticipant reconciles one endpoint's results. A result from before the
// latest path switch settles its tokens and nothing else, so a path the connection
// left can't end it or affect its replacement.
func (s *nativeOrdinarySender) consumeParticipant(participant *endpointParticipant) {
	s.serviceParticipant(participant)
	s.consumeDeferred()
}

func (s *nativeOrdinarySender) consumeResult(participant *endpointParticipant, terminal endpointResult) {
	identity := terminal.identity
	identity.endpoint = 0
	for index := range s.records {
		record := &s.records[index]
		if record.state != ordinaryEndpointOwned && record.state != ordinaryTerminalProcessing || record.identity != identity {
			continue
		}
		if len(terminal.members) != len(record.members) {
			s.err = errors.Join(s.err, errEndpointRequest)
			break
		}
		record.state = ordinaryTerminalProcessing
		start := 0
		for memberIndex, member := range record.members {
			result := terminal.members[memberIndex]
			if result.accepted < 0 || result.uncertain < 0 || result.unsubmitted < 0 || result.accepted+result.uncertain+result.unsubmitted != member.datagrams {
				s.err = errors.Join(s.err, errEndpointRequest)
				break
			}
			if result.accepted == member.datagrams && result.err == nil {
				for _, receipt := range member.metadata.terminalReceipts {
					if receipt != nil {
						receipt.publish(result.returnedAt)
					}
				}
			}
			packets := record.packets[start:member.end]
			start = member.end
			if !s.completeAcceptedPackets(packets, member.datagrams, result.accepted, result.err, result.returnedAt) {
				for packetIndex, packet := range packets {
					datagram := 0
					if member.datagrams != 1 {
						datagram = packetIndex
					}
					outcome := ackhandler.LocalPacketUnsubmitted
					if datagram < result.accepted {
						outcome = ackhandler.LocalPacketAccepted
					} else if datagram < result.accepted+result.uncertain {
						outcome = ackhandler.LocalPacketUncertain
					} else if packet.datagram {
						s.droppedDatagrams++
					}
					s.completePacket(packet, outcome, result.returnedAt, isSendMsgSizeErr(result.err))
					if s.closed {
						// Close already cleared this record during the callback.
						return
					}
				}
			}
			if credit := s.connection.pathCredit; credit != nil && credit.hasAdmission() {
				credit.noteOutcome(protocol.PathID(identity.path), identity.tuple, result.accepted, result.uncertain, member.datagrams)
			}
			err := errors.Join(result.err, terminal.err)
			if identity.generation < s.switchGeneration {
				s.connection.scheduleSending()
			} else if isSendMsgSizeErr(err) {
				if member.metadata.handshake && member.datagrams == 1 && member.bytes > protocol.MinInitialPacketSize {
					s.connection.handshakeSendFeedback.publish(member.metadata.pathGeneration)
				}
			} else if err != nil && !localEndpointBackpressure(err) && !errors.Is(err, errEndpointRevoked) && !s.pathSendFailed(participant, identity, err) && !s.endpointOwnsResult(participant) {
				s.err = errors.Join(s.err, err)
			}
		}
		for _, packet := range record.packets[start:record.count] {
			if packet.datagram {
				s.droppedDatagrams++
			}
			s.completePacket(packet, ackhandler.LocalPacketUnsubmitted, monotime.Now(), false)
			if s.closed {
				return
			}
		}
		s.freeRecord(record)
		break
	}
}

// endpointOwnsResult keeps a quarantined endpoint's result off the connection, whose
// survival the registry decides. Only a connection without admission fails with its
// own endpoint.
func (s *nativeOrdinarySender) endpointOwnsResult(participant *endpointParticipant) bool {
	if participant == s.participant && !s.admitted() {
		return false
	}
	return participant.endpoint.quarantined()
}

// pathSendFailed hands a final non-timeout result to the path policy, once path
// admission exists. A result with endpoint-wide evidence stays the endpoint's, and
// a connection without admission keeps its baseline error.
func (s *nativeOrdinarySender) pathSendFailed(participant *endpointParticipant, identity endpointIdentity, err error) bool {
	credit := s.connection.pathCredit
	if credit == nil || !credit.hasAdmission() || endpointFatalWrite(err) || participant.endpoint.quarantined() {
		return false
	}
	credit.sendFailed(protocol.PathID(identity.path), identity.tuple, monotime.Now())
	return true
}

func (s *nativeOrdinarySender) reject() {
	s.err = errors.Join(s.err, errLocalPacketRoute)
	s.connection.scheduleSending()
}

func (s *nativeOrdinarySender) reset() {
	s.cancelLocal(nil)
	s.endpoint.cancelGeneration(s.participant, s.generation)
	for _, target := range s.targets {
		target.endpoint.cancelGeneration(target, s.generation)
	}
	s.generation++
}

// rebind makes the new tuple a new generation. Recovery has already retired the
// old registrations, so their late results reconcile terminal identity only.
func (s *nativeOrdinarySender) rebind(address net.Addr, info packetInfo) bool {
	destination, ok := nativeDestination(address)
	if !ok {
		s.reject()
		return false
	}
	s.reset()
	s.destination, s.oob = destination, info.OOB()
	return true
}

// replacePath switches to the target transport's endpoint. Recovery has already
// retired the old registrations, so their late results reconcile terminal identity
// through the old participant, which stays attached until close. It returns the new
// path's conn, or nil after rejecting a target that can't take the route.
func (s *nativeOrdinarySender) replacePath(target *Transport, conn sendConn) sendConn {
	endpoint := nativeTransportEndpoint(target)
	var participant *endpointParticipant
	if endpoint != nil && !s.closed && !s.drained {
		participant = s.probeParticipant(endpoint)
	}
	if participant == nil {
		s.reject()
		return nil
	}
	s.reset()
	s.switchGeneration = s.generation
	if participant != s.participant {
		s.targets = slices.DeleteFunc(s.targets, func(p *endpointParticipant) bool { return p == participant })
		s.targets = append(s.targets, s.participant)
		s.participant, s.endpoint = participant, endpoint
	}
	s.oob = nil
	next := &nativeOrdinaryConn{sendConn: conn, sender: s}
	s.writeState = &next.writeState
	return next
}

// moveZero makes target and address path zero's current tuple. Unlike replacePath it
// cancels nothing: the old tuple's fence has resolved, and other paths' work is theirs.
func (s *nativeOrdinarySender) moveZero(target *Transport, address net.Addr, info packetInfo, conn sendConn) sendConn {
	destination, ok := nativeDestination(address)
	participant := s.origin
	if target != nil {
		participant = s.probeParticipant(nativeTransportEndpoint(target))
	}
	if participant == nil || !ok {
		s.reject()
		return nil
	}
	s.finish()
	s.cancelZero()
	if participant != s.participant {
		s.targets = slices.DeleteFunc(s.targets, func(p *endpointParticipant) bool { return p == participant })
		s.targets = append(s.targets, s.participant)
		s.participant, s.endpoint = participant, participant.endpoint
	}
	s.destination, s.oob, s.info = destination, info.OOB(), info
	next := &nativeOrdinaryConn{sendConn: conn, sender: s}
	s.writeState = &next.writeState
	return next
}

// rebindZero is moveZero for a peer's new address on the same endpoint.
func (s *nativeOrdinarySender) rebindZero(address net.Addr, info packetInfo, conn sendConn) {
	destination, ok := nativeDestination(address)
	if !ok {
		s.reject()
		return
	}
	s.cancelZero()
	s.destination, s.oob, s.info = destination, info.OOB(), info
	s.writeState.wroteFirstPacket = false
	s.writeState.gsoDisabled.Store(false)
	if native, ok := conn.(*nativeOrdinaryConn); ok {
		native.sendConn.ChangeRemoteAddr(address, info)
	}
}

// resetWriteState starts a new tuple's fallback. The old tuple's fence has resolved,
// so no request still reads it.
func (s *nativeOrdinarySender) resetWriteState(route *pathRoute) {
	route.writeState.wroteFirstPacket = false
	route.writeState.gsoDisabled.Store(false)
}

// transportParticipant finds a transport's participant without attaching one.
func (s *nativeOrdinarySender) transportParticipant(target *Transport) *endpointParticipant {
	if target == nil {
		return s.origin
	}
	endpoint := nativeTransportEndpoint(target)
	if endpoint != nil && endpoint == s.endpoint {
		return s.participant
	}
	for _, participant := range s.targets {
		if participant.endpoint == endpoint {
			return participant
		}
	}
	return nil
}

// detachReady consumes a transport's results, and reports that it holds no request
// or record. Revocation drops unconsumed results, so an earlier detach would strand
// their recovery tokens. It never waits for native I/O.
func (s *nativeOrdinarySender) detachReady(target *Transport) bool {
	participant := s.transportParticipant(target)
	if participant == nil {
		return true
	}
	s.cancelLocal(participant)
	participant.endpoint.cancelReadiness(participant, 0)
	s.consumeParticipant(participant)
	for index := range s.records {
		if s.records[index].state != ordinaryFree && s.records[index].endpoint == participant.endpoint {
			return false
		}
	}
	return !participant.endpoint.participantBusy(participant)
}

func (e *nativeEndpoint) participantBusy(p *endpointParticipant) bool {
	busy := false
	e.do(func(s *endpointState) {
		for index := range s.records {
			if s.records[index].used && s.records[index].participant == p {
				busy = true
				return
			}
		}
	})
	return busy
}

// detachTransport ends a participant. A primary with path zero gone hands its place
// to another, so the transport's later close can't fault this connection.
func (s *nativeOrdinarySender) detachTransport(target *Transport) {
	participant := s.transportParticipant(target)
	if participant == nil {
		return
	}
	if participant == s.participant {
		if len(s.targets) == 0 {
			return
		}
		s.participant, s.endpoint = s.targets[0], s.targets[0].endpoint
		s.targets = s.targets[1:]
	} else {
		s.targets = slices.DeleteFunc(s.targets, func(p *endpointParticipant) bool { return p == participant })
	}
	if s.origin == participant {
		s.origin = nil
	}
	participant.endpoint.detach(participant)
}

func (s *nativeOrdinarySender) Close() {
	if s.closed {
		return
	}
	s.closed = true
	s.cancelLocal(nil)
	s.recovery.CloseLocal(monotime.Now())
	s.endpoint.detach(s.participant)
	for _, target := range s.targets {
		target.endpoint.detach(target)
	}
	clear(s.records[:])
	s.deferred = nil
}

func (c *nativeOrdinaryConn) localRoute() {}

func (c *nativeOrdinaryConn) Write([]byte, uint16, protocol.ECN) error { return errLocalPacketRoute }

func (c *nativeOrdinaryConn) WriteTo([]byte, net.Addr, packetInfo) error { return errLocalPacketRoute }

func (c *nativeOrdinaryConn) Close() error { return errLocalPacketRoute }

func (c *nativeOrdinaryConn) ChangeRemoteAddr(address net.Addr, info packetInfo) {
	if c.sender.rebind(address, info) {
		c.sendConn.ChangeRemoteAddr(address, info)
	}
}

func (c *nativeOrdinaryConn) capabilities() connCapabilities {
	capabilities := c.sendConn.capabilities()
	capabilities.GSO = capabilities.GSO && !c.writeState.gsoDisabled.Load()
	return capabilities
}
