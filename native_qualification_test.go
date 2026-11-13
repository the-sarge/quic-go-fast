//go:build nativequal && linux && amd64

package quic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/netip"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/stretchr/testify/require"
)

// This is E6's disposable native fixture: interop/multipath/native_qualification.py
// runs one class per process, under strace for the injected classes. A class never
// skips. Its timing is judged by the driver, because only uninstrumented observations
// are compared to the tolerance.
const nativeQualBudget = 100 * time.Millisecond

type nativeQualEvidence struct {
	Class     string             `json:"class"`
	Go        string             `json:"go"`
	Traversal bool               `json:"traversal"`
	TimingMS  map[string]float64 `json:"timing_ms"`
	Facts     map[string]any     `json:"facts"`
}

func nativeQualMS(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

// nativeQualRun gives a class its evidence record, written only if the class passes.
func nativeQualRun(t *testing.T, class string, f func(*testing.T, *nativeQualEvidence)) {
	t.Run(class, func(t *testing.T) {
		path := os.Getenv("NATIVE_QUAL_EVIDENCE")
		require.NotEmpty(t, path, "NATIVE_QUAL_EVIDENCE must name the evidence file")
		evidence := &nativeQualEvidence{Class: class, Go: runtime.Version(), TimingMS: map[string]float64{}, Facts: map[string]any{}}
		f(t, evidence)
		if t.Failed() {
			return
		}
		data, err := json.MarshalIndent(evidence, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, data, 0o644))
	})
}

var nativeQualStacks = struct {
	sync.Mutex
	buffer []byte
}{buffer: make([]byte, 1<<20)}

// nativeQualWriterWaiting reports whether a goroutine stack shows the endpoint's
// writer inside the real Go poller's write wait.
func nativeQualWriterWaiting() bool {
	nativeQualStacks.Lock()
	defer nativeQualStacks.Unlock()
	stacks := string(nativeQualStacks.buffer[:runtime.Stack(nativeQualStacks.buffer, true)])
	for _, goroutine := range strings.Split(stacks, "\n\n") {
		if strings.Contains(goroutine, "internal/poll.(*pollDesc).waitWrite") && strings.Contains(goroutine, "quic-go.writeEndpointRequest") {
			return true
		}
	}
	return false
}

func nativeQualSockets(t *testing.T) (receiver, socket *net.UDPConn, destination netip.AddrPort) {
	t.Helper()
	receiver, socket = newUDPConnLocalhost(t), newUDPConnLocalhost(t)
	return receiver, socket, receiver.LocalAddr().(*net.UDPAddr).AddrPort()
}

func nativeQualRequest(id, registration uint64, destination netip.AddrPort, deadline time.Time, data string) endpointRequest {
	return endpointRequest{
		identity:    endpointIdentity{connection: id, registration: registration},
		destination: destination,
		members:     []endpointMember{{buffer: endpointTestBuffer(data)}},
		deadline:    deadline,
	}
}

const (
	// R21: lane 0 offers marked software groups, so request-contention reaches the
	// writer yield while lanes 1 and 2 keep the scalar data and control paths.
	nativeQualMarkedLane = 0
	nativeQualSegments   = 4
	// The shutdown activity's participant ID can't collide with a connection's.
	nativeQualActivityID       = 1 << 62
	nativeQualActivityRequests = 256
	nativeQualActivityMembers  = 32
)

// nativeQualMarkedKey is the producer's identity for one offered request.
type nativeQualMarkedKey struct{ connection, path, registration uint64 }

func nativeQualKey(identity endpointIdentity) nativeQualMarkedKey {
	return nativeQualMarkedKey{identity.connection, identity.path, identity.registration}
}

type nativeQualCapture struct {
	key      nativeQualMarkedKey
	deadline time.Time
}

// nativeQualMarked witnesses the writer yield without a production observer. The
// yield predicate is Linux, no member or deadline-reset error, a marked group, and
// a whole-borrow return before the cutoff. A witness pairs a successful marked
// result its producer took with the immutable completion fact the writer published
// for that same request, sampled under the endpoint lock. Offered traffic alone
// never counts, and no payload is read after publication.
type nativeQualMarked struct {
	segment           string
	members, segments int
	offered           atomic.Int64

	mutex     sync.Mutex
	captured  map[*endpointToken]nativeQualCapture
	succeeded map[nativeQualMarkedKey]struct{}
}

func newNativeQualMarked(segment string, members, segments int) *nativeQualMarked {
	return &nativeQualMarked{
		segment: segment, members: members, segments: segments,
		captured: make(map[*endpointToken]nativeQualCapture), succeeded: make(map[nativeQualMarkedKey]struct{}),
	}
}

// request builds the shape the software-group constructor marks: segmented members
// sent as scalar datagrams, with no native GSO.
func (m *nativeQualMarked) request(id, registration uint64, destination netip.AddrPort) endpointRequest {
	request := endpointRequest{
		identity:                   endpointIdentity{connection: id, registration: registration},
		destination:                destination,
		shortSoftwareDatagramGroup: true,
	}
	for range m.members {
		request.members = append(request.members, endpointMember{buffer: endpointTestBuffer(strings.Repeat(m.segment, m.segments)), gsoSize: uint16(len(m.segment))})
	}
	return request
}

func nativeQualRelease(request endpointRequest) {
	for _, member := range request.members {
		member.buffer.Release()
	}
}

// sample records the writer's current marked request. It runs under the endpoint
// lock, which owns the canonical request until reconciliation. fresh reports the
// first sighting of this token.
func (m *nativeQualMarked) sample(s *endpointState) (token *endpointToken, fresh bool) {
	token = s.current
	if token == nil || !token.request.shortSoftwareDatagramGroup {
		return nil, false
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if _, seen := m.captured[token]; seen {
		return token, false
	}
	m.captured[token] = nativeQualCapture{key: nativeQualKey(token.request.identity), deadline: token.request.deadline}
	return token, true
}

// took records a marked result its producer consumed, if every datagram was accepted.
func (m *nativeQualMarked) took(result endpointResult) {
	if result.failure() != nil || len(result.members) != m.members {
		return
	}
	for _, member := range result.members {
		if member.accepted != m.segments || member.uncertain != 0 || member.unsubmitted != 0 {
			return
		}
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.succeeded[nativeQualKey(result.identity)] = struct{}{}
}

func (m *nativeQualMarked) consume(e *nativeEndpoint, p *endpointParticipant) {
	for {
		result, ok := e.take(p)
		if !ok {
			return
		}
		m.took(result)
	}
}

// witnessed counts sampled marked requests whose published fact satisfies the
// yield predicate and whose producer took the same request's success. margin is
// the smallest distance of a witnessed return before its cutoff.
func (m *nativeQualMarked) witnessed() (count int, margin time.Duration) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	for token, capture := range m.captured {
		fact := token.completion.Load()
		if fact == nil || fact.fault || fact.result.failure() != nil {
			continue
		}
		cutoff := capture.deadline.Add(endpointTolerance)
		if !fact.returnedAt.Before(cutoff) {
			continue
		}
		if _, ok := m.succeeded[capture.key]; !ok {
			continue
		}
		if count == 0 || cutoff.Sub(fact.returnedAt) < margin {
			margin = cutoff.Sub(fact.returnedAt)
		}
		count++
	}
	return count, margin
}

// record publishes the geometry and witness counts as facts, never as timing.
func (m *nativeQualMarked) record(evidence *nativeQualEvidence, prefix string) int {
	witnessed, margin := m.witnessed()
	m.mutex.Lock()
	sampled, succeeded := len(m.captured), len(m.succeeded)
	m.mutex.Unlock()
	for key, value := range map[string]any{
		"marked_group_members":            m.members,
		"marked_group_datagrams":          m.members * m.segments,
		"marked_segment_bytes":            len(m.segment),
		"marked_offered":                  m.offered.Load(),
		"marked_succeeded":                succeeded,
		"marked_sampled":                  sampled,
		"marked_witnessed":                witnessed,
		"marked_witness_cutoff_margin_ms": nativeQualMS(margin),
	} {
		evidence.Facts[prefix+key] = value
	}
	return witnessed
}

// produce offers at most limit marked requests and takes their results. Sealing
// ends the activity: offers are refused and the revoked participant retires.
func (m *nativeQualMarked) produce(e *nativeEndpoint, p *endpointParticipant, destination netip.AddrPort, limit int) error {
	var registration uint64
	var request endpointRequest
	pending := false
	for {
		m.consume(e, p)
		if !pending && registration < uint64(limit) {
			registration++
			request, pending = m.request(p.id, registration, destination), true
		}
		if pending {
			err := e.offer(p, 0, request)
			if err == nil {
				m.offered.Add(1)
				pending = false
				continue
			}
			if !errors.Is(err, errEndpointCapacity) {
				nativeQualRelease(request)
				if errors.Is(err, net.ErrClosed) {
					return nil
				}
				return err
			}
		}
		select {
		case <-p.retired:
			return nil
		case <-p.ready:
		case <-p.completed:
		case <-time.After(endpointRetry):
		}
	}
}

// awaitInFlight returns a marked request still inside its native borrow, and the
// time close begins. The request is first sighted in a sample that began after a
// witness was already established. Its completion is checked again just before
// closing is taken. A return that lands between the writer's timestamp and that
// check, or between the check and closing, is still possible; the caller's
// assertion on the published return fails closed there rather than passing.
func (m *nativeQualMarked) awaitInFlight(t *testing.T, e *nativeEndpoint) (*endpointToken, time.Time) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		armed, _ := m.witnessed()
		var token *endpointToken
		e.do(func(s *endpointState) {
			if current, fresh := m.sample(s); armed > 0 && fresh && current.completion.Load() == nil {
				token = current
			}
		})
		if token != nil && token.completion.Load() == nil {
			return token, time.Now()
		}
		require.True(t, time.Now().Before(deadline), "no fresh marked request reached the writer after a witnessed success")
		time.Sleep(50 * time.Microsecond)
	}
}

// nativeQualOffer retries a capacity refusal, as a producer does: local backpressure
// holds its lane behind the retry floor.
func nativeQualOffer(t *testing.T, e *nativeEndpoint, p *endpointParticipant, request endpointRequest) {
	t.Helper()
	for {
		err := e.offer(p, 0, request)
		if !errors.Is(err, errEndpointCapacity) {
			require.NoError(t, err)
			return
		}
		time.Sleep(endpointRetry)
	}
}

// nativeQualTake waits for one published result and when it became visible.
func nativeQualTake(t *testing.T, e *nativeEndpoint, p *endpointParticipant) (endpointResult, time.Time) {
	t.Helper()
	for {
		if result, ok := e.take(p); ok {
			return result, time.Now()
		}
		select {
		case <-p.completed:
		case <-time.After(5 * time.Second):
			require.FailNow(t, "no result was published")
		}
	}
}

func nativeQualRead(t *testing.T, receiver *net.UDPConn) string {
	t.Helper()
	require.NoError(t, receiver.SetReadDeadline(time.Now().Add(5*time.Second)))
	var buffer [2048]byte
	n, _, err := receiver.ReadFromUDP(buffer[:])
	require.NoError(t, err)
	return string(buffer[:n])
}

// nativeQualStall offers a request the injected EAGAIN holds in the poller, and
// waits for the writer to be observed there. Returning false is a failed claim.
func nativeQualStall(t *testing.T, e *nativeEndpoint, p *endpointParticipant, request endpointRequest) bool {
	t.Helper()
	nativeQualOffer(t, e, p, request)
	for !time.Now().After(request.deadline) {
		if nativeQualWriterWaiting() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return false
}

// nativeQualThreadPinned forwards to the exact socket after binding the writer
// goroutine to its thread, because strace counts when= per tracee. It returns a
// simulated EIO for a GSO attempt, the one input this kernel gave no real trigger for.
type nativeQualThreadPinned struct {
	socket       *net.UDPConn
	pin          sync.Once
	simulatedEIO atomic.Int32
}

func (n *nativeQualThreadPinned) SetWriteDeadline(deadline time.Time) error {
	n.pin.Do(runtime.LockOSThread)
	return n.socket.SetWriteDeadline(deadline)
}

func (n *nativeQualThreadPinned) WriteMsgUDPAddrPort(b, oob []byte, destination netip.AddrPort) (int, int, error) {
	if len(oob) != 0 {
		n.simulatedEIO.Add(1)
		return 0, 0, &os.SyscallError{Syscall: "sendmsg", Err: syscall.EIO}
	}
	return n.socket.WriteMsgUDPAddrPort(b, oob, destination)
}

func nativeQualSNMP(t *testing.T, field string) int64 {
	t.Helper()
	data, err := os.ReadFile("/proc/net/snmp")
	require.NoError(t, err)
	var names []string
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "Udp:" {
			continue
		}
		if names == nil {
			names = fields
			continue
		}
		for i, name := range names {
			if name == field {
				value, err := strconv.ParseInt(fields[i], 10, 64)
				require.NoError(t, err)
				return value
			}
		}
	}
	require.FailNow(t, "missing Udp counter", field)
	return 0
}

// nativeQualBlast tallies one phase: per-lane accepted counts, the latest native
// return past its deadline, and the slowest publication after a native return.
// Its marked lane offers single-member software groups to the same writer.
type nativeQualBlast struct {
	marked      *nativeQualMarked
	accepted    [3]atomic.Int64
	expired     atomic.Int64
	uncertain   atomic.Int64
	refusals    atomic.Int64
	overrun     atomic.Int64
	publication atomic.Int64
}

func (b *nativeQualBlast) note(result endpointResult, deadline, seen time.Time) {
	if result.identity.path == nativeQualMarkedLane {
		b.marked.took(result)
	}
	b.accepted[result.identity.path].Add(int64(result.members[0].accepted))
	b.uncertain.Add(int64(result.members[0].uncertain))
	if result.members[0].unsubmitted != 0 {
		b.expired.Add(1)
	}
	// Only a request with no attempted datagram never reached native I/O.
	if result.members[0].accepted == 0 && result.members[0].uncertain == 0 {
		return
	}
	returned := result.members[0].returnedAt.ToTime()
	nativeQualRaise(&b.overrun, int64(returned.Sub(deadline)))
	nativeQualRaise(&b.publication, int64(seen.Sub(returned)))
}

// nativeQualRaise keeps the largest value any participant reports.
func nativeQualRaise(maximum *atomic.Int64, value int64) {
	for {
		current := maximum.Load()
		if value <= current || maximum.CompareAndSwap(current, value) {
			return
		}
	}
}

// participant offers every lane, retrying through the readiness path, and consumes
// results so no consumer stalls.
func (b *nativeQualBlast) participant(t *testing.T, e *nativeEndpoint, p *endpointParticipant, destination netip.AddrPort, perLane int, payload string, stop <-chan struct{}) {
	t.Helper()
	deadlines := make(map[uint64]time.Time)
	outstanding := 0
	drain := func() {
		for {
			result, ok := e.take(p)
			if !ok {
				return
			}
			b.note(result, deadlines[result.identity.registration], time.Now())
			delete(deadlines, result.identity.registration)
			outstanding--
		}
	}
	registration := uint64(0)
	for i := 0; i < perLane; i++ {
		for lane := range 3 {
			select {
			case <-stop:
				i = perLane
			default:
			}
			if i == perLane {
				break
			}
			registration++
			var request endpointRequest
			if lane == nativeQualMarkedLane {
				request = b.marked.request(p.id, registration, destination)
			} else {
				request = nativeQualRequest(p.id, registration, destination, time.Time{}, payload)
			}
			request.identity.path = uint64(lane)
			for {
				deadline := time.Now().Add(nativeQualBudget)
				request.deadline = deadline
				deadlines[registration] = deadline
				err := e.offer(p, lane, request)
				if err == nil {
					if lane == nativeQualMarkedLane {
						b.marked.offered.Add(1)
					}
					outstanding++
					break
				}
				if !errors.Is(err, errEndpointCapacity) {
					t.Errorf("participant %d lane %d: %v", p.id, lane, err)
					nativeQualRelease(request)
					return
				}
				// Consuming only here leaves results pending, so participants
				// reach their allotment and the readiness path does real work.
				b.refusals.Add(1)
				drain()
				select {
				case <-p.ready:
				case <-p.completed:
				case <-time.After(endpointRetry):
				}
			}
		}
	}
	for outstanding != 0 {
		before := outstanding
		drain()
		if outstanding == before {
			select {
			case <-p.completed:
			case <-time.After(5 * time.Second):
				t.Errorf("participant %d: %d results never arrived", p.id, outstanding)
				return
			}
		}
	}
}

// A marked group can return an attempted prefix with an unsubmitted suffix. It still
// counts as expired, but its native return and publication stay in the maxima.
func TestNativeQualBlastKeepsPartialTiming(t *testing.T) {
	deadline := time.Now()
	returned := deadline.Add(10 * time.Millisecond)
	b := &nativeQualBlast{marked: newNativeQualMarked("abc", 1, nativeQualSegments)}
	b.note(endpointResult{
		identity: endpointIdentity{path: nativeQualMarkedLane},
		members:  []endpointMemberResult{{accepted: 2, unsubmitted: 2, err: os.ErrDeadlineExceeded, returnedAt: monotime.FromTime(returned)}},
	}, deadline, returned.Add(30*time.Millisecond))
	require.EqualValues(t, 1, b.expired.Load())
	require.EqualValues(t, 2, b.accepted[nativeQualMarkedLane].Load())
	require.EqualValues(t, 10*time.Millisecond, b.overrun.Load())
	require.EqualValues(t, 30*time.Millisecond, b.publication.Load())
	require.Empty(t, b.marked.succeeded, "a partial group is not a witness")

	// A wholly unsubmitted request never reached native I/O: no native timing.
	late := deadline.Add(time.Second)
	b.note(endpointResult{
		identity: endpointIdentity{path: 1},
		members:  []endpointMemberResult{{unsubmitted: 1, err: os.ErrDeadlineExceeded, returnedAt: monotime.FromTime(late)}},
	}, deadline, late.Add(time.Second))
	require.EqualValues(t, 2, b.expired.Load())
	require.EqualValues(t, 10*time.Millisecond, b.overrun.Load())
	require.EqualValues(t, 30*time.Millisecond, b.publication.Load())
}

func TestNativeQualRaiseKeepsMaximum(t *testing.T) {
	var maximum atomic.Int64
	var raisers sync.WaitGroup
	for raiser := range 32 {
		raisers.Go(func() {
			for value := range 10000 {
				nativeQualRaise(&maximum, int64(value))
				if raiser == 0 && value == 5000 {
					nativeQualRaise(&maximum, 1<<40)
				}
			}
		})
	}
	raisers.Wait()
	require.EqualValues(t, 1<<40, maximum.Load(), "a smaller concurrent sample can't replace a larger one")
}

// nativeQualHeld is a fake native whose holdAt send waits for one release, and
// whose failAt send fails. It exercises the witness without UDP or timing claims.
type nativeQualHeld struct {
	sends, holdAt, failAt atomic.Int64
	release               chan struct{}
}

func (n *nativeQualHeld) SetWriteDeadline(time.Time) error { return nil }

func (n *nativeQualHeld) WriteMsgUDPAddrPort(b, _ []byte, _ netip.AddrPort) (int, int, error) {
	send := n.sends.Add(1)
	if send == n.holdAt.Load() {
		<-n.release
	}
	if send == n.failAt.Load() {
		return 0, 0, &os.SyscallError{Syscall: "sendmsg", Err: syscall.ECONNREFUSED}
	}
	return len(b), 0, nil
}

func TestNativeQualMarkedWitness(t *testing.T) {
	destination := netip.MustParseAddrPort("127.0.0.1:1234")
	// held runs one request through a writer held at send holdAt, sampling it there.
	held := func(t *testing.T, native *nativeQualHeld, holdAt int64, m *nativeQualMarked, request endpointRequest) (*nativeEndpoint, *endpointParticipant, *endpointToken) {
		t.Helper()
		native.holdAt.Store(holdAt)
		e := startNativeEndpoint(native, nativeQualBudget, nil)
		p, err := e.attach(1)
		require.NoError(t, err)
		require.NoError(t, e.offer(p, 0, request))
		synctest.Wait()
		var token *endpointToken
		var fresh bool
		e.do(func(s *endpointState) { token, fresh = m.sample(s) })
		if request.shortSoftwareDatagramGroup {
			require.True(t, fresh)
			require.Nil(t, token.completion.Load(), "sampled inside the native borrow")
			e.do(func(s *endpointState) { _, fresh = m.sample(s) })
			require.False(t, fresh, "a token is fresh only once")
		}
		return e, p, token
	}

	t.Run("a taken success pairs with its fact", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			native := &nativeQualHeld{release: make(chan struct{})}
			m := newNativeQualMarked("abc", 2, 3)
			e, p, _ := held(t, native, 1, m, m.request(1, 1, destination))
			defer func() { <-e.stop() }()
			native.release <- struct{}{}
			synctest.Wait()
			count, _ := m.witnessed()
			require.Zero(t, count, "a published fact alone is not a witness")
			m.consume(e, p)
			count, margin := m.witnessed()
			require.Equal(t, 1, count)
			require.Equal(t, 120*time.Millisecond, margin, "no time passed in the bubble")
			require.EqualValues(t, 6, native.sends.Load(), "two members of three scalar datagrams")
			evidence := &nativeQualEvidence{Facts: map[string]any{}}
			require.Equal(t, 1, m.record(evidence, "pressure_"))
			require.Equal(t, 6, evidence.Facts["pressure_marked_group_datagrams"])
			require.Equal(t, 1, evidence.Facts["pressure_marked_witnessed"])
		})
	})

	t.Run("an unmarked request is never sampled", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			native := &nativeQualHeld{release: make(chan struct{})}
			m := newNativeQualMarked("abc", 2, 3)
			request := m.request(1, 1, destination)
			request.shortSoftwareDatagramGroup = false
			e, p, token := held(t, native, 1, m, request)
			defer func() { <-e.stop() }()
			require.Nil(t, token)
			native.release <- struct{}{}
			synctest.Wait()
			m.consume(e, p)
			count, _ := m.witnessed()
			require.Zero(t, count)
		})
	})

	t.Run("a member error is not a witness", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			native := &nativeQualHeld{release: make(chan struct{})}
			native.failAt.Store(2)
			m := newNativeQualMarked("abc", 2, 3)
			e, p, token := held(t, native, 1, m, m.request(1, 1, destination))
			defer func() { <-e.stop() }()
			native.release <- struct{}{}
			synctest.Wait()
			m.consume(e, p)
			require.Error(t, token.completion.Load().result.failure())
			count, _ := m.witnessed()
			require.Zero(t, count)
			require.Empty(t, m.succeeded)
		})
	})

	t.Run("a return after the cutoff is not a witness", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			native := &nativeQualHeld{release: make(chan struct{})}
			m := newNativeQualMarked("abc", 2, 3)
			// Holding the last send lets every datagram be accepted after the cutoff.
			e, p, _ := held(t, native, 6, m, m.request(1, 1, destination))
			defer func() { <-e.stop() }()
			time.Sleep(nativeQualBudget + endpointTolerance)
			native.release <- struct{}{}
			synctest.Wait()
			m.consume(e, p)
			_, faulted := e.faultObservation()
			require.True(t, faulted, "control owns the same cutoff")
			require.Len(t, m.succeeded, 1, "every datagram was accepted")
			count, _ := m.witnessed()
			require.Zero(t, count)
		})
	})

	t.Run("close starts while a fresh marked request is in the writer", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			native := &nativeQualHeld{release: make(chan struct{})}
			native.holdAt.Store(1)
			e := startNativeEndpoint(native, nativeQualBudget, nil)
			p, err := e.attach(nativeQualActivityID)
			require.NoError(t, err)
			m := newNativeQualMarked("abc", 2, 3)
			done := make(chan error, 1)
			go func() { done <- m.produce(e, p, destination, 8) }()
			synctest.Wait()
			e.do(func(s *endpointState) { m.sample(s) })
			// Request 1 completes; request 2 waits at its first send.
			native.holdAt.Store(7)
			native.release <- struct{}{}
			synctest.Wait()
			token, closing := m.awaitInFlight(t, e)
			stopped := e.stop()
			time.Sleep(time.Millisecond)
			native.release <- struct{}{}
			<-stopped
			require.NoError(t, <-done)
			fact := token.completion.Load()
			require.NotNil(t, fact)
			require.False(t, fact.fault)
			require.True(t, fact.returnedAt.After(closing))
			require.ErrorIs(t, fact.result.failure(), errEndpointRevoked, "sealing revokes the rest of the request")
			require.Equal(t, 1, fact.result.members[0].accepted)
			count, _ := m.witnessed()
			require.Equal(t, 1, count, "request 1 is the activity's witness")
			require.GreaterOrEqual(t, m.offered.Load(), int64(2))
		})
	})
}

func TestNativeQualification(t *testing.T) {
	nativeQualRun(t, "expired-before-dispatch", func(t *testing.T, evidence *nativeQualEvidence) {
		receiver, socket, destination := nativeQualSockets(t)
		e, err := newNativeEndpoint(socket, nativeQualBudget, nil)
		require.NoError(t, err)
		defer func() { <-e.stop() }()
		p, err := e.attach(1)
		require.NoError(t, err)

		offered := time.Now()
		nativeQualOffer(t, e, p, nativeQualRequest(1, 1, destination, offered.Add(-time.Millisecond), "expired"))
		expired, seen := nativeQualTake(t, e, p)
		require.ErrorIs(t, expired.failure(), os.ErrDeadlineExceeded)
		require.Equal(t, 1, expired.members[0].unsubmitted)
		require.Zero(t, expired.members[0].accepted)
		require.Zero(t, expired.members[0].uncertain)
		evidence.TimingMS["expiry"] = nativeQualMS(seen.Sub(offered))

		// The first datagram the receiver sees is the successor's: the expired
		// request never reached the socket, and its successor got a usable deadline.
		time.Sleep(2 * endpointRetry)
		successor := time.Now()
		nativeQualOffer(t, e, p, nativeQualRequest(1, 2, destination, successor.Add(nativeQualBudget), "successor"))
		result, seen := nativeQualTake(t, e, p)
		require.NoError(t, result.failure())
		require.Equal(t, 1, result.members[0].accepted)
		require.Equal(t, "successor", nativeQualRead(t, receiver))
		evidence.TimingMS["successor_publication"] = nativeQualMS(seen.Sub(result.members[0].returnedAt.ToTime()))
		_, faulted := e.faultObservation()
		require.False(t, faulted)
	})

	nativeQualRun(t, "eagain-to-deadline", func(t *testing.T, evidence *nativeQualEvidence) {
		_, socket, destination := nativeQualSockets(t)
		e, err := newNativeEndpoint(socket, nativeQualBudget, nil)
		require.NoError(t, err)
		defer func() { <-e.stop() }()
		p, err := e.attach(1)
		require.NoError(t, err)

		deadline := time.Now().Add(nativeQualBudget)
		evidence.Traversal = nativeQualStall(t, e, p, nativeQualRequest(1, 1, destination, deadline, "stalled"))
		result, seen := nativeQualTake(t, e, p)
		require.ErrorIs(t, result.failure(), os.ErrDeadlineExceeded)
		require.Equal(t, 1, result.members[0].uncertain, "a native timeout never proves zero submission")
		require.Zero(t, result.members[0].unsubmitted)
		require.Zero(t, result.members[0].accepted)
		evidence.TimingMS["injected_native_return"] = nativeQualMS(result.members[0].returnedAt.ToTime().Sub(deadline))
		evidence.TimingMS["injected_publication"] = nativeQualMS(seen.Sub(result.members[0].returnedAt.ToTime()))
		_, faulted := e.faultObservation()
		require.False(t, faulted)
	})

	nativeQualRun(t, "retry-fallback", func(t *testing.T, evidence *nativeQualEvidence) {
		// The driver's disposable namespace drops UDP, so the first real sendmsg
		// returns EPERM. Every later sendmsg on this thread is an injected EAGAIN.
		_, socket, destination := nativeQualSockets(t)
		native := &nativeQualThreadPinned{socket: socket}
		e := startNativeEndpoint(native, nativeQualBudget, nil)
		defer func() { <-e.stop() }()
		p, err := e.attach(1)
		require.NoError(t, err)

		deadline := time.Now().Add(nativeQualBudget)
		retry := nativeQualRequest(1, 1, destination, deadline, "permission retry")
		retry.members[0].writeState = &endpointWriteState{}
		retried := nativeQualStall(t, e, p, retry)
		result, _ := nativeQualTake(t, e, p)
		require.ErrorIs(t, result.failure(), os.ErrDeadlineExceeded)
		require.Equal(t, 1, result.members[0].uncertain)
		evidence.TimingMS["injected_retry_return"] = nativeQualMS(result.members[0].returnedAt.ToTime().Sub(deadline))

		time.Sleep(2 * endpointRetry)
		deadline = time.Now().Add(nativeQualBudget)
		fallback := nativeQualRequest(1, 2, destination, deadline, strings.Repeat("g", 300))
		fallback.members[0].gsoSize, fallback.members[0].nativeGSO = 100, true
		state := &endpointWriteState{}
		fallback.members[0].writeState = state
		fellBack := nativeQualStall(t, e, p, fallback)
		result, _ = nativeQualTake(t, e, p)
		require.ErrorIs(t, result.failure(), os.ErrDeadlineExceeded)
		require.Equal(t, 1, result.members[0].uncertain, "only the attempted segment is uncertain")
		require.Equal(t, 2, result.members[0].unsubmitted)
		require.True(t, state.gsoDisabled.Load())
		require.EqualValues(t, 1, native.simulatedEIO.Load())
		evidence.TimingMS["injected_fallback_return"] = nativeQualMS(result.members[0].returnedAt.ToTime().Sub(deadline))
		evidence.Traversal = retried && fellBack
		evidence.Facts["simulated_gso_eio"] = native.simulatedEIO.Load()
		_, faulted := e.faultObservation()
		require.False(t, faulted)
	})

	nativeQualRun(t, "queued-expiry", func(t *testing.T, evidence *nativeQualEvidence) {
		_, socket, destination := nativeQualSockets(t)
		e, err := newNativeEndpoint(socket, nativeQualBudget, nil)
		require.NoError(t, err)
		defer func() { <-e.stop() }()
		p, err := e.attach(1)
		require.NoError(t, err)

		start := time.Now()
		currentDeadline := start.Add(nativeQualBudget)
		evidence.Traversal = nativeQualStall(t, e, p, nativeQualRequest(1, 1, destination, currentDeadline, "current"))
		queuedDeadline := start.Add(nativeQualBudget / 2)
		nativeQualOffer(t, e, p, nativeQualRequest(1, 2, destination, queuedDeadline, "queued"))

		queued, seen := nativeQualTake(t, e, p)
		require.EqualValues(t, 2, queued.identity.registration, "the queued request expires first")
		require.ErrorIs(t, queued.failure(), os.ErrDeadlineExceeded)
		require.Equal(t, 1, queued.members[0].unsubmitted)
		require.True(t, seen.Before(currentDeadline), "its expiry can't wait for the current request's return")
		require.True(t, nativeQualWriterWaiting(), "the current request is still in native I/O")
		evidence.TimingMS["injected_queued_expiry"] = nativeQualMS(seen.Sub(queuedDeadline))

		current, _ := nativeQualTake(t, e, p)
		require.EqualValues(t, 1, current.identity.registration)
		require.Equal(t, 1, current.members[0].uncertain)
		evidence.TimingMS["injected_current_return"] = nativeQualMS(current.members[0].returnedAt.ToTime().Sub(currentDeadline))
	})

	nativeQualRun(t, "shutdown-join", func(t *testing.T, evidence *nativeQualEvidence) {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		serverSocket, clientSocket, receiver := newUDPConnLocalhost(t), newUDPConnLocalhost(t), newUDPConnLocalhost(t)
		serverTransport, clientTransport := &Transport{Conn: serverSocket}, &Transport{Conn: clientSocket}
		for _, tr := range []*Transport{serverTransport, clientTransport} {
			require.NoError(t, tr.configureNativeIO(nativeQualBudget))
		}
		listener, err := serverTransport.Listen(nativeServerTLS(), nil)
		require.NoError(t, err)
		client, err := clientTransport.Dial(ctx, listener.Addr(), nativeClientTLS(), nil)
		require.NoError(t, err)
		server, err := listener.Accept(ctx)
		require.NoError(t, err)

		stream, err := client.OpenStreamSync(ctx)
		require.NoError(t, err)
		payload := strings.Repeat("granted route\n", 2048)
		_, err = stream.Write([]byte(payload))
		require.NoError(t, err)
		require.NoError(t, stream.Close())
		accepted, err := server.AcceptStream(ctx)
		require.NoError(t, err)
		data, err := io.ReadAll(accepted)
		require.NoError(t, err)
		require.Equal(t, payload, string(data))

		writing := time.Now()
		written, err := clientTransport.WriteTo([]byte("public"), receiver.LocalAddr())
		evidence.TimingMS["public_write_return"] = nativeQualMS(time.Since(writing))
		require.NoError(t, err)
		require.Equal(t, len("public"), written)
		require.Equal(t, "public", nativeQualRead(t, receiver))

		// R21: the client endpoint closes during bounded marked-group activity. Its
		// datagrams go to a sink nobody reads, so the receiver's reads stay exact.
		clientEndpoint := nativeTransportEndpoint(clientTransport)
		require.NotNil(t, clientEndpoint)
		sink := newUDPConnLocalhost(t).LocalAddr().(*net.UDPAddr).AddrPort()
		marked := newNativeQualMarked(strings.Repeat("s", 300), nativeQualActivityMembers, nativeQualSegments)
		activityDone := make(chan error, 1)
		for name, tr := range map[string]*Transport{"client": clientTransport, "server": serverTransport} {
			require.ErrorIs(t, tr.waitNativeIOStopped(ctx), ErrMultipathNotStoppingV1)
			var inFlight *endpointToken
			closing := time.Now()
			if tr == clientTransport {
				activity, err := clientEndpoint.attach(nativeQualActivityID)
				require.NoError(t, err)
				go func() { activityDone <- marked.produce(clientEndpoint, activity, sink, nativeQualActivityRequests) }()
				inFlight, closing = marked.awaitInFlight(t, clientEndpoint)
			}
			require.NoError(t, tr.Close())
			require.NoError(t, tr.waitNativeIOStopped(ctx))
			joined := time.Since(closing)
			evidence.Facts[name+"_join_ms"] = nativeQualMS(joined)
			require.Less(t, joined, nativeQualBudget+endpointTolerance, "the current request's budget bounds the join")
			_, err = tr.WriteTo([]byte("late"), receiver.LocalAddr())
			require.ErrorIs(t, err, net.ErrClosed)
			if inFlight == nil {
				continue
			}
			// The join follows writer exit, which follows the borrow's published
			// return. That return after close began is the active-close precondition.
			// Sealing may revoke the rest of this request; its disposition is kept.
			fact := inFlight.completion.Load()
			require.NotNil(t, fact, "the writer publishes before its exit")
			require.False(t, fact.fault)
			after := fact.returnedAt.Sub(closing)
			require.Positive(t, after, "close began while a marked group was in the writer")
			evidence.Facts["active_close_return_after_close_ms"] = nativeQualMS(after)
			var accepted, uncertain, unsubmitted int
			for _, member := range fact.result.members {
				accepted, uncertain, unsubmitted = accepted+member.accepted, uncertain+member.uncertain, unsubmitted+member.unsubmitted
			}
			evidence.Facts["active_close_accepted"] = accepted
			evidence.Facts["active_close_uncertain"] = uncertain
			evidence.Facts["active_close_unsubmitted"] = unsubmitted
			if err := fact.result.failure(); err != nil {
				evidence.Facts["active_close_error"] = err.Error()
			}
			_, faulted := clientEndpoint.faultObservation()
			require.False(t, faulted)
			select {
			case err := <-activityDone:
				require.NoError(t, err)
			case <-time.After(5 * time.Second):
				require.FailNow(t, "the marked activity did not end with the seal")
			}
		}
		require.NoError(t, listener.Close())
		require.Positive(t, marked.record(evidence, ""), "no successful marked group from the closing activity reached the writer yield")

		// The join returned write and deadline authority over the caller's socket.
		require.NoError(t, clientSocket.SetWriteDeadline(time.Now().Add(-time.Second)))
		_, err = clientSocket.WriteToUDP([]byte("expired"), receiver.LocalAddr().(*net.UDPAddr))
		require.ErrorIs(t, err, os.ErrDeadlineExceeded)
		require.NoError(t, clientSocket.SetWriteDeadline(time.Time{}))
		_, err = clientSocket.WriteToUDP([]byte("mine again"), receiver.LocalAddr().(*net.UDPAddr))
		require.NoError(t, err)
		require.Equal(t, "mine again", nativeQualRead(t, receiver))
	})

	nativeQualRun(t, "request-contention", func(t *testing.T, evidence *nativeQualEvidence) {
		receiver, socket, destination := nativeQualSockets(t)
		// A sink that never returns shows that publication can't backpressure the writer.
		sinkRelease := make(chan struct{})
		var releaseSink sync.Once
		defer releaseSink.Do(func() { close(sinkRelease) })
		e, err := newNativeEndpoint(socket, nativeQualBudget, func(endpointDiagnostic) { <-sinkRelease })
		require.NoError(t, err)
		defer func() { <-e.stop() }()
		var participants []*endpointParticipant
		for id := uint64(1); id <= 32; id++ {
			p, err := e.attach(id)
			require.NoError(t, err)
			participants = append(participants, p)
		}
		addr := net.UDPAddrFromAddrPort(destination)

		var received atomic.Int64
		readerDone := make(chan struct{})
		go func() {
			defer close(readerDone)
			var buffer [2048]byte
			for {
				if _, _, err := receiver.ReadFromUDP(buffer[:]); err != nil {
					return
				}
				received.Add(1)
			}
		}()

		// The bounded request classes share the real writer until each lane's
		// fixed count is done.
		const perLane = 64
		blast := &nativeQualBlast{marked: newNativeQualMarked("contention", 1, nativeQualSegments)}
		var workers, classes sync.WaitGroup
		for _, p := range participants {
			workers.Go(func() { blast.participant(t, e, p, destination, perLane, "contention", nil) })
		}
		stopClasses := make(chan struct{})
		var statelessSent, statelessAttempts, publicSent, publicAttempts atomic.Int64
		contending := time.Now()
		classes.Go(func() {
			for {
				select {
				case <-stopClasses:
					return
				default:
				}
				statelessAttempts.Add(1)
				if e.writeResponse([]byte("stateless"), addr, packetInfo{}, nil) == nil {
					statelessSent.Add(1)
				}
				time.Sleep(endpointRetry)
			}
		})
		classes.Go(func() {
			for {
				select {
				case <-stopClasses:
					return
				default:
				}
				publicAttempts.Add(1)
				if n, err := e.writePublic([]byte("public"), addr, nil); err == nil && n == len("public") {
					publicSent.Add(1)
				}
				time.Sleep(endpointRetry)
			}
		})
		peakStop := make(chan struct{})
		peak := 0
		var peaks sync.WaitGroup
		peaks.Go(func() {
			for {
				select {
				case <-peakStop:
					return
				case <-time.After(100 * time.Microsecond):
					e.do(func(s *endpointState) {
						peak = max(peak, s.count)
						blast.marked.sample(s)
					})
				}
			}
		})
		workers.Wait()
		evidence.Facts["contention_ms"] = nativeQualMS(time.Since(contending))
		close(peakStop)
		peaks.Wait()
		close(stopClasses)
		classes.Wait()
		require.Positive(t, blast.refusals.Load(), "this phase has to contend for admission")
		evidence.Facts["peak_endpoint_requests"] = peak
		for lane := range blast.accepted {
			require.Positive(t, blast.accepted[lane].Load(), "lane %d made no progress", lane)
		}
		require.Positive(t, statelessSent.Load())
		require.Positive(t, publicSent.Load())
		_, faulted := e.faultObservation()
		require.False(t, faulted)
		evidence.TimingMS["native_return_overrun"] = nativeQualMS(time.Duration(blast.overrun.Load()))
		evidence.TimingMS["publication"] = nativeQualMS(time.Duration(blast.publication.Load()))
		for lane := range blast.accepted {
			evidence.Facts["lane_"+strconv.Itoa(lane)+"_accepted"] = blast.accepted[lane].Load()
		}
		evidence.Facts["expired"] = blast.expired.Load()
		evidence.Facts["uncertain"] = blast.uncertain.Load()
		evidence.Facts["refusals"] = blast.refusals.Load()
		evidence.Facts["stateless_sent"] = statelessSent.Load()
		evidence.Facts["stateless_attempts"] = statelessAttempts.Load()
		evidence.Facts["public_sent"] = publicSent.Load()
		evidence.Facts["public_attempts"] = publicAttempts.Load()
		evidence.Facts["diagnostics_dropped"] = e.dropped.Load()
		require.Positive(t, blast.marked.record(evidence, ""), "no successful marked group reached the writer yield")

		// The pressure phase only records whether real traffic made the writer
		// wait. No observed wait is a recorded fact, not pressure evidence.
		require.NoError(t, socket.SetWriteBuffer(2048))
		sndbufErrors := nativeQualSNMP(t, "SndbufErrors")
		stopPressure := make(chan struct{})
		pressure := &nativeQualBlast{marked: newNativeQualMarked(strings.Repeat("p", 300), 1, nativeQualSegments)}
		var pressured sync.WaitGroup
		payload := strings.Repeat("p", 1200)
		for _, p := range participants {
			pressured.Go(func() { pressure.participant(t, e, p, destination, 1<<30, payload, stopPressure) })
		}
		samples, waits := 0, 0
		for sampling := time.Now(); time.Since(sampling) < time.Second; time.Sleep(5 * time.Millisecond) {
			samples++
			if nativeQualWriterWaiting() {
				waits++
			}
			e.do(func(s *endpointState) { pressure.marked.sample(s) })
		}
		close(stopPressure)
		pressured.Wait()
		evidence.Facts["pressure_stack_samples"] = samples
		evidence.Facts["pressure_stack_waits"] = waits
		evidence.Facts["pressure_sndbuf_errors"] = nativeQualSNMP(t, "SndbufErrors") - sndbufErrors
		evidence.Facts["pressure_accepted"] = pressure.accepted[0].Load() + pressure.accepted[1].Load() + pressure.accepted[2].Load()
		evidence.Facts["pressure_expired"] = pressure.expired.Load()
		evidence.Facts["pressure_uncertain"] = pressure.uncertain.Load()
		evidence.TimingMS["pressure_native_return_overrun"] = nativeQualMS(time.Duration(pressure.overrun.Load()))
		evidence.TimingMS["pressure_publication"] = nativeQualMS(time.Duration(pressure.publication.Load()))
		_, faulted = e.faultObservation()
		require.False(t, faulted, "the pressure witness needs an unfaulted endpoint")
		require.Positive(t, pressure.marked.record(evidence, "pressure_"), "no successful marked group reached the writer yield under pressure")

		releaseSink.Do(func() { close(sinkRelease) })
		require.NoError(t, receiver.SetReadDeadline(time.Now()))
		<-readerDone
		require.Positive(t, received.Load())
		evidence.Facts["received"] = received.Load()
	})
}
