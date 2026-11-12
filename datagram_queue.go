package quic

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/utils/ringbuffer"
	"github.com/quic-go/quic-go/internal/wire"
)

const (
	maxDatagramSendQueueLen = 128
	maxDatagramRcvQueueLen  = 1024

	// One backing size covers common near-MTU payloads, including 1190-byte
	// sends. Keep exact-size allocations outside this range to avoid rounding
	// up small payloads or retaining jumbo buffers. This is a cache policy.
	minReusableDatagramPayloadSize = 1024
	maxReusableDatagramPayloadSize = 1280
)

type datagramQueue struct {
	closeOnce sync.Once
	sendMx    sync.Mutex
	sendQueue ringbuffer.RingBuffer[*wire.DatagramFrame]
	sent      chan struct{} // used to notify Add that a datagram was dequeued
	sendDrops atomic.Uint64 // queued frames no eligible path could carry
	// Idle backing storage for copies made at admission, bounded by the send
	// queue limit. sendMx protects both taking and returning these buffers.
	sendPayloadCache []*[maxReusableDatagramPayloadSize]byte

	rcvMx    sync.Mutex
	rcvQueue [][]byte
	rcvd     chan struct{} // used to notify Receive that a new datagram was received
	rcvDrops atomic.Uint64 // payloads refused because rcvQueue was full
	// rcvLen mirrors len(rcvQueue) for readers without rcvMx: every push, pop
	// and clear of rcvQueue stores it afterwards, still under rcvMx.
	rcvLen atomic.Int32

	closeErr error
	closed   chan struct{}

	hasData func()

	logger utils.Logger
}

func newDatagramQueue(hasData func(), logger utils.Logger) *datagramQueue {
	return &datagramQueue{
		hasData: hasData,
		rcvd:    make(chan struct{}, 1),
		sent:    make(chan struct{}, 1),
		closed:  make(chan struct{}),
		logger:  logger,
	}
}

// Add queues a new DATAGRAM frame for sending.
// Up to 128 DATAGRAM frames will be queued.
// Once that limit is reached, Add blocks until the queue size has reduced.
func (h *datagramQueue) Add(f *wire.DatagramFrame) error {
	return h.AddContext(context.Background(), f)
}

func (h *datagramQueue) AddContext(ctx context.Context, f *wire.DatagramFrame) error {
	return h.addContext(ctx, f, false)
}

func (h *datagramQueue) addContext(ctx context.Context, f *wire.DatagramFrame, copyPayload bool) error {
	h.sendMx.Lock()

	for {
		if err := ctx.Err(); err != nil {
			if h.sendQueue.Len() < maxDatagramSendQueueLen {
				select {
				case h.sent <- struct{}{}:
				default:
				}
			}
			h.sendMx.Unlock()
			return err
		}
		// Recheck closure before admitting a frame, including after a dequeue.
		select {
		case <-h.closed:
			h.sendMx.Unlock()
			return h.closeErr
		default:
		}
		if h.sendQueue.Len() < maxDatagramSendQueueLen {
			if copyPayload {
				h.copySendPayload(f)
			}
			h.sendQueue.PushBack(f)
			h.sendMx.Unlock()
			h.hasData()
			return nil
		}
		select {
		case <-h.sent: // drain the queue so we don't loop immediately
		default:
		}
		h.sendMx.Unlock()
		select {
		case <-ctx.Done():
		case <-h.closed:
			return h.closeErr
		case <-h.sent:
		}
		h.sendMx.Lock()
	}
}

// copySendPayload is called with sendMx held once admission has committed.
func (h *datagramQueue) copySendPayload(f *wire.DatagramFrame) {
	size := len(f.Data)
	// Debug logs the bytes after serialization. Keep its copy with the frame.
	if size < minReusableDatagramPayloadSize || size > maxReusableDatagramPayloadSize || h.logger.Debug() {
		data := make([]byte, size)
		copy(data, f.Data)
		f.Data = data
		return
	}
	var buf *[maxReusableDatagramPayloadSize]byte
	if n := len(h.sendPayloadCache); n > 0 {
		buf = h.sendPayloadCache[n-1]
		h.sendPayloadCache[n-1] = nil
		h.sendPayloadCache = h.sendPayloadCache[:n-1]
	} else {
		buf = new([maxReusableDatagramPayloadSize]byte)
	}
	copy(buf[:size], f.Data)
	f.SetReusableData(buf[:size])
}

func (h *datagramQueue) recycleSendPayload(f *wire.DatagramFrame) {
	data := f.DetachReusableData()
	if data == nil {
		return
	}
	h.sendMx.Lock()
	defer h.sendMx.Unlock()
	select {
	case <-h.closed:
		return
	default:
	}
	if len(h.sendPayloadCache) == maxDatagramSendQueueLen {
		return
	}
	if h.sendPayloadCache == nil {
		h.sendPayloadCache = make([]*[maxReusableDatagramPayloadSize]byte, 0, maxDatagramSendQueueLen)
	}
	h.sendPayloadCache = append(h.sendPayloadCache, (*[maxReusableDatagramPayloadSize]byte)(data[:cap(data)]))
}

// Peek gets the next DATAGRAM frame for sending.
// If actually sent out, Pop needs to be called before the next call to Peek.
func (h *datagramQueue) Peek() *wire.DatagramFrame {
	h.sendMx.Lock()
	defer h.sendMx.Unlock()
	if h.sendQueue.Empty() {
		return nil
	}
	return h.sendQueue.PeekFront()
}

func (h *datagramQueue) Pop() {
	h.sendMx.Lock()
	defer h.sendMx.Unlock()
	_ = h.sendQueue.PopFront()
	select {
	case h.sent <- struct{}{}:
	default:
	}
}

// dropUnfit discards the next frame, which no eligible path can carry. It is
// single-shot and awaits no future path growth.
func (h *datagramQueue) dropUnfit() {
	h.Pop()
	h.sendDrops.Add(1)
	if h.logger.Debug() {
		h.logger.Debugf("Dropping queued DATAGRAM frame: no eligible path can carry it")
	}
}

// HandleDatagramFrame handles a received DATAGRAM frame.
func (h *datagramQueue) HandleDatagramFrame(f *wire.DatagramFrame) {
	var queued bool
	h.rcvMx.Lock()
	if len(h.rcvQueue) < maxDatagramRcvQueueLen {
		data := make([]byte, len(f.Data))
		copy(data, f.Data)
		h.rcvQueue = append(h.rcvQueue, data)
		h.rcvLen.Store(int32(len(h.rcvQueue)))
		queued = true
		select {
		case h.rcvd <- struct{}{}:
		default:
		}
	}
	h.rcvMx.Unlock()
	if queued {
		return
	}
	h.rcvDrops.Add(1)
	if h.logger.Debug() {
		h.logger.Debugf("Discarding received DATAGRAM frame (%d bytes payload)", len(f.Data))
	}
}

// receiveDrops returns how many received payloads HandleDatagramFrame has
// refused because the queue was full. It is safe to call concurrently.
func (h *datagramQueue) receiveDrops() uint64 {
	return h.rcvDrops.Load()
}

// receiveQueueLen returns how many received payloads await Receive. It is
// safe to call concurrently and does not take rcvMx.
func (h *datagramQueue) receiveQueueLen() int {
	return int(h.rcvLen.Load())
}

// invalidateReceived discards every queued received payload.
func (h *datagramQueue) invalidateReceived() {
	h.rcvMx.Lock()
	clear(h.rcvQueue)
	h.rcvQueue = h.rcvQueue[:0]
	h.rcvLen.Store(0)
	h.rcvMx.Unlock()
}

// Receive gets a received DATAGRAM frame.
func (h *datagramQueue) Receive(ctx context.Context) ([]byte, error) {
	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	for {
		h.rcvMx.Lock()
		if len(h.rcvQueue) > 0 {
			data := h.rcvQueue[0]
			// Reslicing keeps the backing array; clear the slot so it no
			// longer pins the delivered payload.
			h.rcvQueue[0] = nil
			h.rcvQueue = h.rcvQueue[1:]
			h.rcvLen.Store(int32(len(h.rcvQueue)))
			h.rcvMx.Unlock()
			return data, nil
		}
		h.rcvMx.Unlock()
		select {
		case <-h.rcvd:
			continue
		case <-h.closed:
			return nil, h.closeErr
		case <-ctx.Done():
			return nil, context.Cause(ctx)
		}
	}
}

func (h *datagramQueue) CloseWithError(e error) {
	h.closeOnce.Do(func() {
		h.closeErr = e
		close(h.closed)
	})
}

// clearSendPayloadCache is called by the terminal owner after notifying waiters
// of closure. An admission already holding sendMx can finish before cleanup.
func (h *datagramQueue) clearSendPayloadCache() {
	h.sendMx.Lock()
	h.sendPayloadCache = nil
	h.sendMx.Unlock()
}

// sealAdmission fences new sends without removing already admitted frames.
func (h *datagramQueue) sealAdmission(reason error) {
	h.sendMx.Lock()
	h.CloseWithError(reason)
	h.sendPayloadCache = nil
	h.sendMx.Unlock()
}

// discardOversized preserves the order of surviving queued messages. A sender
// racing the size change can still admit using its earlier estimate; the packer
// retains its existing final fit check for that case.
func (h *datagramQueue) discardOversized(room protocol.ByteCount, version protocol.Version) {
	h.sendMx.Lock()
	defer h.sendMx.Unlock()
	dropped := false
	for range h.sendQueue.Len() {
		frame := h.sendQueue.PopFront()
		if frame.Length(version) > room {
			h.sendDrops.Add(1)
			dropped = true
		} else {
			h.sendQueue.PushBack(frame)
		}
	}
	if dropped {
		select {
		case h.sent <- struct{}{}:
		default:
		}
	}
}
