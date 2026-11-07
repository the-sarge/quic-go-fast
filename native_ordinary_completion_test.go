//go:build darwin || linux || freebsd

package quic

import (
	"errors"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

func TestNativeOrdinaryAcceptedCompletion(t *testing.T) {
	for _, kind := range []string{"accepted", "partial", "error", "MTU", "coalesced"} {
		t.Run(kind, func(t *testing.T) {
			h, err := ackhandler.NewLocalSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, false, nil, protocol.PerspectiveServer, nil, utils.DefaultLogger)
			require.NoError(t, err)
			now := monotime.Time(time.Second)
			h.DropPackets(protocol.EncryptionInitial, now)
			h.DropPackets(protocol.EncryptionHandshake, now)
			packets := make([]ordinaryLocalPacket, 3)
			for i := range packets {
				pn := h.PopPacketNumber(protocol.Encryption1RTT)
				packets[i].token, err = h.RegisterLocalPacket(ackhandler.PacketContext{}, now, pn, protocol.InvalidPacketNumber, nil, []ackhandler.Frame{{Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, protocol.ECNNon, 1200)
				require.NoError(t, err)
			}
			require.NoError(t, h.CompleteLocalPacket(packets[0].token, ackhandler.LocalPacketAccepted, now))
			packets = packets[1:]
			accepted, datagrams := 2, 2
			var resultErr error
			switch kind {
			case "partial":
				accepted = 1
			case "error":
				resultErr = errors.New("native error")
			case "MTU":
				packets[1].mtu = &mtuFinderAckHandler{}
			case "coalesced":
				accepted, datagrams = 1, 1
			}
			s := &nativeOrdinarySender{recovery: h}
			require.Equal(t, kind == "accepted", s.completeAcceptedPackets(packets, datagrams, accepted, resultErr, now.Add(time.Millisecond)))
			if kind != "accepted" {
				for _, p := range packets {
					require.NoError(t, h.CompleteLocalPacket(p.token, ackhandler.LocalPacketAccepted, now))
				}
			}
		})
	}
}
