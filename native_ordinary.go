//go:build darwin || linux || freebsd

package quic

import (
	"errors"
	"net"
	"net/netip"
	"runtime"
	"sync/atomic"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

var nextNativeOrdinary atomic.Uint64

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
	identity  endpointIdentity
	packets   [maxGSOSegments + 3]ordinaryLocalPacket
	count     int
	datagrams int
	metadata  sendMetadata
	bytes     protocol.ByteCount
}

type nativeOrdinarySender struct {
	endpoint         *nativeEndpoint
	participant      *endpointParticipant
	recovery         ackhandler.LocalSentPacketHandler
	connection       *Conn
	destination      netip.AddrPort
	oob              []byte
	writeState       endpointWriteState
	nativeGSO        bool
	generation       uint64
	records          [4]ordinaryLocalRecord
	pending          *ordinaryLocalRecord
	err              error
	closed           bool
	droppedDatagrams uint64
	// targets holds at most one participant per other endpoint a client probed.
	// Each pass of the connection loop consumes their records, which have no
	// protocol consequence, so they need no wakeup of their own.
	targets []*endpointParticipant
}

type nativeOrdinaryConn struct {
	sendConn
	sender *nativeOrdinarySender
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
	c.conn = &nativeOrdinaryConn{sendConn: c.conn, sender: queue}
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
	if s.closed || s.err != nil {
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
		admission, err := s.endpoint.prepare(s.participant, 0, int(protocol.MaxLargePacketBufferSize))
		if err != nil {
			if !errors.Is(err, errEndpointCapacity) {
				s.err = err
			}
			return true
		}
		*record = ordinaryLocalRecord{admission: admission, identity: endpointIdentity{connection: s.participant.id, generation: s.generation, pathGeneration: s.connection.pathGeneration, tuple: s.connection.pathGeneration, registration: admission.serial}}
		s.pending = record
		return false
	}
	return true
}

func (s *nativeOrdinarySender) Available() <-chan struct{} { return s.participant.completed }
func (s *nativeOrdinarySender) completed() <-chan struct{} { return s.participant.completed }
func (s *nativeOrdinarySender) faulted() <-chan struct{}   { return s.endpoint.fault }
func (s *nativeOrdinarySender) Run() error                 { return nil }

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
	oob := append([]byte(nil), s.oob...)
	if ecn != protocol.ECNUnsupported {
		if s.destination.Addr().Unmap().Is4() {
			oob = appendIPv4ECNMsg(oob, ecn)
		} else {
			oob = appendIPv6ECNMsg(oob, ecn)
		}
	}
	request := endpointRequest{
		identity: record.identity, destination: s.destination, buffers: []*packetBuffer{buffer},
		deadline: record.admission.deadline, oob: oob, gsoSize: segment, nativeGSO: s.nativeGSO,
		writeState: &s.writeState, credit: metadata.credit,
	}
	record.datagrams = request.datagrams()
	if segment != 0 && record.count != record.datagrams {
		s.err = errEndpointRequest
	} else {
		s.err = s.endpoint.submit(record.admission, request)
	}
	if s.err != nil {
		buffer.Release()
		metadata.credit.complete()
		s.finish()
		return
	}
	s.pending = nil
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
	target, err := endpoint.attach(s.participant.id)
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
	s.endpoint.cancelAdmission(record.admission)
	*record = ordinaryLocalRecord{}
}

func (s *nativeOrdinarySender) consume() error {
	for {
		result, ok := s.endpoint.take(s.participant)
		if !ok {
			break
		}
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
			if isSendMsgSizeErr(result.err) {
				if record.metadata.handshake && record.datagrams == 1 && record.bytes > protocol.MinInitialPacketSize {
					s.connection.handshakeSendFeedback.publish(record.metadata.pathGeneration)
				}
			} else if result.err != nil && !localEndpointBackpressure(result.err) && !errors.Is(result.err, errEndpointRevoked) {
				s.err = errors.Join(s.err, result.err)
			}
			*record = ordinaryLocalRecord{}
			break
		}
	}
	for _, target := range s.targets {
		for {
			if _, ok := target.endpoint.take(target); !ok {
				break
			}
		}
	}
	select {
	case <-s.endpoint.fault:
		s.err = errors.Join(s.err, errEndpointFault)
	default:
	}
	return s.err
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
	capabilities.GSO = capabilities.GSO && !c.sender.writeState.gsoDisabled.Load()
	return capabilities
}
