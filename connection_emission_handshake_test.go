package quic

import (
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/mocks"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func newHandshakeEmissionConnection(t *testing.T, client bool) *testConnection {
	t.Helper()
	ctrl := gomock.NewController(t)
	config := &Config{InitialPacketSize: 1200, DisablePathMTUDiscovery: true}
	var tc *testConnection
	if client {
		crypto := mocks.NewMockCryptoSetup(ctrl)
		crypto.EXPECT().DiscardInitialKeys().AnyTimes()
		tc = newClientTestConnection(t, ctrl, config, false, connectionOptCryptoSetup(crypto))
	} else {
		tc = newServerTestConnection(t, ctrl, config, false)
	}
	c := tc.conn
	sealing := NewMockSealingManager(ctrl)
	sealing.EXPECT().GetInitialSealer().DoAndReturn(func() (handshake.LongHeaderSealer, error) {
		if c.droppedInitialKeys {
			return nil, handshake.ErrKeysDropped
		}
		return newMockShortHeaderSealer(ctrl), nil
	}).AnyTimes()
	sealing.EXPECT().GetHandshakeSealer().Return(newMockShortHeaderSealer(ctrl), nil).AnyTimes()
	sealing.EXPECT().Get1RTTSealer().Return(nil, handshake.ErrKeysNotYetAvailable).AnyTimes()
	sealing.EXPECT().Get0RTTSealer().Return(nil, handshake.ErrKeysNotYetAvailable).AnyTimes()
	c.packer = newPacketPacker(tc.srcConnID, c.connIDManager.Get, c.initialStream, c.handshakeStream, c.sentPacketHandler, c.retransmissionQueue, sealing, c.framer, &c.receivedPacketHandler, c.datagramQueue, c.perspective)
	c.sentPacketHandler.ReceivedBytes(1<<20, monotime.Now())
	return tc
}

func TestEmissionCoalescedHandshake(t *testing.T) {
	for _, client := range []bool{false, true} {
		t.Run(map[bool]string{false: "server", true: "client"}[client], func(t *testing.T) {
			c := newHandshakeEmissionConnection(t, client).conn
			c.pathGeneration = 9
			c.retransmissionQueue.addInitial(&wire.PingFrame{})
			c.handshakeStream.Write([]byte("handshake payload"))
			now := monotime.Now()
			result := c.emitPackets(now)
			require.NoError(t, result.err)
			require.True(t, c.sentFirstPacket)
			require.Equal(t, client, c.droppedInitialKeys)
			require.Equal(t, now, c.firstAckElicitingPacketAfterIdleSentTime)
			q := c.sendQueue.(*sendQueue)
			require.Len(t, q.queue, 1)
			entry := <-q.queue
			defer entry.buf.Release()
			hdrs, more := parsePacket(t, entry.buf.Data)
			require.Len(t, hdrs, 2)
			require.Empty(t, more)
			require.Equal(t, protocol.PacketTypeInitial, hdrs[0].Type)
			require.Equal(t, protocol.PacketTypeHandshake, hdrs[1].Type)
			require.True(t, entry.metadata.handshake)
			require.EqualValues(t, 9, entry.metadata.pathGeneration)
			_, err := c.sentPacketHandler.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: hdrs[1].PacketNumber, Largest: hdrs[1].PacketNumber}}}, protocol.EncryptionHandshake, now.Add(time.Millisecond))
			require.NoError(t, err, "registration precedes queue handoff")
			if client {
				c.handshakeStream.Write([]byte("next flight"))
				require.NoError(t, c.emitPackets(now.Add(time.Millisecond)).err)
				next := <-q.queue
				defer next.buf.Release()
				nextHdrs, _ := parsePacket(t, next.buf.Data)
				require.Len(t, nextHdrs, 1)
				require.Equal(t, protocol.PacketTypeHandshake, nextHdrs[0].Type)
			}
		})
	}
}

// Retain real recovery and packet construction; select only the opportunity
// mode so ACK allowances are independent of a wall-clock congestion fixture.
func TestEmissionAckAllowance(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		for _, mode := range []ackhandler.SendMode{ackhandler.SendAck, ackhandler.SendPacingLimited} {
			t.Run(mode.String()+map[bool]string{false: "/handshake", true: "/confirmed"}[confirmed], func(t *testing.T) {
				var c *Conn
				level := protocol.EncryptionInitial
				if confirmed {
					c = newEmissionTestConnection(t, false).conn
					level = protocol.Encryption1RTT
				} else {
					c = newHandshakeEmissionConnection(t, false).conn
				}
				now := monotime.Now()
				require.NoError(t, c.receivedPacketHandler.ReceivedPacket(4, protocol.ECNNon, level, now, true))
				require.NoError(t, c.receivedPacketHandler.ReceivedPacket(5, protocol.ECNNon, level, now, true))
				c.sentPacketHandler = emissionRecoveryOutcome{SentPacketHandler: c.sentPacketHandler, mode: mode, deadline: now.Add(time.Millisecond)}
				result := c.triggerSending(now)
				require.NoError(t, result.err)
				q := c.sendQueue.(*sendQueue)
				require.Len(t, q.queue, 1)
				entry := <-q.queue
				defer entry.buf.Release()
				require.True(t, c.firstAckElicitingPacketAfterIdleSentTime.IsZero())
				if mode == ackhandler.SendAck {
					require.Equal(t, blockModeCongestionLimited, c.blocked)
				} else {
					require.Equal(t, now.Add(time.Millisecond), c.pacingDeadline)
				}
				require.NoError(t, c.triggerSending(now).err)
				require.Empty(t, q.queue, "an ACK is sent at most once until more packets arrive")
				require.NoError(t, c.receivedPacketHandler.ReceivedPacket(6, protocol.ECNNon, level, now, true))
				require.NoError(t, c.receivedPacketHandler.ReceivedPacket(7, protocol.ECNNon, level, now, true))
				c.sentPacketHandler = emissionRecoveryOutcome{SentPacketHandler: c.sentPacketHandler, mode: ackhandler.SendNone}
				blocked := c.triggerSending(now)
				require.Equal(t, emissionHardBlocked, blocked.stop)
				require.Empty(t, q.queue, "hard blocking forbids even ACK-only output")
			})
		}
	}
}

func TestEmissionAckFullQueuePacing(t *testing.T) {
	c := newEmissionTestConnection(t, false).conn
	q := c.sendQueue.(*sendQueue)
	for !q.WouldBlock() {
		q.Send(getPacketBuffer(), 0, protocol.ECNNon, sendMetadata{})
	}
	defer func() {
		for len(q.queue) > 0 {
			(<-q.queue).buf.Release()
		}
	}()
	now := monotime.Now()
	c.sentPacketHandler = emissionRecoveryOutcome{SentPacketHandler: c.sentPacketHandler, mode: ackhandler.SendPacingLimited, deadline: now.Add(-time.Millisecond)}
	require.NoError(t, c.receivedPacketHandler.ReceivedPacket(1, protocol.ECNNon, protocol.Encryption1RTT, now, true))
	require.NoError(t, c.receivedPacketHandler.ReceivedPacket(2, protocol.ECNNon, protocol.Encryption1RTT, now, true))
	c.pacingDeadline = now.Add(-time.Millisecond)
	result := c.triggerSending(now)
	require.NoError(t, result.err)
	require.Equal(t, emissionQueueFull, result.stop)
	require.NotNil(t, result.available)
	require.True(t, c.pacingDeadline.IsZero(), "full queue cancels expired pacing to avoid a busy loop")
	for len(q.queue) > 0 {
		(<-q.queue).buf.Release()
	}
	result = c.triggerSending(now)
	require.NoError(t, result.err)
	require.True(t, result.progress, "capacity check must not consume the pending ACK")
	require.Len(t, q.queue, 1)
}

type emissionPTOOpportunity struct {
	ackhandler.SentPacketHandler
	level  protocol.EncryptionLevel
	stopAt protocol.PacketNumber
}

func (h emissionPTOOpportunity) SendMode(monotime.Time) ackhandler.SendMode {
	pn, _ := h.PeekPacketNumber(h.level)
	if pn >= h.stopAt {
		return ackhandler.SendNone
	}
	switch h.level {
	case protocol.EncryptionInitial:
		return ackhandler.SendPTOInitial
	case protocol.EncryptionHandshake:
		return ackhandler.SendPTOHandshake
	default:
		return ackhandler.SendPTOAppData
	}
}

func TestEmissionPTOOutput(t *testing.T) {
	for _, level := range []protocol.EncryptionLevel{protocol.EncryptionInitial, protocol.EncryptionHandshake, protocol.Encryption1RTT} {
		for _, outstanding := range []bool{false, true} {
			t.Run(level.String()+map[bool]string{false: "/empty", true: "/retransmit"}[outstanding], func(t *testing.T) {
				var c *Conn
				if level == protocol.Encryption1RTT {
					c = newEmissionTestConnection(t, false).conn
				} else {
					c = newHandshakeEmissionConnection(t, false).conn
				}
				now := monotime.Now()
				q := c.sendQueue.(*sendQueue)
				if outstanding {
					// A real PING is registered first, so QueueProbePacket exercises recovery
					// and the real retransmission callback rather than a fabricated packet.
					packet, err := c.packer.PackPTOProbePacket(level, 1200, true, now, protocol.Version1)
					require.NoError(t, err)
					c.emission.sendCoalesced(packet, protocol.ECNNon, now)
					(<-q.queue).buf.Release()
				}
				first, _ := c.sentPacketHandler.PeekPacketNumber(level)
				c.sentPacketHandler = emissionPTOOpportunity{SentPacketHandler: c.sentPacketHandler, level: level, stopAt: first + 2}
				result := c.triggerSending(now)
				require.NoError(t, result.err)
				require.True(t, result.progress)
				require.Equal(t, emissionHardBlocked, result.stop)
				require.Len(t, q.queue, 2, "PTO continues immediately until recovery changes mode")
				for i := range 2 {
					entry := <-q.queue
					var pn protocol.PacketNumber
					if level == protocol.Encryption1RTT {
						_, n, _, _, err := wire.ParseShortHeader(entry.buf.Data, c.connIDManager.Get().Len())
						require.NoError(t, err)
						pn = n
					} else {
						hdrs, more := parsePacket(t, entry.buf.Data)
						require.Len(t, hdrs, 1)
						require.Empty(t, more)
						pn = hdrs[0].PacketNumber
						require.Equal(t, level, map[protocol.PacketType]protocol.EncryptionLevel{protocol.PacketTypeInitial: protocol.EncryptionInitial, protocol.PacketTypeHandshake: protocol.EncryptionHandshake}[hdrs[0].Type])
					}
					require.Equal(t, first+protocol.PacketNumber(i), pn)
					entry.buf.Release()
				}
				_, err := c.sentPacketHandler.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: first + 1, Largest: first + 1}}}, level, now.Add(time.Millisecond))
				require.NoError(t, err, "PTO registration precedes handoff")
			})
		}
	}
}
