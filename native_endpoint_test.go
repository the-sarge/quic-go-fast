//go:build unix

package quic

import (
	"net"
	"net/netip"
	"os"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

type endpointTestNative struct {
	deadline func(time.Time) error
	send     func([]byte) (int, error)
}

func (n endpointTestNative) SetWriteDeadline(deadline time.Time) error {
	if n.deadline != nil {
		return n.deadline(deadline)
	}
	return nil
}

func (n endpointTestNative) WriteMsgUDPAddrPort(b, _ []byte, _ netip.AddrPort) (int, int, error) {
	if n.send == nil {
		return len(b), 0, nil
	}
	written, err := n.send(b)
	return written, 0, err
}

func endpointTestRequest(id uint64, data ...string) endpointRequest {
	r := endpointRequest{identity: endpointIdentity{connection: id, generation: 7, pathGeneration: 5, tuple: 3}, destination: netip.MustParseAddrPort("127.0.0.1:1234")}
	for _, value := range data {
		r.buffers = append(r.buffers, endpointTestBuffer(value))
	}
	return r
}

func TestNativeEndpointQueuedExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		e := startNativeEndpoint(endpointTestNative{send: func(b []byte) (int, error) { <-release; return len(b), nil }}, time.Second, nil)
		defer func() { close(release); <-e.stop() }()
		p, err := e.attach(1)
		require.NoError(t, err)
		require.NoError(t, e.offer(p, 0, endpointTestRequest(1, "current")))
		synctest.Wait()
		for _, delay := range []time.Duration{5 * time.Millisecond, 10 * time.Millisecond, 10 * time.Millisecond} {
			r := endpointTestRequest(1, "queued")
			r.deadline = time.Now().Add(delay)
			require.NoError(t, e.offer(p, 1, r))
		}
		next := endpointTestRequest(1, "waiting producer")
		require.ErrorIs(t, e.offer(p, 1, next), errEndpointCapacity)
		time.Sleep(6 * time.Millisecond)
		result, ok := e.take(p)
		require.True(t, ok, "queued expiry must not wait for native return")
		require.Equal(t, 1, result.unsubmitted)
		require.ErrorIs(t, result.err, os.ErrDeadlineExceeded)
		<-p.ready
		// Another expiry invalidates this already-issued grant until the floor
		// elapses. A stale wakeup must not authorize another immediate transfer.
		time.Sleep(4100 * time.Microsecond)
		require.ErrorIs(t, e.offer(p, 1, next), errEndpointCapacity)
		time.Sleep(time.Millisecond)
		<-p.ready
		require.NoError(t, e.offer(p, 1, next))
	})
}

func TestNativeEndpointClaimRevocation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		writes := 0
		e := startNativeEndpoint(endpointTestNative{deadline: func(d time.Time) error {
			if !d.IsZero() {
				<-release
			}
			return nil
		}, send: func(b []byte) (int, error) { writes++; return len(b), nil }}, time.Second, nil)
		defer func() { <-e.stop() }()
		p, err := e.attach(1)
		require.NoError(t, err)
		require.NoError(t, e.offer(p, 0, endpointTestRequest(1, "claimed")))
		synctest.Wait()
		fence := e.detach(p)
		synctest.Wait()
		select {
		case <-fence:
			t.Fatal("deadline setup still owns the token")
		default:
		}
		_, err = e.attach(1)
		require.Error(t, err, "retiring participant must stay reserved until native acknowledgment")
		close(release)
		<-fence
		require.Zero(t, writes)
		fresh, err := e.attach(1)
		require.NoError(t, err)
		require.NotSame(t, p, fresh)
		r := endpointTestRequest(1, "stale")
		require.Error(t, e.offer(p, 0, r))
		r.buffers[0].Release()
		require.NoError(t, e.offer(fresh, 0, endpointTestRequest(1, "fresh")))
		<-fresh.completed
		result, ok := e.take(fresh)
		require.True(t, ok)
		require.Equal(t, 1, result.accepted)
	})
}

func TestNativeEndpointSubmissionFacts(t *testing.T) {
	for _, test := range []struct {
		name                   string
		err                    error
		uncertain, unsubmitted int
	}{
		{"uncertain", os.ErrDeadlineExceeded, 1, 1},
		{"rejected", syscall.EMSGSIZE, 0, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				attempts := 0
				e := startNativeEndpoint(endpointTestNative{send: func(b []byte) (int, error) {
					attempts++
					if attempts == 2 {
						return 0, test.err
					}
					return len(b), nil
				}}, time.Second, nil)
				defer func() { <-e.stop() }()
				p, err := e.attach(1)
				require.NoError(t, err)
				require.NoError(t, e.offer(p, 0, endpointTestRequest(1, "accepted", "attempted", "tail")))
				<-p.completed
				result, ok := e.take(p)
				require.True(t, ok)
				require.Equal(t, 1, result.accepted)
				require.Equal(t, test.uncertain, result.uncertain)
				require.Equal(t, test.unsubmitted, result.unsubmitted)
				require.ErrorIs(t, result.err, test.err)
				require.Equal(t, 2, attempts, "neither accepted prefix nor uncertain attempt may be replayed")
			})
		})
	}
}

func TestNativeEndpointOverdueBorrow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		e := startNativeEndpoint(endpointTestNative{send: func(b []byte) (int, error) { <-release; return len(b), nil }}, 10*time.Millisecond, nil)
		p, err := e.attach(1)
		require.NoError(t, err)
		require.NoError(t, e.offer(p, 0, endpointTestRequest(1, "borrowed")))
		synctest.Wait()
		time.Sleep(31 * time.Millisecond)
		select {
		case <-e.fault:
		default:
			t.Error("overdue detection needs no new producer")
		}
		_, ok := e.take(p)
		require.False(t, ok, "a fault must not fabricate native return")
		stopped := e.stop()
		select {
		case <-stopped:
			t.Error("stop must retain an unresolved native borrow")
		default:
		}
		close(release)
		<-stopped
		_, err = e.attach(2)
		require.ErrorIs(t, err, net.ErrClosed, "late return must not reopen endpoint")
	})
}

func TestNativeEndpointReturnAtWatchdogBoundary(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := startNativeEndpoint(endpointTestNative{send: func(b []byte) (int, error) {
			time.Sleep(10*time.Millisecond + endpointTolerance)
			return len(b), nil
		}}, 10*time.Millisecond, nil)
		defer func() { <-e.stop() }()
		p, err := e.attach(1)
		require.NoError(t, err)
		require.NoError(t, e.offer(p, 0, endpointTestRequest(1, "accepted at overdue boundary")))
		time.Sleep(31 * time.Millisecond)
		synctest.Wait()
		result, ok := e.take(p)
		require.True(t, ok)
		require.Equal(t, 1, result.accepted, "quarantine must preserve the actual native acceptance")
		require.NoError(t, result.err)
		select {
		case <-e.fault:
		default:
			t.Fatal("native return cannot erase the watchdog boundary")
		}
	})
}

func endpointTestLargeRequest(id uint64) endpointRequest {
	r := endpointTestRequest(id)
	for range endpointBatch {
		b := getLargePacketBuffer()
		b.Data = append(b.Data, 1)
		r.buffers = append(r.buffers, b)
	}
	return r
}

func TestNativeEndpointFairLargeAdmission(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		permits := make(chan struct{}, endpointBatch)
		e := startNativeEndpoint(endpointTestNative{send: func(b []byte) (int, error) { <-permits; return len(b), nil }}, time.Second, nil)
		defer func() { close(permits); <-e.stop() }()
		var large *endpointParticipant
		for _, id := range []uint64{1, 2, 4} {
			p, err := e.attach(id)
			require.NoError(t, err)
			if id == 4 {
				large = p
			}
			for j := range 3 {
				r := endpointTestLargeRequest(id)
				if id == 4 && j == 0 {
					r.deadline = time.Now().Add(10 * time.Millisecond)
				}
				require.NoError(t, e.offer(p, 0, r))
			}
		}
		r := endpointTestLargeRequest(4)
		require.ErrorIs(t, e.offer(large, 0, r), errEndpointCapacity, "data class must respect its byte cap")
		small, err := e.attach(5)
		require.NoError(t, err)
		smallRequest := endpointTestRequest(5, "small")
		require.ErrorIs(t, e.offer(small, 0, smallRequest), errEndpointCapacity, "small arrivals cannot overtake selected large request")
		defer smallRequest.buffers[0].Release()
		e.cancelReadiness(small, 0) // producer withdraws its still-owned work
		time.Sleep(10100 * time.Microsecond)
		require.ErrorIs(t, e.offer(large, 0, r), errEndpointCapacity, "an accumulating grant must also respect the timeout floor")
		time.Sleep(time.Millisecond)
		synctest.Wait()
		select {
		case <-large.ready:
		default:
			t.Fatal("capacity release lost the selected producer's wakeup")
		}
		require.NoError(t, e.offer(large, 0, r))
	})
}

func endpointTestBuffer(data string) *packetBuffer {
	b := getPacketBuffer()
	b.Data = append(b.Data, data...)
	return b
}

func TestNativeEndpointStalledConsumer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := startNativeEndpoint(endpointTestNative{}, time.Second, nil)
		defer func() { <-e.stop() }()
		p, err := e.attach(1)
		require.NoError(t, err)
		for range 4 {
			require.NoError(t, e.offer(p, 0, endpointTestRequest(1, "retained result")))
			synctest.Wait()
		}
		r := endpointTestRequest(1, "refused")
		defer r.buffers[0].Release()
		require.ErrorIs(t, e.offer(p, 0, r), errEndpointCapacity)
		other, err := e.attach(2)
		require.NoError(t, err)
		require.NoError(t, e.offer(other, 0, endpointTestRequest(2, "progress")))
		<-other.completed
		result, ok := e.take(other)
		require.True(t, ok)
		require.Equal(t, 1, result.accepted)
		for range 4 {
			result, ok = e.take(p)
			require.True(t, ok)
			require.Equal(t, 1, result.accepted)
		}
		_, ok = e.take(p)
		require.False(t, ok)
	})
}

func TestNativeEndpointExpiredClaimAndNextDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		var deadlines []time.Time
		writes := 0
		e := startNativeEndpoint(endpointTestNative{deadline: func(d time.Time) error {
			deadlines = append(deadlines, d)
			if len(deadlines) == 1 {
				<-release
			}
			return nil
		}, send: func(b []byte) (int, error) { writes++; return len(b), nil }}, 10*time.Millisecond, nil)
		defer func() { <-e.stop() }()
		p, err := e.attach(1)
		require.NoError(t, err)
		require.NoError(t, e.offer(p, 0, endpointTestRequest(1, "expires in deadline setup")))
		synctest.Wait()
		time.Sleep(11 * time.Millisecond)
		close(release)
		<-p.completed
		result, ok := e.take(p)
		require.True(t, ok)
		require.ErrorIs(t, result.err, os.ErrDeadlineExceeded)
		require.Equal(t, 1, result.unsubmitted)
		require.Zero(t, writes)
		time.Sleep(time.Millisecond)
		require.NoError(t, e.offer(p, 0, endpointTestRequest(1, "fresh")))
		<-p.completed
		result, ok = e.take(p)
		require.True(t, ok)
		require.Equal(t, 1, result.accepted)
		require.Len(t, deadlines, 4)
		require.True(t, deadlines[1].IsZero())
		require.True(t, deadlines[2].After(deadlines[0]))
		require.True(t, deadlines[3].IsZero())
	})
}

func TestNativeEndpointDiagnosticSaturation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		e := startNativeEndpoint(endpointTestNative{}, time.Second, func(endpointDiagnostic) { <-release })
		defer close(release)
		p, err := e.attach(1)
		require.NoError(t, err)
		// One blocked sink plus the fixed queue and one overflow record.
		for range 130 {
			require.NoError(t, e.offer(p, 0, endpointTestRequest(1, "diagnostic")))
			<-p.completed
			_, ok := e.take(p)
			require.True(t, ok)
		}
		require.Positive(t, e.dropped.Load())
		<-e.stop() // sink execution is not part of the native fence
	})
}

func TestNativeEndpointClassAndParticipantLimits(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		e := startNativeEndpoint(endpointTestNative{send: func(b []byte) (int, error) { <-release; return len(b), nil }}, time.Second, nil)
		defer func() { close(release); <-e.stop() }()
		var first *endpointParticipant
		for id := range uint64(endpointParticipants) {
			p, err := e.attach(id + 1)
			require.NoError(t, err)
			if id == 0 {
				first = p
			}
			if id < 26 {
				for range 4 {
					require.NoError(t, e.offer(p, 0, endpointTestRequest(id+1, "data")))
				}
			}
			if id == 26 || id == 27 {
				for range 4 {
					require.NoError(t, e.offer(p, 2, endpointTestRequest(id+1, "control")))
				}
			}
		}
		_, err := e.attach(endpointParticipants + 1)
		require.ErrorIs(t, err, errEndpointCapacity)
		for range 8 {
			require.NoError(t, e.offer(e.stateless, 0, endpointTestRequest(0, "stateless")))
			require.NoError(t, e.offer(e.public, 0, endpointTestRequest(0, "public")))
		}
		for _, p := range []*endpointParticipant{first, e.stateless, e.public} {
			r := endpointTestRequest(p.id, "overflow")
			require.ErrorIs(t, e.offer(p, 0, r), errEndpointCapacity)
			r.buffers[0].Release()
		}
		tooMany := endpointTestRequest(first.id)
		for range endpointBatch + 1 {
			tooMany.buffers = append(tooMany.buffers, endpointTestBuffer("oversize batch"))
		}
		require.ErrorIs(t, e.offer(first, 0, tooMany), errEndpointRequest)
		for _, b := range tooMany.buffers {
			b.Release()
		}
	})
}

func TestNativeEndpointCompletionPool(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := startNativeEndpoint(endpointTestNative{}, time.Second, nil)
		defer func() { <-e.stop() }()
		var first *endpointParticipant
		for id := uint64(1); id <= 64; id++ {
			p, err := e.attach(id)
			require.NoError(t, err)
			if id == 1 {
				first = p
			}
			for range 4 {
				require.NoError(t, e.offer(p, 0, endpointTestRequest(id, "result")))
				synctest.Wait()
			}
		}
		p, err := e.attach(65)
		require.NoError(t, err)
		r := endpointTestRequest(65, "after consumption")
		require.ErrorIs(t, e.offer(p, 0, r), errEndpointCapacity)
		_, ok := e.take(first)
		require.True(t, ok)
		<-p.ready
		require.NoError(t, e.offer(p, 0, r))
		<-p.completed
		result, ok := e.take(p)
		require.True(t, ok)
		require.Equal(t, 1, result.accepted)
	})
}

func TestNativeEndpointReadinessLease(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		permits := make(chan struct{}, 1)
		e := startNativeEndpoint(endpointTestNative{send: func(b []byte) (int, error) { <-permits; return len(b), nil }}, time.Second, nil)
		defer func() { close(permits); <-e.stop() }()
		p, err := e.attach(1)
		require.NoError(t, err)
		for range 4 {
			require.NoError(t, e.offer(p, 0, endpointTestRequest(1, "queued")))
		}
		r := endpointTestRequest(1, "paused producer")
		defer r.buffers[0].Release()
		require.ErrorIs(t, e.offer(p, 0, r), errEndpointCapacity)
		permits <- struct{}{}
		<-p.completed
		_, ok := e.take(p)
		require.True(t, ok)
		generation := <-p.ready
		other, err := e.attach(2)
		require.NoError(t, err)
		next := endpointTestRequest(2, "next turn")
		require.ErrorIs(t, e.offer(other, 0, next), errEndpointCapacity)
		time.Sleep(11 * time.Millisecond)
		select {
		case observed := <-other.ready:
			require.Greater(t, observed, generation, "lease reclamation releases a capacity turn")
		default:
			t.Fatal("paused producer held capacity beyond lease")
		}
		require.NoError(t, e.offer(other, 0, next))
		time.Sleep(2 * time.Millisecond)
		require.NoError(t, e.offer(other, 0, endpointTestRequest(2, "continued progress")), "an unused lease must not re-register abandoned work")
	})
}

func TestNativeEndpointDeadlineFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		writes := 0
		e := startNativeEndpoint(endpointTestNative{deadline: func(time.Time) error { return net.ErrClosed }, send: func(b []byte) (int, error) { writes++; return len(b), nil }}, time.Second, nil)
		defer func() { <-e.stop() }()
		p, err := e.attach(1)
		require.NoError(t, err)
		require.NoError(t, e.offer(p, 0, endpointTestRequest(1, "deadline rejected")))
		<-p.completed
		result, ok := e.take(p)
		require.True(t, ok)
		require.ErrorIs(t, result.err, net.ErrClosed)
		require.Equal(t, 1, result.unsubmitted)
		require.Zero(t, writes)
		select {
		case <-e.fault:
		default:
			t.Fatal("deadline failure must fault the endpoint")
		}
	})
}

func TestNativeEndpointServiceTurns(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		permits := make(chan struct{})
		started := make(chan string, 8)
		e := startNativeEndpoint(endpointTestNative{send: func(b []byte) (int, error) { started <- string(b); <-permits; return len(b), nil }}, time.Second, nil)
		defer func() { close(permits); <-e.stop() }()
		p, err := e.attach(1)
		require.NoError(t, err)
		require.NoError(t, e.offer(p, 0, endpointTestRequest(1, "initial")))
		require.Equal(t, "initial", <-started)
		require.NoError(t, e.offer(p, 0, endpointTestRequest(1, "path0")))
		require.NoError(t, e.offer(p, 1, endpointTestRequest(1, "path1")))
		peer, err := e.attach(2)
		require.NoError(t, err)
		require.NoError(t, e.offer(peer, 0, endpointTestRequest(2, "peer")))
		control, err := e.attach(3)
		require.NoError(t, err)
		for _, data := range []string{"c1", "c2", "c3", "c4"} {
			require.NoError(t, e.offer(control, 2, endpointTestRequest(3, data)))
		}
		for _, expected := range []string{"c1", "c2", "peer", "c3", "c4", "path1", "path0"} {
			permits <- struct{}{}
			require.Equal(t, expected, <-started)
		}
		permits <- struct{}{}
	})
}

func TestNativeEndpointConstructionDomain(t *testing.T) {
	_, err := newNativeEndpoint(nil, time.Second, nil)
	require.ErrorIs(t, err, errEndpointRequest)
	socket, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	defer socket.Close()
	for _, duration := range []time.Duration{0, time.Microsecond, time.Second + 1} {
		_, err = newNativeEndpoint(socket, duration, nil)
		require.ErrorIs(t, err, errEndpointRequest)
	}
	connected, err := net.DialUDP("udp4", nil, socket.LocalAddr().(*net.UDPAddr))
	require.NoError(t, err)
	defer connected.Close()
	_, err = newNativeEndpoint(connected, time.Second, nil)
	require.ErrorIs(t, err, errEndpointRequest)
	first, err := newNativeEndpoint(socket, time.Second, nil)
	require.NoError(t, err)
	p, err := first.attach(1)
	require.NoError(t, err)
	<-first.stop()
	second, err := newNativeEndpoint(socket, time.Second, nil)
	require.NoError(t, err)
	defer func() { <-second.stop() }()
	r := endpointTestRequest(1, "old endpoint")
	require.Error(t, second.offer(p, 0, r))
	r.buffers[0].Release()
}

func TestNativeEndpointUDP(t *testing.T) {
	receiver, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	defer receiver.Close()
	socket, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	defer socket.Close()
	diagnostic := make(chan endpointDiagnostic, 1)
	e, err := newNativeEndpoint(socket, time.Second, func(d endpointDiagnostic) { diagnostic <- d })
	require.NoError(t, err)
	defer func() { <-e.stop() }()
	p, err := e.attach(17)
	require.NoError(t, err)
	r := endpointRequest{destination: receiver.LocalAddr().(*net.UDPAddr).AddrPort(), identity: endpointIdentity{connection: 17, path: 1, tuple: 2, registration: 3}}
	r.identity.endpoint = e.generation
	r.buffers = append(r.buffers, endpointTestBuffer("endpoint"))
	require.NoError(t, e.offer(p, 0, r))
	<-p.completed
	result, ok := e.take(p)
	require.True(t, ok)
	require.Equal(t, r.identity, result.identity)
	require.Equal(t, 1, result.accepted)
	require.Zero(t, result.uncertain)
	require.NoError(t, result.err)
	require.Equal(t, r.identity, (<-diagnostic).identity)
	require.NoError(t, receiver.SetReadDeadline(time.Now().Add(time.Second)))
	var data [32]byte
	n, _, err := receiver.ReadFromUDP(data[:])
	require.NoError(t, err)
	require.Equal(t, "endpoint", string(data[:n]))
}
