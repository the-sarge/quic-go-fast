package http3

import (
	"context"
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
