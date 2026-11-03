//go:build unix

package quic

import (
	"errors"
	"io"
	"net"
	"net/netip"
	"os"
	"slices"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
)

// This route is deliberately inactive: no Transport or connection constructor
// selects it. E2–E5 migrate producers; R19 owns public activation. In particular,
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

type endpointRequest struct {
	identity    endpointIdentity
	destination netip.AddrPort
	buffers     []*packetBuffer
	datagram    *endpointDatagram
	epoch       *responseEpoch // nil grants no additional revocation owner
	deadline    time.Time      // zero selects admission+B; an earlier deadline is allowed
	oob         []byte
	gsoSize     uint16
	nativeGSO   bool
	writeState  *endpointWriteState
	credit      *sendReservation
}

type endpointResult struct {
	identity                         endpointIdentity
	accepted, uncertain, unsubmitted int
	bytes                            int
	err                              error
	returnedAt                       monotime.Time
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
	return request.epoch.valid() && (request.datagram == nil || request.datagram.epoch.valid())
}

func (request endpointRequest) packetCount() int {
	if request.datagram != nil {
		return 1
	}
	return len(request.buffers)
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
	commands          chan func(*endpointState)
	work              chan *endpointToken
	returned          chan struct{}
	done              chan struct{}
	fault             chan struct{}
	diagnostics       chan endpointDiagnostic
	dropped           atomic.Uint64
	stateless, public *endpointParticipant
}

type endpointParticipant struct {
	endpoint  *nativeEndpoint
	id        uint64
	slot      int
	ready     chan uint64
	completed chan struct{}
	retired   chan struct{}
	class     endpointClass
	// The fields below belong exclusively to endpoint control.
	active                     bool
	retained                   int
	results                    []int
	wait                       [3]endpointWait
	admissionPath, servicePath int
}

type endpointWait struct {
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
	buffers        [endpointBatch]*packetBuffer
	result         endpointResult
}

type endpointToken struct {
	index    int
	request  endpointRequest
	canceled atomic.Bool
	// The writer publishes once, after all native/deadline operations. The
	// immutable fact is authoritative even while its wakeup is still pending.
	completion atomic.Pointer[endpointWriteResult]
}

type endpointWriteResult struct {
	result     endpointResult
	fault      bool
	returnedAt time.Time
}

type endpointState struct {
	endpoint                           *nativeEndpoint
	budget                             time.Duration
	participants                       [endpointParticipants + 2]*endpointParticipant
	records                            [endpointRecords]endpointRecord
	queue                              []int
	current                            *endpointToken
	count, bytes                       int
	sealed, faulted                    bool
	classCount, classBytes             [4]int
	grant                              *endpointGrant
	admissionCursor, serviceCursor     [4]int
	admissionControls, serviceControls int
	admissionClass, serviceClass       int
	capacityGeneration                 uint64
	nextAdmission                      uint64
}

func newNativeEndpoint(socket *net.UDPConn, budget time.Duration, sink func(endpointDiagnostic)) (*nativeEndpoint, error) {
	if socket == nil || socket.RemoteAddr() != nil || budget < time.Millisecond || budget > time.Second {
		return nil, errEndpointRequest
	}
	return startNativeEndpoint(socket, budget, sink), nil
}

func startNativeEndpoint(native endpointNativeIO, budget time.Duration, sink func(endpointDiagnostic)) *nativeEndpoint {
	e := &nativeEndpoint{
		generation: nextNativeEndpoint.Add(1),
		commands:   make(chan func(*endpointState)), work: make(chan *endpointToken, 1),
		returned: make(chan struct{}, 1), done: make(chan struct{}), fault: make(chan struct{}),
		diagnostics: make(chan endpointDiagnostic, 128),
	}
	go func() {
		for d := range e.diagnostics {
			if sink != nil {
				sink(d)
			}
		}
	}()
	s := &endpointState{endpoint: e, budget: budget, queue: make([]int, 0, endpointRequests)}
	for i, class := range []endpointClass{endpointStateless, endpointPublic} {
		p := newEndpointParticipant(e, 0, endpointParticipants+i, class)
		s.participants[p.slot] = p
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

func newEndpointParticipant(e *nativeEndpoint, id uint64, slot int, class endpointClass) *endpointParticipant {
	return &endpointParticipant{
		endpoint: e, id: id, slot: slot, class: class, active: true,
		ready: make(chan uint64, 1), completed: make(chan struct{}, 1), retired: make(chan struct{}), results: make([]int, 0, 8),
	}
}

// do waits only for serialized control, never for native I/O or a consumer.
func (e *nativeEndpoint) do(f func(*endpointState)) {
	ack := make(chan struct{})
	select {
	case e.commands <- func(s *endpointState) { f(s); close(ack) }:
		<-ack
	case <-e.done:
	}
}

func (e *nativeEndpoint) attach(id uint64) (*endpointParticipant, error) {
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
				s.participants[i], err = p, nil
				return
			}
		}
	})
	return p, err
}

func (s *endpointState) valid(p *endpointParticipant) bool {
	return p != nil && p.endpoint == s.endpoint && p.slot >= 0 && p.slot < len(s.participants) && s.participants[p.slot] == p && p.active
}

// offer transfers buffers only on success. On refusal the caller still owns
// both the buffers and their backing array; neither is retained by the endpoint.
func (e *nativeEndpoint) offer(p *endpointParticipant, lane int, request endpointRequest) error {
	err := net.ErrClosed
	e.do(func(s *endpointState) {
		if s.sealed || s.faulted || !s.valid(p) {
			return
		}
		if !request.authorized() {
			return
		}
		err = errEndpointRequest
		if lane < 0 || lane > 2 || request.identity.connection != p.id || !request.destination.IsValid() || request.packetCount() == 0 || request.packetCount() > endpointBatch {
			return
		}
		if request.datagram != nil && (len(request.buffers) != 0 || p.class < endpointStateless) {
			return
		}
		if p.class != endpointData && lane != 0 {
			return
		}
		n := 0
		if request.datagram != nil {
			n = cap(request.datagram.data)
		}
		for i, b := range request.buffers {
			if b == nil || b.refCount != 1 || len(b.Data) == 0 {
				return
			}
			if slices.Contains(request.buffers[:i], b) {
				return
			}
			n += cap(b.Data)
		}
		var admission endpointAdmission
		admission, err = s.admit(p, lane, n)
		if err == nil {
			s.commit(admission, request)
		}
	})
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
	if !wait.pending || wait.bytes != size {
		if s.grant != nil && s.grant.participant == p && s.grant.lane == lane {
			s.releaseGrant()
		}
		wait.pending, wait.bytes = true, size
	}
	s.chooseGrant(now)
	if s.grant == nil || s.grant.participant != p || s.grant.lane != lane || s.grant.expires.IsZero() {
		return endpointAdmission{}, errEndpointCapacity
	}
	for index := range s.records {
		if s.records[index].used {
			continue
		}
		s.nextAdmission++
		admission := endpointAdmission{index: index, serial: s.nextAdmission, deadline: now.Add(s.budget)}
		s.records[index] = endpointRecord{used: true, reserved: true, serial: admission.serial, participant: p, lane: lane, class: class, bytes: size}
		s.count++
		s.bytes += size
		s.classCount[class]++
		s.classBytes[class] += size
		p.retained++
		wait.pending = false
		s.grant = nil
		select {
		case <-p.ready:
		default:
		}
		if len(p.results) == 0 {
			select {
			case <-p.completed:
			default:
			}
		}
		return admission, nil
	}
	return endpointAdmission{}, errEndpointCapacity
}

func (s *endpointState) commit(admission endpointAdmission, request endpointRequest) {
	record := &s.records[admission.index]
	var size int
	if request.datagram != nil {
		size = cap(request.datagram.data)
	}
	for _, buffer := range request.buffers {
		size += cap(buffer.Data)
	}
	released := record.bytes - size
	s.bytes -= released
	s.classBytes[record.class] -= released
	record.bytes = size
	if released != 0 {
		s.capacityGeneration++
	}
	record.reserved = false
	record.request = request
	record.request.identity.endpoint = s.endpoint.generation
	record.request.oob = slices.Clone(request.oob)
	copy(record.buffers[:], request.buffers)
	record.request.buffers = record.buffers[:len(request.buffers)]
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
	*record = endpointRecord{}
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
		s.records[i] = endpointRecord{}
		p.retained--
		s.capacityGeneration++
		select {
		case <-p.completed:
		default:
		}
		if len(p.results) != 0 {
			p.completed <- struct{}{}
		}
	})
	return result, ok
}

func (e *nativeEndpoint) stop() <-chan struct{} {
	e.do(func(s *endpointState) { s.sealed = true })
	return e.done
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

// detach cancels queued work immediately. A claimed request, including blocked
// deadline setup/reset, keeps the participant's fence until actual native return.
func (e *nativeEndpoint) detach(p *endpointParticipant) <-chan struct{} {
	e.do(func(s *endpointState) {
		if s.valid(p) {
			s.revoke(p)
		}
	})
	return p.retired
}

func (s *endpointState) retire(p *endpointParticipant) {
	if !p.active && p.retained == 0 && s.participants[p.slot] == p {
		s.participants[p.slot] = nil
		close(p.retired)
	}
}

func (s *endpointState) revoke(p *endpointParticipant) {
	p.active = false
	clear(p.wait[:])
	if s.grant != nil && s.grant.participant == p {
		s.releaseGrant()
	}
	for _, i := range p.results {
		s.records[i] = endpointRecord{}
		p.retained--
	}
	p.results = p.results[:0]
	for index := range s.records {
		record := &s.records[index]
		if record.used && record.reserved && record.participant == p {
			s.cancelAdmission(endpointAdmission{index: index, serial: record.serial})
		}
	}
	if s.current != nil && s.records[s.current.index].participant == p {
		s.current.canceled.Store(true)
	}
	s.retire(p)
}

func (s *endpointState) publish(index int, result endpointResult) {
	r := &s.records[index]
	result.identity = r.request.identity
	datagram := r.request.datagram
	for _, b := range r.request.buffers {
		b.Release()
	}
	r.request.credit.complete()
	if result.returnedAt.IsZero() {
		result.returnedAt = monotime.Now()
	}
	s.count--
	s.bytes -= r.bytes
	s.classCount[r.class]--
	s.classBytes[r.class] -= r.bytes
	s.capacityGeneration++
	if localEndpointBackpressure(result.err) {
		r.participant.wait[r.lane].notBefore = time.Now().Add(endpointRetry)
	}
	r.request = endpointRequest{}
	clear(r.buffers[:])
	r.result, r.terminal = result, true
	p := r.participant
	if datagram != nil {
		if datagram.result != nil {
			datagram.result <- result
		}
		select {
		case s.endpoint.diagnostics <- endpointDiagnostic{identity: result.identity, kind: 1}:
		default:
			s.endpoint.dropped.Add(1)
		}
		*r = endpointRecord{}
		p.retained--
		s.retire(p)
		return
	}
	if !p.active {
		*r = endpointRecord{}
		p.retained--
		s.retire(p)
		return
	}
	p.results = append(p.results, index)
	select {
	case p.completed <- struct{}{}:
	default:
	}
	select {
	case s.endpoint.diagnostics <- endpointDiagnostic{identity: result.identity, kind: 1}:
	default:
		s.endpoint.dropped.Add(1)
	}
}

func (s *endpointState) run() {
	e := s.endpoint
	defer close(e.done)
	defer close(e.diagnostics)
	defer close(e.work)
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		now := time.Now()
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
				s.publish(i, endpointResult{unsubmitted: r.request.datagrams(), err: err})
				continue
			}
			j++
		}
		if s.sealed && s.current != nil {
			s.current.canceled.Store(true)
		}
		if s.sealed && s.current == nil && len(s.queue) == 0 {
			return
		}
		s.chooseGrant(now)
		if s.current == nil && len(s.queue) != 0 {
			j := s.chooseService()
			i := s.queue[j]
			s.queue = append(s.queue[:j], s.queue[j+1:]...)
			s.current = &endpointToken{index: i, request: s.records[i].request}
			e.work <- s.current
		}
		var wake <-chan time.Time
		var next time.Time
		if s.current != nil && !s.faulted {
			next = s.current.request.deadline.Add(endpointTolerance)
		}
		if s.grant != nil && !s.grant.expires.IsZero() {
			next = earlierEndpointTime(next, s.grant.expires)
		}
		if s.grant == nil && !s.sealed && !s.faulted {
			for _, p := range s.participants {
				if p != nil {
					for _, w := range p.wait {
						if w.pending && w.notBefore.After(now) {
							next = earlierEndpointTime(next, w.notBefore)
						}
					}
				}
			}
		}
		for _, i := range s.queue {
			deadline := s.records[i].request.deadline
			if next.IsZero() || deadline.Before(next) {
				next = deadline
			}
		}
		if !next.IsZero() {
			timer.Reset(time.Until(next))
			wake = timer.C
		}
		select {
		case <-wake:
		case command := <-e.commands:
			command(s)
		case <-e.returned:
			// Coalesced notification only. The next reconciliation reads the
			// current token's fact, never a result carried by an old wakeup.
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
			s.fail()
		}
		return // still fenced; only the writer can acknowledge native return
	}
	if completion.fault || !completion.returnedAt.Before(cutoff) {
		s.fail()
	}
	s.publish(token.index, completion.result)
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
		g.participant.wait[g.lane].pending = false
		g.participant.wait[g.lane].notBefore = now.Add(endpointRetry)
		s.releaseGrant()
	}
}

// A selected large request accumulates capacity before its lease begins. Other
// arrivals cannot consume that capacity. An unused lease ends its registration;
// a producer that still owns pending work can register again after the floor.
func (s *endpointState) chooseGrant(now time.Time) {
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
		for class := endpointData; class <= endpointPublic; class++ {
			for offset := range len(s.participants) {
				p := s.participants[(s.admissionCursor[class]+offset)%len(s.participants)]
				if p == nil || !p.active || (p.class != endpointData && p.class != class) || (p.class == endpointData && class > endpointControl) {
					continue
				}
				limit := 4
				if p.class != endpointData {
					limit = 8
				}
				if p.retained >= limit {
					continue
				}
				lanes := [2]int{p.admissionPath, 1 - p.admissionPath}
				count := 2
				if class == endpointControl {
					lanes[0], count = 2, 1
				} else if class > endpointControl {
					lanes[0], count = 0, 1
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
	if g.expires.IsZero() && s.count < endpointRequests && n <= endpointBytes-s.bytes && s.classCount[class] < endpointClassCounts[class] && n <= endpointClassBytes[class]-s.classBytes[class] {
		g.expires = now.Add(endpointLease)
		select {
		case p.ready <- s.capacityGeneration:
		default:
		}
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

func (s *endpointState) fail() {
	if !s.faulted {
		s.faulted = true
		close(s.endpoint.fault)
		if s.current != nil {
			s.current.canceled.Store(true)
		}
	}
}

func (e *nativeEndpoint) write(native endpointNativeIO) {
	for token := range e.work {
		r := token.request
		result := endpointResult{unsubmitted: r.datagrams()}
		fault := false
		if err := native.SetWriteDeadline(r.deadline); err != nil {
			result.err, fault = err, true
		} else {
			result = writeEndpointRequest(native, token)
			fault = errors.Is(result.err, net.ErrClosed) || errors.Is(result.err, syscall.EBADF) || errors.Is(result.err, syscall.ENOTSOCK)
		}
		if err := native.SetWriteDeadline(time.Time{}); err != nil {
			result.err, fault = errors.Join(result.err, err), true
		}
		// Publication ends this token's native borrow. Do not access payload
		// after Store: control may immediately release/reuse its storage.
		result.returnedAt = monotime.Now()
		token.completion.Store(&endpointWriteResult{result: result, fault: fault, returnedAt: result.returnedAt.ToTime()})
		select {
		case e.returned <- struct{}{}:
		default:
		}
	}
}

func (r endpointRequest) datagrams() int {
	if r.datagram != nil {
		return 1
	}
	if r.gsoSize != 0 && len(r.buffers) == 1 {
		return (len(r.buffers[0].Data) + int(r.gsoSize) - 1) / int(r.gsoSize)
	}
	return len(r.buffers)
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

func localEndpointBackpressure(err error) bool {
	if timeout, ok := errors.AsType[net.Error](err); ok && timeout.Timeout() {
		return true
	}
	return errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.ENOBUFS)
}

func writeEndpointDatagram(native endpointNativeIO, token *endpointToken, data, oob []byte) (bool, error) {
	if err := endpointSubmissionAllowed(token); err != nil {
		return false, err
	}
	request := token.request
	first := request.writeState != nil && !request.writeState.wroteFirstPacket
	written, _, err := native.WriteMsgUDPAddrPort(data, oob, request.destination)
	if request.writeState != nil {
		request.writeState.wroteFirstPacket = true
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

func writeEndpointRequest(native endpointNativeIO, token *endpointToken) endpointResult {
	request := token.request
	result := endpointResult{unsubmitted: request.datagrams()}
	if request.datagram != nil {
		if err := endpointSubmissionAllowed(token); err != nil {
			result.err = err
			return result
		}
		written, _, err := native.WriteMsgUDPAddrPort(request.datagram.data, request.datagram.oob, request.destination)
		result.bytes = written
		if err == nil && written == len(request.datagram.data) {
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
	for _, buffer := range request.buffers {
		data := buffer.Data
		segment := len(data)
		if request.gsoSize != 0 {
			segment = int(request.gsoSize)
			if request.nativeGSO && request.writeState != nil && !request.writeState.gsoDisabled.Load() {
				oob := appendUDPSegmentSizeMsg(slices.Clone(request.oob), request.gsoSize)
				attempted, err := writeEndpointDatagram(native, token, data, oob)
				if endpointSendmsgError(err, syscall.EIO) {
					request.writeState.gsoDisabled.Store(true)
				} else {
					units := request.datagrams()
					result.err = err
					if err == nil {
						result.accepted, result.unsubmitted = units, 0
					} else if attempted && !errors.Is(err, syscall.EMSGSIZE) {
						result.uncertain, result.unsubmitted = units, 0
					}
					return result
				}
			}
		}
		for len(data) != 0 {
			length := min(segment, len(data))
			attempted, err := writeEndpointDatagram(native, token, data[:length], request.oob)
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
	}
	return result
}
