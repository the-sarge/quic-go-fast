//go:build linux || darwin || windows

package quic

import (
	"bytes"
	"context"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/testutils"
	"github.com/quic-go/quic-go/testutils/simnet"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// This fake preserves real packet construction and transport recovery. Its local
// write boundary can refuse a datagram after discovery has validated jumbo.
type jumboRefusingConn struct {
	*basicConn
	limit         atomic.Int64
	dropSize      atomic.Int64
	dropAtOrAbove atomic.Int64
	dropped       atomic.Int64
	refused       atomic.Int64
	gso           bool
	segmented     atomic.Int64
	batchRefused  atomic.Int64
	block         chan struct{}
	entered       chan struct{}
}

func (c *jumboRefusingConn) capabilities() connCapabilities {
	return connCapabilities{DF: true, GSO: c.gso}
}

func (c *jumboRefusingConn) WritePacket(b []byte, addr net.Addr, oob []byte, gso uint16, ecn protocol.ECN) (int, error) {
	size := len(b)
	if gso != 0 {
		size = int(gso)
	}
	if c.dropSize.CompareAndSwap(int64(size), 0) {
		c.dropped.Add(1)
		return len(b), nil
	}
	if limit := c.limit.Load(); limit != 0 && int64(size) > limit {
		if c.block != nil {
			select {
			case c.entered <- struct{}{}:
			default:
			}
			<-c.block
		}
		c.refused.Add(1)
		if gso != 0 {
			c.segmented.Add(1)
		}
		return 0, testutils.SendMsgSizeErr
	}
	if threshold := c.dropAtOrAbove.Load(); threshold != 0 && int64(size) >= threshold {
		c.dropped.Add(1)
		return len(b), nil
	}
	if gso != 0 {
		total := 0
		for len(b) > 0 {
			n := min(len(b), int(gso))
			if _, err := c.basicConn.WritePacket(b[:n], addr, oob, 0, ecn); err != nil {
				return total, err
			}
			total += n
			b = b[n:]
		}
		return total, nil
	}
	return c.basicConn.WritePacket(b, addr, oob, 0, ecn)
}

func newJumboRefusalPair(t *testing.T, controller, route string) (*Conn, *Conn, *jumboRefusingConn, *Transport) {
	t.Helper()
	network := &simnet.Simnet{Router: &simnet.PerfectRouter{}}
	settings := simnet.NodeBiDiLinkSettings{Uplink: simnet.LinkSettings{MTU: 9000}, Downlink: simnet.LinkSettings{MTU: 9000}, Latency: time.Millisecond}
	clientPC := network.NewEndpoint(&net.UDPAddr{IP: net.ParseIP("1.0.0.1"), Port: 9001}, settings)
	serverPC := network.NewEndpoint(&net.UDPAddr{IP: net.ParseIP("1.0.0.2"), Port: 9002}, settings)
	migrationPC := network.NewEndpoint(&net.UDPAddr{IP: net.ParseIP("1.0.0.3"), Port: 9003}, settings)
	migration := &Transport{Conn: &basicConn{PacketConn: migrationPC, supportsDF: true, packetCeiling: 8972}}
	require.NoError(t, migration.setPacketCeilingForTesting(8972))
	require.NoError(t, network.Start())
	deferCleanup := func() { network.Close(); clientPC.Close(); serverPC.Close(); migrationPC.Close() }
	t.Cleanup(deferCleanup)
	socket := &jumboRefusingConn{basicConn: &basicConn{PacketConn: clientPC, supportsDF: true, packetCeiling: 8972}}
	socket.gso = route == "segmented"
	client := &Transport{Conn: socket}
	if route == "external batch" {
		require.NoError(t, client.ConfigureExternalPacketIOV1(socket, false, func(bufs [][]byte, oob []byte, addr *net.UDPAddr) (int, error) {
			for i, b := range bufs {
				if _, err := socket.WritePacket(b, addr, oob, 0, protocol.ECNUnsupported); err != nil {
					if isSendMsgSizeErr(err) {
						socket.batchRefused.Add(1)
						return i, nil
					}
					return 0, err
				}
			}
			return len(bufs), nil
		}))
	}
	server := &Transport{Conn: &basicConn{PacketConn: serverPC, supportsDF: true, packetCeiling: 8972}}
	t.Cleanup(func() { client.Close(); server.Close(); migration.Close() })
	require.NoError(t, client.setPacketCeilingForTesting(8972))
	require.NoError(t, server.setPacketCeilingForTesting(8972))
	conf := &Config{InitialPacketSize: 1232, EnableDatagrams: true, KeepAlivePeriod: 15 * time.Second, MaxIdleTimeout: time.Minute}
	require.NoError(t, conf.SetCongestionControlV1(controller))
	serverTLS, clientTLS := silentDropTLSConfigs()
	listener, err := server.Listen(serverTLS, conf)
	require.NoError(t, err)
	t.Cleanup(func() { listener.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, listener.Addr(), clientTLS, conf)
	require.NoError(t, err)
	s, err := listener.Accept(ctx)
	require.NoError(t, err)
	select {
	case <-c.PathMTUSettledV1():
	case <-ctx.Done():
		t.Fatal("jumbo discovery did not settle")
	}
	ceiling, stageOne := c.PathMTUInfoV1()
	require.EqualValues(t, 8972, ceiling)
	require.EqualValues(t, 1438, stageOne)
	require.EqualValues(t, 8957, c.maxPacketSize())
	return c, s, socket, migration
}

func TestJumboRefusalStream(t *testing.T) {
	for _, controller := range []string{"reno", "bbrv3"} {
		routes := []string{"ordinary", "segmented", "external batch"}
		if controller == "bbrv3" {
			routes = []string{"ordinary"}
		}
		for _, route := range routes {
			t.Run(controller+"/"+route, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					c, s, socket, migration := newJumboRefusalPair(t, controller, route)
					synctest.Wait()
					if route == "external batch" {
						socket.block, socket.entered = make(chan struct{}), make(chan struct{}, 1)
					}
					socket.limit.Store(1500)
					data := bytes.Repeat([]byte("intact after local refusal"), 32768)
					finished := make(chan error, 1)
					go func() {
						stream, err := c.OpenUniStream()
						if err == nil {
							_, err = stream.Write(data)
						}
						if err == nil {
							err = stream.Close()
						}
						finished <- err
					}()

					if route == "external batch" {
						<-socket.entered
						// Hold the first scalar refusal until pacing has filled
						// a bounded group. The callback witness must not depend
						// on whether the worker wins a scheduling race.
						time.Sleep(100 * time.Millisecond)
						synctest.Wait()
						require.GreaterOrEqual(t, len(c.emission.queue.(*sendQueue).queue), 2)
						close(socket.block)
					}

					ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
					defer cancel()
					stream, err := s.AcceptUniStream(ctx)
					require.NoError(t, err, "fallback must release the refused flight")
					require.NoError(t, stream.SetReadDeadline(time.Now().Add(5*time.Second)))
					received, err := io.ReadAll(stream)
					require.NoError(t, err)
					require.NoError(t, <-finished)
					require.Equal(t, data, received)
					require.Positive(t, socket.refused.Load())
					if route == "segmented" {
						require.Positive(t, socket.segmented.Load(), "a real GSO entry must be refused")
					}
					if route == "external batch" {
						require.Positive(t, socket.batchRefused.Load(), "external callback must decline jumbo before per-entry attribution")
					}
					synctest.Wait()
					require.EqualValues(t, 1438, c.maxPacketSize())
					require.True(t, c.mtuFallback.confirmed, "an ACK at the smaller size confirms fallback")
					select {
					case <-c.PathMTUFallbackV1():
					default:
						t.Fatal("fallback was not signaled")
					}
					// A later path reset must preserve stay-down while retaining the
					// negotiated ceiling as a distinct read-only observation.
					if route == "ordinary" {
						path, err := c.AddPath(migration)
						require.NoError(t, err)
						require.NoError(t, path.Probe(ctx))
						require.NoError(t, path.Switch())
						time.Sleep(time.Second)
						synctest.Wait()
						require.Equal(t, migration.Conn.LocalAddr(), c.LocalAddr())
						require.LessOrEqual(t, c.maxPacketSize(), protocol.ByteCount(1452))
						ceiling, _ := c.PathMTUInfoV1()
						require.EqualValues(t, 8972, ceiling)
						require.EqualValues(t, 1452, c.mtuDiscoverer.ceiling)
						if c.emission.bbr != nil {
							require.EqualValues(t, 8972, c.emission.bbr.ceiling)
						}
					}
					require.NoError(t, c.CloseWithError(0, "done"))
				})
			})
		}
	}
}

func newJumboRefusalEmission(t *testing.T) *testConnection {
	t.Helper()
	tc := newConfiguredEmissionTestConnection(t, false, &Config{InitialPacketSize: 1232, EnableDatagrams: true})
	c := tc.conn
	c.packetCeiling = 8972
	c.packetSizeFeedback.wakeup = make(chan struct{}, 1)
	c.mtuFallback.dropped = make(chan struct{})
	c.emission.refusals = &c.packetSizeFeedback
	c.emission.queue.(*sendQueue).refusals = &c.packetSizeFeedback
	c.peerParams = &wire.TransportParameters{MaxUDPPayloadSize: 8972}
	c.pathGeneration = 9
	c.mtuDiscoverer = c.newPathMTUFinder(1232, 8972)
	for !c.mtuDiscoverer.done() {
		ping, _ := c.mtuDiscoverer.GetPing(monotime.Now())
		ping.Handler.OnAcked(ping.Frame)
	}
	c.sentPacketHandler.SetMaxDatagramSize(c.maxPacketSize())
	c.maxPayloadSizeEstimate.Store(uint32(estimateMaxPayloadSize(c.maxPacketSize())))
	return tc
}

func TestJumboRefusalDiscoveryProbe(t *testing.T) {
	tc := newJumboRefusalEmission(t)
	c := tc.conn
	// Restart from the tested standard stage, then validate the first jumbo
	// probe. The next refused probe must not undo that validated jumbo size.
	c.mtuDiscoverer.Reset(1438, 8972)
	ping, _ := c.mtuDiscoverer.GetPing(monotime.Now())
	ping.Handler.OnAcked(ping.Frame)
	require.EqualValues(t, 5205, c.maxPacketSize())
	result := c.emission.mtuProbe(c.mtuDiscoverer, monotime.Now())
	require.NoError(t, result.err)
	q := c.emission.queue.(*sendQueue)
	entry := <-q.queue
	require.True(t, entry.metadata.mtuProbe)
	require.EqualValues(t, 9, entry.metadata.pathGeneration)
	q.queue <- entry
	tc.sendConn.EXPECT().Write(gomock.Any(), uint16(0), gomock.Any()).Return(testutils.SendMsgSizeErr)
	close(q.closeCalled)
	require.NoError(t, q.Run())
	c.applyPacketSizeFallback(monotime.Now())
	require.EqualValues(t, 5205, c.maxPacketSize())
	require.Zero(t, c.mtuFallback.started)
}

func TestJumboRefusalPolicyAndQueuedDatagrams(t *testing.T) {
	for _, tc := range []struct {
		name          string
		current, peer protocol.ByteCount
		generation    uint64
		fallback      bool
	}{
		{"current jumbo", 8957, 8972, 9, true},
		{"stale path", 8957, 8972, 8, false},
		{"never above standard", 1438, 8972, 9, false},
		{"peer capped", 1438, 1452, 9, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newJumboRefusalEmission(t).conn
			c.peerParams.MaxUDPPayloadSize = tc.peer
			if tc.current <= 1452 {
				c.mtuDiscoverer.Reset(1232, 1452)
				for !c.mtuDiscoverer.done() {
					ping, _ := c.mtuDiscoverer.GetPing(monotime.Now())
					ping.Handler.OnAcked(ping.Frame)
				}
			}
			require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: bytes.Repeat([]byte{'j'}, 7000)}))
			small := &wire.DatagramFrame{DataLenPresent: true, Data: []byte("surviving message")}
			require.NoError(t, c.datagramQueue.Add(small))
			c.packetSizeFeedback.publish(tc.generation, 8957)
			c.applyPacketSizeFallback(monotime.Now())
			if !tc.fallback {
				require.Equal(t, tc.current, c.maxPacketSize())
				require.Zero(t, c.mtuFallback.started)
				return
			}
			require.EqualValues(t, 1438, c.maxPacketSize())
			require.EqualValues(t, 1, c.datagramQueue.sendDrops.Load())
			require.Same(t, small, c.datagramQueue.Peek())
			require.EqualValues(t, estimateMaxPayloadSize(1438), c.maxPayloadSizeEstimate.Load())
			// Duplicate and late facts do not reset the controller or signal twice.
			started := c.mtuFallback.started
			c.packetSizeFeedback.publish(9, 8957)
			c.applyPacketSizeFallback(monotime.Now().Add(time.Second))
			require.Equal(t, started, c.mtuFallback.started)
			require.False(t, c.mtuFallback.confirmed)
			// Old-path or old-send ACK facts cannot confirm the new size.
			c.confirmPacketSizeFallback(ackhandler.PacketContext{TupleGeneration: 8}, 1438, started)
			c.confirmPacketSizeFallback(ackhandler.PacketContext{TupleGeneration: 9}, 1438, started.Add(-time.Millisecond))
			require.False(t, c.mtuFallback.confirmed)
			c.confirmPacketSizeFallback(ackhandler.PacketContext{TupleGeneration: 9}, 1438, started)
			require.True(t, c.mtuFallback.confirmed)
		})
	}
}

func TestJumboRefusalFeedbackLifetimeAndSegmentSize(t *testing.T) {
	for _, tc := range []struct {
		name            string
		size            int
		segment         uint16
		probe, eligible bool
	}{
		{"ordinary", 7000, 0, false, true},
		{"segmented jumbo", 14000, 7000, false, true},
		{"large batch standard segments", 7000, 1400, false, false},
		{"discovery probe", 7000, 0, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				conn := NewMockSendConn(gomock.NewController(t))
				feedback := &packetSizeFeedback{wakeup: make(chan struct{}, 1)}
				q := newSendQueue(conn, nil).(*sendQueue)
				q.refusals = feedback
				buf := getLargePacketBuffer()
				buf.Data = buf.Data[:tc.size]
				conn.EXPECT().Write(gomock.Any(), tc.segment, protocol.ECNNon).Return(testutils.SendMsgSizeErr)
				q.Send(buf, tc.segment, protocol.ECNNon, sendMetadata{pathGeneration: 9, mtuProbe: tc.probe})
				startAndFinishQueue(t, q, nil)
				generation, size, pending := feedback.take()
				require.Equal(t, tc.eligible, pending)
				if pending {
					require.EqualValues(t, 9, generation)
					if tc.segment != 0 {
						require.EqualValues(t, tc.segment, size)
					} else {
						require.EqualValues(t, tc.size, size)
					}
				}
				// A stopped queue has no worker that could publish another fact.
				_, _, pending = feedback.take()
				require.False(t, pending)
			})
		})
	}
	feedback := &packetSizeFeedback{wakeup: make(chan struct{}, 1)}
	feedback.publish(9, 7000)
	feedback.publish(8, 8000)
	generation, size, pending := feedback.take()
	require.True(t, pending)
	require.EqualValues(t, 9, generation)
	require.EqualValues(t, 7000, size)
}

// The connection stops making policy decisions before draining its worker. A
// local refusal that resolves during that drain cannot change size after close.
func TestJumboRefusalDuringShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, _, socket, _ := newJumboRefusalPair(t, "reno", "ordinary")
		synctest.Wait()
		socket.block, socket.entered = make(chan struct{}), make(chan struct{}, 1)
		socket.limit.Store(1500)
		require.NoError(t, c.SendDatagram(bytes.Repeat([]byte{'x'}, 7000)))
		<-socket.entered
		done := make(chan error, 1)
		go func() { done <- c.CloseWithError(0, "done") }()
		synctest.Wait() // run loop has exited; Close is joining the blocked worker
		require.NotNil(t, c.closeErr.Load())
		close(socket.block)
		require.NoError(t, <-done)
		synctest.Wait()
		require.EqualValues(t, 8957, c.maxPacketSize())
		select {
		case <-c.PathMTUFallbackV1():
			t.Fatal("shutdown feedback changed packet size")
		default:
		}
	})
}

// With only one unreliable application message, the transport must supply both
// confirmation and the later small ACK that distinguishes a lost probe from an outage.
func TestJumboFallbackDatagramOnly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, _, socket, _ := newJumboRefusalPair(t, "reno", "ordinary")
		synctest.Wait()
		socket.limit.Store(1500)
		socket.dropSize.Store(1438)
		require.NoError(t, c.SendDatagram(bytes.Repeat([]byte{'d'}, 7000)))
		<-c.PathMTUFallbackV1()
		time.Sleep(time.Second)
		synctest.Wait()
		require.EqualValues(t, 1, socket.dropped.Load())
		require.Equal(t, 1, c.mtuFallback.lost, "the first probe needs a deadline and the follow-up PING ACK for its verdict")
		require.True(t, c.mtuFallback.confirmed, "DATAGRAM-only fallback stalled without transport confirmation")
		require.EqualValues(t, 1438, c.maxPacketSize())
		require.NoError(t, c.CloseWithError(0, "done"))
	})
}

func TestJumboFallbackReachesInitialSize(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, _, socket, _ := newJumboRefusalPair(t, "reno", "ordinary")
		synctest.Wait()
		socket.limit.Store(1300)
		require.NoError(t, c.SendDatagram(bytes.Repeat([]byte{'d'}, 7000)))
		<-c.PathMTUFallbackV1()
		time.Sleep(time.Second)
		synctest.Wait()
		require.True(t, c.mtuFallback.confirmed)
		require.EqualValues(t, 1232, c.mtuFallback.size, "three lost standard probes reach the initial size")
		require.EqualValues(t, 1232, c.maxPacketSize())
		require.EqualValues(t, 1452, c.mtuDiscoverer.ceiling, "stage two stays disabled")
		require.EqualValues(t, 1342, c.mtuDiscoverer.inFlight, "confirmation restarts stage one's first probe")
		require.Zero(t, c.mtuDiscoverer.StageOneSize(), "the new search is still running")
		require.NoError(t, c.CloseWithError(0, "done"))
	})
}

func TestJumboFallbackAllProbesLost(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, _, socket, _ := newJumboRefusalPair(t, "reno", "ordinary")
		synctest.Wait()
		socket.limit.Store(1500)
		socket.dropAtOrAbove.Store(1232)
		require.NoError(t, c.SendDatagram(bytes.Repeat([]byte{'d'}, 7000)))
		<-c.PathMTUFallbackV1()
		time.Sleep(time.Second)
		synctest.Wait()
		require.EqualValues(t, 6, socket.dropped.Load(), "only three probes at each size")
		require.False(t, c.mtuFallback.active)
		require.False(t, c.mtuFallback.confirmed)
		require.Equal(t, 3, c.mtuFallback.lost)
		require.EqualValues(t, 1232, c.maxPacketSize())
		require.Zero(t, c.packetSizeRecoveryTime())
		require.NoError(t, c.Context().Err(), "probe exhaustion doesn't close the connection")
		// Ordinary keepalives continue after recovery has stopped.
		time.Sleep(61 * time.Second)
		synctest.Wait()
		require.EqualValues(t, 6, socket.dropped.Load(), "recovery stays stopped across ordinary keepalives")
		require.NoError(t, c.Context().Err())
		require.NoError(t, c.CloseWithError(0, "done"))
	})
}

func TestJumboFallbackPathChangeCancelsRecovery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, _, socket, migration := newJumboRefusalPair(t, "reno", "ordinary")
		synctest.Wait()
		socket.limit.Store(1500)
		socket.dropAtOrAbove.Store(1232)
		require.NoError(t, c.SendDatagram(bytes.Repeat([]byte{'d'}, 7000)))
		<-c.PathMTUFallbackV1()
		synctest.Wait()
		require.NotNil(t, c.mtuFallback.probe)
		oldGeneration := c.pathGeneration
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		path, err := c.AddPath(migration)
		require.NoError(t, err)
		require.NoError(t, path.Probe(ctx))
		require.NoError(t, path.Switch())
		time.Sleep(time.Second)
		synctest.Wait()
		require.Greater(t, c.pathGeneration, oldGeneration)
		require.False(t, c.mtuFallback.active)
		require.Nil(t, c.mtuFallback.probe)
		require.Zero(t, c.mtuFallback.lost, "migration loss callbacks aren't recovery verdicts")
		require.EqualValues(t, 1438, c.mtuFallback.size, "migration doesn't trigger the initial-size fallback")
		require.LessOrEqual(t, c.maxPacketSize(), protocol.ByteCount(1452))
		require.NoError(t, c.CloseWithError(0, "done"))
	})
}

func TestJumboFallbackProbeDeadlineAndLaterAck(t *testing.T) {
	tc := newJumboRefusalEmission(t)
	c := tc.conn
	now := monotime.Now()
	c.packetSizeFeedback.publish(c.pathGeneration, 8957)
	c.applyPacketSizeFallback(now)
	require.NoError(t, c.triggerSending(now).err)
	probe := c.mtuFallback.probe
	require.NotNil(t, probe)
	require.Zero(t, c.sentPacketHandler.GetLossDetectionTimeout(), "a recovery probe doesn't arm the ordinary loss alarm")
	c.advancePacketSizeRecovery(probe.deadline)
	require.Zero(t, c.mtuFallback.lost, "a deadline alone is an outage, not loss")
	require.NoError(t, c.triggerSending(probe.deadline).err)
	require.NotZero(t, c.sentPacketHandler.GetLossDetectionTimeout(), "the follow-up PING keeps ordinary loss rules")
	c.confirmPacketSizeFallback(ackhandler.PacketContext{TupleGeneration: c.pathGeneration}, 64, probe.sent)
	require.False(t, probe.laterSmallAck, "the small packet must have been sent later")
	c.confirmPacketSizeFallback(ackhandler.PacketContext{TupleGeneration: c.pathGeneration}, 64, probe.deadline)
	c.advancePacketSizeRecovery(probe.deadline.Add(-time.Nanosecond))
	require.Zero(t, c.mtuFallback.lost, "a later ACK alone isn't a loss verdict")
	c.advancePacketSizeRecovery(probe.deadline)
	require.Equal(t, 1, c.mtuFallback.lost)
	q := c.emission.queue.(*sendQueue)
	tc.sendConn.EXPECT().Write(gomock.Any(), uint16(0), gomock.Any()).Return(nil).Times(2)
	close(q.closeCalled)
	require.NoError(t, q.Run())
}

func TestJumboFallbackProbeCongestionLimited(t *testing.T) {
	tc := newJumboRefusalEmission(t)
	c := tc.conn
	now := monotime.Now()
	c.packetSizeFeedback.publish(c.pathGeneration, 8957)
	c.applyPacketSizeFallback(now)
	// Fill the actual congestion window using real packet registration. Drain
	// only local buffers; no ACK frees the flight before the recovery opportunity.
	q := c.emission.queue.(*sendQueue)
	for range 64 {
		now = now.Add(time.Second)
		if c.sentPacketHandler.SendMode(now) == ackhandler.SendAck {
			break
		}
		require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: bytes.Repeat([]byte{'d'}, 1000)}))
		result := c.emission.advance(now, false)
		require.NoError(t, result.err)
		require.True(t, result.progress)
		for len(q.queue) > 0 {
			(<-q.queue).buf.Release()
		}
	}
	require.Equal(t, ackhandler.SendAck, c.sentPacketHandler.SendMode(now))
	require.NoError(t, c.triggerSending(now).err)
	require.NotNil(t, c.mtuFallback.probe)
	require.NotZero(t, c.mtuFallback.probe.deadline)
	require.Len(t, q.queue, 1)
	tc.sendConn.EXPECT().Write(gomock.Any(), uint16(0), gomock.Any()).DoAndReturn(func(b []byte, _ uint16, _ protocol.ECN) error {
		require.Len(t, b, 1438)
		return nil
	})
	close(q.closeCalled)
	require.NoError(t, q.Run())
}

func TestJumboFallbackProbeAckAfterOrdinaryLossThreshold(t *testing.T) {
	for _, expired := range []bool{false, true} {
		name := "before owned deadline"
		if expired {
			name = "after owned loss verdict"
		}
		t.Run(name, func(t *testing.T) {
			tc := newJumboRefusalEmission(t)
			c := tc.conn
			now := monotime.Now()
			c.packetSizeFeedback.publish(c.pathGeneration, 8957)
			c.applyPacketSizeFallback(now)
			probeNumber, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
			require.NoError(t, c.triggerSending(now).err)
			probe := c.mtuFallback.probe
			q := c.emission.queue.(*sendQueue)
			t.Cleanup(func() {
				for len(q.queue) > 0 {
					(<-q.queue).buf.Release()
				}
			})
			var later protocol.PacketNumber
			for i := range 3 {
				later, _ = c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
				c.framer.QueueControlFrame(&wire.PingFrame{})
				result := c.emission.advance(now.Add(time.Duration(i+1)*time.Millisecond), false)
				require.NoError(t, result.err)
				require.True(t, result.progress)
			}
			ack := func(pn protocol.PacketNumber, at monotime.Time) {
				_, err := c.sentPacketHandler.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: pn, Largest: pn}}}, protocol.Encryption1RTT, at)
				require.NoError(t, err)
			}
			ack(later, now.Add(4*time.Millisecond))
			require.False(t, c.mtuFallback.confirmed)
			at := now.Add(5 * time.Millisecond)
			require.True(t, at.Before(probe.deadline))
			if expired {
				c.advancePacketSizeRecovery(probe.deadline)
				require.Equal(t, 1, c.mtuFallback.lost)
				require.NoError(t, c.triggerSending(probe.deadline).err)
				require.NotSame(t, probe, c.mtuFallback.probe)
				at = probe.deadline.Add(time.Millisecond)
			}
			ack(probeNumber, at)
			require.True(t, c.mtuFallback.confirmed, "a valid probe ACK must survive ordinary loss-threshold processing")
			require.False(t, c.mtuFallback.active)
			require.Nil(t, c.mtuFallback.probe)
			require.Zero(t, c.sentPacketHandler.GetLossDetectionTimeout(), "terminal confirmation retires the remaining recovery flight")
		})
	}
}
