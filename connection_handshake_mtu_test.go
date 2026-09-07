package quic

import (
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/mocks"
	mockackhandler "github.com/quic-go/quic-go/internal/mocks/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestHandshakeMTUFallbackInitialization(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		t.Run(map[bool]string{false: "healthy", true: "fallback"}[fallback], func(t *testing.T) {
			c := newClientTestConnection(t, nil, &Config{InitialPacketSize: 1452}, false).conn
			require.EqualValues(t, 1452, c.maxPacketSize())
			want := protocol.ByteCount(1452)
			if fallback {
				c.handshakeSendFeedback.publish(c.pathGeneration)
				c.applyHandshakeMTUFallback()
				want = 1200
			}
			require.Equal(t, want, c.maxPacketSize())
			// Preserve the baseline's start-size behavior even with a smaller peer limit.
			c.peerParams = &wire.TransportParameters{MaxUDPPayloadSize: 1400, ActiveConnectionIDLimit: 2}
			c.applyTransportParameters()
			require.Equal(t, want, c.maxPacketSize())
			require.EqualValues(t, 1400, c.mtuDiscoverer.max())
			require.EqualValues(t, estimateMaxPayloadSize(want), c.maxPayloadSizeEstimate.Load())
			require.EqualValues(t, 1452, c.config.InitialPacketSize)
		})
	}
}

func TestHandshakeMTUFallbackPhaseAndPath(t *testing.T) {
	c := newClientTestConnection(t, nil, &Config{InitialPacketSize: 1452}, false).conn
	c.pathGeneration = 2
	c.handshakeSendFeedback.publish(1)
	c.applyHandshakeMTUFallback()
	require.EqualValues(t, 1452, c.maxPacketSize())
	c.handshakeConfirmed = true
	c.handshakeSendFeedback.publish(2)
	c.applyHandshakeMTUFallback()
	require.EqualValues(t, 1452, c.maxPacketSize())
	c.handshakeConfirmed = false
	c.handshakeSendFeedback.publish(2)
	c.applyHandshakeMTUFallback()
	require.EqualValues(t, 1200, c.maxPacketSize())
	c.handshakeSendFeedback.publish(2)
	c.applyHandshakeMTUFallback()
	require.EqualValues(t, 1200, c.maxPacketSize())
}

func TestHandshakeMTUFallbackDiscovery(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "enabled", true: "disabled"}[disabled], func(t *testing.T) {
			ctrl := gomock.NewController(t)
			cs := mocks.NewMockCryptoSetup(ctrl)
			c := newServerTestConnection(t, ctrl, &Config{InitialPacketSize: 1452, DisablePathMTUDiscovery: disabled}, false, connectionOptCryptoSetup(cs)).conn
			c.peerParams = &wire.TransportParameters{MaxUDPPayloadSize: 1500, ActiveConnectionIDLimit: 2}
			c.applyTransportParameters()
			c.handshakeSendFeedback.publish(0)
			c.applyHandshakeMTUFallback()
			now := monotime.Now()
			require.EqualValues(t, 1200, c.maxPacketSize())
			require.False(t, c.mtuDiscoverer.ShouldSendProbe(now.Add(time.Hour)))
			cs.EXPECT().DiscardInitialKeys()
			cs.EXPECT().SetHandshakeConfirmed()
			sc := NewMockSendConn(ctrl)
			sc.EXPECT().capabilities().Return(connCapabilities{DF: true}).AnyTimes()
			c.conn = sc
			require.NoError(t, c.handleHandshakeConfirmed(now))
			require.Equal(t, !disabled, c.mtuDiscoverer.ShouldSendProbe(now.Add(time.Hour)))
			require.EqualValues(t, 1200, c.maxPacketSize())
			if !disabled {
				ping, size := c.mtuDiscoverer.GetPing(now.Add(time.Hour))
				require.EqualValues(t, 1326, size)
				ping.Handler.OnAcked(ping.Frame)
				require.EqualValues(t, 1326, c.maxPacketSize())
			}
		})
	}
}

func TestHandshakeMTUFallbackCongestionGrowth(t *testing.T) {
	ctrl := gomock.NewController(t)
	cs := mocks.NewMockCryptoSetup(ctrl)
	c := newClientTestConnection(t, ctrl, &Config{InitialPacketSize: 1337}, false, connectionOptCryptoSetup(cs)).conn
	c.peerParams = &wire.TransportParameters{MaxUDPPayloadSize: 1500, ActiveConnectionIDLimit: 2}
	c.applyTransportParameters()
	c.handshakeSendFeedback.publish(0)
	c.applyHandshakeMTUFallback()
	c.handshakeConfirmed = true
	// Keep the real congestion controller behind the ACK seam: decreasing its size panics.
	realHandler := c.sentPacketHandler
	sph := mockackhandler.NewMockSentPacketHandler(ctrl)
	c.sentPacketHandler = sph
	sph.EXPECT().ReceivedAck(gomock.Any(), protocol.Encryption1RTT, gomock.Any()).Return(true, nil).Times(3)
	cs.EXPECT().SetLargest1RTTAcked(gomock.Any()).Times(3)
	for _, expected := range []protocol.ByteCount{1337, 1389, 1420} {
		ping, _ := c.mtuDiscoverer.GetPing(monotime.Now())
		ping.Handler.OnAcked(ping.Frame)
		sph.EXPECT().SetMaxDatagramSize(expected).Do(func(size protocol.ByteCount) { realHandler.SetMaxDatagramSize(size) })
		require.NoError(t, c.handleAckFrame(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: 1, Largest: 1}}}, protocol.Encryption1RTT, monotime.Now()))
		require.EqualValues(t, estimateMaxPayloadSize(c.maxPacketSize()), c.maxPayloadSizeEstimate.Load())
	}
}

func TestHandshakeMTUFallbackBeforePacking(t *testing.T) {
	ctrl := gomock.NewController(t)
	sph := mockackhandler.NewMockSentPacketHandler(ctrl)
	tc := newClientTestConnection(t, ctrl, &Config{InitialPacketSize: 1452}, false, connectionOptSentPacketHandler(sph))
	tc.conn.pacingDeadline = deadlineSendImmediately
	tc.conn.handshakeSendFeedback.publish(0)
	sph.EXPECT().SendMode(gomock.Any()).Return(ackhandler.SendAck)
	sph.EXPECT().ECNMode(false).Return(protocol.ECNNon)
	tc.packer.EXPECT().PackCoalescedPacket(true, protocol.ByteCount(1200), gomock.Any(), protocol.Version1).Return(nil, nil)
	require.NoError(t, tc.conn.triggerSending(monotime.Now()))
}

func TestHandshakeMTUFallbackSendClassification(t *testing.T) {
	for _, tt := range []struct {
		name     string
		types    []protocol.PacketType
		eligible bool
	}{
		{"Initial", []protocol.PacketType{protocol.PacketTypeInitial}, true},
		{"Handshake", []protocol.PacketType{protocol.PacketTypeHandshake}, true},
		{"Initial and 0-RTT", []protocol.PacketType{protocol.PacketTypeInitial, protocol.PacketType0RTT}, true},
		{"0-RTT only", []protocol.PacketType{protocol.PacketType0RTT}, false},
		{"1-RTT only", nil, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			sph := mockackhandler.NewMockSentPacketHandler(ctrl)
			sender := NewMockSender(ctrl)
			c := newServerTestConnection(t, ctrl, nil, false, connectionOptSentPacketHandler(sph), connectionOptSender(sender)).conn
			c.pathGeneration = 7
			packet := &coalescedPacket{buffer: getPacketBuffer()}
			defer packet.buffer.Release()
			for _, typ := range tt.types {
				packet.longHdrPackets = append(packet.longHdrPackets, &longHeaderPacket{header: &wire.ExtendedHeader{Header: wire.Header{Type: typ}}})
			}
			if len(tt.types) == 0 {
				packet.shortHdrPacket = &shortHeaderPacket{}
			}
			sph.EXPECT().SentPacket(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), false, false).Times(max(1, len(tt.types)))
			sender.EXPECT().Send(packet.buffer, uint16(0), protocol.ECNNon, sendMetadata{handshake: tt.eligible, pathGeneration: 7})
			require.NoError(t, c.sendPackedCoalescedPacket(packet, protocol.ECNNon, monotime.Now()))
		})
	}
}
