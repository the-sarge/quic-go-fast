package self_test

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/quic-go/testutils/simnet"

	"github.com/stretchr/testify/require"
)

// The HTTP/3 server idle timer measures HTTP inactivity. It starts at handshake
// completion, so a handshake slower than the idle timeout does not consume it.
func TestHTTPServerIdleTimerStartsAtHandshakeCompletion(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const rtt = 100 * time.Millisecond
		const idleTimeout = rtt / 2

		clientPacketConn, serverPacketConn, closeFn := newSimnetLink(t, rtt)
		defer closeFn(t)
		// One transport owns the client socket for every dial in this test.
		clientTransport := &quic.Transport{Conn: clientPacketConn}
		defer clientTransport.Close()

		mux := http.NewServeMux()
		mux.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, "Hello, World!\n")
		})
		closeServer := startSimnetHTTPServer(t, serverPacketConn, mux, idleTimeout)
		defer closeServer()

		var conn *quic.Conn
		tr := newSimnetHTTPTransport(clientTransport, serverPacketConn, getTLSClientConfig(), func(c *quic.Conn) { conn = c })
		defer tr.Close()

		resp, err := tr.RoundTrip(newRequest(t, http.MethodGet, "https://localhost/hello"))
		require.NoError(t, err)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, "Hello, World!\n", string(body))
		require.NoError(t, resp.Body.Close())

		// Once the exchange is over, the idle timer still closes the connection.
		requireHTTPIdleTimeoutClose(t, conn)
	})
}

// A 0-RTT request can be active when the handshake completes. Handshake
// completion must not start the idle timer underneath that exchange.
func TestHTTPServerIdleTimerWaitsFor0RTTExchange(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const rtt = 100 * time.Millisecond
		const idleTimeout = rtt / 2
		const handlerDuration = 3 * rtt // ends long after handshake completion plus idleTimeout

		clientPacketConn, serverPacketConn, closeFn := newSimnetLink(t, rtt)
		defer closeFn(t)
		// One transport owns the client socket for every dial in this test.
		clientTransport := &quic.Transport{Conn: clientPacketConn}
		defer clientTransport.Close()

		mux := http.NewServeMux()
		mux.HandleFunc("/ticket", func(w http.ResponseWriter, r *http.Request) {})
		mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
			if r.TLS.HandshakeComplete {
				w.WriteHeader(http.StatusTeapot) // not a 0-RTT request
				return
			}
			time.Sleep(handlerDuration)
			io.WriteString(w, "done")
		})
		closeServer := startSimnetHTTPServer(t, serverPacketConn, mux, idleTimeout)
		defer closeServer()

		tlsConf := resumableSimnetTLSConfig(t, clientTransport, serverPacketConn, "https://localhost/ticket")
		var conn *quic.Conn
		tr := newSimnetHTTPTransport(clientTransport, serverPacketConn, tlsConf, func(c *quic.Conn) { conn = c })
		defer tr.Close()

		resp, err := tr.RoundTrip(newRequest(t, http3.MethodGet0RTT, "https://localhost/slow"))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, "done", string(body))
		require.NoError(t, resp.Body.Close())

		// The timer starts once the exchange completes.
		requireHTTPIdleTimeoutClose(t, conn)
	})
}

// A 0-RTT exchange can finish before the handshake completes. The connection
// is idle from then on, but the idle timer still waits for handshake completion.
func TestHTTPServerIdleTimerAfterEarly0RTTExchange(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const rtt = 100 * time.Millisecond
		const idleTimeout = rtt / 2
		// The server completes the handshake when the client's Finished arrives,
		// 1.5 RTT after the client starts dialing.
		const serverHandshakeComplete = 3 * rtt / 2

		clientPacketConn, serverPacketConn, closeFn := newSimnetLink(t, rtt)
		defer closeFn(t)
		// One transport owns the client socket for every dial in this test.
		clientTransport := &quic.Transport{Conn: clientPacketConn}
		defer clientTransport.Close()

		mux := http.NewServeMux()
		mux.HandleFunc("/ticket", func(w http.ResponseWriter, r *http.Request) {})
		mux.HandleFunc("/fast", func(w http.ResponseWriter, r *http.Request) {
			if r.TLS.HandshakeComplete {
				w.WriteHeader(http.StatusTeapot) // not a 0-RTT request
			}
		})
		closeServer := startSimnetHTTPServer(t, serverPacketConn, mux, idleTimeout)
		defer closeServer()

		tlsConf := resumableSimnetTLSConfig(t, clientTransport, serverPacketConn, "https://localhost/ticket")
		var conn *quic.Conn
		tr := newSimnetHTTPTransport(clientTransport, serverPacketConn, tlsConf, func(c *quic.Conn) { conn = c })
		defer tr.Close()

		start := time.Now()
		resp, err := tr.RoundTrip(newRequest(t, http3.MethodGet0RTT, "https://localhost/fast"))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.NoError(t, resp.Body.Close())

		requireHTTPIdleTimeoutClose(t, conn)
		require.GreaterOrEqual(t, time.Since(start), serverHandshakeComplete+idleTimeout)
	})
}

func startSimnetHTTPServer(t *testing.T, pc *simnet.SimConn, handler http.Handler, idleTimeout time.Duration) (close func()) {
	t.Helper()
	ln, err := quic.ListenEarly(
		pc,
		http3.ConfigureTLSConfig(getTLSConfig()),
		getQuicConfig(&quic.Config{Allow0RTT: true}),
	)
	require.NoError(t, err)
	server := &http3.Server{Handler: handler, IdleTimeout: idleTimeout}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.ServeListener(ln) }()
	return func() {
		require.NoError(t, server.Close())
		require.ErrorIs(t, <-serveErr, http.ErrServerClosed)
		ln.Close()
	}
}

func newSimnetHTTPTransport(client *quic.Transport, server *simnet.SimConn, tlsConf *tls.Config, onDial func(*quic.Conn)) *http3.Transport {
	return &http3.Transport{
		TLSClientConfig: tlsConf,
		QUICConfig:      getQuicConfig(nil),
		Dial: func(ctx context.Context, _ string, tlsConf *tls.Config, conf *quic.Config) (*quic.Conn, error) {
			conn, err := client.DialEarly(ctx, server.LocalAddr(), tlsConf, conf)
			if err == nil && onDial != nil {
				onDial(conn)
			}
			return conn, err
		},
	}
}

// resumableSimnetTLSConfig returns a client TLS config holding a session ticket
// from the server, so that a later connection can send 0-RTT requests.
func resumableSimnetTLSConfig(t *testing.T, client *quic.Transport, server *simnet.SimConn, ticketURL string) *tls.Config {
	t.Helper()
	tlsConf := getTLSClientConfig()
	puts := make(chan string, 1)
	tlsConf.ClientSessionCache = newClientSessionCache(tls.NewLRUClientSessionCache(1), nil, puts)
	tr := newSimnetHTTPTransport(client, server, tlsConf, nil)
	defer tr.Close()

	resp, err := tr.RoundTrip(newRequest(t, http.MethodGet, ticketURL))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	select {
	case <-puts:
	case <-time.After(time.Second):
		t.Fatal("did not receive session ticket")
	}
	return tlsConf
}

func newRequest(t *testing.T, method, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	require.NoError(t, err)
	return req
}

// requireHTTPIdleTimeoutClose waits for the server's HTTP/3 idle timer to close conn.
func requireHTTPIdleTimeoutClose(t *testing.T, conn *quic.Conn) {
	t.Helper()
	select {
	case <-conn.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("idle connection was not closed")
	}
	var appErr *quic.ApplicationError
	require.ErrorAs(t, context.Cause(conn.Context()), &appErr)
	require.True(t, appErr.Remote)
	require.Equal(t, quic.ApplicationErrorCode(http3.ErrCodeNoError), appErr.ErrorCode)
	require.Equal(t, "idle timeout", appErr.ErrorMessage)
}
