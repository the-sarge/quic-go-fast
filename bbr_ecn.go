package quic

import "github.com/quic-go/quic-go/internal/ackhandler"

// enableBBRECN is constructor plumbing for the private complete transport path.
// No public constructor activates it before the full controller is available.
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
