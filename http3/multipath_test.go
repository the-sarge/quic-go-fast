//go:build darwin || linux || freebsd

package http3

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"runtime"
	"testing"
	"time"

	"github.com/quic-go/quic-go"

	"github.com/stretchr/testify/require"
)

// multipathProfileSupported names the one qualified platform of the public profile.
const multipathProfileSupported = runtime.GOOS == "linux" && runtime.GOARCH == "amd64"

func multipathQUICConfig(t *testing.T) *quic.Config {
	t.Helper()
	conf := &quic.Config{}
	require.NoError(t, conf.ConfigureMultipathV1(true, 2, "primary-backup"))
	return conf
}

// requireMultipathProfile skips a test that needs the public profile on a build where it
// isn't qualified. TestMultipathHTTP3PublicRefusal covers those builds.
func requireMultipathProfile(t *testing.T) {
	t.Helper()
	if !multipathProfileSupported {
		t.Skipf("multipath is qualified only on linux/amd64, not %s/%s", runtime.GOOS, runtime.GOARCH)
	}
}

func TestMultipathHTTP3PublicRefusal(t *testing.T) {
	if multipathProfileSupported {
		t.Skip("the qualified platform accepts the profile; the lifecycle tests cover it")
	}
	conf := multipathQUICConfig(t)
	t.Run("default client", func(t *testing.T) {
		tr := &Transport{QUICConfig: conf, TLSClientConfig: getTLSClientConfig()}
		defer tr.Close()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://127.0.0.1:9/", nil)
		require.NoError(t, err)
		_, err = tr.RoundTrip(req)
		require.ErrorIs(t, err, quic.ErrMultipathUnsupportedV1)
	})
	t.Run("address server", func(t *testing.T) {
		s := &Server{Addr: "127.0.0.1:0", TLSConfig: getTLSConfig(), QUICConfig: conf}
		require.ErrorIs(t, s.ListenAndServe(), quic.ErrMultipathUnsupportedV1)
	})
}

func TestMultipathHTTP3DefaultClientOwnsItsEndpoint(t *testing.T) {
	requireMultipathProfile(t)
	serverTr := &quic.Transport{Conn: newUDPConnLocalhost(t)}
	require.NoError(t, serverTr.ConfigureMultipathIOV1(time.Second))
	defer serverTr.Close()
	ln, err := serverTr.ListenEarly(getTLSConfig(), multipathQUICConfig(t))
	require.NoError(t, err)
	defer ln.Close()
	serverConns := make(chan *quic.Conn, 1)
	s := &Server{
		Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		ConnContext: func(ctx context.Context, c *quic.Conn) context.Context {
			serverConns <- c
			return ctx
		},
	}
	defer s.Close()
	go s.ServeListener(ln)

	tr := &Transport{QUICConfig: multipathQUICConfig(t), TLSClientConfig: getTLSClientConfig()}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+serverTr.Conn.LocalAddr().String()+"/", nil)
	require.NoError(t, err)
	rsp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	rsp.Body.Close()
	server := <-serverConns
	<-server.HandshakeComplete()
	negotiated, err := server.MultipathNegotiatedV1()
	require.NoError(t, err)
	require.True(t, negotiated, "the default client advertised on its granted socket")

	socket := tr.transport4.Conn.(*net.UDPConn)
	require.NoError(t, tr.Close())
	_, err = socket.WriteToUDP([]byte("x"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9})
	require.ErrorIs(t, err, net.ErrClosed, "the owner joined and released its socket")
	// Well inside the 30 s default idle timeout, and the close itself rather than a timeout.
	select {
	case <-server.Context().Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the client's close didn't arrive")
	}
	var closeErr *quic.ApplicationError
	require.ErrorAs(t, context.Cause(server.Context()), &closeErr)
	require.True(t, closeErr.Remote)
}

func TestMultipathHTTP3CallerSockets(t *testing.T) {
	requireMultipathProfile(t)
	t.Run("Serve can't grant a caller's socket", func(t *testing.T) {
		s := &Server{TLSConfig: getTLSConfig(), QUICConfig: multipathQUICConfig(t)}
		socket := newUDPConnLocalhost(t)
		require.ErrorIs(t, s.Serve(socket), quic.ErrMultipathUnsupportedV1)
		_, err := socket.WriteToUDP([]byte("x"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9})
		require.NoError(t, err, "the socket stays the caller's")
	})
	t.Run("a dial hook keeps its constructor's gate", func(t *testing.T) {
		ungranted := &quic.Transport{Conn: newUDPConnLocalhost(t)}
		defer ungranted.Close()
		tr := &Transport{
			QUICConfig:      multipathQUICConfig(t),
			TLSClientConfig: getTLSClientConfig(),
			Dial: func(ctx context.Context, _ string, tlsConf *tls.Config, conf *quic.Config) (*quic.Conn, error) {
				return ungranted.DialEarly(ctx, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9}, tlsConf, conf)
			},
		}
		defer tr.Close()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://127.0.0.1:9/", nil)
		require.NoError(t, err)
		_, err = tr.RoundTrip(req)
		require.ErrorIs(t, err, quic.ErrMultipathNotConfiguredV1)
	})
	t.Run("an address server owns its endpoint", func(t *testing.T) {
		socket, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		require.NoError(t, err)
		addr := socket.LocalAddr().String()
		require.NoError(t, socket.Close())
		s := &Server{Addr: addr, TLSConfig: getTLSConfig(), QUICConfig: multipathQUICConfig(t), Handler: http.NewServeMux()}
		served := make(chan error, 1)
		go func() { served <- s.ListenAndServe() }()
		var conn *quic.Conn
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		require.Eventually(t, func() bool {
			var err error
			dialCtx, dialCancel := context.WithTimeout(ctx, 200*time.Millisecond)
			defer dialCancel()
			conf := multipathQUICConfig(t)
			dialer := &quic.Transport{Conn: newUDPConnLocalhost(t)}
			require.NoError(t, dialer.ConfigureMultipathIOV1(time.Second))
			t.Cleanup(func() { dialer.Close() })
			udpAddr, resolveErr := net.ResolveUDPAddr("udp", addr)
			require.NoError(t, resolveErr)
			conn, err = dialer.Dial(dialCtx, udpAddr, getTLSClientConfig(), conf)
			return err == nil
		}, 5*time.Second, 10*time.Millisecond)
		negotiated, err := conn.MultipathNegotiatedV1()
		require.NoError(t, err)
		require.True(t, negotiated, "the helper granted its own socket before advertising")
		require.NoError(t, s.Close())
		err = <-served
		require.True(t, errors.Is(err, http.ErrServerClosed), "got %v", err)
	})
}
