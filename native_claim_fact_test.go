//go:build unix

package quic

import (
	"slices"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func TestE12ClaimFactLayout(t *testing.T) {
	t.Logf("endpointToken=%d endpointRequest=%d endpointWriteResult=%d bytes", unsafe.Sizeof(endpointToken{}), unsafe.Sizeof(endpointRequest{}), unsafe.Sizeof(endpointWriteResult{}))
	t.Logf("endpointOneMemberClaim=%d endpointTwoMemberClaim=%d endpointMemberResult=%d endpointResult=%d bytes", unsafe.Sizeof(endpointOneMemberClaim{}), unsafe.Sizeof(endpointTwoMemberClaim{}), unsafe.Sizeof(endpointMemberResult{}), unsafe.Sizeof(endpointResult{}))
}

func TestE12ClaimFactLifecycle(t *testing.T) {
	entered, release := make(chan string, 2), make(chan struct{})
	e := endpointPumpWithoutProgress(endpointTestNative{send: func(data []byte) (int, error) {
		entered <- string(data)
		<-release
		return len(data), nil
	}})
	defer func() { close(release); stopEndpointPumpWithoutProgress(e) }()
	p, err := e.attach(1)
	require.NoError(t, err)
	first := endpointTestRequest(1, "first fact")
	first.identity.registration = 11
	first.epoch = &responseEpoch{parent: &responseEpoch{}}
	require.NoError(t, e.offer(p, 0, first))
	require.Equal(t, "first fact", <-entered)

	var token *endpointToken
	var fact *endpointWriteResult
	var snapshot endpointWriteResult
	func() {
		e.mutex.Lock()
		defer e.mutex.Unlock()
		token = e.state.current
		require.NotNil(t, token)
		require.Same(t, &e.state.records[token.index].request, token.request)
		require.Nil(t, token.completion.Load(), "embedded zero state is not a published return")
		select {
		case <-e.progress:
		default:
		}
		release <- struct{}{}
		// The failed writer reentry publishes and notifies before control can
		// reconcile or release the canonical request's native borrow.
		select {
		case <-e.progress:
		case <-time.After(time.Second):
			t.Fatal("native return did not publish while control was held")
		}
		fact = token.completion.Load()
		require.NotNil(t, fact)
		require.Same(t, &token.fact, fact)
		require.NotNil(t, token.request)
		require.Equal(t, 1, first.members[0].buffer.refCount)
		snapshot = *fact
		snapshot.result.members = slices.Clone(fact.result.members)
	}()
	<-p.completed
	firstResult, ok := e.take(p)
	require.True(t, ok)
	require.NoError(t, firstResult.failure())
	require.Equal(t, uint64(11), firstResult.identity.registration)
	require.Nil(t, token.request, "retained fact cannot keep request metadata or descriptors")
	require.Zero(t, first.members[0].buffer.refCount)

	second := endpointTestRequest(1, "successor fact")
	second.identity.registration = 22
	require.NoError(t, e.offer(p, 0, second))
	require.Equal(t, "successor fact", <-entered)
	e.do(func(s *endpointState) {
		require.NotNil(t, s.current)
		require.NotSame(t, token, s.current)
		require.Equal(t, token.index, s.current.index, "the consumed record is reused by the real owner")
		require.Equal(t, uint64(22), s.current.request.identity.registration)
	})
	require.Equal(t, snapshot, *fact)
	release <- struct{}{}
	<-p.completed
	secondResult, ok := e.take(p)
	require.True(t, ok)
	require.NoError(t, secondResult.failure())
	require.Equal(t, uint64(22), secondResult.identity.registration)
	require.NotSame(t, &firstResult.members[0], &secondResult.members[0])
	require.Equal(t, snapshot, *fact, "successor publication must not overwrite the old immutable fact")
	require.Equal(t, snapshot.result.members, firstResult.members)
	require.Zero(t, second.members[0].buffer.refCount)
}
