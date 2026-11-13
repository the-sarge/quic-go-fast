package quic

import "github.com/quic-go/quic-go/internal/ackhandler"

// enableBBRECN binds the selected BBR controller to queue-drain and endpoint
// capability authority before packet registration.
func (e *packetEmission) enableBBRECN() {
	if e.bbr == nil {
		panic("BBR ECN requires local send ownership")
	}
	ackhandler.EnableBBRECN(*e.recovery, e.bbrECNAuthority(func() *bbrSendPolicy { return e.bbr }))
}

// bbrECNAuthority reports endpoint capability and, for the counter fence, the
// generation and old-generation drain of one policy's local credit.
func (e *packetEmission) bbrECNAuthority(policy func() *bbrSendPolicy) func(bool) (uint64, bool, bool) {
	return func(drain bool) (uint64, bool, bool) {
		capable := e.opportunityCapabilities().ECN
		if !drain {
			return 0, false, capable
		}
		credit := policy().credit
		credit.mu.Lock()
		generation, drained := credit.generation, credit.pending == credit.current
		credit.mu.Unlock()
		return generation, drained, capable
	}
}
