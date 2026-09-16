package quic

import (
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
)

// receiveCoalescingState records setup decisions, not hypothetical kernel support.
// It is diagnostic-only; GRO remains the receive path's activation flag.
type receiveCoalescingState struct {
	eligible       bool
	disabledReason string
}

type connCapabilities struct {
	// This connection has the Don't Fragment (DF) bit set.
	// This means it makes to run DPLPMTUD.
	DF bool
	// Segmented sends supported (Linux GSO or Windows USO).
	// Read-only discovery does not grant receive-format or close authority.
	GSO bool
	// ECN (Explicit Congestion Notifications) supported
	ECN bool
	// GRO (Generic Receive Offload) enabled on this socket. Only ever set on
	// transport-created sockets or explicitly permissioned external sockets.
	// Receive-format permission never transfers Close ownership.
	GRO               bool
	receiveCoalescing receiveCoalescingState
}

// rawConn is a connection that allow reading of a receivedPackeh.
type rawConn interface {
	ReadPacket() (receivedPacket, error)
	// WritePacket writes a packet on the wire.
	// gsoSize is the size of a single packet, or 0 to disable GSO.
	// It is invalid to set gsoSize if capabilities.GSO is not set.
	WritePacket(b []byte, addr net.Addr, packetInfoOOB []byte, gsoSize uint16, ecn protocol.ECN) (int, error)
	LocalAddr() net.Addr
	SetReadDeadline(time.Time) error
	io.Closer

	capabilities() connCapabilities
}

// OOBCapablePacketConn is a connection that allows the reading of ECN bits from the IP header.
// If the PacketConn passed to the [Transport] satisfies this interface, quic-go will use it.
// On platforms that support batch reads, native UDP connections and wrappers
// implementing ReadBatch use batching. Other supported wrappers receive through
// [OOBCapablePacketConn.ReadMsgUDP], preserving their receive policy.
// [OOBCapablePacketConn.WriteMsgUDP] is used instead of [net.PacketConn.WriteTo]
// to write packets.
type OOBCapablePacketConn interface {
	net.PacketConn
	SyscallConn() (syscall.RawConn, error)
	SetReadBuffer(int) error
	ReadMsgUDP(b, oob []byte) (n, oobn, flags int, addr *net.UDPAddr, err error)
	WriteMsgUDP(b, oob []byte, addr *net.UDPAddr) (n, oobn int, err error)
}

var _ OOBCapablePacketConn = &net.UDPConn{}

// wrapConn prepares a net.PacketConn for use by the transport.
// allowReceiveCoalescing grants socket-wide receive-format mutation. External
// permission is resolved by Transport after validating the exact binding.
func wrapConn(pc net.PacketConn, allowReceiveCoalescing bool) (rawConn, error) {
	return wrapConnWithManagedBuffers(pc, allowReceiveCoalescing, nil)
}

func wrapConnWithManagedBuffers(pc net.PacketConn, allowReceiveCoalescing bool, managed *managedBufferSetup) (rawConn, error) {
	if managed == nil {
		warnBufferSize(setReceiveBuffer(pc))
		warnBufferSize(setSendBuffer(pc))
	}

	conn, ok := pc.(interface {
		SyscallConn() (syscall.RawConn, error)
	})
	var supportsDF bool
	if ok {
		rawConn, err := conn.SyscallConn()
		if err != nil {
			return nil, err
		}

		// only set DF on UDP sockets
		if _, ok := pc.LocalAddr().(*net.UDPAddr); ok {
			var err error
			supportsDF, err = setDF(rawConn)
			if err != nil {
				return nil, err
			}
		}
	}
	c, ok := pc.(OOBCapablePacketConn)
	if !ok {
		if managed == nil {
			utils.DefaultLogger.Infof("PacketConn is not a net.UDPConn. Disabling optimizations possible on UDP connections.")
		}
		return &basicConn{PacketConn: pc, supportsDF: supportsDF}, nil
	}
	return newConn(c, supportsDF, allowReceiveCoalescing)
}

// Preserve the shared warning budget and opt-out for ordinary and managed setup.
func warnBufferSize(err error) {
	if err == nil || strings.Contains(err.Error(), "use of closed network connection") {
		return
	}
	setBufferWarningOnce.Do(func() {
		if disable, _ := strconv.ParseBool(os.Getenv("QUIC_GO_DISABLE_RECEIVE_BUFFER_WARNING")); disable {
			return
		}
		log.Printf("%s. See https://github.com/quic-go/quic-go/wiki/UDP-Buffer-Sizes for details.", err)
	})
}

// The basicConn is the most trivial implementation of a rawConn.
// It reads a single packet from the underlying net.PacketConn.
// It is used when
// * the net.PacketConn is not a OOBCapablePacketConn, and
// * when the OS doesn't support OOB.
type basicConn struct {
	net.PacketConn
	supportsDF bool
}

var _ rawConn = &basicConn{}

func (c *basicConn) ReadPacket() (receivedPacket, error) {
	buffer := getPacketBuffer()
	// The packet size should not exceed protocol.MaxPacketBufferSize bytes
	// If it does, we only read a truncated packet, which will then end up undecryptable
	buffer.Data = buffer.Data[:protocol.MaxPacketBufferSize]
	n, addr, err := c.ReadFrom(buffer.Data)
	if err != nil {
		buffer.Release()
		return receivedPacket{}, err
	}
	return receivedPacket{
		remoteAddr: addr,
		rcvTime:    monotime.Now(),
		data:       buffer.Data[:n],
		buffer:     buffer,
	}, nil
}

func (c *basicConn) WritePacket(b []byte, addr net.Addr, _ []byte, gsoSize uint16, ecn protocol.ECN) (n int, err error) {
	if gsoSize != 0 {
		panic("cannot use GSO with a basicConn")
	}
	if ecn != protocol.ECNUnsupported {
		panic("cannot use ECN with a basicConn")
	}
	return c.WriteTo(b, addr)
}

func (c *basicConn) capabilities() connCapabilities { return connCapabilities{DF: c.supportsDF} }
