//go:build darwin && !ios && !quic_go_no_private_syscalls

package quic

import (
	"net"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
)

// skipUnlessQualifiedKernel skips loopback tests that invoke the real
// private syscall on hosts whose Darwin kernel major is not allowlisted —
// the same posture the D1 self-check test takes: unlisted majors never
// activate the path in production, so they are not asserted against it.
func skipUnlessQualifiedKernel(t *testing.T) {
	t.Helper()
	major, err := getMacOSVersion()
	require.NoError(t, err)
	if _, ok := qualifiedDarwinKernelMajors[major]; !ok {
		t.Skipf("running Darwin kernel major %d is not in the qualified set", major)
	}
}

func newLoopbackOOBConn(t *testing.T) (*oobConn, *net.UDPAddr) {
	t.Helper()
	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv6loopback})
	require.NoError(t, err)
	t.Cleanup(func() { udpConn.Close() })
	oc, err := newConn(udpConn, true, true)
	require.NoError(t, err)
	return oc, udpConn.LocalAddr().(*net.UDPAddr)
}

// newConn on a transport-created darwin socket must install the recvmsg_x
// wrapper at the batchConn seam; a caller-provided batchConn implementation
// owns its batching and must pass through unwrapped.
func TestOOBConnInstallsRecvmsgXWrapper(t *testing.T) {
	resetRecvmsgXForTesting(t)
	oc, _ := newLoopbackOOBConn(t)
	require.IsType(t, &recvmsgXConn{}, oc.batchConn)
}

// With the capability off, a read must engage exactly one message slot:
// one pool buffer pulled, one message offered to the fallback path. The
// batchSize-1 read behavior — and its memory shape — is preserved
// byte-identically.
func TestOOBConnCapabilityOffReadsSingle(t *testing.T) {
	resetRecvmsgXForTesting(t)
	t.Setenv(recvmsgXDisableEnv, "true")
	recvmsgXEnsureQualified()
	oc, addr := newLoopbackOOBConn(t)

	sender, err := net.DialUDP("udp", nil, addr)
	require.NoError(t, err)
	defer sender.Close()
	_, err = sender.Write([]byte("solo"))
	require.NoError(t, err)

	p, err := oc.ReadPacket()
	require.NoError(t, err)
	require.Equal(t, "solo", string(p.data))
	for i := 1; i < batchSize; i++ {
		require.Nil(t, oc.buffers[i], "buffer slot %d must stay untouched while the capability is off", i)
	}
	wrapper := oc.batchConn.(*recvmsgXConn)
	require.Zero(t, wrapper.batchReads.Load())
	require.NotZero(t, wrapper.fallbackReads.Load())
}

// With the capability engaged, queued datagrams drain through batched
// recvmsg_x reads: every datagram surfaces exactly once with its own
// payload, its own ECN mark, and a source address identical to the
// fallback path's representation (an address-shape change on mid-connection
// engagement would masquerade as a path migration).
func TestOOBConnEngagedBatchRead(t *testing.T) {
	skipUnlessQualifiedKernel(t)
	resetRecvmsgXForTesting(t)
	oc, addr := newLoopbackOOBConn(t)

	sender, err := net.DialUDP("udp", nil, addr)
	require.NoError(t, err)
	defer sender.Close()
	rawSender, err := sender.SyscallConn()
	require.NoError(t, err)
	var optErr error
	require.NoError(t, rawSender.Control(func(fd uintptr) {
		optErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_TCLASS, 0x2 /* ECT(0) */)
	}))
	require.NoError(t, optErr)

	// One capability-off read pins the fallback path's address
	// representation for this sender.
	_, err = sender.Write([]byte("addr-probe"))
	require.NoError(t, err)
	probe, err := oc.ReadPacket()
	require.NoError(t, err)
	require.Equal(t, "addr-probe", string(probe.data))
	fallbackAddr := probe.remoteAddr.String()

	recvmsgXEnsureQualified()
	require.True(t, recvmsgXAvailable(), "the qualified host must engage after the self-check")

	const datagrams = 5
	want := map[string]bool{}
	for i := range datagrams {
		payload := string(rune('A' + i))
		want[payload] = true
		_, err = sender.Write([]byte(payload))
		require.NoError(t, err)
	}
	// Loopback delivery is asynchronous; wait until the receive buffer holds
	// every datagram so the first refill can drain them in one batch.
	rawRecv, err := oc.OOBCapablePacketConn.(*net.UDPConn).SyscallConn()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		var queued int
		require.NoError(t, rawRecv.Control(func(fd uintptr) {
			// FIONREAD (<sys/filio.h>): bytes queued in the receive buffer.
			// Neither syscall nor x/sys/unix exports it on darwin.
			const fionread = 0x4004667f
			queued, _ = unix.IoctlGetInt(int(fd), fionread)
		}))
		return queued >= datagrams
	}, 2*time.Second, time.Millisecond)

	wrapper := oc.batchConn.(*recvmsgXConn)
	readsBefore := wrapper.batchReads.Load()
	for range datagrams {
		p, err := oc.ReadPacket()
		require.NoError(t, err)
		payload := string(p.data)
		require.True(t, want[payload], "unexpected or duplicated payload %q", payload)
		delete(want, payload)
		require.Equal(t, protocol.ECT0, p.ecn, "each message must carry its own ECN mark")
		require.Equal(t, fallbackAddr, p.remoteAddr.String(), "engaged reads must keep the fallback path's address representation")
		p.buffer.Release()
	}
	require.Empty(t, want, "every datagram must surface exactly once")
	require.Equal(t, readsBefore+1, wrapper.batchReads.Load(), "the queued datagrams must drain in one batched read")
	require.EqualValues(t, datagrams, wrapper.batchDatagrams.Load())
}
