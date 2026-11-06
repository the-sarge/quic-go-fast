package ackhandler

import (
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

// ResetForMTUReduction preserves the path RTT while retiring the old flight and
// restarting congestion control at the smaller size. Recovery retains ownership
// of registration order and lifetimes. The synchronous observer reports ACK facts
// only; the connection run loop owns the confirmation verdict.
func ResetForMTUReduction(handler SentPacketHandler, now monotime.Time, size protocol.ByteCount, acked func(PacketContext, protocol.ByteCount, monotime.Time)) {
	h := handler.(*sentPacketHandler)
	h.mtuAcked = acked
	h.resetCongestionCapture(true)
	h.resetForPacketSize(now, size)
}
