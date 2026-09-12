//go:build windows

package quic

import (
	"encoding/binary"
	"fmt"
	"net"
	"net/netip"
	"os"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/quic-go/quic-go/internal/protocol"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests for Slice W3 (Windows coalesced receive, URO): the
// UDP_RECV_MAX_COALESCED_SIZE capability probe on transport-owned sockets,
// UDP_COALESCED_INFO parsing, and the split of a coalesced read into
// per-datagram slab views through the shared coalesced-storage contract.

// requireUROCapableHost enforces the closure evidence's host posture: on a
// hosted CI runner (GITHUB_ACTIONS set) the qualified Windows surface is
// URO-capable, so a probe reporting no capability is a real failure — never
// a vacuous skip that would silently drop the URO closure classes. Off CI, a
// probe-false host is legitimate (an older Windows build) and the
// URO-dependent test skips loudly. The probe's own failure branches are
// covered deterministically by TestWindowsUROProbeFailure.
func requireUROCapableHost(t *testing.T, conn rawConn) {
	t.Helper()
	if conn.capabilities().GRO {
		return
	}
	if os.Getenv("GITHUB_ACTIONS") != "" {
		t.Fatal("URO probe reported unsupported on a hosted CI runner; the qualified Windows surface must exercise the URO closure classes")
	}
	t.Skip("URO probe reported unsupported on this host; the URO-capable CI matrix asserts this capability strictly")
}

// uroSocketOption reads UDP_RECV_MAX_COALESCED_SIZE back from the socket,
// reporting the maximum coalesced message size Winsock has enabled (0 when
// receive coalescing is off).
func uroSocketOption(t *testing.T, udpConn *net.UDPConn) int {
	t.Helper()
	rawConn, err := udpConn.SyscallConn()
	require.NoError(t, err)
	var val int
	var serr error
	require.NoError(t, rawConn.Control(func(fd uintptr) {
		val, serr = windows.GetsockoptInt(windows.Handle(fd), windows.IPPROTO_UDP, windows.UDP_RECV_MAX_COALESCED_SIZE)
	}))
	require.NoError(t, serr)
	return val
}

func newWindowsOwnedConn(t *testing.T, ownsSocket bool) (*windowsConn, *net.UDPConn) {
	t.Helper()
	udpConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	require.NoError(t, err)
	t.Cleanup(func() { udpConn.Close() })
	conn, err := newConn(udpConn, true, ownsSocket)
	require.NoError(t, err)
	return conn, udpConn
}

// The probe enables receive coalescing on transport-owned sockets up to the
// coalesced buffer tier's capacity, so a kernel-coalesced read is never
// truncated.
func TestWindowsUROProbeOnTransportOwnedSocket(t *testing.T) {
	conn, udpConn := newWindowsOwnedConn(t, true)
	requireUROCapableHost(t, conn)
	require.True(t, conn.capabilities().GRO)
	require.Equal(t, protocol.MaxCoalescedPacketBufferSize, uroSocketOption(t, udpConn))
}

// The transport must never issue the UDP_RECV_MAX_COALESCED_SIZE setsockopt
// on a caller-supplied socket: coalescing is socket-wide, and reads the
// caller performs after transport close must not see silently truncated
// coalesced payloads (the acceptance criteria's negative criterion).
func TestWindowsURONotEnabledOnCallerSuppliedSocket(t *testing.T) {
	conn, udpConn := newWindowsOwnedConn(t, false)
	require.False(t, conn.capabilities().GRO)
	require.Equal(t, 0, uroSocketOption(t, udpConn))
}

// Kill-switch closure class: QUIC_GO_DISABLE_GRO governs coalesced receive
// on Windows exactly as it does on Linux, and with the switch set the
// socket option is never issued, keeping the W1/W2 receive path byte for
// byte.
func TestWindowsURODisabledByEnv(t *testing.T) {
	t.Setenv("QUIC_GO_DISABLE_GRO", "1")
	conn, udpConn := newWindowsOwnedConn(t, true)
	require.False(t, conn.capabilities().GRO)
	require.Equal(t, 0, uroSocketOption(t, udpConn))
}

// Probe-failure closure class: a probe that cannot interrogate the socket —
// the Control call errors, or the UDP_RECV_MAX_COALESCED_SIZE setsockopt is
// rejected (a Windows build without URO) — must report no capability,
// leaving the receive path on the W1/W2 foundation behavior.
func TestWindowsUROProbeFailure(t *testing.T) {
	// Pin the ambient kill switch off: with QUIC_GO_DISABLE_GRO set in the
	// environment the probe would report false before reaching the socket,
	// and these subtests would pass vacuously.
	t.Setenv("QUIC_GO_DISABLE_GRO", "0")
	t.Run("control error", func(t *testing.T) {
		require.False(t, isUROEnabled(&probeFailingRawConn{controlErr: assert.AnError}))
	})
	t.Run("setsockopt error", func(t *testing.T) {
		require.False(t, isUROEnabled(&probeFailingRawConn{}))
	})
}

// coalescedInfoMsg builds the control message Winsock attaches to a
// coalesced read (ws2def.h WSACMSGHDR: SIZE_T cmsg_len, INT cmsg_level, INT
// cmsg_type, data at WSA_CMSGDATA_ALIGN(sizeof(WSACMSGHDR)); payload one
// DWORD segment size), as a hand-built layout literal independent of the
// parser under test.
func coalescedInfoMsg(t *testing.T, size uint32) []byte {
	t.Helper()
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("layout literal assumes a 64-bit SIZE_T")
	}
	buf := make([]byte, 24)                      // WSA_CMSG_SPACE(4): 16-byte header + 4-byte DWORD, padded to 8
	binary.LittleEndian.PutUint64(buf[0:8], 20)  // cmsg_len: WSA_CMSG_LEN(4), unpadded
	binary.LittleEndian.PutUint32(buf[8:12], 17) // cmsg_level: IPPROTO_UDP
	binary.LittleEndian.PutUint32(buf[12:16], windows.UDP_COALESCED_INFO)
	binary.LittleEndian.PutUint32(buf[16:20], size)
	return buf
}

// ipv4PktInfoMsg builds an IP_PKTINFO control message (in_pktinfo: IN_ADDR
// ipi_addr, ULONG ipi_ifindex) as a hand-built layout literal.
func ipv4PktInfoMsg(t *testing.T, addr [4]byte, ifIndex uint32) []byte {
	t.Helper()
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("layout literal assumes a 64-bit SIZE_T")
	}
	buf := make([]byte, 24)                     // WSA_CMSG_SPACE(8): 16-byte header + 8-byte body
	binary.LittleEndian.PutUint64(buf[0:8], 24) // cmsg_len: WSA_CMSG_LEN(8)
	binary.LittleEndian.PutUint32(buf[8:12], 0) // cmsg_level: IPPROTO_IP
	binary.LittleEndian.PutUint32(buf[12:16], windows.IP_PKTINFO)
	copy(buf[16:20], addr[:])
	binary.LittleEndian.PutUint32(buf[20:24], ifIndex)
	return buf
}

// uroReadConn delivers scripted messages, one per ReadMsgUDP call, the way
// Winsock delivers coalesced reads: payload copied into the posted buffer,
// ancillary data into the posted control buffer. It asserts the conn posts
// a coalesced-tier buffer, so a kernel-coalesced read is never truncated.
type uroReadConn struct {
	*net.UDPConn
	t           *testing.T
	payloads    [][]byte
	oobs        [][]byte
	addr        *net.UDPAddr
	callCounter int
}

func (c *uroReadConn) ReadMsgUDP(b, oob []byte) (n, oobn, flags int, addr *net.UDPAddr, err error) {
	c.t.Helper()
	require.Less(c.t, c.callCounter, len(c.payloads), "unexpected ReadMsgUDP call")
	require.Len(c.t, b, protocol.MaxCoalescedPacketBufferSize, "a URO-enabled conn must post a coalesced-tier buffer")
	payload, oobData := c.payloads[c.callCounter], c.oobs[c.callCounter]
	n = copy(b, payload)
	oobn = copy(oob, oobData)
	c.callCounter++
	return n, oobn, 0, c.addr, nil
}

func newUROConn(t *testing.T, rc *uroReadConn) *windowsConn {
	t.Helper()
	udpConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	require.NoError(t, err)
	t.Cleanup(func() { udpConn.Close() })
	rc.UDPConn = udpConn
	// The scripted reads never touch Winsock, so force the capability
	// instead of depending on the live probe: parsing, splitting, and
	// pending-view coverage must run on URO-unavailable builds too.
	return &windowsConn{
		OOBCapablePacketConn: rc,
		oobBuffer:            make([]byte, oobBufferSize),
		cap:                  connCapabilities{DF: true, GRO: true},
	}
}

func TestWindowsUROReadSplitsCoalescedRead(t *testing.T) {
	segs := testCoalescedSegments(3, 1200)
	segs[2] = segs[2][:500] // short tail
	payload := append(append(append([]byte{}, segs[0]...), segs[1]...), segs[2]...)
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 42), Port: 1234}
	rc := &uroReadConn{t: t, payloads: [][]byte{payload, []byte("next read")}, oobs: [][]byte{coalescedInfoMsg(t, 1200), nil}, addr: addr}
	conn := newUROConn(t, rc)

	var slab *coalescedSlab
	views := make([]*packetBuffer, 3)
	for i, want := range segs {
		p, err := conn.ReadPacket()
		require.NoError(t, err)
		require.Equal(t, want, p.data)
		require.Equal(t, addr, p.remoteAddr)
		require.NotNil(t, p.buffer.slab, "segment %d must be a slab-backed view", i)
		if i == 0 {
			slab = p.buffer.slab
		} else {
			require.Same(t, slab, p.buffer.slab, "siblings share one slab")
		}
		views[i] = p.buffer
	}
	require.Equal(t, 1, rc.callCounter, "all segments must come from one socket read")

	for _, v := range views {
		require.False(t, slab.released())
		v.Release()
	}
	require.True(t, slab.released(), "slab must recycle after the last view releases")

	// the next ReadPacket issues the next read; without a coalesced-info
	// cmsg the datagram is still slab-backed so retention-queue copies stay
	// uniform
	p, err := conn.ReadPacket()
	require.NoError(t, err)
	require.Equal(t, []byte("next read"), p.data)
	require.NotNil(t, p.buffer.slab)
	next := p.buffer.slab
	p.buffer.Release()
	require.True(t, next.released())
	require.Equal(t, 2, rc.callCounter)
}

func TestWindowsUROReadEmptyDatagram(t *testing.T) {
	rc := &uroReadConn{t: t, payloads: [][]byte{{}}, oobs: [][]byte{nil}, addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}}
	conn := newUROConn(t, rc)

	p, err := conn.ReadPacket()
	require.NoError(t, err)
	require.Empty(t, p.data)
	require.NotNil(t, p.buffer)
	require.Nil(t, p.buffer.slab, "an empty read has no segments to view")
	p.buffer.Release()
}

func TestWindowsUROReadPacketInfoInheritance(t *testing.T) {
	segs := testCoalescedSegments(2, 100)
	payload := append(append([]byte{}, segs[0]...), segs[1]...)
	oob := append(coalescedInfoMsg(t, 100), ipv4PktInfoMsg(t, [4]byte{10, 0, 0, 9}, 7)...)
	rc := &uroReadConn{t: t, payloads: [][]byte{payload}, oobs: [][]byte{oob}, addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}}
	conn := newUROConn(t, rc)

	for i := range segs {
		p, err := conn.ReadPacket()
		require.NoError(t, err)
		require.Equal(t, segs[i], p.data)
		require.Equal(t, netip.AddrFrom4([4]byte{10, 0, 0, 9}), p.info.addr, "segment %d must inherit packet info", i)
		require.EqualValues(t, 7, p.info.ifIndex)
		p.buffer.Release()
	}
}

func TestWindowsUROReleaseReadBuffersReleasesPendingViews(t *testing.T) {
	segs := testCoalescedSegments(3, 800)
	payload := append(append(append([]byte{}, segs[0]...), segs[1]...), segs[2]...)
	rc := &uroReadConn{t: t, payloads: [][]byte{payload}, oobs: [][]byte{coalescedInfoMsg(t, 800)}, addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}}
	conn := newUROConn(t, rc)

	p, err := conn.ReadPacket()
	require.NoError(t, err)
	slab := p.buffer.slab
	require.NotNil(t, slab)

	// reading stops with two undelivered sibling views pending
	conn.releaseReadBuffers()
	require.False(t, slab.released(), "the delivered view still holds the slab")
	p.buffer.Release()
	require.True(t, slab.released())
}

// TestWindowsUSOUROInteractionMatrix validates all four USO/URO offload
// combinations for payload and ancillary metadata (the W track's
// second-lander obligation): every datagram of a burst arrives intact and in
// order, packet info reports the correct destination address on a
// wildcard-bound socket, coalesced reads split into slab-backed views only
// when URO is on, and ECN stays unsupported on the Windows conn in every
// combination (the Server 2022 ancillary policy keeps TTL/DSCP receive
// features disabled while offloads are active; the conn requests neither).
func TestWindowsUSOUROInteractionMatrix(t *testing.T) {
	const segSize, numSegs = 1200, 8
	for _, tc := range []struct{ uso, uro bool }{
		{true, true},
		{true, false},
		{false, true},
		{false, false},
	} {
		name := fmt.Sprintf("uso=%v,uro=%v", tc.uso, tc.uro)
		t.Run(name, func(t *testing.T) {
			if !tc.uso {
				t.Setenv("QUIC_GO_DISABLE_GSO", "1")
			}
			if !tc.uro {
				t.Setenv("QUIC_GO_DISABLE_GRO", "1")
			}

			// A wildcard bind activates packet-info parsing, so the matrix
			// validates the destination-address metadata alongside payload.
			recvUDP, err := net.ListenUDP("udp4", &net.UDPAddr{Port: 0})
			require.NoError(t, err)
			defer recvUDP.Close()
			recvConn, err := newConn(recvUDP, true, true)
			require.NoError(t, err)
			sendUDP, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
			require.NoError(t, err)
			defer sendUDP.Close()
			sendConn, err := newConn(sendUDP, true, true)
			require.NoError(t, err)

			require.Equal(t, tc.uro, recvConn.capabilities().GRO, "URO capability must match the combination")
			require.Equal(t, tc.uso, sendConn.capabilities().GSO, "USO capability must match the combination")
			if tc.uro {
				requireUROCapableHost(t, recvConn)
			}
			if tc.uso {
				requireUSOCapableHost(t, sendConn)
			}

			dest := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: recvUDP.LocalAddr().(*net.UDPAddr).Port}
			segs := testCoalescedSegments(numSegs, segSize)
			if tc.uso {
				var batch []byte
				for _, seg := range segs {
					batch = append(batch, seg...)
				}
				_, err = sendConn.WritePacket(batch, dest, nil, segSize, protocol.ECNUnsupported)
				require.NoError(t, err)
			} else {
				for _, seg := range segs {
					_, err = sendConn.WritePacket(seg, dest, nil, 0, protocol.ECNUnsupported)
					require.NoError(t, err)
				}
			}

			require.NoError(t, recvUDP.SetReadDeadline(time.Now().Add(scaleDuration(5*time.Second))))
			for i := range numSegs {
				p, err := recvConn.ReadPacket()
				require.NoError(t, err)
				require.Equal(t, segs[i], p.data, "datagram %d must arrive intact and in order", i)
				require.Equal(t, netip.AddrFrom4([4]byte{127, 0, 0, 1}), p.info.addr, "datagram %d must carry the destination packet info", i)
				require.Equal(t, protocol.ECNUnsupported, p.ecn, "the Windows conn requests no ECN ancillary data")
				if tc.uro {
					require.NotNil(t, p.buffer.slab, "URO-on reads are slab-backed views")
				} else {
					require.Nil(t, p.buffer.slab, "URO-off reads keep the W1 foundation storage")
				}
				p.buffer.Release()
			}
		})
	}
}

// TestWindowsUROEndToEndLoopback sends one USO batch across loopback into a
// URO-enabled socket and verifies every datagram arrives intact and in
// order, whether or not Winsock coalesced them. Engagement (several
// datagrams per read) is logged, not asserted: the adoption protocol owns
// the engagement evidence.
func TestWindowsUROEndToEndLoopback(t *testing.T) {
	recvUDP, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	require.NoError(t, err)
	defer recvUDP.Close()
	recvConn, err := newConn(recvUDP, true, true)
	require.NoError(t, err)
	requireUROCapableHost(t, recvConn)

	sendUDP, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	require.NoError(t, err)
	defer sendUDP.Close()
	sendConn, err := newConn(sendUDP, true, true)
	require.NoError(t, err)
	requireUSOCapableHost(t, sendConn)

	const segSize, numSegs = 1200, 8
	segs := testCoalescedSegments(numSegs, segSize)
	var batch []byte
	for _, seg := range segs {
		batch = append(batch, seg...)
	}
	_, err = sendConn.WritePacket(batch, recvUDP.LocalAddr(), nil, segSize, protocol.ECNUnsupported)
	require.NoError(t, err)

	require.NoError(t, recvUDP.SetReadDeadline(time.Now().Add(scaleDuration(5*time.Second))))
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
