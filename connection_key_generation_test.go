package quic

import (
	"testing"

	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/mocks"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type generationSealer struct {
	handshake.ShortHeaderSealer
	generation protocol.KeyPhase
}

func (s generationSealer) KeyGeneration() protocol.KeyPhase { return s.generation }

func TestSealedGenerationReachesConnectionACK(t *testing.T) {
	tc := newEmissionTestConnection(t, false)
	c := tc.conn
	ctrl := gomock.NewController(t)
	sealing := NewMockSealingManager(ctrl)
	sealing.EXPECT().Get1RTTSealer().Return(generationSealer{newMockShortHeaderSealer(ctrl), 2}, nil).AnyTimes()
	c.emission.packer.cryptoSetup = sealing
	pn, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
	require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("generation receipt")}))
	now := monotime.Now()
	require.NoError(t, c.triggerSending(now).err)
	cs := mocks.NewMockCryptoSetup(ctrl)
	c.cryptoStreamHandler = cs
	cs.EXPECT().Set1RTTAcked(protocol.ApplicationKeyReceipt{Valid: true, PathID: 0, PacketNumber: pn, KeyGeneration: 2}, now)
	c.lastPacketReceivedTime = now
	require.NoError(t, c.handleAckFrame(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: pn, Largest: pn}}}, protocol.Encryption1RTT, now))
	// Recovery has pooled the record. A duplicate ACK cannot manufacture another receipt.
	require.NoError(t, c.handleAckFrame(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: pn, Largest: pn}}}, protocol.Encryption1RTT, now))
}

type exhaustedSealer struct{ handshake.ShortHeaderSealer }

func (s exhaustedSealer) Seal([]byte, []byte, protocol.PacketNumber, []byte) ([]byte, error) {
	return nil, &qerr.TransportError{ErrorCode: qerr.AEADLimitReached}
}

func TestSealingLimitStopsEmissionCommit(t *testing.T) {
	tc := newEmissionTestConnection(t, false)
	c := tc.conn
	ctrl := gomock.NewController(t)
	sealing := NewMockSealingManager(ctrl)
	sealing.EXPECT().Get1RTTSealer().Return(exhaustedSealer{newMockShortHeaderSealer(ctrl)}, nil).AnyTimes()
	c.emission.packer.cryptoSetup = sealing
	pn, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
	require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("exhausted protection")}))
	result := c.triggerSending(monotime.Now())
	var limit *qerr.TransportError
	require.ErrorAs(t, result.err, &limit)
	require.Equal(t, qerr.AEADLimitReached, limit.ErrorCode)
	next, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
	require.Equal(t, pn, next, "failed sealing cannot consume a packet number")
	require.Empty(t, c.emission.queue.(*sendQueue).queue, "failed sealing cannot hand off ciphertext")
}
