//go:build windows

package quic

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"net/netip"
	"os"
	"syscall"
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
// UDP_COALESCED_INFO parsing, the copy of each datagram of a coalesced read
// into its own ordinary buffer (#860), and the guard against long reads
// without their segment-size record.

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
// on an unregistered caller-supplied socket: coalescing is socket-wide, and reads the
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
		enabled, reason := enableURO(&probeFailingRawConn{controlErr: assert.AnError})
		require.False(t, enabled)
		require.Equal(t, "activation_failed_or_unavailable", reason)
	})
	t.Run("setsockopt error", func(t *testing.T) {
		enabled, reason := enableURO(&probeFailingRawConn{})
		require.False(t, enabled)
		require.Equal(t, "activation_failed_or_unavailable", reason)
	})
}

// Error-classification fixture: no URO-incapable Windows build exists in the
// qualified measurement surfaces, so the error such a build returns from the
// UDP_RECV_MAX_COALESCED_SIZE setsockopt — WSAENOPROTOOPT — is injected here
// instead. The probe's contract is uniform classification: WSAENOPROTOOPT
// gets no special handling over any other setsockopt failure, every failure
// reports "capability off", and only success enables the capability.
func TestWindowsUROProbeErrorClassification(t *testing.T) {
	// Pin the ambient kill switch off so the probe reaches the socket path.
	t.Setenv("QUIC_GO_DISABLE_GRO", "0")
	for _, tc := range []struct {
		name    string
		serr    error
		enabled bool
	}{
		{"WSAENOPROTOOPT reports capability off", windows.WSAENOPROTOOPT, false},
		{"any other failure classifies the same", assert.AnError, false},
		{"success enables the capability", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// With no controlErr set, probeFailingRawConn's Control succeeds;
			// the injected setter ignores the fd it passes.
			conn := &probeFailingRawConn{}
			var setterCalled bool
			enabled, reason := enableUROWith(conn, func(uintptr) error {
				setterCalled = true
				return tc.serr
			})
			require.Equal(t, tc.enabled, enabled)
			if tc.enabled {
				require.Equal(t, "none", reason)
			} else {
				require.Equal(t, "activation_failed_or_unavailable", reason)
			}
			require.True(t, conn.controlCalled, "the probe must reach Control")
			require.True(t, setterCalled, "the probe must issue the setsockopt")
		})
	}
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
// With socketAfterScript, reads past the script go to the real socket.
type uroReadConn struct {
	*net.UDPConn
	t                 *testing.T
	payloads          [][]byte
	oobs              [][]byte
	flags             []int
	errs              []error
	addr              *net.UDPAddr
	callCounter       int
	socketAfterScript bool
	// syscallErr, when set, fails SyscallConn, as on a socket whose
	// descriptor can no longer be reached.
	syscallErr   error
	syscallCalls int
}

func (c *uroReadConn) SyscallConn() (syscall.RawConn, error) {
	c.syscallCalls++
	if c.syscallErr != nil {
		return nil, c.syscallErr
	}
	return c.UDPConn.SyscallConn()
}

func (c *uroReadConn) ReadMsgUDP(b, oob []byte) (n, oobn, flags int, addr *net.UDPAddr, err error) {
	if c.socketAfterScript && c.callCounter >= len(c.payloads) {
		return c.UDPConn.ReadMsgUDP(b, oob)
	}
	c.t.Helper()
	require.Less(c.t, c.callCounter, len(c.payloads), "unexpected ReadMsgUDP call")
	require.Len(c.t, b, protocol.MaxCoalescedPacketBufferSize, "a URO-enabled conn must post a coalesced-tier buffer")
	payload, oobData := c.payloads[c.callCounter], c.oobs[c.callCounter]
	n = copy(b, payload)
	oobn = copy(oob, oobData)
	if len(c.flags) > c.callCounter {
		flags = c.flags[c.callCounter]
	}
	if len(c.errs) > c.callCounter {
		err = c.errs[c.callCounter]
	}
	c.callCounter++
	return n, oobn, flags, c.addr, err
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

// requireCopiedPacket asserts that p is delivered in a pooled buffer of the
// given capacity, never in the reader's coalesced-tier buffer.
func requireCopiedPacket(t *testing.T, p receivedPacket, capacity int) {
	t.Helper()
	require.NotNil(t, p.buffer)
	require.Equal(t, capacity, cap(p.buffer.Data))
}

func TestWindowsUROReadSplitsCoalescedRead(t *testing.T) {
	segs := testCoalescedSegments(3, 1200)
	segs[2] = segs[2][:500] // short tail
	payload := append(append(append([]byte{}, segs[0]...), segs[1]...), segs[2]...)
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 42), Port: 1234}
	rc := &uroReadConn{t: t, payloads: [][]byte{payload, []byte("next read")}, oobs: [][]byte{coalescedInfoMsg(t, 1200), nil}, addr: addr}
	conn := newUROConn(t, rc)

	packets := make([]receivedPacket, 3)
	for i, want := range segs {
		p, err := conn.ReadPacket()
		require.NoError(t, err)
		require.Equal(t, want, p.data)
		require.Equal(t, addr, p.remoteAddr)
		requireCopiedPacket(t, p, protocol.MaxPacketBufferSize)
		packets[i] = p
	}
	require.Equal(t, 1, rc.callCounter, "all datagrams must come from one socket read")

	// The next read reuses the reader's buffer. The delivered datagrams are
	// copies, so it doesn't change them, and they release independently.
	p, err := conn.ReadPacket()
	require.NoError(t, err)
	require.Equal(t, []byte("next read"), p.data)
	requireCopiedPacket(t, p, protocol.MaxPacketBufferSize)
	require.Equal(t, 2, rc.callCounter)
	for i, packet := range packets {
		require.Equal(t, segs[i], packet.data, "datagram %d must not alias the reader's buffer", i)
		packet.buffer.Release()
	}
	p.buffer.Release()
	require.Zero(t, conn.receiveCoalescingFallbackCount().Load())
}

func TestWindowsUROReadEmptyDatagram(t *testing.T) {
	rc := &uroReadConn{t: t, payloads: [][]byte{{}}, oobs: [][]byte{nil}, addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}}
	conn := newUROConn(t, rc)

	p, err := conn.ReadPacket()
	require.NoError(t, err)
	require.Empty(t, p.data)
	requireCopiedPacket(t, p, protocol.MaxPacketBufferSize)
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

// A reader stop leaves nothing pending: the undelivered datagrams of the
// read are discarded with the reader's buffer, and a delivered datagram stays
// valid because it is a copy.
func TestWindowsUROReleaseReadBuffersDiscardsUndelivered(t *testing.T) {
	segs := testCoalescedSegments(3, 800)
	payload := append(append(append([]byte{}, segs[0]...), segs[1]...), segs[2]...)
	rc := &uroReadConn{t: t, payloads: [][]byte{payload, []byte("after stop")}, oobs: [][]byte{coalescedInfoMsg(t, 800), nil}, addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}}
	conn := newUROConn(t, rc)

	p, err := conn.ReadPacket()
	require.NoError(t, err)
	require.Equal(t, segs[0], p.data)

	// reading stops with two undelivered datagrams in the reader's buffer
	conn.releaseReadBuffers()
	require.Nil(t, conn.readBuffer, "the reader's buffer is returned")
	require.Equal(t, segs[0], p.data)
	p.buffer.Release()

	next, err := conn.ReadPacket()
	require.NoError(t, err)
	require.Equal(t, []byte("after stop"), next.data, "nothing from the stopped read is delivered")
	require.Equal(t, 2, rc.callCounter)
	next.buffer.Release()
	conn.releaseReadBuffers()
}

// newUROSocketConn is newUROConn on a socket with URO actually enabled, so
// the guard's socket option change can be read back.
func newUROSocketConn(t *testing.T, rc *uroReadConn) *windowsConn {
	t.Helper()
	conn := newUROConn(t, rc)
	t.Setenv("QUIC_GO_DISABLE_GRO", "0")
	raw, err := rc.SyscallConn()
	require.NoError(t, err)
	conn.cap.GRO, _ = enableURO(raw)
	requireUROCapableHost(t, conn)
	require.Equal(t, protocol.MaxCoalescedPacketBufferSize, uroSocketOption(t, rc.UDPConn))
	return conn
}

// A read longer than any packet without its UDP_COALESCED_INFO record is how
// some Windows hosts deliver coalesced reads (quinn-rs/quinn#2041). The guard
// turns URO off for the socket's life, counts the event once and drops the
// read. After that, a long read without its record is one datagram, copied
// out as copyCoalescedDatagram copies any datagram (this one, under 20 KiB,
// whole into the large tier), and a read with its record is still split.
func TestWindowsUROGuardLongReadWithoutRecord(t *testing.T) {
	long := bytes.Repeat([]byte{7}, 3*1200)
	segs := testCoalescedSegments(2, 1200)
	rc := &uroReadConn{
		t:        t,
		payloads: [][]byte{long, []byte("next"), append(append([]byte{}, segs[0]...), segs[1]...), long},
		oobs:     [][]byte{nil, nil, coalescedInfoMsg(t, 1200), nil},
		addr:     &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1},
	}
	conn := newUROSocketConn(t, rc)

	p, err := conn.ReadPacket()
	require.NoError(t, err)
	require.Equal(t, []byte("next"), p.data, "the long read is dropped")
	require.Equal(t, 2, rc.callCounter)
	p.buffer.Release()
	require.EqualValues(t, 1, conn.receiveCoalescingFallbackCount().Load())
	require.Zero(t, uroSocketOption(t, rc.UDPConn), "URO is turned off on the socket")

	// A read that still carries its record is split.
	for _, seg := range segs {
		p, err := conn.ReadPacket()
		require.NoError(t, err)
		require.Equal(t, seg, p.data)
		p.buffer.Release()
	}
	// The guard doesn't trip again: a long read without its record is one
	// datagram.
	p, err = conn.ReadPacket()
	require.NoError(t, err)
	require.Equal(t, long, p.data)
	requireCopiedPacket(t, p, protocol.MaxLargePacketBufferSize)
	p.buffer.Release()
	require.EqualValues(t, 1, conn.receiveCoalescingFallbackCount().Load(), "counted once")
	conn.releaseReadBuffers()
}

// If URO cannot be turned off, it may still be on: the guard stays armed and
// keeps dropping long reads without their record instead of delivering one as
// a datagram. It counts and tries to turn URO off only once.
func TestWindowsUROGuardTurnOffFails(t *testing.T) {
	long := bytes.Repeat([]byte{7}, 3*1200)
	segs := testCoalescedSegments(2, 1200)
	rc := &uroReadConn{
		t:          t,
		payloads:   [][]byte{long, []byte("a"), long, append(append([]byte{}, segs[0]...), segs[1]...), []byte("b")},
		oobs:       [][]byte{nil, nil, nil, coalescedInfoMsg(t, 1200), nil},
		addr:       &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1},
		syscallErr: assert.AnError,
	}
	conn := newUROConn(t, rc)
	t.Cleanup(conn.releaseReadBuffers)
	for _, want := range [][]byte{[]byte("a"), segs[0], segs[1], []byte("b")} {
		p, err := conn.ReadPacket()
		require.NoError(t, err)
		require.Equal(t, want, p.data)
		p.buffer.Release()
	}
	require.Equal(t, 5, rc.callCounter, "both long reads are dropped")
	require.EqualValues(t, 1, conn.receiveCoalescingFallbackCount().Load(), "counted once")
	require.Equal(t, 1, rc.syscallCalls, "URO is turned off at most once")
}

// Reads with their record, and reads of at most one packet without it, leave
// URO on and the count at zero.
func TestWindowsUROGuardIgnoresRecordedAndShortReads(t *testing.T) {
	segs := testCoalescedSegments(3, 1200)
	full := bytes.Repeat([]byte{9}, protocol.MaxPacketBufferSize)
	rc := &uroReadConn{
		t:        t,
		payloads: [][]byte{append(append(append([]byte{}, segs[0]...), segs[1]...), segs[2]...), full, []byte("short")},
		oobs:     [][]byte{coalescedInfoMsg(t, 1200), nil, nil},
		addr:     &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1},
	}
	conn := newUROSocketConn(t, rc)
	for _, want := range append(segs, full, []byte("short")) {
		p, err := conn.ReadPacket()
		require.NoError(t, err)
		require.Equal(t, want, p.data)
		requireCopiedPacket(t, p, protocol.MaxPacketBufferSize)
		p.buffer.Release()
	}
	require.Zero(t, conn.receiveCoalescingFallbackCount().Load())
	require.Equal(t, protocol.MaxCoalescedPacketBufferSize, uroSocketOption(t, rc.UDPConn), "URO stays on")
	conn.releaseReadBuffers()
}

// A transport reports its reader's count, found by interface assertion.
func TestWindowsUROGuardCountOnTransport(t *testing.T) {
	rc := &uroReadConn{
		t:                 t,
		payloads:          [][]byte{bytes.Repeat([]byte{7}, 3000)},
		oobs:              [][]byte{nil},
		addr:              &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1},
		socketAfterScript: true,
	}
	conn := newUROSocketConn(t, rc)
	tr := &Transport{Conn: conn}
	t.Cleanup(func() { tr.Close() })
	var v any = tr
	counter, ok := v.(interface{ ReceiveCoalescingFallbacksV1() uint64 })
	require.True(t, ok)
	require.Zero(t, counter.ReceiveCoalescingFallbacksV1())
	// initialize the transport, starting its reader
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := tr.ReadNonQUICPacket(ctx, nil)
	require.ErrorIs(t, err, context.Canceled)
	require.Eventually(t, func() bool { return counter.ReceiveCoalescingFallbacksV1() == 1 }, scaleDuration(5*time.Second), time.Millisecond)
}

// TestWindowsUSOUROInteractionMatrix validates all four USO/URO offload
// combinations for payload and ancillary metadata (the W track's
// second-lander obligation): every datagram of a burst arrives intact and in
// order, packet info reports the correct destination address on a
// wildcard-bound socket, every datagram arrives in its own ordinary buffer
// whether or not URO is on, and ECN stays unsupported on the Windows conn in every
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

			// For a combination that expects a capability on, the guarded
			// host helper decides skip (off-CI host without the offload) vs.
			// fail (a CI runner must be capable) before any enabled-state
			// assertion, so an incapable dev host skips loudly rather than
			// failing an equality check. For a combination that expects it
			// off (kill switch or caller-supplied), the disabled state must
			// always hold regardless of host.
			if tc.uro {
				requireUROCapableHost(t, recvConn)
			} else {
				require.False(t, recvConn.capabilities().GRO, "URO must be off for this combination")
			}
			if tc.uso {
				requireUSOCapableHost(t, sendConn)
			} else {
				require.False(t, sendConn.capabilities().GSO, "USO must be off for this combination")
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
			t.Cleanup(recvConn.releaseReadBuffers)
			for i := range numSegs {
				p, err := recvConn.ReadPacket()
				require.NoError(t, err)
				require.Equal(t, segs[i], p.data, "datagram %d must arrive intact and in order", i)
				require.Equal(t, netip.AddrFrom4([4]byte{127, 0, 0, 1}), p.info.addr, "datagram %d must carry the destination packet info", i)
				require.Equal(t, protocol.ECNUnsupported, p.ecn, "the Windows conn requests no ECN ancillary data")
				requireCopiedPacket(t, p, protocol.MaxPacketBufferSize)
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
	reads := &countingReadConn{UDPConn: recvUDP}
	recvConn, err := newConn(reads, true, true)
	require.NoError(t, err)
	requireUROCapableHost(t, recvConn)
	t.Cleanup(recvConn.releaseReadBuffers)

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
	for i := range numSegs {
		p, err := recvConn.ReadPacket()
		require.NoError(t, err)
		require.Equal(t, segs[i], p.data, "datagram %d must arrive intact and in order", i)
		requireCopiedPacket(t, p, protocol.MaxPacketBufferSize)
		p.buffer.Release()
	}
	t.Logf("%d datagrams in %d socket reads", numSegs, reads.reads)
}

// countingReadConn counts socket reads. Only the reader calls ReadMsgUDP.
type countingReadConn struct {
	*net.UDPConn
	reads int
}

func (c *countingReadConn) ReadMsgUDP(b, oob []byte) (int, int, int, *net.UDPAddr, error) {
	c.reads++
	return c.UDPConn.ReadMsgUDP(b, oob)
}
