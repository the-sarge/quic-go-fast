package quic

import (
	"context"
	"os"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestTimeoutReadersReleaseBuffer(t *testing.T) {
	for _, peek := range []bool{false, true} {
		name := "Read"
		if peek {
			name = "Peek"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				str := newReceiveStream(42, nil, newTestStreamFlowController(42))
				late := make(chan error, 1)
				go func() {
					time.Sleep(2 * time.Second)
					late <- str.handleStreamFrame(&wire.StreamFrame{Data: []byte("late")}, monotime.Now())
				}()
				buf := make([]byte, 4)
				var n int
				var err error
				if peek {
					n, err = (&peekerWithTimeout{Peeker: str, Timeout: time.Second}).Peek(buf)
				} else {
					n, err = (&readerWithTimeout{Reader: str, Timeout: time.Second}).Read(buf)
				}
				assert.Zero(t, n)
				assert.ErrorIs(t, err, os.ErrDeadlineExceeded)
				copy(buf, "mine")
				require.NoError(t, <-late)
				synctest.Wait()
				require.Equal(t, "mine", string(buf), "late data must not reach the caller's reused buffer")
				require.NoError(t, str.SetReadDeadline(time.Time{}))
				n, err = str.Read(buf)
				require.NoError(t, err)
				require.Equal(t, 4, n)
				require.Equal(t, "late", string(buf))
			})
		})
	}
}

func TestTimeoutWriterReleasesBuffer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sender := NewMockStreamSender(gomock.NewController(t))
		sender.EXPECT().onHasStreamData(gomock.Any(), gomock.Any()).AnyTimes()
		str := newSendStream(context.Background(), 42, sender, newTestStreamFlowControllerWithSendWindow(42, protocol.MaxByteCount), false)
		buf := make([]byte, 4096)
		n, err := (&writerWithTimeout{Writer: str, Timeout: time.Second}).Write(buf)
		assert.ErrorIs(t, err, os.ErrDeadlineExceeded)
		require.Less(t, n, len(buf))
		// Reuse the entire caller buffer before the transport drains queued data.
		for i := range buf {
			buf[i] = 0xff
		}
		data := make([]byte, 0)
		for {
			frame, _, more := str.popStreamFrame(protocol.MaxPacketBufferSize, protocol.Version1)
			if frame.Frame != nil {
				data = append(data, frame.Frame.Data...)
			}
			if !more {
				break
			}
		}
		synctest.Wait()
		require.Equal(t, make([]byte, n), data)
	})
}
