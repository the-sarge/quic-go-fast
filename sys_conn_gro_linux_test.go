//go:build linux

package quic

import (
	"net"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/stretchr/testify/require"
)

// groSocketOption reads the UDP_GRO socket option, reporting whether the
// kernel has GRO enabled on the socket.
func groSocketOption(t *testing.T, udpConn *net.UDPConn) int {
	t.Helper()
	rawConn, err := udpConn.SyscallConn()
	require.NoError(t, err)
	var val int
	var serr error
	require.NoError(t, rawConn.Control(func(fd uintptr) {
		val, serr = unix.GetsockoptInt(int(fd), unix.IPPROTO_UDP, unix.UDP_GRO)
	}))
	require.NoError(t, serr)
	return val
}

func TestGROProbeOnTransportOwnedSocket(t *testing.T) {
	if kernelVersionMajor < 5 {
		t.Skip("UDP_GRO requires kernel 5+")
	}
	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	defer udpConn.Close()

	c, err := newConn(udpConn, true, true)
	require.NoError(t, err)
	require.True(t, c.capabilities().GRO)
	require.Equal(t, 1, groSocketOption(t, udpConn))
}

// The transport must never issue a UDP_GRO setsockopt on a caller-supplied
// socket: coalescing is socket-wide, and reads the caller performs after
// transport close must not see silently truncated coalesced payloads.
func TestGRONotEnabledOnCallerSuppliedSocket(t *testing.T) {
	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	defer udpConn.Close()

	c, err := newConn(udpConn, true, false)
	require.NoError(t, err)
	require.False(t, c.capabilities().GRO)
	require.Equal(t, 0, groSocketOption(t, udpConn))
}

func TestGRODisabledByEnv(t *testing.T) {
	t.Setenv("QUIC_GO_DISABLE_GRO", "1")
	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	defer udpConn.Close()

	c, err := newConn(udpConn, true, true)
	require.NoError(t, err)
	require.False(t, c.capabilities().GRO)
	require.Equal(t, 0, groSocketOption(t, udpConn))
}
