package http3

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go/http3/qlog"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/quic-go/quic-go/quicvarint"
	"github.com/stretchr/testify/require"
)

// goAwayRecorder holds the GOAWAY event across connection cancellation.
type goAwayRecorder struct {
	started chan struct{}
	release chan struct{}
	closed  chan struct{}
	mutex   sync.Mutex
	active  bool
	overlap bool
	late    bool
	event   qlog.FrameCreated
}

func (r *goAwayRecorder) RecordEvent(event qlogwriter.Event) {
	frame, ok := event.(qlog.FrameCreated)
	if !ok {
		return
	}
	if _, ok := frame.Frame.Frame.(qlog.GoAwayFrame); !ok {
		return
	}
	r.mutex.Lock()
	select {
	case <-r.closed:
		r.late = true
	default:
	}
	r.active = true
	r.event = frame
	r.mutex.Unlock()
	close(r.started)
	<-r.release
	r.mutex.Lock()
	r.active = false
	r.mutex.Unlock()
}

func (r *goAwayRecorder) Close() error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.overlap = r.active
	close(r.closed)
	return nil
}

func TestServerGOAWAYRecorderShutdown(t *testing.T) {
	client, conn := newConnPair(t)
	recorder := &goAwayRecorder{
		started: make(chan struct{}),
		release: make(chan struct{}),
		closed:  make(chan struct{}),
	}
	// Transport setup has already acquired its nil recorder. Configure only the
	// next producer, acquired synchronously by HTTP/3 server setup below.
	conn.QlogTrace().(*qlogTrace).recorder = recorder
	server := &Server{}
	serveDone := make(chan struct{})
	var shutdownDone chan struct{}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(recorder.release) }) }
	go func() {
		defer close(serveDone)
		_ = server.ServeQUICConn(conn)
	}()
	defer func() {
		release()
		_ = conn.CloseWithError(0, "test cleanup")
		select {
		case <-serveDone:
		case <-time.After(3 * time.Second):
			t.Error("server did not finish after releasing GOAWAY recorder")
		}
		if shutdownDone != nil {
			select {
			case <-shutdownDone:
			case <-time.After(3 * time.Second):
				t.Error("Shutdown did not finish after releasing GOAWAY recorder")
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	control, err := client.AcceptUniStream(ctx)
	require.NoError(t, err)
	require.NoError(t, control.SetReadDeadline(time.Now().Add(3*time.Second)))
	typ, err := quicvarint.Read(quicvarint.NewReader(control))
	require.NoError(t, err)
	require.EqualValues(t, streamTypeControlStream, typ)
	parser := frameParser{r: control}
	frame, err := parser.ParseNext(nil)
	require.NoError(t, err)
	require.IsType(t, &settingsFrame{}, frame)

	shutdownDone = make(chan struct{})
	go func() {
		defer close(shutdownDone)
		_ = server.Shutdown(ctx)
	}()
	select {
	case <-recorder.started:
	case <-ctx.Done():
		t.Fatal("GOAWAY recording did not start")
	}
	require.NoError(t, conn.CloseWithError(0, "cancellation during GOAWAY"))
	select {
	case <-recorder.closed:
		t.Error("recorder closed while GOAWAY recording was blocked")
	case <-time.After(50 * time.Millisecond):
	}
	release()
	select {
	case <-recorder.closed:
	case <-ctx.Done():
		t.Fatal("recorder did not close after GOAWAY recording finished")
	}
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	require.False(t, recorder.overlap)
	require.False(t, recorder.late)
	require.Equal(t, qlog.FrameCreated{
		StreamID: control.StreamID(),
		Frame:    qlog.Frame{Frame: qlog.GoAwayFrame{StreamID: 0}},
	}, recorder.event)
}

func TestServerGOAWAYRecorderAfterShutdown(t *testing.T) {
	client, _ := newConnPair(t)
	recorder := &shutdownRecorder{closed: make(chan struct{})}
	conn := newRawConn(client, false, nil, nopControlStrHandler, recorder, nil)
	require.NoError(t, client.CloseWithError(0, "test shutdown"))
	select {
	case <-recorder.closed:
	case <-time.After(time.Second):
		t.Fatal("recorder did not close")
	}

	// Exercise the same recording path as Server.handleConn after admission seals.
	conn.recordGoAway(3, 4)
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	require.Zero(t, recorder.afterClose)
	// Rejected recording must not leave tracked work behind.
	conn.qloggerWG.Wait()
}
