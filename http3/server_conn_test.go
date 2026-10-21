package http3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/quic-go/quic-go/quicvarint"
	"github.com/stretchr/testify/require"
)

func TestRawServerConnTruncatedRequestBody(t *testing.T) {
	client, server := newConnPair(t)
	str, err := client.OpenStream()
	require.NoError(t, err)
	require.NoError(t, str.SetDeadline(time.Now().Add(time.Second)))
	req := httptest.NewRequest(http.MethodPost, "https://www.example.com", bytes.NewBufferString("foo"))
	req.ContentLength = 6
	_, err = str.Write(encodeRequest(t, req))
	require.NoError(t, err)
	require.NoError(t, str.Close())

	called := false
	s := &Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		require.Equal(t, int64(6), r.ContentLength)
		data, err := io.ReadAll(r.Body)
		require.Equal(t, "foo", string(data))
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	})}
	conn, err := s.NewRawServerConn(server)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	incoming, err := server.AcceptStream(ctx)
	require.NoError(t, err)
	conn.HandleRequestStream(incoming)
	require.True(t, called)
	expectStreamReadReset(t, str, quic.StreamErrorCode(ErrCodeMessageError))
}

func TestRawServerConnPreservesCallerReadDeadline(t *testing.T) {
	for _, timeout := range []time.Duration{0, -1} {
		for _, completeHeaders := range []bool{false, true} {
			t.Run(fmt.Sprintf("timeout_%s/complete_headers_%t", timeout, completeHeaders), func(t *testing.T) {
				client, server := newConnPair(t)
				bodyResult := make(chan error, 1)
				s := Server{ReadHeaderTimeout: timeout, Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
					_, err := io.ReadAll(r.Body)
					bodyResult <- err
				})}
				conn, err := s.NewRawServerConn(server)
				require.NoError(t, err)
				str, err := client.OpenStream()
				require.NoError(t, err)
				data := []byte{0x1, 0x20, 0}
				if completeHeaders {
					data = encodeRequest(t, httptest.NewRequest(http.MethodPost, "https://example.com", nil))
				}
				_, err = str.Write(data)
				require.NoError(t, err)
				incoming, err := server.AcceptStream(t.Context())
				require.NoError(t, err)
				// The caller owns this deadline for both headers and the body.
				require.NoError(t, incoming.SetReadDeadline(time.Now().Add(scaleDuration(50*time.Millisecond))))
				done := make(chan struct{})
				go func() { defer close(done); conn.HandleRequestStream(incoming) }()
				defer func() {
					conn.CloseWithError(0, "")
					<-done
				}()
				if completeHeaders {
					select {
					case err := <-bodyResult:
						require.ErrorIs(t, err, os.ErrDeadlineExceeded)
					case <-time.After(scaleDuration(time.Second)):
						t.Fatal("caller body-read deadline was lost")
					}
				} else {
					// CI scales the caller deadline too; allow a matching margin
					// instead of the reset helper's fixed one-second deadline.
					require.NoError(t, str.SetReadDeadline(time.Now().Add(scaleDuration(time.Second))))
					_, err = str.Read(make([]byte, 1))
					var streamErr *quic.StreamError
					require.ErrorAs(t, err, &streamErr)
					require.True(t, streamErr.Remote)
					require.Equal(t, quic.StreamErrorCode(ErrCodeRequestIncomplete), streamErr.ErrorCode)
				}
			})
		}
	}
}

func TestRawServerConnReadHeaderTimeoutConfig(t *testing.T) {
	for _, timeout := range []time.Duration{0, -1, 50 * time.Millisecond} {
		t.Run(timeout.String(), func(t *testing.T) {
			_, server := newConnPair(t)
			s := Server{ReadHeaderTimeout: timeout}
			conn, err := s.NewRawServerConn(server)
			require.NoError(t, err)
			if timeout == 0 {
				require.Equal(t, 30*time.Second, conn.readHeaderTimeout)
			} else {
				require.Equal(t, timeout, conn.readHeaderTimeout)
			}
		})
	}
}

func TestRawServerConnHeaderDeadlineWithActiveRequest(t *testing.T) {
	for _, prefix := range [][]byte{{0x40}, {0x1, 0x40}, {0x21, 0x40}, {0x1, 0x20, 0}} {
		t.Run(fmt.Sprintf("prefix_%x", prefix), func(t *testing.T) {
			client, server := newConnPair(t)
			entered, release := make(chan struct{}), make(chan struct{})
			defer close(release)
			s := Server{
				IdleTimeout:       time.Second,
				ReadHeaderTimeout: 50 * time.Millisecond,
				Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					close(entered)
					<-release
					body, err := io.ReadAll(r.Body)
					require.NoError(t, err)
					require.Equal(t, "body", string(body))
				}),
			}
			conn, err := s.NewRawServerConn(server)
			require.NoError(t, err)
			startRequest := func(data []byte) (*quic.Stream, <-chan struct{}) {
				t.Helper()
				str, err := client.OpenStream()
				require.NoError(t, err)
				_, err = str.Write(data)
				require.NoError(t, err)
				incoming, err := server.AcceptStream(t.Context())
				require.NoError(t, err)
				done := make(chan struct{})
				go func() { defer close(done); conn.HandleRequestStream(incoming) }()
				return str, done
			}
			active, activeDone := startRequest(encodeRequest(t, httptest.NewRequest(http.MethodPost, "https://example.com", nil)))
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("handler did not start")
			}
			pending, pendingDone := startRequest(prefix)
			expectStreamReadReset(t, pending, quic.StreamErrorCode(ErrCodeRequestIncomplete))
			expectStreamWriteReset(t, pending, quic.StreamErrorCode(ErrCodeRequestIncomplete))
			<-pendingDone
			require.Zero(t, conn.pendingHeaderBytes.Load())
			require.NoError(t, server.Context().Err())
			// The header deadline must have been cleared for the active body.
			data := (&dataFrame{Length: 4}).Append(nil)
			_, err = active.Write(append(data, "body"...))
			require.NoError(t, err)
			require.NoError(t, active.Close())
			release <- struct{}{}
			<-activeDone
		})
	}
}

func TestRawServerConnPendingHeaderBudget(t *testing.T) {
	client, server := newConnPair(t)
	var calls atomic.Int32
	s := Server{MaxHeaderBytes: 4096, Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		// Consume EOF so handler completion cannot race the client's FIN.
		_, err := io.Copy(io.Discard, r.Body)
		require.NoError(t, err)
		calls.Add(1)
	})}
	conn, err := s.NewRawServerConn(server)
	require.NoError(t, err)
	startRequest := func(data []byte) (*quic.Stream, <-chan struct{}) {
		t.Helper()
		str, err := client.OpenStream()
		require.NoError(t, err)
		_, err = str.Write(data)
		require.NoError(t, err)
		incoming, err := server.AcceptStream(t.Context())
		require.NoError(t, err)
		done := make(chan struct{})
		go func() { defer close(done); conn.HandleRequestStream(incoming) }()
		return str, done
	}
	prefix := (&headersFrame{Length: 4096}).Append(nil)
	first, firstDone := startRequest(append(bytes.Clone(prefix), make([]byte, 1024)...))
	require.Eventually(t, func() bool { return conn.pendingHeaderBytes.Load() == 2048 }, time.Second, time.Millisecond)
	second, secondDone := startRequest(append(bytes.Clone(prefix), make([]byte, 1024)...))
	require.Eventually(t, func() bool { return conn.pendingHeaderBytes.Load() == 4096 }, time.Second, time.Millisecond)
	third, thirdDone := startRequest(prefix)
	expectStreamReadReset(t, third, quic.StreamErrorCode(ErrCodeExcessiveLoad))
	expectStreamWriteReset(t, third, quic.StreamErrorCode(ErrCodeExcessiveLoad))
	<-thirdDone
	require.EqualValues(t, 4096, conn.pendingHeaderBytes.Load())
	first.CancelWrite(0)
	second.CancelWrite(0)
	<-firstDone
	<-secondDone
	require.Zero(t, conn.pendingHeaderBytes.Load())
	// Invalid QPACK must release its reservation too.
	invalid, invalidDone := startRequest([]byte{0x1, 0x1, 0xff})
	expectStreamReadReset(t, invalid, quic.StreamErrorCode(ErrCodeQPACKDecompressionFailed))
	<-invalidDone
	require.Zero(t, conn.pendingHeaderBytes.Load())
	valid, validDone := startRequest(encodeRequest(t, httptest.NewRequest(http.MethodGet, "https://example.com", nil)))
	require.NoError(t, valid.Close())
	<-validDone
	require.EqualValues(t, 1, calls.Load())
	require.Zero(t, conn.pendingHeaderBytes.Load())
	require.NoError(t, server.Context().Err())
}

func TestRawServerConnHeaderTimeoutDuringQlog(t *testing.T) {
	client, server := newConnPair(t)
	recorder := &streamLifetimeRecorder{
		closed: make(chan struct{}), entered: make(chan struct{}),
		release: make(chan struct{}), block: isParsedHeaderEvent,
	}
	var calls atomic.Int32
	conn := newRawServerConn(server, false, 0, recorder, nil, context.Background(),
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }), 4096)
	conn.readHeaderTimeout = scaleDuration(100 * time.Millisecond)
	str, err := client.OpenStream()
	require.NoError(t, err)
	_, err = str.Write(encodeRequest(t, httptest.NewRequest(http.MethodGet, "https://example.com", nil)))
	require.NoError(t, err)
	incoming, err := server.AcceptStream(t.Context())
	require.NoError(t, err)
	done := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(recorder.release) }) }
	go func() { defer close(done); conn.HandleRequestStream(incoming) }()
	defer func() {
		release()
		conn.CloseWithError(0, "test cleanup")
		waitQlogBoundary(t, done)
	}()

	// Valid headers have decoded, but their parsed event has not returned.
	waitQlogBoundary(t, recorder.entered)
	require.Zero(t, conn.pendingHeaderBytes.Load())
	select {
	case <-str.Context().Done():
	case <-time.After(scaleDuration(time.Second)):
		t.Fatal("header timeout did not cancel the stream while qlog was blocked")
	}
	expectStreamReadReset(t, str, quic.StreamErrorCode(ErrCodeRequestIncomplete))
	expectStreamWriteReset(t, str, quic.StreamErrorCode(ErrCodeRequestIncomplete))
	release()
	waitQlogBoundary(t, done)
	require.Zero(t, calls.Load(), "timeout must prevent activation after successful decoding")
	require.Empty(t, conn.activeRequests)
}

// blockingRecorder holds a SETTINGS event in flight across connection shutdown.
type blockingRecorder struct {
	started chan struct{}
	release chan struct{}
	closed  chan struct{}
}

func (r *blockingRecorder) RecordEvent(qlogwriter.Event) {
	close(r.started)
	<-r.release
}

func (r *blockingRecorder) Close() error {
	close(r.closed)
	return nil
}

func TestRawServerConnQloggerWaitsForProducer(t *testing.T) {
	client, server := newConnPair(t)
	recorder := &blockingRecorder{started: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{})}
	// Always release the producer, including when the shutdown assertion fails.
	defer func() { close(recorder.release) }()
	conn := newRawServerConn(server, false, 0, recorder, nil, context.Background(), nil, 0)
	stream, err := client.OpenUniStream()
	require.NoError(t, err)
	data := quicvarint.Append(nil, streamTypeControlStream)
	data = (&settingsFrame{}).Append(data)
	_, err = stream.Write(data)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	incoming, err := server.AcceptUniStream(ctx)
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn.HandleUnidirectionalStream(incoming)
	}()
	select {
	case <-recorder.started:
	case <-ctx.Done():
		t.Fatal("SETTINGS producer did not start")
	}
	require.NoError(t, conn.CloseWithError(0, ""))
	select {
	case <-recorder.closed:
		t.Fatal("recorder closed while SETTINGS producer was active")
	case <-time.After(50 * time.Millisecond):
	}
	// Let RecordEvent finish, then wait for the connection callback to close it.
	recorder.release <- struct{}{}
	select {
	case <-recorder.closed:
	case <-ctx.Done():
		t.Fatal("recorder did not close after producer finished")
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("stream handler did not finish")
	}
}

func TestRawServerConnResponseCompletion(t *testing.T) {
	for _, failFlush := range []bool{false, true} {
		name := "informational response and trailers"
		if failFlush {
			name = "trailer attempt after flush failure"
		}
		t.Run(name, func(t *testing.T) {
			client, server := newConnPair(t)
			str, err := client.OpenStream()
			require.NoError(t, err)
			require.NoError(t, str.SetDeadline(time.Now().Add(time.Second)))
			_, err = str.Write(encodeRequest(t, httptest.NewRequest(http.MethodGet, "https://www.example.com", nil)))
			require.NoError(t, err)
			require.NoError(t, str.Close())

			var logs bytes.Buffer
			s := &Server{
				Logger: slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
				Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusEarlyHints)
					w.Header().Set("Trailer", "Result")
					_, err := w.Write([]byte("foobar"))
					require.NoError(t, err)
					w.Header().Set("Result", "done")
					if failFlush {
						require.NoError(t, http.NewResponseController(w).SetWriteDeadline(time.Now().Add(-time.Second)))
					}
				}),
			}
			conn, err := s.NewRawServerConn(server)
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			incoming, err := server.AcceptStream(ctx)
			require.NoError(t, err)
			conn.HandleRequestStream(incoming)

			if failFlush {
				// Both operations report their own error, in completion order.
				flush := bytes.Index(logs.Bytes(), []byte("could not flush to stream"))
				trailers := bytes.Index(logs.Bytes(), []byte("could not write trailers"))
				require.NotEqual(t, -1, flush, logs.String())
				require.Greater(t, trailers, flush, logs.String())
				return
			}
			require.Equal(t, []string{"103"}, decodeHeader(t, str)[":status"])
			headers := decodeHeader(t, str)
			require.Equal(t, []string{"200"}, headers[":status"])
			require.Equal(t, []string{"6"}, headers["content-length"])
			require.NotContains(t, headers, "result")
			fp := frameParser{r: str}
			frame, err := fp.ParseNext(nil)
			require.NoError(t, err)
			require.Equal(t, &dataFrame{Length: 6}, frame)
			body := make([]byte, 6)
			_, err = io.ReadFull(str, body)
			require.NoError(t, err)
			require.Equal(t, "foobar", string(body))
			require.Equal(t, []string{"done"}, decodeHeader(t, str)["result"])
			_, err = fp.ParseNext(nil)
			require.ErrorIs(t, err, io.EOF)
		})
	}
}

func TestRawServerConnMultipartFormCleanup(t *testing.T) {
	for _, tc := range []struct {
		name       string
		panicValue any
		failFlush  bool
		hijack     bool
	}{
		{name: "normal return"},
		{name: "response flush failure", failFlush: true},
		{name: "panic", panicValue: "handler panic"},
		{name: "abort handler", panicValue: http.ErrAbortHandler},
		{name: "hijacked stream", hijack: true},
		{name: "panic after hijack", hijack: true, panicValue: "handler panic"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			part, err := writer.CreateFormFile("upload", "upload.txt")
			require.NoError(t, err)
			const contents = "multipart upload contents"
			_, err = io.WriteString(part, contents)
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			req := httptest.NewRequest(http.MethodPost, "https://www.example.com", &body)
			req.Header.Set("Content-Type", writer.FormDataContentType())

			client, server := newConnPair(t)
			str, err := client.OpenStream()
			require.NoError(t, err)
			require.NoError(t, str.SetDeadline(time.Now().Add(time.Second)))
			_, err = str.Write(encodeRequest(t, req))
			require.NoError(t, err)
			require.NoError(t, str.Close())

			var tempFile string
			var form *multipart.Form
			var hijacked *Stream
			s := &Server{
				Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
				Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					// A zero memory limit forces even this small file to disk.
					require.NoError(t, r.ParseMultipartForm(0))
					form = r.MultipartForm
					t.Cleanup(func() { require.NoError(t, form.RemoveAll()) })
					file, err := form.File["upload"][0].Open()
					require.NoError(t, err)
					defer file.Close()
					require.IsType(t, &os.File{}, file)
					tempFile = file.(*os.File).Name()
					require.FileExists(t, tempFile)
					data, err := io.ReadAll(file)
					require.NoError(t, err)
					require.Equal(t, contents, string(data))

					if tc.hijack {
						hijacked = w.(HTTPStreamer).HTTPStream()
					}
					if tc.failFlush {
						require.NoError(t, http.NewResponseController(w).SetWriteDeadline(time.Now().Add(-time.Second)))
					}
					if tc.panicValue != nil {
						panic(tc.panicValue)
					}
				}),
			}
			conn, err := s.NewRawServerConn(server)
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			incoming, err := server.AcceptStream(ctx)
			require.NoError(t, err)
			conn.HandleRequestStream(incoming)

			require.NotEmpty(t, tempFile)
			if tc.hijack {
				// Taking over the stream also transfers multipart cleanup to the caller.
				require.FileExists(t, tempFile)
				file, err := form.File["upload"][0].Open()
				require.NoError(t, err)
				data, err := io.ReadAll(file)
				require.NoError(t, err)
				require.NoError(t, file.Close())
				require.Equal(t, contents, string(data))
				require.NoError(t, hijacked.Close())
				require.NoError(t, form.RemoveAll())
			}
			require.NoFileExists(t, tempFile)
		})
	}
}
