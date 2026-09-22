//go:build darwin || linux || freebsd

package quic

import (
	"encoding/binary"
	"errors"
	"log"
	"net"
	"net/netip"
	"os"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
	"golang.org/x/sys/unix"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
)

const (
	ecnMask       = 0x3
	oobBufferSize = 128
)

// Contrary to what the naming suggests, the ipv{4,6}.Message is not dependent on the IP version.
// They're both just aliases for x/net/internal/socket.Message.
// This means we can use this struct to read from a socket that receives both IPv4 and IPv6 messages.
var _ ipv4.Message = ipv6.Message{}

type batchConn interface {
	ReadBatch(ms []ipv4.Message, flags int) (int, error)
}

// readMsgConn preserves a wrapper's receive policy. The reader supplies at least
// one message with one payload buffer and always uses zero input flags.
type readMsgConn struct {
	OOBCapablePacketConn
}

func (c readMsgConn) ReadBatch(ms []ipv4.Message, _ int) (int, error) {
	m := &ms[0]
	n, nn, flags, addr, err := c.ReadMsgUDP(m.Buffers[0], m.OOB)
	m.N, m.NN, m.Flags, m.Addr = n, nn, flags, addr
	if err != nil {
		return 0, err
	}
	return 1, nil
}

func isECNDisabledUsingEnv() bool {
	disabled, err := strconv.ParseBool(os.Getenv("QUIC_GO_DISABLE_ECN"))
	return err == nil && disabled
}

type oobConn struct {
	OOBCapablePacketConn
	batchConn batchConn

	readPos uint8
	// Packets received from the kernel, but not yet returned by ReadPacket().
	messages []ipv4.Message
	// A nil slot has transferred ownership; non-nil storage belongs to this reader.
	buffers [batchSize]*packetBuffer
	// Per-datagram views split from a coalesced (GRO) read, not yet returned
	// by ReadPacket(). Each holds one reference on the read's shared slab.
	delivery coalescedDelivery

	cap connCapabilities
	// managedRead uses one full-size ReadMsgUDP receive when ECN metadata is
	// retained without GRO. It never batches ahead across a lease boundary.
	managedRead bool
	managedOOB  []byte
}

var _ rawConn = &oobConn{}

// Managed endpoints can retain ordinary reads when only ECN setup is denied.
// Other callers still return this error, with the historical error text.
var errECNSetupDenied = errors.New("activating ECN failed for both IPv4 and IPv6")

type oobConnSetup struct {
	// ecnUnavailable identifies optional-only ECN setup failure after descriptor
	// access and any required packet-info setup succeeded. Callers retain policy.
	ecnUnavailable bool
	ecnIPv4Err     error
	ecnIPv6Err     error
}

func newConn(c OOBCapablePacketConn, supportsDF, allowReceiveCoalescing bool) (*oobConn, error) {
	conn, _, err := newConnWithSetup(c, supportsDF, allowReceiveCoalescing)
	return conn, err
}

func newConnWithSetup(c OOBCapablePacketConn, supportsDF, allowReceiveCoalescing bool) (*oobConn, oobConnSetup, error) {
	rawConn, err := c.SyscallConn()
	if err != nil {
		return nil, oobConnSetup{}, err
	}
	var needsPacketInfo bool
	if udpAddr, ok := c.LocalAddr().(*net.UDPAddr); ok && udpAddr.IP.IsUnspecified() {
		needsPacketInfo = true
	}
	// We don't know if this a IPv4-only, IPv6-only or a IPv4-and-IPv6 connection.
	// Try enabling receiving of ECN and packet info for both IP versions.
	// We expect at least one of those syscalls to succeed.
	var errECNIPv4, errECNIPv6, errPIIPv4, errPIIPv6 error
	if err := rawConn.Control(func(fd uintptr) {
		errECNIPv4 = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_RECVTOS, 1)
		errECNIPv6 = unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_RECVTCLASS, 1)

		if needsPacketInfo {
			errPIIPv4 = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, ipv4PKTINFO, 1)
			errPIIPv6 = unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_RECVPKTINFO, 1)
		}
	}); err != nil {
		return nil, oobConnSetup{}, err
	}
	setup := oobConnSetup{ecnIPv4Err: errECNIPv4, ecnIPv6Err: errECNIPv6}
	setup.ecnUnavailable = errECNIPv4 != nil && errECNIPv6 != nil && (!needsPacketInfo || errPIIPv4 == nil || errPIIPv6 == nil)
	switch {
	case errECNIPv4 == nil && errECNIPv6 == nil:
		utils.DefaultLogger.Debugf("Activating reading of ECN bits for IPv4 and IPv6.")
	case errECNIPv4 == nil && errECNIPv6 != nil:
		utils.DefaultLogger.Debugf("Activating reading of ECN bits for IPv4.")
	case errECNIPv4 != nil && errECNIPv6 == nil:
		utils.DefaultLogger.Debugf("Activating reading of ECN bits for IPv6.")
	case errECNIPv4 != nil && errECNIPv6 != nil:
		if errECNIPv4 == unix.EPERM && errECNIPv6 == unix.EPERM && (!needsPacketInfo || errPIIPv4 == nil || errPIIPv6 == nil) {
			return nil, setup, errECNSetupDenied
		}
		return nil, setup, errors.New("activating ECN failed for both IPv4 and IPv6")
	}
	if needsPacketInfo {
		switch {
		case errPIIPv4 == nil && errPIIPv6 == nil:
			utils.DefaultLogger.Debugf("Activating reading of packet info for IPv4 and IPv6.")
		case errPIIPv4 == nil && errPIIPv6 != nil:
			utils.DefaultLogger.Debugf("Activating reading of packet info bits for IPv4.")
		case errPIIPv4 != nil && errPIIPv6 == nil:
			utils.DefaultLogger.Debugf("Activating reading of packet info bits for IPv6.")
		case errPIIPv4 != nil && errPIIPv6 != nil:
			return nil, setup, errors.New("activating packet info failed for both IPv4 and IPv6")
		}
	}

	// A participating wrapper owns batch receive. Only exact native sockets may
	// bypass ReadMsgUDP through descriptor-backed batching.
	receive := receiveCoalescingState{eligible: runtime.GOOS == "linux"}
	var bc batchConn
	if ibc, ok := c.(batchConn); ok {
		bc = ibc
	} else {
		if _, ok := c.(net.Conn); !ok {
			return nil, setup, errors.New("quic: OOBCapablePacketConn must implement net.Conn or ReadBatch")
		}
		if udp, ok := c.(*net.UDPConn); ok {
			bc = ipv4.NewPacketConn(udp)
		} else {
			bc = readMsgConn{c}
			// Ordinary wrappers have not opted into the batched coalesced format.
			allowReceiveCoalescing = false
			receive.eligible = false
		}
	}

	msgs := make([]ipv4.Message, batchSize)
	for i := range msgs {
		// preallocate the [][]byte
		msgs[i].Buffers = make([][]byte, 1)
	}
	oobConn := &oobConn{
		OOBCapablePacketConn: c,
		batchConn:            bc,
		messages:             msgs,
		readPos:              batchSize,
		cap: connCapabilities{
			DF:                supportsDF,
			GSO:               isGSOEnabled(rawConn),
			ECN:               isECNEnabled(),
			receiveCoalescing: receive,
		},
		managedOOB: make([]byte, oobBufferSize),
	}
	if allowReceiveCoalescing {
		// Permission and wrapper policy gate the mutating activation attempt.
		oobConn.cap.GRO, oobConn.cap.receiveCoalescing.disabledReason = enableGRO(rawConn)
	}
	for i := range batchSize {
		oobConn.messages[i].OOB = make([]byte, oobBufferSize)
	}
	return oobConn, setup, nil
}

var invalidCmsgOnceV4, invalidCmsgOnceV6 sync.Once

func (c *oobConn) ReadPacket() (receivedPacket, error) {
	if c.managedRead && !c.cap.GRO {
		return c.readManagedPacket()
	}
	for {
		// A coalesced read is split into per-datagram views, returned one per
		// call to preserve ReadPacket's one-datagram contract.
		if p, ok := c.delivery.next(); ok {
			return p, nil
		}
		for len(c.messages) == int(c.readPos) { // all messages read. Read the next batch of messages.
			c.messages = c.messages[:batchSize]
			for i := range c.buffers {
				if c.buffers[i] == nil {
					var buffer *packetBuffer
					if c.cap.GRO {
						// a single GRO read can deliver up to 65535 bytes
						buffer = getCoalescedPacketBuffer()
						buffer.Data = buffer.Data[:protocol.MaxCoalescedPacketBufferSize]
					} else {
						buffer = getPacketBuffer()
						buffer.Data = buffer.Data[:protocol.MaxPacketBufferSize]
					}
					c.buffers[i] = buffer
				}
				c.messages[i].Buffers[0] = c.buffers[i].Data
			}
			c.readPos = 0

			n, err := c.batchConn.ReadBatch(c.messages, 0)
			if err != nil {
				// Preserve the existing policy: even a non-empty failed batch is discarded.
				c.releaseReadBuffers()
				return receivedPacket{}, err
			}
			c.messages = c.messages[:n]
		}

		msg := c.messages[c.readPos]
		buffer := c.buffers[c.readPos]
		readBuffer := msg.Buffers[0]
		c.buffers[c.readPos] = nil
		c.messages[c.readPos].Buffers[0] = nil
		c.readPos++

		p, err := c.decodeReadPacket(msg, readBuffer, buffer)
		if err == nil {
			return p, nil
		}
		buffer.Release()
		if !c.cap.GRO {
			return receivedPacket{}, err
		}
		// Ancillary truncation can result from caller-enabled socket options.
		// Reject this read, not every connection sharing the transport. Actual
		// socket errors still terminate through the ReadBatch branch above.
		utils.DefaultLogger.Debugf("Dropping packet with invalid coalesced receive metadata: %s", err)
	}
}

func (c *oobConn) readManagedPacket() (receivedPacket, error) {
	for {
		buffer := getCoalescedPacketBuffer()
		buffer.Data = buffer.Data[:protocol.MaxCoalescedPacketBufferSize]
		n, oobn, flags, addr, err := c.ReadMsgUDP(buffer.Data, c.managedOOB)
		if err != nil {
			buffer.Release()
			return receivedPacket{}, err
		}
		msg := ipv4.Message{Buffers: [][]byte{buffer.Data}, OOB: c.managedOOB, N: n, NN: oobn, Flags: flags, Addr: addr}
		packet, err := c.decodeReadPacket(msg, buffer.Data, buffer)
		if err == nil {
			return packet, nil
		}
		buffer.Release()
		utils.DefaultLogger.Debugf("Dropping packet with invalid managed ECN metadata: %s", err)
	}
}

// decodeReadPacket transfers storage only on success. The reader releases a
// rejected read; parsing metadata never publishes a partial set of siblings.
func (c *oobConn) decodeReadPacket(msg ipv4.Message, readBuffer []byte, buffer *packetBuffer) (receivedPacket, error) {
	if (c.cap.GRO || c.managedRead) && (msg.Flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || msg.N < 0 || msg.N > len(readBuffer) || msg.NN < 0 || msg.NN > len(msg.OOB)) {
		return receivedPacket{}, errors.New("quic: truncated or invalid coalesced read")
	}
	payload := readBuffer[:msg.N]

	data := msg.OOB[:msg.NN]
	p := receivedPacket{
		remoteAddr: msg.Addr,
		rcvTime:    monotime.Now(),
		data:       payload,
		buffer:     buffer,
	}
	var groSegmentSize int
	for len(data) > 0 {
		hdr, body, remainder, err := unix.ParseOneSocketControlMessage(data)
		if err != nil {
			return receivedPacket{}, err
		}
		if size, ok := parseUDPGROSegmentSize(&hdr, body); ok {
			if c.cap.GRO && (groSegmentSize != 0 || size <= 0 || size > len(payload)) {
				return receivedPacket{}, errors.New("quic: invalid UDP_GRO segment size")
			}
			groSegmentSize = size
		}
		if hdr.Level == unix.IPPROTO_IP {
			switch hdr.Type {
			case msgTypeIPTOS:
				if len(body) != 1 {
					return receivedPacket{}, errors.New("invalid IPTOS size")
				}
				p.ecn = protocol.ParseECNHeaderBits(body[0] & ecnMask)
			case ipv4PKTINFO:
				ip, ifIndex, ok := parseIPv4PktInfo(body)
				if ok {
					p.info.addr = ip
					p.info.ifIndex = ifIndex
				} else {
					invalidCmsgOnceV4.Do(func() {
						log.Printf("Received invalid IPv4 packet info control message: %+x. "+
							"This should never occur, please open a new issue and include details about the architecture.", body)
					})
				}
			}
		}
		if hdr.Level == unix.IPPROTO_IPV6 {
			switch hdr.Type {
			case unix.IPV6_TCLASS:
				if len(body) != 4 {
					return receivedPacket{}, errors.New("invalid IPV6_TCLASS size")
				}
				bits := uint8(binary.NativeEndian.Uint32(body)) & ecnMask
				p.ecn = protocol.ParseECNHeaderBits(bits)
			case unix.IPV6_PKTINFO:
				// struct in6_pktinfo {
				// 	struct in6_addr ipi6_addr;    /* src/dst IPv6 address */
				// 	unsigned int    ipi6_ifindex; /* send/recv interface index */
				// };
				if len(body) == 20 {
					p.info.addr = netip.AddrFrom16(*(*[16]byte)(body[:16])).Unmap()
					p.info.ifIndex = binary.NativeEndian.Uint32(body[16:])
				} else {
					invalidCmsgOnceV6.Do(func() {
						log.Printf("Received invalid IPv6 packet info control message: %+x. "+
							"This should never occur, please open a new issue and include details about the architecture.", body)
					})
				}
			}
		}
		data = remainder
	}
	if !c.cap.GRO || len(payload) == 0 {
		return p, nil
	}
	return c.delivery.accept(p, groSegmentSize), nil
}

// releaseReadBuffers runs only after reading stops, or while discarding a failed
// batch. Returned packets belong to their consumers, and the socket stays open.
func (c *oobConn) releaseReadBuffers() {
	c.delivery.discard()
	c.messages = c.messages[:batchSize]
	for i, buffer := range c.buffers {
		if buffer != nil {
			buffer.Release()
			c.buffers[i] = nil
		}
		c.messages[i].Buffers[0] = nil
	}
	c.messages = c.messages[:0]
	c.readPos = 0
}

// WritePacket writes a new packet.
func (c *oobConn) WritePacket(b []byte, addr net.Addr, packetInfoOOB []byte, gsoSize uint16, ecn protocol.ECN) (int, error) {
	oob := packetInfoOOB
	if gsoSize > 0 {
		if !c.capabilities().GSO {
			panic("GSO disabled")
		}
		oob = appendUDPSegmentSizeMsg(oob, gsoSize)
	}
	if ecn != protocol.ECNUnsupported {
		if !c.capabilities().ECN {
			panic("tried to send an ECN-marked packet although ECN is disabled")
		}
		if remoteUDPAddr, ok := addr.(*net.UDPAddr); ok {
			if remoteUDPAddr.IP.To4() != nil {
				oob = appendIPv4ECNMsg(oob, ecn)
			} else {
				oob = appendIPv6ECNMsg(oob, ecn)
			}
		}
	}
	n, _, err := c.WriteMsgUDP(b, oob, addr.(*net.UDPAddr))
	return n, err
}

func (c *oobConn) capabilities() connCapabilities {
	return c.cap
}

type packetInfo struct {
	addr    netip.Addr
	ifIndex uint32
}

func (info *packetInfo) OOB() []byte {
	if info == nil {
		return nil
	}
	if info.addr.Is4() {
		ip := info.addr.As4()
		// struct in_pktinfo {
		// 	unsigned int   ipi_ifindex;  /* Interface index */
		// 	struct in_addr ipi_spec_dst; /* Local address */
		// 	struct in_addr ipi_addr;     /* Header Destination address */
		// };
		cm := ipv4.ControlMessage{
			Src:     ip[:],
			IfIndex: int(info.ifIndex),
		}
		return cm.Marshal()
	} else if info.addr.Is6() {
		ip := info.addr.As16()
		// struct in6_pktinfo {
		// 	struct in6_addr ipi6_addr;    /* src/dst IPv6 address */
		// 	unsigned int    ipi6_ifindex; /* send/recv interface index */
		// };
		cm := ipv6.ControlMessage{
			Src:     ip[:],
			IfIndex: int(info.ifIndex),
		}
		return cm.Marshal()
	}
	return nil
}

func appendIPv4ECNMsg(b []byte, val protocol.ECN) []byte {
	startLen := len(b)
	b = append(b, make([]byte, unix.CmsgSpace(ecnIPv4DataLen))...)
	h := (*unix.Cmsghdr)(unsafe.Pointer(&b[startLen]))
	h.Level = syscall.IPPROTO_IP
	h.Type = unix.IP_TOS
	h.SetLen(unix.CmsgLen(ecnIPv4DataLen))

	// UnixRights uses the private `data` method, but I *think* this achieves the same goal.
	offset := startLen + unix.CmsgSpace(0)
	b[offset] = val.ToHeaderBits()
	return b
}

func appendIPv6ECNMsg(b []byte, val protocol.ECN) []byte {
	startLen := len(b)
	const dataLen = 4
	b = append(b, make([]byte, unix.CmsgSpace(dataLen))...)
	h := (*unix.Cmsghdr)(unsafe.Pointer(&b[startLen]))
	h.Level = syscall.IPPROTO_IPV6
	h.Type = unix.IPV6_TCLASS
	h.SetLen(unix.CmsgLen(dataLen))

	// UnixRights uses the private `data` method, but I *think* this achieves the same goal.
	offset := startLen + unix.CmsgSpace(0)
	binary.NativeEndian.PutUint32(b[offset:offset+dataLen], uint32(val.ToHeaderBits()))
	return b
}
