//go:build darwin || linux || freebsd

package quic

import (
	"context"
	"os"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

// newBBRPathFixture is the reliable fixture with BBR selected: path 1 is validated
// at admissionTupleA, and every packet crosses real protection, real path recovery
// and the native endpoint.
func newBBRPathFixture(t *testing.T, perspective protocol.Perspective) *reliableFixture {
	t.Helper()
	return newReliableFixtureFor(t, perspective, nil, "bbrv3")
}

// drain runs passes as the connection loop would, sending again at once or after
// each pacing deadline, until an opportunity leaves nothing to wake for.
func (f *reliableFixture) drain(t *testing.T) {
	t.Helper()
	for range 64 {
		f.pass(t)
		switch deadline := f.c.pacingDeadline; deadline {
		case 0:
			return
		case deadlineSendImmediately:
		default:
			time.Sleep(time.Until(deadline.ToTime()) + time.Microsecond)
		}
	}
	t.Fatal("sending never settled")
}

func (f *reliableFixture) bbrPath(id protocol.PathID) *bbrSendPolicy {
	return f.c.emission.bbr.paths[id]
}

// Admission gives path 1 its own BBR controller, pacing and local credit. Its
// data spends only that policy, and only an ACK carried on path 1 trains its RTT.
func TestBBRPathTwoPathTransfer(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newBBRPathFixture(t, protocol.PerspectiveServer)
		e := &f.c.emission
		path := f.bbrPath(1)
		require.NotNil(t, path, "admission installed the path's policy")
		require.NotSame(t, e.bbr.controller, path.controller)
		require.NotSame(t, e.bbr.credit, path.credit)
		_, ok := f.recovery(1).(deliveryProbe)
		require.True(t, ok, "the path's recovery samples delivery")

		f.c.pathCredit.selectPrimaryPrivate(1)
		start := monotime.Now()
		rootRefill := e.bbr.refilledAt
		e.bbr.supplyGap = congestion.SendUnknown // setup's gap on path zero isn't this transfer's
		f.write(t, 4000)
		f.drain(t)
		onOne := f.parseWrites(t, 1)
		require.Equal(t, 4000, reliableBytes(onOne), "every byte leaves on path 1")
		require.Empty(t, f.parseWrites(t, 0))
		require.Greater(t, path.refilledAt, start, "path 1's pacing paid for it")
		require.Equal(t, rootRefill, e.bbr.refilledAt, "the root's pacing did not")
		require.Zero(t, e.bbrPendingBytes(e.bbr))

		// A cross-path ACK settles delivery without training path 1's RTT.
		time.Sleep(20 * time.Millisecond)
		f.ackPath(t, 1, 0, onOne[:1])
		require.False(t, f.c.pathCredit.rtt[1].HasMeasurement(), "a cross-path ACK trains nothing")
		f.ackPath(t, 1, 1, onOne[1:])
		require.True(t, f.c.pathCredit.rtt[1].HasMeasurement(), "a same-path ACK trains path 1")
		require.Equal(t, congestion.SendApplicationLimited, path.supplyGap, "path 1's opportunity found the supply gap")
		require.Equal(t, congestion.SendUnknown, e.bbr.supplyGap, "the root's policy saw no opportunity")
	})
}

// Final reclamation removes the path's policy with its recovery context. A late
// ACK for the reclaimed ID is ignored, a replacement gets a fresh policy, and the
// close leaves on every live path.
func TestBBRPathRetirementReclaimsPolicy(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newBBRPathFixture(t, protocol.PerspectiveClient)
		f.c.pathCredit.selectPrimaryPrivate(1)
		f.write(t, 1000)
		f.drain(t)
		old := f.parseWrites(t, 1)
		require.NotEmpty(t, old)
		retired := f.bbrPath(1)

		f.socket.writes = nil
		f.finishRetirement(t, 1)
		require.Nil(t, f.recovery(1))
		require.Nil(t, f.bbrPath(1), "reclamation removes the policy with its context")
		f.drain(t)
		require.Equal(t, 1000, reliableBytes(f.parseWrites(t, 0)), "the returned bytes leave on path zero")
		lateAck := &wire.AckFrame{HasPathID: true, PathID: 1, AckRanges: []wire.AckRange{{Smallest: old[0].pn, Largest: old[len(old)-1].pn}}}
		require.NoError(t, creditIngress(t, f.tc, monotime.Now(), lateAck), "a late ACK for a reclaimed ID is ignored")

		f.peerGrant(t, 1, 2)
		f.pass(t)
		f.openValidated(t, 2)
		require.NotNil(t, f.bbrPath(2))
		require.NotSame(t, retired, f.bbrPath(2), "a replacement gets its own policy")
		f.socket.writes = nil
		f.write(t, 500)
		f.drain(t)
		require.Equal(t, 500, reliableBytes(f.parseWrites(t, 2)))

		f.socket.writes = nil
		f.closeLocally(t, "both")
		require.Len(t, f.closeWrites(t, 0), 1)
		require.Len(t, f.closeWrites(t, 2), 1)
	})
}

// Network suspicion promotes the backup. The stranded work and new work leave on
// it, paid for by the backup's own pacing and credit.
func TestBBRPathSuspectPromotesBackup(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newBBRPathFixture(t, protocol.PerspectiveServer)
		p := f.c.pathCredit
		require.True(t, p.members[1].backup)
		backup := f.bbrPath(1)
		before := backup.refilledAt

		f.write(t, 1000)
		f.drain(t)
		require.Equal(t, before, backup.refilledAt, "the backup carries nothing while path zero is primary")
		f.socket.writes = nil
		for range suspectPTOAttempts {
			f.policyPTO(t, 0)
		}
		require.True(t, p.members[0].suspect)
		require.Equal(t, protocol.PathID(1), p.primary)
		f.drain(t)
		require.Equal(t, 1000, reliableBytes(f.parseWrites(t, 1)), "the stranded work moves to the promoted backup")
		require.Greater(t, backup.refilledAt, before, "and spends the backup's own pacing")

		f.socket.writes = nil
		f.write(t, 700)
		f.drain(t)
		require.Zero(t, reliableBytes(f.parseWrites(t, 0)))
		require.Equal(t, 700, reliableBytes(f.parseWrites(t, 1)))
		require.Zero(t, f.c.emission.bbrPendingBytes(backup))
	})
}

// A same-ID migration starts the path policy's next generation beside the
// context's MigratedPath. Old-tuple feedback still settles, and new data leaves on
// the new tuple under the same policy.
func TestBBRPathMigrationResetsPolicy(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newBBRPathFixture(t, protocol.PerspectiveClient)
		p := f.c.pathCredit
		p.selectPrimaryPrivate(1)
		require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathNewConnectionIDFrame{PathID: 1, SequenceNumber: 1, ConnectionID: replacementPeerCID(1), StatelessResetToken: protocol.StatelessResetToken{0xab}}))
		path := f.bbrPath(1)
		f.write(t, 1000)
		f.drain(t)
		old := f.parseWrites(t, 1)
		require.NotEmpty(t, old)
		generation := path.credit.generation

		target, socket := migrationTarget(t, f.admissionFixture)
		migrate(t, f.admissionFixture, 1, target, socket)
		require.Same(t, path, f.bbrPath(1), "the path keeps its policy")
		require.Equal(t, generation+1, path.credit.generation, "and starts its next generation")

		f.ackPath(t, 1, 0, old)
		require.False(t, f.recovery(1).QueueProbePacket(protocol.Encryption1RTT), "old-tuple feedback still settles")

		f.socket = socket
		f.socket.writes = nil
		f.write(t, 600)
		f.drain(t)
		require.Equal(t, 600, reliableBytes(f.parseWrites(t, 1)), "new data leaves on the new tuple")
		require.Zero(t, f.c.emission.bbrPendingBytes(path))
	})
}

// A local write timeout on the path is backpressure, not network loss: each
// uncertain write returns the path's credit, the path stays primary and the
// policy carries the next write.
func TestBBRPathLocalTimeoutIsBackpressure(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newBBRPathFixture(t, protocol.PerspectiveServer)
		p := f.c.pathCredit
		p.selectPrimaryPrivate(1)
		path := f.bbrPath(1)

		f.socket.result = func([]byte) (int, error) { return 0, os.ErrDeadlineExceeded }
		f.write(t, 3000)
		for range 4 {
			time.Sleep(endpointRetry)
			f.drain(t)
		}
		require.NotEmpty(t, f.parseWrites(t, 1), "the path attempted its writes")
		require.Zero(t, f.c.emission.bbrPendingBytes(path), "every uncertain write returned its credit")
		require.Zero(t, f.c.emission.bbrPendingBytes(f.c.emission.bbr), "and charged no other policy")
		require.False(t, p.members[1].suspect)
		require.Equal(t, protocol.PathID(1), p.primary)

		f.socket.result = nil
		f.socket.writes = nil
		f.write(t, 500)
		for range 4 {
			time.Sleep(endpointRetry)
			f.drain(t)
		}
		require.GreaterOrEqual(t, reliableBytes(f.parseWrites(t, 1)), 500, "the path's policy carries the next write")
		require.Zero(t, reliableBytes(f.parseWrites(t, 0)))
	})
}

// A DATAGRAM that fits only the backup is steered there alone, on the backup's
// pacing and credit; the primary keeps the stream data.
func TestBBRPathSteeredDatagram(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newBBRPathFixture(t, protocol.PerspectiveServer)
		f.c.pathCredit.selectPrimaryPrivate(1)
		f.setPathMTU(0, 1400)
		zero := f.c.emission.bbr
		before := zero.refilledAt
		f.queueDatagram(t, 1180) // fits only path zero, with room left for stream data
		f.write(t, 500)
		f.drain(t)

		onZero := f.parseWrites(t, 0)
		require.Equal(t, []int{1180}, reliableDatagrams(onZero))
		require.Zero(t, reliableBytes(onZero), "the steered packet takes no stream data with it")
		require.Greater(t, zero.refilledAt, before, "path zero's pacing paid for it")
		onOne := f.parseWrites(t, 1)
		require.Equal(t, 500, reliableBytes(onOne))
		require.Empty(t, reliableDatagrams(onOne))
		require.Zero(t, f.c.datagramQueue.sendDrops.Load())
	})
}

// While the primary's migration fences its old tuple, no path may carry ordinary
// work: the held opportunity sends only path zero's ACK, on path zero's credit.
func TestBBRPathHeldDuringFence(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newBBRPathFixture(t, protocol.PerspectiveClient)
		p := f.c.pathCredit
		e := &f.c.emission
		p.selectPrimaryPrivate(1)
		require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathNewConnectionIDFrame{PathID: 1, SequenceNumber: 1, ConnectionID: replacementPeerCID(1), StatelessResetToken: protocol.StatelessResetToken{0xab}}))
		target, socket := migrationTarget(t, f.admissionFixture)
		errs := migrateAsync(context.Background(), f.admissionFixture, 1, target)
		f.pass(t)
		challenges := socketChallenges(t, f.admissionFixture, socket, 1)
		require.NotEmpty(t, challenges)

		f.socket.hold = nativeProbeHoldAll
		f.socket.writes = nil
		f.write(t, 1000)
		f.pass(t)
		require.NotEmpty(t, f.socket.writes, "a submission is claimed on the old tuple")
		require.True(t, f.receive(t, 1, p.members[1].addr, 1200, &wire.PathResponseFrame{Data: challenges[0]}))
		f.pass(t)
		require.Empty(t, errs, "the claim holds the commit")
		require.True(t, p.members[1].candidate.fencing)
		require.Zero(t, e.bbrPendingBytes(e.bbr))

		f.write(t, 500)
		require.True(t, f.receive(t, 0, f.c.RemoteAddr(), 0, &wire.PingFrame{}))
		time.Sleep(50 * time.Millisecond)
		f.pass(t)
		require.True(t, e.held)
		require.Positive(t, e.bbrPendingBytes(e.bbr), "path zero's ACK is on path zero's credit")

		f.socket.hold = nil
		f.socket.release()
		synctest.Wait()
		f.pass(t)
		require.NoError(t, <-errs)
		require.Zero(t, reliableBytes(f.parseWrites(t, 0)), "no stream data left on path zero while held")
	})
}
