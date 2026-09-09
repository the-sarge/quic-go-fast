package self_test

import (
	"context"
	"io"
	"math"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/testutils/simnet"

	"github.com/stretchr/testify/require"
)

func TestHandshakeCorruption(t *testing.T) {
	for _, tc := range []struct {
		name         string
		packetType   protocol.PacketType
		limit        int32
		corrupt      bool
		wantDeadline bool
	}{
		{name: "initial/corrupt", packetType: protocol.PacketTypeInitial, limit: 1, corrupt: true},
		{name: "initial/control", packetType: protocol.PacketTypeInitial, limit: 1},
		{name: "handshake/corrupt", packetType: protocol.PacketTypeHandshake, limit: 3, corrupt: true},
		{name: "handshake/control", packetType: protocol.PacketTypeHandshake, limit: 3},
		{name: "handshake/deadline", packetType: protocol.PacketTypeHandshake, limit: math.MaxInt32, corrupt: true, wantDeadline: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var selected atomic.Int32
				router := &callbackRouter{Router: &simnet.PerfectRouter{}}
				clientPacketConn, serverPacketConn, closeLink := newSimnetLinkWithRouter(t, 5*time.Millisecond, router)
				defer closeLink(t)
				// Mutate the copied datagram at the network boundary, before delivery.
				router.OnSendPacket = func(p simnet.Packet) {
					if p.From.String() != serverPacketConn.LocalAddr().String() || selected.Load() >= tc.limit {
						return
					}
					offset, ok := handshakeCorruptionOffset(p.Data, tc.packetType)
					if !ok {
						return
					}
					selected.Add(1)
					if tc.corrupt {
						p.Data[offset] ^= 1
					}
				}
				serverTransport := &quic.Transport{Conn: serverPacketConn}
				defer serverTransport.Close()
				clientTransport := &quic.Transport{Conn: clientPacketConn}
				defer clientTransport.Close()
				conf := getQuicConfig(&quic.Config{DisablePathMTUDiscovery: true})
				listener, err := serverTransport.Listen(getTLSConfig(), conf)
				require.NoError(t, err)
				defer listener.Close()
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				conn, err := clientTransport.Dial(ctx, serverPacketConn.LocalAddr(), getTLSClientConfig(), conf)
				if tc.wantDeadline {
					require.ErrorIs(t, err, context.DeadlineExceeded)
					require.Nil(t, conn)
					require.Greater(t, selected.Load(), int32(1), "exercise repeated handshake corruption before cancellation")
					return
				}
				require.NoError(t, err)
				defer conn.CloseWithError(0, "")
				serverConn, err := listener.Accept(ctx)
				require.NoError(t, err)
				defer serverConn.CloseWithError(0, "")
				deadline, _ := ctx.Deadline()
				stream, err := conn.OpenStreamSync(ctx)
				require.NoError(t, err)
				require.NoError(t, stream.SetDeadline(deadline))
				payload := GeneratePRData(4096)
				_, err = stream.Write(payload)
				require.NoError(t, err)
				require.NoError(t, stream.Close())
				serverStream, err := serverConn.AcceptStream(ctx)
				require.NoError(t, err)
				require.NoError(t, serverStream.SetDeadline(deadline))
				received, err := io.ReadAll(serverStream)
				require.NoError(t, err)
				require.Equal(t, payload, received)
				_, err = serverStream.Write(received)
				require.NoError(t, err)
				require.NoError(t, serverStream.Close())
				echoed, err := io.ReadAll(stream)
				require.NoError(t, err)
				require.Equal(t, payload, echoed)
				if tc.corrupt {
					require.Equal(t, tc.limit, selected.Load())
				} else {
					require.Positive(t, selected.Load())
				}
			})
		})
	}
}

// Target a protected byte in the requested QUIC packet, including when it is
// coalesced behind an Initial packet. The rest of the datagram is untouched.
func handshakeCorruptionOffset(data []byte, packetType protocol.PacketType) (int, bool) {
	offset := 0
	for len(data) > 0 && wire.IsLongHeaderPacket(data[0]) {
		header, packet, rest, err := wire.ParsePacket(data)
		if err != nil {
			return 0, false
		}
		if header.Type == packetType {
			return offset + len(packet) - 1, true
		}
		offset += len(packet)
		data = rest
	}
	return 0, false
}
