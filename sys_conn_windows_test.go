//go:build windows

package quic

import (
	"encoding/binary"
	"net"
	"net/netip"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/quic-go/quic-go/internal/protocol"

	"github.com/stretchr/testify/require"
)

func TestWindowsConn(t *testing.T) {
	t.Run("IPv4", func(t *testing.T) {
		udpConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
		require.NoError(t, err)
		conn, err := newConn(udpConn, true, false)
		require.NoError(t, err)
		require.True(t, conn.capabilities().DF)
		require.False(t, conn.capabilities().GSO)
		require.False(t, conn.capabilities().ECN)
		require.False(t, conn.capabilities().GRO)
		require.NoError(t, conn.Close())
	})

	t.Run("IPv6", func(t *testing.T) {
		udpConn, err := net.ListenUDP("udp6", &net.UDPAddr{IP: net.IPv6loopback, Port: 0})
		require.NoError(t, err)
		conn, err := newConn(udpConn, false, false)
		require.NoError(t, err)
		require.False(t, conn.capabilities().DF)
		require.NoError(t, conn.Close())
	})
}

// TestWindowsConnPacketInfo exercises packet-info parity with the OOB path:
// a socket bound to an unspecified address learns the local address every
// packet arrived on, and the reply path routes through that address via the
// control message packetInfo.OOB() encodes.
func TestWindowsConnPacketInfo(t *testing.T) {
	tests := []struct {
		name       string
		network    string
		listenAddr *net.UDPAddr
		dialNet    string
		dialIP     net.IP
		wantAddr   netip.Addr
	}{
		{
			name:       "IPv4",
			network:    "udp4",
			listenAddr: &net.UDPAddr{IP: net.IPv4zero, Port: 0},
			dialNet:    "udp4",
			dialIP:     net.IPv4(127, 0, 0, 1),
			wantAddr:   netip.MustParseAddr("127.0.0.1"),
		},
		{
			name:       "IPv6",
			network:    "udp6",
			listenAddr: &net.UDPAddr{IP: net.IPv6unspecified, Port: 0},
			dialNet:    "udp6",
			dialIP:     net.IPv6loopback,
			wantAddr:   netip.MustParseAddr("::1"),
		},
		{
			name:       "dual-stack with IPv4 client",
			network:    "udp",
			listenAddr: &net.UDPAddr{Port: 0},
			dialNet:    "udp4",
			dialIP:     net.IPv4(127, 0, 0, 1),
			wantAddr:   netip.MustParseAddr("127.0.0.1"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			udpConn, err := net.ListenUDP(tt.network, tt.listenAddr)
			require.NoError(t, err)
			conn, err := wrapConn(udpConn, true)
			require.NoError(t, err)
			defer conn.Close()
			port := udpConn.LocalAddr().(*net.UDPAddr).Port

			client, err := net.DialUDP(tt.dialNet, nil, &net.UDPAddr{IP: tt.dialIP, Port: port})
			require.NoError(t, err)
			defer client.Close()
			_, err = client.Write([]byte("request"))
			require.NoError(t, err)

			require.NoError(t, conn.SetReadDeadline(time.Now().Add(scaleDuration(5*time.Second))))
			p, err := conn.ReadPacket()
			require.NoError(t, err)
			defer p.buffer.Release()
			require.Equal(t, []byte("request"), p.data)
			require.Equal(t, tt.wantAddr, p.info.addr)

			// Reply through the address the request arrived on.
			oob := p.info.OOB()
			require.NotEmpty(t, oob)
			_, err = conn.WritePacket([]byte("reply"), p.remoteAddr, oob, 0, protocol.ECNUnsupported)
			require.NoError(t, err)

			require.NoError(t, client.SetReadDeadline(time.Now().Add(scaleDuration(5*time.Second))))
			reply := make([]byte, 100)
			n, err := client.Read(reply)
			require.NoError(t, err)
			require.Equal(t, []byte("reply"), reply[:n])
		})
	}
}

func TestWindowsPacketInfoRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		info packetInfo
	}{
		{name: "IPv4", info: packetInfo{addr: netip.MustParseAddr("1.2.3.4"), ifIndex: 7}},
		{name: "IPv6", info: packetInfo{addr: netip.MustParseAddr("2001:db8::1"), ifIndex: 9, pktinfoV6: true}},
		{name: "IPv4 on dual-stack socket", info: packetInfo{addr: netip.MustParseAddr("127.0.0.1"), ifIndex: 1, pktinfoV6: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oob := tt.info.OOB()
			require.NotEmpty(t, oob)
			parsed := parsePacketInfo(oob)
			require.Equal(t, tt.info, parsed)
		})
	}
}

// TestWindowsPacketInfoParseLiteral parses a control buffer built by hand
// from the documented WSACMSGHDR/IN_PKTINFO layout (ws2def.h: SIZE_T
// cmsg_len, INT cmsg_level, INT cmsg_type, data at
// WSA_CMSGDATA_ALIGN(sizeof(WSACMSGHDR)); IN_PKTINFO: IN_ADDR then ULONG
// interface index), independent of the encoder under test.
func TestWindowsPacketInfoParseLiteral(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("layout literal assumes a 64-bit SIZE_T")
	}
	buf := make([]byte, 24)
	binary.LittleEndian.PutUint64(buf[0:8], 24)                   // cmsg_len: 16-byte header + 8-byte IN_PKTINFO
	binary.LittleEndian.PutUint32(buf[8:12], 0)                   // cmsg_level: IPPROTO_IP
	binary.LittleEndian.PutUint32(buf[12:16], windows.IP_PKTINFO) // cmsg_type
	copy(buf[16:20], []byte{10, 11, 12, 13})                      // ipi_addr
	binary.LittleEndian.PutUint32(buf[20:24], 3)                  // ipi_ifindex
	parsed := parsePacketInfo(buf)
	require.Equal(t, netip.MustParseAddr("10.11.12.13"), parsed.addr)
	require.Equal(t, uint32(3), parsed.ifIndex)
	require.False(t, parsed.pktinfoV6)
}

// The replaced basicConn surfaced a nil or non-UDP destination address as a
// socket error; the message-I/O conn must preserve that behavior rather than
// panic on its address assertion.
func TestWindowsConnWriteInvalidAddr(t *testing.T) {
	newWindowsConn := func(t *testing.T) rawConn {
		t.Helper()
		udpConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
		require.NoError(t, err)
		t.Cleanup(func() { udpConn.Close() })
		conn, err := newConn(udpConn, true, false)
		require.NoError(t, err)
		return conn
	}
	newBasicConn := func(t *testing.T) rawConn {
		t.Helper()
		udpConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
		require.NoError(t, err)
		t.Cleanup(func() { udpConn.Close() })
		conn, err := wrapConn(&nonOOBPacketConn{PacketConn: udpConn}, false)
		require.NoError(t, err)
		return conn
	}
	for _, tt := range []struct {
		name string
		conn func(*testing.T) rawConn
	}{
		{name: "windowsConn", conn: newWindowsConn},
		{name: "basicConn", conn: newBasicConn},
	} {
		t.Run(tt.name, func(t *testing.T) {
			conn := tt.conn(t)
			_, err := conn.WritePacket([]byte("foobar"), nil, nil, 0, protocol.ECNUnsupported)
			require.Error(t, err)
			_, err = conn.WritePacket([]byte("foobar"), &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}, nil, 0, protocol.ECNUnsupported)
			require.Error(t, err)
		})
	}
}

// A structurally invalid control buffer must not fail the read: packet info
// falls back to absent, matching the OOB path's fallback-when-absent rule.
func TestWindowsPacketInfoMalformed(t *testing.T) {
	for _, buf := range [][]byte{
		{0x01},
		{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0, 0, 0, 0, 0, 0, 0, 0}, // header claims impossible length
		make([]byte, 64), // zero-length cmsg
	} {
		parsed := parsePacketInfo(buf)
		require.Equal(t, packetInfo{}, parsed)
	}
}
