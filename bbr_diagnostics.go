package quic

import (
	"fmt"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/qlog"
)

// Trace at most one snapshot per second of send opportunities. Disabled tracing
// does not format snapshots or allocate per-packet diagnostics.
func (c *Conn) traceBBR(now monotime.Time) {
	p := c.emission.bbr
	if c.qlogger == nil || p == nil || p.controller == nil || (!p.lastTrace.IsZero() && now.Sub(p.lastTrace) < time.Second) {
		return
	}
	p.lastTrace = now
	p.credit.mu.Lock()
	pending, current := p.credit.pending, p.credit.current
	p.credit.mu.Unlock()
	recovery := c.sentPacketHandler.(interface{ BBRDiagnostics() string }).BBRDiagnostics()
	c.qlogger.RecordEvent(qlog.DebugEvent{EventName: "congestion_control", Message: fmt.Sprintf("selected=bbrv3 policy=bbrv3-draft06-classic-ecn-v1 %s pacing=%d quantum=%d pending=%d old_pending=%d %s", p.controller.Diagnostics(), p.rate, p.quantum, pending, pending-current, recovery)})
}
