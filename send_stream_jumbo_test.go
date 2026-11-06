package quic

import (
	"bytes"
	"context"
	"testing"
	"testing/synctest"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestSendStreamJumboWrite(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const streamID protocol.StreamID = 42
		sender := NewMockStreamSender(gomock.NewController(t))
		str := newSendStream(context.Background(), streamID, sender, newTestStreamFlowControllerWithSendWindow(streamID, protocol.MaxByteCount), false)
		sender.EXPECT().onHasStreamData(streamID, str).AnyTimes()
		data := bytes.Repeat([]byte("jumbo"), 3580) // two 8,900-byte payloads and a 100-byte tail
		done := make(chan struct{})
		go func() {
			defer close(done)
			n, err := str.Write(data)
			require.NoError(t, err)
			require.Equal(t, len(data), n)
			require.NoError(t, str.Close())
		}()
		synctest.Wait()

		for i := range 2 {
			maxBytes := protocol.ByteCount(8904)
			if i > 0 {
				maxBytes = 8906
			}
			frame, blocked, more := str.popStreamFrame(maxBytes, protocol.Version1)
			require.Nil(t, blocked)
			require.True(t, more)
			require.Equal(t, protocol.ByteCount(i*8900), frame.Frame.Offset)
			require.Len(t, frame.Frame.Data, 8900)
			require.Equal(t, data[i*8900:(i+1)*8900], frame.Frame.Data)
			require.Equal(t, 8972, cap(frame.Frame.Data))
			requireJumboStreamFrameRoundTrip(t, frame.Frame)
			frame.Frame.PutBack()
			synctest.Wait()
			if i == 0 {
				select {
				case <-done:
					t.Fatal("Write returned with more than 1,452 bytes still queued")
				default:
				}
			}
		}
		<-done
		frame, blocked, more := str.popStreamFrame(8972, protocol.Version1)
		require.Nil(t, blocked)
		require.False(t, more)
		require.Equal(t, protocol.ByteCount(17800), frame.Frame.Offset)
		require.Equal(t, data[17800:], frame.Frame.Data)
		require.Equal(t, 1452, cap(frame.Frame.Data))
		require.True(t, frame.Frame.Fin)
		requireJumboStreamFrameRoundTrip(t, frame.Frame)
		frame.Frame.PutBack()
	})
}

func TestSendStreamJumboTryWriteAll(t *testing.T) {
	const streamID protocol.StreamID = 42
	sender := NewMockStreamSender(gomock.NewController(t))
	str := newSendStream(context.Background(), streamID, sender, newTestStreamFlowControllerWithSendWindow(streamID, protocol.MaxByteCount), false)
	sender.EXPECT().onHasStreamData(streamID, str).AnyTimes()
	data := bytes.Repeat([]byte("jumbo"), 3580)
	require.NoError(t, str.TryWriteAll(data))
	require.NoError(t, str.Close())

	for i := range 2 {
		maxBytes := protocol.ByteCount(8904)
		if i > 0 {
			maxBytes = 8906
		}
		frame, blocked, more := str.popStreamFrame(maxBytes, protocol.Version1)
		require.Nil(t, blocked)
		require.True(t, more)
		require.Equal(t, protocol.ByteCount(i*8900), frame.Frame.Offset)
		require.Len(t, frame.Frame.Data, 8900)
		require.Equal(t, data[i*8900:(i+1)*8900], frame.Frame.Data)
		require.Equal(t, 8972, cap(frame.Frame.Data))
		requireJumboStreamFrameRoundTrip(t, frame.Frame)
		frame.Frame.PutBack()
	}
	frame, blocked, more := str.popStreamFrame(8972, protocol.Version1)
	require.Nil(t, blocked)
	require.False(t, more)
	require.Equal(t, protocol.ByteCount(17800), frame.Frame.Offset)
	require.Equal(t, data[17800:], frame.Frame.Data)
	require.Equal(t, 1452, cap(frame.Frame.Data))
	require.True(t, frame.Frame.Fin)
	frame.Frame.PutBack()
}

func requireJumboStreamFrameRoundTrip(t *testing.T, frame *wire.StreamFrame) {
	t.Helper()
	parser := wire.NewFrameParser(false, false, false)
	parser.SetMaxStreamDataLen(8972)
	encoded, err := frame.Append(nil, protocol.Version1)
	require.NoError(t, err)
	parsed, n, err := parser.ParseStreamFrame(wire.FrameType(encoded[0]), encoded[1:], protocol.Version1)
	require.NoError(t, err)
	require.Equal(t, len(encoded)-1, n)
	require.Equal(t, frame.StreamID, parsed.StreamID)
	require.Equal(t, frame.Offset, parsed.Offset)
	require.Equal(t, frame.Data, parsed.Data)
	require.Equal(t, frame.Fin, parsed.Fin)
	parsed.PutBack()
}

func TestSendStreamJumboRetransmission(t *testing.T) {
	for _, queued := range []bool{false, true} {
		name := "Write"
		if queued {
			name = "TryWriteAll"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				const streamID protocol.StreamID = 42
				sender := NewMockStreamSender(gomock.NewController(t))
				str := newSendStream(context.Background(), streamID, sender, newTestStreamFlowControllerWithSendWindow(streamID, protocol.MaxByteCount), false)
				sender.EXPECT().onHasStreamData(streamID, str).AnyTimes()
				sender.EXPECT().onHasStreamRetransmission(streamID, str).AnyTimes()
				sender.EXPECT().onStreamCompleted(streamID).AnyTimes()
				data := bytes.Repeat([]byte("jumbo"), 1780)
				if queued {
					require.NoError(t, str.TryWriteAll(data))
					require.NoError(t, str.Close())
				} else {
					go func() {
						n, err := str.Write(data)
						require.NoError(t, err)
						require.Equal(t, len(data), n)
						require.NoError(t, str.Close())
					}()
					synctest.Wait()
				}
				original, _, _ := str.popStreamFrame(8904, protocol.Version1)
				require.Len(t, original.Frame.Data, 8900)
				synctest.Wait()
				original.Handler.OnLost(original.Frame)

				prefix, more := str.popRetransmissionFrame(4004, protocol.Version1)
				require.True(t, more)
				require.Len(t, prefix.Frame.Data, 4000)
				require.Equal(t, data[:4000], prefix.Frame.Data)
				requireJumboStreamFrameRoundTrip(t, prefix.Frame)
				prefix.Handler.OnLost(prefix.Frame)

				tail, more := str.popRetransmissionFrame(8972, protocol.Version1)
				require.True(t, more)
				require.Equal(t, protocol.ByteCount(4000), tail.Frame.Offset)
				require.Equal(t, data[4000:], tail.Frame.Data)
				requireJumboStreamFrameRoundTrip(t, tail.Frame)
				tail.Handler.OnAcked(tail.Frame)

				resent, more := str.popRetransmissionFrame(8972, protocol.Version1)
				require.False(t, more)
				require.Zero(t, resent.Frame.Offset)
				require.Equal(t, data[:4000], resent.Frame.Data)
				requireJumboStreamFrameRoundTrip(t, resent.Frame)
				resent.Handler.OnAcked(resent.Frame)
			})
		})
	}
}
