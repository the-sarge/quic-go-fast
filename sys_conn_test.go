package quic

import (
	"net"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestBasicConn(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	c := NewMockPacketConn(mockCtrl)
	addr := &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 1234}
	c.EXPECT().ReadFrom(gomock.Any()).DoAndReturn(func(b []byte) (int, net.Addr, error) {
		data := []byte("foobar")
		require.Equal(t, protocol.MaxPacketBufferSize, len(b))
		return copy(b, data), addr, nil
	})

	conn, err := wrapConn(c, true)
	require.NoError(t, err)
	p, err := conn.ReadPacket()
	require.NoError(t, err)
	defer p.buffer.Release()
	require.Equal(t, []byte("foobar"), p.data)
	require.WithinDuration(t, time.Now(), p.rcvTime.ToTime(), scaleDuration(100*time.Millisecond))
	require.Equal(t, addr, p.remoteAddr)
}

func TestBasicConnReadFailure(t *testing.T) {
	c := NewMockPacketConn(gomock.NewController(t))
	c.EXPECT().ReadFrom(gomock.Any()).DoAndReturn(func(b []byte) (int, net.Addr, error) {
		return copy(b, "discarded"), nil, net.ErrClosed
	})
	conn := &basicConn{PacketConn: c}
	p, err := conn.ReadPacket()
	require.ErrorIs(t, err, net.ErrClosed)
	require.Nil(t, p.buffer)
	require.Empty(t, p.data)
}

type cleanupRawReader struct {
	rawConn
	released bool
}

func (c *cleanupRawReader) ReadPacket() (receivedPacket, error) {
	return receivedPacket{}, net.ErrClosed
}
func (c *cleanupRawReader) releaseReadBuffers() { c.released = true }

func TestListenerReleasesReadBuffers(t *testing.T) {
	conn := &cleanupRawReader{}
	tr := &Transport{}
	tr.listen(conn)
	require.True(t, conn.released)
}
