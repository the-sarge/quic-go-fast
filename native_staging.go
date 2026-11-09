//go:build darwin || linux || freebsd

package quic

import (
	"errors"
	"net"
	"os"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

// exchange detaches the bounded immutable terminal FIFO and optionally admits one
// exact frozen head in the same endpoint transaction. Refusal leaves payload ownership
// with the producer. No callback or protocol processing runs under endpoint control.
func (e *nativeEndpoint) exchange(p *endpointParticipant, request *endpointRequest) ([]endpointResult, error) {
	var results []endpointResult
	err := net.ErrClosed
	e.do(func(s *endpointState) {
		if p == nil || p.endpoint != e {
			return
		}
		if len(p.results) != 0 {
			results = make([]endpointResult, 0, len(p.results))
		}
		for _, i := range p.results {
			results = append(results, s.records[i].result)
			s.records[i].reset()
			p.retained--
			s.capacityGeneration++
		}
		p.results = p.results[:0]
		// A drain-only exchange still observes admission usability. Local staged
		// work must not wait for a grant or its deadline after this owner closes.
		if s.sealed || s.faulted || !s.valid(p) || p.draining {
			return
		}
		err = nil
		if request != nil {
			err = s.offer(p, 0, *request)
		}
	})
	return results, err
}

// stagedHead is the sole ordinary demand owner for one endpoint participant.
// A later local record may be built for another route, but cannot pass this head.
func (s *nativeOrdinarySender) stagedHead(p *endpointParticipant) *ordinaryLocalRecord {
	var head *ordinaryLocalRecord
	for i := range s.records {
		r := &s.records[i]
		if r.state == ordinaryStaged && r.participant == p && (head == nil || r.identity.registration < head.identity.registration) {
			head = r
		}
	}
	return head
}

func (s *nativeOrdinarySender) serviceParticipant(p *endpointParticipant) {
	if p == nil {
		return
	}
	// Consume this participant's atomic hint before entering endpoint control. A later
	// publication leaves another hint even if the shared channel was coalesced.
	notified := p.notification.Swap(0) != 0
	ready := false
	select {
	case <-p.ready:
		ready = true
	default:
	}
	// At most four records exist; this loop can only advance to a different head.
	for range len(s.records) + 1 {
		head := s.stagedHead(p)
		var request *endpointRequest
		if head != nil && (!head.offered || ready) {
			request = &head.request
		}
		if request == nil && !notified {
			return
		}
		results, err := p.endpoint.exchange(p, request)
		notified = false
		if errors.Is(err, net.ErrClosed) {
			// The endpoint owns admitted requests through their native fence; only
			// Building/Staged records can be settled on this terminal observation.
			s.cancelLocal(p)
		}
		if request != nil && head.state == ordinaryStaged {
			head.offered = true
			if err == nil {
				head.state = ordinaryEndpointOwned
				// Commit cloned the descriptors and OOB. Retain only cleared private
				// backing; the endpoint now owns the only payload reference.
				head.clearRequest()
			} else if !errors.Is(err, errEndpointCapacity) {
				s.settleLocal(head, err)
			}
		}
		// Detach every fact before any callback. TerminalProcessing prevents callback
		// reentry from reusing a record whose original facts are still being applied.
		for _, result := range results {
			s.deferResult(result)
		}
		if request == nil || errors.Is(err, errEndpointCapacity) {
			return
		}
		ready = false
	}
}

// clearRequest runs after snapshot transfer, never on a refused Staged request.
// Empty private backing carries no payload or authority.
func (r *ordinaryLocalRecord) clearRequest() {
	members := r.request.members
	clear(members[:cap(members)])
	r.request = endpointRequest{}
	if cap(members) <= endpointBatch {
		r.request.members = members[:0]
	}
}

// freeRecord is called only at the existing final Free transitions, after all
// terminal facts and callback-capable release/completion work return. Read the
// headers here, not before callbacks: Close must never have its backing restored.
func (s *nativeOrdinarySender) freeRecord(r *ordinaryLocalRecord) {
	packets, members, requestMembers := r.packets, r.members, r.request.members
	clear(packets[:cap(packets)])
	clear(members[:cap(members)])
	clear(requestMembers[:cap(requestMembers)])
	*r = ordinaryLocalRecord{}
	if s.closed {
		return
	}
	// These cap empty-storage retention only. Construction and admission retain
	// their original limits, and a larger allocation is simply discarded.
	if cap(packets) <= endpointBatch*(maxGSOSegments+3) {
		r.packets = packets[:0]
	}
	if cap(members) <= endpointBatch {
		r.members = members[:0]
	}
	if cap(requestMembers) <= endpointBatch {
		r.request.members = requestMembers[:0]
	}
}

func (s *nativeOrdinarySender) settleLocal(r *ordinaryLocalRecord, err error) {
	if r.state != ordinaryBuilding && r.state != ordinaryStaged {
		return
	}
	if r.state == ordinaryBuilding && r.count == 0 {
		if s.pending == r {
			s.pending = nil
		}
		s.freeRecord(r)
		return
	}
	p := r.participant
	if s.stagedHead(p) == r && r.offered {
		p.endpoint.cancelReadiness(p, 0)
	}
	if s.pending == r {
		s.pending = nil
	}
	r.state = ordinaryTerminalProcessing
	r.request.releasePayload()
	for _, packet := range r.packets[:r.count] {
		if packet.datagram {
			s.droppedDatagrams++
		}
		s.completePacket(packet, ackhandler.LocalPacketUnsubmitted, monotime.Now(), false)
	}
	if credit := s.connection.pathCredit; credit != nil && credit.hasAdmission() {
		credit.noteOutcome(protocol.PathID(r.identity.path), r.identity.tuple, 0, 0, r.datagrams)
	}
	// Local refusal is governed by the same route/fault policy as endpoint facts;
	// an unadmitted revoked/expired request is not native path-failure evidence.
	if err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, errEndpointRevoked) && !errors.Is(err, os.ErrDeadlineExceeded) {
		s.err = errors.Join(s.err, err)
	}
	s.freeRecord(r)
	s.connection.scheduleSending()
}

func (s *nativeOrdinarySender) cancelLocal(p *endpointParticipant) {
	for i := range s.records {
		r := &s.records[i]
		if p == nil || r.participant == p {
			s.settleLocal(r, errEndpointRevoked)
		}
	}
}

func (s *nativeOrdinarySender) expireLocal() {
	now := monotime.Now()
	for i := range s.records {
		r := &s.records[i]
		if r.state != ordinaryStaged {
			continue
		}
		if !r.request.authorized() {
			s.settleLocal(r, errEndpointRevoked)
		} else if !now.Before(r.deadline) {
			s.settleLocal(r, os.ErrDeadlineExceeded)
		}
	}
}

func (s *nativeOrdinarySender) localDeadline() monotime.Time {
	var deadline monotime.Time
	for i := range s.records {
		r := &s.records[i]
		if r.state == ordinaryStaged && (deadline == 0 || r.deadline.Before(deadline)) {
			deadline = r.deadline
		}
	}
	return deadline
}

// progress is event driven. The atomic hint also covers a select that already
// consumed the shared wake, without polling an endpoint for terminal facts.
func (s *nativeOrdinarySender) progress(p *endpointParticipant) {
	head := s.stagedHead(p)
	if p.notification.Load() != 0 || (head != nil && !head.offered) {
		s.serviceParticipant(p)
	}
}

// deferResult transfers immutable facts into the same record that owns their
// recovery sidecar. Registration/handoff never invokes older recovery callbacks.
func (s *nativeOrdinarySender) deferResult(result endpointResult) {
	s.connection.scheduleSending()
	identity := result.identity
	identity.endpoint = 0
	for i := range s.records {
		r := &s.records[i]
		if r.state == ordinaryEndpointOwned && r.identity == identity {
			r.state = ordinaryTerminalProcessing
			r.terminal = result
			s.deferred = append(s.deferred, r)
			return
		}
	}
}

func (s *nativeOrdinarySender) consumeDeferred() {
	if s.processing {
		return
	}
	s.processing = true
	defer func() { s.processing = false }()
	for len(s.deferred) != 0 {
		r := s.deferred[0]
		s.deferred = s.deferred[1:]
		result, p := r.terminal, r.participant
		r.terminal = endpointResult{}
		if r.state == ordinaryTerminalProcessing {
			s.consumeResult(p, result)
		}
	}
	s.deferred = nil
}

func (s *nativeOrdinarySender) cancelZero() {
	for i := range s.records {
		if s.records[i].identity.path == 0 {
			s.settleLocal(&s.records[i], errEndpointRevoked)
		}
	}
}

func (r *ordinaryLocalRecord) memberEnd() int {
	if len(r.members) == 0 {
		return 0
	}
	return r.members[len(r.members)-1].end
}

// bufferRoom counts all local states, including endpoint-owned payloads whose
// member descriptors have transferred. A path never owns over 32 buffers and the
// connection never owns over 4 MiB, even while another endpoint is blocked.
func (s *nativeOrdinarySender) bufferRoom(route *pathRoute, capacity protocol.ByteCount) bool {
	var backing protocol.ByteCount
	buffers := 0
	path := uint64(0)
	if route != nil {
		path = uint64(route.context.PathID)
	}
	for i := range s.records {
		r := &s.records[i]
		if r.state == ordinaryFree {
			continue
		}
		backing += r.backing
		if r.identity.path == path {
			buffers += len(r.members)
		}
	}
	return buffers < endpointBatch && backing+capacity <= 4<<20
}

// reserveBuffer checks the actual pool tier before destructive construction.
// Reaching a request bound seals it without waiting for future work.
func (s *nativeOrdinarySender) reserveBuffer(capacity protocol.ByteCount) (result localBufferReservation) {
	if s.pending != nil && (len(s.pending.members) == endpointBatch || s.pending.backing+capacity > 1<<20) {
		s.finish()
		result.finished = true
	}
	blocked, finished := s.wouldBlock()
	result.finished = result.finished || finished
	result.available = !blocked && s.bufferRoom(s.route, capacity)
	return result
}

func (s *nativeOrdinarySender) canSoftwareSegment() bool {
	return s.connection.handshakeConfirmed && s.generation == 1 && s.scalarPacketAllowance()
}

func (s *nativeOrdinarySender) scalarPacketAllowance() bool {
	conn, ok := s.connection.conn.(*nativeOrdinaryConn)
	// The underlying capability cannot turn false through this native writer:
	// its EIO latch is separate. A replacement is excluded, as are routed paths.
	return ok && s.switchGeneration == 0 && s.origin == s.participant && s.route == nil && s.connection.pathCredit == nil && s.connection.emission.bbr == nil && len(s.connection.emission.terminalReceipts) == 0 &&
		!conn.sendConn.capabilities().GSO && !conn.writeState.gsoDisabled.Load()
}

// softwarePacketRoom must not seal an unhanded buffer. Actual pool capacity is
// reserved before construction; handed members retain their charge until Free.
func (s *nativeOrdinarySender) softwarePacketRoom() bool {
	return !s.closed && !s.drained && s.err == nil && s.recovery.CanRegisterLocalPackets(1) && s.bufferRoom(s.route, 0)
}

func (s *nativeOrdinarySender) constructionIdentity(route *pathRoute) endpointIdentity {
	identity := endpointIdentity{connection: s.participant.id, generation: s.generation, pathGeneration: s.connection.pathGeneration, tuple: s.connection.pathGeneration}
	if route != nil {
		identity.path, identity.pathGeneration, identity.tuple = uint64(route.context.PathID), route.context.TupleGeneration, route.context.TupleGeneration
	}
	return identity
}

func (s *nativeOrdinarySender) constructionEpoch() *responseEpoch {
	if s.route != nil {
		return s.route.epoch
	}
	if credit := s.connection.pathCredit; credit != nil {
		return credit.zeroEpoch()
	}
	return nil
}

// releasePayload follows the final native fence (or a decision never to submit).
// Releasing all buffers first prevents a credit wake from reusing a live borrow.
func (request endpointRequest) releasePayload() {
	for i := range request.members {
		member := &request.members[i]
		plan := member.protection
		member.protection = nil
		plan.release()
		if member.buffer != nil {
			member.buffer.Release()
		}
	}
	for _, member := range request.members {
		member.credit.complete()
	}
}

// finishPrefixWhenNativeEmpty reports only this call's actual finish action.
// The query grants nothing, and finish runs after endpoint control is released.
func (s *nativeOrdinarySender) finishPrefixWhenNativeEmpty() bool {
	r := s.pending
	if s.closed || s.drained || s.err != nil || r == nil || r.state != ordinaryBuilding || len(r.members) == 0 || r.count != r.memberEnd() || !s.canSoftwareSegment() {
		return false
	}
	if !r.participant.endpoint.nativeWorkEmpty(r.participant) {
		return false
	}
	s.finish()
	return true
}
