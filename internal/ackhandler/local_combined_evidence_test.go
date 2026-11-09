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

func TestE07LocalPTOCurrentEvidence(t *testing.T) {
	h := newLocalTestHandler(t).(*sentPacketHandler)
	now := monotime.Time(time.Second)
	h.DropPackets(protocol.EncryptionInitial, now)
	h.DropPackets(protocol.EncryptionHandshake, now)
	t.Cleanup(func() { h.CloseLocal(now.Add(time.Second)) })
	h.rttStats.UpdateRTT(20*time.Millisecond, 0)
	h.rttStats.SetMaxAckDelay(25 * time.Millisecond)
	h.ptoCount = 2
	_, token := registerLocalTestPacket(t, h, now, protocol.Encryption1RTT, []Frame{{Frame: &wire.PingFrame{}}})
	when, level := h.localPTOTimeAndSpace()
	require.Zero(t, when)
	require.Equal(t, protocol.EncryptionLevel(0), level)
	returned := now.Add(time.Millisecond)
	require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, returned))
	when, level = h.localPTOTimeAndSpace()
	require.Equal(t, returned.Add(h.getScaledPTO(true)), when)
	require.Equal(t, protocol.Encryption1RTT, level)
	previous := when
	h.rttStats.UpdateRTT(80*time.Millisecond, 0)
	when, _ = h.localPTOTimeAndSpace()
	require.Greater(t, when, previous)
	require.Equal(t, returned.Add(h.getScaledPTO(true)), when)
	previous = when
	h.ptoCount++
	when, _ = h.localPTOTimeAndSpace()
	require.Greater(t, when, previous)
	require.Equal(t, returned.Add(h.getScaledPTO(true)), when)
}

func TestE07AcceptedECNPhases(t *testing.T) {
	for _, tc := range []struct {
		name          string
		active        bool
		firstSkip     protocol.PacketNumber
		packetNumbers []protocol.PacketNumber
	}{
		{"unknown/contiguous", false, 32, []protocol.PacketNumber{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14}},
		{"unknown/gap", false, 14, []protocol.PacketNumber{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 15}},
		{"active/contiguous", true, 32, []protocol.PacketNumber{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}},
		{"active/gap", true, 15, []protocol.PacketNumber{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 16}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			active := tc.active
			newState := func() (*sentPacketHandler, []LocalCompletionToken, monotime.Time, []protocol.PacketNumber) {
				local, err := NewLocalSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, true, nil, protocol.PerspectiveServer, nil, utils.DefaultLogger)
				require.NoError(t, err)
				h := local.(*sentPacketHandler)
				// Match legal PN histories; ECN ranges must not merge across a skip.
				// Any later randomly chosen skip lies beyond this finite fixture.
				h.appDataPackets.pns.(*skippingPacketNumberGenerator).nextToSkip = tc.firstSkip
				var packetNumbers []protocol.PacketNumber
				now := monotime.Time(time.Second)
				h.DropPackets(protocol.EncryptionInitial, now)
				h.DropPackets(protocol.EncryptionHandshake, now)
				t.Cleanup(func() { h.CloseLocal(now.Add(time.Second)) })
				register := func() LocalCompletionToken {
					pn := h.PopPacketNumber(protocol.Encryption1RTT)
					token, err := h.RegisterLocalPacket(PacketContext{}, now, pn, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, protocol.ECT0, 1200)
					require.NoError(t, err)
					packetNumbers = append(packetNumbers, pn)
					return token
				}
				require.Equal(t, protocol.ECT0, h.ECNMode(true))
				seed := register()
				require.NoError(t, h.CompleteLocalPacket(seed, LocalPacketAccepted, now.Add(time.Millisecond)))
				if active {
					_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(seed.registration.pn), ECT0: 1}, protocol.Encryption1RTT, now.Add(2*time.Millisecond))
					require.NoError(t, err)
					now = now.Add(3 * time.Millisecond)
					seed = register()
					require.NoError(t, h.CompleteLocalPacket(seed, LocalPacketAccepted, now.Add(time.Millisecond)))
				}
				require.Equal(t, protocol.ECT0, h.ECNMode(true))
				tokens := make([]LocalCompletionToken, 14)
				for i := range tokens {
					tokens[i] = register()
				}
				returned := now.Add(5 * time.Millisecond)
				require.True(t, TryCompleteLocalAcceptedPackets(h, tokens[:12], returned))
				if active {
					require.Equal(t, ecnStateCapable, h.ecnTracker.(*ecnTracker).state)
					require.Zero(t, h.local.ecn.testingPending)
				} else {
					require.Equal(t, ecnStateUnknown, h.ecnTracker.(*ecnTracker).state)
					require.Equal(t, 2, h.local.ecn.testingPending)
				}
				return h, tokens[12:], returned, packetNumbers
			}
			fast, tokens, returned, fastPNs := newState()
			scalar, scalarTokens, scalarReturned, scalarPNs := newState()
			require.Equal(t, tc.packetNumbers, fastPNs)
			require.Equal(t, fastPNs, scalarPNs)
			require.True(t, TryCompleteLocalAcceptedPackets(fast, tokens, returned))
			for _, token := range scalarTokens {
				require.NoError(t, scalar.CompleteLocalPacket(token, LocalPacketAccepted, scalarReturned))
			}
			require.Zero(t, fast.local.ecn.testingPending)
			require.Zero(t, fast.local.ecn.unresolved)
			require.Equal(t, scalar.local.ecn.confirmed, fast.local.ecn.confirmed)
			require.Equal(t, scalar.ecnTracker.(*ecnTracker).state, fast.ecnTracker.(*ecnTracker).state)
			require.Equal(t, scalar.ecnTracker.(*ecnTracker).numSentECT0, fast.ecnTracker.(*ecnTracker).numSentECT0)
			require.Equal(t, scalar.local.ecn.records.len(), fast.local.ecn.records.len())
			for i := range fast.local.ecn.records.len() {
				require.Equal(t, *scalar.local.ecn.records.at(i), *fast.local.ecn.records.at(i))
			}
			require.Equal(t, scalar.local.owner, fast.local.owner)
			require.Equal(t, scalar.appDataPackets.history.numUnresolvedLocal, fast.appDataPackets.history.numUnresolvedLocal)
			require.Equal(t, scalar.GetLossDetectionTimeout(), fast.GetLossDetectionTimeout())
			require.Equal(t, scalar.bytesInFlight, fast.bytesInFlight)
		})
	}
}
