//go:build darwin || linux || freebsd

package quic

import (
	"bytes"
	"net"
	"net/netip"
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

var (
	nativeProbePeer     = netip.MustParseAddrPort("1.2.3.4:4321")
	nativeProbeAddress  = &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 4242}
	nativeProbeResolved = netip.MustParseAddrPort("192.0.2.1:4242")
	nativeProbeConnID   = protocol.ParseConnectionID([]byte{8, 7, 6, 5})
)

type nativeProbeWrite struct {
	data        []byte
	destination netip.AddrPort
}

// nativeProbeSocket records every submission and blocks each one that hold
// selects, so a test can act while the native borrow is still current.
type nativeProbeSocket struct {
	writes []nativeProbeWrite
	hold   func([]byte) bool
	result func([]byte) (int, error)
	gate   chan struct{}
}

func newNativeProbeSocket() *nativeProbeSocket { return &nativeProbeSocket{gate: make(chan struct{})} }

func (s *nativeProbeSocket) SetWriteDeadline(time.Time) error { return nil }

func (s *nativeProbeSocket) WriteMsgUDPAddrPort(data, _ []byte, destination netip.AddrPort) (int, int, error) {
	s.writes = append(s.writes, nativeProbeWrite{data: bytes.Clone(data), destination: destination})
	if s.hold != nil && s.hold(data) {
		<-s.gate
	}
	if s.result != nil {
		written, err := s.result(data)
		return written, 0, err
	}
	return len(data), 0, nil
}

func (s *nativeProbeSocket) release() {
	select {
	case <-s.gate:
	default:
		close(s.gate)
	}
}

func nativeProbeHoldAll([]byte) bool { return true }

func startNativeProbeEndpoint(t *testing.T, socket *nativeProbeSocket) *nativeEndpoint {
	t.Helper()
	endpoint := startNativeEndpoint(socket, 100*time.Millisecond, nil)
	t.Cleanup(func() {
		socket.release()
		<-endpoint.stop()
	})
	return endpoint
}

func nativeProbeRecoveryConnection(t *testing.T, controller string) (*testConnection, *nativeOrdinarySender, *nativeProbeSocket) {
	t.Helper()
	conf := &Config{InitialPacketSize: 1200, EnableDatagrams: true}
	require.NoError(t, conf.SetCongestionControlV1(controller))
	tc := newConfiguredEmissionTestConnection(t, false, conf)
	c := tc.conn
	c.rttStats.UpdateRTT(5*time.Millisecond, 0)
	socket := newNativeProbeSocket()
	endpoint := startNativeProbeEndpoint(t, socket)
	require.NoError(t, c.useNativeOrdinary(endpoint, nativeProbePeer, packetInfo{}))
	queue := c.emission.local.(*nativeOrdinarySender)
	t.Cleanup(queue.Close)
	return tc, queue, socket
}

func nativeProbeServerManager() *pathManager {
	return newPathManager(
		func(pathID) (protocol.ConnectionID, bool) { return nativeProbeConnID, true },
		func(pathID) {},
		func() time.Duration { return 100 * time.Millisecond },
		utils.DefaultLogger,
	)
}

func nativeProbeChallenge(t *testing.T, frames []ackhandler.Frame) [8]byte {
	t.Helper()
	for _, frame := range frames {
		if challenge, ok := frame.Frame.(*wire.PathChallengeFrame); ok {
			return challenge.Data
		}
	}
	t.Fatal("no PATH_CHALLENGE in the prepared frames")
	return [8]byte{}
}

func nativeProbeClient(t *testing.T, target *Transport) (*pathManagerOutgoing, *Path, protocol.ConnectionID, ackhandler.Frame) {
	t.Helper()
	manager := newPathManagerOutgoing(
		func(pathID) (protocol.ConnectionID, bool) { return nativeProbeConnID, true },
		func(pathID) {},
		func() {},
	)
	path := manager.NewPath(target, 200*time.Millisecond, func() {})
	require.NoError(t, manager.beginProbe(path))
	connID, frame, selected, ok := manager.NextPathToProbe()
	require.True(t, ok)
	require.Same(t, target, selected)
	return manager, path, connID, frame
}

func nativeProbeRetained(endpoint *nativeEndpoint, participant *endpointParticipant) int {
	retained := -1
	endpoint.do(func(*endpointState) { retained = participant.retained })
	return retained
}

// nativeProbeOccupy gives the participant's mailbox allowance to admitted work
// that the blocked socket cannot finish, so the next offer is refused.
func nativeProbeOccupy(t *testing.T, endpoint *nativeEndpoint, participant *endpointParticipant) {
	t.Helper()
	for range 4 {
		require.NoError(t, endpoint.offer(participant, 0, endpointTestRequest(participant.id, "occupy native")))
	}
	synctest.Wait()
}

func TestNativeProbeResponseBeforeCompletion(t *testing.T) {
	for _, controller := range []string{"reno", "bbrv3"} {
		t.Run(controller+"/server", func(t *testing.T) {
			nativeOrdinaryTest(t, func(t *testing.T) {
				tc, queue, socket := nativeProbeRecoveryConnection(t, controller)
				c := tc.conn
				socket.hold = nativeProbeHoldAll
				observed := observeConstructionBuffer(t)
				manager := nativeProbeServerManager()
				now := monotime.Now()
				connID, frames, _ := manager.HandlePacket(nativeProbeAddress, now, 1200, nil, false)
				candidate := manager.getPath(nativeProbeAddress)
				require.NoError(t, c.emission.serverProbe(connID, frames, candidate, nativeProbeAddress, packetInfo{}, 0, now), "the connection loop does not wait for the native write")
				synctest.Wait()
				require.Len(t, socket.writes, 1)
				require.Equal(t, nativeProbeResolved, socket.writes[0].destination)
				require.Positive(t, candidate.bytesSent, "amplification is charged before dispatch")
				require.EqualValues(t, 1, (*observed).refCount, "the endpoint owns the transferred storage until return")
				manager.HandlePathResponseFrame(&wire.PathResponseFrame{Data: nativeProbeChallenge(t, frames)}, monotime.Now())
				require.True(t, candidate.validated, "a response can arrive before local completion")
				require.True(t, candidate.mtuValidated)
				_, err := c.sentPacketHandler.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: 0, Largest: 0}}}, protocol.Encryption1RTT, monotime.Now())
				require.NoError(t, err, "a probe ACK can arrive before local completion")
				sent := candidate.bytesSent
				socket.release()
				synctest.Wait()
				require.Zero(t, (*observed).refCount)
				require.NoError(t, c.emission.consumeLocal())
				require.Zero(t, nativeProbeRetained(queue.endpoint, queue.participant))
				require.True(t, candidate.validated, "a late completion cannot undo validation")
				require.Equal(t, sent, candidate.bytesSent, "a late completion cannot refund amplification")
			})
		})
		t.Run(controller+"/client", func(t *testing.T) {
			nativeOrdinaryTest(t, func(t *testing.T) {
				tc, queue, socket := nativeProbeRecoveryConnection(t, controller)
				c := tc.conn
				targetSocket := newNativeProbeSocket()
				targetSocket.hold = nativeProbeHoldAll
				endpoint := startNativeProbeEndpoint(t, targetSocket)
				observed := observeConstructionBuffer(t)
				manager, path, connID, frame := nativeProbeClient(t, &Transport{responses: newEndpointResponses(endpoint)})
				require.NoError(t, c.emission.clientProbe(connID, frame, path.tr, nativeProbeAddress, monotime.Now()), "the connection loop does not wait for the native write")
				synctest.Wait()
				require.Empty(t, socket.writes, "the probe uses the target's socket")
				require.Len(t, targetSocket.writes, 1)
				require.Equal(t, nativeProbeResolved, targetSocket.writes[0].destination)
				require.EqualValues(t, 1, (*observed).refCount, "the endpoint owns the transferred storage until return")
				manager.HandlePathResponseFrame(&wire.PathResponseFrame{Data: nativeProbeChallenge(t, []ackhandler.Frame{frame})})
				require.NoError(t, manager.switchToPath(path.id), "a response can arrive before local completion")
				targetSocket.release()
				synctest.Wait()
				require.Zero(t, (*observed).refCount)
				require.NoError(t, c.emission.consumeLocal())
				require.Len(t, queue.targets, 1)
				require.Zero(t, nativeProbeRetained(endpoint, queue.targets[0]), "each run-loop pass consumes the target's completion records")
			})
		})
	}
}

func TestNativeProbeRefusalAfterCommit(t *testing.T) {
	t.Run("server", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			tc, queue, socket := nativeProbeRecoveryConnection(t, "reno")
			c := tc.conn
			socket.hold = nativeProbeHoldAll
			nativeProbeOccupy(t, queue.endpoint, queue.participant)
			writes := len(socket.writes)
			observed := observeConstructionBuffer(t)
			manager := nativeProbeServerManager()
			now := monotime.Now()
			connID, frames, _ := manager.HandlePacket(nativeProbeAddress, now, 1200, nil, false)
			candidate := manager.getPath(nativeProbeAddress)
			before, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
			require.NoError(t, c.emission.serverProbe(connID, frames, candidate, nativeProbeAddress, packetInfo{}, 0, now), "refusal is local backpressure, not a connection error")
			after, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
			require.Greater(t, after, before, "the packet number stays consumed")
			require.Positive(t, candidate.bytesSent, "refusal does not refund amplification")
			require.Equal(t, 1, candidate.numChallenges, "refusal does not refund the challenge")
			require.Zero(t, (*observed).refCount, "the producer releases refused storage once")
			queue.endpoint.do(func(*endpointState) {
				require.False(t, queue.participant.wait[nativeProbeLane].pending, "refusal leaves no retry waiter")
			})
			require.False(t, candidate.canProbe(monotime.Now()), "the backoff owns the next attempt, so refusal cannot spin")
			manager.HandlePathResponseFrame(&wire.PathResponseFrame{Data: nativeProbeChallenge(t, frames)}, monotime.Now())
			require.True(t, candidate.validated, "a refused challenge remains recognizable")
			synctest.Wait()
			require.Len(t, socket.writes, writes, "a refused probe never reaches the socket")
		})
	})
	t.Run("client capacity", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			tc, queue, _ := nativeProbeRecoveryConnection(t, "reno")
			c := tc.conn
			targetSocket := newNativeProbeSocket()
			targetSocket.hold = nativeProbeHoldAll
			endpoint := startNativeProbeEndpoint(t, targetSocket)
			target := &Transport{responses: newEndpointResponses(endpoint)}
			first, firstPath, firstID, firstFrame := nativeProbeClient(t, target)
			require.NoError(t, c.emission.clientProbe(firstID, firstFrame, firstPath.tr, nativeProbeAddress, monotime.Now()))
			synctest.Wait()
			nativeProbeOccupyRemaining(t, endpoint, queue.targets[0])
			writes := len(targetSocket.writes)
			observed := observeConstructionBuffer(t)
			before, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
			_, secondPath, secondID, secondFrame := nativeProbeClient(t, target)
			require.NoError(t, c.emission.clientProbe(secondID, secondFrame, secondPath.tr, nativeProbeAddress, monotime.Now()))
			after, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
			require.Greater(t, after, before, "the packet number stays consumed")
			require.Zero(t, (*observed).refCount, "the producer releases refused storage once")
			synctest.Wait()
			require.Len(t, targetSocket.writes, writes, "a refused probe never reaches the socket")
			first.HandlePathResponseFrame(&wire.PathResponseFrame{Data: nativeProbeChallenge(t, []ackhandler.Frame{firstFrame})})
			require.NoError(t, first.switchToPath(firstPath.id))
		})
	})
	t.Run("client target refused before construction", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			tc, queue, _ := nativeProbeRecoveryConnection(t, "reno")
			c := tc.conn
			targetSocket := newNativeProbeSocket()
			endpoint := startNativeProbeEndpoint(t, targetSocket)
			<-endpoint.stop()
			manager, path, connID, frame := nativeProbeClient(t, &Transport{responses: newEndpointResponses(endpoint)})
			before, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
			require.NoError(t, c.emission.clientProbe(connID, frame, path.tr, nativeProbeAddress, monotime.Now()), "attachment refusal is not a connection error")
			after, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
			require.Equal(t, before, after, "refused attachment precedes construction")
			require.Empty(t, queue.targets)
			manager.HandlePathResponseFrame(&wire.PathResponseFrame{Data: nativeProbeChallenge(t, []ackhandler.Frame{frame})})
			require.NoError(t, manager.switchToPath(path.id), "the committed challenge stays charged and recognizable")
		})
	})
	t.Run("client ungranted target", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			tc, _, _ := nativeProbeRecoveryConnection(t, "reno")
			c := tc.conn
			_, path, connID, frame := nativeProbeClient(t, &Transport{})
			before, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
			require.ErrorIs(t, c.emission.clientProbe(connID, frame, path.tr, nativeProbeAddress, monotime.Now()), errLocalPacketRoute, "a direct write would block the connection loop")
			after, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
			require.Equal(t, before, after)
		})
	})
}

// A current probe already holds one of the participant's four records.
func nativeProbeOccupyRemaining(t *testing.T, endpoint *nativeEndpoint, participant *endpointParticipant) {
	t.Helper()
	for range 3 {
		require.NoError(t, endpoint.offer(participant, 0, endpointTestRequest(participant.id, "occupy native")))
	}
	synctest.Wait()
}

func TestNativeMTUProbeLocalOutcomes(t *testing.T) {
	const maxSize protocol.ByteCount = 1452
	start := func(t *testing.T, tc *testConnection) *mtuFinder {
		t.Helper()
		c := tc.conn
		finder := newMTUDiscoverer(c.rttStats, 1200, maxSize, nil)
		finder.Start(monotime.Now().Add(-time.Hour))
		c.mtuDiscoverer = finder
		return finder
	}
	sendProbe := func(t *testing.T, c *Conn) {
		t.Helper()
		result := c.emission.send(monotime.Now(), true)
		require.NoError(t, result.err)
		require.True(t, result.progress, "the real packer constructs the MTU probe")
	}
	isProbe := func(data []byte) bool { return len(data) > 1200 }
	for _, controller := range []string{"reno", "bbrv3"} {
		t.Run(controller+"/queued expiry", func(t *testing.T) {
			nativeOrdinaryTest(t, func(t *testing.T) {
				tc, queue, socket := nativeProbeRecoveryConnection(t, controller)
				c := tc.conn
				socket.result = func(data []byte) (int, error) {
					time.Sleep(110 * time.Millisecond)
					return len(data), nil
				}
				require.NoError(t, queue.endpoint.offer(queue.participant, 0, endpointTestRequest(queue.participant.id, "occupy native")))
				synctest.Wait()
				finder := start(t, tc)
				sendProbe(t, c)
				require.NotEqual(t, protocol.InvalidByteCount, finder.inFlight)
				time.Sleep(150 * time.Millisecond)
				synctest.Wait()
				require.NoError(t, c.emission.consumeLocal(), "local expiry cannot terminate the connection")
				require.Equal(t, protocol.InvalidByteCount, finder.inFlight, "local disposal ends the attempt")
				require.Equal(t, maxSize, finder.max(), "local disposal is not PMTU loss")
				require.False(t, finder.lastProbeWasLost)
				require.Zero(t, c.connStats.PacketsLost.Load(), "local disposal is not network loss")
				require.True(t, finder.ShouldSendProbe(monotime.Now().Add(time.Second)), "the existing probe delay schedules the next attempt")
				for _, write := range socket.writes {
					require.False(t, isProbe(write.data))
				}
			})
		})
		t.Run(controller+"/accepted then ACK", func(t *testing.T) {
			nativeOrdinaryTest(t, func(t *testing.T) {
				tc, _, socket := nativeProbeRecoveryConnection(t, controller)
				c := tc.conn
				finder := start(t, tc)
				size := finder.probeSize()
				sendProbe(t, c)
				synctest.Wait()
				require.Len(t, socket.writes, 1)
				require.Len(t, socket.writes[0].data, int(size))
				require.NoError(t, c.emission.consumeLocal())
				_, err := c.sentPacketHandler.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: 0, Largest: 0}}}, protocol.Encryption1RTT, monotime.Now())
				require.NoError(t, err)
				require.Equal(t, size, finder.CurrentSize(), "an accepted probe still raises the size")
				require.Equal(t, protocol.InvalidByteCount, finder.inFlight)
			})
		})
	}
	t.Run("native timeout then loss", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			tc, _, socket := nativeProbeRecoveryConnection(t, "reno")
			c := tc.conn
			socket.result = func(data []byte) (int, error) {
				if isProbe(data) {
					return 0, os.ErrDeadlineExceeded
				}
				return len(data), nil
			}
			finder := start(t, tc)
			sendProbe(t, c)
			synctest.Wait()
			require.NoError(t, c.emission.consumeLocal())
			require.NotEqual(t, protocol.InvalidByteCount, finder.inFlight, "an uncertain probe stays outstanding")
			require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("later packet")}))
			require.NoError(t, c.emission.send(monotime.Now(), true).err)
			synctest.Wait()
			require.NoError(t, c.emission.consumeLocal())
			time.Sleep(time.Second)
			next, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
			largest := next - 1
			_, err := c.sentPacketHandler.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: largest, Largest: largest}}}, protocol.Encryption1RTT, monotime.Now())
			require.NoError(t, err)
			c.sentPacketHandler.OnLossDetectionTimeout(monotime.Now().Add(time.Hour))
			require.Equal(t, protocol.InvalidByteCount, finder.inFlight, "network loss resolves the attempt")
			require.Equal(t, maxSize, finder.max(), "nothing proves an uncertain probe left the host")
			require.False(t, finder.lastProbeWasLost)
		})
	})
	t.Run("size rejection", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			tc, _, socket := nativeProbeRecoveryConnection(t, "reno")
			c := tc.conn
			socket.result = func([]byte) (int, error) {
				return 0, &os.SyscallError{Syscall: "sendmsg", Err: syscall.EMSGSIZE}
			}
			finder := start(t, tc)
			size := finder.probeSize()
			sendProbe(t, c)
			synctest.Wait()
			require.NoError(t, c.emission.consumeLocal())
			require.Equal(t, protocol.InvalidByteCount, finder.inFlight)
			require.Equal(t, size, finder.lost[0], "a size rejection stays PMTU feedback")
			require.True(t, finder.lastProbeWasLost)
			require.Zero(t, c.connStats.PacketsLost.Load(), "it is not network loss")
		})
	})
	t.Run("early ACK then late result", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			tc, _, socket := nativeProbeRecoveryConnection(t, "reno")
			c := tc.conn
			socket.hold = nativeProbeHoldAll
			socket.result = func([]byte) (int, error) { return 0, os.ErrDeadlineExceeded }
			finder := start(t, tc)
			size := finder.probeSize()
			sendProbe(t, c)
			synctest.Wait()
			_, err := c.sentPacketHandler.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: 0, Largest: 0}}}, protocol.Encryption1RTT, monotime.Now())
			require.NoError(t, err)
			require.Equal(t, size, finder.CurrentSize())
			_, newer := finder.GetPing(monotime.Now())
			socket.release()
			synctest.Wait()
			require.NoError(t, c.emission.consumeLocal())
			require.Equal(t, newer, finder.inFlight, "a late result cannot touch a newer probe")
			require.Equal(t, size, finder.CurrentSize(), "a late result cannot undo the ACK")
		})
	})
}

func nativeProbeRebind(tc *testConnection, destination *net.UDPAddr, info packetInfo) {
	c := tc.conn
	tc.sendConn.EXPECT().ChangeRemoteAddr(destination, info)
	now := monotime.Now()
	c.peerParams = &wire.TransportParameters{}
	c.mtuDiscoverer = c.newPathMTUFinder(1200, 1452)
	c.resetPathPacketSize(now)
	c.startMTUDiscovery(now)
	c.pathGeneration++
	c.emission.resetLocalPath(c.pathGeneration, now)
	c.emission.rebindPath(destination, info)
}

func TestNativeRebindingGeneration(t *testing.T) {
	payload := []byte("reliable data follows the rebinding")
	for _, phase := range []string{"queued", "current"} {
		t.Run(phase, func(t *testing.T) {
			nativeOrdinaryTest(t, func(t *testing.T) {
				tc, queue, socket := nativeProbeRecoveryConnection(t, "reno")
				c := tc.conn
				socket.hold = func(data []byte) bool {
					return phase == "queued" && bytes.Equal(data, []byte("occupy native")) || phase == "current" && bytes.Contains(data, payload) && len(socket.writes) == 1
				}
				socket.result = func(data []byte) (int, error) {
					if phase == "current" && len(socket.writes) == 1 {
						return 0, os.ErrDeadlineExceeded
					}
					return len(data), nil
				}
				if phase == "queued" {
					require.NoError(t, queue.endpoint.offer(queue.participant, 0, endpointTestRequest(queue.participant.id, "occupy native")))
					synctest.Wait()
				}
				c.connFlowController.UpdateSendWindow(1 << 20)
				flow := newStreamFlowController(0, c.connFlowController, 1<<20, 1<<20, 1<<20, c.flowControlRTT(), c.logger)
				stream := newSendStream(c.ctx, 0, c, flow, false)
				require.NoError(t, stream.TryWriteAll(payload))
				require.NoError(t, c.emission.send(monotime.Now(), true).err)
				synctest.Wait()
				firstPN, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
				moved := &net.UDPAddr{IP: net.IPv4(198, 51, 100, 7), Port: 7000}
				nativeProbeRebind(tc, moved, packetInfo{})
				require.Equal(t, netip.MustParseAddrPort("198.51.100.7:7000"), queue.destination)
				socket.release()
				synctest.Wait()
				require.NoError(t, c.emission.consumeLocal(), "the old generation's result stays nonfatal")
				require.NoError(t, c.ctx.Err())
				time.Sleep(endpointRetry) // local backpressure holds the next admission this long
				require.NoError(t, c.emission.send(monotime.Now(), true).err)
				synctest.Wait()
				require.NoError(t, c.emission.consumeLocal())
				var onNewTuple int
				for _, write := range socket.writes {
					if !bytes.Contains(write.data, payload) {
						continue
					}
					if write.destination == queue.destination {
						onNewTuple++
						_, pn, _, _, err := wire.ParseShortHeader(write.data, c.connIDManager.Get().Len())
						require.NoError(t, err)
						require.GreaterOrEqual(t, pn, firstPN, "recovery constructs a fresh packet instead of replaying encrypted bytes")
					} else {
						require.Equal(t, "current", phase, "a revoked queued request never reaches the old tuple")
						require.Equal(t, nativeProbePeer, write.destination)
					}
				}
				require.Equal(t, 1, onNewTuple, "the late result cannot requeue reliable data a second time")
			})
		})
	}
	t.Run("invalid destination", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			tc, queue, _ := nativeProbeRecoveryConnection(t, "reno")
			c := tc.conn
			c.conn.ChangeRemoteAddr(&net.TCPAddr{IP: net.IPv4(198, 51, 100, 7), Port: 7000}, packetInfo{})
			require.Equal(t, nativeProbePeer, queue.destination)
			require.ErrorIs(t, c.emission.consumeLocal(), errLocalPacketRoute)
		})
	})
}

func TestNativeProbeRevocation(t *testing.T) {
	queuedProbe := func(t *testing.T) (*testConnection, *nativeOrdinarySender, *nativeProbeSocket) {
		t.Helper()
		tc, queue, socket := nativeProbeRecoveryConnection(t, "reno")
		socket.hold = nativeProbeHoldAll
		require.NoError(t, queue.endpoint.offer(queue.participant, 0, endpointTestRequest(queue.participant.id, "occupy native")))
		synctest.Wait()
		return tc, queue, socket
	}
	serverProbe := func(t *testing.T, c *Conn) (*pathManager, *path) {
		t.Helper()
		manager := nativeProbeServerManager()
		now := monotime.Now()
		connID, frames, _ := manager.HandlePacket(nativeProbeAddress, now, 1200, nil, false)
		candidate := manager.getPath(nativeProbeAddress)
		require.NoError(t, c.emission.serverProbe(connID, frames, candidate, nativeProbeAddress, packetInfo{}, 0, now))
		return manager, candidate
	}
	noProbeWritten := func(t *testing.T, socket *nativeProbeSocket) {
		t.Helper()
		socket.release()
		synctest.Wait()
		for _, write := range socket.writes {
			require.Equal(t, []byte("occupy native"), write.data, "a revoked probe never reaches the socket")
		}
	}
	t.Run("server path expiry", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			tc, queue, socket := queuedProbe(t)
			c := tc.conn
			manager, candidate := serverProbe(t, c)
			manager.Expire(candidate.validationDeadline)
			noProbeWritten(t, socket)
			require.NoError(t, c.emission.consumeLocal())
			require.Zero(t, nativeProbeRetained(queue.endpoint, queue.participant))
		})
	})
	t.Run("server path switch", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			tc, _, socket := queuedProbe(t)
			manager, _ := serverProbe(t, tc.conn)
			manager.SwitchToPath(&net.UDPAddr{IP: net.IPv4(198, 51, 100, 7), Port: 7000})
			noProbeWritten(t, socket)
		})
	})
	t.Run("client path close", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			tc, queue, socket := queuedProbe(t)
			c := tc.conn
			_, path, connID, frame := nativeProbeClient(t, &Transport{responses: newEndpointResponses(queue.endpoint)})
			require.NoError(t, c.emission.clientProbe(connID, frame, path.tr, nativeProbeAddress, monotime.Now()))
			require.NoError(t, path.Close())
			noProbeWritten(t, socket)
		})
	})
	t.Run("target transport close", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			tc, queue, socket := queuedProbe(t)
			c := tc.conn
			_, path, connID, frame := nativeProbeClient(t, &Transport{responses: newEndpointResponses(queue.endpoint)})
			require.NoError(t, c.emission.clientProbe(connID, frame, path.tr, nativeProbeAddress, monotime.Now()))
			path.tr.responses.revoke()
			noProbeWritten(t, socket)
		})
	})
	t.Run("canceled generation then late completion", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			tc, queue, socket := nativeProbeRecoveryConnection(t, "reno")
			c := tc.conn
			var returns atomic.Int32
			socket.hold = nativeProbeHoldAll
			socket.result = func(data []byte) (int, error) {
				returns.Add(1)
				return len(data), nil
			}
			observed := observeConstructionBuffer(t)
			_, candidate := serverProbe(t, c)
			synctest.Wait()
			require.Len(t, socket.writes, 1)
			require.EqualValues(t, 1, (*observed).refCount)
			sent := candidate.bytesSent
			generation := queue.generation
			nativeProbeRebind(tc, &net.UDPAddr{IP: net.IPv4(198, 51, 100, 7), Port: 7000}, packetInfo{})
			require.Greater(t, queue.generation, generation)
			require.EqualValues(t, 1, (*observed).refCount, "cancellation cannot reclaim a current borrow")
			require.Zero(t, returns.Load())
			socket.release()
			synctest.Wait()
			require.Zero(t, (*observed).refCount)
			require.NoError(t, c.emission.consumeLocal())
			require.Equal(t, sent, candidate.bytesSent, "the late completion cannot refund the charge")
			require.Zero(t, nativeProbeRetained(queue.endpoint, queue.participant))
			require.Len(t, socket.writes, 1)
		})
	})
	t.Run("connection close keeps a target borrow", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			tc, queue, _ := nativeProbeRecoveryConnection(t, "reno")
			c := tc.conn
			targetSocket := newNativeProbeSocket()
			targetSocket.hold = nativeProbeHoldAll
			endpoint := startNativeProbeEndpoint(t, targetSocket)
			observed := observeConstructionBuffer(t)
			_, path, connID, frame := nativeProbeClient(t, &Transport{responses: newEndpointResponses(endpoint)})
			require.NoError(t, c.emission.clientProbe(connID, frame, path.tr, nativeProbeAddress, monotime.Now()))
			synctest.Wait()
			target := queue.targets[0]
			queue.Close()
			require.EqualValues(t, 1, (*observed).refCount, "close cannot reclaim a current borrow")
			select {
			case <-target.retired:
				t.Fatal("close cannot retire a participant with a current borrow")
			default:
			}
			targetSocket.release()
			synctest.Wait()
			require.Zero(t, (*observed).refCount)
			select {
			case <-target.retired:
			default:
				t.Fatal("actual return must retire the detached target participant")
			}
		})
	})
}
