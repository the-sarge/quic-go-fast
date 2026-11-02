package ackhandler

import (
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
)

// ackTargets is an immutable snapshot of the receive histories described by a
// carrier. Two target spaces are sufficient for the bounded multipath profile.
// These are pruning facts, not frame or application delivery ownership.
type ackTarget struct {
	path  protocol.PathID
	below protocol.PacketNumber
}
type ackTargets struct {
	targets [2]ackTarget
	count   int
}

func snapshotAckTargets(largest protocol.PacketNumber, frames []Frame, level protocol.EncryptionLevel) ackTargets {
	var targets ackTargets
	if level != protocol.Encryption1RTT {
		return targets
	}
	if largest != protocol.InvalidPacketNumber {
		targets.add(0, largest+1)
	}
	for _, f := range frames {
		if ack, ok := f.Frame.(*wire.AckFrame); ok {
			id := protocol.PathID(0)
			if ack.HasPathID {
				id = ack.PathID
			}
			targets.add(id, ack.LargestAcked()+1)
		}
	}
	return targets
}

func (a *ackTargets) add(id protocol.PathID, below protocol.PacketNumber) {
	for i := 0; i < a.count; i++ {
		if a.targets[i].path == id {
			a.targets[i].below = max(a.targets[i].below, below)
			return
		}
	}
	if a.count == len(a.targets) {
		panic("ackhandler: ACK carrier exceeds target limit")
	}
	a.targets[a.count] = ackTarget{path: id, below: below}
	a.count++
}

func (a ackTargets) apply(ignore func(protocol.PathID, protocol.PacketNumber)) {
	if ignore == nil {
		return
	}
	for i := 0; i < a.count; i++ {
		target := a.targets[i]
		ignore(target.path, target.below)
	}
}
