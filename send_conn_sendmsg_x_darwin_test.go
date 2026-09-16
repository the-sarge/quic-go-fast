//go:build darwin && !ios && !quic_go_no_private_syscalls

package quic

import (
	"fmt"
	"net"
	"sync"
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
	sender := &Transport{Conn: senderUDP}
	require.NoError(t, sender.init(false))
	defer sender.Close()
	sc := newSendConn(sender.conn, receiverUDP.LocalAddr(), packetInfo{}, utils.DefaultLogger)
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
	for i := range payloads {
		p, err := receiver.ReadPacket()
		require.NoError(t, err)
		require.Equal(t, payloads[i], p.data, "datagram order must survive submission")
		received[string(p.data)]++
		require.Equal(t, protocol.ECT0, p.ecn, "the batch's shared ECN control message must reach the receiver")
		p.buffer.Release()
	}
	for _, payload := range payloads {
		require.Equal(t, 1, received[string(payload)], "payload %q must arrive exactly once", payload)
	}

	q.Close()
	require.NoError(t, <-done)
	// The transport reader can reuse released send buffers from the pool.
	// Join it before inspecting those buffers after send-worker completion.
	require.NoError(t, sender.Close())
	for _, buf := range bufs {
		require.Zero(t, buf.refCount)
	}

	subsAfter, batchedAfter, _ := sendmsgXCountersSnapshot()
	require.Greater(t, subsAfter, subsBefore, "the batch path must have submitted at least one sendmsg_x batch")
	require.GreaterOrEqual(t, batchedAfter-batchedBefore, uint64(2), "at least one submission must have batched multiple packets")
}

// customWriteMsgConn is a caller-provided OOBCapablePacketConn that
// overrides WriteMsgUDP — the documented extension point. Batching bypasses
// WriteMsgUDP by design, so it must decline for this socket and every
// datagram must keep flowing through the override.
type customWriteMsgConn struct {
	*net.UDPConn
	writeMsgCalls int
}

func (c *customWriteMsgConn) WriteMsgUDP(b, oob []byte, addr *net.UDPAddr) (int, int, error) {
	c.writeMsgCalls++
	return c.UDPConn.WriteMsgUDP(b, oob, addr)
}

// TestSendmsgXCustomConnKeepsWriteMsgUDP: a caller-supplied conn with an
// overridden WriteMsgUDP never has its writes bypassed by batched raw
// submissions, even on a qualified host with the capability engaged.
func TestSendmsgXCustomConnKeepsWriteMsgUDP(t *testing.T) {
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
	defer receiverUDP.Close()

	senderUDP, err := net.ListenUDP("udp", nil)
	require.NoError(t, err)
	custom := &customWriteMsgConn{UDPConn: senderUDP}
	sender := &Transport{Conn: custom}
	require.NoError(t, sender.init(false))
	defer sender.Close()
	sc := newSendConn(sender.conn, receiverUDP.LocalAddr(), packetInfo{}, utils.DefaultLogger)
	defer sc.Close()

	q := newSendQueue(sc, nil)
	subsBefore, _, _ := sendmsgXCountersSnapshot()

	const packets = 6
	bufs := make([]*packetBuffer, packets)
	for i := range bufs {
		bufs[i] = getPacketWithContents([]byte{'c', 'w', byte('a' + i)})
		q.Send(bufs[i], 0, protocol.ECNNon, sendMetadata{})
	}
	done := make(chan error, 1)
	go func() { done <- q.Run() }()

	require.NoError(t, receiverUDP.SetReadDeadline(time.Now().Add(5*time.Second)))
	buf := make([]byte, 64)
	for range packets {
		_, _, err := receiverUDP.ReadFromUDP(buf)
		require.NoError(t, err)
	}
	q.Close()
	require.NoError(t, <-done)

	require.Equal(t, packets, custom.writeMsgCalls, "every datagram must flow through the caller's WriteMsgUDP override")
	subsAfter, _, _ := sendmsgXCountersSnapshot()
	require.Equal(t, subsBefore, subsAfter, "a custom conn must never receive batched raw submissions")
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
	sender := &Transport{Conn: senderUDP}
	require.NoError(t, sender.init(false))
	defer sender.Close()
	sc := newSendConn(sender.conn, receiverUDP.LocalAddr(), packetInfo{}, utils.DefaultLogger)
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
	for i := range payloads {
		p, err := receiver.ReadPacket()
		require.NoError(t, err)
		require.Equal(t, payloads[i], p.data, "datagram order must survive submission")
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

func TestFixedPeerNativeBatch(t *testing.T) {
	major, err := getMacOSVersion()
	require.NoError(t, err)
	if _, ok := qualifiedDarwinKernelMajors[major]; !ok {
		t.Skipf("unqualified Darwin kernel %d", major)
	}
	resetSendmsgXForTesting(t)
	sendmsgXEnsureQualified()
	require.True(t, sendmsgXAvailable())
	selected, foreign := newUDPConnLocalhost(t), newUDPConnLocalhost(t)
	tr := &Transport{Conn: newUDPConnLocalhost(t)}
	require.NoError(t, configureFixedPeer(t, tr, selected.LocalAddr().(*net.UDPAddr)))
	require.NoError(t, tr.init(false))
	defer tr.Close()
	sc := newSendConn(tr.conn, selected.LocalAddr(), packetInfo{}, utils.DefaultLogger)
	before, _, _ := sendmsgXCountersSnapshot()
	n, err := sc.sendBatch([][]byte{[]byte("one"), []byte("two")}, protocol.ECNUnsupported)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	after, _, _ := sendmsgXCountersSnapshot()
	require.Greater(t, after, before)
	sc.ChangeRemoteAddr(foreign.LocalAddr(), packetInfo{})
	n, err = sc.sendBatch([][]byte{[]byte("blocked")}, protocol.ECNUnsupported)
	require.Error(t, err)
	require.Zero(t, n)
	final, _, _ := sendmsgXCountersSnapshot()
	require.Equal(t, after, final)
	require.NoError(t, selected.SetReadDeadline(time.Now().Add(time.Second)))
	for _, want := range []string{"one", "two"} {
		b := make([]byte, 64)
		n, _, err := selected.ReadFrom(b)
		require.NoError(t, err)
		require.Equal(t, want, string(b[:n]))
	}
}

// The registered callback remains the policy boundary even when its private
// factory writer accelerates submission.
func TestExternalDarwinBatchWriterEngagement(t *testing.T) {
	major, err := getMacOSVersion()
	require.NoError(t, err)
	if _, ok := qualifiedDarwinKernelMajors[major]; !ok {
		t.Skipf("unqualified Darwin kernel %d", major)
	}
	resetSendmsgXForTesting(t)
	sendmsgXEnsureQualified()
	require.True(t, sendmsgXAvailable())
	sender, receiver := listenExternalUDP(t), listenExternalUDP(t)
	wrapper := &externalWriteObserver{PacketConn: sender}
	tr := &Transport{Conn: wrapper}
	writer, err := tr.UDPBatchWriterV1(sender)
	require.NoError(t, err)
	calls := 0
	require.NoError(t, tr.ConfigureExternalPacketIOV1(wrapper, false, func(bufs [][]byte, oob []byte, addr *net.UDPAddr) (int, error) {
		calls++
		if addr.String() != receiver.LocalAddr().String() {
			return 0, net.ErrClosed
		}
		return writer(bufs, oob, addr)
	}))
	require.NoError(t, tr.init(false))
	defer tr.Close()
	sc := newSendConn(tr.conn, receiver.LocalAddr(), packetInfo{}, utils.DefaultLogger)
	before, _, _ := sendmsgXCountersSnapshot()
	n, err := sc.sendBatch([][]byte{[]byte("first"), []byte("second")}, protocol.ECNUnsupported)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	after, _, _ := sendmsgXCountersSnapshot()
	require.Greater(t, after, before, "factory writer must engage qualified native submission")
	require.NoError(t, receiver.SetReadDeadline(time.Now().Add(time.Second)))
	for _, want := range []string{"first", "second"} {
		buf := make([]byte, 64)
		n, _, err := receiver.ReadFromUDP(buf)
		require.NoError(t, err)
		require.Equal(t, want, string(buf[:n]))
	}
	foreign := listenExternalUDP(t)
	sc.ChangeRemoteAddr(foreign.LocalAddr(), packetInfo{})
	n, err = sc.sendBatch([][]byte{[]byte("blocked"), []byte("blocked")}, protocol.ECNUnsupported)
	require.ErrorIs(t, err, net.ErrClosed)
	require.Zero(t, n)
	final, _, _ := sendmsgXCountersSnapshot()
	require.Equal(t, after, final)
	require.Equal(t, 2, calls)
	require.Zero(t, wrapper.writes.Load())
}

func TestExternalDarwinBatchWriterFallback(t *testing.T) {
	for _, mode := range []string{"disabled", "unqualified"} {
		t.Run(mode, func(t *testing.T) {
			resetSendmsgXForTesting(t)
			if mode == "disabled" {
				t.Setenv(sendmsgXDisableEnv, "true")
			} else {
				original := sendmsgXKernelMajor
				sendmsgXKernelMajor = func() (int, error) { return -1, nil }
				t.Cleanup(func() { sendmsgXKernelMajor = original })
			}
			sendmsgXEnsureQualified()
			require.False(t, sendmsgXAvailable())
			sender, receiver := listenExternalUDP(t), listenExternalUDP(t)
			writer, err := (&Transport{}).UDPBatchWriterV1(sender)
			require.NoError(t, err)
			before, _, fallback := sendmsgXCountersSnapshot()
			n, err := writer([][]byte{[]byte("first"), []byte("second")}, nil, receiver.LocalAddr().(*net.UDPAddr))
			require.NoError(t, err)
			require.Equal(t, 2, n)
			after, _, fallbackAfter := sendmsgXCountersSnapshot()
			require.Equal(t, before, after)
			require.Greater(t, fallbackAfter, fallback)
			require.NoError(t, receiver.SetReadDeadline(time.Now().Add(time.Second)))
			for _, want := range []string{"first", "second"} {
				buf := make([]byte, 64)
				n, _, err := receiver.ReadFromUDP(buf)
				require.NoError(t, err)
				require.Equal(t, want, string(buf[:n]))
			}
		})
	}
}

func TestExternalDarwinBatchWriterConcurrent(t *testing.T) {
	major, err := getMacOSVersion()
	require.NoError(t, err)
	if _, ok := qualifiedDarwinKernelMajors[major]; !ok {
		t.Skipf("unqualified Darwin kernel %d", major)
	}
	resetSendmsgXForTesting(t)
	sendmsgXEnsureQualified()
	require.True(t, sendmsgXAvailable())
	udp, err := net.ListenUDP("udp", nil)
	require.NoError(t, err)
	defer udp.Close()
	tr := &Transport{Conn: udp}
	writer, err := tr.UDPBatchWriterV1(udp)
	require.NoError(t, err)
	receivers := make([]rawConn, 2)
	for i, network := range []string{"udp4", "udp6"} {
		ip := net.IPv4(127, 0, 0, 1)
		if i == 1 {
			ip = net.IPv6loopback
		}
		conn, err := net.ListenUDP(network, &net.UDPAddr{IP: ip})
		require.NoError(t, err)
		t.Cleanup(func() { conn.Close() })
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
		receivers[i], err = wrapConn(conn, true)
		require.NoError(t, err)
	}
	// A single transport callback is shared by two independent send workers.
	require.NoError(t, tr.ConfigureExternalPacketIOV1(udp, false, func(bufs [][]byte, oob []byte, addr *net.UDPAddr) (int, error) {
		if addr.String() != receivers[0].LocalAddr().String() && addr.String() != receivers[1].LocalAddr().String() {
			return 0, net.ErrClosed
		}
		return writer(bufs, oob, addr)
	}))
	require.NoError(t, tr.init(false))
	defer tr.Close()
	before, _, _ := sendmsgXCountersSnapshot()
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	start := make(chan struct{})
	for i, receiver := range receivers {
		sc := newSendConn(tr.conn, receiver.LocalAddr(), packetInfo{}, utils.DefaultLogger)
		wg.Go(func() {
			<-start
			payloads := [][]byte{[]byte(fmt.Sprintf("%d-first", i)), []byte(fmt.Sprintf("%d-second", i))}
			n, err := sc.sendBatch(payloads, protocol.ECT0)
			if err == nil && n != 2 {
				err = fmt.Errorf("accepted %d packets", n)
			}
			errors <- err
		})
	}
	close(start)
	wg.Wait()
	for range receivers {
		require.NoError(t, <-errors)
	}
	for i, receiver := range receivers {
		for _, suffix := range []string{"first", "second"} {
			packet, err := receiver.ReadPacket()
			require.NoError(t, err)
			require.Equal(t, fmt.Sprintf("%d-%s", i, suffix), string(packet.data))
			require.Equal(t, protocol.ECT0, packet.ecn)
			packet.buffer.Release()
		}
	}
	after, _, _ := sendmsgXCountersSnapshot()
	require.Equal(t, uint64(2), after-before)
}

func TestExternalDarwinBatchWriterStandardInputs(t *testing.T) {
	resetSendmsgXForTesting(t)
	sendmsgXEnsureQualified()
	sender, receiver := listenExternalUDP(t), listenExternalUDP(t)
	writer, err := (&Transport{}).UDPBatchWriterV1(sender)
	require.NoError(t, err)
	payloads := [][]byte{[]byte("first"), []byte("second")}
	for _, addr := range []*net.UDPAddr{nil, {IP: net.IPv4(127, 0, 0, 1), Port: 65536}, {IP: net.IP{1, 2, 3}, Port: 1234}} {
		n, err := writer(payloads, nil, addr)
		require.Error(t, err)
		require.Zero(t, n)
	}
	connected, err := net.DialUDP("udp4", nil, receiver.LocalAddr().(*net.UDPAddr))
	require.NoError(t, err)
	defer connected.Close()
	writer, err = (&Transport{}).UDPBatchWriterV1(connected)
	require.NoError(t, err)
	n, err := writer(payloads, nil, receiver.LocalAddr().(*net.UDPAddr))
	require.Error(t, err, "connected WriteMsgUDP rejects an explicit destination")
	require.Zero(t, n)
	n, err = writer(payloads, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.NoError(t, receiver.SetReadDeadline(time.Now().Add(time.Second)))
	for _, want := range []string{"first", "second"} {
		buf := make([]byte, 64)
		n, _, err := receiver.ReadFromUDP(buf)
		require.NoError(t, err)
		require.Equal(t, want, string(buf[:n]))
	}
}
