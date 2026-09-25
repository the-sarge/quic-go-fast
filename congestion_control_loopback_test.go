package quic

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/testdata"
	"github.com/stretchr/testify/require"
)

func congestionControlPair(t *testing.T, serverConfig, clientConfig *Config) (*Conn, *Conn) {
	t.Helper()
	tlsConfig := testdata.GetTLSConfig()
	tlsConfig.NextProtos = []string{"congestion-control-v1"}
	serverTransport := &Transport{Conn: newUDPConnLocalhost(t)}
	t.Cleanup(func() { serverTransport.Close() })
	listener, err := serverTransport.Listen(tlsConfig, serverConfig)
	require.NoError(t, err)
	t.Cleanup(func() { listener.Close() })
	transport := &Transport{Conn: newUDPConnLocalhost(t)}
	t.Cleanup(func() { transport.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	client, err := transport.Dial(ctx, listener.Addr(), &tls.Config{RootCAs: testdata.GetRootCA(), ServerName: "localhost", NextProtos: tlsConfig.NextProtos}, clientConfig)
	require.NoError(t, err)
	t.Cleanup(func() { client.CloseWithError(0, "") })
	server, err := listener.Accept(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { server.CloseWithError(0, "") })
	return client, server
}

func exchangeCongestionControlTraffic(t *testing.T, from, to *Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	stream, err := from.OpenUniStreamSync(ctx)
	require.NoError(t, err)
	payload := bytes.Repeat([]byte("reliable control and stream payload\n"), 2048)
	written := make(chan error, 1)
	go func() {
		_, err := stream.Write(payload)
		if err == nil {
			err = stream.Close()
		}
		written <- err
	}()
	received, err := to.AcceptUniStream(ctx)
	require.NoError(t, err)
	require.NoError(t, received.SetReadDeadline(time.Now().Add(5*time.Second)))
	data, err := io.ReadAll(received)
	require.NoError(t, err)
	require.Equal(t, payload, data)
	require.NoError(t, <-written)
	require.NoError(t, from.SendDatagram([]byte("application datagram")))
	datagram, err := to.ReceiveDatagram(ctx)
	require.NoError(t, err)
	require.Equal(t, "application datagram", string(datagram))
}

func TestCongestionControlV1PerClientReplacement(t *testing.T) {
	for _, kind := range []string{"unset", "clone", "nil", "explicit Reno"} {
		t.Run(kind, func(t *testing.T) {
			conf := &Config{}
			require.NoError(t, conf.SetCongestionControlV1("bbrv3"))
			conf.GetConfigForClient = func(*ClientInfo) (*Config, error) {
				switch kind {
				case "clone":
					return conf.Clone(), nil
				case "nil":
					return nil, nil
				case "explicit Reno":
					replacement := conf.Clone()
					if err := replacement.SetCongestionControlV1("reno"); err != nil {
						return nil, err
					}
					return replacement, nil
				default:
					return &Config{}, nil
				}
			}
			client, server := congestionControlPair(t, conf, nil)
			require.Equal(t, "reno", client.CongestionControlV1())
			expected := "reno"
			if kind == "clone" {
				expected = "bbrv3"
			}
			require.Equal(t, expected, server.CongestionControlV1())
		})
	}
}

func TestCongestionControlV1RealStreamAndDatagram(t *testing.T) {
	for _, selection := range []string{"reno", "bbrv3"} {
		t.Run(selection, func(t *testing.T) {
			conf := &Config{EnableDatagrams: true}
			require.NoError(t, conf.SetCongestionControlV1(selection))
			client, server := congestionControlPair(t, conf, conf)
			// The API promises that later caller changes cannot change effective identity.
			require.NoError(t, conf.SetCongestionControlV1("reno"))
			for _, conn := range []*Conn{client, server} {
				require.Equal(t, selection, conn.CongestionControlV1())
			}
			exchangeCongestionControlTraffic(t, client, server)
			exchangeCongestionControlTraffic(t, server, client)
			require.NoError(t, client.CloseWithError(0, "finished"))
			require.Equal(t, selection, client.CongestionControlV1())
		})
	}
}

func TestCongestionControlV1MigrationKeepsIdentity(t *testing.T) {
	conf := &Config{EnableDatagrams: true}
	require.NoError(t, conf.SetCongestionControlV1("bbrv3"))
	client, server := congestionControlPair(t, conf, conf)
	exchangeCongestionControlTraffic(t, client, server)
	next := &Transport{Conn: newUDPConnLocalhost(t)}
	defer next.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	path, err := client.AddPath(next)
	require.NoError(t, err)
	require.ErrorIs(t, path.Switch(), ErrPathNotValidated)
	require.NoError(t, path.Probe(ctx))
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if client.CongestionControlV1() != "bbrv3" || server.CongestionControlV1() != "bbrv3" {
				t.Error("selection changed during migration")
				return
			}
		}
	}()
	defer func() { close(stop); <-done }()
	require.NoError(t, path.Switch())
	exchangeCongestionControlTraffic(t, client, server)
	exchangeCongestionControlTraffic(t, server, client)
	require.Equal(t, next.Conn.LocalAddr().String(), client.LocalAddr().String())
	require.Equal(t, next.Conn.LocalAddr().String(), server.RemoteAddr().String())
	require.NoError(t, client.CloseWithError(0, "finished"))
	require.Equal(t, "bbrv3", client.CongestionControlV1())
}
