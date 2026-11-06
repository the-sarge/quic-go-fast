//go:build linux

package quic

import (
	"net"
	"os"
	"testing"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"golang.org/x/sys/unix"
)

// Linux v6.12 udp_send_skb returns EINVAL for a segment above the path MTU,
// as well as for unrelated invalid arguments. Only a scalar retry resolves it.
// https://github.com/torvalds/linux/blob/v6.12/net/ipv4/udp.c#L875
func TestJumboGSOInvalidArgumentAttribution(t *testing.T) {
	for _, tc := range []struct {
		name    string
		segment uint16
		scalar  error
		calls   int
		refused bool
		fatal   error
		wantGSO bool
	}{
		{"oversized segment", 2000, unix.EMSGSIZE, 2, true, nil, true},
		{"segmentation unsupported", 2000, nil, 3, false, nil, false},
		{"unrelated scalar failure", 2000, unix.EINVAL, 2, false, unix.EINVAL, true},
		{"standard unchanged", 1400, nil, 1, false, unix.EINVAL, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := NewMockRawConn(gomock.NewController(t))
			raw.EXPECT().LocalAddr().Return(&net.UDPAddr{})
			raw.EXPECT().capabilities().Return(connCapabilities{DF: true, GSO: true}).AnyTimes()
			calls := 0
			raw.EXPECT().WritePacket(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), protocol.ECNNon).DoAndReturn(func(b []byte, _ net.Addr, _ []byte, gso uint16, _ protocol.ECN) (int, error) {
				calls++
				if gso != 0 {
					return 0, &os.SyscallError{Syscall: "sendmsg", Err: unix.EINVAL}
				}
				return len(b), tc.scalar
			}).Times(tc.calls)
			conn := newSendConn(raw, &net.UDPAddr{}, packetInfo{}, utils.DefaultLogger)
			feedback := &packetSizeFeedback{wakeup: make(chan struct{}, 1)}
			q := newSendQueue(conn, nil).(*sendQueue)
			q.refusals = feedback
			buf := getPacketBufferForCeiling(8972)
			buf.Data = buf.Data[:2*int(tc.segment)]
			q.Send(buf, tc.segment, protocol.ECNNon, sendMetadata{pathGeneration: 7})
			close(q.closeCalled)
			err := q.Run()
			if tc.fatal == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.fatal)
			}
			require.Equal(t, tc.wantGSO, conn.capabilities().GSO)
			require.Equal(t, tc.calls, calls)
			generation, size, pending := feedback.take()
			require.Equal(t, tc.refused, pending)
			if pending {
				require.EqualValues(t, 7, generation)
				require.EqualValues(t, tc.segment, size)
			}
		})
	}
}
