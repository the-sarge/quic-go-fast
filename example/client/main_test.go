package main

import (
	"bytes"
	"crypto/tls"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/quic-go/internal/testdata"

	"github.com/stretchr/testify/require"
)

func TestFetchResponseSizeLimit(t *testing.T) {
	const limit = 8
	for _, tc := range []struct {
		name    string
		body    string
		stream  bool
		wantErr bool
	}{
		{name: "empty"},
		{name: "below limit", body: "hello"},
		{name: "exact limit", body: "12345678"},
		{name: "above limit", body: "123456789", wantErr: true},
		{name: "unknown length above limit", body: "123456789", stream: true, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, addr := newTestHTTP3Client(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.stream {
					w.(http.Flusher).Flush()
				}
				_, _ = io.WriteString(w, tc.body)
			}))
			for _, quiet := range []bool{false, true} {
				name := "display"
				if quiet {
					name = "quiet"
				}
				t.Run(name, func(t *testing.T) {
					var buf bytes.Buffer
					var output io.Writer = &buf
					if quiet {
						output = io.Discard
					}
					n, err := fetchResponse(client, addr, output, limit)
					if tc.wantErr {
						require.EqualError(t, err, "response body exceeds 8 bytes")
					} else {
						require.NoError(t, err)
					}
					want := tc.body[:min(len(tc.body), limit)]
					require.Equal(t, int64(len(want)), n)
					if quiet {
						require.Empty(t, buf.String())
					} else {
						require.Equal(t, want, buf.String())
					}
				})
			}
		})
	}
}

func TestFetchResponseStopsEndlessBody(t *testing.T) {
	finished := make(chan struct{})
	client, addr := newTestHTTP3Client(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(finished)
		_, _ = io.WriteString(w, "123456789")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	n, err := fetchResponse(client, addr, io.Discard, 8)
	require.EqualError(t, err, "response body exceeds 8 bytes")
	require.Equal(t, int64(8), n)
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("oversized response stream was not canceled")
	}
}

func TestFetchResponseTimeout(t *testing.T) {
	for _, tc := range []struct {
		name      string
		prefix    string
		flush     bool
		keepAlive bool
	}{
		{name: "stalled headers"},
		{name: "stalled body", prefix: "x", flush: true},
		{name: "stalled at exact size limit", prefix: "12345678", flush: true},
		{name: "slow body", flush: true, keepAlive: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			finished := make(chan struct{})
			client, addr := newTestHTTP3Client(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(finished)
				if tc.flush {
					_, _ = io.WriteString(w, tc.prefix)
					w.(http.Flusher).Flush()
				}
				if tc.keepAlive {
					ticker := time.NewTicker(100 * time.Millisecond)
					defer ticker.Stop()
					for {
						select {
						case <-ticker.C:
							if _, err := io.WriteString(w, "x"); err != nil {
								return
							}
							w.(http.Flusher).Flush()
						case <-r.Context().Done():
							return
						}
					}
				}
				<-r.Context().Done()
			}))
			client.Timeout = time.Second
			limit := int64(8)
			if tc.keepAlive {
				limit = math.MaxInt64
			}
			var output bytes.Buffer
			started := time.Now()
			n, err := fetchResponse(client, addr, &output, limit)
			require.Error(t, err)
			elapsed := time.Since(started)
			require.GreaterOrEqual(t, elapsed, client.Timeout)
			require.Less(t, elapsed, 5*time.Second)
			if timeoutErr, ok := errors.AsType[net.Error](err); ok {
				require.True(t, timeoutErr.Timeout(), "expected a timeout: %v", err)
			} else {
				// HTTP/3 stream cancellation can win the race with the client's timeout error.
				var streamErr *http3.Error
				require.ErrorAs(t, err, &streamErr)
				require.Equal(t, http3.ErrCodeRequestCanceled, streamErr.ErrorCode)
				require.False(t, streamErr.Remote)
			}
			if tc.keepAlive {
				// A total request deadline also bounds a peer that keeps sending data.
				require.Positive(t, n)
			} else {
				require.Equal(t, int64(len(tc.prefix)), n)
				require.Equal(t, tc.prefix, output.String())
			}
			select {
			case <-finished:
			case <-time.After(5 * time.Second):
				t.Fatal("timed out request was not canceled")
			}
		})
	}
}

func TestFetchResponseClosesBody(t *testing.T) {
	readErr := errors.New("read failed")
	writeErr := errors.New("write failed")
	for _, tc := range []struct {
		name    string
		body    io.Reader
		output  io.Writer
		limit   int64
		wantErr error
	}{
		{name: "success", body: strings.NewReader("hello"), output: io.Discard, limit: math.MaxInt64},
		{name: "size limit", body: strings.NewReader("too large"), output: io.Discard, limit: 1},
		{name: "read error", body: failingReader{readErr}, output: io.Discard, limit: 8, wantErr: readErr},
		{name: "overflow probe error", body: io.MultiReader(strings.NewReader("12345678"), failingReader{readErr}), output: io.Discard, limit: 8, wantErr: readErr},
		{name: "write error", body: strings.NewReader("hello"), output: failingWriter{writeErr}, limit: 8, wantErr: writeErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &trackedBody{Reader: tc.body}
			client := &http.Client{Transport: testRoundTripper(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
			})}
			_, err := fetchResponse(client, "https://example.com", tc.output, tc.limit)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else if tc.name == "size limit" {
				require.EqualError(t, err, "response body exceeds 1 bytes")
			} else {
				require.NoError(t, err)
			}
			require.True(t, body.closed)
		})
	}
}

func newTestHTTP3Client(t *testing.T, handler http.Handler) (*http.Client, string) {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	server := &http3.Server{TLSConfig: testdata.GetTLSConfig(), Handler: handler}
	served := make(chan error, 1)
	go func() { served <- server.Serve(conn) }()
	t.Cleanup(func() {
		require.NoError(t, server.Close())
		require.NoError(t, conn.Close())
		select {
		case err := <-served:
			require.ErrorIs(t, err, http.ErrServerClosed)
		case <-time.After(5 * time.Second):
			t.Error("HTTP/3 server did not stop")
		}
	})
	transport := &http3.Transport{TLSClientConfig: &tls.Config{
		RootCAs:    testdata.GetRootCA(),
		ServerName: "localhost",
	}}
	t.Cleanup(func() { require.NoError(t, transport.Close()) })
	return &http.Client{Transport: transport, Timeout: 10 * time.Second}, "https://" + conn.LocalAddr().String()
}

type testRoundTripper func(*http.Request) (*http.Response, error)

func (f testRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error {
	b.closed = true
	return nil
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }
