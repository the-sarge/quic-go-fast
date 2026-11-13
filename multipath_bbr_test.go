//go:build darwin || linux || freebsd

package quic

import (
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
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
