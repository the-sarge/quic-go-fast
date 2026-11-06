package quic

import (
	"errors"
	"fmt"

	"github.com/quic-go/quic-go/internal/protocol"
)

// packetCeiling is fixed before a Transport creates readers or connections.
// Zero preserves the standard endpoint ceiling.
type packetCeiling protocol.ByteCount

func (c packetCeiling) size() protocol.ByteCount {
	if c == 0 {
		return protocol.MaxPacketBufferSize
	}
	return protocol.ByteCount(c)
}

// ConfigurePacketCeilingV1 sets the largest UDP payload this Transport advertises
// and searches for. n must be between 1452 and 8972 bytes, inclusive. Call it
// before any operation that initializes the Transport. Unconfigured Transports
// retain the 1452-byte ceiling. The peer and path can impose a smaller limit.
//
// This experimental opt-in is intended for operator-managed MTU 9000 links. It
// includes path-shrink recovery but does not establish real-link qualification
// or a recovery-time guarantee. It cannot be combined with
// [Transport.ConfigureMultipathIOV1], in either configuration order.
func (t *Transport) ConfigurePacketCeilingV1(n int) error {
	t.packetIO.mutex.Lock()
	defer t.packetIO.mutex.Unlock()
	if t.packetIO.started {
		return errors.New("quic: packet ceiling configuration after initialization")
	}
	if n < 1452 || n > 8972 {
		return errors.New("quic: packet ceiling outside [1452, 8972]")
	}
	if t.nativeIOConn != nil {
		return fmt.Errorf("%w: packet ceiling with native packet I/O", ErrMultipathUnsupportedV1)
	}
	t.packetCeiling = packetCeiling(n)
	return nil
}
