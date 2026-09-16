//go:build windows

package quic

import (
	"encoding/binary"
	"net"
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
	}{
		{name: "ordinary"},
		{name: "registered without permission", registered: true},
		{name: "permitted native", registered: true, permitted: true},
		{name: "permitted wrapper", registered: true, permitted: true, wrapped: true},
		{name: "disabled wrapper", registered: true, permitted: true, disabled: true, wrapped: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("QUIC_GO_DISABLE_GRO", "0")
			if tc.disabled {
				t.Setenv("QUIC_GO_DISABLE_GRO", "1")
			}
			udp := listenExternalUDP(t)
			var conn net.PacketConn = udp
			if tc.wrapped {
				conn = &struct{ *net.UDPConn }{udp}
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
			if tc.permitted && !tc.disabled {
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
				} else if tc.disabled {
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
	}{
		{name: "zero segment", oob: coalescedInfoMsg(t, 0)},
		{name: "segment beyond payload", oob: coalescedInfoMsg(t, 201)},
		{name: "short segment metadata", oob: short},
		{name: "duplicate segment metadata", oob: append(coalescedInfoMsg(t, 100), coalescedInfoMsg(t, 50)...)},
		{name: "truncated payload", oob: coalescedInfoMsg(t, 100), flags: windows.MSG_TRUNC},
		{name: "truncated control", flags: windows.MSG_CTRUNC},
		{name: "malformed control", oob: []byte{1}},
		{name: "malformed trailing control", oob: append(coalescedInfoMsg(t, 100), 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rc := &uroReadConn{t: t, payloads: [][]byte{make([]byte, 200), []byte("fresh")}, oobs: [][]byte{tc.oob, nil}, flags: []int{tc.flags, 0}, addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}}
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
