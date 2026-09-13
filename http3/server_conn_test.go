package http3

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/quic-go/quic-go/quicvarint"
	"github.com/stretchr/testify/require"
)

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
