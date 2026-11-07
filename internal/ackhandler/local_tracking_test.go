package ackhandler

import (
	"fmt"
	"testing"
	"time"
	"unsafe"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

func requireTrackedPackets(t *testing.T, h *sentPacketHandler, want int) {
	t.Helper()
	require.Equal(t, want, h.numTrackedPackets(), "capacity query")
}

func TestLocalTrackingHoleAndPrefix(t *testing.T) {
	for _, completeFirst := range []bool{false, true} {
		for _, outcome := range []LocalPacketOutcome{LocalPacketAccepted, LocalPacketUncertain, LocalPacketUnsubmitted} {
			t.Run(fmt.Sprintf("completion-first=%t/outcome=%d", completeFirst, outcome), func(t *testing.T) {
				h := newLocalTestHandler(t).(*sentPacketHandler)
				now := monotime.Time(time.Second)
				var tracker packetTracker
				var tokens [3]LocalCompletionToken
				for i := range tokens {
					_, tokens[i] = registerLocalTestPacket(t, h, now, protocol.EncryptionInitial, []Frame{tracker.NewPingFrame(protocol.PacketNumber(i))})
				}
				requireTrackedPackets(t, h, 3)
				_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(1)}, protocol.EncryptionInitial, now)
				require.NoError(t, err)
				requireTrackedPackets(t, h, 3) // The interior hole still represents the token.
				complete := func() {
					err := h.CompleteLocalPacket(tokens[1], outcome, now)
					if outcome == LocalPacketUnsubmitted {
						require.ErrorIs(t, err, ErrLocalCompletionContradiction)
					} else {
						require.NoError(t, err)
					}
					require.Error(t, h.CompleteLocalPacket(tokens[1], outcome, now))
				}
				if completeFirst {
					complete()
					requireTrackedPackets(t, h, 3)
				}
				_, err = h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(0)}, protocol.EncryptionInitial, now)
				require.NoError(t, err)
				if !completeFirst {
					requireTrackedPackets(t, h, 3) // Both trimmed identities still await completion.
					complete()
				}
				requireTrackedPackets(t, h, 2)
				require.NoError(t, h.CompleteLocalPacket(tokens[0], LocalPacketAccepted, now))
				requireTrackedPackets(t, h, 1)
				require.NoError(t, h.CompleteLocalPacket(tokens[2], LocalPacketUnsubmitted, now))
				requireTrackedPackets(t, h, 0)
			})
		}
	}
}

func TestLocalTrackingReplacementAndValidation(t *testing.T) {
	h := newLocalTestHandler(t).(*sentPacketHandler)
	now := monotime.Time(time.Second)
	pn, old := registerLocalTestPacket(t, h, now, protocol.EncryptionInitial, nil)
	h.ResetForRetry(now)
	// The registration interface admits a first PN in an empty replacement space.
	current, err := h.RegisterLocalPacket(PacketContext{}, now, pn, protocol.InvalidPacketNumber, nil, nil, protocol.EncryptionInitial, protocol.ECNNon, 1)
	require.NoError(t, err)
	require.NotSame(t, old.registration.epoch, current.registration.epoch)
	requireTrackedPackets(t, h, 2)
	foreign := newLocalTestHandler(t).(*sentPacketHandler)
	_, foreignToken := registerLocalTestPacket(t, foreign, now, protocol.EncryptionInitial, nil)
	for _, invalid := range []struct {
		token   LocalCompletionToken
		outcome LocalPacketOutcome
		at      monotime.Time
	}{
		{foreignToken, LocalPacketAccepted, now},
		{current, localPacketPending, now},
		{current, LocalPacketUnsubmitted + 1, now},
		{current, LocalPacketAccepted, 0},
		{current, LocalPacketAccepted, now.Add(-time.Nanosecond)},
	} {
		require.Error(t, h.CompleteLocalPacket(invalid.token, invalid.outcome, invalid.at))
		requireTrackedPackets(t, h, 2)
		require.Equal(t, 1, h.initialPackets.history.numUnresolvedLocal)
	}
	require.NoError(t, h.CompleteLocalPacket(old, LocalPacketAccepted, now))
	requireTrackedPackets(t, h, 1)
	require.Equal(t, 1, h.initialPackets.history.numUnresolvedLocal, "old epoch cannot consume the replacement slot")
	require.Error(t, h.CompleteLocalPacket(old, LocalPacketAccepted, now))
	requireTrackedPackets(t, h, 1)
	require.NoError(t, h.CompleteLocalPacket(current, LocalPacketAccepted, now))
	requireTrackedPackets(t, h, 1) // ACK-only history survives token consumption.
	require.Zero(t, h.initialPackets.history.numUnresolvedLocal)
}

func TestLocalTrackingPathRetirement(t *testing.T) {
	h := newLocalTestHandler(t).(*sentPacketHandler)
	now := monotime.Time(time.Second)
	h.DropPackets(protocol.EncryptionInitial, now)
	h.DropPackets(protocol.EncryptionHandshake, now)
	require.NoError(t, h.AddPath(17, 1200))
	var tokens [2]LocalCompletionToken
	for i, id := range []protocol.PathID{0, 17} {
		path := h.path(id)
		pn := path.PopPacketNumber(protocol.Encryption1RTT)
		token, err := h.RegisterLocalPacket(PacketContext{PathID: id}, now, pn, protocol.InvalidPacketNumber, nil, nil, protocol.Encryption1RTT, protocol.ECNNon, 1)
		require.NoError(t, err)
		tokens[i] = token
	}
	requireTrackedPackets(t, h, 2)
	requireTrackedPackets(t, h.path(17), 2)
	h.RetirePath(0)
	h.RetirePath(17)
	for _, id := range []protocol.PathID{0, 17} {
		require.False(t, h.PathQuiescent(id))
	}
	for i, id := range []protocol.PathID{0, 17} {
		require.NoError(t, h.CompleteLocalPacket(tokens[i], LocalPacketAccepted, now))
		requireTrackedPackets(t, h, 2-i)
		require.True(t, h.PathQuiescent(id))
		h.RemovePath(id)
		requireTrackedPackets(t, h, 1-i)
	}
	require.Zero(t, h.appDataPackets.history.numUnresolvedLocal)
	require.Empty(t, h.appDataPackets.history.unresolvedLocal)
}

func TestLocalTrackingSharedApplicationHistory(t *testing.T) {
	h := newLocalTestHandler(t).(*sentPacketHandler)
	now := monotime.Time(time.Second)
	h.appDataPackets.pns = newSequentialPacketNumberGenerator(0)
	_, early := registerLocalTestPacket(t, h, now, protocol.Encryption0RTT, nil)
	_, regular := registerLocalTestPacket(t, h, now, protocol.Encryption1RTT, nil)
	pn := h.PopPacketNumber(protocol.Encryption1RTT)
	mtu, err := h.RegisterLocalMTUProbe(PacketContext{}, now, pn, nil, protocol.ECNNon, 1400)
	require.NoError(t, err)
	pn = h.PopPacketNumber(protocol.Encryption1RTT)
	require.NoError(t, h.RegisterLocalPathProbe(PacketContext{}, now, pn, nil, 1200))
	hist := &h.appDataPackets.history
	require.Equal(t, []bool{true, true, true, false}, hist.unresolvedLocal)
	requireTrackedPackets(t, h, 4)
	h.DropPackets(protocol.Encryption0RTT, now)
	requireTrackedPackets(t, h, 4)
	require.Equal(t, []bool{true, true, false}, hist.unresolvedLocal)
	require.NoError(t, h.CompleteLocalPacket(early, LocalPacketUnsubmitted, now))
	requireTrackedPackets(t, h, 3)
	h.MigratedPath(now, 1200)
	requireTrackedPackets(t, h, 2)
	require.Empty(t, hist.unresolvedLocal)
	require.Zero(t, hist.numUnresolvedLocal)
	require.NoError(t, h.CompleteLocalPacket(regular, LocalPacketAccepted, now))
	require.NoError(t, h.CompleteLocalPacket(mtu, LocalPacketUncertain, now))
	requireTrackedPackets(t, h, 0)
	registerLocalTestPacket(t, h, now, protocol.Encryption1RTT, nil)
	requireTrackedPackets(t, h, 1)
	require.Equal(t, []bool{true}, hist.unresolvedLocal)
}

func BenchmarkLocalTrackingCount(b *testing.B) {
	b.Logf("layout: history=%d packet=%d handler=%d space=%d", unsafe.Sizeof(sentPacketHistory{}), unsafe.Sizeof(packet{}), unsafe.Sizeof(sentPacketHandler{}), unsafe.Sizeof(packetNumberSpace{}))
	for _, population := range []int{1, 64, 1024} {
		b.Run(fmt.Sprint(population), func(b *testing.B) {
			local, err := NewLocalSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, false, nil, protocol.PerspectiveServer, nil, utils.DefaultLogger)
			require.NoError(b, err)
			h := local.(*sentPacketHandler)
			for range population {
				pn := h.PopPacketNumber(protocol.EncryptionInitial)
				_, err := h.RegisterLocalPacket(PacketContext{}, monotime.Time(time.Second), pn, protocol.InvalidPacketNumber, nil, nil, protocol.EncryptionInitial, protocol.ECNNon, 1)
				require.NoError(b, err)
			}
			b.ReportAllocs()
			for b.Loop() {
				if got := h.numTrackedPackets(); got != population {
					b.Fatalf("tracked %d, want %d", got, population)
				}
			}
		})
	}
}
