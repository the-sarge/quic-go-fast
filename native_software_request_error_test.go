//go:build darwin || linux || freebsd

package quic

import (
	"bytes"
	"os"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

type nativeSoftwareFenceCompletion struct {
	token   ackhandler.LocalCompletionToken
	outcome ackhandler.LocalPacketOutcome
	at      monotime.Time
}

type nativeSoftwareFenceObserver struct {
	ackhandler.LocalSentPacketHandler
	completed []nativeSoftwareFenceCompletion
}

func (o *nativeSoftwareFenceObserver) CompleteLocalPacket(token ackhandler.LocalCompletionToken, outcome ackhandler.LocalPacketOutcome, at monotime.Time) error {
	o.completed = append(o.completed, nativeSoftwareFenceCompletion{token: token, outcome: outcome, at: at})
	return o.LocalSentPacketHandler.CompleteLocalPacket(token, outcome, at)
}

func TestNativeSoftwareIndependentRequestErrorKeepsFinalFenceAndOrder(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		resetEntered, releaseReset, successorReset := make(chan struct{}), make(chan struct{}), make(chan struct{})
		unblockReset := func() {
			select {
			case <-releaseReset:
			default:
				close(releaseReset)
			}
		}
		var writes [][]byte
		var writeTimes []monotime.Time
		var resets int
		var firstResetReturned monotime.Time
		cause := &os.SyscallError{Syscall: "sendmsg", Err: syscall.ENOBUFS}
		endpoint := startNativeEndpoint(endpointTestNative{
			send: func(data []byte) (int, error) {
				writes = append(writes, bytes.Clone(data))
				time.Sleep(time.Millisecond)
				writeTimes = append(writeTimes, monotime.Now())
				if len(writes) == 2 {
					return 0, cause
				}
				return len(data), nil
			},
			deadline: func(deadline time.Time) error {
				if deadline.IsZero() {
					resets++
					if resets == 1 {
						close(resetEntered)
						<-releaseReset
						firstResetReturned = monotime.Now()
					} else {
						close(successorReset)
					}
				}
				return nil
			},
		}, 100*time.Millisecond, nil)
		t.Cleanup(func() { unblockReset(); <-endpoint.stop() })

		// The same concrete recovery setup as nativeProbeRecoveryConnection,
		// with only the native boundary replaced to hold deadline reset.
		conf := &Config{InitialPacketSize: 1200, EnableDatagrams: true}
		require.NoError(t, conf.SetCongestionControlV1("reno"))
		tc := newConfiguredEmissionTestConnection(t, false, conf)
		c := tc.conn
		c.rttStats.UpdateRTT(5*time.Millisecond, 0)
		require.NoError(t, c.useNativeOrdinary(endpoint, nativeProbePeer, packetInfo{}))
		sender := c.emission.local.(*nativeOrdinarySender)
		t.Cleanup(sender.Close)
		observer := &nativeSoftwareFenceObserver{LocalSentPacketHandler: sender.recovery}
		sender.recovery = observer

		scalar := getLargePacketBuffer()
		require.True(t, sender.reserveBuffer(scalar.Cap()).available)
		_, pnLen := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
		payloadSize := 1200 - int(wire.ShortHeaderLen(c.connIDManager.Get(), pnLen)) - 7 - 3
		for _, value := range []byte{0x41, 0x42, 0x43} {
			require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: bytes.Repeat([]byte{value}, payloadSize)}))
			receipt, err := c.emission.appendPacket(scalar, 1200, false, protocol.ECNNon, monotime.Now())
			require.NoError(t, err)
			require.EqualValues(t, 1200, receipt.size)
		}
		sender.Send(scalar, 1200, protocol.ECNNon, sendMetadata{})
		untouched := getPacketBuffer()
		appendNativeGroupMember(t, tc, sender, untouched, "untouched same-request member", protocol.ECNNon, sendMetadata{})
		predecessor := sender.pending
		predecessorIdentity := predecessor.identity
		wantTokens := make([]ackhandler.LocalCompletionToken, 0, 5)
		for _, packet := range predecessor.packets[:predecessor.count] {
			wantTokens = append(wantTokens, packet.token)
		}
		wantWrites := [][]byte{bytes.Clone(scalar.Data[:1200]), bytes.Clone(scalar.Data[1200:2400])}
		sender.finish()
		<-resetEntered
		synctest.Wait()

		var predecessorToken *endpointToken
		endpoint.do(func(state *endpointState) {
			predecessorToken = state.current
			require.NotNil(t, predecessorToken)
			require.Equal(t, predecessorIdentity.registration, predecessorToken.request.identity.registration)
			require.Nil(t, predecessorToken.completion.Load(), "member return does not end the deadline-reset borrow")
			require.Empty(t, sender.participant.results, "no terminal result is published during reset")
		})
		require.Equal(t, ordinaryEndpointOwned, predecessor.state)
		require.Equal(t, 1, scalar.refCount)
		require.Equal(t, 1, untouched.refCount, "an untouched member shares the request's final fence")
		require.Empty(t, observer.completed)
		require.Len(t, writes, 2)

		// A later independent request can be admitted without consuming the
		// predecessor's nonfatal error or ending its native/deadline borrow.
		successor := getPacketBuffer()
		appendNativeGroupMember(t, tc, sender, successor, "independent successor", protocol.ECNNon, sendMetadata{})
		successorRecord := sender.pending
		successorIdentity := successorRecord.identity
		wantTokens = append(wantTokens, successorRecord.packets[0].token)
		wantWrites = append(wantWrites, bytes.Clone(successor.Data))
		require.NotEqual(t, predecessorIdentity.registration, successorIdentity.registration)
		sender.finish()
		require.Equal(t, ordinaryEndpointOwned, successorRecord.state)
		endpoint.do(func(state *endpointState) {
			require.Same(t, predecessorToken, state.current)
			require.Len(t, state.queue, 1)
			queued := state.records[state.queue[0]].request
			require.Equal(t, successorIdentity.registration, queued.identity.registration)
			require.True(t, queued.authorized())
			require.True(t, time.Now().Before(queued.deadline))
			require.Nil(t, predecessorToken.completion.Load())
		})
		require.Empty(t, observer.completed)
		require.Len(t, writes, 2, "the queued successor cannot cross the predecessor's held final reset")
		require.Equal(t, 1, successor.refCount)
		time.Sleep(2 * time.Millisecond)
		unblockReset()
		<-successorReset
		synctest.Wait()

		require.Equal(t, wantWrites, writes, "only the scalar prefix/error attempts and qualified successor reach native I/O")
		require.Empty(t, observer.completed, "writer continuation does not consume Conn recovery facts")
		require.Equal(t, 2, resets)
		require.Zero(t, scalar.refCount)
		require.Zero(t, untouched.refCount)
		require.Zero(t, successor.refCount)
		fact := predecessorToken.completion.Load()
		require.NotNil(t, fact)
		require.Nil(t, predecessorToken.request)
		require.False(t, fact.returnedAt.Before(firstResetReturned.ToTime()))
		require.True(t, fact.returnedAt.After(writeTimes[1].ToTime()))

		var terminals []endpointResult
		endpoint.do(func(state *endpointState) {
			require.Nil(t, state.current)
			require.Empty(t, state.queue)
			require.False(t, state.faulted, "local backpressure is not an endpoint failure")
			for _, index := range sender.participant.results {
				terminals = append(terminals, state.records[index].result)
			}
		})
		require.Len(t, terminals, 2)
		require.Equal(t, predecessorIdentity.registration, terminals[0].identity.registration)
		require.Equal(t, successorIdentity.registration, terminals[1].identity.registration)
		require.Len(t, terminals[0].members, 2)
		first, tail := terminals[0].members[0], terminals[0].members[1]
		require.Equal(t, 1, first.accepted)
		require.Equal(t, 1, first.uncertain)
		require.Equal(t, 1, first.unsubmitted)
		require.ErrorIs(t, first.err, syscall.ENOBUFS)
		require.Equal(t, writeTimes[1], first.returnedAt, "member time is the actual post-sequence return, excluding reset")
		require.True(t, first.returnedAt.After(writeTimes[0]))
		require.Zero(t, tail.accepted)
		require.Zero(t, tail.uncertain)
		require.Equal(t, 1, tail.unsubmitted)
		require.NoError(t, tail.err, "untouched members do not inherit the failed member's error")
		require.False(t, tail.returnedAt.Before(first.returnedAt))
		require.True(t, tail.returnedAt.Before(firstResetReturned))
		require.Len(t, terminals[1].members, 1)
		later := terminals[1].members[0]
		require.Equal(t, 1, later.accepted)
		require.Zero(t, later.uncertain)
		require.Zero(t, later.unsubmitted)
		require.NoError(t, later.err)
		require.Equal(t, writeTimes[2], later.returnedAt)

		require.NoError(t, sender.consume())
		require.Len(t, observer.completed, 5)
		wantOutcomes := []ackhandler.LocalPacketOutcome{
			ackhandler.LocalPacketAccepted, ackhandler.LocalPacketUncertain,
			ackhandler.LocalPacketUnsubmitted, ackhandler.LocalPacketUnsubmitted,
			ackhandler.LocalPacketAccepted,
		}
		wantTimes := []monotime.Time{first.returnedAt, first.returnedAt, first.returnedAt, tail.returnedAt, later.returnedAt}
		for i, completed := range observer.completed {
			require.Equal(t, wantTokens[i], completed.token, "Conn consumes original registration order")
			require.Equal(t, wantOutcomes[i], completed.outcome)
			require.Equal(t, wantTimes[i], completed.at)
		}
		require.EqualValues(t, 2, sender.droppedDatagrams)
		for i := range sender.records {
			require.Equal(t, ordinaryFree, sender.records[i].state)
		}
	})
}
