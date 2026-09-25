package congestion

import "fmt"

// Diagnostics is a connection-owned snapshot for optional tracing, not a
// controller interface or synchronization boundary.
func (b *BBRSender) Diagnostics() string {
	phase := [...]string{"Startup", "Drain", "Down", "Cruise", "Refill", "Up", "ProbeRTT"}[b.phase]
	return fmt.Sprintf("phase=%s window=%d ce_active=%t ce_rate=%d ce_flight=%d recovery=%t", phase, b.window, b.ce.active, b.ce.rate, b.ce.flight, b.InRecovery())
}
