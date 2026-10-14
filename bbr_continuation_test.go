package quic

import (
	"bytes"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/mocks"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestBBRContinuationWithoutLoopWakeup(t *testing.T) {
	t.Run("ordinary handoff", func(t *testing.T) {
		c := newEmissionTestConnection(t, false).conn
		now := monotime.Now()
		c.emission.bbr = newBBRSendPolicy(100_000_000, 1200, now)
		q := c.emission.queue.(*sendQueue)
		defer func() {
			for len(q.queue) > 0 {
				(<-q.queue).release()
			}
		}()
		for _, d := range []string{"first", "second", "third"} {
			require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte(d)}))
		}
		select { // a token left by connection setup
		case <-c.sendingScheduled:
		default:
		}
		for i := range 3 {
			result := c.triggerSending(now)
			require.NoError(t, result.err)
			require.True(t, result.progress)
			require.False(t, result.retry, "continuation needs no scheduling token")
			require.Equal(t, deadlineSendImmediately, result.deadline)
			require.Equal(t, deadlineSendImmediately, c.pacingDeadline)
			require.Zero(t, len(c.sendingScheduled), "no scheduling token")
			require.Len(t, q.queue, i+1, "still one datagram per opportunity")
		}
		result := c.triggerSending(now)
		require.False(t, result.progress)
		require.NotEqual(t, deadlineSendImmediately, result.deadline, "nothing left: no continuation")
		require.Len(t, q.queue, 3)
	})

	t.Run("paced stop keeps its deadline", func(t *testing.T) {
		c := newEmissionTestConnection(t, false).conn
		now := monotime.Now()
		c.emission.bbr = newBBRSendPolicy(1_000_000, 1200, now)
		q := c.emission.queue.(*sendQueue)
		defer func() {
			for len(q.queue) > 0 {
				(<-q.queue).release()
			}
		}()
		for range 3 {
			require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: bytes.Repeat([]byte{'d'}, 1100)}))
		}
		var result emissionResult
		for range 3 {
			result = c.triggerSending(now)
			require.NoError(t, result.err)
			if result.stop == emissionPaced {
				break
			}
			require.Equal(t, deadlineSendImmediately, result.deadline)
		}
		require.Equal(t, emissionPaced, result.stop, "Q holds two full datagrams")
		require.True(t, result.deadline.After(now))
		require.Equal(t, result.deadline, c.pacingDeadline)
		require.Len(t, q.queue, 2)
	})
}

// bbrLoopConnection runs BBR emission through the real connection loop. onSeal
// runs on the connection goroutine while each 1-RTT packet is sealed.
func bbrLoopConnection(t *testing.T, onSeal func(c *Conn, n int, payload []byte)) *testConnection {
	tc := newConfiguredEmissionTestConnection(t, false, &Config{InitialPacketSize: 1200, EnableDatagrams: true, DisablePathMTUDiscovery: true, MaxIdleTimeout: 10 * time.Second})
	c := tc.conn
	ctrl := gomock.NewController(t)
	sealer := mocks.NewMockShortHeaderSealer(ctrl)
	sealer.EXPECT().KeyPhase().Return(protocol.KeyPhaseOne).AnyTimes()
	sealer.EXPECT().Overhead().Return(7).AnyTimes()
	sealer.EXPECT().EncryptHeader(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	n := 0
	sealer.EXPECT().Seal(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_, src []byte, _ protocol.PacketNumber, _ []byte) []byte {
		n++
		onSeal(c, n, src)
		return append(src, bytes.Repeat([]byte{'s'}, 7)...)
	}).AnyTimes()
	sealing := NewMockSealingManager(ctrl)
	sealing.EXPECT().Get1RTTSealer().Return(sealer, nil).AnyTimes()
	c.emission.packer = newPacketPacker(tc.srcConnID, c.connIDManager.Get, c.initialStream, c.handshakeStream, c.sentPacketHandler, c.retransmissionQueue, sealing, c.framer, &c.receivedPacketHandler, c.datagramQueue, c.perspective)
	c.emission.bbr = newBBRSendPolicy(100_000_000, 1200, monotime.Now())
	tc.sendConn.EXPECT().Write(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	return tc
}

// A received packet, a close request and an expired timer arriving while one
// BBR datagram is built are each serviced by the connection before the next.
func TestBBRContinuationServicesConnectionLoop(t *testing.T) {
	datagrams := [][]byte{[]byte("datagram A"), []byte("datagram B"), []byte("datagram C")}
	sealedB := func(payload []byte) bool { return bytes.Contains(payload, datagrams[1]) }
	sealedC := func(payload []byte) bool { return bytes.Contains(payload, datagrams[2]) }
	run := func(t *testing.T, tc *testConnection, advance time.Duration) error {
		c := tc.conn
		for _, d := range datagrams {
			require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: d}))
		}
		tc.connRunner.EXPECT().Remove(gomock.Any()).AnyTimes()
		tc.connRunner.EXPECT().ReplaceWithClosed(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		errCh := make(chan error, 1)
		go func() { errCh <- c.run() }()
		c.scheduleSending()
		synctest.Wait()
		if advance > 0 {
			// Let a hook's sleep finish before teardown can race the loop.
			time.Sleep(advance)
			synctest.Wait()
		}
		c.destroy(nil)
		synctest.Wait()
		return <-errCh
	}

	t.Run("received packet", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var queuedAtB int
			sawB := false
			tc := bbrLoopConnection(t, func(c *Conn, n int, payload []byte) {
				switch {
				case bytes.Contains(payload, datagrams[0]):
					// Dropped at header parsing (unsupported version), before any decryption.
					buf := getPacketWithContents(append([]byte{0xc0, 0xde, 0xad, 0xbe, 0xef, 0, 0}, make([]byte, 40)...))
					c.handlePacket(receivedPacket{data: buf.Data, buffer: buf, rcvTime: monotime.Now()})
				case sealedB(payload):
					sawB = true
					c.receivedPacketMx.Lock()
					queuedAtB = c.receivedPackets.Len()
					c.receivedPacketMx.Unlock()
				}
			})
			require.NoError(t, run(t, tc, 0))
			require.True(t, sawB)
			require.Zero(t, queuedAtB, "the packet received during A is handled before B is built")
		})
	})

	t.Run("close request", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			sawB := false
			tc := bbrLoopConnection(t, func(c *Conn, n int, payload []byte) {
				if bytes.Contains(payload, datagrams[0]) {
					c.destroyImpl(errors.New("close during A"))
				}
				sawB = sawB || sealedB(payload)
			})
			err := run(t, tc, 0)
			require.ErrorContains(t, err, "close during A")
			require.False(t, sawB, "close is serviced before B is built")
		})
	})

	t.Run("idle timer", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			sawC := false
			tc := bbrLoopConnection(t, func(c *Conn, n int, payload []byte) {
				if sealedB(payload) {
					// Past the idle timeout (no negotiated value: 3 PTO), inside B's opportunity.
					time.Sleep(time.Minute)
				}
				sawC = sawC || sealedC(payload)
			})
			err := run(t, tc, 2*time.Minute)
			var idle *qerr.IdleTimeoutError
			require.ErrorAs(t, err, &idle)
			require.False(t, sawC, "the expired idle timer is serviced before C is built")
		})
	})
}
