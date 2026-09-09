package self_test

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/quic-go/quic-go"

	"github.com/stretchr/testify/require"
)

func setupDeadlineTest(t *testing.T) (serverStr, clientStr *quic.Stream) {
	t.Helper()
	server, err := quic.Listen(newUDPConnLocalhost(t), getTLSConfig(), getQuicConfig(&quic.Config{InitialStreamReceiveWindow: 1024, MaxStreamReceiveWindow: 1024}))
	require.NoError(t, err)
	t.Cleanup(func() { server.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, err := quic.Dial(ctx, newUDPConnLocalhost(t), server.Addr(), getTLSClientConfig(), getQuicConfig(nil))
	require.NoError(t, err)
	t.Cleanup(func() { conn.CloseWithError(0, "") })
	clientStr, err = conn.OpenStream()
	require.NoError(t, err)
	_, err = clientStr.Write([]byte{0}) // need to write one byte so the server learns about the stream
	require.NoError(t, err)

	serverConn, err := server.Accept(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { serverConn.CloseWithError(0, "") })
	serverStr, err = serverConn.AcceptStream(ctx)
	require.NoError(t, err)

	_, err = serverStr.Read([]byte{0})
	require.NoError(t, err)
	return serverStr, clientStr
}

// runDeadlineOperation always has a native watchdog. In the asynchronous case,
// a separate setter expires the deadline while the peer withholds data / credit.
// The setter is joined even when an assertion in the caller fails.
func runDeadlineOperation(t *testing.T, async bool, setDeadline func(time.Time) error, operation func() (int, error)) (int, error) {
	t.Helper()
	const expiry = 5 * time.Millisecond
	if !async {
		require.NoError(t, setDeadline(time.Now().Add(expiry)))
		return operation()
	}
	require.NoError(t, setDeadline(time.Now().Add(scaleDuration(time.Second))))
	setterDone := make(chan error, 1)
	go func() {
		time.Sleep(expiry)
		setterDone <- setDeadline(time.Now().Add(expiry))
	}()
	defer func() { require.NoError(t, <-setterDone) }()
	return operation()
}

func TestReadDeadlineSync(t *testing.T)  { testReadDeadline(t, false) }
func TestReadDeadlineAsync(t *testing.T) { testReadDeadline(t, true) }

func testReadDeadline(t *testing.T, async bool) {
	serverStr, clientStr := setupDeadlineTest(t)
	buf := make([]byte, 1)
	// The server sends nothing: every operation must block, independent of
	// transfer speed. Resetting the deadline must allow another blocked read.
	for range 10 {
		n, err := runDeadlineOperation(t, async, clientStr.SetReadDeadline, func() (int, error) {
			return clientStr.Read(buf)
		})
		require.Zero(t, n)
		require.ErrorIs(t, err, os.ErrDeadlineExceeded)
	}
	require.NoError(t, clientStr.SetReadDeadline(time.Time{}))
	require.NoError(t, serverStr.SetWriteDeadline(time.Now().Add(scaleDuration(time.Second))))
	_, err := serverStr.Write([]byte("x"))
	require.NoError(t, err)
	n, err := (&readerWithTimeout{Reader: clientStr, Timeout: scaleDuration(time.Second)}).Read(buf)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, []byte("x"), buf)
}

func TestWriteDeadlineSync(t *testing.T)  { testWriteDeadline(t, false) }
func TestWriteDeadlineAsync(t *testing.T) { testWriteDeadline(t, true) }

func testWriteDeadline(t *testing.T, async bool) {
	serverStr, clientStr := setupDeadlineTest(t)
	data := GeneratePRData(64 << 10)
	var written int
	// The server's fixed 1 KiB receive window cannot admit this payload until
	// it reads. No reader is started until all ten expirations are observed.
	for range 10 {
		n, err := runDeadlineOperation(t, async, clientStr.SetWriteDeadline, func() (int, error) {
			return clientStr.Write(data[written:])
		})
		written += n
		require.Less(t, written, len(data))
		require.ErrorIs(t, err, os.ErrDeadlineExceeded)
	}
	require.NoError(t, clientStr.SetWriteDeadline(time.Time{}))
	require.NoError(t, serverStr.SetReadDeadline(time.Now().Add(scaleDuration(5*time.Second))))
	type readResult struct {
		data []byte
		err  error
	}
	received := make(chan readResult, 1)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		b, err := io.ReadAll(serverStr)
		received <- readResult{b, err}
	}()
	defer func() {
		serverStr.CancelRead(0)
		<-readerDone
	}()
	require.NoError(t, clientStr.SetWriteDeadline(time.Now().Add(scaleDuration(5*time.Second))))
	n, err := clientStr.Write(data[written:])
	require.NoError(t, err)
	require.Equal(t, len(data), written+n)
	require.NoError(t, clientStr.Close())
	result := <-received
	require.NoError(t, result.err)
	require.Equal(t, data, result.data)
}
