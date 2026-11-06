package quic

import (
	"net"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/mocks"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func requirePathMTUSettled(t *testing.T, c *Conn, want bool) {
	t.Helper()
	select {
	case <-c.PathMTUSettledV1():
		require.True(t, want, "path MTU settled")
	default:
		require.False(t, want, "path MTU not settled")
	}
}

// reportedDatagramLimit is the limit an oversized SendDatagram reports.
func reportedDatagramLimit(t *testing.T, c *Conn) int64 {
	t.Helper()
	var tooLarge *DatagramTooLargeError
	require.ErrorAs(t, c.SendDatagram(make([]byte, protocol.MaxPacketBufferSize)), &tooLarge)
	return tooLarge.MaxDatagramPayloadSize
}

// newPathMTUSettledTestConnection returns a client connection that has applied the
// peer's transport parameters, but has not confirmed the handshake. Its socket
// reports df.
func newPathMTUSettledTestConnection(t *testing.T, conf *Config, maxUDPPayloadSize protocol.ByteCount, df bool, ceiling ...packetCeiling) (*Conn, *mocks.MockCryptoSetup) {
	t.Helper()
	ctrl := gomock.NewController(t)
	cs := mocks.NewMockCryptoSetup(ctrl)
	tc := newClientTestConnection(t, ctrl, conf, false, connectionOptCryptoSetup(cs))
	c := tc.conn
	if len(ceiling) != 0 {
		c.packetCeiling = ceiling[0]
	}
	sc := NewMockSendConn(ctrl)
	sc.EXPECT().capabilities().Return(connCapabilities{DF: df}).AnyTimes()
	sc.EXPECT().RemoteAddr().Return(tc.remoteAddr).AnyTimes()
	sc.EXPECT().LocalAddr().Return(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}).AnyTimes()
	c.conn = sc
	c.peerParams = &wire.TransportParameters{
		MaxUDPPayloadSize:       maxUDPPayloadSize,
		MaxDatagramFrameSize:    wire.MaxDatagramSize,
		ActiveConnectionIDLimit: 2,
	}
	c.applyTransportParameters()
	return c, cs
}

func confirmPathMTUSettledTestConnection(t *testing.T, c *Conn, cs *mocks.MockCryptoSetup, now monotime.Time) {
	t.Helper()
	cs.EXPECT().DiscardInitialKeys()
	cs.EXPECT().SetHandshakeConfirmed()
	require.NoError(t, c.handleHandshakeConfirmed(now))
}

func TestPathMTUSettledAtConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name              string
		disabled, df      bool
		maxUDPPayloadSize protocol.ByteCount
		settled           bool
	}{
		{name: "discovery disabled", disabled: true, df: true, maxUDPPayloadSize: 1500, settled: true},
		{name: "DF unavailable", maxUDPPayloadSize: 1500, settled: true},
		{name: "nothing to search", df: true, maxUDPPayloadSize: protocol.InitialPacketSize + maxMTUDiff, settled: true},
		{name: "discovery runs", df: true, maxUDPPayloadSize: 1500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conf := &Config{DisablePathMTUDiscovery: tc.disabled, EnableDatagrams: true}
			c, cs := newPathMTUSettledTestConnection(t, conf, tc.maxUDPPayloadSize, tc.df)
			requirePathMTUSettled(t, c, false)
			confirmPathMTUSettledTestConnection(t, c, cs, monotime.Now())
			requirePathMTUSettled(t, c, tc.settled)
			require.EqualValues(t, estimateMaxPayloadSize(protocol.InitialPacketSize), reportedDatagramLimit(t, c))
		})
	}
}

// The search is driven through the real sent packet handler and the connection's ACK
// seam. It concludes on an acknowledged probe or on a lost one, and a loss is declared
// either during ACK processing or by the loss detection timer.
func TestPathMTUSettledProbeOutcome(t *testing.T) {
	const (
		acked = iota
		lostOnAck
		lostOnTimer
	)
	for _, tc := range []struct {
		name    string
		outcome int
	}{
		{"acknowledged", acked},
		{"lost during ACK processing", lostOnAck},
		{"lost to the loss detection timer", lostOnTimer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, cs := newPathMTUSettledTestConnection(t, &Config{EnableDatagrams: true}, 1500, true)
			cs.EXPECT().Set1RTTAcked(gomock.Any(), gomock.Any()).AnyTimes()
			now := monotime.Now()
			confirmPathMTUSettledTestConnection(t, c, cs, now)
			requirePathMTUSettled(t, c, false)

			send := func(frame ackhandler.Frame, size protocol.ByteCount, mtuProbe bool) protocol.PacketNumber {
				pn := c.sentPacketHandler.PopPacketNumber(protocol.Encryption1RTT)
				c.sentPacketHandler.SentPacket(now, pn, protocol.InvalidPacketNumber, nil, []ackhandler.Frame{frame}, protocol.Encryption1RTT, protocol.ECNNon, size, mtuProbe, false)
				return pn
			}
			sendPing := func() protocol.PacketNumber {
				return send(ackhandler.Frame{Frame: &wire.PingFrame{}}, 100, false)
			}
			ack := func(pns ...protocol.PacketNumber) {
				var ranges []wire.AckRange
				for _, pn := range slices.Backward(slices.Sorted(slices.Values(pns))) {
					ranges = append(ranges, wire.AckRange{Smallest: pn, Largest: pn})
				}
				// As packet handling does before it handles the frames.
				c.lastPacketReceivedTime = now
				require.NoError(t, c.handleAckFrame(&wire.AckFrame{AckRanges: ranges}, protocol.Encryption1RTT, now))
			}
			resolveProbe := func(outcome int) {
				t.Helper()
				ping, size := c.mtuDiscoverer.GetPing(now)
				probe := send(ping, size, true)
				switch outcome {
				case acked:
					now = now.Add(10 * time.Millisecond)
					ack(probe)
				case lostOnAck:
					// Three later packets meet the packet threshold.
					pns := []protocol.PacketNumber{sendPing(), sendPing(), sendPing()}
					now = now.Add(10 * time.Millisecond)
					ack(pns...)
				case lostOnTimer:
					// One later packet, acknowledged within the RTT, arms the time threshold.
					// A probe alone doesn't keep the timer armed, so another packet stays
					// outstanding, as data does on a live connection.
					pn := sendPing()
					sendPing()
					now = now.Add(10 * time.Millisecond)
					ack(pn)
					require.NotEqual(t, protocol.InvalidByteCount, c.mtuDiscoverer.inFlight, "not yet lost")
					now = c.sentPacketHandler.GetLossDetectionTimeout()
					require.False(t, now.IsZero(), "loss timer armed")
					_, err := c.sentPacketHandler.OnLossDetectionTimeout(now)
					require.NoError(t, err)
				}
				require.Equal(t, protocol.InvalidByteCount, c.mtuDiscoverer.inFlight, "probe resolved")
			}

			// Grow first, so that the settled limit differs from the initial one.
			resolveProbe(acked)
			for i := 0; !c.mtuDiscoverer.done(); i++ {
				require.Less(t, i, 30)
				requirePathMTUSettled(t, c, false)
				resolveProbe(tc.outcome)
			}
			requirePathMTUSettled(t, c, true)
			limit := reportedDatagramLimit(t, c)
			require.Greater(t, limit, int64(estimateMaxPayloadSize(protocol.InitialPacketSize)))
			require.EqualValues(t, estimateMaxPayloadSize(c.mtuDiscoverer.CurrentSize()), limit)
			now = now.Add(10 * time.Millisecond)
			ack(sendPing())
			require.Equal(t, limit, reportedDatagramLimit(t, c), "a later read")
		})
	}
}

func TestPathMTUSettledPathChange(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	sender := NewMockSender(mockCtrl)
	c := newClientTestConnection(t, mockCtrl,
		&Config{EnableDatagrams: true},
		false,
		connectionOptSender(sender),
		connectionOptHandshakeConfirmed(),
	).conn
	c.peerParams = &wire.TransportParameters{MaxUDPPayloadSize: 1500, MaxDatagramFrameSize: wire.MaxDatagramSize, ActiveConnectionIDLimit: 2}
	c.applyTransportParameters()
	initialLimit := int64(estimateMaxPayloadSize(protocol.InitialPacketSize))
	now := monotime.Now()
	// grow acknowledges one probe, as the ACK path does, without concluding the search.
	grow := func() {
		t.Helper()
		ping, _ := c.mtuDiscoverer.GetPing(now)
		ping.Handler.OnAcked(ping.Frame)
		require.False(t, c.mtuDiscoverer.done())
		c.maxPayloadSizeEstimate.Store(uint32(estimateMaxPayloadSize(c.mtuDiscoverer.CurrentSize())))
		require.Greater(t, reportedDatagramLimit(t, c), initialLimit)
	}
	switchPath := func() {
		t.Helper()
		raw := NewMockRawConn(mockCtrl)
		raw.EXPECT().LocalAddr().Return(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4321}).AnyTimes()
		raw.EXPECT().capabilities().Return(connCapabilities{DF: true}).AnyTimes()
		c.switchToNewPath(&Transport{conn: raw}, now)
	}

	grow()
	requirePathMTUSettled(t, c, false)
	sender.EXPECT().Close()
	switchPath()
	requirePathMTUSettled(t, c, true)
	require.Equal(t, initialLimit, reportedDatagramLimit(t, c), "the first path is abandoned")

	// Discovery runs again on the new path, and a later path change resets it again.
	grow()
	switchPath()
	// Closing joins the last path's send worker; replacing a path closes the one before.
	defer c.emission.queue.Close()
	requirePathMTUSettled(t, c, true)
	require.Equal(t, initialLimit, reportedDatagramLimit(t, c), "the limit read after the signal shrinks")
}

// The handshake fallback replaces the finder before confirmation. The replacement
// reports to the same signal, which closes once.
func TestPathMTUSettledHandshakeFallback(t *testing.T) {
	c, cs := newPathMTUSettledTestConnection(t, &Config{EnableDatagrams: true}, 1500, true)
	finder := c.mtuDiscoverer
	c.handshakeSendFeedback.publish(c.pathGeneration)
	c.applyHandshakeMTUFallback()
	require.NotSame(t, finder, c.mtuDiscoverer)
	requirePathMTUSettled(t, c, false)
	now := monotime.Now()
	confirmPathMTUSettledTestConnection(t, c, cs, now)
	requirePathMTUSettled(t, c, false)
	for i := 0; !c.mtuDiscoverer.done(); i++ {
		require.Less(t, i, 30)
		requirePathMTUSettled(t, c, false)
		ping, _ := c.mtuDiscoverer.GetPing(now)
		ping.Handler.OnLost(ping.Frame)
	}
	requirePathMTUSettled(t, c, true)
	require.EqualValues(t, estimateMaxPayloadSize(protocol.MinInitialPacketSize), reportedDatagramLimit(t, c))
	// The connection's end settles again, without a second close.
	c.pathMTUSettled.settle()
	requirePathMTUSettled(t, c, true)
}

func TestPathMTUSettledConnectionEndBeforeHandshake(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		tc := newServerTestConnection(t, mockCtrl, &Config{HandshakeIdleTimeout: 7 * time.Second}, false)
		useLifecyclePacketPacker(t, mockCtrl, tc)
		tc.connRunner.EXPECT().Remove(gomock.Any()).AnyTimes()
		requirePathMTUSettled(t, tc.conn, false)
		errChan := make(chan error, 1)
		go func() { errChan <- tc.conn.run() }()
		synctest.Wait()
		requirePathMTUSettled(t, tc.conn, false)
		require.ErrorIs(t, <-errChan, &IdleTimeoutError{})
		requirePathMTUSettled(t, tc.conn, true)
	})
}

func TestPathMTUInfoCeiling(t *testing.T) {
	var early Conn
	ceiling, stageOne := early.PathMTUInfoV1()
	require.Zero(t, ceiling, "transport parameters are not known")
	require.Zero(t, stageOne, "stage one has not finished")
	for _, tc := range []struct {
		name       string
		local      packetCeiling
		peer, want protocol.ByteCount
	}{
		{name: "default", peer: 8972, want: 1452},
		{name: "local ceiling", local: 8972, peer: 9000, want: 8972},
		{name: "peer ceiling", local: 8972, peer: 1452, want: 1452},
		{name: "smaller peer", local: 8972, peer: 1400, want: 1400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newPathMTUSettledTestConnection(t, &Config{InitialPacketSize: 1232}, tc.peer, true, tc.local)
			ceiling, stageOne := c.PathMTUInfoV1()
			require.EqualValues(t, tc.want, ceiling)
			require.Zero(t, stageOne)
			require.LessOrEqual(t, c.peerMaxPacketSize(), protocol.ByteCount(1452), "F5 owns actual jumbo emission")
		})
	}
}

func TestPathMTUInfoStageOneLoss(t *testing.T) {
	c, _ := newPathMTUSettledTestConnection(t, &Config{InitialPacketSize: 1232}, 8972, true, 8972)
	now := monotime.Now()
	for i := 0; !c.mtuDiscoverer.done(); i++ {
		require.Less(t, i, 30)
		ping, size := c.mtuDiscoverer.GetPing(now)
		if size > 1392 {
			ping.Handler.OnLost(ping.Frame)
		} else {
			ping.Handler.OnAcked(ping.Frame)
		}
	}
	ceiling, stageOne := c.PathMTUInfoV1()
	require.EqualValues(t, 8972, ceiling)
	require.EqualValues(t, 1390, stageOne, "report the tested size, rather than the narrowed bound")
	c.resetPathPacketSize(now)
	ceiling, stageOne = c.PathMTUInfoV1()
	require.EqualValues(t, 8972, ceiling)
	require.Zero(t, stageOne, "a new path has not finished stage one")
}

func TestPathMTUSettledAfterTwoStages(t *testing.T) {
	c, cs := newPathMTUSettledTestConnection(t, &Config{InitialPacketSize: 1232, EnableDatagrams: true}, 8972, true, 8972)
	// Simulate jumbo receipts without emitting a jumbo packet. F5 will lift the
	// connection's emission cap once jumbo packet construction is available.
	c.mtuDiscoverer = c.newPathMTUFinder(1232, 8972)
	cs.EXPECT().Set1RTTAcked(gomock.Any(), gomock.Any()).AnyTimes()
	now := monotime.Now()
	confirmPathMTUSettledTestConnection(t, c, cs, now)
	for i := 0; !c.mtuDiscoverer.done(); i++ {
		require.Less(t, i, 20)
		requirePathMTUSettled(t, c, false)
		ping, size := c.mtuDiscoverer.GetPing(now)
		pn := c.sentPacketHandler.PopPacketNumber(protocol.Encryption1RTT)
		c.sentPacketHandler.SentPacket(now, pn, protocol.InvalidPacketNumber, nil, []ackhandler.Frame{ping}, protocol.Encryption1RTT, protocol.ECNNon, size, true, false)
		now = now.Add(10 * time.Millisecond)
		c.lastPacketReceivedTime = now
		require.NoError(t, c.handleAckFrame(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: pn, Largest: pn}}}, protocol.Encryption1RTT, now))
		if i == 3 {
			ceiling, stageOne := c.PathMTUInfoV1()
			require.EqualValues(t, 8972, ceiling)
			require.EqualValues(t, 1438, stageOne)
			requirePathMTUSettled(t, c, false)
		}
	}
	requirePathMTUSettled(t, c, true)
	require.EqualValues(t, estimateMaxPayloadSize(8957), c.maxPayloadSizeEstimate.Load(), "final limit is published before settled")
}

func TestPathMTUInfoHandshakeFallback(t *testing.T) {
	c, _ := newPathMTUSettledTestConnection(t, &Config{}, 8972, true, 8972)
	// Exercise the two-stage finder that F5 enables, without socket emission.
	c.mtuDiscoverer = c.newPathMTUFinder(c.effectiveInitialPacketSize, 8972)
	c.lowerUnsetInitialPacketSize()
	require.EqualValues(t, protocol.MinInitialPacketSize, c.mtuDiscoverer.CurrentSize())
	for i := 0; !c.mtuDiscoverer.done(); i++ {
		require.Less(t, i, 20)
		ping, _ := c.mtuDiscoverer.GetPing(monotime.Now())
		ping.Handler.OnAcked(ping.Frame)
	}
	require.Greater(t, c.mtuDiscoverer.CurrentSize(), protocol.ByteCount(8000), "handshake fallback retains the overall search ceiling")
	ceiling, stageOne := c.PathMTUInfoV1()
	require.EqualValues(t, 8972, ceiling)
	require.EqualValues(t, 1436, stageOne)
}
