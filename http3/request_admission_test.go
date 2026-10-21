package http3

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/require"
)

// Hold the existing map lock to pause admission after tracking has acquired its
// work obligation. Stack observation only establishes this test-owned schedule;
// neither a sleep nor a production hook decides when to close the connection.
func pauseRequestAdmission(t *testing.T, cc *ClientConn) (wait, resume func()) {
	t.Helper()
	cc.rawConn.streamMx.Lock()
	var once sync.Once
	resume = func() { once.Do(cc.rawConn.streamMx.Unlock) }
	t.Cleanup(resume)
	wait = func() { t.Helper(); waitRequestAdmission(t) }
	return wait, resume
}

func waitRequestAdmission(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool {
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		return strings.Contains(string(buf[:n]), "http3.(*rawConn).TrackStream(")
	}, 3*time.Second, time.Millisecond, "request did not reach tracking")
}

func requireAdmissionCleanup(t *testing.T, cc *ClientConn) {
	t.Helper()
	require.False(t, cc.rawConn.hasActiveStreams())
	done := make(chan struct{})
	go func() { cc.rawConn.qloggerWG.Wait(); close(done) }()
	receiveExchange(t, done)
}

func TestRequestAdmissionAfterTracking(t *testing.T) {
	for _, logging := range []bool{false, true} {
		name := "without_qlog"
		if logging {
			name = "with_qlog"
		}
		t.Run(name, func(t *testing.T) {
			client, _ := newConnPair(t)
			var rec *streamLifetimeRecorder
			if logging {
				rec = &streamLifetimeRecorder{closed: make(chan struct{})}
				client.QlogTrace().(*qlogTrace).recorder = rec
			}
			cc := (&Transport{}).NewClientConn(client)
			wait, resume := pauseRequestAdmission(t, cc)
			result := make(chan error, 1)
			go func() {
				_, err := cc.OpenRequestStream(t.Context())
				result <- err
			}()
			wait()
			require.NoError(t, client.CloseWithError(quic.ApplicationErrorCode(ErrCodeExcessiveLoad), "admission ended"))
			receiveExchange(t, client.Context().Done())
			resume()
			err := receiveExchange(t, result)
			requireHTTP3Error(t, err, false, "admission ended")
			_, private := errors.AsType[*errConnUnusable](err)
			require.False(t, private)
			requireAdmissionCleanup(t, cc)
			if rec != nil {
				receiveExchange(t, rec.closed)
				rec.mx.Lock()
				defer rec.mx.Unlock()
				require.Zero(t, rec.late)
				require.Zero(t, rec.overlap)
			}
		})
	}
}

func TestRequestAdmissionAfterTrackingTransport(t *testing.T) {
	client, _ := newConnPair(t)
	replacement, server := newConnPair(t)
	var dials atomic.Int32
	created := make(chan *ClientConn, 1)
	tr := &Transport{
		Dial: func(context.Context, string, *tls.Config, *quic.Config) (*quic.Conn, error) {
			if dials.Add(1) == 1 {
				return client, nil
			}
			return replacement, nil
		},
		newClientConn: func(conn *quic.Conn) clientConn {
			cc := newClientConn(conn, false, nil, 0, false, nil)
			if conn == client {
				cc.rawConn.streamMx.Lock()
				created <- cc
			}
			return cc
		},
	}
	t.Cleanup(func() { tr.Close() })
	body := &closeSensitiveInput{reader: bytes.NewReader([]byte("untouched upload"))}
	req := httptest.NewRequest(http.MethodPost, "https://example.com/", body).WithContext(t.Context())
	require.Nil(t, req.GetBody)
	type response struct {
		rsp *http.Response
		err error
	}
	result := make(chan response, 1)
	go func() { rsp, err := tr.RoundTrip(req); result <- response{rsp, err} }()
	cc := receiveExchange(t, created)
	// The constructor handed back a connection with its map lock held. Reuse
	// the same bounded observation as the public-opening regression.
	var once sync.Once
	resume := func() { once.Do(cc.rawConn.streamMx.Unlock) }
	t.Cleanup(resume)
	waitRequestAdmission(t)
	require.Zero(t, body.closes.Load())
	require.EqualValues(t, len("untouched upload"), body.reader.Len())
	require.NoError(t, client.CloseWithError(0, "admission ended"))
	receiveExchange(t, client.Context().Done())
	resume()

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	str, err := server.AcceptStream(ctx)
	require.NoError(t, err, "Transport must redial for untouched input")
	require.NoError(t, str.SetReadDeadline(time.Now().Add(3*time.Second)))
	decodeHeader(t, str)
	upload, err := io.ReadAll(str)
	require.NoError(t, err)
	require.Equal(t, []byte("\x00\x10untouched upload"), upload)
	_, err = str.Write(encodeResponse(t, http.StatusOK))
	require.NoError(t, err)
	require.NoError(t, str.Close())
	res := receiveExchange(t, result)
	require.NoError(t, res.err)
	require.Equal(t, http.StatusOK, res.rsp.StatusCode)
	_, err = io.Copy(io.Discard, res.rsp.Body)
	require.NoError(t, err)
	require.NoError(t, res.rsp.Body.Close())
	receiveExchange(t, res.rsp.Body.(*exchangeBody).lifetime.stopped)
	require.EqualValues(t, 2, dials.Load())
	require.EqualValues(t, 1, body.closes.Load())
	require.Zero(t, body.reader.Len())
	requireAdmissionCleanup(t, cc)
}

func newAdmissionSuppliedConn(t *testing.T) (*quic.Conn, context.CancelCauseFunc) {
	t.Helper()
	parentCancel := make(chan context.CancelCauseFunc, 1)
	transport := &quic.Transport{
		Conn: newUDPConnLocalhost(t),
		ConnContext: func(ctx context.Context, _ *quic.ClientInfo) (context.Context, error) {
			ctx, cancel := context.WithCancelCause(ctx)
			parentCancel <- cancel
			return ctx, nil
		},
	}
	t.Cleanup(func() { transport.Close() })
	listener, err := transport.Listen(getTLSConfig(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { listener.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	peer, err := quic.Dial(ctx, newUDPConnLocalhost(t), listener.Addr(), getTLSClientConfig(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { peer.CloseWithError(0, "test cleanup") })
	supplied, err := listener.Accept(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { supplied.CloseWithError(0, "test cleanup") })
	return supplied, receiveExchange(t, parentCancel)
}

func TestRequestAdmissionAfterTrackingSuppliedContext(t *testing.T) {
	for _, raw := range []bool{false, true} {
		name := "client"
		if raw {
			name = "raw"
		}
		t.Run(name, func(t *testing.T) {
			conn, cancel := newAdmissionSuppliedConn(t)
			defer cancel(nil)
			var cc *ClientConn
			if raw {
				cc = (&Transport{}).NewRawClientConn(conn).ClientConn
			} else {
				cc = (&Transport{}).NewClientConn(conn)
			}
			// Live supplied connections retain ordinary admission and cleanup.
			str, err := cc.OpenRequestStream(t.Context())
			require.NoError(t, err)
			str.CancelRead(0)
			str.CancelWrite(0)
			require.False(t, cc.rawConn.hasActiveStreams())
			wait, resume := pauseRequestAdmission(t, cc)
			result := make(chan error, 1)
			go func() { _, err := cc.OpenRequestStream(t.Context()); result <- err }()
			wait()
			cause := errors.New("supplied parent ended")
			cancel(cause)
			receiveExchange(t, conn.Context().Done())
			resume()
			err = receiveExchange(t, result)
			require.ErrorIs(t, err, cause)
			_, private := errors.AsType[*errConnUnusable](err)
			require.False(t, private, "public opening must not expose the retry classification")
			requireAdmissionCleanup(t, cc)
		})
	}
}

func TestRequestAdmissionAfterTrackingCanceledRequest(t *testing.T) {
	client, _ := newConnPair(t)
	cc := (&Transport{}).NewClientConn(client)
	wait, resume := pauseRequestAdmission(t, cc)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	body := &closeSensitiveInput{reader: bytes.NewReader([]byte("untouched"))}
	req := httptest.NewRequest(http.MethodPost, "https://example.com/", body).WithContext(ctx)
	result := make(chan error, 1)
	go func() { _, err := cc.RoundTrip(req); result <- err }()
	wait()
	cancel()
	require.NoError(t, client.CloseWithError(0, "admission ended"))
	receiveExchange(t, client.Context().Done())
	resume()
	require.ErrorIs(t, receiveExchange(t, result), context.Canceled)
	require.EqualValues(t, 1, body.closes.Load())
	requireAdmissionCleanup(t, cc)
}

func TestRequestAdmissionAfterTrackingWriteFailure(t *testing.T) {
	client, _ := newConnPair(t)
	var dials atomic.Int32
	tr := &Transport{Dial: func(context.Context, string, *tls.Config, *quic.Config) (*quic.Conn, error) {
		dials.Add(1)
		return client, nil
	}}
	t.Cleanup(func() { tr.Close() })
	body := &closeSensitiveInput{reader: bytes.NewReader([]byte("upload"))}
	ctx := httptrace.WithClientTrace(t.Context(), &httptrace.ClientTrace{
		WroteHeaders: func() { client.CloseWithError(0, "write phase ended") },
	})
	req := httptest.NewRequest(http.MethodPost, "https://example.com/", body).WithContext(ctx)
	require.Nil(t, req.GetBody)
	_, err := tr.RoundTrip(req)
	require.Error(t, err)
	_, private := errors.AsType[*errConnUnusable](err)
	require.False(t, private)
	require.EqualValues(t, 1, dials.Load(), "write-phase failure must not permit replay")
	require.EqualValues(t, 1, body.closes.Load())
}
