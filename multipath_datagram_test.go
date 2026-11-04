//go:build darwin || linux || freebsd

package quic

import (
	"bytes"
	"errors"
	"os"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

// setPathMTU moves a path straight to a discovered size. Only
// TestPathMTUDiscoveryOnSelectedPath needs the real probe exchange.
func (f *reliableFixture) setPathMTU(id protocol.PathID, size protocol.ByteCount) {
	c := f.c
	if id == 0 {
		c.mtuDiscoverer = c.newPathMTUFinder(size, protocol.MaxPacketBufferSize)
		c.maxPayloadSizeEstimate.Store(uint32(estimateMaxPayloadSize(size)))
		return
	}
	c.pathCredit.members[id].route.mtu.init(size, protocol.MaxPacketBufferSize)
	c.pathCredit.notePMTU()
}

func (f *reliableFixture) queueDatagram(t *testing.T, n int) {
	t.Helper()
	require.NoError(t, f.c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: make([]byte, n)}))
}

func reliableDatagrams(writes []reliableWrite) []int {
	var sizes []int
	for _, write := range writes {
		for _, frame := range write.datagrams {
			sizes = append(sizes, len(frame.Data))
		}
	}
	return sizes
}

func TestSelectPathForDatagram(t *testing.T) {
	both := func(fitsZero, readyZero, fitsOne, readyOne bool) []pathView {
		return []pathView{{id: 0, fits: fitsZero, ready: readyZero}, {id: 1, fits: fitsOne, ready: readyOne}}
	}
	for _, tc := range []struct {
		name     string
		views    []pathView
		chosen   protocol.PathID
		steerTo  protocol.PathID
		steering bool
	}{
		{"the primary fits", both(true, true, true, true), 1, 0, false},
		{"another path fits", both(true, true, false, true), 1, 0, true},
		{"the fitting path isn't ready, so the primary keeps sending", both(true, false, false, true), 1, 0, false},
		{"a pending PTO stays first", []pathView{{id: 0, fits: true, ready: true}, {id: 1, probing: true}}, 1, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chosen, steerTo, steering, held := selectPath(tc.views, 1)
			require.False(t, held)
			require.Equal(t, tc.chosen, chosen)
			require.Equal(t, tc.steerTo, steerTo)
			require.Equal(t, tc.steering, steering)
		})
	}
}

func TestSelectPathHoldsWithoutPrimary(t *testing.T) {
	views := []pathView{{id: 0, fits: true, ready: true}, {id: 1, fits: true, ready: true}}
	_, _, _, held := selectPath(views, noPrimary)
	require.True(t, held, "with no primary, a stale path in view carries nothing ordinary")
	_, _, _, held = selectPath(nil, noPrimary)
	require.True(t, held)
	chosen, _, _, held := selectPath([]pathView{{id: 0, suspect: true, probing: true}}, noPrimary)
	require.False(t, held, "a suspect path's pending PTO still goes")
	require.Zero(t, chosen)
}

func TestPathDatagramSmallerMTUSurvivor(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		f.c.peerParams.MaxDatagramFrameSize = wire.MaxDatagramSize
		f.c.pathCredit.selectPrimaryPrivate(1)
		f.setPathMTU(1, 1400)
		f.queueDatagram(t, 1250)
		f.pass(t)
		require.Equal(t, []int{1250}, reliableDatagrams(f.parseWrites(t, 1)), "the selected path's own size carries it")
		f.socket.writes = nil

		require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathAbandonFrame{PathID: 1}))
		f.pass(t)
		f.socket.writes = nil
		require.NoError(t, f.c.SendDatagram(make([]byte, 1000)))
		f.pass(t)
		require.Equal(t, []int{1000}, reliableDatagrams(f.parseWrites(t, 0)))
		require.Zero(t, f.c.datagramQueue.sendDrops.Load())
		for _, write := range f.socket.writes {
			require.LessOrEqual(t, len(write.data), int(protocol.MinInitialPacketSize))
		}
	})
}

// A frame filling a fresh packet can't share one with an ACK. It waits for the next
// packet, for a plain ACK on path zero and a PATH_ACK on the other path alike.
func TestPathDatagramFreshPacketAfterAckPressure(t *testing.T) {
	for _, carrier := range []protocol.PathID{0, 1} {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newReliableFixture(t, protocol.PerspectiveServer)
			f.c.pathCredit.selectPrimaryPrivate(carrier)
			addr := f.c.RemoteAddr()
			if carrier != 0 {
				addr = admissionTupleA
			}
			for range 2 {
				require.True(t, f.receive(t, carrier, addr, 0, &wire.PingFrame{}))
			}
			f.c.emission.selectRoute(monotime.Now())
			room, ok := f.c.emission.packer.datagramRoom(f.c.emission.route, f.c.emission.packetSize())
			require.True(t, ok)
			n := int(room) - int((&wire.DatagramFrame{DataLenPresent: true, Data: make([]byte, room-3)}).Length(protocol.Version1)) + int(room) - 3
			f.queueDatagram(t, n)
			f.pass(t)

			writes := f.parseWrites(t, carrier)
			require.Equal(t, []int{n}, reliableDatagrams(writes), "ACK pressure never discards it")
			require.Zero(t, f.c.datagramQueue.sendDrops.Load())
			var acked bool
			for _, write := range writes {
				if len(write.datagrams) > 0 {
					require.Empty(t, write.acks)
					require.True(t, acked, "the ACK left first")
				}
				acked = acked || len(write.acks) > 0
			}
		})
	}
}

// A message admitted before a smaller path appeared still fits the larger one. It
// leaves there alone: stream data stays on the primary.
func TestPathDatagramAnotherEligiblePathFits(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		f.c.pathCredit.selectPrimaryPrivate(1)
		f.setPathMTU(0, 1400)
		f.queueDatagram(t, 1300)
		f.write(t, 500)
		f.pass(t)

		zero := f.parseWrites(t, 0)
		require.Equal(t, []int{1300}, reliableDatagrams(zero))
		require.Zero(t, reliableBytes(zero), "the steered packet takes no stream data with it")
		one := f.parseWrites(t, 1)
		require.Equal(t, 500, reliableBytes(one))
		require.Empty(t, reliableDatagrams(one))
		require.Zero(t, f.c.datagramQueue.sendDrops.Load())
	})
}

// A congested fitting path holds the message without stalling the primary. Its ACK
// releases it, and the primary's congestion state was never an input.
func TestPathDatagramHeldWhileFittingPathCongested(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		f.setPathMTU(0, 1400)
		f.write(t, 1<<20)
		for range 64 {
			f.pass(t)
			if f.recovery(0).SendMode(monotime.Now()) == ackhandler.SendAck {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		require.Equal(t, ackhandler.SendAck, f.recovery(0).SendMode(monotime.Now()))
		congested := f.parseWrites(t, 0)
		f.socket.writes = nil

		f.c.pathCredit.selectPrimaryPrivate(1)
		f.queueDatagram(t, 1300)
		f.pass(t)
		require.Positive(t, reliableBytes(f.parseWrites(t, 1)), "the primary keeps sending")
		require.NotNil(t, f.c.datagramQueue.Peek(), "and the message is held")
		require.Zero(t, f.c.datagramQueue.sendDrops.Load())
		f.socket.writes = nil

		f.ackPath(t, 0, 0, congested)
		f.pass(t)
		require.Equal(t, []int{1300}, reliableDatagrams(f.parseWrites(t, 0)))
		require.Nil(t, f.c.datagramQueue.Peek())
	})
}

func TestPathDatagramEligibilityLossDropsOnce(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		f.c.pathCredit.selectPrimaryPrivate(1)
		f.setPathMTU(1, 1400)
		f.queueDatagram(t, 1300)
		require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathAbandonFrame{PathID: 1}))
		f.pass(t)
		f.pass(t)
		require.Nil(t, f.c.datagramQueue.Peek(), "no eligible path can carry it, and growth isn't awaited")
		require.EqualValues(t, 1, f.c.datagramQueue.sendDrops.Load())
		require.Empty(t, reliableDatagrams(f.parseWrites(t, 0)))
		require.Empty(t, reliableDatagrams(f.parseWrites(t, 1)))

		f.queueDatagram(t, 1000)
		f.pass(t)
		require.Equal(t, []int{1000}, reliableDatagrams(f.parseWrites(t, 0)), "what follows it still leaves")
		require.EqualValues(t, 1, f.c.datagramQueue.sendDrops.Load())
	})
}

func TestPathDatagramNoRetransmissionOnUncertainSend(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		f.c.pathCredit.selectPrimaryPrivate(1)
		f.socket.result = func([]byte) (int, error) { return 0, os.ErrDeadlineExceeded }
		f.queueDatagram(t, 100)
		f.pass(t)
		synctest.Wait()
		require.NoError(t, f.c.emission.consumeLocal())
		f.socket.result = nil
		require.Equal(t, []int{100}, reliableDatagrams(f.parseWrites(t, 1)))
		require.Nil(t, f.c.datagramQueue.Peek())
		f.socket.writes = nil

		f.expirePTO(t, 1)
		f.pass(t)
		f.pass(t)
		require.Empty(t, reliableDatagrams(f.parseWrites(t, 0)), "single-shot: the probe carries no copy")
		require.Empty(t, reliableDatagrams(f.parseWrites(t, 1)))
		require.NoError(t, f.c.Context().Err(), "local uncertainty is not a connection failure")
	})
}

func TestPathDatagramLegacyObservationUnchanged(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		c := f.c
		settled := func() bool {
			select {
			case <-c.PathMTUSettledV1():
				return true
			default:
				return false
			}
		}
		c.mtuDiscoverer = c.newPathMTUFinder(protocol.MinInitialPacketSize, 1400)
		before := c.maxPayloadSizeEstimate.Load()

		// Another path's concluded search belongs to that path alone.
		member := c.pathCredit.members[1].route.mtu
		require.Nil(t, member.settled)
		require.Nil(t, member.qlogger)
		member.init(1400, 1410)
		require.True(t, member.done())
		c.pathCredit.notePMTU()
		c.pathCredit.selectPrimaryPrivate(1)
		f.pass(t)
		require.False(t, settled())
		require.Equal(t, before, c.maxPayloadSizeEstimate.Load(), "the first path's observation is its own")
		require.EqualValues(t, 1400, c.pathCredit.pmtuObservation(1))
		require.EqualValues(t, protocol.MinInitialPacketSize, c.pathCredit.pmtuObservation(0))

		// The first path's conclusion still settles, on an acknowledged probe's raise.
		c.pathCredit.selectPrimaryPrivate(0)
		f.socket.writes = nil
		f.write(t, 100)
		f.pass(t)
		c.mtuDiscoverer.init(1400, 1410)
		f.ackPath(t, 0, 0, f.parseWrites(t, 0))
		require.True(t, settled())
		require.EqualValues(t, estimateMaxPayloadSize(1400), c.maxPayloadSizeEstimate.Load())
	})
}

func TestPathDatagramSendLimitFollowsEligiblePaths(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		c := f.c
		c.peerParams.MaxDatagramFrameSize = wire.MaxDatagramSize
		requireLimit := func(limit protocol.ByteCount) {
			t.Helper()
			c.emission.selectRoute(monotime.Now())
			var tooLarge *DatagramTooLargeError
			require.True(t, errors.As(c.SendDatagram(make([]byte, limit+1)), &tooLarge))
			require.EqualValues(t, limit, tooLarge.MaxDatagramPayloadSize)
			require.NoError(t, c.SendDatagram(make([]byte, limit)))
			f.pass(t)
			f.socket.writes = nil
		}
		f.setPathMTU(0, 1400)
		requireLimit(estimateMaxPayloadSize(protocol.MinInitialPacketSize))
		f.setPathMTU(1, 1300)
		requireLimit(estimateMaxPayloadSize(1300))
		f.setPathMTU(1, 1450)
		requireLimit(estimateMaxPayloadSize(1400))
		f.setPathMTU(1, 1300)

		require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathAbandonFrame{PathID: 1}))
		f.pass(t)
		requireLimit(estimateMaxPayloadSize(1400))
		require.Zero(t, c.datagramQueue.sendDrops.Load())
	})
}

// With a long CID on every eligible path, the limit is what those paths can carry,
// so an admitted message at the limit is never dropped for its size.
func TestPathDatagramSendLimitReservesLongCID(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		c := f.c
		c.peerParams.MaxDatagramFrameSize = wire.MaxDatagramSize
		long := protocol.ParseConnectionID(bytes.Repeat([]byte{0xc}, protocol.MaxConnIDLen))
		for _, id := range []protocol.PathID{0, 1} {
			set := c.connIDManager.pathCIDs[id]
			record := set.records[set.active]
			record.ConnectionID = long
			set.records[set.active] = record
		}
		c.emission.selectRoute(monotime.Now())
		const limit = 1200 - (1 + protocol.MaxConnIDLen + 4) - 16 - 3
		var tooLarge *DatagramTooLargeError
		require.True(t, errors.As(c.SendDatagram(make([]byte, limit+1)), &tooLarge))
		require.EqualValues(t, limit, tooLarge.MaxDatagramPayloadSize)
		require.NoError(t, c.SendDatagram(make([]byte, limit)))
		f.pass(t)
		require.Nil(t, c.datagramQueue.Peek())
		require.Zero(t, c.datagramQueue.sendDrops.Load(), "it left in a fresh packet")
		require.NotEmpty(t, f.socket.writes)
		for _, write := range f.socket.writes {
			require.LessOrEqual(t, len(write.data), int(protocol.MinInitialPacketSize))
		}
	})
}

func TestPathMTUDiscoveryOnSelectedPath(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		c := f.c
		finder := c.pathCredit.members[1].route.mtu
		require.True(t, finder.NextProbeTime().IsZero(), "the connection's configuration disabled discovery")
		c.pathCredit.selectPrimaryPrivate(1)
		zeroPN, _ := f.recovery(0).PeekPacketNumber(protocol.Encryption1RTT)

		finder.Start(monotime.Now())
		time.Sleep(time.Until(finder.NextProbeTime().ToTime()) + time.Millisecond)
		f.pass(t)
		writes := f.parseWrites(t, 1)
		require.NotEmpty(t, writes)
		probed := protocol.ByteCount(len(writes[0].data))
		require.Greater(t, probed, protocol.ByteCount(protocol.MinInitialPacketSize), "the probe leaves on the selected path, with its identity")
		after, _ := f.recovery(0).PeekPacketNumber(protocol.Encryption1RTT)
		require.Equal(t, zeroPN, after)

		f.ackPath(t, 1, 1, writes[:1])
		require.Equal(t, probed, finder.CurrentSize())
		require.EqualValues(t, probed, c.pathCredit.pmtuObservation(1))

		// The next packet may use the size this path alone proved.
		f.socket.writes = nil
		f.write(t, 4000)
		f.pass(t)
		require.Equal(t, int(probed), len(f.socket.writes[0].data))
	})
}

func TestPathDatagramShortSegmentsFollowRoute(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		c := f.c
		c.emission.local.(*nativeOrdinarySender).nativeGSO = false
		c.emission.shortSegments = true
		c.pathCredit.selectPrimaryPrivate(1)
		f.setPathMTU(1, 1400)
		f.queueDatagram(t, 100)
		f.queueDatagram(t, 100)
		require.Nil(t, c.emission.capacity())
		result := c.emission.advance(monotime.Now(), true)
		require.NoError(t, result.err)
		c.emission.local.finish()
		synctest.Wait()
		require.Equal(t, []int{100, 100}, reliableDatagrams(f.parseWrites(t, 1)), "each short segment carries the route's identity")
		require.Empty(t, f.parseWrites(t, 0))
	})
}
