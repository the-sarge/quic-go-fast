//go:build !darwin && !linux && !freebsd

package quic

import (
	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/protocol"
)

type nativeProtectionConstruction struct{}

func (*nativeProtectionConstruction) hasPlan() bool { return false }

func (*nativeProtectionConstruction) begin(*packetEmission, *packetBuffer)      {}
func (*nativeProtectionConstruction) registeredPacket()                         {}
func (*nativeProtectionConstruction) synchronous()                              {}
func (*nativeProtectionConstruction) discard()                                  {}
func (*nativeProtectionConstruction) observeSealer(handshake.ShortHeaderSealer) {}
func (*nativeProtectionConstruction) reserve(sealer, protocol.PacketNumber, int, int, protocol.PacketNumberLen, int) (bool, error) {
	return false, nil
}
func (*nativeProtectionConstruction) send(e *packetEmission, b *packetBuffer, gso uint16, ecn protocol.ECN, metadata sendMetadata) {
	e.queue.Send(b, gso, ecn, metadata)
}
