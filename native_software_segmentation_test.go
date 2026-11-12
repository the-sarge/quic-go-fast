//go:build darwin || linux || freebsd

package quic

import (
	"bytes"
	"testing"
	"testing/synctest"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

func TestE21ScalarStreamUsesSegmentedStorage(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		tc, sender, socket := nativeProbeRecoveryConnection(t, "reno")
		socket.hold = nativeProbeHoldAll
		c := tc.conn
		c.connFlowController.UpdateSendWindow(1 << 20)
		flow := newStreamFlowController(0, c.connFlowController, 1<<20, 1<<20, 1<<20, c.flowControlRTT(), c.logger)
		stream := newSendStream(c.ctx, 0, c, flow, false)
		require.NoError(t, stream.TryWriteAll(bytes.Repeat([]byte("stream"), 1000)))
		result := c.emission.advanceForPlatform(monotime.Now(), false, true)
		require.NoError(t, result.err)
		r := sender.pending
		require.NotNil(t, r)
		require.Greater(t, r.count, 1, "real stream construction registered each packet immediately")
		require.Len(t, r.members, 1, "naturally available packets share one physical buffer")
		require.Equal(t, r.count, r.datagrams)
		require.False(t, r.request.members[0].nativeGSO)
		require.NotZero(t, r.request.members[0].gsoSize)
		packets := r.count
		sender.finish()
		socket.release()
		synctest.Wait()
		require.NoError(t, sender.consume())
		require.Len(t, socket.writes, packets, "existing writer still submits one API call per datagram")
		for _, write := range socket.writes {
			require.LessOrEqual(t, len(write.data), 1200)
		}
	})
}

// The longer third packet cannot join the first short member. It must still
// start a new member in order, and a shorter fourth packet may end that member.
func TestNativeSoftwareShortDatagramGrouping(t *testing.T) {
	t.Setenv(disableShortGSOSegmentsEnv, "false")
	nativeOrdinaryTest(t, func(t *testing.T) {
		tc, sender, socket := nativeSoftwareBoundaryConnection(t)
		c := tc.conn
		payloads := gsoTestPayloads(900, 900, 950, 900)
		queueGSOTestDatagrams(t, c, payloads)
		result := c.emission.advanceForPlatform(monotime.Now(), false, true)
		require.NoError(t, result.err)
		r := sender.pending
		require.NotNil(t, r)
		require.Equal(t, 4, r.count, "a continuation miss consumes no packet number or DATAGRAM")
		require.Equal(t, 4, r.datagrams)
		require.Len(t, r.request.members, 2)
		for i, member := range r.request.members {
			require.Equal(t, 2, member.datagrams())
			require.False(t, member.nativeGSO)
			require.NotZero(t, member.gsoSize)
			require.Nil(t, member.protection, "software protection remains synchronous")
			if i == 1 {
				require.Less(t, len(member.buffer.Data), int(member.gsoSize)*2, "the final segment can be shorter")
			}
		}
		require.Nil(t, c.datagramQueue.Peek())
		sender.finish()
		synctest.Wait()
		require.NoError(t, sender.consume())
		require.Len(t, socket.writes, len(payloads), "software grouping preserves scalar socket submissions")
		for i, write := range socket.writes {
			p := decodeGSOTestPacket(t, c.connIDManager.Get().Len(), write.data)
			require.Equal(t, [][]byte{payloads[i]}, p.datagrams)
		}
		require.Equal(t, ordinaryFree, r.state)
	})
}

// Keep real registration and native outcome owners. The failed suffix owns a
// token but no handed buffer, so it must not consume the last physical slot.
func TestNativeSoftwarePhysicalAllowanceAcrossTransferAndFailedSuffix(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		tc, sender, socket := nativeProbeRecoveryConnection(t, "reno")
		socket.hold = nativeProbeHoldAll
		c := tc.conn
		for range endpointBatch - 1 {
			appendNativeGroupMember(t, tc, sender, getPacketBuffer(), "one physical buffer", protocol.ECNNon, sendMetadata{})
		}
		r := sender.pending
		require.Equal(t, 31, r.datagrams)
		failed := getLargePacketBuffer()
		require.True(t, sender.reserveBuffer(failed.Cap()).available)
		require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("failed buffer prefix")}))
		_, err := c.emission.appendPacket(failed, 1200, false, protocol.ECNNon, monotime.Now())
		require.NoError(t, err)
		failed.Release() // same unhanded registered suffix as a later construction failure
		sender.finish()
		require.Equal(t, ordinaryEndpointOwned, r.state)
		require.Equal(t, 32, r.count)
		require.True(t, sender.softwarePacketRoom(), "a frozen failed suffix is not a physical member")
		// A larger second packet needs another physical member. The available
		// slot is enough for the first, but not for the larger continuation.
		for _, payload := range []string{"last allowed packet", "larger packet must remain queued"} {
			require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte(payload)}))
		}
		result := c.emission.advanceForPlatform(monotime.Now(), false, true)
		require.NoError(t, result.err)
		require.Equal(t, emissionQueueFull, result.stop)
		require.NotNil(t, c.datagramQueue.Peek())
		require.Equal(t, "larger packet must remain queued", string(c.datagramQueue.Peek().Data))
		require.Equal(t, 1, sender.pending.datagrams)
		reservation := sender.reserveBuffer(protocol.MaxPacketBufferSize)
		require.False(t, reservation.available)
		require.True(t, reservation.finished, "physical exhaustion finishes the active builder")
		require.True(t, sender.WouldBlock(), "endpoint-owned members retain all physical slots")
		socket.release()
		synctest.Wait()
		require.True(t, sender.WouldBlock(), "native publication is not terminal consumption")
		require.NoError(t, sender.consume())
		require.EqualValues(t, 1, sender.droppedDatagrams)
		require.False(t, sender.WouldBlock(), "final consumption retires the sidecar charge")
	})
}

func TestE21ScalarReceiveBoundaryAndUnsupportedDomain(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		tc, sender, socket := nativeProbeRecoveryConnection(t, "reno")
		c := tc.conn
		for _, payload := range []string{"first packet", "next packet"} {
			require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte(payload)}))
		}
		c.receivedPackets.PushBack(receivedPacket{})
		result := c.emission.advanceForPlatform(monotime.Now(), false, true)
		require.NoError(t, result.err)
		require.Equal(t, emissionReceivePending, result.stop)
		require.EqualValues(t, 1, c.connStats.PacketsSent.Load())
		require.Equal(t, "next packet", string(c.datagramQueue.Peek().Data))
		sender.finish()
		synctest.Wait()
		require.NoError(t, sender.consume())
		require.Len(t, socket.writes, 1)
		c.receivedPackets.PopFront()
		sender.writeState.gsoDisabled.Store(true)
		require.False(t, sender.canSoftwareSegment(), "an error-latched tuple keeps its old constructor")
		sender.writeState.gsoDisabled.Store(false)
		sender.generation++
		require.False(t, sender.canSoftwareSegment(), "only the original ordinary generation is selected")
	})
}

func TestE21CoalescedHandshakeKeepsDatagramCharge(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		tc := newHandshakeEmissionConnection(t, false)
		c := tc.conn
		socket := newNativeProbeSocket()
		socket.hold = nativeProbeHoldAll
		endpoint := startNativeProbeEndpoint(t, socket)
		require.NoError(t, c.useNativeOrdinary(endpoint, nativeProbePeer, packetInfo{}))
		sender := c.emission.local.(*nativeOrdinarySender)
		t.Cleanup(sender.Close)
		_, err := c.initialStream.Write([]byte("Initial"))
		require.NoError(t, err)
		_, err = c.handshakeStream.Write([]byte("Handshake"))
		require.NoError(t, err)
		require.True(t, c.emission.coalesced(monotime.Now()).progress)
		var found bool
		for i := range sender.records {
			r := &sender.records[i]
			if r.state != ordinaryFree {
				found = true
				require.Greater(t, r.count, 1)
				require.Equal(t, 1, r.datagrams, "coalesced registration count is not datagram allowance")
			}
		}
		require.True(t, found)
		require.False(t, sender.canSoftwareSegment())
		socket.release()
		synctest.Wait()
		require.NoError(t, sender.consume())
	})
}
