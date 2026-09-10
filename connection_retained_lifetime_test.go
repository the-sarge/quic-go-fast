package quic

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/mocks"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestConnectionRetainedLifetimeOverflow(t *testing.T) {
	tc := newServerTestConnection(t, nil, nil, false)
	for range protocol.MaxConnUnprocessedPackets {
		tc.conn.handlePacket(lifetimePacket(t, tc, []byte{0}))
	}
	t.Cleanup(func() {
		for !tc.conn.receivedPackets.Empty() {
			tc.conn.receivedPackets.PopFront().buffer.Release()
		}
	})
	p := lifetimePacket(t, tc, []byte{0})
	tc.conn.handlePacket(p)
	require.Equal(t, protocol.MaxConnUnprocessedPackets, tc.conn.receivedPackets.Len())
	require.Zero(t, p.buffer.refCount, "overflow must consume rejected input")
}

func TestConnectionRetainedLifetimeReplay(t *testing.T) {
	for _, failEarly := range []bool{false, true} {
		name := "success"
		if failEarly {
			name = "early error"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctrl := gomock.NewController(t)
				cs := mocks.NewMockCryptoSetup(ctrl)
				u := NewMockUnpacker(ctrl)
				tc := newClientTestConnection(t, ctrl, nil, false, connectionOptCryptoSetup(cs), connectionOptUnpacker(u))
				var packets []receivedPacket
				for pn := protocol.PacketNumber(1); pn <= 4; pn++ {
					data, err := wire.AppendShortHeader(nil, tc.srcConnID, pn, protocol.PacketNumberLen1, protocol.KeyPhaseZero)
					require.NoError(t, err)
					packets = append(packets, lifetimePacket(t, tc, append(data, byte(pn))))
				}
				tc.conn.undecryptablePacketsToProcess = []receivedPacketWithChecksum{{receivedPacket: packets[0]}, {receivedPacket: packets[1]}}
				tc.conn.undecryptablePackets = []receivedPacketWithChecksum{{receivedPacket: packets[2]}}
				tc.conn.handlePacket(packets[3])
				wantErr := &qerr.TransportError{ErrorCode: qerr.InternalError, ErrorMessage: "stop replay test"}
				cs.EXPECT().StartHandshake(gomock.Any())
				gomock.InOrder(
					cs.EXPECT().NextEvent().Return(handshake.Event{Kind: handshake.EventNoEvent}),
					cs.EXPECT().NextEvent().Return(handshake.Event{Kind: handshake.EventReceived0RTTReadKeys}),
					cs.EXPECT().NextEvent().Return(handshake.Event{Kind: handshake.EventNoEvent}),
				)
				var order []byte
				n := 4
				if failEarly {
					n = 1
				}
				u.EXPECT().UnpackShortHeader(gomock.Any(), gomock.Any()).DoAndReturn(func(_ monotime.Time, data []byte) (protocol.PacketNumber, protocol.PacketNumberLen, protocol.KeyPhaseBit, []byte, error) {
					pn := data[len(data)-1]
					order = append(order, pn)
					if pn == 1 {
						require.NoError(t, tc.conn.handleHandshakeEvents(monotime.Now()))
					}
					if failEarly || pn == 4 {
						return 0, 0, 0, nil, wantErr
					}
					return protocol.PacketNumber(pn), protocol.PacketNumberLen1, protocol.KeyPhaseZero, []byte{0}, nil
				}).Times(n)
				cs.EXPECT().Close()
				tc.connRunner.EXPECT().Remove(gomock.Any())
				require.ErrorIs(t, tc.conn.run(), wantErr)
				if failEarly {
					require.Equal(t, []byte{1}, order)
				} else {
					require.Equal(t, []byte{1, 2, 3, 4}, order, "finish the detached batch, then scheduled work, before ordinary input")
				}
				for i, p := range packets {
					require.Zero(t, p.buffer.refCount, "packet %d must be disposed", i+1)
				}
				require.Empty(t, tc.conn.undecryptablePacketsToProcess)
			})
		})
	}
}

func TestConnectionRetainedLifetimeHandshakeDiscard(t *testing.T) {
	ctrl := gomock.NewController(t)
	cs := mocks.NewMockCryptoSetup(ctrl)
	u := NewMockUnpacker(ctrl)
	tc := newServerTestConnection(t, ctrl, nil, false, connectionOptCryptoSetup(cs), connectionOptUnpacker(u))
	hdr := &wire.ExtendedHeader{
		Header:       wire.Header{Type: protocol.PacketTypeHandshake, Version: Version1, DestConnectionID: tc.srcConnID, Length: 2},
		PacketNumber: 1, PacketNumberLen: protocol.PacketNumberLen1,
	}
	data, err := hdr.Append(nil, Version1)
	require.NoError(t, err)
	p := lifetimePacket(t, tc, append(data, 0))
	original := bytes.Clone(p.data)
	p.buffer.Split()
	p.buffer.Split()
	tc.conn.undecryptablePackets = []receivedPacketWithChecksum{{receivedPacket: p}, {receivedPacket: p}}
	cryptoData, err := (&wire.CryptoFrame{Data: []byte("handshake")}).Append(nil, Version1)
	require.NoError(t, err)
	u.EXPECT().UnpackLongHeader(gomock.Any(), gomock.Any()).Return(&unpackedPacket{hdr: hdr, encryptionLevel: protocol.EncryptionHandshake, data: cryptoData}, nil)
	cs.EXPECT().DiscardInitialKeys().AnyTimes()
	cs.EXPECT().HandleMessage([]byte("handshake"), protocol.EncryptionHandshake)
	gomock.InOrder(
		cs.EXPECT().NextEvent().Return(handshake.Event{Kind: handshake.EventHandshakeComplete}),
		cs.EXPECT().NextEvent().Return(handshake.Event{Kind: handshake.EventNoEvent}),
	)
	cs.EXPECT().SetHandshakeConfirmed()
	cs.EXPECT().GetSessionTicket().DoAndReturn(func() ([]byte, error) {
		require.Equal(t, 2, p.buffer.refCount, "only the active parsing hold and dispatched view remain")
		require.Equal(t, original, p.data)
		return nil, nil
	})
	processed, err := tc.conn.handleOnePacket(p, 42)
	require.NoError(t, err)
	require.True(t, processed)
	require.Empty(t, tc.conn.undecryptablePackets)
	require.Zero(t, p.buffer.refCount)
}

func TestConnectionRetainedLifetimeTermination(t *testing.T) {
	for _, mode := range []string{"start handshake", "handshake event", "normal close"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctrl := gomock.NewController(t)
				cs := mocks.NewMockCryptoSetup(ctrl)
				tc := newClientTestConnection(t, ctrl, nil, false, connectionOptCryptoSetup(cs))
				queued := lifetimePacket(t, tc, []byte{0})
				tc.conn.handlePacket(queued)
				retained := lifetimePacket(t, tc, []byte{0})
				retained.buffer.Split()
				tc.conn.undecryptablePackets = []receivedPacketWithChecksum{{receivedPacket: retained}}
				tc.conn.undecryptablePacketsToProcess = []receivedPacketWithChecksum{{receivedPacket: retained}}
				wantErr := errors.New("handshake startup failed")
				switch mode {
				case "start handshake":
					cs.EXPECT().StartHandshake(gomock.Any()).Return(wantErr)
				case "handshake event":
					cs.EXPECT().StartHandshake(gomock.Any())
					cs.EXPECT().NextEvent().Return(handshake.Event{Kind: handshake.EventReceivedTransportParameters, TransportParameters: &wire.TransportParameters{}})
				case "normal close":
					wantErr = &qerr.ApplicationError{ErrorCode: 42}
					cs.EXPECT().StartHandshake(gomock.Any()).Do(func(context.Context) error { tc.conn.closeLocal(wantErr); return nil })
					cs.EXPECT().NextEvent().Return(handshake.Event{Kind: handshake.EventNoEvent})
					cs.EXPECT().Close()
					tc.connRunner.EXPECT().Remove(gomock.Any())
				}
				err := tc.conn.run()
				if mode == "handshake event" {
					var transportErr *qerr.TransportError
					require.ErrorAs(t, err, &transportErr)
					require.Equal(t, qerr.TransportParameterError, transportErr.ErrorCode)
				} else {
					require.ErrorIs(t, err, wantErr)
				}
				require.ErrorIs(t, context.Cause(tc.conn.Context()), err)
				require.Zero(t, queued.buffer.refCount)
				require.Zero(t, retained.buffer.refCount, "both retained views must be disposed")
				require.Empty(t, tc.conn.undecryptablePackets)
				require.Empty(t, tc.conn.undecryptablePacketsToProcess)
				late := lifetimePacket(t, tc, []byte{0})
				tc.conn.handlePacket(late)
				require.True(t, tc.conn.receivedPackets.Empty(), "cached routers cannot enqueue after termination")
				require.Zero(t, late.buffer.refCount)
			})
		})
	}
}

func TestConnectionRetainedLifetimeAdmissionRace(t *testing.T) {
	tc := newServerTestConnection(t, nil, nil, false)
	// Separate acquisitions: producers do not share a non-atomic refcount.
	admitted := lifetimePacket(t, tc, []byte{0})
	racing := lifetimePacket(t, tc, []byte{0})
	tc.conn.handlePacket(admitted)
	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Go(func() { <-start; tc.conn.handlePacket(racing) })
	workers.Go(func() { <-start; tc.conn.closePacketAdmission() })
	close(start)
	workers.Wait()
	require.True(t, tc.conn.receivedPackets.Empty())
	require.Zero(t, admitted.buffer.refCount)
	require.Zero(t, racing.buffer.refCount)
	late := lifetimePacket(t, tc, []byte{0})
	tc.conn.handlePacket(late)
	require.True(t, tc.conn.receivedPackets.Empty())
	require.Zero(t, late.buffer.refCount)
}
