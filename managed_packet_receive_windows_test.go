//go:build windows

package quic

import (
	"net"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
)

func TestWindowsManagedReceiveRegistration(t *testing.T) {
	t.Setenv("QUIC_GO_DISABLE_GRO", "0")
	probe, _ := newWindowsOwnedConn(t, true)
	requireUROCapableHost(t, probe)
	endpoint, acquire := newTestManagedEndpoint(t)
	lease, err := acquire()
	require.NoError(t, err)
	t.Cleanup(func() { lease.Close() })
	tr := &Transport{Conn: lease}
	require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil))
	udp := endpoint.(*managedPacketConn).endpoint.conn.(*net.UDPConn)
	require.Equal(t, protocol.MaxCoalescedPacketBufferSize, uroSocketOption(t, udp))
	require.NoError(t, lease.Close())
	peer := listenExternalUDP(t)
	_, err = peer.WriteTo([]byte("after lease"), endpoint.LocalAddr())
	require.NoError(t, err)
	require.NoError(t, endpoint.SetReadDeadline(time.Now().Add(time.Second)))
	b := make([]byte, 1500)
	n, addr, err := endpoint.ReadFrom(b)
	require.NoError(t, err)
	require.Equal(t, "after lease", string(b[:n]))
	require.Equal(t, peer.LocalAddr().String(), addr.String())
}
