package quic

import (
	"bytes"
	"testing"
	"testing/synctest"
	"time"

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
	c.packer = newPacketPacker(tc.srcConnID, c.connIDManager.Get, c.initialStream, c.handshakeStream, c.sentPacketHandler, c.retransmissionQueue, sealing, c.framer, &c.receivedPacketHandler, c.datagramQueue, c.perspective)
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
			q := c.sendQueue.(*sendQueue)
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
				q := c.sendQueue.(*sendQueue)
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
			q := c.sendQueue.(*sendQueue)
			close(q.closeCalled)
			require.NoError(t, q.Run())
			require.Equal(t, want, got)
			require.Equal(t, []protocol.PacketNumber{0, 1, 2}, packets)
			require.Equal(t, map[bool]int{false: 3, true: 1}[gso], writes)
		})
	}
}
