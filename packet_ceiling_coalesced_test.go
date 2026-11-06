//go:build darwin || linux || freebsd || windows

package quic

import (
	"bytes"
	"testing"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
)

func TestPacketCeilingCoalescedReader(t *testing.T) {
	for _, ceiling := range []packetCeiling{0, 1400} {
		cut := 1452
		if ceiling != 0 {
			cut = 1400
		}
		for _, tc := range []struct {
			size    int
			managed bool
			want    int
		}{
			{size: 1800, want: 1800},
			{size: protocol.MaxLargePacketBufferSize + 1, want: cut},
			{size: protocol.MaxLargePacketBufferSize + 1, managed: true, want: protocol.MaxLargePacketBufferSize + 1},
		} {
			payload := bytes.Repeat([]byte{'p'}, tc.size)
			var cursor coalescedReadCursor
			cursor.start(receivedPacket{data: payload}, 0)
			p, ok := cursor.next(tc.managed, ceiling)
			require.True(t, ok)
			require.Equal(t, payload[:tc.want], p.data)
			p.buffer.Release()
			_, ok = cursor.next(tc.managed, ceiling)
			require.False(t, ok)
		}
	}
}
