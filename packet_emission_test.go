package quic

import (
	"fmt"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestEmissionResultQueueWakeup(t *testing.T) {
	for _, gso := range []bool{false, true} {
		for _, occupied := range []int{sendQueueCapacity - 1, sendQueueCapacity} {
			t.Run(fmt.Sprintf("gso=%t/occupied=%d", gso, occupied), func(t *testing.T) {
				tc := newEmissionTestConnection(t, gso)
				c := tc.conn
				q := c.sendQueue.(*sendQueue)
				for range occupied {
					buf := getPacketBuffer()
					buf.Data = append(buf.Data, 0xff)
					q.Send(buf, 0, protocol.ECNUnsupported, sendMetadata{})
				}
				require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("send opportunity")}))
				result := c.emission.advance(monotime.Now(), gso)
				require.NoError(t, result.err)
				require.Equal(t, emissionQueueFull, result.stop)
				require.Equal(t, occupied < sendQueueCapacity, result.progress)
				require.Equal(t, q.Available(), result.available)
				require.True(t, q.WouldBlock())
				pn, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
				require.EqualValues(t, sendQueueCapacity-occupied, pn)
				tc.sendConn.EXPECT().Write(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(sendQueueCapacity)
				close(q.closeCalled)
				require.NoError(t, q.Run())
				select {
				case <-result.available:
				default:
					t.Fatal("worker progress did not signal the returned wakeup")
				}
			})
		}
	}
}

// Recovery still registers real packets; only its next permitted send mode is
// controlled, so this fixture can observe each policy outcome deterministically.
type emissionRecoveryOutcome struct {
	ackhandler.SentPacketHandler
	mode     ackhandler.SendMode
	deadline monotime.Time
}

func (h emissionRecoveryOutcome) SendMode(monotime.Time) ackhandler.SendMode { return h.mode }
func (h emissionRecoveryOutcome) TimeUntilSend() monotime.Time               { return h.deadline }

func TestEmissionResultRecoveryOutcome(t *testing.T) {
	for _, gso := range []bool{false, true} {
		for _, tt := range []struct {
			name string
			mode ackhandler.SendMode
			stop emissionStop
		}{
			{"congestion", ackhandler.SendAck, emissionCongestionLimited},
			{"hard blocked", ackhandler.SendNone, emissionHardBlocked},
			{"pacing", ackhandler.SendPacingLimited, emissionPaced},
			{"probe pending", ackhandler.SendPTOAppData, emissionProbePending},
		} {
			t.Run(fmt.Sprintf("gso=%t/%s", gso, tt.name), func(t *testing.T) {
				tc := newEmissionTestConnection(t, gso)
				c := tc.conn
				now := monotime.Now()
				deadline := now.Add(time.Millisecond)
				c.sentPacketHandler = emissionRecoveryOutcome{c.sentPacketHandler, tt.mode, deadline}
				require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("one real packet")}))
				result := c.emission.advance(now, gso)
				require.NoError(t, result.err)
				require.True(t, result.progress)
				require.Equal(t, tt.stop, result.stop)
				require.Nil(t, result.available)
				if tt.mode == ackhandler.SendPacingLimited {
					require.Equal(t, deadline, result.deadline)
				} else {
					require.True(t, result.deadline.IsZero())
				}
				q := c.sendQueue.(*sendQueue)
				require.Len(t, q.queue, 1)
				tc.sendConn.EXPECT().Write(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
				close(q.closeCalled)
				require.NoError(t, q.Run())
			})
		}
	}
}

func TestEmissionResultConnection(t *testing.T) {
	for _, gso := range []bool{false, true} {
		t.Run(fmt.Sprintf("gso=%t", gso), func(t *testing.T) {
			tc := newEmissionTestConnection(t, gso)
			c := tc.conn
			result := c.triggerSending(monotime.Now())
			require.NoError(t, result.err)
			require.False(t, result.progress)
			require.Equal(t, emissionNoData, result.stop)
			require.Nil(t, result.available)
			for _, payload := range []string{"first packet", "next packet"} {
				require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte(payload)}))
			}
			c.receivedPackets.PushBack(receivedPacket{})
			result = c.triggerSending(monotime.Now())
			require.NoError(t, result.err)
			require.True(t, result.progress)
			require.Equal(t, emissionReceivePending, result.stop)
			require.Equal(t, deadlineSendImmediately, c.pacingDeadline)
			require.NotNil(t, c.datagramQueue.Peek())
			c.receivedPackets.PopFront()
			q := c.sendQueue.(*sendQueue)
			tc.sendConn.EXPECT().Write(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
			close(q.closeCalled)
			require.NoError(t, q.Run())
		})
	}
}

func TestEmissionEmptyCallerBuffer(t *testing.T) {
	for _, gso := range []bool{false, true} {
		t.Run(fmt.Sprintf("gso=%t", gso), func(t *testing.T) {
			c := newEmissionTestConnection(t, gso).conn
			observed := observeEmissionBuffers(t, gso)
			result := c.emission.advance(monotime.Now(), gso)
			require.NoError(t, result.err)
			require.False(t, result.progress)
			require.Equal(t, emissionNoData, result.stop)
			require.Len(t, *observed, 1)
			require.Zero(t, (*observed)[0].refCount)
			require.Empty(t, c.sendQueue.(*sendQueue).queue)
		})
	}
}

// Change only the ECN policy after a real registration, retaining recovery and
// construction so output demonstrates the boundary between the two batches.
type emissionECNTransition struct{ ackhandler.SentPacketHandler }

func (h emissionECNTransition) ECNMode(bool) protocol.ECN {
	pn, _ := h.PeekPacketNumber(protocol.Encryption1RTT)
	if pn == 0 {
		return protocol.ECT0
	}
	return protocol.ECNNon
}

func TestEmissionECNBatchBoundary(t *testing.T) {
	tc := newEmissionTestConnection(t, true)
	c := tc.conn
	c.sentPacketHandler = emissionECNTransition{c.sentPacketHandler}
	_, pnLen := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
	size := 1200 - int(wire.ShortHeaderLen(c.connIDManager.Get(), pnLen)) - 7 - 3
	for range 2 {
		require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: make([]byte, size)}))
	}
	result := c.emission.advance(monotime.Now(), true)
	require.NoError(t, result.err)
	require.True(t, result.progress)
	q := c.sendQueue.(*sendQueue)
	require.Len(t, q.queue, 2)
	for _, want := range []protocol.ECN{protocol.ECT0, protocol.ECNNon} {
		entry := <-q.queue
		require.Equal(t, want, entry.ecn)
		require.EqualValues(t, 1200, entry.gsoSize)
		require.Len(t, entry.buf.Data, 1200)
		entry.buf.Release()
	}
}
