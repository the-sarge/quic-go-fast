package quic

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func bbrDepthTestConnection(t *testing.T, controller string, gso bool) *testConnection {
	t.Helper()
	conf := &Config{InitialPacketSize: 1200, EnableDatagrams: true, DisablePathMTUDiscovery: true}
	require.NoError(t, conf.SetCongestionControlV1(controller))
	return newConfiguredEmissionTestConnection(t, gso, conf)
}

// A BBR connection's queue holds bbrSendQueueCapacity entries and a Reno
// connection's sendQueueCapacity, at creation and across path replacement.
func TestBBRSendQueueDepth(t *testing.T) {
	for _, tc := range []struct {
		controller string
		depth      int
	}{{"bbrv3", bbrSendQueueCapacity}, {"reno", sendQueueCapacity}} {
		t.Run(tc.controller, func(t *testing.T) {
			tc2 := bbrDepthTestConnection(t, tc.controller, false)
			c := tc2.conn
			require.Equal(t, tc.controller == "bbrv3", c.emission.bbr != nil)
			old := c.emission.queue.(*sendQueue)
			require.Equal(t, tc.depth, cap(old.queue))
			go old.Run() // replacePath drains and joins the old worker
			q := c.emission.replacePath(tc2.sendConn, &c.handshakeSendFeedback).(*sendQueue)
			require.Equal(t, tc.depth, cap(q.queue), "path replacement keeps the depth")
			go q.Run()
			q.Close()
		})
	}
}

// With one-packet entries the deeper queue, not the credit, stops a BBR
// connection: behind a busy worker the queue takes exactly
// bbrSendQueueCapacity entries, the emission then reports a full-queue stop
// that waits on the queue's signal, a freed slot signals it, and pending
// bytes stay within 2Q throughout.
func TestBBRSendQueueDepthOnePacketEntries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tc := bbrDepthTestConnection(t, "bbrv3", false)
		c := tc.conn
		now := monotime.Now()
		c.emission.bbr = newBBRSendPolicy(1_000_000_000, 1200, now) // Q = 65536: the credit admits far more than the queue holds
		credit, bound := c.emission.bbr.credit, 2*c.emission.bbr.quantum
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
		// The first entry is dequeued into the blocked submission; then the queue fills.
		for i := 0; i <= bbrSendQueueCapacity; i++ {
			require.False(t, q.WouldBlock(), "entry %d", i)
			require.Nil(t, c.emission.capacity())
			r := credit.reserve(1200, bound, true, false)
			require.NotNil(t, r, "the credit admits every one-packet entry")
			q.Send(getPacketWithContents(make([]byte, 1200)), 0, protocol.ECNNon, sendMetadata{credit: r})
			synctest.Wait()
		}
		require.LessOrEqual(t, credit.pending, bound)
		require.Len(t, q.queue, bbrSendQueueCapacity)
		require.True(t, q.WouldBlock())
		available := c.emission.capacity()
		require.Equal(t, q.Available(), available, "the full queue stops the connection")
		require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("waits")}))
		result := c.triggerSending(now)
		require.Equal(t, emissionQueueFull, result.stop)
		require.Equal(t, q.Available(), result.available)
		require.NotNil(t, c.datagramQueue.Peek(), "a full queue stops before packing")
		close(release)
		synctest.Wait()
		select {
		case <-available:
		default:
			t.Fatal("a freed slot must signal the waiting connection")
		}
		require.False(t, q.WouldBlock())
		require.Zero(t, credit.pending)
	})
}

// With full-size GSO entries the 2Q credit binds before the deeper queue: the
// connection stops on the credit's wakeup with queue slots free, pending
// reaches at most 2Q, and completions return every byte.
func TestBBRSendQueueDepthCreditBindsFirst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tc := bbrDepthTestConnection(t, "bbrv3", true)
		c := tc.conn
		now := monotime.Now()
		c.emission.bbr = newBBRSendPolicy(1_000_000, 1200, now) // Q = 2400, 2Q = 4800
		bound := 2 * c.emission.bbr.quantum
		for range 16 {
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
		var stop emissionResult
		for i := 0; i < 64 && stop.available == nil; i++ {
			now = now.Add(3 * time.Millisecond)
			for range 4 {
				r := c.emission.send(now, true)
				require.NoError(t, r.err)
				require.LessOrEqual(t, c.emission.bbr.credit.pending, bound)
				if r.stop == emissionQueueFull {
					stop = r
					break
				}
			}
			synctest.Wait()
		}
		require.Equal(t, (<-chan struct{})(c.emission.bbr.credit.available), stop.available, "byte credit refuses first")
		require.Equal(t, bound, c.emission.bbr.credit.pending)
		require.Less(t, len(q.queue), bbrSendQueueCapacity)
		close(release)
		synctest.Wait()
		require.Zero(t, c.emission.bbr.credit.pending)
	})
}

// Teardown with a full deep queue: a fatal write stops the worker with
// bbrSendQueueCapacity entries still queued, and Close releases every buffer
// and returns every reservation exactly once.
func TestBBRSendQueueDepthCloseReleasesOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctrl := gomock.NewController(t)
		conn := NewMockSendConn(ctrl)
		q := newSendQueueWithCapacity(conn, nil, bbrSendQueueCapacity).(*sendQueue)
		credit := newLocalSendCredit()
		bound := protocol.ByteCount(2 * 65536)
		finish := make(chan struct{})
		conn.EXPECT().Write(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func([]byte, uint16, protocol.ECN) error { <-finish; return assert.AnError })
		send := func() *packetBuffer {
			b := getPacketWithContents(make([]byte, 1200))
			q.Send(b, 0, protocol.ECNNon, sendMetadata{credit: credit.reserve(1200, bound, true, false)})
			return b
		}
		bufs := []*packetBuffer{send()}
		done := make(chan error, 1)
		go func() { done <- q.Run() }()
		synctest.Wait()
		for range bbrSendQueueCapacity {
			bufs = append(bufs, send())
		}
		require.True(t, q.WouldBlock())
		require.EqualValues(t, 1200*(bbrSendQueueCapacity+1), credit.pending)
		close(finish)
		require.ErrorIs(t, <-done, assert.AnError)
		q.Close()
		require.Zero(t, credit.pending)
		for _, b := range bufs {
			require.Zero(t, b.refCount)
		}
	})
}

// Reno's queue is unchanged: full at sendQueueCapacity entries.
func TestBBRSendQueueDepthRenoUnchanged(t *testing.T) {
	tc := bbrDepthTestConnection(t, "reno", false)
	q := tc.conn.emission.queue.(*sendQueue)
	for range sendQueueCapacity {
		require.False(t, q.WouldBlock())
		q.Send(getPacketWithContents([]byte("reno")), 0, protocol.ECNNon, sendMetadata{})
	}
	require.True(t, q.WouldBlock())
	for len(q.queue) > 0 {
		(<-q.queue).release()
	}
}
