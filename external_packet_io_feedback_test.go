//go:build linux || darwin || windows

package quic

import (
	"context"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"

	"github.com/stretchr/testify/require"
)

func TestExternalPacketIOMessageSizeFeedback(t *testing.T) {
	udp, receiver := listenExternalUDP(t), listenExternalUDP(t)
	wrapper := &externalWriteObserver{PacketConn: udp}
	tr := &Transport{Conn: wrapper}
	writer, err := tr.UDPBatchWriterV1(udp)
	require.NoError(t, err)
	require.NoError(t, tr.ConfigureExternalPacketIOV1(wrapper, false, writer))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = tr.ReadNonQUICPacket(ctx, nil)
	require.ErrorIs(t, err, context.Canceled)
	defer tr.Close()
	feedback := &handshakeSendFeedback{wakeup: make(chan struct{}, 1)}
	q := newSendQueue(newSendConn(tr.conn, receiver.LocalAddr(), packetInfo{}, utils.DefaultLogger), feedback)
	// Use the large receive-storage tier solely to force an actual UDP size
	// rejection on loopback. Production QUIC packets are smaller than this.
	oversized := getCoalescedPacketBuffer()
	oversized.Data = oversized.Data[:65535]
	bufs := []*packetBuffer{getPacketWithContents([]byte("prefix")), oversized, getPacketWithContents([]byte("suffix"))}
	q.Send(bufs[0], 0, protocol.ECNUnsupported, sendMetadata{})
	q.Send(bufs[1], 0, protocol.ECNUnsupported, sendMetadata{handshake: true, pathGeneration: 9})
	q.Send(bufs[2], 0, protocol.ECNUnsupported, sendMetadata{})
	done := make(chan error, 1)
	go func() { done <- q.Run() }()
	// Close joins after the queued entries have been handled, including on error.
	q.Close()
	require.NoError(t, <-done)
	generation, pending := feedback.take()
	require.True(t, pending)
	require.EqualValues(t, 9, generation)
	require.NoError(t, receiver.SetReadDeadline(time.Now().Add(time.Second)))
	for _, expected := range []string{"prefix", "suffix"} {
		buf := make([]byte, 64)
		n, _, err := receiver.ReadFromUDP(buf)
		require.NoError(t, err)
		require.Equal(t, expected, string(buf[:n]))
	}
	require.EqualValues(t, 2, wrapper.writes.Load(), "only the rejected message and the suffix use ordinary submission")
	for _, buf := range bufs {
		require.Zero(t, buf.refCount)
	}
}
