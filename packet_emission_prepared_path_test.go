//go:build darwin || linux || freebsd

package quic

import (
	"testing"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
)

func TestPreparedEmissionAdmittedOrdinaryRoutes(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		c := f.c
		var contexts []ackhandler.PacketContext
		zeroRegistrations := 0
		c.emission.policy = &preparedTestPolicy{
			emissionPolicy: c.emission.policy,
			onLog:          func(v ackhandler.PreparedPacketView, _ nativeHeaderFacts) { contexts = append(contexts, v.Context()) },
			onActivity:     func(ackhandler.PreparedPacketView) {},
			onRegistration: func() { zeroRegistrations++ },
		}
		c.pathCredit.selectPrimaryPrivate(1)
		f.write(t, 100)
		f.pass(t)
		require.Equal(t, 100, reliableBytes(f.parseWrites(t, 1)))
		require.NotEmpty(t, contexts, "admitted nonzero native route uses preparation transfer")
		require.Equal(t, protocol.PathID(1), contexts[0].PathID)
		require.Equal(t, c.pathCredit.members[1].tuple, contexts[0].TupleGeneration)
		require.Zero(t, zeroRegistrations, "nonzero path use cannot charge path-zero CID rotation")
		contexts = nil
		c.pathCredit.selectPrimaryPrivate(0)
		f.write(t, 100)
		f.pass(t)
		require.NotEmpty(t, contexts, "native path zero retains preparation transfer when path credit is installed")
		require.Equal(t, protocol.PathID(0), contexts[0].PathID)
		require.Positive(t, zeroRegistrations)
		require.Equal(t, 100, reliableBytes(f.parseWrites(t, 0)))
	})
}
