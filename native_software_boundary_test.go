//go:build darwin || linux || freebsd

package quic

import (
	"bytes"
	"runtime"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
)

func queueNativeSoftwareStream(t *testing.T, c *Conn) {
	t.Helper()
	c.connFlowController.UpdateSendWindow(1 << 20)
	flow := newStreamFlowController(0, c.connFlowController, 1<<20, 1<<20, 1<<20, c.flowControlRTT(), c.logger)
	stream := newSendStream(c.ctx, 0, c, flow, false)
	require.NoError(t, stream.TryWriteAll(bytes.Repeat([]byte("physical-boundary"), 6000)))
}

func nativeSoftwareBoundaryConnection(t *testing.T) (*testConnection, *nativeOrdinarySender, *nativeProbeSocket) {
	t.Helper()
	conf := &Config{InitialPacketSize: 1200, EnableDatagrams: true, DisablePathMTUDiscovery: true}
	require.NoError(t, conf.SetCongestionControlV1("reno"))
	tc := newConfiguredEmissionTestConnection(t, false, conf)
	// Keep real pacing; a short RTT makes the physical bound reachable in a turn.
	tc.conn.rttStats.UpdateRTT(100*time.Microsecond, 0)
	socket := newNativeProbeSocket()
	endpoint := startNativeProbeEndpoint(t, socket)
	require.NoError(t, tc.conn.useNativeOrdinary(endpoint, nativeProbePeer, packetInfo{}))
	sender := tc.conn.emission.local.(*nativeOrdinarySender)
	t.Cleanup(sender.Close)
	return tc, sender, socket
}

func TestNativeSoftwareReservationBoundaryYieldsBeforePacking(t *testing.T) {
	for _, action := range []string{"close", "replace", "receive"} {
		t.Run(action, func(t *testing.T) {
			nativeOrdinaryTest(t, func(t *testing.T) {
				tc, sender, socket := nativeSoftwareBoundaryConnection(t)
				c := tc.conn
				var buffers []*packetBuffer
				// Charge real pool storage until the next software allocation
				// crosses the existing request byte bound, without invalid geometry.
				nextCapacity := gsoPacketBufferSize(c.emission.packetSize())
				for sender.pending == nil || sender.pending.backing+nextCapacity <= 1<<20 {
					buffer := getCoalescedPacketBuffer()
					appendNativeGroupMember(t, tc, sender, buffer, "resource-boundary", protocol.ECNNon, sendMetadata{})
					buffers = append(buffers, buffer)
					require.Less(t, len(buffers), endpointBatch)
				}
				r := sender.pending
				require.NotNil(t, r)
				require.LessOrEqual(t, r.backing, protocol.ByteCount(1<<20))
				targetSocket := newNativeProbeSocket()
				target := &Transport{responses: newEndpointResponses(startNativeProbeEndpoint(t, targetSocket))}
				completed := false
				completion := &e06CompletionObserver{LocalSentPacketHandler: sender.recovery}
				completion.onComplete = func() {
					if completed {
						return
					}
					require.Equal(t, ordinaryTerminalProcessing, r.state)
					for _, buffer := range buffers {
						require.Zero(t, buffer.refCount, "unadmitted payload releases before completion callbacks")
					}
					completed = true
					switch action {
					case "close":
						sender.Close()
					case "replace":
						c.sentPacketHandler.MigratedPath(monotime.Now(), 1200)
						c.emission.replaceLocalPath(target, tc.sendConn)
					case "receive":
						c.receivedPackets.PushBack(receivedPacket{})
					}
				}
				sender.recovery = completion
				<-sender.endpoint.stop() // legitimate synchronous refusal at the resource finish
				queueNativeSoftwareStream(t, c)
				before := c.connStats.PacketsSent.Load()
				select {
				case <-c.sendingScheduled:
				default:
				}
				var result emissionResult
				if runtime.GOOS == "linux" {
					result = c.triggerSending(monotime.Now())
				} else {
					result = c.emission.advanceForPlatform(monotime.Now(), false, true)
					sender.finish() // normal send's deferred finish disposes an empty reservation
				}
				require.NoError(t, result.err)
				require.True(t, completed)
				require.Equal(t, before, c.connStats.PacketsSent.Load(), "the stale turn never packs after the callback")
				require.Empty(t, socket.writes)
				require.Empty(t, targetSocket.writes)
				if action == "close" {
					require.True(t, sender.closed)
					require.Equal(t, emissionQueueFull, result.stop)
					require.False(t, result.retry)
					return
				}
				require.True(t, result.retry, "live callback boundaries return to normal Conn selection")
				if runtime.GOOS == "linux" {
					select {
					case <-c.sendingScheduled:
					default:
						t.Fatal("triggerSending lost the live retry wake")
					}
				}
				if action == "receive" {
					require.True(t, sender.canSoftwareSegment(), "the policy callback did not change software capability")
					next := c.triggerSending(monotime.Now())
					require.NoError(t, next.err)
					require.Equal(t, emissionReceivePending, next.stop, "fresh normal selection observes the new receive policy")
					require.NotNil(t, c.receivedPackets.PopFront())
				} else {
					require.False(t, sender.canSoftwareSegment())
					require.NoError(t, c.triggerSending(monotime.Now()).err)
				}
				require.Greater(t, c.connStats.PacketsSent.Load(), before)
				synctest.Wait()
				require.NoError(t, sender.consume())
				if action == "replace" {
					require.NotEmpty(t, targetSocket.writes)
				}
			})
		})
	}
}
