package http09

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/internal/testdata"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/quic-go/quic-go/qlogwriter/jsontext"
	"github.com/quic-go/quic-go/testutils/events"

	"github.com/stretchr/testify/require"
)

func startServer(t *testing.T, handler *http.ServeMux) net.Addr {
	t.Helper()
	server := &Server{Handler: handler}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	require.NoError(t, err)
	tr := &quic.Transport{Conn: conn}
	tlsConf := testdata.GetTLSConfig()
	tlsConf.NextProtos = []string{NextProto}
	ln, err := tr.ListenEarly(tlsConf, httpTestConfig(t, "server"))
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = server.ServeListener(ln)
	}()
	t.Cleanup(func() {
		require.NoError(t, ln.Close())
		<-done
		require.NoError(t, tr.Close())
		require.NoError(t, conn.Close())
	})
	return ln.Addr()
}

func TestHTTPRequest(t *testing.T) {
	http.HandleFunc("/helloworld", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("Hello World!"))
	})

	addr := startServer(t, nil)

	rt := newTestRoundTripper(t)

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("https://%s/helloworld", addr), nil)
	rsp, err := rt.RoundTrip(req)
	require.NoError(t, err)
	requireLoopbackClients(t, rt)
	data, err := io.ReadAll(rsp.Body)
	require.NoError(t, err)
	require.Equal(t, []byte("Hello World!"), data)
}

func TestHTTPHeaders(t *testing.T) {
	http.HandleFunc("/headers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("foo", "bar")
		w.WriteHeader(1337)
		_, _ = w.Write([]byte("done"))
	})

	addr := startServer(t, nil)

	rt := newTestRoundTripper(t)

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("https://%s/headers", addr), nil)
	rsp, err := rt.RoundTrip(req)
	require.NoError(t, err)
	requireLoopbackClients(t, rt)
	data, err := io.ReadAll(rsp.Body)
	require.NoError(t, err)
	require.Equal(t, []byte("done"), data)
	// HTTP/0.9 doesn't support HTTP headers
}

// httpTestConfig records connection progress for the existing request assertions.
// Passing tests are quiet; failures show a bounded tail, which can include teardown.
func httpTestConfig(t *testing.T, side string) *quic.Config {
	t.Helper()
	recorder := &events.Recorder{}
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		observed := recorder.EventsWithTime()
		t.Logf("HTTP/0.9 %s: %d connection events (last 100 follow)", side, len(observed))
		if len(observed) > 100 {
			observed = observed[len(observed)-100:]
		}
		for _, event := range observed {
			var buf bytes.Buffer
			if err := event.Event.Encode(jsontext.NewEncoder(&buf), event.Time); err != nil {
				t.Logf("HTTP/0.9 %s: encoding %s: %v", side, event.Event.Name(), err)
				continue
			}
			t.Logf("HTTP/0.9 %s %s %s: %s", side, event.Time.Format("15:04:05.000000"), event.Event.Name(), buf.String())
		}
	})
	return &quic.Config{Tracer: func(context.Context, bool, quic.ConnectionID) qlogwriter.Trace {
		return &events.Trace{Recorder: recorder}
	}}
}

func newTestRoundTripper(t *testing.T) *RoundTripper {
	t.Helper()
	rt := &RoundTripper{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, QuicConfig: httpTestConfig(t, "client")}
	if runtime.GOOS == "darwin" {
		rt.dialEarly = func(ctx context.Context, addr string, tlsConf *tls.Config, conf *quic.Config) (*quic.Conn, error) {
			return dialLocalhost(ctx, quic.DialEarly, addr, tlsConf, conf)
		}
	}
	t.Cleanup(func() { rt.Close() })
	return rt
}

// dialLocalhost dials from a new 127.0.0.1 socket instead of a wildcard
// dual-stack one. Like quic.DialAddr's socket, it belongs to the connection
// and closes when the connection ends.
func dialLocalhost(
	ctx context.Context,
	dial func(context.Context, net.PacketConn, net.Addr, *tls.Config, *quic.Config) (*quic.Conn, error),
	addr string,
	tlsConf *tls.Config,
	conf *quic.Config,
) (*quic.Conn, error) {
	remote, err := net.ResolveUDPAddr("udp4", addr)
	if err != nil {
		return nil, err
	}
	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		return nil, err
	}
	conn, err := dial(ctx, udpConn, remote, tlsConf, conf)
	if err != nil {
		udpConn.Close()
		return nil, err
	}
	context.AfterFunc(conn.Context(), func() { udpConn.Close() })
	return conn, nil
}

// requireLoopbackClient enforces that a darwin client is not on a wildcard
// dual-stack socket, which can miss loopback replies (golang/go#67226).
func requireLoopbackClient(t *testing.T, conn *quic.Conn) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		return
	}
	local, ok := conn.LocalAddr().(*net.UDPAddr)
	require.True(t, ok && local.IP.Equal(net.IPv4(127, 0, 0, 1)), "client socket %v is not bound to 127.0.0.1 (golang/go#67226)", conn.LocalAddr())
}

func requireLoopbackClients(t *testing.T, rt *RoundTripper) {
	t.Helper()
	rt.mutex.Lock()
	defer rt.mutex.Unlock()
	require.NotEmpty(t, rt.clients)
	for _, c := range rt.clients {
		require.NotNil(t, c.conn)
		requireLoopbackClient(t, c.conn)
	}
}

func TestServerFixtureClosesSocket(t *testing.T) {
	var addr net.Addr
	t.Run("fixture", func(t *testing.T) { addr = startServer(t, nil) })
	conn, err := net.ListenUDP("udp", addr.(*net.UDPAddr))
	require.NoError(t, err, "server fixture must release its UDP socket")
	require.NoError(t, conn.Close())
}
