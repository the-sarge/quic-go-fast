//go:build unix

package quic

import (
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
	<-p.completed
	result, ok := e.take(p)
	require.True(t, ok)
	require.NoError(t, result.failure())
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
