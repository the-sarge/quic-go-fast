//go:build unix

package quic

import (
	"bytes"
	"errors"
	"io"
	"iter"
	"net"
	"net/netip"
	"os"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/monotime"
)

// This route is deliberately private: only Transport.configureNativeIO selects
// it, and no exported constructor does. R19 owns public activation. In particular,
// this is not a PacketConn provider or permission to unwrap a caller's socket.
const (
	endpointRequests     = 128
	endpointRecords      = 256
	endpointParticipants = 4096
	endpointBatch        = 32
	endpointBytes        = 8 << 20
	endpointTolerance    = 20 * time.Millisecond
	endpointLease        = 10 * time.Millisecond
	endpointRetry        = time.Millisecond
)

type endpointClass uint8

const (
	endpointData endpointClass = iota
	endpointControl
	endpointStateless
	endpointPublic
)

var (
	endpointClassCounts = [4]int{104, 8, 8, 8}
	endpointClassBytes  = [4]int{6 << 20, 1 << 20, 512 << 10, 512 << 10}
)

var (
	errEndpointCapacity = errors.New("quic: native endpoint capacity unavailable")
	errEndpointRequest  = errors.New("quic: invalid native endpoint request")
	errEndpointRevoked  = errors.New("quic: native endpoint request revoked")
	errEndpointFault    = errors.New("quic: native endpoint faulted")
)

// Identities are values captured by the protocol owner, never read from mutable
// connection state by the writer. Results carry them back unchanged.
type endpointIdentity struct {
	endpoint, connection, generation, path, pathGeneration, tuple, registration uint64
}

var nextNativeEndpoint atomic.Uint64

// A member owns exactly one payload alternative. Packet buffers are released by
// the endpoint; raw datagrams retain their separate public/stateless lifetime.
// Geometry and credit belong to this member, never to the enclosing request.
type endpointMember struct {
	protection *nativeProtectionPlan
	buffer     *packetBuffer
	datagram   *endpointDatagram
	oob        []byte
	gsoSize    uint16
	nativeGSO  bool
	writeState *endpointWriteState
	credit     *sendReservation
}

type endpointRequest struct {
	identity                   endpointIdentity
	destination                netip.AddrPort
	members                    []endpointMember
	epoch                      *responseEpoch // nil grants no additional revocation owner
	deadline                   time.Time      // zero selects admission+B; an earlier deadline is allowed
	shortSoftwareDatagramGroup bool           // includes a segmented short DATAGRAM member sent as scalar UDP writes
}

type endpointMemberResult struct {
	accepted, uncertain, unsubmitted int
	bytes                            int
	err                              error
	returnedAt                       monotime.Time // native sequence return, or local terminal decision
}

type endpointResult struct {
	identity endpointIdentity
	members  []endpointMemberResult
	err      error // deadline reset failure; never rewrites member facts
}

// failure derives the request failure without storing a second result authority.
func (r endpointResult) failure() error {
	err := r.err
	for _, member := range r.members {
		err = errors.Join(err, member.err)
	}
	return err
}

type endpointWriteState struct {
	wroteFirstPacket bool
	gsoDisabled      atomic.Bool
}

type endpointAdmission struct {
	index    int
	serial   uint64
	deadline time.Time
}

// authorized reports whether every owner that can revoke this request permits it.
func (request endpointRequest) authorized() bool {
	if !request.epoch.valid() {
		return false
	}
	for _, member := range request.members {
		if member.datagram != nil && !member.datagram.epoch.valid() {
			return false
		}
	}
	return true
}

// checkedSize is the single validator for admitted member geometry. It does not
// consume payloads on refusal. Raw datagrams are singleton auxiliary requests.
func (request endpointRequest) checkedSize(class endpointClass) (int, bool) {
	if !request.destination.IsValid() || len(request.members) == 0 || len(request.members) > endpointBatch {
		return 0, false
	}
	size := 0
	for i, member := range request.members {
		if member.datagram != nil {
			if member.protection != nil || member.buffer != nil || len(request.members) != 1 || class < endpointStateless || member.gsoSize != 0 || member.nativeGSO || member.writeState != nil || member.credit != nil {
				return 0, false
			}
			size += cap(member.datagram.data)
			continue
		}
		buffer := member.buffer
		if buffer == nil || buffer.refCount != 1 || len(buffer.Data) == 0 {
			return 0, false
		}
		if member.protection != nil && !member.protection.validForMember(member) {
			return 0, false
		}
		for _, earlier := range request.members[:i] {
			if earlier.buffer == buffer || (member.credit != nil && earlier.credit == member.credit) {
				return 0, false
			}
		}
		size += cap(buffer.Data)
	}
	return size, size <= 1<<20
}

func (request endpointRequest) snapshot() endpointRequest { //nolint:unused,nolintlint // Ordinary native callers are limited to darwin, linux and freebsd.
	return request.snapshotInto(nil)
}

// snapshotInto copies across owners even when members supplies cleared private
// storage from an earlier request. Neither descriptors nor caller OOB are shared.
func (request endpointRequest) snapshotInto(members []endpointMember) endpointRequest {
	if request.members == nil {
		return request
	}
	if members == nil || cap(members) < len(request.members) {
		// Exact logical capacity keeps a maximum-size request within the private
		// retention bound even when its allocation occupies a larger size class.
		members = make([]endpointMember, len(request.members))
	} else {
		members = members[:len(request.members)]
	}
	copy(members, request.members)
	request.members = members
	for i := range request.members {
		// The writer only borrows OOB; GSO appends to private scratch. Keep one
		// endpoint-owned clone per equal run without sharing any caller bytes.
		if i != 0 && len(request.members[i].oob) != 0 && bytes.Equal(request.members[i].oob, request.members[i-1].oob) {
			request.members[i].oob = request.members[i-1].oob
			continue
		}
		request.members[i].oob = slices.Clone(request.members[i].oob)
	}
	return request
}

func (member endpointMember) datagrams() int {
	if member.datagram != nil || member.gsoSize == 0 {
		return 1
	}
	return (len(member.buffer.Data) + int(member.gsoSize) - 1) / int(member.gsoSize)
}

func newEndpointResult(storage []endpointMemberResult, count int) endpointResult {
	if cap(storage) < count {
		storage = make([]endpointMemberResult, count)
	} else {
		storage = storage[:count]
	}
	return endpointResult{members: storage}
}

func (request endpointRequest) unsubmitted(err error, storage []endpointMemberResult) endpointResult {
	result := newEndpointResult(storage, len(request.members))
	now := monotime.Now()
	for i, member := range request.members {
		result.members[i] = endpointMemberResult{unsubmitted: member.datagrams(), err: err, returnedAt: now}
	}
	return result
}

// These are the native system boundary, not callbacks on the control path.
// The only socket constructor below accepts an exact, unconnected UDPConn.
type endpointNativeIO interface {
	SetWriteDeadline(time.Time) error
	WriteMsgUDPAddrPort([]byte, []byte, netip.AddrPort) (int, int, error)
}

type endpointDiagnostic struct {
	identity endpointIdentity
	kind     uint8
}

type nativeEndpoint struct {
	generation        uint64
	budget            time.Duration // immutable local preparation budget
	mutex             sync.Mutex
	state             *endpointState
	work              chan struct{} // enqueue/park hint; never carries a claim
	progress          chan struct{}
	halt              chan struct{}
	writer            chan struct{} // closed when the writer can no longer touch the socket
	done              chan struct{} // closed once, after writer, to return write authority
	fault             chan struct{}
	faultRecord       endpointFaultRecord // written once, before fault closes
	stopped           func()              // owned by control; runs just before done closes
	diagnostics       chan endpointDiagnostic
	dropped           atomic.Uint64
	stateless, public *endpointParticipant
}

type endpointFaultCause uint8

const (
	// endpointFaultOverdue found no acknowledgment at the cutoff.
	endpointFaultOverdue endpointFaultCause = iota + 1
	// endpointFaultLateReturn found an acknowledgment stamped at or after the cutoff.
	endpointFaultLateReturn
	// endpointFaultNative is a closed socket or a failed deadline operation.
	endpointFaultNative
)

// endpointFaultRecord is reserved in the endpoint, so publishing it needs no
// capacity and can't be dropped. It outlives a successful join: the join returns
// authority without certifying that earlier writes succeeded.
type endpointFaultRecord struct {
	cause    endpointFaultCause
	identity endpointIdentity
	err      error
}

type endpointParticipant struct {
	endpoint     *nativeEndpoint
	id           uint64
	slot         int
	ready        chan uint64
	completed    chan struct{}
	retired      chan struct{}
	class        endpointClass
	notification atomic.Uint64 // coalesced progress hint, never admission authority
	// The fields below belong exclusively to endpoint control.
	active                     bool
	retained                   int
	results                    []int
	wait                       [3]endpointWait
	admissionPath, servicePath int
	// draining means the connection has sealed its protocol consequences: its
	// queued work keeps running, and control disposes of its results. Detaching
	// takes effect once its close obligation and retained requests are gone.
	draining, detaching bool
	closings            []endpointClosing
	// closeDeposited stays set once a close is deposited. Retirement then resolves it,
	// whichever way that close ended.
	closeDeposited bool
}

// endpointClosing is one of a connection's close obligations, at most one per path, held in a field that
// attachment already reserved. The buffer stays outside the endpoint's capacity
// until admission, like any other producer-owned packet.
type endpointClosing struct {
	request  endpointRequest
	size     int
	deadline time.Time
}

type endpointWait struct {
	sequence  uint64
	pending   bool
	bytes     int
	notBefore time.Time
}

type endpointGrant struct {
	participant *endpointParticipant
	lane        int
	expires     time.Time
}

type endpointRecord struct {
	used, terminal bool
	reserved       bool
	serial         uint64
	revoked        bool
	participant    *endpointParticipant
	lane           int
	class          endpointClass
	bytes          int
	request        endpointRequest
	result         endpointResult
}

// clearRequest follows the final native fence (or a decision never to submit)
// and payload release. Empty backing retains no payload, identity or authority.
func (r *endpointRecord) clearRequest() {
	members := r.request.members
	clear(members[:cap(members)])
	r.request = endpointRequest{}
	if cap(members) <= endpointBatch {
		r.request.members = members[:0]
	}
}

// reset ends the record's existing reservation or result lifetime. Only bounded,
// fully cleared descriptor storage remains with this same canonical record.
func (r *endpointRecord) reset() {
	r.clearRequest()
	members := r.request.members
	*r = endpointRecord{request: endpointRequest{members: members}}
}

type endpointToken struct {
	index    int
	request  *endpointRequest // canonical current record, fenced through actual return
	canceled atomic.Bool
	// The writer publishes once, after all native/deadline operations. The
	// immutable fact is authoritative even while its wakeup is still pending.
	completion atomic.Pointer[endpointWriteResult]
	fact       endpointWriteResult
}

type endpointWriteResult struct {
	result     endpointResult
	fault      bool
	returnedAt time.Time
}

// Small claims share one fresh allocation with their immutable member facts.
// These objects are never recycled: an old result can outlive its endpoint record.
type endpointOneMemberClaim struct {
	token   endpointToken
	members [1]endpointMemberResult
}

type endpointTwoMemberClaim struct {
	token   endpointToken
	members [2]endpointMemberResult
}

func newEndpointToken(index int, request *endpointRequest) *endpointToken {
	var token *endpointToken
	switch len(request.members) {
	case 1:
		claim := new(endpointOneMemberClaim)
		token = &claim.token
		token.fact.result.members = claim.members[:]
	case 2:
		claim := new(endpointTwoMemberClaim)
		token = &claim.token
		token.fact.result.members = claim.members[:]
	default:
		token = new(endpointToken)
	}
	token.index, token.request = index, request
	return token
}

type endpointState struct {
	endpoint                           *nativeEndpoint
	budget                             time.Duration
	participants                       [endpointParticipants + 2]*endpointParticipant
	occupied                           []*endpointParticipant // slot-ordered references to canonical participants
	records                            [endpointRecords]endpointRecord
	queue                              []int
	current                            *endpointToken
	count, bytes                       int
	sealed, faulted, terminal          bool
	credits                            []*sendReservation // detached and returned outside control
	classCount, classBytes             [4]int
	grant                              *endpointGrant
	admissionCursor, serviceCursor     [4]int
	admissionControls, serviceControls int
	admissionClass, serviceClass       int
	capacityGeneration                 uint64
	nextAdmission                      uint64
	closers                            []*endpointParticipant
}

func newNativeEndpoint(socket *net.UDPConn, budget time.Duration, sink func(endpointDiagnostic)) (*nativeEndpoint, error) {
	if socket == nil || socket.RemoteAddr() != nil || budget < time.Millisecond || budget > time.Second {
		return nil, errEndpointRequest
	}
	return startNativeEndpoint(socket, budget, sink), nil
}

func startNativeEndpoint(native endpointNativeIO, budget time.Duration, sink func(endpointDiagnostic)) *nativeEndpoint {
	e := &nativeEndpoint{
		budget:     budget,
		generation: nextNativeEndpoint.Add(1),
		work:       make(chan struct{}, 1), progress: make(chan struct{}, 1), halt: make(chan struct{}),
		writer: make(chan struct{}), done: make(chan struct{}), fault: make(chan struct{}),
	}
	if sink != nil {
		e.diagnostics = make(chan endpointDiagnostic, 128)
		go func() {
			for d := range e.diagnostics {
				sink(d)
			}
		}()
	}
	s := &endpointState{
		endpoint: e, budget: budget, queue: make([]int, 0, endpointRequests),
		occupied: make([]*endpointParticipant, 0, endpointParticipants+2),
	}
	e.state = s
	for i, class := range []endpointClass{endpointStateless, endpointPublic} {
		p := newEndpointParticipant(e, 0, endpointParticipants+i, class)
		s.install(p)
		if class == endpointStateless {
			e.stateless = p
		} else {
			e.public = p
		}
	}
	go e.write(native)
	go s.run()
	return e
}

func (e *nativeEndpoint) emitDiagnostic(d endpointDiagnostic) {
	if e.diagnostics == nil {
		return
	}
	select {
	case e.diagnostics <- d:
	default:
		e.dropped.Add(1)
	}
}

func newEndpointParticipant(e *nativeEndpoint, id uint64, slot int, class endpointClass) *endpointParticipant {
	return &endpointParticipant{
		endpoint: e, id: id, slot: slot, class: class, active: true,
		ready: make(chan uint64, 1), completed: make(chan struct{}, 1), retired: make(chan struct{}), results: make([]int, 0, 8),
	}
}

// do is one synchronous transaction under the endpoint's sole state owner.
// Native operations, credit locks and the final socket-grant callback never run here.
func (e *nativeEndpoint) do(f func(*endpointState)) {
	e.mutex.Lock()
	s := e.state
	if !s.terminal {
		s.advance(time.Now())
		f(s)
		s.advance(time.Now())
		s.wakeWriter()
		e.notifyProgress()
	}
	e.unlock()
}

// nativeWorkEmpty observes canonical native supply, not admission or serviceability.
// Even a published return stays busy until the existing owner reconciles current.
func (e *nativeEndpoint) nativeWorkEmpty(p *endpointParticipant) bool { //nolint:unused,nolintlint // Ordinary native staging is limited to darwin, linux and freebsd.
	e.mutex.Lock()
	s := e.state
	empty := !s.sealed && !s.faulted && !s.terminal && s.valid(p) && !p.draining && s.current == nil && len(s.queue) == 0
	e.mutex.Unlock()
	return empty
}

func (e *nativeEndpoint) notifyProgress() {
	select {
	case e.progress <- struct{}{}:
	default:
	}
}

// unlock detaches credit returns before dropping control. Buffer/plan disposal
// uses only concrete pool operations; credit completion takes a separate lock.
func (e *nativeEndpoint) unlock() {
	credits := e.state.credits
	e.state.credits = nil
	e.mutex.Unlock()
	for _, credit := range credits {
		credit.complete()
	}
}

func (s *endpointState) releasePayload(request endpointRequest) {
	for i := range request.members {
		member := &request.members[i]
		plan := member.protection
		member.protection = nil
		plan.release()
		if member.buffer != nil {
			member.buffer.Release()
		}
		if member.credit != nil {
			s.credits = append(s.credits, member.credit)
		}
	}
}

func (e *nativeEndpoint) attach(id uint64) (*endpointParticipant, error) {
	return e.attachShared(id, nil)
}

// attachShared attaches a participant that signals an existing wake channel, or a
// new one when wake is nil. Only the channel's one consumer receives from it.
func (e *nativeEndpoint) attachShared(id uint64, wake chan struct{}) (*endpointParticipant, error) {
	var p *endpointParticipant
	err := net.ErrClosed
	e.do(func(s *endpointState) {
		if s.sealed || s.faulted {
			err = net.ErrClosed
			return
		}
		err = errEndpointCapacity
		for _, existing := range s.participants[:endpointParticipants] {
			if existing != nil && existing.id == id {
				return
			}
		}
		for i, existing := range s.participants[:endpointParticipants] {
			if existing == nil {
				p = newEndpointParticipant(e, id, i, endpointData)
				if wake != nil {
					p.completed = wake
				}
				s.install(p)
				err = nil
				return
			}
		}
	})
	return p, err
}

func (s *endpointState) valid(p *endpointParticipant) bool {
	return p != nil && p.endpoint == s.endpoint && p.slot >= 0 && p.slot < len(s.participants) && s.participants[p.slot] == p && p.active
}

func (s *endpointState) participantPosition(slot int) int {
	i, _ := slices.BinarySearchFunc(s.occupied, slot, func(p *endpointParticipant, slot int) int { return p.slot - slot })
	return i
}

func (s *endpointState) install(p *endpointParticipant) {
	s.participants[p.slot] = p
	s.occupied = slices.Insert(s.occupied, s.participantPosition(p.slot), p)
}

// occupiedParticipants preserves the table's circular slot order without walking
// unused capacity. Membership changes only at installation and final retirement;
// callers must not change membership during this walk.
func (s *endpointState) occupiedParticipants(cursor int) iter.Seq[*endpointParticipant] {
	return func(yield func(*endpointParticipant) bool) {
		start := s.participantPosition(cursor)
		for _, p := range s.occupied[start:] {
			if !yield(p) {
				return
			}
		}
		for _, p := range s.occupied[:start] {
			if !yield(p) {
				return
			}
		}
	}
}

// offer transfers buffers only on success. On refusal the caller still owns
// both the buffers and their backing array; neither is retained by the endpoint.
func (e *nativeEndpoint) offer(p *endpointParticipant, lane int, request endpointRequest) error {
	err := net.ErrClosed
	e.do(func(s *endpointState) { err = s.offer(p, lane, request) })
	return err
}

func (s *endpointState) offer(p *endpointParticipant, lane int, request endpointRequest) error {
	if s.sealed || s.faulted || !s.valid(p) || p.draining || !request.authorized() {
		return net.ErrClosed
	}
	if lane < 0 || lane > 2 || request.identity.connection != p.id || (p.class != endpointData && lane != 0) {
		return errEndpointRequest
	}
	n, valid := request.checkedSize(p.class)
	if !valid {
		return errEndpointRequest
	}
	wait := &p.wait[lane]
	if wait.sequence != request.identity.registration {
		wait.pending = false
		if g := s.grant; g != nil && g.participant == p && g.lane == lane {
			s.releaseGrant()
		}
		wait.sequence = request.identity.registration
	}
	admission, err := s.admit(p, lane, n)
	if err == nil {
		s.commit(admission, request)
	}
	return err
}

func (s *endpointState) admit(p *endpointParticipant, lane, size int) (endpointAdmission, error) {
	if s.sealed || s.faulted || !s.valid(p) {
		return endpointAdmission{}, net.ErrClosed
	}
	if lane < 0 || lane > 2 || (p.class != endpointData && lane != 0) || size < 0 || (size == 0 && p.class < endpointStateless) || size > 1<<20 {
		return endpointAdmission{}, errEndpointRequest
	}
	class := p.class
	if class == endpointData && lane == 2 {
		class = endpointControl
	}
	if class != endpointData && size > 64<<10 {
		return endpointAdmission{}, errEndpointRequest
	}
	now := time.Now()
	s.reclaimGrant(now)
	wait := &p.wait[lane]
	direct := class >= endpointStateless
	recordsNeeded := 1
	if direct {
		// Consider registered persistent demand before spending a shared record.
		// A selected grant protects one record even while accumulating capacity;
		// materialized reservations are already charged in records below.
		s.chooseGrant(now)
		if now.Before(wait.notBefore) || p.retained >= 8 || !s.hasCapacity(class, size) {
			return endpointAdmission{}, errEndpointCapacity
		}
		if s.grant != nil {
			recordsNeeded++
		}
	} else {
		if !wait.pending || wait.bytes != size {
			if s.grant != nil && s.grant.participant == p && s.grant.lane == lane {
				s.releaseGrant()
			}
			wait.pending, wait.bytes = true, size
		}
		s.arbitrate(now, p, lane)
		if s.grant == nil || s.grant.participant != p || s.grant.lane != lane || s.grant.expires.IsZero() {
			return endpointAdmission{}, errEndpointCapacity
		}
	}
	for index := range s.records {
		if s.records[index].used {
			continue
		}
		recordsNeeded--
		if recordsNeeded != 0 {
			continue
		}
		s.nextAdmission++
		admission := endpointAdmission{index: index, serial: s.nextAdmission, deadline: now.Add(s.budget)}
		record := &s.records[index]
		*record = endpointRecord{
			used: true, reserved: true, serial: admission.serial, participant: p, lane: lane, class: class, bytes: size,
			request: endpointRequest{members: record.request.members},
		}
		s.count++
		s.bytes += size
		s.classCount[class]++
		s.classBytes[class] += size
		p.retained++
		if !direct {
			wait.pending = false
			s.grant = nil
			select {
			case <-p.ready:
			default:
			}
		}
		return admission, nil
	}
	return endpointAdmission{}, errEndpointCapacity
}

func (s *endpointState) hasCapacity(class endpointClass, size int) bool {
	return s.count < endpointRequests && size <= endpointBytes-s.bytes && s.classCount[class] < endpointClassCounts[class] && size <= endpointClassBytes[class]-s.classBytes[class]
}

func (s *endpointState) commit(admission endpointAdmission, request endpointRequest) {
	record := &s.records[admission.index]
	size, _ := request.checkedSize(record.participant.class)
	released := record.bytes - size
	s.bytes -= released
	s.classBytes[record.class] -= released
	record.bytes = size
	if released != 0 {
		s.capacityGeneration++
	}
	record.reserved = false
	record.request = request.snapshotInto(record.request.members)
	record.request.identity.endpoint = s.endpoint.generation
	if request.deadline.IsZero() || request.deadline.After(admission.deadline) {
		record.request.deadline = admission.deadline
	}
	s.queue = append(s.queue, admission.index)
}

func (s *endpointState) cancelAdmission(admission endpointAdmission) {
	record := &s.records[admission.index]
	if !record.used || !record.reserved || record.serial != admission.serial {
		return
	}
	s.count--
	s.bytes -= record.bytes
	s.classCount[record.class]--
	s.classBytes[record.class] -= record.bytes
	record.participant.retained--
	s.capacityGeneration++
	record.reset()
}

func (e *nativeEndpoint) take(p *endpointParticipant) (endpointResult, bool) {
	var result endpointResult
	var ok bool
	e.do(func(s *endpointState) {
		if p == nil || p.endpoint != e || len(p.results) == 0 {
			return
		}
		i := p.results[0]
		p.results = p.results[1:]
		result, ok = s.records[i].result, true
		s.records[i].reset()
		p.retained--
		s.capacityGeneration++
		// A connection's participants can share this channel, so control only
		// signals it. A drain here could swallow another endpoint's wakeup.
		if len(p.results) != 0 {
			p.notification.Add(1)
			select {
			case p.completed <- struct{}{}:
			default:
			}
		}
	})
	return result, ok
}

func (e *nativeEndpoint) stop() <-chan struct{} {
	e.seal()
	return e.done
}

// seal stops admission for good. Queued work resolves as unsubmitted and the
// current request keeps its token until actual return, so this never waits for
// native I/O.
func (e *nativeEndpoint) seal() {
	e.do(func(s *endpointState) { s.sealed = true })
}

// awaitDepositedCloses gives each close already deposited here its write budget before
// the owner seals. Retirement covers a written, expired or dropped close, and a fault
// ends the rest. A participant that deposited none adds no wait. It never joins a native
// call: past budget+endpointTolerance the seal goes ahead, and a close still unresolved
// is lost.
func (e *nativeEndpoint) awaitDepositedCloses() {
	var retired []<-chan struct{}
	var budget time.Duration
	e.do(func(s *endpointState) {
		budget = s.budget
		for _, p := range s.participants {
			if p != nil && p.active && p.closeDeposited {
				retired = append(retired, p.retired)
			}
		}
	})
	if len(retired) == 0 {
		return
	}
	timer := time.NewTimer(budget + endpointTolerance)
	defer timer.Stop()
	for _, r := range retired {
		select {
		case <-r:
		case <-e.fault:
			return
		case <-timer.C:
			return
		}
	}
}

// cancelReadiness withdraws work that was refused and remains producer-owned.
// It never cancels an admitted request or touches a native borrow.
func (e *nativeEndpoint) cancelReadiness(p *endpointParticipant, lane int) {
	e.do(func(s *endpointState) {
		if !s.valid(p) || lane < 0 || lane >= len(p.wait) {
			return
		}
		p.wait[lane].pending = false
		if g := s.grant; g != nil && g.participant == p && g.lane == lane {
			s.releaseGrant()
		}
	})
}

// detach cancels queued work immediately, unless the participant is draining. A
// claimed request, including blocked deadline setup/reset, keeps the participant's
// fence until actual native return.
func (e *nativeEndpoint) detach(p *endpointParticipant) <-chan struct{} {
	e.do(func(s *endpointState) {
		if !s.valid(p) {
			return
		}
		if p.draining {
			p.detaching = true
			s.finishDrain(p)
			return
		}
		s.revoke(p)
	})
	return p.retired
}

// serviceClosers runs after arbitration, so a lease granted in this pass is used
// in this pass.
func (s *endpointState) serviceClosers(now time.Time) {
	for j := 0; j < len(s.closers); {
		p := s.closers[j]
		closing := &p.closings[0]
		if s.faulted || !now.Before(closing.deadline) {
			s.dropClosing(p)
			s.finishDrain(p)
			continue
		}
		if p.retained == 0 {
			if admission, err := s.admit(p, 2, closing.size); err == nil {
				request := closing.request
				request.deadline = closing.deadline
				s.commit(admission, request)
				p.closings = slices.Delete(p.closings, 0, 1)
				if len(p.closings) == 0 {
					s.closers = slices.Delete(s.closers, j, j+1)
				}
				s.chooseGrant(now)
				continue
			}
		}
		j++
	}
}

func (s *endpointState) dropClosing(p *endpointParticipant) {
	if len(p.closings) == 0 {
		return
	}
	for _, closing := range p.closings {
		s.releasePayload(closing.request)
	}
	p.closings = nil
	s.closers = slices.DeleteFunc(s.closers, func(closer *endpointParticipant) bool { return closer == p })
}

func (s *endpointState) finishDrain(p *endpointParticipant) {
	if p.active && p.detaching && len(p.closings) == 0 && p.retained == 0 {
		s.revoke(p)
	}
}

func (s *endpointState) retire(p *endpointParticipant) {
	if !p.active && p.retained == 0 && s.participants[p.slot] == p {
		s.participants[p.slot] = nil
		i := s.participantPosition(p.slot)
		s.occupied = slices.Delete(s.occupied, i, i+1)
		close(p.retired)
		// A connection spared by a read failure settles what this participant left.
		p.notification.Add(1)
		select {
		case p.completed <- struct{}{}:
		default:
		}
	}
}

// withdraw ends everything that waits on the participant's consumer.
func (s *endpointState) withdraw(p *endpointParticipant) {
	clear(p.wait[:])
	if s.grant != nil && s.grant.participant == p {
		s.releaseGrant()
	}
	for _, i := range p.results {
		s.records[i].reset()
		p.retained--
	}
	p.results = p.results[:0]
	for index := range s.records {
		record := &s.records[index]
		if record.used && record.reserved && record.participant == p {
			s.cancelAdmission(endpointAdmission{index: index, serial: record.serial})
		}
	}
}

func (s *endpointState) revoke(p *endpointParticipant) {
	p.active = false
	s.dropClosing(p)
	s.withdraw(p)
	if s.current != nil && s.records[s.current.index].participant == p {
		s.current.canceled.Store(true)
	}
	// Local producers can still own frozen requests, even while a current
	// native borrow prevents retirement. Wake them to settle only local work.
	p.notification.Add(1)
	select {
	case p.completed <- struct{}{}:
	default:
	}
	s.retire(p)
}

func (s *endpointState) publish(index int, result endpointResult) {
	r := &s.records[index]
	result.identity = r.request.identity
	var datagram *endpointDatagram
	for _, member := range r.request.members {
		if member.datagram != nil {
			datagram = member.datagram
		}
	}
	s.releasePayload(r.request)
	s.count--
	s.bytes -= r.bytes
	s.classCount[r.class]--
	s.classBytes[r.class] -= r.bytes
	s.capacityGeneration++
	if localEndpointBackpressure(result.failure()) {
		r.participant.wait[r.lane].notBefore = time.Now().Add(endpointRetry)
	}
	r.clearRequest()
	r.result, r.terminal = result, true
	p := r.participant
	if datagram != nil {
		if datagram.result != nil {
			datagram.result <- result
		}
		s.endpoint.emitDiagnostic(endpointDiagnostic{identity: result.identity, kind: 1})
		r.reset()
		p.retained--
		s.retire(p)
		return
	}
	if !p.active || p.draining {
		r.reset()
		p.retained--
		s.finishDrain(p)
		s.retire(p)
		return
	}
	p.results = append(p.results, index)
	p.notification.Add(1)
	select {
	case p.completed <- struct{}{}:
	default:
	}
	s.endpoint.emitDiagnostic(endpointDiagnostic{identity: result.identity, kind: 1})
}

// advance reconciles immutable returns and time-based state without claiming
// work. Every caller holds the same mutex; only the writer chooses a native turn.
func (s *endpointState) advance(now time.Time) {
	s.reconcileCurrent(now)
	if s.sealed {
		for _, p := range s.participants {
			if p != nil && p.active {
				s.revoke(p)
			}
		}
	}
	for j := 0; j < len(s.queue); {
		i := s.queue[j]
		r := &s.records[i]
		revoked := r.revoked || !r.request.authorized()
		if s.faulted || !r.participant.active || revoked || !now.Before(r.request.deadline) {
			err := os.ErrDeadlineExceeded
			if s.faulted {
				err = errEndpointFault
			}
			if !r.participant.active || revoked {
				err = errEndpointRevoked
			}
			s.queue = append(s.queue[:j], s.queue[j+1:]...)
			s.publish(i, r.request.unsubmitted(err, nil))
			continue
		}
		j++
	}
	if s.sealed && s.current != nil {
		s.current.canceled.Store(true)
	}
	s.chooseGrant(now)
	s.serviceClosers(now)
}

func (s *endpointState) wakeWriter() {
	if !s.terminal && s.current == nil && len(s.queue) != 0 {
		select {
		case s.endpoint.work <- struct{}{}:
		default:
		}
	}
}

func (s *endpointState) nextWake(now time.Time) (next time.Time) {
	if s.current != nil && !s.faulted {
		next = s.current.request.deadline.Add(endpointTolerance)
	}
	if s.grant != nil && !s.grant.expires.IsZero() {
		next = earlierEndpointTime(next, s.grant.expires)
	}
	if s.grant == nil && !s.sealed && !s.faulted {
		for p := range s.occupiedParticipants(0) {
			for _, w := range p.wait {
				if w.pending && w.notBefore.After(now) {
					next = earlierEndpointTime(next, w.notBefore)
				}
			}
		}
	}
	for _, i := range s.queue {
		next = earlierEndpointTime(next, s.records[i].request.deadline)
	}
	for _, p := range s.closers {
		next = earlierEndpointTime(next, p.closings[0].deadline)
	}
	return next
}

// run alone owns timer arming and terminal join. A wake arriving after the
// deadline snapshot must remain pending until the next reconciliation.
func (s *endpointState) run() {
	e := s.endpoint
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		e.mutex.Lock()
		now := time.Now()
		s.advance(now)
		if s.sealed && s.current == nil && len(s.queue) == 0 {
			s.terminal = true
			close(e.halt)
			stopped := e.stopped
			e.unlock()
			<-e.writer
			if e.diagnostics != nil {
				close(e.diagnostics)
			}
			if stopped != nil {
				stopped()
			}
			close(e.done)
			return
		}
		s.wakeWriter()
		next := s.nextWake(now)
		e.unlock()
		var wake <-chan time.Time
		if !next.IsZero() {
			timer.Reset(time.Until(next))
			wake = timer.C
		} else {
			timer.Stop()
		}
		select {
		case <-wake:
		case <-e.progress:
		}
	}
}

// reconcileCurrent is the sole native-timing decision and terminal handoff.
// The fact load is the overdue inspection: once an absent-fact inspection
// faults, subsequent publication cannot reopen the endpoint, even if its
// timestamp predates the cutoff. A visible timely fact wins over control time.
func (s *endpointState) reconcileCurrent(now time.Time) {
	token := s.current
	if token == nil {
		return
	}
	cutoff := token.request.deadline.Add(endpointTolerance)
	completion := token.completion.Load()
	if completion == nil {
		if !now.Before(cutoff) {
			s.fail(endpointFaultOverdue, nil)
		}
		return // still fenced; only the writer can acknowledge native return
	}
	if completion.fault {
		s.fail(endpointFaultNative, completion.result.failure())
	} else if !completion.returnedAt.Before(cutoff) {
		s.fail(endpointFaultLateReturn, completion.result.failure())
	}
	s.publish(token.index, completion.result)
	// A retained fact must not retain the canonical record after its handoff.
	token.request = nil
	s.current = nil
}

func earlierEndpointTime(a, b time.Time) time.Time {
	if a.IsZero() || b.Before(a) {
		return b
	}
	return a
}

func endpointTurn(available [4]bool, controls, cursor *int) endpointClass {
	if available[endpointData] && *controls >= 2 {
		*controls = 0
		return endpointData
	}
	for j := range 3 {
		class := endpointClass(1 + (*cursor+j)%3)
		if available[class] {
			*cursor = int(class) % 3
			*controls = min(2, *controls+1)
			return class
		}
	}
	*controls = 0
	return endpointData
}

// Releasing a turn and publishing the capacity generation share the same owner
// as admission, so a waiter never has to race a separate rearming operation.
func (s *endpointState) releaseGrant() {
	if s.grant == nil {
		return
	}
	select {
	case <-s.grant.participant.ready:
	default:
	}
	s.grant = nil
	s.capacityGeneration++
}

// Reclamation precedes fresh registration and does not arbitrate a new turn.
func (s *endpointState) reclaimGrant(now time.Time) {
	if s.sealed || s.faulted {
		s.releaseGrant()
		return
	}
	// Backpressure can arrive after a readiness grant was issued. Revalidate
	// that same central eligibility rule before any caller can consume it.
	if g := s.grant; g != nil && now.Before(g.participant.wait[g.lane].notBefore) {
		s.releaseGrant()
	}
	if g := s.grant; g != nil && !g.expires.IsZero() && !now.Before(g.expires) {
		// A frozen ordinary head remains demand until explicit withdrawal.
		// Complete-input attempts (sequence zero) retain release-on-refusal behavior.
		g.participant.wait[g.lane].pending = g.participant.wait[g.lane].sequence != 0
		g.participant.wait[g.lane].notBefore = now.Add(endpointRetry)
		s.releaseGrant()
	}
}

// A selected large request accumulates capacity before its lease begins. Other
// arrivals cannot consume that capacity. An unused lease ends its registration;
// a producer that still owns pending work can register again after the floor.
// Exact staged heads retain their immutable demand across that retry floor.
func (s *endpointState) chooseGrant(now time.Time) {
	s.arbitrate(now, nil, 0)
}

// arbitrate wakes the lease's holder, unless that holder is admitting right now.
// Waking it then would leave a signal with nothing to do on its shared channel.
func (s *endpointState) arbitrate(now time.Time, admitting *endpointParticipant, admittingLane int) {
	s.reclaimGrant(now)
	if s.sealed || s.faulted {
		return
	}
	if s.grant == nil {
		free := false
		for i := range s.records {
			if !s.records[i].used {
				free = true
				break
			}
		}
		if !free {
			return
		}
		var candidates [4]*endpointGrant
		var available [4]bool
		for class := endpointData; class <= endpointControl; class++ {
			for p := range s.occupiedParticipants(s.admissionCursor[class]) {
				if !p.active || p.class != endpointData {
					continue
				}
				if p.retained >= 4 {
					continue
				}
				lanes := [2]int{p.admissionPath, 1 - p.admissionPath}
				count := 2
				if class == endpointControl {
					lanes[0], count = 2, 1
				}
				for _, lane := range lanes[:count] {
					w := p.wait[lane]
					if w.pending && !now.Before(w.notBefore) {
						candidates[class] = &endpointGrant{participant: p, lane: lane}
						available[class] = true
						break
					}
				}
				if available[class] {
					break
				}
			}
		}
		class := endpointTurn(available, &s.admissionControls, &s.admissionClass)
		s.grant = candidates[class]
		if s.grant == nil {
			return
		}
		p := s.grant.participant
		s.admissionCursor[class] = (p.slot + 1) % len(s.participants)
		if class == endpointData {
			p.admissionPath = 1 - s.grant.lane
		}
	}
	g := s.grant
	p := g.participant
	class := p.class
	if class == endpointData && g.lane == 2 {
		class = endpointControl
	}
	n := p.wait[g.lane].bytes
	if g.expires.IsZero() && s.hasCapacity(class, n) {
		g.expires = now.Add(endpointLease)
		if p == admitting && g.lane == admittingLane {
			return
		}
		select {
		case p.ready <- s.capacityGeneration:
		default:
		}
		p.notification.Add(1)
		select {
		case p.completed <- struct{}{}:
		default:
		}
	}
}

func (s *endpointState) chooseService() int {
	var available [4]bool
	for _, i := range s.queue {
		available[s.records[i].class] = true
	}
	class := endpointTurn(available, &s.serviceControls, &s.serviceClass)
	best, score := -1, int(^uint(0)>>1)
	for j, i := range s.queue {
		r := &s.records[i]
		if r.class != class {
			continue
		}
		p := r.participant
		rank := ((p.slot - s.serviceCursor[class] + len(s.participants)) % len(s.participants)) * 2
		if class == endpointData && r.lane != p.servicePath {
			rank++
		}
		if rank < score {
			best, score = j, rank
		}
	}
	p := s.records[s.queue[best]].participant
	s.serviceCursor[class] = (p.slot + 1) % len(s.participants)
	if class == endpointData {
		p.servicePath = 1 - s.records[s.queue[best]].lane
	}
	return best
}

func (s *endpointState) fail(cause endpointFaultCause, err error) {
	if !s.faulted {
		s.faulted = true
		s.endpoint.faultRecord = endpointFaultRecord{cause: cause, err: err}
		if s.current != nil {
			s.endpoint.faultRecord.identity = s.current.request.identity
		}
		close(s.endpoint.fault)
		// An idle shared participant has no result to wake its connection with.
		for _, p := range s.participants {
			if p != nil {
				p.notification.Add(1)
				select {
				case p.completed <- struct{}{}:
				default:
				}
			}
		}
		if s.current != nil {
			s.current.canceled.Store(true)
		}
	}
}

// claim takes one fair turn only after reconciling authorization, expiry and
// native facts at the request boundary. No private successor chain is retained.
func (s *endpointState) claim() *endpointToken {
	if s.terminal || s.sealed || s.faulted || s.current != nil || len(s.queue) == 0 {
		return nil
	}
	j := s.chooseService()
	i := s.queue[j]
	s.queue = append(s.queue[:j], s.queue[j+1:]...)
	s.current = newEndpointToken(i, &s.records[i].request)
	return s.current
}

func (e *nativeEndpoint) write(native endpointNativeIO) {
	defer close(e.writer)
	var scratch handshake.PacketProtectionScratch
	var oobScratch [128]byte
	e.mutex.Lock()
	for {
		s := e.state
		s.advance(time.Now())
		token := s.claim()
		if token == nil {
			e.notifyProgress() // timer/terminal owner must observe the parked boundary
			e.unlock()
			select {
			case <-e.halt:
				return
			case <-e.work:
			}
			e.mutex.Lock()
			continue
		}
		e.notifyProgress() // independent watchdog observes this new claim
		e.unlock()
		var result endpointResult
		var writeErr error
		fault := false
		if err := native.SetWriteDeadline(token.request.deadline); err != nil {
			result = token.request.unsubmitted(err, token.fact.result.members)
			writeErr, fault = err, true
		} else {
			result = writeEndpointRequestWithScratch(native, token, &scratch, oobScratch[:0])
			writeErr = result.failure()
			fault = endpointFatalWrite(writeErr)
		}
		if err := native.SetWriteDeadline(time.Time{}); err != nil {
			result.err, fault = err, true
		}
		// This separate fact fences the entire native/deadline borrow. Member
		// timestamps remain their actual sequence returns, including on reset error.
		returnedAt := time.Now()
		yield := runtime.GOOS == "linux" && writeErr == nil && result.err == nil && token.request.shortSoftwareDatagramGroup &&
			returnedAt.Before(token.request.deadline.Add(endpointTolerance))
		// Do not access token payload after Store: control can release it at once.
		token.fact = endpointWriteResult{result: result, fault: fault, returnedAt: returnedAt}
		token.completion.Store(&token.fact)
		if yield {
			// Give the reader a chance to run between groups of scalar sends.
			// The completion owner can proceed while this writer is descheduled.
			e.notifyProgress()
			runtime.Gosched()
		}
		// A failed reconciliation attempt notifies independent progress before
		// waiting on control. Publication is never coupled to lock acquisition.
		if !e.mutex.TryLock() {
			e.notifyProgress()
			e.mutex.Lock()
		}
	}
}

func endpointSubmissionAllowed(token *endpointToken) error {
	if token.canceled.Load() || !token.request.authorized() {
		return errEndpointRevoked
	}
	if !time.Now().Before(token.request.deadline) {
		return os.ErrDeadlineExceeded
	}
	return nil
}

// endpointFatalWrite is concrete evidence that the socket itself is gone. Every
// other write error says nothing about another destination on this endpoint.
func endpointFatalWrite(err error) bool {
	return errors.Is(err, net.ErrClosed) || errors.Is(err, syscall.EBADF) || errors.Is(err, syscall.ENOTSOCK)
}

func localEndpointBackpressure(err error) bool {
	if timeout, ok := errors.AsType[net.Error](err); ok && timeout.Timeout() {
		return true
	}
	return errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.ENOBUFS)
}

func writeEndpointDatagram(native endpointNativeIO, token *endpointToken, member endpointMember, data, oob []byte) (bool, error) {
	if err := endpointSubmissionAllowed(token); err != nil {
		return false, err
	}
	request := *token.request
	first := member.writeState != nil && !member.writeState.wroteFirstPacket
	written, _, err := native.WriteMsgUDPAddrPort(data, oob, request.destination)
	if member.writeState != nil {
		member.writeState.wroteFirstPacket = true
	}
	if first && endpointSendmsgError(err, syscall.EPERM) {
		if denied := endpointSubmissionAllowed(token); denied != nil {
			return false, denied
		}
		written, _, err = native.WriteMsgUDPAddrPort(data, oob, request.destination)
	}
	if err == nil && written != len(data) {
		err = io.ErrShortWrite
	}
	return true, err
}

func endpointSendmsgError(err error, cause error) bool {
	system, ok := errors.AsType[*os.SyscallError](err)
	return ok && system.Syscall == "sendmsg" && errors.Is(system.Err, cause)
}

func writeEndpointRequestWithScratch(native endpointNativeIO, token *endpointToken, scratch *handshake.PacketProtectionScratch, oobScratch []byte) endpointResult {
	request := *token.request
	result := newEndpointResult(token.fact.result.members, len(request.members))
	for i, member := range request.members {
		result.members[i] = writeEndpointMember(native, token, member, scratch, oobScratch)
		if result.members[i].err != nil {
			// The untouched suffix has no native error or native-return time.
			now := monotime.Now()
			for j := i + 1; j < len(request.members); j++ {
				result.members[j] = endpointMemberResult{unsubmitted: request.members[j].datagrams(), returnedAt: now}
			}
			break
		}
	}
	return result
}

func writeEndpointMember(native endpointNativeIO, token *endpointToken, member endpointMember, scratch *handshake.PacketProtectionScratch, oobScratch []byte) (result endpointMemberResult) {
	result.unsubmitted = member.datagrams()
	defer func() { result.returnedAt = monotime.Now() }()
	if member.datagram != nil {
		if err := endpointSubmissionAllowed(token); err != nil {
			result.err = err
			return result
		}
		written, _, err := native.WriteMsgUDPAddrPort(member.datagram.data, member.oob, token.request.destination)
		result.bytes = written
		if err == nil && written == len(member.datagram.data) {
			result.accepted, result.unsubmitted = 1, 0
			return result
		}
		if err == nil {
			err = io.ErrShortWrite
		}
		result.err = err
		if !errors.Is(err, syscall.EMSGSIZE) {
			result.uncertain, result.unsubmitted = 1, 0
		}
		return result
	}
	if member.protection != nil {
		if err := endpointSubmissionAllowed(token); err != nil {
			result.err = err
			return result
		}
		member.protection.protectAll(scratch)
	}
	data := member.buffer.Data
	segment := len(data)
	if member.gsoSize != 0 {
		segment = int(member.gsoSize)
		if member.nativeGSO && member.writeState != nil && !member.writeState.gsoDisabled.Load() {
			// The sole writer reuses this private copy only after the native call
			// and EPERM retry return. Oversize append allocations stay temporary.
			oob := appendUDPSegmentSizeMsg(append(oobScratch[:0], member.oob...), member.gsoSize)
			attempted, err := writeEndpointDatagram(native, token, member, data, oob)
			if endpointSendmsgError(err, syscall.EIO) {
				member.writeState.gsoDisabled.Store(true)
			} else {
				result.err = err
				if err == nil {
					result.accepted, result.unsubmitted = member.datagrams(), 0
				} else if attempted && !errors.Is(err, syscall.EMSGSIZE) {
					result.uncertain, result.unsubmitted = member.datagrams(), 0
				}
				return result
			}
		}
	}
	for len(data) != 0 {
		length := min(segment, len(data))
		attempted, err := writeEndpointDatagram(native, token, member, data[:length], member.oob)
		if err != nil {
			result.err = err
			if attempted && !errors.Is(err, syscall.EMSGSIZE) {
				result.uncertain++
				result.unsubmitted--
			}
			return result
		}
		result.accepted++
		result.unsubmitted--
		data = data[length:]
	}
	return result
}
