package self_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptrace"
	"strconv"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/stretchr/testify/require"
)

func TestHTTP3ServerHotswap(t *testing.T) {
	capture := newHTTPCapture(t)
	mux1 := http.NewServeMux()
	mux1.HandleFunc("/hello1", func(w http.ResponseWriter, r *http.Request) {
		capture.record("server1", "handler_enter", r.URL.Path)
		n, err := io.WriteString(w, "Hello, World 1!\n") // don't assert: stream may be reset.
		capture.record("server1", "handler_write", fmt.Sprintf("bytes=%d error=%v", n, err))
	})

	mux2 := http.NewServeMux()
	mux2.HandleFunc("/hello2", func(w http.ResponseWriter, r *http.Request) {
		capture.record("server2", "handler_enter", r.URL.Path)
		n, err := io.WriteString(w, "Hello, World 2!\n") // don't assert: stream may be reset.
		capture.record("server2", "handler_write", fmt.Sprintf("bytes=%d error=%v", n, err))
	})

	server1 := &http3.Server{
		Handler:    mux1,
		QUICConfig: getQuicConfig(nil),
	}
	server2 := &http3.Server{
		Handler:    mux2,
		QUICConfig: getQuicConfig(nil),
	}

	for i, server := range []*http3.Server{server1, server2} {
		name := fmt.Sprintf("server%d", i+1)
		server.Logger = slog.New(&httpCaptureLog{capture: capture, group: name + "."})
		server.ConnContext = func(ctx context.Context, conn *quic.Conn) context.Context {
			capture.record(name, "admitted", fmt.Sprintf("conn=%p", conn))
			return ctx
		}
	}

	tlsConf := http3.ConfigureTLSConfig(getTLSConfig())
	socket := newUDPConnLocalhost(t)
	t.Cleanup(capture.beginCleanup)
	capture.record("listener", "listen_enter", socket.LocalAddr().String())
	ln, err := quic.ListenEarly(socket, tlsConf, capture.config(getQuicConfig(nil), "listener"))
	capture.record("listener", "listen_return", fmt.Sprintf("error_type=%T error=%v", err, err))
	require.NoError(t, err)
	port := strconv.Itoa(ln.Addr().(*net.UDPAddr).Port)

	clientNumber := 0
	newClient := func() *http.Client {
		clientNumber++
		capture.record("fixture", "new_client", clientNumber)
		return &http.Client{
			Transport: &http3.Transport{
				TLSClientConfig:    getTLSClientConfig(),
				DisableCompression: true,
				QUICConfig:         capture.config(getQuicConfig(&quic.Config{MaxIdleTimeout: 10 * time.Second}), fmt.Sprintf("client%d", clientNumber)),
			},
		}
	}

	client := newClient()

	defer func() {
		capture.beginCleanup()
		capture.record("listener", "close_enter", nil)
		err := ln.Close()
		capture.record("listener", "close_return", fmt.Sprint(err))
		require.NoError(t, err)
	}()

	// open first server and make single request to it
	errChan1 := make(chan error, 1)
	go func() {
		capture.record("server1", "serve_enter", nil)
		err := server1.ServeListener(&hotswapCaptureListener{EarlyListener: ln, capture: capture, name: "server1"})
		capture.record("server1", "serve_return", fmt.Sprint(err))
		errChan1 <- err
	}()

	capture.record("client1", "get_enter", nil)
	resp, err := hotswapCaptureGet(client, capture, "client1", "https://localhost:"+port+"/hello1")
	capture.record("client1", "get_return", fmt.Sprint(err))
	require.NoError(t, err)
	capture.record(fmt.Sprintf("client%d", clientNumber), "response_headers", resp.StatusCode)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	capture.record(fmt.Sprintf("client%d", clientNumber), "body_consumed", fmt.Sprintf("bytes=%d error_type=%T error=%v", len(body), err, err))
	require.NoError(t, err)
	require.Equal(t, "Hello, World 1!\n", string(body))

	// open second server with same underlying listener
	errChan2 := make(chan error, 1)
	go func() {
		capture.record("server2", "serve_enter", nil)
		err := server2.ServeListener(&hotswapCaptureListener{EarlyListener: ln, capture: capture, name: "server2"})
		capture.record("server2", "serve_return", fmt.Sprint(err))
		errChan2 <- err
	}()

	time.Sleep(scaleDuration(20 * time.Millisecond))
	select {
	case err := <-errChan1:
		t.Fatalf("server1 stopped unexpectedly: %v", err)
	case err := <-errChan2:
		t.Fatalf("server2 stopped unexpectedly: %v", err)
	default:
	}

	// now close first server
	capture.record("server1", "close_enter", nil)
	err = server1.Close()
	capture.record("server1", "close_return", fmt.Sprint(err))
	require.NoError(t, err)
	select {
	case err := <-errChan1:
		require.ErrorIs(t, err, http.ErrServerClosed)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for server1 to stop")
	}
	capture.record(fmt.Sprintf("client%d", clientNumber), "close_enter", nil)
	err = client.Transport.(*http3.Transport).Close()
	capture.record(fmt.Sprintf("client%d", clientNumber), "close_return", fmt.Sprint(err))
	require.NoError(t, err)
	client = newClient()
	defer func() {
		capture.beginCleanup()
		capture.record(fmt.Sprintf("client%d", clientNumber), "close_enter", nil)
		err := client.Transport.(*http3.Transport).Close()
		capture.record(fmt.Sprintf("client%d", clientNumber), "close_return", fmt.Sprint(err))
		require.NoError(t, err)
	}()

	// verify that new connections are handled by the second server now
	capture.record("client2", "get_enter", nil)
	resp, err = hotswapCaptureGet(client, capture, "client2", "https://localhost:"+port+"/hello2")
	capture.record("client2", "get_return", fmt.Sprint(err))
	require.NoError(t, err)
	capture.record(fmt.Sprintf("client%d", clientNumber), "response_headers", resp.StatusCode)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, err = io.ReadAll(resp.Body)
	capture.record(fmt.Sprintf("client%d", clientNumber), "body_consumed", fmt.Sprintf("bytes=%d error_type=%T error=%v", len(body), err, err))
	require.NoError(t, err)
	require.Equal(t, "Hello, World 2!\n", string(body))

	// close the other server
	capture.record("server2", "close_enter", nil)
	err = server2.Close()
	capture.record("server2", "close_return", fmt.Sprint(err))
	require.NoError(t, err)
	select {
	case err := <-errChan2:
		require.ErrorIs(t, err, http.ErrServerClosed)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for server2 to stop")
	}
	maybeFailHTTPCaptureFixture(t)
}

// Forward the original listener and context without changing ownership.
type hotswapCaptureListener struct {
	*quic.EarlyListener
	capture *httpCapture
	name    string
}

func (l *hotswapCaptureListener) Accept(ctx context.Context) (*quic.Conn, error) {
	l.capture.record(l.name, "accept_enter", fmt.Sprint(ctx.Err()))
	conn, err := l.EarlyListener.Accept(ctx)
	l.capture.record(l.name, "accept_return", fmt.Sprintf("conn=%p error=%v context=%v", conn, err, ctx.Err()))
	l.capture.observeConn(l.name, conn)
	return conn, err
}

func (l *hotswapCaptureListener) Close() error {
	l.capture.record(l.name, "unexpected_listener_close", nil)
	return l.EarlyListener.Close()
}

// Use the transport's existing resolver and shared UDP transport. Replacing Dial
// solely for observation would change the fixture's socket ownership.
func hotswapCaptureGet(client *http.Client, capture *httpCapture, name, url string) (*http.Response, error) {
	trace := &httptrace.ClientTrace{
		GetConn:  func(addr string) { capture.record(name, "get_conn", addr) },
		DNSStart: func(info httptrace.DNSStartInfo) { capture.record(name, "dns_start", info.Host) },
		DNSDone: func(info httptrace.DNSDoneInfo) {
			capture.record(name, "dns_done", fmt.Sprintf("addresses=%v error_type=%T error=%v", info.Addrs, info.Err, info.Err))
		},
		ConnectStart: func(network, addr string) { capture.record(name, "connect_start", network+" "+addr) },
		ConnectDone: func(network, addr string, err error) {
			capture.record(name, "connect_done", fmt.Sprintf("network=%s address=%s error_type=%T error=%v", network, addr, err, err))
		},
		TLSHandshakeStart: func() { capture.record(name, "tls_start", nil) },
		TLSHandshakeDone: func(state tls.ConnectionState, err error) {
			capture.record(name, "early_dial_return", fmt.Sprintf("handshake_complete=%t error_type=%T error=%v", state.HandshakeComplete, err, err))
		},
		GotConn: func(info httptrace.GotConnInfo) {
			capture.record(name, "got_conn", fmt.Sprintf("local=%s remote=%s reused=%t", info.Conn.LocalAddr(), info.Conn.RemoteAddr(), info.Reused))
		},
		GotFirstResponseByte: func() { capture.record(name, "first_response_byte", nil) },
	}
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(context.Background(), trace), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	capture.record(name, "request_result", fmt.Sprintf("error_type=%T error=%v", err, err))
	if resp != nil && resp.TLS != nil {
		capture.record(name, "response_tls", map[string]any{"handshake_complete": resp.TLS.HandshakeComplete})
	}
	return resp, err
}
