//go:build linux || darwin || windows

package quic

import (
	"net"
	"testing"
	"testing/synctest"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestSendQueueHandshakeMTUEligibility(t *testing.T) {
	for _, tt := range []struct {
		name     string
		err      error
		marked   bool
		gso      uint16
		size     int
		eligible bool
	}{
		{"handshake", &net.OpError{Op: "write", Err: testutils.SendMsgSizeErr}, true, 0, 1452, true},
		{"success", nil, true, 0, 1452, false},
		{"other error", assert.AnError, true, 0, 1452, false},
		{"unmarked or PMTU probe", testutils.SendMsgSizeErr, false, 0, 1452, false},
		{"GSO", testutils.SendMsgSizeErr, true, 1200, 1452, false},
		{"minimum", testutils.SendMsgSizeErr, true, 0, 1200, false},
		{"subminimum", testutils.SendMsgSizeErr, true, 0, 1199, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctrl := gomock.NewController(t)
				conn := NewMockSendConn(ctrl)
				feedback := &handshakeSendFeedback{wakeup: make(chan struct{}, 1)}
				q := newSendQueue(conn, feedback)
				conn.EXPECT().Write(gomock.Any(), tt.gso, protocol.ECNNon).Return(tt.err)
				q.Send(getPacketWithContents(make([]byte, tt.size)), tt.gso, protocol.ECNNon, sendMetadata{handshake: tt.marked, pathGeneration: 7})
				result := make(chan error, 1)
				go func() { result <- q.Run() }()
				synctest.Wait()
				q.Close()
				if tt.err == assert.AnError {
					require.ErrorIs(t, <-result, assert.AnError)
				} else {
					require.NoError(t, <-result)
				}
				generation, pending := feedback.take()
				require.Equal(t, tt.eligible, pending)
				if pending {
					require.EqualValues(t, 7, generation)
				}
			})
		})
	}
}

func TestSendQueueHandshakeMTUDrain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		conn := NewMockSendConn(gomock.NewController(t))
		feedback := &handshakeSendFeedback{wakeup: make(chan struct{}, 1)}
		q := newSendQueue(conn, feedback)
		conn.EXPECT().Write(gomock.Any(), uint16(0), protocol.ECNNon).Return(testutils.SendMsgSizeErr).Times(sendQueueCapacity)
		for generation := range sendQueueCapacity {
			q.Send(getPacketWithContents(make([]byte, 1452)), 0, protocol.ECNNon, sendMetadata{handshake: true, pathGeneration: uint64(generation)})
		}
		// Drain a full queue without a feedback consumer. Close must still join the worker.
		result := make(chan error, 1)
		go func() { result <- q.Run() }()
		q.Close()
		require.NoError(t, <-result)
		require.Len(t, feedback.wakeup, 1)
		generation, pending := feedback.take()
		require.True(t, pending)
		require.EqualValues(t, sendQueueCapacity-1, generation, "stale pending events must not hide the latest path")
		_, pending = feedback.take()
		require.False(t, pending)
		require.Empty(t, feedback.wakeup)
	})
}
