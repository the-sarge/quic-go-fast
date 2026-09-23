//go:build darwin && !ios

package quic

import "golang.org/x/sys/unix"

func (s managedDFNativeSocket) options() ([]managedDFOption, error) {
	version, err := getMacOSVersion()
	if err != nil {
		return nil, err
	}
	if version < macOSVersion11 {
		return nil, nil
	}
	addr, err := unix.Getsockname(s.fd)
	if err != nil {
		return nil, err
	}
	switch addr.(type) {
	case *unix.SockaddrInet4:
		return []managedDFOption{{unix.IPPROTO_IP, unix.IP_DONTFRAG, 1}}, nil
	case *unix.SockaddrInet6:
		only, err := unix.GetsockoptInt(s.fd, unix.IPPROTO_IPV6, unix.IPV6_V6ONLY)
		if err != nil {
			return nil, err
		}
		if only == 0 && version < macOSVersion15 {
			return nil, nil
		}
		// Darwin's IPv6 option controls mapped IPv4 on dual-stack sockets too.
		return []managedDFOption{{unix.IPPROTO_IPV6, unix.IPV6_DONTFRAG, 1}}, nil
	default:
		return nil, nil
	}
}
