package quic

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/testdata"
	"github.com/stretchr/testify/require"
)

// setPacketCeilingForTesting is the temporary configuration seam for the jumbo
// slices. F9 replaces it with the exported, validated opt-in.
func (t *Transport) setPacketCeilingForTesting(n protocol.ByteCount) error {
	t.packetIO.mutex.Lock()
	defer t.packetIO.mutex.Unlock()
	if t.packetIO.started || n < 1200 || n > 8972 {
		return errors.New("invalid test packet ceiling")
	}
	t.packetCeiling = packetCeiling(n)
	return nil
}

func TestPacketCeilingAdvertisementAndDiscovery(t *testing.T) {
	for _, tc := range []struct {
		name          string
		configured    protocol.ByteCount
		serverDefault bool
		want          protocol.ByteCount
		probes        []protocol.ByteCount
	}{
		{name: "default", want: 1452, probes: []protocol.ByteCount{1342, 1397, 1424, 1438}},
		{name: "threaded", configured: 1400, want: 1400, probes: []protocol.ByteCount{1316, 1358, 1379}},
		{name: "local ceiling below peer", configured: 1400, serverDefault: true, want: 1400, probes: []protocol.ByteCount{1316, 1358, 1379}},
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
						require.NoError(t, server.setPacketCeilingForTesting(tc.configured))
					}
					require.NoError(t, client.setPacketCeilingForTesting(tc.configured))
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
				require.Equal(t, tc.want, s.peerParams.MaxUDPPayloadSize, "client advertisement")
				for _, conn := range []*Conn{c, s} {
					requirePacketCeilingSearch(t, conn, tc.probes)
					// A new path must use the same endpoint ceiling.
					conn.resetPathPacketSize(monotime.Now())
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
				require.NoError(t, tr.setPacketCeilingForTesting(tc.configured))
			}
			require.NoError(t, tr.init(false))
			select {
			case size := <-socket.posted:
				require.Equal(t, tc.want, size)
			case <-time.After(time.Second):
				t.Fatal("reader did not start")
			}
			require.Error(t, tr.setPacketCeilingForTesting(1300), "a reader's ceiling is fixed at construction")
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
		require.NoError(t, server.setPacketCeilingForTesting(8972))
		require.NoError(t, client.setPacketCeilingForTesting(8972))
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
