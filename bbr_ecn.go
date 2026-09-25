package quic

import "github.com/quic-go/quic-go/internal/ackhandler"

// enableBBRECN binds the selected BBR controller to queue-drain and endpoint
// capability authority before packet registration.
func (e *packetEmission) enableBBRECN() {
	if e.bbr == nil {
		panic("BBR ECN requires local send ownership")
	}
	ackhandler.EnableBBRECN(*e.recovery, func() (uint64, bool, bool) {
		credit := e.bbr.credit
		credit.mu.Lock()
		generation, drained := credit.generation, credit.pending == credit.current
		credit.mu.Unlock()
		return generation, drained, (*e.conn).capabilities().ECN
	})
}
