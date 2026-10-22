package quic

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// Retain native Initial protection, packing, recovery and the output queue.
func newSizeBoundEmissionConnection(t *testing.T) *testConnection {
	t.Helper()
	tc := newClientTestConnection(t, gomock.NewController(t), &Config{InitialPacketSize: 1200, EnableDatagrams: true, DisablePathMTUDiscovery: true}, false)
	c := tc.conn
	t.Cleanup(func() {
		c.ctxCancel(context.Canceled)
		require.NoError(t, c.cryptoStreamHandler.Close())
		q := c.emission.queue.(*sendQueue)
		for len(q.queue) > 0 {
			(<-q.queue).buf.Release()
		}
	})
	return tc
}

func TestEmissionOversizedInitialPTO(t *testing.T) {
	for _, tokenLen := range []int{1200, protocol.MaxPacketBufferSize} {
		t.Run(fmt.Sprintf("token=%d", tokenLen), func(t *testing.T) {
			c := newSizeBoundEmissionConnection(t).conn
			c.emission.setToken(make([]byte, tokenLen))
			observed := observeConstructionBuffer(t)
			var err error
			require.NotPanics(t, func() {
				err = c.emission.sendProbePacket(ackhandler.SendPTOInitial, monotime.Now())
			})
			// The empty probe adds a PING and two sample-padding bytes.
			require.EqualError(t, err, fmt.Sprintf("packet too large (%d bytes, allowed 1200 bytes)", tokenLen+43))
			require.Nil(t, *observed)
			require.Empty(t, c.emission.queue.(*sendQueue).queue)
			require.Zero(t, c.connStats.PacketsSent.Load())
			pn, _ := c.sentPacketHandler.PeekPacketNumber(protocol.EncryptionInitial)
			require.Zero(t, pn)
		})
	}
}

func TestEmissionOversizedClose(t *testing.T) {
	for _, token := range []bool{false, true} {
		for _, inputLen := range []int{1200, protocol.MaxPacketBufferSize} {
			t.Run(fmt.Sprintf("token=%t/bytes=%d", token, inputLen), func(t *testing.T) {
				c := newSizeBoundEmissionConnection(t).conn
				reason := ""
				if token {
					c.emission.setToken(make([]byte, inputLen))
				} else {
					reason = strings.Repeat("x", inputLen)
				}
				observed := observeConstructionBuffer(t)
				var retained []byte
				var err error
				require.NotPanics(t, func() {
					retained, err = c.emission.close(&qerr.TransportError{ErrorCode: qerr.ProtocolViolation, ErrorMessage: reason})
				})
				require.EqualError(t, err, fmt.Sprintf("packet too large (%d bytes, allowed 1200 bytes)", inputLen+45))
				require.Nil(t, retained)
				require.Nil(t, *observed)
				require.Empty(t, c.emission.queue.(*sendQueue).queue)
				require.Zero(t, c.connStats.PacketsSent.Load())
				pn, _ := c.sentPacketHandler.PeekPacketNumber(protocol.EncryptionInitial)
				require.Zero(t, pn)
			})
		}
	}
}

func TestEmissionOversizedInitialCoalesced(t *testing.T) {
	for _, onlyAck := range []bool{false, true} {
		for _, tokenLen := range []int{1200, protocol.MaxPacketBufferSize} {
			t.Run(fmt.Sprintf("ack-only=%t/token=%d", onlyAck, tokenLen), func(t *testing.T) {
				c := newSizeBoundEmissionConnection(t).conn
				c.emission.setToken(make([]byte, tokenLen))
				now := monotime.Now()
				for _, pn := range []protocol.PacketNumber{4, 5} {
					require.NoError(t, c.receivedPacketHandler.ReceivedPacket(pn, protocol.ECNNon, protocol.EncryptionInitial, now, true))
				}
				observed := observeConstructionBuffer(t)
				var err error
				require.NotPanics(t, func() {
					if onlyAck {
						var sent bool
						sent, err = c.emission.maybeSendAckOnlyPacket(now, false)
						require.False(t, sent)
					} else {
						result := c.emission.coalesced(now)
						err = result.err
						require.False(t, result.progress)
					}
				})
				// Six-byte connection IDs, a two-byte token length and length
				// field, one packet-number byte, a five-byte ACK and a 16-byte tag.
				require.EqualError(t, err, fmt.Sprintf("packet too large (%d bytes, allowed 1200 bytes)", tokenLen+46))
				require.False(t, isSendMsgSizeErr(err))
				require.EqualValues(t, 1200, c.maxPacketSize())
				require.Nil(t, *observed, "reject before acquiring output storage")
				require.Empty(t, c.emission.queue.(*sendQueue).queue)
				require.Zero(t, c.connStats.PacketsSent.Load())
				pn, _ := c.sentPacketHandler.PeekPacketNumber(protocol.EncryptionInitial)
				require.Zero(t, pn)
			})
		}
	}
}

func TestEmissionPacketSizeBoundary(t *testing.T) {
	// Fixed RFC 9000 encodings with six-byte source/destination IDs and
	// packet number zero: ACK + token = token+46, empty probe = token+43,
	// and transport close without a token = reason+45 (all include the tag).
	for _, path := range []string{"coalesced", "ack-only", "PTO", "close"} {
		for _, size := range []int{1199, 1200, 1201} {
			t.Run(fmt.Sprintf("%s/size=%d", path, size), func(t *testing.T) {
				tc := newSizeBoundEmissionConnection(t)
				c := tc.conn
				now := monotime.Now()
				reason := ""
				switch path {
				case "coalesced", "ack-only":
					c.emission.setToken(make([]byte, size-46))
					for _, pn := range []protocol.PacketNumber{4, 5} {
						require.NoError(t, c.receivedPacketHandler.ReceivedPacket(pn, protocol.ECNNon, protocol.EncryptionInitial, now, true))
					}
				case "PTO":
					c.emission.setToken(make([]byte, size-43))
				case "close":
					reason = strings.Repeat("x", size-45)
				}
				var written []byte
				if path == "close" && size <= 1200 {
					tc.sendConn.EXPECT().Write(gomock.Any(), uint16(0), gomock.Any()).DoAndReturn(func(b []byte, _ uint16, _ protocol.ECN) error {
						written = bytes.Clone(b)
						return nil
					})
				}
				observed := observeConstructionBuffer(t)
				var err error
				switch path {
				case "coalesced":
					err = c.emission.coalesced(now).err
				case "ack-only":
					_, err = c.emission.maybeSendAckOnlyPacket(now, false)
				case "PTO":
					err = c.emission.sendProbePacket(ackhandler.SendPTOInitial, now)
				case "close":
					var retained []byte
					retained, err = c.emission.close(&qerr.TransportError{ErrorCode: qerr.ProtocolViolation, ErrorMessage: reason})
					require.Equal(t, written, retained)
				}
				q := c.emission.queue.(*sendQueue)
				if size > 1200 {
					require.EqualError(t, err, "packet too large (1201 bytes, allowed 1200 bytes)")
					require.Nil(t, *observed)
					require.Empty(t, q.queue)
					pn, _ := c.sentPacketHandler.PeekPacketNumber(protocol.EncryptionInitial)
					require.Zero(t, pn)
					return
				}
				require.NoError(t, err)
				if path != "close" {
					require.Len(t, q.queue, 1)
					entry := <-q.queue
					written = bytes.Clone(entry.buf.Data)
					entry.buf.Release()
					require.EqualValues(t, 1, c.connStats.PacketsSent.Load())
				}
				require.Len(t, written, 1200)
				require.NotNil(t, *observed)
				require.Zero(t, (*observed).refCount)
				pn, _ := c.sentPacketHandler.PeekPacketNumber(protocol.EncryptionInitial)
				require.EqualValues(t, 1, pn)
				hdr, packet, rest, err := wire.ParsePacket(written)
				require.NoError(t, err)
				require.Empty(t, rest)
				_, opener := handshake.NewInitialAEAD(tc.destConnID, protocol.PerspectiveServer, protocol.Version1)
				ext, data, err := (&packetUnpacker{}).unpackLongHeaderPacket(opener, hdr, packet)
				require.NoError(t, err)
				require.Zero(t, ext.PacketNumber)
				parser := wire.NewFrameParser(false, false, false)
				typ, n, err := parser.ParseType(data, protocol.EncryptionInitial)
				require.NoError(t, err)
				data = data[n:]
				var consumed int
				if path == "coalesced" || path == "ack-only" {
					require.Equal(t, wire.FrameTypeAck, typ)
					ack, n, err := parser.ParseAckFrame(typ, data, protocol.EncryptionInitial, protocol.Version1)
					require.NoError(t, err)
					require.Equal(t, []wire.AckRange{{Smallest: 4, Largest: 5}}, ack.AckRanges)
					consumed = n
				} else {
					frame, n, err := parser.ParseLessCommonFrame(typ, data, protocol.Version1)
					require.NoError(t, err)
					consumed = n
					if path == "PTO" {
						require.IsType(t, &wire.PingFrame{}, frame)
					} else {
						require.Equal(t, &wire.ConnectionCloseFrame{ErrorCode: uint64(qerr.ProtocolViolation), ReasonPhrase: reason}, frame)
					}
				}
				_, _, err = parser.ParseType(data[consumed:], protocol.EncryptionInitial)
				require.ErrorIs(t, err, io.EOF, "only padding may follow the expected frame")
			})
		}
	}
}

func TestEmissionOversizedAggregateClose(t *testing.T) {
	c := newHandshakeEmissionConnection(t, false).conn
	// Each reason-bearing component fits 1200 bytes; their aggregate is 1259.
	observed := observeConstructionBuffer(t)
	retained, err := c.emission.close(&qerr.TransportError{ErrorCode: qerr.ProtocolViolation, ErrorMessage: strings.Repeat("x", 600)})
	require.EqualError(t, err, "packet too large (1259 bytes, allowed 1200 bytes)")
	require.Nil(t, retained)
	require.Nil(t, *observed)
	for _, level := range []protocol.EncryptionLevel{protocol.EncryptionInitial, protocol.EncryptionHandshake} {
		pn, _ := c.sentPacketHandler.PeekPacketNumber(level)
		require.Zero(t, pn)
	}
}

func TestEmissionApplicationCloseSize(t *testing.T) {
	reason := strings.Repeat("x", protocol.MaxPacketBufferSize)
	t.Run("application level rejects", func(t *testing.T) {
		c := newEmissionTestConnection(t, false).conn
		sealing := c.emission.packer.cryptoSetup.(*MockSealingManager)
		sealing.EXPECT().GetInitialSealer().Return(nil, handshake.ErrKeysDropped)
		sealing.EXPECT().GetHandshakeSealer().Return(nil, handshake.ErrKeysDropped)
		observed := observeConstructionBuffer(t)
		retained, err := c.emission.close(&qerr.ApplicationError{ErrorCode: 42, ErrorMessage: reason})
		require.ErrorContains(t, err, "packet too large")
		require.ErrorContains(t, err, "allowed 1200 bytes")
		require.Nil(t, retained)
		require.Nil(t, *observed)
		pn, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
		require.Zero(t, pn)
	})
	t.Run("Initial and Handshake suppress reason", func(t *testing.T) {
		tc := newHandshakeEmissionConnection(t, false)
		tc.sendConn.EXPECT().Write(gomock.Any(), uint16(0), gomock.Any()).DoAndReturn(func(b []byte, _ uint16, _ protocol.ECN) error {
			require.Len(t, b, 57) // server close-only Initials are not padded
			parser := wire.NewFrameParser(false, false, false)
			for _, level := range []protocol.EncryptionLevel{protocol.EncryptionInitial, protocol.EncryptionHandshake} {
				hdr, packet, rest, err := wire.ParsePacket(b)
				require.NoError(t, err)
				ext, err := hdr.ParseExtended(packet)
				require.NoError(t, err)
				data := packet[ext.ParsedLen() : len(packet)-7] // fixture protection overhead
				typ, n, err := parser.ParseType(data, level)
				require.NoError(t, err)
				frame, _, err := parser.ParseLessCommonFrame(typ, data[n:], protocol.Version1)
				require.NoError(t, err)
				require.Equal(t, &wire.ConnectionCloseFrame{ErrorCode: uint64(qerr.ApplicationErrorErrorCode)}, frame)
				b = rest
			}
			require.Empty(t, b)
			return nil
		})
		retained, err := tc.conn.emission.close(&qerr.ApplicationError{ErrorCode: 42, ErrorMessage: reason})
		require.NoError(t, err)
		require.Len(t, retained, 57)
	})
}

func TestEmissionOversizedPTOAfterRetirement(t *testing.T) {
	c := newSizeBoundEmissionConnection(t).conn
	now := monotime.Now()
	crypto := &wire.CryptoFrame{Data: []byte("outstanding Initial crypto")}
	c.retransmissionQueue.addInitial(crypto)
	require.NoError(t, c.emission.sendProbePacket(ackhandler.SendPTOInitial, now))
	q := c.emission.queue.(*sendQueue)
	require.Len(t, q.queue, 1)
	(<-q.queue).buf.Release()
	for _, pn := range []protocol.PacketNumber{4, 5} {
		require.NoError(t, c.receivedPacketHandler.ReceivedPacket(pn, protocol.ECNNon, protocol.EncryptionInitial, now, true))
	}
	c.emission.setToken(make([]byte, 1200))
	observed := observeConstructionBuffer(t)
	err := c.emission.sendProbePacket(ackhandler.SendPTOInitial, now)
	require.EqualError(t, err, "packet too large (1246 bytes, allowed 1200 bytes)")
	require.Nil(t, *observed)
	require.Empty(t, q.queue)
	require.EqualValues(t, 1, c.connStats.PacketsSent.Load(), "only the original probe was registered")
	pn, _ := c.sentPacketHandler.PeekPacketNumber(protocol.EncryptionInitial)
	require.EqualValues(t, 1, pn)
	// Recovery retired the old probe before construction. Its OnLost result
	// stays queued once; the rejected construction neither sends nor refunds it.
	require.Same(t, crypto, c.retransmissionQueue.GetFrame(protocol.EncryptionInitial, 1200, protocol.Version1))
	require.Nil(t, c.retransmissionQueue.GetFrame(protocol.EncryptionInitial, 1200, protocol.Version1))
}

func TestEmissionOversizedInitialPreservesQueuedStorage(t *testing.T) {
	tc := newSizeBoundEmissionConnection(t)
	c := tc.conn
	str := newSendStream(c.Context(), 0, c, newTestStreamFlowControllerWithSendWindow(0, 1200), false)
	t.Cleanup(func() { str.closeForShutdown(context.Canceled) })
	require.NoError(t, str.TryWriteAll([]byte("pooled stream storage")))
	pending := str.nextFrame
	datagram := &wire.DatagramFrame{DataLenPresent: true, Data: []byte("queued datagram")}
	require.NoError(t, c.datagramQueue.Add(datagram))
	c.emission.setToken(make([]byte, 1200))
	now := monotime.Now()
	for _, pn := range []protocol.PacketNumber{4, 5} {
		require.NoError(t, c.receivedPacketHandler.ReceivedPacket(pn, protocol.ECNNon, protocol.EncryptionInitial, now, true))
	}
	observed := observeConstructionBuffer(t)
	require.ErrorContains(t, c.emission.coalesced(now).err, "packet too large")
	require.Nil(t, *observed)
	require.Same(t, pending, str.nextFrame)
	require.Equal(t, []byte("pooled stream storage"), pending.Data)
	require.Zero(t, str.numOutstandingFrames)
	require.Empty(t, str.retransmissionQueue)
	require.Same(t, datagram, c.datagramQueue.Peek())
	require.Empty(t, c.emission.queue.(*sendQueue).queue)

	// With Initial keys gone, the same real frame producers can emit their
	// untouched application data. Transparent 1-RTT protection is sufficient.
	sealing := NewMockSealingManager(gomock.NewController(t))
	sealing.EXPECT().GetInitialSealer().Return(nil, handshake.ErrKeysDropped).AnyTimes()
	sealing.EXPECT().GetHandshakeSealer().Return(nil, handshake.ErrKeysDropped).AnyTimes()
	sealing.EXPECT().Get1RTTSealer().Return(newMockShortHeaderSealer(gomock.NewController(t)), nil).AnyTimes()
	c.emission.packer.cryptoSetup = sealing
	result := c.emission.coalesced(now)
	require.NoError(t, result.err)
	require.True(t, result.progress)
	require.Nil(t, str.nextFrame)
	require.EqualValues(t, 1, str.numOutstandingFrames)
	require.Nil(t, c.datagramQueue.Peek())
	q := c.emission.queue.(*sendQueue)
	require.Len(t, q.queue, 1)
	entry := <-q.queue
	entry.buf.Release()
	require.Zero(t, (*observed).refCount)
	ack := &wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: 0, Largest: 0}}}
	for range 2 { // a duplicate ACK must not run a second pooled-frame callback
		_, err := c.sentPacketHandler.ReceivedAck(ack, protocol.Encryption1RTT, now)
		require.NoError(t, err)
		require.Zero(t, str.numOutstandingFrames)
	}
	result = c.emission.coalesced(now)
	require.NoError(t, result.err)
	require.False(t, result.progress)
	require.Empty(t, q.queue)
}
