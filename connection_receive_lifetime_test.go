package quic

import (
	"bytes"
	"errors"
	"testing"

	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// lifetimePacket makes the wire views share the acquired pool storage, unlike
// fixtures that carry a pool token alongside separately allocated packet bytes.
func lifetimePacket(t *testing.T, tc *testConnection, data []byte) receivedPacket {
	t.Helper()
	b := getPacketBuffer()
	b.Data = append(b.Data, data...)
	return receivedPacket{buffer: b, data: b.Data, remoteAddr: tc.remoteAddr, rcvTime: monotime.Now()}
}

func TestConnectionReceiveLifetimeMalformedFirstHeader(t *testing.T) {
	tc := newServerTestConnection(t, nil, nil, false)
	p := lifetimePacket(t, tc, []byte{0xc0})
	processed, err := tc.conn.handleOnePacket(p, 42)
	require.NoError(t, err)
	require.False(t, processed)
	require.Zero(t, p.buffer.refCount, "the processing input must be consumed without a dispatched view")
}

func TestConnectionReceiveLifetimeRejectedRetention(t *testing.T) {
	ctrl := gomock.NewController(t)
	u := NewMockUnpacker(ctrl)
	tc := newServerTestConnection(t, ctrl, nil, false, connectionOptUnpacker(u))
	// Existing retained inputs belong to the deferred-storage owner, not this call.
	tc.conn.undecryptablePackets = make([]receivedPacketWithChecksum, protocol.MaxUndecryptablePackets)
	data, err := wire.AppendShortHeader(nil, tc.srcConnID, 1, protocol.PacketNumberLen2, protocol.KeyPhaseZero)
	require.NoError(t, err)
	p := lifetimePacket(t, tc, data)
	u.EXPECT().UnpackShortHeader(gomock.Any(), gomock.Any()).Return(
		protocol.PacketNumber(0), protocol.PacketNumberLen2, protocol.KeyPhaseZero, nil, handshake.ErrKeysNotYetAvailable,
	)
	processed, err := tc.conn.handleOnePacket(p, 42)
	require.NoError(t, err)
	require.False(t, processed)
	require.Len(t, tc.conn.undecryptablePackets, protocol.MaxUndecryptablePackets)
	require.Zero(t, p.buffer.refCount, "rejected admission must finish its view")
}

func TestConnectionReceiveLifetimeVersionNegotiation(t *testing.T) {
	tc := newServerTestConnection(t, nil, nil, false)
	p := lifetimePacket(t, tc, wire.ComposeVersionNegotiation(nil, nil, []Version{Version1}))
	processed, err := tc.conn.handleOnePacket(p, 42)
	require.NoError(t, err)
	require.False(t, processed)
	require.Zero(t, p.buffer.refCount)
}

func TestConnectionReceiveLifetimeViews(t *testing.T) {
	for _, name := range []string{"short success", "long long", "long short", "malformed suffix", "mismatched suffix", "fatal handler", "retained replay", "mixed retained processed"} {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			u := NewMockUnpacker(ctrl)
			tc := newServerTestConnection(t, ctrl, nil, false, connectionOptUnpacker(u))
			long := func(pn protocol.PacketNumber, cid protocol.ConnectionID) []byte {
				hdr := wire.ExtendedHeader{
					Header:       wire.Header{Type: protocol.PacketTypeInitial, Version: Version1, DestConnectionID: cid, Length: 2},
					PacketNumber: pn, PacketNumberLen: protocol.PacketNumberLen1,
				}
				b, err := hdr.Append(nil, Version1)
				require.NoError(t, err)
				return append(b, 0) // PADDING
			}
			short, err := wire.AppendShortHeader(nil, tc.srcConnID, 3, protocol.PacketNumberLen2, protocol.KeyPhaseZero)
			require.NoError(t, err)
			short = append(short, 0)
			data := long(1, tc.srcConnID)
			switch name {
			case "short success":
				data = short
			case "long long", "mixed retained processed":
				data = append(data, long(2, tc.srcConnID)...)
			case "long short":
				data = append(data, short...)
			case "malformed suffix":
				data = append(data, 0xc0)
			case "mismatched suffix":
				data = append(data, long(2, protocol.ParseConnectionID([]byte{9, 9, 9}))...)
			}
			p := lifetimePacket(t, tc, data)
			original := bytes.Clone(p.data)
			fatal := errors.New("fatal unpack error")
			calls := 0
			checkLive := func() {
				// The active parsing hold and dispatched view must both be live.
				require.GreaterOrEqual(t, p.buffer.refCount, 2)
				require.Equal(t, original, p.data)
			}
			if name != "short success" {
				n := 1
				if name == "long long" || name == "mixed retained processed" || name == "retained replay" {
					n = 2
				}
				u.EXPECT().UnpackLongHeader(gomock.Any(), gomock.Any()).DoAndReturn(func(h *wire.Header, b []byte) (*unpackedPacket, error) {
					checkLive()
					calls++
					if name == "fatal handler" {
						return nil, fatal
					}
					if calls == 1 && (name == "retained replay" || name == "mixed retained processed") {
						return nil, handshake.ErrKeysNotYetAvailable
					}
					if name == "mixed retained processed" {
						// Model synchronous disposal by the retained-view owner while
						// the next view is being processed. I2 owns the actual drain.
						require.Len(t, tc.conn.undecryptablePackets, 1)
						retained := tc.conn.undecryptablePackets[0]
						require.Same(t, p.buffer, retained.buffer)
						retained.buffer.Decrement()
						retained.buffer.MaybeRelease()
						tc.conn.undecryptablePackets = nil
						checkLive()
					}
					return &unpackedPacket{
						hdr:             &wire.ExtendedHeader{Header: *h, PacketNumber: protocol.PacketNumber(calls), PacketNumberLen: protocol.PacketNumberLen1},
						encryptionLevel: protocol.EncryptionInitial, data: []byte{0},
					}, nil
				}).Times(n)
			}
			if name == "short success" || name == "long short" {
				u.EXPECT().UnpackShortHeader(p.rcvTime, gomock.Any()).DoAndReturn(func(monotime.Time, []byte) (protocol.PacketNumber, protocol.PacketNumberLen, protocol.KeyPhaseBit, []byte, error) {
					checkLive()
					return 3, protocol.PacketNumberLen2, protocol.KeyPhaseZero, []byte{0}, nil
				})
			}
			processed, err := tc.conn.handleOnePacket(p, 42)
			if name == "fatal handler" {
				require.ErrorIs(t, err, fatal)
				require.False(t, processed)
			} else {
				require.NoError(t, err)
				require.Equal(t, name != "retained replay", processed)
			}
			if name == "retained replay" {
				require.Equal(t, 1, p.buffer.refCount)
				require.Len(t, tc.conn.undecryptablePackets, 1)
				retained := tc.conn.undecryptablePackets[0]
				require.Same(t, p.buffer, retained.buffer)
				require.Equal(t, original, retained.data)
				require.Equal(t, p.rcvTime, retained.rcvTime)
				require.EqualValues(t, 42, retained.checksum)
				tc.conn.undecryptablePackets = nil
				processed, err = tc.conn.handleOnePacket(retained.receivedPacket, retained.checksum)
				require.NoError(t, err)
				require.True(t, processed)
			}
			require.Zero(t, p.buffer.refCount)
		})
	}
}
