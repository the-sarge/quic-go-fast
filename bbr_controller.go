package quic

import (
	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

// enableBBR is private constructor plumbing for B1 tests. Public construction
// cannot select the partial policy; B6 owns activation of the complete sender.
func (e *packetEmission) enableBBR(now monotime.Time) *congestion.BBRSender {
	if e.bbr != nil {
		panic("BBR already installed")
	}
	size := e.policy.maxPacketSize()
	e.bbr = newBBRSendPolicy(1, size, now)
	b := ackhandler.EnableBBR(*e.recovery, size, func() protocol.ByteCount {
		e.bbr.credit.mu.Lock()
		defer e.bbr.credit.mu.Unlock()
		// The current construction reservation is capacity for the packet being
		// registered, not older queued work. Keep it charged for admission, but
		// exclude it when asking whether a delivery sampling epoch can begin.
		pending := e.bbr.credit.pending
		if e.reservation != nil {
			pending -= e.reservation.bytes
		}
		return pending
	})
	e.bbr.controller = b
	e.bbr.update(b.PacingRate(), size, now)
	return b
}
