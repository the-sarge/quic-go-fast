package self_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	quicproxy "github.com/quic-go/quic-go/integrationtests/tools/proxy"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/testutils/events"

	"github.com/stretchr/testify/require"
)

func TestInitialPacketSize(t *testing.T) {
	server := newUDPConnLocalhost(t)
	client := newUDPConnLocalhost(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		quic.Dial(ctx, client, server.LocalAddr(), getTLSClientConfig(), getQuicConfig(&quic.Config{
			InitialPacketSize: 1337,
		}))
	}()

	buf := make([]byte, 2000)
	n, _, err := server.ReadFrom(buf)
	require.NoError(t, err)
	require.Equal(t, 1337, n)

	cancel()
	<-done
}

func TestPathMTUDiscovery(t *testing.T) {
	testPathMTUDiscovery(t, false)
}

func TestPathMTUDiscoveryLostProbes(t *testing.T) {
	testPathMTUDiscovery(t, true)
}

func testPathMTUDiscovery(t *testing.T, loseProbes bool) {
	t.Helper()
	rtt := scaleDuration(5 * time.Millisecond)
	const mtu = 1400
	payload := PRDataLong
	if loseProbes {
		// Keep the initial echo short, so the convergence loop below observes
		// the smaller successful probes before discovery can advance again.
		payload = PRData[:64*1024]
	}

	ln, err := quic.Listen(
		newUDPConnLocalhost(t),
		getTLSConfig(),
		getQuicConfig(&quic.Config{
			InitialPacketSize:       1234,
			DisablePathMTUDiscovery: true,
			EnableDatagrams:         true,
		}),
	)
	require.NoError(t, err)
	defer ln.Close()

	serverErrChan := make(chan error, 1)
	go func() {
		conn, err := ln.Accept(context.Background())
		if err != nil {
			serverErrChan <- err
			return
		}
		str, err := conn.AcceptStream(context.Background())
		if err != nil {
			serverErrChan <- err
			return
		}
		defer str.Close()
		if _, err := io.Copy(str, str); err != nil {
			serverErrChan <- err
			return
		}
	}()

	var mx sync.Mutex
	var maxPacketSizeServer int
	var clientPacketSizes []int
	var droppedProbeSizes []int
	proxy := &quicproxy.Proxy{
		Conn:        newUDPConnLocalhost(t),
		ServerAddr:  ln.Addr().(*net.UDPAddr),
		DelayPacket: func(quicproxy.Direction, net.Addr, net.Addr, []byte) time.Duration { return rtt / 2 },
		DropPacket: func(dir quicproxy.Direction, _, _ net.Addr, packet []byte) bool {
			if len(packet) > mtu {
				return true
			}
			mx.Lock()
			defer mx.Unlock()
			switch dir {
			case quicproxy.DirectionIncoming:
				clientPacketSizes = append(clientPacketSizes, len(packet))
				if loseProbes && len(packet) > mtu-25 && len(droppedProbeSizes) < 2 {
					// Lose two near-MTU probes independently of the path limit.
					// Their PING-only contents and loss are checked below.
					droppedProbeSizes = append(droppedProbeSizes, len(packet))
					return true
				}
			case quicproxy.DirectionOutgoing:
				if len(packet) > maxPacketSizeServer {
					maxPacketSizeServer = len(packet)
				}
			}
			return false
		},
	}
	require.NoError(t, proxy.Start())
	defer proxy.Close()

	// Make sure to use v4-only socket here.
	// We can't reliably set the DF bit on dual-stack sockets on older versions of macOS (before Sequoia).
	tr := &quic.Transport{Conn: newUDPConnLocalhost(t)}
	defer tr.Close()

	var eventRecorder events.Recorder
	conn, err := tr.Dial(
		context.Background(),
		proxy.LocalAddr(),
		getTLSClientConfig(),
		getQuicConfig(&quic.Config{
			InitialPacketSize: protocol.MinInitialPacketSize,
			EnableDatagrams:   true,
			Tracer:            newTracer(&eventRecorder),
		}),
	)
	require.NoError(t, err)
	defer conn.CloseWithError(0, "")

	err = conn.SendDatagram(make([]byte, 2000))
	require.Error(t, err)
	var datagramErr *quic.DatagramTooLargeError
	require.ErrorAs(t, err, &datagramErr)
	initialMaxDatagramSize := datagramErr.MaxDatagramPayloadSize

	str, err := conn.OpenStream()
	require.NoError(t, err)

	clientErrChan := make(chan error, 1)
	go func() {
		data := make([]byte, len(payload))
		_, err := io.ReadFull(str, data)
		if err != nil {
			clientErrChan <- err
			return
		}
		if !bytes.Equal(data, payload) {
			clientErrChan <- fmt.Errorf("echoed data doesn't match: %x", data)
			return
		}
		clientErrChan <- nil
	}()

	require.NoError(t, str.SetDeadline(time.Now().Add(20*time.Second)))
	_, err = str.Write(payload)
	require.NoError(t, err)

	select {
	case err := <-clientErrChan:
		require.NoError(t, err)
	case err := <-serverErrChan:
		t.Fatalf("server error: %v", err)
	case <-time.After(20 * time.Second):
		t.Fatal("timeout")
	}

	// The echo can finish while discovery is still waiting to send another probe.
	// Keep traffic flowing until the existing tolerance is reached, within the
	// test's existing 20-second budget. A final probe loss need not emit Done.
	extra := PRData[:16*1024]
	echoed := make([]byte, len(extra))
	for {
		updates := eventRecorder.Events(qlog.MTUUpdated{})
		if len(updates) > 0 {
			last := updates[len(updates)-1].(qlog.MTUUpdated)
			if last.Value >= mtu-25 || last.Done {
				break
			}
		}
		_, err = str.Write(extra)
		if err != nil {
			t.Fatalf("MTU discovery echo write failed: %v (MTU updates: %v)", err, eventRecorder.Events(qlog.MTUUpdated{}))
		}
		_, err = io.ReadFull(str, echoed)
		if err != nil {
			t.Fatalf("MTU discovery echo read failed: %v (MTU updates: %v)", err, eventRecorder.Events(qlog.MTUUpdated{}))
		}
		require.Equal(t, extra, echoed)
	}
	require.NoError(t, str.Close())
	trailing, err := io.ReadAll(str)
	require.NoError(t, err)
	require.Empty(t, trailing)

	// Finish ACK processing before comparing the DATAGRAM limit with MTU events.
	// MTUUpdated is recorded before the corresponding limit is published.
	require.NoError(t, conn.CloseWithError(0, ""))
	err = conn.SendDatagram(make([]byte, 2000))
	require.Error(t, err)
	require.ErrorAs(t, err, &datagramErr)
	finalMaxDatagramSize := datagramErr.MaxDatagramPayloadSize

	mx.Lock()
	defer mx.Unlock()
	require.NotEmpty(t, eventRecorder.Events(qlog.MTUUpdated{}))

	var mtus []int
	for _, ev := range eventRecorder.Events(qlog.MTUUpdated{}) {
		mtus = append(mtus, ev.(qlog.MTUUpdated).Value)
	}

	maxPacketSizeClient := mtus[len(mtus)-1]
	t.Logf("max client packet size: %d, MTU: %d", maxPacketSizeClient, mtu)
	t.Logf("max datagram size: initial: %d, final: %d", initialMaxDatagramSize, finalMaxDatagramSize)
	t.Logf("max server packet size: %d, MTU: %d", maxPacketSizeServer, mtu)

	require.GreaterOrEqual(t, maxPacketSizeClient, mtu-25)
	const maxDiff = 40 // this includes the 21 bytes for the short header, 16 bytes for the encryption tag, and framing overhead
	require.GreaterOrEqual(t, int(initialMaxDatagramSize), protocol.MinInitialPacketSize-maxDiff)
	require.GreaterOrEqual(t, int(finalMaxDatagramSize), maxPacketSizeClient-maxDiff)
	// MTU discovery was disabled on the server side
	require.Equal(t, 1234, maxPacketSizeServer)
	lost := make(map[qlog.PacketNumber]bool)
	for _, ev := range eventRecorder.Events(qlog.PacketLost{}) {
		p := ev.(qlog.PacketLost)
		if p.Header.PacketType == qlog.PacketType1RTT {
			lost[p.Header.PacketNumber] = true
		}
	}
	probeSizes := make(map[int]int)
	lostProbeSizes := make(map[int]int)
	var unresolvedProbes int
	for _, ev := range eventRecorder.Events(qlog.PacketSent{}) {
		p := ev.(qlog.PacketSent)
		if p.Header.PacketType != qlog.PacketType1RTT || p.Raw.Length <= maxPacketSizeClient {
			continue
		}
		// Only padded PING probes may exceed the discovered MTU, never data.
		require.Len(t, p.Frames, 1, "oversized packet %d", p.Header.PacketNumber)
		require.IsType(t, &qlog.PingFrame{}, p.Frames[0].Frame, "oversized packet %d", p.Header.PacketNumber)
		probeSizes[p.Raw.Length]++
		if lost[p.Header.PacketNumber] {
			lostProbeSizes[p.Raw.Length]++
		} else {
			unresolvedProbes++
		}
	}
	// A lost probe can be followed by another probe above the final estimate.
	// At most one probe may still be unresolved when the connection closes.
	require.LessOrEqual(t, unresolvedProbes, 1)
	for _, size := range clientPacketSizes {
		if size > maxPacketSizeClient {
			require.Positive(t, probeSizes[size], "observed oversized datagram must match a PING-only probe")
			probeSizes[size]--
		}
	}
	if loseProbes {
		require.Len(t, droppedProbeSizes, 2)
		for _, size := range droppedProbeSizes {
			require.Greater(t, size, maxPacketSizeClient, "exercise multiple lost probes above the final estimate")
			require.Positive(t, lostProbeSizes[size], "controlled loss must be a probe declared lost by recovery")
			lostProbeSizes[size]--
		}
	}
}
