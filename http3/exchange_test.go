package http3

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/qpack"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/quicvarint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func receiveExchange[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(time.Second):
		t.Fatal("exchange did not finish")
		var zero T
		return zero
	}
}

func TestExchangeCanceledDialWaiter(t *testing.T) {
	// A canceled waiter must release its own acquisition without canceling the shared dial.
	dialing := make(chan struct{})
	entry := &roundTripperWithCount{dialing: dialing}
	tr := &Transport{Dial: func(context.Context, string, *tls.Config, *quic.Config) (*quic.Conn, error) { panic("unexpected dial") }, clients: map[string]*roundTripperWithCount{"example.com:443": entry}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := tr.RoundTrip(httptest.NewRequest(http.MethodGet, "https://example.com", nil).WithContext(ctx))
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, entry.useCount.Load())
	select {
	case <-dialing:
		t.Fatal("canceled the shared dial")
	default:
	}
}

func exchangeTransport(t *testing.T) (*Transport, *quic.Conn, *quic.Conn) {
	t.Helper()
	client, server := newConnPair(t)
	tr := &Transport{Dial: func(context.Context, string, *tls.Config, *quic.Config) (*quic.Conn, error) { return client, nil }}
	t.Cleanup(func() { tr.Close() })
	return tr, client, server
}

func exchangeResponse(t *testing.T, tr http.RoundTripper, server *quic.Conn, req *http.Request, headers []byte) (*http.Response, *quic.Stream) {
	t.Helper()
	type result struct {
		rsp *http.Response
		err error
	}
	resultCh := make(chan result, 1)
	go func() { rsp, err := tr.RoundTrip(req); resultCh <- result{rsp, err} }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	str, err := server.AcceptStream(ctx)
	require.NoError(t, err)
	require.NoError(t, str.SetReadDeadline(time.Now().Add(time.Second)))
	decodeHeader(t, str)
	_, err = str.Write(headers)
	require.NoError(t, err)
	res := receiveExchange(t, resultCh)
	require.NoError(t, res.err)
	t.Cleanup(func() { res.rsp.Body.Close() })
	return res.rsp, str
}

func exchangeEntry(t *testing.T, tr *Transport) *roundTripperWithCount {
	t.Helper()
	tr.mutex.Lock()
	defer tr.mutex.Unlock()
	entry := tr.clients["example.com:443"]
	require.NotNil(t, entry)
	return entry
}

func TestExchangeActiveMultiplexedResponses(t *testing.T) {
	tr, client, server := exchangeTransport(t)
	req := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	rsp1, str1 := exchangeResponse(t, tr, server, req, encodeResponse(t, http.StatusOK))
	rsp2, str2 := exchangeResponse(t, tr, server, req.Clone(req.Context()), encodeResponse(t, http.StatusOK))
	entry := exchangeEntry(t, tr)
	require.EqualValues(t, 2, entry.useCount.Load())
	require.NoError(t, rsp1.Body.Close())
	require.EqualValues(t, 1, entry.useCount.Load())
	tr.CloseIdleConnections()
	require.NoError(t, client.Context().Err())
	payload := []byte("still readable")
	frame := quicvarint.Append(nil, 0)
	frame = quicvarint.Append(frame, uint64(len(payload)))
	_, err := str2.Write(append(frame, payload...))
	require.NoError(t, err)
	require.NoError(t, str2.Close())
	data, err := io.ReadAll(rsp2.Body)
	require.NoError(t, err)
	require.Equal(t, payload, data)
	require.Zero(t, entry.useCount.Load())
	require.NoError(t, rsp2.Body.Close())
	require.Zero(t, entry.useCount.Load())
	str1.CancelRead(0)
	tr.CloseIdleConnections()
	receiveExchange(t, client.Context().Done())
}

// The request input rendezvous exposes upload entry and allows Close to unblock Read.
type exchangeInput struct {
	entered  chan struct{}
	unblock  chan struct{}
	finished chan struct{}
	once     sync.Once
	closes   atomic.Int32
}

func newExchangeInput() *exchangeInput {
	return &exchangeInput{entered: make(chan struct{}), unblock: make(chan struct{}), finished: make(chan struct{})}
}

func (b *exchangeInput) Read([]byte) (int, error) {
	b.once.Do(func() { close(b.entered) })
	select {
	case <-b.unblock:
	case <-b.finished:
	}
	return 0, io.EOF
}

func (b *exchangeInput) Close() error {
	if b.closes.Add(1) == 1 {
		close(b.unblock)
	}
	return nil
}

func TestExchangeEarlyResponsePreservesUpload(t *testing.T) {
	tr, _, server := exchangeTransport(t)
	body := newExchangeInput()
	t.Cleanup(func() {
		if body.closes.Load() == 0 {
			body.Close()
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, "https://example.com", body).WithContext(ctx)
	rsp, _ := exchangeResponse(t, tr, server, req, encodeResponse(t, http.StatusOK))
	receiveExchange(t, body.entered)
	require.NoError(t, rsp.Body.Close())
	require.Zero(t, body.closes.Load(), "ordinary response Close must preserve the upload")
}

func TestExchangeUploadTerminalEvents(t *testing.T) {
	for _, terminal := range []string{"response-close-then-cancel", "unread-cancel", "connection-close"} {
		t.Run(terminal, func(t *testing.T) {
			tr, client, server := exchangeTransport(t)
			body := newExchangeInput()
			t.Cleanup(func() {
				if body.closes.Load() == 0 {
					body.Close()
				}
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			req := httptest.NewRequest(http.MethodPost, "https://example.com", body).WithContext(ctx)
			rsp, str := exchangeResponse(t, tr, server, req, encodeResponse(t, http.StatusOK))
			receiveExchange(t, body.entered)
			entry := exchangeEntry(t, tr)
			if terminal == "response-close-then-cancel" {
				require.NoError(t, rsp.Body.Close())
				require.EqualValues(t, 1, entry.useCount.Load())
				require.Zero(t, body.closes.Load())
				tr.CloseIdleConnections()
				require.NoError(t, client.Context().Err())
			}
			if terminal == "connection-close" {
				require.NoError(t, client.CloseWithError(0, "test"))
			} else {
				cancel()
			}
			// Observe the owner shutdown as well as the externally visible input cleanup.
			receiveExchange(t, rsp.Body.(*exchangeBody).lifetime.stopped)
			require.EqualValues(t, 1, body.closes.Load())
			require.Zero(t, entry.useCount.Load())
			if terminal != "connection-close" {
				expectStreamWriteReset(t, str, quic.StreamErrorCode(ErrCodeRequestCanceled))
			}
			require.NoError(t, rsp.Body.Close())
			require.Zero(t, entry.useCount.Load())
		})
	}
}

func TestExchangeUploadFinishesAfterResponse(t *testing.T) {
	tr, client, server := exchangeTransport(t)
	body := newExchangeInput()
	t.Cleanup(func() {
		if body.closes.Load() == 0 {
			body.Close()
		}
	})
	req := httptest.NewRequest(http.MethodPost, "https://example.com", body)
	req.Trailer = http.Header{"Finished": {"yes"}}
	rsp, str := exchangeResponse(t, tr, server, req, encodeResponse(t, http.StatusOK))
	receiveExchange(t, body.entered)
	require.NoError(t, str.Close())
	_, err := io.ReadAll(rsp.Body)
	require.NoError(t, err)
	entry := exchangeEntry(t, tr)
	require.EqualValues(t, 1, entry.useCount.Load())
	tr.CloseIdleConnections()
	require.NoError(t, client.Context().Err())
	// Let Read finish normally; the uploader then closes input and sends trailers.
	close(body.finished)
	trailers := decodeHeader(t, str)
	require.Equal(t, []string{"yes"}, trailers["finished"])
	_, err = io.ReadAll(str)
	require.NoError(t, err)
	receiveExchange(t, rsp.Body.(*exchangeBody).lifetime.stopped)
	require.Zero(t, entry.useCount.Load())
	require.EqualValues(t, 1, body.closes.Load())
}

func TestExchangeGzipCompletion(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(map[bool]string{false: "buffered-source-eof", true: "decoder-error-before-source-eof"}[invalid], func(t *testing.T) {
			tr, _, server := exchangeTransport(t)
			// Encode a response with automatic gzip decoding.
			var encoded bytes.Buffer
			gz := gzip.NewWriter(&encoded)
			_, err := gz.Write(bytes.Repeat([]byte("x"), 32768))
			require.NoError(t, err)
			require.NoError(t, gz.Close())
			payload := encoded.Bytes()
			if invalid {
				payload = []byte("not a gzip stream")
			}
			header := exchangeHeaders(t, http.StatusOK, http.Header{"Content-Encoding": {"gzip"}})
			rsp, str := exchangeResponse(t, tr, server, httptest.NewRequest(http.MethodGet, "https://example.com", nil), header)
			frame := (&dataFrame{Length: uint64(len(payload))}).Append(nil)
			_, err = str.Write(append(frame, payload...))
			require.NoError(t, err)
			if !invalid {
				require.NoError(t, str.Close())
			}
			entry := exchangeEntry(t, tr)
			raw := &eofObservedBody{ReadCloser: rsp.Body.(*exchangeBody).ReadCloser.(*gzipReader).body}
			rsp.Body.(*exchangeBody).ReadCloser.(*gzipReader).body = raw
			buf := make([]byte, 1)
			n, err := rsp.Body.Read(buf)
			if invalid {
				require.ErrorIs(t, err, gzip.ErrHeader)
				require.False(t, raw.eof)
				require.Zero(t, entry.useCount.Load())
				expectStreamWriteReset(t, str, quic.StreamErrorCode(ErrCodeRequestCanceled))
			} else {
				require.NoError(t, err)
				require.Equal(t, 1, n)
				require.True(t, raw.eof, "raw compressed source should already have returned EOF")
				require.EqualValues(t, 1, entry.useCount.Load())
				data, err := io.ReadAll(rsp.Body)
				require.NoError(t, err)
				require.Len(t, data, 32767)
				require.Zero(t, entry.useCount.Load())
			}
		})
	}
}

func exchangeHeaders(t *testing.T, status int, header http.Header) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := qpack.NewEncoder(&buf)
	require.NoError(t, enc.WriteField(qpack.HeaderField{Name: ":status", Value: strconv.Itoa(status)}))
	for k, values := range header {
		for _, v := range values {
			require.NoError(t, enc.WriteField(qpack.HeaderField{Name: strings.ToLower(k), Value: v}))
		}
	}
	return append((&headersFrame{Length: uint64(buf.Len())}).Append(nil), buf.Bytes()...)
}

func TestExchangeActualBodyCompletion(t *testing.T) {
	for _, tc := range []struct {
		name, method string
		status       int
		header       http.Header
	}{
		{"head", http.MethodHead, 200, nil},
		{"no-content", http.MethodGet, 204, nil},
		{"length-zero", http.MethodGet, 200, http.Header{"Content-Length": {"0"}}},
		{"connect", http.MethodConnect, 200, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr, client, server := exchangeTransport(t)
			req, err := http.NewRequest(tc.method, "https://example.com", nil)
			require.NoError(t, err)
			rsp, str := exchangeResponse(t, tr, server, req, exchangeHeaders(t, tc.status, tc.header))
			entry := exchangeEntry(t, tr)
			require.EqualValues(t, 1, entry.useCount.Load())
			tr.CloseIdleConnections()
			require.NoError(t, client.Context().Err())
			require.NoError(t, str.Close())
			_, err = io.ReadAll(rsp.Body)
			require.NoError(t, err)
			require.Zero(t, entry.useCount.Load())
		})
	}
}

type closeSensitiveInput struct {
	reader *bytes.Reader
	closes atomic.Int32
}

func (b *closeSensitiveInput) Read(p []byte) (int, error) {
	if b.closes.Load() != 0 {
		return 0, errors.New("input was closed")
	}
	return b.reader.Read(p)
}
func (b *closeSensitiveInput) Close() error { b.closes.Add(1); return nil }

func TestExchangeRetryUntouchedInput(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		t.Run(map[bool]string{false: "retry-success", true: "terminal-failure"}[terminal], func(t *testing.T) {
			oldConn, _ := newConnPair(t)
			oldClient := (&Transport{}).NewClientConn(oldConn)
			require.NoError(t, oldConn.CloseWithError(0, "unusable"))
			oldDialing := make(chan struct{})
			close(oldDialing)
			old := &roundTripperWithCount{conn: oldConn, clientConn: oldClient, dialing: oldDialing, cancel: func() {}}
			tr, _, server := exchangeTransport(t)
			if terminal {
				tr.Dial = func(context.Context, string, *tls.Config, *quic.Config) (*quic.Conn, error) { return oldConn, nil }
			}
			tr.clients = map[string]*roundTripperWithCount{"example.com:443": old}
			input := &closeSensitiveInput{reader: bytes.NewReader([]byte("upload"))}
			req := httptest.NewRequest(http.MethodPost, "https://example.com", input)
			require.Nil(t, req.GetBody)
			if terminal {
				_, err := tr.RoundTrip(req)
				require.Error(t, err)
			} else {
				rsp, str := exchangeResponse(t, tr, server, req, encodeResponse(t, http.StatusOK))
				// The server observes the same still-open input on the retry.
				upload, err := io.ReadAll(str)
				require.NoError(t, err)
				require.Equal(t, append([]byte{0, 6}, []byte("upload")...), upload)
				require.NoError(t, str.Close())
				_, err = io.ReadAll(rsp.Body)
				require.NoError(t, err)
				require.Zero(t, exchangeEntry(t, tr).useCount.Load())
			}
			require.Zero(t, old.useCount.Load())
			require.EqualValues(t, 1, input.closes.Load())
		})
	}
}

func TestExchangePreResponseFailure(t *testing.T) {
	tr, _, server := exchangeTransport(t)
	input := newExchangeInput()
	t.Cleanup(func() {
		if input.closes.Load() == 0 {
			input.Close()
		}
	})
	req := httptest.NewRequest(http.MethodPost, "https://example.com", input)
	result := make(chan error, 1)
	go func() { _, err := tr.RoundTrip(req); result <- err }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	str, err := server.AcceptStream(ctx)
	require.NoError(t, err)
	str.SetReadDeadline(time.Now().Add(time.Second))
	decodeHeader(t, str)
	receiveExchange(t, input.entered)
	entry := exchangeEntry(t, tr)
	str.CancelWrite(quic.StreamErrorCode(ErrCodeInternalError))
	require.Error(t, receiveExchange(t, result))
	require.EqualValues(t, 1, input.closes.Load())
	require.Eventually(t, func() bool { return entry.useCount.Load() == 0 }, time.Second, time.Millisecond)
}

func TestExchangeAcquiredDialFailure(t *testing.T) {
	entered, finish := make(chan struct{}), make(chan struct{})
	tr := &Transport{Dial: func(context.Context, string, *tls.Config, *quic.Config) (*quic.Conn, error) {
		close(entered)
		<-finish
		return nil, assert.AnError
	}}
	input := &closeSensitiveInput{reader: bytes.NewReader(nil)}
	result := make(chan error, 1)
	go func() {
		_, err := tr.RoundTrip(httptest.NewRequest(http.MethodGet, "https://example.com", input))
		result <- err
	}()
	receiveExchange(t, entered)
	entry := exchangeEntry(t, tr)
	require.EqualValues(t, 1, entry.useCount.Load())
	close(finish)
	require.ErrorIs(t, receiveExchange(t, result), assert.AnError)
	require.Zero(t, entry.useCount.Load())
	require.EqualValues(t, 1, input.closes.Load())
}

func TestExchangeDirectClient(t *testing.T) {
	t.Run("unread-cancellation", func(t *testing.T) {
		client, server := newConnPair(t)
		cc := (&Transport{}).NewClientConn(client)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		input := newExchangeInput()
		t.Cleanup(func() {
			if input.closes.Load() == 0 {
				input.Close()
			}
		})
		rsp, _ := exchangeResponse(t, cc, server, httptest.NewRequest(http.MethodPost, "https://example.com", input).WithContext(ctx), encodeResponse(t, http.StatusOK))
		receiveExchange(t, input.entered)
		cancel()
		receiveExchange(t, rsp.Body.(*exchangeBody).lifetime.stopped)
		require.EqualValues(t, 1, input.closes.Load())
	})
	t.Run("terminal-open-failure", func(t *testing.T) {
		client, _ := newConnPair(t)
		cc := (&Transport{}).NewClientConn(client)
		require.NoError(t, client.CloseWithError(0, "closed"))
		input := &closeSensitiveInput{reader: bytes.NewReader([]byte("untouched"))}
		_, err := cc.RoundTrip(httptest.NewRequest(http.MethodPost, "https://example.com", input))
		require.Error(t, err)
		require.EqualValues(t, 1, input.closes.Load())
	})
}

func TestExchangeOverlappingTerminalEvents(t *testing.T) {
	tr, _, server := exchangeTransport(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input := newExchangeInput()
	t.Cleanup(func() {
		if input.closes.Load() == 0 {
			input.Close()
		}
	})
	rsp, str := exchangeResponse(t, tr, server, httptest.NewRequest(http.MethodPost, "https://example.com", input).WithContext(ctx), encodeResponse(t, http.StatusOK))
	receiveExchange(t, input.entered)
	entry := exchangeEntry(t, tr)
	require.NoError(t, str.Close())
	var wg sync.WaitGroup
	for _, f := range []func(){func() { io.ReadAll(rsp.Body) }, func() { rsp.Body.Close() }, cancel, func() { close(input.finished) }} {
		wg.Go(f)
	}
	wg.Wait()
	receiveExchange(t, rsp.Body.(*exchangeBody).lifetime.stopped)
	require.Zero(t, entry.useCount.Load())
	require.EqualValues(t, 1, input.closes.Load())
}

type eofObservedBody struct {
	io.ReadCloser
	eof bool
}

func (b *eofObservedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err == io.EOF {
		b.eof = true
	}
	return n, err
}

func TestExchangeResponseReadFailure(t *testing.T) {
	tr, _, server := exchangeTransport(t)
	rsp, str := exchangeResponse(t, tr, server, httptest.NewRequest(http.MethodGet, "https://example.com", nil), encodeResponse(t, http.StatusOK))
	entry := exchangeEntry(t, tr)
	str.CancelWrite(quic.StreamErrorCode(ErrCodeInternalError))
	_, err := io.ReadAll(rsp.Body)
	require.ErrorIs(t, err, &Error{ErrorCode: ErrCodeInternalError, Remote: true})
	require.Zero(t, entry.useCount.Load())
}
