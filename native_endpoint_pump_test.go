//go:build unix

package quic

import (
	"net"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNativeEndpointSynchronousControl(t *testing.T) {
	e := startNativeEndpoint(endpointTestNative{}, time.Second, nil)
	defer func() { <-e.stop() }()
	allocations := testing.AllocsPerRun(10, func() {
		quarantined := false
		e.do(func(s *endpointState) { quarantined = s.faulted || s.sealed })
		require.False(t, quarantined)
	})
	require.Zero(t, allocations, "a local endpoint query needs no command/ack rendezvous")
}

// Without the progress goroutine, these operations can only use synchronous
// control and writer-owned continuation. The real writer/native seam is unchanged.
func endpointPumpWithoutProgress(native endpointNativeIO) *nativeEndpoint {
	e := &nativeEndpoint{
		generation: nextNativeEndpoint.Add(1), budget: time.Second,
		work: make(chan struct{}, 1), progress: make(chan struct{}, 1), halt: make(chan struct{}),
		writer: make(chan struct{}), done: make(chan struct{}), fault: make(chan struct{}),
		diagnostics: make(chan endpointDiagnostic, 128),
	}
	e.state = &endpointState{endpoint: e, budget: time.Second}
	go e.write(native)
	return e
}

func stopEndpointPumpWithoutProgress(e *nativeEndpoint) {
	e.seal()
	close(e.halt)
	<-e.writer
}

func TestNativeEndpointWriterContinuationWithoutProgress(t *testing.T) {
	writes := make(chan string, 3)
	e := endpointPumpWithoutProgress(endpointTestNative{send: func(data []byte) (int, error) {
		writes <- string(data)
		return len(data), nil
	}})
	defer stopEndpointPumpWithoutProgress(e)
	p, err := e.attach(1)
	require.NoError(t, err)
	for _, data := range []string{"first", "successor"} {
		require.NoError(t, e.offer(p, 0, endpointTestRequest(1, data)))
	}
	for _, expected := range []string{"first", "successor"} {
		require.Equal(t, expected, <-writes)
	}
	for consumed := 0; consumed < 2; {
		<-p.completed
		for {
			result, ok := e.take(p)
			if !ok {
				break
			}
			require.NoError(t, result.failure())
			require.Equal(t, 1, result.members[0].accepted)
			consumed++
		}
	}
	// The writer has no private chain now; a later enqueue must wake its park.
	require.NoError(t, e.offer(p, 0, endpointTestRequest(1, "after park")))
	require.Equal(t, "after park", <-writes)
	for {
		<-p.completed // A coalesced hint can remain from the initial result drain.
		result, ok := e.take(p)
		if !ok {
			continue
		}
		require.NoError(t, result.failure())
		break
	}
}

func TestNativeEndpointFailedReentryNotifiesProgress(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	e := endpointPumpWithoutProgress(endpointTestNative{send: func(data []byte) (int, error) {
		close(entered)
		<-release
		return len(data), nil
	}})
	defer stopEndpointPumpWithoutProgress(e)
	p, err := e.attach(1)
	require.NoError(t, err)
	require.NoError(t, e.offer(p, 0, endpointTestRequest(1, "native fact")))
	<-entered
	e.mutex.Lock()
	token := e.state.current
	select {
	case <-e.progress:
	default:
	}
	close(release)
	// Cleanup unlocks even if the assertion fails; the writer must first publish
	// and notify independent progress while reentry is still unavailable.
	func() {
		defer e.mutex.Unlock()
		select {
		case <-e.progress:
			require.NotNil(t, token.completion.Load())
		case <-time.After(time.Second):
			t.Fatal("writer waited for control before notifying its published fact")
		}
	}()
	<-p.completed
	result, ok := e.take(p)
	require.True(t, ok)
	require.NoError(t, result.failure())
}

func TestNativeEndpointEarlierDeadlineAfterArming(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		e := startNativeEndpoint(endpointTestNative{send: func(data []byte) (int, error) {
			<-release
			return len(data), nil
		}}, time.Second, nil)
		defer func() { close(release); <-e.stop() }()
		p, err := e.attach(1)
		require.NoError(t, err)
		require.NoError(t, e.offer(p, 0, endpointTestRequest(1, "blocked writer")))
		long := endpointTestRequest(1, "later expiry")
		long.deadline = time.Now().Add(200 * time.Millisecond)
		require.NoError(t, e.offer(p, 1, long))
		synctest.Wait() // the independent timer has armed its later snapshot
		short := endpointTestRequest(1, "earlier expiry")
		short.identity.registration = 3
		short.deadline = time.Now().Add(5 * time.Millisecond)
		require.NoError(t, e.offer(p, 1, short))
		synctest.Wait()
		time.Sleep(6 * time.Millisecond)
		select {
		case <-p.completed:
		default:
			t.Fatal("earlier queued expiry lost its wake while the writer was blocked")
		}
		result, ok := e.take(p)
		require.True(t, ok)
		require.Equal(t, uint64(3), result.identity.registration)
		require.Equal(t, 1, result.members[0].unsubmitted)
	})
}

func TestNativeEndpointCoveredProgress(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := make(chan string, 3)
		permits := make(chan struct{})
		e := endpointPumpWithoutProgress(endpointTestNative{send: func(data []byte) (int, error) {
			started <- string(data)
			<-permits
			return len(data), nil
		}})
		defer func() { close(permits); stopEndpointPumpWithoutProgress(e) }()
		p, err := e.attach(1)
		require.NoError(t, err)
		require.NoError(t, e.offer(p, 0, endpointTestRequest(1, "first")))
		require.Equal(t, "first", <-started)
		require.NoError(t, e.offer(p, 0, endpointTestRequest(1, "successor")))
		synctest.Wait()
		// No progress receiver exists. A later empty channel therefore proves
		// suppression, not that a live runner happened to consume the hint.
		e.mutex.Lock()
		e.state.armedDeadline = time.Now().Add(50 * time.Millisecond)
		select {
		case <-e.progress:
		default:
		}
		e.mutex.Unlock()
		permits <- struct{}{}
		require.Equal(t, "successor", <-started)
		synctest.Wait()
		require.Empty(t, e.progress, "the earlier timer covers the successor claim")
		result, ok := e.take(p)
		require.True(t, ok)
		require.NoError(t, result.failure())
		require.Equal(t, 1, result.members[0].accepted)
		permits <- struct{}{}
		synctest.Wait()
		require.Empty(t, e.progress, "the empty writer park has no new obligation")
		result, ok = e.take(p)
		require.True(t, ok)
		require.NoError(t, result.failure())
		// Writer supply is independent of the suppressed progress hint.
		require.NoError(t, e.offer(p, 0, endpointTestRequest(1, "after park")))
		require.Equal(t, "after park", <-started)
		permits <- struct{}{}
		synctest.Wait()
		result, ok = e.take(p)
		require.True(t, ok)
		require.NoError(t, result.failure())
		require.Empty(t, e.progress)
	})
}

func TestNativeEndpointTerminalReadyProgress(t *testing.T) {
	t.Run("idle seal", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			e := startNativeEndpoint(endpointTestNative{}, time.Second, nil)
			defer func() { e.notifyProgress(); <-e.stop() }()
			synctest.Wait() // the runner has no deadline or pending hint
			e.stop()
			synctest.Wait()
			select {
			case <-e.done:
			default:
				t.Fatal("idle seal lost terminal progress without a deadline")
			}
		})
	})
	t.Run("sealed final return", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			reset := make(chan struct{}, 1)
			e := startNativeEndpoint(endpointTestNative{deadline: func(deadline time.Time) error {
				if deadline.IsZero() {
					<-reset
				}
				return nil
			}}, time.Second, nil)
			defer func() { close(reset); e.notifyProgress(); <-e.stop() }()
			p, err := e.attach(1)
			require.NoError(t, err)
			request := endpointTestRequest(1, "final borrow")
			buffer := request.members[0].buffer
			require.NoError(t, e.offer(p, 0, request))
			synctest.Wait() // member returned, but deadline reset still borrows
			e.stop()
			synctest.Wait()
			require.Equal(t, 1, buffer.refCount)
			select {
			case <-e.done:
				t.Fatal("sealed endpoint joined before its native fence")
			default:
			}
			reset <- struct{}{}
			synctest.Wait()
			select {
			case <-e.done:
			default:
				t.Fatal("final return lost terminal progress when next wake became zero")
			}
			require.Zero(t, buffer.refCount)
		})
	})
}

func TestNativeEndpointRetryProgressAfterSuccessfulReentry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{}, 1)
		e := endpointPumpWithoutProgress(endpointTestNative{send: func([]byte) (int, error) {
			<-release
			return 0, syscall.EAGAIN
		}})
		runnerStarted := false
		defer func() {
			close(release)
			if runnerStarted {
				<-e.stop()
			} else {
				stopEndpointPumpWithoutProgress(e)
			}
		}()
		p, err := e.attach(1)
		require.NoError(t, err)
		require.NoError(t, e.offer(p, 0, endpointTestRequest(1, "backpressure")))
		synctest.Wait()
		prepare := func() (endpointAdmission, error) {
			var admission endpointAdmission
			err := net.ErrClosed
			e.do(func(s *endpointState) { admission, err = s.admit(p, 0, 1) })
			return admission, err
		}
		// Fill this participant's retained allowance through real reservations,
		// so the next preparation leaves genuine pending demand.
		for range 3 {
			_, err := prepare()
			require.NoError(t, err)
		}
		_, err = prepare()
		require.ErrorIs(t, err, errEndpointCapacity)
		e.mutex.Lock()
		e.state.armedDeadline = time.Now().Add(10 * time.Millisecond)
		select {
		case <-e.progress:
		default:
		}
		e.mutex.Unlock()
		// No runner or other caller can contend until the writer has parked.
		// The required hint must come from successful reentry, not TryLock failure.
		release <- struct{}{}
		<-p.completed
		synctest.Wait()
		select {
		case <-e.progress:
		default:
			t.Fatal("successful return did not notify the earlier retry floor")
		}
		result, ok := e.take(p) // free one retained slot before the retry floor
		require.True(t, ok)
		require.ErrorIs(t, result.failure(), syscall.EAGAIN)
		runnerStarted = true
		go e.state.run()
		synctest.Wait()
		require.Empty(t, p.ready)
		time.Sleep(endpointRetry / 2)
		synctest.Wait()
		require.Empty(t, p.ready, "retry readiness must not precede its floor")
		time.Sleep(endpointRetry / 2)
		synctest.Wait()
		select {
		case <-p.ready:
		default:
			t.Fatal("retry floor needed another producer transaction to make progress")
		}
		admission, err := prepare()
		require.NoError(t, err)
		e.do(func(s *endpointState) { s.cancelAdmission(admission) })
	})
}
