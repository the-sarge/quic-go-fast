package quic

import (
	"bytes"
	"context"
	"fmt"
	"runtime"
	"testing"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestSendStreamTryWriteAllLinearStorage(t *testing.T) {
	for _, size := range []int{32 << 10, 128 << 10, 512 << 10} {
		for _, chunkSize := range []int{size, 2048} {
			t.Run(fmt.Sprintf("bytes=%d/chunk=%d", size, chunkSize), func(t *testing.T) {
				const streamID protocol.StreamID = 42
				fc := newTestStreamFlowControllerWithSendWindow(streamID, protocol.ByteCount(size))
				sender := NewMockStreamSender(gomock.NewController(t))
				str := newSendStream(context.Background(), streamID, sender, fc, false)
				sender.EXPECT().onHasStreamData(streamID, str).Times(size / chunkSize)
				data := make([]byte, size)
				for i := range data {
					data[i] = byte(i)
				}
				var before, admitted, drained runtime.MemStats
				runtime.ReadMemStats(&before)
				for start := 0; start < size; start += chunkSize {
					require.NoError(t, str.TryWriteAll(data[start:start+chunkSize]))
				}
				runtime.ReadMemStats(&admitted)
				// A fixed byte budget per accepted byte catches exact-size growth on
				// every admission. Slack includes runtime, assertions and mock overhead.
				require.Less(t, admitted.TotalAlloc-before.TotalAlloc, uint64(8*size+128<<10))
				require.Zero(t, fc.SendWindowSize())
				require.ErrorIs(t, str.TryWriteAll([]byte("rejected")), ErrWouldBlock)
				clear(data) // the caller can reuse every accepted byte immediately

				for offset := 0; offset < size; {
					const packetData = 1024
					n := min(packetData, size-offset)
					pending := str.nextFrame.Data
					frame, blocked, more := str.popStreamFrame(expectedFrameHeaderLen(streamID, protocol.ByteCount(offset))+protocol.ByteCount(n), protocol.Version1)
					require.NotNil(t, frame.Frame)
					require.Equal(t, protocol.ByteCount(offset), frame.Frame.Offset)
					require.NotEmpty(t, frame.Frame.Data)
					require.LessOrEqual(t, len(frame.Frame.Data), n)
					n = len(frame.Frame.Data)
					require.LessOrEqual(t, cap(frame.Frame.Data), int(protocol.MaxPacketBufferSize))
					for i, b := range frame.Frame.Data {
						if b != byte(offset+i) {
							t.Fatalf("corrupted byte at offset %d", offset+i)
						}
					}
					offset += n
					require.Equal(t, offset < size, more)
					if more {
						require.Nil(t, blocked)
					}
					if len(pending)-n > int(protocol.MaxPacketBufferSize) {
						// This storage invariant rules out moving the tail in place as
						// well as allocating a fresh tail: only the prefix is copied.
						require.True(t, &pending[n] == &str.nextFrame.Data[0], "large tail must advance without copying")
					}
					frame.Handler.OnAcked(frame.Frame)
				}
				runtime.ReadMemStats(&drained)
				require.Less(t, drained.TotalAlloc-admitted.TotalAlloc, uint64(8*size+128<<10))
				require.Nil(t, str.nextFrame)
				require.Zero(t, str.numOutstandingFrames)
				t.Logf("accepted %d bytes: admission %d B, segmentation %d B", size, admitted.TotalAlloc-before.TotalAlloc, drained.TotalAlloc-admitted.TotalAlloc)
			})
		}
	}
}

func TestSendStreamLargeTryWriteAllRetransmissionLifetime(t *testing.T) {
	const streamID protocol.StreamID = 42
	sender := NewMockStreamSender(gomock.NewController(t))
	str := newSendStream(context.Background(), streamID, sender, newTestStreamFlowControllerWithSendWindow(streamID, protocol.MaxByteCount), false)
	sender.EXPECT().onHasStreamData(streamID, str).AnyTimes()
	sender.EXPECT().onHasStreamRetransmission(streamID, str).AnyTimes()
	data := bytes.Repeat([]byte("a"), 32<<10)
	require.NoError(t, str.TryWriteAll(data))
	first, _, _ := str.popStreamFrame(1024, protocol.Version1)
	expected := bytes.Clone(first.Frame.Data)
	first.Handler.OnLost(first.Frame)
	// Append while the large pending allocation is partially consumed, then
	// overwrite caller storage and drain/ACK newer frames before retransmitting.
	require.NoError(t, str.TryWriteAll(bytes.Repeat([]byte("b"), 64<<10)))
	clear(data)
	for str.nextFrame != nil {
		f, _, _ := str.popStreamFrame(1024, protocol.Version1)
		f.Handler.OnAcked(f.Frame)
	}
	var retransmitted []byte
	for more := true; more; {
		f, hasMore := str.popRetransmissionFrame(128, protocol.Version1)
		require.NotNil(t, f.Frame)
		require.Equal(t, protocol.ByteCount(len(retransmitted)), f.Frame.Offset)
		require.LessOrEqual(t, cap(f.Frame.Data), int(protocol.MaxPacketBufferSize))
		retransmitted = append(retransmitted, f.Frame.Data...)
		f.Handler.OnAcked(f.Frame)
		more = hasMore
	}
	require.Equal(t, expected, retransmitted)
	require.Zero(t, str.numOutstandingFrames)
}

func TestSendStreamLargeTryWriteAllCancellationLifetime(t *testing.T) {
	for _, reliableSize := range []int{0, 40, 32 << 10} {
		t.Run(fmt.Sprintf("reliable=%d", reliableSize), func(t *testing.T) {
			const streamID protocol.StreamID = 42
			const size = 32 << 10
			sender := NewMockStreamSender(gomock.NewController(t))
			str := newSendStream(context.Background(), streamID, sender, newTestStreamFlowControllerWithSendWindow(streamID, protocol.MaxByteCount), reliableSize > 0)
			sender.EXPECT().onHasStreamData(streamID, str).AnyTimes()
			sender.EXPECT().onHasStreamRetransmission(streamID, str).AnyTimes()
			sender.EXPECT().onHasStreamControlFrame(streamID, str)
			require.NoError(t, str.TryWriteAll(bytes.Repeat([]byte("r"), max(reliableSize, 40))))
			if reliableSize > 0 {
				str.SetReliableBoundary()
				// The suffix must be discarded, including its large backing store.
				require.NoError(t, str.TryWriteAll(bytes.Repeat([]byte("x"), size)))
			}
			if reliableSize == 0 {
				require.NoError(t, str.TryWriteAll(bytes.Repeat([]byte("r"), size-40)))
			}
			first, _, _ := str.popStreamFrame(expectedFrameHeaderLen(streamID, 0)+20, protocol.Version1)
			str.CancelWrite(42)
			control, ok, _ := str.getControlFrame(monotime.Now())
			require.True(t, ok)
			reset := control.Frame.(*wire.ResetStreamFrame)
			if reliableSize > 0 {
				require.Equal(t, protocol.ByteCount(size+reliableSize), reset.FinalSize)
				require.Equal(t, protocol.ByteCount(reliableSize), reset.ReliableSize)
				first.Handler.OnLost(first.Frame)
				f, more := str.popRetransmissionFrame(1024, protocol.Version1)
				require.False(t, more)
				require.Equal(t, bytes.Repeat([]byte("r"), len(f.Frame.Data)), f.Frame.Data)
				f.Handler.OnAcked(f.Frame)
				for str.nextFrame != nil {
					f, _, _ := str.popStreamFrame(1024, protocol.Version1)
					require.NotNil(t, f.Frame)
					require.LessOrEqual(t, cap(f.Frame.Data), int(protocol.MaxPacketBufferSize))
					require.Equal(t, bytes.Repeat([]byte("r"), len(f.Frame.Data)), f.Frame.Data)
					f.Handler.OnAcked(f.Frame)
				}
				require.Equal(t, protocol.ByteCount(reliableSize), str.writeOffset)
			} else {
				require.Equal(t, protocol.ByteCount(size), reset.FinalSize)
				require.Nil(t, str.nextFrame)
				// A late recovery callback still owns its packet storage after reset.
				require.Equal(t, bytes.Repeat([]byte("r"), len(first.Frame.Data)), first.Frame.Data)
				first.Handler.OnLost(first.Frame)
			}
			require.Empty(t, str.retransmissionQueue)
			f, _, more := str.popStreamFrame(1024, protocol.Version1)
			require.Nil(t, f.Frame)
			require.False(t, more)
		})
	}
}
