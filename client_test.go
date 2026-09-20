package quic

import (
	"context"
	"crypto/tls"
	"net"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDial(t *testing.T) {
	t.Run("Dial", func(t *testing.T) {
		testDial(t,
			func(ctx context.Context, addr net.Addr) error {
				conn := newUDPConnLocalhost(t)
				_, err := Dial(ctx, conn, addr, &tls.Config{}, nil)
				return err
			},
			false,
		)
	})

	t.Run("DialEarly", func(t *testing.T) {
		testDial(t,
			func(ctx context.Context, addr net.Addr) error {
				conn := newUDPConnLocalhost(t)
				_, err := DialEarly(ctx, conn, addr, &tls.Config{}, nil)
				return err
			},
			false,
		)
	})

	t.Run("DialAddr", func(t *testing.T) {
		testDial(t,
			func(ctx context.Context, addr net.Addr) error {
				_, err := DialAddr(ctx, addr.String(), &tls.Config{}, nil)
				return err
			},
			true,
		)
	})

	t.Run("DialAddrEarly", func(t *testing.T) {
		testDial(t,
			func(ctx context.Context, addr net.Addr) error {
				_, err := DialAddrEarly(ctx, addr.String(), &tls.Config{}, nil)
				return err
			},
			true,
		)
	})
}

func testDial(t *testing.T,
	dialFn func(context.Context, net.Addr) error,
	shouldCloseConn bool,
) {
	var capture *dialCapture
	var socket **net.UDPConn
	if shouldCloseConn {
		capture = newDialCapture(t.Name())
		socket = captureAddrSocket(t, capture)
		defer func() {
			if report := capture.finish(t.Failed()); report != nil {
				t.Logf("dial-capture %s", report)
			}
		}()
	}
	server := newUDPConnLocalhost(t)

	ctx, cancel := context.WithCancel(context.Background())
	errChan := make(chan error, 1)
	go func() { errChan <- dialFn(ctx, server.LocalAddr()) }()

	server.SetReadDeadline(time.Now().Add(time.Second))
	_, addr, err := server.ReadFrom(make([]byte, 1500))
	capture.record("datagram_received", map[string]any{"source": addr, "server": server.LocalAddr().String(), "error": dialCaptureError(err)})
	require.NoError(t, err)
	capture.record("cancel_requested", nil)
	cancel()
	select {
	case err := <-errChan:
		capture.record("dial_return", map[string]any{"error": dialCaptureError(err), "canceled": ctx.Err() == context.Canceled})
		if capture != nil {
			capture.observeSocket(*socket)
		}
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		capture.record("dial_result_timeout", "original socket state unavailable without dial completion")
		t.Fatal("timeout")
	}

	if shouldCloseConn {
		// The socket that the client used for dialing should be closed now.
		// Binding to the same address would error if the address was still in use.
		require.Eventually(t, func() bool {
			conn, err := capture.rebind(addr.(*net.UDPAddr))
			if err != nil {
				return false
			}
			closeErr := conn.Close()
			capture.record("rebind_socket_close", dialCaptureError(closeErr))
			return true
		}, scaleDuration(200*time.Millisecond), scaleDuration(10*time.Millisecond))
		require.False(t, areTransportsRunning())
		if os.Getenv("QUIC_GO_DIAL_CAPTURE_FAIL_TEST") == t.Name() {
			t.Error("controlled dial capture failure")
		}
		return
	}

	// The socket that the client used for dialing should not be closed now.
	// Binding to the same address will error if the address was still in use.
	_, err = net.ListenUDP("udp", addr.(*net.UDPAddr))
	require.Error(t, err)
	if runtime.GOOS == "windows" {
		require.ErrorContains(t, err, "bind: Only one usage of each socket address")
	} else {
		require.ErrorContains(t, err, "address already in use")
	}

	require.False(t, areTransportsRunning())
}

// captureAddrSocket retains the actual socket so finalizers cannot hide missing cleanup.
// These tests must remain sequential while the socket factory is replaced.
func captureAddrSocket(t *testing.T, capture *dialCapture) **net.UDPConn {
	t.Helper()
	original := listenUDPConn
	var socket *net.UDPConn
	listenUDPConn = func(network string, addr *net.UDPAddr) (*net.UDPConn, error) {
		conn, err := original(network, addr)
		if err == nil {
			socket = conn
			if capture != nil && capture.ownerEnabled {
				raw, controlErr := conn.SyscallConn()
				var descriptor *uintptr
				if controlErr == nil {
					controlErr = raw.Control(func(fd uintptr) { descriptor = &fd })
				}
				capture.record("original_socket_created", map[string]any{"descriptor": descriptor, "address": conn.LocalAddr().String(), "control_error": dialCaptureError(controlErr)})
			}
			t.Cleanup(func() { conn.Close() })
		}
		return conn, err
	}
	t.Cleanup(func() { listenUDPConn = original })
	return &socket
}

func TestDialAddrSetupFailureClosesSocket(t *testing.T) {
	for _, dial := range []struct {
		name string
		fn   func(context.Context, string, *tls.Config, *Config) (*Conn, error)
	}{
		{"DialAddr", DialAddr},
		{"DialAddrEarly", DialAddrEarly},
	} {
		t.Run(dial.name, func(t *testing.T) {
			for _, tc := range []struct {
				name      string
				addr      string
				tlsConf   *tls.Config
				conf      *Config
				errorText string
			}{
				{"resolution", "127.0.0.1", &tls.Config{}, nil, "missing port"},
				{"TLS configuration", "127.0.0.1:443", nil, nil, "tls.Config not set"},
				{"QUIC configuration", "127.0.0.1:443", &tls.Config{}, &Config{Versions: []Version{0x1234}}, "invalid QUIC version"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					socket := captureAddrSocket(t, nil)
					conn, err := dial.fn(context.Background(), tc.addr, tc.tlsConf, tc.conf)
					require.ErrorContains(t, err, tc.errorText)
					require.Nil(t, conn)
					require.NotNil(t, *socket)
					require.ErrorIs(t, (*socket).SetReadDeadline(time.Now()), net.ErrClosed)
				})
			}
		})
	}
}

func TestSetupFailurePreservesCallerSocket(t *testing.T) {
	for _, setup := range []struct {
		name string
		fn   func(net.PacketConn, *tls.Config, *Config) error
	}{
		{"Dial", func(socket net.PacketConn, tlsConf *tls.Config, conf *Config) error {
			_, err := Dial(context.Background(), socket, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 443}, tlsConf, conf)
			return err
		}},
		{"DialEarly", func(socket net.PacketConn, tlsConf *tls.Config, conf *Config) error {
			_, err := DialEarly(context.Background(), socket, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 443}, tlsConf, conf)
			return err
		}},
		{"Listen", func(socket net.PacketConn, tlsConf *tls.Config, conf *Config) error {
			_, err := Listen(socket, tlsConf, conf)
			return err
		}},
		{"ListenEarly", func(socket net.PacketConn, tlsConf *tls.Config, conf *Config) error {
			_, err := ListenEarly(socket, tlsConf, conf)
			return err
		}},
	} {
		t.Run(setup.name, func(t *testing.T) {
			for _, tc := range []struct {
				name      string
				tlsConf   *tls.Config
				conf      *Config
				errorText string
			}{
				{"TLS configuration", nil, nil, "tls.Config not set"},
				{"QUIC configuration", &tls.Config{}, &Config{Versions: []Version{0x1234}}, "invalid QUIC version"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					socket := newUDPConnLocalhost(t)
					require.ErrorContains(t, setup.fn(socket, tc.tlsConf, tc.conf), tc.errorText)
					require.NoError(t, socket.SetReadDeadline(time.Time{}))
					peer := newUDPConnLocalhost(t)
					_, err := socket.WriteTo([]byte("still open"), peer.LocalAddr())
					require.NoError(t, err)
					require.NoError(t, peer.SetReadDeadline(time.Now().Add(time.Second)))
					buf := make([]byte, 32)
					n, _, err := peer.ReadFrom(buf)
					require.NoError(t, err)
					require.Equal(t, "still open", string(buf[:n]))
				})
			}
		})
	}
}
