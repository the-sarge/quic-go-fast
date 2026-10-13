package quic

import (
	"encoding/binary"
	"encoding/json"
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
//   - pack: a stream-0 STREAM frame is popped for a packet (offset range);
//   - enqueue: the queue entry carrying the last popped stream-0 frame enters
//     the send queue, with the entries and bytes already queued ahead of it and
//     the queue's capacity;
//   - dequeue and submit: the send worker takes that entry and its write
//     returns (stamped at release, before the buffer is recycled);
//   - arrive: a stream-0 STREAM frame is handled (its packet's receive time and
//     the handling time);
//   - read: an application read on stream 0 returns (the read position after);
//   - note: the fixture's sender records each reply's id and whether its
//     request was offered inside the measured window.
//
// Per-packet cost is a stream-ID compare in the frame paths, one pointer
// compare per dequeue and release, a plain byte counter in Send (connection
// loop only) and one uncontended atomic add per dequeue (send worker only).
// Clock reads and the event log run only for stream-0 events.
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
	ctlSynSlowWorker   = 0 // sender: the worker spins inside every submission
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
	ceArrive
	ceRead
	ceNote
)

var ceNames = []string{"write", "pack", "enqueue", "dequeue", "submit", "arrive", "read", "note"}

// ctlEvent fields by kind (T is CLOCK_MONOTONIC nanoseconds):
//
//	write:   A offset, B id
//	pack:    A offset, B end, C 1 for a retransmission, R pack sequence
//	enqueue: A entries ahead, B bytes ahead, C capacity, D entry bytes, E first and R last pack sequence in the entry
//	dequeue: R last pack sequence of the entry
//	submit:  R last pack sequence of the entry
//	arrive:  T receive time, A offset, B end, C handling time
//	read:    A read position after, B bytes
//	note:    A id, B measured (0/1), C fixture sent and D fixture received (wall-clock UnixNano)
type ctlEvent struct {
	K             int8
	T             int64
	A, B, C, D, E int64
	R             int64
}

const ctlMaxEvents = 1 << 14

type ctlSlot struct {
	buf atomic.Pointer[packetBuffer]
	ref atomic.Int64
}

var ctl struct {
	on        bool
	role      string
	output    string
	monoOff   int64 // runtime.nanotime minus monotime.Now
	wall0     int64
	mono0     int64
	mu        sync.Mutex
	events    []ctlEvent
	dropped   int
	nPack     int64 // connection loop: pack sequence
	pending   bool  // connection loop: a stream-0 frame was popped since the last enqueue
	first     int64
	last      int64
	slots     [2]ctlSlot
	unplaced  atomic.Bool
	holdArmed atomic.Bool
	_         [64]byte
	enqBytes  int64 // connection loop only
	_         [56]byte
	deqBytes  atomic.Int64 // send worker only (atomic for the connection loop's reads)
	_         [56]byte
}

func init() {
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
	ctl.events = make([]ctlEvent, 0, ctlMaxEvents)
	ctl.monoOff = ctlNanotime() - int64(monotime.Now())
	ctl.wall0, ctl.mono0 = time.Now().UnixNano(), ctlNanotime()
	ctl.on = true
}

func ctlLog(e ctlEvent) {
	ctl.mu.Lock()
	if len(ctl.events) < ctlMaxEvents {
		ctl.events = append(ctl.events, e)
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
	n := int64(len(p.Data))
	if !ctl.pending {
		ctl.enqBytes += n
		return
	}
	// Read the worker's counter only when tagging, so the connection loop never
	// pulls the worker's cache line on an ordinary packet.
	ahead := ctl.enqBytes - ctl.deqBytes.Load()
	ctl.enqBytes += n
	ctl.pending = false
	t := ctlNanotime()
	for i := range ctl.slots {
		if ctl.slots[i].buf.Load() == nil {
			ctl.slots[i].ref.Store(ctl.last)
			ctl.slots[i].buf.Store(p)
			break
		}
	}
	if (ctlSynSenderHold > 0 && ctl.role == "send") || (ctlSynReceiverHold > 0 && ctl.role == "receive") {
		ctl.holdArmed.Store(true)
	}
	ctlLog(ctlEvent{K: ceEnqueue, T: t, A: int64(len(h.queue)), B: ahead, C: int64(cap(h.queue)), D: n, E: ctl.first, R: ctl.last})
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
func ctlDequeued(e queueEntry) {
	ctl.deqBytes.Add(int64(len(e.buf.Data)))
	if i := ctlSlotOf(e.buf); i >= 0 {
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

// ctlBeforeWrite: the send worker is about to submit an entry (per-packet path).
func ctlBeforeWrite(e queueEntry) {
	if ctlSynSlowWorker > 0 && ctl.role == "send" {
		ctlSpin(ctlSynSlowWorker)
	}
	if ctlSynUnplaced > 0 && ctl.role == "send" {
		if i := ctlSlotOf(e.buf); i >= 0 {
			ctlLog(ctlEvent{K: ceSubmit, T: ctlNanotime(), R: ctl.slots[i].ref.Load()})
			ctl.slots[i].buf.Store(nil)
			ctlSpin(ctlSynUnplaced)
		}
	}
}

// ctlReleased: an entry's write returned; stamped before its buffer is recycled (send worker).
func ctlReleased(e queueEntry) {
	if i := ctlSlotOf(e.buf); i >= 0 {
		ctlLog(ctlEvent{K: ceSubmit, T: ctlNanotime(), R: ctl.slots[i].ref.Load()})
		ctl.slots[i].buf.Store(nil)
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
	ctl.mu.Lock()
	defer ctl.mu.Unlock()
	rows := make([][8]int64, len(ctl.events))
	for i, e := range ctl.events {
		rows[i] = [8]int64{int64(e.K), e.T, e.A, e.B, e.C, e.D, e.E, e.R}
	}
	b, err := json.Marshal(map[string]any{
		"role": ctl.role, "stream": int64(ctlStream), "kinds": ceNames, "fields": []string{"k", "t", "a", "b", "c", "d", "e", "r"},
		"events": rows, "dropped": ctl.dropped, "wall0_unix_ns": ctl.wall0, "mono0_ns": ctl.mono0, "flush_mono_ns": ctlNanotime(),
		"enqueued_bytes": ctl.enqBytes, "dequeued_bytes": ctl.deqBytes.Load(),
		"synthetic": map[string]int64{"sender_hold_us": ctlSynSenderHold, "receiver_hold_us": ctlSynReceiverHold,
			"slow_worker_us": ctlSynSlowWorker, "busy_loop_us": ctlSynBusyLoop, "reply_delay_us": ctlSynReplyDelay,
			"unplaced_us": ctlSynUnplaced},
	})
	if err == nil {
		_ = os.WriteFile(ctl.output, append(b, '\n'), 0o600)
	}
}
