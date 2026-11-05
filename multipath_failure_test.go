//go:build darwin || linux || freebsd

package quic

import (
	"context"
	"errors"
	"io"
	"net"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"

	"github.com/stretchr/testify/require"
)

var errReadLost = errors.New("read loss")

// on reads a socket other than the fixture's own.
func (f *reliableFixture) on(socket *nativeProbeSocket, read func()) {
	original := f.socket
	f.socket = socket
	defer func() { f.socket = original }()
	read()
}

func (f *targetPathFixture) targetBytes(t *testing.T, socket *nativeProbeSocket, id protocol.PathID) int {
	t.Helper()
	var n int
	f.on(socket, func() { n = reliableBytes(f.parseWrites(t, id)) })
	return n
}

// grantOrigin gives the fixture's own map the endpoint a real transport publishes, so
// its close seals what the connection sends through.
func (f *reliableFixture) grantOrigin() *Transport {
	origin := (*Transport)(f.route)
	origin.responses = newEndpointResponses(f.c.emission.local.(*nativeOrdinarySender).origin.endpoint)
	origin.ConnectionIDLength = origin.connIDLen
	origin.initOnce.Do(func() {})
	return origin
}

func requireEnded(t *testing.T, c *Conn, cause error) {
	t.Helper()
	closeErr := c.closeErr.Load()
	require.NotNil(t, closeErr, "the transport destroyed the connection")
	require.True(t, closeErr.immediate)
	require.ErrorIs(t, closeErr.err, cause)
}

func heldPass(t *testing.T, f *reliableFixture) {
	t.Helper()
	f.pass(t)
	f.pass(t)
}

// TestPathFailurePrimaryContinues faults the endpoint holding path zero.
func TestPathFailurePrimaryContinues(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f, targetSocket := newTargetPathFixture(t)
		p := f.c.pathCredit
		require.Zero(t, p.primary)
		f.grantOrigin()
		observed := f.c.observedRTT()
		require.NoError(t, f.faultPrimary(t))
		heldPass(t, f.reliableFixture)

		zero := p.members[0]
		require.True(t, zero.live, "the failed path stays a member")
		require.False(t, zero.suspect, "it isn't suspect")
		require.False(t, zero.established.probeEpoch.revoked.Load(), "and nothing abandons it")
		require.Equal(t, protocol.PathID(1), p.primary)
		require.Equal(t, 1000, f.targetBytes(t, targetSocket, 1), "reliable work moves to the survivor")
		require.NotSame(t, observed, f.c.observedRTT(), "the RTT observation follows the primary")
		state := p.observation(zero)
		require.Equal(t, "live", state.Lifecycle)
		require.Equal(t, "endpoint-fault", state.LastOutcome)
		require.Nil(t, f.c.closeErr.Load())
	})
}

// TestPathFailureReadLossSpares loses the read side with a native call still out.
func TestPathFailureReadLossSpares(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f, targetSocket := newTargetPathFixture(t)
		p := f.c.pathCredit
		sender := f.c.emission.local.(*nativeOrdinarySender)
		origin := f.grantOrigin()
		f.socket.hold = func([]byte) bool { return true }
		f.write(t, 1000)
		f.pass(t)
		require.Len(t, f.socket.writes, 1)

		origin.close(errReadLost)
		synctest.Wait()
		require.Nil(t, f.c.closeErr.Load(), "the connection is spared")
		require.NotEmpty(t, f.route.handlers)
		f.pass(t)
		require.Equal(t, 0, f.targetBytes(t, targetSocket, 1), "the borrow is outstanding, so nothing is settled")
		held := 0
		for index := range sender.records {
			if sender.records[index].admission.serial != 0 {
				held++
			}
		}
		require.Equal(t, 1, held)

		f.socket.hold = nil
		f.socket.release()
		synctest.Wait()
		select {
		case <-f.c.emission.localWakeup():
		default:
			t.Fatal("retirement must wake the connection")
		}
		heldPass(t, f.reliableFixture)
		for index := range sender.records {
			require.Zero(t, sender.records[index].admission.serial, "each record settles once")
		}
		require.Equal(t, "uncertain", p.members[0].lastOutcome)
		require.Equal(t, 1000, f.targetBytes(t, targetSocket, 1), "the survivor delivers the work")
		require.Equal(t, protocol.PathID(1), p.primary)
		require.Equal(t, "endpoint-fault", p.observation(p.members[0]).LastOutcome)
	})
}

// TestPathFailureExplicitCloseDestroys keeps Transport.Close's logical destruction.
func TestPathFailureExplicitCloseDestroys(t *testing.T) {
	t.Run("before any failure", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f, _ := newTargetPathFixture(t)
			origin := f.grantOrigin()
			done := make(chan struct{})
			go func() {
				origin.close(nil)
				close(done)
			}()
			synctest.Wait()
			requireEnded(t, f.c, net.ErrClosed)
			require.False(t, f.route.detachRoutes(f.c, nil, nil), "close wins over detachment")
			f.c.ctxCancel(nil)
			<-done
		})
	})
	t.Run("after a read failure", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f, _ := newTargetPathFixture(t)
			origin := f.grantOrigin()
			origin.close(errReadLost)
			synctest.Wait()
			require.Nil(t, f.c.closeErr.Load())
			done := make(chan struct{})
			go func() {
				origin.close(nil)
				close(done)
			}()
			synctest.Wait()
			requireEnded(t, f.c, net.ErrClosed)
			require.False(t, f.route.detachRoutes(f.c, nil, nil), "an explicit close still refuses")
			f.c.ctxCancel(nil)
			<-done
			origin.close(nil) // and repeating it does nothing
		})
	})
}

// TestPathFailureLastSurvivorEnds strands nothing when the survivor goes too.
func TestPathFailureLastSurvivorEnds(t *testing.T) {
	t.Run("by fault", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f, _ := newTargetPathFixture(t)
			require.NoError(t, f.faultPrimary(t))
			f.pass(t)
			endpoint := nativeTransportEndpoint(f.target)
			endpoint.do(func(state *endpointState) { state.fail(endpointFaultCause(0), errEndpointFault) })
			synctest.Wait()
			require.ErrorIs(t, f.c.emission.consumeLocal(), errEndpointFault)
		})
	})
	t.Run("by read loss, with the first cause", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f, _ := newTargetPathFixture(t)
			f.grantOrigin().close(errReadLost)
			synctest.Wait()
			require.NoError(t, f.c.emission.consumeLocal())
			f.target.close(errors.New("later"))
			synctest.Wait()
			err := f.c.emission.consumeLocal()
			require.ErrorIs(t, err, errReadLost)
			require.NotContains(t, err.Error(), "later")
		})
	})
}

// TestPathFailureNoSpinOrLateResult keeps the handed-over fault quiet.
func TestPathFailureNoSpinOrLateResult(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f, _ := newTargetPathFixture(t)
		sender := f.c.emission.local.(*nativeOrdinarySender)
		sender.nativeGSO = false
		f.socket.hold = func([]byte) bool { return true }
		f.write(t, 3000)
		f.pass(t)

		// One call is out, and the rest are queued, when the endpoint faults.
		sender.endpoint.do(func(state *endpointState) { state.fail(endpointFaultCause(0), errEndpointFault) })
		synctest.Wait()
		require.NoError(t, f.c.emission.consumeLocal())
		select {
		case <-f.c.emission.localFault():
			t.Fatal("a closed channel in the select would spin")
		default:
		}

		f.socket.hold = nil
		f.socket.result = destinationError(syscall.EBADF)
		f.socket.release()
		synctest.Wait()
		require.NoError(t, f.c.emission.consumeLocal(), "a late fatal result can't end the connection")
		require.Nil(t, f.c.closeErr.Load())
		for index := range sender.records {
			require.Zero(t, sender.records[index].admission.serial)
		}

		// Nothing is left to wake the loop.
		heldPass(t, f.reliableFixture)
		f.socket.result = nil
		select {
		case <-f.c.emission.localFault():
			t.Fatal("still quiet")
		case <-time.After(time.Millisecond):
		}
	})
}

// TestPathFailureDatagramLimit stops a failed path zero bounding SendDatagram.
func TestPathFailureDatagramLimit(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f, _ := newTargetPathFixture(t)
		f.c.datagramQueue = newDatagramQueue(f.c.scheduleSending, f.c.logger)
		f.setPathMTU(1, 1400)
		f.pass(t)
		limit := func() protocol.ByteCount {
			return min(protocol.ByteCount(f.c.maxPayloadSizeEstimate.Load()), protocol.ByteCount(f.c.pathPayloadLimit.Load()))
		}
		before := limit()
		require.NoError(t, f.faultPrimary(t))
		heldPass(t, f.reliableFixture)
		require.Greater(t, limit(), before, "the limit follows the survivor")
	})
}

// TestPathFailureNothingPacksOnFailedZero covers every packet a nil route could spend.
func TestPathFailureNothingPacksOnFailedZero(t *testing.T) {
	t.Run("a pending response spends no packet number", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f, _ := newTargetPathFixture(t)
			require.True(t, f.receive(t, 0, f.c.RemoteAddr(), 0, &wire.PathChallengeFrame{Data: [8]byte{1}}))
			sender := f.c.emission.local.(*nativeOrdinarySender)
			sender.endpoint.do(func(state *endpointState) { state.fail(endpointFaultCause(0), errEndpointFault) })
			synctest.Wait()
			pn, _ := f.recovery(0).PeekPacketNumber(protocol.Encryption1RTT)
			f.socket.writes = nil
			f.pass(t)
			require.Empty(t, f.socket.writes)
			next, _ := f.recovery(0).PeekPacketNumber(protocol.Encryption1RTT)
			require.Equal(t, pn, next)
			require.Nil(t, f.c.pathCredit.members[0].response)
		})
	})
	t.Run("capacity doesn't wait, and a suspect survivor carries ACKs", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f, targetSocket := newTargetPathFixture(t)
			p := f.c.pathCredit
			require.NoError(t, f.faultPrimary(t))
			f.pass(t)
			p.markSuspect(p.members[1], monotime.Now())
			f.pass(t)
			f.pass(t)
			require.Equal(t, noPrimary, p.primary)
			require.Nil(t, f.c.emission.capacity(), "the failed endpoint must not hold back admission probes")

			targetSocket.writes = nil
			require.True(t, f.receive(t, 1, admissionTupleA, 0, &wire.PingFrame{}))
			require.True(t, f.receive(t, 1, admissionTupleA, 0, &wire.PingFrame{}))
			f.pass(t)
			acked := false
			f.on(targetSocket, func() {
				for _, write := range f.parseWrites(t, 1) {
					acked = acked || len(write.acks) != 0
				}
			})
			require.True(t, acked, "the ACK leaves on the suspect path")
			require.True(t, p.members[1].suspect)
			require.Equal(t, noPrimary, p.primary, "without promoting it")
		})
	})
}

// TestPathFailureMigrateAway restores redundancy through the existing migration.
func TestPathFailureMigrateAway(t *testing.T) {
	setup := func(t *testing.T) (*targetPathFixture, *Transport, *nativeProbeSocket) {
		f, _ := newTargetPathFixture(t)
		spareZeroCID(t, f.admissionFixture, 8)
		next, socket := migrationTarget(t, f.admissionFixture)
		return f, next, socket
	}
	t.Run("the fence waits, the commit moves zero, and it carries", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f, next, socket := setup(t)
			p := f.c.pathCredit
			sender := f.c.emission.local.(*nativeOrdinarySender)
			f.socket.hold = func([]byte) bool { return true }
			f.write(t, 1000)
			f.pass(t)
			sender.endpoint.do(func(state *endpointState) { state.fail(endpointFaultCause(0), errEndpointFault) })
			synctest.Wait()
			require.NoError(t, f.c.emission.consumeLocal())

			errs := migrateAsync(context.Background(), f.admissionFixture, 0, next)
			f.pass(t)
			challenges := socketChallenges(t, f.admissionFixture, socket, 0)
			require.NotEmpty(t, challenges, "a probe toward a healthy target still leaves")
			require.True(t, f.receive(t, 0, p.members[0].addr, 1200, &wire.PathResponseFrame{Data: challenges[len(challenges)-1]}))
			f.pass(t)
			require.Empty(t, errs, "the held native call holds the attempt")
			require.Equal(t, (*Transport)(nil), p.members[0].transport)

			f.socket.hold = nil
			f.socket.release()
			synctest.Wait()
			f.pass(t)
			f.pass(t)
			require.NoError(t, <-errs)
			require.Equal(t, next, p.members[0].transport)
			require.True(t, p.eligible(p.members[0]), "zero moved to a healthy target carries again")

			p.selectPrimaryPrivate(0)
			socket.writes = nil
			second, err := f.c.OpenUniStream()
			require.NoError(t, err)
			writeAsync(t, second, 500)
			f.pass(t)
			f.pass(t)
			var carried int
			f.on(socket, func() { carried = reliableBytes(f.parseWrites(t, 0)) })
			require.GreaterOrEqual(t, carried, 500)
		})
	})
	t.Run("a failed target refuses with nothing charged", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f, _, _ := setup(t)
			p := f.c.pathCredit
			require.NoError(t, f.faultPrimary(t))
			f.pass(t)
			units, refs := p.historyUnits, p.attachments[nil].refs
			origin := f.grantOrigin()

			errs := migrateAsync(context.Background(), f.admissionFixture, 0, origin)
			f.pass(t)
			require.ErrorIs(t, <-errs, net.ErrClosed, "ahead of the same-target shortcut")

			errs = migrateAsync(context.Background(), f.admissionFixture, 1, origin)
			f.pass(t)
			require.ErrorIs(t, <-errs, net.ErrClosed, "through the origin")

			errs = f.openAsync(context.Background(), origin, admissionTupleB)
			f.pass(t)
			require.ErrorIs(t, <-errs, net.ErrClosed, "an open refuses the same way")

			f.c.peerParams.DisableActiveMigration = true
			errs = f.openAsync(context.Background(), origin, p.handshakeAddr)
			f.pass(t)
			require.ErrorIs(t, <-errs, net.ErrClosed, "ahead of the peer's migration refusal")
			require.Equal(t, units, p.historyUnits)
			require.Equal(t, refs, p.attachments[nil].refs)
		})
	})
	t.Run("a refusal frees the operation before the caller learns of it", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f, _, _ := setup(t)
			request := &pathOpenRequest{done: make(chan error)}
			f.c.pathOpen.Store(request)
			go func() { _ = f.c.pathCredit.refuseRequest(request, errPathTargetFailed) }()
			synctest.Wait()
			require.Nil(t, f.c.pathOpen.Load(), "a caller's immediate retry must not meet its own request")
			require.ErrorIs(t, <-request.done, net.ErrClosed)
		})
	})
	t.Run("a target failing before the response ends the open", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			target, socket := migrationTarget(t, f)
			errs := f.openAsync(context.Background(), target, admissionTupleA)
			f.pass(t)
			challenges := socketChallenges(t, f, socket, 1)
			require.NotEmpty(t, challenges)

			nativeTransportEndpoint(target).do(func(state *endpointState) { state.fail(endpointFaultCause(0), errEndpointFault) })
			synctest.Wait()
			require.True(t, f.receive(t, 1, admissionTupleA, 1200, &wire.PathResponseFrame{Data: challenges[0]}))
			f.pass(t)
			require.ErrorIs(t, <-errs, net.ErrClosed, "the response can't claim success")
			require.False(t, f.c.pathCredit.members[1].live)
			require.NotNil(t, f.c.pathCredit.terminals[1])
			require.True(t, f.c.pathCredit.eligible(f.c.pathCredit.members[0]), "path zero carries on")
		})
	})
	t.Run("a target failing during the fence ends the attempt", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f, next, socket := setup(t)
			p := f.c.pathCredit
			f.socket.hold = func([]byte) bool { return true }
			f.write(t, 1000)
			f.pass(t)
			errs := migrateAsync(context.Background(), f.admissionFixture, 0, next)
			f.pass(t)
			challenges := socketChallenges(t, f.admissionFixture, socket, 0)
			require.NotEmpty(t, challenges)
			require.True(t, f.receive(t, 0, p.members[0].addr, 1200, &wire.PathResponseFrame{Data: challenges[len(challenges)-1]}))
			f.pass(t)
			require.Empty(t, errs)
			revoked := p.members[0].established

			endpoint := nativeTransportEndpoint(next)
			endpoint.do(func(state *endpointState) { state.fail(endpointFaultCause(0), errEndpointFault) })
			f.socket.hold = nil
			f.socket.release()
			synctest.Wait()
			f.pass(t)
			require.ErrorIs(t, <-errs, net.ErrClosed)
			require.Equal(t, (*Transport)(nil), p.members[0].transport, "the old tuple stays selected")
			require.NotSame(t, revoked, p.members[0].established)
			require.False(t, p.members[0].established.probeEpoch.revoked.Load(), "on a fresh epoch")
		})
	})
}

// TestPathFailureRetireAndDetach restores redundancy through the existing retirement.
func TestPathFailureRetireAndDetach(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f, targetSocket := newTargetPathFixture(t)
		p := f.c.pathCredit
		origin := f.grantOrigin()
		origin.close(errReadLost)
		synctest.Wait()
		f.pass(t)
		targetSocket.writes = nil

		require.NoError(t, p.abandon(0, monotime.Now()))
		f.pass(t)
		f.pass(t)
		abandoned := false
		f.on(targetSocket, func() {
			for _, write := range f.peerWrites(t, 1) {
				for _, frame := range write.frames {
					if _, ok := frame.(*wire.PathAbandonFrame); ok {
						abandoned = true
					}
				}
			}
		})
		require.True(t, abandoned, "the signal leaves on the survivor")

		time.Sleep(p.terminals[0].expiry.Sub(monotime.Now()) + time.Millisecond)
		f.pass(t)
		f.pass(t)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, f.c.waitTransportDetachedPrivate(ctx, origin))
		require.NotContains(t, f.route.handlers, f.tc.srcConnID)

		origin.close(nil)
		require.Nil(t, f.c.closeErr.Load(), "a later close is harmless")
	})
}

// TestPathFailureRealSocket closes a real socket under a live connection, so both
// producers report, and the peer has to find the survivor through its own recovery.
func TestPathFailureRealSocket(t *testing.T) {
	p := newMultipathPair(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	path, err := p.client.OpenPathV1(ctx, grantedTransport(t), p.serverAddr)
	require.NoError(t, err)
	waitPathState(t, path, func(state MultipathPathStateV1) bool { return state.Lifecycle == "live" })

	require.NoError(t, p.clientTr.Conn.Close())
	<-p.clientTr.listening
	require.NoError(t, p.client.Context().Err(), "the survivor keeps the connection")
	waitPathState(t, path, func(state MultipathPathStateV1) bool { return state.Primary })

	const size = 256 << 10
	stream, err := p.client.OpenUniStreamSync(ctx)
	require.NoError(t, err)
	_, err = stream.Write(make([]byte, size))
	require.NoError(t, err)
	require.NoError(t, stream.Close())
	incoming, err := p.server.AcceptUniStream(ctx)
	require.NoError(t, err)
	data, err := io.ReadAll(incoming)
	require.NoError(t, err)
	require.Len(t, data, size)

	require.NoError(t, p.clientTr.Close())
	<-p.client.Context().Done()
	require.ErrorIs(t, context.Cause(p.client.Context()), net.ErrClosed, "an explicit close still destroys it")
}

// TestPathFailureOrdinaryUnchanged keeps the baseline for everything else.
func TestPathFailureOrdinaryUnchanged(t *testing.T) {
	t.Run("an ordinary fault fails the connection", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			tc, sender, _ := nativeProbeRecoveryConnection(t, "reno")
			sender.endpoint.do(func(state *endpointState) { state.fail(endpointFaultCause(0), errEndpointFault) })
			synctest.Wait()
			require.ErrorIs(t, tc.conn.emission.consumeLocal(), errEndpointFault)
			<-tc.conn.emission.localFault()
		})
	})
	t.Run("an unactivated connection is destroyed", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixtureUnactivated(t)
			route := (*Transport)(f.route)
			done := make(chan struct{})
			go func() {
				route.close(errReadLost)
				close(done)
			}()
			synctest.Wait()
			requireEnded(t, f.c, errReadLost)
			f.c.ctxCancel(nil)
			<-done
		})
	})
	t.Run("a server has no other endpoint", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newReliableFixture(t, protocol.PerspectiveServer)
			origin := f.grantOrigin()
			done := make(chan struct{})
			go func() {
				origin.close(errReadLost)
				close(done)
			}()
			synctest.Wait()
			requireEnded(t, f.c, errReadLost)
			f.c.ctxCancel(nil)
			<-done
		})
	})
}

// requireFreedBeforeReport runs a failing step against a request whose result channel is
// unbuffered, so the connection parks at the send. What the slot holds there is what a
// caller finds on hearing the error, with no further pass.
func requireFreedBeforeReport(t *testing.T, f *admissionFixture, request *pathOpenRequest, want error, step func() error) {
	t.Helper()
	stepErr := make(chan error, 1)
	go func() { stepErr <- step() }()
	synctest.Wait()
	slot := f.c.pathOpen.Load()
	reported := <-request.done
	require.NoError(t, <-stepErr)
	require.Nil(t, slot, "a caller's immediate retry must not meet its own request")
	require.ErrorIs(t, reported, want)
}

// failingOpen admits an open whose probe has already left and resolved.
func failingOpen(t *testing.T, f *admissionFixture, target *Transport) *pathOpenRequest {
	t.Helper()
	request := &pathOpenRequest{ctx: context.Background(), transport: target, addr: admissionTupleA, done: make(chan error)}
	f.c.pathOpen.Store(request)
	f.pass(t)
	f.pass(t)
	require.True(t, request.accepted)
	return request
}

// failingMigration admits a migration whose probe has already left and resolved.
func failingMigration(t *testing.T, f *admissionFixture, id protocol.PathID, target *Transport) *pathOpenRequest {
	t.Helper()
	request := &pathOpenRequest{ctx: context.Background(), transport: target, migrate: true, id: id, done: make(chan error)}
	f.c.pathOpen.Store(request)
	f.pass(t)
	f.pass(t)
	require.True(t, request.accepted)
	return request
}

func failEndpoint(target *Transport) {
	nativeTransportEndpoint(target).do(func(state *endpointState) { state.fail(endpointFaultCause(0), errEndpointFault) })
	synctest.Wait()
}

func TestPathFailureFreesOperationBeforeReport(t *testing.T) {
	t.Run("an open's validation timeout", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			request := failingOpen(t, f, nil)
			p := f.c.pathCredit
			time.Sleep(pathTimeout)
			requireFreedBeforeReport(t, f, request, errPathValidationTimeout, func() error { return p.service(monotime.Now()) })
			require.True(t, p.terminals[1].signaled, "no submission can follow the abandonment")
			require.False(t, p.members[1].live)
		})
	})
	t.Run("an open's target failing", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			target, _ := migrationTarget(t, f)
			request := failingOpen(t, f, target)
			p := f.c.pathCredit
			failEndpoint(target)
			requireFreedBeforeReport(t, f, request, net.ErrClosed, func() error { return p.service(monotime.Now()) })
			require.True(t, p.terminals[1].signaled)
			require.True(t, p.eligible(p.members[0]), "path zero carries on")
		})
	})
	t.Run("an open's member retiring", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			request := failingOpen(t, f, nil)
			p := f.c.pathCredit
			requireFreedBeforeReport(t, f, request, ErrPathClosed, func() error { return p.abandon(1, monotime.Now()) })
			require.True(t, p.terminals[1].signaled)
			require.True(t, p.terminals[1].received, "the received abandonment still starts its retention")
		})
	})
	t.Run("a migration's validation timeout", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			target, _ := migrationTarget(t, f)
			request := failingMigration(t, f, 0, target)
			p := f.c.pathCredit
			time.Sleep(pathTimeout)
			requireFreedBeforeReport(t, f, request, errPathValidationTimeout, func() error { return p.service(monotime.Now()) })
			require.True(t, request.retained.released)
			require.Zero(t, p.historyUnits)
			require.Nil(t, p.members[0].transport, "the old tuple stays selected")
		})
	})
	t.Run("a migration's target failing", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			target, _ := migrationTarget(t, f)
			request := failingMigration(t, f, 0, target)
			p := f.c.pathCredit
			failEndpoint(target)
			requireFreedBeforeReport(t, f, request, net.ErrClosed, func() error { return p.service(monotime.Now()) })
			require.True(t, request.retained.released)
			require.Zero(t, p.historyUnits)
		})
	})
	t.Run("the peer retiring a migration candidate's CID", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newReliableFixture(t, protocol.PerspectiveClient)
			target, socket := migrationTarget(t, f.admissionFixture)
			request := failingMigration(t, f.admissionFixture, 0, target)
			p := f.c.pathCredit
			challenges := socketChallenges(t, f.admissionFixture, socket, 0)
			require.NotEmpty(t, challenges)
			f.socket.hold = nativeProbeHoldAll
			f.write(t, 1000)
			f.pass(t)
			require.True(t, f.receive(t, 0, p.members[0].addr, 1200, &wire.PathResponseFrame{Data: challenges[0]}))
			f.pass(t)
			require.True(t, p.members[0].fencing())
			revoked := p.members[0].established
			cid := make([]byte, f.tc.destConnID.Len())
			cid[0] = 0xe0
			require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathNewConnectionIDFrame{PathID: 0, SequenceNumber: 9, RetirePriorTo: 9, ConnectionID: protocol.ParseConnectionID(cid), StatelessResetToken: protocol.StatelessResetToken{0xe0}}))
			f.socket.hold = nil
			f.socket.release()
			synctest.Wait()
			requireFreedBeforeReport(t, f.admissionFixture, request, errPathCandidateCIDRetired, func() error { return p.service(monotime.Now()) })
			require.NotSame(t, revoked, p.members[0].established, "the old tuple has its fresh epoch before the caller hears")
		})
	})
	t.Run("a migration's member retiring", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newReliableFixture(t, protocol.PerspectiveClient)
			p := f.c.pathCredit
			require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathNewConnectionIDFrame{PathID: 1, SequenceNumber: 1, ConnectionID: replacementPeerCID(1), StatelessResetToken: protocol.StatelessResetToken{0xab}}))
			units := p.historyUnits
			target, _ := migrationTarget(t, f.admissionFixture)
			request := failingMigration(t, f.admissionFixture, 1, target)
			require.Equal(t, units+1, p.historyUnits)
			requireFreedBeforeReport(t, f.admissionFixture, request, ErrPathClosed, func() error { return p.abandon(1, monotime.Now()) })
			require.Equal(t, units, p.historyUnits, "the attempt's unit is released, and the path's stays")
		})
	})
}

// TestPathFailureRetryFindsNoStaleSlot starts the next operation with no pass between: it
// holds the slot itself, and only its own admission limits decide from there.
func TestPathFailureRetryFindsNoStaleSlot(t *testing.T) {
	t.Run("an open after a failed open", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			p := f.c.pathCredit
			request := failingOpen(t, f, nil)
			time.Sleep(pathTimeout)
			requireFreedBeforeReport(t, f, request, errPathValidationTimeout, func() error { return p.service(monotime.Now()) })

			ctx, cancel := context.WithCancel(context.Background())
			errs := f.openAsync(ctx, nil, admissionTupleB)
			retry := f.c.pathOpen.Load()
			require.NotNil(t, retry)
			require.NotSame(t, request, retry)
			require.Empty(t, errs, "no backpressure from the finished request")

			f.pass(t)
			require.False(t, retry.accepted, "no peer grant yet, and that limit still applies")
			require.Empty(t, errs)
			require.NoError(t, creditIngress(t, f.tc, monotime.Now(),
				&wire.MaxPathIDFrame{MaximumPathID: 2},
				&wire.PathNewConnectionIDFrame{PathID: 2, ConnectionID: replacementPeerCID(2), StatelessResetToken: protocol.StatelessResetToken{0xa2}},
			))
			f.pass(t)
			f.pass(t)
			require.True(t, retry.accepted)
			require.Equal(t, protocol.PathID(2), retry.id, "the failed attempt's ID is never reused")
			cancel()
			require.ErrorIs(t, <-errs, context.Canceled)
		})
	})
	t.Run("a migration after a failed migration", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			spareZeroCID(t, f, 8)
			p := f.c.pathCredit
			failed, _ := migrationTarget(t, f)
			request := failingMigration(t, f, 0, failed)
			failEndpoint(failed)
			requireFreedBeforeReport(t, f, request, net.ErrClosed, func() error { return p.service(monotime.Now()) })

			next, socket := migrationTarget(t, f)
			migrate(t, f, 0, next, socket)
		})
	})
}

// TestPathFailureHeldObligation holds a probe's native call across the failure. The caller
// hears at once and the attempt's obligation keeps the slot and its charges, until the one
// pass that sees the call return releases them. The endpoint's watchdog would fault a call
// held for a whole validation lifetime, so these bring the attempt's deadline forward.
func TestPathFailureHeldObligation(t *testing.T) {
	t.Run("an open's unresolved fence", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			p := f.c.pathCredit
			f.socket.hold = nativeProbeHoldAll
			errs := f.openAsync(context.Background(), nil, admissionTupleA)
			f.pass(t)
			require.Len(t, f.socket.writes, 1, "the probe is claimed")
			p.members[1].deadline = monotime.Now()
			f.pass(t)
			require.ErrorIs(t, <-errs, errPathValidationTimeout, "the failure doesn't wait for the native call")
			terminal := p.terminals[1]
			require.NotNil(t, terminal)
			require.False(t, terminal.signaled, "the fence precedes the signal")
			require.NotNil(t, f.c.pathOpen.Load(), "and the slot stays charged")
			require.True(t, p.paths[1], "the ID stays consumed")
			_, err := f.c.openPathPrivate(context.Background(), nil, admissionTupleB)
			require.ErrorIs(t, err, ErrMultipathBackpressureV1)

			f.socket.hold = nil
			f.socket.release()
			f.pass(t)
			require.True(t, terminal.signaled)
			require.Nil(t, f.c.pathOpen.Load(), "the pass that signals frees the slot")
			f.pass(t)
			require.Equal(t, 1, admissionAbandons(f.peerWrites(t, 0), 1), "exactly one abandonment")
		})
	})
	t.Run("a migration's unreleased entry", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			p := f.c.pathCredit
			target, socket := migrationTarget(t, f)
			socket.hold = nativeProbeHoldAll
			errs := migrateAsync(context.Background(), f, 0, target)
			f.pass(t)
			require.NotEmpty(t, socket.writes, "the probe is claimed")
			p.members[0].candidate.probe.validationDeadline = monotime.Now()
			f.pass(t)
			require.ErrorIs(t, <-errs, errPathValidationTimeout, "the failure doesn't wait for the native call")
			require.Equal(t, 1, p.historyUnits, "the claim holds the charge")
			require.Contains(t, p.attachments, target)
			request := f.c.pathOpen.Load()
			require.NotNil(t, request, "and the slot")
			require.ErrorIs(t, f.c.migratePathPrivate(context.Background(), 0, target), ErrMultipathBackpressureV1)

			socket.hold = nil
			socket.release()
			f.pass(t)
			require.True(t, request.retained.released)
			require.Zero(t, p.historyUnits, "released once")
			require.Nil(t, f.c.pathOpen.Load(), "the pass that releases the entry frees the slot")
			f.pass(t)
			require.Zero(t, p.historyUnits, "and only once")
			require.NoError(t, f.c.waitTransportDetachedPrivate(context.Background(), target))
		})
	})
	t.Run("a migration's old-tuple fence holds its path, not the slot", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f, _ := newTargetPathFixture(t)
			spareZeroCID(t, f.admissionFixture, 8)
			p := f.c.pathCredit
			failed, socket := migrationTarget(t, f.admissionFixture)
			errs := migrateAsync(context.Background(), f.admissionFixture, 0, failed)
			f.pass(t)
			challenges := socketChallenges(t, f.admissionFixture, socket, 0)
			require.NotEmpty(t, challenges)
			f.socket.hold = nativeProbeHoldAll
			f.write(t, 1000)
			f.pass(t)
			require.True(t, f.receive(t, 0, p.members[0].addr, 1200, &wire.PathResponseFrame{Data: challenges[len(challenges)-1]}))
			f.pass(t)
			require.True(t, p.members[0].fencing())

			failEndpoint(failed)
			f.pass(t)
			require.ErrorIs(t, <-errs, net.ErrClosed)
			require.Nil(t, f.c.pathOpen.Load())

			next, nextSocket := migrationTarget(t, f.admissionFixture)
			retry := migrateAsync(context.Background(), f.admissionFixture, 0, next)
			f.pass(t)
			require.NotNil(t, f.c.pathOpen.Load(), "the retry holds the slot")
			require.False(t, f.c.pathOpen.Load().accepted, "and waits there for its path's fence")
			require.Empty(t, retry)

			f.socket.hold = nil
			f.socket.release()
			synctest.Wait()
			f.pass(t)
			retried := socketChallenges(t, f.admissionFixture, nextSocket, 0)
			require.NotEmpty(t, retried, "the fence resolved, so the retry's probe leaves")
			require.True(t, f.receive(t, 0, p.members[0].addr, 1200, &wire.PathResponseFrame{Data: retried[len(retried)-1]}))
			f.pass(t)
			f.pass(t)
			require.NoError(t, <-retry)
			require.Equal(t, next, p.members[0].transport)
		})
	})
}

func TestPathFailureOutcomeOwnership(t *testing.T) {
	t.Run("a later cancellation doesn't replace the connection's failure", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			errs := f.openAsync(ctx, nil, admissionTupleA)
			f.pass(t)
			f.pass(t)
			time.Sleep(pathTimeout)
			f.pass(t)
			cancel()
			require.ErrorIs(t, <-errs, errPathValidationTimeout)
		})
	})
	t.Run("a finished request's cleanup never frees a newer one", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			p := f.c.pathCredit
			finished := &pathOpenRequest{done: make(chan error, 1), migrate: true, retained: &pathRetained{released: true}}
			require.True(t, finished.claim())
			newer := &pathOpenRequest{ctx: context.Background(), addr: admissionTupleA, done: make(chan error, 1)}
			f.c.pathOpen.Store(newer)
			p.failOperation(finished, errPathValidationTimeout)
			require.Same(t, newer, f.c.pathOpen.Load())
			require.ErrorIs(t, <-finished.done, errPathValidationTimeout)
		})
	})
}
