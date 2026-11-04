//go:build darwin || linux || freebsd

package quic

import (
	"os"
	"slices"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"

	"github.com/stretchr/testify/require"
)

// policyPTO fires the aggregated timer until id's PTO expires, through the same two
// calls as the connection loop, and sends its probes.
func (f *reliableFixture) policyPTO(t *testing.T, id protocol.PathID) {
	t.Helper()
	for range 8 {
		time.Sleep(time.Until(f.c.sentPacketHandler.GetLossDetectionTimeout().ToTime()) + time.Millisecond)
		outcome, err := f.c.sentPacketHandler.OnLossDetectionTimeout(monotime.Now())
		require.NoError(t, err)
		f.c.pathCredit.notePTO(outcome, monotime.Now())
		if outcome.PTO && outcome.PathID == id {
			f.pass(t)
			return
		}
	}
	t.Fatalf("path %d's PTO did not fire", id)
}

// suspectAfterPTOs makes id suspect with three known-accepted attempts.
func (f *reliableFixture) suspectAfterPTOs(t *testing.T, id protocol.PathID) {
	t.Helper()
	f.write(t, 1000)
	f.pass(t)
	for range suspectPTOAttempts {
		f.policyPTO(t, id)
	}
	require.True(t, f.c.pathCredit.members[id].suspect)
}

// answerChallenge answers path id's newest PATH_CHALLENGE from its current tuple.
func (f *reliableFixture) answerChallenge(t *testing.T, id protocol.PathID) {
	t.Helper()
	challenges := policyChallenges(f.parseWrites(t, id))
	require.NotEmpty(t, challenges)
	addr := f.c.pathCredit.members[id].addr
	require.True(t, f.receive(t, id, addr, 1200, &wire.PathResponseFrame{Data: challenges[len(challenges)-1]}))
}

func policyChallenges(writes []reliableWrite) [][8]byte {
	var challenges [][8]byte
	for _, write := range writes {
		for _, frame := range write.frames {
			if challenge, ok := frame.(*wire.PathChallengeFrame); ok {
				challenges = append(challenges, challenge.Data)
			}
		}
	}
	return challenges
}

func (f *reliableFixture) observed() *utils.RTTStats { return f.c.observedRTT() }

func destinationError(cause syscall.Errno) func([]byte) (int, error) {
	return func([]byte) (int, error) { return 0, &os.SyscallError{Syscall: "sendmsg", Err: cause} }
}

func TestPathPolicyThreeAcceptedPTOsPromoteBackup(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		p := f.c.pathCredit
		require.True(t, p.members[1].backup, "a newly validated path starts as a backup")
		require.Zero(t, p.primary, "initial path zero is the sticky primary")

		f.write(t, 1000)
		f.pass(t)
		f.policyPTO(t, 0)
		f.policyPTO(t, 0)
		require.False(t, p.members[0].suspect, "two attempts are not enough")
		require.Equal(t, 2, p.members[0].attempts)
		f.socket.writes = nil
		f.policyPTO(t, 0)

		require.True(t, p.members[0].suspect)
		require.Equal(t, protocol.PathID(1), p.primary)
		require.True(t, p.members[1].backup, "promotion doesn't rewrite local use")
		require.Same(t, p.rtt[1], f.observed())
		require.Nil(t, p.terminals[0], "suspicion spends no wire ID")
		f.pass(t)
		onZero := f.parseWrites(t, 0)
		for _, write := range onZero {
			for _, frame := range write.frames {
				_, abandon := frame.(*wire.PathAbandonFrame)
				require.False(t, abandon, "and sends no PATH_ABANDON")
			}
		}
		require.NotEmpty(t, policyChallenges(onZero), "a suspect path keeps bounded probes")

		// The work stranded on the suspect path returns to the logical owners, so the
		// promoted path carries it without waiting out the backed-off PTO.
		require.Equal(t, 1000, reliableBytes(f.parseWrites(t, 1)))

		// New ordinary work never leaves on the suspect path, though its last PTO's
		// probe credit is still pending.
		f.socket.writes = nil
		f.c.peerParams.MaxDatagramFrameSize = wire.MaxDatagramSize
		f.queueDatagram(t, 100)
		f.write(t, 700)
		f.pass(t)
		onZero = f.parseWrites(t, 0)
		require.Zero(t, reliableBytes(onZero))
		for _, write := range onZero {
			require.Empty(t, write.datagrams)
		}
		onOne := f.parseWrites(t, 1)
		require.Equal(t, 700, reliableBytes(onOne))
		carried := 0
		for _, write := range onOne {
			carried += len(write.datagrams)
		}
		require.Equal(t, 1, carried)

		// The observation follows the survivor, and retention still counts a slower zero.
		p.rtt[1].UpdateRTT(time.Millisecond, 0)
		f.c.rttStats.UpdateRTT(2*time.Second, 0)
		require.Same(t, p.rtt[1], f.observed())
		require.Equal(t, f.c.rttStats.PTO(true), p.largestPTO(true))
	})
}

func TestPathPolicySoleAdmittedPathReachesSuspicion(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newAdmissionFixture(t, protocol.PerspectiveServer)
		f.supplyPeerCIDs(t)
		r := &reliableFixture{admissionFixture: f, stream: reliableStream(t, f.c)}
		r.pass(t)
		r.ackPath(t, 0, 0, r.parseWrites(t, 0))
		r.pass(t)
		f.socket.writes = nil
		p := f.c.pathCredit
		require.Nil(t, p.members[1], "path zero is the only member")

		r.suspectAfterPTOs(t, 0)
		require.Equal(t, noPrimary, p.primary)
		r.answerChallenge(t, 0)
		require.False(t, p.members[0].suspect)
		require.Zero(t, p.primary)

		// The work held while it was suspect leaves without the probes it gave up.
		f.socket.writes = nil
		r.pass(t)
		require.Equal(t, 1000, reliableBytes(r.parseWrites(t, 0)))
	})
}

func TestPathPolicyLocalResultsDoNotCountTowardSuspicion(t *testing.T) {
	for name, nativeError := range map[string]error{"timeout": os.ErrDeadlineExceeded, "capacity": syscall.ENOBUFS} {
		t.Run(name, func(t *testing.T) {
			nativeOrdinaryTest(t, func(t *testing.T) {
				f := newReliableFixture(t, protocol.PerspectiveServer)
				p := f.c.pathCredit
				source := f.observed()

				// One accepted flight qualifies one attempt, though packets from it stay
				// outstanding for later PTOs. Everything sent after it stays local.
				f.write(t, 8000)
				f.pass(t)
				f.socket.result = func([]byte) (int, error) { return 0, nativeError }
				for range suspectPTOAttempts + 2 {
					f.policyPTO(t, 0)
				}
				require.Equal(t, 1, p.members[0].attempts)
				require.False(t, p.members[0].suspect)
				require.Zero(t, p.primary)
				require.Same(t, source, f.observed())
				require.NoError(t, f.c.emission.consumeLocal())
			})
		})
	}
}

func TestPathPolicyCrossPathAckEstablishesLiveness(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		p := f.c.pathCredit
		f.c.pathCredit.selectPrimaryPrivate(1)
		f.write(t, 1000)
		f.pass(t)
		f.policyPTO(t, 1)
		f.policyPTO(t, 1)
		require.Equal(t, 2, p.members[1].attempts)
		sent := f.parseWrites(t, 1) // the newest is the last probe, still outstanding

		// Unrelated traffic on path zero says nothing about path 1.
		before := p.members[1].livenessAt
		require.True(t, f.receive(t, 0, f.c.RemoteAddr(), 0, &wire.PingFrame{}))
		require.Equal(t, 2, p.members[1].attempts)
		require.Equal(t, before, p.members[1].livenessAt)

		time.Sleep(time.Millisecond)
		f.ackPath(t, 1, 0, sent[len(sent)-1:])
		require.Zero(t, p.members[1].attempts, "the ACK came home on another path")
		require.Greater(t, p.members[1].livenessAt, before)
	})
}

func TestPathPolicyOldTupleEvidenceDoesNotCount(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		p := f.c.pathCredit
		f.c.pathCredit.selectPrimaryPrivate(1)
		f.write(t, 1000)
		f.pass(t)
		sent := f.parseWrites(t, 1)
		f.policyPTO(t, 1)
		require.Equal(t, 1, p.members[1].attempts)

		p.cutTuple(p.members[1])
		require.Zero(t, p.members[1].attempts, "cutover resets the attempts")
		before := p.members[1].livenessAt
		time.Sleep(time.Millisecond)
		f.ackPath(t, 1, 1, sent[:1])
		require.Equal(t, before, p.members[1].livenessAt, "an old-tuple ACK restores nothing")
	})
}

func TestPathPolicyStaleBackupRevalidatesBeforePromotion(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		p := f.c.pathCredit
		time.Sleep(minBackupFreshness + time.Second)
		f.pass(t)
		source := f.observed()
		f.socket.writes = nil

		f.suspectAfterPTOs(t, 0)
		require.Equal(t, noPrimary, p.primary, "a stale backup isn't promoted")
		require.Same(t, source, f.observed(), "with no eligible primary the last observation stays")
		require.Zero(t, reliableBytes(f.parseWrites(t, 1)), "and carries no application data")

		f.queueDatagram(t, 100)
		f.queueDatagram(t, 100)
		f.pass(t)
		require.Nil(t, f.c.datagramQueue.Peek(), "the hold is no eligible carrier")
		require.EqualValues(t, 2, f.c.datagramQueue.sendDrops.Load())
		require.Empty(t, reliableDatagrams(f.parseWrites(t, 0)))
		require.Empty(t, reliableDatagrams(f.parseWrites(t, 1)))

		f.answerChallenge(t, 1)
		require.Equal(t, protocol.PathID(1), p.primary)
		require.Same(t, p.rtt[1], f.observed())
		f.socket.writes = nil
		f.pass(t)
		require.Equal(t, 1000, reliableBytes(f.parseWrites(t, 1)))
	})

	// A PTO pending on the stale backup is still its probe, and carries no DATAGRAM.
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		p := f.c.pathCredit
		p.selectPrimaryPrivate(1)
		f.write(t, 1000)
		f.pass(t)
		p.selectPrimaryPrivate(0)
		time.Sleep(minBackupFreshness + time.Second)
		f.expirePTO(t, 1)
		require.Equal(t, ackhandler.SendPTOAppData, f.recovery(1).SendMode(monotime.Now()))
		p.markSuspect(p.members[0], monotime.Now())
		require.Equal(t, noPrimary, p.primary)

		f.socket.writes = nil
		f.queueDatagram(t, 100)
		f.pass(t)
		require.Nil(t, f.c.datagramQueue.Peek())
		require.EqualValues(t, 1, f.c.datagramQueue.sendDrops.Load())
		require.NotEmpty(t, f.parseWrites(t, 1), "the probe still leaves")
		require.Empty(t, reliableDatagrams(f.parseWrites(t, 1)))
	})
}

func TestPathPolicyAllBackupKeepsTraffic(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		p := f.c.pathCredit
		source := f.observed()
		require.NoError(t, p.setLocalUsePrivate(0, true, monotime.Now()))
		require.Zero(t, p.primary, "all backup keeps the healthy current primary")
		require.Same(t, source, f.observed())

		f.write(t, 1000)
		f.pass(t)
		require.Equal(t, 1000, reliableBytes(f.parseWrites(t, 0)), "backup is no prohibition on useful traffic")

		f.socket.writes = nil
		require.NoError(t, p.setLocalUsePrivate(1, false, monotime.Now()))
		require.Equal(t, protocol.PathID(1), p.primary, "an explicit active path is preferred")
		require.Same(t, source, f.observed(), "path zero stays the observation while it is eligible")
	})
}

func TestPathPolicyRecoveryDemotesWithoutOscillation(t *testing.T) {
	for _, order := range []struct {
		name  string
		acked func(reliableWrite) bool // what an ACK covers before the response; nil sends none
	}{
		{name: "response first"},
		{name: "ack of drained data first", acked: func(write reliableWrite) bool { return len(write.streams) != 0 }},
		{name: "ack of the challenge first", acked: func(write reliableWrite) bool { return len(policyChallenges([]reliableWrite{write})) != 0 }},
	} {
		t.Run(order.name, func(t *testing.T) {
			nativeOrdinaryTest(t, func(t *testing.T) {
				f := newReliableFixture(t, protocol.PerspectiveServer)
				p := f.c.pathCredit
				observation := f.c.rttObservation
				f.socket.writes = nil
				f.suspectAfterPTOs(t, 0)
				require.Equal(t, protocol.PathID(1), p.primary)
				require.Same(t, p.rtt[1], f.observed())

				if order.acked != nil {
					f.ackPath(t, 0, 0, slices.DeleteFunc(f.parseWrites(t, 0), func(write reliableWrite) bool { return !order.acked(write) }))
					require.True(t, p.members[0].suspect, "neither ACK is liveness")
				}
				f.answerChallenge(t, 0)
				require.False(t, p.members[0].suspect)
				require.True(t, p.members[0].backup, "a recovered path comes back as a backup, though it was active")
				require.Equal(t, protocol.PathID(1), p.primary, "and leaves the promoted primary alone")
				require.Same(t, f.c.rttStats, f.observed(), "a live, eligible path zero is the legacy observation")
				require.Same(t, observation, f.c.rttObservation)

				// The probes it couldn't send while suspect don't put it ahead of the primary.
				f.socket.writes = nil
				f.write(t, 1000)
				f.pass(t)
				require.Zero(t, reliableBytes(f.parseWrites(t, 0)))
				require.Equal(t, 1000, reliableBytes(f.parseWrites(t, 1)))

				// An explicit choice overrides the demotion.
				require.NoError(t, p.setLocalUsePrivate(0, false, monotime.Now()))
				require.Zero(t, p.primary)
				require.False(t, p.members[0].backup)
			})
		})
	}
}

func TestPathPolicyLivenessKeepsHealthyPathPTO(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		p := f.c.pathCredit
		f.write(t, 1000)
		f.pass(t)
		f.socket.writes = nil
		f.expirePTO(t, 0)
		require.Equal(t, ackhandler.SendPTOAppData, f.recovery(0).SendMode(monotime.Now()))

		// Only suspicion strands probes, so a path that never left the scheduler still owes its own.
		p.noteLiveness(p.members[0], monotime.Now())
		require.Equal(t, ackhandler.SendPTOAppData, f.recovery(0).SendMode(monotime.Now()))
		f.pass(t)
		require.Equal(t, 1000, reliableBytes(f.parseWrites(t, 0)))
	})
}

func TestPathPolicyDestinationErrorMovesToSurvivor(t *testing.T) {
	for _, cause := range []syscall.Errno{syscall.ECONNREFUSED, syscall.EHOSTUNREACH, syscall.ENETUNREACH, syscall.EACCES, syscall.EPERM} {
		t.Run(cause.Error(), func(t *testing.T) {
			nativeOrdinaryTest(t, func(t *testing.T) {
				f := newReliableFixture(t, protocol.PerspectiveServer)
				p := f.c.pathCredit
				f.socket.result = destinationError(cause)
				f.write(t, 1000)
				f.pass(t)
				f.socket.result = nil

				require.True(t, p.members[0].suspect)
				require.False(t, p.members[1].suspect, "only the matching tuple")
				require.Equal(t, protocol.PathID(1), p.primary)
				require.Nil(t, p.terminals[0])
				f.socket.writes = nil
				f.pass(t)
				require.Equal(t, 1000, reliableBytes(f.parseWrites(t, 1)))
			})
		})
	}
}

func TestPathPolicyDestinationErrorWithoutSurvivorRetainsValidation(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		p := f.c.pathCredit
		f.socket.result = destinationError(syscall.EHOSTUNREACH)
		f.write(t, 1000)
		f.pass(t)
		f.c.pathCredit.selectPrimaryPrivate(1)
		f.pass(t)
		require.True(t, p.members[0].suspect)
		require.True(t, p.members[1].suspect)
		require.Equal(t, noPrimary, p.primary)
		source := f.observed()
		f.socket.result = nil

		// Nothing ordinary leaves, and the peer's packets are still acknowledged.
		f.socket.writes = nil
		f.write(t, 500)
		require.True(t, f.receive(t, 0, f.c.RemoteAddr(), 0, &wire.PingFrame{}))
		f.pass(t)
		require.Equal(t, blockModeCongestionLimited, f.c.blocked, "the hold keeps the ACK and loss alarms")
		time.Sleep(time.Until(f.c.receivedPacketHandler.GetAlarmTimeout().ToTime()) + time.Millisecond)
		f.pass(t)
		require.Zero(t, reliableBytes(f.parseWrites(t, 0))+reliableBytes(f.parseWrites(t, 1)))
		acked := false
		for _, write := range f.parseWrites(t, 0) {
			acked = acked || len(write.acks) != 0
		}
		require.True(t, acked)
		require.NoError(t, f.c.emission.consumeLocal(), "this isn't a connection error")
		require.Same(t, source, f.observed())

		f.answerChallenge(t, 1)
		require.Equal(t, protocol.PathID(1), p.primary)
		f.socket.writes = nil
		f.pass(t)
		require.Equal(t, 1500, reliableBytes(f.parseWrites(t, 1)))
	})
}

func TestPathPolicyStaleDestinationResultDoesNotDemote(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		p := f.c.pathCredit
		f.c.pathCredit.selectPrimaryPrivate(1)
		f.socket.hold = func([]byte) bool { return true }
		f.socket.result = destinationError(syscall.ECONNREFUSED)
		f.write(t, 1000)
		f.pass(t)

		// The tuple moves on while the old one's native result is outstanding.
		p.cutTuple(p.members[1])
		f.socket.hold = nil
		close(f.socket.gate)
		synctest.Wait()
		f.socket.result = nil
		require.NoError(t, f.c.emission.consumeLocal())
		require.False(t, p.members[1].suspect)
		require.Equal(t, protocol.PathID(1), p.primary)
	})
}

func TestPathPolicyEndpointFatalStaysWithEndpoint(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		f.socket.result = destinationError(syscall.EBADF)
		f.write(t, 1000)
		synctest.Wait()
		require.NoError(t, f.c.pathCredit.expire(monotime.Now()))
		f.c.triggerSending(monotime.Now())
		synctest.Wait()
		require.Error(t, f.c.emission.consumeLocal(), "a server's paths share its one endpoint, so none survives")
		require.False(t, f.c.pathCredit.members[0].suspect, "and is not a destination failure")
	})
}

func TestPathPolicyPeerStatusIsAdvisory(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		p := f.c.pathCredit
		addr := f.c.RemoteAddr()
		require.True(t, f.receive(t, 0, addr, 0, &wire.PathStatusFrame{PathID: 0, SequenceNumber: 2, Backup: true}))
		require.True(t, p.members[0].peerBackup)
		require.False(t, p.members[0].backup, "the peer can't change local use")
		require.Zero(t, p.primary, "nor move a healthy primary")

		require.True(t, f.receive(t, 0, addr, 0, &wire.PathStatusFrame{PathID: 0, SequenceNumber: 1, Backup: false}))
		require.True(t, p.members[0].peerBackup, "a stale sequence number is ignored")

		// A backup-preferring peer can't hold back the only surviving path.
		require.True(t, f.receive(t, 0, addr, 0, &wire.PathStatusFrame{PathID: 1, SequenceNumber: 3, Backup: true}))
		f.suspectAfterPTOs(t, 0)
		require.Equal(t, protocol.PathID(1), p.primary)
	})
}
