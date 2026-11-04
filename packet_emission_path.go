package quic

import (
	"net"
	"slices"

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
	suspect bool               // only its pending PTO keeps it in view; it carries no DATAGRAM
}

// selectPath takes views in path ID order. A pending PTO goes first so the probe
// elicits its ACK on the path that needs one, then the primary. With no primary in
// view, ordinary work is held. When the next DATAGRAM doesn't fit the choice, it also
// names a fitting path to try. A congestion-limited one isn't named, so the choice's
// traffic continues while the DATAGRAM stays queued until an ACK. A paced one is
// named, and its deadline wakes the connection.
func selectPath(views []pathView, primary protocol.PathID) (chosen, steerTo protocol.PathID, steering, held bool) {
	for _, view := range views {
		if view.probing {
			return view.id, 0, false, false
		}
	}
	index := slices.IndexFunc(views, func(view pathView) bool { return view.id == primary })
	if index < 0 {
		return 0, 0, false, true
	}
	first := views[index]
	if !first.fits {
		for _, view := range views {
			if view.fits && view.ready {
				return first.id, view.id, true, false
			}
		}
	}
	return first.id, 0, false, false
}

// selectRoute binds this opportunity before any capacity read or destructive
// packing, and tells the sender, which cancels a reservation made for another route.
func (e *packetEmission) selectRoute(now monotime.Time) {
	if e.pathCredit == nil || e.local == nil {
		return
	}
	route, steerTo, steering, held := e.pathCredit.selectRoute(now)
	e.held = held
	// Steering also needs endpoint capacity, so a saturated fitting endpoint holds
	// the DATAGRAM and not the chosen route's traffic. Its completions wake the loop.
	if steering {
		e.useRoute(steerTo)
		if !e.queue.WouldBlock() {
			e.steered = true
			return
		}
	}
	e.steered = false
	e.useRoute(route)
}

func (e *packetEmission) useRoute(route *pathRoute) {
	if route != e.route {
		e.route = route
		e.local.useRoute(route)
	}
}

// heldOpportunity keeps ACKs flowing while no path may carry ordinary work. Probes
// have already had their turn, and a path's recovery or revalidation wakes the
// connection.
func (e *packetEmission) heldOpportunity(now monotime.Time, confirmed bool) emissionResult {
	if e.queue.WouldBlock() {
		return emissionResult{stop: emissionQueueFull, available: e.queue.Available()}
	}
	result := emissionResult{stop: emissionHardBlocked, blocked: blockModeHardBlocked}
	if e.selected().SendMode(now) != ackhandler.SendNone {
		result.progress, result.err = e.maybeSendAckOnlyPacket(now, confirmed)
	}
	return result
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
