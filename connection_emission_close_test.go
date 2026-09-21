package quic

import (
	"bytes"
	"context"
	"errors"
	"net"
	"testing"

	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestEmissionCloseRetainedLifetime(t *testing.T) {
	for _, writeError := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "write-error"}[writeError], func(t *testing.T) {
			tc := newEmissionTestConnection(t, false)
			c := tc.conn
			sealing := c.emission.packer.cryptoSetup.(*MockSealingManager)
			sealing.EXPECT().GetInitialSealer().Return(nil, handshake.ErrKeysDropped)
			sealing.EXPECT().GetHandshakeSealer().Return(nil, handshake.ErrKeysDropped)
			observed := observeConstructionBuffer(t)
			var first []byte
			var writeErr error
			if writeError {
				writeErr = errors.New("close write failed")
			}
			tc.sendConn.EXPECT().Write(gomock.Any(), uint16(0), gomock.Any()).DoAndReturn(func(b []byte, _ uint16, _ protocol.ECN) error {
				require.EqualValues(t, 1, (*observed).refCount)
				first = bytes.Clone(b)
				return writeErr
			})
			retained, err := c.emission.close(&qerr.ApplicationError{ErrorCode: 42, ErrorMessage: "retained"})
			require.ErrorIs(t, err, writeErr)
			require.NotEmpty(t, first)
			require.Equal(t, first, retained)
			require.Zero(t, (*observed).refCount, "temporary close storage must be returned")
			// Poison the released construction allocation before exercising retained retransmission.
			for i := range (*observed).Data {
				(*observed).Data[i] = 0xa5
			}
			handler := newClosedLocalConn(func(net.Addr, packetInfo) { require.Equal(t, first, retained) }, c.logger)
			handler.handlePacket(receivedPacket{buffer: getPacketBuffer()})
		})
	}
}

func TestCloseConstructorFailureLifetime(t *testing.T) {
	for _, longHeader := range []bool{false, true} {
		t.Run(map[bool]string{false: "short", true: "long"}[longHeader], func(t *testing.T) {
			ctrl := gomock.NewController(t)
			tc := newServerTestConnection(t, ctrl, nil, false)
			useLifecyclePacketPacker(t, ctrl, tc)
			c := tc.conn
			p := c.emission.packer
			p.pnManager = mismatchedConstructionNumber{c.sentPacketHandler}
			sealing := NewMockSealingManager(gomock.NewController(t))
			if longHeader {
				sealing.EXPECT().GetInitialSealer().Return(newMockShortHeaderSealer(gomock.NewController(t)), nil)
				sealing.EXPECT().Get1RTTSealer().Return(nil, handshake.ErrKeysNotYetAvailable)
			} else {
				sealing.EXPECT().GetInitialSealer().Return(nil, handshake.ErrKeysDropped)
				sealing.EXPECT().Get1RTTSealer().Return(newMockShortHeaderSealer(gomock.NewController(t)), nil)
			}
			sealing.EXPECT().GetHandshakeSealer().Return(nil, handshake.ErrKeysNotYetAvailable)
			p.cryptoSetup = sealing
			observed := observeConstructionBuffer(t)
			packet, err := p.PackConnectionClose(&qerr.TransportError{ErrorCode: qerr.InternalError}, 1200, protocol.Version1)
			require.ErrorContains(t, err, "Peeked and Popped")
			require.Nil(t, packet)
			require.NotNil(t, *observed)
			require.Zero(t, (*observed).refCount)
		})
	}
}

// Keep real construction and recovery while selecting the pre-confirmation keys.
func TestEmissionCloseCoalescedClient1RTT(t *testing.T) {
	for _, writeError := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "write-error"}[writeError], func(t *testing.T) {
			ctrl := gomock.NewController(t)
			tc := newClientTestConnection(t, ctrl, &Config{InitialPacketSize: 1452, DisablePathMTUDiscovery: true}, false)
			c := tc.conn
			defer c.ctxCancel(context.Canceled)
			defer c.cryptoStreamHandler.Close()
			sealing := NewMockSealingManager(ctrl)
			sealing.EXPECT().GetInitialSealer().Return(newMockShortHeaderSealer(ctrl), nil)
			sealing.EXPECT().GetHandshakeSealer().Return(newMockShortHeaderSealer(ctrl), nil)
			sealing.EXPECT().Get0RTTSealer().Return(nil, handshake.ErrKeysDropped)
			sealing.EXPECT().Get1RTTSealer().Return(newMockShortHeaderSealer(ctrl), nil)
			c.emission.packer.cryptoSetup = sealing
			levels := []protocol.EncryptionLevel{protocol.EncryptionInitial, protocol.EncryptionHandshake, protocol.Encryption1RTT}
			for _, level := range levels {
				pn, _ := c.sentPacketHandler.PeekPacketNumber(level)
				require.Zero(t, pn)
			}
			observed := observeConstructionBuffer(t)
			var written []byte
			var writeErr error
			if writeError {
				writeErr = errors.New("close write failed")
			}
			tc.sendConn.EXPECT().Write(gomock.Any(), uint16(0), gomock.Any()).DoAndReturn(func(b []byte, _ uint16, _ protocol.ECN) error {
				require.EqualValues(t, 1, (*observed).refCount)
				require.Len(t, b, 1452)
				hdrs, more := parsePacket(t, b)
				require.Len(t, hdrs, 2)
				require.Equal(t, protocol.PacketTypeInitial, hdrs[0].Type)
				require.Equal(t, protocol.PacketTypeHandshake, hdrs[1].Type)
				require.Zero(t, hdrs[0].PacketNumber)
				require.Zero(t, hdrs[1].PacketNumber)
				_, pn, _, _, err := wire.ParseShortHeader(more, tc.destConnID.Len())
				require.NoError(t, err)
				require.Zero(t, pn)
				written = bytes.Clone(b)
				return writeErr
			})
			retained, err := c.emission.close(&qerr.ApplicationError{ErrorCode: 42, ErrorMessage: "coalesced close"})
			require.ErrorIs(t, err, writeErr)
			require.Len(t, written, 1452)
			require.Equal(t, written, retained)
			require.Zero(t, (*observed).refCount, "temporary close storage must be returned")
			for _, level := range levels {
				pn, _ := c.sentPacketHandler.PeekPacketNumber(level)
				require.Equal(t, protocol.PacketNumber(1), pn)
			}
			for i := range (*observed).Data {
				(*observed).Data[i] = 0xa5
			}
			require.Equal(t, written, retained, "retransmission bytes must outlive the construction buffer")
		})
	}
}
