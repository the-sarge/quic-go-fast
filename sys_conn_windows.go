//go:build windows

package quic

import (
	"encoding/binary"
	"errors"
	"log"
	"net"
	"net/netip"
	"os"
	"strconv"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
)

const oobBufferSize = 128

// windowsConn is the Windows message-I/O datapath (Windows datapath plan,
// Slices W1 and W2). Reads and writes go through
// net.UDPConn.ReadMsgUDP/WriteMsgUDP, which execute WSARecvMsg/WSASendMsg on
// the runtime's IOCP poller with standard deadline, cancellation, and close
// semantics. This conn owns Windows control-message encoding and parsing:
// IPv4/IPv6 packet info, giving sockets bound to an unspecified address the
// same authoritative local-address handling as the OOB platforms, and the
// per-send UDP_SEND_MSG_SIZE segment-size message that hands a batched send
// to USO the way UDP_SEGMENT hands one to Linux GSO.
type windowsConn struct {
	OOBCapablePacketConn

	// ReadPacket is only ever called from a single reader goroutine, so one
	// control buffer can be reused across reads. Its contents are consumed
	// (parsed into the receivedPacket) before the next read overwrites it.
	oobBuffer []byte

	cap connCapabilities
}

var _ rawConn = &windowsConn{}

// ownsSocket reports whether the transport created the socket. The W plan
// scopes offload probes to transport-owned sockets, so a caller-supplied
// socket keeps the W1 foundation behavior with no offload capability.
func newConn(c OOBCapablePacketConn, supportsDF, ownsSocket bool) (*windowsConn, error) {
	var needsPacketInfo bool
	if udpAddr, ok := c.LocalAddr().(*net.UDPAddr); ok && udpAddr.IP.IsUnspecified() {
		needsPacketInfo = true
	}
	if needsPacketInfo {
		rawConn, err := c.SyscallConn()
		if err != nil {
			return nil, err
		}
		// We don't know if this a IPv4-only, IPv6-only or a IPv4-and-IPv6
		// connection. Try enabling receiving of packet info for both IP
		// versions. We expect at least one of those calls to succeed.
		var errPIIPv4, errPIIPv6 error
		if err := rawConn.Control(func(fd uintptr) {
			errPIIPv4 = windows.SetsockoptInt(windows.Handle(fd), windows.IPPROTO_IP, windows.IP_PKTINFO, 1)
			errPIIPv6 = windows.SetsockoptInt(windows.Handle(fd), windows.IPPROTO_IPV6, windows.IPV6_PKTINFO, 1)
		}); err != nil {
			return nil, err
		}
		switch {
		case errPIIPv4 == nil && errPIIPv6 == nil:
			utils.DefaultLogger.Debugf("Activating reading of packet info for IPv4 and IPv6.")
		case errPIIPv4 == nil && errPIIPv6 != nil:
			utils.DefaultLogger.Debugf("Activating reading of packet info for IPv4.")
		case errPIIPv4 != nil && errPIIPv6 == nil:
			utils.DefaultLogger.Debugf("Activating reading of packet info for IPv6.")
		case errPIIPv4 != nil && errPIIPv6 != nil:
			return nil, errors.New("activating packet info failed for both IPv4 and IPv6")
		}
	}
	var uso bool
	if ownsSocket {
		rawConn, err := c.SyscallConn()
		if err != nil {
			return nil, err
		}
		uso = isUSOEnabled(rawConn)
	}
	return &windowsConn{
		OOBCapablePacketConn: c,
		oobBuffer:            make([]byte, oobBufferSize),
		cap:                  connCapabilities{DF: supportsDF, GSO: uso},
	}, nil
}

// isUSOEnabled tests if this Windows build supports UDP segmentation offload
// (USO) by reading the UDP_SEND_MSG_SIZE socket option; a build without USO
// rejects the option. The read mutates nothing, mirroring the Linux
// UDP_SEGMENT getsockopt probe, and honors the QUIC_GO_DISABLE_GSO kill
// switch that governs segmented send on every platform.
func isUSOEnabled(conn syscall.RawConn) bool {
	if disabled, err := strconv.ParseBool(os.Getenv("QUIC_GO_DISABLE_GSO")); err == nil && disabled {
		return false
	}
	var serr error
	if err := conn.Control(func(fd uintptr) {
		_, serr = windows.GetsockoptInt(windows.Handle(fd), windows.IPPROTO_UDP, windows.UDP_SEND_MSG_SIZE)
	}); err != nil {
		return false
	}
	return serr == nil
}

func (c *windowsConn) ReadPacket() (receivedPacket, error) {
	buffer := getPacketBuffer()
	// The packet size should not exceed protocol.MaxPacketBufferSize bytes
	// If it does, we only read a truncated packet, which will then end up undecryptable
	buffer.Data = buffer.Data[:protocol.MaxPacketBufferSize]
	n, oobn, _, addr, err := c.ReadMsgUDP(buffer.Data, c.oobBuffer)
	if err != nil {
		buffer.Release()
		return receivedPacket{}, err
	}
	return receivedPacket{
		remoteAddr: addr,
		rcvTime:    monotime.Now(),
		data:       buffer.Data[:n],
		buffer:     buffer,
		info:       parsePacketInfo(c.oobBuffer[:oobn]),
	}, nil
}

func (c *windowsConn) WritePacket(b []byte, addr net.Addr, packetInfoOOB []byte, gsoSize uint16, ecn protocol.ECN) (int, error) {
	oob := packetInfoOOB
	if gsoSize > 0 {
		if !c.cap.GSO {
			panic("GSO disabled")
		}
		// USO's per-send segment size is a DWORD, unlike the uint16 Linux
		// UDP_SEGMENT carries.
		var data []byte
		oob, data = appendCmsg(oob, windows.IPPROTO_UDP, windows.UDP_SEND_MSG_SIZE, 4)
		binary.NativeEndian.PutUint32(data, uint32(gsoSize))
	}
	if ecn != protocol.ECNUnsupported {
		panic("cannot use ECN with a windowsConn")
	}
	udpAddr, ok := addr.(*net.UDPAddr)
	if !ok {
		// The replaced basicConn returned the socket's error for a nil or
		// non-UDP destination; keep that behavior instead of panicking on
		// the assertion. WriteTo rejects every non-UDP address, so this
		// never sends.
		return c.WriteTo(b, addr)
	}
	n, _, err := c.WriteMsgUDP(b, oob, udpAddr)
	if err == nil && gsoSize > 0 {
		// Winsock reports zero transferred bytes on send completions that
		// carry UDP_SEND_MSG_SIZE (observed on Server 2025 for segmented
		// and single-packet submissions alike). A UDP send is
		// all-or-nothing, so success means the whole buffer was accepted;
		// report it, preserving the rawConn byte-count contract.
		n = len(b)
	}
	return n, err
}

func (c *windowsConn) capabilities() connCapabilities { return c.cap }

func inspectReadBuffer(c syscall.RawConn) (int, error) {
	var size int
	var serr error
	if err := c.Control(func(fd uintptr) {
		size, serr = windows.GetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_RCVBUF)
	}); err != nil {
		return 0, err
	}
	return size, serr
}

func inspectWriteBuffer(c syscall.RawConn) (int, error) {
	var size int
	var serr error
	if err := c.Control(func(fd uintptr) {
		size, serr = windows.GetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_SNDBUF)
	}); err != nil {
		return 0, err
	}
	return size, serr
}

type packetInfo struct {
	addr    netip.Addr
	ifIndex uint32
	// pktinfoV6 records that the kernel delivered the packet info as
	// IPV6_PKTINFO (an IPv6 or dual-stack socket). The reply control message
	// must use the family the socket actually speaks: a dual-stack socket
	// carries IPv4 traffic as v4-mapped addresses at the IPPROTO_IPV6 level,
	// while addr stays unmapped for parity with the OOB platforms.
	pktinfoV6 bool
}

// Control-message layout per ws2def.h: WSACMSGHDR is SIZE_T cmsg_len, INT
// cmsg_level, INT cmsg_type; data starts at
// WSA_CMSGDATA_ALIGN(sizeof(WSACMSGHDR)) and successive headers at
// WSA_CMSGHDR_ALIGN(cmsg_len). Both alignments equal the platform pointer
// size, so header size and data offset coincide on 32- and 64-bit builds.
const (
	wsaCmsgHdrSize    = int(unsafe.Sizeof(windows.WSACMSGHDR{}))
	wsaCmsgAlignTo    = int(unsafe.Alignof(windows.WSACMSGHDR{}))
	wsaCmsgDataOffset = (wsaCmsgHdrSize + wsaCmsgAlignTo - 1) &^ (wsaCmsgAlignTo - 1)
)

func wsaCmsgAlign(n int) int { return (n + wsaCmsgAlignTo - 1) &^ (wsaCmsgAlignTo - 1) }

// appendCmsg appends a control message and returns the extended buffer along
// with the message's zeroed data section. The buffer grows by
// WSA_CMSG_SPACE(dataLen) — WSASendMsg rejects a control buffer that is not
// padded to the platform alignment — while cmsg_len stays the unpadded
// WSA_CMSG_LEN(dataLen).
func appendCmsg(b []byte, level, typ int32, dataLen int) ([]byte, []byte) {
	startLen := len(b)
	b = append(b, make([]byte, wsaCmsgAlign(wsaCmsgDataOffset+dataLen))...)
	h := (*windows.WSACMSGHDR)(unsafe.Pointer(&b[startLen]))
	h.Len = uintptr(wsaCmsgDataOffset + dataLen)
	h.Level = level
	h.Type = typ
	return b, b[startLen+wsaCmsgDataOffset:]
}

var invalidCmsgOnceV4, invalidCmsgOnceV6 sync.Once

// parsePacketInfo extracts IPv4/IPv6 packet info from a WSAMSG control
// buffer. Packet info is best-effort with an absent-info fallback, so a
// structurally invalid buffer stops parsing without failing the read.
func parsePacketInfo(oob []byte) packetInfo {
	var info packetInfo
	for len(oob) >= wsaCmsgDataOffset {
		hdr := (*windows.WSACMSGHDR)(unsafe.Pointer(&oob[0]))
		if hdr.Len < uintptr(wsaCmsgDataOffset) || hdr.Len > uintptr(len(oob)) {
			return info
		}
		body := oob[wsaCmsgDataOffset:hdr.Len]
		if hdr.Level == windows.IPPROTO_IP && hdr.Type == windows.IP_PKTINFO {
			// struct in_pktinfo { IN_ADDR ipi_addr; ULONG ipi_ifindex; }
			if len(body) == 8 {
				info.addr = netip.AddrFrom4(*(*[4]byte)(body[:4]))
				info.ifIndex = binary.NativeEndian.Uint32(body[4:])
				info.pktinfoV6 = false
			} else {
				invalidCmsgOnceV4.Do(func() {
					log.Printf("Received invalid IPv4 packet info control message: %+x. "+
						"This should never occur, please open a new issue and include details about the architecture.", body)
				})
			}
		}
		if hdr.Level == windows.IPPROTO_IPV6 && hdr.Type == windows.IPV6_PKTINFO {
			// struct in6_pktinfo { IN6_ADDR ipi6_addr; ULONG ipi6_ifindex; }
			if len(body) == 20 {
				info.addr = netip.AddrFrom16(*(*[16]byte)(body[:16])).Unmap()
				info.ifIndex = binary.NativeEndian.Uint32(body[16:])
				info.pktinfoV6 = true
			} else {
				invalidCmsgOnceV6.Do(func() {
					log.Printf("Received invalid IPv6 packet info control message: %+x. "+
						"This should never occur, please open a new issue and include details about the architecture.", body)
				})
			}
		}
		next := wsaCmsgAlign(int(hdr.Len))
		if next > len(oob) {
			return info
		}
		oob = oob[next:]
	}
	return info
}

func (info *packetInfo) OOB() []byte {
	if info == nil {
		return nil
	}
	switch {
	case info.pktinfoV6 || info.addr.Is6():
		// As16 yields the v4-mapped form for an unmapped IPv4 address, which
		// is how a dual-stack socket addresses IPv4 traffic.
		ip := info.addr.As16()
		b, data := appendCmsg(nil, windows.IPPROTO_IPV6, windows.IPV6_PKTINFO, 20)
		copy(data, ip[:])
		binary.NativeEndian.PutUint32(data[16:], info.ifIndex)
		return b
	case info.addr.Is4():
		ip := info.addr.As4()
		b, data := appendCmsg(nil, windows.IPPROTO_IP, windows.IP_PKTINFO, 8)
		copy(data, ip[:])
		binary.NativeEndian.PutUint32(data[4:], info.ifIndex)
		return b
	}
	return nil
}
