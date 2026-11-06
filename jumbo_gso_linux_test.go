//go:build linux

package quic

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/testdata"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

// Observe the kernel's UDP_SEGMENT input while retaining real UDP, DF, TLS,
// discovery and congestion control. Only successful jumbo writes count.
type jumboGSORecordingConn struct {
	*net.UDPConn
	mu       sync.Mutex
	segments int
	parseErr error
}

func (c *jumboGSORecordingConn) WriteMsgUDP(b, oob []byte, addr *net.UDPAddr) (int, int, error) {
	n, oobn, err := c.UDPConn.WriteMsgUDP(b, oob, addr)
	if err != nil {
		return n, oobn, err
	}
	controls, parseErr := unix.ParseSocketControlMessage(oob)
	c.mu.Lock()
	defer c.mu.Unlock()
	if parseErr != nil {
		c.parseErr = parseErr
	}
	for _, control := range controls {
		if control.Header.Level == unix.IPPROTO_UDP && control.Header.Type == unix.UDP_SEGMENT && len(control.Data) >= 2 {
			segment := int(binary.NativeEndian.Uint16(control.Data))
			if segment > 8900 {
				c.segments = max(c.segments, (n+segment-1)/segment)
			}
		}
	}
	return n, oobn, err
}

func TestJumboGSOStreamLoopback(t *testing.T) {
	t.Setenv("QUIC_GO_DISABLE_GSO", "false")
	for _, controller := range []string{"reno", "bbrv3"} {
		t.Run(controller, func(t *testing.T) {
			serverSocket, clientSocket := newUDPConnLocalhost(t), newUDPConnLocalhost(t)
			recording := &jumboGSORecordingConn{UDPConn: clientSocket}
			server, client := &Transport{Conn: serverSocket}, &Transport{Conn: recording}
			defer server.Close()
			defer client.Close()
			require.NoError(t, server.setPacketCeilingForTesting(8972))
			require.NoError(t, client.setPacketCeilingForTesting(8972))
			conf := &Config{InitialPacketSize: 1232}
			require.NoError(t, conf.SetCongestionControlV1(controller))
			serverTLS := testdata.GetTLSConfig()
			serverTLS.NextProtos = []string{"jumbo-gso"}
			ln, err := server.Listen(serverTLS, conf)
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), scaleDuration(15*time.Second))
			defer cancel()
			c, err := client.Dial(ctx, ln.Addr(), &tls.Config{InsecureSkipVerify: true, NextProtos: []string{"jumbo-gso"}}, conf)
			require.NoError(t, err)
			s, err := ln.Accept(ctx)
			require.NoError(t, err)
			if !c.emission.activeConn().capabilities().GSO {
				t.Skip("Linux loopback has no UDP_SEGMENT support")
			}
			select {
			case <-c.PathMTUSettledV1():
			case <-ctx.Done():
				t.Fatal("jumbo discovery did not settle")
			}

			data := bytes.Repeat([]byte("intact segmented jumbo stream"), 131072)
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
			recording.mu.Lock()
			segments, parseErr := recording.segments, recording.parseErr
			recording.mu.Unlock()
			require.NoError(t, parseErr)
			t.Logf("%s: largest successful jumbo GSO batch %d segments; %d stream bytes intact", controller, segments, len(received))
			require.Greater(t, segments, 2, "jumbo GSO must use more than the old two-segment batch")
			require.LessOrEqual(t, segments, 7, "jumbo batches stay within the existing 64 KiB tier")
		})
	}
}

// Control only BBR's send allowance; packet registration stays with recovery.
type gsoAggregateRecovery struct {
	ackhandler.SentPacketHandler
}

func (gsoAggregateRecovery) PrepareBBRSend(monotime.Time) {}

func (gsoAggregateRecovery) SendAllowance(monotime.Time) (ackhandler.SendMode, protocol.ByteCount) {
	return ackhandler.SendAny, protocol.MaxByteCount
}

// The storage tier includes room the IPv4 aggregate cannot use. Full-size
// packets and equal short DATAGRAM segments must share the usable byte bound.
func TestJumboGSOAggregateLimit(t *testing.T) {
	for _, row := range []struct {
		name    string
		bbr     bool
		current protocol.ByteCount
		segment int
		packets int
	}{
		{"reno full", false, 4369, 4369, 16},
		{"bbrv3 full", true, 4369, 4369, 16},
		{"reno short", false, 8957, 2184, 31},
	} {
		t.Run(row.name, func(t *testing.T) {
			var test *testConnection
			now := monotime.Now()
			if row.bbr {
				test, now = newGSOEmissionTest(t, false)
			} else {
				tc, recovery := newGSOBatchTestConnection(t)
				recovery.EXPECT().ECNMode(true).Return(protocol.ECNUnsupported).AnyTimes()
				test = tc
			}
			c := test.conn
			c.packetCeiling = 8972
			c.peerParams = &wire.TransportParameters{MaxUDPPayloadSize: 8972}
			c.mtuDiscoverer = c.newPathMTUFinder(row.current, c.effectivePacketCeiling())
			c.sentPacketHandler.SetMaxDatagramSize(row.current)
			if row.bbr {
				c.sentPacketHandler = gsoAggregateRecovery{SentPacketHandler: c.sentPacketHandler}
				c.emission.bbr = newBBRSendPolicyWithCeiling(100_000_000, row.current, 8972, now)
			}
			payloadSize := gsoDatagramPayloadSize(test) - (int(row.current) - row.segment)
			var sizes []int
			for range row.packets {
				sizes = append(sizes, payloadSize)
			}
			payloads := gsoTestPayloads(sizes...)
			queueGSOTestDatagrams(t, c, payloads)
			writes := emitGSOWrites(t, test, now)
			gsoTestDatagramWrites(t, writes, payloads)
			largest := 0
			for _, write := range writes {
				require.Equal(t, 65535, write.capacity, "retain the existing storage tier")
				if len(write.packets) > 1 {
					require.Equal(t, uint16(row.segment), write.segment)
				}
				total := 0
				for _, packet := range write.packets {
					total += packet.size
				}
				require.LessOrEqual(t, total, 65507, "aggregate UDP payload fits Linux IPv4")
				largest = max(largest, len(write.packets))
			}
			require.Greater(t, largest, 2)
		})
	}
}
