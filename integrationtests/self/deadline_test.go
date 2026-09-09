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
	server, err := quic.Listen(newUDPConnLocalhost(t), getTLSConfig(), getQuicConfig(&quic.Config{
		InitialStreamReceiveWindow: 1024,
		MaxStreamReceiveWindow:     1024,
	}))
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

// armDeadline keeps the async case bounded even if its setter is delayed.
// Cleanup joins the setter before the caller resets a deadline or closes a stream.
func armDeadline(t *testing.T, async bool, set func(time.Time) error) func() {
	t.Helper()
	if !async {
		require.NoError(t, set(time.Now().Add(scaleDuration(10*time.Millisecond))))
		return func() {}
	}
	require.NoError(t, set(time.Now().Add(scaleDuration(5*time.Second))))
	done := make(chan struct{})
	timer := time.AfterFunc(scaleDuration(10*time.Millisecond), func() {
		defer close(done)
		_ = set(time.Now())
	})
	join := func() {
		if timer.Stop() {
			close(done)
		}
		<-done
	}
	t.Cleanup(join)
	return join
}

func TestReadDeadlineSync(t *testing.T)  { testReadDeadline(t, false) }
func TestReadDeadlineAsync(t *testing.T) { testReadDeadline(t, true) }

func testReadDeadline(t *testing.T, async bool) {
	t.Helper()
	serverStr, clientStr := setupDeadlineTest(t)
	buf := make([]byte, 1)
	// The peer sends nothing until all ten blocked reads have expired.
	for range 10 {
		join := armDeadline(t, async, clientStr.SetReadDeadline)
		n, err := clientStr.Read(buf)
		join()
		require.Zero(t, n)
		require.ErrorIs(t, err, os.ErrDeadlineExceeded)
	}
	require.NoError(t, clientStr.SetReadDeadline(time.Now().Add(scaleDuration(5*time.Second))))
	payload := []byte("data after read deadline")
	require.NoError(t, serverStr.SetWriteDeadline(time.Now().Add(scaleDuration(5*time.Second))))
	_, err := serverStr.Write(payload)
	require.NoError(t, err)
	require.NoError(t, serverStr.Close())
	data, err := io.ReadAll(clientStr)
	require.NoError(t, err)
	require.Equal(t, payload, data)
}

func TestWriteDeadlineSync(t *testing.T)  { testWriteDeadline(t, false) }
func TestWriteDeadlineAsync(t *testing.T) { testWriteDeadline(t, true) }

func testWriteDeadline(t *testing.T, async bool) {
	t.Helper()
	serverStr, clientStr := setupDeadlineTest(t)
	// The peer's receive window stays at 1 KiB and it does not read yet.
	// This payload exceeds both that window and the stream's local send buffer.
	payload := PRDataLong[:256<<10]
	written := 0
	for range 10 {
		join := armDeadline(t, async, clientStr.SetWriteDeadline)
		n, err := clientStr.Write(payload[written:])
		join()
		written += n
		require.ErrorIs(t, err, os.ErrDeadlineExceeded)
		require.Less(t, written, len(payload))
	}
	type result struct {
		data []byte
		err  error
	}
	results := make(chan result, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		data, err := io.ReadAll(serverStr)
		results <- result{data, err}
	}()
	// Also join on assertion failures, before connection cleanup runs.
	defer func() {
		serverStr.CancelRead(0)
		<-done
	}()
	require.NoError(t, clientStr.SetWriteDeadline(time.Now().Add(scaleDuration(5*time.Second))))
	n, err := clientStr.Write(payload[written:])
	require.NoError(t, err)
	require.Equal(t, len(payload), written+n)
	require.NoError(t, clientStr.Close())
	select {
	case result := <-results:
		require.NoError(t, result.err)
		require.Equal(t, payload, result.data)
	case <-time.After(scaleDuration(5 * time.Second)):
		t.Fatal("timed out waiting for peer to read")
	}
}
