package quic

import (
	"net"
	"testing"
	"testing/synctest"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeBatchWrite struct {
	data []byte
	gso  uint16
	ecn  protocol.ECN
}

// fakeBatchSendConn is the closure suite's fake batch sender: batched
// submissions are scripted through accept, and every batch and per-packet
// write is recorded so tests can assert exactly-once delivery and per-entry
// error attribution.
type fakeBatchSendConn struct {
	available func() bool
	accept    func(call int, bufs [][]byte) int
	writeErr  func(call int, b []byte) error

	batches   [][][]byte
	batchECNs []protocol.ECN
	writes    []fakeBatchWrite
}

var _ sendConn = &fakeBatchSendConn{}

func (c *fakeBatchSendConn) batchSendAvailable() bool {
	if c.available == nil {
		return true
	}
	return c.available()
}

func (c *fakeBatchSendConn) sendBatch(bufs [][]byte, ecn protocol.ECN) int {
	copied := make([][]byte, len(bufs))
	for i, b := range bufs {
		copied[i] = append([]byte(nil), b...)
	}
	c.batches = append(c.batches, copied)
	c.batchECNs = append(c.batchECNs, ecn)
	return c.accept(len(c.batches)-1, bufs)
}

func (c *fakeBatchSendConn) Write(b []byte, gsoSize uint16, ecn protocol.ECN) error {
	c.writes = append(c.writes, fakeBatchWrite{data: append([]byte(nil), b...), gso: gsoSize, ecn: ecn})
	if c.writeErr == nil {
		return nil
	}
	return c.writeErr(len(c.writes)-1, b)
}

func (c *fakeBatchSendConn) WriteTo([]byte, net.Addr, packetInfo) error { return nil }
func (c *fakeBatchSendConn) Close() error                               { return nil }
func (c *fakeBatchSendConn) LocalAddr() net.Addr                        { return nil }
func (c *fakeBatchSendConn) RemoteAddr() net.Addr                       { return nil }
func (c *fakeBatchSendConn) ChangeRemoteAddr(net.Addr, packetInfo)      {}
func (c *fakeBatchSendConn) capabilities() connCapabilities             { return connCapabilities{} }

func startAndFinishQueue(t *testing.T, q sender, wantErr error) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- q.Run() }()
	synctest.Wait()
	q.Close()
	if wantErr == nil {
		require.NoError(t, <-done)
	} else {
		require.ErrorIs(t, <-done, wantErr)
	}
}

// Full acceptance: every queued entry that shares (gsoSize 0, ecn) is
// submitted as one batch, accepted in full, and never resent per-packet.
func TestSendQueueBatchFullAcceptance(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		conn := &fakeBatchSendConn{accept: func(_ int, bufs [][]byte) int { return len(bufs) }}
		q := newSendQueue(conn, nil)

		payloads := [][]byte{[]byte("pkt0"), []byte("pkt1"), []byte("pkt2")}
		bufs := make([]*packetBuffer, len(payloads))
		for i, p := range payloads {
			bufs[i] = getPacketWithContents(p)
			q.Send(bufs[i], 0, protocol.ECT1, sendMetadata{})
		}

		done := make(chan error, 1)
		go func() { done <- q.Run() }()
		synctest.Wait()
		q.Close()
		require.NoError(t, <-done)

		require.Empty(t, conn.writes, "full acceptance must not fall back to per-packet sends")
		require.Len(t, conn.batches, 1, "expected exactly one batch submission")
		require.Equal(t, payloads, conn.batches[0], "batch must carry every payload in order")
		require.Equal(t, []protocol.ECN{protocol.ECT1}, conn.batchECNs)
		for _, buf := range bufs {
			require.Zero(t, buf.refCount, "every batched buffer must be released exactly once")
		}
	})
}

// Partial acceptance then success: accepted entries are never resent, the
// first unaccepted entry is retried through the per-packet path, and the
// tail re-enters batching without loss or duplication.
func TestSendQueueBatchPartialAcceptance(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		conn := &fakeBatchSendConn{accept: func(call int, bufs [][]byte) int {
			if call == 0 {
				return 2 // accept pkt0, pkt1; pkt2 is the first unaccepted entry
			}
			return len(bufs)
		}}
		q := newSendQueue(conn, nil)

		payloads := [][]byte{[]byte("pkt0"), []byte("pkt1"), []byte("pkt2"), []byte("pkt3"), []byte("pkt4")}
		bufs := make([]*packetBuffer, len(payloads))
		for i, p := range payloads {
			bufs[i] = getPacketWithContents(p)
			q.Send(bufs[i], 0, protocol.ECT0, sendMetadata{})
		}
		startAndFinishQueue(t, q, nil)

		require.Len(t, conn.batches, 2)
		require.Equal(t, payloads, conn.batches[0], "first submission offers every queued payload")
		require.Len(t, conn.writes, 1, "exactly the first unaccepted entry is retried per-packet")
		require.Equal(t, []byte("pkt2"), conn.writes[0].data)
		require.Equal(t, [][]byte{[]byte("pkt3"), []byte("pkt4")}, conn.batches[1], "the tail re-enters batching")
		for _, buf := range bufs {
			require.Zero(t, buf.refCount)
		}
	})
}

// Partial acceptance then a message-size error: the per-packet retry of the
// first unaccepted entry attaches the size error and handshake MTU feedback
// to that entry's own metadata, and the connection is not torn down.
func TestSendQueueBatchPartialAcceptanceMsgSizeFeedback(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		conn := &fakeBatchSendConn{
			accept: func(call int, bufs [][]byte) int {
				if call == 0 {
					return 1
				}
				return len(bufs)
			},
			writeErr: func(_ int, b []byte) error {
				if len(b) == 1452 {
					return testutils.SendMsgSizeErr
				}
				return nil
			},
		}
		feedback := &handshakeSendFeedback{wakeup: make(chan struct{}, 1)}
		q := newSendQueue(conn, feedback)

		// pkt1 is the entry the size error must attach to: an eligible
		// handshake datagram with its own path generation.
		bufs := []*packetBuffer{
			getPacketWithContents(make([]byte, 1300)),
			getPacketWithContents(make([]byte, 1452)),
			getPacketWithContents(make([]byte, 1280)),
		}
		q.Send(bufs[0], 0, protocol.ECNNon, sendMetadata{})
		q.Send(bufs[1], 0, protocol.ECNNon, sendMetadata{handshake: true, pathGeneration: 9})
		q.Send(bufs[2], 0, protocol.ECNNon, sendMetadata{})
		startAndFinishQueue(t, q, nil)

		require.Len(t, conn.batches, 1)
		require.Len(t, conn.writes, 2, "the retried entry and the single-entry tail go per-packet")
		require.Len(t, conn.writes[0].data, 1452, "the retry must carry the first unaccepted entry")
		require.Len(t, conn.writes[1].data, 1280)
		generation, pending := feedback.take()
		require.True(t, pending, "the size error must publish feedback for the retried entry")
		require.EqualValues(t, 9, generation, "feedback must carry the retried entry's generation")
		_, pending = feedback.take()
		require.False(t, pending, "only the failing entry publishes feedback")
		for _, buf := range bufs {
			require.Zero(t, buf.refCount)
		}
	})
}

// Partial acceptance then a non-size error: the per-packet retry surfaces the
// fatal error, the run loop returns it, and every dequeued buffer is
// released exactly once.
func TestSendQueueBatchPartialAcceptanceFatalError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		conn := &fakeBatchSendConn{
			accept:   func(int, [][]byte) int { return 1 },
			writeErr: func(int, []byte) error { return assert.AnError },
		}
		q := newSendQueue(conn, nil)

		bufs := make([]*packetBuffer, 4)
		for i := range bufs {
			bufs[i] = getPacketWithContents([]byte{byte(i)})
			q.Send(bufs[i], 0, protocol.ECNNon, sendMetadata{})
		}
		startAndFinishQueue(t, q, assert.AnError)

		require.Len(t, conn.writes, 1, "the fatal error must stop the group")
		for _, buf := range bufs {
			require.Zero(t, buf.refCount, "a fatal batched send must release every dequeued buffer")
		}
	})
}

// Structural failure (e.g. ENOSYS latch): the batch layer reports the
// capability as unavailable, and every entry goes through the per-packet
// path — the batch submission path is never entered.
func TestSendQueueBatchUnavailableFallsBack(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		conn := &fakeBatchSendConn{
			available: func() bool { return false },
			accept: func(int, [][]byte) int {
				t.Error("sendBatch must not be called while unavailable")
				return 0
			},
		}
		q := newSendQueue(conn, nil)

		bufs := make([]*packetBuffer, 3)
		for i := range bufs {
			bufs[i] = getPacketWithContents([]byte{byte(i)})
			q.Send(bufs[i], 0, protocol.ECT1, sendMetadata{})
		}
		startAndFinishQueue(t, q, nil)

		require.Empty(t, conn.batches)
		require.Len(t, conn.writes, 3, "every entry must fall back to the per-packet path")
		for _, buf := range bufs {
			require.Zero(t, buf.refCount)
		}
	})
}

// A mid-group latch: the capability can turn unavailable between
// submissions; the remaining entries drain through the per-packet path
// without loss or duplication.
func TestSendQueueBatchMidGroupLatch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		latched := false
		conn := &fakeBatchSendConn{}
		conn.available = func() bool { return !latched }
		conn.accept = func(int, [][]byte) int {
			latched = true // e.g. a structurally invalid result latched the capability off
			return 1
		}
		q := newSendQueue(conn, nil)

		payloads := [][]byte{[]byte("pkt0"), []byte("pkt1"), []byte("pkt2")}
		bufs := make([]*packetBuffer, len(payloads))
		for i, p := range payloads {
			bufs[i] = getPacketWithContents(p)
			q.Send(bufs[i], 0, protocol.ECT1, sendMetadata{})
		}
		startAndFinishQueue(t, q, nil)

		require.Len(t, conn.batches, 1)
		require.Len(t, conn.writes, 2, "after the latch the tail drains per-packet")
		require.Equal(t, []byte("pkt1"), conn.writes[0].data)
		require.Equal(t, []byte("pkt2"), conn.writes[1].data)
		for _, buf := range bufs {
			require.Zero(t, buf.refCount)
		}
	})
}

// Entries that do not share (gsoSize, ecn) never share a batch: a differing
// entry flushes the group and starts a new one, preserving order.
func TestSendQueueBatchGroupsByECN(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		conn := &fakeBatchSendConn{accept: func(_ int, bufs [][]byte) int { return len(bufs) }}
		q := newSendQueue(conn, nil)

		bufs := []*packetBuffer{
			getPacketWithContents([]byte("a0")),
			getPacketWithContents([]byte("a1")),
			getPacketWithContents([]byte("b0")),
			getPacketWithContents([]byte("b1")),
		}
		q.Send(bufs[0], 0, protocol.ECT1, sendMetadata{})
		q.Send(bufs[1], 0, protocol.ECT1, sendMetadata{})
		q.Send(bufs[2], 0, protocol.ECNCE, sendMetadata{})
		q.Send(bufs[3], 0, protocol.ECNCE, sendMetadata{})
		startAndFinishQueue(t, q, nil)

		require.Empty(t, conn.writes)
		require.Len(t, conn.batches, 2)
		require.Equal(t, [][]byte{[]byte("a0"), []byte("a1")}, conn.batches[0])
		require.Equal(t, [][]byte{[]byte("b0"), []byte("b1")}, conn.batches[1])
		require.Equal(t, []protocol.ECN{protocol.ECT1, protocol.ECNCE}, conn.batchECNs)
		for _, buf := range bufs {
			require.Zero(t, buf.refCount)
		}
	})
}

// GSO-segmented entries (gsoSize > 0) never enter a batched submission; they
// keep the per-packet path that owns UDP_SEGMENT encoding.
func TestSendQueueBatchSkipsGSO(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		conn := &fakeBatchSendConn{accept: func(int, [][]byte) int {
			t.Error("sendBatch must not be called for GSO-segmented entries")
			return 0
		}}
		q := newSendQueue(conn, nil)

		bufs := make([]*packetBuffer, 2)
		for i := range bufs {
			bufs[i] = getPacketWithContents([]byte("gso"))
			q.Send(bufs[i], 1200, protocol.ECT1, sendMetadata{})
		}
		startAndFinishQueue(t, q, nil)

		require.Empty(t, conn.batches)
		require.Len(t, conn.writes, 2)
		require.Equal(t, uint16(1200), conn.writes[0].gso)
		for _, buf := range bufs {
			require.Zero(t, buf.refCount)
		}
	})
}
