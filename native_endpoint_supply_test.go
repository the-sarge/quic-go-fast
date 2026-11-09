//go:build darwin || linux || freebsd

package quic

import (
	"net"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
)

// The real control owner is used without background workers so an admitted
// queue and an unreconciled fact can be observed without scheduler assumptions.
func newPrefixControlEndpoint(t *testing.T) *nativeEndpoint {
	t.Helper()
	e := &nativeEndpoint{
		generation: nextNativeEndpoint.Add(1), budget: time.Second,
		work: make(chan struct{}, 1), progress: make(chan struct{}, 1), fault: make(chan struct{}),
	}
	e.state = &endpointState{endpoint: e, budget: time.Second}
	t.Cleanup(e.seal)
	return e
}

func TestNativeEndpointEmptySupplyQuery(t *testing.T) {
	e := newPrefixControlEndpoint(t)
	p, err := e.attach(1)
	require.NoError(t, err)
	require.True(t, e.nativeWorkEmpty(p))
	require.False(t, e.nativeWorkEmpty(nil))
	require.False(t, e.nativeWorkEmpty(&endpointParticipant{endpoint: e, id: p.id, slot: p.slot, active: true}))

	var admission endpointAdmission
	e.do(func(s *endpointState) { admission, err = s.admit(p, 0, 64) })
	require.NoError(t, err)
	require.Equal(t, 1, e.state.count)
	require.True(t, e.nativeWorkEmpty(p), "reserved capacity is not current or queued native work")
	e.do(func(s *endpointState) { s.cancelAdmission(admission) })

	request := endpointTestRequest(p.id, "canonical native work")
	buffer := request.members[0].buffer
	require.NoError(t, e.offer(p, 0, request))
	select {
	case <-e.work:
	default:
	}
	select {
	case <-e.progress:
	default:
	}
	require.False(t, e.nativeWorkEmpty(p), "an admitted queue is busy before any claim")
	require.Len(t, e.state.queue, 1)
	require.Empty(t, e.work, "the query does not wake the writer")
	require.Empty(t, e.progress, "the query does not notify progress")

	var token *endpointToken
	e.do(func(s *endpointState) { token = s.claim() })
	require.NotNil(t, token)
	require.False(t, e.nativeWorkEmpty(p), "a current native/deadline borrow is busy")
	// Exercise real member execution, then leave its immutable final fact
	// unreconciled under this worker-free control fixture.
	token.fact = endpointWriteResult{result: writeEndpointRequestWithScratch(endpointTestNative{}, token, nil, nil), returnedAt: time.Now()}
	token.completion.Store(&token.fact)
	require.False(t, e.nativeWorkEmpty(p), "publication alone cannot make current empty")
	require.Same(t, token, e.state.current)
	require.Empty(t, p.results, "the query cannot reconcile or detach a fact")
	require.Equal(t, 1, buffer.refCount)
	e.do(func(*endpointState) {})
	require.Nil(t, e.state.current)
	require.Len(t, p.results, 1)
	require.Equal(t, 1, p.retained)
	require.True(t, e.nativeWorkEmpty(p), "a reconciled terminal result is no longer native work")
	require.Equal(t, 1, p.retained, "observation does not consume the terminal record")
	_, ok := e.take(p)
	require.True(t, ok)
	e.drain(p)
	require.False(t, e.nativeWorkEmpty(p), "a draining participant cannot select a prefix")
	e.detach(p)
	require.False(t, e.nativeWorkEmpty(p), "a detached participant is not usable")

	for _, ending := range []string{"sealed", "faulted"} {
		t.Run(ending, func(t *testing.T) {
			closed := newPrefixControlEndpoint(t)
			participant, err := closed.attach(2)
			require.NoError(t, err)
			if ending == "sealed" {
				closed.seal()
			} else {
				closed.do(func(s *endpointState) { s.fail(endpointFaultNative, net.ErrClosed) })
			}
			require.False(t, closed.nativeWorkEmpty(participant))
		})
	}
}

func TestNativeSoftwareStaleEmptyObservationUsesOrdinaryAdmission(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		tc := newEmissionTestConnection(t, false)
		c := tc.conn
		e := newPrefixControlEndpoint(t)
		require.NoError(t, c.useNativeOrdinary(e, nativeProbePeer, packetInfo{}))
		sender := c.emission.local.(*nativeOrdinarySender)
		t.Cleanup(sender.Close)
		appendNativeGroupMember(t, tc, sender, getPacketBuffer(), "already handed prefix", protocol.ECNNon, sendMetadata{})
		record := sender.pending
		require.True(t, e.nativeWorkEmpty(sender.participant))
		other, err := e.attach(sender.participant.id ^ (uint64(1) << 63))
		require.NoError(t, err)
		require.NoError(t, e.offer(other, 0, endpointTestRequest(other.id, "competing producer")))
		require.False(t, e.nativeWorkEmpty(sender.participant))
		// This is the admitted race between the two existing owner operations:
		// no reservation was retained by the earlier observation.
		sender.finish()
		require.Equal(t, ordinaryEndpointOwned, record.state)
		require.Equal(t, 2, e.state.count)
		require.Len(t, e.state.queue, 2)
		var first *endpointToken
		e.do(func(s *endpointState) { first = s.claim() })
		require.NotNil(t, first)
		require.Equal(t, sender.participant.id, first.request.identity.connection, "the existing cold round-robin cursor, not enqueue order, selects service")
		first.fact = endpointWriteResult{result: writeEndpointRequestWithScratch(endpointTestNative{}, first, nil, nil), returnedAt: time.Now()}
		first.completion.Store(&first.fact)
		e.do(func(*endpointState) {})
		require.NotEqual(t, ordinaryFree, record.state)
		require.NoError(t, sender.consume())
		require.Equal(t, ordinaryFree, record.state)
		var second *endpointToken
		e.do(func(s *endpointState) { second = s.claim() })
		require.NotNil(t, second)
		require.Equal(t, other.id, second.request.identity.connection)
		second.fact = endpointWriteResult{result: writeEndpointRequestWithScratch(endpointTestNative{}, second, nil, nil), returnedAt: time.Now()}
		second.completion.Store(&second.fact)
		e.do(func(*endpointState) {})
		result, ok := e.take(other)
		require.True(t, ok)
		require.NoError(t, result.failure())
		require.Equal(t, 1, result.members[0].accepted)
	})
}
