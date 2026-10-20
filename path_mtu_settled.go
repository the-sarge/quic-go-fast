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
// stays the same while the connection remains on its first path. Settled means
// that the search is over, not that the path will keep that size.
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
