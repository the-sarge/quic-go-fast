package ackhandler

import (
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

func TestPacketContextRegistration(t *testing.T) {
	h := newSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, false, nil, protocol.PerspectiveClient, nil, utils.DefaultLogger, nil)
	now := monotime.Now()
	for _, level := range []protocol.EncryptionLevel{protocol.EncryptionInitial, protocol.EncryptionHandshake, protocol.Encryption0RTT, protocol.Encryption1RTT} {
		context := PacketContext{}
		if level == protocol.Encryption0RTT || level == protocol.Encryption1RTT {
			context.TupleGeneration = 7
		}
		pn := h.PopPacketNumber(level)
		h.SentPacketWithContext(context, now, pn, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.PingFrame{}, Handler: &customFrameHandler{}}}, level, protocol.ECNUnsupported, 100, false, false)
		found := false
		for number, p := range h.getPacketNumberSpace(level).history.Packets() {
			if number == pn {
				found = true
				require.Equal(t, context, p.Context)
				require.Equal(t, level, p.EncryptionLevel)
			}
		}
		require.True(t, found)
	}
	require.EqualValues(t, 400, h.bytesInFlight)
	// Migration retains the existing disposal/retransmission behavior, with a
	// fresh tuple snapshot on subsequent application registrations.
	h.MigratedPath(now.Add(time.Millisecond), 1200)
	pn := h.PopPacketNumber(protocol.Encryption1RTT)
	h.SentPacketWithContext(PacketContext{TupleGeneration: 8}, now.Add(time.Millisecond), pn, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.PingFrame{}, Handler: &customFrameHandler{}}}, protocol.Encryption1RTT, protocol.ECNUnsupported, 100, false, false)
	_, p := h.appDataPackets.history.FirstOutstanding()
	require.NotNil(t, p)
	require.Equal(t, PacketContext{TupleGeneration: 8}, p.Context)
	_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pn)}, protocol.Encryption1RTT, now.Add(10*time.Millisecond))
	require.NoError(t, err)
	require.EqualValues(t, 200, h.bytesInFlight, "Initial and Handshake retain independent accounting")
}

func TestPacketContextRejectsUnsupportedIdentityBeforeAccounting(t *testing.T) {
	for _, tt := range []struct {
		name    string
		context PacketContext
		level   protocol.EncryptionLevel
	}{
		{"nonzero path", PacketContext{PathID: 1}, protocol.Encryption1RTT},
		{"Initial tuple", PacketContext{TupleGeneration: 1}, protocol.EncryptionInitial},
		{"Handshake tuple", PacketContext{TupleGeneration: 1}, protocol.EncryptionHandshake},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, false, nil, protocol.PerspectiveClient, nil, utils.DefaultLogger, nil)
			require.Panics(t, func() {
				h.SentPacketWithContext(tt.context, monotime.Now(), 0, protocol.InvalidPacketNumber, nil, nil, tt.level, protocol.ECNUnsupported, 100, false, false)
			})
			require.Zero(t, h.bytesSent)
			require.Zero(t, h.connStats.PacketsSent.Load())
		})
	}
}
