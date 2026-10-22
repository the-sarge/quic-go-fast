package quic

import (
	"bytes"
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestConnectionPathValidationRetry(t *testing.T) {
	for _, test := range []struct {
		name        string
		firstSize   int
		answerFirst bool
	}{
		{name: "compact", firstSize: 100},
		{name: "padded", firstSize: 1200},
		{name: "MTU after compact response", firstSize: 100, answerFirst: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctrl := gomock.NewController(t)
				unpacker := NewMockUnpacker(ctrl)
				tc := newServerTestConnection(t, ctrl, &Config{DisablePathMTUDiscovery: true}, false, connectionOptUnpacker(unpacker), connectionOptHandshakeConfirmed())
				useSchedulingPacketPacker(t, ctrl, tc, nil)
				c := tc.conn
				require.NoError(t, c.handleTransportParameters(&wire.TransportParameters{MaxUDPPayloadSize: 1456}))
				addr := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 4242}
				var writes [][]byte
				tc.sendConn.EXPECT().WriteTo(gomock.Any(), addr, gomock.Any()).DoAndReturn(func(data []byte, _ net.Addr, _ packetInfo) error {
					writes = append(writes, bytes.Clone(data))
					return nil
				}).AnyTimes()
				pn := protocol.PacketNumber(10)
				receive := func(size int, frame wire.Frame) {
					t.Helper()
					payload := []byte{1} // PING requests migration after validation.
					if frame != nil {
						var err error
						payload, err = frame.Append(payload, c.version)
						require.NoError(t, err)
					}
					unpacker.EXPECT().UnpackShortHeader(gomock.Any(), gomock.Any()).Return(pn, protocol.PacketNumberLen2, protocol.KeyPhaseZero, payload, nil)
					packet := getShortHeaderPacket(t, addr, tc.srcConnID, pn, nil)
					packet.data = append(packet.data, make([]byte, size-len(packet.data))...)
					pn++
					processed, err := c.handleShortHeaderPacket(packet, false, 0)
					require.NoError(t, err)
					require.True(t, processed)
				}
				receive(test.firstSize, nil)
				require.Len(t, writes, 1)
				challenge, err := decodeSchedulingProbe(writes[0], c.connIDManager.Get())
				require.NoError(t, err)
				if test.answerFirst {
					receive(100, &wire.PathResponseFrame{Data: challenge.Data})
					require.Len(t, writes, 2)
					require.Len(t, writes[1], 1200)
					challenge, err = decodeSchedulingProbe(writes[1], c.connIDManager.Get())
					require.NoError(t, err)
				}
				_, challengePN, _, _, err := wire.ParseShortHeader(writes[len(writes)-1], c.connIDManager.Get().Len())
				require.NoError(t, err)
				time.Sleep(time.Millisecond)
				acked, err := c.sentPacketHandler.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: challengePN, Largest: challengePN}}}, protocol.Encryption1RTT, monotime.Now())
				require.NoError(t, err)
				require.True(t, acked)
				before := len(writes)
				receive(1200, nil)
				require.Len(t, writes, before, "respect the probe backoff")
				time.Sleep(200 * time.Millisecond)
				receive(1200, nil)
				require.Len(t, writes, before+1, "retry the unanswered challenge")
				retry, err := decodeSchedulingProbe(writes[len(writes)-1], c.connIDManager.Get())
				require.NoError(t, err)
				require.NotEqual(t, challenge.Data, retry.Data)
				tc.sendConn.EXPECT().ChangeRemoteAddr(addr, gomock.Any())
				receive(1200, &wire.PathResponseFrame{Data: retry.Data})
				require.EqualValues(t, 1, c.pathGeneration)
			})
		})
	}
}

func TestConnectionPathValidationExpiry(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(map[bool]string{false: "silent", true: "send blocked"}[blocked], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				tc := newEmissionTestConnection(t, false)
				c := tc.conn
				require.NoError(t, c.handleTransportParameters(&wire.TransportParameters{MaxIdleTimeout: time.Minute, MaxUDPPayloadSize: 1456, MaxDatagramFrameSize: 1200}))
				retired := make(chan pathID, 1)
				c.pathManager = newPathManager(
					func(pathID) (protocol.ConnectionID, bool) { return c.connIDManager.Get(), true },
					func(id pathID) { retired <- id },
					func() time.Duration { return 200 * time.Millisecond }, c.logger,
				)
				_, frames, _ := c.pathManager.HandlePacket(&net.UDPAddr{Port: 4242}, monotime.Now(), 1200, nil, true)
				require.Len(t, frames, 1)
				q := c.emission.queue.(*sendQueue)
				release := make(chan struct{})
				if blocked {
					for range sendQueueCapacity {
						q.Send(getPacketBuffer(), 0, protocol.ECNUnsupported, sendMetadata{})
					}
					require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("occupy the slot taken by the worker")}))
				}
				tc.sendConn.EXPECT().Write(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func([]byte, uint16, protocol.ECN) error {
					if blocked {
						<-release
					}
					return nil
				}).AnyTimes()
				tc.connRunner.EXPECT().Remove(gomock.Any()).AnyTimes()
				done := make(chan error, 1)
				go func() { done <- c.run() }()
				defer func() {
					close(release)
					c.destroy(nil)
					synctest.Wait()
					require.NoError(t, <-done)
					require.Empty(t, c.pathManager.paths)
				}()
				c.scheduleSending()
				synctest.Wait()
				if blocked {
					// The worker advertises room only after its write completes.
					// Wake the owner to fill the slot now held by the blocked write.
					c.scheduleSending()
					synctest.Wait()
					require.True(t, q.WouldBlock(), "send capacity remains occupied during expiry")
				}
				time.Sleep(5*time.Second - time.Nanosecond)
				synctest.Wait()
				require.Empty(t, retired)
				time.Sleep(time.Nanosecond)
				synctest.Wait()
				select {
				case id := <-retired:
					require.Equal(t, pathID(0), id)
				default:
					t.Error("candidate did not expire")
					select {
					case err := <-done:
						t.Logf("connection stopped before expiry: %v", err)
						done <- err
					default:
					}
				}
				require.Empty(t, c.pathManager.paths)
			})
		})
	}
}
