package quic

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/testdata"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

// Keep the real socket's OOB/DF interface while observing actual UDP writes.
// GSO is disabled in this test, so each observed write is one UDP datagram.
// Record the submitted length on success: Windows overlapped sends can report
// zero transferred bytes even when the datagram arrives (see windowsConn.WritePacket).
type jumboSendRecordingConn struct {
	*net.UDPConn
	largest atomic.Int64
}

func (c *jumboSendRecordingConn) record(n int) {
	for old := c.largest.Load(); int64(n) > old; old = c.largest.Load() {
		if c.largest.CompareAndSwap(old, int64(n)) {
			return
		}
	}
}

func (c *jumboSendRecordingConn) WriteMsgUDP(b, oob []byte, addr *net.UDPAddr) (int, int, error) {
	n, oobn, err := c.UDPConn.WriteMsgUDP(b, oob, addr)
	if err == nil {
		c.record(len(b))
	}
	return n, oobn, err
}

func (c *jumboSendRecordingConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	n, err := c.UDPConn.WriteTo(b, addr)
	if err == nil {
		c.record(len(b))
	}
	return n, err
}

func TestJumboStreamLoopback(t *testing.T) {
	t.Setenv("QUIC_GO_DISABLE_GSO", "true")
	for _, controller := range []string{"reno", "bbrv3"} {
		t.Run(controller, func(t *testing.T) {
			// Establish actual loopback support before any QUIC assertion may skip.
			socket, peer := newUDPConnLocalhost(t), newUDPConnLocalhost(t)
			payload := bytes.Repeat([]byte{'j'}, 8972)
			require.NoError(t, socket.SetReadDeadline(time.Now().Add(scaleDuration(time.Second))))
			if _, err := peer.WriteTo(payload, socket.LocalAddr()); err != nil {
				t.Skipf("loopback cannot send 8972 bytes: %v", err)
			}
			b := make([]byte, 8973)
			n, _, err := socket.ReadFrom(b)
			if err != nil || n != 8972 {
				t.Skipf("loopback cannot carry 8972 bytes: n=%d, err=%v", n, err)
			}
			require.Equal(t, payload, b[:n])
			require.NoError(t, socket.SetReadDeadline(time.Time{}))

			clientSocket := &jumboSendRecordingConn{UDPConn: peer}
			server := &Transport{Conn: socket}
			client := &Transport{Conn: clientSocket}
			defer server.Close()
			defer client.Close()
			require.NoError(t, server.setPacketCeilingForTesting(8972))
			require.NoError(t, client.setPacketCeilingForTesting(8972))
			conf := &Config{InitialPacketSize: 1232, EnableDatagrams: true}
			require.NoError(t, conf.SetCongestionControlV1(controller))
			serverTLS := testdata.GetTLSConfig()
			serverTLS.NextProtos = []string{"jumbo-send"}
			ln, err := server.Listen(serverTLS, conf)
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), scaleDuration(15*time.Second))
			defer cancel()
			c, err := client.Dial(ctx, ln.Addr(), &tls.Config{InsecureSkipVerify: true, NextProtos: []string{"jumbo-send"}}, conf)
			require.NoError(t, err)
			s, err := ln.Accept(ctx)
			require.NoError(t, err)
			select {
			case <-c.PathMTUSettledV1():
			case <-ctx.Done():
				t.Fatal("jumbo discovery did not settle")
			}
			ceiling, stageOne := c.PathMTUInfoV1()
			require.EqualValues(t, 8972, ceiling)
			require.EqualValues(t, 1438, stageOne)
			require.Greater(t, clientSocket.largest.Load(), int64(8900), "padded discovery probes reach jumbo size")
			clientSocket.largest.Store(0)

			data := bytes.Repeat([]byte("intact jumbo stream"), 16384)
			sent := make(chan error, 1)
			go func() {
				stream, err := c.OpenUniStreamSync(ctx)
				if err == nil {
					_, err = stream.Write(data)
					if err == nil {
						err = stream.Close()
					}
				}
				sent <- err
			}()
			stream, err := s.AcceptUniStream(ctx)
			require.NoError(t, err)
			received, err := io.ReadAll(stream)
			require.NoError(t, err)
			require.NoError(t, <-sent)
			require.Equal(t, data, received)
			require.Greater(t, clientSocket.largest.Load(), int64(8900), "ordinary STREAM packets use discovered size")
			t.Logf("%s: largest STREAM UDP payload %d", controller, clientSocket.largest.Load())
			datagram := bytes.Repeat([]byte{'d'}, 1400)
			require.NoError(t, c.SendDatagram(datagram))
			receivedDatagram, err := s.ReceiveDatagram(ctx)
			require.NoError(t, err)
			require.Equal(t, datagram, receivedDatagram)
		})
	}
}

// These fixtures have no worker; empty the jumbo pool so even a transient wrong
// borrow is observed. The queue and probe capacities pin the retained tier too.
func TestUnconfiguredEmissionKeepsStandardTier(t *testing.T) {
	jumboBufferPool = sync.Pool{New: func() any { panic("unconfigured send borrowed jumbo tier") }}
	t.Cleanup(func() {
		jumboBufferPool = sync.Pool{New: func() any { return &packetBuffer{Data: make([]byte, 0, jumboPacketBufferSize)} }}
	})
	for _, controller := range []string{"reno", "bbrv3"} {
		t.Run(controller, func(t *testing.T) {
			conf := &Config{InitialPacketSize: 1232, EnableDatagrams: true, DisablePathMTUDiscovery: true}
			require.NoError(t, conf.SetCongestionControlV1(controller))
			c := newConfiguredEmissionTestConnection(t, false, conf).conn
			require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("standard send")}))
			result := c.emission.send(monotime.Now(), true)
			require.NoError(t, result.err)
			require.True(t, result.progress)
			q := c.emission.queue.(*sendQueue)
			require.NotEmpty(t, q.queue)
			for len(q.queue) > 0 {
				entry := <-q.queue
				require.Equal(t, 1452, cap(entry.buf.Data))
				entry.release()
			}
			for _, size := range []protocol.ByteCount{1342, 1397, 1424, 1438} {
				_, buffer, err := c.emission.packer.PackMTUProbePacket(ackhandler.Frame{Frame: &wire.PingFrame{}}, size, protocol.Version1)
				require.NoError(t, err)
				require.EqualValues(t, size, buffer.Len())
				require.Equal(t, 1452, cap(buffer.Data))
				buffer.Release()
			}
		})
	}
}

func TestBBRSendPolicyPacketCeiling(t *testing.T) {
	now := monotime.Now()
	standard := newBBRSendPolicy(1_000_000, 1232, now)
	require.Panics(t, func() { standard.update(1_000_000, 1453, now) })
	jumbo := newBBRSendPolicyWithCeiling(1_000_000, 1232, 8972, now)
	require.NotPanics(t, func() { jumbo.update(1_000_000, 8957, now) })
	require.Panics(t, func() { jumbo.update(1_000_000, 8973, now) })
}
