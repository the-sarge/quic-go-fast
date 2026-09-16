//go:build darwin || linux || freebsd

package quic

import (
	"encoding/binary"
	"net"
	"testing"
	"time"

	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
)

type ordinaryFilterConn struct {
	*net.UDPConn
	selected *net.UDPAddr
	filtered int
}

type ordinaryReadMsgConn struct {
	*net.UDPConn
	read func([]byte, []byte) (int, int, int, *net.UDPAddr, error)
}

func (c *ordinaryReadMsgConn) ReadMsgUDP(b, oob []byte) (int, int, int, *net.UDPAddr, error) {
	return c.read(b, oob)
}

func TestOOBNonBatchWrapperMetadata(t *testing.T) {
	addr := &net.UDPAddr{IP: net.IPv6loopback, Port: 1234}
	var ecn [4]byte
	binary.NativeEndian.PutUint32(ecn[:], 3)
	control := lifetimeControlMessage(unix.IPPROTO_IPV6, unix.IPV6_TCLASS, ecn[:])
	calls := 0
	wrapper := &ordinaryReadMsgConn{UDPConn: newUDPConnLocalhost(t)}
	wrapper.read = func(b, oob []byte) (int, int, int, *net.UDPAddr, error) {
		calls++
		if calls == 1 {
			return copy(b, "metadata"), copy(oob, control), unix.MSG_TRUNC, addr, nil
		}
		return copy(b, "next"), 0, 0, addr, nil
	}
	reader, err := newConn(wrapper, true, true)
	require.NoError(t, err)
	t.Cleanup(reader.releaseReadBuffers)
	// Observe flags at the message boundary: ordinary decoding does not inspect
	// them, but the adapter must preserve the wrapper's complete read result.
	msgs := []ipv4.Message{{Buffers: [][]byte{make([]byte, 100)}, OOB: make([]byte, oobBufferSize)}}
	n, err := reader.batchConn.ReadBatch(msgs, 0)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, "metadata", string(msgs[0].Buffers[0][:msgs[0].N]))
	require.Equal(t, control, msgs[0].OOB[:msgs[0].NN])
	require.Equal(t, unix.MSG_TRUNC, msgs[0].Flags)
	require.Equal(t, addr, msgs[0].Addr)
	n, err = reader.batchConn.ReadBatch(msgs, 0)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, "next", string(msgs[0].Buffers[0][:msgs[0].N]))
	require.Zero(t, msgs[0].NN)
	require.Zero(t, msgs[0].Flags)

	// The same wrapper metadata reaches the existing decoder, and the next
	// packet has independent storage and no stale ECN metadata.
	calls = 0
	p, err := reader.ReadPacket()
	require.NoError(t, err)
	defer p.buffer.Release()
	require.Equal(t, protocol.ECNCE, p.ecn)
	require.Equal(t, addr, p.remoteAddr)
	next, err := reader.ReadPacket()
	require.NoError(t, err)
	defer next.buffer.Release()
	require.Equal(t, "metadata", string(p.data))
	require.Equal(t, "next", string(next.data))
	require.Equal(t, protocol.ECNUnsupported, next.ecn)
}

func TestOOBNonBatchWrapperReadError(t *testing.T) {
	wrapper := &ordinaryReadMsgConn{UDPConn: newUDPConnLocalhost(t)}
	wrapper.read = func(b, _ []byte) (int, int, int, *net.UDPAddr, error) {
		return copy(b, "discarded"), 0, 0, nil, net.ErrClosed
	}
	reader, err := newConn(wrapper, true, false)
	require.NoError(t, err)
	t.Cleanup(reader.releaseReadBuffers)
	p, err := reader.ReadPacket()
	require.ErrorIs(t, err, net.ErrClosed)
	require.Nil(t, p.buffer)
	for _, b := range reader.buffers {
		require.Nil(t, b)
	}
	wrapper.read = func(b, _ []byte) (int, int, int, *net.UDPAddr, error) {
		return copy(b, "fresh"), 0, 0, wrapper.LocalAddr().(*net.UDPAddr), nil
	}
	p, err = reader.ReadPacket()
	require.NoError(t, err)
	defer p.buffer.Release()
	require.Equal(t, "fresh", string(p.data))
	require.NoError(t, reader.SetReadDeadline(time.Time{}), "read cleanup leaves the caller-owned socket open")
}

type ordinaryBatchWrapper struct {
	OOBCapablePacketConn
	batchConn
}

func TestOOBReceiveSelection(t *testing.T) {
	t.Run("native", func(t *testing.T) {
		reader, err := newConn(newUDPConnLocalhost(t), true, false)
		require.NoError(t, err)
		require.IsType(t, &ipv4.PacketConn{}, reader.batchConn, "native sockets retain descriptor batching")
	})
	t.Run("participating batch wrapper", func(t *testing.T) {
		msgConn := &ordinaryReadMsgConn{UDPConn: newUDPConnLocalhost(t), read: func(_, _ []byte) (int, int, int, *net.UDPAddr, error) {
			t.Fatal("a participating wrapper must receive through ReadBatch")
			return 0, 0, 0, nil, net.ErrClosed
		}}
		wrapper := &ordinaryBatchWrapper{OOBCapablePacketConn: msgConn, batchConn: lifetimeBatchReader(func(ms []ipv4.Message) (int, error) {
			for i := range ms {
				ms[i].N = copy(ms[i].Buffers[0], []byte{byte(i)})
			}
			return len(ms), nil
		})}
		reader, err := newConn(wrapper, true, false)
		require.NoError(t, err)
		t.Cleanup(reader.releaseReadBuffers)
		for i := range batchSize {
			p, err := reader.ReadPacket()
			require.NoError(t, err)
			require.Equal(t, []byte{byte(i)}, p.data)
			p.buffer.Release()
		}
	})
}

func (c *ordinaryFilterConn) ReadMsgUDP(b, oob []byte) (int, int, int, *net.UDPAddr, error) {
	for {
		n, nn, flags, addr, err := c.UDPConn.ReadMsgUDP(b, oob)
		if err != nil || addr.String() == c.selected.String() {
			return n, nn, flags, addr, err
		}
		c.filtered++
	}
}

func TestOOBNonBatchWrapperFiltersOrdinaryDatagrams(t *testing.T) {
	for _, network := range []string{"udp4", "udp6"} {
		t.Run(network, func(t *testing.T) {
			ip := net.IPv4(127, 0, 0, 1)
			if network == "udp6" {
				ip = net.IPv6loopback
			}
			listen := func() *net.UDPConn {
				t.Helper()
				c, err := net.ListenUDP(network, &net.UDPAddr{IP: ip})
				require.NoError(t, err)
				t.Cleanup(func() { c.Close() })
				return c
			}
			udp, selected, foreign := listen(), listen(), listen()
			wrapper := &ordinaryFilterConn{UDPConn: udp, selected: selected.LocalAddr().(*net.UDPAddr)}
			reader, err := newConn(wrapper, true, true)
			require.NoError(t, err)
			t.Cleanup(reader.releaseReadBuffers)
			require.NoError(t, reader.SetReadDeadline(time.Now().Add(scaleDuration(time.Second))))

			_, err = foreign.WriteToUDP([]byte("foreign"), udp.LocalAddr().(*net.UDPAddr))
			require.NoError(t, err)
			_, err = selected.WriteToUDP([]byte("selected"), udp.LocalAddr().(*net.UDPAddr))
			require.NoError(t, err)
			p, err := reader.ReadPacket()
			require.NoError(t, err)
			defer p.buffer.Release()
			require.Equal(t, "selected", string(p.data))
			require.Equal(t, selected.LocalAddr(), p.remoteAddr)
			require.Equal(t, 1, wrapper.filtered)
			require.False(t, reader.capabilities().GRO)
		})
	}
}
