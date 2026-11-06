package quic_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	quic "github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/internal/testdata"
	"github.com/quic-go/quic-go/internal/testutils/quicproxy"
	"github.com/stretchr/testify/require"
)

// This test uses only exported Transport and Conn operations over ordinary UDP
// sockets. Its Windows execution is the public opt-in's loopback support gate.
func TestPacketCeilingPublicLoopback(t *testing.T) {
	t.Setenv("QUIC_GO_DISABLE_GSO", "true")
	udp := func() *net.UDPConn {
		t.Helper()
		socket, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		require.NoError(t, err)
		t.Cleanup(func() { socket.Close() })
		return socket
	}
	serverSocket, clientSocket, proxySocket := udp(), udp(), udp()
	// Establish the entire accepted UDP payload, independently of discovery's
	// final binary-search size. Failure here requires applying the Windows gate.
	payload := bytes.Repeat([]byte{'j'}, 8972)
	require.NoError(t, serverSocket.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, err := clientSocket.WriteTo(payload, serverSocket.LocalAddr())
	require.NoError(t, err)
	buffer := make([]byte, 8973)
	n, _, err := serverSocket.ReadFrom(buffer)
	require.NoError(t, err)
	require.Equal(t, payload, buffer[:n])
	require.NoError(t, serverSocket.SetReadDeadline(time.Time{}))

	var limit, largest atomic.Int64
	proxy := &quicproxy.Proxy{
		Conn: proxySocket, ServerAddr: serverSocket.LocalAddr().(*net.UDPAddr), PacketSize: 8973,
		DropPacket: func(dir quicproxy.Direction, _, _ net.Addr, b []byte) bool {
			if dir != quicproxy.DirectionIncoming {
				return false
			}
			for old := largest.Load(); int64(len(b)) > old; old = largest.Load() {
				if largest.CompareAndSwap(old, int64(len(b))) {
					break
				}
			}
			return limit.Load() != 0 && int64(len(b)) > limit.Load()
		},
	}
	require.NoError(t, proxy.Start())
	t.Cleanup(func() { require.NoError(t, proxy.Close()) })
	server, client := &quic.Transport{Conn: serverSocket}, &quic.Transport{Conn: clientSocket}
	t.Cleanup(func() { client.Close(); server.Close() })
	require.NoError(t, server.ConfigurePacketCeilingV1(8972))
	require.NoError(t, client.ConfigurePacketCeilingV1(8972))
	serverTLS := testdata.GetTLSConfig()
	serverTLS.NextProtos = []string{"public-packet-ceiling"}
	conf := &quic.Config{InitialPacketSize: 1232, EnableDatagrams: true}
	ln, err := server.Listen(serverTLS, conf)
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, proxySocket.LocalAddr(), &tls.Config{InsecureSkipVerify: true, NextProtos: serverTLS.NextProtos}, conf)
	require.NoError(t, err)
	s, err := ln.Accept(ctx)
	require.NoError(t, err)
	select {
	case <-c.PathMTUSettledV1():
	case <-ctx.Done():
		t.Fatal("public-API jumbo discovery did not settle")
	}
	ceiling, stageOne := c.PathMTUInfoV1()
	require.EqualValues(t, 8972, ceiling)
	require.EqualValues(t, 1438, stageOne)
	transfer := func(data []byte) {
		t.Helper()
		done := make(chan error, 1)
		t.Cleanup(func() { c.CloseWithError(0, ""); <-done })
		go func() {
			defer close(done)
			stream, err := c.OpenUniStreamSync(ctx)
			if err == nil {
				_, err = stream.Write(data)
				if err == nil {
					err = stream.Close()
				}
			}
			done <- err
		}()
		stream, err := s.AcceptUniStream(ctx)
		require.NoError(t, err)
		require.NoError(t, stream.SetReadDeadline(time.Now().Add(10*time.Second)))
		got, err := io.ReadAll(stream)
		require.NoError(t, err)
		require.NoError(t, <-done)
		require.Equal(t, data, got)
	}
	largest.Store(0)
	transfer(bytes.Repeat([]byte("public jumbo stream"), 16384))
	require.Greater(t, largest.Load(), int64(8900), "ordinary public-API STREAM packets use jumbo size")
	t.Logf("public API: raw UDP payload 8972, largest STREAM UDP payload %d", largest.Load())
	// The peer receiving this message proves jumbo-active data delivery before
	// the proxy begins dropping packets above the new path limit.
	require.NoError(t, c.SendDatagram(bytes.Repeat([]byte{'d'}, 8200)))
	got, err := s.ReceiveDatagram(ctx)
	require.NoError(t, err)
	require.Len(t, got, 8200)
	limit.Store(1500)
	transfer(bytes.Repeat([]byte("public stream survives shrink"), 8192))
	select {
	case <-c.PathMTUFallbackV1():
	default:
		t.Fatal("public-API shrink did not trigger recovery")
	}
	t.Log("public API: intact stream after far-side shrink to 1500")
}
