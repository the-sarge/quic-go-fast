package quic

import (
	"net"
	"sync/atomic"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

// endpointWriteState belongs to one tuple. It lives outside the native route's build
// constraint because each route holds one by value.
type endpointWriteState struct {
	wroteFirstPacket bool //nolint:unused // only the native route writes it
	gsoDisabled      atomic.Bool
}

// pathRoute is one selected nonzero path's sending context. Each member owns its
// route, so selection allocates nothing. A nil route is path zero, whose packer,
// recovery and sender keep their ordinary branch.
type pathRoute struct {
	context    ackhandler.PacketContext
	connID     protocol.ConnectionID
	recovery   ackhandler.SentPacketHandler
	addr       net.Addr
	info       packetInfo
	transport  *Transport // nil is the connection's own transport
	epoch      *responseEpoch
	writeState endpointWriteState
}

// pathView is what the scheduler may know about one eligible path. It holds no
// pointer to recovery or to any payload.
type pathView struct {
	id      protocol.PathID
	probing bool // a PTO is pending on this path
}

// selectPath takes views in path ID order, and path zero is always first. A
// pending PTO goes first so the probe elicits its ACK on the path that needs one,
// then the primary, then the lowest eligible ID.
func selectPath(views []pathView, primary protocol.PathID) protocol.PathID {
	for _, view := range views {
		if view.probing {
			return view.id
		}
	}
	for _, view := range views {
		if view.id == primary {
			return view.id
		}
	}
	return views[0].id
}

// selectRoute binds this opportunity before any capacity read or destructive
// packing, and tells the sender, which cancels a reservation made for another route.
func (e *packetEmission) selectRoute(now monotime.Time) {
	if e.pathCredit == nil || e.local == nil {
		return
	}
	if route := e.pathCredit.selectRoute(now); route != e.route {
		e.route = route
		e.local.useRoute(route)
	}
}

// selected is the recovery that owns the current opportunity.
func (e *packetEmission) selected() ackhandler.SentPacketHandler {
	if e.route != nil {
		return e.route.recovery
	}
	return *e.recovery
}

// packetSize stays at the admitted minimum on a nonzero path until R11 gives it
// PMTU state.
func (e *packetEmission) packetSize() protocol.ByteCount {
	if e.route != nil {
		return protocol.MinInitialPacketSize
	}
	return e.policy.maxPacketSize()
}

// pathProbeOpportunity gives admission its own turn before dispatch, so sustained
// SendAck on the selected path can't hold a challenge back until its deadline.
func (e *packetEmission) pathProbeOpportunity(now monotime.Time) (bool, error) {
	if !e.pathCredit.hasAdmission() || (*e.recovery).SendMode(now) == ackhandler.SendNone {
		return false, nil
	}
	intent := e.pathCredit.nextProbe(now)
	if intent == nil {
		return false, nil
	}
	return e.admissionProbe(intent, now)
}
