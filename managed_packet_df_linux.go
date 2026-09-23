package quic

import "golang.org/x/sys/unix"

func (s managedDFNativeSocket) options() ([]managedDFOption, error) {
	addr, err := unix.Getsockname(s.fd)
	if err != nil {
		return nil, err
	}
	ipv4 := managedDFOption{unix.IPPROTO_IP, unix.IP_MTU_DISCOVER, unix.IP_PMTUDISC_PROBE}
	ipv6 := managedDFOption{unix.IPPROTO_IPV6, unix.IPV6_MTU_DISCOVER, unix.IPV6_PMTUDISC_PROBE}
	switch addr.(type) {
	case *unix.SockaddrInet4:
		return []managedDFOption{ipv4}, nil
	case *unix.SockaddrInet6:
		only, err := unix.GetsockoptInt(s.fd, unix.IPPROTO_IPV6, unix.IPV6_V6ONLY)
		if err != nil {
			return nil, err
		}
		if only != 0 {
			return []managedDFOption{ipv6}, nil
		}
		return []managedDFOption{ipv4, ipv6}, nil
	default:
		return nil, nil
	}
}
