package quic

import (
	"net"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

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
	mtu        *mtuFinder // this path's own limit; set when the path goes live
}

// pathView is what the scheduler may know about one eligible path. It holds no
// pointer to recovery or to any payload.
type pathView struct {
	id      protocol.PathID
	probing bool               // a PTO is pending on this path
	fits    bool               // a fresh packet can carry the next DATAGRAM, or none is queued
	ready   bool               // not congestion-limited, so that DATAGRAM could leave here now
	room    protocol.ByteCount // frame length a fresh packet here holds, once read
}

// selectPath takes views in path ID order, and path zero is always first. A
// pending PTO goes first so the probe elicits its ACK on the path that needs one,
// then the primary, then the lowest eligible ID. It steers only when the next
// DATAGRAM doesn't fit that choice. A fitting path that can't send yet doesn't
// stall the choice, whose traffic continues while the DATAGRAM stays queued.
func selectPath(views []pathView, primary protocol.PathID) (_ protocol.PathID, steered bool) {
	for _, view := range views {
		if view.probing {
			return view.id, false
		}
	}
	chosen := views[0]
	for _, view := range views {
		if view.id == primary {
			chosen = view
			break
		}
	}
	if !chosen.fits {
		for _, view := range views {
			if view.fits && view.ready {
				return view.id, true
			}
		}
	}
	return chosen.id, false
}

// selectRoute binds this opportunity before any capacity read or destructive
// packing, and tells the sender, which cancels a reservation made for another route.
func (e *packetEmission) selectRoute(now monotime.Time) {
	if e.pathCredit == nil || e.local == nil {
		return
	}
	route, steered := e.pathCredit.selectRoute(now)
	e.steered = steered
	if route != e.route {
		e.route = route
		e.local.useRoute(route)
	}
}

// steeredDatagram sends the next DATAGRAM alone, so no stream data follows it off
// the primary, and returns to selection for whatever is next.
func (e *packetEmission) steeredDatagram(now monotime.Time) emissionResult {
	if e.queue.WouldBlock() {
		return emissionResult{stop: emissionQueueFull, available: e.queue.Available()}
	}
	buf := getPacketBuffer()
	ecn := e.selected().ECNMode(true)
	if _, _, err := e.appendPacket(buf, e.packetSize(), true, ecn, now); err != nil {
		buf.Release()
		if err == errNothingToPack {
			return emissionResult{}
		}
		return emissionResult{err: err}
	}
	e.handoff(buf, 0, ecn, sendMetadata{})
	return emissionResult{progress: true, stop: emissionProbeSent}
}

// selected is the recovery that owns the current opportunity.
func (e *packetEmission) selected() ackhandler.SentPacketHandler {
	if e.route != nil {
		return e.route.recovery
	}
	return *e.recovery
}

// packetSize is the current path's own limit.
func (e *packetEmission) packetSize() protocol.ByteCount {
	if e.route != nil {
		return e.route.mtu.CurrentSize()
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
