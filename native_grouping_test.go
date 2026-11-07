//go:build darwin || linux || freebsd

package quic

import (
	"errors"
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
	"go.uber.org/mock/gomock"
)

func TestNativeNaturalTurnGroupsRealEmission(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		tc, sender, socket := nativeProbeRecoveryConnection(t, "reno")
		socket.hold = nativeProbeHoldAll
		for _, payload := range []string{"first member", "second member", "third member"} {
			require.NoError(t, tc.conn.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte(payload)}))
		}
		result := tc.conn.emission.send(monotime.Now(), true)
		require.NoError(t, result.err)
		require.True(t, result.progress)
		var records, packets int
		for i := range sender.records {
			r := &sender.records[i]
			if r.state != ordinaryFree {
				records++
				packets += r.count
			}
		}
		require.Equal(t, 1, records, "the natural emission turn has one canonical request")
		require.Equal(t, 3, packets, "every packet is immediately registered")
		socket.release()
		synctest.Wait()
		require.NoError(t, sender.consume())
		require.Len(t, socket.writes, 3)
		for i := range sender.records {
			require.Equal(t, ordinaryFree, sender.records[i].state)
		}
	})
}

func appendNativeGroupMember(t *testing.T, tc *testConnection, sender *nativeOrdinarySender, buf *packetBuffer, payload string, ecn protocol.ECN, metadata sendMetadata) {
	t.Helper()
	require.True(t, sender.reserveBuffer(buf.Cap()))
	require.NoError(t, tc.conn.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte(payload)}))
	_, err := tc.conn.emission.appendPacket(buf, 1200, false, ecn, monotime.Now())
	require.NoError(t, err)
	sender.Send(buf, 0, ecn, metadata)
}

func TestNativeGroupBarriersAndBounds(t *testing.T) {
	for _, barrier := range []string{"turn", "route", "tuple", "control", "member bound", "backing bound"} {
		t.Run(barrier, func(t *testing.T) {
			nativeOrdinaryTest(t, func(t *testing.T) {
				tc, sender, socket := nativeProbeRecoveryConnection(t, "reno")
				socket.hold = nativeProbeHoldAll
				count := 2
				switch barrier {
				case "member bound":
					count = 32
				case "backing bound":
					count = 16
				}
				for range count {
					buf := getPacketBuffer()
					if barrier == "backing bound" {
						buf.Release()
						buf = getCoalescedPacketBuffer()
					}
					appendNativeGroupMember(t, tc, sender, buf, "one real packet", protocol.ECNNon, sendMetadata{})
				}
				r := sender.pending
				require.NotNil(t, r)
				require.Len(t, r.request.members, count)
				switch barrier {
				case "turn":
					sender.finish()
				case "route":
					sender.useRoute(&pathRoute{})
				case "tuple":
					tc.conn.pathGeneration++
					require.True(t, sender.reserveBuffer(protocol.MaxPacketBufferSize))
					require.NotSame(t, r, sender.pending, "tuple identity changes seal the old builder")
				case "control":
					appendNativeGroupMember(t, tc, sender, getPacketBuffer(), "control barrier", protocol.ECNNon, sendMetadata{handshake: true})
				case "member bound":
					require.True(t, sender.WouldBlock(), "path buffer bound prevents destructive construction")
				case "backing bound":
					require.True(t, sender.reserveBuffer(protocol.MaxCoalescedPacketBufferSize))
					require.NotSame(t, r, sender.pending, "the next pool tier exceeds the first request's 1 MiB backing bound")
				}
				require.Equal(t, ordinaryEndpointOwned, r.state)
				require.LessOrEqual(t, r.backing, protocol.ByteCount(1<<20))
				sender.finish()
				socket.release()
				synctest.Wait()
				require.NoError(t, sender.consume())
			})
		})
	}
}

func TestNativeFrozenGroupSharedRefusalAndExpiry(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		tc, sender, socket := nativeProbeRecoveryConnection(t, "reno")
		socket.hold = nativeProbeHoldAll
		nativeProbeOccupy(t, sender.endpoint, sender.participant)
		for _, payload := range []string{"first frozen member", "second frozen member"} {
			appendNativeGroupMember(t, tc, sender, getPacketBuffer(), payload, protocol.ECNNon, sendMetadata{})
		}
		r := sender.pending
		deadline := r.deadline
		buffers := []*packetBuffer{r.request.members[0].buffer, r.request.members[1].buffer}
		sender.finish()
		require.Equal(t, ordinaryStaged, r.state)
		require.Equal(t, deadline, sender.localDeadline())
		var bytes int
		sender.endpoint.do(func(*endpointState) { bytes = sender.participant.wait[0].bytes })
		require.EqualValues(t, 2*protocol.MaxPacketBufferSize, bytes)
		time.Sleep(monotime.Until(deadline))
		require.NoError(t, sender.consume())
		for _, buffer := range buffers {
			require.Zero(t, buffer.refCount)
		}
		require.EqualValues(t, 2, sender.droppedDatagrams)
		socket.hold = nil
		socket.release()
		synctest.Wait()
		for _, write := range socket.writes {
			require.NotContains(t, string(write.data), "frozen member")
		}
	})
}

func TestNativeGroupPartialConstructionKeepsEarlierMembers(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		tc, sender, socket := nativeProbeRecoveryConnection(t, "reno")
		appendNativeGroupMember(t, tc, sender, getPacketBuffer(), "completed earlier member", protocol.ECNNon, sendMetadata{})
		failed := getLargePacketBuffer()
		require.True(t, sender.reserveBuffer(failed.Cap()))
		require.NoError(t, tc.conn.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("registered failed-buffer prefix")}))
		_, err := tc.conn.emission.appendPacket(failed, 1200, false, protocol.ECNNon, monotime.Now())
		require.NoError(t, err)
		cause := errors.New("later construction failure")
		ctrl := gomock.NewController(t)
		sealing := NewMockSealingManager(ctrl)
		sealing.EXPECT().Get1RTTSealer().Return(nil, cause)
		tc.conn.emission.packer.cryptoSetup = sealing
		_, err = tc.conn.emission.appendPacket(failed, 1200, false, protocol.ECNNon, monotime.Now())
		require.ErrorIs(t, err, cause)
		failed.Release()
		sender.finish()
		synctest.Wait()
		require.NoError(t, sender.consume())
		require.Len(t, socket.writes, 1)
		require.Contains(t, string(socket.writes[0].data), "completed earlier member")
		require.EqualValues(t, 1, sender.droppedDatagrams)
		for i := range sender.records {
			require.Equal(t, ordinaryFree, sender.records[i].state)
		}
	})
}

func TestNativeGroupMixedMembersPreserveEarlierReceipt(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		tc, sender, socket := nativeProbeRecoveryConnection(t, "reno")
		calls := 0
		var acceptedAt monotime.Time
		socket.result = func(data []byte) (int, error) {
			calls++
			if calls == 3 {
				return 0, &os.SyscallError{Syscall: "sendmsg", Err: syscall.EMSGSIZE}
			}
			acceptedAt = monotime.Now()
			return len(data), nil
		}
		buf := getLargePacketBuffer()
		require.True(t, sender.reserveBuffer(buf.Cap()))
		var segment protocol.ByteCount
		for range 2 {
			require.NoError(t, tc.conn.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("same size")}))
			receipt, err := tc.conn.emission.appendPacket(buf, 1200, false, protocol.ECNNon, monotime.Now())
			require.NoError(t, err)
			segment = receipt.size
		}
		sender.Send(buf, uint16(segment), protocol.ECNNon, sendMetadata{})
		appendNativeGroupMember(t, tc, sender, getPacketBuffer(), "failed singleton", protocol.ECNUnsupported, sendMetadata{})
		appendNativeGroupMember(t, tc, sender, getPacketBuffer(), "untouched member", protocol.ECNNon, sendMetadata{})
		receipt := &pathAbandonReceipt{}
		// Install a receipt on the first member to exercise the canonical result
		// mapping independently of the producer's terminal-control seal barrier.
		sender.pending.members[0].metadata.terminalReceipts = []*pathAbandonReceipt{receipt}
		sender.finish()
		synctest.Wait()
		require.NoError(t, sender.consume())
		require.Len(t, socket.writes, 3, "the final member remains unattempted")
		require.EqualValues(t, 2, sender.droppedDatagrams, "only proven-unsubmitted DATAGRAMs are dropped")
		require.Equal(t, acceptedAt, receipt.acceptedAt(), "the successful member retains its own actual return time")
	})
}

func TestNativeGroupRecoveryCapacityStopsBeforeExtraction(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		tc, sender, _ := nativeProbeRecoveryConnection(t, "reno")
		c := tc.conn
		for sender.recovery.CanRegisterLocalPackets(2) {
			now := monotime.Now()
			pn := c.sentPacketHandler.PopPacketNumber(protocol.Encryption1RTT)
			token, err := sender.recovery.RegisterLocalPacket(ackhandler.PacketContext{}, now, pn, protocol.InvalidPacketNumber, nil, nil, protocol.Encryption1RTT, protocol.ECNNon, 1200)
			require.NoError(t, err)
			require.NoError(t, sender.recovery.CompleteLocalPacket(token, ackhandler.LocalPacketAccepted, now))
		}
		require.True(t, sender.recovery.CanRegisterLocalPackets(1))
		for _, payload := range []string{"last available registration", "remains queued"} {
			require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte(payload)}))
		}
		require.False(t, sender.WouldBlock())
		result := c.emission.withGSO(monotime.Now())
		require.NoError(t, result.err)
		require.True(t, result.progress)
		require.Contains(t, []emissionStop{emissionQueueFull, emissionHardBlocked}, result.stop)
		require.Equal(t, "remains queued", string(c.datagramQueue.Peek().Data))
		sender.finish()
		synctest.Wait()
		require.NoError(t, sender.consume())
	})
}

func TestNativeGroupEpochBarrierOnAdmittedRoute(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		f.c.pathCredit.selectPrimaryPrivate(1)
		require.Nil(t, f.c.emission.capacity())
		sender := f.c.emission.local.(*nativeOrdinarySender)
		appendNativeGroupMember(t, f.tc, sender, getPacketBuffer(), "old epoch", protocol.ECNNon, sendMetadata{})
		old := sender.pending
		sender.route.epoch = &responseEpoch{}
		require.True(t, sender.reserveBuffer(protocol.MaxPacketBufferSize))
		require.Equal(t, ordinaryEndpointOwned, old.state)
		require.NotSame(t, old, sender.pending)
		sender.finish()
		synctest.Wait()
		require.NoError(t, sender.consume())
	})
}
