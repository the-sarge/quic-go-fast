package quic

import (
	"bytes"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

// gsoTestPacket is one packet of a handed-off emission buffer, decoded with
// the fixture's transparent seven-byte protection tag.
type gsoTestPacket struct {
	size        int
	datagrams   [][]byte
	streamBytes int
	others      int // frames that are neither DATAGRAM nor STREAM
}

type gsoTestWrite struct {
	opportunity int // the emission call that handed it off
	segment     uint16
	packets     []gsoTestPacket
}

func newGSOEmissionTest(t *testing.T, bbr bool) (*testConnection, monotime.Time) {
	t.Helper()
	tc := newEmissionTestConnection(t, true)
	now := monotime.Now()
	if bbr {
		tc.conn.emission.bbr = newBBRSendPolicy(100_000_000, 1200, now)
	}
	return tc, now
}

// emitGSOWrites runs emission until it stops making progress, draining and
// releasing every handed-off buffer. Time advances between opportunities so
// neither Reno's pacer nor BBR's budget ends the run early.
func emitGSOWrites(t *testing.T, tc *testConnection, now monotime.Time) []gsoTestWrite {
	t.Helper()
	c := tc.conn
	q := c.emission.queue.(*sendQueue)
	var writes []gsoTestWrite
	for opportunity := range 200 {
		result := c.emission.send(now, true)
		require.NoError(t, result.err)
		for drained := false; !drained; {
			select {
			case e := <-q.queue:
				w := decodeGSOTestWrite(t, c, e)
				w.opportunity = opportunity
				writes = append(writes, w)
				e.release()
			default:
				drained = true
			}
		}
		if !result.progress {
			return writes
		}
		now = now.Add(10 * time.Millisecond)
	}
	t.Fatal("emission did not settle")
	return nil
}

// decodeGSOTestWrite splits a buffer the way the kernel does and checks the
// equal-segment rule: every segment but the last is exactly the segment size,
// the last is no longer, and a batch has at least two and at most 64.
func decodeGSOTestWrite(t *testing.T, c *Conn, e queueEntry) gsoTestWrite {
	t.Helper()
	data := e.buf.Data
	w := gsoTestWrite{segment: e.gsoSize}
	segment := len(data)
	if e.gsoSize > 0 {
		segment = int(e.gsoSize)
	}
	for len(data) > 0 {
		n := min(len(data), segment)
		if e.gsoSize > 0 && n < segment {
			require.Len(t, data, n, "only the last segment may be shorter")
		}
		w.packets = append(w.packets, decodeGSOTestPacket(t, c.connIDManager.Get().Len(), data[:n]))
		data = data[n:]
	}
	if e.gsoSize > 0 {
		require.GreaterOrEqual(t, len(w.packets), 2, "a lone packet carries no segment size")
		require.LessOrEqual(t, len(w.packets), maxGSOSegments)
	}
	return w
}

func decodeGSOTestPacket(t *testing.T, connIDLen int, packet []byte) gsoTestPacket {
	t.Helper()
	payload, err := schedulingPacketPayload(packet, connIDLen)
	require.NoError(t, err)
	p := gsoTestPacket{size: len(packet)}
	parser := wire.NewFrameParser(true, false, false)
	for len(payload) > 0 {
		typ, n, err := parser.ParseType(payload, protocol.Encryption1RTT)
		require.NoError(t, err)
		payload = payload[n:]
		var m int
		switch {
		case typ == 0: // padding
		case typ.IsDatagramFrameType():
			var f *wire.DatagramFrame
			f, m, err = parser.ParseDatagramFrame(typ, payload, protocol.Version1)
			p.datagrams = append(p.datagrams, bytes.Clone(f.Data)) // the buffer returns to its pool
		case typ.IsStreamFrameType():
			var f *wire.StreamFrame
			f, m, err = parser.ParseStreamFrame(typ, payload, protocol.Version1)
			p.streamBytes += len(f.Data)
		case typ.IsAckFrameType():
			_, m, err = parser.ParseAckFrame(typ, payload, protocol.Encryption1RTT, protocol.Version1)
			p.others++
		default:
			_, m, err = parser.ParseLessCommonFrame(typ, payload, protocol.Version1)
			p.others++
		}
		require.NoError(t, err)
		payload = payload[m:]
	}
	return p
}

// gsoTestDatagramWrites summarizes DATAGRAM-only writes as the payload
// indexes of each packet, checking every payload is delivered intact.
func gsoTestDatagramWrites(t *testing.T, writes []gsoTestWrite, payloads [][]byte) ([][]int, []uint16) {
	t.Helper()
	var batches [][]int
	var segments []uint16
	next := 0
	for _, w := range writes {
		var batch []int
		for _, p := range w.packets {
			require.Len(t, p.datagrams, 1, "one DATAGRAM per packet")
			require.Zero(t, p.streamBytes)
			require.Zero(t, p.others)
			require.Less(t, next, len(payloads))
			require.Equal(t, payloads[next], p.datagrams[0], "DATAGRAMs leave in order, none discarded")
			batch = append(batch, next)
			next++
		}
		batches = append(batches, batch)
		segments = append(segments, w.segment)
	}
	require.Equal(t, len(payloads), next, "every queued DATAGRAM is sent")
	return batches, segments
}

func gsoTestPayloads(sizes ...int) [][]byte {
	var payloads [][]byte
	for i, size := range sizes {
		payloads = append(payloads, bytes.Repeat([]byte{byte(i + 1)}, size))
	}
	return payloads
}

func queueGSOTestDatagrams(t *testing.T, c *Conn, payloads [][]byte) {
	t.Helper()
	for _, p := range payloads {
		require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: p}))
	}
}

// A run of equal-size short DATAGRAM packets batches with the first packet's
// size as the segment size; a packet that comes out shorter ends the batch,
// and a DATAGRAM that does not fit the segment size stays queued for the next
// write instead of being discarded.
func TestGSOEqualSizeBatches(t *testing.T) {
	const maxSize = 1200
	for _, bbr := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "BBR"}[bbr], func(t *testing.T) {
			for _, tc := range []struct {
				name     string
				shortBy  []int // bytes each DATAGRAM packet is shorter than maxSize
				batches  [][]int
				segments []uint16
			}{
				{name: "equal short packets", shortBy: []int{60, 60, 60, 60}, batches: [][]int{{0, 1, 2, 3}}, segments: []uint16{maxSize - 60}},
				{name: "shorter packet ends the batch", shortBy: []int{60, 60, 61, 60}, batches: [][]int{{0, 1, 2}, {3}}, segments: []uint16{maxSize - 60, 0}},
				{name: "longer DATAGRAM waits for the next write", shortBy: []int{60, 60, 30, 60}, batches: [][]int{{0, 1}, {2, 3}}, segments: []uint16{maxSize - 60, maxSize - 30}},
				{name: "full-size batch with a short tail", shortBy: []int{0, 0, 60}, batches: [][]int{{0, 1, 2}}, segments: []uint16{maxSize}},
				{name: "lone short packet", shortBy: []int{60}, batches: [][]int{{0}}, segments: []uint16{0}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					test, now := newGSOEmissionTest(t, bbr)
					full := gsoDatagramPayloadSize(test)
					var sizes []int
					for _, short := range tc.shortBy {
						sizes = append(sizes, full-short)
					}
					payloads := gsoTestPayloads(sizes...)
					queueGSOTestDatagrams(t, test.conn, payloads)
					writes := emitGSOWrites(t, test, now)
					batches, segments := gsoTestDatagramWrites(t, writes, payloads)
					require.Equal(t, tc.batches, batches)
					require.Equal(t, tc.segments, segments)
					for i, w := range writes {
						for j, p := range w.packets {
							require.Equal(t, maxSize-tc.shortBy[tc.batches[i][j]], p.size)
						}
						// Ending a batch is not running out of data: Reno's loop
						// continues in the same opportunity; BBR hands off one
						// buffer per opportunity.
						if bbr {
							require.Equal(t, i, w.opportunity)
						} else {
							require.Zero(t, w.opportunity)
						}
					}
				})
			}
		})
	}
}

// QUIC_GO_DISABLE_SHORT_GSO_SEGMENTS restores the max-size-only rule: short
// DATAGRAM packets each go out alone, without a segment size (#742).
func TestGSOEqualSizeKillSwitch(t *testing.T) {
	t.Setenv(disableShortGSOSegmentsEnv, "true")
	for _, bbr := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "BBR"}[bbr], func(t *testing.T) {
			test, now := newGSOEmissionTest(t, bbr)
			full := gsoDatagramPayloadSize(test)
			payloads := gsoTestPayloads(full-60, full-60, full-60)
			queueGSOTestDatagrams(t, test.conn, payloads)
			batches, segments := gsoTestDatagramWrites(t, emitGSOWrites(t, test, now), payloads)
			require.Equal(t, [][]int{{0}, {1}, {2}}, batches)
			require.Equal(t, []uint16{0, 0, 0}, segments)
		})
	}
}

// Tiny DATAGRAMs never produce a batch of more than 64 segments, the oldest
// UDP_MAX_SEGMENTS Linux kernels still enforce, although far more fit the
// buffer.
func TestGSOEqualSizeSegmentLimit(t *testing.T) {
	for _, bbr := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "BBR"}[bbr], func(t *testing.T) {
			test, now := newGSOEmissionTest(t, bbr)
			sizes := make([]int, maxGSOSegments+16)
			for i := range sizes {
				sizes[i] = 8
			}
			payloads := gsoTestPayloads(sizes...)
			// Concurrent SendDatagram callers can keep the queue topped up past
			// its 32-entry admission bound; push directly to model that.
			for _, p := range payloads {
				test.conn.datagramQueue.sendQueue.PushBack(&wire.DatagramFrame{DataLenPresent: true, Data: p})
			}
			batches, _ := gsoTestDatagramWrites(t, emitGSOWrites(t, test, now), payloads)
			require.Len(t, batches, 2)
			require.Len(t, batches[0], maxGSOSegments)
			require.Len(t, batches[1], 16)
		})
	}
}

// queueGSOTestStream queues n bytes of stream data on one unidirectional
// stream, written by a goroutine that the test cleans up.
func queueGSOTestStream(t *testing.T, c *Conn, n int) {
	t.Helper()
	c.peerParams = &wire.TransportParameters{InitialMaxStreamDataUni: 1 << 20}
	c.connFlowController.UpdateSendWindow(1 << 20)
	c.streamsMap.HandleMaxStreamsFrame(&wire.MaxStreamsFrame{Type: protocol.StreamTypeUni, MaxStreamNum: 1})
	s, err := c.OpenUniStream()
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = s.Write(make([]byte, n))
	}()
	synctest.Wait()
	t.Cleanup(func() {
		s.CancelWrite(0)
		synctest.Wait()
		<-done
	})
}

// requireFullSizeStream checks that writes carry exactly n stream bytes in
// STREAM-only packets, every one full size except the last, batched with
// the maximum packet size as their segment size.
func requireFullSizeStream(t *testing.T, writes []gsoTestWrite, maxSize, n int) {
	t.Helper()
	var packets []gsoTestPacket
	for _, w := range writes {
		if len(w.packets) > 1 {
			require.Equal(t, uint16(maxSize), w.segment)
		}
		packets = append(packets, w.packets...)
	}
	streamBytes := 0
	for i, p := range packets {
		require.Empty(t, p.datagrams)
		require.Positive(t, p.streamBytes)
		if i < len(packets)-1 {
			require.Equal(t, maxSize, p.size, "stream packets are full size")
		}
		streamBytes += p.streamBytes
	}
	require.Equal(t, n, streamBytes)
}

// Stream data is never packed with a short segment size as its limit: after
// a short DATAGRAM packet, only DATAGRAM packets continue the batch, and the
// queued stream data starts a fresh full-size batch.
func TestGSOEqualSizeStreamStartsFreshBatch(t *testing.T) {
	const maxSize = 1200
	for _, bbr := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "BBR"}[bbr], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				test, now := newGSOEmissionTest(t, bbr)
				full := gsoDatagramPayloadSize(test)
				// 60 bytes is less than a minimum STREAM frame, so the first
				// packet carries only its DATAGRAM despite the queued stream.
				payloads := gsoTestPayloads(full-60, full-60)
				queueGSOTestDatagrams(t, test.conn, payloads)
				queueGSOTestStream(t, test.conn, 6000)
				writes := emitGSOWrites(t, test, now)
				require.GreaterOrEqual(t, len(writes), 2)
				require.Equal(t, uint16(maxSize-60), writes[0].segment)
				require.Len(t, writes[0].packets, 2)
				for i, p := range writes[0].packets {
					require.Equal(t, [][]byte{payloads[i]}, p.datagrams)
					require.Zero(t, p.streamBytes)
				}
				requireFullSizeStream(t, writes[1:], maxSize, 6000)
			})
		})
	}
}

// Only a packet carrying a DATAGRAM frame may set a short segment size. A
// short control-only packet goes alone, and the stream data behind it is
// packed at full size.
func TestGSOEqualSizeControlPacketGoesAlone(t *testing.T) {
	const maxSize = 1200
	for _, bbr := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "BBR"}[bbr], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				test, now := newGSOEmissionTest(t, bbr)
				c := test.conn
				_, pnLen := c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
				maxPayload := maxSize - int(wire.ShortHeaderLen(c.connIDManager.Get(), pnLen)) - 7
				// Leave 100 bytes, less than a minimum STREAM frame, after a
				// NEW_TOKEN frame with a two-byte length.
				c.framer.QueueControlFrame(&wire.NewTokenFrame{Token: make([]byte, maxPayload-100-3)})
				queueGSOTestStream(t, c, 6000)
				writes := emitGSOWrites(t, test, now)
				require.GreaterOrEqual(t, len(writes), 2)
				require.Zero(t, writes[0].segment)
				require.Len(t, writes[0].packets, 1)
				require.Equal(t, 1, writes[0].packets[0].others)
				require.Zero(t, writes[0].packets[0].streamBytes)
				require.Equal(t, maxSize-100, writes[0].packets[0].size)
				requireFullSizeStream(t, writes[1:], maxSize, 6000)
			})
		})
	}
}

// BBR's bounded path reserves whole maximum-size packets; a batch of shorter
// segments still resizes the reservation to the physical bytes and spends
// exactly those bytes of pacing budget.
func TestBBRBoundedEqualSizeCredit(t *testing.T) {
	test, now := newGSOEmissionTest(t, true)
	c := test.conn
	full := gsoDatagramPayloadSize(test)
	queueGSOTestDatagrams(t, c, gsoTestPayloads(full-60, full-60, full-60))
	budget := c.emission.bbr.budget(now)
	require.True(t, c.emission.send(now, true).progress)
	q := c.emission.queue.(*sendQueue)
	require.Len(t, q.queue, 1)
	entry := <-q.queue
	defer entry.release()
	require.Equal(t, uint16(1200-60), entry.gsoSize)
	require.Equal(t, protocol.ByteCount(3*(1200-60)), entry.buf.Len())
	require.Equal(t, entry.buf.Len(), c.emission.bbr.credit.pending, "the reservation is resized to the physical bytes")
	require.Equal(t, budget-entry.buf.Len(), c.emission.bbr.budget(now), "pacing spends the physical bytes")
}

// The batch owner's continuation rule: a full-size first packet continues; a
// short one continues only if it carries a DATAGRAM frame and the switch is
// on; later packets continue while they match the segment size, up to the
// segment limit.
func TestGSOBatchContinuation(t *testing.T) {
	datagram := []ackhandler.Frame{{Frame: &wire.DatagramFrame{Data: []byte("d")}}}
	control := []ackhandler.Frame{{Frame: &wire.PingFrame{}}}
	for _, tc := range []struct {
		name   string
		short  bool
		size   protocol.ByteCount
		frames []ackhandler.Frame
		more   bool
	}{
		{name: "full size", short: true, size: 1200, frames: control, more: true},
		{name: "full size, switch off", size: 1200, frames: control, more: true},
		{name: "short DATAGRAM packet", short: true, size: 1100, frames: datagram, more: true},
		{name: "short control-only packet", short: true, size: 1100, frames: control},
		{name: "short ACK-only packet", short: true, size: 40},
		{name: "short DATAGRAM packet, switch off", size: 1100, frames: datagram},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newGSOBatch(1200, tc.short)
			limit, datagramOnly := b.next()
			require.Equal(t, protocol.ByteCount(1200), limit)
			require.False(t, datagramOnly)
			require.Equal(t, tc.more, b.add(tc.size, tc.frames))
			require.Zero(t, b.gsoSize(), "one packet has no segment size")
			if !tc.more {
				return
			}
			limit, datagramOnly = b.next()
			require.Equal(t, tc.size, limit)
			require.Equal(t, tc.size < 1200, datagramOnly)
			require.False(t, b.add(tc.size-1, datagram), "a shorter packet is the last segment")
			require.Equal(t, uint16(tc.size), b.gsoSize())
		})
	}
	t.Run("segment limit", func(t *testing.T) {
		b := newGSOBatch(1200, true)
		for range maxGSOSegments - 1 {
			require.True(t, b.add(100, datagram))
		}
		require.False(t, b.add(100, datagram))
		require.Equal(t, uint16(100), b.gsoSize())
	})
}
