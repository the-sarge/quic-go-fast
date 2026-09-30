package self_test

import (
	"context"
	"io"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	quicproxy "github.com/quic-go/quic-go/integrationtests/tools/proxy"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/quic-go/quic-go/testutils/events"
	"github.com/stretchr/testify/require"
)

// This contract has only standard and upstream types, so consumers can negotiate
// the optional extension without naming a fork-specific exported policy type.
type networkAdmissionV1 interface {
	ConfigureNetworkAdmissionV1(net.PacketConn, func(netip.AddrPort, netip.AddrPort, []byte) bool, func(netip.AddrPort, netip.AddrPort, []byte) bool, bool) error
}

func configureNetworkAdmission(t *testing.T, tr *quic.Transport, receive, send func(netip.AddrPort, netip.AddrPort, []byte) bool, disableMigration bool) error {
	t.Helper()
	api, ok := any(tr).(networkAdmissionV1)
	require.True(t, ok)
	return api.ConfigureNetworkAdmissionV1(tr.Conn, receive, send, disableMigration)
}

func TestNetworkAdmissionEligibleRebindingAndSpoofedTraffic(t *testing.T) {
	serverTransport := &quic.Transport{Conn: newUDPConnLocalhost(t)}
	defer serverTransport.Close()
	denied := newUDPConnLocalhost(t)
	deniedEndpoint := denied.LocalAddr().(*net.UDPAddr).AddrPort()
	var rejected atomic.Int64
	require.NoError(t, configureNetworkAdmission(t, serverTransport,
		func(remote, _ netip.AddrPort, _ []byte) bool {
			if remote == deniedEndpoint {
				rejected.Add(1)
				return false
			}
			return remote.Addr().IsLoopback()
		}, func(remote, _ netip.AddrPort, _ []byte) bool { return remote.Addr().IsLoopback() }, true))
	var recorder events.Recorder
	ln, err := serverTransport.Listen(getTLSConfig(), &quic.Config{Tracer: func(context.Context, bool, quic.ConnectionID) qlogwriter.Trace {
		return &events.Trace{Recorder: &recorder}
	}})
	require.NoError(t, err)
	var captureMutex sync.Mutex
	var genuine, serverPacket []byte
	var outgoing atomic.Int64
	proxy := &quicproxy.Proxy{Conn: newUDPConnLocalhost(t), ServerAddr: ln.Addr().(*net.UDPAddr), ObserveSocket: func(e quicproxy.SocketEvent) {
		if e.Direction == quicproxy.DirectionIncoming && e.Operation == "write" && e.Err == nil && len(e.Data) > 0 && !wire.IsLongHeaderPacket(e.Data[0]) {
			captureMutex.Lock()
			genuine = append([]byte(nil), e.Data...)
			captureMutex.Unlock()
		}
		if e.Direction == quicproxy.DirectionOutgoing && e.Operation == "write" && e.Err == nil {
			outgoing.Add(1)
			captureMutex.Lock()
			serverPacket = append([]byte(nil), e.Data...)
			captureMutex.Unlock()
		}
	}}
	require.NoError(t, proxy.Start())
	defer proxy.Close()
	clientTransport := &quic.Transport{Conn: newUDPConnLocalhost(t)}
	defer clientTransport.Close()
	serverEndpoint := proxy.LocalAddr().(*net.UDPAddr).AddrPort()
	var clientRejected atomic.Int64
	require.NoError(t, configureNetworkAdmission(t, clientTransport,
		func(remote, _ netip.AddrPort, _ []byte) bool {
			if remote != serverEndpoint {
				clientRejected.Add(1)
				return false
			}
			return true
		}, func(remote, _ netip.AddrPort, _ []byte) bool { return remote == serverEndpoint }, false))
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	client, err := clientTransport.Dial(ctx, proxy.LocalAddr(), getTLSClientConfig(), nil)
	require.NoError(t, err)
	defer client.CloseWithError(0, "")
	server, err := ln.Accept(ctx)
	require.NoError(t, err)
	defer server.CloseWithError(0, "")
	exchangeAdmissionStream(t, ctx, client, server)
	captureMutex.Lock()
	replay := append([]byte(nil), genuine...)
	wrongServer := append([]byte(nil), serverPacket...)
	captureMutex.Unlock()
	require.NotEmpty(t, replay)
	_, err = denied.WriteTo(replay, ln.Addr())
	require.NoError(t, err)
	_, err = denied.WriteTo(wrongServer, clientTransport.Conn.LocalAddr())
	require.NoError(t, err)
	exchangeAdmissionStream(t, ctx, client, server)
	require.Positive(t, rejected.Load())
	require.Eventually(t, func() bool { return clientRejected.Load() > 0 }, time.Second, time.Millisecond)
	require.NoError(t, denied.SetReadDeadline(time.Now().Add(scaleDuration(20*time.Millisecond))))
	_, _, err = denied.ReadFrom(make([]byte, 1500))
	require.Error(t, err, "forbidden genuine input must not generate any response")
	changed := newUDPConnLocalhost(t)
	require.NoError(t, proxy.SwitchConn(clientTransport.Conn.LocalAddr().(*net.UDPAddr), changed))
	before := outgoing.Load()
	exchangeAdmissionStream(t, ctx, client, server)
	require.Eventually(t, func() bool { return outgoing.Load() > before }, time.Second, time.Millisecond)
	require.Eventually(t, func() bool {
		for _, event := range recorder.Events(qlog.PacketSent{}) {
			for _, frame := range event.(qlog.PacketSent).Frames {
				if _, ok := frame.Frame.(*qlog.PathChallengeFrame); ok {
					return true
				}
			}
		}
		return false
	}, time.Second, time.Millisecond, "eligible port rebinding must enter standard path validation")
}

func exchangeAdmissionStream(t *testing.T, ctx context.Context, client, server *quic.Conn) {
	t.Helper()
	stream, err := client.OpenUniStreamSync(ctx)
	require.NoError(t, err)
	_, err = stream.Write([]byte("admitted reliable content"))
	require.NoError(t, err)
	require.NoError(t, stream.Close())
	received, err := server.AcceptUniStream(ctx)
	require.NoError(t, err)
	require.NoError(t, received.SetReadDeadline(time.Now().Add(5*time.Second)))
	data, err := io.ReadAll(received)
	require.NoError(t, err)
	require.Equal(t, "admitted reliable content", string(data))
}
