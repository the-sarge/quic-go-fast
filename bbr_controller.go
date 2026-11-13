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
		return e.bbrPendingBytes(e.bbr)
	})
	if f, ok := e.packer.framer.(*framer); ok {
		f.enableDeliveryObservations()
	}
	e.bbr.controller = b
	e.bbr.update(b.PacingRate(), size, now)
	return b
}
