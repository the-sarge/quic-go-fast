//go:build darwin || linux || freebsd

package quic

import (
	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
)

// completeAcceptedPackets attempts only a complete ordinary native member.
// Recovery validates every identity before consuming any. Coalesced handshake,
// MTU hooks and mixed results retain the ordered scalar completion path.
func (s *nativeOrdinarySender) completeAcceptedPackets(packets []ordinaryLocalPacket, datagrams, accepted int, resultErr error, returnedAt monotime.Time) bool {
	if resultErr != nil || accepted != datagrams || len(packets) != datagrams || len(packets) < 2 || len(packets) > maxGSOSegments {
		return false
	}
	var tokens [maxGSOSegments]ackhandler.LocalCompletionToken
	for i, packet := range packets {
		if packet.mtu != nil {
			return false
		}
		tokens[i] = packet.token
	}
	return ackhandler.TryCompleteLocalAcceptedPackets(s.recovery, tokens[:len(packets)], returnedAt)
}
