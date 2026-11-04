//go:build darwin || linux || freebsd

package quic

import (
	"context"
	"net"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type migrationRawConn struct {
	rawConn
	addr net.Addr
}

func (c *migrationRawConn) LocalAddr() net.Addr            { return c.addr }
func (c *migrationRawConn) capabilities() connCapabilities { return connCapabilities{} }

// migrationTarget is a granted, already initialized transport on its own socket.
func migrationTarget(t *testing.T, f *admissionFixture) (*Transport, *nativeProbeSocket) {
	t.Helper()
	socket := newNativeProbeSocket()
	target := &Transport{
		ConnectionIDLength: f.tc.srcConnID.Len(), connIDLen: f.tc.srcConnID.Len(),
		handlers: make(map[protocol.ConnectionID]packetHandlerEntry), resetTokens: make(map[protocol.StatelessResetToken]packetHandler),
		logger: utils.DefaultLogger,
		conn:   &migrationRawConn{addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 7000}},
	}
	target.initOnce.Do(func() {})
	target.responses = newEndpointResponses(startNativeProbeEndpoint(t, socket))
	withFirstPathFinder(f)
	return target, socket
}

// withFirstPathFinder installs what the handshake gives a real connection.
func withFirstPathFinder(f *admissionFixture) {
	if f.c.mtuDiscoverer == nil {
		f.c.mtuDiscoverer = newMTUDiscoverer(f.c.rttStats, protocol.MinInitialPacketSize, protocol.MaxPacketBufferSize, nil)
	}
}

func migrateAsync(ctx context.Context, f *admissionFixture, id protocol.PathID, target *Transport) <-chan error {
	errs := make(chan error, 1)
	go func() { errs <- f.c.migratePathPrivate(ctx, id, target) }()
	synctest.Wait()
	return errs
}

// spareZeroCID supplies path zero's peer CID at seq, so a migration can use it.
func spareZeroCID(t *testing.T, f *admissionFixture, seq uint64) {
	t.Helper()
	cid := make([]byte, f.tc.destConnID.Len())
	cid[0], cid[1] = 0xc0, byte(seq)
	require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathNewConnectionIDFrame{PathID: 0, SequenceNumber: seq, ConnectionID: protocol.ParseConnectionID(cid), StatelessResetToken: protocol.StatelessResetToken{0xc0, byte(seq)}}))
}

// socketChallenges reads the challenges a target's socket carried for path id.
func socketChallenges(t *testing.T, f *admissionFixture, socket *nativeProbeSocket, id protocol.PathID) [][8]byte {
	t.Helper()
	original := f.socket
	f.socket = socket
	defer func() { f.socket = original }()
	return admissionChallenges(f.peerWrites(t, id))
}

// migrate runs one whole cutover to target and requires its success.
func migrate(t *testing.T, f *admissionFixture, id protocol.PathID, target *Transport, socket *nativeProbeSocket) {
	t.Helper()
	errs := migrateAsync(context.Background(), f, id, target)
	f.pass(t)
	challenges := socketChallenges(t, f, socket, id)
	require.NotEmpty(t, challenges, "the challenge leaves through the target")
	require.True(t, f.receive(t, id, f.c.pathCredit.members[id].addr, 1200, &wire.PathResponseFrame{Data: challenges[len(challenges)-1]}))
	f.pass(t)
	require.NoError(t, <-errs)
	require.Equal(t, target, f.c.pathCredit.members[id].transport)
}

func TestPathMigrationTargetRefusal(t *testing.T) {
	t.Run("no path moves before confirmation", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			target, socket := migrationTarget(t, f)
			f.c.handshakeConfirmed = false
			errs := migrateAsync(context.Background(), f, 0, target)
			require.NoError(t, f.c.pathCredit.service(monotime.Now()))
			require.Empty(t, errs)
			require.Empty(t, (*packetHandlerMap)(target).handlers)
			require.Zero(t, f.c.pathCredit.historyUnits)
			f.c.handshakeConfirmed = true
			f.pass(t)
			challenges := socketChallenges(t, f, socket, 0)
			require.NotEmpty(t, challenges, "normal progress afterward")
			require.True(t, f.receive(t, 0, f.c.pathCredit.members[0].addr, 1200, &wire.PathResponseFrame{Data: challenges[0]}))
			f.pass(t)
			require.NoError(t, <-errs)
		})
	})
	t.Run("a nil target", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			require.ErrorIs(t, f.c.migratePathPrivate(context.Background(), 0, nil), ErrMultipathInvalidConfigV1)
		})
	})
	t.Run("a mismatched CID length, uninitialized, for migration and open", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			target, _ := migrationTarget(t, f)
			target.ConnectionIDLength = f.tc.srcConnID.Len() + 2
			require.ErrorIs(t, f.c.migratePathPrivate(context.Background(), 0, target), ErrMultipathInvalidConfigV1)
			require.ErrorIs(t, f.c.openPathPrivate(context.Background(), target, admissionTupleA), ErrMultipathInvalidConfigV1)
			require.Empty(t, (*packetHandlerMap)(target).handlers)
			require.False(t, f.c.pathCredit.paths[1], "no ID is consumed")
			require.Nil(t, f.c.pathCredit.members[0].transport)
		})
	})
	t.Run("a route collision rolls back and leaves the connection open", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			target, _ := migrationTarget(t, f)
			other := &Conn{}
			require.True(t, (*packetHandlerMap)(target).Add(f.issuedCID(1), other))
			errs := migrateAsync(context.Background(), f, 0, target)
			f.pass(t)
			require.ErrorIs(t, <-errs, ErrMultipathInvalidConfigV1)
			require.Len(t, (*packetHandlerMap)(target).handlers, 1, "only the other connection's route remains")
			require.Zero(t, f.c.pathCredit.historyUnits)
			require.Nil(t, f.c.pathCredit.conn.pathOpen.Load())
		})
	})
	t.Run("a closed target", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			target, _ := migrationTarget(t, f)
			target.transportClosed = true
			errs := migrateAsync(context.Background(), f, 0, target)
			f.pass(t)
			require.ErrorIs(t, <-errs, net.ErrClosed)
			require.Empty(t, (*packetHandlerMap)(target).handlers)
			require.Zero(t, f.c.pathCredit.historyUnits)
			require.NotContains(t, f.c.pathCredit.attachments, target)
		})
	})
	t.Run("the peer's migration refusal covers its handshake address only", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			f.c.peerParams.DisableActiveMigration = true
			target, socket := migrationTarget(t, f)
			errs := migrateAsync(context.Background(), f, 0, target)
			f.pass(t)
			require.ErrorIs(t, <-errs, ErrMultipathUnsupportedV1)
			require.Empty(t, (*packetHandlerMap)(target).handlers)
			require.Empty(t, socket.writes)

			openErrs := f.openAsync(context.Background(), nil, f.c.RemoteAddr())
			f.pass(t)
			require.ErrorIs(t, <-openErrs, ErrMultipathUnsupportedV1)
			require.False(t, f.c.pathCredit.paths[1], "no ID is consumed")

			f.c.pathCredit.recordValidated(admissionTupleA)
			openErrs = f.openAsync(context.Background(), nil, admissionTupleA)
			f.pass(t)
			require.NoError(t, <-openErrs, "another admitted address stays allowed")
			require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathNewConnectionIDFrame{PathID: 1, SequenceNumber: 1, ConnectionID: replacementPeerCID(1), StatelessResetToken: protocol.StatelessResetToken{0xab}}))
			migrate(t, f, 1, target, socket)
		})
	})
}

// TestPathMigrationNegotiatedZero moves the only path, twice, with no wire credit.
func TestPathMigrationNegotiatedZero(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newAdmissionFixture(t, protocol.PerspectiveClient)
		f.supplyPeerCIDs(t)
		p := f.c.pathCredit
		p.localMax, p.peerMax = 0, 0
		target, socket := migrationTarget(t, f)
		before := f.c.sentPacketHandler.PeekPacketNumber
		pn, _ := before(protocol.Encryption1RTT)
		tuple := p.members[0].tuple
		migrate(t, f, 0, target, socket)
		require.Equal(t, tuple+1, p.members[0].tuple)
		require.Equal(t, p.members[0].tuple, f.c.pathGeneration, "one generation for path zero")
		after, _ := before(protocol.Encryption1RTT)
		require.Greater(t, after, pn, "the PN space continues")
		require.Equal(t, protocol.PathID(0), p.localMax, "no credit is spent")
		require.Equal(t, 1, p.historyUnits, "the old generation is charged")
		require.Equal(t, target.conn.LocalAddr(), f.c.LocalAddr())

		spareZeroCID(t, f, 2)
		next, nextSocket := migrationTarget(t, f)
		migrate(t, f, 0, next, nextSocket)
		require.Equal(t, 2, p.historyUnits)
	})
}

// TestPathMigrationExhaustedCredit moves an idle backup while the primary stays busy.
// The server only moves on a non-probing packet from the new tuple, so one must leave.
func TestPathMigrationExhaustedCredit(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveClient)
		p := f.c.pathCredit
		require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathNewConnectionIDFrame{PathID: 1, SequenceNumber: 1, ConnectionID: replacementPeerCID(1), StatelessResetToken: protocol.StatelessResetToken{0xab}}))
		grant := p.localMax
		target, socket := migrationTarget(t, f.admissionFixture)
		// The first announcement is refused locally, so it must be retried.
		refuse := true
		socket.result = func(data []byte) (int, error) {
			if refuse && p.members[1].transport == target {
				refuse = false
				return 0, syscall.ENOBUFS
			}
			return len(data), nil
		}
		migrate(t, f.admissionFixture, 1, target, socket)
		require.Equal(t, grant, p.localMax, "same wire ID, no credit")
		f.write(t, 1000)
		f.pass(t)
		require.Zero(t, p.primary, "ordinary traffic stays on the primary")
		announced := func() int {
			original := f.socket
			f.socket = socket
			defer func() { f.socket = original }()
			count := 0
			for _, write := range f.parseWrites(t, 1) {
				for _, frame := range write.frames {
					switch frame.(type) {
					case *wire.PathChallengeFrame, *wire.PathResponseFrame:
					default:
						count++ // a non-probing frame
					}
				}
			}
			return count
		}
		first := announced()
		require.NotZero(t, first, "a non-probing packet leaves on the new tuple")
		time.Sleep(p.members[1].announceAt.Sub(monotime.Now()) + time.Millisecond)
		f.pass(t)
		require.Greater(t, announced(), first, "a refusal doesn't discharge the obligation")

		m := p.members[1]
		m.announces = maxPathAnnounceBackoff + 4
		p.noteAnnounced(m, monotime.Now())
		require.True(t, m.announcing, "a long run of refusals keeps it too")
		require.LessOrEqual(t, m.announces, maxPathAnnounceBackoff)

		// While the path can't announce, its elapsed deadline arms no timer.
		time.Sleep(m.announceAt.Sub(monotime.Now()) + time.Millisecond)
		p.markSuspect(m, monotime.Now())
		require.NoError(t, p.service(monotime.Now()))
		if next := p.nextAdmissionExpiry(); next != 0 {
			require.True(t, next.After(monotime.Now()), "no immediate wake")
		}
		m.suspect = false

		// A saturated backup endpoint doesn't hold the healthy primary's work.
		socket.hold = nativeProbeHoldAll
		p.selectPrimaryPrivate(1)
		f.write(t, 6000)
		f.pass(t)
		p.selectPrimaryPrivate(0)
		m.announceAt = monotime.Now()
		f.socket.writes = nil
		f.write(t, 1000)
		f.pass(t)
		require.NotZero(t, reliableBytes(f.parseWrites(t, 0)), "the primary keeps sending")
		require.True(t, m.announceAt.After(monotime.Now()), "and the announcement backs off")
		socket.hold = nil
		socket.release()
		synctest.Wait()
		f.pass(t)

		original := f.socket
		f.socket = socket
		writes := f.parseWrites(t, 1)
		f.socket = original
		f.ackPath(t, 1, 0, writes)
		require.False(t, p.members[1].announcing, "only an acknowledgment discharges it")
	})
}

// TestPathMigrationAnswersOnNewTuple answers a peer's challenge on the socket it
// reached, with the same remote address on both, and stops answering on the old one.
func TestPathMigrationAnswersOnNewTuple(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newAdmissionFixture(t, protocol.PerspectiveClient)
		f.supplyPeerCIDs(t)
		p := f.c.pathCredit
		target, socket := migrationTarget(t, f)
		errs := migrateAsync(context.Background(), f, 0, target)
		f.pass(t)
		remote := p.members[0].addr
		packet := f.peerPacket(t, 0, remote, 1200, false, &wire.PathChallengeFrame{Data: [8]byte{7}})
		packet.transport = target
		require.Same(t, target, packet.Clone().transport, "a coalesced component keeps it")
		_, err := f.c.handleShortHeaderPacket(packet, false, 0)
		require.NoError(t, err)
		require.True(t, p.members[0].live)
		original := f.socket.writes
		f.pass(t)
		responses := func(s *nativeProbeSocket) int {
			saved := f.socket
			f.socket = s
			defer func() { f.socket = saved }()
			count := 0
			for _, write := range f.peerWrites(t, 0) {
				for _, frame := range write.frames {
					if response, ok := frame.(*wire.PathResponseFrame); ok && response.Data == [8]byte{7} {
						count++
					}
				}
			}
			return count
		}
		require.Equal(t, 1, responses(socket), "the new socket answers")
		require.Zero(t, responses(f.socket))
		require.Equal(t, len(original), len(f.socket.writes))
		challenges := socketChallenges(t, f, socket, 0)
		require.True(t, f.receive(t, 0, remote, 1200, &wire.PathResponseFrame{Data: challenges[0]}))
		f.pass(t)
		require.NoError(t, <-errs)

		// The old socket's traffic can't restore the old tuple, or draw a reply.
		tuple := p.members[0].tuple
		packet = f.peerPacket(t, 0, remote, 100, false, &wire.PathChallengeFrame{Data: [8]byte{8}})
		packet.transport = p.origin
		_, err = f.c.handleShortHeaderPacket(packet, false, 0)
		require.NoError(t, err)
		f.pass(t)
		require.Equal(t, tuple, p.members[0].tuple)
		require.Equal(t, target, p.members[0].transport)
		require.Nil(t, p.members[0].response)
	})
}

// TestPathMigrationLateOldGenerationAck delivers the old flight's ACK after cutover.
// The retention deadline holds the snapshot from before the RTT reset.
func TestPathMigrationLateOldGenerationAck(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveClient)
		p := f.c.pathCredit
		p.selectPrimaryPrivate(1)
		f.c.pathCredit.rtt[1].UpdateRTT(200*time.Millisecond, 0)
		require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathNewConnectionIDFrame{PathID: 1, SequenceNumber: 1, ConnectionID: replacementPeerCID(1), StatelessResetToken: protocol.StatelessResetToken{0xab}}))
		f.write(t, 1000)
		f.pass(t)
		old := f.parseWrites(t, 1)
		require.NotEmpty(t, old)
		pto := p.largestPTO(true)
		target, socket := migrationTarget(t, f.admissionFixture)
		cutover := monotime.Now()
		migrate(t, f.admissionFixture, 1, target, socket)
		require.Len(t, p.retained, 1)
		require.Equal(t, cutover.Add(threePathPTO(pto)), p.retained[0].expiry, "the pre-reset largest PTO")
		tuple := p.members[1].tuple
		liveness := p.members[1].livenessAt
		time.Sleep(time.Millisecond)
		f.ackPath(t, 1, 0, old)
		require.Equal(t, tuple, p.members[1].tuple)
		require.Equal(t, liveness, p.members[1].livenessAt, "old-tuple feedback proves nothing about this tuple")
		require.Equal(t, cutover.Add(threePathPTO(pto)), p.retained[0].expiry, "traffic never refreshes retention")
	})
}

// TestPathMigrationFenceBeforeCommit holds the old tuple's submission. Nothing commits
// or packs against the revoked epoch, a survivor still carries ACKs, and a queued
// DATAGRAM that fits the fencing path stays queued.
func TestPathMigrationFenceBeforeCommit(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f, survivorSocket := newTargetPathFixture(t)
		withFirstPathFinder(f.admissionFixture)
		p := f.c.pathCredit
		require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathNewConnectionIDFrame{PathID: 0, SequenceNumber: 2, ConnectionID: replacementPeerCID(9), StatelessResetToken: protocol.StatelessResetToken{0xac}}))
		target, socket := migrationTarget(t, f.admissionFixture)
		errs := migrateAsync(context.Background(), f.admissionFixture, 0, target)
		f.pass(t)
		challenges := socketChallenges(t, f.admissionFixture, socket, 0)
		require.NotEmpty(t, challenges)
		f.socket.hold = nativeProbeHoldAll
		f.socket.writes = nil
		f.write(t, 1000)
		f.pass(t)
		require.NotEmpty(t, f.socket.writes, "a submission is claimed on the old tuple")
		require.True(t, f.receive(t, 0, p.members[0].addr, 1200, &wire.PathResponseFrame{Data: challenges[0]}))
		f.pass(t)
		require.Empty(t, errs, "the claim holds the commit")
		require.Nil(t, p.members[0].transport)
		require.Zero(t, p.primary, "a fence doesn't move the primary")
		require.True(t, p.members[0].candidate.fencing)

		claimed := len(f.socket.writes)
		require.NoError(t, f.c.datagramQueue.Add(&wire.DatagramFrame{Data: make([]byte, 100)}))
		survivorSocket.writes = nil
		require.True(t, f.receive(t, 1, admissionTupleA, 0, &wire.PingFrame{}))
		time.Sleep(50 * time.Millisecond)
		f.pass(t)
		require.Len(t, f.socket.writes, claimed, "nothing packs on the revoked epoch")
		require.NotEmpty(t, survivorSocket.writes, "the survivor carries its ACK")
		require.NotNil(t, f.c.datagramQueue.Peek(), "a fence drops nothing")

		f.socket.hold = nil
		f.socket.release()
		synctest.Wait()
		f.pass(t)
		require.NoError(t, <-errs)
		require.Equal(t, target, p.members[0].transport)
	})
}

func TestPathMigrationCancelBeforeCommit(t *testing.T) {
	t.Run("the peer retires the chosen CID during the fence", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newReliableFixture(t, protocol.PerspectiveClient)
			withFirstPathFinder(f.admissionFixture)
			p := f.c.pathCredit
			target, socket := migrationTarget(t, f.admissionFixture)
			errs := migrateAsync(context.Background(), f.admissionFixture, 0, target)
			f.pass(t)
			challenges := socketChallenges(t, f.admissionFixture, socket, 0)
			f.socket.hold = nativeProbeHoldAll
			f.write(t, 1000)
			f.pass(t)
			tuple, revoked := p.members[0].tuple, p.members[0].established
			require.True(t, f.receive(t, 0, p.members[0].addr, 1200, &wire.PathResponseFrame{Data: challenges[0]}))
			f.pass(t)
			cid := make([]byte, f.tc.destConnID.Len())
			cid[0] = 0xe0
			require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathNewConnectionIDFrame{PathID: 0, SequenceNumber: 9, RetirePriorTo: 9, ConnectionID: protocol.ParseConnectionID(cid), StatelessResetToken: protocol.StatelessResetToken{0xe0}}))
			f.socket.hold = nil
			f.socket.release()
			synctest.Wait()
			f.pass(t)
			require.ErrorIs(t, <-errs, errPathCandidateCIDRetired)
			require.Equal(t, tuple, p.members[0].tuple)
			require.Nil(t, p.members[0].transport)
			require.NotSame(t, revoked, p.members[0].established, "a fresh old-tuple epoch")
			require.False(t, p.members[0].established.probeEpoch.revoked.Load())
		})
	})
	t.Run("during validation", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			p := f.c.pathCredit
			target, _ := migrationTarget(t, f)
			ctx, cancel := context.WithCancel(context.Background())
			errs := migrateAsync(ctx, f, 0, target)
			f.pass(t)
			cancel()
			require.ErrorIs(t, <-errs, context.Canceled)
			tuple := p.members[0].tuple
			f.pass(t)
			f.pass(t)
			require.Nil(t, p.members[0].transport, "the old tuple stays selected")
			require.Equal(t, tuple, p.members[0].tuple)
			require.Nil(t, p.members[0].candidate)
			require.Zero(t, p.historyUnits)
			require.Nil(t, f.c.pathOpen.Load())
		})
	})
	t.Run("during the fence", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newReliableFixture(t, protocol.PerspectiveClient)
			p := f.c.pathCredit
			require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathNewConnectionIDFrame{PathID: 0, SequenceNumber: 2, ConnectionID: replacementPeerCID(9), StatelessResetToken: protocol.StatelessResetToken{0xac}}))
			target, socket := migrationTarget(t, f.admissionFixture)
			ctx, cancel := context.WithCancel(context.Background())
			errs := migrateAsync(ctx, f.admissionFixture, 0, target)
			f.pass(t)
			challenges := socketChallenges(t, f.admissionFixture, socket, 0)
			f.socket.hold = nativeProbeHoldAll
			f.write(t, 1000)
			f.pass(t)
			revoked := p.members[0].established
			require.True(t, f.receive(t, 0, p.members[0].addr, 1200, &wire.PathResponseFrame{Data: challenges[0]}))
			f.pass(t)
			require.True(t, revoked.probeEpoch.revoked.Load())
			cancel()
			require.ErrorIs(t, <-errs, context.Canceled)
			f.pass(t)
			require.Nil(t, p.members[0].transport)
			require.NotSame(t, revoked, p.members[0].established)
			require.False(t, p.members[0].established.probeEpoch.revoked.Load(), "the old tuple has a fresh epoch")
			f.socket.hold = nil
			f.socket.release()
			synctest.Wait()
			f.pass(t)
			f.socket.writes = nil
			f.write(t, 1000)
			f.pass(t)
			require.NotEmpty(t, f.socket.writes, "the old tuple keeps sending")
		})
	})
}

func TestPathMigrationCancelAfterCommit(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newAdmissionFixture(t, protocol.PerspectiveClient)
		f.supplyPeerCIDs(t)
		target, socket := migrationTarget(t, f)
		ctx, cancel := context.WithCancel(context.Background())
		errs := migrateAsync(ctx, f, 0, target)
		f.pass(t)
		challenges := socketChallenges(t, f, socket, 0)
		require.True(t, f.receive(t, 0, f.c.pathCredit.members[0].addr, 1200, &wire.PathResponseFrame{Data: challenges[0]}))
		f.pass(t)
		cancel()
		require.NoError(t, <-errs, "cutover committed, so this reports success")
		require.Equal(t, target, f.c.pathCredit.members[0].transport)
	})
}

func TestPathMigrationAbandonCancels(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveClient)
		p := f.c.pathCredit
		require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathNewConnectionIDFrame{PathID: 1, SequenceNumber: 1, ConnectionID: replacementPeerCID(1), StatelessResetToken: protocol.StatelessResetToken{0xab}}))
		units := p.historyUnits
		target, _ := migrationTarget(t, f.admissionFixture)
		errs := migrateAsync(context.Background(), f.admissionFixture, 1, target)
		f.pass(t)
		require.Equal(t, units+1, p.historyUnits)
		require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathAbandonFrame{PathID: 1}))
		require.ErrorIs(t, <-errs, ErrPathClosed)
		f.pass(t)
		f.pass(t)
		require.Equal(t, units, p.historyUnits, "the attempt's unit is released, and the path's stays")
		require.Nil(t, f.c.pathOpen.Load())
	})
}

// TestPathMigrationIncomingRebinding moves a server's path on the peer's highest
// non-probing packet from a validated tuple, on the CID that validated it.
func TestPathMigrationIncomingRebinding(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newAdmissionFixture(t, protocol.PerspectiveServer)
		f.supplyPeerCIDs(t)
		withFirstPathFinder(f)
		p := f.c.pathCredit
		old := p.members[0].addr
		require.True(t, f.receive(t, 0, admissionTupleB, 1200, &wire.PathChallengeFrame{Data: [8]byte{1}}))
		f.pass(t)
		challenges := admissionChallenges(f.peerWrites(t, 0))
		require.NotEmpty(t, challenges)
		standby := p.members[0].candidate.connID
		require.True(t, f.receive(t, 0, admissionTupleB, 1200, &wire.PathResponseFrame{Data: challenges[0]}))
		f.pass(t)
		require.Equal(t, old, p.members[0].addr, "a probe moves nothing")
		require.Equal(t, standby, p.members[0].standby.connID)

		f.tc.sendConn.EXPECT().ChangeRemoteAddr(admissionTupleB, gomock.Any())
		require.True(t, f.receive(t, 0, admissionTupleB, 100, &wire.PingFrame{}))
		f.pass(t)
		f.pass(t)
		require.Equal(t, admissionTupleB, p.members[0].addr)
		require.Equal(t, standby, f.c.connIDManager.Get(), "the CID that validated it")
		sender := f.c.emission.local.(*nativeOrdinarySender)
		require.Equal(t, admissionTupleB.AddrPort().Port(), sender.destination.Port(), "path zero's sends follow")
		require.Equal(t, 1, p.historyUnits)

		// The packet that created a candidate counts, and newer traffic on the current
		// tuple makes it stale. C is validated while A's packet is the highest.
		tupleC := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 12), Port: 1002}
		tupleD := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 13), Port: 1003}
		cid := func(seq uint64) {
			c := make([]byte, f.tc.destConnID.Len())
			c[0], c[1] = 0xd0, byte(seq)
			require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathNewConnectionIDFrame{PathID: 0, SequenceNumber: seq, ConnectionID: protocol.ParseConnectionID(c), StatelessResetToken: protocol.StatelessResetToken{0xd0, byte(seq)}}))
		}
		cid(5)
		time.Sleep(time.Until(p.retained[0].expiry.ToTime()) + time.Millisecond)
		f.pass(t)
		f.socket.writes = nil
		require.True(t, f.receive(t, 0, tupleC, 1200, &wire.PingFrame{}))
		f.pass(t)
		require.True(t, f.receive(t, 0, admissionTupleB, 100, &wire.PingFrame{}), "newer on the current tuple")
		stale := admissionChallenges(f.peerWrites(t, 0))
		require.NotEmpty(t, stale)
		require.True(t, f.receive(t, 0, tupleC, 1200, &wire.PathResponseFrame{Data: stale[0]}))
		f.pass(t)
		f.pass(t)
		require.Equal(t, admissionTupleB, p.members[0].addr, "stale evidence moves nothing")

		cid(6)
		f.socket.writes = nil
		f.tc.sendConn.EXPECT().ChangeRemoteAddr(tupleD, gomock.Any())
		require.True(t, f.receive(t, 0, tupleD, 1200, &wire.PingFrame{}))
		f.pass(t)
		fresh := admissionChallenges(f.peerWrites(t, 0))
		require.NotEmpty(t, fresh)
		require.True(t, f.receive(t, 0, tupleD, 1200, &wire.PathResponseFrame{Data: fresh[0]}))
		f.pass(t)
		f.pass(t)
		require.Equal(t, tupleD, p.members[0].addr, "the creating packet is enough while it stays highest")
		old = admissionTupleB

		// A probing packet from the old address moves nothing back.
		require.True(t, f.receive(t, 0, old, 100, &wire.PathChallengeFrame{Data: [8]byte{2}}))
		f.pass(t)
		require.Equal(t, tupleD, p.members[0].addr)
	})
}

func TestPathMigrationContention(t *testing.T) {
	t.Run("a second tuple gets no candidate", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveServer)
			f.supplyPeerCIDs(t)
			require.True(t, f.receive(t, 0, admissionTupleA, 1200, &wire.PingFrame{}))
			admitted := f.c.pathCredit.members[0].candidate
			require.NotNil(t, admitted)
			require.True(t, f.receive(t, 0, admissionTupleB, 1200, &wire.PingFrame{}))
			require.Same(t, admitted, f.c.pathCredit.members[0].candidate)
		})
	})
	t.Run("a waiting migration takes an expired revalidation's slot", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newReliableFixture(t, protocol.PerspectiveClient)
			p := f.c.pathCredit
			require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathNewConnectionIDFrame{PathID: 1, SequenceNumber: 1, ConnectionID: replacementPeerCID(1), StatelessResetToken: protocol.StatelessResetToken{0xab}}))
			m := p.members[1]
			p.markSuspect(m, monotime.Now())
			revalidation := m.candidate
			require.NotNil(t, revalidation)
			target, _ := migrationTarget(t, f.admissionFixture)
			ctx, cancel := context.WithCancel(context.Background())
			errs := migrateAsync(ctx, f.admissionFixture, 1, target)
			f.pass(t)
			require.Same(t, revalidation, m.candidate, "an admitted candidate isn't displaced")
			require.Empty(t, errs)
			time.Sleep(time.Until(revalidation.probe.validationDeadline.ToTime()) + time.Millisecond)
			select {
			case <-f.c.sendingScheduled:
			default:
			}
			f.pass(t)
			select {
			case <-f.c.sendingScheduled:
			default:
				t.Fatal("the released slot wakes the waiting migration")
			}
			f.pass(t)
			require.NotNil(t, m.candidate)
			require.Equal(t, target, m.candidate.transport, "revalidation doesn't renew ahead of it")
			require.Nil(t, m.transport, "the old tuple stays until commit")
			cancel()
			require.ErrorIs(t, <-errs, context.Canceled)
			f.pass(t)
		})
	})
}

// TestPathMigrationFifthRefused follows the plan's paper trace on an empty pool.
func TestPathMigrationFifthRefused(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newAdmissionFixture(t, protocol.PerspectiveClient)
		f.supplyPeerCIDs(t)
		p := f.c.pathCredit
		for i := range maxPathHistoryUnits {
			if i > 0 {
				spareZeroCID(t, f, uint64(i+1))
			}
			target, socket := migrationTarget(t, f)
			migrate(t, f, 0, target, socket)
			require.Equal(t, i+1, p.historyUnits)
		}
		spareZeroCID(t, f, maxPathHistoryUnits+1)
		target, _ := migrationTarget(t, f)
		errs := migrateAsync(context.Background(), f, 0, target)
		f.pass(t)
		require.ErrorIs(t, <-errs, ErrMultipathResourceLimitV1)
		require.Empty(t, (*packetHandlerMap)(target).handlers, "it fails before attaching")
		require.Equal(t, maxPathHistoryUnits, p.historyUnits)

		// The connection's own timer reaches retention, with no other traffic.
		require.Equal(t, p.retained[0].expiry, p.nextAdmissionExpiry())

		// One generation's expiry frees exactly one unit.
		time.Sleep(time.Until(p.retained[0].expiry.ToTime()) + time.Millisecond)
		f.pass(t)
		require.Less(t, p.historyUnits, maxPathHistoryUnits)
	})
}

// TestPathMigrationSharedTransportStaysAttached leaves a transport another path uses,
// and a reused transport still costs a generation.
func TestPathMigrationSharedTransportStaysAttached(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f, socket := newTargetPathFixture(t)
		withFirstPathFinder(f.admissionFixture)
		p := f.c.pathCredit
		target := p.members[1].transport
		require.NotNil(t, target)
		target.conn = &migrationRawConn{addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 7001}}
		units := p.historyUnits
		attachments := len(p.attachments)
		spareZeroCID(t, f.admissionFixture, 2)
		migrate(t, f.admissionFixture, 0, target, socket)
		require.Equal(t, units+1, p.historyUnits, "reuse doesn't avoid the generation charge")
		require.Len(t, p.attachments, attachments, "reuse adds no attachment")
		time.Sleep(time.Until(p.retained[0].expiry.ToTime()) + time.Millisecond)
		f.pass(t)
		require.NoError(t, f.c.waitTransportDetachedPrivate(context.Background(), p.origin), "the origin is free")

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.ErrorIs(t, f.c.waitTransportDetachedPrivate(ctx, target), context.DeadlineExceeded, "two paths still use it")
		require.Contains(t, (*packetHandlerMap)(target).handlers, f.issuedCID(1))
	})
}

// TestPathMigrationCancellationReleasesAfterFence holds a canceled probe's claim.
// Its charges stay until the claim returns, and they never free an older unit.
func TestPathMigrationCancellationReleasesAfterFence(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newAdmissionFixture(t, protocol.PerspectiveClient)
		f.supplyPeerCIDs(t)
		p := f.c.pathCredit
		target, socket := migrationTarget(t, f)
		socket.hold = nativeProbeHoldAll
		ctx, cancel := context.WithCancel(context.Background())
		errs := migrateAsync(ctx, f, 0, target)
		f.pass(t)
		require.NotEmpty(t, socket.writes, "the probe is claimed")
		cancel()
		require.ErrorIs(t, <-errs, context.Canceled)
		f.pass(t)
		require.Equal(t, 1, p.historyUnits, "the claim holds the charge")
		require.Contains(t, p.attachments, target)
		require.NotNil(t, f.c.pathOpen.Load(), "and the one local operation")
		socket.hold = nil
		socket.release()
		synctest.Wait()
		f.pass(t)
		f.pass(t)
		require.Zero(t, p.historyUnits)
		require.NotContains(t, p.attachments, target, "nothing uses it now, so it detaches")
		require.Nil(t, f.c.pathOpen.Load())
		require.NoError(t, f.c.waitTransportDetachedPrivate(context.Background(), target))
	})
}

// TestPathDetachAuthority leaves the origin and proves it can no longer destroy or
// speak for the connection, then pits close and a captured reset against detachment.
func TestPathDetachAuthority(t *testing.T) {
	detachOrigin := func(t *testing.T, f *admissionFixture) *Transport {
		t.Helper()
		p := f.c.pathCredit
		target, socket := migrationTarget(t, f)
		migrate(t, f, 0, target, socket)
		time.Sleep(time.Until(p.retained[0].expiry.ToTime()) + time.Millisecond)
		f.pass(t)
		f.pass(t)
		return target
	}
	t.Run("the keyed origin leaves: no routes, no resets, and its socket can close", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			f.route.StatelessResetKey = &StatelessResetKey{1}
			p := f.c.pathCredit
			origin := p.origin
			target := detachOrigin(t, f)
			require.NoError(t, f.c.waitTransportDetachedPrivate(context.Background(), origin))
			require.Empty(t, f.route.handlers)
			require.Empty(t, f.route.resetTokens)
			require.Contains(t, (*packetHandlerMap)(target).handlers, f.tc.srcConnID)
			_, attached := f.c.connIDGenerator.connRunners[f.route]
			require.False(t, attached)
			require.False(t, origin.maybeSendStatelessReset(receivedPacket{data: make([]byte, 100)}), "a surviving CID must draw no valid reset")

			errs := f.openAsync(context.Background(), nil, admissionTupleA)
			f.pass(t)
			require.ErrorIs(t, <-errs, ErrMultipathInvalidConfigV1, "no member can use the transport it left")
			require.False(t, p.paths[1], "no ID is consumed")

			(*Transport)(f.route).transportClosed = true
			sender := f.c.emission.local.(*nativeOrdinarySender)
			require.NotSame(t, sender.endpoint, nativeTransportEndpoint(origin))
			f.pass(t)
			require.NoError(t, f.c.emission.consumeLocal())
			f.c.pathCredit.reselect(monotime.Now())
			require.True(t, p.members[0].live)
		})
	})
	t.Run("close wins and is never reported as detachment", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			p := f.c.pathCredit
			origin := p.origin
			target, socket := migrationTarget(t, f)
			migrate(t, f, 0, target, socket)
			f.route.mutex.Lock()
			(*Transport)(f.route).transportClosed = true
			f.route.mutex.Unlock()
			time.Sleep(time.Until(p.retained[0].expiry.ToTime()) + time.Millisecond)
			f.pass(t)
			f.pass(t)
			require.Contains(t, p.attachments, (*Transport)(nil), "the connection is about to be destroyed")
			require.Contains(t, f.route.handlers, f.tc.srcConnID)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			require.ErrorIs(t, f.c.waitTransportDetachedPrivate(ctx, origin), context.DeadlineExceeded)
		})
	})
	t.Run("a captured reset holds publication", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			p := f.c.pathCredit
			target, socket := migrationTarget(t, f)
			migrate(t, f, 0, target, socket)
			f.route.capturedResets = map[packetHandler]int{f.c: 1}
			time.Sleep(time.Until(p.retained[0].expiry.ToTime()) + time.Millisecond)
			f.pass(t)
			f.pass(t)
			require.Contains(t, p.attachments, (*Transport)(nil))
			delete(f.route.capturedResets, f.c)
			f.pass(t)
			require.NotContains(t, p.attachments, (*Transport)(nil))
		})
	})
	t.Run("a nil target and a never attached one", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			require.ErrorIs(t, f.c.waitTransportDetachedPrivate(context.Background(), nil), ErrMultipathInvalidConfigV1)
			require.NoError(t, f.c.waitTransportDetachedPrivate(context.Background(), &Transport{}))
		})
	})
}
