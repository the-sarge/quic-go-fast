//go:build windows

package quic

import (
	"encoding/binary"
	"net"
	"net/netip"
	"os"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/quic-go/quic-go/internal/protocol"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests for Slice W2 (Windows segmented send, USO): the UDP_SEND_MSG_SIZE
// capability probe on transport-owned sockets, the per-send segment-size
// control message, and the send-error classification the MTU discovery
// feedback path depends on.

// requireUSOCapableHost enforces the closure evidence's host posture: on a
// hosted CI runner (GITHUB_ACTIONS set) the qualified Windows surface is
// USO-capable, so a probe reporting no capability is a real failure — never
// a vacuous skip that would silently drop the USO closure classes. Off CI, a
// probe-false host is legitimate (an older Windows build) and the
// USO-dependent test skips loudly. The probe's own failure branches are
// covered deterministically by TestWindowsUSOProbeFailure.
func requireUSOCapableHost(t *testing.T, conn rawConn) {
	t.Helper()
	if conn.capabilities().GSO {
		return
	}
	if os.Getenv("GITHUB_ACTIONS") != "" {
		t.Fatal("USO probe reported unsupported on a hosted CI runner; the qualified Windows surface must exercise the USO closure classes")
	}
	t.Skip("USO probe reported unsupported on this host; the USO-capable CI matrix asserts this capability strictly")
}

// The probe runs only on transport-owned sockets and honors the existing
// QUIC_GO_DISABLE_GSO kill switch, mirroring the Linux isGSOEnabled probe.
func TestWindowsConnUSOCapability(t *testing.T) {
	newOwnedConn := func(t *testing.T, ownsSocket bool) *windowsConn {
		t.Helper()
		udpConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
		require.NoError(t, err)
		t.Cleanup(func() { udpConn.Close() })
		conn, err := newConn(udpConn, true, ownsSocket)
		require.NoError(t, err)
		return conn
	}

	t.Run("transport-owned socket", func(t *testing.T) {
		conn := newOwnedConn(t, true)
		requireUSOCapableHost(t, conn)
		require.True(t, conn.capabilities().GSO)
	})

	t.Run("caller-supplied socket", func(t *testing.T) {
		conn := newOwnedConn(t, false)
		require.False(t, conn.capabilities().GSO)
	})

	t.Run("kill switch", func(t *testing.T) {
		t.Setenv("QUIC_GO_DISABLE_GSO", "1")
		conn := newOwnedConn(t, true)
		require.False(t, conn.capabilities().GSO)
	})
}

// oobRecordingConn records the control buffer of every WriteMsgUDP call
// without putting it on the wire, so a test can inspect the exact bytes the
// conn hands to WSASendMsg.
type oobRecordingConn struct {
	*net.UDPConn
	oobs [][]byte
}

func (c *oobRecordingConn) WriteMsgUDP(b, oob []byte, addr *net.UDPAddr) (int, int, error) {
	c.oobs = append(c.oobs, append([]byte(nil), oob...))
	return len(b), len(oob), nil
}

// A segmented send must carry exactly the documented UDP_SEND_MSG_SIZE
// control message (ws2def.h WSACMSGHDR: SIZE_T cmsg_len, INT cmsg_level, INT
// cmsg_type, data at WSA_CMSGDATA_ALIGN(sizeof(WSACMSGHDR)); payload one
// DWORD segment size), compared against a hand-built layout literal
// independent of the encoder under test.
func TestWindowsConnSegmentedSendEncoding(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("layout literal assumes a 64-bit SIZE_T")
	}
	segmentSizeMsg := func(size uint32) []byte {
		buf := make([]byte, 24)                      // WSA_CMSG_SPACE(4): 16-byte header + 4-byte DWORD, padded to 8
		binary.LittleEndian.PutUint64(buf[0:8], 20)  // cmsg_len: WSA_CMSG_LEN(4), unpadded
		binary.LittleEndian.PutUint32(buf[8:12], 17) // cmsg_level: IPPROTO_UDP
		binary.LittleEndian.PutUint32(buf[12:16], windows.UDP_SEND_MSG_SIZE)
		binary.LittleEndian.PutUint32(buf[16:20], size)
		return buf
	}

	newRecordingConn := func(t *testing.T) (*windowsConn, *oobRecordingConn) {
		t.Helper()
		udpConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
		require.NoError(t, err)
		t.Cleanup(func() { udpConn.Close() })
		rec := &oobRecordingConn{UDPConn: udpConn}
		return &windowsConn{
			OOBCapablePacketConn: rec,
			oobBuffer:            make([]byte, oobBufferSize),
			cap:                  connCapabilities{DF: true, GSO: true},
		}, rec
	}

	dest := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}

	t.Run("segment size only", func(t *testing.T) {
		conn, rec := newRecordingConn(t)
		_, err := conn.WritePacket([]byte("foobar"), dest, nil, 1200, protocol.ECNUnsupported)
		require.NoError(t, err)
		require.Len(t, rec.oobs, 1)
		require.Equal(t, segmentSizeMsg(1200), rec.oobs[0])
	})

	t.Run("appended after packet info", func(t *testing.T) {
		conn, rec := newRecordingConn(t)
		info := packetInfo{addr: netip.MustParseAddr("10.11.12.13"), ifIndex: 3}
		packetInfoOOB := info.OOB()
		require.NotEmpty(t, packetInfoOOB)
		_, err := conn.WritePacket([]byte("foobar"), dest, packetInfoOOB, 1350, protocol.ECNUnsupported)
		require.NoError(t, err)
		require.Len(t, rec.oobs, 1)
		require.Equal(t, append(append([]byte(nil), packetInfoOOB...), segmentSizeMsg(1350)...), rec.oobs[0])
		require.Equal(t, info, parsePacketInfo(packetInfoOOB), "packet info prefix must survive the append")
	})

	t.Run("no control message without GSO", func(t *testing.T) {
		conn, rec := newRecordingConn(t)
		_, err := conn.WritePacket([]byte("foobar"), dest, nil, 0, protocol.ECNUnsupported)
		require.NoError(t, err)
		require.Len(t, rec.oobs, 1)
		require.Empty(t, rec.oobs[0])
	})
}

// Full-acceptance closure class: a segmented submission is delivered as
// individual datagrams of the requested segment size, the last one carrying
// the remainder, with payload bytes preserved in order.
func TestWindowsConnSegmentedSend(t *testing.T) {
	udpConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	require.NoError(t, err)
	defer udpConn.Close()
	conn, err := newConn(udpConn, true, true)
	require.NoError(t, err)
	requireUSOCapableHost(t, conn)

	receiver, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	require.NoError(t, err)
	defer receiver.Close()

	const segmentSize = 500
	payload := make([]byte, 2*segmentSize+250) // two full segments plus a short tail
	for i := range payload {
		payload[i] = byte(i)
	}
	n, err := conn.WritePacket(payload, receiver.LocalAddr(), nil, segmentSize, protocol.ECNUnsupported)
	require.NoError(t, err)
	// Winsock reports zero transferred bytes on UDP_SEND_MSG_SIZE
	// completions; the conn normalizes a successful segmented send to the
	// full buffer length.
	require.Equal(t, len(payload), n)

	var received []byte
	var sizes []int
	buf := make([]byte, protocol.MaxPacketBufferSize)
	for len(received) < len(payload) {
		require.NoError(t, receiver.SetReadDeadline(time.Now().Add(scaleDuration(5*time.Second))))
		n, _, err := receiver.ReadFromUDP(buf)
		require.NoError(t, err)
		received = append(received, buf[:n]...)
		sizes = append(sizes, n)
	}
	require.Equal(t, payload, received)
	require.Equal(t, []int{segmentSize, segmentSize, 250}, sizes)

	// A GSO-mode submission always carries the segment-size message, even
	// when it holds a single packet smaller than the segment size (the
	// platform-neutral emission path sets gsoSize to the max packet size for
	// every batch); such a send must arrive as exactly one datagram.
	single := payload[:300]
	n, err = conn.WritePacket(single, receiver.LocalAddr(), nil, segmentSize, protocol.ECNUnsupported)
	require.NoError(t, err)
	require.Equal(t, len(single), n)
	require.NoError(t, receiver.SetReadDeadline(time.Now().Add(scaleDuration(5*time.Second))))
	n, _, err = receiver.ReadFromUDP(buf)
	require.NoError(t, err)
	require.Equal(t, single, buf[:n])
}

// Message-size closure class: the error a too-large send surfaces through
// the message-I/O path must satisfy isSendMsgSizeErr, because the send queue
// uses that classification to keep MTU-probe failures nonfatal and feed
// DPLPMTUD (send_queue.go). The platform-neutral queue behavior is covered
// by the send-queue suite; this pins the Windows error wrapping end to end.
func TestWindowsConnSendMsgSizeErrClassification(t *testing.T) {
	udpConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	require.NoError(t, err)
	defer udpConn.Close()
	conn, err := newConn(udpConn, true, true)
	require.NoError(t, err)

	dest := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: udpConn.LocalAddr().(*net.UDPAddr).Port}
	oversized := make([]byte, 70000) // above the 65535-byte UDP maximum
	_, err = conn.WritePacket(oversized, dest, nil, 0, protocol.ECNUnsupported)
	require.Error(t, err)
	require.True(t, isSendMsgSizeErr(err), "a too-large send must classify as a message-size error: %v", err)
	require.False(t, isGSOError(err))
}

// probeFailingRawConn drives isUSOEnabled's failure branches
// deterministically: a Control that itself errors, or a Control that hands
// the probe an invalid socket handle so the getsockopt fails.
type probeFailingRawConn struct {
	controlErr    error
	controlCalled bool
}

func (c *probeFailingRawConn) Control(f func(fd uintptr)) error {
	c.controlCalled = true
	if c.controlErr != nil {
		return c.controlErr
	}
	f(uintptr(windows.InvalidHandle))
	return nil
}

func (c *probeFailingRawConn) Read(func(fd uintptr) bool) error  { return nil }
func (c *probeFailingRawConn) Write(func(fd uintptr) bool) error { return nil }

// Probe-failure closure class: a probe that cannot interrogate the socket —
// the Control call errors, or the UDP_SEND_MSG_SIZE getsockopt is rejected
// (a Windows build without USO) — must report no capability, leaving the
// send path on the W1 foundation behavior.
func TestWindowsUSOProbeFailure(t *testing.T) {
	// Pin the kill switch off: an ambient QUIC_GO_DISABLE_GSO=1 would make
	// the probe return before Control, leaving both branches unexercised.
	t.Setenv("QUIC_GO_DISABLE_GSO", "")
	t.Run("control error", func(t *testing.T) {
		conn := &probeFailingRawConn{controlErr: assert.AnError}
		require.False(t, isUSOEnabled(conn))
		require.True(t, conn.controlCalled, "the probe must reach Control")
	})
	t.Run("getsockopt error", func(t *testing.T) {
		conn := &probeFailingRawConn{}
		require.False(t, isUSOEnabled(conn))
		require.True(t, conn.controlCalled, "the probe must reach Control")
	})
}

// Kill-switch closure class (a failed probe clears the capability the same
// way, so the downstream behavior is shared): without the GSO capability a
// segmented write is a caller bug (the platform-neutral contract in
// sys_conn.go), and unsegmented writes keep the W1 foundation behavior.
func TestWindowsConnSegmentedSendRequiresCapability(t *testing.T) {
	t.Setenv("QUIC_GO_DISABLE_GSO", "1")
	udpConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	require.NoError(t, err)
	defer udpConn.Close()
	conn, err := newConn(udpConn, true, true)
	require.NoError(t, err)
	require.False(t, conn.capabilities().GSO)
	require.Panics(t, func() {
		_, _ = conn.WritePacket([]byte("foobar"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}, nil, 1200, protocol.ECNUnsupported)
	})
}
