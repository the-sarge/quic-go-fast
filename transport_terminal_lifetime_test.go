package quic

import (
	"context"
	"fmt"
	"net"
	"testing"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/testutils/events"
	"github.com/stretchr/testify/require"
)

func TestTransportTerminalLifetime(t *testing.T) {
	for _, tc := range []struct {
		name   string
		data   []byte
		reason qlog.PacketDropReason
	}{
		{name: "empty"},
		{name: "connection_id_rejection", data: []byte{0x40}, reason: qlog.PacketDropHeaderParseError},
		{name: "no_server", data: []byte{0xc0, 0, 0, 0, 1, 4, 1, 2, 3, 4}, reason: qlog.PacketDropUnknownConnectionID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tracer := &events.Recorder{}
			tr := &Transport{connIDLen: 4, logger: utils.DefaultLogger, Tracer: tracer}
			buf := getPacketBuffer()
			buf.Data = append(buf.Data, tc.data...)
			tr.handlePacket(receivedPacket{buffer: buf, data: buf.Data, remoteAddr: &net.UDPAddr{}})
			require.Zero(t, buf.refCount)
			if tc.reason != "" {
				dropped := tracer.Events()
				require.Len(t, dropped, 1)
				require.Equal(t, tc.reason, dropped[0].(qlog.PacketDropped).Trigger)
			}
		})
	}
}

func TestTransportTerminalLifetimeReset(t *testing.T) {
	token := protocol.StatelessResetToken{1, 2, 3}
	destroyed := make(chan error, 1)
	tr := &Transport{
		connIDLen: 4, logger: utils.DefaultLogger,
		resetTokens: map[protocol.StatelessResetToken]packetHandler{token: &mockPacketHandler{destruction: destroyed}},
	}
	buf := getPacketBuffer()
	buf.Data = append(buf.Data, 0x40, 9, 8, 7, 6)
	buf.Data = append(buf.Data, token[:]...)
	tr.handlePacket(receivedPacket{buffer: buf, data: buf.Data})
	require.Zero(t, buf.refCount)
	require.ErrorIs(t, <-destroyed, &StatelessResetError{})
}

func TestTransportTerminalLifetimeForward(t *testing.T) {
	connID := protocol.ParseConnectionID([]byte{1, 2, 3, 4})
	packets := make(chan receivedPacket, 1)
	tr := &Transport{
		connIDLen: 4, logger: utils.DefaultLogger,
		handlers: map[protocol.ConnectionID]packetHandler{connID: &mockPacketHandler{packets: packets}},
	}
	buf := getPacketBuffer()
	var err error
	buf.Data, err = wire.AppendShortHeader(buf.Data, connID, 1, 1, protocol.KeyPhaseZero)
	require.NoError(t, err)
	want := append([]byte(nil), buf.Data...)
	tr.handlePacket(receivedPacket{buffer: buf, data: buf.Data})
	p := <-packets
	require.Same(t, buf, p.buffer)
	require.Equal(t, 1, buf.refCount)
	require.Equal(t, want, p.data)
	p.buffer.Release()
}

func TestTransportTerminalLifetimeNonQUIC(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		name := "disabled"
		if enabled {
			name = "full_queue"
		}
		t.Run(name, func(t *testing.T) {
			tracer := &events.Recorder{}
			tr := &Transport{Tracer: tracer, nonQUICPackets: make(chan receivedPacket)}
			tr.readingNonQUICPackets.Store(enabled)
			buf := getPacketBuffer()
			buf.Data = append(buf.Data, 0, 1, 2, 3)
			tr.handlePacket(receivedPacket{buffer: buf, data: buf.Data})
			require.Zero(t, buf.refCount)
			if enabled {
				dropped := tracer.Events(qlog.PacketDropped{})
				require.Len(t, dropped, 1)
				require.Equal(t, qlog.PacketDropDOSPrevention, dropped[0].(qlog.PacketDropped).Trigger)
			}
		})
	}
}

func TestTransportTerminalLifetimeNonQUICRead(t *testing.T) {
	for _, size := range []int{8, 2} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			tr := &Transport{nonQUICPackets: make(chan receivedPacket, 1)}
			// Isolate the copy-out owner; no socket reader participates in this test.
			tr.initOnce.Do(func() {})
			tr.readingNonQUICPackets.Store(true)
			buf := getPacketBuffer()
			buf.Data = append(buf.Data, 0, 1, 2, 3)
			addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}
			tr.handlePacket(receivedPacket{buffer: buf, data: buf.Data, remoteAddr: addr})
			require.Equal(t, 1, buf.refCount)
			out := make([]byte, size)
			n, gotAddr, err := tr.ReadNonQUICPacket(context.Background(), out)
			require.NoError(t, err)
			require.Equal(t, addr, gotAddr)
			require.Equal(t, []byte{0, 1, 2, 3}[:min(size, 4)], out[:n])
			require.Zero(t, buf.refCount)
		})
	}
}
