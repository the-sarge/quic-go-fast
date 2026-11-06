package quic

import "github.com/quic-go/quic-go/internal/protocol"

// packetCeiling is fixed before a Transport creates readers or connections.
// Zero preserves the standard endpoint ceiling.
type packetCeiling protocol.ByteCount

func (c packetCeiling) size() protocol.ByteCount {
	if c == 0 {
		return protocol.MaxPacketBufferSize
	}
	return protocol.ByteCount(c)
}
