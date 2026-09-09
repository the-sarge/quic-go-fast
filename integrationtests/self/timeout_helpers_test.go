package self_test

import (
	"io"
	"os"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

type lateTimeoutBody struct {
	closed  chan struct{}
	release chan struct{}
}

func (b *lateTimeoutBody) Read(p []byte) (int, error) {
	<-b.closed
	<-b.release
	p[0] = 'x'
	return 1, io.EOF
}
func (b *lateTimeoutBody) Close() error { close(b.closed); return nil }

func TestReaderWithTimeoutJoinsLateBody(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		body := &lateTimeoutBody{closed: make(chan struct{}), release: make(chan struct{})}
		buffer := make([]byte, 1)
		done := make(chan struct{})
		var n int
		var err error
		go func() {
			defer close(done)
			n, err = (&readerWithTimeout{Reader: body, Timeout: time.Second}).Read(buffer)
		}()
		time.Sleep(2 * time.Second)
		synctest.Wait()
		select {
		case <-done:
			t.Error("returned before body released the buffer")
		default:
		}
		close(body.release)
		<-done
		require.Equal(t, 1, n)
		require.ErrorIs(t, err, io.EOF)
		require.Equal(t, []byte("x"), buffer)
		buffer[0] = 'z' // safe for the caller to reuse immediately after return
	})
}

func TestReaderWithTimeoutBlockedStream(t *testing.T) {
	serverStr, clientStr := setupDeadlineTest(t)
	buffer := make([]byte, 1)
	n, err := (&readerWithTimeout{Reader: clientStr, Timeout: 5 * time.Millisecond}).Read(buffer)
	require.Zero(t, n)
	require.ErrorIs(t, err, os.ErrDeadlineExceeded)
	// The timeout setter has finished: clearing the deadline cannot be undone
	// by a callback that outlives the wrapper.
	require.NoError(t, clientStr.SetReadDeadline(time.Time{}))
	_, err = serverStr.Write([]byte("x"))
	require.NoError(t, err)
	n, err = (&readerWithTimeout{Reader: clientStr, Timeout: scaleDuration(time.Second)}).Read(buffer)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, []byte("x"), buffer)
}
