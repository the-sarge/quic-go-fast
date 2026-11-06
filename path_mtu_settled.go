package quic

// pathMTUSettled is the connection-owned signal behind PathMTUSettledV1. It
// closes once, for the connection's first path only. It is touched only on the
// connection's goroutine, except for the channel, which is set at construction.
type pathMTUSettled struct {
	ch     chan struct{}
	closed bool
}

func newPathMTUSettled() pathMTUSettled {
	return pathMTUSettled{ch: make(chan struct{})}
}

// settle closes the channel on its first call. Every later call, from any
// trigger, does nothing.
func (s *pathMTUSettled) settle() {
	if s.closed {
		return
	}
	s.closed = true
	close(s.ch)
}

// PathMTUSettledV1 returns a channel that is closed once path MTU discovery is
// over on the connection's first path. That happens at the first of these:
//   - the discovery search concludes. The DATAGRAM limit that SendDatagram
//     reports, through DatagramTooLargeError, already reflects the final size;
//   - discovery will not run: Config.DisablePathMTUDiscovery is set, the socket
//     cannot set the DF bit, or the peer's max_udp_payload_size leaves nothing to
//     search. Discovery starts at handshake confirmation, so these close the
//     channel then;
//   - the connection leaves its first path before the search concludes;
//   - the connection ends, for any reason and at any stage.
//
// After the channel closes because the search concluded, the DATAGRAM limit
// stays the same while the connection remains on its first path, except for
// recovery on a jumbo-active connection. A local size refusal lowers it to the
// tested stage-one size and closes PathMTUFallbackV1. Failed confirmation can
// lower it again to the initial packet size. Confirming that size restarts
// standard discovery, which can raise the limit again up to 1452 bytes. Settled
// means that the search is over, not that the path will keep that size.
//
// A path change resets the DATAGRAM limit to the initial packet size, and
// discovery then runs again on the new path. This includes a path change the
// server follows without application involvement, for example after NAT
// rebinding. The channel is not reopened or replaced, so a limit read after it
// closes can later shrink, and SendDatagram then refuses payloads above the new
// limit.
//
// Every call returns the same channel. It is safe to call concurrently and
// never blocks.
func (c *Conn) PathMTUSettledV1() <-chan struct{} {
	return c.pathMTUSettled.ch
}

// PathMTUInfoV1 reports the effective packet ceiling and the tested stage-one
// packet size, in bytes of UDP payload. The ceiling is the smaller of the local
// Transport's ceiling and the peer's advertised max_udp_payload_size. It is zero
// until the peer's transport parameters have been received and accepted; it does not promise
// that the path supports that size.
//
// The stage-one size is zero until the current path's standard-size search has
// finished. It is the largest size validated by that search, or the initial size
// if no larger probe succeeded. It remains available during stage two and resets
// to zero when a new path starts searching, or when recovery confirms the initial
// size and restarts the standard-size search on the same path. If discovery cannot
// run, it can stay zero even after PathMTUSettledV1 closes.
//
// This observation does not enable jumbo packets or change the sending limit.
// It is safe to call concurrently and never blocks.
func (c *Conn) PathMTUInfoV1() (effectiveCeiling, stageOneSize int64) {
	stageOne := c.pathMTUStageOneSize.Load()
	return int64(c.pathMTUEffectiveCeiling.Load()), int64(stageOne)
}
