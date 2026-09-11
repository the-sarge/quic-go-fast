package quic

import (
	"bytes"
	"testing"

	"github.com/quic-go/quic-go/internal/ackhandler"
	mockackhandler "github.com/quic-go/quic-go/internal/mocks/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

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
	hdrLen, _, _, _, err := wire.ParseShortHeader(packet, tc.conn.connIDManager.Get().Len())
	require.NoError(t, err)
	payload := packet[hdrLen : len(packet)-7]
	parser := wire.NewFrameParser(true, false, false)
	typ, n, err := parser.ParseType(payload, protocol.Encryption1RTT)
	require.NoError(t, err)
	require.True(t, typ.IsDatagramFrameType())
	f, m, err := parser.ParseDatagramFrame(typ, payload[n:], protocol.Version1)
	require.NoError(t, err)
	require.Equal(t, len(payload), n+m)
	return f.Data
}

// Decode control output with the existing wire parser; transparent protection
// contributes the same seven-byte tag used by the packer fixtures.
func schedulingFrames(t *testing.T, tc *testConnection, packet []byte) []wire.Frame {
	t.Helper()
	n, _, _, _, err := wire.ParseShortHeader(packet, tc.conn.connIDManager.Get().Len())
	require.NoError(t, err)
	payload := packet[n : len(packet)-7]
	parser := wire.NewFrameParser(true, false, false)
	var frames []wire.Frame
	for len(payload) > 0 {
		typ, n, err := parser.ParseType(payload, protocol.Encryption1RTT)
		require.NoError(t, err)
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
		require.NoError(t, err)
		frames = append(frames, f)
		payload = payload[m:]
	}
	return frames
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

func schedulingGSODatagrams(t *testing.T, tc *testConnection, batch []byte, segment uint16) [][]byte {
	t.Helper()
	require.Positive(t, segment)
	var payloads [][]byte
	for len(batch) > 0 {
		n := min(len(batch), int(segment))
		payloads = append(payloads, schedulingDatagram(t, tc, batch[:n]))
		batch = batch[n:]
	}
	return payloads
}
