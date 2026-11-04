//go:build darwin || linux || freebsd

package quic

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

// closeWrites returns the datagrams that open on path id and carry a CONNECTION_CLOSE.
func (f *reliableFixture) closeWrites(t *testing.T, id protocol.PathID) []admissionWrite {
	t.Helper()
	var closes []admissionWrite
	for _, write := range f.peerWrites(t, id) {
		for _, frame := range write.frames {
			if _, ok := frame.(*wire.ConnectionCloseFrame); ok {
				closes = append(closes, write)
			}
		}
	}
	return closes
}

func (f *reliableFixture) closeLocally(t *testing.T, message string) {
	t.Helper()
	f.c.sentFirstPacket = true // the fixture has no Initial flight
	f.c.handleCloseError(&closeError{err: &qerr.ApplicationError{ErrorCode: 7, ErrorMessage: message}})
	f.c.emission.local.Close() // as the run loop does; recovery results no longer hold the close
	synctest.Wait()
}

// trigger routes a packet through the transport that holds id's connection ID.
func (f *reliableFixture) trigger(t *testing.T, route *packetHandlerMap, cid protocol.ConnectionID, from net.Addr) {
	t.Helper()
	buffer := getPacketBuffer()
	buffer.Data = append(buffer.Data, 0x40)
	buffer.Data = append(buffer.Data, cid.Bytes()...)
	buffer.Data = append(buffer.Data, make([]byte, 32)...)
	require.True(t, route.deliverPermitted(cid, receivedPacket{remoteAddr: from, buffer: buffer, data: buffer.Data}))
}

// TestPathCloseSurvivesAbandonedZero closes after the peer abandoned path zero. The
// close goes through path 1's own identity and tuple, and never reads zero's CID state.
func TestPathCloseSurvivesAbandonedZero(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveClient)
		require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathAbandonFrame{PathID: 0}))
		f.pass(t)
		f.pass(t)
		f.socket.writes = nil

		f.closeLocally(t, "alternate")

		closes := f.closeWrites(t, 1)
		require.Len(t, closes, 1)
		requireTuple(t, admissionTupleA, closes[0].destination)
		require.Empty(t, f.closeWrites(t, 0))
	})
}

// TestPathCloseAfterZeroReclaimed is the same close once zero's records are gone.
func TestPathCloseAfterZeroReclaimed(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveClient)
		require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathAbandonFrame{PathID: 0}))
		f.pass(t)
		f.pass(t)
		time.Sleep(time.Until(f.c.pathCredit.terminals[0].expiry.ToTime()) + time.Millisecond)
		f.pass(t)
		f.pass(t)
		require.Nil(t, f.c.connIDManager.pathCIDs[0])
		f.socket.writes = nil

		f.closeLocally(t, "reclaimed")

		require.Len(t, f.closeWrites(t, 1), 1)
	})
}

// TestPathCloseEveryLivePath sends each path its own packet, on its own tuple. When a
// write is outstanding, both closes wait for it on the one endpoint.
func TestPathCloseEveryLivePath(t *testing.T) {
	for name, held := range map[string]bool{"idle": false, "outstanding write": true} {
		t.Run(name, func(t *testing.T) {
			nativeOrdinaryTest(t, func(t *testing.T) {
				f := newReliableFixture(t, protocol.PerspectiveClient)
				if held {
					f.socket.hold = nativeProbeHoldAll
					f.write(t, 1000)
					f.pass(t)
				}
				f.socket.writes = nil

				f.closeLocally(t, "both")
				if held {
					f.socket.hold = nil
					f.socket.release()
					synctest.Wait()
				}

				zero, one := f.closeWrites(t, 0), f.closeWrites(t, 1)
				require.Len(t, zero, 1)
				require.Len(t, one, 1)
				requireTuple(t, nativeProbePeerAddr(), zero[0].destination)
				requireTuple(t, admissionTupleA, one[0].destination)
				require.NotEqual(t, zero[0].data, one[0].data)
			})
		})
	}
}

// TestPathCloseOversizedReasonCostsOneContext keeps the path that can carry the reason.
func TestPathCloseOversizedReasonCostsOneContext(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveClient)
		f.setPathMTU(1, 1400)
		reason := strings.Repeat("r", 1300)
		f.socket.writes = nil

		f.closeLocally(t, reason)

		require.Empty(t, f.closeWrites(t, 0), "the reason doesn't fit path zero")
		require.Len(t, f.closeWrites(t, 1), 1)
	})
}

// TestPathCloseCachedResponses answers each trigger with a retained path's own
// packet at that path's tuple, whichever tuple the trigger came from.
func TestPathCloseCachedResponses(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveClient)
		f.route.closeQueue = make(chan closePacket, 4)
		local := packetInfo{addr: netip.MustParseAddr("192.0.2.1"), ifIndex: 3}
		f.c.pathCredit.members[0].info = local
		stranger := &net.UDPAddr{IP: net.IPv4(198, 51, 100, 9), Port: 7}
		zeroCID := f.c.connIDGenerator.activeSrcConnIDs[0]
		oneCID := f.issuedCID(1)
		f.socket.writes = nil
		f.closeLocally(t, "cached")
		live := f.socket.writes
		require.Len(t, live, 2)

		f.trigger(t, f.route, oneCID, stranger)
		reply := <-f.route.closeQueue
		require.Equal(t, admissionTupleA.String(), reply.addr.String(), "never the trigger's source")
		require.Equal(t, f.closeWrites(t, 1)[0].data, reply.payload)

		// The counter is shared, so this is the second trigger and it is answered.
		f.trigger(t, f.route, zeroCID, stranger)
		reply = <-f.route.closeQueue
		require.Equal(t, nativeProbePeer.String(), reply.addr.String())
		require.Equal(t, f.closeWrites(t, 0)[0].data, reply.payload)
		require.Equal(t, local, reply.info, "the original local source, not the trigger's")

		// The third trigger falls to the exponential backoff.
		f.trigger(t, f.route, oneCID, stranger)
		require.Empty(t, f.route.closeQueue)
	})
}

// TestPathCloseTriggerOnRetiredPath answers through the survivor, whichever path
// was abandoned: an abandoned context carries neither the close nor a cached reply.
func TestPathCloseTriggerOnRetiredPath(t *testing.T) {
	for retired, survivor := range map[protocol.PathID]protocol.PathID{0: 1, 1: 0} {
		t.Run(fmt.Sprintf("path %d", retired), func(t *testing.T) {
			nativeOrdinaryTest(t, func(t *testing.T) {
				f := newReliableFixture(t, protocol.PerspectiveClient)
				f.route.closeQueue = make(chan closePacket, 4)
				cid := f.c.connIDGenerator.activeSrcConnIDs[0]
				if retired == 1 {
					cid = f.issuedCID(1)
				}
				tuple := map[protocol.PathID]string{0: nativeProbePeerAddr().String(), 1: admissionTupleA.String()}[survivor]
				require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathAbandonFrame{PathID: retired}))
				f.pass(t)
				f.pass(t)
				f.socket.writes = nil
				f.closeLocally(t, "retired")
				require.Empty(t, f.closeWrites(t, retired))
				closes := f.closeWrites(t, survivor)
				require.Len(t, closes, 1)

				f.trigger(t, f.route, cid, nativeProbePeerAddr())
				reply := <-f.route.closeQueue
				require.Equal(t, tuple, reply.addr.String())
				require.Equal(t, closes[0].data, reply.payload)
			})
		})
	}
}

// TestPathCloseRetentionIsNotRefreshed holds routing for one snapshot of the largest
// PTO, whatever arrives meanwhile, then releases the reference and its queued sends.
func TestPathCloseRetentionIsNotRefreshed(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveClient)
		f.route.closeQueue = make(chan closePacket, 8)
		f.c.pathCredit.rtt[1].UpdateRTT(200*time.Millisecond, 0)
		largest := f.c.pathCredit.largestPTO(true)
		require.Greater(t, largest, f.c.observedRTT().PTO(false), "the slower path sets retention")
		oneCID := f.issuedCID(1)
		f.closeLocally(t, "retention")
		for range 5 {
			time.Sleep(largest / 2)
			f.trigger(t, f.route, oneCID, nativeProbePeerAddr())
		}
		time.Sleep(largest/2 - time.Millisecond)
		routed := func() bool {
			f.route.mutex.Lock()
			defer f.route.mutex.Unlock()
			_, ok := f.route.handlers[oneCID]
			return ok
		}
		require.True(t, routed(), "still retained just before expiry")
		time.Sleep(2 * time.Millisecond)
		synctest.Wait()
		require.False(t, routed(), "traffic did not extend the timer")
	})
}

// TestPathCloseFaultedPrimaryUsesSurvivor closes through a healthy second endpoint.
func TestPathCloseFaultedPrimaryUsesSurvivor(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f, targetSocket := newTargetPathFixture(t)
		cid := f.issuedCID(1)
		zeroCID := f.c.connIDGenerator.activeSrcConnIDs[0]
		(*packetHandlerMap)(f.target).closeQueue = make(chan closePacket, 4)
		f.route.closeQueue = make(chan closePacket, 4)
		fault := f.faultPrimary(t)

		f.c.sentFirstPacket = true
		f.c.handleCloseError(&closeError{err: fault, immediate: true})
		f.c.emission.local.Close()
		synctest.Wait()

		require.Len(t, targetSocket.writes, 1)
		requireTuple(t, admissionTupleA, targetSocket.writes[0].destination)
		require.Contains(t, (*packetHandlerMap)(f.target).handlers, cid, "routing is retained for the close")

		// A trigger on the failed endpoint's transport is answered by the survivor's.
		f.trigger(t, f.route, zeroCID, nativeProbePeerAddr())
		require.Empty(t, f.route.closeQueue)
		reply := <-(*packetHandlerMap)(f.target).closeQueue
		require.Equal(t, targetSocket.writes[0].data, reply.payload)
		require.Equal(t, admissionTupleA.String(), reply.addr.String())
	})
}

// TestPathCloseFaultWithoutSurvivorDestroys keeps immediate destruction.
func TestPathCloseFaultWithoutSurvivorDestroys(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveClient)
		cid := f.issuedCID(1)
		fault := f.faultPrimary(t)
		writes := len(f.socket.writes)

		f.c.sentFirstPacket = true
		f.c.handleCloseError(&closeError{err: fault, immediate: true})
		f.c.emission.local.Close()
		synctest.Wait()

		require.Len(t, f.socket.writes, writes)
		require.NotContains(t, f.route.handlers, cid)
	})
}

func requireTuple(t *testing.T, want *net.UDPAddr, got netip.AddrPort) {
	t.Helper()
	require.Equal(t, want.String(), netip.AddrPortFrom(got.Addr().Unmap(), got.Port()).String())
}

func nativeProbePeerAddr() *net.UDPAddr {
	return net.UDPAddrFromAddrPort(netip.AddrPortFrom(nativeProbePeer.Addr(), nativeProbePeer.Port()))
}

// newUnconfirmedFixture is an admission fixture's connection before confirmation.
func newUnconfirmedFixture(t *testing.T) *admissionFixture {
	t.Helper()
	return newAdmissionFixtureUnactivated(t)
}

// faultPrimary sends on path zero into a socket failure that faults its endpoint.
func (f *reliableFixture) faultPrimary(t *testing.T) error {
	t.Helper()
	f.socket.result = destinationError(syscall.EBADF)
	f.write(t, 1000)
	synctest.Wait()
	require.NoError(t, f.c.pathCredit.expire(monotime.Now()))
	f.c.triggerSending(monotime.Now())
	synctest.Wait()
	f.socket.result = nil
	fault := f.c.emission.consumeLocal()
	require.ErrorIs(t, fault, errEndpointFault)
	return fault
}

// targetPathFixture is a reliable fixture with path 1 on a second endpoint.
type targetPathFixture struct {
	*reliableFixture
	target *Transport
}

func newTargetPathFixture(t *testing.T) (*targetPathFixture, *nativeProbeSocket) {
	t.Helper()
	admission := newAdmissionFixture(t, protocol.PerspectiveClient)
	admission.supplyPeerCIDs(t)
	stream := reliableStream(t, admission.c)
	targetSocket := newNativeProbeSocket()
	target := &Transport{ConnectionIDLength: admission.tc.srcConnID.Len(), connIDLen: admission.tc.srcConnID.Len(), handlers: make(map[protocol.ConnectionID]packetHandlerEntry), resetTokens: make(map[protocol.StatelessResetToken]packetHandler), logger: utils.DefaultLogger}
	target.initOnce.Do(func() {})
	target.responses = newEndpointResponses(startNativeProbeEndpoint(t, targetSocket))
	errs := admission.openAsync(context.Background(), target, admissionTupleA)
	admission.pass(t)
	var challenge [8]byte
	probe := admission.socket
	admission.socket = targetSocket
	challenge = admissionChallenges(admission.peerWrites(t, 1))[0]
	admission.socket = probe
	require.True(t, admission.receive(t, 1, admissionTupleA, 1200, &wire.PathResponseFrame{Data: challenge}))
	admission.pass(t)
	require.NoError(t, <-errs)
	f := &reliableFixture{admissionFixture: admission, stream: stream}
	f.pass(t)
	targetSocket.writes = nil
	admission.socket.writes = nil
	return &targetPathFixture{reliableFixture: f, target: target}, targetSocket
}

// TestPathPrivateActivationAtCompletion advertises, then activates on a peer that also
// advertised, before the client is confirmed.
func TestPathPrivateActivationAtCompletion(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newAdmissionFixture(t, protocol.PerspectiveClient)
		params := &wire.TransportParameters{}
		f.c.config.multipath = multipathSelection{enabled: true, maxPaths: 2}
		f.c.advertiseMultipath(params)
		require.NotNil(t, params.InitialMaxPathID)
		require.True(t, f.c.negotiatesMultipath(params))
		require.False(t, f.c.negotiatesMultipath(&wire.TransportParameters{}), "an ordinary peer stays ordinary")
		f.c.config.multipath = multipathSelection{}
		f.c.pathSurface = nil
		require.False(t, f.c.negotiatesMultipath(params))
		ordinary := &wire.TransportParameters{}
		f.c.advertiseMultipath(ordinary)
		require.Nil(t, ordinary.InitialMaxPathID, "no advertisement without the selection")

		// Before confirmation, on a fresh connection with its own endpoint route.
		g := newUnconfirmedFixture(t)
		local := packetInfo{addr: netip.MustParseAddr("192.0.2.1"), ifIndex: 3}
		g.c.emission.local.(*nativeOrdinarySender).info = local
		maximum := protocol.PathID(1)
		g.c.config.multipath = multipathSelection{enabled: true, maxPaths: 2}
		g.c.advertiseMultipath(&wire.TransportParameters{})
		g.c.peerParams = &wire.TransportParameters{InitialMaxPathID: &maximum, ActiveConnectionIDLimit: 4}
		require.False(t, g.c.handshakeConfirmed)
		require.NoError(t, g.c.activateMultipath())
		require.NotNil(t, g.c.pathCredit)
		require.True(t, g.c.pathCredit.hasAdmission())
		require.Equal(t, local, g.c.pathCredit.members[0].info, "path zero keeps its route's local source")
		require.NoError(t, creditIngress(t, g.tc, monotime.Now(), &wire.MaxPathIDFrame{MaximumPathID: 1}), "a negotiated control frame before confirmation")

		used := make(chan error, 1)
		go func() {
			used <- g.c.runPathCommand(func(r *pathCreditRegistry, now monotime.Time) error { return r.setLocalUsePrivate(0, false, now) })
		}()
		synctest.Wait()
		require.NoError(t, g.c.pathCredit.serviceRequest(monotime.Now()))
		require.NoError(t, <-used)
	})
}
