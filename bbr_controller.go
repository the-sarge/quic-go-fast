package quic

import (
	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

// enableBBR installs the selected controller and bounded emission together,
// before any packet registration.
func (e *packetEmission) enableBBR(now monotime.Time, ceiling protocol.ByteCount) *congestion.BBRSender {
	if e.bbr != nil {
		panic("BBR already installed")
	}
	size := e.policy.maxPacketSize()
	e.bbr = newBBRSendPolicyWithCeiling(1, size, ceiling, now)
	b := ackhandler.EnableBBRWithPacketCeiling(*e.recovery, size, ceiling, func() protocol.ByteCount {
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
	if f, ok := e.packer.framer.(*framer); ok {
		f.enableDeliveryObservations()
	}
	e.bbr.controller = b
	e.bbr.update(b.PacingRate(), size, now)
	return b
}
