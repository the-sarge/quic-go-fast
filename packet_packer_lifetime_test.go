package quic

import (
	"sync"
	"testing"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// Sequential constructor tests install an empty pool so the private allocation
// can be observed before reuse. No production allocation hook is needed.
func observeConstructionBuffer(t *testing.T) **packetBuffer {
	t.Helper()
	var observed *packetBuffer
	bufferPool = sync.Pool{New: func() any {
		observed = &packetBuffer{Data: make([]byte, 0, protocol.MaxPacketBufferSize)}
		return observed
	}}
	t.Cleanup(func() {
		bufferPool = sync.Pool{New: func() any {
			return &packetBuffer{Data: make([]byte, 0, protocol.MaxPacketBufferSize)}
		}}
	})
	return &observed
}

func TestAckConstructorEmptyLifetime(t *testing.T) {
	c := newEmissionTestConnection(t, false).conn
	observed := observeConstructionBuffer(t)
	_, buf, err := c.emission.packer.PackAckOnlyPacket(1200, monotime.Now(), protocol.Version1)
	require.ErrorIs(t, err, errNothingToPack)
	require.NotNil(t, *observed)
	require.Zero(t, (*observed).refCount, "unreturned construction storage must be released")
	require.Nil(t, buf)
}

// Change only the final packet-number consistency check: payload production and
// encoding stay real, and consumed packet numbers must not be refunded.
type mismatchedConstructionNumber struct{ ackhandler.SentPacketHandler }

func (m mismatchedConstructionNumber) PopPacketNumber(level protocol.EncryptionLevel) protocol.PacketNumber {
	return m.SentPacketHandler.PopPacketNumber(level) + 1
}

func TestCoalescedConstructorFailureLifetime(t *testing.T) {
	ctrl := gomock.NewController(t)
	tc := newServerTestConnection(t, ctrl, &Config{InitialPacketSize: 1200}, false)
	c := tc.conn
	sealing := NewMockSealingManager(ctrl)
	sealing.EXPECT().GetInitialSealer().Return(newMockShortHeaderSealer(ctrl), nil).AnyTimes()
	sealing.EXPECT().GetHandshakeSealer().Return(nil, handshake.ErrKeysNotYetAvailable).AnyTimes()
	sealing.EXPECT().Get1RTTSealer().Return(nil, handshake.ErrKeysNotYetAvailable).AnyTimes()
	p := newPacketPacker(tc.srcConnID, c.connIDManager.Get, c.initialStream, c.handshakeStream, mismatchedConstructionNumber{c.sentPacketHandler}, c.retransmissionQueue, sealing, c.framer, &c.receivedPacketHandler, c.datagramQueue, c.perspective)
	c.retransmissionQueue.addInitial(&wire.PingFrame{})
	observed := observeConstructionBuffer(t)
	packet, err := p.PackCoalescedPacket(false, 1200, monotime.Now(), protocol.Version1)
	require.ErrorContains(t, err, "Peeked and Popped")
	require.Nil(t, packet)
	require.NotNil(t, *observed)
	require.Zero(t, (*observed).refCount)
	pn, _ := c.sentPacketHandler.PeekPacketNumber(protocol.EncryptionInitial)
	require.EqualValues(t, 1, pn)
}

func TestPTOConstructorFailureLifetime(t *testing.T) {
	for _, level := range []protocol.EncryptionLevel{protocol.EncryptionInitial, protocol.EncryptionHandshake, protocol.Encryption1RTT} {
		t.Run(level.String(), func(t *testing.T) {
			ctrl := gomock.NewController(t)
			tc := newServerTestConnection(t, ctrl, &Config{InitialPacketSize: 1200}, false)
			c := tc.conn
			sealing := NewMockSealingManager(ctrl)
			sealing.EXPECT().GetInitialSealer().Return(newMockShortHeaderSealer(ctrl), nil).AnyTimes()
			sealing.EXPECT().GetHandshakeSealer().Return(newMockShortHeaderSealer(ctrl), nil).AnyTimes()
			sealing.EXPECT().Get1RTTSealer().Return(newMockShortHeaderSealer(ctrl), nil).AnyTimes()
			p := newPacketPacker(tc.srcConnID, c.connIDManager.Get, c.initialStream, c.handshakeStream, mismatchedConstructionNumber{c.sentPacketHandler}, c.retransmissionQueue, sealing, c.framer, &c.receivedPacketHandler, c.datagramQueue, c.perspective)
			observed := observeConstructionBuffer(t)
			packet, err := p.PackPTOProbePacket(level, 1200, true, monotime.Now(), protocol.Version1)
			require.ErrorContains(t, err, "Peeked and Popped")
			require.Nil(t, packet)
			require.NotNil(t, *observed)
			require.Zero(t, (*observed).refCount)
			pn, _ := c.sentPacketHandler.PeekPacketNumber(level)
			require.EqualValues(t, 1, pn)
		})
	}
}

func TestProbeConstructorFailureLifetime(t *testing.T) {
	for _, mtu := range []bool{false, true} {
		for _, missingKeys := range []bool{false, true} {
			name := "path"
			if mtu {
				name = "MTU"
			}
			if missingKeys {
				name += "/keys"
			} else {
				name += "/append"
			}
			t.Run(name, func(t *testing.T) {
				tc := newEmissionTestConnection(t, false)
				c := tc.conn
				p := c.emission.packer
				p.pnManager = mismatchedConstructionNumber{c.sentPacketHandler}
				if missingKeys {
					sealing := NewMockSealingManager(gomock.NewController(t))
					sealing.EXPECT().Get1RTTSealer().Return(nil, handshake.ErrKeysNotYetAvailable)
					p.cryptoSetup = sealing
				}
				observed := observeConstructionBuffer(t)
				var buf *packetBuffer
				var err error
				if mtu {
					_, buf, err = p.PackMTUProbePacket(ackhandler.Frame{Frame: &wire.PingFrame{}}, 1300, protocol.Version1)
				} else {
					_, buf, err = p.PackPathProbePacket(c.connIDManager.Get(), []ackhandler.Frame{{Frame: &wire.PathChallengeFrame{Data: [8]byte{1}}}}, protocol.Version1)
				}
				if missingKeys {
					require.ErrorIs(t, err, handshake.ErrKeysNotYetAvailable)
				} else {
					require.ErrorContains(t, err, "Peeked and Popped")
				}
				require.NotNil(t, *observed)
				require.Zero(t, (*observed).refCount, "unreturned probe storage must be released")
				require.Nil(t, buf)
			})
		}
	}
}
