//go:build darwin || linux || freebsd

package quic

import (
	"errors"
	"net"
	"net/netip"
	"runtime"
	"slices"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
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

type ordinaryLocalRecord struct {
	admission endpointAdmission
	endpoint  *nativeEndpoint // where admission is held
	identity  endpointIdentity
	packets   [maxGSOSegments + 3]ordinaryLocalPacket
	count     int
	datagrams int
	metadata  sendMetadata
	bytes     protocol.ByteCount
}

type nativeOrdinarySender struct {
	endpoint    *nativeEndpoint
	participant *endpointParticipant
	recovery    ackhandler.LocalSentPacketHandler
	connection  *Conn
	destination netip.AddrPort
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
		destination: destination, oob: info.OOB(), nativeGSO: runtime.GOOS == "linux", generation: 1,
	}
	c.emission.local, c.emission.queue = queue, queue
	conn := &nativeOrdinaryConn{sendConn: c.conn, sender: queue}
	queue.writeState = &conn.writeState
	c.conn = conn
	return nil
}

func (e *nativeEndpoint) prepare(p *endpointParticipant, lane, size int) (endpointAdmission, error) {
	var admission endpointAdmission
	err := net.ErrClosed
	e.do(func(s *endpointState) { admission, err = s.admit(p, lane, size) })
	return admission, err
}

func (e *nativeEndpoint) submit(admission endpointAdmission, request endpointRequest) error {
	err := net.ErrClosed
	e.do(func(s *endpointState) {
		if admission.index < 0 || admission.index >= len(s.records) {
			return
		}
		record := &s.records[admission.index]
		if !record.used || !record.reserved || record.serial != admission.serial || !s.valid(record.participant) || s.sealed || s.faulted {
			return
		}
		err = errEndpointRequest
		if request.identity.connection != record.participant.id || !request.destination.IsValid() || len(request.buffers) != 1 || request.buffers[0] == nil || request.buffers[0].refCount != 1 || len(request.buffers[0].Data) == 0 || cap(request.buffers[0].Data) > record.bytes {
			return
		}
		s.commit(admission, request)
		err = nil
	})
	return err
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
	err := net.ErrClosed
	e.do(func(s *endpointState) {
		if s.sealed || s.faulted || !s.valid(p) || !p.draining || p.closing.pending {
			return
		}
		err = errEndpointRequest
		if request.identity.connection != p.id || !request.destination.IsValid() || len(request.buffers) != 1 || request.buffers[0] == nil || request.buffers[0].refCount != 1 || len(request.buffers[0].Data) == 0 || cap(request.buffers[0].Data) > 64<<10 {
			return
		}
		p.closing = endpointClosing{pending: true, request: request, size: cap(request.buffers[0].Data), deadline: time.Now().Add(s.budget)}
		s.closers = append(s.closers, p)
		err = nil
	})
	return err
}

func (e *nativeEndpoint) cancelAdmission(admission endpointAdmission) {
	e.do(func(s *endpointState) { s.cancelAdmission(admission) })
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
	if s.drained || s.closed || s.err != nil {
		return true
	}
	if s.pending != nil {
		return false
	}
	packets := 1
	if !s.connection.handshakeConfirmed {
		packets = 3
	}
	if !s.recovery.CanRegisterLocalPackets(packets) {
		return true
	}
	for index := range s.records {
		record := &s.records[index]
		if record.admission.serial != 0 {
			continue
		}
		participant := s.routeParticipant()
		if participant == nil {
			return true
		}
		if s.routeSaturated(participant.endpoint) {
			return true
		}
		admission, err := participant.endpoint.prepare(participant, 0, int(protocol.MaxLargePacketBufferSize))
		if err != nil {
			if !errors.Is(err, errEndpointCapacity) && !errors.Is(err, net.ErrClosed) {
				s.err = err
			}
			return true
		}
		identity := endpointIdentity{connection: s.participant.id, generation: s.generation, pathGeneration: s.connection.pathGeneration, tuple: s.connection.pathGeneration, registration: admission.serial}
		if s.route != nil {
			identity.path, identity.pathGeneration, identity.tuple = uint64(s.route.context.PathID), s.route.context.TupleGeneration, s.route.context.TupleGeneration
		}
		*record = ordinaryLocalRecord{admission: admission, endpoint: participant.endpoint, identity: identity}
		s.pending = record
		return false
	}
	return true
}

func (s *nativeOrdinarySender) Available() <-chan struct{} { return s.participant.completed }
func (s *nativeOrdinarySender) completed() <-chan struct{} { return s.participant.completed }
func (s *nativeOrdinarySender) faulted() <-chan struct{}   { return s.endpoint.fault }
func (s *nativeOrdinarySender) Run() error                 { return nil }

// routeParticipant is where the selected path's packets are reserved and submitted.
func (s *nativeOrdinarySender) routeParticipant() *endpointParticipant {
	if s.route == nil || s.route.transport == nil {
		return s.participant
	}
	endpoint := nativeTransportEndpoint(s.route.transport)
	if endpoint == nil {
		return nil
	}
	return s.probeParticipant(endpoint)
}

// routeSaturated keeps one of the connection's records free for another endpoint, so
// a slow socket can't hold back a path on an independent one. The total is unchanged,
// and a connection with one endpoint keeps every record.
func (s *nativeOrdinarySender) routeSaturated(endpoint *nativeEndpoint) bool {
	if len(s.targets) == 0 {
		return false
	}
	held := 0
	for index := range s.records {
		if s.records[index].admission.serial != 0 && s.records[index].endpoint == endpoint {
			held++
		}
	}
	return held >= len(s.records)-1
}

// useRoute binds the next reservation. No packet is registered against an unused
// reservation, so changing route only returns it to its owning endpoint.
func (s *nativeOrdinarySender) useRoute(route *pathRoute) {
	s.finish()
	if previous := s.routeParticipant(); previous != nil {
		previous.endpoint.cancelReadiness(previous, 0)
	}
	s.route = route
}

// routeUsable reports whether a path's endpoint can still take its packets. A
// fault or close quarantines that endpoint and its paths; other paths continue.
func (s *nativeOrdinarySender) routeUsable(target *Transport) bool {
	if target == nil {
		return true
	}
	endpoint := nativeTransportEndpoint(target)
	return endpoint != nil && !endpoint.quarantined()
}

func (s *nativeOrdinarySender) register(token ackhandler.LocalCompletionToken, datagram bool) {
	if s.pending == nil || s.pending.count == len(s.pending.packets) {
		panic("local packet registration without reserved construction capacity")
	}
	s.pending.packets[s.pending.count] = ordinaryLocalPacket{token: token, datagram: datagram}
	s.pending.count++
}

func (s *nativeOrdinarySender) registerMTUProbe(handler *mtuFinderAckHandler) {
	s.pending.packets[s.pending.count-1].mtu = handler
}

func (s *nativeOrdinarySender) Send(buffer *packetBuffer, segment uint16, ecn protocol.ECN, metadata sendMetadata) {
	record := s.pending
	if record == nil {
		panic("local packet handoff without reserved construction capacity")
	}
	record.metadata, record.bytes = metadata, buffer.Len()
	destination, base, writeState := s.destination, s.oob, s.writeState
	var epoch *responseEpoch
	routed := true
	if route := s.route; route != nil {
		destination, routed = nativeDestination(route.addr)
		base, writeState, epoch = route.info.OOB(), &route.writeState, route.epoch
	}
	request := endpointRequest{
		identity: record.identity, destination: destination, buffers: []*packetBuffer{buffer},
		deadline: record.admission.deadline, oob: packetOOB(base, destination, ecn), gsoSize: segment, nativeGSO: s.nativeGSO,
		writeState: writeState, credit: metadata.credit, epoch: epoch,
	}
	record.datagrams = request.datagrams()
	err := errEndpointRequest
	if routed && (segment == 0 || record.count == record.datagrams) {
		err = record.endpoint.submit(record.admission, request)
	}
	if err != nil {
		// A sealed refusal isn't this connection's failure: the transport's destroy
		// carries the close cause, and a fault arrives through the fault channel.
		if !errors.Is(err, net.ErrClosed) {
			s.err = err
		}
		buffer.Release()
		metadata.credit.complete()
		s.finish()
		return
	}
	s.pending = nil
}

func (s *nativeOrdinarySender) packetOOB(ecn protocol.ECN) []byte {
	return packetOOB(s.oob, s.destination, ecn)
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
	s.finish()
	s.endpoint.drain(s.participant)
}

// sendClose consumes the buffer. The endpoint holds it as this connection's close
// obligation, so the caller never waits for native I/O.
func (s *nativeOrdinarySender) sendClose(buffer *packetBuffer, ecn protocol.ECN) error {
	s.drain()
	err := s.endpoint.depositClose(s.participant, endpointRequest{
		identity:    endpointIdentity{connection: s.participant.id, generation: s.generation, pathGeneration: s.connection.pathGeneration, tuple: s.connection.pathGeneration},
		destination: s.destination, buffers: []*packetBuffer{buffer}, oob: s.packetOOB(ecn), writeState: s.writeState,
	})
	if err != nil {
		buffer.Release()
	}
	return err
}

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

// sendProbe consumes the buffer. All commitments precede it, so refusal keeps
// every charge and leaves the next attempt to the validation budget. Results stay
// best effort; the endpoint fault channel reports endpoint-wide failure.
func (s *nativeOrdinarySender) sendProbe(buffer *packetBuffer, target *Transport, address net.Addr, info packetInfo, epoch *responseEpoch) error {
	participant := s.participant
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
		destination: destination, buffers: []*packetBuffer{buffer}, oob: info.OOB(), epoch: epoch,
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
	endpoint := s.endpoint
	if target != nil {
		endpoint = nativeTransportEndpoint(target)
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
	for _, packet := range record.packets[:record.count] {
		s.completePacket(packet, ackhandler.LocalPacketUnsubmitted, monotime.Now(), false)
	}
	record.endpoint.cancelAdmission(record.admission)
	*record = ordinaryLocalRecord{}
}

func (s *nativeOrdinarySender) consume() error {
	s.consumeParticipant(s.participant)
	for _, target := range s.targets {
		s.consumeParticipant(target)
	}
	select {
	case <-s.endpoint.fault:
		s.err = errors.Join(s.err, errEndpointFault)
	default:
	}
	return s.err
}

// consumeParticipant reconciles one endpoint's results. A result from before the
// latest path switch settles its tokens and nothing else, so a path the connection
// left can't end it or affect its replacement.
func (s *nativeOrdinarySender) consumeParticipant(participant *endpointParticipant) {
	for {
		result, ok := participant.endpoint.take(participant)
		if !ok {
			return
		}
		// A target's result can free the capacity the selected route is waiting for.
		s.connection.scheduleSending()
		identity := result.identity
		identity.endpoint = 0
		for index := range s.records {
			record := &s.records[index]
			if record.admission.serial == 0 || record.identity != identity {
				continue
			}
			if result.accepted < 0 || result.uncertain < 0 || result.unsubmitted < 0 || result.accepted+result.uncertain+result.unsubmitted != record.datagrams {
				s.err = errors.Join(s.err, errEndpointRequest)
				break
			}
			if result.accepted == record.datagrams && result.err == nil {
				for _, receipt := range record.metadata.terminalReceipts {
					if receipt != nil {
						receipt.publish(result.returnedAt)
					}
				}
			}
			for packetIndex, packet := range record.packets[:record.count] {
				datagram := 0
				if record.datagrams != 1 {
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
			}
			if identity.generation < s.switchGeneration {
				s.connection.scheduleSending()
			} else if isSendMsgSizeErr(result.err) {
				if record.metadata.handshake && record.datagrams == 1 && record.bytes > protocol.MinInitialPacketSize {
					s.connection.handshakeSendFeedback.publish(record.metadata.pathGeneration)
				}
			} else if result.err != nil && !localEndpointBackpressure(result.err) && !errors.Is(result.err, errEndpointRevoked) && (participant == s.participant || !participant.endpoint.quarantined()) {
				// A target's fault stays on its endpoint, whose routes the registry drains.
				s.err = errors.Join(s.err, result.err)
			}
			*record = ordinaryLocalRecord{}
			break
		}
	}
}

func (s *nativeOrdinarySender) reject() {
	s.err = errors.Join(s.err, errLocalPacketRoute)
	s.connection.scheduleSending()
}

func (s *nativeOrdinarySender) reset() {
	s.finish()
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

func (s *nativeOrdinarySender) Close() {
	if s.closed {
		return
	}
	s.closed = true
	s.recovery.CloseLocal(monotime.Now())
	s.finish()
	s.endpoint.detach(s.participant)
	for _, target := range s.targets {
		target.endpoint.detach(target)
	}
	clear(s.records[:])
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
