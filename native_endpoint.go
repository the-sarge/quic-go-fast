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
	deadline    time.Time // zero selects admission+B; an earlier deadline is allowed
}

type endpointResult struct {
	identity                         endpointIdentity
	accepted, uncertain, unsubmitted int
	err                              error
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
	returned          chan endpointWriteResult
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
}

type endpointWriteResult struct {
	token  *endpointToken
	result endpointResult
	fault  bool
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
		returned: make(chan endpointWriteResult, 1), done: make(chan struct{}), fault: make(chan struct{}),
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
	err := errEndpointCapacity
	e.do(func(s *endpointState) {
		if s.sealed || s.faulted {
			err = net.ErrClosed
			return
		}
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
		err = errEndpointRequest
		if lane < 0 || lane > 2 || request.identity.connection != p.id || !request.destination.IsValid() || len(request.buffers) == 0 || len(request.buffers) > endpointBatch {
			return
		}
		class := p.class
		if class == endpointData && lane == 2 {
			class = endpointControl
		}
		if p.class != endpointData && lane != 0 {
			return
		}
		n := 0
		for i, b := range request.buffers {
			if b == nil || b.refCount != 1 || len(b.Data) == 0 {
				return
			}
			if slices.Contains(request.buffers[:i], b) {
				return
			}
			n += cap(b.Data)
		}
		if n > 1<<20 || (class != endpointData && n > 64<<10) {
			return
		}
		err = errEndpointCapacity
		now := time.Now()
		w := &p.wait[lane]
		if !w.pending || w.bytes != n {
			if s.grant != nil && s.grant.participant == p && s.grant.lane == lane {
				s.grant = nil
			}
			w.pending, w.bytes = true, n
		}
		s.chooseGrant(now)
		if s.grant == nil || s.grant.participant != p || s.grant.lane != lane || s.grant.expires.IsZero() {
			return
		}
		for i := range s.records {
			r := &s.records[i]
			if r.used {
				continue
			}
			*r = endpointRecord{used: true, participant: p, lane: lane, class: class, bytes: n, request: request}
			r.request.identity.endpoint = e.generation
			copy(r.buffers[:], request.buffers)
			r.request.buffers = r.buffers[:len(request.buffers)]
			limit := time.Now().Add(s.budget)
			if request.deadline.IsZero() || request.deadline.After(limit) {
				r.request.deadline = limit
			}
			s.queue = append(s.queue, i)
			s.count++
			s.bytes += n
			s.classCount[class]++
			s.classBytes[class] += n
			p.retained++
			w.pending = false
			s.grant = nil
			select {
			case <-p.ready:
			default:
			}
			err = nil
			return
		}
	})
	return err
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
		s.grant = nil
	}
	for _, i := range p.results {
		s.records[i] = endpointRecord{}
		p.retained--
	}
	p.results = p.results[:0]
	if s.current != nil && s.records[s.current.index].participant == p {
		s.current.canceled.Store(true)
	}
	s.retire(p)
}

func (s *endpointState) publish(index int, result endpointResult) {
	r := &s.records[index]
	result.identity = r.request.identity
	for _, b := range r.request.buffers {
		b.Release()
	}
	s.count--
	s.bytes -= r.bytes
	s.classCount[r.class]--
	s.classBytes[r.class] -= r.bytes
	s.capacityGeneration++
	if errors.Is(result.err, os.ErrDeadlineExceeded) || errors.Is(result.err, syscall.EAGAIN) || errors.Is(result.err, syscall.EWOULDBLOCK) || errors.Is(result.err, syscall.ENOBUFS) {
		r.participant.wait[r.lane].notBefore = time.Now().Add(endpointRetry)
	}
	r.request = endpointRequest{}
	clear(r.buffers[:])
	r.result, r.terminal = result, true
	p := r.participant
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
		if s.current != nil && !now.Before(s.current.request.deadline.Add(endpointTolerance)) {
			s.fail()
		}
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
			if s.faulted || !r.participant.active || !now.Before(r.request.deadline) {
				err := os.ErrDeadlineExceeded
				if s.faulted {
					err = errEndpointFault
				}
				if !r.participant.active {
					err = errEndpointRevoked
				}
				s.queue = append(s.queue[:j], s.queue[j+1:]...)
				s.publish(i, endpointResult{unsubmitted: len(r.request.buffers), err: err})
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
		case result := <-e.returned:
			if result.fault {
				s.fail()
			}
			s.publish(result.token.index, result.result)
			s.current = nil
		}
	}
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

// A selected large request accumulates capacity before its lease begins. Other
// arrivals cannot consume that capacity. A paused grantee loses its turn after
// the finite lease and cannot immediately reacquire it in a tight retry loop.
func (s *endpointState) chooseGrant(now time.Time) {
	if s.sealed || s.faulted {
		s.grant = nil
		return
	}
	// Backpressure can arrive after a readiness grant was issued. Revalidate
	// that same central eligibility rule before any caller can consume it.
	if g := s.grant; g != nil && now.Before(g.participant.wait[g.lane].notBefore) {
		select {
		case <-g.participant.ready:
		default:
		}
		s.grant = nil
	}
	if g := s.grant; g != nil && !g.expires.IsZero() && !now.Before(g.expires) {
		g.participant.wait[g.lane].notBefore = now.Add(endpointRetry)
		select {
		case <-g.participant.ready:
		default:
		}
		s.grant = nil
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
		result := endpointResult{unsubmitted: len(r.buffers)}
		fault := false
		if err := native.SetWriteDeadline(r.deadline); err != nil {
			result.err, fault = err, true
		} else {
			for _, b := range r.buffers {
				if token.canceled.Load() {
					result.err = errEndpointRevoked
					break
				}
				if !time.Now().Before(r.deadline) {
					result.err = os.ErrDeadlineExceeded
					break
				}
				n, _, err := native.WriteMsgUDPAddrPort(b.Data, nil, r.destination)
				if err == nil && n == len(b.Data) {
					result.accepted++
					result.unsubmitted--
					continue
				}
				if err == nil {
					err = io.ErrShortWrite
				}
				result.err = err
				if errors.Is(err, net.ErrClosed) || errors.Is(err, syscall.EBADF) || errors.Is(err, syscall.ENOTSOCK) {
					fault = true
				}
				// A timeout (including a zero byte count) cannot establish zero
				// submission. Only a known atomic datagram rejection can do so.
				if !errors.Is(err, syscall.EMSGSIZE) {
					result.uncertain++
					result.unsubmitted--
				}
				break
			}
		}
		if err := native.SetWriteDeadline(time.Time{}); err != nil {
			result.err, fault = errors.Join(result.err, err), true
		}
		e.returned <- endpointWriteResult{token: token, result: result, fault: fault}
	}
}
