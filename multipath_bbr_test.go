//go:build darwin || linux || freebsd

package quic

import (
	"context"
	"fmt"
	"os"
	"slices"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
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

// A failed or uncertain local write on the path is backpressure, not network
// loss: each write returns the path's credit, the path stays primary and the
// policy carries the next write.
func TestBBRPathLocalWriteFailureIsBackpressure(t *testing.T) {
	for name, nativeError := range map[string]error{"timeout": os.ErrDeadlineExceeded, "capacity": syscall.ENOBUFS} {
		t.Run(name, func(t *testing.T) {
			nativeOrdinaryTest(t, func(t *testing.T) {
				f := newBBRPathFixture(t, protocol.PerspectiveServer)
				p := f.c.pathCredit
				p.selectPrimaryPrivate(1)
				path := f.bbrPath(1)
				settle := func() {
					for range 4 {
						time.Sleep(endpointRetry)
						f.drain(t)
					}
				}

				f.socket.result = func([]byte) (int, error) { return 0, nativeError }
				f.write(t, 3000)
				settle()
				require.NotEmpty(t, f.parseWrites(t, 1), "the path attempted its writes")
				require.Zero(t, f.c.emission.bbrPendingBytes(path), "every local result returned its credit")
				require.Zero(t, f.c.emission.bbrPendingBytes(f.c.emission.bbr), "and charged no other policy")
				require.False(t, p.members[1].suspect)
				require.Equal(t, protocol.PathID(1), p.primary)

				f.socket.result = nil
				f.socket.writes = nil
				f.write(t, 500)
				settle()
				require.GreaterOrEqual(t, reliableBytes(f.parseWrites(t, 1)), 500, "the path's policy carries the next write")
				require.Zero(t, reliableBytes(f.parseWrites(t, 0)))
			})
		})
	}
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

// Selection can move the opportunity off the route whose native construction
// capacity the opportunity's first check reserved. An ACK sent while the new
// route is paced still has capacity to register on.
func TestBBRPathRouteChangeReacquiresCapacity(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newBBRPathFixture(t, protocol.PerspectiveServer)
		p := f.c.pathCredit
		p.selectPrimaryPrivate(1)
		f.write(t, 1000)
		f.drain(t)
		require.Equal(t, 1000, reliableBytes(f.parseWrites(t, 1)))

		p.selectPrimaryPrivate(0)
		zero := f.c.emission.bbr
		require.True(t, f.receive(t, 0, f.c.RemoteAddr(), 0, &wire.PingFrame{}))
		f.socket.writes = nil
		time.Sleep(50 * time.Millisecond) // the ACK is due
		zero.tokens, zero.updated = 0, monotime.Now()
		f.pass(t)
		var acks int
		for _, write := range f.parseWrites(t, 0) {
			acks += len(write.acks)
		}
		require.Equal(t, 1, acks, "path zero's ACK leaves while its data is paced")
	})
}

// An admission probe for another path is not the selected route's opportunity:
// it leaves path zero's supply gap and idle state as they were.
func TestBBRPathProbeKeepsSelectedSupplyState(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newBBRPathFixture(t, protocol.PerspectiveClient)
		e := &f.c.emission
		require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathNewConnectionIDFrame{PathID: 1, SequenceNumber: 1, ConnectionID: replacementPeerCID(1), StatelessResetToken: protocol.StatelessResetToken{0xab}}))
		f.write(t, 1000)
		f.drain(t)
		f.ackPath(t, 0, 0, f.parseWrites(t, 0))
		f.drain(t)
		root := f.c.sentPacketHandler.(deliveryProbe)
		require.Equal(t, congestion.SendApplicationLimited, e.bbr.supplyGap)
		require.True(t, root.DeliveryStats().Idle)

		target, socket := migrationTarget(t, f.admissionFixture)
		ctx, cancel := context.WithCancel(context.Background())
		errs := migrateAsync(ctx, f.admissionFixture, 1, target)
		synctest.Wait()
		require.NoError(t, f.c.pathCredit.expire(monotime.Now()))
		result := f.c.triggerSending(monotime.Now())
		require.NoError(t, result.err)
		synctest.Wait()
		require.NotEmpty(t, socketChallenges(t, f.admissionFixture, socket, 1), "this opportunity was path 1's challenge")
		require.Equal(t, congestion.SendApplicationLimited, e.bbr.supplyGap, "path zero's gap stays")
		require.True(t, root.DeliveryStats().Idle, "and so does its idle state")
		cancel()
		f.pass(t)
		require.ErrorIs(t, <-errs, context.Canceled)
	})
}

// A cutover's announcement never waits on pacing, but whatever payload it carries
// is the announced route's ordinary paced work: with that route's pacing or
// ordinary credit refusing a whole packet, it is a PING alone and spends nothing.
func TestBBRPathAnnouncementPacing(t *testing.T) {
	// announce migrates path 1, the primary, holding prepare's reservation across
	// the cutover, then sets path 1's pacing tokens, queues 1,000 bytes and runs the
	// one opportunity the due announcement takes. It returns the packets on the new tuple.
	announce := func(t *testing.T, f *reliableFixture, tokens func(*bbrSendPolicy) int64, prepare func(*bbrSendPolicy) *sendReservation) []reliableWrite {
		t.Helper()
		p := f.c.pathCredit
		p.selectPrimaryPrivate(1)
		require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathNewConnectionIDFrame{PathID: 1, SequenceNumber: 1, ConnectionID: replacementPeerCID(1), StatelessResetToken: protocol.StatelessResetToken{0xab}}))
		path := f.bbrPath(1)
		var held *sendReservation
		if prepare != nil {
			held = prepare(path)
			t.Cleanup(held.completeLocal)
		}
		target, socket := migrationTarget(t, f.admissionFixture)
		migrate(t, f.admissionFixture, 1, target, socket)
		f.socket = socket
		f.socket.writes = nil
		m := p.members[1]
		require.True(t, m.announcing)
		m.announceAt = monotime.Now()
		now := monotime.Now()
		path.budget(now)
		path.tokens, path.updated = tokens(path), now
		attempts := m.announces
		f.write(t, 1000)
		// Only the announcement's own opportunity, so no later ordinary one can
		// restore the supply state it might have changed.
		require.NoError(t, f.c.emission.consumeLocal())
		require.NoError(t, p.expire(monotime.Now()))
		require.NoError(t, f.c.triggerSending(monotime.Now()).err)
		synctest.Wait()
		require.NoError(t, f.c.emission.consumeLocal())
		writes := f.parseWrites(t, 1)
		require.Len(t, writes, 1, "the announcement leaves on the new tuple")
		require.Equal(t, attempts+1, m.announces, "the attempt is recorded")
		require.True(t, m.announceAt.After(monotime.Now()), "and backs off")
		return writes
	}
	pingOnly := func(t *testing.T, write reliableWrite) {
		t.Helper()
		require.Empty(t, write.streams, "no STREAM payload")
		require.Empty(t, write.datagrams, "no DATAGRAM payload")
		require.Len(t, write.frames, 1)
		require.IsType(t, &wire.PingFrame{}, write.frames[0], "a PING alone")
	}

	t.Run("zero budget", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newBBRPathFixture(t, protocol.PerspectiveClient)
			path := f.bbrPath(1)
			var refilled monotime.Time
			writes := announce(t, f, func(p *bbrSendPolicy) int64 {
				refilled = p.refilledAt
				return 0
			}, nil)
			pingOnly(t, writes[0])
			require.Zero(t, path.tokens, "the exempt announcement spends no pacing")
			require.Equal(t, refilled, path.refilledAt)

			f.ackPath(t, 1, 0, writes)
			require.False(t, f.c.pathCredit.members[1].announcing, "path 1's recovery registered it, so its ACK discharges the obligation")
			f.drain(t)
			require.Equal(t, 1000, reliableBytes(f.parseWrites(t, 1)), "the data waited for pacing")
		})
	})

	t.Run("budget covers", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newBBRPathFixture(t, protocol.PerspectiveClient)
			e := &f.c.emission
			path := f.bbrPath(1)
			var root bbrSendPolicy
			var refilled monotime.Time
			var before protocol.ByteCount
			var gap congestion.SendLimitation
			writes := announce(t, f, func(p *bbrSendPolicy) int64 {
				refilled, before, gap = p.refilledAt, p.quantum, p.supplyGap
				root = *e.bbr
				return int64(p.quantum) * 1e9
			}, nil)
			require.Equal(t, 1000, reliableBytes(writes), "payload rides the paced announcement")
			require.True(t, slices.ContainsFunc(writes[0].frames, func(f wire.Frame) bool {
				_, ok := f.(*wire.PingFrame)
				return ok
			}), "with its PING")
			require.Equal(t, before-protocol.ByteCount(len(writes[0].data)), path.budget(monotime.Now()), "the announced route pays the packet's bytes")
			require.True(t, path.refilledAt.After(refilled), "and its refill moves")
			require.Equal(t, root.tokens, e.bbr.tokens, "the root policy pays nothing")
			require.Equal(t, root.refilledAt, e.bbr.refilledAt)
			require.Equal(t, root.supplyGap, e.bbr.supplyGap, "and no route's supply is observed")
			require.Equal(t, gap, path.supplyGap)
		})
	})

	t.Run("older generation pending", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newBBRPathFixture(t, protocol.PerspectiveClient)
			path := f.bbrPath(1)
			var refilled monotime.Time
			writes := announce(t, f, func(p *bbrSendPolicy) int64 {
				refilled = p.refilledAt
				return int64(p.quantum) * 1e9
			}, func(p *bbrSendPolicy) *sendReservation {
				// Old-tuple bytes still pending refuse ordinary credit after the cutover.
				return p.credit.reserve(100, p.creditLimit(), false, false)
			})
			pingOnly(t, writes[0])
			require.Equal(t, int64(path.quantum)*1e9, path.tokens, "ordinary credit refused the payload, so nothing is paced")
			require.Equal(t, refilled, path.refilledAt)
		})
	})
}

// A BBR receiver's ACK turn on a backup charges that route's local credit, but no
// pacing, and is a signaling turn: path zero's supply state stays as it was. Turns
// take no payload, whatever is queued. The backup's own count puts the periodic PING
// on every twentieth turn, however much the primary sends between them.
func TestBBRPathAckTurn(t *testing.T) {
	noKeyUpdates(t)
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newBBRPathFixture(t, protocol.PerspectiveServer)
		e := &f.c.emission
		f.peerSends(t, 1, 2)
		f.write(t, 1000)
		f.drain(t)
		f.ackPath(t, 0, 0, f.parseWrites(t, 0))
		f.drain(t)
		f.socket.writes = nil
		root := f.c.sentPacketHandler.(deliveryProbe)
		require.Equal(t, congestion.SendApplicationLimited, e.bbr.supplyGap)
		require.True(t, root.DeliveryStats().Idle)

		path := f.bbrPath(1)
		tokens := path.tokens
		f.peerSends(t, 1, 2)
		// Hold the write, so the turn's credit is still pending when it's read.
		f.socket.hold = nativeProbeHoldAll
		result := f.c.triggerSending(monotime.Now())
		require.NoError(t, result.err)
		require.True(t, result.retry, "a sent turn requests continuation")
		synctest.Wait()
		path.credit.mu.Lock()
		charged := path.credit.pending
		path.credit.mu.Unlock()
		require.Positive(t, charged, "the turn holds its route's credit")
		f.socket.hold = nil
		f.socket.release()
		synctest.Wait()
		require.NoError(t, e.consumeLocal())
		require.Equal(t, tokens, path.tokens, "and debits no pacing")
		require.Equal(t, congestion.SendApplicationLimited, e.bbr.supplyGap, "path zero's gap stays")
		require.True(t, root.DeliveryStats().Idle, "and so does its idle state")
		onOne := f.parseWrites(t, 1)
		require.Len(t, onOne, 1)
		require.Equal(t, 1, acksFor(onOne, 1))

		// The call after a turn runs none, even when the selected route's local credit
		// blocks it. That isn't queue exhaustion, so a waiting home target still gets
		// its retry.
		f.peerSends(t, 1, 2)
		held := e.bbr.credit.reserve(e.bbr.creditLimit(), e.bbr.creditLimit(), false, false)
		require.NotNil(t, held)
		f.socket.writes = nil
		result = f.c.triggerSending(monotime.Now())
		require.NoError(t, result.err)
		require.Equal(t, emissionQueueFull, result.stop, "path zero's credit blocks the call")
		require.True(t, result.retry, "the waiting home target is retried")
		require.Empty(t, f.parseWrites(t, 1), "and waited out the skipped call")
		held.completeLocal()
		f.drain(t)
		f.c.peerParams.MaxDatagramFrameSize = wire.MaxDatagramSize
		f.c.pathCredit.routeOf(1).numNonAckElicitingAcks = 0
		// turn runs one ACK turn on path 1 with every kind of payload queued, then lets
		// the primary send it, and reports whether the turn carried the PING.
		turn := func(label string) bool {
			t.Helper()
			f.socket.writes = nil
			f.peerSends(t, 1, 2)
			f.c.retransmissionQueue.addAppData(&wire.MaxStreamsFrame{Type: protocol.StreamTypeUni, MaxStreamNum: 100})
			f.queueDatagram(t, 100)
			f.write(t, 100)
			tokens := path.tokens
			f.sendOnce(t)
			require.Equal(t, tokens, path.tokens, "%s debits no pacing", label)
			onOne := f.parseWrites(t, 1)
			require.Len(t, onOne, 1, label)
			require.Equal(t, 1, acksFor(onOne, 1))
			require.Empty(t, onOne[0].streams)
			require.Empty(t, onOne[0].datagrams)
			ping := len(onOne[0].frames) != 0
			if ping {
				require.Len(t, onOne[0].frames, 1, label)
				require.IsType(t, &wire.PingFrame{}, onOne[0].frames[0])
			}
			// The primary's data, retransmission and DATAGRAM leave between turns.
			f.drain(t)
			onZero := f.parseWrites(t, 0)
			require.Equal(t, 100, reliableBytes(onZero))
			require.NotEmpty(t, slices.DeleteFunc(slices.Clone(onZero), func(w reliableWrite) bool { return len(w.datagrams) == 0 }))
			require.False(t, f.c.retransmissionQueue.HasData(protocol.Encryption1RTT))
			f.ackPath(t, 0, 0, onZero)
			return ping
		}
		const interval = protocol.MaxNonAckElicitingAcks + 1
		for i := 1; i <= interval+5; i++ {
			require.Equal(t, i == interval, turn(fmt.Sprintf("turn %d", i)), "only the twentieth turn carries the PING")
		}

		// Five turns after that PING, an ack-eliciting packet on path 1 resets its count:
		// the unanswered PING's PTO sends bare PING probes there.
		f.socket.writes = nil
		f.expirePTO(t, 1)
		f.drain(t)
		probes := f.parseWrites(t, 1)
		require.NotEmpty(t, probes)
		for _, probe := range probes {
			require.Empty(t, probe.streams)
			require.Len(t, probe.frames, 1)
			require.IsType(t, &wire.PingFrame{}, probe.frames[0])
		}
		f.ackPath(t, 1, 1, probes)
		f.drain(t)
		for i := 1; i <= interval; i++ {
			require.Equal(t, i == interval, turn(fmt.Sprintf("turn %d after the probe", i)), "the next PING is twenty turns after the probe, not after the last PING")
		}

		// A PTO probe with nothing to carry is a PING, which resets its own carrier's
		// count on every multipath carrier, path zero included, and no other. Packed
		// last, since nothing registers it.
		p := e.packer
		route := f.c.pathCredit.routeOf(1)
		for _, carrier := range []*pathRoute{route, nil} {
			route.numNonAckElicitingAcks, p.numNonAckElicitingAcks = 5, 5
			packet, err := p.packPTOProbePacket(carrier, ackhandler.AckCarriage{}, protocol.Encryption1RTT, 1200, true, monotime.Now(), protocol.Version1)
			require.NoError(t, err)
			require.True(t, packet.shortHdrPacket.IsAckEliciting())
			packet.buffer.Release()
			require.Zero(t, *p.pingCount(carrier), "the probe resets its carrier's count")
			other := route
			if carrier != nil {
				other = nil
			}
			require.Equal(t, 5, *p.pingCount(other), "and leaves the other carrier's alone")
		}
	})
}
