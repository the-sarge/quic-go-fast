//go:build darwin || linux || freebsd

package quic

import (
	"bytes"
	"net"
	"net/netip"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
)

func TestNativeEndpointMixedMemberGeometry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		destination := netip.MustParseAddrPort("127.0.0.1:1234")
		info := packetInfo{addr: netip.MustParseAddr("127.0.0.2")}
		oob := [][]byte{
			packetOOB(nil, destination, protocol.ECT0),
			packetOOB(info.OOB(), destination, protocol.ECT1),
			packetOOB(nil, destination, protocol.ECNCE),
		}
		var payloads []string
		var metadata [][]byte
		var returned []monotime.Time
		e := startNativeEndpoint(endpointResponseTestNative{sendMetadata: func(data, control []byte, address netip.AddrPort) (int, int, error) {
			require.Equal(t, destination, address)
			payloads = append(payloads, string(data))
			metadata = append(metadata, bytes.Clone(control))
			time.Sleep(time.Millisecond)
			returned = append(returned, monotime.Now())
			return len(data), len(control), nil
		}}, time.Second, nil)
		defer func() { <-e.stop() }()
		p, err := e.attach(1)
		require.NoError(t, err)
		request := endpointTestRequest(1, "aaa", "bbbbc", "c")
		request.members[0].gsoSize = 2
		request.members[1].gsoSize, request.members[1].nativeGSO = 2, true
		request.members[1].writeState = &endpointWriteState{}
		for i := range request.members {
			request.members[i].oob = bytes.Clone(oob[i])
		}
		require.NoError(t, e.offer(p, 0, request))
		// Descriptor storage and OOB are snapshotted; payload ownership transfers.
		request.members[0].gsoSize = 1
		clear(request.members[0].oob)
		<-p.completed
		result, ok := e.take(p)
		require.True(t, ok)
		require.NoError(t, result.failure())
		require.Equal(t, []string{"aa", "a", "bbbbc", "c"}, payloads)
		require.Equal(t, [][]byte{oob[0], oob[0], appendUDPSegmentSizeMsg(bytes.Clone(oob[1]), 2), oob[2]}, metadata)
		require.Len(t, result.members, 3)
		for i, accepted := range []int{2, 3, 1} {
			require.Equal(t, accepted, result.members[i].accepted)
			require.Zero(t, result.members[i].uncertain)
			require.Zero(t, result.members[i].unsubmitted)
		}
		require.Equal(t, returned[1], result.members[0].returnedAt)
		require.Equal(t, returned[2], result.members[1].returnedAt)
		require.Equal(t, returned[3], result.members[2].returnedAt)
	})
}

func TestNativeEndpointPartialMemberLeavesUntouchedSuffix(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var payloads []string
		e := startNativeEndpoint(endpointTestNative{send: func(data []byte) (int, error) {
			payloads = append(payloads, string(data))
			time.Sleep(time.Millisecond)
			if len(payloads) == 3 {
				return 0, syscall.ECONNREFUSED
			}
			return len(data), nil
		}}, time.Second, nil)
		defer func() { <-e.stop() }()
		p, err := e.attach(1)
		require.NoError(t, err)
		request := endpointTestRequest(1, "prefix", "aabbcc", "untouched")
		request.members[1].gsoSize = 2
		require.NoError(t, e.offer(p, 0, request))
		<-p.completed
		result, ok := e.take(p)
		require.True(t, ok)
		require.Equal(t, []string{"prefix", "aa", "bb"}, payloads)
		require.Equal(t, 1, result.members[0].accepted)
		require.NoError(t, result.members[0].err)
		require.Equal(t, 1, result.members[1].accepted)
		require.Equal(t, 1, result.members[1].uncertain)
		require.Equal(t, 1, result.members[1].unsubmitted)
		require.ErrorIs(t, result.members[1].err, syscall.ECONNREFUSED)
		require.Zero(t, result.members[2].accepted)
		require.Zero(t, result.members[2].uncertain)
		require.Equal(t, 1, result.members[2].unsubmitted)
		require.NoError(t, result.members[2].err)
		require.Greater(t, result.members[1].returnedAt, result.members[0].returnedAt)
		require.GreaterOrEqual(t, result.members[2].returnedAt, result.members[1].returnedAt)
	})
}

func TestNativeEndpointMemberBorrowAndCreditSurviveCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		writes := 0
		e := startNativeEndpoint(endpointTestNative{send: func(data []byte) (int, error) {
			writes++
			if writes == 2 {
				<-release
			}
			return len(data), nil
		}}, time.Second, nil)
		defer func() { <-e.stop() }()
		p, err := e.attach(1)
		require.NoError(t, err)
		request := endpointTestRequest(1, "accepted", "borrowed", "canceled")
		credit := newLocalSendCredit()
		for i := range request.members {
			request.members[i].credit = credit.reserve(1, 3, false, false)
			require.NotNil(t, request.members[i].credit)
		}
		require.NoError(t, e.offer(p, 0, request))
		synctest.Wait()
		e.cancelGeneration(p, request.identity.generation)
		synctest.Wait()
		for _, member := range request.members {
			require.Equal(t, 1, member.buffer.refCount, "earlier member acceptance cannot release payload")
		}
		require.EqualValues(t, 3, credit.pending)
		select {
		case <-credit.available:
			t.Fatal("cancellation returned credit before the native borrow ended")
		default:
		}
		close(release)
		<-p.completed
		result, ok := e.take(p)
		require.True(t, ok)
		require.Equal(t, 2, writes)
		require.Equal(t, 1, result.members[0].accepted)
		require.Equal(t, 1, result.members[1].accepted)
		require.NoError(t, result.members[0].err)
		require.NoError(t, result.members[1].err)
		require.Equal(t, 1, result.members[2].unsubmitted)
		require.ErrorIs(t, result.members[2].err, errEndpointRevoked)
		for _, member := range request.members {
			require.Zero(t, member.buffer.refCount)
		}
		require.Zero(t, credit.pending)
		<-credit.available
	})
}

func TestNativeEndpointDeadlineMemberFacts(t *testing.T) {
	for _, setup := range []bool{true, false} {
		name := "reset"
		if setup {
			name = "setup"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				writes := 0
				var returned []monotime.Time
				e := startNativeEndpoint(endpointTestNative{
					send: func(data []byte) (int, error) {
						writes++
						time.Sleep(time.Millisecond)
						returned = append(returned, monotime.Now())
						return len(data), nil
					},
					deadline: func(deadline time.Time) error {
						if deadline.IsZero() {
							time.Sleep(time.Millisecond)
						}
						if setup == !deadline.IsZero() {
							return net.ErrClosed
						}
						return nil
					},
				}, time.Second, nil)
				defer func() { <-e.stop() }()
				p, err := e.attach(1)
				require.NoError(t, err)
				require.NoError(t, e.offer(p, 0, endpointTestRequest(1, "first", "second")))
				<-p.completed
				result, ok := e.take(p)
				require.True(t, ok)
				require.ErrorIs(t, result.failure(), net.ErrClosed)
				require.Len(t, result.members, 2)
				if setup {
					require.Zero(t, writes)
					for _, member := range result.members {
						require.Equal(t, 1, member.unsubmitted)
						require.ErrorIs(t, member.err, net.ErrClosed)
						require.False(t, member.returnedAt.IsZero())
					}
				} else {
					require.Equal(t, 2, writes)
					require.ErrorIs(t, result.err, net.ErrClosed)
					for i, member := range result.members {
						require.Equal(t, 1, member.accepted)
						require.NoError(t, member.err, "reset cannot rewrite an accepted member")
						require.Equal(t, returned[i], member.returnedAt)
					}
				}
				select {
				case <-e.fault:
				default:
					t.Fatal("deadline failure must fault the endpoint")
				}
			})
		})
	}
}
