//go:build darwin || linux || freebsd

package quic

import (
	"bytes"
	"context"
	"slices"
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

// reliableFixture has a validated path 1 at admissionTupleA, so every packet
// crosses real protection, real path recovery and the real endpoint.
type reliableFixture struct {
	*admissionFixture
	stream *SendStream
}

func newReliableFixture(t *testing.T, perspective protocol.Perspective) *reliableFixture {
	t.Helper()
	f := newAdmissionFixture(t, perspective)
	f.supplyPeerCIDs(t)
	c := f.c
	c.peerParams = &wire.TransportParameters{InitialMaxData: 1 << 24, InitialMaxStreamDataUni: 1 << 24, InitialMaxStreamDataBidiRemote: 1 << 24, InitialMaxStreamDataBidiLocal: 1 << 24, MaxUniStreamNum: 16, MaxBidiStreamNum: 16}
	c.streamsMap.HandleTransportParameters(c.peerParams)
	c.connFlowController.UpdateSendWindow(1 << 24)
	stream, err := c.OpenUniStream()
	require.NoError(t, err)
	if perspective == protocol.PerspectiveServer {
		require.True(t, f.receive(t, 1, admissionTupleA, 1200, &wire.PingFrame{}))
		f.pass(t)
		challenges := admissionChallenges(f.peerWrites(t, 1))
		require.NotEmpty(t, challenges)
		require.True(t, f.receive(t, 1, admissionTupleA, 1200, &wire.PathResponseFrame{Data: challenges[0]}))
		require.True(t, c.pathCredit.members[1].live)
	} else {
		c.pathCredit.recordValidated(admissionTupleA)
		errs := f.openAsync(context.Background(), nil, admissionTupleA)
		f.pass(t)
		require.NoError(t, <-errs)
	}
	r := &reliableFixture{admissionFixture: f, stream: stream}
	r.pass(t)
	// Path zero's setup control frames are done, so its alarm reflects only the scenario.
	r.ackPath(t, 0, 0, r.parseWrites(t, 0))
	r.pass(t)
	f.socket.writes = nil
	return r
}

// writeAsync queues n bytes, since Write returns only once they are packed. Cleanup
// releases a write that a test's congestion limit deliberately never finishes.
func writeAsync(t *testing.T, stream *SendStream, n int) {
	t.Helper()
	go func() { _, _ = stream.Write(bytes.Repeat([]byte{'r'}, n)) }()
	t.Cleanup(func() {
		stream.CancelWrite(0)
		synctest.Wait()
	})
	synctest.Wait()
}

func (f *reliableFixture) write(t *testing.T, n int) {
	t.Helper()
	writeAsync(t, f.stream, n)
}

// pass also consumes this pass's results, which recovery needs before an ACK or
// loss can act on the packets it just sent.
func (f *reliableFixture) pass(t *testing.T) {
	t.Helper()
	f.admissionFixture.pass(t)
	require.NoError(t, f.c.emission.consumeLocal())
}

func (f *reliableFixture) pathAlarm(id protocol.PathID) monotime.Time {
	return f.c.sentPacketHandler.(interface {
		PathLossDetectionTimeout(protocol.PathID) monotime.Time
	}).PathLossDetectionTimeout(id)
}

// expirePTO runs the aggregated timer until id's PTO fires. Earlier alarms, such as
// an expired path probe, expire nothing the test depends on.
func (f *reliableFixture) expirePTO(t *testing.T, id protocol.PathID) {
	t.Helper()
	for range 8 {
		time.Sleep(time.Until(f.c.sentPacketHandler.GetLossDetectionTimeout().ToTime()) + time.Millisecond)
		outcome, err := f.c.sentPacketHandler.OnLossDetectionTimeout(monotime.Now())
		require.NoError(t, err)
		if outcome.PTO && outcome.PathID == id {
			return
		}
	}
	t.Fatalf("path %d's PTO did not fire", id)
}

func (f *reliableFixture) recovery(id protocol.PathID) ackhandler.SentPacketHandler {
	return f.c.sentPacketHandler.(interface {
		RecoveryOnPath(protocol.PathID) ackhandler.SentPacketHandler
	}).RecoveryOnPath(id)
}

// streamWrites parses each datagram with the peer's keys for id and returns the
// STREAM ranges it carries, alongside the writes' packet numbers.
type reliableWrite struct {
	nativeProbeWrite
	pn      protocol.PacketNumber
	streams []*wire.StreamFrame
	acks    []*wire.AckFrame
	frames  []wire.Frame
	// datagrams holds each DATAGRAM frame's payload as sent.
	datagrams []*wire.DatagramFrame
}

func (f *reliableFixture) parseWrites(t *testing.T, id protocol.PathID) []reliableWrite {
	t.Helper()
	var writes []reliableWrite
	connIDLen := admissionPeerCID.Len()
	if id == 0 {
		connIDLen = f.tc.destConnID.Len()
	}
	for _, write := range f.socket.writes {
		// A segmented write holds equal segments; a lone packet is the whole write.
		segment := len(write.data)
		for offset := 0; offset < len(write.data); offset += segment {
			end := min(offset+segment, len(write.data))
			unpacker := newPacketUnpacker(f.peer, connIDLen)
			pn, _, _, payload, err := unpacker.UnpackShortHeader(id, monotime.Now(), bytes.Clone(write.data[offset:end]))
			if err != nil {
				continue
			}
			parsed := reliableWrite{nativeProbeWrite: write, pn: pn}
			parser := wire.NewFrameParser(true, false, false)
			parser.EnableMultipath()
			for len(payload) > 0 {
				typ, n, err := parser.ParseType(payload, protocol.Encryption1RTT)
				if err != nil {
					break
				}
				payload = payload[n:]
				switch {
				case typ.IsAckFrameType() || typ.IsPathAckFrameType():
					ack, n, err := parser.ParseAckFrame(typ, payload, protocol.Encryption1RTT, protocol.Version1)
					require.NoError(t, err)
					cp := *ack
					parsed.acks = append(parsed.acks, &cp)
					payload = payload[n:]
				case typ.IsStreamFrameType():
					frame, n, err := parser.ParseStreamFrame(typ, payload, protocol.Version1)
					require.NoError(t, err)
					parsed.streams = append(parsed.streams, frame)
					payload = payload[n:]
				case typ.IsDatagramFrameType():
					frame, n, err := parser.ParseDatagramFrame(typ, payload, protocol.Version1)
					require.NoError(t, err)
					parsed.datagrams = append(parsed.datagrams, frame)
					payload = payload[n:]
				default:
					frame, n, err := parser.ParseLessCommonFrame(typ, payload, protocol.Version1)
					require.NoError(t, err)
					parsed.frames = append(parsed.frames, frame)
					payload = payload[n:]
				}
			}
			writes = append(writes, parsed)
		}
	}
	return writes
}

func reliableBytes(writes []reliableWrite) int {
	total := 0
	for _, write := range writes {
		for _, frame := range write.streams {
			total += len(frame.Data)
		}
	}
	return total
}

// ackPath acknowledges every given packet on id, carried by a packet on carrier.
func (f *reliableFixture) ackPath(t *testing.T, id, carrier protocol.PathID, writes []reliableWrite) {
	t.Helper()
	if len(writes) == 0 {
		return
	}
	// Only the sent numbers, since a generator-skipped number must never be acknowledged.
	numbers := make([]protocol.PacketNumber, 0, len(writes))
	for _, write := range writes {
		numbers = append(numbers, write.pn)
	}
	slices.Sort(numbers)
	slices.Reverse(numbers)
	ack := &wire.AckFrame{}
	for _, pn := range numbers {
		if last := len(ack.AckRanges) - 1; last >= 0 && ack.AckRanges[last].Smallest == pn+1 {
			ack.AckRanges[last].Smallest = pn
			continue
		}
		ack.AckRanges = append(ack.AckRanges, wire.AckRange{Smallest: pn, Largest: pn})
	}
	if id != 0 {
		ack.HasPathID, ack.PathID = true, id
	}
	addr := f.c.RemoteAddr()
	if carrier != 0 {
		addr = admissionTupleA
	}
	require.True(t, f.receive(t, carrier, addr, 0, ack))
}

func TestPathReliableTwoPathTransfer(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		f.c.pathCredit.selectPrimaryPrivate(1)
		zeroPN, _ := f.recovery(0).PeekPacketNumber(protocol.Encryption1RTT)
		f.write(t, 4000)
		f.pass(t)

		onOne := f.parseWrites(t, 1)
		require.Equal(t, 4000, reliableBytes(onOne), "every byte leaves through path 1's own keys")
		for _, write := range f.socket.writes {
			require.Equal(t, admissionTupleA.AddrPort().Port(), write.destination.Port(), "the tuple is path 1's")
			require.LessOrEqual(t, len(write.data)%int(protocol.MinInitialPacketSize+1), int(protocol.MinInitialPacketSize))
		}
		after, _ := f.recovery(0).PeekPacketNumber(protocol.Encryption1RTT)
		require.Equal(t, zeroPN, after, "path zero's packet numbers are untouched")
		require.Empty(t, f.parseWrites(t, 0))

		// Acknowledgment on the path samples that path's own RTT and retires its data once.
		f.socket.writes = nil
		time.Sleep(20 * time.Millisecond)
		f.ackPath(t, 1, 1, onOne)
		require.True(t, f.c.pathCredit.rtt[1].HasMeasurement())
		f.pass(t)
		require.False(t, f.recovery(1).QueueProbePacket(protocol.Encryption1RTT), "nothing is outstanding")
		require.False(t, f.c.retransmissionQueue.HasData(protocol.Encryption1RTT))
	})
}

func TestPathReliableOriginalAckAfterCrossPathRetransmission(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		f.c.pathCredit.selectPrimaryPrivate(1)
		f.write(t, 1000)
		f.pass(t)
		original := f.parseWrites(t, 1)
		require.Equal(t, 1000, reliableBytes(original))

		// The first transmission is declared lost on path 1, and the logical range is
		// sent again on path 0 under its own packet number and flight.
		f.socket.writes = nil
		f.c.pathCredit.selectPrimaryPrivate(0)
		require.True(t, f.recovery(1).QueueProbePacket(protocol.Encryption1RTT))
		f.pass(t)
		retransmitted := f.parseWrites(t, 0)
		require.Equal(t, 1000, reliableBytes(retransmitted))

		// The late ACK of the original retires nothing a second time.
		f.socket.writes = nil
		f.ackPath(t, 1, 1, original)
		f.pass(t)
		require.Zero(t, reliableBytes(f.parseWrites(t, 0)), "the logical range is not sent again")
		require.NotZero(t, f.pathAlarm(0), "path 0's flight is untouched by path 1's ACK")

		f.ackPath(t, 0, 0, retransmitted)
		f.pass(t)
		require.Zero(t, f.pathAlarm(0))
		require.False(t, f.recovery(1).QueueProbePacket(protocol.Encryption1RTT))
		require.False(t, f.c.retransmissionQueue.HasData(protocol.Encryption1RTT))
	})
}

func TestPathReliablePartialPackingPreservesWork(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		f.c.pathCredit.selectPrimaryPrivate(1)
		// Revocation between packing and submission returns the whole range.
		f.socket.hold = func([]byte) bool { return true }
		f.write(t, 3000)
		f.c.pathCredit.decline(1, 0, ErrPathClosed)
		f.pass(t)
		f.socket.hold = nil
		f.socket.release()
		f.pass(t)
		f.pass(t)
		require.Equal(t, 3000, reliableBytes(f.parseWrites(t, 0)), "work the path could not send moves to its survivor")
	})
	// A packet smaller than the frame splits it and leaves the rest with the stream.
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		f.c.pathCredit.selectPrimaryPrivate(1)
		f.write(t, 2000)
		require.Nil(t, f.c.emission.capacity())
		route := f.c.emission.route
		require.NotNil(t, route)
		buf := getPacketBuffer()
		defer buf.Release()
		p, err := f.c.emission.packer.appendPacket(route, buf, false, 600, monotime.Now(), protocol.Version1)
		require.NoError(t, err)
		require.Len(t, p.StreamFrames, 1)
		require.Less(t, len(p.StreamFrames[0].Frame.Data), 600)
		require.Equal(t, protocol.PathID(1), p.Context.PathID)
		require.NoError(t, f.c.emission.register(p.Context, monotime.Now(), p.PacketNumber, protocol.InvalidPacketNumber, p.StreamFrames, p.Frames, protocol.Encryption1RTT, protocol.ECNNon, p.Length, false, false))
		f.c.emission.local.finish()
		f.pass(t)
		require.Equal(t, 2000, reliableBytes(f.parseWrites(t, 1)), "the rest stays with its stream, and the disposed part returns to it")
	})
}

func TestPathReliableGSOContextsDoNotBatch(t *testing.T) {
	// A segmented batch stays on its selected route, at that route's size. The
	// endpoint writes each segment here, so each one can be checked on its own.
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		c := f.c
		c.emission.local.(*nativeOrdinarySender).nativeGSO = false
		c.pathCredit.selectPrimaryPrivate(1)
		f.write(t, 3000)
		require.Nil(t, c.emission.capacity())
		result := c.emission.advance(monotime.Now(), true)
		require.NoError(t, result.err)
		c.emission.local.finish()
		synctest.Wait()
		require.Equal(t, 3000, reliableBytes(f.parseWrites(t, 1)))
		require.GreaterOrEqual(t, len(f.socket.writes), 3)
		for _, write := range f.socket.writes {
			require.Equal(t, admissionTupleA.AddrPort().Port(), write.destination.Port())
			require.LessOrEqual(t, len(write.data), int(protocol.MinInitialPacketSize))
		}

		// The next opportunity binds path zero, whose packets never join that batch.
		f.socket.writes = nil
		c.pathCredit.selectPrimaryPrivate(0)
		f.write(t, 1000)
		f.pass(t)
		zero := f.parseWrites(t, 0)
		require.Equal(t, 1000, reliableBytes(zero))
		for _, write := range zero {
			require.Equal(t, nativeProbePeer.Port(), write.destination.Port())
		}
		require.Empty(t, f.parseWrites(t, 1))
	})
	// A tuple's own EIO fallback disables batching on that route alone.
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		f.c.pathCredit.selectPrimaryPrivate(1)
		f.write(t, 3000)
		require.Nil(t, f.c.emission.capacity())
		f.c.emission.route.writeState.gsoDisabled.Store(true)
		result := f.c.emission.advance(monotime.Now(), true)
		require.NoError(t, result.err)
		f.c.emission.local.finish()
		synctest.Wait()
		require.GreaterOrEqual(t, len(f.socket.writes), 3, "each packet is its own write")
	})
}

func TestPathReliablePrimaryCongestionDoesNotMoveTraffic(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		f.c.pathCredit.selectPrimaryPrivate(1)
		f.write(t, 1<<20)
		for range 64 {
			f.pass(t)
			if mode := f.recovery(1).SendMode(monotime.Now()); mode == ackhandler.SendAck {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		require.Equal(t, ackhandler.SendAck, f.recovery(1).SendMode(monotime.Now()))
		require.Equal(t, ackhandler.SendAny, f.recovery(0).SendMode(monotime.Now()), "the alternate has room")
		f.socket.writes = nil
		f.pass(t)
		require.Zero(t, reliableBytes(f.parseWrites(t, 0)), "a full window is never path failure")
		require.Zero(t, reliableBytes(f.parseWrites(t, 1)))
	})
}

func TestPathReliablePTOOnOtherPath(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		f.c.pathCredit.selectPrimaryPrivate(1)
		f.write(t, 1000)
		f.pass(t)
		require.Equal(t, 1000, reliableBytes(f.parseWrites(t, 1)))
		f.c.pathCredit.selectPrimaryPrivate(0)
		f.socket.writes = nil

		f.expirePTO(t, 1)
		f.pass(t)
		onOne := f.parseWrites(t, 1)
		require.Equal(t, 1000, reliableBytes(onOne), "the probe carries the data on the path whose PTO fired")
		require.NotEqual(t, ackhandler.SendPTOAppData, f.recovery(1).SendMode(monotime.Now()), "its obligation clears there")
	})
}

func TestPathReliableAdmissionProbeUnderSendAck(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newAdmissionFixture(t, protocol.PerspectiveServer)
		f.supplyPeerCIDs(t)
		c := f.c
		c.peerParams = &wire.TransportParameters{InitialMaxData: 1 << 24, InitialMaxStreamDataUni: 1 << 24, MaxUniStreamNum: 16}
		c.streamsMap.HandleTransportParameters(c.peerParams)
		c.connFlowController.UpdateSendWindow(1 << 24)
		stream, err := c.OpenUniStream()
		require.NoError(t, err)
		writeAsync(t, stream, 1<<20)
		for range 64 {
			f.pass(t)
			if c.sentPacketHandler.SendMode(monotime.Now()) == ackhandler.SendAck {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		require.Equal(t, ackhandler.SendAck, c.sentPacketHandler.SendMode(monotime.Now()))
		f.socket.writes = nil
		require.True(t, f.receive(t, 1, admissionTupleA, 1200, &wire.PingFrame{}))
		f.pass(t)
		require.NotEmpty(t, admissionChallenges(f.peerWrites(t, 1)), "a held path zero does not starve validation")
	})
}

func TestPathReliableTerminalReschedule(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		f.c.pathCredit.selectPrimaryPrivate(1)
		f.write(t, 1000)
		f.pass(t)
		original := f.parseWrites(t, 1)
		require.Equal(t, 1000, reliableBytes(original))
		f.socket.writes = nil

		require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathAbandonFrame{PathID: 1}))
		f.pass(t)
		f.pass(t)
		survivor := f.parseWrites(t, 0)
		require.Equal(t, 1000, reliableBytes(survivor), "accepted work returns to its owner and leaves on the survivor")
		require.Empty(t, f.parseWrites(t, 1), "nothing follows the abandonment on the retired path")

		// Evidence stays retained: the late ACK changes nothing and fails nothing.
		f.socket.writes = nil
		f.ackPath(t, 1, 0, original)
		f.pass(t)
		require.Zero(t, reliableBytes(f.parseWrites(t, 0)))
	})
}

func TestPathReliableTargetTransportRoute(t *testing.T) {
	newTarget := func(t *testing.T, f *admissionFixture) (*Transport, *nativeProbeSocket, *nativeEndpoint) {
		socket := newNativeProbeSocket()
		endpoint := startNativeProbeEndpoint(t, socket)
		target := &Transport{connIDLen: f.tc.srcConnID.Len(), handlers: make(map[protocol.ConnectionID]packetHandlerEntry), resetTokens: make(map[protocol.StatelessResetToken]packetHandler), logger: utils.DefaultLogger}
		target.responses = newEndpointResponses(endpoint)
		return target, socket, endpoint
	}
	open := func(t *testing.T, f *admissionFixture, target *Transport, socket *nativeProbeSocket) {
		errs := f.openAsync(context.Background(), target, admissionTupleA)
		f.pass(t)
		require.Len(t, socket.writes, 1)
		unpacker := newPacketUnpacker(f.peer, admissionPeerCID.Len())
		_, _, _, payload, err := unpacker.UnpackShortHeader(1, monotime.Now(), bytes.Clone(socket.writes[0].data))
		require.NoError(t, err)
		parser := wire.NewFrameParser(true, false, false)
		parser.EnableMultipath()
		var challenge *wire.PathChallengeFrame
		for len(payload) > 0 && challenge == nil {
			typ, n, err := parser.ParseType(payload, protocol.Encryption1RTT)
			require.NoError(t, err)
			payload = payload[n:]
			frame, n, err := parser.ParseLessCommonFrame(typ, payload, protocol.Version1)
			require.NoError(t, err)
			payload = payload[n:]
			challenge, _ = frame.(*wire.PathChallengeFrame)
		}
		require.NotNil(t, challenge)
		require.True(t, f.receive(t, 1, admissionTupleA, 1200, &wire.PathResponseFrame{Data: challenge.Data}))
		require.NoError(t, <-errs)
		require.True(t, f.c.pathCredit.members[1].live)
		socket.writes = nil
	}
	stream := func(t *testing.T, f *admissionFixture) *SendStream {
		c := f.c
		c.peerParams = &wire.TransportParameters{InitialMaxData: 1 << 24, InitialMaxStreamDataUni: 1 << 24, MaxUniStreamNum: 16}
		c.streamsMap.HandleTransportParameters(c.peerParams)
		c.connFlowController.UpdateSendWindow(1 << 24)
		s, err := c.OpenUniStream()
		require.NoError(t, err)
		return s
	}
	t.Run("data leaves through the target and its result is consumed", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			target, socket, _ := newTarget(t, f)
			t.Cleanup(socket.release)
			open(t, f, target, socket)
			f.c.pathCredit.selectPrimaryPrivate(1)
			s := stream(t, f)
			f.socket.writes = nil
			writeAsync(t, s, 3000)
			f.pass(t)
			require.GreaterOrEqual(t, len(socket.writes), 3, "a target route writes through its own endpoint")
			require.Empty(t, f.socket.writes)
			f.pass(t)
			for _, record := range f.c.emission.local.(*nativeOrdinarySender).records {
				require.Zero(t, record.admission.serial, "each target result reconciles its record")
			}
		})
	})
	t.Run("an idle target fault leaves the healthy path sending", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			target, socket, endpoint := newTarget(t, f)
			t.Cleanup(socket.release)
			open(t, f, target, socket)
			f.c.pathCredit.selectPrimaryPrivate(1)
			s := stream(t, f)
			writeAsync(t, s, 1000)
			f.pass(t)
			require.NotEmpty(t, socket.writes)
			f.socket.writes = nil

			endpoint.do(func(state *endpointState) { state.fail(endpointFaultCause(0), errEndpointFault) })
			synctest.Wait()
			select {
			case <-f.c.emission.localWakeup():
			default:
				t.Fatal("the shared completion channel must wake the connection")
			}
			f.pass(t)
			f.pass(t)
			require.Equal(t, 1000, reliableBytes((&reliableFixture{admissionFixture: f}).parseWrites(t, 0)), "its work moves to the healthy path")
			require.False(t, f.c.pathCredit.members[1].established.probeEpoch.revoked.Load(), "quarantine is not abandonment")
		})
	})
	t.Run("a fault with outstanding target requests stays on that endpoint", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			target, socket, endpoint := newTarget(t, f)
			t.Cleanup(socket.release)
			open(t, f, target, socket)
			f.c.pathCredit.selectPrimaryPrivate(1)
			s := stream(t, f)
			socket.hold = func([]byte) bool { return true }
			writeAsync(t, s, 3000)
			f.c.emission.local.(*nativeOrdinarySender).nativeGSO = false
			f.pass(t)
			f.socket.writes = nil

			// One request is current and the rest queued, so each reports the fault.
			endpoint.do(func(state *endpointState) { state.fail(endpointFaultCause(0), errEndpointFault) })
			socket.hold = nil
			socket.release()
			f.pass(t)
			require.NoError(t, f.c.emission.consumeLocal(), "a target's fault doesn't end the connection")
			f.pass(t)
			require.Equal(t, 3000, reliableBytes((&reliableFixture{admissionFixture: f}).parseWrites(t, 0)), "every byte continues on the healthy endpoint")
			for _, record := range f.c.emission.local.(*nativeOrdinarySender).records {
				require.Zero(t, record.admission.serial, "each fault result reconciles once")
			}
		})
	})
	t.Run("an ordinary connection keeps every record after attaching a target", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			_, sender, _ := nativeProbeRecoveryConnection(t, "reno")
			sender.targets = append(sender.targets, sender.participant)
			for index := range sender.records {
				sender.records[index].endpoint = sender.endpoint
				sender.records[index].admission.serial = uint64(index + 1)
			}
			require.False(t, sender.routeSaturated(sender.endpoint))
			sender.targets = nil
			clear(sender.records[:])
		})
	})
	t.Run("a saturated fitting endpoint holds the DATAGRAM, not the chosen route", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			target, socket, _ := newTarget(t, f)
			t.Cleanup(socket.release)
			open(t, f, target, socket)
			c, r := f.c, &reliableFixture{admissionFixture: f}
			s := stream(t, f)
			c.emission.local.(*nativeOrdinarySender).nativeGSO = false
			r.setPathMTU(1, 1400)

			c.pathCredit.selectPrimaryPrivate(1)
			socket.hold = func([]byte) bool { return true }
			writeAsync(t, s, 1<<16)
			for range 8 {
				r.pass(t)
				if c.emission.capacity() != nil {
					break
				}
			}
			require.NotNil(t, c.emission.capacity(), "the target is saturated")

			c.pathCredit.selectPrimaryPrivate(0)
			r.queueDatagram(t, 1300)
			f.socket.writes = nil
			require.Nil(t, c.emission.capacity(), "the fitting endpoint's capacity never blocks the chosen route")
			require.Nil(t, c.emission.route)
			r.pass(t)
			zero := r.parseWrites(t, 0)
			require.Positive(t, reliableBytes(zero), "the primary keeps sending")
			require.Empty(t, reliableDatagrams(zero))
			require.NotNil(t, c.datagramQueue.Peek(), "and the message waits")
			require.Zero(t, c.datagramQueue.sendDrops.Load())

			socket.hold = nil
			socket.writes = nil
			socket.release()
			synctest.Wait()
			select {
			case <-c.emission.localWakeup():
			default:
				t.Fatal("the fitting endpoint's completion wakes the connection")
			}
			r.pass(t)
			require.Nil(t, c.datagramQueue.Peek())
			require.Zero(t, c.datagramQueue.sendDrops.Load())
			r.socket = socket
			require.Equal(t, []int{1300}, reliableDatagrams(r.parseWrites(t, 1)))
		})
	})
	t.Run("a saturated previous endpoint does not hold a pending PTO back", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			target, socket, _ := newTarget(t, f)
			t.Cleanup(socket.release)
			open(t, f, target, socket)
			c, r := f.c, &reliableFixture{admissionFixture: f}
			s := stream(t, f)
			sender := c.emission.local.(*nativeOrdinarySender)
			sender.nativeGSO = false

			// Path zero gets accepted work, so its PTO can fire later.
			writeAsync(t, s, 1000)
			r.pass(t)
			require.Equal(t, 1000, reliableBytes(r.parseWrites(t, 0)))
			f.socket.writes = nil

			// The held target takes every record it may, and leaves one for another endpoint.
			c.pathCredit.selectPrimaryPrivate(1)
			socket.hold = func([]byte) bool { return true }
			writeAsync(t, s, 1<<16)
			for range 8 {
				r.pass(t)
				if c.emission.capacity() != nil {
					break
				}
			}
			require.NotNil(t, c.emission.capacity(), "the target is saturated")
			require.NotEmpty(t, socket.writes)

			r.expirePTO(t, 0)
			require.Nil(t, c.emission.capacity(), "selection precedes the capacity read")
			require.Nil(t, c.emission.route)
			require.NotNil(t, sender.pending)
			require.Same(t, sender.endpoint, sender.pending.endpoint, "the reservation is on the selected path's endpoint")
			r.pass(t)
			require.NotEmpty(t, r.parseWrites(t, 0), "the probe leaves through the healthy endpoint before the target clears")
			socket.hold = nil
			socket.release()
			synctest.Wait()
		})
	})
}

// Each finder runs while its own path is selected. An idle one arms no deadline, so
// it can't busy-wake the loop.
func TestPathReliableMTUTimerFollowsRoute(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		c := f.c
		c.mtuDiscoverer = c.newPathMTUFinder(protocol.MinInitialPacketSize, 1400)
		c.mtuDiscoverer.Start(monotime.Now())
		member := c.pathCredit.members[1].route.mtu
		c.pathCredit.selectPrimaryPrivate(1)
		c.emission.selectRoute(monotime.Now())
		require.Nil(t, c.prepareEmission(monotime.Now().Add(time.Hour)).mtu, "path zero's finder and the unstarted member's both stay quiet")
		require.True(t, c.mtuProbeTime().IsZero(), "and arm no deadline")

		member.Start(monotime.Now())
		require.False(t, c.mtuProbeTime().IsZero(), "the selected path's own finder arms it")
		require.Same(t, member, c.prepareEmission(monotime.Now().Add(time.Hour)).mtu)

		c.pathCredit.selectPrimaryPrivate(0)
		c.emission.selectRoute(monotime.Now())
		require.Same(t, c.mtuDiscoverer, c.prepareEmission(monotime.Now().Add(time.Hour)).mtu, "path zero resumes its own")
	})
}

func TestPathReliableRetentionUsesLargestPTO(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		before := f.c.observedRTT().PTO(true)
		f.c.pathCredit.rtt[1].UpdateRTT(2*time.Second, 0)
		large := f.c.pathCredit.rtt[1].PTO(true)
		require.Greater(t, large, before)
		now := monotime.Now()
		require.NoError(t, f.c.pathCredit.abandon(1, now))
		require.Equal(t, pathDeadline(now, threePathPTO(large)), f.c.pathCredit.terminals[1].expiry)
	})
}

func TestPathReliableClientOpenPathCarriesData(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveClient)
		f.c.pathCredit.selectPrimaryPrivate(1)
		f.write(t, 1000)
		f.pass(t)
		require.Equal(t, 1000, reliableBytes(f.parseWrites(t, 1)))
		require.Empty(t, f.parseWrites(t, 0))
	})
}
