package quic

import (
	"bytes"
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Exercise the actual socket boundary, including its existing over-limit
// behavior. A marker after an oversized datagram makes rejected reads observable.
func requireJumboLoopback(t *testing.T, reader rawConn, socket net.PacketConn, handling string, capacity int) {
	t.Helper()
	peer := newUDPConnLocalhost(t)
	require.NoError(t, socket.SetReadDeadline(time.Now().Add(scaleDuration(5*time.Second))))
	for _, size := range []int{8972, 8973} {
		payload := bytes.Repeat([]byte{'j'}, size)
		_, err := peer.WriteTo(payload, socket.LocalAddr())
		require.NoError(t, err)
		if size == 8973 && handling == "reject" {
			_, err = peer.WriteTo([]byte("after oversized"), socket.LocalAddr())
			require.NoError(t, err)
		}
		p, err := reader.ReadPacket()
		if size == 8973 && handling == "prefix" && runtime.GOOS == "windows" {
			// Winsock reports truncation as a socket error on ordinary reads.
			require.Error(t, err)
			continue
		}
		require.NoError(t, err)
		func() {
			defer p.buffer.Release()
			switch {
			case size == 8973 && handling == "reject":
				require.Equal(t, []byte("after oversized"), p.data)
			case size == 8973 && handling == "prefix":
				require.Equal(t, payload[:8972], p.data)
			default:
				require.Equal(t, payload, p.data)
			}
			if capacity != 0 {
				require.Equal(t, capacity, cap(p.buffer.Data))
			}
		}()
	}
}

func TestJumboBasicReaderLoopback(t *testing.T) {
	socket := newUDPConnLocalhost(t)
	tr := &Transport{Conn: socket}
	require.NoError(t, tr.setPacketCeilingForTesting(8972))
	reader := &basicConn{PacketConn: socket, packetCeiling: tr.packetCeiling}
	requireJumboLoopback(t, reader, socket, "prefix", 8972)
}

func TestUnconfiguredBasicReaderKeepsStandardTier(t *testing.T) {
	socket := newUDPConnLocalhost(t)
	reader := &basicConn{PacketConn: socket}
	peer := newUDPConnLocalhost(t)
	require.NoError(t, socket.SetReadDeadline(time.Now().Add(scaleDuration(5*time.Second))))
	_, err := peer.WriteTo([]byte("standard"), socket.LocalAddr())
	require.NoError(t, err)
	p, err := reader.ReadPacket()
	require.NoError(t, err)
	defer p.buffer.Release()
	require.Equal(t, []byte("standard"), p.data)
	require.Equal(t, 1452, cap(p.buffer.Data))
}
