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

func newLocalPendingSpanTest(t *testing.T, ecn bool) (*sentPacketHandler, []LocalCompletionToken) {
	t.Helper()
	handler, err := NewLocalSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, ecn, nil, protocol.PerspectiveServer, nil, utils.DefaultLogger)
	require.NoError(t, err)
	h := handler.(*sentPacketHandler)
	now := monotime.Time(time.Second)
	h.DropPackets(protocol.EncryptionInitial, now)
	h.DropPackets(protocol.EncryptionHandshake, now)
	tokens := make([]LocalCompletionToken, 4)
	for i := range tokens {
		mark := protocol.ECNNon
		if ecn {
			mark = protocol.ECT0
		}
		pn := h.PopPacketNumber(protocol.Encryption1RTT)
		tokens[i], err = h.RegisterLocalPacket(PacketContext{}, now, pn, protocol.InvalidPacketNumber, nil, []Frame{{Frame: &wire.PingFrame{}}}, protocol.Encryption1RTT, mark, 1200)
		require.NoError(t, err)
	}
	return h, tokens
}

func newLocalSpanTest(t *testing.T, ecn bool) (*sentPacketHandler, []LocalCompletionToken) {
	t.Helper()
	h, tokens := newLocalPendingSpanTest(t, ecn)
	require.NoError(t, h.CompleteLocalPacket(tokens[0], LocalPacketAccepted, monotime.Time(time.Second)))
	return h, tokens[1:]
}

func TestLocalAcceptedSpanScalarEquivalence(t *testing.T) {
	for _, ecn := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "ECN"}[ecn], func(t *testing.T) {
			fast, tokens := newLocalSpanTest(t, ecn)
			scalar, scalarTokens := newLocalSpanTest(t, ecn)
			returnedAt := monotime.Time(time.Second + time.Millisecond)
			fast.rttStats.UpdateRTT(8*time.Millisecond, 0)
			scalar.rttStats.UpdateRTT(8*time.Millisecond, 0)
			require.True(t, TryCompleteLocalAcceptedPackets(fast, tokens, returnedAt))
			for _, token := range scalarTokens {
				require.NoError(t, scalar.CompleteLocalPacket(token, LocalPacketAccepted, returnedAt))
			}
			require.Equal(t, scalar.GetLossDetectionTimeout(), fast.GetLossDetectionTimeout())
			require.Equal(t, scalar.bytesInFlight, fast.bytesInFlight)
			require.Equal(t, scalar.local.owner, fast.local.owner)
			require.Equal(t, scalar.appDataPackets.history.numUnresolvedLocal, fast.appDataPackets.history.numUnresolvedLocal)
			require.Equal(t, scalar.appDataPackets.history.Difference(3, 0), fast.appDataPackets.history.Difference(3, 0))
			for i, token := range tokens {
				require.Equal(t, scalarTokens[i].registration.outcome, token.registration.outcome)
				require.Equal(t, returnedAt, token.registration.eligibleAt)
				require.ErrorIs(t, fast.CompleteLocalPacket(token, LocalPacketAccepted, returnedAt), errLocalCompletionToken)
			}
			if ecn {
				require.Equal(t, scalar.local.ecn.confirmed, fast.local.ecn.confirmed)
				require.Equal(t, scalar.local.ecn.unresolved, fast.local.ecn.unresolved)
			}
		})
	}
}

func TestLocalAcceptedSpanDeclinesAtomically(t *testing.T) {
	for _, kind := range []string{"duplicate", "reversed", "foreign", "old epoch", "consumed", "early ACK", "rich", "pending ECN", "lost queue", "MTU", "handshake", "backward time", "physical prefix"} {
		t.Run(kind, func(t *testing.T) {
			h, tokens := newLocalSpanTest(t, kind == "pending ECN")
			at := monotime.Time(time.Second + time.Millisecond)
			switch kind {
			case "duplicate":
				tokens[2] = tokens[1]
			case "reversed":
				tokens[1], tokens[2] = tokens[2], tokens[1]
			case "foreign":
				_, other := newLocalSpanTest(t, false)
				tokens[2] = other[2]
			case "old epoch":
				h.appDataPackets = h.appDataPackets.resetForRetry(true)
			case "consumed":
				require.NoError(t, h.CompleteLocalPacket(tokens[2], LocalPacketAccepted, at))
			case "early ACK":
				_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(tokens[2].registration.pn)}, protocol.Encryption1RTT, at)
				require.NoError(t, err)
			case "rich":
				h.congestionEvents = &congestionDispatch{}
			case "pending ECN":
				h.local.ecn.pending = true
			case "lost queue":
				h.lostPackets.Add(100, at)
			case "MTU":
				tokens[2].registration.packet.IsPathMTUProbePacket = true
			case "handshake":
				h.handshakeConfirmed = false
			case "backward time":
				at = monotime.Time(time.Second - 1)
			case "physical prefix":
				h.appDataPackets.largestAcked = 0
			}
			before := h.local.owner
			alarm := h.GetLossDetectionTimeout()
			require.False(t, TryCompleteLocalAcceptedPackets(h, tokens, at))
			require.Equal(t, before, h.local.owner)
			require.Equal(t, alarm, h.GetLossDetectionTimeout())
			require.Equal(t, localPacketPending, tokens[0].registration.outcome, "preflight must not consume a valid prefix")
		})
	}
}

func TestLocalAcceptedSpanAfterScalarPrefix(t *testing.T) {
	for _, ecn := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "ECN"}[ecn], func(t *testing.T) {
			fast, tokens := newLocalPendingSpanTest(t, ecn)
			scalar, scalarTokens := newLocalPendingSpanTest(t, ecn)
			returnedAt := monotime.Time(time.Second + time.Millisecond)
			fast.rttStats.UpdateRTT(8*time.Millisecond, 0)
			scalar.rttStats.UpdateRTT(8*time.Millisecond, 0)
			before := fast.local.owner
			require.Zero(t, fast.appDataPackets.history.latestLocalPTO())
			require.False(t, TryCompleteLocalAcceptedPackets(fast, tokens, returnedAt))
			require.Equal(t, before, fast.local.owner)
			require.Zero(t, fast.GetLossDetectionTimeout())
			require.Equal(t, len(tokens), fast.appDataPackets.history.numUnresolvedLocal)
			for _, token := range tokens {
				require.Equal(t, localPacketPending, token.registration.outcome)
			}
			require.NoError(t, fast.CompleteLocalPacket(tokens[0], LocalPacketAccepted, returnedAt))
			require.Equal(t, returnedAt, fast.appDataPackets.history.latestLocalPTO())
			require.True(t, TryCompleteLocalAcceptedPackets(fast, tokens[1:], returnedAt))
			for _, token := range scalarTokens {
				require.NoError(t, scalar.CompleteLocalPacket(token, LocalPacketAccepted, returnedAt))
			}
			require.Equal(t, scalar.GetLossDetectionTimeout(), fast.GetLossDetectionTimeout())
			require.Greater(t, fast.GetLossDetectionTimeout(), returnedAt)
			require.Equal(t, scalar.bytesInFlight, fast.bytesInFlight)
			require.Equal(t, scalar.local.owner, fast.local.owner)
			require.Equal(t, localCompletionOwner{}, fast.local.owner)
			require.Zero(t, fast.appDataPackets.history.numUnresolvedLocal)
			require.Equal(t, scalar.appDataPackets.history.Difference(3, 0), fast.appDataPackets.history.Difference(3, 0))
			for _, token := range tokens {
				require.Equal(t, LocalPacketAccepted, token.registration.outcome)
				require.Equal(t, returnedAt, token.registration.eligibleAt)
				require.ErrorIs(t, fast.CompleteLocalPacket(token, LocalPacketAccepted, returnedAt), errLocalCompletionToken)
			}
			if ecn {
				require.Equal(t, scalar.local.ecn.confirmed, fast.local.ecn.confirmed)
				require.Equal(t, scalar.local.ecn.unresolved, fast.local.ecn.unresolved)
			}
		})
	}
	for _, kind := range []string{"ACK-retired prefix", "consumed suffix", "ACK-retired suffix"} {
		t.Run(kind, func(t *testing.T) {
			h, tokens := newLocalPendingSpanTest(t, false)
			at := monotime.Time(time.Second + time.Millisecond)
			require.False(t, TryCompleteLocalAcceptedPackets(h, tokens, at))
			if kind == "ACK-retired prefix" {
				_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(tokens[0].registration.pn)}, protocol.Encryption1RTT, at)
				require.NoError(t, err)
			}
			require.NoError(t, h.CompleteLocalPacket(tokens[0], LocalPacketAccepted, at))
			switch kind {
			case "ACK-retired prefix":
				require.Zero(t, h.appDataPackets.history.latestLocalPTO(), "an ACK-retired prefix cannot establish PTO evidence")
			case "consumed suffix":
				require.NoError(t, h.CompleteLocalPacket(tokens[3], LocalPacketAccepted, at))
			case "ACK-retired suffix":
				_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(tokens[3].registration.pn)}, protocol.Encryption1RTT, at)
				require.NoError(t, err)
			}
			before := h.local.owner
			alarm, flight := h.GetLossDetectionTimeout(), h.bytesInFlight
			unresolved := h.appDataPackets.history.numUnresolvedLocal
			require.False(t, TryCompleteLocalAcceptedPackets(h, tokens[1:], at))
			require.Equal(t, before, h.local.owner)
			require.Equal(t, alarm, h.GetLossDetectionTimeout())
			require.Equal(t, flight, h.bytesInFlight)
			require.Equal(t, unresolved, h.appDataPackets.history.numUnresolvedLocal)
			for _, token := range tokens[1:3] {
				require.Equal(t, localPacketPending, token.registration.outcome, "retry refusal leaves the untouched prefix pending")
				require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, at))
			}
			if kind != "consumed suffix" {
				require.NoError(t, h.CompleteLocalPacket(tokens[3], LocalPacketAccepted, at))
			}
			require.Equal(t, localCompletionOwner{}, h.local.owner)
		})
	}
}
