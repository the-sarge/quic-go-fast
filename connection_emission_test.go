package quic

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/handshake"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// These characterizations retain the real packer, frame producers, recovery and
// send queue. Only protection is transparent, so the socket boundary can decode
// output without establishing TLS. Native protection/GSO are measured separately.
func newEmissionTestConnection(t *testing.T, gso bool) *testConnection {
	t.Helper()
	ctrl := gomock.NewController(t)
	tc := newServerTestConnection(t, ctrl, &Config{InitialPacketSize: 1200, EnableDatagrams: true, DisablePathMTUDiscovery: true}, gso, connectionOptHandshakeConfirmed())
	c := tc.conn
	sealing := NewMockSealingManager(ctrl)
	sealing.EXPECT().Get1RTTSealer().Return(newMockShortHeaderSealer(ctrl), nil).AnyTimes()
	c.emission.packer = newPacketPacker(tc.srcConnID, c.connIDManager.Get, c.initialStream, c.handshakeStream, c.sentPacketHandler, c.retransmissionQueue, sealing, c.framer, &c.receivedPacketHandler, c.datagramQueue, c.perspective)
	c.sentPacketHandler.ReceivedBytes(1<<20, monotime.Now())
	c.sentPacketHandler.DropPackets(protocol.EncryptionInitial, monotime.Now())
	c.sentPacketHandler.DropPackets(protocol.EncryptionHandshake, monotime.Now())
	return tc
}

func TestEmissionReceiveFairness(t *testing.T) {
	for _, gso := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "GSO"}[gso], func(t *testing.T) {
			tc := newEmissionTestConnection(t, gso)
			c := tc.conn
			first := &wire.DatagramFrame{DataLenPresent: true, Data: []byte("first short batch")}
			next := &wire.DatagramFrame{DataLenPresent: true, Data: []byte("next batch")}
			require.NoError(t, c.datagramQueue.Add(first))
			require.NoError(t, c.datagramQueue.Add(next))
			// A pending receive must get a turn before another batch. The receive
			// handler is outside this seam; the sentinel is never decoded.
			c.receivedPackets.PushBack(receivedPacket{})
			require.NoError(t, c.sendPackets(monotime.Now()))
			require.Same(t, next, c.datagramQueue.Peek())
			require.Equal(t, deadlineSendImmediately, c.pacingDeadline)
			q := c.emission.queue.(*sendQueue)
			require.Len(t, q.queue, 1)
			c.receivedPackets.PopFront()
			require.NoError(t, c.sendPackets(monotime.Now()))
			require.Nil(t, c.datagramQueue.Peek())
			require.Len(t, q.queue, 2)
			tc.sendConn.EXPECT().Write(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(2)
			close(q.closeCalled)
			require.NoError(t, q.Run())
		})
	}
}

func TestEmissionFullQueueResume(t *testing.T) {
	for _, gso := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "GSO"}[gso], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				tc := newEmissionTestConnection(t, gso)
				c := tc.conn
				q := c.emission.queue.(*sendQueue)
				for range sendQueueCapacity {
					buf := getPacketBuffer()
					buf.Data = append(buf.Data, 0xff)
					q.Send(buf, 0, protocol.ECNUnsupported, sendMetadata{})
				}
				first := &wire.DatagramFrame{DataLenPresent: true, Data: []byte("one available slot")}
				next := &wire.DatagramFrame{DataLenPresent: true, Data: []byte("resume after worker drains")}
				require.NoError(t, c.datagramQueue.Add(first))
				require.NoError(t, c.datagramQueue.Add(next))
				release := make(chan struct{})
				writes, output := 0, 0
				tc.sendConn.EXPECT().Write(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(b []byte, _ uint16, _ protocol.ECN) error {
					writes++
					if writes == 1 {
						<-release
					}
					if len(b) > 1 {
						output++
					}
					return nil
				}).AnyTimes()
				errCh := make(chan error, 1)
				go func() { errCh <- c.run() }()
				released := false
				defer func() {
					if !released {
						close(release)
					}
					tc.connRunner.EXPECT().Remove(gomock.Any()).AnyTimes()
					c.destroy(nil)
					synctest.Wait()
					require.NoError(t, <-errCh)
				}()
				c.scheduleSending()
				synctest.Wait()
				// Wake the connection after the worker has taken its first entry.
				// A worker advertises availability only after its syscall returns.
				c.scheduleSending()
				synctest.Wait()
				require.True(t, q.WouldBlock())
				require.Same(t, next, c.datagramQueue.Peek(), "full queue must not destructively pack the next batch")
				pn, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
				require.EqualValues(t, 1, pn)
				close(release)
				released = true
				synctest.Wait()
				require.Nil(t, c.datagramQueue.Peek())
				require.Equal(t, 2, output)
			})
		})
	}
}

func TestEmissionDatagramOutput(t *testing.T) {
	for _, gso := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "GSO"}[gso], func(t *testing.T) {
			tc := newEmissionTestConnection(t, gso)
			c := tc.conn
			_, pnLen := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
			// Fill a segment, including the transparent sealer's seven-byte tag
			// and the DATAGRAM type and two-byte length.
			payloadSize := 1200 - int(wire.ShortHeaderLen(c.connIDManager.Get(), pnLen)) - 7 - 3
			want := [][]byte{bytes.Repeat([]byte{0x41}, payloadSize), bytes.Repeat([]byte{0x42}, payloadSize), []byte("last short datagram")}
			for _, payload := range want {
				require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: payload}))
			}
			require.NoError(t, c.sendPackets(monotime.Now()))
			var got [][]byte
			var packets []protocol.PacketNumber
			writes := 0
			tc.sendConn.EXPECT().Write(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(b []byte, segment uint16, ecn protocol.ECN) error {
				writes++
				require.Equal(t, protocol.ECNUnsupported, ecn)
				if gso {
					require.EqualValues(t, 1200, segment)
				} else {
					require.Zero(t, segment)
				}
				for len(b) > 0 {
					n := len(b)
					if segment != 0 {
						n = min(n, int(segment))
					}
					packet := b[:n]
					b = b[n:]
					hdrLen, pn, _, _, err := wire.ParseShortHeader(packet, c.connIDManager.Get().Len())
					require.NoError(t, err)
					packets = append(packets, pn)
					parser := wire.NewFrameParser(true, false, false)
					payload := packet[hdrLen : n-7]
					typ, consumed, err := parser.ParseType(payload, protocol.Encryption1RTT)
					require.NoError(t, err)
					require.True(t, typ.IsDatagramFrameType())
					frame, consumedFrame, err := parser.ParseDatagramFrame(typ, payload[consumed:], protocol.Version1)
					require.NoError(t, err)
					require.Equal(t, len(payload), consumed+consumedFrame)
					got = append(got, bytes.Clone(frame.Data))
					// An ACK accepted at the socket boundary demonstrates registration
					// already happened, rather than happening after I/O completion.
					acked, err := c.sentPacketHandler.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: pn, Largest: pn}}}, protocol.Encryption1RTT, monotime.Now().Add(time.Millisecond))
					require.NoError(t, err)
					require.True(t, acked)
				}
				return nil
			}).AnyTimes()
			q := c.emission.queue.(*sendQueue)
			close(q.closeCalled)
			require.NoError(t, q.Run())
			require.Equal(t, want, got)
			require.Equal(t, []protocol.PacketNumber{0, 1, 2}, packets)
			require.Equal(t, map[bool]int{false: 3, true: 1}[gso], writes)
		})
	}
}

// As in observeConstructionBuffer, install an empty pool for sequential
// observations without a worker. Keep each allocation distinct until checked.
func observeEmissionBuffers(t *testing.T, gso bool) *[]*packetBuffer {
	t.Helper()
	pool, capacity := &bufferPool, protocol.MaxPacketBufferSize
	if gso {
		pool, capacity = &largeBufferPool, protocol.MaxLargePacketBufferSize
	}
	constructor := pool.New
	var observed []*packetBuffer
	*pool = sync.Pool{New: func() any {
		b := &packetBuffer{Data: make([]byte, 0, capacity)}
		observed = append(observed, b)
		return b
	}}
	t.Cleanup(func() { *pool = sync.Pool{New: constructor} })
	return &observed
}

func TestEmissionFatalCallerBuffer(t *testing.T) {
	for _, gso := range []bool{false, true} {
		for _, failAt := range []int{1, 2} {
			t.Run(fmt.Sprintf("gso=%t/append=%d", gso, failAt), func(t *testing.T) {
				tc := newEmissionTestConnection(t, gso)
				c := tc.conn
				cause := errors.New("fatal packet construction")
				ctrl := gomock.NewController(t)
				sealing := NewMockSealingManager(ctrl)
				sealer := newMockShortHeaderSealer(ctrl)
				appends := 0
				sealing.EXPECT().Get1RTTSealer().DoAndReturn(func() (handshake.ShortHeaderSealer, error) {
					appends++
					if appends == failAt {
						return nil, cause
					}
					return sealer, nil
				}).Times(failAt)
				c.emission.packer.cryptoSetup = sealing
				observed := observeEmissionBuffers(t, gso)
				_, pnLen := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
				size := 1200 - int(wire.ShortHeaderLen(c.connIDManager.Get(), pnLen)) - 7 - 3
				for range 2 {
					require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: bytes.Repeat([]byte{0x41}, size)}))
				}
				require.ErrorIs(t, c.sendPackets(monotime.Now()), cause)
				allocations := failAt
				if gso {
					allocations = 1
				}
				require.Len(t, *observed, allocations)
				if !gso && failAt == 2 {
					require.Equal(t, 1, (*observed)[0].refCount, "earlier ordinary output remains owned by the queue")
				}
				// No allocation follows emission: pool reuse cannot disguise release.
				require.Zero(t, (*observed)[allocations-1].refCount, "fatal caller-owned storage must be released")
				q := c.emission.queue.(*sendQueue)
				queued := 0
				if !gso {
					queued = failAt - 1
				}
				require.Len(t, q.queue, queued)
				pn, _ := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
				require.EqualValues(t, failAt-1, pn, "fatal output must not refund packet numbers or registration")
				if failAt == 2 {
					acked, err := c.sentPacketHandler.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: 0, Largest: 0}}}, protocol.Encryption1RTT, monotime.Now().Add(time.Millisecond))
					require.NoError(t, err)
					require.True(t, acked, "registration survives failed handoff")
				}
				for len(q.queue) > 0 {
					(<-q.queue).buf.Release()
				}
			})
		}
	}
}

// Preserve the historical outcome/allocation fixture entrypoint.
func (c *Conn) sendPackets(now monotime.Time) error { return c.emitPackets(now).err }

func (c *Conn) emitPackets(now monotime.Time) emissionResult {
	result := c.emission.finish(c.emission.sendAny(now, c.handshakeConfirmed))
	c.pacingDeadline = result.deadline
	if result.retry {
		c.scheduleSending()
	}
	return result
}
