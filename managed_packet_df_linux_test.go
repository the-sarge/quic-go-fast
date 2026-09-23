package quic

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestManagedDFLinuxLeaseRestoresNativeOptions(t *testing.T) {
	for _, network := range []string{"udp4", "udp6", "udp"} {
		t.Run(network, func(t *testing.T) {
			endpoint, acquire, err := (&Transport{}).NewManagedPacketEndpointV1(network, nil)
			require.NoError(t, err)
			defer endpoint.Close()
			socket := endpoint.(*managedPacketConn).endpoint.conn.(*net.UDPConn)
			raw, err := socket.SyscallConn()
			require.NoError(t, err)
			levels, options := []int{unix.IPPROTO_IP}, []int{unix.IP_MTU_DISCOVER}
			if network == "udp6" {
				levels, options = []int{unix.IPPROTO_IPV6}, []int{unix.IPV6_MTU_DISCOVER}
			}
			if network == "udp" {
				levels, options = append(levels, unix.IPPROTO_IPV6), append(options, unix.IPV6_MTU_DISCOVER)
			}
			read := func() []int {
				values := make([]int, len(levels))
				require.NoError(t, raw.Control(func(fd uintptr) {
					for i, level := range levels {
						value, err := unix.GetsockoptInt(int(fd), level, options[i])
						require.NoError(t, err)
						values[i] = value
					}
				}))
				return values
			}
			before := read()
			for range 2 {
				lease, err := acquire()
				require.NoError(t, err)
				tr := &Transport{Conn: lease}
				require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil))
				for _, value := range read() {
					require.Equal(t, unix.IP_PMTUDISC_PROBE, value)
				}
				base, err := wrapConnWithManagedBuffers(lease, false, tr.managedBuffers())
				require.NoError(t, err)
				conn := tr.wrapExternalPacketIO(base)
				require.True(t, conn.capabilities().DF)
				require.NoError(t, lease.Close())
				require.Equal(t, before, read())
				require.False(t, conn.capabilities().DF)
			}
		})
	}
}
