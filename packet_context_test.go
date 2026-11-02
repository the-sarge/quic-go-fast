package quic

import (
	"net"
	"testing"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// Observe the production registration boundary while retaining real recovery.
type contextRecordingRecovery struct {
	ackhandler.SentPacketHandler
	contexts []ackhandler.PacketContext
}

func (h *contextRecordingRecovery) SentPacketWithContext(context ackhandler.PacketContext, now monotime.Time, pn, largestAcked protocol.PacketNumber, streams []ackhandler.StreamFrame, frames []ackhandler.Frame, level protocol.EncryptionLevel, ecn protocol.ECN, size protocol.ByteCount, mtu, probe bool) {
	h.contexts = append(h.contexts, context)
	h.SentPacketHandler.SentPacketWithContext(context, now, pn, largestAcked, streams, frames, level, ecn, size, mtu, probe)
}

func TestEmissionCapturesPathZeroTupleBeforeRegistration(t *testing.T) {
	tc := newEmissionTestConnection(t, false)
	c := tc.conn
	h := &contextRecordingRecovery{SentPacketHandler: c.sentPacketHandler}
	c.sentPacketHandler = h
	now := monotime.Now()
	require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("old tuple")}))
	buf := getPacketBuffer()
	defer buf.Release()
	p, err := c.emission.packer.AppendPacket(buf, 1200, now, c.version)
	require.NoError(t, err)
	require.Equal(t, ackhandler.PacketContext{}, p.Context)
	// A later preferred tuple must not rewrite a constructed packet's identity.
	ctrl := gomock.NewController(t)
	sender := NewMockSender(ctrl)
	sender.EXPECT().Close()
	c.emission.queue = sender
	c.peerParams = &wire.TransportParameters{MaxUDPPayloadSize: 1500}
	c.mtuDiscoverer = c.newPathMTUFinder(c.effectiveInitialPacketSize, 1500)
	raw := NewMockRawConn(ctrl)
	raw.EXPECT().LocalAddr().Return(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4321}).AnyTimes()
	raw.EXPECT().capabilities().Return(connCapabilities{}).AnyTimes()
	c.switchToNewPath(&Transport{conn: raw}, now)
	defer c.emission.queue.Close()
	c.emission.registerPacket(p, protocol.ECNUnsupported, now)
	require.Equal(t, []ackhandler.PacketContext{{}}, h.contexts)
	require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("new tuple")}))
	p, _, err = c.emission.appendPacket(buf, 1200, true, protocol.ECNUnsupported, now)
	require.NoError(t, err)
	require.Equal(t, ackhandler.PacketContext{TupleGeneration: 1}, p.Context)
	require.Equal(t, []ackhandler.PacketContext{{}, {TupleGeneration: 1}}, h.contexts)
}
