package ackhandler

import (
	"testing"
	"time"
	"unsafe"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

func TestLocalRegistrationIdentityAndLifetime(t *testing.T) {
	// The token carries the one canonical identity, rather than another mutable
	// owner/id/epoch projection. Its lifetime is independent of a pooled packet.
	require.Equal(t, unsafe.Sizeof(uintptr(0)), unsafe.Sizeof(LocalCompletionToken{}))
	t.Logf("bytes: token=%d registration=%d chunk=%d owner=%d local=%d packet=%d history=%d ECN record=%d", unsafe.Sizeof(LocalCompletionToken{}), unsafe.Sizeof(localRegistration{}), unsafe.Sizeof([localRegistrationChunkSize]localRegistration{}), unsafe.Sizeof(localCompletionOwner{}), unsafe.Sizeof(localCompletions{}), unsafe.Sizeof(packet{}), unsafe.Sizeof(sentPacketHistory{}), unsafe.Sizeof(localECNRecord{}))
	h := newLocalTestHandler(t).(*sentPacketHandler)
	now := monotime.Time(time.Second)
	var tracker packetTracker
	pn, old := registerLocalTestPacket(t, h, now, protocol.EncryptionInitial, []Frame{tracker.NewPingFrame(0)})
	_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pn)}, protocol.EncryptionInitial, now.Add(time.Millisecond))
	require.NoError(t, err)
	require.NoError(t, h.CompleteLocalPacket(old, LocalPacketAccepted, now.Add(time.Millisecond)))
	for range 24 {
		_, token := registerLocalTestPacket(t, h, now, protocol.EncryptionInitial, nil)
		require.NotEqual(t, old, token)
		require.NoError(t, h.CompleteLocalPacket(token, LocalPacketAccepted, now))
		require.ErrorIs(t, h.CompleteLocalPacket(old, LocalPacketAccepted, now), errLocalCompletionToken)
	}
}

func TestLocalRegistrationRetirementAndOldReader(t *testing.T) {
	h := newLocalTestHandler(t).(*sentPacketHandler)
	now := monotime.Time(time.Second)
	var tracker packetTracker
	pn, early := registerLocalTestPacket(t, h, now, protocol.EncryptionInitial, []Frame{tracker.NewPingFrame(0)})
	_, late := registerLocalTestPacket(t, h, now, protocol.EncryptionInitial, []Frame{tracker.NewPingFrame(1)})
	require.Equal(t, localCompletionOwner{pending: 2, live: 2}, h.local.owner)
	_, err := h.ReceivedAck(&wire.AckFrame{AckRanges: ackRanges(pn)}, protocol.EncryptionInitial, now)
	require.NoError(t, err)
	require.Equal(t, localCompletionOwner{pending: 2, live: 1}, h.local.owner)
	require.Nil(t, early.registration.packet)
	oldHistory := &h.initialPackets.history
	oldPacket := late.registration.packet
	h.ResetForRetry(now.Add(time.Millisecond))
	require.Equal(t, localCompletionOwner{pending: 2}, h.local.owner)
	var retained []*packet
	for _, p := range oldHistory.Packets() {
		retained = append(retained, p)
	}
	require.Equal(t, []*packet{oldPacket}, retained, "Retry preserves old physical history readers")
	require.Nil(t, late.registration.packet)
	require.NoError(t, h.CompleteLocalPacket(early, LocalPacketAccepted, now.Add(time.Millisecond)))
	require.NoError(t, h.CompleteLocalPacket(late, LocalPacketUnsubmitted, now.Add(time.Millisecond)))
	require.Equal(t, localCompletionOwner{}, h.local.owner)
	require.Zero(t, h.initialPackets.history.Len())
}
