//go:build darwin && !ios && !quic_go_no_private_syscalls

package quic

import (
	"net"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"

	"github.com/stretchr/testify/require"
)

// TestSendmsgXBatchSendEndToEnd drives the production send path — sendQueue,
// sconn, oobConn, and the real kernel — on a qualified host: queued packets
// engage the batch path (submissions observed via the engaged counters), and
// every datagram arrives exactly once with its ECN mark intact at a
// v4-mapped destination.
func TestSendmsgXBatchSendEndToEnd(t *testing.T) {
	major, err := getMacOSVersion()
	require.NoError(t, err)
	if _, qualified := qualifiedDarwinKernelMajors[major]; !qualified {
		t.Skipf("running Darwin kernel major %d is not in the qualified set", major)
	}
	resetSendmsgXForTesting(t)
	sendmsgXEnsureQualified()
	require.True(t, sendmsgXAvailable())

	receiverUDP, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	receiver, err := wrapConn(receiverUDP, true)
	require.NoError(t, err)
	defer receiver.Close()

	senderUDP, err := net.ListenUDP("udp", nil) // dual-stack, the production shape
	require.NoError(t, err)
	senderRaw, err := wrapConn(senderUDP, true)
	require.NoError(t, err)
	sc := newSendConn(senderRaw, receiverUDP.LocalAddr(), packetInfo{}, utils.DefaultLogger)
	defer sc.Close()

	q := newSendQueue(sc, nil)

	subsBefore, batchedBefore, _ := sendmsgXCountersSnapshot()

	payloads := make([][]byte, sendQueueCapacity)
	bufs := make([]*packetBuffer, sendQueueCapacity)
	for i := range payloads {
		payloads[i] = []byte{'d', '1', '-', byte('a' + i)}
		bufs[i] = getPacketWithContents(payloads[i])
		q.Send(bufs[i], 0, protocol.ECT0, sendMetadata{})
	}
	done := make(chan error, 1)
	go func() { done <- q.Run() }()

	received := make(map[string]int, len(payloads))
	require.NoError(t, receiverUDP.SetReadDeadline(time.Now().Add(5*time.Second)))
	for range payloads {
		p, err := receiver.ReadPacket()
		require.NoError(t, err)
		received[string(p.data)]++
		require.Equal(t, protocol.ECT0, p.ecn, "the batch's shared ECN control message must reach the receiver")
		p.buffer.Release()
	}
	for _, payload := range payloads {
		require.Equal(t, 1, received[string(payload)], "payload %q must arrive exactly once", payload)
	}

	q.Close()
	require.NoError(t, <-done)
	for _, buf := range bufs {
		require.Zero(t, buf.refCount)
	}

	subsAfter, batchedAfter, _ := sendmsgXCountersSnapshot()
	require.Greater(t, subsAfter, subsBefore, "the batch path must have submitted at least one sendmsg_x batch")
	require.GreaterOrEqual(t, batchedAfter-batchedBefore, uint64(2), "at least one submission must have batched multiple packets")
}

// TestSendmsgXDisabledPathInert: with the kill switch set, the same
// production send path stays on the per-packet fallback — no batch
// submissions, fallback counters observed, and delivery still exact.
func TestSendmsgXDisabledPathInert(t *testing.T) {
	resetSendmsgXForTesting(t)
	t.Setenv(sendmsgXDisableEnv, "true")
	sendmsgXEnsureQualified()
	require.False(t, sendmsgXAvailable())

	receiverUDP, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	receiver, err := wrapConn(receiverUDP, true)
	require.NoError(t, err)
	defer receiver.Close()

	senderUDP, err := net.ListenUDP("udp", nil)
	require.NoError(t, err)
	senderRaw, err := wrapConn(senderUDP, true)
	require.NoError(t, err)
	sc := newSendConn(senderRaw, receiverUDP.LocalAddr(), packetInfo{}, utils.DefaultLogger)
	defer sc.Close()

	q := newSendQueue(sc, nil)
	subsBefore, _, fallbackBefore := sendmsgXCountersSnapshot()

	payloads := make([][]byte, 4)
	for i := range payloads {
		payloads[i] = []byte{'o', 'f', 'f', byte('a' + i)}
		q.Send(getPacketWithContents(payloads[i]), 0, protocol.ECT0, sendMetadata{})
	}
	done := make(chan error, 1)
	go func() { done <- q.Run() }()

	received := make(map[string]int, len(payloads))
	require.NoError(t, receiverUDP.SetReadDeadline(time.Now().Add(5*time.Second)))
	for range payloads {
		p, err := receiver.ReadPacket()
		require.NoError(t, err)
		received[string(p.data)]++
		p.buffer.Release()
	}
	for _, payload := range payloads {
		require.Equal(t, 1, received[string(payload)])
	}
	q.Close()
	require.NoError(t, <-done)

	subsAfter, _, fallbackAfter := sendmsgXCountersSnapshot()
	require.Equal(t, subsBefore, subsAfter, "the disabled path must never submit a batch")
	require.Greater(t, fallbackAfter, fallbackBefore, "fallback counters must observe the inert path")
}
