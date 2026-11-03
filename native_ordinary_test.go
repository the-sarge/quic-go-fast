//go:build darwin || linux || freebsd

package quic

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/mocks"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func nativeOrdinaryTest(t *testing.T, run func(*testing.T)) {
	t.Helper()
	now := time.Now()
	synctest.Test(t, func(t *testing.T) {
		if offset := time.Until(now); offset > 0 {
			time.Sleep(offset)
		}
		run(t)
	})
}

type ordinaryTestNative struct {
	endpointTestNative
	observe func([]byte, netip.AddrPort)
}

func (n ordinaryTestNative) WriteMsgUDPAddrPort(data, oob []byte, destination netip.AddrPort) (int, int, error) {
	n.observe(oob, destination)
	return n.endpointTestNative.WriteMsgUDPAddrPort(data, oob, destination)
}

func TestNativeOrdinaryDatagramDisposal(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		tc := newEmissionTestConnection(t, false)
		c := tc.conn
		var output [][]byte
		e := startNativeEndpoint(endpointTestNative{send: func(data []byte) (int, error) {
			if bytes.Equal(data, []byte("occupy native")) {
				time.Sleep(110 * time.Millisecond)
			} else {
				output = append(output, bytes.Clone(data))
			}
			return len(data), nil
		}}, 100*time.Millisecond, nil)
		defer func() { <-e.stop() }()
		blocker, err := e.attach(77)
		require.NoError(t, err)
		require.NoError(t, e.offer(blocker, 0, endpointTestRequest(77, "occupy native")))
		synctest.Wait()
		require.NoError(t, c.useNativeOrdinary(e, netip.MustParseAddrPort("1.2.3.4:4321"), packetInfo{}))
		require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("expired datagram")}))
		stop := runNativeOrdinaryTestConnection(t, tc, nil)
		defer stop()
		time.Sleep(150 * time.Millisecond)
		synctest.Wait()
		require.NoError(t, c.ctx.Err())
		require.Empty(t, output)
		require.EqualValues(t, 1, c.emission.local.(*nativeOrdinarySender).droppedDatagrams)
		require.Nil(t, c.datagramQueue.Peek())
		require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("fresh datagram")}))
		c.scheduleSending()
		synctest.Wait()
		require.Len(t, output, 1)
		require.Contains(t, string(output[0]), "fresh datagram")
		require.NotContains(t, string(output[0]), "expired datagram")
		_, pn, _, _, err := wire.ParseShortHeader(output[0], c.connIDManager.Get().Len())
		require.NoError(t, err)
		require.EqualValues(t, 1, pn)
	})
}

func TestNativeOrdinarySegmentPrefix(t *testing.T) {
	for _, nativeGSO := range []bool{false, true} {
		for _, outcome := range []string{"accepted", "timeout", "size", "destination"} {
			t.Run(fmt.Sprintf("nativeGSO=%t/%s", nativeGSO, outcome), func(t *testing.T) {
				nativeOrdinaryTest(t, func(t *testing.T) {
					tc := newEmissionTestConnection(t, true)
					c := tc.conn
					var deadlines []time.Time
					var submitted [][]byte
					var finalError error
					switch outcome {
					case "timeout":
						finalError = os.ErrDeadlineExceeded
					case "size":
						finalError = &os.SyscallError{Syscall: "sendmsg", Err: syscall.EMSGSIZE}
					case "destination":
						finalError = &os.SyscallError{Syscall: "sendmsg", Err: syscall.ECONNREFUSED}
					}
					calls, segments := 0, 0
					info := packetInfo{addr: netip.MustParseAddr("127.0.0.2")}
					socket := endpointTestNative{
						deadline: func(deadline time.Time) error { deadlines = append(deadlines, deadline); return nil },
						send: func(data []byte) (int, error) {
							calls++
							if nativeGSO && calls == 1 {
								return 0, &os.SyscallError{Syscall: "sendmsg", Err: syscall.EPERM}
							}
							if nativeGSO && calls == 2 {
								require.Len(t, data, 3600, "this fixture explicitly models a native GSO attempt")
								if outcome == "accepted" {
									for offset := 0; offset < len(data); offset += 1200 {
										submitted = append(submitted, bytes.Clone(data[offset:offset+1200]))
									}
									return len(data), nil
								}
								return 0, &os.SyscallError{Syscall: "sendmsg", Err: syscall.EIO}
							}
							segments++
							require.Len(t, data, 1200, "fallback progress is in individual datagrams, independent of platform stubs")
							submitted = append(submitted, bytes.Clone(data))
							if segments == 2 && finalError != nil {
								if outcome == "timeout" {
									time.Sleep(100 * time.Millisecond)
								}
								return 0, finalError
							}
							return len(data), nil
						},
					}
					e := startNativeEndpoint(ordinaryTestNative{endpointTestNative: socket, observe: func(oob []byte, destination netip.AddrPort) {
						require.Equal(t, netip.MustParseAddrPort("1.2.3.4:4321"), destination)
						if !nativeGSO || calls >= 2 {
							require.Equal(t, info.OOB(), oob, "segmented fallback preserves captured source metadata")
						}
					}}, 100*time.Millisecond, nil)
					require.NoError(t, c.useNativeOrdinary(e, netip.MustParseAddrPort("1.2.3.4:4321"), info))
					queue := c.emission.local.(*nativeOrdinarySender)
					queue.nativeGSO = nativeGSO
					defer func() { queue.Close(); <-e.stop() }()
					_, pnLen := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
					payloadSize := 1200 - int(wire.ShortHeaderLen(c.connIDManager.Get(), pnLen)) - 7 - 3
					for _, value := range []byte{0x41, 0x42, 0x43} {
						require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: bytes.Repeat([]byte{value}, payloadSize)}))
					}
					require.NoError(t, c.emission.send(monotime.Now(), true).err)
					synctest.Wait()
					if outcome == "timeout" {
						time.Sleep(100 * time.Millisecond)
						synctest.Wait()
					}
					err := queue.consume()
					if outcome == "destination" {
						require.ErrorIs(t, err, syscall.ECONNREFUSED, "ordinary non-timeout failures retain their fatal policy")
					} else {
						require.NoError(t, err)
					}
					require.Len(t, deadlines, 2, "permission and GSO fallback do not replace the absolute deadline")
					require.False(t, deadlines[0].IsZero())
					require.True(t, deadlines[1].IsZero())
					require.Equal(t, nativeGSO && outcome != "accepted", queue.writeState.gsoDisabled.Load())
					wantDropped := uint64(0)
					if finalError != nil {
						wantDropped = 1
						if outcome == "size" {
							wantDropped = 2
						}
					}
					require.Equal(t, wantDropped, queue.droppedDatagrams)
					wantSegments := 3
					if finalError != nil {
						wantSegments = 2
					}
					require.Len(t, submitted, wantSegments)
					for index, data := range submitted {
						_, pn, _, _, err := wire.ParseShortHeader(data, c.connIDManager.Get().Len())
						require.NoError(t, err)
						require.EqualValues(t, index, pn)
					}
					largest := protocol.PacketNumber(wantSegments - 1)
					if outcome == "size" {
						largest = 0
					}
					_, err = c.sentPacketHandler.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: 0, Largest: largest}}}, protocol.Encryption1RTT, monotime.Now())
					require.NoError(t, err, "accepted and uncertain portions retain their original recovery registrations")
					// The generator may randomly skip the next number, but never reuses one.
					pn, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
					require.GreaterOrEqual(t, pn, protocol.PacketNumber(3))
				})
			})
		}
	}
}

func TestNativeOrdinaryTerminalLifetime(t *testing.T) {
	register := func(t *testing.T, c *Conn, frames []ackhandler.Frame) {
		t.Helper()
		now := monotime.Now()
		pn := c.sentPacketHandler.PopPacketNumber(protocol.Encryption1RTT)
		if c.emission.local != nil {
			local := c.sentPacketHandler.(ackhandler.LocalSentPacketHandler)
			token, err := local.RegisterLocalPacket(ackhandler.PacketContext{}, now, pn, protocol.InvalidPacketNumber, nil, frames, protocol.Encryption1RTT, protocol.ECNNon, 1200)
			require.NoError(t, err)
			require.NoError(t, local.CompleteLocalPacket(token, ackhandler.LocalPacketAccepted, now))
		} else {
			c.sentPacketHandler.SentPacketWithContext(ackhandler.PacketContext{}, now, pn, protocol.InvalidPacketNumber, nil, frames, protocol.Encryption1RTT, protocol.ECNNon, 1200, false, false)
		}
	}
	for _, lifecycle := range []string{"Retry", "dropped keys", "close", "early ACK"} {
		t.Run(lifecycle, func(t *testing.T) {
			nativeOrdinaryTest(t, func(t *testing.T) {
				tc := newHandshakeEmissionConnection(t, false)
				c := tc.conn
				release := make(chan struct{})
				calls := 0
				e := startNativeEndpoint(endpointTestNative{send: func(data []byte) (int, error) {
					calls++
					if calls == 1 {
						<-release
					}
					return len(data), nil
				}}, 100*time.Millisecond, nil)
				require.NoError(t, c.useNativeOrdinary(e, netip.MustParseAddrPort("1.2.3.4:4321"), packetInfo{}))
				queue := c.emission.local.(*nativeOrdinarySender)
				released := false
				defer func() {
					queue.Close()
					if !released {
						close(release)
					}
					<-e.stop()
				}()
				for range 4 {
					c.initialStream.Write([]byte("registered handshake crypto"))
					require.True(t, c.emission.coalesced(monotime.Now()).progress)
					queue.finish()
					synctest.Wait()
				}
				pn, _ := c.sentPacketHandler.PeekPacketNumber(protocol.EncryptionInitial)
				c.initialStream.Write([]byte("producer remains owned while the mailbox is full"))
				result := c.emission.coalesced(monotime.Now())
				require.Equal(t, emissionQueueFull, result.stop)
				require.True(t, c.initialStream.HasData())
				blockedPN, _ := c.sentPacketHandler.PeekPacketNumber(protocol.EncryptionInitial)
				require.Equal(t, pn, blockedPN, "completion capacity is reserved before destructive construction")
				var borrowed *packetBuffer
				e.do(func(state *endpointState) {
					require.Equal(t, 4, queue.participant.retained)
					require.LessOrEqual(t, state.bytes, 4<<20)
					borrowed = state.current.request.buffers[0]
				})
				token := queue.records[0].packets[0].token
				switch lifecycle {
				case "Retry":
					c.emission.resetLocal()
					c.sentPacketHandler.ResetForRetry(monotime.Now())
					require.NoError(t, c.emission.consumeLocal())
					require.True(t, queue.participant.active)
					require.True(t, c.emission.coalesced(monotime.Now()).progress)
					queue.finish()
				case "dropped keys":
					c.sentPacketHandler.DropPackets(protocol.EncryptionInitial, monotime.Now())
				case "close":
					c.emission.drain()
					require.NotEqual(t, ackhandler.SendNone, c.sentPacketHandler.SendMode(monotime.Now()), "the close packet still needs recovery's packet numbers")
					queue.Close()
					require.Equal(t, ackhandler.SendNone, c.sentPacketHandler.SendMode(monotime.Now()))
					select {
					case <-queue.participant.retired:
						t.Fatal("close cannot reclaim a current native borrow")
					default:
					}
				case "early ACK":
					_, err := c.sentPacketHandler.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: 0, Largest: 0}}}, protocol.EncryptionInitial, monotime.Now())
					require.NoError(t, err)
				}
				require.Equal(t, 1, borrowed.refCount, "protocol transitions cannot end the native borrow")
				var beforeReturn protocol.PacketNumber
				if lifecycle == "Retry" || lifecycle == "early ACK" {
					beforeReturn, _ = c.sentPacketHandler.PeekPacketNumber(protocol.EncryptionInitial)
				}
				close(release)
				released = true
				synctest.Wait()
				require.Zero(t, borrowed.refCount)
				if lifecycle == "close" {
					select {
					case <-queue.participant.retired:
					default:
						t.Fatal("actual return must retire the detached participant")
					}
					return
				}
				require.NoError(t, c.emission.consumeLocal())
				for _, record := range queue.records {
					require.Zero(t, record.admission.serial)
				}
				require.Error(t, queue.recovery.CompleteLocalPacket(token, ackhandler.LocalPacketAccepted, monotime.Now()), "late duplicate facts cannot consume a registration again")
				if lifecycle == "Retry" || lifecycle == "early ACK" {
					afterReturn, _ := c.sentPacketHandler.PeekPacketNumber(protocol.EncryptionInitial)
					require.Equal(t, beforeReturn, afterReturn, "old results cannot mutate replacement packet-number history")
				}
			})
		})
	}
	for _, startup := range []string{"start handshake", "handshake event"} {
		t.Run(startup, func(t *testing.T) {
			nativeOrdinaryTest(t, func(t *testing.T) {
				ctrl := gomock.NewController(t)
				crypto := mocks.NewMockCryptoSetup(ctrl)
				tc := newClientTestConnection(t, ctrl, &Config{DisablePathMTUDiscovery: true}, false, connectionOptCryptoSetup(crypto))
				c := tc.conn
				endpoint := startNativeEndpoint(endpointTestNative{}, 100*time.Millisecond, nil)
				require.NoError(t, c.useNativeOrdinary(endpoint, netip.MustParseAddrPort("1.2.3.4:4321"), packetInfo{}))
				queue := c.emission.local.(*nativeOrdinarySender)
				defer func() { queue.Close(); <-endpoint.stop() }()
				startupError := errors.New("handshake startup failed")
				if startup == "start handshake" {
					crypto.EXPECT().StartHandshake(gomock.Any()).Return(startupError)
				} else {
					crypto.EXPECT().StartHandshake(gomock.Any())
					crypto.EXPECT().NextEvent().Return(handshake.Event{Kind: handshake.EventReceivedTransportParameters, TransportParameters: &wire.TransportParameters{}})
				}
				err := c.run()
				defer c.timer.Stop()
				if startup == "start handshake" {
					require.ErrorIs(t, err, startupError)
				} else {
					var transportError *qerr.TransportError
					require.ErrorAs(t, err, &transportError)
					require.Equal(t, qerr.TransportParameterError, transportError.ErrorCode)
				}
				require.ErrorIs(t, context.Cause(c.Context()), err)
				synctest.Wait()
				select {
				case <-queue.participant.retired:
				default:
					t.Fatal("startup failure must retire the attached native participant")
				}
				require.False(t, queue.recovery.CanRegisterLocalPackets(1), "startup failure must seal local recovery")
			})
		})
	}
	for _, controller := range []string{"reno", "bbrv3"} {
		for _, event := range []string{"ACK", "PTO"} {
			for _, route := range []string{"legacy", "private"} {
				t.Run("tracking pressure/"+controller+"/"+event+"/"+route, func(t *testing.T) {
					nativeOrdinaryTest(t, func(t *testing.T) {
						tc := bbrDepthTestConnection(t, controller, false)
						c := tc.conn
						c.rttStats.UpdateRTT(5*time.Millisecond, 0)
						c.rttStats.SetMaxAckDelay(protocol.MaxAckDelay)
						c.sentPacketHandler.ReceivedBytes(64<<20, monotime.Now())
						var writes []monotime.Time
						var packets [][]byte
						observe := func(data []byte) {
							writes = append(writes, monotime.Now())
							packets = append(packets, bytes.Clone(data))
						}
						if route == "private" {
							endpoint := startNativeEndpoint(endpointTestNative{send: func(data []byte) (int, error) {
								observe(data)
								return len(data), nil
							}}, 100*time.Millisecond, nil)
							defer func() { <-endpoint.stop() }()
							require.NoError(t, c.useNativeOrdinary(endpoint, netip.MustParseAddrPort("1.2.3.4:4321"), packetInfo{}))
						} else {
							tc.sendConn.EXPECT().Write(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(data []byte, _ uint16, _ protocol.ECN) error {
								observe(data)
								return nil
							}).AnyTimes()
						}
						first, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
						for {
							next, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
							if next-first >= protocol.MaxTrackedSentPackets-128 {
								break
							}
							register(t, c, nil)
						}
						register(t, c, []ackhandler.Frame{{Frame: &wire.PingFrame{}}})
						now := monotime.Now()
						require.Equal(t, ackhandler.SendAck, c.sentPacketHandler.SendMode(now))
						deadline := c.sentPacketHandler.GetLossDetectionTimeout()
						require.False(t, deadline.IsZero())
						if route == "private" {
							local := c.sentPacketHandler.(ackhandler.LocalSentPacketHandler)
							require.True(t, local.CanRegisterLocalPackets(1))
							require.False(t, local.CanRegisterLocalPackets(maxGSOSegments+3))
						}
						if event == "ACK" {
							received := tc.receivedPacketHandler()
							require.NoError(t, received.ReceivedPacket(1, protocol.ECNNon, protocol.Encryption1RTT, now, true))
							require.NotNil(t, received.GetAckFrame(protocol.Encryption1RTT, now, false))
							require.NoError(t, received.ReceivedPacket(2, protocol.ECNNon, protocol.Encryption1RTT, now, true))
							deadline = received.GetAlarmTimeout()
							require.False(t, deadline.IsZero())
							require.True(t, deadline.Before(c.sentPacketHandler.GetLossDetectionTimeout()))
						}
						stop := runNativeOrdinaryTestConnection(t, tc, nil)
						defer stop()
						time.Sleep(deadline.Sub(now) + time.Millisecond)
						synctest.Wait()
						require.NoError(t, c.ctx.Err())
						require.NotEmpty(t, writes, "tracking pressure must preserve actionable control timers")
						require.Equal(t, deadline, writes[0], "the connection selects the control deadline, not idle termination")
						frames := schedulingFrames(t, tc, packets[0])
						found := false
						for _, frame := range frames {
							_, ack := frame.(*wire.AckFrame)
							_, ping := frame.(*wire.PingFrame)
							found = found || (event == "ACK" && ack) || (event == "PTO" && ping)
						}
						require.True(t, found, "the real packer constructs the expected control packet")
					})
				})
			}
		}
	}
	t.Run("tracking quantum", func(t *testing.T) {
		nativeOrdinaryTest(t, func(t *testing.T) {
			require.Greater(t, protocol.MaxTrackedSentPackets-protocol.MaxOutstandingSentPackets, 2*(maxGSOSegments+3))
			tc := newEmissionTestConnection(t, false)
			c := tc.conn
			endpoint := startNativeEndpoint(endpointTestNative{}, 100*time.Millisecond, nil)
			require.NoError(t, c.useNativeOrdinary(endpoint, netip.MustParseAddrPort("1.2.3.4:4321"), packetInfo{}))
			queue := c.emission.local.(*nativeOrdinarySender)
			defer func() { queue.Close(); <-endpoint.stop() }()
			for queue.recovery.CanRegisterLocalPackets(3) {
				register(t, c, nil)
			}
			require.True(t, queue.recovery.CanRegisterLocalPackets(1))
			c.handshakeConfirmed = false
			c.initialStream.Write([]byte("coalesced work remains producer-owned"))
			before, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
			require.Equal(t, emissionQueueFull, c.emission.coalesced(monotime.Now()).stop)
			after, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
			require.Equal(t, before, after)
			require.True(t, c.initialStream.HasData())
			c.handshakeConfirmed = true
			require.False(t, queue.WouldBlock(), "the confirmed atomic quantum still fits")
			queue.finish()
			for queue.recovery.CanRegisterLocalPackets(1) {
				register(t, c, nil)
			}
			require.True(t, queue.WouldBlock(), "actual atomic-quantum exhaustion remains a hard block")
		})
	})
}

func runNativeOrdinaryTestConnection(t *testing.T, tc *testConnection, expected error) func() {
	t.Helper()
	tc.conn.idleTimeout = time.Second
	tc.connRunner.EXPECT().Remove(gomock.Any()).AnyTimes()
	tc.connRunner.EXPECT().ReplaceWithClosed(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	stopped := make(chan error, 1)
	go func() {
		var err error
		defer func() { stopped <- err }()
		err = tc.conn.run()
	}()
	return func() {
		tc.conn.destroy(nil)
		synctest.Wait()
		if expected != nil {
			require.ErrorIs(t, <-stopped, expected)
		} else {
			require.NoError(t, <-stopped)
		}
	}
}

func TestNativeOrdinaryStreamResumes(t *testing.T) {
	for _, scenario := range []struct {
		name, outcome, controller string
		ecn                       bool
	}{
		{name: "queued expiry", outcome: "queued expiry", controller: "reno"},
		{name: "native timeout", outcome: "native timeout", controller: "reno"},
		{name: "BBR ECN native timeout", outcome: "native timeout", controller: "bbrv3", ecn: true},
		{name: "destination failure", outcome: "destination", controller: "reno"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			nativeOrdinaryTest(t, func(t *testing.T) {
				outcome := scenario.outcome
				tc := bbrDepthTestConnection(t, scenario.controller, false)
				c := tc.conn
				if scenario.ecn {
					c.conn = &ecnTestSendConn{sendConn: c.conn, capable: true}
				}
				c.rttStats.UpdateRTT(5*time.Millisecond, 0)
				payload := []byte("reliable stream survives local backpressure")
				var packets [][]byte
				e := startNativeEndpoint(endpointTestNative{send: func(data []byte) (int, error) {
					if bytes.Equal(data, []byte("occupy native")) {
						time.Sleep(110 * time.Millisecond)
						return len(data), nil
					}
					packets = append(packets, bytes.Clone(data))
					if outcome == "destination" {
						return 0, &os.SyscallError{Syscall: "sendmsg", Err: syscall.ECONNREFUSED}
					}
					if outcome == "native timeout" && len(packets) == 1 {
						time.Sleep(100 * time.Millisecond)
						return 0, os.ErrDeadlineExceeded
					}
					return len(data), nil
				}}, 100*time.Millisecond, nil)
				defer func() { <-e.stop() }()
				if outcome == "queued expiry" {
					blocker, err := e.attach(77)
					require.NoError(t, err)
					require.NoError(t, e.offer(blocker, 0, endpointTestRequest(77, "occupy native")))
					synctest.Wait()
				}
				require.NoError(t, c.useNativeOrdinary(e, netip.MustParseAddrPort("1.2.3.4:4321"), packetInfo{}))
				c.connFlowController.UpdateSendWindow(1 << 20)
				flow := newStreamFlowController(0, c.connFlowController, 1<<20, 1<<20, 1<<20, c.flowControlRTT(), c.logger)
				stream := newSendStream(c.ctx, 0, c, flow, false)
				require.NoError(t, stream.TryWriteAll(payload))
				var expected error
				if outcome == "destination" {
					expected = syscall.ECONNREFUSED
				}
				stop := runNativeOrdinaryTestConnection(t, tc, expected)
				defer stop()
				time.Sleep(200 * time.Millisecond)
				synctest.Wait()
				if outcome == "destination" {
					require.ErrorIs(t, context.Cause(c.ctx), syscall.ECONNREFUSED)
					return
				}
				require.NoError(t, c.ctx.Err(), "local deadlines cannot terminate an ordinary connection")
				require.NotEmpty(t, packets)
				var previous protocol.PacketNumber = -1
				var streamPackets int
				for _, data := range packets {
					_, pn, _, _, err := wire.ParseShortHeader(data, c.connIDManager.Get().Len())
					require.NoError(t, err)
					require.Greater(t, pn, previous, "recovery constructs fresh packets instead of replaying encrypted bytes")
					previous = pn
					if bytes.Contains(data, payload) {
						streamPackets++
					}
				}
				require.GreaterOrEqual(t, previous, protocol.PacketNumber(1), "the disposed or uncertain packet number remains consumed")
				require.Positive(t, streamPackets)
				if outcome == "native timeout" {
					require.GreaterOrEqual(t, streamPackets, 2)
				}
			})
		})
	}
}
