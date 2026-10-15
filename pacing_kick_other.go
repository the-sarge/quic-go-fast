//go:build !linux

package quic

import "github.com/quic-go/quic-go/internal/monotime"

// pacingKick exists only on Linux, where Go's idle timer wakes have
// millisecond granularity.
type pacingKick struct{}

func newPacingKick() *pacingKick { return nil }

func (k *pacingKick) arm(monotime.Time) {}

func (k *pacingKick) close() {}
