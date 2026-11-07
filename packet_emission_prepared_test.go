package quic

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestPreparedPackerMatchesValueBytes(t *testing.T) {
	var encoded [][]byte
	for _, preparedPath := range []bool{false, true} {
		ctrl := gomock.NewController(t)
		tp := newTestPacketPacker(t, ctrl, protocol.PerspectiveServer)
		tp.pnManager.EXPECT().PeekPacketNumber(protocol.Encryption1RTT).Return(protocol.PacketNumber(42), protocol.PacketNumberLen2)
		tp.pnManager.EXPECT().PopPacketNumber(protocol.Encryption1RTT).Return(protocol.PacketNumber(42))
		tp.sealingManager.EXPECT().Get1RTTSealer().Return(newMockShortHeaderSealer(ctrl), nil)
		tp.framer.EXPECT().HasData().Return(true)
		ack := &wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: 7, Largest: 9}}}
		tp.ackFramer.EXPECT().GetAckFrame(protocol.Encryption1RTT, gomock.Any(), false).Return(ack)
		stream := &wire.StreamFrame{StreamID: 4, DataLenPresent: true, Data: []byte("construction facts")}
		expectAppendFrames(tp.framer, nil, []ackhandler.StreamFrame{{Frame: stream}})
		buf := getPacketBuffer()
		if preparedPath {
			prepared := ackhandler.PreparePacket()
			var header nativeHeaderFacts
			require.NoError(t, tp.packer.appendPreparedPacket(nil, buf, 1200, false, monotime.Now(), protocol.Version1, &prepared, &header))
			require.EqualValues(t, 42, header.packetNumber)
			require.Same(t, ack, header.ack)
			require.Same(t, stream, prepared.View().StreamFrames()[0].Frame)
			require.Equal(t, buf.Len(), prepared.View().Length())
			prepared.Discard()
		} else {
			_, err := tp.packer.AppendPacket(buf, 1200, monotime.Now(), protocol.Version1)
			require.NoError(t, err)
		}
		encoded = append(encoded, bytes.Clone(buf.Data))
		buf.Release()
	}
	require.Equal(t, encoded[0], encoded[1])
}

// Only the registration hook is replaced; the packer and recovery are real.
type preparedTestLocal struct {
	localPacketSender
	onRegister func(ackhandler.LocalCompletionToken, bool)
}

func (s *preparedTestLocal) register(token ackhandler.LocalCompletionToken, datagram bool) {
	s.onRegister(token, datagram)
}

type preparedTestPolicy struct {
	emissionPolicy
	onLog          func(ackhandler.PreparedPacketView, nativeHeaderFacts)
	onActivity     func(ackhandler.PreparedPacketView)
	onRegistration func()
}

func (p *preparedTestPolicy) logPreparedShortHeaderPacket(v ackhandler.PreparedPacketView, h nativeHeaderFacts, _ protocol.ECN, _ protocol.ByteCount) {
	p.onLog(v, h)
}

func (p *preparedTestPolicy) notePreparedEmissionActivity(v ackhandler.PreparedPacketView, _ monotime.Time) {
	p.onActivity(v)
}
func (p *preparedTestPolicy) noteEmissionRegistration() { p.onRegistration() }

func TestPreparedEmissionResolvesReceiversAfterCallbacks(t *testing.T) {
	c := newEmissionTestConnection(t, false).conn
	original := c.sentPacketHandler
	replacement, err := ackhandler.EnableLocalCompletions(original)
	require.NoError(t, err)
	c.sentPacketHandler = replacement
	var seen []string
	wrong := &preparedTestLocal{onRegister: func(ackhandler.LocalCompletionToken, bool) { t.Fatal("captured stale local receiver") }}
	c.emission.local = wrong
	require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("real prepared packet")}))
	var token ackhandler.LocalCompletionToken
	c.emission.policy = &preparedTestPolicy{
		emissionPolicy: c.emission.policy,
		onLog: func(v ackhandler.PreparedPacketView, _ nativeHeaderFacts) {
			seen = append(seen, "log")
			require.NotEmpty(t, v.Frames())
		},
		onActivity: func(ackhandler.PreparedPacketView) {
			seen = append(seen, "activity")
			// Replacement happens at the original pre-registration policy boundary.
			fresh, err := ackhandler.NewLocalSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, false, nil, protocol.PerspectiveServer, nil, utils.DefaultLogger)
			require.NoError(t, err)
			c.sentPacketHandler = fresh
			c.emission.local = &preparedTestLocal{onRegister: func(tok ackhandler.LocalCompletionToken, datagram bool) {
				seen = append(seen, "local")
				token = tok
				require.True(t, datagram)
			}}
		},
		onRegistration: func() { seen = append(seen, "registration") },
	}
	buf := getPacketBuffer()
	defer buf.Release()
	receipt, err := c.emission.appendPacket(buf, 1200, false, protocol.ECNNon, monotime.Now())
	require.NoError(t, err)
	require.Equal(t, []string{"log", "activity", "local", "registration"}, seen)
	require.True(t, carriesDatagram(receipt.frames))
	require.Equal(t, buf.Len(), receipt.size)
	fresh := c.sentPacketHandler.(ackhandler.LocalSentPacketHandler)
	require.NoError(t, fresh.CompleteLocalPacket(token, ackhandler.LocalPacketUnsubmitted, monotime.Now()))
}

func TestPreparedEmissionCanBecomeNonlocalAtActivity(t *testing.T) {
	c := newEmissionTestConnection(t, false).conn
	c.emission.local = &preparedTestLocal{onRegister: func(ackhandler.LocalCompletionToken, bool) { t.Fatal("nonlocal registration reached local receiver") }}
	require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("nonlocal after callback")}))
	c.emission.policy = &preparedTestPolicy{
		emissionPolicy: c.emission.policy,
		onLog:          func(ackhandler.PreparedPacketView, nativeHeaderFacts) {},
		onActivity:     func(ackhandler.PreparedPacketView) { c.emission.local = nil },
		onRegistration: func() {},
	}
	buf := getPacketBuffer()
	defer buf.Release()
	_, err := c.emission.appendPacket(buf, 1200, false, protocol.ECNNon, monotime.Now())
	require.NoError(t, err)
	require.Positive(t, c.connStats.BytesSent.Load())
}

type preparedTestLogger struct {
	utils.Logger
	onDebug func()
}

func (l *preparedTestLogger) Debug() bool {
	if l.onDebug != nil {
		callback := l.onDebug
		l.onDebug = nil
		callback()
	}
	return false
}

func TestPreparedEmissionKeepsLateDatagramObservations(t *testing.T) {
	c := newEmissionTestConnection(t, false).conn
	now := monotime.Now()
	var frames []ackhandler.Frame
	original := &wire.DatagramFrame{DataLenPresent: true, Data: []byte("observe twice")}
	require.NoError(t, c.datagramQueue.Add(original))
	c.emission.local = &preparedTestLocal{onRegister: func(ackhandler.LocalCompletionToken, bool) { t.Fatal("stale post-callback local receiver") }}
	logger := &preparedTestLogger{Logger: utils.DefaultLogger}
	local, err := ackhandler.NewLocalSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, false, nil, protocol.PerspectiveServer, nil, logger)
	require.NoError(t, err)
	c.sentPacketHandler = local
	var token ackhandler.LocalCompletionToken
	c.emission.policy = &preparedTestPolicy{
		emissionPolicy: c.emission.policy,
		onLog:          func(v ackhandler.PreparedPacketView, _ nativeHeaderFacts) { frames = v.Frames() },
		onActivity:     func(ackhandler.PreparedPacketView) {},
		onRegistration: func() {},
	}
	logger.onDebug = func() {
		require.Same(t, original, frames[0].Frame)
		frames[0].Frame = &wire.PingFrame{}
		c.emission.local = &preparedTestLocal{onRegister: func(tok ackhandler.LocalCompletionToken, datagram bool) {
			token = tok
			require.False(t, datagram, "registration scans the original slice after recovery callbacks")
			frames[0].Frame = original
		}}
	}
	buf := getPacketBuffer()
	defer buf.Release()
	receipt, err := c.emission.appendPacket(buf, 1200, false, protocol.ECNNon, now)
	require.NoError(t, err)
	require.Same(t, &frames[0], &receipt.frames[0])
	require.True(t, carriesDatagram(receipt.frames), "GSO's later scan still observes the original backing")
	require.NoError(t, local.CompleteLocalPacket(token, ackhandler.LocalPacketUnsubmitted, now.Add(time.Millisecond)))
}

func TestPreparedPackerFailurePreservesRegisteredPrefix(t *testing.T) {
	c := newEmissionTestConnection(t, false).conn
	local, err := ackhandler.EnableLocalCompletions(c.sentPacketHandler)
	require.NoError(t, err)
	c.sentPacketHandler = local
	var tokens []ackhandler.LocalCompletionToken
	c.emission.local = &preparedTestLocal{onRegister: func(tok ackhandler.LocalCompletionToken, _ bool) { tokens = append(tokens, tok) }}
	now := monotime.Now()
	buf := getPacketBuffer()
	defer buf.Release()
	require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("registered prefix")}))
	first, err := c.emission.appendPacket(buf, 1200, false, protocol.ECNNon, now)
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	prefix := bytes.Clone(buf.Data)
	require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("failed suffix")}))
	c.emission.packer.pnManager = mismatchedConstructionNumber{local}
	_, err = c.emission.appendPacket(buf, 1200, false, protocol.ECNNon, now)
	require.ErrorContains(t, err, "Peeked and Popped")
	require.Len(t, tokens, 1)
	require.Equal(t, prefix, buf.Data[:first.size])
	require.NoError(t, local.CompleteLocalPacket(tokens[0], ackhandler.LocalPacketUnsubmitted, now.Add(time.Millisecond)))
}

type preparedTestACKSource struct{ ack *wire.AckFrame }

func (s preparedTestACKSource) GetAckFrame(protocol.EncryptionLevel, monotime.Time, bool) *wire.AckFrame {
	return s.ack
}

func TestPreparedEmissionReadsACKCarrierAfterActivityAndSurvivesEarlyACK(t *testing.T) {
	c := newEmissionTestConnection(t, false).conn
	now := monotime.Now()
	var ignored []protocol.PacketNumber
	local, err := ackhandler.NewLocalSentPacketHandler(0, 1200, utils.NewRTTStats(), &utils.ConnectionStats{}, true, false, func(_ protocol.PathID, below protocol.PacketNumber) { ignored = append(ignored, below) }, protocol.PerspectiveServer, nil, utils.DefaultLogger)
	require.NoError(t, err)
	c.sentPacketHandler = local
	ack := &wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: 7, Largest: 9}}}
	c.emission.packer.acks = preparedTestACKSource{ack: ack}
	require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("early ACK storage lifetime")}))
	c.emission.policy = &preparedTestPolicy{
		emissionPolicy: c.emission.policy,
		onLog:          func(ackhandler.PreparedPacketView, nativeHeaderFacts) {},
		onActivity:     func(ackhandler.PreparedPacketView) { ack.AckRanges[0].Largest = 19 },
		onRegistration: func() { require.Equal(t, []protocol.PacketNumber{20}, ignored) },
	}
	c.emission.local = &preparedTestLocal{onRegister: func(token ackhandler.LocalCompletionToken, datagram bool) {
		require.True(t, datagram)
		// ACK retires and pools the recovery packet before emission returns.
		_, err := local.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: 0, Largest: 0}}}, protocol.Encryption1RTT, now.Add(time.Millisecond))
		require.NoError(t, err)
		require.NoError(t, local.CompleteLocalPacket(token, ackhandler.LocalPacketAccepted, now.Add(2*time.Millisecond)))
	}}
	buf := getPacketBuffer()
	defer buf.Release()
	receipt, err := c.emission.appendPacket(buf, 1200, false, protocol.ECNNon, now)
	require.NoError(t, err)
	require.True(t, carriesDatagram(receipt.frames))
}

type preparedSealFailure struct {
	handshake.ShortHeaderSealer
	err error
}

func (s preparedSealFailure) Seal([]byte, []byte, protocol.PacketNumber, []byte) ([]byte, error) {
	return nil, s.err
}

func TestPreparedPackerSealRefusalRetainsPreparation(t *testing.T) {
	ctrl := gomock.NewController(t)
	tp := newTestPacketPacker(t, ctrl, protocol.PerspectiveServer)
	sentinel := errors.New("seal refusal")
	tp.pnManager.EXPECT().PeekPacketNumber(protocol.Encryption1RTT).Return(protocol.PacketNumber(42), protocol.PacketNumberLen2)
	tp.sealingManager.EXPECT().Get1RTTSealer().Return(preparedSealFailure{ShortHeaderSealer: newMockShortHeaderSealer(ctrl), err: sentinel}, nil)
	tp.framer.EXPECT().HasData().Return(true)
	tp.ackFramer.EXPECT().GetAckFrame(protocol.Encryption1RTT, gomock.Any(), false)
	frame := &wire.StreamFrame{StreamID: 4, DataLenPresent: true, Data: []byte("refused protection")}
	expectAppendFrames(tp.framer, nil, []ackhandler.StreamFrame{{Frame: frame}})
	prepared := ackhandler.PreparePacket()
	defer prepared.Discard()
	var header nativeHeaderFacts
	buf := getPacketBuffer()
	defer buf.Release()
	err := tp.packer.appendPreparedPacket(nil, buf, 1200, false, monotime.Now(), protocol.Version1, &prepared, &header)
	require.ErrorIs(t, err, sentinel)
	require.Same(t, frame, prepared.View().StreamFrames()[0].Frame)
	// No PopPacketNumber expectation: a refused seal cannot commit the number.
}

func TestPreparedEmissionShortDatagramContinuation(t *testing.T) {
	c := newEmissionTestConnection(t, true).conn
	local, err := ackhandler.EnableLocalCompletions(c.sentPacketHandler)
	require.NoError(t, err)
	c.sentPacketHandler = local
	var tokens []ackhandler.LocalCompletionToken
	c.emission.local = &preparedTestLocal{onRegister: func(token ackhandler.LocalCompletionToken, _ bool) { tokens = append(tokens, token) }}
	preparedPackets := 0
	c.emission.policy = &preparedTestPolicy{
		emissionPolicy: c.emission.policy,
		onLog:          func(ackhandler.PreparedPacketView, nativeHeaderFacts) { preparedPackets++ },
		onActivity:     func(ackhandler.PreparedPacketView) {},
		onRegistration: func() {},
	}
	now := monotime.Now()
	buf := getPacketBuffer()
	defer buf.Release()
	batch := newGSOBatch(1200, true)
	for i := range 2 {
		require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("equal short segments")}))
		size, datagramOnly := batch.next()
		require.Equal(t, i == 1, datagramOnly)
		receipt, err := c.emission.appendPacket(buf, size, datagramOnly, protocol.ECNNon, now)
		require.NoError(t, err)
		require.True(t, batch.add(receipt.size, receipt.frames))
	}
	tooLarge := &wire.DatagramFrame{DataLenPresent: true, Data: make([]byte, 100)}
	require.NoError(t, c.datagramQueue.Add(tooLarge))
	limit, datagramOnly := batch.next()
	before := buf.Len()
	_, err = c.emission.appendPacket(buf, limit, datagramOnly, protocol.ECNNon, now)
	require.ErrorIs(t, err, errNothingToPack)
	require.Same(t, tooLarge, c.datagramQueue.Peek(), "a refused continuation retains queue ownership")
	require.Equal(t, before, buf.Len())
	nextPN, _ := local.PeekPacketNumber(protocol.Encryption1RTT)
	require.EqualValues(t, 2, nextPN, "refusal commits no packet number")
	require.Len(t, tokens, 2)
	require.Equal(t, 2, preparedPackets, "the short DATAGRAM continuation transfers preparation-owned facts too")
	for _, token := range tokens {
		require.NoError(t, local.CompleteLocalPacket(token, ackhandler.LocalPacketUnsubmitted, now.Add(time.Millisecond)))
	}
}
