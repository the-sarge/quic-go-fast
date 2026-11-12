//go:build darwin || linux || freebsd

package quic

import (
	"bytes"
	"os"
	"syscall"
	"testing"
	"testing/synctest"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

func TestNativeSoftwareBuffersSurvivePrimaryReplacement(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		tc, sender, socket := nativeProbeRecoveryConnection(t, "reno")
		socket.hold = nativeProbeHoldAll
		c := tc.conn
		_, pnLen := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
		payloadSize := 1200 - int(wire.ShortHeaderLen(c.connIDManager.Get(), pnLen)) - 7 - 3
		buffer := getLargePacketBuffer()
		require.True(t, sender.reserveBuffer(buffer.Cap()).available)
		for range 6 {
			require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: bytes.Repeat([]byte("x"), payloadSize)}))
			_, err := c.emission.appendPacket(buffer, 1200, false, protocol.ECNNon, monotime.Now())
			require.NoError(t, err)
		}
		sender.Send(buffer, 1200, protocol.ECNNon, sendMetadata{})
		r := sender.pending
		require.Len(t, r.members, 1)
		require.Equal(t, 6, r.datagrams)
		sender.finish()
		synctest.Wait()
		// Recovery migrates before the sender: old local identity and native
		// borrow remain live while newly built buffers use the replacement.
		c.sentPacketHandler.MigratedPath(monotime.Now(), 1200)
		targetSocket := newNativeProbeSocket()
		target := &Transport{responses: newEndpointResponses(startNativeProbeEndpoint(t, targetSocket))}
		c.emission.replaceLocalPath(target, tc.sendConn)
		require.NotZero(t, sender.switchGeneration)
		for range endpointBatch - 1 {
			appendNativeGroupMember(t, tc, sender, getPacketBuffer(), "new scalar packet", protocol.ECNNon, sendMetadata{})
		}
		require.Equal(t, endpointBatch-1, sender.pending.datagrams)
		require.True(t, sender.WouldBlock(), "old software buffer plus new scalar buffers retain 32 physical slots")
		require.Equal(t, 1, buffer.refCount)
		socket.release()
		synctest.Wait()
		require.True(t, sender.WouldBlock(), "actual return/publication is not sidecar consumption")
		require.NoError(t, sender.consume())
		require.Zero(t, buffer.refCount)
		require.False(t, sender.WouldBlock())
	})
}

func TestE23HardwareFallbackKeepsOneSlotAcrossReplacement(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		tc := newEmissionTestConnection(t, true)
		c := tc.conn
		socket := newNativeProbeSocket()
		socket.hold = func(data []byte) bool { return len(data) <= 1200 }
		var fallback bool
		socket.result = func(data []byte) (int, error) {
			if !fallback && len(data) > 1200 {
				fallback = true
				return 0, &os.SyscallError{Syscall: "sendmsg", Err: syscall.EIO}
			}
			return len(data), nil
		}
		endpoint := startNativeProbeEndpoint(t, socket)
		require.NoError(t, c.useNativeOrdinary(endpoint, nativeProbePeer, packetInfo{}))
		sender := c.emission.local.(*nativeOrdinarySender)
		sender.nativeGSO = true // concrete native-GSO/EIO behavior on every fixture platform
		t.Cleanup(sender.Close)
		_, pnLen := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
		payloadSize := 1200 - int(wire.ShortHeaderLen(c.connIDManager.Get(), pnLen)) - 7 - 3
		for range 6 {
			require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: bytes.Repeat([]byte("x"), payloadSize)}))
		}
		require.NoError(t, c.emission.withGSO(monotime.Now()).err)
		r := sender.pending
		require.Len(t, r.members, 1)
		require.Equal(t, 6, r.datagrams)
		require.False(t, r.request.shortSoftwareDatagramGroup, "hardware GSO does not mark a request")
		sender.finish()
		synctest.Wait()
		require.True(t, fallback)
		require.True(t, sender.writeState.gsoDisabled.Load())
		c.sentPacketHandler.MigratedPath(monotime.Now(), 1200)
		targetSocket := newNativeProbeSocket()
		target := &Transport{responses: newEndpointResponses(startNativeProbeEndpoint(t, targetSocket))}
		c.emission.replaceLocalPath(target, tc.sendConn)
		for range 31 {
			appendNativeGroupMember(t, tc, sender, getPacketBuffer(), "new scalar packet", protocol.ECNNon, sendMetadata{})
		}
		require.Equal(t, 31, sender.pending.datagrams, "old hardware batch costs one, even after scalar fallback")
		require.True(t, sender.WouldBlock())
		socket.release()
		synctest.Wait()
		require.True(t, sender.WouldBlock(), "immutable cost remains through publication")
		require.NoError(t, sender.consume())
		require.False(t, sender.WouldBlock())
	})
}

func TestNativeSegmentedRegistrationMismatchSettles(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		tc, sender, socket := nativeProbeRecoveryConnection(t, "reno")
		buffer := getPacketBuffer()
		require.True(t, sender.reserveBuffer(buffer.Cap()).available)
		require.NoError(t, tc.conn.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("one registered packet")}))
		_, err := tc.conn.emission.appendPacket(buffer, 1200, false, protocol.ECNNon, monotime.Now())
		require.NoError(t, err)
		// Two scalar datagrams cannot describe one registered short-header packet.
		sender.Send(buffer, uint16(buffer.Len()/2), protocol.ECNNon, sendMetadata{})
		require.ErrorIs(t, sender.err, errEndpointRequest)
		require.Nil(t, sender.pending)
		require.Zero(t, buffer.refCount)
		require.EqualValues(t, 1, sender.droppedDatagrams)
		require.Empty(t, socket.writes)
		for i := range sender.records {
			r := &sender.records[i]
			require.Equal(t, ordinaryFree, r.state)
			for _, member := range r.members[:cap(r.members)] {
				require.Zero(t, member, "settlement clears every retained member reference")
			}
		}
	})
}
