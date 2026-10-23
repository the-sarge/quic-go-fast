package http3

import (
	"crypto/tls"
	"errors"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go/http3/internal/testdata"

	"github.com/stretchr/testify/require"
)

// failingListener makes the combined helper's TCP serving loop fail on demand.
type failingListener struct {
	net.Listener

	mu        sync.Mutex
	err       error
	accepting int
}

func (l *failingListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	if l.err != nil {
		defer l.mu.Unlock()
		return nil, l.err
	}
	l.accepting++
	l.mu.Unlock()

	conn, err := l.Listener.Accept()

	l.mu.Lock()
	defer l.mu.Unlock()
	l.accepting--
	if l.err != nil {
		if conn != nil {
			conn.Close()
		}
		return nil, l.err
	}
	return conn, err
}

// fail makes the pending and every later Accept return err.
func (l *failingListener) fail(err error) {
	l.mu.Lock()
	l.err = err
	l.mu.Unlock()
	l.Close()
}

func (l *failingListener) acceptInProgress() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.accepting > 0
}

// failingPacketConn makes the combined helper's QUIC serving loop fail on
// demand. It hides the *net.UDPConn so that the transport reads through ReadFrom.
type failingPacketConn struct {
	net.PacketConn

	mu  sync.Mutex
	err error
}

func (c *failingPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	if err := c.failure(); err != nil {
		return 0, nil, err
	}
	n, addr, err := c.PacketConn.ReadFrom(b)
	if failErr := c.failure(); failErr != nil {
		return 0, nil, failErr
	}
	return n, addr, err
}

func (c *failingPacketConn) failure() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// fail makes the pending and every later ReadFrom return err, which the QUIC
// transport treats as a permanent read failure.
func (c *failingPacketConn) fail(err error) {
	c.mu.Lock()
	c.err = err
	c.mu.Unlock()
	c.SetReadDeadline(time.Now())
}

type combinedServer struct {
	tcp *failingListener
	udp *failingPacketConn

	returned chan struct{}
	err      error // set before returned is closed
}

// startCombinedServer runs the combined helper on loopback with failable
// sockets. The TCP and UDP ports differ; Alt-Svc announces the UDP port.
func startCombinedServer(t *testing.T, handler http.Handler) *combinedServer {
	t.Helper()
	tcpCh := make(chan *failingListener, 1)
	udpCh := make(chan *failingPacketConn, 1)
	hooks := combinedSocketHooks{
		wrapTCP: func(ln net.Listener) net.Listener {
			l := &failingListener{Listener: ln}
			tcpCh <- l
			return l
		},
		wrapUDP: func(conn net.PacketConn) net.PacketConn {
			c := &failingPacketConn{PacketConn: conn}
			udpCh <- c
			return c
		},
	}
	s := &combinedServer{returned: make(chan struct{})}
	certFile, keyFile := testdata.GetCertificatePaths()
	go func() {
		defer close(s.returned)
		s.err = listenAndServeTLS("127.0.0.1:0", certFile, keyFile, handler, hooks)
	}()
	for s.tcp == nil || s.udp == nil {
		select {
		case s.tcp = <-tcpCh:
		case s.udp = <-udpCh:
		case <-s.returned:
			t.Fatalf("combined helper returned during startup: %v", s.err)
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for the combined helper's sockets")
		}
	}
	t.Cleanup(func() {
		s.tcp.fail(errors.New("test cleanup"))
		s.wait(t)
	})
	return s
}

// wait returns the combined helper's error once it has returned.
func (s *combinedServer) wait(t *testing.T) error {
	t.Helper()
	select {
	case <-s.returned:
		return s.err
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for the combined helper to return")
		return nil
	}
}

func combinedTestTLSConfig() *tls.Config {
	return &tls.Config{RootCAs: testdata.GetRootCA(), ServerName: "localhost"}
}

func newCombinedTCPClient(t *testing.T, http2 bool) *http.Client {
	t.Helper()
	tr := &http.Transport{TLSClientConfig: combinedTestTLSConfig(), ForceAttemptHTTP2: http2}
	t.Cleanup(tr.CloseIdleConnections)
	return &http.Client{Transport: tr, Timeout: 10 * time.Second}
}

func newCombinedQUICClient(t *testing.T) *http.Client {
	t.Helper()
	tr := &Transport{TLSClientConfig: combinedTestTLSConfig()}
	t.Cleanup(func() { tr.Close() })
	return &http.Client{Transport: tr, Timeout: 10 * time.Second}
}

func getBody(t *testing.T, client *http.Client, url string) *http.Response {
	t.Helper()
	resp, err := client.Get(url)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "combined", string(body))
	return resp
}

// blockingRequest is a request held by its handler until the request
// context is canceled.
type blockingRequest struct {
	hijack bool // the handler hijacks the connection and closes it on return

	started    chan struct{}
	finished   chan struct{}
	clientDone chan error
}

// blockingHandlerLinger keeps a canceled handler running a little longer, so
// a helper that does not wait for its handlers returns before it finishes.
const blockingHandlerLinger = 100 * time.Millisecond

func newBlockingRequest() *blockingRequest {
	return &blockingRequest{
		started:    make(chan struct{}),
		finished:   make(chan struct{}),
		clientDone: make(chan error, 1),
	}
}

func (b *blockingRequest) handle(w http.ResponseWriter, r *http.Request) {
	defer close(b.finished)
	if b.hijack {
		conn, _, err := http.NewResponseController(w).Hijack()
		if err != nil {
			panic(err)
		}
		defer conn.Close()
	}
	close(b.started)
	<-r.Context().Done()
	time.Sleep(blockingHandlerLinger)
}

// send starts the request and waits until its handler is running.
func (b *blockingRequest) send(t *testing.T, request func() (*http.Response, error)) {
	t.Helper()
	go func() {
		resp, err := request()
		if err == nil {
			resp.Body.Close()
		}
		b.clientDone <- err
	}()
	select {
	case <-b.started:
	case err := <-b.clientDone:
		t.Fatalf("blocking request ended before its handler started: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for the blocking handler to start")
	}
}

// requireInterrupted checks that the handler has returned and that the
// client saw its request interrupted.
func (b *blockingRequest) requireInterrupted(t *testing.T) {
	t.Helper()
	select {
	case <-b.finished:
	default:
		t.Fatal("handler still running after the combined helper returned")
	}
	select {
	case err := <-b.clientDone:
		require.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for the interrupted client request")
	}
}

func newCombinedMux(blocking *blockingRequest) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "combined")
	})
	if blocking != nil {
		mux.HandleFunc("/block", blocking.handle)
	}
	return mux
}

// requireSocketsReleased checks that neither of the combined helper's sockets
// is still open: the TCP port refuses connections and the UDP port rebinds.
func requireSocketsReleased(t *testing.T, tcpAddr, udpAddr net.Addr) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", tcpAddr.String(), 5*time.Second)
	if err == nil {
		conn.Close()
		t.Fatalf("TCP listener %s still accepts connections", tcpAddr)
	}
	var nerr net.Error
	require.False(t, errors.As(err, &nerr) && nerr.Timeout(), "dialing %s timed out: %v", tcpAddr, err)
	udpConn, err := net.ListenPacket("udp", udpAddr.String())
	require.NoError(t, err, "UDP socket %s still bound", udpAddr)
	require.NoError(t, udpConn.Close())
}

func TestListenAndServeTLSServesBothProtocols(t *testing.T) {
	s := startCombinedServer(t, newCombinedMux(nil))
	udpPort := s.udp.LocalAddr().(*net.UDPAddr).Port

	// HTTP/3 first: Alt-Svc is announced once the QUIC listener is registered.
	resp := getBody(t, newCombinedQUICClient(t), "https://"+s.udp.LocalAddr().String()+"/")
	require.Equal(t, 3, resp.ProtoMajor)

	resp = getBody(t, newCombinedTCPClient(t, true), "https://"+s.tcp.Addr().String()+"/")
	require.Equal(t, 2, resp.ProtoMajor)
	require.Equal(t, []string{`h3=":` + strconv.Itoa(udpPort) + `"; ma=2592000`}, resp.Header.Values("Alt-Svc"))

	errStop := errors.New("stop serving")
	s.tcp.fail(errStop)
	require.ErrorIs(t, s.wait(t), errStop)
}

func TestListenAndServeTLSTCPFailureStopsQUIC(t *testing.T) {
	blocking := newBlockingRequest()
	s := startCombinedServer(t, newCombinedMux(blocking))
	client := newCombinedQUICClient(t)
	blocking.send(t, func() (*http.Response, error) {
		return client.Get("https://" + s.udp.LocalAddr().String() + "/block")
	})

	errTCP := errors.New("tcp accept failed")
	s.tcp.fail(errTCP)
	err := s.wait(t)

	require.ErrorIs(t, err, errTCP)
	blocking.requireInterrupted(t)
	requireSocketsReleased(t, s.tcp.Addr(), s.udp.LocalAddr())
}

func TestListenAndServeTLSQUICFailureStopsTCP(t *testing.T) {
	for _, tc := range []struct {
		name       string
		http2      bool
		unreadBody bool // closing the connection doesn't cancel the request context
		hijack     bool // the connection is the handler's, not the server's
	}{
		{name: "HTTP/2", http2: true},
		{name: "HTTP/1.1 with unread body", unreadBody: true},
		{name: "hijacked HTTP/1.1", hijack: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			blocking := newBlockingRequest()
			blocking.hijack = tc.hijack
			s := startCombinedServer(t, newCombinedMux(blocking))
			client := newCombinedTCPClient(t, tc.http2)
			url := "https://" + s.tcp.Addr().String() + "/block"
			blocking.send(t, func() (*http.Response, error) {
				if tc.unreadBody {
					return client.Post(url, "text/plain", strings.NewReader("unread request body"))
				}
				return client.Get(url)
			})

			errUDP := errors.New("udp read failed")
			s.udp.fail(errUDP)
			err := s.wait(t)

			require.ErrorIs(t, err, errUDP)
			require.False(t, s.tcp.acceptInProgress(), "TCP serving loop still accepting")
			blocking.requireInterrupted(t)
			requireSocketsReleased(t, s.tcp.Addr(), s.udp.LocalAddr())
		})
	}
}

func TestListenAndServeTLSStartupFailureReleasesUDP(t *testing.T) {
	// Windows can deny UDP binds to TCP-allocated ports and vice versa. Spread
	// candidates across unprivileged ports so a restricted ephemeral range does
	// not consume every attempt, and require both protocols to bind the address.
	logBind := func(attempt int, network, addr string, err error) {
		t.Helper()
		t.Logf("port selection attempt %d: %s listen %s: %v", attempt, network, addr, err)
		for cause := errors.Unwrap(err); cause != nil; cause = errors.Unwrap(cause) {
			t.Logf("port selection attempt %d: cause %T: %v (%#v)", attempt, cause, cause, cause)
		}
	}
	var tcpLn net.Listener
	var addr string
	for attempt := range 10 {
		addr = net.JoinHostPort("127.0.0.1", strconv.Itoa(1024+rand.IntN(65536-1024)))
		candidate, err := net.Listen("tcp", addr)
		logBind(attempt+1, "TCP", addr, err)
		if err != nil {
			continue
		}
		t.Cleanup(func() { candidate.Close() })
		addr = candidate.Addr().String()
		udpConn, err := net.ListenPacket("udp", addr)
		logBind(attempt+1, "UDP", addr, err)
		if err != nil {
			require.NoError(t, candidate.Close())
			continue
		}
		t.Cleanup(func() { udpConn.Close() })
		require.NoError(t, udpConn.Close())
		tcpLn = candidate
		break
	}
	require.NotNil(t, tcpLn, "no port free for both TCP and UDP after 10 attempts; see per-attempt bind errors")

	// The hooks only run once both sockets are open.
	hooks := combinedSocketHooks{
		wrapTCP: func(net.Listener) net.Listener {
			t.Error("TCP listener opened on a held port")
			return nil
		},
		wrapUDP: func(net.PacketConn) net.PacketConn {
			t.Error("serving started on a held port")
			return nil
		},
	}
	certFile, keyFile := testdata.GetCertificatePaths()
	err := listenAndServeTLS(addr, certFile, keyFile, newCombinedMux(nil), hooks)

	// The UDP socket is bound first, so a TCP listen error means it was open.
	var opErr *net.OpError
	require.ErrorAs(t, err, &opErr)
	require.Equal(t, "listen", opErr.Op)
	require.Equal(t, "tcp", opErr.Net)
	udpConn, err := net.ListenPacket("udp", addr)
	require.NoError(t, err, "UDP socket still bound after startup failure")
	t.Cleanup(func() { udpConn.Close() })
	require.NoError(t, udpConn.Close())
}
