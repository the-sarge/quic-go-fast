package quic

import (
	"errors"
	"fmt"

	"github.com/quic-go/quic-go/internal/ackhandler"
)

// ConfigureAckThresholdV1 makes connections created by this transport
// acknowledge Application Data every threshold ack-eliciting packets instead
// of every second one. The threshold must be between 2 and 32. Call it once,
// before any operation that initializes the transport. Transports without it
// keep ordinary behavior.
//
// With the threshold, an acknowledgment is queued at the earliest of:
// threshold ack-eliciting packets since the last acknowledgment; two or more,
// once the oldest of them was received 1 ms ago; max_ack_delay after a lone
// packet. Both delays run from the oldest unacknowledged ack-eliciting packet
// and later packets never restart them. A packet previously reported missing,
// a new gap and an ECN-CE mark still queue an acknowledgment at once.
//
// This is a local receiver rule: max_ack_delay and the transport parameters are
// unchanged, and the ACK frequency extension is not negotiated. The peer's
// congestion controller must tolerate fewer acknowledgments; enable it only
// for peers known to run one that does.
func (t *Transport) ConfigureAckThresholdV1(threshold int) error {
	if threshold < ackhandler.MinAckThreshold || threshold > ackhandler.MaxAckThreshold {
		return fmt.Errorf("quic: acknowledgment threshold %d outside [%d, %d]", threshold, ackhandler.MinAckThreshold, ackhandler.MaxAckThreshold)
	}
	t.packetIO.mutex.Lock()
	defer t.packetIO.mutex.Unlock()
	if t.packetIO.started {
		return errors.New("quic: acknowledgment threshold configuration after initialization")
	}
	if t.ackThreshold != 0 {
		return errors.New("quic: acknowledgment threshold already configured")
	}
	t.ackThreshold = threshold
	return nil
}

// installAckThreshold runs after construction and before the connection loop
// starts or receives a packet.
func (c *Conn) installAckThreshold(threshold int) {
	if threshold != 0 {
		c.receivedPacketHandler.SetAppDataAckThreshold(threshold)
	}
}
