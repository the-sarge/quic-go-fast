package quic

import (
	"context"
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

// TestPathCloseEveryLivePath sends each path its own packet, on its own tuple.
func TestPathCloseEveryLivePath(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveClient)
		f.socket.writes = nil

		f.closeLocally(t, "both")

		require.Len(t, f.socket.writes, 2)
		zero, one := f.closeWrites(t, 0), f.closeWrites(t, 1)
		require.Len(t, zero, 1)
		require.Len(t, one, 1)
		require.Equal(t, nativeProbePeer, zero[0].destination)
		requireTuple(t, admissionTupleA, one[0].destination)
		require.NotEqual(t, zero[0].data, one[0].data)
	})
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

		// The third trigger falls to the exponential backoff.
		f.trigger(t, f.route, oneCID, stranger)
		require.Empty(t, f.route.closeQueue)
	})
}

// TestPathCloseTriggerOnRetiredPath answers through the survivor: an abandoned
// context never carries a cached reply.
func TestPathCloseTriggerOnRetiredPath(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveClient)
		f.route.closeQueue = make(chan closePacket, 4)
		zeroCID := f.c.connIDGenerator.activeSrcConnIDs[0]
		require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathAbandonFrame{PathID: 0}))
		f.pass(t)
		f.pass(t)
		f.socket.writes = nil
		f.closeLocally(t, "retired")

		f.trigger(t, f.route, zeroCID, nativeProbePeerAddr())
		reply := <-f.route.closeQueue
		require.Equal(t, admissionTupleA.String(), reply.addr.String())
		require.Equal(t, f.closeWrites(t, 1)[0].data, reply.payload)
	})
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
		start := time.Now()
		for time.Since(start) < 3*largest-time.Millisecond {
			time.Sleep(largest / 2)
			if time.Since(start) < 3*largest-time.Millisecond {
				f.trigger(t, f.route, oneCID, nativeProbePeerAddr())
			}
		}
		require.Contains(t, f.route.handlers, oneCID, "still retained just before expiry")
		time.Sleep(2 * time.Millisecond)
		synctest.Wait()
		require.NotContains(t, f.route.handlers, oneCID, "traffic did not extend the timer")
	})
}

// TestPathCloseFaultedPrimaryUsesSurvivor closes through a healthy second endpoint.
func TestPathCloseFaultedPrimaryUsesSurvivor(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f, targetSocket := newTargetPathFixture(t)
		cid := f.issuedCID(1)
		fault := f.faultPrimary(t)

		f.c.sentFirstPacket = true
		f.c.handleCloseError(&closeError{err: fault, immediate: true})
		f.c.emission.local.Close()
		synctest.Wait()

		require.Len(t, targetSocket.writes, 1)
		requireTuple(t, admissionTupleA, targetSocket.writes[0].destination)
		require.Contains(t, (*packetHandlerMap)(f.target).handlers, cid, "routing is retained for the close")
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
	target := &Transport{connIDLen: admission.tc.srcConnID.Len(), handlers: make(map[protocol.ConnectionID]packetHandlerEntry), resetTokens: make(map[protocol.StatelessResetToken]packetHandler), logger: utils.DefaultLogger}
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
