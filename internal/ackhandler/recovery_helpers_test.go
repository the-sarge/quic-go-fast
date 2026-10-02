package ackhandler

import (
	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
)

// Helpers shared by the differential and work-scaling tests. They depend only
// on API that the frozen component also has.

func ackFrame(ranges ...wire.AckRange) *wire.AckFrame {
	return &wire.AckFrame{AckRanges: ranges}
}

func ackRange(lo, hi protocol.PacketNumber) wire.AckRange {
	return wire.AckRange{Smallest: lo, Largest: hi}
}

type twinSink struct{ sent []congestion.SendEvent }

func (s *twinSink) Sent(e congestion.SendEvent)       { s.sent = append(s.sent, e) }
func (s *twinSink) Feedback(congestion.FeedbackEvent) {}
