//go:build windows

package quic

import (
	"bytes"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

type ceilingWindowsReadConn struct {
	*net.UDPConn
	payload []byte
	posted  int
}

func (c *ceilingWindowsReadConn) ReadMsgUDP(b, _ []byte) (int, int, int, *net.UDPAddr, error) {
	c.posted = len(b)
	return copy(b, c.payload), 0, 0, c.LocalAddr().(*net.UDPAddr), nil
}

func TestWindowsPacketCeilingOrdinaryReader(t *testing.T) {
	for _, ceiling := range []packetCeiling{0, 1400} {
		socket := &ceilingWindowsReadConn{UDPConn: newUDPConnLocalhost(t), payload: bytes.Repeat([]byte{'p'}, 1600)}
		reader, err := newConnWithPacketCeiling(socket, true, false, ceiling)
		require.NoError(t, err)
		want := 1452
		if ceiling != 0 {
			want = 1400
		}
		p, err := reader.ReadPacket()
		require.NoError(t, err)
		require.Equal(t, want, socket.posted)
		require.Equal(t, socket.payload[:want], p.data)
		p.buffer.Release()
	}
}

func TestWindowsPacketCeilingUROGuard(t *testing.T) {
	socket := &uroReadConn{t: t, payloads: [][]byte{bytes.Repeat([]byte{'p'}, 1420), []byte("next")}, oobs: [][]byte{nil, nil}, addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}}
	socket.UDPConn = newUDPConnLocalhost(t)
	reader, err := newConnWithPacketCeiling(socket, true, false, 1400)
	require.NoError(t, err)
	reader.cap.GRO = true // exercise the guard even on a host without URO
	defer reader.releaseReadBuffers()
	p, err := reader.ReadPacket()
	require.NoError(t, err)
	require.Equal(t, "next", string(p.data))
	require.True(t, reader.uroGuardTripped)
	p.buffer.Release()
}
