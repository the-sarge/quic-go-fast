package quic

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
	_ "unsafe" // go:linkname

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

// Measurement-only #744 control-path recorder. It times each control request
// of the campaign fixture (a 32-byte message on stream 0, echoed by the
// receiver) through both endpoints' send paths. Both endpoints run the same
// binary and stamp CLOCK_MONOTONIC (runtime.nanotime), which loopback
// endpoints on one host share, so their stamps are directly comparable.
//
// Events, in each endpoint's own log (ctl-<role>.json beside -output):
//   - write: the application writes a 32-byte message on stream 0 (its id and
//     the stream's write offset);
//   - pack: a stream-0 STREAM frame is popped for a packet (offset range,
//     retransmission flag);
//   - enqueue: the queue entry carrying the last popped stream-0 frame enters
//     the send queue, with the entries queued ahead of it (the channel's length,
//     read once) and their bytes (the sizes of that many most recently enqueued
//     entries, from the connection loop's own ring, so both describe the same
//     snapshot), the queue's capacity and the endpoint's cumulative enqueued
//     bytes;
//   - dequeue: the send worker takes that entry;
//   - submit: the worker is about to hand that entry to the socket (before the
//     write, so a peer's receipt always follows it);
//   - done: that entry's write returned (stamped in release, before the buffer
//     is recycled);
//   - discard: a tagged entry was released without being submitted;
//   - arrive: a stream-0 STREAM frame is handled (its packet's socket-read time
//     and the handling time);
//   - read: an application read on stream 0 returns (the read position after);
//   - note: the fixture's sender records each reply's id and whether its
//     request was offered inside the measured window.
//
// Per-packet cost: a stream-ID compare in the frame paths; in Send (connection
// loop only) one store into a private ring of entry sizes and two counter
// increments; and at dequeue, submission and release (send worker) one load of
// the in-flight tag count, which is zero except while a control entry is
// queued. Nothing on an ordinary packet is shared between the two goroutines
// except that read-mostly count. Clock reads and the event log run only for
// stream-0 events.
//
// build.py copies this file into exported fixture trees and rewrites the
// synthetic-case constants for the Stage 0 cases. Not a maintained hook.

//go:linkname ctlNanotime runtime.nanotime
func ctlNanotime() int64

const ctlStream protocol.StreamID = 0

// Synthetic-case injections (microseconds; 0 disables). build.py rewrites them
// for the registered Stage 0 cases only.
const (
	ctlSynSenderHold   = 0 // sender: the worker holds a tagged entry before its dequeue stamp
	ctlSynReceiverHold = 0 // receiver: the same at the receiver's queue
	ctlSynSlowWorker   = 0 // sender: the worker spins before every submission
	ctlSynBusyLoop     = 0 // receiver: the connection loop spins for every received packet
	ctlSynReplyDelay   = 0 // receiver: the fixture spins between reading a request and writing its reply
	ctlSynUnplaced     = 0 // sender: a tagged entry's submission stamp precedes a spin and its write
)

const (
	ceWrite = iota
	cePack
	ceEnqueue
	ceDequeue
	ceSubmit
	ceDone
	ceDiscard
	ceArrive
	ceRead
	ceNote
)

var ceNames = []string{"write", "pack", "enqueue", "dequeue", "submit", "done", "discard", "arrive", "read", "note"}

// ctlEvent fields by kind (T is CLOCK_MONOTONIC nanoseconds):
//
//	write:   A offset, B id
//	pack:    A offset, B end, C 1 for a retransmission, R pack sequence
//	enqueue: A entries ahead (the channel's length), B their bytes, C capacity, D entry bytes,
//	         E first pack sequence in the entry, F cumulative enqueued bytes before it, G the channel's length
//	         (equal to A; kept for the schema), R last pack sequence in the entry
//	dequeue, submit, done, discard: R last pack sequence of the entry
//	arrive:  T socket-read time, A offset, B end, C handling time
//	read:    A read position after, B bytes
//	note:    A id, B measured (0/1), C fixture sent and D fixture received (wall-clock UnixNano)
type ctlEvent struct {
	K                   int8
	T                   int64
	A, B, C, D, E, F, G int64
	R                   int64
}

const (
	// About ten events per request at each endpoint: a loopback-long run logs about 1,300. The log is a
	// fixed-size array in BSS, not a heap allocation, so it does not move the Go heap or its GC goal; only
	// the pages written become resident.
	ctlMaxEvents = 4096
	ctlRing      = 64 // entry sizes kept by the connection loop: a power of two, at least the deepest queue (32)
)

var ctlEvents [ctlMaxEvents]ctlEvent

type ctlSlot struct {
	buf atomic.Pointer[packetBuffer]
	ref atomic.Int64
}

var ctl struct {
	on        bool
	role      string
	output    string
	monoOff   int64 // runtime.nanotime minus monotime.Now, from the tightest bracketed sample
	monoHalf  int64 // that sample's half-width
	wall0     int64
	mono0     int64
	mu        sync.Mutex
	n         int // events logged into ctlEvents
	dropped   int
	nPack     int64 // connection loop: pack sequence
	pending   bool  // connection loop: a stream-0 frame was popped since the last enqueue
	first     int64
	last      int64
	slots     [2]ctlSlot
	exhausted atomic.Int64 // tags lost because both slots were busy
	holdArmed atomic.Bool
	_         [64]byte
	inflight  atomic.Int32 // tagged entries not yet released: the worker's only per-packet read
	_         [60]byte
	enqCount  uint64 // connection loop only
	enqBytes  int64  // connection loop only
	ring      [ctlRing]int32
}

// ctlCalibrate brackets monotime.Now between two nanotime reads and keeps the
// tightest of 101 samples: the offset at its midpoint and its half-width.
func ctlCalibrate() (off, half int64) {
	half = math.MaxInt64
	for range 101 {
		t1 := ctlNanotime()
		m := int64(monotime.Now())
		t2 := ctlNanotime()
		if h := (t2 - t1 + 1) / 2; h < half {
			half, off = h, t1+(t2-t1)/2-m
		}
	}
	return off, half
}

// ctlQuiet keeps every hook but never enables logging or tagging (development-only variants).
const ctlQuiet = false

func init() {
	if ctlQuiet {
		return
	}
	var out, role string
	for i, a := range os.Args {
		if (a == "-output" || a == "--output") && i+1 < len(os.Args) {
			out = os.Args[i+1]
		}
		if (a == "-role" || a == "--role") && i+1 < len(os.Args) {
			role = os.Args[i+1]
		}
	}
	if out == "" || (role != "send" && role != "receive") {
		return
	}
	ctl.role = role
	ctl.output = filepath.Join(filepath.Dir(out), "ctl-"+role+".json")
	ctl.monoOff, ctl.monoHalf = ctlCalibrate()
	ctl.wall0, ctl.mono0 = time.Now().UnixNano(), ctlNanotime()
	ctl.on = true
}

func ctlLog(e ctlEvent) {
	ctl.mu.Lock()
	if ctl.n < ctlMaxEvents {
		ctlEvents[ctl.n] = e
		ctl.n++
	} else {
		ctl.dropped++
	}
	ctl.mu.Unlock()
}

func ctlSpin(us int64) {
	end := ctlNanotime() + us*1000
	for ctlNanotime() < end {
	}
}

// ctlWrite: SendStream.Write on stream 0 (application goroutine).
func ctlWrite(s *SendStream, p []byte) {
	if !ctl.on || len(p) != 32 {
		return
	}
	t := ctlNanotime()
	s.mutex.Lock()
	off := int64(s.writeOffset)
	s.mutex.Unlock()
	ctlLog(ctlEvent{K: ceWrite, T: t, A: off, B: int64(binary.BigEndian.Uint64(p[:8]))})
}

// ctlPopped: a stream-0 STREAM frame was popped for packing (connection loop).
func ctlPopped(offset, end protocol.ByteCount, retransmission bool) {
	if !ctl.on {
		return
	}
	ctl.nPack++
	if !ctl.pending {
		ctl.pending, ctl.first = true, ctl.nPack
	}
	ctl.last = ctl.nPack
	r := int64(0)
	if retransmission {
		r = 1
	}
	ctlLog(ctlEvent{K: cePack, T: ctlNanotime(), A: int64(offset), B: int64(end), C: r, R: ctl.nPack})
}

// ctlEnqueue: sendQueue.Send, before the entry enters the queue (connection loop).
func ctlEnqueue(h *sendQueue, p *packetBuffer) {
	if !ctl.pending {
		n := len(p.Data)
		ctl.ring[ctl.enqCount&(ctlRing-1)] = int32(n)
		ctl.enqCount++
		ctl.enqBytes += int64(n)
		return
	}
	ctlEnqueueTagged(h, p)
}

func ctlEnqueueTagged(h *sendQueue, p *packetBuffer) {
	n := int64(len(p.Data))
	// The channel's length, read once, is the snapshot: FIFO makes the queued
	// entries the most recently enqueued ones, whose sizes are in the ring.
	ahead := int64(len(h.queue))
	var bytes int64
	for i := uint64(1); i <= uint64(ahead); i++ {
		bytes += int64(ctl.ring[(ctl.enqCount-i)&(ctlRing-1)])
	}
	cum := ctl.enqBytes
	ctl.ring[ctl.enqCount&(ctlRing-1)] = int32(n)
	ctl.enqCount++
	ctl.enqBytes += n
	ctl.pending = false
	t := ctlNanotime()
	tagged := false
	for i := range ctl.slots {
		if ctl.slots[i].buf.Load() == nil {
			ctl.slots[i].ref.Store(ctl.last)
			ctl.slots[i].buf.Store(p)
			ctl.inflight.Add(1)
			tagged = true
			break
		}
	}
	if !tagged {
		ctl.exhausted.Add(1)
		return
	}
	if (ctlSynSenderHold > 0 && ctl.role == "send") || (ctlSynReceiverHold > 0 && ctl.role == "receive") {
		ctl.holdArmed.Store(true)
	}
	ctlLog(ctlEvent{K: ceEnqueue, T: t, A: ahead, B: bytes, C: int64(cap(h.queue)), D: n, E: ctl.first, F: cum, G: ahead,
		R: ctl.last})
}

func ctlSlotOf(b *packetBuffer) int {
	for i := range ctl.slots {
		if ctl.slots[i].buf.Load() == b {
			return i
		}
	}
	return -1
}

// ctlDequeued: the send worker took an entry from the queue.
func ctlDequeued(b *packetBuffer) {
	if ctl.inflight.Load() != 0 {
		ctlDequeuedTagged(b)
	}
}

func ctlDequeuedTagged(b *packetBuffer) {
	if i := ctlSlotOf(b); i >= 0 {
		if ctl.holdArmed.Load() {
			ctl.holdArmed.Store(false)
			if ctl.role == "send" {
				ctlSpin(ctlSynSenderHold)
			} else {
				ctlSpin(ctlSynReceiverHold)
			}
		}
		ctlLog(ctlEvent{K: ceDequeue, T: ctlNanotime(), R: ctl.slots[i].ref.Load()})
	}
}

// ctlBeforeWrite: the send worker is about to submit one entry (per-packet path).
func ctlBeforeWrite(b *packetBuffer) {
	if ctlSynSlowWorker > 0 && ctl.role == "send" {
		ctlSpin(ctlSynSlowWorker)
	}
	if ctl.inflight.Load() != 0 {
		ctlBeforeSubmitTagged(b)
		if ctlSynUnplaced > 0 && ctl.role == "send" && ctlSlotOf(b) >= 0 {
			ctlSpin(ctlSynUnplaced)
		}
	}
}

// ctlBeforeSubmit: an entry is about to be handed to the socket (batched path).
func ctlBeforeSubmit(b *packetBuffer) {
	if ctl.inflight.Load() != 0 {
		ctlBeforeSubmitTagged(b)
	}
}

func ctlBeforeSubmitTagged(b *packetBuffer) {
	if i := ctlSlotOf(b); i >= 0 {
		ctlLog(ctlEvent{K: ceSubmit, T: ctlNanotime(), R: ctl.slots[i].ref.Load()})
	}
}

// ctlReleased: an entry's buffer is about to be recycled (send worker, or the drain at Close).
func ctlReleased(b *packetBuffer) {
	if ctl.inflight.Load() != 0 {
		ctlUntag(b, ceDone)
	}
}

// ctlDiscarded: Send found the queue stopped and released the entry unsent.
func ctlDiscarded(b *packetBuffer) {
	if ctl.inflight.Load() != 0 {
		ctlUntag(b, ceDiscard)
	}
}

func ctlUntag(b *packetBuffer, kind int8) {
	if i := ctlSlotOf(b); i >= 0 {
		ctlLog(ctlEvent{K: kind, T: ctlNanotime(), R: ctl.slots[i].ref.Load()})
		ctl.slots[i].buf.Store(nil)
		ctl.inflight.Add(-1)
	}
}

// ctlArrived: a stream-0 STREAM frame is handled (connection loop); rcv is its packet's receive time.
func ctlArrived(offset, end protocol.ByteCount, rcv monotime.Time) {
	if !ctl.on {
		return
	}
	ctlLog(ctlEvent{K: ceArrive, T: int64(rcv) + ctl.monoOff, A: int64(offset), B: int64(end), C: ctlNanotime()})
}

// ctlRead: an application read on stream 0 returned n > 0 bytes.
func ctlRead(pos protocol.ByteCount, n int) {
	if !ctl.on || n <= 0 {
		return
	}
	ctlLog(ctlEvent{K: ceRead, T: ctlNanotime(), A: int64(pos), B: int64(n)})
}

// ctlLoop: the connection loop took a received packet (busy-loop synthetic case only).
func ctlLoop() {
	if ctlSynBusyLoop > 0 && ctl.role == "receive" {
		ctlSpin(ctlSynBusyLoop)
	}
}

// CtlNote records one reply at the fixture's sender: its id, whether its
// request was offered in the measured window, and the fixture's own stamps.
func CtlNote(id uint64, measured bool, sent, received int64) {
	if !ctl.on {
		return
	}
	m := int64(0)
	if measured {
		m = 1
	}
	ctlLog(ctlEvent{K: ceNote, T: ctlNanotime(), A: int64(id), B: m, C: sent, D: received})
}

// CtlReplyDelay is the fixture receiver's delayed-reply synthetic case.
func CtlReplyDelay() {
	if ctlSynReplyDelay > 0 && ctl.role == "receive" {
		ctlSpin(ctlSynReplyDelay)
	}
}

// CtlFlush writes the log; the fixture calls it once, before closing its transport.
func CtlFlush() {
	if !ctl.on {
		return
	}
	off1, half1 := ctlCalibrate()
	ctl.mu.Lock()
	defer ctl.mu.Unlock()
	rows := make([][10]int64, ctl.n)
	for i, e := range ctlEvents[:ctl.n] {
		rows[i] = [10]int64{int64(e.K), e.T, e.A, e.B, e.C, e.D, e.E, e.F, e.G, e.R}
	}
	b, err := json.Marshal(map[string]any{
		"role": ctl.role, "stream": int64(ctlStream), "kinds": ceNames,
		"fields": []string{"k", "t", "a", "b", "c", "d", "e", "f", "g", "r"},
		"events": rows, "dropped": ctl.dropped, "tags_exhausted": ctl.exhausted.Load(),
		"wall0_unix_ns": ctl.wall0, "mono0_ns": ctl.mono0, "flush_mono_ns": ctlNanotime(),
		"clock": map[string]int64{"offset_ns": ctl.monoOff, "half_width_ns": ctl.monoHalf, "flush_offset_ns": off1,
			"flush_half_width_ns": half1},
		"enqueued": map[string]int64{"entries": int64(ctl.enqCount), "bytes": ctl.enqBytes},
		"synthetic": map[string]int64{"sender_hold_us": ctlSynSenderHold, "receiver_hold_us": ctlSynReceiverHold,
			"slow_worker_us": ctlSynSlowWorker, "busy_loop_us": ctlSynBusyLoop, "reply_delay_us": ctlSynReplyDelay,
			"unplaced_us": ctlSynUnplaced},
	})
	if err == nil {
		_ = os.WriteFile(ctl.output, append(b, '\n'), 0o600)
	}
}
