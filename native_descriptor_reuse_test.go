//go:build darwin || linux || freebsd

package quic

import (
	"bytes"
	"net/netip"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEndpointDescriptorsReuseAfterFinalReturn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		setup, reset := make(chan struct{}), make(chan struct{})
		wantOOB := []byte("old control")
		e := startNativeEndpoint(endpointResponseTestNative{
			endpointTestNative: endpointTestNative{deadline: func(deadline time.Time) error {
				if deadline.IsZero() {
					<-reset
				} else {
					<-setup
				}
				return nil
			}},
			sendMetadata: func(data, oob []byte, _ netip.AddrPort) (int, int, error) {
				assert.Equal(t, wantOOB, oob)
				return len(data), len(oob), nil
			},
		}, time.Second, nil)
		defer func() { close(setup); close(reset); <-e.stop() }()
		p, err := e.attach(1)
		require.NoError(t, err)
		values := make([]string, endpointBatch)
		for i := range values {
			values[i] = "old member"
		}
		request := endpointTestRequest(1, values...)
		request.identity.registration = 11
		control := bytes.Clone(wantOOB)
		buffers := make([]*packetBuffer, len(request.members))
		for i := range request.members {
			request.members[i].oob = control
			buffers[i] = request.members[i].buffer
		}
		require.NoError(t, e.offer(p, 0, request))
		synctest.Wait()
		var token *endpointToken
		var descriptors []endpointMember
		e.do(func(s *endpointState) {
			token = s.current
			if !assert.NotNil(t, token) {
				return
			}
			descriptors = token.request.members
			assert.Len(t, descriptors, endpointBatch)
			assert.Equal(t, endpointBatch, cap(descriptors), "maximum valid requests retain bounded backing")
			assert.NotSame(t, &request.members[0], &descriptors[0])
		})
		if t.Failed() {
			return
		}
		clear(control)
		clear(request.members)
		setup <- struct{}{}
		synctest.Wait()
		_, ok := e.take(p)
		require.False(t, ok, "a returned member cannot end the outstanding deadline-reset borrow")
		e.do(func(s *endpointState) {
			assert.Same(t, token, s.current)
			assert.Nil(t, token.completion.Load())
			assert.Same(t, &descriptors[0], &token.request.members[0])
			for i, member := range descriptors {
				assert.Same(t, buffers[i], member.buffer)
				assert.Equal(t, 1, member.buffer.refCount)
				assert.Equal(t, wantOOB, member.oob)
			}
		})
		reset <- struct{}{}
		synctest.Wait()
		fact := token.completion.Load()
		require.NotNil(t, fact)
		require.Nil(t, token.request)
		factSnapshot := *fact
		factSnapshot.result.members = slices.Clone(fact.result.members)
		e.do(func(s *endpointState) {
			r := &s.records[token.index]
			assert.True(t, r.used, "an unconsumed result still owns its record")
			assert.True(t, r.terminal)
			assert.Empty(t, r.request.members)
			assert.Equal(t, endpointBatch, cap(r.request.members))
			for _, member := range descriptors {
				assert.Equal(t, endpointMember{}, member, "publication clears every descriptor before retaining storage")
			}
		})
		firstResult, ok := e.take(p)
		require.True(t, ok)
		require.NoError(t, firstResult.failure())
		firstSnapshot := firstResult
		firstSnapshot.members = slices.Clone(firstResult.members)
		for _, buffer := range buffers {
			require.Zero(t, buffer.refCount)
		}
		wantOOB = []byte("new control")
		successor := endpointTestRequest(1, "successor")
		successor.identity.registration = 22
		successor.members[0].oob = bytes.Clone(wantOOB)
		require.NoError(t, e.offer(p, 0, successor))
		synctest.Wait()
		e.do(func(s *endpointState) {
			assert.NotSame(t, token, s.current)
			assert.Equal(t, token.index, s.current.index)
			assert.Same(t, &descriptors[0], &s.current.request.members[0], "only the retired canonical record reuses its own backing")
			for _, member := range descriptors[1:] {
				assert.Equal(t, endpointMember{}, member, "a shorter successor cannot retain stale tail references")
			}
		})
		clear(successor.members[0].oob)
		clear(successor.members)
		setup <- struct{}{}
		synctest.Wait()
		reset <- struct{}{}
		synctest.Wait()
		secondResult, ok := e.take(p)
		require.True(t, ok)
		require.NoError(t, secondResult.failure())
		require.Equal(t, uint64(22), secondResult.identity.registration)
		require.NotSame(t, &firstResult.members[0], &secondResult.members[0])
		require.Equal(t, firstSnapshot, firstResult)
		require.Equal(t, factSnapshot, *fact, "retaining old immutable facts cannot alias successor storage")
	})
}

func TestEndpointDescriptorStorageBound(t *testing.T) {
	var empty endpointRecord
	empty.reset()
	require.Nil(t, empty.request.members, "idle records allocate no backing")
	for _, capacity := range []int{endpointBatch, endpointBatch + 1} {
		backing := make([]endpointMember, capacity)
		for i := range backing {
			backing[i] = endpointMember{
				protection: new(nativeProtectionPlan), buffer: new(packetBuffer), datagram: new(endpointDatagram),
				oob: []byte("old OOB"), gsoSize: 1200, nativeGSO: true, writeState: new(endpointWriteState), credit: new(sendReservation),
			}
		}
		r := endpointRecord{used: true, terminal: true, serial: 42, participant: new(endpointParticipant), bytes: 99, request: endpointRequest{
			identity: endpointIdentity{connection: 1}, members: backing[:1], epoch: new(responseEpoch), deadline: time.Now(),
		}, result: endpointResult{members: []endpointMemberResult{{accepted: 1}}}}
		r.reset()
		for _, member := range backing {
			require.Equal(t, endpointMember{}, member, "clearing covers capacity beyond the old visible length")
		}
		if capacity == endpointBatch {
			require.Empty(t, r.request.members)
			require.Equal(t, endpointBatch, cap(r.request.members))
			require.Same(t, &backing[0], &r.request.members[:cap(r.request.members)][0])
		} else {
			require.Nil(t, r.request.members, "over-capacity backing is cleared and discarded")
		}
		r.request.members = nil
		require.Equal(t, endpointRecord{}, r, "storage retention carries no identity, payload or result authority")
	}
}
