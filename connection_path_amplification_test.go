package quic

import (
	"bytes"
	"net"
	"testing"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestConnectionUnvalidatedPathAmplification(t *testing.T) {
	for _, test := range []struct {
		name      string
		paths     int
		fundFirst bool
	}{
		{name: "repeated challenges", paths: 1},
		{name: "independent paths", paths: 2, fundFirst: true},
		{name: "all candidate slots occupied", paths: maxPaths},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			unpacker := NewMockUnpacker(ctrl)
			tc := newServerTestConnection(t, ctrl, nil, false, connectionOptUnpacker(unpacker), connectionOptHandshakeConfirmed())
			// The real packer and recovery handler have ample connection-wide credit.
			// Only bytes received from the candidate address may fund its probes.
			useSchedulingPacketPacker(t, ctrl, tc, nil)
			c := tc.conn
			received, sent := make(map[string]int), make(map[string]int)
			var writes [][]byte
			tc.sendConn.EXPECT().WriteTo(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
				func(data []byte, addr net.Addr, _ packetInfo) error {
					sent[addr.String()] += len(data)
					require.LessOrEqual(t, sent[addr.String()], 3*received[addr.String()], "unvalidated path %s", addr)
					writes = append(writes, bytes.Clone(data))
					return nil
				},
			).AnyTimes()
			for i := range 20 {
				addr := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 4242 + i%test.paths}
				size := 100
				if test.fundFirst && i == 0 {
					size = 1200
				}
				challenge := &wire.PathChallengeFrame{Data: [8]byte{byte(i + 1)}}
				payload, err := challenge.Append(nil, c.version)
				require.NoError(t, err)
				pn := protocol.PacketNumber(i + 10)
				unpacker.EXPECT().UnpackShortHeader(gomock.Any(), gomock.Any()).Return(pn, protocol.PacketNumberLen2, protocol.KeyPhaseZero, payload, nil)
				packet := getShortHeaderPacket(t, addr, tc.srcConnID, pn, nil)
				packet.data = append(packet.data, make([]byte, size-len(packet.data))...)
				received[addr.String()] += len(packet.data)
				writes = nil
				processed, err := c.handleShortHeaderPacket(packet, false, 0)
				require.NoError(t, err)
				require.True(t, processed)
				require.Len(t, writes, 1, "each challenge receives a response within the path budget")
				frames, err := decodeSchedulingFrames(writes[0], c.connIDManager.Get().Len())
				require.NoError(t, err)
				var responses int
				for _, frame := range frames {
					if response, ok := frame.(*wire.PathResponseFrame); ok {
						responses++
						require.Equal(t, challenge.Data, response.Data)
					}
				}
				require.Equal(t, 1, responses)

				// Replays cannot add credit or produce another response.
				unpacker.EXPECT().UnpackShortHeader(gomock.Any(), gomock.Any()).Return(pn, protocol.PacketNumberLen2, protocol.KeyPhaseZero, payload, nil)
				duplicate := getShortHeaderPacket(t, addr, tc.srcConnID, pn, make([]byte, 1200))
				processed, err = c.handleShortHeaderPacket(duplicate, false, 0)
				require.NoError(t, err)
				require.False(t, processed)
				require.Len(t, writes, 1)
				require.EqualValues(t, received[addr.String()], c.pathManager.getPath(addr).bytesReceived)
			}
		})
	}
}

func TestConnectionCompactPathValidation(t *testing.T) {
	for _, responseOnOldPath := range []bool{false, true} {
		t.Run(map[bool]string{false: "response on candidate path", true: "response on original path"}[responseOnOldPath], func(t *testing.T) {
			ctrl := gomock.NewController(t)
			unpacker := NewMockUnpacker(ctrl)
			tc := newServerTestConnection(t, ctrl, &Config{DisablePathMTUDiscovery: true}, false, connectionOptUnpacker(unpacker), connectionOptHandshakeConfirmed())
			useSchedulingPacketPacker(t, ctrl, tc, nil)
			c := tc.conn
			require.NoError(t, c.handleTransportParameters(&wire.TransportParameters{MaxUDPPayloadSize: 1456}))
			addr := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 4242}
			var writes [][]byte
			tc.sendConn.EXPECT().WriteTo(gomock.Any(), addr, gomock.Any()).DoAndReturn(func(data []byte, _ net.Addr, _ packetInfo) error {
				writes = append(writes, bytes.Clone(data))
				return nil
			}).AnyTimes()
			pn := protocol.PacketNumber(10)
			receive := func(remote net.Addr, frame wire.Frame, nonProbing bool) {
				t.Helper()
				var payload []byte
				if nonProbing {
					payload = []byte{1} // PING
				}
				if frame != nil {
					var err error
					payload, err = frame.Append(payload, c.version)
					require.NoError(t, err)
				}
				unpacker.EXPECT().UnpackShortHeader(gomock.Any(), gomock.Any()).Return(pn, protocol.PacketNumberLen2, protocol.KeyPhaseZero, payload, nil)
				packet := getShortHeaderPacket(t, remote, tc.srcConnID, pn, nil)
				packet.data = append(packet.data, make([]byte, 100-len(packet.data))...)
				pn++
				processed, err := c.handleShortHeaderPacket(packet, false, 0)
				require.NoError(t, err)
				require.True(t, processed)
			}
			receive(addr, nil, true)
			require.Len(t, writes, 1)
			require.Len(t, writes[0], 300)
			compactChallenge, err := decodeSchedulingProbe(writes[0], c.connIDManager.Get())
			require.NoError(t, err)
			responseAddr := net.Addr(addr)
			if responseOnOldPath {
				responseAddr = tc.remoteAddr
			}
			receive(responseAddr, &wire.PathResponseFrame{Data: compactChallenge.Data}, false)
			if responseOnOldPath {
				receive(addr, nil, true)
			}
			require.Len(t, writes, 2)
			require.Len(t, writes[1], 1200, "reachability validation permits an expanded challenge")
			expandedChallenge, err := decodeSchedulingProbe(writes[1], c.connIDManager.Get())
			require.NoError(t, err)
			require.NotEqual(t, compactChallenge.Data, expandedChallenge.Data)
			require.Equal(t, tc.remoteAddr, c.RemoteAddr(), "wait for validation of the path MTU")
			tc.sendConn.EXPECT().ChangeRemoteAddr(addr, gomock.Any())
			receive(addr, &wire.PathResponseFrame{Data: expandedChallenge.Data}, false)
			require.EqualValues(t, 1, c.pathGeneration, "switched to the validated path")
			require.Len(t, writes, 2)
		})
	}
}
