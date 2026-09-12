package quic

import (
	"net"
	"sync"

	"github.com/quic-go/quic-go/internal/protocol"
)

type sender interface {
	Send(p *packetBuffer, gsoSize uint16, ecn protocol.ECN, metadata sendMetadata)
	SendProbe(*packetBuffer, net.Addr, packetInfo)
	Run() error
	WouldBlock() bool
	Available() <-chan struct{}
	Close()
}

// sendMetadata is captured by the connection loop, never read from it by the worker.
type sendMetadata struct {
	handshake      bool
	pathGeneration uint64
}

// handshakeSendFeedback retains the latest eligible failure without retaining buffers.
// Publishing and consuming the coalesced wakeup under the lock prevents lost wakeups.
type handshakeSendFeedback struct {
	mu         sync.Mutex
	generation uint64
	pending    bool
	wakeup     chan struct{}
}

func (f *handshakeSendFeedback) publish(generation uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.generation, f.pending = generation, true
	select {
	case f.wakeup <- struct{}{}:
	default:
	}
}

func (f *handshakeSendFeedback) take() (uint64, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	generation, pending := f.generation, f.pending
	f.pending = false
	select {
	case <-f.wakeup:
	default:
	}
	return generation, pending
}

type queueEntry struct {
	metadata sendMetadata
	buf      *packetBuffer
	gsoSize  uint16
	ecn      protocol.ECN
}

// batchSender is an optional sendConn capability: submit several packets that
// share an ECN marking (and use no GSO segmentation) as one batched send.
// sendBatch returns how many leading packets the kernel accepted
// (0 <= accepted <= len(bufs)); accepted packets are on the wire and must
// never be resent. A nil error with accepted < len(bufs) means the kernel
// observably declined the remainder (a short count, or an errno that the
// kernel only reports when nothing was sent); the worker then retries the
// first unaccepted entry through the per-packet path, where size errors and
// handshake MTU feedback attach to the correct entry. A non-nil error means
// kernel progress for the offered packets is unknowable (the syscall's error
// convention discards the count); the worker must fail the send path without
// resending anything, so no packet can be duplicated.
type batchSender interface {
	batchSendAvailable() bool
	sendBatch(bufs [][]byte, ecn protocol.ECN) (int, error)
}

type sendQueue struct {
	queue       chan queueEntry
	closeCalled chan struct{} // runStopped when Close() is called
	runStopped  chan struct{} // runStopped when the run loop returns
	available   chan struct{}
	conn        sendConn
	feedback    *handshakeSendFeedback

	// Scratch reused across batched sends; the run loop is the only user.
	batchScratch []queueEntry
	bufsScratch  [][]byte
}

var _ sender = &sendQueue{}

const sendQueueCapacity = 8

// maxSendBatch caps how many queued packets one batched send coalesces.
const maxSendBatch = sendQueueCapacity

func newSendQueue(conn sendConn, feedback *handshakeSendFeedback) sender {
	return &sendQueue{
		conn:        conn,
		feedback:    feedback,
		runStopped:  make(chan struct{}),
		closeCalled: make(chan struct{}),
		available:   make(chan struct{}, 1),
		queue:       make(chan queueEntry, sendQueueCapacity),
	}
}

// Send consumes a packet buffer, transferring it to the worker or releasing it if stopped.
// It's guaranteed to not block. The sole producer must finish sending before calling Close.
// Callers need to make sure that there's actually space in the send queue by calling WouldBlock.
// Otherwise Send will panic.
func (h *sendQueue) Send(p *packetBuffer, gsoSize uint16, ecn protocol.ECN, metadata sendMetadata) {
	select {
	case h.queue <- queueEntry{buf: p, gsoSize: gsoSize, ecn: ecn, metadata: metadata}:
		// clear available channel if we've reached capacity
		if len(h.queue) == sendQueueCapacity {
			select {
			case <-h.available:
			default:
			}
		}
	case <-h.runStopped:
		p.Release()
	default:
		panic("sendQueue.Send would have blocked")
	}
}

// SendProbe borrows storage for one synchronous best-effort write.
// The emission caller releases it after this method returns.
func (h *sendQueue) SendProbe(p *packetBuffer, addr net.Addr, info packetInfo) {
	h.conn.WriteTo(p.Data, addr, info)
}

func (h *sendQueue) WouldBlock() bool {
	return len(h.queue) == sendQueueCapacity
}

func (h *sendQueue) Available() <-chan struct{} {
	return h.available
}

func (h *sendQueue) Run() error {
	defer close(h.runStopped)
	var shouldClose bool
	for {
		if shouldClose && len(h.queue) == 0 {
			return nil
		}
		select {
		case <-h.closeCalled:
			h.closeCalled = nil // prevent this case from being selected again
			// make sure that all queued packets are actually sent out
			shouldClose = true
		case e := <-h.queue:
			if bs, ok := h.conn.(batchSender); ok && bs.batchSendAvailable() {
				if err := h.runBatched(e, bs); err != nil {
					return err
				}
				continue
			}
			if err := h.writeEntry(e); err != nil {
				e.buf.Release()
				return err
			}
			e.buf.Release()
			select {
			case h.available <- struct{}{}:
			default:
			}
		}
	}
}

// writeEntry sends one entry through the per-packet path with the existing
// per-entry error attribution. The additional size-error check enables:
// 1. Checking for "datagram too large" message from the kernel, as such,
// 2. Path MTU discovery, and
// 3. Eventual detection of loss PingFrame.
// The caller keeps ownership of the entry's buffer.
func (h *sendQueue) writeEntry(e queueEntry) error {
	if err := h.conn.Write(e.buf.Data, e.gsoSize, e.ecn); err != nil {
		if !isSendMsgSizeErr(err) {
			return err
		}
		if h.feedback != nil && e.metadata.handshake && e.gsoSize == 0 && e.buf.Len() > protocol.MinInitialPacketSize {
			h.feedback.publish(e.metadata.pathGeneration)
		}
	}
	return nil
}

// runBatched drains entries already queued behind e that share its
// (gsoSize, ecn) into one group and sends each group through
// sendBatchEntries. An entry that differs flushes the current group and
// starts a new one, preserving submission order.
func (h *sendQueue) runBatched(e queueEntry, bs batchSender) error {
	group := append(h.batchScratch[:0], e)
	defer func() { h.batchScratch = group[:0] }()
	for len(h.queue) > 0 && len(group) < maxSendBatch {
		next := <-h.queue
		if next.gsoSize != group[0].gsoSize || next.ecn != group[0].ecn {
			if err := h.sendBatchEntries(group, bs); err != nil {
				next.buf.Release()
				return err
			}
			group = append(group[:0], next)
			continue
		}
		group = append(group, next)
	}
	return h.sendBatchEntries(group, bs)
}

// sendBatchEntries owns every entry in the group: it releases all their
// buffers and signals availability before returning. Accepted entries are
// never resent; after every batched submission the first unaccepted entry is
// retried through the per-packet path so error attribution lands on the
// correct entry, and the remaining tail re-enters batching. Each iteration
// consumes at least one entry, so the loop terminates.
func (h *sendQueue) sendBatchEntries(group []queueEntry, bs batchSender) error {
	defer func() {
		for _, e := range group {
			e.buf.Release()
		}
		select {
		case h.available <- struct{}{}:
		default:
		}
	}()
	i := 0
	for i < len(group) {
		if remaining := group[i:]; len(remaining) >= 2 && remaining[0].gsoSize == 0 && bs.batchSendAvailable() {
			bufs := h.bufsScratch[:0]
			for _, e := range remaining {
				bufs = append(bufs, e.buf.Data)
			}
			accepted, err := bs.sendBatch(bufs, remaining[0].ecn)
			clear(bufs)
			h.bufsScratch = bufs[:0]
			if accepted < 0 || accepted > len(remaining) {
				// Defense in depth: the batch layer bounds and latches on
				// structural results; an out-of-bounds count is treated as
				// nothing accepted so no entry can be skipped or resent.
				accepted = 0
			}
			i += accepted
			if err != nil {
				// Progress for the unaccepted remainder is unknowable, so a
				// per-packet retry could resend a packet the kernel already
				// sent. Fail the send path instead — the same fatal handling
				// the per-packet path applies to this error class — and never
				// duplicate.
				return err
			}
			if accepted == len(remaining) {
				continue
			}
		}
		if err := h.writeEntry(group[i]); err != nil {
			return err
		}
		i++
	}
	return nil
}

func (h *sendQueue) Close() {
	close(h.closeCalled)
	// wait until the run loop returned
	<-h.runStopped
	// The producer has stopped and the worker can no longer own a queued entry.
	// A fatal write may have left entries, including a Send racing worker exit.
	for len(h.queue) > 0 {
		(<-h.queue).buf.Release()
	}
}
