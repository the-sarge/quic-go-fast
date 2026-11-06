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

func TestWindowsManagedPacketCeilingGuard(t *testing.T) {
	script := &uroReadConn{t: t, payloads: [][]byte{bytes.Repeat([]byte{'p'}, 1420), []byte("next")}, oobs: [][]byte{nil, nil}, addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}}
	reader := newUROConn(t, script)
	reader.managed = true
	endpoint, acquire, err := newManagedPacketEndpoint(script)
	require.NoError(t, err)
	defer endpoint.Close()
	endpoint.(*managedPacketConn).endpoint.receiver = reader
	lease, err := acquire()
	require.NoError(t, err)
	defer lease.Close()
	tr := &Transport{Conn: lease}
	require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil))
	tr.packetCeiling = 1400 // retained internal lease-freezing characterization
	require.NoError(t, tr.beginPacketIO())
	b := make([]byte, 2000)
	n, _, err := lease.ReadFrom(b)
	require.NoError(t, err)
	require.Equal(t, "next", string(b[:n]), "the inner guard must reject the metadata-free 1420-byte read")
	require.True(t, reader.uroGuardTripped)
	require.EqualValues(t, 1, reader.receiveCoalescingFallbackCount().Load())
}

func TestWindowsManagedPacketCeilingHandback(t *testing.T) {
	script := &uroReadConn{t: t, payloads: [][]byte{bytes.Repeat([]byte{'p'}, 1420), bytes.Repeat([]byte{'q'}, 1300), bytes.Repeat([]byte{'r'}, 1420), bytes.Repeat([]byte{'s'}, 1420)}, oobs: [][]byte{nil, nil, nil, nil}, addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}}
	reader := newUROConn(t, script)
	reader.managed = true
	endpoint, acquire, err := newManagedPacketEndpoint(script)
	require.NoError(t, err)
	defer endpoint.Close()
	endpoint.(*managedPacketConn).endpoint.receiver = reader
	read := func(conn net.PacketConn, size int, fill byte) {
		t.Helper()
		b := make([]byte, 2000)
		n, _, err := conn.ReadFrom(b)
		require.NoError(t, err)
		require.Equal(t, bytes.Repeat([]byte{fill}, size), b[:n])
		require.False(t, reader.uroGuardTripped)
	}
	read(endpoint, 1420, 'p')
	lease, err := acquire()
	require.NoError(t, err)
	tr := &Transport{Conn: lease}
	require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil))
	tr.packetCeiling = 1400 // retained internal lease-freezing characterization
	require.NoError(t, tr.beginPacketIO())
	read(lease, 1300, 'q')
	require.NoError(t, lease.Close())
	read(endpoint, 1420, 'r')
	next, err := acquire()
	require.NoError(t, err)
	defer next.Close()
	nextTransport := &Transport{Conn: next}
	require.NoError(t, nextTransport.ConfigureManagedPacketIOV1(next, next, nil))
	require.NoError(t, nextTransport.beginPacketIO())
	read(next, 1420, 's')
	require.Same(t, reader, endpoint.(*managedPacketConn).endpoint.receiver, "decoder/URO state survives both leases")
}

func TestJumboWindowsOrdinaryLoopback(t *testing.T) {
	socket := newUDPConnLocalhost(t)
	reader, err := newConnWithPacketCeiling(socket, true, false, 8972)
	require.NoError(t, err)
	defer reader.releaseReadBuffers()
	requireJumboLoopback(t, reader, socket, "prefix", 8972)
}

func TestJumboWindowsCoalescedLoopback(t *testing.T) {
	socket := newUDPConnLocalhost(t)
	reader, err := newConnWithPacketCeiling(socket, true, false, 8972)
	require.NoError(t, err)
	defer reader.releaseReadBuffers()
	reader.cap.GRO = true
	requireJumboLoopback(t, reader, socket, "reject", 0)
	require.True(t, reader.uroGuardTripped, "only the over-limit read trips the guard")
}

func TestWindowsJumboManagedPacketCeilingGuard(t *testing.T) {
	script := &uroReadConn{t: t, payloads: [][]byte{bytes.Repeat([]byte{'j'}, 8972), []byte("next")}, oobs: [][]byte{nil, nil}, addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}}
	reader := newUROConn(t, script)
	reader.managed = true
	endpoint, acquire, err := newManagedPacketEndpoint(script)
	require.NoError(t, err)
	defer endpoint.Close()
	endpoint.(*managedPacketConn).endpoint.receiver = reader
	lease, err := acquire()
	require.NoError(t, err)
	defer lease.Close()
	tr := &Transport{Conn: lease}
	require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil))
	require.NoError(t, tr.ConfigurePacketCeilingV1(8972))
	require.NoError(t, tr.beginPacketIO())
	buffer := make([]byte, 10000)
	n, _, err := lease.ReadFrom(buffer)
	require.NoError(t, err)
	require.Equal(t, bytes.Repeat([]byte{'j'}, 8972), buffer[:n])
	require.False(t, reader.uroGuardTripped)
	require.Zero(t, reader.receiveCoalescingFallbackCount().Load())
}
