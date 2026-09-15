package http3

import (
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/stretchr/testify/require"
)

type shutdownRecorder struct {
	mutex      sync.Mutex
	closed     chan struct{}
	afterClose int
}

func (r *shutdownRecorder) RecordEvent(qlogwriter.Event) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	select {
	case <-r.closed:
		r.afterClose++
	default:
	}
}

func (r *shutdownRecorder) Close() error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	close(r.closed)
	return nil
}

func TestConnQlogRejectsLateWork(t *testing.T) {
	client, _ := newConnPair(t)
	stream, err := client.OpenStream()
	require.NoError(t, err)
	recorder := &shutdownRecorder{closed: make(chan struct{})}
	conn := newRawConn(client, true, nil, nopControlStrHandler, recorder, nil)
	require.NoError(t, client.CloseWithError(0, "test shutdown"))
	select {
	case <-recorder.closed:
	case <-time.After(time.Second):
		t.Fatal("qlog shutdown did not finish")
	}
	require.Error(t, conn.sendDatagram(0, []byte("late")))
	_, err = conn.openControlStream(&settingsFrame{})
	require.Error(t, err)
	_, err = conn.TrackStream(stream)
	require.Error(t, err)
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	require.Zero(t, recorder.afterClose, "late work must not use the closed qlog producer")
}
