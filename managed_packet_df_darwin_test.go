//go:build darwin && !ios

package quic

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestManagedDFDarwinLeaseRestoresNativeOptions(t *testing.T) {
	for _, network := range []string{"udp4", "udp6", "udp"} {
		t.Run(network, func(t *testing.T) {
			endpoint, acquire, err := (&Transport{}).NewManagedPacketEndpointV1(network, nil)
			require.NoError(t, err)
			defer endpoint.Close()
			socket := endpoint.(*managedPacketConn).endpoint.conn.(*net.UDPConn)
			raw, err := socket.SyscallConn()
			require.NoError(t, err)
			level, option := unix.IPPROTO_IP, unix.IP_DONTFRAG
			if socket.LocalAddr().(*net.UDPAddr).IP.To4() == nil {
				level, option = unix.IPPROTO_IPV6, unix.IPV6_DONTFRAG
			}
			read := func() int {
				var value int
				require.NoError(t, raw.Control(func(fd uintptr) {
					var err error
					value, err = unix.GetsockoptInt(int(fd), level, option)
					require.NoError(t, err)
				}))
				return value
			}
			before := read()
			for range 2 {
				lease, err := acquire()
				require.NoError(t, err)
				tr := &Transport{Conn: lease}
				require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil))
				require.Equal(t, 1, read(), "registration engages native DF")
				base, err := wrapConnWithManagedBuffers(lease, false, tr.managedBuffers())
				require.NoError(t, err)
				conn := tr.wrapExternalPacketIO(base)
				require.True(t, conn.capabilities().DF)
				require.NoError(t, lease.Close())
				require.Equal(t, before, read(), "release restores the saved option")
				require.False(t, conn.capabilities().DF, "released generations cannot advertise DF")
			}
		})
	}
}
