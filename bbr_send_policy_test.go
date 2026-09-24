package quic

import (
	"bytes"
	"fmt"
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/stretchr/testify/require"
)

func TestBBRPacingQuantumAndDeadline(t *testing.T) {
	now := monotime.Now()
	p := newBBRSendPolicy(100_000_000, 1200, now)
	require.EqualValues(t, 65536, p.quantum)
	require.EqualValues(t, 65536, p.budget(now))
	p.sent(65536, now)
	require.Equal(t, now.Add(12122*time.Nanosecond), p.deadline(1200, now))
	require.EqualValues(t, 1199, p.budget(now.Add(12121*time.Nanosecond)))
	require.EqualValues(t, 1200, p.budget(now.Add(12122*time.Nanosecond)))
	require.EqualValues(t, 65536, p.budget(now.Add(time.Second)))
	p = newBBRSendPolicy(1_000_000, 1200, now)
	require.EqualValues(t, 2560, newBBRSendPolicy(1_000_000, 1280, now).quantum)
	require.EqualValues(t, 2400, p.quantum)
	p.sent(2400, now)
	require.Equal(t, now.Add(1212122*time.Nanosecond), p.deadline(1200, now))
}

func TestBBRPendingCreditWorkerOwned(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctrl := gomock.NewController(t)
		conn := NewMockSendConn(ctrl)
		q := newSendQueue(conn, nil)
		credit := newLocalSendCredit()
		blocked := make(chan struct{})
		conn.EXPECT().Write(gomock.Any(), uint16(0), protocol.ECNNon).DoAndReturn(func([]byte, uint16, protocol.ECN) error { <-blocked; return nil })
		reservation := credit.reserve(1200, 2400, true, false)
		require.NotNil(t, reservation)
		buf := getPacketWithContents(make([]byte, 1200))
		q.Send(buf, 0, protocol.ECNNon, sendMetadata{credit: reservation})
		go func() { require.NoError(t, q.Run()) }()
		synctest.Wait()
		require.NotNil(t, credit.reserve(1200, 2400, true, false))
		require.Nil(t, credit.reserve(1, 2400, true, false), "dequeue must not return credit")
		close(blocked)
		synctest.Wait()
		require.NotNil(t, credit.reserve(1200, 2400, true, false), "completion must return credit")
		q.Close()
		require.Zero(t, buf.refCount)
	})

	t.Run("coalesced physical payload", func(t *testing.T) {
		c := newHandshakeEmissionConnection(t, true).conn
		now := monotime.Now()
		c.emission.bbr = newBBRSendPolicy(1_000_000, 1200, now)
		c.retransmissionQueue.addInitial(&wire.PingFrame{})
		c.handshakeStream.Write([]byte("coalesced data"))
		require.True(t, c.triggerSending(now).progress)
		entry := <-c.emission.queue.(*sendQueue).queue
		defer entry.release()
		hdrs, _ := parsePacket(t, entry.buf.Data)
		require.Len(t, hdrs, 2)
		require.Equal(t, entry.buf.Len(), c.emission.bbr.credit.pending)
		require.Equal(t, protocol.ByteCount(2400)-entry.buf.Len(), c.emission.bbr.budget(now))
	})
	t.Run("ACK does not spend ordinary credit", func(t *testing.T) {
		c := newEmissionTestConnection(t, false).conn
		now := monotime.Now()
		c.emission.bbr = newBBRSendPolicy(1_000_000, 1200, now)
		require.NoError(t, c.receivedPacketHandler.ReceivedPacket(4, protocol.ECNNon, protocol.Encryption1RTT, now, true))
		require.NoError(t, c.receivedPacketHandler.ReceivedPacket(5, protocol.ECNNon, protocol.Encryption1RTT, now, true))
		require.True(t, c.triggerSending(now).progress)
		entry := <-c.emission.queue.(*sendQueue).queue
		defer entry.release()
		require.EqualValues(t, 2400, c.emission.bbr.budget(now))
		require.Equal(t, entry.buf.Len(), c.emission.bbr.credit.pending)
	})
	t.Run("ACK and PTO exemptions", func(t *testing.T) {
		c := newEmissionTestConnection(t, false).conn
		now := monotime.Now()
		c.emission.bbr = newBBRSendPolicy(1_000_000, 1200, now)
		p := c.emission.bbr
		q := c.emission.queue.(*sendQueue)
		defer func() {
			for len(q.queue) > 0 {
				(<-q.queue).release()
			}
		}()
		require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("outstanding data")}))
		require.True(t, c.triggerSending(now).progress)
		alarm := c.sentPacketHandler.GetLossDetectionTimeout()
		require.NoError(t, c.sentPacketHandler.OnLossDetectionTimeout(alarm))
		p.sent(p.budget(alarm), alarm)
		before := p.credit.pending
		require.True(t, c.triggerSending(alarm).progress, "PTO bypasses exhausted ordinary pacing")
		require.Greater(t, p.credit.pending, before)
		require.Zero(t, p.budget(alarm))
		// Consume the remaining authorized PTO; after that only ACK traffic is exempt.
		require.True(t, c.triggerSending(alarm).progress)
		require.NoError(t, c.receivedPacketHandler.ReceivedPacket(4, protocol.ECNNon, protocol.Encryption1RTT, alarm, true))
		require.NoError(t, c.receivedPacketHandler.ReceivedPacket(5, protocol.ECNNon, protocol.Encryption1RTT, alarm, true))
		result := c.triggerSending(alarm)
		require.NoError(t, result.err)
		require.True(t, result.progress)
		require.Equal(t, emissionPaced, result.stop)
		require.Zero(t, p.budget(alarm))
	})
}

func TestBBRPendingCreditConcurrentDrainRefill(t *testing.T) {
	for _, gso := range []bool{false, true} {
		t.Run(fmt.Sprint(gso), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				tc := newEmissionTestConnection(t, gso)
				c := tc.conn
				now := monotime.Now()
				c.emission.bbr = newBBRSendPolicy(1_000_000, 1200, now)
				// Full datagrams make the physical-byte bound visible at the real packer.
				for range 6 {
					require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: make([]byte, 1187)}))
				}
				release := make(chan struct{})
				tc.sendConn.EXPECT().Write(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func([]byte, uint16, protocol.ECN) error { <-release; return nil }).AnyTimes()
				q := c.emission.queue.(*sendQueue)
				go func() { require.NoError(t, q.Run()) }()
				t.Cleanup(func() {
					select {
					case <-release:
					default:
						close(release)
					}
					q.Close()
				})
				for range 6 {
					require.NoError(t, c.emission.send(now, true).err)
				}
				synctest.Wait()
				now = now.Add(3 * time.Millisecond)
				for range 6 {
					require.NoError(t, c.emission.send(now, true).err)
				}
				synctest.Wait()
				result := c.emission.send(now.Add(3*time.Millisecond), true)
				require.Equal(t, emissionQueueFull, result.stop)
				require.NotNil(t, result.available, "byte credit must return a wakeup even with queue slots free")
				require.NotNil(t, c.datagramQueue.Peek(), "credit refusal must precede destructive packing")
				require.EqualValues(t, 4800, c.emission.bbr.credit.pending)
				close(release)
				synctest.Wait()
				require.Zero(t, c.emission.bbr.credit.pending)
				require.NoError(t, c.emission.send(now.Add(6*time.Millisecond), true).err)
				synctest.Wait()
			})
		})
	}

	t.Run("whole datagram window allowance", func(t *testing.T) {
		c := newEmissionTestConnection(t, false).conn
		now := monotime.Now()
		c.emission.bbr = newBBRSendPolicy(100_000_000, 1200, now)
		q := c.emission.queue.(*sendQueue)
		defer func() {
			for len(q.queue) > 0 {
				(<-q.queue).release()
			}
		}()
		require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("window remainder")}))
		require.True(t, c.triggerSending(now).progress)
		(<-q.queue).release()
		for range 100 {
			require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: make([]byte, 1187)}))
			now = now.Add(time.Millisecond)
			result := c.triggerSending(now)
			if result.stop == emissionCongestionLimited {
				_, allowance := c.sentPacketHandler.(boundedRecovery).SendAllowance(now)
				require.Greater(t, allowance, protocol.ByteCount(0))
				require.Less(t, allowance, protocol.ByteCount(1200))
				require.Empty(t, q.queue)
				require.NotNil(t, c.datagramQueue.Peek())
				return
			}
			require.True(t, result.progress)
			(<-q.queue).release()
		}
		t.Fatal("did not reach the real recovery window")
	})
}

func TestBBRPendingCreditPartialAndUnknownProgress(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(fmt.Sprint(unknown), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				credit := newLocalSendCredit()
				conn := &fakeBatchSendConn{accept: func(_ int, bufs [][]byte) (int, error) {
					require.Nil(t, credit.reserve(1, 3600, true, false), "worker group retains its reservation")
					if unknown {
						return 1, assert.AnError
					}
					return 1, nil
				}}
				q := newSendQueue(conn, nil)
				var bufs []*packetBuffer
				for i := range 3 {
					b := getPacketWithContents(bytes.Repeat([]byte{byte(i)}, 1200))
					bufs = append(bufs, b)
					q.Send(b, 0, protocol.ECNNon, sendMetadata{credit: credit.reserve(1200, 3600, true, false)})
				}
				var want error
				if unknown {
					want = assert.AnError
				}
				startAndFinishQueue(t, q, want)
				if unknown {
					require.Empty(t, conn.writes)
				} else {
					require.Len(t, conn.writes, 2)
					require.EqualValues(t, 1, conn.writes[0].data[0])
					require.EqualValues(t, 2, conn.writes[1].data[0])
				}
				require.Zero(t, credit.pending)
				for _, b := range bufs {
					require.Zero(t, b.refCount)
				}
			})
		})
	}
}

func TestBBRPendingCreditStoppedAndClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		credit := newLocalSendCredit()
		conn := NewMockSendConn(gomock.NewController(t))
		q := newSendQueue(conn, nil)
		finish := make(chan struct{})
		conn.EXPECT().Write(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func([]byte, uint16, protocol.ECN) error { <-finish; return assert.AnError })
		active := getPacketWithContents([]byte("active"))
		q.Send(active, 0, protocol.ECNNon, sendMetadata{credit: credit.reserve(active.Len(), 1000, true, false)})
		done := make(chan error, 1)
		go func() { done <- q.Run() }()
		synctest.Wait()
		var bufs []*packetBuffer
		for range sendQueueCapacity {
			b := getPacketWithContents([]byte("pending"))
			bufs = append(bufs, b)
			q.Send(b, 0, protocol.ECNNon, sendMetadata{credit: credit.reserve(b.Len(), 1000, true, false)})
		}
		close(finish)
		require.ErrorIs(t, <-done, assert.AnError)
		rejected := getPacketWithContents([]byte("rejected"))
		before := credit.pending
		q.Send(rejected, 0, protocol.ECNNon, sendMetadata{credit: credit.reserve(rejected.Len(), 1000, true, false)})
		require.Equal(t, before, credit.pending)
		require.Zero(t, rejected.refCount)
		q.Close()
		require.Zero(t, credit.pending)
		require.Zero(t, active.refCount)
		for _, b := range bufs {
			require.Zero(t, b.refCount)
		}
	})
}

func TestBBRPendingCreditGSOFallback(t *testing.T) {
	if !platformSupportsGSO {
		t.Skip("sequential GSO fallback requires Linux; hosted Linux gate owns this cell")
	}
	synctest.Test(t, func(t *testing.T) {
		credit := newLocalSendCredit()
		raw := NewMockRawConn(gomock.NewController(t))
		raw.EXPECT().LocalAddr()
		remote := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}
		conn := newSendConn(raw, remote, packetInfo{}, utils.DefaultLogger)
		q := newSendQueue(conn, nil)
		b := getPacketWithContents([]byte("abcdef"))
		raw.EXPECT().WritePacket(b.Data, remote, gomock.Any(), uint16(3), protocol.ECNNon).Return(0, errGSO)
		gomock.InOrder(
			raw.EXPECT().WritePacket([]byte("abc"), remote, gomock.Any(), uint16(0), protocol.ECNNon).DoAndReturn(func([]byte, net.Addr, []byte, uint16, protocol.ECN) (int, error) {
				require.Nil(t, credit.reserve(1, 6, true, false))
				return 3, nil
			}),
			raw.EXPECT().WritePacket([]byte("def"), remote, gomock.Any(), uint16(0), protocol.ECNNon).DoAndReturn(func([]byte, net.Addr, []byte, uint16, protocol.ECN) (int, error) {
				require.Nil(t, credit.reserve(1, 6, true, false))
				return 0, assert.AnError
			}),
		)
		q.Send(b, 3, protocol.ECNNon, sendMetadata{credit: credit.reserve(6, 6, true, false)})
		startAndFinishQueue(t, q, assert.AnError)
		require.Zero(t, credit.pending)
		require.Zero(t, b.refCount)
	})
}

func TestBBRPendingCreditRateDecrease(t *testing.T) {
	now := monotime.Now()
	p := newBBRSendPolicy(100_000_000, 1200, now)
	r := p.credit.reserve(9000, 2*p.quantum, true, false)
	require.NotNil(t, r)
	p.update(1_000_000, 1200, now)
	require.EqualValues(t, 2400, p.budget(now))
	require.Nil(t, p.credit.reserve(1200, 2*p.quantum, true, false))
	require.EqualValues(t, 9000, p.credit.pending, "rate reduction cannot erase committed debt")
	r.complete()
	next := p.credit.reserve(1200, 2*p.quantum, true, false)
	require.NotNil(t, next)
	next.complete()
	p.sent(2400, now)
	require.EqualValues(t, 2400, p.budget(now.Add(time.Hour)), "worker delay cannot accumulate catch-up credit")
}

func TestBBRPendingCreditMTUException(t *testing.T) {
	c := newEmissionTestConnection(t, false).conn
	now := monotime.Now()
	c.emission.bbr = newBBRSendPolicy(1_000_000, 1200, now)
	p := c.emission.bbr
	c.mtuDiscoverer = newMTUDiscoverer(c.rttStats, 1200, 1400, nil)
	c.mtuDiscoverer.Start(now.Add(-time.Hour))
	held := p.credit.reserve(1200, 2*p.quantum, true, false)
	result := c.triggerSending(now)
	require.Equal(t, emissionQueueFull, result.stop)
	require.True(t, c.mtuDiscoverer.ShouldSendProbe(now), "refusal must not consume probe state")
	held.complete()
	result = c.triggerSending(now)
	require.NoError(t, result.err)
	require.True(t, result.progress)
	q := c.emission.queue.(*sendQueue)
	entry := <-q.queue
	defer func() {
		if entry.buf != nil {
			entry.release()
		}
	}()
	require.Len(t, entry.buf.Data, 1300)
	require.EqualValues(t, 1300, p.credit.pending)
	require.Nil(t, p.credit.reserve(1, 2*p.quantum, false, false), "even exempt control waits for the isolated probe")
	// Exercise the exceptional reservation itself beyond Q; no platform is
	// claimed to support that probe size by this typed credit test.
	entry.release()
	entry.buf = nil
	entry.metadata.credit = nil
	exception := p.credit.reserve(7000, 2*p.quantum, false, true)
	require.NotNil(t, exception)
	require.Nil(t, p.credit.reserve(1, 2*p.quantum, true, false))
	exception.complete()
	require.Zero(t, p.credit.pending)
}

func TestBBRPendingCreditMigrationDebt(t *testing.T) {
	c := newEmissionTestConnection(t, false).conn
	now := monotime.Now()
	c.emission.bbr = newBBRSendPolicy(1_000_000, 1200, now)
	require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("old path")}))
	require.True(t, c.triggerSending(now).progress)
	q := c.emission.queue.(*sendQueue)
	old := <-q.queue
	defer func() {
		if old.buf != nil {
			old.release()
		}
	}()
	c.emission.resetLocalPath(1, now)
	require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("new path")}))
	result := c.triggerSending(now)
	require.Equal(t, emissionQueueFull, result.stop)
	require.NotNil(t, result.available)
	require.NotNil(t, c.datagramQueue.Peek())
	// A control reservation is still permitted inside the total bound.
	control := c.emission.bbr.credit.reserve(1200, 4800, false, false)
	require.NotNil(t, control)
	control.complete()
	old.release()
	old.buf = nil
	old.metadata.credit = nil
	require.True(t, c.triggerSending(now).progress)
	current := <-q.queue
	defer current.release()
	require.EqualValues(t, 1, current.metadata.pathGeneration)
	require.EqualValues(t, 1, current.metadata.credit.generation)
}
