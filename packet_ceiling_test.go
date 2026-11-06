package quic

import (
	"bytes"
	"context"
	"crypto/tls"
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/testdata"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

func TestPacketCeilingAdvertisementAndDiscovery(t *testing.T) {
	for _, tc := range []struct {
		name          string
		configured    protocol.ByteCount
		serverDefault bool
		want          protocol.ByteCount
		probes        []protocol.ByteCount
	}{
		{name: "explicit standard", configured: 1452, want: 1452, probes: []protocol.ByteCount{1342, 1397, 1424, 1438}},
		{name: "default", want: 1452, probes: []protocol.ByteCount{1342, 1397, 1424, 1438}},
		{name: "threaded", configured: 1400, want: 1400, probes: []protocol.ByteCount{1316, 1358, 1379}},
		{name: "local ceiling below peer", configured: 1400, serverDefault: true, want: 1400, probes: []protocol.ByteCount{1316, 1358, 1379}},
		{name: "jumbo local capped by peer", configured: 8972, serverDefault: true, want: 1452, probes: []protocol.ByteCount{1342, 1397, 1424, 1438}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				clientSocket, serverSocket, closeLink := newSimnetLink(t, 5*time.Millisecond)
				defer closeLink()
				server := &Transport{Conn: serverSocket}
				client := &Transport{Conn: clientSocket}
				defer server.Close()
				defer client.Close()
				if tc.configured != 0 {
					if !tc.serverDefault {
						if tc.configured < 1452 {
							server.packetCeiling = packetCeiling(tc.configured) // retained internal threading characterization
						} else {
							require.NoError(t, server.ConfigurePacketCeilingV1(int(tc.configured)))
						}
					}
					if tc.configured < 1452 {
						client.packetCeiling = packetCeiling(tc.configured)
					} else {
						require.NoError(t, client.ConfigurePacketCeilingV1(int(tc.configured)))
					}
				}
				conf := &Config{InitialPacketSize: 1232, DisablePathMTUDiscovery: true}
				serverTLS := testdata.GetTLSConfig()
				serverTLS.NextProtos = []string{"packet-ceiling"}
				ln, err := server.Listen(serverTLS, conf)
				require.NoError(t, err)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				c, err := client.Dial(ctx, ln.Addr(), &tls.Config{InsecureSkipVerify: true, NextProtos: []string{"packet-ceiling"}}, conf)
				require.NoError(t, err)
				s, err := ln.Accept(ctx)
				require.NoError(t, err)
				require.NoError(t, c.CloseWithError(0, ""))
				require.NoError(t, s.CloseWithError(0, ""))
				// CloseWithError publishes the close before the final RTT snapshot.
				// Finish both run loops before this fixture drives discovery.
				synctest.Wait()
				serverWant := tc.want
				if tc.serverDefault {
					serverWant = 1452
				}
				require.Equal(t, serverWant, c.peerParams.MaxUDPPayloadSize, "server advertisement")
				clientWant := tc.want
				if tc.configured != 0 {
					clientWant = tc.configured
				}
				require.Equal(t, clientWant, s.peerParams.MaxUDPPayloadSize, "client advertisement")
				for _, conn := range []*Conn{c, s} {
					require.Equal(t, tc.want, conn.effectivePacketCeiling())
					requirePacketCeilingSearch(t, conn, tc.probes)
					// A new path must use the same endpoint ceiling.
					conn.resetPathPacketSize(monotime.Now())
					require.Equal(t, tc.want, conn.effectivePacketCeiling())
					requirePacketCeilingSearch(t, conn, tc.probes)
				}
			})
		})
	}
}

// Drive only successful probe receipts after both connection loops have stopped.
// Literal wire sizes pin the search independently of the finder's arithmetic.
func requirePacketCeilingSearch(t *testing.T, c *Conn, probes []protocol.ByteCount) {
	t.Helper()
	now := monotime.Now()
	c.mtuDiscoverer.Start(now)
	for _, want := range probes {
		now = now.Add(time.Second)
		require.True(t, c.mtuDiscoverer.ShouldSendProbe(now))
		ping, size := c.mtuDiscoverer.GetPing(now)
		require.Equal(t, want, size)
		ping.Handler.OnAcked(ping.Frame)
	}
	require.Equal(t, probes[len(probes)-1], c.mtuDiscoverer.CurrentSize())
	require.False(t, c.mtuDiscoverer.ShouldSendProbe(now.Add(time.Second)))
}

// ceilingPostingConn observes the system boundary's offered receive limit.
type ceilingPostingConn struct {
	net.PacketConn
	posted chan int
}

func (c *ceilingPostingConn) ReadFrom(b []byte) (int, net.Addr, error) {
	select {
	case c.posted <- len(b):
	default:
	}
	return c.PacketConn.ReadFrom(b)
}

func TestPacketCeilingBasicReader(t *testing.T) {
	for _, tc := range []struct {
		name       string
		configured protocol.ByteCount
		want       int
	}{
		{name: "default", want: 1452},
		{name: "threaded", configured: 1400, want: 1400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			socket := &ceilingPostingConn{PacketConn: newUDPConnLocalhost(t), posted: make(chan int, 1)}
			tr := &Transport{Conn: socket}
			defer tr.Close()
			if tc.configured != 0 {
				tr.packetCeiling = packetCeiling(tc.configured) // retained internal reader characterization
			}
			require.NoError(t, tr.init(false))
			select {
			case size := <-socket.posted:
				require.Equal(t, tc.want, size)
			case <-time.After(time.Second):
				t.Fatal("reader did not start")
			}
			require.Error(t, tr.ConfigurePacketCeilingV1(8972), "a reader's ceiling is fixed at construction")
		})
	}
}

func TestJumboPacketCeilingAdvertisement(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		clientSocket, serverSocket, closeLink := newSimnetLink(t, 5*time.Millisecond)
		defer closeLink()
		server := &Transport{Conn: serverSocket}
		client := &Transport{Conn: clientSocket}
		defer server.Close()
		defer client.Close()
		require.NoError(t, server.ConfigurePacketCeilingV1(8972))
		require.NoError(t, client.ConfigurePacketCeilingV1(8972))
		conf := &Config{DisablePathMTUDiscovery: true}
		serverTLS := testdata.GetTLSConfig()
		serverTLS.NextProtos = []string{"jumbo-ceiling"}
		ln, err := server.Listen(serverTLS, conf)
		require.NoError(t, err)
		c, err := client.Dial(t.Context(), ln.Addr(), &tls.Config{InsecureSkipVerify: true, NextProtos: []string{"jumbo-ceiling"}}, conf)
		require.NoError(t, err)
		s, err := ln.Accept(t.Context())
		require.NoError(t, err)
		require.NoError(t, c.CloseWithError(0, ""))
		require.NoError(t, s.CloseWithError(0, ""))
		synctest.Wait()
		require.EqualValues(t, 8972, c.peerParams.MaxUDPPayloadSize)
		require.EqualValues(t, 8972, s.peerParams.MaxUDPPayloadSize)
	})
}

func TestPacketCeilingStreamParser(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ceiling packetCeiling
		accept  bool
	}{
		{name: "default"},
		{name: "configured", ceiling: 8972, accept: true},
		{name: "above local ceiling", ceiling: 8800},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &Conn{ctx: context.Background(), config: populateConfig(nil), packetCeiling: tc.ceiling, logger: utils.DefaultLogger}
			c.preSetup()
			data := bytes.Repeat([]byte("jumbo"), 1780)
			encoded, err := (&wire.StreamFrame{StreamID: 42, DataLenPresent: true, Data: data}).Append(nil, protocol.Version1)
			require.NoError(t, err)
			frame, n, err := c.frameParser.ParseStreamFrame(wire.FrameType(encoded[0]), encoded[1:], protocol.Version1)
			if !tc.accept {
				require.Error(t, err)
				require.Nil(t, frame)
				return
			}
			require.NoError(t, err)
			require.Equal(t, len(encoded)-1, n)
			require.Equal(t, data, frame.Data)
			require.Equal(t, 8972, cap(frame.Data))
			frame.PutBack()
		})
	}
}
