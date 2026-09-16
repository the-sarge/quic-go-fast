//go:build windows

package quic

import (
	"net"
	"testing"
	"time"

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
			// platform setup without starting a competing receive operation.
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
