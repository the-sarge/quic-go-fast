package self_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/ipv4"
)

// Linux's existing recvmmsg path can return multiple queued datagrams in one
// call. Other platforms' ipv4 batch readers can return one message at a time.
func TestCorruptionCaptureSuccessfulReadBatch(t *testing.T) {
	t.Setenv("QUIC_GO_CORRUPTION_CAPTURE_DIR", t.TempDir())
	c := newCorruptionCapture(t, "successful receive batch")
	udp, sender := newUDPConnLocalhost(t), newUDPConnLocalhost(t)
	require.NoError(t, udp.SetReadDeadline(time.Now().Add(time.Second)))
	socket := &corruptionUDPConn{UDPConn: udp, batch: ipv4.NewPacketConn(udp), capture: c, source: "socket controlled"}
	for _, data := range []string{"first", "second"} {
		_, err := sender.WriteTo([]byte(data), udp.LocalAddr())
		require.NoError(t, err)
	}
	messages := []ipv4.Message{
		{Buffers: [][]byte{make([]byte, 128)}, OOB: make([]byte, 128)},
		{Buffers: [][]byte{make([]byte, 128)}, OOB: make([]byte, 128)},
	}
	n, err := socket.ReadBatch(messages, 0)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.Equal(t, "first", string(messages[0].Buffers[0][:messages[0].N]))
	require.Equal(t, "second", string(messages[1].Buffers[0][:messages[1].N]))
	c.finish("controlled successful read")
	records := readCorruptionCapture(t, c.path)
	require.Len(t, records, 5)
	require.Contains(t, records[1].Data, "operation=read_batch batch=1 result_messages=2")
	require.Contains(t, records[2].Source, "batch=1 index=0 count=2")
	require.Contains(t, records[2].Data, "payload=6669727374")
	require.Contains(t, records[3].Source, "batch=1 index=1 count=2")
	require.Contains(t, records[3].Data, "payload=7365636f6e64")
}
