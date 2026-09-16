//go:build windows

package quic

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/testutils/events"

	"github.com/stretchr/testify/require"
)

func TestWindowsExternalReceivePermission(t *testing.T) {
	for _, tc := range []struct {
		name       string
		registered bool
		permitted  bool
		disabled   bool
		wrapped    bool
		opaque     bool
	}{
		{name: "ordinary"},
		{name: "opaque permitted wrapper", registered: true, permitted: true, opaque: true},
		{name: "registered without permission", registered: true},
		{name: "permitted native", registered: true, permitted: true},
		{name: "permitted wrapper", registered: true, permitted: true, wrapped: true},
		{name: "disabled wrapper", registered: true, permitted: true, disabled: true, wrapped: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("QUIC_GO_DISABLE_GRO", "0")
			if tc.permitted && !tc.disabled && !tc.opaque {
				probe, _ := newWindowsOwnedConn(t, true)
				requireUROCapableHost(t, probe)
			}
			if tc.disabled {
				t.Setenv("QUIC_GO_DISABLE_GRO", "1")
			}
			udp := listenExternalUDP(t)
			var conn net.PacketConn = udp
			if tc.wrapped {
				conn = &struct{ *net.UDPConn }{udp}
			}
			if tc.opaque {
				conn = &nonOOBPacketConn{PacketConn: udp}
			}
			var recorder events.Recorder
			tr := &Transport{Conn: conn, Tracer: &recorder}
			if tc.registered {
				require.NoError(t, tr.ConfigureExternalPacketIOV1(conn, tc.permitted, nil))
			}
			// Close initializes the transport, exercising the real binding and
			// platform setup and joining the transport reader before readback.
			require.NoError(t, tr.Close())
			want := 0
			if tc.permitted && !tc.disabled && !tc.opaque {
				want = protocol.MaxCoalescedPacketBufferSize
			}
			require.Equal(t, want, uroSocketOption(t, udp))
			require.NoError(t, udp.SetReadDeadline(time.Time{}), "permission must not transfer Close ownership")
			if tc.registered {
				recorded := recorder.Events(qlog.DebugEvent{})
				require.Len(t, recorded, 1)
				message := recorded[0].(qlog.DebugEvent).Message
				if want != 0 {
					require.Contains(t, message, "receive_supported=true receive_enabled=true")
				} else if tc.disabled || tc.opaque {
					require.Contains(t, message, "receive_disabled_reason=disabled_or_unavailable")
				} else {
					require.Contains(t, message, "receive_disabled_reason=no_permission")
				}
			}
		})
	}
}

// Invalid receive metadata must discard the entire read before any sibling is
// exposed, then allow the next ordinary datagram through the same reader.
func TestWindowsExternalURORejectsInvalidRead(t *testing.T) {
	short := coalescedInfoMsg(t, 100)
	binary.LittleEndian.PutUint64(short[:8], 18)
	for _, tc := range []struct {
		name  string
		oob   []byte
		flags int
		err   error
	}{
		{name: "zero segment", oob: coalescedInfoMsg(t, 0)},
		{name: "segment beyond payload", oob: coalescedInfoMsg(t, 201)},
		{name: "short segment metadata", oob: short},
		{name: "duplicate segment metadata", oob: append(coalescedInfoMsg(t, 100), coalescedInfoMsg(t, 50)...)},
		{name: "truncated payload", oob: coalescedInfoMsg(t, 100), flags: windows.MSG_TRUNC},
		{name: "truncated control", flags: windows.MSG_CTRUNC},
		{name: "Winsock truncated control error", flags: windows.MSG_CTRUNC, err: &net.OpError{Op: "read", Net: "udp", Err: windows.WSAEMSGSIZE}},
		{name: "malformed control", oob: []byte{1}},
		{name: "malformed trailing control", oob: append(coalescedInfoMsg(t, 100), 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rc := &uroReadConn{t: t, payloads: [][]byte{make([]byte, 200), []byte("fresh")}, oobs: [][]byte{tc.oob, nil}, flags: []int{tc.flags, 0}, errs: []error{tc.err, nil}, addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}}
			conn := newUROConn(t, rc)
			t.Cleanup(conn.releaseReadBuffers)
			p, err := conn.ReadPacket()
			require.NoError(t, err)
			defer p.buffer.Release()
			require.Equal(t, "fresh", string(p.data))
			require.Equal(t, 2, rc.callCounter)
		})
	}
}

// The wrapper remains the selected-peer owner even with URO permission.
// Loopback validates this policy and lifecycle, not native URO engagement.
type externalUROFilter struct {
	*net.UDPConn
	peer       *net.UDPAddr
	filtered   atomic.Int32
	aggregates atomic.Int32
}

func (c *externalUROFilter) ReadMsgUDP(b, oob []byte) (int, int, int, *net.UDPAddr, error) {
	for {
		n, nn, flags, addr, err := c.UDPConn.ReadMsgUDP(b, oob)
		if err != nil {
			return n, nn, flags, addr, err
		}
		if addr.String() == c.peer.String() {
			if n > 1232 {
				c.aggregates.Add(1)
			}
			return n, nn, flags, addr, nil
		}
		c.filtered.Add(1)
	}
}

func TestWindowsExternalUROWrapperLifecycle(t *testing.T) {
	for _, network := range []string{"udp4", "udp6"} {
		t.Run(network, func(t *testing.T) {
			t.Setenv("QUIC_GO_DISABLE_GRO", "0")
			probe, _ := newWindowsOwnedConn(t, true)
			requireUROCapableHost(t, probe)
			listen := func() *net.UDPConn {
				t.Helper()
				ip := net.IPv4(127, 0, 0, 1)
				if network == "udp6" {
					ip = net.IPv6loopback
				}
				c, err := net.ListenUDP(network, &net.UDPAddr{IP: ip})
				require.NoError(t, err)
				t.Cleanup(func() { c.Close() })
				return c
			}
			udp, selected, foreign := listen(), listen(), listen()
			wrapper := &externalUROFilter{UDPConn: udp, peer: selected.LocalAddr().(*net.UDPAddr)}
			tr := &Transport{Conn: wrapper}
			require.NoError(t, tr.ConfigureExternalPacketIOV1(wrapper, true, nil))
			t.Cleanup(func() { require.NoError(t, tr.Close()) })
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, _, err := tr.ReadNonQUICPacket(ctx, make([]byte, 1500))
			require.ErrorIs(t, err, context.Canceled)
			require.Equal(t, protocol.MaxCoalescedPacketBufferSize, uroSocketOption(t, udp))
			// Finish observing the rejected read before sending the selected data.
			_, err = foreign.WriteToUDP([]byte{9}, udp.LocalAddr().(*net.UDPAddr))
			require.NoError(t, err)
			require.Eventually(t, func() bool { return wrapper.filtered.Load() > 0 }, scaleDuration(5*time.Second), time.Millisecond)
			want := bytes.Repeat([]byte{1}, 1232)
			_, err = selected.WriteToUDP(want, udp.LocalAddr().(*net.UDPAddr))
			require.NoError(t, err)
			ctx, cancel = context.WithTimeout(context.Background(), scaleDuration(5*time.Second))
			defer cancel()
			b := make([]byte, 1500)
			n, addr, err := tr.ReadNonQUICPacket(ctx, b)
			require.NoError(t, err)
			require.Equal(t, want, b[:n])
			require.Equal(t, selected.LocalAddr().String(), addr.String())
			require.NoError(t, tr.Close())
			require.NoError(t, udp.SetReadDeadline(time.Time{}))
		})
	}
}

func TestWindowsExternalUROSetupFailureRemainsCallerOwned(t *testing.T) {
	t.Setenv("QUIC_GO_DISABLE_GRO", "0")
	probe, _ := newWindowsOwnedConn(t, true)
	requireUROCapableHost(t, probe)
	udp := listenExternalUDP(t)
	tr := &Transport{Conn: udp}
	require.NoError(t, tr.ConfigureExternalPacketIOV1(udp, true, nil))
	_, err := tr.Dial(context.Background(), udp.LocalAddr(), &tls.Config{}, &Config{Versions: []Version{0xdeadbeef}})
	require.ErrorContains(t, err, "invalid QUIC version")
	require.Equal(t, protocol.MaxCoalescedPacketBufferSize, uroSocketOption(t, udp))
	require.NoError(t, tr.Close())
	require.NoError(t, udp.SetReadDeadline(time.Time{}))
	require.NoError(t, udp.Close())
	require.ErrorIs(t, udp.SetReadDeadline(time.Time{}), net.ErrClosed)
}

func TestWindowsExternalUROReadErrorDoesNotReplay(t *testing.T) {
	want := errors.New("partial message receive")
	rc := &uroReadConn{t: t, payloads: [][]byte{make([]byte, 200), []byte("fresh")}, oobs: [][]byte{coalescedInfoMsg(t, 100), nil}, errs: []error{want, nil}, addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}}
	conn := newUROConn(t, rc)
	t.Cleanup(conn.releaseReadBuffers)
	p, err := conn.ReadPacket()
	require.ErrorIs(t, err, want)
	require.Nil(t, p.buffer)
	p, err = conn.ReadPacket()
	require.NoError(t, err)
	defer p.buffer.Release()
	require.Equal(t, "fresh", string(p.data))
	require.Equal(t, 2, rc.callCounter)
}

// Run only against a separate native endpoint. The peer receives "Q04 URO"
// and replies from the same UDP socket with one segmented burst: 32 datagrams,
// each filled with its 1-based index, 1232 bytes each except a 500-byte tail.
// This is an engagement regression, not a performance measurement.
func TestWindowsExternalURONativeEngagement(t *testing.T) {
	peerAddress := os.Getenv("QUIC_GO_Q04_URO_PEER")
	if peerAddress == "" {
		t.Skip("requires a separate endpoint via QUIC_GO_Q04_URO_PEER")
	}
	localAddress := os.Getenv("QUIC_GO_Q04_URO_LOCAL")
	if localAddress == "" {
		localAddress = "0.0.0.0:0"
	}
	peer, err := net.ResolveUDPAddr("udp4", peerAddress)
	require.NoError(t, err)
	local, err := net.ResolveUDPAddr("udp4", localAddress)
	require.NoError(t, err)
	udp, err := net.ListenUDP("udp4", local)
	require.NoError(t, err)
	defer udp.Close()
	t.Setenv("QUIC_GO_DISABLE_GRO", "0")
	wrapper := &externalUROFilter{UDPConn: udp, peer: peer}
	tr := &Transport{Conn: wrapper}
	require.NoError(t, tr.ConfigureExternalPacketIOV1(wrapper, true, nil))
	defer tr.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = tr.ReadNonQUICPacket(ctx, make([]byte, 1500))
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, protocol.MaxCoalescedPacketBufferSize, uroSocketOption(t, udp))
	_, err = tr.WriteTo([]byte("Q04 URO"), peer)
	require.NoError(t, err)
	ctx, cancel = context.WithTimeout(context.Background(), scaleDuration(10*time.Second))
	defer cancel()
	for i := 1; i <= 32; i++ {
		size := 1232
		if i == 32 {
			size = 500
		}
		b := make([]byte, 1500)
		n, addr, err := tr.ReadNonQUICPacket(ctx, b)
		require.NoError(t, err)
		require.Equal(t, peer.String(), addr.String())
		require.Equal(t, bytes.Repeat([]byte{byte(i)}, size), b[:n])
	}
	require.Positive(t, wrapper.aggregates.Load(), "separate-endpoint kernel coalescing must actually engage")
	t.Logf("external Windows wrapper: %d coalesced read(s); 32 exact datagrams delivered", wrapper.aggregates.Load())
}
