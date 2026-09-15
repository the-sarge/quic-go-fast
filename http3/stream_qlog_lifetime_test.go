package http3

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go/http3/qlog"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/stretchr/testify/require"
)

// streamLifetimeRecorder detects recorder overlap and late events.
type streamLifetimeRecorder struct {
	mx      sync.Mutex
	closed  chan struct{}
	entered chan struct{}
	release chan struct{}
	active  int
	overlap int
	late    int
	events  []qlogwriter.Event
	block   func(qlogwriter.Event) bool
}

func (r *streamLifetimeRecorder) RecordEvent(e qlogwriter.Event) {
	r.mx.Lock()
	r.active++
	r.events = append(r.events, e)
	select {
	case <-r.closed:
		r.late++
	default:
	}
	r.mx.Unlock()
	if r.block != nil && r.block(e) {
		close(r.entered)
		<-r.release
	}
	r.mx.Lock()
	r.active--
	r.mx.Unlock()
}

func (r *streamLifetimeRecorder) Close() error {
	r.mx.Lock()
	defer r.mx.Unlock()
	r.overlap += r.active
	close(r.closed)
	return nil
}

func waitQlogBoundary(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("qlog lifetime did not reach expected boundary")
	}
}

func TestQlogLifetimeIncompleteHeader(t *testing.T) {
	client, server := newConnPair(t)
	rec := &streamLifetimeRecorder{closed: make(chan struct{}), entered: make(chan struct{}), release: make(chan struct{}), block: isParsedHeaderEvent}
	var release sync.Once
	defer release.Do(func() { close(rec.release) })
	conn := newRawServerConn(server, false, 0, rec, nil, context.Background(), nil, 0)
	str, err := client.OpenStream()
	require.NoError(t, err)
	// Announce a header block but end the request before supplying its payload.
	_, err = str.Write((&headersFrame{Length: 8}).Append(nil))
	require.NoError(t, err)
	require.NoError(t, str.Close())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	incoming, err := server.AcceptStream(ctx)
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn.HandleRequestStream(incoming)
	}()
	t.Cleanup(func() {
		release.Do(func() { close(rec.release) })
		_ = conn.CloseWithError(0, "test cleanup")
		waitQlogBoundary(t, done)
		waitQlogBoundary(t, rec.closed)
	})
	waitQlogBoundary(t, rec.entered)
	// Receive cleanup already happened inside the failed read. Connection
	// cancellation now ends the send half while the real header event is active.
	require.NoError(t, conn.CloseWithError(0, "test cancellation"))
	require.Eventually(t, func() bool {
		conn.rawConn.qloggerMx.Lock()
		defer conn.rawConn.qloggerMx.Unlock()
		return conn.rawConn.qloggerClosing
	}, time.Second, time.Millisecond)
	select {
	case <-rec.closed:
		t.Error("recorder closed during final header recording")
	default:
	}
	release.Do(func() { close(rec.release) })
	waitQlogBoundary(t, done)
	waitQlogBoundary(t, rec.closed)
	rec.mx.Lock()
	defer rec.mx.Unlock()
	require.Zero(t, rec.overlap, "Close overlapped the final invalid-header RecordEvent")
	require.Zero(t, rec.late)
}

func TestQlogLifetimeReturnedStreamData(t *testing.T) {
	for _, duringRecording := range []bool{false, true} {
		name := "after recorder closure"
		if duringRecording {
			name = "during admitted recording"
		}
		t.Run(name, func(t *testing.T) {
			client, server := newConnPair(t)
			rec := &streamLifetimeRecorder{closed: make(chan struct{})}
			if duringRecording {
				rec.entered, rec.release = make(chan struct{}), make(chan struct{})
				rec.block = isCreatedDataEvent
			}
			returned := make(chan *Stream, 1)
			conn := newRawServerConn(server, false, 0, rec, nil, context.Background(), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				returned <- w.(HTTPStreamer).HTTPStream()
			}), 0)
			str, err := client.OpenStream()
			require.NoError(t, err)
			_, err = str.Write(encodeRequest(t, httptest.NewRequest(http.MethodGet, "https://example.com/", nil)))
			require.NoError(t, err)
			require.NoError(t, str.Close())
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			incoming, err := server.AcceptStream(ctx)
			require.NoError(t, err)
			conn.HandleRequestStream(incoming)
			hstr := <-returned
			var writeErr error
			done := make(chan struct{})
			var release sync.Once
			if duringRecording {
				t.Cleanup(func() {
					release.Do(func() { close(rec.release) })
					_ = conn.CloseWithError(0, "test cleanup")
					waitQlogBoundary(t, done)
					waitQlogBoundary(t, rec.closed)
				})
				go func() {
					defer close(done)
					_, writeErr = hstr.Write([]byte("admitted DATA"))
				}()
				waitQlogBoundary(t, rec.entered)
			}
			hstr.CancelRead(0)
			hstr.CancelWrite(0)
			require.False(t, conn.rawConn.hasActiveStreams(), "stream-map cleanup must complete")
			require.NoError(t, conn.CloseWithError(0, "test cancellation"))
			if duringRecording {
				require.Eventually(t, func() bool {
					conn.rawConn.qloggerMx.Lock()
					defer conn.rawConn.qloggerMx.Unlock()
					return conn.rawConn.qloggerClosing
				}, time.Second, time.Millisecond)
				select {
				case <-rec.closed:
					t.Error("recorder closed during admitted stream recording")
				default:
				}
				release.Do(func() { close(rec.release) })
				waitQlogBoundary(t, done)
			}
			waitQlogBoundary(t, rec.closed)
			if !duringRecording {
				_, writeErr = hstr.Write([]byte("late DATA"))
			}
			require.Error(t, writeErr, "canceled stream must still report its write failure")
			rec.mx.Lock()
			defer rec.mx.Unlock()
			require.Zero(t, rec.overlap)
			require.Zero(t, rec.late, "returned stream recorded after recorder Close")
		})
	}
}

func TestQlogLifetimeClientResponse(t *testing.T) {
	client, server := newConnPair(t)
	rec := &streamLifetimeRecorder{closed: make(chan struct{}), entered: make(chan struct{}), release: make(chan struct{}), block: isParsedHeaderEvent}
	client.QlogTrace().(*qlogTrace).recorder = rec
	cc := (&Transport{}).NewClientConn(client)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	str, err := cc.OpenRequestStream(ctx)
	require.NoError(t, err)
	require.NoError(t, str.SendRequestHeader(httptest.NewRequest(http.MethodGet, "https://example.com/", nil)))
	require.NoError(t, str.Close())
	incoming, err := server.AcceptStream(ctx)
	require.NoError(t, err)
	_, err = incoming.Write((&headersFrame{Length: 8}).Append(nil))
	require.NoError(t, err)
	require.NoError(t, incoming.Close())
	done := make(chan struct{})
	var readErr error
	var release sync.Once
	t.Cleanup(func() {
		release.Do(func() { close(rec.release) })
		_ = client.CloseWithError(0, "test cleanup")
		waitQlogBoundary(t, done)
		waitQlogBoundary(t, rec.closed)
	})
	go func() {
		defer close(done)
		_, readErr = str.ReadResponse()
	}()
	waitQlogBoundary(t, rec.entered)
	require.NoError(t, client.CloseWithError(0, "test cancellation"))
	require.Eventually(t, func() bool {
		cc.rawConn.qloggerMx.Lock()
		defer cc.rawConn.qloggerMx.Unlock()
		return cc.rawConn.qloggerClosing
	}, time.Second, time.Millisecond)
	select {
	case <-rec.closed:
		t.Error("recorder closed during admitted response recording")
	default:
	}
	release.Do(func() { close(rec.release) })
	waitQlogBoundary(t, done)
	waitQlogBoundary(t, rec.closed)
	require.Error(t, readErr)
	rec.mx.Lock()
	defer rec.mx.Unlock()
	require.Zero(t, rec.overlap)
	require.Zero(t, rec.late)
}

func isParsedHeaderEvent(e qlogwriter.Event) bool {
	f, ok := e.(qlog.FrameParsed)
	if !ok {
		return false
	}
	_, ok = f.Frame.Frame.(qlog.HeadersFrame)
	return ok
}

func isCreatedDataEvent(e qlogwriter.Event) bool {
	f, ok := e.(qlog.FrameCreated)
	if !ok {
		return false
	}
	_, ok = f.Frame.Frame.(qlog.DataFrame)
	return ok
}

func TestQlogLifetimeResponseCompletion(t *testing.T) {
	client, server := newConnPair(t)
	rec := &streamLifetimeRecorder{closed: make(chan struct{}), entered: make(chan struct{}), release: make(chan struct{}), block: isCreatedDataEvent}
	ready, resume := make(chan struct{}), make(chan struct{})
	var writeErr error
	conn := newRawServerConn(server, false, 0, rec, nil, context.Background(), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Trailer", "X-Final")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(ready)
		<-resume
		_, writeErr = w.Write([]byte("final data"))
		w.Header().Set("X-Final", "done")
	}), 0)
	str, err := client.OpenStream()
	require.NoError(t, err)
	_, err = str.Write(encodeRequest(t, httptest.NewRequest(http.MethodGet, "https://example.com/", nil)))
	require.NoError(t, err)
	require.NoError(t, str.Close())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	incoming, err := server.AcceptStream(ctx)
	require.NoError(t, err)
	done := make(chan struct{})
	var resumeOnce, release sync.Once
	t.Cleanup(func() {
		resumeOnce.Do(func() { close(resume) })
		release.Do(func() { close(rec.release) })
		_ = conn.CloseWithError(0, "test cleanup")
		waitQlogBoundary(t, done)
		waitQlogBoundary(t, rec.closed)
	})
	go func() {
		defer close(done)
		conn.HandleRequestStream(incoming)
	}()
	waitQlogBoundary(t, ready)
	require.NoError(t, conn.CloseWithError(0, "test cancellation"))
	require.Eventually(t, func() bool {
		conn.rawConn.qloggerMx.Lock()
		defer conn.rawConn.qloggerMx.Unlock()
		return conn.rawConn.qloggerClosing
	}, time.Second, time.Millisecond)
	// Already admitted handler work must retain its DATA and final trailer
	// attempt even when both events are produced after connection cancellation.
	resumeOnce.Do(func() { close(resume) })
	waitQlogBoundary(t, rec.entered)
	select {
	case <-rec.closed:
		t.Error("recorder closed before admitted handler completion")
	default:
	}
	release.Do(func() { close(rec.release) })
	waitQlogBoundary(t, done)
	waitQlogBoundary(t, rec.closed)
	require.Error(t, writeErr)
	rec.mx.Lock()
	defer rec.mx.Unlock()
	require.Zero(t, rec.overlap)
	require.Zero(t, rec.late)
	var createdHeaders []qlog.HeadersFrame
	for _, e := range rec.events {
		if f, ok := e.(qlog.FrameCreated); ok {
			if h, ok := f.Frame.Frame.(qlog.HeadersFrame); ok {
				createdHeaders = append(createdHeaders, h)
			}
		}
	}
	require.Len(t, createdHeaders, 2)
	require.Equal(t, []qlog.HeaderField{{Name: "x-final", Value: "done"}}, createdHeaders[1].HeaderFields)
}
