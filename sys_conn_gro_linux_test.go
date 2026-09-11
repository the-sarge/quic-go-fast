//go:build linux

package quic

import (
	"encoding/binary"
	"net"
	"net/netip"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"

	"github.com/quic-go/quic-go/internal/protocol"

	"github.com/stretchr/testify/require"
)

// groSocketOption reads the UDP_GRO socket option, reporting whether the
// kernel has GRO enabled on the socket.
func groSocketOption(t *testing.T, udpConn *net.UDPConn) int {
	t.Helper()
	rawConn, err := udpConn.SyscallConn()
	require.NoError(t, err)
	var val int
	var serr error
	require.NoError(t, rawConn.Control(func(fd uintptr) {
		val, serr = unix.GetsockoptInt(int(fd), unix.IPPROTO_UDP, unix.UDP_GRO)
	}))
	require.NoError(t, serr)
	return val
}

func TestGROProbeOnTransportOwnedSocket(t *testing.T) {
	if kernelVersionMajor < 5 {
		t.Skip("UDP_GRO requires kernel 5+")
	}
	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	defer udpConn.Close()

	c, err := newConn(udpConn, true, true)
	require.NoError(t, err)
	require.True(t, c.capabilities().GRO)
	require.Equal(t, 1, groSocketOption(t, udpConn))
}

// The transport must never issue a UDP_GRO setsockopt on a caller-supplied
// socket: coalescing is socket-wide, and reads the caller performs after
// transport close must not see silently truncated coalesced payloads.
func TestGRONotEnabledOnCallerSuppliedSocket(t *testing.T) {
	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	defer udpConn.Close()

	c, err := newConn(udpConn, true, false)
	require.NoError(t, err)
	require.False(t, c.capabilities().GRO)
	require.Equal(t, 0, groSocketOption(t, udpConn))
}

func TestGRODisabledByEnv(t *testing.T) {
	t.Setenv("QUIC_GO_DISABLE_GRO", "1")
	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	defer udpConn.Close()

	c, err := newConn(udpConn, true, true)
	require.NoError(t, err)
	require.False(t, c.capabilities().GRO)
	require.Equal(t, 0, groSocketOption(t, udpConn))
}

// appendUDPGROMsg builds the control message a GRO-enabled kernel attaches to
// a coalesced read: level IPPROTO_UDP, type UDP_GRO, payload the int segment
// size.
func appendUDPGROMsg(b []byte, size int) []byte {
	startLen := len(b)
	const dataLen = 4
	b = append(b, make([]byte, unix.CmsgSpace(dataLen))...)
	h := (*unix.Cmsghdr)(unsafe.Pointer(&b[startLen]))
	h.Level = unix.IPPROTO_UDP
	h.Type = unix.UDP_GRO
	h.SetLen(unix.CmsgLen(dataLen))
	binary.NativeEndian.PutUint32(b[startLen+unix.CmsgSpace(0):], uint32(size))
	return b
}

// appendIPTOSMsg builds an IP_TOS control message carrying ECN header bits.
func appendIPTOSMsg(b []byte, tos byte) []byte {
	startLen := len(b)
	const dataLen = 1
	b = append(b, make([]byte, unix.CmsgSpace(dataLen))...)
	h := (*unix.Cmsghdr)(unsafe.Pointer(&b[startLen]))
	h.Level = unix.IPPROTO_IP
	h.Type = unix.IP_TOS
	h.SetLen(unix.CmsgLen(dataLen))
	b[startLen+unix.CmsgSpace(0)] = tos
	return b
}

// groBatchConn delivers scripted messages, one per ReadBatch call, the way
// the kernel delivers coalesced reads: payload copied into the preposted
// buffer, ancillary data into the preposted OOB buffer.
type groBatchConn struct {
	t           *testing.T
	payloads    [][]byte
	oobs        [][]byte
	addr        *net.UDPAddr
	callCounter int
}

func (c *groBatchConn) ReadBatch(ms []ipv4.Message, _ int) (int, error) {
	c.t.Helper()
	require.Less(c.t, c.callCounter, len(c.payloads), "unexpected ReadBatch call")
	payload, oob := c.payloads[c.callCounter], c.oobs[c.callCounter]
	require.GreaterOrEqual(c.t, len(ms[0].Buffers[0]), len(payload), "preposted buffer too small for coalesced read")
	ms[0].N = copy(ms[0].Buffers[0], payload)
	ms[0].NN = copy(ms[0].OOB, oob)
	ms[0].Addr = c.addr
	c.callCounter++
	return 1, nil
}

func newGROConn(t *testing.T, bc batchConn) *oobConn {
	t.Helper()
	udpConn := newUDPConnLocalhost(t)
	oc, err := newConn(udpConn, true, true)
	require.NoError(t, err)
	require.True(t, oc.capabilities().GRO, "GRO probe failed; kernel too old?")
	oc.batchConn = bc
	return oc
}

func TestGROReadSplitsCoalescedRead(t *testing.T) {
	segs := testCoalescedSegments(3, 1200)
	segs[2] = segs[2][:500] // short tail
	payload := append(append(append([]byte{}, segs[0]...), segs[1]...), segs[2]...)
	oob := appendIPTOSMsg(appendUDPGROMsg(nil, 1200), 0b10) // ECT(0)
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 42), Port: 1234}
	bc := &groBatchConn{t: t, payloads: [][]byte{payload, []byte("next read")}, oobs: [][]byte{oob, nil}, addr: addr}
	oc := newGROConn(t, bc)

	var slab *coalescedSlab
	views := make([]*packetBuffer, 3)
	for i, want := range segs {
		p, err := oc.ReadPacket()
		require.NoError(t, err)
		require.Equal(t, want, p.data)
		require.Equal(t, addr, p.remoteAddr)
		require.Equal(t, protocol.ECT0, p.ecn, "segment %d must inherit the read's ECN marking", i)
		require.NotNil(t, p.buffer.slab, "segment %d must be a slab-backed view", i)
		if i == 0 {
			slab = p.buffer.slab
		} else {
			require.Same(t, slab, p.buffer.slab, "siblings share one slab")
		}
		views[i] = p.buffer
	}
	require.Equal(t, 1, bc.callCounter, "all segments must come from one socket read")

	for _, v := range views {
		require.False(t, slab.released())
		v.Release()
	}
	require.True(t, slab.released(), "slab must recycle after the last view releases")

	// the next ReadPacket reads the next batch; without a GRO cmsg the
	// datagram is still slab-backed so retention-queue copies stay uniform
	p, err := oc.ReadPacket()
	require.NoError(t, err)
	require.Equal(t, []byte("next read"), p.data)
	require.NotNil(t, p.buffer.slab)
	next := p.buffer.slab
	p.buffer.Release()
	require.True(t, next.released())
	require.Equal(t, 2, bc.callCounter)
}

func TestGROReadEmptyDatagram(t *testing.T) {
	bc := &groBatchConn{t: t, payloads: [][]byte{{}}, oobs: [][]byte{nil}, addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}}
	oc := newGROConn(t, bc)

	p, err := oc.ReadPacket()
	require.NoError(t, err)
	require.Empty(t, p.data)
	require.NotNil(t, p.buffer)
	require.Nil(t, p.buffer.slab, "an empty read has no segments to view")
	p.buffer.Release()
}

func TestGROReadPacketInfoInheritance(t *testing.T) {
	segs := testCoalescedSegments(2, 100)
	payload := append(append([]byte{}, segs[0]...), segs[1]...)
	// in_pktinfo: ifindex, spec_dst, addr
	pktinfo := make([]byte, 12)
	binary.NativeEndian.PutUint32(pktinfo, 7)
	copy(pktinfo[8:], []byte{10, 0, 0, 9})
	oob := appendUDPGROMsg(nil, 100)
	startLen := len(oob)
	oob = append(oob, make([]byte, unix.CmsgSpace(len(pktinfo)))...)
	h := (*unix.Cmsghdr)(unsafe.Pointer(&oob[startLen]))
	h.Level = unix.IPPROTO_IP
	h.Type = unix.IP_PKTINFO
	h.SetLen(unix.CmsgLen(len(pktinfo)))
	copy(oob[startLen+unix.CmsgSpace(0):], pktinfo)

	bc := &groBatchConn{t: t, payloads: [][]byte{payload}, oobs: [][]byte{oob}, addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}}
	oc := newGROConn(t, bc)

	for i := range segs {
		p, err := oc.ReadPacket()
		require.NoError(t, err)
		require.Equal(t, netip.AddrFrom4([4]byte{10, 0, 0, 9}), p.info.addr, "segment %d must inherit packet info", i)
		require.EqualValues(t, 7, p.info.ifIndex)
		p.buffer.Release()
	}
}

func TestGROReleaseReadBuffersReleasesPendingViews(t *testing.T) {
	segs := testCoalescedSegments(3, 800)
	payload := append(append(append([]byte{}, segs[0]...), segs[1]...), segs[2]...)
	bc := &groBatchConn{t: t, payloads: [][]byte{payload}, oobs: [][]byte{appendUDPGROMsg(nil, 800)}, addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}}
	oc := newGROConn(t, bc)

	p, err := oc.ReadPacket()
	require.NoError(t, err)
	slab := p.buffer.slab
	require.NotNil(t, slab)

	// reading stops with two undelivered sibling views pending
	oc.releaseReadBuffers()
	require.False(t, slab.released(), "the delivered view still holds the slab")
	p.buffer.Release()
	require.True(t, slab.released())
}

// TestGROEndToEndLoopback sends one GSO batch across loopback into a
// GRO-enabled socket and verifies every datagram arrives intact and in
// order, whether or not the kernel coalesced them. Engagement (several
// datagrams per read) is logged, not asserted: the adoption protocol owns
// the engagement evidence.
func TestGROEndToEndLoopback(t *testing.T) {
	recvUDP, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	defer recvUDP.Close()
	recvConn, err := newConn(recvUDP, true, true)
	require.NoError(t, err)
	if !recvConn.capabilities().GRO {
		t.Skip("kernel does not support UDP_GRO")
	}

	sendUDP, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	defer sendUDP.Close()
	sendConn, err := newConn(sendUDP, true, true)
	require.NoError(t, err)
	if !sendConn.capabilities().GSO {
		t.Skip("kernel does not support UDP_SEGMENT")
	}

	const segSize, numSegs = 1200, 8
	segs := testCoalescedSegments(numSegs, segSize)
	var batch []byte
	for _, seg := range segs {
		batch = append(batch, seg...)
	}
	_, err = sendConn.WritePacket(batch, recvUDP.LocalAddr(), nil, segSize, protocol.ECNUnsupported)
	require.NoError(t, err)

	require.NoError(t, recvUDP.SetReadDeadline(time.Now().Add(5*time.Second)))
	slabs := map[*coalescedSlab]int{}
	for i := range numSegs {
		p, err := recvConn.ReadPacket()
		require.NoError(t, err)
		require.Equal(t, segs[i], p.data, "datagram %d must arrive intact and in order", i)
		require.NotNil(t, p.buffer.slab)
		slabs[p.buffer.slab]++
		p.buffer.Release()
	}
	coalesced := 0
	for _, n := range slabs {
		if n > 1 {
			coalesced += n
		}
	}
	t.Logf("%d datagrams in %d socket reads (%d arrived coalesced)", numSegs, len(slabs), coalesced)
}
