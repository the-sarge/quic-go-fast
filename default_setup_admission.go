package quic

import (
	"net"
	"net/netip"
	"sync"
)

const (
	defaultMaxPendingHandshakes      = 1024
	defaultMaxPendingHandshakesPerIP = 128
)

// All unconfigured listeners share this budget, including listeners on different
// transports. Source ports, IPv4-mapped addresses and IPv6 zones cannot multiply
// one IP's allowance. The source map contains only currently held reservations.
var defaultServerSetupBudget = serverSetupBudget{
	maxTotal: defaultMaxPendingHandshakes,
	maxPerIP: defaultMaxPendingHandshakesPerIP,
}

type serverSetupBudget struct {
	mu       sync.Mutex
	total    int
	byIP     map[netip.Addr]int
	maxTotal int
	maxPerIP int
}

// acquire reserves capacity atomically with the pressure check. Unvalidated
// sources need Retry once either budget is half full. A valid token proves
// reachability, but never bypasses the hard limits.
func (b *serverSetupBudget) acquire(remote net.Addr, validated bool) (_ *setupReservation, retry, ok bool) {
	ep, _ := packetEndpoint(remote)
	ip := ep.Addr().Unmap().WithZone("")
	b.mu.Lock()
	defer b.mu.Unlock()
	if !validated && (b.total >= b.maxTotal/2 || b.byIP[ip] >= b.maxPerIP/2) {
		return nil, true, false
	}
	if b.total >= b.maxTotal || b.byIP[ip] >= b.maxPerIP {
		return nil, false, false
	}
	if b.byIP == nil {
		b.byIP = make(map[netip.Addr]int)
	}
	b.total++
	b.byIP[ip]++
	return &setupReservation{
		handshakeOnly: true,
		release: func(bool) {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.total--
			b.byIP[ip]--
			if b.byIP[ip] == 0 {
				delete(b.byIP, ip)
			}
		},
	}, false, true
}

// handshakeFinished returns only the built-in budget. Caller reservations keep
// their explicit completion, acceptance, transfer and deadline contract.
func (r *setupReservation) handshakeFinished() {
	if r == nil || !r.handshakeOnly {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.settleLocked(false)
}
