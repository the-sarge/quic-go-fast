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
	"time"

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
type nativeQualBlast struct {
	accepted    [3]atomic.Int64
	expired     atomic.Int64
	uncertain   atomic.Int64
	refusals    atomic.Int64
	overrun     atomic.Int64
	publication atomic.Int64
}

func (b *nativeQualBlast) note(result endpointResult, deadline, seen time.Time) {
	b.accepted[result.identity.path].Add(int64(result.members[0].accepted))
	b.uncertain.Add(int64(result.members[0].uncertain))
	if result.members[0].unsubmitted != 0 {
		b.expired.Add(1)
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
			request := nativeQualRequest(p.id, registration, destination, time.Time{}, payload)
			request.identity.path = uint64(lane)
			for {
				deadline := time.Now().Add(nativeQualBudget)
				request.deadline = deadline
				deadlines[registration] = deadline
				err := e.offer(p, lane, request)
				if err == nil {
					outstanding++
					break
				}
				if !errors.Is(err, errEndpointCapacity) {
					t.Errorf("participant %d lane %d: %v", p.id, lane, err)
					request.members[0].buffer.Release()
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

		for name, tr := range map[string]*Transport{"client": clientTransport, "server": serverTransport} {
			require.ErrorIs(t, tr.waitNativeIOStopped(ctx), ErrMultipathNotStoppingV1)
			closing := time.Now()
			require.NoError(t, tr.Close())
			require.NoError(t, tr.waitNativeIOStopped(ctx))
			joined := time.Since(closing)
			evidence.Facts[name+"_join_ms"] = nativeQualMS(joined)
			require.Less(t, joined, nativeQualBudget+endpointTolerance, "the current request's budget bounds the join")
			_, err = tr.WriteTo([]byte("late"), receiver.LocalAddr())
			require.ErrorIs(t, err, net.ErrClosed)
		}
		require.NoError(t, listener.Close())

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
		blast := &nativeQualBlast{}
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
					e.do(func(s *endpointState) { peak = max(peak, s.count) })
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

		// The pressure phase only records whether real traffic made the writer
		// wait. No observed wait is a recorded fact, not pressure evidence.
		require.NoError(t, socket.SetWriteBuffer(2048))
		sndbufErrors := nativeQualSNMP(t, "SndbufErrors")
		stopPressure := make(chan struct{})
		pressure := &nativeQualBlast{}
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

		releaseSink.Do(func() { close(sinkRelease) })
		require.NoError(t, receiver.SetReadDeadline(time.Now()))
		<-readerDone
		require.Positive(t, received.Load())
		evidence.Facts["received"] = received.Load()
	})
}
