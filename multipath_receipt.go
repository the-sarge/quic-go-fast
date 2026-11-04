package quic

import (
	"net"
	"slices"
	"sync"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/wire"
)

// pathAbandonReceipt has one immutable terminal identity. The worker may publish
// only the first accepted submission time; all lifecycle policy stays with Conn.
type pathAbandonReceipt struct {
	mu       sync.Mutex
	accepted monotime.Time
	closed   bool
	wakeup   chan struct{}
}

func (r *pathAbandonReceipt) publish(now monotime.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.accepted != 0 {
		return
	}
	r.accepted = now
	select {
	case r.wakeup <- struct{}{}:
	default:
	}
}

func (r *pathAbandonReceipt) acceptedAt() monotime.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.accepted
}

func (r *pathAbandonReceipt) seal() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
}

func (p *pathCreditRegistry) newTerminal() *pathTerminal {
	return &pathTerminal{receipt: &pathAbandonReceipt{wakeup: p.conn.sendingScheduled}}
}

func (p *pathCreditRegistry) needsReceipt() bool {
	for _, terminal := range p.terminals {
		if !terminal.expired && terminal.receipt.acceptedAt() == 0 {
			return true
		}
	}
	return false
}

// captureTerminalReceipts runs only after the real packer selected its frames.
// Queuing or registering an abandonment never establishes socket acceptance.
func (e *packetEmission) captureTerminalReceipts(frames []ackhandler.Frame) {
	if e.pathCredit == nil {
		return
	}
	for _, frame := range frames {
		abandon, ok := frame.Frame.(*wire.PathAbandonFrame)
		if !ok {
			continue
		}
		terminal := e.pathCredit.terminals[abandon.PathID]
		if terminal == nil || terminal.expired {
			continue
		}
		if !slices.Contains(e.terminalReceipts, terminal.receipt) {
			e.terminalReceipts = append(e.terminalReceipts, terminal.receipt)
		}
	}
}

func (m sendMetadata) hasTerminalReceipt() bool {
	return len(m.terminalReceipts) != 0
}

func (p *pathCreditRegistry) close() {
	p.seal()
	p.endOperation(net.ErrClosed)
}

// seal ends the registry. The pending local operation waits for endOperation, because the
// connection classifies its terminal error only afterward.
func (p *pathCreditRegistry) seal() {
	p.closed = true
	p.failures.seal()
	for _, terminal := range p.terminals {
		terminal.receipt.seal()
	}
	p.closeAdmission()
	p.publishClosed()
}
