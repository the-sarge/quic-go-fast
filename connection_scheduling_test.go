package quic

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/quic-go/quic-go/internal/ackhandler"
	mockackhandler "github.com/quic-go/quic-go/internal/mocks/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestSchedulingPacketPayloadTruncated(t *testing.T) {
	packet, err := wire.AppendShortHeader(nil, protocol.ConnectionID{}, 1, protocol.PacketNumberLen2, protocol.KeyPhaseZero)
	require.NoError(t, err)
	packet = append(packet, make([]byte, 6)...)
	_, err = schedulingPacketPayload(packet, 0)
	require.ErrorContains(t, err, "protection tag")
}

func TestSchedulingDatagramMalformed(t *testing.T) {
	_, err := decodeSchedulingDatagram(nil, 0)
	require.Error(t, err)
	for _, test := range []struct {
		name    string
		payload []byte
		message string
	}{
		{name: "wrong frame", payload: []byte{0x01}, message: "expected DATAGRAM"},
		{name: "truncated data", payload: []byte{0x31, 0x02, 'a'}, message: "EOF"},
		{name: "trailing data", payload: []byte{0x31, 0x01, 'a', 'b'}, message: "consumed 3 of 4"},
	} {
		t.Run(test.name, func(t *testing.T) {
			packet, err := wire.AppendShortHeader(nil, protocol.ConnectionID{}, 1, protocol.PacketNumberLen2, protocol.KeyPhaseZero)
			require.NoError(t, err)
			packet = append(packet, test.payload...)
			packet = append(packet, make([]byte, 7)...)
			_, err = decodeSchedulingDatagram(packet, 0)
			require.ErrorContains(t, err, test.message)
		})
	}
}

func TestSchedulingFramesMalformed(t *testing.T) {
	_, err := decodeSchedulingFrames(nil, 0)
	require.Error(t, err)
	packet, err := wire.AppendShortHeader(nil, protocol.ConnectionID{}, 1, protocol.PacketNumberLen2, protocol.KeyPhaseZero)
	require.NoError(t, err)
	// PATH_CHALLENGE requires eight data bytes.
	packet = append(packet, 0x1a, 1)
	packet = append(packet, make([]byte, 7)...)
	_, err = decodeSchedulingFrames(packet, 0)
	require.ErrorContains(t, err, "EOF")
}

func TestSchedulingProbeMalformed(t *testing.T) {
	_, err := decodeSchedulingProbe(nil, protocol.ConnectionID{})
	require.Error(t, err)
	for _, test := range []struct {
		name    string
		payload []byte
		message string
	}{
		{name: "missing frame", message: "expected one PATH_CHALLENGE"},
		{name: "wrong frame", payload: []byte{0x01}, message: "expected PATH_CHALLENGE"},
	} {
		t.Run(test.name, func(t *testing.T) {
			packet, err := wire.AppendShortHeader(nil, protocol.ConnectionID{}, 1, protocol.PacketNumberLen2, protocol.KeyPhaseZero)
			require.NoError(t, err)
			packet = append(packet, test.payload...)
			packet = append(packet, make([]byte, 7)...)
			_, err = decodeSchedulingProbe(packet, protocol.ConnectionID{})
			require.ErrorContains(t, err, test.message)
		})
	}
}

// The real packer fixtures use transparent protection with a seven-byte tag.
// Decode only a snapshot and metadata captured by the protocol-state owner.
func schedulingPacketPayload(packet []byte, connIDLen int) ([]byte, error) {
	hdrLen, _, _, _, err := wire.ParseShortHeader(packet, connIDLen)
	if err != nil {
		return nil, err
	}
	if len(packet)-hdrLen < 7 {
		return nil, fmt.Errorf("packet is missing the seven-byte protection tag")
	}
	return packet[hdrLen : len(packet)-7], nil
}

// Only scheduling inputs are controlled. Packet numbers, registration and
// recovery storage remain with the connection's real recovery handler.
type schedulingRecoveryInputs struct {
	ackhandler.SentPacketHandler
	inputs ackhandler.SentPacketHandler
}

func (h schedulingRecoveryInputs) SendMode(now monotime.Time) ackhandler.SendMode {
	return h.inputs.SendMode(now)
}
func (h schedulingRecoveryInputs) TimeUntilSend() monotime.Time { return h.inputs.TimeUntilSend() }
func (h schedulingRecoveryInputs) GetLossDetectionTimeout() monotime.Time {
	return h.inputs.GetLossDetectionTimeout()
}

func (h schedulingRecoveryInputs) ECNMode(short bool) protocol.ECN { return h.inputs.ECNMode(short) }

func useSchedulingPacketPacker(t *testing.T, ctrl *gomock.Controller, tc *testConnection, inputs ackhandler.SentPacketHandler) {
	t.Helper()
	c := tc.conn
	if inputs != nil {
		c.sentPacketHandler = schedulingRecoveryInputs{SentPacketHandler: c.sentPacketHandler, inputs: inputs}
	}
	useLifecyclePacketPacker(t, ctrl, tc)
	c.sentPacketHandler.ReceivedBytes(1<<20, monotime.Now())
	if c.handshakeConfirmed {
		c.sentPacketHandler.DropPackets(protocol.EncryptionInitial, monotime.Now())
		c.sentPacketHandler.DropPackets(protocol.EncryptionHandshake, monotime.Now())
	}
}

func schedulingDatagram(t *testing.T, tc *testConnection, packet []byte) []byte {
	t.Helper()
	data, err := decodeSchedulingDatagram(packet, tc.conn.connIDManager.Get().Len())
	require.NoError(t, err)
	return data
}

type schedulingWriteObservation struct {
	data    []byte
	segment uint16
	ecn     protocol.ECN
}

func decodeSchedulingDatagram(packet []byte, connIDLen int) ([]byte, error) {
	payload, err := schedulingPacketPayload(packet, connIDLen)
	if err != nil {
		return nil, err
	}
	parser := wire.NewFrameParser(true, false, false)
	typ, n, err := parser.ParseType(payload, protocol.Encryption1RTT)
	if err != nil {
		return nil, err
	}
	if !typ.IsDatagramFrameType() {
		return nil, fmt.Errorf("expected DATAGRAM frame, got %d", typ)
	}
	f, m, err := parser.ParseDatagramFrame(typ, payload[n:], protocol.Version1)
	if err != nil {
		return nil, err
	}
	if n+m != len(payload) {
		return nil, fmt.Errorf("DATAGRAM consumed %d of %d payload bytes", n+m, len(payload))
	}
	return f.Data, nil
}

// Decode control output with the existing wire parser; transparent protection
// contributes the same seven-byte tag used by the packer fixtures.
func schedulingFrames(t *testing.T, tc *testConnection, packet []byte) []wire.Frame {
	t.Helper()
	frames, err := decodeSchedulingFrames(packet, tc.conn.connIDManager.Get().Len())
	require.NoError(t, err)
	return frames
}

func decodeSchedulingFrames(packet []byte, connIDLen int) ([]wire.Frame, error) {
	payload, err := schedulingPacketPayload(packet, connIDLen)
	if err != nil {
		return nil, err
	}
	parser := wire.NewFrameParser(true, false, false)
	var frames []wire.Frame
	for len(payload) > 0 {
		typ, n, err := parser.ParseType(payload, protocol.Encryption1RTT)
		if err != nil {
			return nil, err
		}
		payload = payload[n:]
		if typ == 0 {
			continue
		}
		var f wire.Frame
		var m int
		if typ.IsAckFrameType() {
			f, m, err = parser.ParseAckFrame(typ, payload, protocol.Encryption1RTT, protocol.Version1)
		} else {
			f, m, err = parser.ParseLessCommonFrame(typ, payload, protocol.Version1)
		}
		if err != nil {
			return nil, err
		}
		frames = append(frames, f)
		payload = payload[m:]
	}
	return frames, nil
}

func decodeSchedulingProbe(packet []byte, destConnID protocol.ConnectionID) (*wire.PathChallengeFrame, error) {
	if len(packet) < 1+destConnID.Len() {
		return nil, fmt.Errorf("probe is missing destination connection ID")
	}
	if !bytes.Equal(destConnID.Bytes(), packet[1:1+destConnID.Len()]) {
		return nil, fmt.Errorf("probe destination connection ID mismatch")
	}
	frames, err := decodeSchedulingFrames(packet, destConnID.Len())
	if err != nil {
		return nil, err
	}
	if len(frames) != 1 {
		return nil, fmt.Errorf("expected one PATH_CHALLENGE frame, got %d frames", len(frames))
	}
	challenge, ok := frames[0].(*wire.PathChallengeFrame)
	if !ok {
		return nil, fmt.Errorf("expected PATH_CHALLENGE frame, got %T", frames[0])
	}
	return challenge, nil
}

func newGSOBatchTestConnection(t *testing.T) (*testConnection, *mockackhandler.MockSentPacketHandler) {
	t.Helper()
	ctrl := gomock.NewController(t)
	sph := mockackhandler.NewMockSentPacketHandler(ctrl)
	tc := newServerTestConnection(t, ctrl, nil, true, connectionOptHandshakeConfirmed())
	useSchedulingPacketPacker(t, ctrl, tc, sph)
	sph.EXPECT().SendMode(gomock.Any()).Return(ackhandler.SendAny).AnyTimes()
	sph.EXPECT().TimeUntilSend().AnyTimes()
	sph.EXPECT().GetLossDetectionTimeout().AnyTimes()
	return tc, sph
}

func gsoDatagramPayloadSize(tc *testConnection) int {
	_, pnLen := tc.conn.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
	// Transparent protection adds a seven-byte tag; these DATAGRAM frames
	// use a one-byte type and a two-byte length.
	return int(tc.conn.maxPacketSize()-wire.ShortHeaderLen(tc.conn.connIDManager.Get(), pnLen)) - 7 - 3
}

func queueGSODatagrams(t *testing.T, tc *testConnection, payloadSizes ...int) [][]byte {
	t.Helper()
	var payloads [][]byte
	for i, size := range payloadSizes {
		data := bytes.Repeat([]byte{byte(i)}, size)
		payloads = append(payloads, data)
		require.NoError(t, tc.conn.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: data}))
	}
	return payloads
}

func schedulingGSODatagrams(t *testing.T, connIDLen int, batch []byte, segment uint16) [][]byte {
	t.Helper()
	require.Positive(t, segment)
	var payloads [][]byte
	for len(batch) > 0 {
		n := min(len(batch), int(segment))
		data, err := decodeSchedulingDatagram(batch[:n], connIDLen)
		require.NoError(t, err)
		payloads = append(payloads, data)
		batch = batch[n:]
	}
	return payloads
}
