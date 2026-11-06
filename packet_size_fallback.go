package quic

import (
	"sync"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

// packetSizeFeedback contains only an immutable local-refusal fact. The worker
// never reads connection state or decides whether a size change is appropriate.
// The latest path wins, so stale pending feedback cannot hide current feedback.
type packetSizeFeedback struct {
	mu         sync.Mutex
	generation uint64
	size       protocol.ByteCount
	pending    bool
	wakeup     chan struct{}
}

func (f *packetSizeFeedback) publish(generation uint64, size protocol.ByteCount) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.pending || generation > f.generation {
		f.generation, f.size = generation, size
	} else if generation == f.generation {
		f.size = max(f.size, size)
	}
	f.pending = true
	select {
	case f.wakeup <- struct{}{}:
	default:
	}
}

func (f *packetSizeFeedback) take() (uint64, protocol.ByteCount, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	generation, size, pending := f.generation, f.size, f.pending
	f.pending = false
	select {
	case <-f.wakeup:
	default:
	}
	return generation, size, pending
}

// mtuFallback is owned by the connection run loop. The channel alone is public.
// started stays set across path changes: a new connection is needed for jumbo.
type mtuFallback struct {
	started           monotime.Time
	generation        uint64
	size              protocol.ByteCount
	confirmed         bool
	active            bool
	firstPacketNumber protocol.PacketNumber
	lost              int
	probe             *packetSizeRecoveryProbe
	dropped           chan struct{}
}

// PathMTUFallbackV1 returns a channel that closes when a local size refusal or
// a confirmed far-side shrink causes this connection to fall back from jumbo to its tested stage-one size.
// It reports that first size drop, not confirmation or application delivery.
// Failed confirmation can lower the limit again to the initial packet size;
// confirming that size restarts standard discovery and can raise the limit up to
// 1452 bytes. These later transitions do not signal another channel closure. The
// negotiated ceiling reported by PathMTUInfoV1 stays unchanged. The connection
// will never search above 1452 again, including after a path change.
// The channel is nil when the Transport was not configured for jumbo packets.
// Every call returns the same read-only channel; calls are safe concurrently.
func (c *Conn) PathMTUFallbackV1() <-chan struct{} { return c.mtuFallback.dropped }

// applyPacketSizeFallback is the sole owner of a post-discovery size reduction.
// The same owner reduces to the initial size if transport confirmation fails.
func (c *Conn) applyPacketSizeFallback(now monotime.Time) {
	if c.packetSizeFeedback.wakeup == nil {
		return
	}
	generation, size, pending := c.packetSizeFeedback.take()
	if !pending || generation != c.pathGeneration || c.mtuFallback.started != 0 ||
		!c.handshakeConfirmed || c.effectivePacketCeiling() <= protocol.MaxPacketBufferSize ||
		c.mtuDiscoverer == nil || c.mtuDiscoverer.CurrentSize() <= protocol.MaxPacketBufferSize ||
		size <= protocol.MaxPacketBufferSize {
		return
	}
	c.startPacketSizeFallback(now)
}

func (c *Conn) startPacketSizeFallback(now monotime.Time) {
	fallback := c.mtuDiscoverer.StageOneSize()
	// No tested standard stage exists on this path yet.
	if fallback == 0 {
		return
	}
	c.mtuFallback.started, c.mtuFallback.generation = now, c.pathGeneration
	c.mtuFallback.active = true
	c.mtuWatch = packetSizeWatch{}
	c.reducePacketSizeForRecovery(now, fallback)
	c.pathMTUSettled.settle()
	close(c.mtuFallback.dropped)
}

func (c *Conn) reducePacketSizeForRecovery(now monotime.Time, fallback protocol.ByteCount) {
	c.mtuFallback.size = fallback
	// Recovery numbers, unlike clock stamps, distinguish sends in one clock tick.
	c.mtuFallback.firstPacketNumber, _ = c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
	c.mtuFallback.probe = nil
	// Reset the finder first, so loss callbacks from retired probes are stale.
	// A settled finder at the tested size prevents any further search on this path.
	c.mtuDiscoverer.Reset(fallback, fallback)
	ackhandler.ResetForMTUReduction(c.sentPacketHandler, now, fallback, c.confirmPacketSizeFallback)
	c.maxPayloadSizeEstimate.Store(uint32(estimateMaxPayloadSize(fallback)))
	if c.datagramQueue != nil {
		if room, ok := c.emission.packer.datagramRoom(nil, fallback); ok {
			c.datagramQueue.discardOversized(room, c.version)
		}
	}
	c.emission.resetLocalPath(c.pathGeneration, now)
	c.pacingDeadline = deadlineSendImmediately
	c.scheduleSending()
}

func (c *Conn) confirmPacketSizeFallback(context ackhandler.PacketContext, size protocol.ByteCount, number protocol.PacketNumber) {
	c.notePacketSizeAck(context, size, number)
	f := &c.mtuFallback
	if !f.active || context.PathID != 0 || context.TupleGeneration != f.generation ||
		c.pathGeneration != f.generation || number < f.firstPacketNumber {
		return
	}
	if size == f.size {
		ackhandler.RetireMTURecoveryProbes(c.sentPacketHandler, monotime.Now(), false)
		f.confirmed, f.active, f.probe = true, false, nil
		if size == c.effectiveInitialPacketSize {
			c.mtuDiscoverer.Reset(size, c.discoveryPacketCeiling())
			c.startMTUDiscovery(monotime.Now())
		}
		return
	}
	if p := f.probe; p != nil && p.sent != 0 && size <= protocol.MaxPacketBufferSize && number > p.number {
		p.laterSmallAck = true
	}
}
