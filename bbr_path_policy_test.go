package quic

import (
	"testing"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/stretchr/testify/require"
)

type deliveryProbe interface {
	ObserveDeliveryStop(congestion.SendLimitation)
	DeliveryStats() congestion.DeliveryStats
}

// Each BBR path has its own pacing and local credit. Exhausting one path's
// credit is local backpressure: it reaches neither the other path nor network
// loss, and each pending authority excludes only its own construction allowance.
func TestBBRPathCreditExhaustionIsNotLoss(t *testing.T) {
	c := newEmissionTestConnection(t, false).conn
	now := monotime.Now()
	e := &c.emission
	e.enableBBR(now, protocol.MaxPacketBufferSize)
	c.sentPacketHandler.DropPackets(protocol.EncryptionInitial, now)
	c.sentPacketHandler.DropPackets(protocol.EncryptionHandshake, now)
	path, err := e.addBBRPath(17, 1200, utils.NewRTTStats(), now)
	require.NoError(t, err)
	_, err = e.addBBRPath(17, 1200, utils.NewRTTStats(), now)
	require.Error(t, err)
	require.NotSame(t, e.bbr.credit, path.credit)
	require.NotSame(t, e.bbr.controller, path.controller)
	recovery := c.sentPacketHandler.(interface {
		RecoveryOnPath(protocol.PathID) ackhandler.SentPacketHandler
	}).RecoveryOnPath(17)
	root := c.sentPacketHandler.(deliveryProbe)
	onPath := recovery.(deliveryProbe)

	queued := path.credit.reserve(2*path.quantum, 2*path.quantum, true, false)
	require.NotNil(t, queued)
	require.Nil(t, path.credit.reserve(1200, 2*path.quantum, true, false), "the path's credit is exhausted")
	onPath.ObserveDeliveryStop(congestion.SendApplicationLimited)
	require.False(t, onPath.DeliveryStats().Idle, "queued local work is not idle")
	require.Zero(t, recovery.GetLossDetectionTimeout(), "local backpressure arms no network recovery")
	require.Zero(t, onPath.DeliveryStats().Retained)

	root.ObserveDeliveryStop(congestion.SendApplicationLimited)
	require.True(t, root.DeliveryStats().Idle, "another path's queued work does not hold the root")
	budget := e.bbr.budget(now)
	path.sent(path.budget(now), now)
	require.Equal(t, budget, e.bbr.budget(now), "pacing is per path")

	// A path's construction allowance is excluded from its own pending work only.
	e.reservation = path.credit.reserve(1200, 4*path.quantum, false, false)
	require.NotNil(t, e.reservation)
	require.Zero(t, e.bbrPendingBytes(e.bbr))
	require.Equal(t, 2*path.quantum, e.bbrPendingBytes(path))
	require.True(t, root.DeliveryStats().Idle)
	e.reservation.completeLocal()
	e.reservation = nil
	queued.complete()
	require.Zero(t, e.bbrPendingBytes(path))
	require.True(t, onPath.DeliveryStats().Idle)

	e.removeBBRPath(17)
	require.Nil(t, e.bbr.paths[17])
	require.Nil(t, c.sentPacketHandler.(interface {
		RecoveryOnPath(protocol.PathID) ackhandler.SentPacketHandler
	}).RecoveryOnPath(17), "the policy and its recovery context are reclaimed together")
}

// A BBR path validates ECN beside a validating root, fenced by its own credit:
// after a migration, only this path's old-generation sends hold its marking.
func TestBBRPathECNFollowsPathCredit(t *testing.T) {
	c := newEmissionTestConnection(t, false).conn
	now := monotime.Now()
	conn := &ecnTestSendConn{sendConn: c.conn, capable: true}
	c.conn = conn
	e := &c.emission
	e.enableBBR(now, protocol.MaxPacketBufferSize)
	e.enableBBRECN()
	c.sentPacketHandler.DropPackets(protocol.EncryptionInitial, now)
	c.sentPacketHandler.DropPackets(protocol.EncryptionHandshake, now)
	path, err := e.addBBRPath(17, 1200, utils.NewRTTStats(), now)
	require.NoError(t, err)
	recovery := c.sentPacketHandler.(interface {
		RecoveryOnPath(protocol.PathID) ackhandler.SentPacketHandler
	}).RecoveryOnPath(17)
	require.Equal(t, protocol.ECT0, recovery.ECNMode(true))

	old := path.credit.reserve(1200, 2*path.quantum, false, false)
	require.NotNil(t, old)
	path.sent(path.budget(now), now)
	recovery.MigratedPath(now, protocol.MinInitialPacketSize)
	e.resetBBRPath(17, now)
	e.resetBBRPath(18, now) // no such path
	require.Equal(t, path.quantum, path.budget(now), "the new tuple starts with full pacing credit")
	require.Equal(t, protocol.ECNNon, recovery.ECNMode(true), "this path's old-generation send is pending")
	root := e.bbr.credit.reserve(1200, 2*e.bbr.quantum, false, false)
	require.NotNil(t, root)
	old.complete()
	require.Equal(t, protocol.ECT0, recovery.ECNMode(true), "another path's pending work does not hold the fence")
	root.complete()

	conn.capable = false
	require.Equal(t, protocol.ECNUnsupported, recovery.ECNMode(true))
}
