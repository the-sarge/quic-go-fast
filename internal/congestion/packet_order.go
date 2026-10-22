package congestion

import "github.com/quic-go/quic-go/internal/protocol"

// PacketKey identifies a registered transmission in a normalized packet-number
// space. Recovery admits ACK identities before passing them to the controller;
// this key is not evidence that an arbitrary peer packet number was sent.
type PacketKey struct {
	Number protocol.PacketNumber
	space  uint8
}

func NewPacketKey(level protocol.EncryptionLevel, number protocol.PacketNumber) PacketKey {
	var space uint8
	switch level {
	case protocol.EncryptionInitial:
		space = 0
	case protocol.EncryptionHandshake:
		space = 1
	case protocol.Encryption0RTT, protocol.Encryption1RTT:
		space = 2
	default:
		panic("invalid congestion packet-number space")
	}
	return PacketKey{Number: number, space: space}
}

// packetPrefix describes every registration up to one instant. Packet numbers
// increase strictly within each space, including across Retry and key disposal.
// Presence distinguishes a space with packet zero from an absent space.
type packetPrefix struct {
	numbers [3]protocol.PacketNumber
	present uint8
}

func (p *packetPrefix) note(key PacketKey) {
	p.numbers[key.space] = key.Number
	p.present |= 1 << key.space
}

func (p packetPrefix) after(key PacketKey) bool {
	return p.present&(1<<key.space) == 0 || key.Number > p.numbers[key.space]
}
