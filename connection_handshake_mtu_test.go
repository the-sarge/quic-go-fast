package quic

import (
	"fmt"
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
	for _, tc := range []struct {
		name     string
		size     uint16
		fallback bool
		want     protocol.ByteCount
	}{
		{"unset healthy", 0, false, protocol.InitialPacketSize},
		{"unset fallback", 0, true, protocol.MinInitialPacketSize},
		{"explicit healthy", 1452, false, 1452},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newClientTestConnection(t, nil, &Config{InitialPacketSize: tc.size}, false).conn
			if tc.fallback {
				c.handshakeSendFeedback.publish(c.pathGeneration)
				c.applyHandshakeMTUFallback()
			}
			require.Equal(t, tc.want, c.maxPacketSize())
			require.EqualValues(t, estimateMaxPayloadSize(tc.want), c.maxPayloadSizeEstimate.Load())
			// Preserve the baseline's start-size behavior even with a smaller peer limit.
			c.peerParams = &wire.TransportParameters{MaxUDPPayloadSize: 1400, ActiveConnectionIDLimit: 2}
			c.applyTransportParameters()
			require.Equal(t, tc.want, c.maxPacketSize())
			require.EqualValues(t, 1400, c.mtuDiscoverer.max())
			require.EqualValues(t, estimateMaxPayloadSize(tc.want), c.maxPayloadSizeEstimate.Load())
			require.Equal(t, tc.size, c.config.InitialPacketSize)
		})
	}
}

func TestHandshakeMTUFallbackServerStart(t *testing.T) {
	for _, tc := range []struct {
		size uint16
		want protocol.ByteCount
	}{{0, protocol.InitialPacketSize}, {1452, 1452}} {
		t.Run(fmt.Sprintf("InitialPacketSize=%d", tc.size), func(t *testing.T) {
			c := newServerTestConnection(t, nil, &Config{InitialPacketSize: tc.size}, false).conn
			require.EqualValues(t, protocol.MinInitialPacketSize, c.maxPacketSize(), "until the client's transport parameters")
			c.peerParams = &wire.TransportParameters{MaxUDPPayloadSize: 1500, ActiveConnectionIDLimit: 2}
			c.applyTransportParameters()
			require.Equal(t, tc.want, c.maxPacketSize())
		})
	}
}

func TestHandshakeMTUFallbackExplicitFloor(t *testing.T) {
	c := newClientTestConnection(t, nil, &Config{InitialPacketSize: 1452}, false).conn
	c.peerParams = &wire.TransportParameters{MaxUDPPayloadSize: 1500, ActiveConnectionIDLimit: 2}
	c.applyTransportParameters()
	finder := c.mtuDiscoverer
	// Explicit sends are never marked, so only direct publication reaches the fallback.
	c.handshakeSendFeedback.publish(c.pathGeneration)
	c.applyHandshakeMTUFallback()
	_, pending := c.handshakeSendFeedback.take()
	require.False(t, pending, "the event is consumed")
	require.EqualValues(t, 1452, c.effectiveInitialPacketSize)
	require.Same(t, finder, c.mtuDiscoverer)
	require.EqualValues(t, 1452, c.maxPacketSize())
	require.EqualValues(t, estimateMaxPayloadSize(1452), c.maxPayloadSizeEstimate.Load())
}

func TestHandshakeMTUFallbackPhaseAndPath(t *testing.T) {
	c := newClientTestConnection(t, nil, &Config{}, false).conn
	c.pathGeneration = 2
	c.handshakeSendFeedback.publish(1)
	c.applyHandshakeMTUFallback()
	require.EqualValues(t, protocol.InitialPacketSize, c.maxPacketSize())
	c.handshakeConfirmed = true
	c.handshakeSendFeedback.publish(2)
	c.applyHandshakeMTUFallback()
	require.EqualValues(t, protocol.InitialPacketSize, c.maxPacketSize())
	c.handshakeConfirmed = false
	c.handshakeSendFeedback.publish(2)
	c.applyHandshakeMTUFallback()
	require.EqualValues(t, 1200, c.maxPacketSize())
	c.handshakeSendFeedback.publish(2)
	c.applyHandshakeMTUFallback()
	require.EqualValues(t, 1200, c.maxPacketSize())
}

func TestHandshakeMTUFallbackDiscovery(t *testing.T) {
	for _, tc := range []struct {
		name         string
		disabled, df bool
	}{
		{"enabled", false, true},
		{"disabled", true, true},
		{"unavailable", false, false},
		{"disabled unavailable", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			cs := mocks.NewMockCryptoSetup(ctrl)
			c := newServerTestConnection(t, ctrl, &Config{DisablePathMTUDiscovery: tc.disabled}, false, connectionOptCryptoSetup(cs)).conn
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
			sc.EXPECT().capabilities().Return(connCapabilities{DF: tc.df}).AnyTimes()
			c.conn = sc
			require.NoError(t, c.handleHandshakeConfirmed(now))
			require.Equal(t, !tc.disabled && tc.df, c.mtuDiscoverer.ShouldSendProbe(now.Add(time.Hour)))
			require.EqualValues(t, 1200, c.maxPacketSize())
			if !tc.disabled && tc.df {
				ping, size := c.mtuDiscoverer.GetPing(now.Add(time.Hour))
				require.EqualValues(t, 1326, size)
				ping.Handler.OnAcked(ping.Frame)
				require.EqualValues(t, 1326, c.maxPacketSize())
			}
		})
	}
}

func TestHandshakeMTUFallbackCongestionGrowth(t *testing.T) {
	for _, tc := range []struct {
		name     string
		size     uint16
		fallback bool
		ceiling  protocol.ByteCount
		growth   []protocol.ByteCount // max(discovered MTU, configured or default initial size)
	}{
		// Discovery restarts from 1200 and probes 1250 and 1275 first, but congestion
		// accounting never drops below 1280.
		{"unset fallback", 0, true, 1300, []protocol.ByteCount{1280, 1280, 1287}},
		{"explicit", 1337, false, 1500, []protocol.ByteCount{1394, 1423, 1437}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			cs := mocks.NewMockCryptoSetup(ctrl)
			c := newClientTestConnection(t, ctrl, &Config{InitialPacketSize: tc.size}, false, connectionOptCryptoSetup(cs)).conn
			c.peerParams = &wire.TransportParameters{MaxUDPPayloadSize: tc.ceiling, ActiveConnectionIDLimit: 2}
			c.applyTransportParameters()
			if tc.fallback {
				c.handshakeSendFeedback.publish(0)
				c.applyHandshakeMTUFallback()
			}
			c.handshakeConfirmed = true
			// Keep the real congestion controller behind the ACK seam: decreasing its size panics.
			realHandler := c.sentPacketHandler
			sph := mockackhandler.NewMockSentPacketHandler(ctrl)
			c.sentPacketHandler = sph
			sph.EXPECT().ReceivedAck(gomock.Any(), protocol.Encryption1RTT, gomock.Any()).Return(true, nil).Times(len(tc.growth))
			cs.EXPECT().SetLargest1RTTAcked(gomock.Any()).Times(len(tc.growth))
			for _, expected := range tc.growth {
				ping, _ := c.mtuDiscoverer.GetPing(monotime.Now())
				ping.Handler.OnAcked(ping.Frame)
				sph.EXPECT().SetMaxDatagramSize(expected).Do(func(size protocol.ByteCount) { realHandler.SetMaxDatagramSize(size) })
				require.NoError(t, c.handleAckFrame(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: 1, Largest: 1}}}, protocol.Encryption1RTT, monotime.Now()))
				require.EqualValues(t, estimateMaxPayloadSize(c.maxPacketSize()), c.maxPayloadSizeEstimate.Load())
			}
		})
	}
}

func TestHandshakeMTUFallbackBeforePacking(t *testing.T) {
	ctrl := gomock.NewController(t)
	tc := newClientTestConnection(t, ctrl, &Config{}, false)
	useLifecyclePacketPacker(t, ctrl, tc)
	c := tc.conn
	now := monotime.Now()
	require.NoError(t, c.receivedPacketHandler.ReceivedPacket(4, protocol.ECNNon, protocol.EncryptionInitial, now, true))
	c.sentPacketHandler = emissionRecoveryOutcome{SentPacketHandler: c.sentPacketHandler, mode: ackhandler.SendAck}
	c.handshakeSendFeedback.publish(c.pathGeneration)
	c.pacingDeadline = deadlineSendImmediately
	require.NoError(t, c.triggerSending(now).err)
	q := c.emission.queue.(*sendQueue)
	require.Len(t, q.queue, 1)
	entry := <-q.queue
	defer entry.buf.Release()
	require.Len(t, entry.buf.Data, 1200, "eligible feedback reduces the budget before construction")
	hdrs, _ := parsePacket(t, entry.buf.Data)
	require.Len(t, hdrs, 1)
	require.Equal(t, protocol.PacketTypeInitial, hdrs[0].Type)
}

func handshakePTO(encLevel protocol.EncryptionLevel) ackhandler.TimeoutOutcome {
	return ackhandler.TimeoutOutcome{PTO: true, EncLevel: encLevel}
}

// newHandshakeTimeoutTestConnection returns a connection whose packet size is its
// effective initial size: the server learns the client's transport parameters.
func newHandshakeTimeoutTestConnection(t *testing.T, pers protocol.Perspective, conf *Config) *Conn {
	t.Helper()
	if pers == protocol.PerspectiveClient {
		return newClientTestConnection(t, nil, conf, false).conn
	}
	c := newServerTestConnection(t, nil, conf, false).conn
	c.peerParams = &wire.TransportParameters{MaxUDPPayloadSize: 1500, ActiveConnectionIDLimit: 2}
	c.applyTransportParameters()
	return c
}

func TestHandshakeTimeoutFallback(t *testing.T) {
	for _, pers := range []protocol.Perspective{protocol.PerspectiveClient, protocol.PerspectiveServer} {
		t.Run(pers.String(), func(t *testing.T) {
			t.Run("the second eligible PTO lowers an unset size", func(t *testing.T) {
				c := newHandshakeTimeoutTestConnection(t, pers, &Config{})
				c.countHandshakeTimeout(handshakePTO(protocol.EncryptionInitial))
				require.EqualValues(t, protocol.InitialPacketSize, c.maxPacketSize())
				// Recovery resets its own PTO count on ACK progress and key discards; this count is cumulative.
				c.countHandshakeTimeout(handshakePTO(protocol.EncryptionHandshake))
				require.EqualValues(t, 1200, c.maxPacketSize())
				require.EqualValues(t, estimateMaxPayloadSize(1200), c.maxPayloadSizeEstimate.Load())
			})

			t.Run("only Initial and Handshake PTOs before completion count", func(t *testing.T) {
				c := newHandshakeTimeoutTestConnection(t, pers, &Config{})
				c.countHandshakeTimeout(handshakePTO(protocol.EncryptionInitial))
				for range 2 {
					c.countHandshakeTimeout(ackhandler.TimeoutOutcome{})
					c.countHandshakeTimeout(handshakePTO(protocol.Encryption1RTT))
				}
				require.EqualValues(t, protocol.InitialPacketSize, c.maxPacketSize())
				c.handshakeComplete = true
				c.countHandshakeTimeout(handshakePTO(protocol.EncryptionHandshake))
				require.EqualValues(t, protocol.InitialPacketSize, c.maxPacketSize(), "completion ends eligibility")
			})

			t.Run("explicit sizes are never lowered", func(t *testing.T) {
				for _, size := range []uint16{1200, 1280} {
					c := newHandshakeTimeoutTestConnection(t, pers, &Config{InitialPacketSize: size})
					for range 3 {
						c.countHandshakeTimeout(handshakePTO(protocol.EncryptionInitial))
					}
					require.EqualValues(t, size, c.maxPacketSize())
				}
			})
		})
	}

	t.Run("a path change starts a new count", func(t *testing.T) {
		c := newClientTestConnection(t, nil, &Config{}, false).conn
		c.countHandshakeTimeout(handshakePTO(protocol.EncryptionInitial))
		c.pathGeneration++
		c.countHandshakeTimeout(handshakePTO(protocol.EncryptionInitial))
		require.EqualValues(t, protocol.InitialPacketSize, c.maxPacketSize())
		c.countHandshakeTimeout(handshakePTO(protocol.EncryptionInitial))
		require.EqualValues(t, 1200, c.maxPacketSize())
		// A new count never restores the size.
		c.pathGeneration++
		c.countHandshakeTimeout(handshakePTO(protocol.EncryptionInitial))
		require.EqualValues(t, 1200, c.maxPacketSize())
	})

	t.Run("a Retry starts a new count", func(t *testing.T) {
		tc := newClientTestConnection(t, nil, &Config{}, false)
		c := tc.conn
		c.countHandshakeTimeout(handshakePTO(protocol.EncryptionInitial))
		retryConnID := protocol.ParseConnectionID([]byte{0xde, 0xad, 0xbe, 0xef})
		wasProcessed, err := c.handleOnePacket(getRetryPacket(t, retryConnID, tc.srcConnID, tc.destConnID, []byte("token")), 0)
		require.NoError(t, err)
		require.True(t, wasProcessed)
		require.True(t, c.receivedRetry)
		c.countHandshakeTimeout(handshakePTO(protocol.EncryptionInitial))
		require.EqualValues(t, protocol.InitialPacketSize, c.maxPacketSize())
		c.countHandshakeTimeout(handshakePTO(protocol.EncryptionInitial))
		require.EqualValues(t, 1200, c.maxPacketSize())
	})
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
		for _, size := range []uint16{0, 1452} {
			// An explicit size is a floor, so its sends are never eligible for the fallback.
			t.Run(fmt.Sprintf("%s/InitialPacketSize=%d", tt.name, size), func(t *testing.T) {
				ctrl := gomock.NewController(t)
				sph := mockackhandler.NewMockSentPacketHandler(ctrl)
				sender := NewMockSender(ctrl)
				conf := &Config{InitialPacketSize: size, DisablePathMTUDiscovery: true}
				c := newServerTestConnection(t, ctrl, conf, false, connectionOptSentPacketHandler(sph), connectionOptSender(sender)).conn
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
				sender.EXPECT().Send(packet.buffer, uint16(0), protocol.ECNNon, sendMetadata{handshake: tt.eligible && size == 0, pathGeneration: 7})
				c.emission.sendCoalesced(packet, protocol.ECNNon, monotime.Now())
			})
		}
	}
}
