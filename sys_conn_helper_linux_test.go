//go:build linux

package quic

import (
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/quic-go/quic-go/internal/protocol"

	"github.com/stretchr/testify/require"
)

var (
	errGSO          = &os.SyscallError{Err: unix.EIO}
	errNotPermitted = &os.SyscallError{Syscall: "sendmsg", Err: unix.EPERM}
)

func TestForcingReceiveBufferSize(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("Must be root to force change the receive buffer size")
	}

	c, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	defer c.Close()
	syscallConn, err := c.(*net.UDPConn).SyscallConn()
	require.NoError(t, err)

	const small = 256 << 10 // 256 KB
	require.NoError(t, forceSetReceiveBuffer(syscallConn, small))

	size, err := inspectReadBuffer(syscallConn)
	require.NoError(t, err)
	// the kernel doubles this value (to allow space for bookkeeping overhead)
	require.Equal(t, 2*small, size)

	const large = 32 << 20 // 32 MB
	require.NoError(t, forceSetReceiveBuffer(syscallConn, large))
	size, err = inspectReadBuffer(syscallConn)
	require.NoError(t, err)
	// the kernel doubles this value (to allow space for bookkeeping overhead)
	require.Equal(t, 2*large, size)
}

func TestForcingSendBufferSize(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("Must be root to force change the send buffer size")
	}

	c, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	defer c.Close()
	syscallConn, err := c.(*net.UDPConn).SyscallConn()
	require.NoError(t, err)

	const small = 256 << 10 // 256 KB
	require.NoError(t, forceSetSendBuffer(syscallConn, small))

	size, err := inspectWriteBuffer(syscallConn)
	require.NoError(t, err)
	// the kernel doubles this value (to allow space for bookkeeping overhead)
	require.Equal(t, 2*small, size)

	const large = 32 << 20 // 32 MB
	require.NoError(t, forceSetSendBuffer(syscallConn, large))
	size, err = inspectWriteBuffer(syscallConn)
	require.NoError(t, err)
	// the kernel doubles this value (to allow space for bookkeeping overhead)
	require.Equal(t, 2*large, size)
}

// Equal-size batching sends segments shorter than a full packet, up to
// maxGSOSegments per send; every kernel with UDP_SEGMENT delivers that many
// intact (Linux before 6.9 rejects more than 64 with EINVAL).
func TestSysConnSendShortSegmentLimit(t *testing.T) {
	udpConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	require.NoError(t, err)
	defer udpConn.Close()
	conn, err := newConn(udpConn, true, false)
	require.NoError(t, err)
	if !conn.capabilities().GSO {
		t.Skip("GSO not available on this host")
	}
	receiver, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	require.NoError(t, err)
	defer receiver.Close()

	const segmentSize = 300
	payload := make([]byte, maxGSOSegments*segmentSize)
	for i := range payload {
		payload[i] = byte(i / segmentSize)
	}
	n, err := conn.WritePacket(payload, receiver.LocalAddr(), nil, segmentSize, protocol.ECNUnsupported)
	require.NoError(t, err)
	require.Equal(t, len(payload), n)

	buf := make([]byte, protocol.MaxPacketBufferSize)
	for i := range maxGSOSegments {
		require.NoError(t, receiver.SetReadDeadline(time.Now().Add(5*time.Second)))
		n, _, err := receiver.ReadFromUDP(buf)
		require.NoError(t, err)
		require.Equal(t, payload[i*segmentSize:(i+1)*segmentSize], buf[:n], "segment %d", i)
	}
}

func TestGSOError(t *testing.T) {
	require.True(t, isGSOError(errGSO))
	require.False(t, isGSOError(nil))
	require.False(t, isGSOError(errors.New("test")))
}
