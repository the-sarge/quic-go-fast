//go:build darwin && !ios && !quic_go_no_private_syscalls

package quic

import (
	"net"
	"syscall"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

// TestSendmsgXMsghdrXLayout pins msghdrX byte-exact to XNU struct msghdr_x:
// the 4-byte pads after msg_namelen and msg_iovlen are the ABI trap. The
// expected offsets come from the XNU definition (bsd/sys/socket_private.h),
// independently recorded in docs/audits/2026-09-11-keibisoft-gso-discovery.md.
func TestSendmsgXMsghdrXLayout(t *testing.T) {
	var m msghdrX
	require.Equal(t, uintptr(56), unsafe.Sizeof(m), "struct msghdr_x is 56 bytes on LP64 darwin")
	require.Equal(t, uintptr(0), unsafe.Offsetof(m.Name))
	require.Equal(t, uintptr(8), unsafe.Offsetof(m.Namelen))
	require.Equal(t, uintptr(16), unsafe.Offsetof(m.Iov))
	require.Equal(t, uintptr(24), unsafe.Offsetof(m.Iovlen))
	require.Equal(t, uintptr(32), unsafe.Offsetof(m.Control))
	require.Equal(t, uintptr(40), unsafe.Offsetof(m.Controllen))
	require.Equal(t, uintptr(44), unsafe.Offsetof(m.Flags))
	require.Equal(t, uintptr(48), unsafe.Offsetof(m.Datalen))
}

// TestSendmsgXDestSockaddr pins the destination sockaddr encodings the
// dual-stack production socket requires: a v4-mapped sockaddr_in6 for IPv4
// destinations on an AF_INET6 socket, a native sockaddr_in6 for IPv6
// destinations, and a sockaddr_in only on a genuine AF_INET socket.
func TestSendmsgXDestSockaddr(t *testing.T) {
	t.Run("v4-mapped on dual-stack socket", func(t *testing.T) {
		d := newSendmsgXDest(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0x1234}, syscall.AF_INET6)
		require.True(t, d.isV6)
		require.EqualValues(t, syscall.SizeofSockaddrInet6, d.namelen)
		require.EqualValues(t, syscall.AF_INET6, d.sa6.Family)
		require.Equal(t, [16]byte{10: 0xff, 11: 0xff, 12: 127, 13: 0, 14: 0, 15: 1}, d.sa6.Addr)
		require.Equal(t, [2]byte{0x12, 0x34}, *(*[2]byte)(unsafe.Pointer(&d.sa6.Port)), "port must be network byte order")
	})
	t.Run("native v6", func(t *testing.T) {
		d := newSendmsgXDest(&net.UDPAddr{IP: net.ParseIP("::1"), Port: 443}, syscall.AF_INET6)
		require.True(t, d.isV6)
		require.Equal(t, [16]byte{15: 1}, d.sa6.Addr)
	})
	t.Run("v4 socket", func(t *testing.T) {
		d := newSendmsgXDest(&net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 443}, syscall.AF_INET)
		require.False(t, d.isV6)
		require.EqualValues(t, syscall.SizeofSockaddrInet4, d.namelen)
		require.Equal(t, [4]byte{10, 0, 0, 1}, d.sa4.Addr)
	})
}
