package quic

import "sync/atomic"

// A receiveCoalescingFallbackReader is a socket reader whose guard can turn
// receive coalescing off for the rest of the socket's life: the Windows URO
// reader (#860). The count belongs to the socket's reader.
type receiveCoalescingFallbackReader interface {
	receiveCoalescingFallbackCount() *atomic.Uint64
}

// ReceiveCoalescingFallbacksV1 returns how many times receive coalescing was
// turned off on the socket this transport reads from, because a read longer
// than the largest packet arrived without the record of its datagrams' size.
// Some Windows hosts deliver coalesced reads that way; the read is dropped
// and the socket reads without coalescing from then on. The count is at most
// one per socket and is zero where the socket does not coalesce receives on
// Windows. Transports that share a managed endpoint report the endpoint's
// socket. It never decreases and is safe to call concurrently. Code compiled
// against upstream quic-go finds it by interface assertion.
func (t *Transport) ReceiveCoalescingFallbacksV1() uint64 {
	if e := t.managedPacketEndpoint(); e != nil {
		e.mutex.Lock()
		reader, ok := e.receiver.(receiveCoalescingFallbackReader)
		e.mutex.Unlock()
		if !ok {
			return 0
		}
		return reader.receiveCoalescingFallbackCount().Load()
	}
	if n := t.receiveCoalescingFallbacks.Load(); n != nil {
		return n.Load()
	}
	return 0
}

// managedPacketEndpoint returns the managed endpoint whose reader receives
// this transport's packets, if any.
func (t *Transport) managedPacketEndpoint() *managedPacketEndpoint {
	t.packetIO.mutex.Lock()
	external := t.packetIO.external
	t.packetIO.mutex.Unlock()
	if external != nil && external.managedLease != nil {
		return external.managedLease.endpoint
	}
	if c, ok := t.Conn.(*managedPacketConn); ok && c != nil {
		return c.endpoint
	}
	return nil
}
