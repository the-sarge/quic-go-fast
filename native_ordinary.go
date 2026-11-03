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

type ordinaryLocalPacket struct {
	token    ackhandler.LocalCompletionToken
	datagram bool
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
}

type nativeOrdinaryConn struct {
	sendConn
	sender *nativeOrdinarySender
}

func (c *Conn) useNativeOrdinary(endpoint *nativeEndpoint, destination netip.AddrPort, info packetInfo) error {
	if endpoint == nil || !destination.IsValid() || c.emission.local != nil || c.sentFirstPacket || !c.config.DisablePathMTUDiscovery || c.emission.pathCredit != nil || c.emission.authority != nil || c.emission.sendQoS != nil {
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

func (s *nativeOrdinarySender) WouldBlock() bool {
	if s.closed || s.err != nil {
		return true
	}
	if s.pending != nil {
		return false
	}
	if !s.recovery.CanRegisterLocalPackets(maxGSOSegments + 3) {
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

func (s *nativeOrdinarySender) finish() {
	if s.pending == nil {
		return
	}
	record := s.pending
	s.pending = nil
	for _, packet := range record.packets[:record.count] {
		if err := s.recovery.CompleteLocalPacket(packet.token, ackhandler.LocalPacketUnsubmitted, monotime.Now()); err != nil {
			s.err = errors.Join(s.err, err)
		}
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
				if err := s.recovery.CompleteLocalPacket(packet.token, outcome, result.returnedAt); err != nil {
					s.err = errors.Join(s.err, err)
				}
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
	s.generation++
}

func (s *nativeOrdinarySender) Close() {
	if s.closed {
		return
	}
	s.closed = true
	s.recovery.CloseLocal(monotime.Now())
	s.finish()
	s.endpoint.detach(s.participant)
	clear(s.records[:])
}

func (c *nativeOrdinaryConn) Write([]byte, uint16, protocol.ECN) error   { return errLocalPacketRoute }
func (c *nativeOrdinaryConn) WriteTo([]byte, net.Addr, packetInfo) error { return errLocalPacketRoute }
func (c *nativeOrdinaryConn) Close() error                               { return errLocalPacketRoute }
func (c *nativeOrdinaryConn) ChangeRemoteAddr(net.Addr, packetInfo) {
	c.sender.reject()
}
func (c *nativeOrdinaryConn) capabilities() connCapabilities {
	capabilities := c.sendConn.capabilities()
	capabilities.GSO = capabilities.GSO && !c.sender.writeState.gsoDisabled.Load()
	return capabilities
}
