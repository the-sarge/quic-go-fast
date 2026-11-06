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
		require.Equal(t, 1452, cap(p.buffer.Data))
		p.buffer.Release()
	}
}

func TestJumboDefaultSlotLoopback(t *testing.T) {
	socket := newUDPConnLocalhost(t)
	tr := &Transport{Conn: socket}
	require.NoError(t, tr.setPacketCeilingForTesting(8972))
	reader, err := newConnWithPacketCeiling(socket, true, false, tr.packetCeiling)
	require.NoError(t, err)
	defer reader.releaseReadBuffers()
	reader.setReadSlots(1, false)
	requireJumboLoopback(t, reader, socket, "prefix", 8972)
}

func TestJumboReadSlotsLoopback(t *testing.T) {
	socket := newUDPConnLocalhost(t)
	reader, err := newConnWithPacketCeiling(socket, true, false, 8972)
	require.NoError(t, err)
	defer reader.releaseReadBuffers()
	reader.setReadSlots(8, true)
	requireJumboLoopback(t, reader, socket, "reject", 8973)
	capacity := 0
	for _, b := range reader.buffers {
		if b != nil {
			capacity += cap(b.Data)
		}
	}
	// The last slot transferred to the consumer; seven remain with the reader.
	require.Equal(t, 62811, capacity)
	t.Log("eight posted read slots retain 71784 payload bytes; default: 11624")
}

func TestJumboCoalescedLoopback(t *testing.T) {
	socket := newUDPConnLocalhost(t)
	reader, err := newConnWithPacketCeiling(socket, true, false, 8972)
	require.NoError(t, err)
	defer reader.releaseReadBuffers()
	// A real read without segment metadata is one whole cursor datagram.
	reader.cap.GRO = true
	requireJumboLoopback(t, reader, socket, "whole", 0)
}

func TestJumboUnprocessedQueueKeepsReadSlotBeyond256(t *testing.T) {
	tc := newServerTestConnection(t, nil, nil, false, connectionOptHandshakeConfirmed())
	tc.conn.packetCeiling = 8972
	defer tc.conn.closePacketAdmission()
	queueTestPacket(t, tc, 256)
	slot := getReadSlotBufferForCeiling(8972)
	slot.Data = append(slot.Data, bytes.Repeat([]byte{'j'}, 8972)...)
	tc.conn.handlePacket(receivedPacket{buffer: slot, data: slot.Data, remoteAddr: tc.remoteAddr})
	require.Equal(t, 257, tc.conn.receivedPackets.Len())
	for range 256 {
		tc.conn.receivedPackets.PopFront().buffer.Release()
	}
	p := tc.conn.receivedPackets.PopFront()
	require.Same(t, slot, p.buffer)
	require.Equal(t, 8973, cap(p.buffer.Data))
	require.NotPanics(t, p.buffer.Release)
}
