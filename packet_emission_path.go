package quic

import (
	"net"
	"slices"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
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
	ready   bool               // a whole packet could leave here now or once paced
	room    protocol.ByteCount // frame length a fresh packet here holds, once read

	// What only aggregate reads.
	backup    bool
	sendAt    monotime.Time      // the next send pacing allows; zero or past is now
	queued    protocol.ByteCount // bytes held locally for this path, plus the next packet
	rate      uint64             // conservative delivery rate in bytes per second; zero is no valid sample
	rtt       time.Duration      // smoothed path-local RTT; zero is none measured
	scheduled uint64             // the round-robin ledger
}

// selectPath takes views in path ID order. A pending PTO goes first so the probe
// elicits its ACK on the path that needs one, then the primary. With no primary in
// view, ordinary work is held. When the next DATAGRAM doesn't fit the choice, it also
// names a fitting path to try. A congestion-limited one isn't named, so the choice's
// traffic continues while the DATAGRAM stays queued until an ACK. A paced one is
// named, and its deadline wakes the connection.
func selectPath(views []pathView, primary protocol.PathID) (chosen, steerTo protocol.PathID, steering, held bool) {
	if index := slices.IndexFunc(views, func(view pathView) bool { return view.probing }); index >= 0 {
		return views[index].id, 0, false, false
	}
	index := slices.IndexFunc(views, func(view pathView) bool { return view.id == primary })
	if index < 0 {
		return 0, 0, false, true
	}
	steerTo, steering = steerFor(views[index], views)
	return views[index].id, steerTo, steering, false
}

// steerFor is every policy's per-message DATAGRAM rule: when the next DATAGRAM
// doesn't fit the choice, name the first path it fits that could send it now.
func steerFor(choice pathView, views []pathView) (protocol.PathID, bool) {
	if choice.fits {
		return 0, false
	}
	if index := slices.IndexFunc(views, func(view pathView) bool { return view.fits && view.ready }); index >= 0 {
		return views[index].id, true
	}
	return 0, false
}

// selectRoute binds this opportunity before any capacity read or destructive
// packing. The sender changes only its local builder; frozen staged requests keep
// their original route and exact endpoint demand.
func (e *packetEmission) selectRoute(now monotime.Time) {
	if e.pathCredit == nil || e.local == nil {
		return
	}
	route, steerTo, steering, held := e.pathCredit.selectRoute(now)
	e.held = held
	// Local capacity precedes extraction. Once constructed, a fitting DATAGRAM
	// stays on that route even when exact endpoint admission refuses it.
	if steering {
		// A route selection has left gives up its place. The attempt leaves the chosen
		// route's alone, and one already bound to steerTo keeps its local builder.
		if e.route != steerTo {
			e.useRoute(route)
		}
		if e.local.steer(steerTo) {
			e.route, e.steered = steerTo, true
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

// heldOpportunity keeps ACKs flowing while no path may carry ordinary work, so the
// loop keeps their alarms, as it does when congestion-limited. Admission probes have
// already had their turn, and a path's recovery wakes the connection.
func (e *packetEmission) heldOpportunity(now monotime.Time, confirmed bool) emissionResult {
	if e.route == nil && !e.pathCredit.zeroCarries() {
		// A nil route is path zero, whose keys no longer seal, whose epoch is revoked, or
		// whose endpoint has failed.
		return emissionResult{stop: emissionHardBlocked, blocked: blockModeHardBlocked}
	}
	if e.queue.WouldBlock() {
		return emissionResult{stop: emissionQueueFull, available: e.queue.Available()}
	}
	if e.selected().SendMode(now) == ackhandler.SendNone {
		return emissionResult{stop: emissionHardBlocked, blocked: blockModeHardBlocked}
	}
	result := emissionResult{stop: emissionCongestionLimited, blocked: blockModeCongestionLimited}
	result.progress, result.err = e.maybeSendAckOnlyPacket(now, confirmed)
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
	if _, err := e.appendPacket(buf, e.packetSize(), true, ecn, now); err != nil {
		buf.Release()
		if err == errNothingToPack {
			return emissionResult{}
		}
		return emissionResult{err: err}
	}
	e.handoff(buf, 0, ecn, sendMetadata{}, nil)
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
func (e *packetEmission) packetSize() protocol.ByteCount { return e.packetSizeOn(e.route) }

func (e *packetEmission) packetSizeOn(route *pathRoute) protocol.ByteCount {
	if route != nil {
		return route.mtu.CurrentSize()
	}
	return e.policy.maxPacketSize()
}

// pathView reads one path's sending state for selection and consumes nothing. A
// path is ready when the opportunity it would run gets its local reservation:
// native construction capacity on its route and, for BBR, the local credit the
// opportunity's own plan reserves. Reno sends into any open window. A BBR path's
// pacing is projected to its controller's current rate, and a paced path is
// judged by the work it would do once pacing allows. Readiness never runs the
// controller's send-time preparation, so a window that moves there stops the
// opportunity on congestion, which is the network's verdict, not a local refusal.
func (e *packetEmission) pathView(id protocol.PathID, route *pathRoute, recovery ackhandler.SentPacketHandler, now monotime.Time) pathView {
	if e.bbr == nil {
		mode := recovery.SendMode(now)
		view := pathViewFor(id, mode)
		view.ready = view.ready && e.canConstruct(route)
		if mode == ackhandler.SendPacingLimited {
			view.sendAt = recovery.TimeUntilSend()
		}
		return view
	}
	size := e.packetSizeOn(route)
	p := e.policyOn(route)
	projected := p.projected(size, now)
	mode, allowance := recovery.(boundedRecovery).SendAllowance(now)
	mtu, confirmed := e.dueMTU(route, now), e.pathCredit.conn.handshakeConfirmed
	view := pathView{id: id, fits: true, probing: mode == ackhandler.SendPTOAppData, sendAt: now}
	plan := e.planBounded(route, &projected, mode, allowance, size, mtu, false, confirmed, now)
	if plan.step == stepPaced || plan.step == stepMTUPaced {
		view.sendAt = plan.deadline
		plan = e.planBounded(route, &projected, mode, allowance, size, mtu, false, confirmed, plan.deadline)
	}
	view.ready = (plan.step == stepSend || plan.step == stepMTUProbe) &&
		p.credit.admits(plan.request, projected.creditLimit(), plan.ordinary, plan.isolated) && e.canConstruct(route)
	view.queued = e.bbrPendingBytes(p) + size
	view.rate = p.controller.DeliveryRateEstimate()
	return view
}

// dueMTU is the route's path MTU finder when the opportunity on it would probe.
func (e *packetEmission) dueMTU(route *pathRoute, now monotime.Time) *mtuFinder {
	finder := e.pathCredit.conn.mtuDiscoverer
	if route != nil {
		finder = route.mtu
	}
	if !e.pathCredit.conn.handshakeConfirmed || finder == nil || !finder.ShouldSendProbe(now) {
		return nil
	}
	return finder
}

// canConstruct reports, without reserving, whether the route's native endpoint
// would give this connection construction capacity. The ordinary send queue is
// shared by every route, so it never distinguishes one.
func (e *packetEmission) canConstruct(route *pathRoute) bool {
	return e.local == nil || e.local.canConstruct(route)
}

// announcePath sends the non-probing packet a cutover owes on its new tuple. It is an
// ordinary registered packet, so recovery tracks it and an acknowledgment discharges it.
func (e *packetEmission) announcePath(now monotime.Time) (bool, error) {
	m := e.pathCredit.nextAnnouncement(now)
	if m == nil {
		return false, nil
	}
	selected := e.route
	e.useRoute(e.pathCredit.routeOf(m.id))
	if e.queue.WouldBlock() {
		// This endpoint's capacity is no reason to hold the selected route's work.
		e.pathCredit.noteAnnounced(m, now)
		e.useRoute(selected)
		return false, nil
	}
	// An ACK alone would fill the payload, and recovery wouldn't track the packet.
	e.pathCredit.conn.framer.QueueControlFrame(&wire.PingFrame{})
	packet, err := e.packer.packPTOProbePacket(e.route, protocol.Encryption1RTT, e.packetSize(), true, now, e.version)
	if err != nil {
		return false, err
	}
	e.pathCredit.noteAnnounced(m, now)
	return true, e.sendCoalesced(packet, e.selected().ECNMode(true), now)
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
