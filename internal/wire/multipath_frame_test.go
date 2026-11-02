package wire

import (
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/quicvarint"
	"github.com/stretchr/testify/require"
)

func TestMultipathMaxPathIDRoundTrip(t *testing.T) {
	parser := NewFrameParser(false, false, false)
	parser.EnableMultipath()
	f := &MaxPathIDFrame{MaximumPathID: 64}
	b, err := f.Append(nil, protocol.Version1)
	require.NoError(t, err)
	require.Equal(t, []byte{0x7e, 0x7a, 0x40, 0x40}, b)
	require.Equal(t, protocol.ByteCount(len(b)), f.Length(protocol.Version1))
	typ, n, err := parser.ParseType(b, protocol.Encryption1RTT)
	require.NoError(t, err)
	decoded, consumed, err := parser.ParseLessCommonFrame(typ, b[n:], protocol.Version1)
	require.NoError(t, err)
	require.Equal(t, f, decoded)
	require.Equal(t, len(b), n+consumed)
}

func TestMultipathParserGates(t *testing.T) {
	b := []byte{0x7e, 0x7a, 1}
	parser := NewFrameParser(false, false, false)
	_, _, err := parser.ParseType(b, protocol.Encryption1RTT)
	requireTransportError(t, err, qerr.FrameEncodingError, FrameTypeMaxPathID)
	_, _, err = parser.ParseLessCommonFrame(FrameTypeMaxPathID, b[2:], protocol.Version1)
	requireTransportError(t, err, qerr.FrameEncodingError, FrameTypeMaxPathID)
	parser.EnableMultipath()
	for _, level := range []protocol.EncryptionLevel{protocol.EncryptionInitial, protocol.EncryptionHandshake, protocol.Encryption0RTT} {
		_, _, err = parser.ParseType(b, level)
		requireTransportError(t, err, qerr.ProtocolViolation, FrameTypeMaxPathID)
	}
	t.Run("disabledPATH_ACK", func(t *testing.T) {
		parser := NewFrameParser(false, false, false)
		_, _, err := parser.ParseAckFrame(FrameTypePathAck, []byte{3, 10, 0, 0, 0}, protocol.Encryption1RTT, protocol.Version1)
		requireTransportError(t, err, qerr.FrameEncodingError, FrameTypePathAck)
	})
	t.Run("wrongLevelPATH_ACK", func(t *testing.T) {
		parser := NewFrameParser(false, false, false)
		parser.EnableMultipath()
		_, _, err := parser.ParseAckFrame(FrameTypePathAck, []byte{3, 10, 0, 0, 0}, protocol.EncryptionHandshake, protocol.Version1)
		requireTransportError(t, err, qerr.ProtocolViolation, FrameTypePathAck)
	})
}

func TestMultipathPathIDBounds(t *testing.T) {
	parser := NewFrameParser(false, false, false)
	parser.EnableMultipath()
	b := quicvarint.Append(nil, 1<<32)
	_, _, err := parser.ParseLessCommonFrame(FrameTypeMaxPathID, b, protocol.Version1)
	requireTransportError(t, err, qerr.ProtocolViolation, FrameTypeMaxPathID)
	b = quicvarint.Append(nil, 1<<32-1)
	f, n, err := parser.ParseLessCommonFrame(FrameTypeMaxPathID, b, protocol.Version1)
	require.NoError(t, err)
	require.Equal(t, len(b), n)
	require.Equal(t, &MaxPathIDFrame{MaximumPathID: protocol.MaxPathID}, f)
}

func requireTransportError(t *testing.T, err error, code qerr.TransportErrorCode, typ FrameType) {
	t.Helper()
	var transportErr *qerr.TransportError
	require.ErrorAs(t, err, &transportErr)
	require.Equal(t, code, transportErr.ErrorCode)
	require.Equal(t, uint64(typ), transportErr.FrameType)
}

func TestMultipathPathAbandonRoundTrip(t *testing.T) {
	f := &PathAbandonFrame{PathID: 3, ErrorCode: 62}
	checkMultipathFrame(t, f, []byte{0x7e, 0x75, 3, 62})
}

func checkMultipathFrame(t *testing.T, f Frame, expected []byte) {
	t.Helper()
	b, err := f.Append(nil, protocol.Version1)
	require.NoError(t, err)
	require.Equal(t, expected, b)
	require.Equal(t, protocol.ByteCount(len(b)), f.Length(protocol.Version1))
	parser := NewFrameParser(false, false, false)
	parser.EnableMultipath()
	typ, n, err := parser.ParseType(b, protocol.Encryption1RTT)
	require.NoError(t, err)
	decoded, consumed, err := parser.ParseLessCommonFrame(typ, b[n:], protocol.Version1)
	require.NoError(t, err)
	require.Equal(t, f, decoded)
	require.Equal(t, len(b), n+consumed)
}

func TestMultipathPathStatusRoundTrip(t *testing.T) {
	checkMultipathFrame(t, &PathStatusFrame{PathID: 3, SequenceNumber: 9, Backup: true}, []byte{0x7e, 0x76, 3, 9})
	checkMultipathFrame(t, &PathStatusFrame{PathID: 3, SequenceNumber: 10}, []byte{0x7e, 0x77, 3, 10})
}

func TestMultipathPathNewConnectionIDRoundTrip(t *testing.T) {
	f := &PathNewConnectionIDFrame{PathID: 3, SequenceNumber: 2, RetirePriorTo: 1, ConnectionID: protocol.ParseConnectionID([]byte{0xde, 0xad, 0xbe, 0xef})}
	b := append([]byte{0x7e, 0x78, 3, 2, 1, 4, 0xde, 0xad, 0xbe, 0xef}, make([]byte, 16)...)
	checkMultipathFrame(t, f, b)
}

func TestMultipathPathRetireConnectionIDRoundTrip(t *testing.T) {
	checkMultipathFrame(t, &PathRetireConnectionIDFrame{PathID: 3, SequenceNumber: 2}, []byte{0x7e, 0x79, 3, 2})
}

func TestMultipathPathsBlockedRoundTrip(t *testing.T) {
	checkMultipathFrame(t, &PathsBlockedFrame{MaximumPathID: 3}, []byte{0x7e, 0x7b, 3})
}

func TestMultipathPathCIDsBlockedRoundTrip(t *testing.T) {
	checkMultipathFrame(t, &PathCIDsBlockedFrame{PathID: 3, NextSequenceNumber: 2}, []byte{0x7e, 0x7c, 3, 2})
}

func TestMultipathACKRoundTrip(t *testing.T) {
	parser := NewFrameParser(false, false, false)
	parser.SetAckDelayExponent(protocol.AckDelayExponent)
	parser.EnableMultipath()
	for _, ecn := range []bool{false, true} {
		f := &AckFrame{PathID: 3, HasPathID: true, AckRanges: []AckRange{{Smallest: 10, Largest: 10}}, DelayTime: 8 * time.Microsecond}
		expected := []byte{0x3e, 3, 10, 1, 0, 0}
		if ecn {
			f.ECT0, f.ECT1, f.ECNCE = 2, 3, 4
			expected = []byte{0x3f, 3, 10, 1, 0, 0, 2, 3, 4}
		}
		b, err := f.Append(nil, protocol.Version1)
		require.NoError(t, err)
		require.Equal(t, expected, b)
		require.Equal(t, protocol.ByteCount(len(b)), f.Length(protocol.Version1))
		typ, n, err := parser.ParseType(b, protocol.Encryption1RTT)
		require.NoError(t, err)
		require.True(t, typ.IsAckFrameType())
		decoded, consumed, err := parser.ParseAckFrame(typ, b[n:], protocol.Encryption1RTT, protocol.Version1)
		require.NoError(t, err)
		require.Equal(t, f, decoded)
		require.Equal(t, len(b), n+consumed)
	}
}

func TestMultipathTransportParameter(t *testing.T) {
	id := protocol.PathID(0)
	original := &TransportParameters{InitialMaxPathID: &id, ActiveConnectionIDLimit: 2, InitialSourceConnectionID: protocol.ParseConnectionID([]byte{1}), MaxDatagramFrameSize: protocol.InvalidByteCount}
	b := original.Marshal(protocol.PerspectiveClient)
	var decoded TransportParameters
	require.NoError(t, decoded.Unmarshal(b, protocol.PerspectiveClient))
	require.NotNil(t, decoded.InitialMaxPathID)
	require.Zero(t, *decoded.InitialMaxPathID)
	original.InitialMaxPathID = nil
	require.NoError(t, decoded.Unmarshal(original.Marshal(protocol.PerspectiveClient), protocol.PerspectiveClient))
	require.Nil(t, decoded.InitialMaxPathID)
}

func TestMultipathOrdinaryACKTargetsZeroAfterPathACK(t *testing.T) {
	parser := NewFrameParser(false, false, false)
	parser.EnableMultipath()
	_, _, err := parser.ParseAckFrame(FrameTypePathAck, []byte{3, 10, 0, 0, 0}, protocol.Encryption1RTT, protocol.Version1)
	require.NoError(t, err)
	f, n, err := parser.ParseAckFrame(FrameTypeAck, []byte{10, 0, 0, 0}, protocol.Encryption1RTT, protocol.Version1)
	require.NoError(t, err)
	require.Equal(t, 4, n)
	require.Zero(t, f.PathID)
	require.False(t, f.HasPathID)
	b, err := f.Append(nil, protocol.Version1)
	require.NoError(t, err)
	require.Equal(t, []byte{2, 10, 0, 0, 0}, b)
}

func TestMultipathACKTruncateAccountsForPathID(t *testing.T) {
	f := &AckFrame{HasPathID: true, PathID: protocol.MaxPathID, AckRanges: []AckRange{{Smallest: 100, Largest: 100}, {Smallest: 90, Largest: 90}}}
	f.Truncate(f.Length(protocol.Version1)-1, protocol.Version1)
	require.Len(t, f.AckRanges, 1)
	b, err := f.Append(nil, protocol.Version1)
	require.NoError(t, err)
	require.Equal(t, protocol.ByteCount(len(b)), f.Length(protocol.Version1))
	require.Equal(t, 14, len(b))
}

func TestMultipathTransportParameterValidation(t *testing.T) {
	for _, value := range [][]byte{quicvarint.Append(nil, 1<<32), {0, 0}, {}} {
		b := quicvarint.Append(nil, 0x3e)
		b = quicvarint.Append(b, uint64(len(value)))
		b = append(b, value...)
		b = appendInitialSourceConnectionID(b)
		var p TransportParameters
		err := p.Unmarshal(b, protocol.PerspectiveClient)
		var transportErr *qerr.TransportError
		require.ErrorAs(t, err, &transportErr)
		require.Equal(t, qerr.TransportParameterError, transportErr.ErrorCode)
	}
}

func TestMultipathTransportParameterNotRemembered(t *testing.T) {
	id := protocol.PathID(1)
	p := &TransportParameters{InitialMaxPathID: &id, ActiveConnectionIDLimit: 2, MaxDatagramFrameSize: protocol.InvalidByteCount}
	var restored TransportParameters
	require.NoError(t, restored.UnmarshalFromSessionTicket(p.MarshalForSessionTicket(nil)))
	require.Nil(t, restored.InitialMaxPathID)
}

func TestMultipathMalformedFrameBodies(t *testing.T) {
	parser := NewFrameParser(false, false, false)
	parser.EnableMultipath()
	for _, typ := range []FrameType{FrameTypeMaxPathID, FrameTypePathAbandon, FrameTypePathStatusBackup, FrameTypePathNewConnectionID, FrameTypePathRetireConnectionID, FrameTypePathsBlocked, FrameTypePathCIDsBlocked} {
		_, _, err := parser.ParseLessCommonFrame(typ, nil, protocol.Version1)
		requireTransportError(t, err, qerr.FrameEncodingError, typ)
	}
	_, _, err := parser.ParseAckFrame(FrameTypePathAck, []byte{3}, protocol.Encryption1RTT, protocol.Version1)
	requireTransportError(t, err, qerr.FrameEncodingError, FrameTypePathAck)
}

func TestMultipathConnectionIDCanonicalValidation(t *testing.T) {
	parser := NewFrameParser(false, false, false)
	parser.EnableMultipath()
	for _, b := range [][]byte{{3, 1, 2}, {3, 1, 0, 0}, {3, 1, 0, 21}} {
		_, _, err := parser.ParseLessCommonFrame(FrameTypePathNewConnectionID, b, protocol.Version1)
		requireTransportError(t, err, qerr.FrameEncodingError, FrameTypePathNewConnectionID)
	}
	_, err := (&PathNewConnectionIDFrame{}).Append(nil, protocol.Version1)
	require.Error(t, err)
	_, err = (&PathNewConnectionIDFrame{SequenceNumber: 1, RetirePriorTo: 2, ConnectionID: protocol.ParseConnectionID([]byte{1})}).Append(nil, protocol.Version1)
	require.Error(t, err)
}

func TestMultipathDefaultOffByteEquivalence(t *testing.T) {
	p := &TransportParameters{ActiveConnectionIDLimit: protocol.DefaultActiveConnectionIDLimit, AckDelayExponent: protocol.DefaultAckDelayExponent, MaxAckDelay: protocol.DefaultMaxAckDelay, MaxDatagramFrameSize: protocol.InvalidByteCount, InitialSourceConnectionID: protocol.ParseConnectionID([]byte{1})}
	b := p.Marshal(protocol.PerspectiveClient)
	// The ordinary encoding starts with one random GREASE parameter, as before R2.
	id, n, err := quicvarint.Parse(b)
	require.NoError(t, err)
	require.Equal(t, uint64(27), id%31)
	length, m, err := quicvarint.Parse(b[n:])
	require.NoError(t, err)
	b = b[n+m+int(length):]
	require.Equal(t, []byte{5, 1, 0, 6, 1, 0, 7, 1, 0, 4, 1, 0, 8, 1, 0, 9, 1, 0, 1, 1, 0, 15, 1, 1}, b)
	parser := NewFrameParser(false, false, false)
	for _, typ := range []FrameType{FrameTypePathAck, FrameTypePathAckECN, FrameTypePathAbandon, FrameTypePathStatusBackup, FrameTypePathStatusAvailable, FrameTypePathNewConnectionID, FrameTypePathRetireConnectionID, FrameTypeMaxPathID, FrameTypePathsBlocked, FrameTypePathCIDsBlocked} {
		_, _, err := parser.ParseType(quicvarint.Append(nil, uint64(typ)), protocol.Encryption1RTT)
		requireTransportError(t, err, qerr.FrameEncodingError, typ)
	}
}
