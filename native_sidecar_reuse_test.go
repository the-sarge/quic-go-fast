//go:build darwin || linux || freebsd

package quic

import (
	"testing"
	"testing/synctest"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
)

func requireE06ClearedSidecar(t *testing.T, r *ordinaryLocalRecord) {
	t.Helper()
	require.Equal(t, ordinaryFree, r.state)
	require.Zero(t, r.count)
	require.Zero(t, r.backing)
	require.Zero(t, r.identity)
	require.Empty(t, r.packets)
	require.Empty(t, r.members)
	require.Empty(t, r.request.members)
	for _, p := range r.packets[:cap(r.packets)] {
		require.Equal(t, ordinaryLocalPacket{}, p)
	}
	for _, member := range r.members[:cap(r.members)] {
		require.Equal(t, ordinaryLocalMember{}, member)
	}
	for _, member := range r.request.members[:cap(r.request.members)] {
		require.Equal(t, endpointMember{}, member)
	}
	require.Equal(t, endpointRequest{}, endpointRequest{
		identity: r.request.identity, destination: r.request.destination, epoch: r.request.epoch, deadline: r.request.deadline,
	})
}

func TestE06SidecarReuseAfterTerminal(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		tc, sender, socket := nativeProbeRecoveryConnection(t, "reno")
		socket.hold = nativeProbeHoldAll
		first, second := getPacketBuffer(), getPacketBuffer()
		appendNativeGroupMember(t, tc, sender, first, "first old member", protocol.ECNNon, sendMetadata{})
		appendNativeGroupMember(t, tc, sender, second, "second old member", protocol.ECNNon, sendMetadata{})
		r := sender.pending
		identity := r.identity
		packets, members, descriptors := r.packets, r.members, r.request.members
		receipt := &pathAbandonReceipt{}
		r.members[0].metadata.terminalReceipts = []*pathAbandonReceipt{receipt}
		sender.finish()
		require.Equal(t, ordinaryEndpointOwned, r.state)
		require.Same(t, &descriptors[0], &r.request.members[:cap(r.request.members)][0])
		for _, descriptor := range descriptors {
			require.Equal(t, endpointMember{}, descriptor, "successful transfer clears only the producer's descriptors")
		}
		synctest.Wait()
		sender.endpoint.do(func(state *endpointState) {
			require.NotNil(t, state.current)
			require.NotSame(t, &descriptors[0], &state.current.request.members[0])
			require.Same(t, first, state.current.request.members[0].buffer)
			require.Same(t, second, state.current.request.members[1].buffer)
			require.NotEmpty(t, state.current.request.members[0].oob)
		})
		require.Equal(t, 1, first.refCount, "private descriptor clearing cannot end the native borrow")
		require.Equal(t, 1, second.refCount)
		socket.release()
		synctest.Wait()
		require.NoError(t, sender.consume())
		require.Zero(t, first.refCount)
		require.Zero(t, second.refCount)
		require.NotZero(t, receipt.acceptedAt())
		requireE06ClearedSidecar(t, r)
		for _, p := range packets {
			require.Equal(t, ordinaryLocalPacket{}, p, "old token copies are cleared, not recycled as identities")
		}
		for _, member := range members {
			require.Equal(t, ordinaryLocalMember{}, member)
		}

		appendNativeGroupMember(t, tc, sender, getPacketBuffer(), "fresh member", protocol.ECNNon, sendMetadata{})
		require.Same(t, r, sender.pending)
		require.NotEqual(t, identity, r.identity)
		require.Same(t, &packets[0], &r.packets[0])
		require.Same(t, &members[0], &r.members[0])
		require.Same(t, &descriptors[0], &r.request.members[0], "first-member initialization retains cleared backing")
		sender.finish()
		synctest.Wait()
		require.NoError(t, sender.consume())
		require.Len(t, socket.writes, 3)
		require.Contains(t, string(socket.writes[2].data), "fresh member")
		requireE06ClearedSidecar(t, r)
	})
}

type e06CompletionObserver struct {
	ackhandler.LocalSentPacketHandler
	onComplete func()
}

func (o *e06CompletionObserver) CompleteLocalPacket(token ackhandler.LocalCompletionToken, outcome ackhandler.LocalPacketOutcome, at monotime.Time) error {
	err := o.LocalSentPacketHandler.CompleteLocalPacket(token, outcome, at)
	callback := o.onComplete
	o.onComplete = nil
	if callback != nil {
		callback()
	}
	return err
}

func TestE06SidecarTerminalReentry(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		tc, sender, _ := nativeProbeRecoveryConnection(t, "reno")
		appendNativeGroupMember(t, tc, sender, getPacketBuffer(), "terminal member", protocol.ECNNon, sendMetadata{})
		r := sender.pending
		packets := r.packets
		var replacement *ordinaryLocalRecord
		observer := &e06CompletionObserver{LocalSentPacketHandler: sender.recovery}
		observer.onComplete = func() {
			require.Equal(t, ordinaryTerminalProcessing, r.state)
			require.NotZero(t, packets[0].token, "the original sidecar is still in use during completion")
			require.NoError(t, sender.consume(), "recursive consume cannot apply terminal facts again")
			appendNativeGroupMember(t, tc, sender, getPacketBuffer(), "reentrant member", protocol.ECNNon, sendMetadata{})
			replacement = sender.pending
			require.NotSame(t, r, replacement)
			require.NotSame(t, &packets[0], &replacement.packets[0])
		}
		sender.recovery = observer
		sender.finish()
		synctest.Wait()
		require.NoError(t, sender.consume())
		require.NotNil(t, replacement)
		require.Same(t, replacement, sender.pending)
		require.Equal(t, ordinaryBuilding, replacement.state)
		require.Len(t, replacement.packets, 1)
		requireE06ClearedSidecar(t, r)
		sender.finish()
		synctest.Wait()
		require.NoError(t, sender.consume())
		requireE06ClearedSidecar(t, replacement)
	})
}

func TestE06SidecarLocalSettlementAndClose(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		tc, sender, socket := nativeProbeRecoveryConnection(t, "reno")
		buffer := getPacketBuffer()
		appendNativeGroupMember(t, tc, sender, buffer, "never admitted", protocol.ECNNon, sendMetadata{})
		r := sender.pending
		packets, members, descriptors := r.packets, r.members, r.request.members
		sender.settleLocal(r, errEndpointRevoked)
		require.Nil(t, sender.pending)
		require.Zero(t, buffer.refCount)
		require.Empty(t, socket.writes)
		requireE06ClearedSidecar(t, r)
		buffer = getPacketBuffer()
		appendNativeGroupMember(t, tc, sender, buffer, "close in completion", protocol.ECNNon, sendMetadata{})
		require.Same(t, r, sender.pending)
		require.Same(t, &packets[0], &r.packets[0])
		require.Same(t, &members[0], &r.members[0])
		require.Same(t, &descriptors[0], &r.request.members[0])
		closed := false
		observer := &e06CompletionObserver{LocalSentPacketHandler: sender.recovery}
		observer.onComplete = func() {
			require.Equal(t, ordinaryTerminalProcessing, r.state)
			require.Zero(t, buffer.refCount, "local release precedes packet completion")
			sender.Close()
			closed = true
		}
		sender.recovery = observer
		sender.settleLocal(r, errEndpointRevoked)
		require.True(t, closed)
		require.True(t, sender.closed)
		require.Nil(t, sender.pending)
		require.Empty(t, socket.writes)
		for _, record := range sender.records {
			require.Equal(t, ordinaryLocalRecord{}, record, "post-callback reset cannot restore Close's discarded backing")
		}
	})
}
