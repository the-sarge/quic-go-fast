package self_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHTTPServerRequestParsing compares how the standard library's HTTP/2 server
// and quic-go's HTTP/3 server parse different requests. It aims for equivalent
// behavior wherever the protocols allow it.
func TestHTTPServerRequestParsing(t *testing.T) {
	requests := make(chan *http.Request, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		requests <- r.Clone(context.Background())
		w.WriteHeader(http.StatusTeapot)
	})

	h2Server := httptest.NewUnstartedServer(handler)
	h2Server.EnableHTTP2 = true
	h2Server.Config.DisableGeneralOptionsHandler = true
	h2Server.StartTLS()
	defer h2Server.Close()
	h2Client := h2Server.Client()
	h2Transport := h2Client.Transport.(*http.Transport)
	h2Transport.DisableCompression = true
	h2Transport.ForceAttemptHTTP2 = true

	h3Client := &http.Client{Transport: newRequestParsingClient(t, handler)}

	// Cover origin, asterisk and authority request-target forms, URL parsing
	// edge cases, host authorities, body lengths and trailers.
	// Extended CONNECT is covered separately below; it is not compared to HTTP/2.
	tests := []struct {
		name          string
		method        string
		authority     string
		path          string
		body          string
		contentLength int64
		trailer       http.Header
	}{
		{
			name:      "origin form",
			method:    http.MethodGet,
			authority: "quic-go.net",
			path:      "/foo?bar=baz",
		},
		{
			name:      "HEAD request",
			method:    http.MethodHead,
			authority: "quic-go.net",
			path:      "/head?foo=bar",
		},
		{
			name:      "origin-form OPTIONS",
			method:    http.MethodOptions,
			authority: "quic-go.net",
			path:      "/options",
		},
		{
			name:          "POST request with trailers",
			method:        http.MethodPost,
			authority:     "quic-go.net",
			path:          "/post?foo=bar",
			body:          "request body",
			contentLength: -1,
			trailer:       http.Header{"X-Test-Trailer": {"trailer value"}},
		},
		{
			name:          "POST request with known length",
			method:        http.MethodPost,
			authority:     "quic-go.net",
			path:          "/post-known-length",
			body:          "known-length body",
			contentLength: int64(len("known-length body")),
		},
		{
			name:      "origin form with escaped path",
			method:    http.MethodGet,
			authority: "quic-go.net",
			path:      "/foo%2Fbar?baz=qux",
		},
		{
			name:      "origin form with path starting with double slash",
			method:    http.MethodGet,
			authority: "quic-go.net",
			path:      "//foo",
		},
		{
			name:      "origin form with force query",
			method:    http.MethodGet,
			authority: "quic-go.net",
			path:      "/foo?",
		},
		{
			name:      "origin form with encoded query",
			method:    http.MethodGet,
			authority: "quic-go.net",
			path:      "/search?q=a%2Fb%20c",
		},
		{
			name:      "origin form with explicit authority port",
			method:    http.MethodGet,
			authority: "quic-go.net:443",
			path:      "/port",
		},
		{
			name:      "asterisk form",
			method:    http.MethodOptions,
			authority: "quic-go.net",
			path:      "*",
		},
		{
			name:      "authority form",
			method:    http.MethodConnect,
			authority: "quic-go.net:443",
		},
		{
			name:      "origin form with IPv6 authority",
			method:    http.MethodGet,
			authority: "[2001:db8::1]:443",
			path:      "/ipv6",
		},
		{
			name:      "authority form with IPv6 authority",
			method:    http.MethodConnect,
			authority: "[2001:db8::1]:443",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			requestURL := &url.URL{Scheme: "https", Host: h2Server.Listener.Addr().String()}
			if tc.path != "" {
				parsedURL, err := url.ParseRequestURI(tc.path)
				require.NoError(t, err)
				parsedURL.Scheme = requestURL.Scheme
				parsedURL.Host = requestURL.Host
				requestURL = parsedURL
			}
			request := &http.Request{
				Method: tc.method,
				URL:    requestURL,
				Host:   tc.authority,
				Header: http.Header{
					"User-Agent":    nil,
					"X-Test-Header": {"foo", "bar"},
				},
				Trailer: tc.trailer,
			}
			if tc.body != "" {
				request.Body = io.NopCloser(strings.NewReader(tc.body))
				request.ContentLength = tc.contentLength
			}

			resp, err := h2Client.Do(request.Clone(t.Context()))
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			require.Equal(t, 2, resp.ProtoMajor)
			require.Equal(t, http.StatusTeapot, resp.StatusCode)
			h2Request := receiveParsedRequest(t, requests)

			if tc.body != "" {
				request.Body = io.NopCloser(strings.NewReader(tc.body))
			}
			resp, err = h3Client.Do(request.Clone(t.Context()))
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			require.Equal(t, 3, resp.ProtoMajor)
			require.Equal(t, http.StatusTeapot, resp.StatusCode)
			h3Request := receiveParsedRequest(t, requests)

			t.Logf("HTTP/2 request: %+v", h2Request)
			t.Logf("HTTP/3 request: %+v", h3Request)
			assert.Equal(t, "HTTP/2.0", h2Request.Proto)
			assert.Equal(t, 2, h2Request.ProtoMajor)
			assert.Zero(t, h2Request.ProtoMinor)
			assert.Equal(t, "HTTP/3.0", h3Request.Proto)
			assert.Equal(t, 3, h3Request.ProtoMajor)
			assert.Zero(t, h3Request.ProtoMinor)

			// Body, TLS and RemoteAddr are backed by different transports.
			assert.Equal(t, h2Request.Method, h3Request.Method)
			assert.Equal(t, h2Request.Host, h3Request.Host)
			assert.Equal(t, h2Request.RequestURI, h3Request.RequestURI)
			assert.Equal(t, h2Request.URL, h3Request.URL)
			assert.Equal(t, h2Request.Header, h3Request.Header)
			assert.Equal(t, h2Request.TransferEncoding, h3Request.TransferEncoding)
			assert.Equal(t, h2Request.Close, h3Request.Close)
			assert.Equal(t, h2Request.Trailer, h3Request.Trailer)
			if tc.trailer != nil {
				assert.Equal(t, tc.trailer, h2Request.Trailer)
				assert.Equal(t, tc.trailer, h3Request.Trailer)
			}
			if tc.contentLength > 0 {
				contentLength := strconv.FormatInt(tc.contentLength, 10)
				assert.Equal(t, contentLength, h2Request.Header.Get("Content-Length"))
				assert.Equal(t, contentLength, h3Request.Header.Get("Content-Length"))
			}
			if _, hasContentLength := h2Request.Header["Content-Length"]; hasContentLength {
				assert.Equal(t, h2Request.ContentLength, h3Request.ContentLength)
			} else if tc.body != "" {
				assert.Equal(t, int64(-1), h2Request.ContentLength)
				assert.Equal(t, int64(-1), h3Request.ContentLength)
			} else {
				// HTTP/2 has an END_STREAM flag on the HEADERS frame. HTTP/3 only
				// learns that no body follows when the request stream reaches EOF.
				assert.Zero(t, h2Request.ContentLength)
				assert.Equal(t, int64(-1), h3Request.ContentLength)
			}
		})
	}
}

// Keep explicit transport owners so cleanup joins their receive loops before
// newUDPConnLocalhost releases the caller-owned sockets.
func newRequestParsingClient(t *testing.T, handler http.Handler) *http3.ClientConn {
	t.Helper()
	serverTransport := &quic.Transport{Conn: newUDPConnLocalhost(t)}
	t.Cleanup(func() { assert.NoError(t, serverTransport.Close()) })
	ln, err := serverTransport.ListenEarly(http3.ConfigureTLSConfig(getTLSConfig()), getQuicConfig(nil))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, ln.Close()) })
	server := &http3.Server{Handler: handler}
	served := make(chan error, 1)
	go func() { served <- server.ServeListener(ln) }()
	t.Cleanup(func() {
		assert.NoError(t, server.Close())
		select {
		case err := <-served:
			assert.ErrorIs(t, err, http.ErrServerClosed)
		case <-time.After(scaleDuration(time.Second)):
			t.Error("HTTP/3 parsing server did not stop")
		}
	})

	clientTransport := &quic.Transport{Conn: newUDPConnLocalhost(t)}
	t.Cleanup(func() { assert.NoError(t, clientTransport.Close()) })
	conn, err := clientTransport.Dial(t.Context(), ln.Addr(), http3.ConfigureTLSConfig(getTLSClientConfig()), getQuicConfig(nil))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, conn.CloseWithError(0, "")) })
	tr := &http3.Transport{DisableCompression: true}
	t.Cleanup(func() { assert.NoError(t, tr.Close()) })
	client := tr.NewClientConn(conn)
	t.Cleanup(func() { assert.NoError(t, client.CloseWithError(0, "")) })
	return client
}

func receiveParsedRequest(t *testing.T, requests <-chan *http.Request) *http.Request {
	t.Helper()
	select {
	case r := <-requests:
		return r
	case <-time.After(scaleDuration(time.Second)):
		t.Fatal("handler did not receive request")
		return nil
	}
}

func TestHTTPServerExtendedConnectRequestParsing(t *testing.T) {
	requests := make(chan *http.Request, 1)
	client := newRequestParsingClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		requests <- r.Clone(context.Background())
		w.WriteHeader(http.StatusTeapot)
	}))
	select {
	case <-client.ReceivedSettings():
		require.True(t, client.Settings().EnableExtendedConnect)
	case <-time.After(scaleDuration(time.Second)):
		t.Fatal("did not receive Extended CONNECT setting")
	}
	for _, tc := range []struct {
		name, target, path, rawPath, rawQuery string
		forceQuery                            bool
	}{
		{name: "escaped path", target: "/foo%2Fbar?val=1337", path: "/foo/bar", rawPath: "/foo%2Fbar", rawQuery: "val=1337"},
		{name: "double slash", target: "//foo", path: "//foo"},
		{name: "empty query", target: "/foo?", path: "/foo", forceQuery: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u, err := url.ParseRequestURI(tc.target)
			require.NoError(t, err)
			u.Scheme, u.Host = "https", "[2001:db8::1]:443"
			resp, err := client.RoundTrip((&http.Request{
				Method: http.MethodConnect,
				Proto:  "webtransport",
				URL:    u,
				Host:   u.Host,
				Header: make(http.Header),
			}).WithContext(t.Context()))
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			require.Equal(t, http.StatusTeapot, resp.StatusCode)
			r := receiveParsedRequest(t, requests)
			require.Equal(t, http.MethodConnect, r.Method)
			require.Equal(t, "webtransport", r.Proto)
			require.Equal(t, 3, r.ProtoMajor)
			require.Zero(t, r.ProtoMinor)
			require.Equal(t, "[2001:db8::1]:443", r.Host)
			require.Equal(t, tc.target, r.RequestURI)
			require.Equal(t, &url.URL{Path: tc.path, RawPath: tc.rawPath, RawQuery: tc.rawQuery, ForceQuery: tc.forceQuery}, r.URL)
		})
	}
}
