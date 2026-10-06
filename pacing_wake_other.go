//go:build !linux

package quic

import "github.com/quic-go/quic-go/internal/monotime"

// pacingWaker exists only on Linux, where Go's timer wakes are coarse; other
// platforms keep the pacing deadline on the connection's Go timer.
type pacingWaker struct{ C chan struct{} }

func newPacingWaker() *pacingWaker { return nil }

func (w *pacingWaker) arm(monotime.Time) {}

func (w *pacingWaker) close() {}
