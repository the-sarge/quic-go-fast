//go:build linux || darwin || windows

package quic

import (
	"testing"
	"testing/synctest"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/testutils"

	"github.com/stretchr/testify/require"
)

// This test needs the platform's native oversized-send error
// (testutils.SendMsgSizeErr), which exists on linux, darwin, and windows.
// Partial acceptance then a message-size error: the per-packet retry of the
// first unaccepted entry attaches the size error and handshake MTU feedback
// to that entry's own metadata, and the connection is not torn down.
func TestSendQueueBatchPartialAcceptanceMsgSizeFeedback(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		conn := &fakeBatchSendConn{
			accept: func(call int, bufs [][]byte) (int, error) {
				if call == 0 {
					return 1, nil
				}
				return len(bufs), nil
			},
			writeErr: func(_ int, b []byte) error {
				if len(b) == 1452 {
					return testutils.SendMsgSizeErr
				}
				return nil
			},
		}
		feedback := &handshakeSendFeedback{wakeup: make(chan struct{}, 1)}
		q := newSendQueue(conn, feedback)

		// pkt1 is the entry the size error must attach to: an eligible
		// handshake datagram with its own path generation.
		bufs := []*packetBuffer{
			getPacketWithContents(make([]byte, 1300)),
			getPacketWithContents(make([]byte, 1452)),
			getPacketWithContents(make([]byte, 1280)),
		}
		q.Send(bufs[0], 0, protocol.ECNNon, sendMetadata{})
		q.Send(bufs[1], 0, protocol.ECNNon, sendMetadata{handshake: true, pathGeneration: 9})
		q.Send(bufs[2], 0, protocol.ECNNon, sendMetadata{})
		startAndFinishQueue(t, q, nil)

		require.Len(t, conn.batches, 1)
		require.Len(t, conn.writes, 2, "the retried entry and the single-entry tail go per-packet")
		require.Len(t, conn.writes[0].data, 1452, "the retry must carry the first unaccepted entry")
		require.Len(t, conn.writes[1].data, 1280)
		generation, pending := feedback.take()
		require.True(t, pending, "the size error must publish feedback for the retried entry")
		require.EqualValues(t, 9, generation, "feedback must carry the retried entry's generation")
		_, pending = feedback.take()
		require.False(t, pending, "only the failing entry publishes feedback")
		for _, buf := range bufs {
			require.Zero(t, buf.refCount)
		}
	})
}
