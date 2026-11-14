//go:build darwin || linux || freebsd

package quic

import (
	"bytes"
	"context"
	"math"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/stretchr/testify/require"
)

// reliableFixture has a validated path 1 at admissionTupleA, so every packet
// crosses real protection, real path recovery and the real endpoint.
type reliableFixture struct {
	*admissionFixture
	stream *SendStream
}

func reliableStream(t *testing.T, c *Conn) *SendStream {
	t.Helper()
	c.peerParams = &wire.TransportParameters{InitialMaxData: 1 << 24, InitialMaxStreamDataUni: 1 << 24, InitialMaxStreamDataBidiRemote: 1 << 24, InitialMaxStreamDataBidiLocal: 1 << 24, MaxUniStreamNum: 16, MaxBidiStreamNum: 16}
	c.streamsMap.HandleTransportParameters(c.peerParams)
	c.connFlowController.UpdateSendWindow(1 << 24)
	stream, err := c.OpenUniStream()
	require.NoError(t, err)
	return stream
}

func newReliableFixture(t *testing.T, perspective protocol.Perspective) *reliableFixture {
	t.Helper()
	return newReliableFixtureWith(t, perspective, nil)
}

func newReliableFixtureWith(t *testing.T, perspective protocol.Perspective, tracer func(context.Context, bool, ConnectionID) qlogwriter.Trace) *reliableFixture {
	t.Helper()
	return newReliableFixtureFor(t, perspective, tracer, "reno")
}

func newReliableFixtureFor(t *testing.T, perspective protocol.Perspective, tracer func(context.Context, bool, ConnectionID) qlogwriter.Trace, controller string) *reliableFixture {
	t.Helper()
	f := buildAdmissionFixtureFor(t, perspective, tracer, false, controller)
	f.supplyPeerCIDs(t)
	c := f.c
	stream := reliableStream(t, c)
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

// loopPass follows the run loop's order, in which selection runs in capacity and
// again in send.
func (f *reliableFixture) loopPass(t *testing.T) {
	t.Helper()
	for {
		synctest.Wait()
		require.NoError(t, f.c.emission.consumeLocal())
		if f.c.emission.capacity() != nil {
			break
		}
		result := f.c.triggerSending(monotime.Now())
		require.NoError(t, result.err)
		if !result.retry {
			break
		}
	}
	synctest.Wait()
	require.NoError(t, f.c.emission.consumeLocal())
}

// contendedEndpoint fills an endpoint's data class with other connections' small
// reservations, so returning one gives whoever holds the turn exactly one slot.
type contendedEndpoint struct {
	endpoint *nativeEndpoint
	slots    []endpointAdmission
	nextID   uint64
}

func newContendedEndpoint(t *testing.T, endpoint *nativeEndpoint, firstID uint64) *contendedEndpoint {
	t.Helper()
	e := &contendedEndpoint{endpoint: endpoint, nextID: firstID}
	for {
		other := e.attach(t)
		admission, err := e.reserve(other)
		if err != nil {
			require.ErrorIs(t, err, errEndpointCapacity)
			endpoint.cancelReadiness(other, 0)
			break
		}
		e.slots = append(e.slots, admission)
	}
	synctest.Wait()
	return e
}

func (e *contendedEndpoint) attach(t *testing.T) *endpointParticipant {
	t.Helper()
	other, err := e.endpoint.attach(e.nextID)
	require.NoError(t, err)
	e.nextID++
	return other
}

func (e *contendedEndpoint) reserve(p *endpointParticipant) (admission endpointAdmission, err error) {
	e.endpoint.do(func(state *endpointState) { admission, err = state.admit(p, 0, 64) })
	return admission, err
}

func (e *contendedEndpoint) freeSlot() {
	admission := e.slots[0]
	e.slots = e.slots[1:]
	e.endpoint.cancelAdmission(admission)
	synctest.Wait()
}

// rival is another connection waiting behind whoever holds the turn.
func (e *contendedEndpoint) rival(t *testing.T) *endpointParticipant {
	t.Helper()
	rival := e.attach(t)
	_, err := e.reserve(rival)
	require.ErrorIs(t, err, errEndpointCapacity)
	synctest.Wait()
	return rival
}

func (e *contendedEndpoint) state(p *endpointParticipant) (pending, granted, leased bool) {
	e.endpoint.do(func(state *endpointState) {
		pending = p.wait[0].pending
		granted = state.grant != nil && state.grant.participant == p
		leased = granted && !state.grant.expires.IsZero()
	})
	return pending, granted, leased
}

// lease begins p's lease, once p holds the turn.
func (e *contendedEndpoint) lease(t *testing.T, p *endpointParticipant) {
	t.Helper()
	_, granted, leased := e.state(p)
	require.True(t, granted, "the participant holds the turn")
	if !leased {
		e.freeSlot()
		_, _, leased = e.state(p)
	}
	require.True(t, leased, "the participant's lease has begun")
}

// serve gives each rival its turn, which fills the slot it was leased.
func (e *contendedEndpoint) serve(t *testing.T, rivals ...*endpointParticipant) {
	t.Helper()
	for _, rival := range rivals {
		e.lease(t, rival)
		admission, err := e.reserve(rival)
		require.NoError(t, err)
		e.slots = append(e.slots, admission)
		synctest.Wait()
	}
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
		p, err := f.c.emission.packer.appendPacket(route, ackhandler.AckCarriage{}, buf, false, 600, monotime.Now(), protocol.Version1)
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

// newTargetTransport is a Transport on its own native endpoint and socket, for a
// path whose route targets an independent endpoint.
func newTargetTransport(t *testing.T, f *admissionFixture) (*Transport, *nativeProbeSocket, *nativeEndpoint) {
	t.Helper()
	socket := newNativeProbeSocket()
	endpoint := startNativeProbeEndpoint(t, socket)
	target := &Transport{ConnectionIDLength: f.tc.srcConnID.Len(), connIDLen: f.tc.srcConnID.Len(), handlers: make(map[protocol.ConnectionID]packetHandlerEntry), resetTokens: make(map[protocol.StatelessResetToken]packetHandler), logger: utils.DefaultLogger}
	target.initOnce.Do(func() {})
	target.responses = newEndpointResponses(endpoint)
	return target, socket, endpoint
}

// openOnTarget opens and validates path 1 at admissionTupleA through target.
func openOnTarget(t *testing.T, f *admissionFixture, target *Transport, socket *nativeProbeSocket) {
	t.Helper()
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

// targetStream opens a stream with ample flow control on an admission fixture.
func targetStream(t *testing.T, f *admissionFixture) *SendStream {
	t.Helper()
	c := f.c
	c.peerParams = &wire.TransportParameters{InitialMaxData: 1 << 24, InitialMaxStreamDataUni: 1 << 24, MaxUniStreamNum: 16}
	c.streamsMap.HandleTransportParameters(c.peerParams)
	c.connFlowController.UpdateSendWindow(1 << 24)
	s, err := c.OpenUniStream()
	require.NoError(t, err)
	return s
}

func TestPathReliableTargetTransportRoute(t *testing.T) {
	newTarget, open, stream := newTargetTransport, openOnTarget, targetStream
	waiting := func(endpoint *nativeEndpoint, participant *endpointParticipant) (pending, granted bool) {
		endpoint.do(func(state *endpointState) {
			pending = participant.wait[0].pending
			granted = state.grant != nil && state.grant.participant == participant
		})
		return pending, granted
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
				require.Equal(t, ordinaryFree, record.state, "each target result reconciles its record")
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
				require.Equal(t, ordinaryFree, record.state, "each fault result reconciles once")
			}
		})
	})
	t.Run("an ordinary connection keeps every record after attaching a target", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			_, sender, _ := nativeProbeRecoveryConnection(t, "reno")
			sender.targets = append(sender.targets, sender.participant)
			for index := range sender.records {
				sender.records[index].endpoint = sender.endpoint
				sender.records[index].state = ordinaryEndpointOwned
			}
			require.False(t, sender.routeSaturated(sender.endpoint))
			sender.targets = nil
			clear(sender.records[:])
		})
	})
	t.Run("buffer saturation rejects steering before primary capacity", func(t *testing.T) {
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
			r.setPathMTU(1, 1400)
			c.pathCredit.selectPrimaryPrivate(1)
			// Real ACKs grow the target's congestion allowance beyond 32 packets.
			// This makes the buffer bound reachable through ordinary emission.
			writeAsync(t, s, 64<<10)
			warmed := 0
			for range 16 {
				r.pass(t)
				original := f.socket
				f.socket = socket
				writes := r.parseWrites(t, 1)
				f.socket = original
				warmed += reliableBytes(writes)
				time.Sleep(time.Millisecond)
				r.ackPath(t, 1, 0, writes)
				socket.writes = nil
				time.Sleep(10 * time.Millisecond)
				if warmed == 64<<10 {
					break
				}
			}
			require.Equal(t, 64<<10, warmed)
			socket.hold = nativeProbeHoldAll
			writeAsync(t, s, 128<<10)
			r.pass(t)
			buffers, records := 0, 0
			for i := range sender.records {
				record := &sender.records[i]
				if record.state != ordinaryFree && record.identity.path == 1 {
					buffers += len(record.members)
					records++
				}
			}
			require.Equal(t, 32, buffers, "real target members reach the existing buffer allowance")
			require.Less(t, records, 3, "buffer pressure is independent of the spare-record bound")
			c.pathCredit.selectPrimaryPrivate(0)
			r.queueDatagram(t, 1300)
			f.socket.writes = nil
			require.Nil(t, c.emission.capacity(), "an exhausted fitting candidate cannot block primary capacity")
			r.pass(t)
			require.Positive(t, reliableBytes(r.parseWrites(t, 0)))
			require.NotNil(t, c.datagramQueue.Peek(), "the frozen choice does not destructively extract the waiting DATAGRAM")
			require.Zero(t, c.datagramQueue.sendDrops.Load())
			socket.hold = nil
			socket.release()
			synctest.Wait()
			require.NoError(t, sender.consume())
			r.pass(t)
			require.Nil(t, c.datagramQueue.Peek(), "the DATAGRAM progresses once the fitting route has buffer room")
			for i := range sender.records {
				require.Equal(t, ordinaryFree, sender.records[i].state)
			}
		})
	})
	t.Run("a saturated fitting endpoint holds the DATAGRAM, not the chosen route", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			target, socket, endpoint := newTarget(t, f)
			t.Cleanup(socket.release)
			open(t, f, target, socket)
			c, r := f.c, &reliableFixture{admissionFixture: f}
			s := stream(t, f)
			c.emission.local.(*nativeOrdinarySender).nativeGSO = false
			r.setPathMTU(1, 1400)

			c.pathCredit.selectPrimaryPrivate(1)
			socket.hold = func([]byte) bool { return true }
			writeAsync(t, s, 1<<16)
			// A pending receive ends each natural turn after one member, so this
			// fixture reaches the record bound independently of grouping depth.
			c.receivedPackets.PushBack(receivedPacket{})
			for range 8 {
				r.pass(t)
				if c.emission.capacity() != nil {
					break
				}
			}
			c.receivedPackets.PopFront()
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
			pending, _ := waiting(endpoint, c.emission.local.(*nativeOrdinarySender).probeParticipant(endpoint))
			require.False(t, pending, "its own completions wake it, so it holds no place at the endpoint")

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
	// Other connections fill the fitting endpoint's data class, so no result of this
	// connection's can wake it there. The socket holds them all until released.
	saturated := func(t *testing.T) (*admissionFixture, *reliableFixture, *nativeOrdinarySender, *nativeProbeSocket, *nativeEndpoint, *endpointParticipant, []*endpointParticipant) {
		f := newAdmissionFixture(t, protocol.PerspectiveClient)
		f.supplyPeerCIDs(t)
		target, socket, endpoint := newTarget(t, f)
		t.Cleanup(socket.release)
		open(t, f, target, socket)
		r := &reliableFixture{admissionFixture: f}
		sender := f.c.emission.local.(*nativeOrdinarySender)
		sender.nativeGSO = false
		r.setPathMTU(1, 1400)
		f.c.pathCredit.selectPrimaryPrivate(0)
		participant := sender.probeParticipant(endpoint)
		require.NotNil(t, participant)

		socket.hold = func([]byte) bool { return true }
		var others []*endpointParticipant
		for id := uint64(1 << 40); ; id++ {
			other, err := endpoint.attach(id)
			require.NoError(t, err)
			others = append(others, other)
			request := endpointTestRequest(id, "other")
			err = endpoint.offer(other, 0, request)
			if err != nil {
				require.ErrorIs(t, err, errEndpointCapacity)
				endpoint.cancelReadiness(other, 0)
				for _, member := range request.members {
					member.buffer.Release()
				}
				break
			}
		}
		synctest.Wait()
		return f, r, sender, socket, endpoint, participant, others
	}
	t.Run("another connection's saturation stages the frozen DATAGRAM and wakes it", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f, r, sender, socket, endpoint, participant, _ := saturated(t)
			r.queueDatagram(t, 1300)
			require.Nil(t, f.c.emission.capacity())
			require.True(t, f.c.emission.steered, "local construction selects the fitting route before admission")
			pending, _ := waiting(endpoint, participant)
			require.False(t, pending, "readiness cannot advertise an unconstructed frame")
			r.pass(t)
			require.Nil(t, f.c.datagramQueue.Peek(), "the exact request now owns the extracted DATAGRAM")
			head := sender.stagedHead(participant)
			require.NotNil(t, head)
			require.EqualValues(t, 1, head.identity.path)
			pending, _ = waiting(endpoint, participant)
			require.True(t, pending, "the frozen head owns persistent exact demand")
			socket.hold = nil
			socket.release()
			synctest.Wait()
			socket.writes = nil
			r.pass(t)
			r.pass(t)
			r.socket = socket
			require.Equal(t, []int{1300}, reliableDatagrams(r.parseWrites(t, 1)))
			require.Zero(t, sender.droppedDatagrams)
		})
	})
	t.Run("selection changes only the unconstructed local builder", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f, r, sender, socket, endpoint, participant, _ := saturated(t)
			r.queueDatagram(t, 1300)
			require.Nil(t, f.c.emission.capacity())
			require.Same(t, endpoint, sender.pending.endpoint)
			r.setPathMTU(0, 1400)
			require.Nil(t, f.c.emission.capacity())
			require.Same(t, sender.endpoint, sender.pending.endpoint)
			require.NotNil(t, f.c.datagramQueue.Peek())
			pending, granted := waiting(endpoint, participant)
			require.False(t, pending)
			require.False(t, granted, "unbuilt work holds no endpoint turn")
			socket.hold = nil
			socket.release()
			synctest.Wait()
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
			// A pending receive ends each natural turn after one member, so this
			// fixture reaches the record bound independently of grouping depth.
			c.receivedPackets.PushBack(receivedPacket{})
			for range 8 {
				r.pass(t)
				if c.emission.capacity() != nil {
					break
				}
			}
			c.receivedPackets.PopFront()
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
	// Both endpoints are full, a DATAGRAM fits only the target, and the chosen route
	// holds the turn at its own endpoint, with other connections behind it.
	contended := func(t *testing.T, pto bool) (*reliableFixture, *SendStream, *nativeOrdinarySender, *nativeProbeSocket, *contendedEndpoint, *contendedEndpoint, *endpointParticipant, []*endpointParticipant) {
		f := newAdmissionFixture(t, protocol.PerspectiveClient)
		f.supplyPeerCIDs(t)
		target, socket, endpoint := newTarget(t, f)
		t.Cleanup(socket.release)
		open(t, f, target, socket)
		r := &reliableFixture{admissionFixture: f}
		s := stream(t, f)
		sender := f.c.emission.local.(*nativeOrdinarySender)
		sender.nativeGSO = false
		r.setPathMTU(1, 1400)
		f.c.pathCredit.selectPrimaryPrivate(0)
		fitting := sender.probeParticipant(endpoint)
		require.NotNil(t, fitting)
		if pto {
			writeAsync(t, s, 1000)
			r.pass(t)
			require.Equal(t, 1000, reliableBytes(r.parseWrites(t, 0)))
		}
		f.socket.writes = nil

		fittingLoad := newContendedEndpoint(t, endpoint, 1<<40)
		chosenLoad := newContendedEndpoint(t, sender.endpoint, 1<<41)
		r.queueDatagram(t, 1300)
		writeAsync(t, s, 1000)
		r.loopPass(t)
		require.Zero(t, reliableBytes(r.parseWrites(t, 0)), "the chosen endpoint is full")
		pending, _, _ := fittingLoad.state(fitting)
		require.True(t, pending, "the held frame waits at its endpoint")
		rivals := []*endpointParticipant{chosenLoad.rival(t), chosenLoad.rival(t)}
		return r, s, sender, socket, chosenLoad, fittingLoad, fitting, rivals
	}
	t.Run("a frozen DATAGRAM keeps its route while the chosen endpoint progresses", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			r, s, sender, socket, chosenLoad, fittingLoad, fitting, rivals := contended(t, false)
			c := r.c
			fittingRival := fittingLoad.rival(t)

			chosenLoad.lease(t, sender.participant)
			select {
			case <-c.emission.localWakeup():
			default:
				t.Fatal("the lease wakes the connection")
			}
			r.loopPass(t)
			require.Equal(t, 1000, reliableBytes(r.parseWrites(t, 0)), "the chosen route uses its lease in the pass it wakes")
			require.Nil(t, c.datagramQueue.Peek(), "the frozen request owns the extracted frame")
			pending, granted, _ := fittingLoad.state(fitting)
			require.True(t, pending)
			require.True(t, granted, "the frame keeps its turn at the fitting endpoint")
			r.socket.writes = nil

			// The frame's lease survives selection in capacity and again in send, with
			// another connection waiting to take it.
			fittingLoad.lease(t, fitting)
			r.loopPass(t)
			require.Nil(t, c.datagramQueue.Peek(), "the frame left")
			require.Zero(t, c.datagramQueue.sendDrops.Load())
			require.Zero(t, reliableBytes(r.parseWrites(t, 0)))
			_, granted, _ = fittingLoad.state(fittingRival)
			require.True(t, granted, "the other connection is next at the fitting endpoint")
			own := r.socket
			r.socket = socket
			writes := r.parseWrites(t, 1)
			require.Equal(t, []int{1300}, reliableDatagrams(writes))
			require.Zero(t, reliableBytes(writes), "the frame leaves alone")
			r.socket = own

			// The chosen route's traffic continues, in its own turn.
			writeAsync(t, s, 500)
			r.loopPass(t)
			chosenLoad.serve(t, rivals...)
			r.loopPass(t)
			chosenLoad.lease(t, sender.participant)
			r.loopPass(t)
			require.Equal(t, 500, reliableBytes(r.parseWrites(t, 0)))
		})
	})
	t.Run("route selection preserves both frozen endpoint heads", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			r, _, sender, _, chosenLoad, _, fitting, _ := contended(t, false)
			require.Nil(t, r.c.datagramQueue.Peek())
			require.NotNil(t, sender.stagedHead(fitting))
			require.NotNil(t, sender.stagedHead(sender.participant))
			r.c.pathCredit.selectPrimaryPrivate(1)
			_ = r.c.emission.capacity()
			pending, _, _ := chosenLoad.state(sender.participant)
			require.True(t, pending, "changing selection cannot withdraw an already frozen request")
		})
	})
	t.Run("the spare local record lets a fitting endpoint progress independently", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			r, _, sender, socket, _, fittingLoad, fitting, _ := contended(t, false)
			require.NotNil(t, sender.stagedHead(sender.participant))
			fittingLoad.lease(t, fitting)
			r.loopPass(t)
			r.socket = socket
			require.Equal(t, []int{1300}, reliableDatagrams(r.parseWrites(t, 1)))
			require.NotNil(t, sender.stagedHead(sender.participant), "blocked A keeps its frozen payload while B progresses")
		})
	})
	t.Run("a pending PTO on the chosen route skips steering", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			r, _, sender, _, chosenLoad, _, _, _ := contended(t, true)
			// A PTO timer outlasts the lease, so it fires first.
			r.expirePTO(t, 0)
			chosenLoad.lease(t, sender.participant)
			r.loopPass(t)
			require.NotEmpty(t, r.parseWrites(t, 0), "the probe leaves on the chosen route")
			require.Nil(t, r.c.datagramQueue.Peek(), "the DATAGRAM is already frozen on the other route")
			require.False(t, r.c.emission.steered)
		})
	})
	t.Run("unconstructed selection needs no endpoint withdrawal", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f, r, sender, socket, endpoint, participant, _ := saturated(t)
			r.queueDatagram(t, 1300)
			require.Nil(t, f.c.emission.capacity())
			f.c.pathCredit.selectPrimaryPrivate(1)
			require.Nil(t, f.c.emission.capacity(), "only local capacity precedes construction")
			require.Same(t, endpoint, sender.pending.endpoint)
			pending, granted := waiting(endpoint, participant)
			require.False(t, pending)
			require.False(t, granted)
			socket.hold = nil
			socket.release()
			synctest.Wait()
		})
	})
}

// Two paths on one endpoint share a participant, so the attempt's registration is
// also the chosen route's, and the frame and the traffic take turns.
func TestPathReliableSteeringOnOneEndpoint(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		r := newReliableFixture(t, protocol.PerspectiveClient)
		c := r.c
		sender := c.emission.local.(*nativeOrdinarySender)
		sender.nativeGSO = false
		r.setPathMTU(1, 1400)
		c.pathCredit.selectPrimaryPrivate(0)

		load := newContendedEndpoint(t, sender.endpoint, 1<<40)
		r.queueDatagram(t, 1300)
		r.write(t, 1000)
		r.loopPass(t)
		require.Empty(t, r.socket.writes, "the endpoint is full")
		rival := load.rival(t)

		load.lease(t, sender.participant)
		r.loopPass(t)
		require.Nil(t, c.datagramQueue.Peek(), "the frame takes the shared turn")
		require.Equal(t, []int{1300}, reliableDatagrams(r.parseWrites(t, 1)))

		r.socket.writes = nil
		r.loopPass(t)
		load.serve(t, rival)
		r.loopPass(t)
		load.lease(t, sender.participant)
		r.loopPass(t)
		require.Equal(t, 1000, reliableBytes(r.parseWrites(t, 0)))
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

// selectPrimaryPrivate moves the primary through local use, as SetLocalUseV1 does.
func (p *pathCreditRegistry) selectPrimaryPrivate(id protocol.PathID) {
	now := monotime.Now()
	for other := protocol.PathID(0); other <= p.localMax; other++ {
		if other != id && p.members[other] != nil && p.terminals[other] == nil && p.members[other].live {
			if err := p.setLocalUsePrivate(other, true, now); err != nil {
				panic(err)
			}
		}
	}
	if err := p.setLocalUsePrivate(id, false, now); err != nil {
		panic(err)
	}
}

// acksFor counts the acknowledgments of target among writes; ACK(0) has no path ID.
func acksFor(writes []reliableWrite, target protocol.PathID) int {
	n := 0
	for _, write := range writes {
		for _, ack := range write.acks {
			if (ack.HasPathID && ack.PathID == target) || (!ack.HasPathID && target == 0) {
				n++
			}
		}
	}
	return n
}

// peerSends delivers n ack-eliciting packets from the peer on path id's tuple.
func (f *reliableFixture) peerSends(t *testing.T, id protocol.PathID, n int) {
	t.Helper()
	addr := f.c.RemoteAddr()
	if id != 0 {
		addr = f.c.pathCredit.members[id].addr
	}
	for range n {
		require.True(t, f.receive(t, id, addr, 0, &wire.PingFrame{}))
	}
}

// A primary/backup receiver returns a due backup-path PATH_ACK in an ACK-only turn
// on the backup's own route. The primary's data packets don't carry it.
func TestPathAckTurnOnBackup(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		require.True(t, f.c.pathCredit.members[1].backup)
		f.peerSends(t, 1, 2) // the threshold queues path 1's ACK
		f.write(t, 3000)
		f.pass(t)

		onZero, onOne := f.parseWrites(t, 0), f.parseWrites(t, 1)
		require.Equal(t, 3000, reliableBytes(onZero))
		require.Zero(t, acksFor(onZero, 1), "the primary's packets don't carry path 1's ACK")
		require.Len(t, onOne, 1)
		require.Equal(t, 1, acksFor(onOne, 1))
		require.Empty(t, onOne[0].streams)
		require.Empty(t, onOne[0].frames, "an ACK-only turn")
		require.Equal(t, admissionTupleA.AddrPort().Port(), onOne[0].destination.Port())
	})
}

// The carrier's own target still piggybacks on its data packets, while a home target
// on another path that isn't due is withheld from them until its own rule fires.
func TestPathAckOwnPiggybackWithholdsOtherHome(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		f.peerSends(t, 1, 2) // settle path 1's receive space, so it starts from no ACK owed
		f.pass(t)
		f.socket.writes = nil
		f.peerSends(t, 0, 1)
		f.peerSends(t, 1, 1) // one packet arms only path 1's max_ack_delay alarm
		require.False(t, f.c.receivedPacketHandler.AckDueOnPath(1, monotime.Now()))
		f.write(t, 3000)
		f.pass(t)

		onZero := f.parseWrites(t, 0)
		require.Equal(t, 3000, reliableBytes(onZero))
		require.Equal(t, 1, acksFor(onZero, 0), "path zero's own ACK piggybacks before it is due")
		require.Zero(t, acksFor(onZero, 1), "path 1's undue ACK isn't put on path zero")
		require.Empty(t, f.parseWrites(t, 1))
		require.False(t, f.c.receivedPacketHandler.GetAlarmTimeout().IsZero(), "path 1's alarm still owns it")

		f.socket.writes = nil
		time.Sleep(protocol.MaxAckDelay)
		f.pass(t)
		onOne := f.parseWrites(t, 1)
		require.Equal(t, 1, acksFor(onOne, 1), "it leaves on its own path once due")
		require.Zero(t, acksFor(f.parseWrites(t, 0), 1))
	})
}

// A path that can't carry its own acknowledgments isn't home, so they are released
// to whichever carrier the call uses. That includes a suspect path zero.
func TestPathAckNonHomeReleases(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		p := f.c.pathCredit
		m := p.members[1]
		homes := func() ackhandler.AckCarriage {
			var acks ackhandler.AckCarriage
			p.holdHomes(&acks)
			return acks
		}
		require.True(t, homes().Holds(0))
		require.True(t, homes().Holds(1), "a local backup is still home")
		sender := f.c.emission.local.(*nativeOrdinarySender)
		for _, state := range []struct {
			name string
			set  func() (restore func())
		}{
			{"validating", func() func() { m.live = false; return func() { m.live = true } }},
			{"retiring", func() func() {
				p.terminals[1] = &pathTerminal{}
				return func() { delete(p.terminals, 1) }
			}},
			{"fencing", func() func() {
				m.candidate = &pathCandidate{fencing: true}
				return func() { m.candidate = nil }
			}},
			{"suspect", func() func() { m.suspect = true; return func() { m.suspect = false } }},
			{"failed endpoint", func() func() {
				origin := sender.origin
				sender.origin = nil
				return func() { sender.origin = origin }
			}},
			{"no destination CID", func() func() {
				set := f.c.connIDManager.pathCIDs[1]
				delete(f.c.connIDManager.pathCIDs, 1)
				return func() { f.c.connIDManager.pathCIDs[1] = set }
			}},
		} {
			require.Nil(t, m.candidate)
			restore := state.set()
			require.False(t, homes().Holds(1), state.name)
			restore()
			require.True(t, homes().Holds(1), state.name)
		}

		// A suspect path 1's due ACK rides path zero's packet.
		f.peerSends(t, 1, 2)
		m.suspect = true
		f.pass(t)
		require.Equal(t, 1, acksFor(f.parseWrites(t, 0), 1))
		require.Zero(t, acksFor(f.parseWrites(t, 1), 1), "only its validation probe uses path 1")
		m.suspect = false

		// A suspect path zero isn't home, though zeroCarries ignores suspicion: its due
		// ACK rides the held route, path 1.
		f.socket.writes = nil
		p.members[0].suspect = true
		require.True(t, p.zeroCarries())
		require.False(t, homes().Holds(0))
		f.peerSends(t, 0, 2)
		f.pass(t)
		require.Equal(t, 1, acksFor(f.parseWrites(t, 1), 0))
		require.Zero(t, acksFor(f.parseWrites(t, 0), 0))
	})
}

// refusingLocal refuses path 1's ACK turn locally: with full set, the endpoint
// queue blocks once the turn has switched to that route; otherwise its route has
// no native construction capacity.
type refusingLocal struct {
	localPacketSender
	refused, route *pathRoute
	full, all      bool // all blocks the queue on every route
}

func (l *refusingLocal) useRoute(r *pathRoute) { l.route = r; l.localPacketSender.useRoute(r) }
func (l *refusingLocal) canConstruct(r *pathRoute) bool {
	return (l.full || r != l.refused) && l.localPacketSender.canConstruct(r)
}
func (l *refusingLocal) WouldBlock() bool {
	return l.all || (l.full && l.route == l.refused) || l.localPacketSender.WouldBlock()
}

// Each owner that can refuse a home target's turn releases it into the same call,
// where path zero's packet carries it, with no alarm left to wake for it.
func TestPathAckTurnRefusalReleases(t *testing.T) {
	for _, refusal := range []struct {
		name, controller string
		refuse           func(*reliableFixture) (restore func())
	}{
		{"queue full", "reno", func(f *reliableFixture) func() { return f.refuseLocally(true) }},
		{"native construction capacity", "reno", func(f *reliableFixture) func() { return f.refuseLocally(false) }},
		{"SendNone", "reno", func(f *reliableFixture) func() {
			f.recovery(1).(interface{ CloseLocal(monotime.Time) }).CloseLocal(monotime.Now())
			require.Equal(t, ackhandler.SendNone, f.recovery(1).SendMode(monotime.Now()))
			return func() {}
		}},
		{"BBR credit", "bbrv3", func(f *reliableFixture) func() {
			p := f.bbrPath(1)
			held := p.credit.reserve(p.creditLimit(), p.creditLimit(), false, false)
			require.NotNil(t, held)
			return held.completeLocal
		}},
	} {
		t.Run(refusal.name, func(t *testing.T) {
			nativeOrdinaryTest(t, func(t *testing.T) {
				f := newReliableFixtureFor(t, protocol.PerspectiveServer, nil, refusal.controller)
				f.peerSends(t, 1, 2)
				restore := refusal.refuse(f)
				result := f.c.triggerSending(monotime.Now())
				require.NoError(t, result.err)
				synctest.Wait()
				require.NoError(t, f.c.emission.consumeLocal())
				restore()

				require.Equal(t, 1, acksFor(f.parseWrites(t, 0), 1), "released into the same call")
				require.Zero(t, acksFor(f.parseWrites(t, 1), 1))
				require.False(t, f.c.receivedPacketHandler.AckDueOnPath(1, monotime.Now()))
				require.True(t, f.c.receivedPacketHandler.GetAlarmTimeout().IsZero(), "no stale alarm")
				require.False(t, f.c.emission.ackTurnSent)
			})
		})
	}
}

func (f *reliableFixture) refuseLocally(full bool) (restore func()) {
	e := &f.c.emission
	inner := e.local
	e.local = &refusingLocal{localPacketSender: inner, refused: f.c.pathCredit.routeOf(1), route: e.route, full: full}
	e.queue = e.local
	return func() { e.local, e.queue = inner, inner }
}

// After a same-ID migration, a turn leaves through the path's current tuple, and the
// route keeps its periodic-PING count.
func TestPathAckTurnAfterMigration(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveClient)
		require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathNewConnectionIDFrame{PathID: 1, SequenceNumber: 1, ConnectionID: replacementPeerCID(1), StatelessResetToken: protocol.StatelessResetToken{0xab}}))
		route := f.c.pathCredit.routeOf(1)
		target, socket := migrationTarget(t, f.admissionFixture)
		migrate(t, f.admissionFixture, 1, target, socket)
		require.Same(t, route, f.c.pathCredit.routeOf(1))
		route.numNonAckElicitingAcks = 7
		f.c.pathCredit.refreshRoute(f.c.pathCredit.members[1])
		require.Equal(t, 7, route.numNonAckElicitingAcks, "refreshing the route keeps its count")

		origin := f.socket
		origin.writes, socket.writes = nil, nil
		f.peerSends(t, 1, 2)
		f.pass(t)
		f.socket = socket
		onOne := f.parseWrites(t, 1)
		require.Equal(t, 1, acksFor(onOne, 1), "the turn uses the new tuple")
		require.Empty(t, onOne[0].streams)
		require.Equal(t, 8, route.numNonAckElicitingAcks)
		f.socket = origin
		require.Zero(t, acksFor(f.parseWrites(t, 1), 1))
		require.Zero(t, acksFor(f.parseWrites(t, 0), 1))
	})
}

// An alarm-due acknowledgment leaves in its turn, with its own space's ACK Delay.
func TestPathAckAlarmDueTurnCarriesOwnDelay(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		f.peerSends(t, 1, 2)
		f.pass(t)
		f.socket.writes = nil
		f.peerSends(t, 1, 1)
		time.Sleep(5 * time.Millisecond)
		f.peerSends(t, 0, 1) // path zero's own, later alarm
		f.pass(t)
		require.Empty(t, f.parseWrites(t, 1), "not due before its alarm")

		time.Sleep(protocol.MaxAckDelay - 5*time.Millisecond)
		f.pass(t)
		onOne := f.parseWrites(t, 1)
		require.Equal(t, 1, acksFor(onOne, 1))
		require.Zero(t, acksFor(onOne, 0), "path zero's ACK stays home")
		var delay time.Duration
		for _, ack := range onOne[0].acks {
			delay = ack.DelayTime << protocol.AckDelayExponent // parseWrites decodes with exponent zero
		}
		require.InDelta(t, float64(protocol.MaxAckDelay), float64(delay), float64(100*time.Microsecond))
	})
}

// sustainedTurns exceeds the shared tracking ceiling.
const sustainedTurns = protocol.MaxTrackedSentPackets + 100

// sendOnce runs one emission call, as one pass of the connection loop does.
func (f *reliableFixture) sendOnce(t *testing.T) emissionResult {
	t.Helper()
	result := f.c.triggerSending(monotime.Now())
	require.NoError(t, result.err)
	synctest.Wait()
	require.NoError(t, f.c.emission.consumeLocal())
	return result
}

// Turns and other calls alternate at worst one to one, so recurring receives on a
// backup can't starve the selected route, and the per-carrier PING keeps sustained
// turns' history bounded.
func TestPathAckTurnAlternation(t *testing.T) {
	t.Run("selected route between turns", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newReliableFixture(t, protocol.PerspectiveServer)
			f.peerSends(t, 0, 2)
			f.c.framer.QueueControlFrame(&wire.MaxDataFrame{MaximumData: 1 << 30})
			var zero []reliableWrite
			for range 2 {
				f.peerSends(t, 1, 2) // sustained receive on the backup
				f.sendOnce(t)
				zero = append(zero, f.parseWrites(t, 0)...)
				f.socket.writes = nil
			}
			require.Equal(t, 1, acksFor(zero, 0), "the selected route's ACK leaves between turns")
			var credit bool
			for _, write := range zero {
				for _, frame := range write.frames {
					_, ok := frame.(*wire.MaxDataFrame)
					credit = credit || ok
				}
			}
			require.True(t, credit, "and so does its credit update")
		})
	})
	for _, end := range []string{"queue full", "hard blocked"} {
		t.Run("cleared when "+end, func(t *testing.T) {
			nativeOrdinaryTest(t, func(t *testing.T) {
				f := newReliableFixture(t, protocol.PerspectiveServer)
				// Path zero waits for turns while path 1 holds the opportunity.
				f.c.pathCredit.selectPrimaryPrivate(1)
				f.peerSends(t, 0, 2)
				f.sendOnce(t)
				require.True(t, f.c.emission.ackTurnSent)
				f.peerSends(t, 0, 2)
				if end == "queue full" {
					restore := f.refuseLocally(false)
					f.c.emission.local.(*refusingLocal).all = true
					result := f.sendOnce(t)
					restore()
					require.Equal(t, emissionQueueFull, result.stop)
					require.False(t, result.retry, "the capacity wakeup continues instead")
				} else {
					f.recovery(1).(interface{ CloseLocal(monotime.Time) }).CloseLocal(monotime.Now())
					result := f.sendOnce(t)
					require.Equal(t, blockModeHardBlocked, result.blocked)
					require.NotEqual(t, emissionQueueFull, result.stop)
					require.True(t, result.retry, "a waiting home target asks for a retry")
				}
				require.False(t, f.c.emission.ackTurnSent)
				require.False(t, f.c.emission.ackTurnSkip)
			})
		})
	}
	t.Run("retry sends a threshold-queued target", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newReliableFixture(t, protocol.PerspectiveServer)
			f.peerSends(t, 1, 2)
			f.sendOnce(t)
			f.socket.writes = nil
			f.peerSends(t, 1, 2) // queued by threshold, which arms no alarm
			require.True(t, f.c.receivedPacketHandler.GetAlarmTimeout().IsZero())
			result := f.sendOnce(t)
			require.True(t, result.retry)
			require.Empty(t, f.parseWrites(t, 1), "it waits one call")
			f.sendOnce(t)
			require.Equal(t, 1, acksFor(f.parseWrites(t, 1), 1), "the retry sends it")
		})
	})
	t.Run("sustained turns stay bounded", func(t *testing.T) {
		// The fixture's peer never updates its keys.
		first := handshake.FirstKeyUpdateInterval
		handshake.FirstKeyUpdateInterval = math.MaxUint64
		t.Cleanup(handshake.SetKeyUpdateInterval(math.MaxUint64))
		t.Cleanup(func() { handshake.FirstKeyUpdateInterval = first })
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newReliableFixture(t, protocol.PerspectiveServer)
			var unacked []reliableWrite
			pings := 0
			for i := range sustainedTurns {
				f.peerSends(t, 1, 2)
				f.sendOnce(t)
				f.sendOnce(t)
				onOne := f.parseWrites(t, 1)
				f.socket.writes = nil
				if len(onOne) != 1 || acksFor(onOne, 1) != 1 {
					t.Fatalf("turn %d: %d writes on path 1", i, len(onOne))
				}
				unacked = append(unacked, onOne...)
				if len(onOne[0].frames) == 0 {
					continue
				}
				require.IsType(t, &wire.PingFrame{}, onOne[0].frames[0])
				require.Len(t, unacked, protocol.MaxNonAckElicitingAcks+1, "every twentieth turn carries the PING")
				pings++
				f.ackPath(t, 1, 1, unacked)
				unacked = nil
			}
			require.GreaterOrEqual(t, pings, sustainedTurns/(protocol.MaxNonAckElicitingAcks+1)-1)
			for _, id := range []protocol.PathID{0, 1} {
				require.NotEqual(t, ackhandler.SendNone, f.recovery(id).SendMode(monotime.Now()), "path %d stays sendable", id)
			}
		})
	})
}

// A Reno primary/backup transfer and its failover are unchanged. With the peer idle
// on the backup throughout, B7 gives the backup no packet, PTO, suspicion or
// liveness refresh.
func TestPathAckIdleBackupUnchanged(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newReliableFixture(t, protocol.PerspectiveServer)
		p := f.c.pathCredit
		backup := p.members[1]
		// The setup's validation packets owe path 1 one acknowledgment.
		time.Sleep(protocol.MaxAckDelay)
		f.pass(t)
		f.socket.writes = nil
		liveness, count, alarm := backup.livenessAt, p.routeOf(1).numNonAckElicitingAcks, f.pathAlarm(1)
		for range 4 {
			f.write(t, 4000)
			f.pass(t)
			onZero := f.parseWrites(t, 0)
			require.Equal(t, 4000, reliableBytes(onZero))
			require.Empty(t, f.parseWrites(t, 1), "nothing leaves on the idle backup")
			f.socket.writes = nil
			time.Sleep(10 * time.Millisecond)
			f.ackPath(t, 0, 0, onZero)
			f.peerSends(t, 0, 2)
			f.pass(t)
			require.Empty(t, f.parseWrites(t, 1))
			f.socket.writes = nil
		}
		require.Equal(t, alarm, f.pathAlarm(1), "no new PTO on the backup")
		require.Zero(t, backup.attempts)
		require.False(t, backup.suspect)
		require.Equal(t, liveness, backup.livenessAt, "no liveness refresh")
		require.Equal(t, count, p.routeOf(1).numNonAckElicitingAcks)

		// Failover still promotes the backup, which carries the stranded work.
		f.suspectAfterPTOs(t, 0)
		require.Equal(t, protocol.PathID(1), p.primary)
		require.Equal(t, 1000, reliableBytes(f.parseWrites(t, 1)))
	})
}

// Once a home path's oldest outstanding record is older than one of its PTOs, its
// turns stop and its acknowledgments fall back to the selected carrier. The record
// can be our data on a path whose return direction failed, or the turn's own PING.
func TestPathAckTurnHistoryRoom(t *testing.T) {
	for _, record := range []string{"data", "turn PING"} {
		t.Run(record, func(t *testing.T) {
			nativeOrdinaryTest(t, func(t *testing.T) {
				f := newReliableFixture(t, protocol.PerspectiveServer)
				p := f.c.pathCredit
				if record == "data" {
					p.selectPrimaryPrivate(1)
					f.write(t, 1000)
					f.pass(t)
					require.Equal(t, 1000, reliableBytes(f.parseWrites(t, 1)))
					p.selectPrimaryPrivate(0)
				} else {
					f.pingTurn(t, 1)
				}
				f.socket.writes = nil
				f.peerSends(t, 1, 2)
				f.pass(t)
				require.Equal(t, 1, acksFor(f.parseWrites(t, 1), 1), "within one PTO, turns continue")

				f.socket.writes = nil
				time.Sleep(p.rtt[1].PTO(true) + time.Millisecond)
				f.peerSends(t, 1, 2)
				f.pass(t)
				require.Zero(t, acksFor(f.parseWrites(t, 1), 1), "turns stop")
				require.Equal(t, 1, acksFor(f.parseWrites(t, 0), 1), "and the selected carrier takes it")
			})
		})
	}
}
