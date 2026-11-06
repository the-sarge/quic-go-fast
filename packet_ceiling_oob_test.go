//go:build darwin || linux || freebsd

package quic

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/ipv4"
)

func TestPacketCeilingDefaultSlotReader(t *testing.T) {
	for _, ceiling := range []packetCeiling{0, 1400} {
		reader, err := newConnWithPacketCeiling(newUDPConnLocalhost(t), true, false, ceiling)
		require.NoError(t, err)
		defer reader.releaseReadBuffers()
		reader.setReadSlots(1, false)
		want := 1452
		if ceiling != 0 {
			want = 1400
		}
		payload := bytes.Repeat([]byte{'p'}, 1600)
		reader.batchConn = lifetimeBatchReader(func(ms []ipv4.Message) (int, error) {
			require.Len(t, ms[0].Buffers[0], want)
			ms[0].N = copy(ms[0].Buffers[0], payload)
			return 1, nil
		})
		p, err := reader.ReadPacket()
		require.NoError(t, err)
		require.Equal(t, payload[:want], p.data)
		p.buffer.Release()
	}
}
