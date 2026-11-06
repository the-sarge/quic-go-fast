//go:build linux || darwin || windows

package quic

import (
	"bytes"
	"context"
	"io"
	"math/rand/v2"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	quicproxy "github.com/quic-go/quic-go/internal/testutils/quicproxy"
	"github.com/stretchr/testify/require"
)

type jumboShrinkProxy struct {
	limit                   atomic.Int64
	outage                  atomic.Bool
	impair                  atomic.Int32
	firstSmallAck           chan struct{}
	secondOutage            atomic.Bool
	reordered               atomic.Uint64
	armedSmall              atomic.Bool
	once                    sync.Once
	outageSmall             atomic.Int64
	outageLargeAfterSmall   atomic.Int64
	livenessLossesRemaining atomic.Int64
	livenessDrops           atomic.Int64
}

func newJumboShrinkPair(t *testing.T, initialLimit int64) (*Conn, *Conn, *jumboShrinkProxy) {
	t.Helper()
	serverSocket, clientSocket, proxySocket := newUDPConnLocalhost(t), newUDPConnLocalhost(t), newUDPConnLocalhost(t)
	server := &Transport{Conn: &basicConn{PacketConn: serverSocket, supportsDF: true, packetCeiling: 8972}}
	client := &Transport{Conn: &basicConn{PacketConn: clientSocket, supportsDF: true, packetCeiling: 8972}}
	t.Cleanup(func() { client.Close(); server.Close() })
	require.NoError(t, server.setPacketCeilingForTesting(8972))
	require.NoError(t, client.setPacketCeilingForTesting(8972))
	control := &jumboShrinkProxy{firstSmallAck: make(chan struct{})}
	control.limit.Store(initialLimit)
	random := rand.New(rand.NewPCG(1, 2))
	proxy := &quicproxy.Proxy{
		Conn: proxySocket, ServerAddr: serverSocket.LocalAddr().(*net.UDPAddr), PacketSize: 8973,
		DropPacket: func(dir quicproxy.Direction, _, _ net.Addr, b []byte) bool {
			if control.outage.Load() {
				if dir == quicproxy.DirectionIncoming {
					if len(b) == 64 {
						control.outageSmall.Add(1)
					}
					if len(b) > 1452 && control.outageSmall.Load() > 0 {
						control.outageLargeAfterSmall.Add(1)
					}
				}
				return true
			}
			if dir == quicproxy.DirectionIncoming {
				// During restoration, admit the liveness ACK before any jumbo
				// delivery so the second-outage ordering is deterministic.
				if control.secondOutage.Load() && len(b) > 1452 {
					return true
				}
				if control.secondOutage.Load() && len(b) == 64 {
					control.armedSmall.Store(true)
				}
				if limit := control.limit.Load(); limit != 0 && int64(len(b)) > limit {
					return true
				}
				if len(b) == 64 && control.limit.Load() != 0 && control.livenessLossesRemaining.Load() > 0 {
					control.livenessLossesRemaining.Add(-1)
					control.livenessDrops.Add(1)
					return true
				}
				if control.impair.Load() == 1 {
					return random.IntN(100) < 5
				}
			} else if control.secondOutage.Load() && control.armedSmall.Load() && len(b) <= 1452 {
				control.once.Do(func() { control.outage.Store(true); control.secondOutage.Store(false); close(control.firstSmallAck) })
			}
			return false
		},
		DelayPacket: func(dir quicproxy.Direction, _, _ net.Addr, b []byte) time.Duration {
			if control.impair.Load() == 2 && dir == quicproxy.DirectionIncoming && len(b) > 1452 && control.reordered.Add(1)%3 == 0 {
				return 20 * time.Millisecond
			}
			return 0
		},
	}
	require.NoError(t, proxy.Start())
	t.Cleanup(func() { require.NoError(t, proxy.Close()) })
	conf := &Config{InitialPacketSize: 1232, EnableDatagrams: true, KeepAlivePeriod: 15 * time.Second, MaxIdleTimeout: time.Minute}
	serverTLS, clientTLS := silentDropTLSConfigs()
	ln, err := server.Listen(serverTLS, conf)
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, proxySocket.LocalAddr(), clientTLS, conf)
	require.NoError(t, err)
	s, err := ln.Accept(ctx)
	require.NoError(t, err)
	if initialLimit != 0 {
		stopped := make(chan struct{})
		trafficCtx, stop := context.WithCancel(t.Context())
		go func() {
			defer close(stopped)
			ticker := time.NewTicker(2 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-trafficCtx.Done():
					return
				case <-ticker.C:
					if c.SendDatagram([]byte("discovery traffic")) != nil {
						return
					}
				}
			}
		}()
		t.Cleanup(func() { stop(); c.CloseWithError(0, ""); <-stopped })
		defer func() { stop(); <-stopped }()
	}
	select {
	case <-c.PathMTUSettledV1():
	case <-ctx.Done():
		t.Fatal("discovery did not settle")
	}
	if initialLimit == 0 {
		data := bytes.Repeat([]byte{'j'}, 8200)
		require.NoError(t, c.SendDatagram(data))
		got, err := s.ReceiveDatagram(ctx)
		require.NoError(t, err)
		require.Equal(t, data, got, "a delivered 8200-byte message proves the path armed above 8000")
	}
	return c, s, control
}

func TestJumboFarSideShrink(t *testing.T) {
	for _, returns := range []bool{false, true} {
		name := "no_return_traffic"
		if returns {
			name = "steady_small_return_traffic"
		}
		t.Run(name, func(t *testing.T) {
			if returns {
				testJumboShrinkWithSmallReturns(t)
			} else {
				testJumboShrinkStream(t, 0)
			}
		})
	}
}

func TestJumboFarSideShrinkLostLiveness(t *testing.T) {
	testJumboShrinkStream(t, 2)
}

func testJumboShrinkStream(t *testing.T, lostLiveness int64) {
	t.Helper()
	c, s, proxy := newJumboShrinkPair(t, 0)
	proxy.livenessLossesRemaining.Store(lostLiveness)
	proxy.limit.Store(1500)
	start := time.Now()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	data := bytes.Repeat([]byte("intact through far-side shrink"), 8192)
	done := make(chan error, 1)
	t.Cleanup(func() { c.CloseWithError(0, ""); <-done })
	go func() {
		defer close(done)
		stream, err := c.OpenUniStreamSync(ctx)
		if err == nil {
			_, err = stream.Write(data)
			if err == nil {
				err = stream.Close()
			}
		}
		done <- err
	}()
	stream, err := s.AcceptUniStream(ctx)
	require.NoError(t, err)
	require.NoError(t, stream.SetReadDeadline(time.Now().Add(5*time.Second)))
	got, err := io.ReadAll(stream)
	require.NoError(t, err)
	require.Equal(t, data, got)
	require.NoError(t, <-done)
	select {
	case <-c.PathMTUFallbackV1():
	default:
		t.Fatal("far-side shrink never triggered fallback")
	}
	require.EqualValues(t, lostLiveness, proxy.livenessDrops.Load())
	t.Logf("far-side recovery %s after %d lost liveness packets (0.3s target, not a pass criterion)", time.Since(start), lostLiveness)
}

func testJumboShrinkWithSmallReturns(t *testing.T) {
	c, s, proxy := newJumboShrinkPair(t, 0)
	// Keep acknowledged small packets flowing between the jumbo losses. This
	// exercises ACK-driven loss rather than waiting for an ordinary PTO.
	startJumboTraffic(t, c, s, 64)
	startJumboTraffic(t, s, c, 64)
	time.Sleep(20 * time.Millisecond)
	proxy.limit.Store(1500)
	start := time.Now()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-c.PathMTUFallbackV1():
			t.Logf("steady-return recovery %s (0.3s target, not a pass criterion)", time.Since(start))
			return
		case <-deadline.C:
			t.Fatal("ACK-driven jumbo losses never triggered fallback")
		case <-ticker.C:
			if err := c.SendDatagram(bytes.Repeat([]byte{'j'}, 8200)); err != nil {
				select {
				case <-c.PathMTUFallbackV1():
					return
				default:
					require.NoError(t, err)
				}
			}
		}
	}
}

// The workers exercise the application seam and are joined before socket cleanup.
func startJumboTraffic(t *testing.T, sender, receiver *Conn, size int) *atomic.Int64 {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	var workers sync.WaitGroup
	received := &atomic.Int64{}
	workers.Go(func() {
		ticker := time.NewTicker(2 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if sender.SendDatagram(bytes.Repeat([]byte{'d'}, size)) != nil {
					return
				}
			}
		}
	})
	workers.Go(func() {
		for {
			b, err := receiver.ReceiveDatagram(ctx)
			if err != nil {
				return
			}
			if len(b) == size {
				received.Add(1)
			}
		}
	})
	t.Cleanup(func() { cancel(); sender.CloseWithError(0, ""); workers.Wait() })
	return received
}

func TestJumboFarSideShrinkDatagramsOnly(t *testing.T) {
	c, s, proxy := newJumboShrinkPair(t, 0)
	proxy.limit.Store(1500)
	start := time.Now()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if c.SendDatagram(bytes.Repeat([]byte{'d'}, 8200)) != nil {
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()
	t.Cleanup(func() { c.CloseWithError(0, ""); <-done })
	select {
	case <-c.PathMTUFallbackV1():
	case <-time.After(5 * time.Second):
		t.Fatal("jumbo-only shrink stalled")
	}
	t.Logf("DATAGRAM-only recovery %s (0.3s target, not a pass criterion)", time.Since(start))
	// No small application packet is supplied during transport confirmation.
	<-done
	time.Sleep(150 * time.Millisecond)
	require.Error(t, c.SendDatagram(bytes.Repeat([]byte{'j'}, 8200)))
	require.NoError(t, c.SendDatagram([]byte("after transport confirmation")))
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	b, err := s.ReceiveDatagram(ctx)
	require.NoError(t, err)
	require.Equal(t, []byte("after transport confirmation"), b)
}

func TestJumboDiscoveryLossDoesNotTriggerFallback(t *testing.T) {
	c, s, _ := newJumboShrinkPair(t, 1500)
	ceiling, stageOne := c.PathMTUInfoV1()
	require.EqualValues(t, 8972, ceiling)
	require.EqualValues(t, 1438, stageOne)
	// The estimate is atomic and published before the settled signal. With a
	// 1500-byte UDP cap, the finder's 20-byte termination interval is 1480..1500;
	// 37 bytes is this API's conservative short-header overhead allowance.
	settledSize := c.maxPayloadSizeEstimate.Load() + 37
	require.GreaterOrEqual(t, settledSize, uint32(1480))
	require.LessOrEqual(t, settledSize, uint32(1500))
	t.Logf("settled UDP payload size %d under the 1500-byte proxy cap", settledSize)
	// Discovery on a 1500-byte payload path settles just below that limit, rather
	// than permanently falling back to the standard stage.
	require.NoError(t, c.SendDatagram(bytes.Repeat([]byte{'s'}, 1430)))
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	for {
		b, err := s.ReceiveDatagram(ctx)
		require.NoError(t, err)
		if len(b) == 1430 {
			break
		}
	}
	select {
	case <-c.PathMTUFallbackV1():
		t.Fatal("discovery loss caused fallback")
	default:
	}
}

func TestJumboFarSidePreservation(t *testing.T) {
	for _, kind := range []string{"random_loss", "reordering", "outage_restore", "second_outage_after_small_ack"} {
		t.Run(kind, func(t *testing.T) {
			c, s, proxy := newJumboShrinkPair(t, 0)
			if kind == "random_loss" {
				proxy.impair.Store(1)
			}
			if kind == "reordering" {
				proxy.impair.Store(2)
			}
			if kind == "outage_restore" || kind == "second_outage_after_small_ack" {
				proxy.outage.Store(true)
			}
			if kind == "second_outage_after_small_ack" {
				proxy.secondOutage.Store(true)
			}
			received := startJumboTraffic(t, c, s, 8200)
			outage := 800 * time.Millisecond
			if kind == "outage_restore" {
				outage = 3 * time.Second
			}
			time.Sleep(outage)
			if kind == "outage_restore" {
				require.Positive(t, proxy.outageSmall.Load(), "the liveness phase must be reached")
				require.Positive(t, proxy.outageLargeAfterSmall.Load(), "ordinary PTO must still dispatch jumbo traffic after unanswered liveness")
				t.Logf("outage: %d small liveness packets; %d jumbo packets after first liveness", proxy.outageSmall.Load(), proxy.outageLargeAfterSmall.Load())
			}
			if kind == "outage_restore" || kind == "second_outage_after_small_ack" {
				proxy.outage.Store(false)
			}
			if kind == "second_outage_after_small_ack" {
				select {
				case <-proxy.firstSmallAck:
				case <-time.After(10 * time.Second):
					t.Fatal("liveness ACK was never observed")
				}
				time.Sleep(300 * time.Millisecond)
				proxy.outage.Store(false)
			}
			time.Sleep(800 * time.Millisecond)
			if kind == "outage_restore" || kind == "second_outage_after_small_ack" {
				// Ordinary PTO retains its backed-off schedule during the outage.
				require.Eventually(t, func() bool { return received.Load() > 0 }, 10*time.Second, 5*time.Millisecond, "jumbo application delivery must resume within the idle interval")
			} else {
				require.Positive(t, received.Load(), "jumbo application delivery must continue")
			}
			select {
			case <-c.PathMTUFallbackV1():
				t.Fatal("transient loss or outage caused permanent fallback")
			default:
			}
		})
	}
}
