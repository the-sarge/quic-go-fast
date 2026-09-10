//go:build darwin || linux || freebsd

package quic

import (
	"golang.org/x/sys/unix"
	"net"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/ipv4"
)

type lifetimeBatchReader func([]ipv4.Message) (int, error)

func (f lifetimeBatchReader) ReadBatch(ms []ipv4.Message, _ int) (int, error) { return f(ms) }

func newLifetimeReader(t *testing.T, read lifetimeBatchReader) *oobConn {
	t.Helper()
	c, err := newConn(newUDPConnLocalhost(t), true)
	require.NoError(t, err)
	c.batchConn = read
	t.Cleanup(c.releaseReadBuffers)
	return c
}

func TestOOBReaderBatchErrorRetry(t *testing.T) {
	calls := 0
	c := newLifetimeReader(t, func(ms []ipv4.Message) (int, error) {
		calls++
		switch calls {
		case 1:
			ms[0].N = copy(ms[0].Buffers[0], "discarded")
			return 1, net.ErrClosed
		case 2:
			ms[0].N = copy(ms[0].Buffers[0], "retry")
			return 1, nil
		default:
			t.Fatal("unexpected batch read")
			return 0, net.ErrClosed
		}
	})
	p, err := c.ReadPacket()
	require.ErrorIs(t, err, net.ErrClosed)
	require.Nil(t, p.buffer)
	p, err = c.ReadPacket()
	require.NoError(t, err)
	defer p.buffer.Release()
	require.Equal(t, "retry", string(p.data))
	require.Equal(t, 2, calls)
}

func TestOOBReaderZeroProgress(t *testing.T) {
	calls := 0
	c := newLifetimeReader(t, func(ms []ipv4.Message) (int, error) {
		calls++
		if calls == 1 {
			return 0, nil
		}
		return 0, net.ErrClosed
	})
	_, err := c.ReadPacket()
	require.ErrorIs(t, err, net.ErrClosed)
	require.Equal(t, 2, calls)
}

func TestOOBReaderBatchProgressionAndCleanup(t *testing.T) {
	if batchSize == 1 {
		t.Skip("short/full batches require the Linux batched adapter")
	}
	calls := 0
	c := newLifetimeReader(t, func(ms []ipv4.Message) (int, error) {
		calls++
		n := 2
		if calls == 2 {
			n = batchSize
		}
		for i := range n {
			ms[i].N = copy(ms[i].Buffers[0], []byte{byte(calls), byte(i)})
		}
		return n, nil
	})
	defer c.releaseReadBuffers()
	// Keep every returned packet live while the short batch is refilled and the
	// full batch is consumed, then stop with one unread packet and cached slots.
	var packets []receivedPacket
	for range 2 + batchSize + 1 {
		p, err := c.ReadPacket()
		require.NoError(t, err)
		packets = append(packets, p)
	}
	owned := c.buffers
	c.releaseReadBuffers()
	c.releaseReadBuffers() // Terminal cleanup is safe after failed-batch cleanup too.
	require.Equal(t, 3, calls)
	for i, p := range packets {
		want := []byte{2, byte(i - 2)}
		if i < 2 {
			want = []byte{1, byte(i)}
		}
		if i == 2+batchSize {
			want = []byte{3, 0}
		}
		require.Equal(t, want, p.data)
		require.Equal(t, 1, p.buffer.refCount, "returned storage remains consumer-owned")
		p.buffer.Release()
	}
	for _, b := range owned {
		if b != nil {
			require.Zero(t, b.refCount)
		}
	}
	require.NoError(t, c.SetReadDeadline(time.Time{}), "cleanup must leave the caller-owned socket open")
}

func TestOOBReaderAncillaryFailure(t *testing.T) {
	for _, tc := range []struct {
		name string
		oob  []byte
	}{
		{"malformed", []byte{1}},
		{"invalid_ipv4_tos", lifetimeControlMessage(unix.IPPROTO_IP, msgTypeIPTOS, nil)},
		{"invalid_ipv6_tclass", lifetimeControlMessage(unix.IPPROTO_IPV6, unix.IPV6_TCLASS, []byte{1})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var extracted *packetBuffer
			calls := 0
			var c *oobConn
			c = newLifetimeReader(t, func(ms []ipv4.Message) (int, error) {
				calls++
				if calls == 2 {
					ms[0].N = copy(ms[0].Buffers[0], "next")
					ms[0].NN = 0
					return 1, nil
				}
				extracted = c.buffers[0]
				ms[0].N = copy(ms[0].Buffers[0], "bad ancillary")
				ms[0].NN = copy(ms[0].OOB, tc.oob)
				return 1, nil
			})
			defer c.releaseReadBuffers()
			p, err := c.ReadPacket()
			require.Error(t, err)
			require.Nil(t, p.buffer)
			require.Zero(t, extracted.refCount)
			p, err = c.ReadPacket()
			require.NoError(t, err)
			require.Equal(t, "next", string(p.data))
			p.buffer.Release()
		})
	}
}

func lifetimeControlMessage(level, kind int32, body []byte) []byte {
	data := make([]byte, unix.CmsgSpace(len(body)))
	header := (*unix.Cmsghdr)(unsafe.Pointer(&data[0]))
	header.Level = level
	header.Type = kind
	header.SetLen(unix.CmsgLen(len(body)))
	copy(data[unix.CmsgLen(0):], body)
	return data
}

func TestOOBReaderListenerCleanup(t *testing.T) {
	calls := 0
	c := newLifetimeReader(t, func(ms []ipv4.Message) (int, error) {
		calls++
		ms[0].N = copy(ms[0].Buffers[0], "consumer")
		if calls == 2 {
			ms[0].NN = copy(ms[0].OOB, []byte{1})
		}
		return 1, nil
	})
	p, err := c.ReadPacket()
	require.NoError(t, err)
	defer p.buffer.Release()
	tr := &Transport{}
	tr.listen(c)
	for _, b := range c.buffers {
		require.Nil(t, b)
	}
	require.Equal(t, 1, p.buffer.refCount)
	require.Equal(t, "consumer", string(p.data))
	require.NoError(t, c.SetReadDeadline(time.Time{}))
}
