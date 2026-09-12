//go:build windows

package quic

import (
	"net"
	"os"
	"testing"

	"golang.org/x/sys/windows"

	"github.com/quic-go/quic-go/internal/protocol"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests for Slice W3 (Windows coalesced receive, URO): the
// UDP_RECV_MAX_COALESCED_SIZE capability probe on transport-owned sockets,
// UDP_COALESCED_INFO parsing, and the split of a coalesced read into
// per-datagram slab views through the shared coalesced-storage contract.

// requireUROCapableHost enforces the closure evidence's host posture: on a
// hosted CI runner (GITHUB_ACTIONS set) the qualified Windows surface is
// URO-capable, so a probe reporting no capability is a real failure — never
// a vacuous skip that would silently drop the URO closure classes. Off CI, a
// probe-false host is legitimate (an older Windows build) and the
// URO-dependent test skips loudly. The probe's own failure branches are
// covered deterministically by TestWindowsUROProbeFailure.
func requireUROCapableHost(t *testing.T, conn rawConn) {
	t.Helper()
	if conn.capabilities().GRO {
		return
	}
	if os.Getenv("GITHUB_ACTIONS") != "" {
		t.Fatal("URO probe reported unsupported on a hosted CI runner; the qualified Windows surface must exercise the URO closure classes")
	}
	t.Skip("URO probe reported unsupported on this host; the URO-capable CI matrix asserts this capability strictly")
}

// uroSocketOption reads UDP_RECV_MAX_COALESCED_SIZE back from the socket,
// reporting the maximum coalesced message size Winsock has enabled (0 when
// receive coalescing is off).
func uroSocketOption(t *testing.T, udpConn *net.UDPConn) int {
	t.Helper()
	rawConn, err := udpConn.SyscallConn()
	require.NoError(t, err)
	var val int
	var serr error
	require.NoError(t, rawConn.Control(func(fd uintptr) {
		val, serr = windows.GetsockoptInt(windows.Handle(fd), windows.IPPROTO_UDP, windows.UDP_RECV_MAX_COALESCED_SIZE)
	}))
	require.NoError(t, serr)
	return val
}

func newWindowsOwnedConn(t *testing.T, ownsSocket bool) (*windowsConn, *net.UDPConn) {
	t.Helper()
	udpConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	require.NoError(t, err)
	t.Cleanup(func() { udpConn.Close() })
	conn, err := newConn(udpConn, true, ownsSocket)
	require.NoError(t, err)
	return conn, udpConn
}

// The probe enables receive coalescing on transport-owned sockets up to the
// coalesced buffer tier's capacity, so a kernel-coalesced read is never
// truncated.
func TestWindowsUROProbeOnTransportOwnedSocket(t *testing.T) {
	conn, udpConn := newWindowsOwnedConn(t, true)
	requireUROCapableHost(t, conn)
	require.True(t, conn.capabilities().GRO)
	require.Equal(t, protocol.MaxCoalescedPacketBufferSize, uroSocketOption(t, udpConn))
}

// The transport must never issue the UDP_RECV_MAX_COALESCED_SIZE setsockopt
// on a caller-supplied socket: coalescing is socket-wide, and reads the
// caller performs after transport close must not see silently truncated
// coalesced payloads (the acceptance criteria's negative criterion).
func TestWindowsURONotEnabledOnCallerSuppliedSocket(t *testing.T) {
	conn, udpConn := newWindowsOwnedConn(t, false)
	require.False(t, conn.capabilities().GRO)
	require.Equal(t, 0, uroSocketOption(t, udpConn))
}

// Kill-switch closure class: QUIC_GO_DISABLE_GRO governs coalesced receive
// on Windows exactly as it does on Linux, and with the switch set the
// socket option is never issued, keeping the W1/W2 receive path byte for
// byte.
func TestWindowsURODisabledByEnv(t *testing.T) {
	t.Setenv("QUIC_GO_DISABLE_GRO", "1")
	conn, udpConn := newWindowsOwnedConn(t, true)
	require.False(t, conn.capabilities().GRO)
	require.Equal(t, 0, uroSocketOption(t, udpConn))
}

// Probe-failure closure class: a probe that cannot interrogate the socket —
// the Control call errors, or the UDP_RECV_MAX_COALESCED_SIZE setsockopt is
// rejected (a Windows build without URO) — must report no capability,
// leaving the receive path on the W1/W2 foundation behavior.
func TestWindowsUROProbeFailure(t *testing.T) {
	t.Run("control error", func(t *testing.T) {
		require.False(t, isUROEnabled(&probeFailingRawConn{controlErr: assert.AnError}))
	})
	t.Run("setsockopt error", func(t *testing.T) {
		require.False(t, isUROEnabled(&probeFailingRawConn{}))
	})
}
