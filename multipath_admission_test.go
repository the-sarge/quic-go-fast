//go:build darwin || linux || freebsd

package quic

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/internal/qtls"
	"github.com/quic-go/quic-go/internal/testdata"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"
)

var (
	admissionTupleA  = &net.UDPAddr{IP: net.IPv4(192, 0, 2, 10), Port: 1000}
	admissionTupleB  = &net.UDPAddr{IP: net.IPv4(192, 0, 2, 11), Port: 1001}
	admissionPeerCID = protocol.ParseConnectionID([]byte{0xa, 0xb, 0xc, 0xd})
)

// admissionFixture holds one connection on the private route, with real keys on
// both ends, so every packet crosses the real path-qualified protection.
type admissionFixture struct {
	tc     *testConnection
	c      *Conn
	socket *nativeProbeSocket
	route  *packetHandlerMap
	peer   handshake.CryptoSetup
	nextPN map[protocol.PathID]protocol.PacketNumber
}

func admissionKeys(t *testing.T) (client, server handshake.CryptoSetup) {
	t.Helper()
	defer qtls.SetCipherSuite(tls.TLS_AES_128_GCM_SHA256)()
	params := &wire.TransportParameters{ActiveConnectionIDLimit: 2}
	client = handshake.NewCryptoSetupClient(protocol.ConnectionID{}, params, &tls.Config{RootCAs: testdata.GetRootCA(), ServerName: "localhost", NextProtos: []string{"admission"}}, false, utils.NewRTTStats(), nil, utils.DefaultLogger, protocol.Version1)
	conf := testdata.GetTLSConfig()
	conf.NextProtos = []string{"admission"}
	server = handshake.NewCryptoSetupServer(protocol.ConnectionID{}, &net.UDPAddr{}, &net.UDPAddr{}, params, conf, false, utils.NewRTTStats(), nil, utils.DefaultLogger, protocol.Version1)
	t.Cleanup(func() {
		client.Close()
		server.Close()
	})
	require.NoError(t, client.StartHandshake(context.Background()))
	require.NoError(t, server.StartHandshake(context.Background()))
	complete := make(map[handshake.CryptoSetup]bool)
	for round := 0; len(complete) < 2 && round < 8; round++ {
		for _, pair := range [][2]handshake.CryptoSetup{{client, server}, {server, client}} {
			for event := pair[0].NextEvent(); event.Kind != handshake.EventNoEvent; event = pair[0].NextEvent() {
				switch event.Kind {
				case handshake.EventWriteInitialData:
					require.NoError(t, pair[1].HandleMessage(event.Data, protocol.EncryptionInitial))
				case handshake.EventWriteHandshakeData:
					require.NoError(t, pair[1].HandleMessage(event.Data, protocol.EncryptionHandshake))
				case handshake.EventHandshakeComplete:
					complete[pair[0]] = true
				}
			}
		}
	}
	require.Len(t, complete, 2)
	client.SetHandshakeConfirmed()
	server.SetHandshakeConfirmed()
	return client, server
}

func newAdmissionFixture(t *testing.T, perspective protocol.Perspective) *admissionFixture {
	t.Helper()
	return newAdmissionFixtureWith(t, perspective, nil)
}

func newAdmissionFixtureWith(t *testing.T, perspective protocol.Perspective, tracer func(context.Context, bool, ConnectionID) qlogwriter.Trace) *admissionFixture {
	t.Helper()
	return buildAdmissionFixture(t, perspective, tracer, false)
}

func buildAdmissionFixture(t *testing.T, perspective protocol.Perspective, tracer func(context.Context, bool, ConnectionID) qlogwriter.Trace, unactivated bool) *admissionFixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	conf := &Config{InitialPacketSize: 1200, EnableDatagrams: true, DisablePathMTUDiscovery: true, Tracer: tracer}
	var tc *testConnection
	if perspective == protocol.PerspectiveServer {
		tc = newServerTestConnection(t, ctrl, conf, false, connectionOptHandshakeConfirmed())
	} else {
		tc = newClientTestConnection(t, ctrl, conf, false, connectionOptHandshakeConfirmed())
	}
	c := tc.conn
	c.peerParams = &wire.TransportParameters{}
	clientKeys, serverKeys := admissionKeys(t)
	local, peer := serverKeys, clientKeys
	if perspective == protocol.PerspectiveClient {
		local, peer = clientKeys, serverKeys
	}
	local.DiscardInitialKeys()
	local.SetHandshakeConfirmed() // a close seals no long header packet
	c.emission.packer.cryptoSetup = local
	c.unpacker = newPacketUnpacker(local, tc.srcConnID.Len())
	c.sentPacketHandler.ReceivedBytes(1<<20, monotime.Now())
	c.sentPacketHandler.DropPackets(protocol.EncryptionInitial, monotime.Now())
	c.sentPacketHandler.DropPackets(protocol.EncryptionHandshake, monotime.Now())
	c.handshakeConfirmed = true
	c.rttStats.UpdateRTT(5*time.Millisecond, 0)
	socket := newNativeProbeSocket()
	endpoint := startNativeProbeEndpoint(t, socket)
	require.NoError(t, c.useNativeOrdinary(endpoint, nativeProbePeer, packetInfo{}))
	t.Cleanup(c.emission.local.(*nativeOrdinarySender).Close)
	c.connIDManager.ChangeInitialConnID(tc.destConnID)
	route := &packetHandlerMap{connIDLen: tc.srcConnID.Len(), handlers: make(map[protocol.ConnectionID]packetHandlerEntry), resetTokens: make(map[protocol.StatelessResetToken]packetHandler), logger: utils.DefaultLogger}
	require.True(t, route.Add(tc.srcConnID, c))
	c.connIDGenerator.initialClientDestConnID = nil
	c.connIDGenerator.generator = &protocol.DefaultConnectionIDGenerator{ConnLen: tc.srcConnID.Len()}
	c.connIDGenerator.connRunners = connRunners{route: connectionRunnerCallbacks(route, c)}
	c.connIDGenerator.ownRunner = route
	c.connIDManager.addStatelessResetToken = func(token protocol.StatelessResetToken) { route.AddResetToken(token, c) }
	c.connIDManager.removeStatelessResetToken = route.RemoveResetToken
	fixture := &admissionFixture{tc: tc, c: c, socket: socket, route: route, peer: peer, nextPN: make(map[protocol.PathID]protocol.PacketNumber)}
	if unactivated {
		c.handshakeConfirmed = false
		return fixture
	}
	_, err := c.enableMultipathCredit(2, 1, 4)
	require.NoError(t, err)
	seal, err := peer.Get1RTTSealer()
	require.NoError(t, err)
	require.NoError(t, handshake.AdmitShortHeaderPath(seal, 1, utils.NewRTTStats()))
	return fixture
}

func newAdmissionFixtureUnactivated(t *testing.T) *admissionFixture {
	t.Helper()
	return buildAdmissionFixture(t, protocol.PerspectiveClient, nil, true)
}

func (f *admissionFixture) issuedCID(id protocol.PathID) protocol.ConnectionID {
	for _, cid := range f.c.connIDGenerator.pathCIDs[id].active {
		return cid
	}
	return protocol.ConnectionID{}
}

func (f *admissionFixture) peerPacket(t *testing.T, id protocol.PathID, from net.Addr, padding int, tamper bool, frames ...wire.Frame) receivedPacket {
	t.Helper()
	base, err := f.peer.Get1RTTSealer()
	require.NoError(t, err)
	sealer, err := handshake.ShortHeaderSealerForPath(base, id)
	require.NoError(t, err)
	dest := f.tc.srcConnID
	if id != 0 {
		dest = f.issuedCID(id)
	}
	pn := f.nextPN[id]
	f.nextPN[id] = pn + 1
	raw, err := wire.AppendShortHeader(nil, dest, pn, protocol.PacketNumberLen4, sealer.KeyPhase())
	require.NoError(t, err)
	offset := len(raw)
	for _, frame := range frames {
		raw, err = frame.Append(raw, protocol.Version1)
		require.NoError(t, err)
	}
	raw = append(raw, make([]byte, padding+4)...) // PADDING, and enough for the header sample
	raw = append(make([]byte, 0, len(raw)+sealer.Overhead()), raw...)
	_, err = sealer.Seal(raw[offset:offset], raw[offset:], pn, raw[:offset])
	require.NoError(t, err)
	raw = raw[:len(raw)+sealer.Overhead()]
	sealer.EncryptHeader(raw[offset:offset+16], &raw[0], raw[offset-4:offset])
	if tamper {
		raw[len(raw)-1] ^= 0xff
	}
	buf := getPacketBuffer()
	buf.Data = append(buf.Data[:0], raw...)
	return receivedPacket{pathID: id, remoteAddr: from, rcvTime: monotime.Now(), data: buf.Data, buffer: buf}
}

func (f *admissionFixture) receive(t *testing.T, id protocol.PathID, from net.Addr, padding int, frames ...wire.Frame) bool {
	t.Helper()
	processed, err := f.c.handleShortHeaderPacket(f.peerPacket(t, id, from, padding, false, frames...), false, 0)
	require.NoError(t, err)
	return processed
}

// pass runs the connection loop's per-pass owners, then lets the endpoint write.
func (f *admissionFixture) pass(t *testing.T) {
	t.Helper()
	synctest.Wait()
	require.NoError(t, f.c.emission.consumeLocal())
	require.NoError(t, f.c.pathCredit.expire(monotime.Now()))
	for {
		result := f.c.triggerSending(monotime.Now())
		require.NoError(t, result.err)
		if !result.retry {
			break
		}
	}
	synctest.Wait()
}

type admissionWrite struct {
	nativeProbeWrite
	frames []wire.Frame
}

// peerWrites decrypts each written datagram with the peer's view of path id.
func (f *admissionFixture) peerWrites(t *testing.T, id protocol.PathID) []admissionWrite {
	t.Helper()
	var writes []admissionWrite
	for _, write := range f.socket.writes {
		connIDLen := admissionPeerCID.Len()
		if id == 0 {
			connIDLen = f.tc.destConnID.Len()
		}
		unpacker := newPacketUnpacker(f.peer, connIDLen)
		_, _, _, payload, err := unpacker.UnpackShortHeader(id, monotime.Now(), append([]byte(nil), write.data...))
		if err != nil {
			continue
		}
		parser := wire.NewFrameParser(true, false, false)
		parser.EnableMultipath()
		var frames []wire.Frame
		for len(payload) > 0 {
			typ, n, err := parser.ParseType(payload, protocol.Encryption1RTT)
			if errors.Is(err, io.EOF) {
				break // only PADDING remained
			}
			require.NoError(t, err)
			payload = payload[n:]
			// Control frames are shuffled, so every type before them must parse.
			if typ.IsAckFrameType() || typ.IsPathAckFrameType() {
				_, n, err := parser.ParseAckFrame(typ, payload, protocol.Encryption1RTT, protocol.Version1)
				require.NoError(t, err)
				payload = payload[n:]
				continue
			}
			frame, n, err := parser.ParseLessCommonFrame(typ, payload, protocol.Version1)
			require.NoError(t, err)
			payload = payload[n:]
			frames = append(frames, frame)
		}
		writes = append(writes, admissionWrite{nativeProbeWrite: write, frames: frames})
	}
	return writes
}

func admissionChallenges(writes []admissionWrite) [][8]byte {
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

func admissionAbandons(writes []admissionWrite, id protocol.PathID) int {
	count := 0
	for _, write := range writes {
		for _, frame := range write.frames {
			if abandon, ok := frame.(*wire.PathAbandonFrame); ok && abandon.PathID == id {
				count++
			}
		}
	}
	return count
}

func (f *admissionFixture) supplyPeerCIDs(t *testing.T) {
	t.Helper()
	require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathNewConnectionIDFrame{PathID: 1, ConnectionID: admissionPeerCID, StatelessResetToken: protocol.StatelessResetToken{1}}))
	require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathNewConnectionIDFrame{PathID: 0, SequenceNumber: 1, ConnectionID: protocol.ParseConnectionID(make([]byte, f.tc.destConnID.Len())), StatelessResetToken: protocol.StatelessResetToken{2}}))
}

func TestPathAdmissionAuthenticatedWithoutChallenge(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newAdmissionFixture(t, protocol.PerspectiveServer)
		f.supplyPeerCIDs(t)
		require.True(t, f.receive(t, 1, admissionTupleA, 1200, &wire.PingFrame{}))
		require.Contains(t, f.c.pathCredit.members, protocol.PathID(1))
		require.False(t, f.c.pathCredit.members[1].live)
		f.pass(t)
		challenges := admissionChallenges(f.peerWrites(t, 1))
		require.Len(t, challenges, 1)
		require.Equal(t, admissionTupleA.AddrPort().Port(), f.peerWrites(t, 1)[0].destination.Port())
		require.True(t, f.receive(t, 1, admissionTupleA, 1200, &wire.PathResponseFrame{Data: challenges[0]}))
		require.True(t, f.c.pathCredit.members[1].live)
		require.Nil(t, f.c.pathCredit.members[1].candidate)
	})
}

func TestPathAdmissionUnauthenticatedSparseIDs(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newAdmissionFixture(t, protocol.PerspectiveServer)
		packet := f.peerPacket(t, 1, admissionTupleA, 1200, true, &wire.PingFrame{})
		processed, err := f.c.handleShortHeaderPacket(packet, false, 0)
		require.NoError(t, err)
		require.False(t, processed)
		require.NotContains(t, f.c.pathCredit.members, protocol.PathID(1), "a forged packet allocates nothing")
		require.Nil(t, f.c.sentPacketHandler.(interface {
			RecoveryOnPath(protocol.PathID) ackhandler.SentPacketHandler
		}).RecoveryOnPath(1))
		// A routed ID no window issued has no keys, and a frame's sparse ID is refused.
		sparse := f.peerPacket(t, 0, admissionTupleA, 100, false, &wire.PingFrame{})
		sparse.pathID = 7
		processed, err = f.c.handleShortHeaderPacket(sparse, false, 0)
		require.NoError(t, err)
		require.False(t, processed)
		require.Len(t, f.c.pathCredit.members, 1)
		var transportErr *qerr.TransportError
		require.ErrorAs(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathAbandonFrame{PathID: 1 << 30}), &transportErr)
		require.Equal(t, qerr.ProtocolViolation, transportErr.ErrorCode)
	})
}

func TestPathAdmissionTwoCandidates(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newAdmissionFixture(t, protocol.PerspectiveServer)
		f.supplyPeerCIDs(t)
		require.True(t, f.receive(t, 1, admissionTupleA, 1200, &wire.PingFrame{}))
		require.True(t, f.receive(t, 0, admissionTupleB, 1200, &wire.PathChallengeFrame{Data: [8]byte{9}}))
		members := f.c.pathCredit.members
		require.NotNil(t, members[1].candidate)
		require.NotNil(t, members[0].candidate, "rebinding on an established path takes its own slot")
		f.pass(t)
		require.NotEmpty(t, admissionChallenges(f.peerWrites(t, 1)))
		require.NotEmpty(t, admissionChallenges(f.peerWrites(t, 0)))
		// The same tuple on both IDs stays two candidates, and a tuple that has only
		// probed records validation with no cutover.
		require.True(t, f.receive(t, 0, admissionTupleB, 1200, &wire.PathResponseFrame{Data: admissionChallenges(f.peerWrites(t, 0))[0]}))
		require.Nil(t, members[0].candidate)
		require.True(t, f.c.pathCredit.tupleValidated(admissionTupleB))
		require.False(t, addrsEqual(members[0].addr, admissionTupleB))
		require.NotNil(t, members[1].candidate)
	})
}

func TestPathAdmissionContentionKeepsAdmitted(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newAdmissionFixture(t, protocol.PerspectiveServer)
		f.supplyPeerCIDs(t)
		require.True(t, f.receive(t, 1, admissionTupleA, 1200, &wire.PingFrame{}))
		candidate := f.c.pathCredit.members[1].candidate
		deadline := candidate.probe.validationDeadline
		time.Sleep(time.Second)
		require.True(t, f.receive(t, 1, admissionTupleB, 1200, &wire.PathChallengeFrame{Data: [8]byte{1}}))
		require.Same(t, candidate, f.c.pathCredit.members[1].candidate, "a new tuple never displaces the admitted candidate")
		require.Nil(t, f.c.pathCredit.members[1].response, "and gets no borrowed budget")
		require.True(t, f.receive(t, 1, admissionTupleA, 1200, &wire.PingFrame{}))
		require.Equal(t, deadline, candidate.probe.validationDeadline, "traffic never refreshes the deadline")
		f.pass(t)
		for _, write := range f.peerWrites(t, 1) {
			require.Equal(t, admissionTupleA.AddrPort().Port(), write.destination.Port())
		}
	})
}

func TestPathAdmissionNoCrossTupleAmplification(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newAdmissionFixture(t, protocol.PerspectiveServer)
		f.supplyPeerCIDs(t)
		// Plenty of bytes arrive on the established tuple; the new tuple sent 40.
		require.True(t, f.receive(t, 0, f.c.RemoteAddr(), 1200, &wire.PingFrame{}))
		require.True(t, f.receive(t, 1, admissionTupleA, 0, &wire.PingFrame{}))
		candidate := f.c.pathCredit.members[1].candidate
		f.pass(t)
		require.LessOrEqual(t, candidate.probe.bytesSent, 3*candidate.probe.bytesReceived)
		for _, write := range f.peerWrites(t, 1) {
			require.Less(t, len(write.data), 1200, "the allowance is this tuple's alone")
		}
		// A compact response lifts the limit, but only a padded challenge activates.
		challenges := admissionChallenges(f.peerWrites(t, 1))
		require.Len(t, challenges, 1)
		require.True(t, f.receive(t, 1, admissionTupleA, 0, &wire.PathResponseFrame{Data: challenges[0]}))
		require.False(t, f.c.pathCredit.members[1].live)
		require.True(t, candidate.probe.validated)
		f.pass(t)
		writes := f.peerWrites(t, 1)
		require.GreaterOrEqual(t, len(writes[len(writes)-1].data), 1200)
	})
}

func TestPathAdmissionMismatchedResponseTarget(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newAdmissionFixture(t, protocol.PerspectiveServer)
		f.supplyPeerCIDs(t)
		require.True(t, f.receive(t, 1, admissionTupleA, 1200, &wire.PingFrame{}))
		f.pass(t)
		challenge := admissionChallenges(f.peerWrites(t, 1))[0]
		require.True(t, f.receive(t, 1, admissionTupleA, 100, &wire.PathResponseFrame{Data: [8]byte{0xff}}))
		require.True(t, f.receive(t, 0, f.c.RemoteAddr(), 100, &wire.PathResponseFrame{Data: challenge}), "another path's response is ignored")
		require.False(t, f.c.pathCredit.members[1].live)
		require.False(t, f.c.pathCredit.members[1].candidate.probe.validated)
	})
}

func TestPathAdmissionAbsoluteExpiry(t *testing.T) {
	t.Run("no response", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveServer)
			f.supplyPeerCIDs(t)
			require.True(t, f.receive(t, 1, admissionTupleA, 1200, &wire.PingFrame{}))
			f.pass(t)
			start := monotime.Now()
			require.Equal(t, start.Add(5*time.Second), f.c.pathCredit.members[1].candidate.probe.validationDeadline)
			require.LessOrEqual(t, f.c.pathCredit.nextExpiry(), start.Add(5*time.Second))
			time.Sleep(5 * time.Second)
			f.pass(t)
			require.Contains(t, f.c.pathCredit.terminals, protocol.PathID(1))
			require.Equal(t, 1, admissionAbandons(f.peerWrites(t, 0), 1), "expiry closes a consumed ID on the survivor")
			require.False(t, f.c.pathCredit.consume(1), "the consumed ID is never reused")
			f.receive(t, 1, admissionTupleA, 100, &wire.PingFrame{})
			require.Nil(t, f.c.pathCredit.members[1].candidate, "traffic cannot revive it")
		})
	})
	t.Run("a response handled before expiry service", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveServer)
			f.supplyPeerCIDs(t)
			require.True(t, f.receive(t, 1, admissionTupleA, 1200, &wire.PingFrame{}))
			f.pass(t)
			challenge := admissionChallenges(f.peerWrites(t, 1))[0]
			time.Sleep(6 * time.Second)
			require.True(t, f.receive(t, 1, admissionTupleA, 100, &wire.PathResponseFrame{Data: challenge}))
			require.False(t, f.c.pathCredit.members[1].live, "the deadline is absolute")
			f.pass(t)
			require.Contains(t, f.c.pathCredit.terminals, protocol.PathID(1))
		})
	})
	t.Run("a validated tuple handled before expiry service", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveServer)
			require.True(t, f.receive(t, 1, admissionTupleA, 1200, &wire.PingFrame{}))
			require.Nil(t, f.c.pathCredit.members[1].candidate)
			time.Sleep(6 * time.Second)
			require.True(t, f.receive(t, 1, f.c.RemoteAddr(), 100, &wire.PingFrame{}))
			require.False(t, f.c.pathCredit.members[1].live, "every activation route honors the deadline")
			f.pass(t)
			require.Contains(t, f.c.pathCredit.terminals, protocol.PathID(1))
		})
	})
	t.Run("a committed path with no peer CID", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveServer)
			require.True(t, f.receive(t, 1, admissionTupleA, 1200, &wire.PingFrame{}))
			require.Nil(t, f.c.pathCredit.members[1].candidate)
			start := monotime.Now()
			require.Equal(t, start.Add(5*time.Second), f.c.pathCredit.nextExpiry())
			time.Sleep(3 * time.Second)
			f.supplyPeerCIDs(t)
			require.True(t, f.receive(t, 1, admissionTupleA, 1200, &wire.PingFrame{}))
			require.Equal(t, start.Add(5*time.Second), f.c.pathCredit.members[1].candidate.probe.validationDeadline, "a later CID or retry cannot refresh it")
			time.Sleep(2 * time.Second)
			f.pass(t)
			require.Contains(t, f.c.pathCredit.terminals, protocol.PathID(1))
			require.Equal(t, 1, admissionAbandons(f.peerWrites(t, 0), 1))
		})
	})
}

func TestPathAdmissionReceivedAbandonAwaitsFence(t *testing.T) {
	t.Run("a claimed validation send", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveServer)
			f.supplyPeerCIDs(t)
			f.socket.hold = nativeProbeHoldAll
			require.True(t, f.receive(t, 1, admissionTupleA, 1200, &wire.PingFrame{}))
			f.pass(t)
			require.Len(t, f.socket.writes, 1, "the validation send is claimed")
			cid := f.issuedCID(1)
			require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathAbandonFrame{PathID: 1}))
			terminal := f.c.pathCredit.terminals[1]
			require.False(t, terminal.signaled, "no reciprocal signal may precede the fence")
			f.pass(t)
			require.Len(t, f.socket.writes, 1, "the claimed send blocks the signal, and the probe budget is revoked")
			require.False(t, terminal.signaled)
			f.socket.hold = nil
			f.socket.release()
			f.pass(t)
			require.True(t, terminal.signaled)
			f.pass(t)
			require.Equal(t, 1, admissionAbandons(f.peerWrites(t, 0), 1))
			_, routed := f.route.Get(cid)
			require.True(t, routed, "routing outlives the signal")
			require.NotZero(t, terminal.expiry)
			require.NoError(t, f.c.pathCredit.expire(terminal.expiry))
			_, routed = f.route.Get(cid)
			require.False(t, routed)
			require.Zero(t, f.c.pathCredit.historyUnits)
		})
	})
	t.Run("a validation that left a retry claimed", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveServer)
			f.supplyPeerCIDs(t)
			require.True(t, f.receive(t, 1, admissionTupleA, 1200, &wire.PingFrame{}))
			f.pass(t)
			first := admissionChallenges(f.peerWrites(t, 1))[0]
			candidate := f.c.pathCredit.members[1].candidate
			f.socket.hold = nativeProbeHoldAll
			time.Sleep(time.Duration(candidate.probe.nextProbeTime - monotime.Now()))
			f.pass(t)
			require.Equal(t, 2, candidate.probe.numChallenges, "the retry is claimed")
			require.True(t, f.receive(t, 1, admissionTupleA, 100, &wire.PathResponseFrame{Data: first}))
			require.True(t, f.c.pathCredit.members[1].live)
			require.False(t, candidate.probe.probeEpoch.valid(), "success revokes the retry")
			require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathAbandonFrame{PathID: 1}))
			terminal := f.c.pathCredit.terminals[1]
			f.pass(t)
			require.False(t, terminal.signaled, "a dropped candidate's claim still fences")
			f.socket.hold = nil
			f.socket.release()
			f.pass(t)
			require.True(t, terminal.signaled)
		})
	})
	t.Run("retention that elapses during a claim", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveServer)
			f.supplyPeerCIDs(t)
			f.socket.hold = nativeProbeHoldAll
			require.True(t, f.receive(t, 1, admissionTupleA, 1200, &wire.PingFrame{}))
			f.pass(t)
			cid := f.issuedCID(1)
			require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathAbandonFrame{PathID: 1}))
			terminal := f.c.pathCredit.terminals[1]
			late := terminal.expiry.Add(time.Hour)
			for range 2 {
				require.NoError(t, f.c.pathCredit.expire(late))
			}
			require.False(t, terminal.expired)
			require.False(t, terminal.signaled)
			require.Contains(t, f.c.pathCredit.members, protocol.PathID(1))
			require.Equal(t, 1, f.c.pathCredit.historyUnits)
			_, routed := f.route.Get(cid)
			require.True(t, routed)
			require.NotEqual(t, terminal.expiry, f.c.pathCredit.nextExpiry(), "an elapsed retention timer cannot spin")
			f.socket.hold = nil
			f.socket.release()
			synctest.Wait()
			require.NoError(t, f.c.pathCredit.expire(late))
			require.True(t, terminal.signaled)
			require.True(t, terminal.expired)
			_, routed = f.route.Get(cid)
			require.False(t, routed)
			require.Zero(t, f.c.pathCredit.historyUnits)
		})
	})
}

func TestPathAdmissionResourceSaturation(t *testing.T) {
	t.Run("incoming history", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveServer)
			f.supplyPeerCIDs(t)
			f.c.pathCredit.historyUnits = maxPathHistoryUnits
			f.receive(t, 1, admissionTupleA, 1200, &wire.PingFrame{})
			require.NotContains(t, f.c.pathCredit.members, protocol.PathID(1))
			require.Contains(t, f.c.pathCredit.terminals, protocol.PathID(1), "declined through the terminal owner")
			require.Nil(t, f.c.sentPacketHandler.(interface {
				RecoveryOnPath(protocol.PathID) ackhandler.SentPacketHandler
			}).RecoveryOnPath(1))
		})
	})
	t.Run("local attachments precede routes", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			for range maxPathAttachments {
				f.c.pathCredit.attachments[&Transport{}] = &pathAttachment{refs: 1}
			}
			target := &Transport{ConnectionIDLength: f.tc.srcConnID.Len(), connIDLen: f.tc.srcConnID.Len(), handlers: make(map[protocol.ConnectionID]packetHandlerEntry), resetTokens: make(map[protocol.StatelessResetToken]packetHandler), logger: utils.DefaultLogger}
			target.initOnce.Do(func() {})
			target.responses = newEndpointResponses(startNativeProbeEndpoint(t, newNativeProbeSocket()))
			errs := f.openAsync(context.Background(), target, admissionTupleA)
			f.pass(t)
			require.ErrorIs(t, <-errs, ErrMultipathResourceLimitV1)
			require.Empty(t, (*packetHandlerMap)(target).handlers, "no route exists before admission")
			require.False(t, f.c.pathCredit.paths[1], "the ID is not consumed")
		})
	})
}

func (f *admissionFixture) openAsync(ctx context.Context, target *Transport, addr net.Addr) <-chan error {
	errs := make(chan error, 1)
	go func() {
		_, err := f.c.openPathPrivate(ctx, target, addr)
		errs <- err
	}()
	synctest.Wait()
	return errs
}

func TestPathAdmissionClientOpen(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newAdmissionFixture(t, protocol.PerspectiveClient)
		errs := f.openAsync(context.Background(), nil, admissionTupleA)
		f.pass(t)
		require.Empty(t, errs)
		require.False(t, f.c.pathCredit.paths[1], "waiting for peer CIDs consumes nothing")
		f.supplyPeerCIDs(t)
		f.pass(t)
		require.True(t, f.c.pathCredit.paths[1])
		require.True(t, f.c.connIDManager.pathCIDs[1].records[0].used, "registration records use")
		challenges := admissionChallenges(f.peerWrites(t, 1))
		require.Len(t, challenges, 1)
		require.GreaterOrEqual(t, len(f.peerWrites(t, 1)[0].data), 1200, "a client's probe is padded before any receipt")
		require.Empty(t, errs)
		require.True(t, f.receive(t, 1, admissionTupleA, 100, &wire.PathResponseFrame{Data: challenges[0]}))
		synctest.Wait()
		require.NoError(t, <-errs)
		require.True(t, f.c.pathCredit.members[1].live)
		// The peer's own validation is answered on the same path and tuple.
		require.True(t, f.receive(t, 1, admissionTupleA, 100, &wire.PathChallengeFrame{Data: [8]byte{7}}))
		f.pass(t)
		var answered bool
		for _, write := range f.peerWrites(t, 1) {
			for _, frame := range write.frames {
				if response, ok := frame.(*wire.PathResponseFrame); ok && response.Data == [8]byte{7} {
					answered = true
				}
			}
		}
		require.True(t, answered)
		// The bounded history can forget a live tuple, which keeps its authority.
		f.c.pathCredit.validated = nil
		f.socket.writes = nil
		require.True(t, f.receive(t, 1, admissionTupleA, 100, &wire.PathChallengeFrame{Data: [8]byte{8}}))
		f.pass(t)
		require.NotEmpty(t, f.peerWrites(t, 1), "a live path keeps answering on its tuple")
	})
}

func TestPathAdmissionTargetTransportRoutes(t *testing.T) {
	t.Run("a target that refuses attachment", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			endpoint := startNativeProbeEndpoint(t, newNativeProbeSocket())
			target := &Transport{ConnectionIDLength: f.tc.srcConnID.Len(), connIDLen: f.tc.srcConnID.Len(), handlers: make(map[protocol.ConnectionID]packetHandlerEntry), resetTokens: make(map[protocol.StatelessResetToken]packetHandler), logger: utils.DefaultLogger}
			target.initOnce.Do(func() {})
			target.responses = newEndpointResponses(endpoint)
			ctx, cancel := context.WithCancel(context.Background())
			errs := f.openAsync(ctx, target, admissionTupleA)
			defer func() {
				cancel()
				<-errs
			}()
			f.pass(t)
			require.Equal(t, 1, f.c.pathCredit.members[1].candidate.probe.numChallenges)
			endpoint.seal()
			require.True(t, f.receive(t, 1, admissionTupleA, 100, &wire.PathChallengeFrame{Data: [8]byte{3}}))
			retries := 0
			for ; retries < 4; retries++ {
				result := f.c.triggerSending(monotime.Now())
				require.NoError(t, result.err)
				if !result.retry {
					break
				}
			}
			require.Less(t, retries, 4, "a refused response cannot spin")
			require.Nil(t, f.c.pathCredit.members[1].response)
		})
	})
	t.Run("a target already sealed refuses before any route or ID", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			endpoint := startNativeProbeEndpoint(t, newNativeProbeSocket())
			target := &Transport{ConnectionIDLength: f.tc.srcConnID.Len(), connIDLen: f.tc.srcConnID.Len(), handlers: make(map[protocol.ConnectionID]packetHandlerEntry), resetTokens: make(map[protocol.StatelessResetToken]packetHandler)}
			target.initOnce.Do(func() {})
			target.responses = newEndpointResponses(endpoint)
			endpoint.seal()
			errs := f.openAsync(context.Background(), target, admissionTupleA)
			f.pass(t)
			require.ErrorIs(t, <-errs, net.ErrClosed)
			require.Empty(t, target.handlers)
			require.False(t, f.c.pathCredit.paths[1], "the ID is not consumed")
		})
	})
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newAdmissionFixture(t, protocol.PerspectiveClient)
		f.supplyPeerCIDs(t)
		targetSocket := newNativeProbeSocket()
		target := &Transport{ConnectionIDLength: f.tc.srcConnID.Len(), connIDLen: f.tc.srcConnID.Len(), handlers: make(map[protocol.ConnectionID]packetHandlerEntry), resetTokens: make(map[protocol.StatelessResetToken]packetHandler), logger: utils.DefaultLogger}
		target.initOnce.Do(func() {})
		target.responses = newEndpointResponses(startNativeProbeEndpoint(t, targetSocket))
		ctx, cancel := context.WithCancel(context.Background())
		errs := f.openAsync(ctx, target, admissionTupleA)
		f.pass(t)
		require.Empty(t, errs)
		defer func() {
			cancel()
			<-errs
		}()
		require.Len(t, targetSocket.writes, 1, "the probe uses the target endpoint")
		entry := (*packetHandlerMap)(target).handlers[f.issuedCID(1)]
		require.Equal(t, protocol.PathID(1), entry.pathID, "the target routes path CIDs with their binding")
		require.Equal(t, 1, f.c.pathCredit.attachments[target].refs)
	})
}

func TestPathAdmissionValidatedTupleAnnounces(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newAdmissionFixture(t, protocol.PerspectiveClient)
		f.supplyPeerCIDs(t)
		errs := f.openAsync(context.Background(), nil, f.c.RemoteAddr())
		f.pass(t)
		require.NoError(t, <-errs)
		require.True(t, f.c.pathCredit.members[1].live)
		writes := f.peerWrites(t, 1)
		require.Len(t, writes, 1)
		require.Empty(t, admissionChallenges(writes))
		require.IsType(t, &wire.PingFrame{}, writes[0].frames[0], "the peer needs one packet to learn the path")
	})
}

func TestPathAdmissionCancellation(t *testing.T) {
	t.Run("before an ID is committed", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			ctx, cancel := context.WithCancel(context.Background())
			errs := f.openAsync(ctx, nil, admissionTupleA)
			f.pass(t)
			cancel()
			require.ErrorIs(t, <-errs, context.Canceled)
			f.pass(t)
			require.False(t, f.c.pathCredit.paths[1], "no ID was consumed")
			require.NotContains(t, f.c.pathCredit.terminals, protocol.PathID(1))
			require.Nil(t, f.c.pathOpen.Load(), "the operation slot is free")
			require.Zero(t, f.c.pathCredit.historyUnits)
		})
	})
	t.Run("a response handled before cancellation service", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			ctx, cancel := context.WithCancel(context.Background())
			errs := f.openAsync(ctx, nil, admissionTupleA)
			f.pass(t)
			challenge := admissionChallenges(f.peerWrites(t, 1))[0]
			cancel()
			require.ErrorIs(t, <-errs, context.Canceled)
			require.True(t, f.receive(t, 1, admissionTupleA, 100, &wire.PathResponseFrame{Data: challenge}))
			require.False(t, f.c.pathCredit.members[1].live, "a canceled open never activates")
			f.pass(t)
			require.Contains(t, f.c.pathCredit.terminals, protocol.PathID(1))
		})
	})
	t.Run("a result published before cancellation stands", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			ctx, cancel := context.WithCancel(context.Background())
			request := &pathOpenRequest{ctx: ctx, addr: admissionTupleA, done: make(chan error, 1)}
			require.True(t, request.outcome.CompareAndSwap(pathOpenPending, pathOpenCanceled), "cancellation wins this request")
			f.c.pathOpen.Store(request)
			require.NoError(t, f.c.pathCredit.serviceRequest(monotime.Now()))
			require.Nil(t, f.c.pathOpen.Load())
			require.False(t, request.finish(nil), "a canceled caller is never told otherwise")
			require.Empty(t, request.done)
			errs := f.openAsync(ctx, nil, admissionTupleA)
			f.pass(t)
			require.True(t, f.receive(t, 1, admissionTupleA, 100, &wire.PathResponseFrame{Data: admissionChallenges(f.peerWrites(t, 1))[0]}))
			require.True(t, f.c.pathCredit.members[1].live)
			cancel()
			require.NoError(t, <-errs, "the published result stands")
		})
	})
	t.Run("after an ID is committed", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			f.socket.hold = nativeProbeHoldAll
			ctx, cancel := context.WithCancel(context.Background())
			errs := f.openAsync(ctx, nil, admissionTupleA)
			f.pass(t)
			require.Len(t, f.socket.writes, 1)
			cancel()
			require.ErrorIs(t, <-errs, context.Canceled, "the error asserts nothing about wire retirement")
			f.pass(t)
			terminal := f.c.pathCredit.terminals[1]
			require.NotNil(t, terminal)
			require.False(t, terminal.signaled, "the fence precedes the signal")
			require.NotNil(t, f.c.pathOpen.Load(), "and the operation slot stays charged")
			_, openErr := f.c.openPathPrivate(context.Background(), nil, admissionTupleB)
			require.ErrorIs(t, openErr, ErrMultipathBackpressureV1)
			f.socket.hold = nil
			f.socket.release()
			f.pass(t)
			f.pass(t)
			require.True(t, terminal.signaled)
			require.Equal(t, 1, admissionAbandons(f.peerWrites(t, 0), 1))
			require.False(t, f.c.pathCredit.consume(1), "the ID is never reused")
			require.Nil(t, f.c.pathOpen.Load())
		})
	})
}

func TestPathAdmissionClientDeclinesUnopenedID(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newAdmissionFixture(t, protocol.PerspectiveClient)
		f.receive(t, 1, admissionTupleA, 1200, &wire.PingFrame{})
		require.NotContains(t, f.c.pathCredit.members, protocol.PathID(1))
		require.Contains(t, f.c.pathCredit.terminals, protocol.PathID(1))
		f.pass(t)
		require.Equal(t, 1, admissionAbandons(f.peerWrites(t, 0), 1))
	})
}

func TestPathAdmissionNativeReceiptStartsTimer(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newAdmissionFixture(t, protocol.PerspectiveClient)
		f.receive(t, 1, admissionTupleA, 1200, &wire.PingFrame{})
		terminal := f.c.pathCredit.terminals[1]
		require.Zero(t, terminal.receipt.acceptedAt(), "registration is not acceptance")
		f.pass(t)
		f.pass(t)
		require.NotZero(t, terminal.receipt.acceptedAt(), "the endpoint's whole accepted datagram is")
		require.NotZero(t, terminal.unanswered)
		time.Sleep(31 * time.Second)
		synctest.Wait()
		var transportErr *qerr.TransportError
		require.ErrorAs(t, f.c.pathCredit.expire(monotime.Now()), &transportErr)
		require.Equal(t, qerr.InternalError, transportErr.ErrorCode)
	})
}

func TestPathAdmissionCloseAndStatus(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		f := newAdmissionFixture(t, protocol.PerspectiveClient)
		f.supplyPeerCIDs(t)
		// R8's surviving C-009: a known path's status is accepted, its policy is R15's.
		require.NoError(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathStatusFrame{PathID: 1, Backup: true}))
		var transportErr *qerr.TransportError
		require.ErrorAs(t, creditIngress(t, f.tc, monotime.Now(), &wire.PathStatusFrame{PathID: 9}), &transportErr)
		errs := f.openAsync(context.Background(), nil, admissionTupleA)
		f.pass(t)
		candidate := f.c.pathCredit.members[1].candidate
		f.c.pathCredit.close()
		require.ErrorIs(t, <-errs, net.ErrClosed)
		require.False(t, candidate.probe.probeEpoch.valid(), "close revokes queued probes")
		require.Nil(t, f.c.pathCredit.nextProbe(monotime.Now()))
		require.Nil(t, f.c.pathOpen.Load(), "close ends the local operation")
	})
}

// TestPathOpenFreesOperationBeforeSuccess holds an open's result on an unbuffered
// channel, where the old order would leave the caller's next operation refused.
func TestPathOpenFreesOperationBeforeSuccess(t *testing.T) {
	t.Run("a validated tuple", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			f.c.pathCredit.recordValidated(admissionTupleA)
			request := &pathOpenRequest{ctx: context.Background(), addr: admissionTupleA, done: make(chan error)}
			f.c.pathOpen.Store(request)
			go func() { _ = f.c.pathCredit.service(monotime.Now()) }()
			synctest.Wait()
			require.Nil(t, f.c.pathOpen.Load())
			require.True(t, f.c.pathCredit.members[request.id].live, "the path is live before the caller hears")
			require.NoError(t, <-request.done)
		})
	})
	t.Run("a response", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			request := &pathOpenRequest{ctx: context.Background(), addr: admissionTupleA, done: make(chan error)}
			f.c.pathOpen.Store(request)
			f.pass(t)
			challenges := admissionChallenges(f.peerWrites(t, 1))
			require.NotEmpty(t, challenges)
			p := f.c.pathCredit
			go p.pathResponse(&wire.PathResponseFrame{Data: challenges[0]}, request.id, monotime.Now())
			synctest.Wait()
			require.True(t, p.members[request.id].live, "the path is live before the caller hears")
			require.Nil(t, f.c.pathOpen.Load())
			require.NoError(t, <-request.done)
		})
	})
	t.Run("the connection's end", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			f := newAdmissionFixture(t, protocol.PerspectiveClient)
			f.supplyPeerCIDs(t)
			request := &pathOpenRequest{ctx: context.Background(), addr: admissionTupleA, done: make(chan error)}
			f.c.pathOpen.Store(request)
			go f.c.pathCredit.endOperation(net.ErrClosed)
			synctest.Wait()
			require.Nil(t, f.c.pathOpen.Load())
			require.ErrorIs(t, <-request.done, net.ErrClosed)
		})
	})
}
