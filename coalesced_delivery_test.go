package quic

import (
	"fmt"
	"net"
	"net/netip"
	"testing"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"

	"github.com/stretchr/testify/require"
)

func TestCoalescedDeliverySplits(t *testing.T) {
	for _, tt := range []struct {
		name string
		size int
		want []string
	}{
		{name: "singleton", size: 6, want: []string{"abcdef"}},
		{name: "missing size", size: 0, want: []string{"abcdef"}},
		{name: "negative size", size: -1, want: []string{"abcdef"}},
		{name: "multiple", size: 2, want: []string{"ab", "cd", "ef"}},
		{name: "short tail", size: 4, want: []string{"abcd", "ef"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			buffer := getCoalescedPacketBuffer()
			copy(buffer.Data[:6], "abcdef")
			var delivery coalescedDelivery
			p := delivery.accept(receivedPacket{buffer: buffer, data: buffer.Data[:6]}, tt.size)
			slab := p.buffer.slab
			require.NotNil(t, slab)
			require.Same(t, buffer, slab.buf, "adopt the filled backing without a copy")
			require.EqualValues(t, len(tt.want), slab.views.Load(), "initialize all references before publishing")
			for i, want := range tt.want {
				if i > 0 {
					var ok bool
					p, ok = delivery.next()
					require.True(t, ok)
				}
				require.Equal(t, want, string(p.data))
				require.Same(t, slab, p.buffer.slab)
				require.False(t, slab.released())
				p.buffer.Release()
			}
			require.True(t, slab.released())
			p, ok := delivery.next()
			require.False(t, ok)
			require.Equal(t, receivedPacket{}, p)
		})
	}
}

func TestCoalescedDeliveryDiscard(t *testing.T) {
	for _, releaseFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("release first=%t", releaseFirst), func(t *testing.T) {
			buffer := getCoalescedPacketBuffer()
			copy(buffer.Data[:6], "abcdef")
			var delivery coalescedDelivery
			p := delivery.accept(receivedPacket{buffer: buffer, data: buffer.Data[:6]}, 2)
			slab := p.buffer.slab
			if releaseFirst {
				p.buffer.Release()
				require.False(t, slab.released())
			}
			delivery.discard()
			delivery.discard()
			_, ok := delivery.next()
			require.False(t, ok)
			if !releaseFirst {
				require.False(t, slab.released())
				require.Equal(t, "ab", string(p.data), "cleanup preserves returned bytes")
				p.buffer.Release()
			}
			require.True(t, slab.released())
		})
	}
}

func TestCoalescedDeliveryMetadataAndClearedSlots(t *testing.T) {
	var delivery coalescedDelivery
	for range 2 {
		buffer := getCoalescedPacketBuffer()
		segments := testCoalescedSegments(3, 2)
		buffer.Data = append(buffer.Data, segments[0]...)
		buffer.Data = append(buffer.Data, segments[1]...)
		buffer.Data = append(buffer.Data, segments[2]...)
		addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 42), Port: 1234}
		original := receivedPacket{
			buffer: buffer, data: buffer.Data, remoteAddr: addr,
			rcvTime: monotime.Now(), ecn: protocol.ECT0,
			info: packetInfo{addr: netip.MustParseAddr("10.0.0.9")},
		}
		p := delivery.accept(original, 2)
		slots := delivery.pending
		// Mutating the caller's value cannot change metadata already copied.
		original.ecn = protocol.ECNCE
		original.info.addr = netip.MustParseAddr("10.0.0.99")
		for i, want := range segments {
			if i > 0 {
				var ok bool
				p, ok = delivery.next()
				require.True(t, ok)
				require.Equal(t, receivedPacket{}, slots[i-1])
			}
			require.Equal(t, want, p.data)
			require.Same(t, addr, p.remoteAddr)
			require.Equal(t, original.rcvTime, p.rcvTime)
			require.Equal(t, protocol.ECT0, p.ecn)
			require.Equal(t, packetInfo{addr: netip.MustParseAddr("10.0.0.9")}, p.info)
			p.buffer.Release()
		}
	}
}
