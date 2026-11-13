package ackhandler

import (
	"fmt"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/qlogwriter"
)

// maxRecoveryPaths bounds nonzero contexts. The registry's four retained-history
// units already bound them, as each admitted path holds one until reclamation.
const maxRecoveryPaths = 4

// AddPath installs a further recovery context with its own RTT object.
func (h *sentPacketHandler) AddPath(id protocol.PathID, size protocol.ByteCount) error {
	return h.AddPathWithRTT(id, size, utils.NewRTTStats())
}

// AddPathWithRTT installs a recovery context after authentication or local
// commitment, sampling the RTT object crypto already reads for key retention.
// Neither an ACK nor an untrusted path identifier can allocate recovery state.
// IDs only rise, and retired contexts don't count against the two live slots.
// A rich root admits only rich paths: there is no silent Reno substitution.
func (h *sentPacketHandler) AddPathWithRTT(id protocol.PathID, size protocol.ByteCount, rtt *utils.RTTStats) error {
	if h.congestionEvents != nil {
		return fmt.Errorf("ackhandler: recovery path unavailable: %d", id)
	}
	_, err := h.admitPath(id, size, rtt, false)
	return err
}

// AddBBRPath installs a nonzero recovery context beside a BBR root with its own
// controller, delivery sampler and pending-local authority. The connection's
// logical delivery owner and the root's aggregate store ceilings stay shared.
// The context validates ECN exactly when the root does, through its own ledger
// and the path's generation, drain and capability authority, since each path
// numbers and counts its own packet space. Otherwise it sends unmarked.
func (h *sentPacketHandler) AddBBRPath(id protocol.PathID, size, ceiling protocol.ByteCount, rtt *utils.RTTStats, pending func() protocol.ByteCount, ecn func(drain bool) (generation uint64, drained, capable bool)) (*congestion.BBRSender, error) {
	if _, ok := h.congestion.(*congestion.BBRSender); !ok || size < protocol.MinInitialPacketSize || size > ceiling || (h.bbrECN != nil && ecn == nil) {
		return nil, fmt.Errorf("ackhandler: BBR path unavailable: %d", id)
	}
	b := congestion.NewBBRSenderWithPacketCeiling(size, ceiling)
	path, err := h.addRichPath(id, size, rtt, b, pending)
	if err != nil {
		return nil, err
	}
	path.congestion = b
	if h.bbrECN != nil {
		if path.local != nil {
			path.local.ecn = &localECNEvidence{watermark: protocol.InvalidPacketNumber}
		}
		EnableBBRECN(path, ecn)
	}
	return b, nil
}

// addRichPath admits a context whose sink owns its model from this path's
// value records alone. The fixture form takes any sink.
func (h *sentPacketHandler) addRichPath(id protocol.PathID, size protocol.ByteCount, rtt *utils.RTTStats, sink congestionEventSink, pending func() protocol.ByteCount) (*sentPacketHandler, error) {
	if h.congestionEvents == nil || sink == nil || pending == nil {
		return nil, fmt.Errorf("ackhandler: rich recovery path unavailable: %d", id)
	}
	path, err := h.admitPath(id, size, rtt, true)
	if err != nil {
		return nil, err
	}
	path.congestionEvents = &congestionDispatch{sink: sink, pending: pending, localRoute: path.local != nil, root: h}
	// The root keeps one context's outcome ring; the nonzero contexts divide one
	// further allowance, so the aggregate stays bounded whatever their history.
	path.congestionEvents.recovery.capacity = maxRecoveryOutcomes / maxRecoveryPaths
	h.congestionEvents.root = h
	h.richPaths = true
	return path, nil
}

func (h *sentPacketHandler) admitPath(id protocol.PathID, size protocol.ByteCount, rtt *utils.RTTStats, rich bool) (*sentPacketHandler, error) {
	if (h.local != nil && h.local.closed) || h.pathRoot != nil || id == 0 || id <= h.largestPath || h.liveContexts() >= 2 || len(h.paths) >= maxRecoveryPaths || !h.handshakeConfirmed ||
		len(h.lostPackets.lostPackets) > 32 || len(h.ackCarriers.carriers) > maxRetiredAckCarriers/2 || size < protocol.MinInitialPacketSize {
		return nil, fmt.Errorf("ackhandler: recovery path unavailable: %d", id)
	}
	rtt.SetMaxAckDelay(h.rttStats.MaxAckDelay())
	// A rich path marks no ECN until AddBBRPath installs its own validation.
	path := newSentPacketHandler(0, size, rtt, h.connStats, true, h.enableECN && !rich, h.ignorePacketsBelow, h.perspective, newPathLossRecorder(h.qlogger), h.logger, nil)
	path.pathID, path.pathRoot, path.pathPolicy = id, h, h.pathPolicy
	path.initialPackets, path.handshakePackets = nil, nil
	path.handshakeConfirmed, path.peerCompletedAddressValidation, path.peerAddressValidated = true, true, true
	if h.local != nil {
		path.local = &localCompletions{}
		if h.local.ecn != nil && h.bbrECN == nil && !rich {
			// Each path validates its own ECN, exactly as the root does.
			path.local.ecn = &localECNEvidence{watermark: protocol.InvalidPacketNumber}
			path.ecnTracker.(*ecnTracker).local = path.local.ecn
		}
	}
	// Optional late-receipt stores share their existing aggregate ceilings. A
	// missing receipt cannot discharge Reno undo or create a delivery callback.
	// Path zero keeps half, and the nonzero contexts share the other half.
	h.lostPackets.maxLength, path.lostPackets.maxLength = 32, 32/maxRecoveryPaths
	h.ackCarriers.limit, path.ackCarriers.limit = maxRetiredAckCarriers/2, maxRetiredAckCarriers/2/maxRecoveryPaths
	if h.paths == nil {
		h.paths = make(map[protocol.PathID]*sentPacketHandler, 1)
	}
	h.paths[id] = path
	h.largestPath = id
	return path, nil
}

func (h *sentPacketHandler) liveContexts() int {
	live := 0
	if !h.retiredPath {
		live++
	}
	for _, path := range h.paths {
		if !path.retiredPath {
			live++
		}
	}
	return live
}

// RetirePath marks a context retired at the abandonment signal, once no submission
// can follow it. Its evidence stays until RemovePath.
func (h *sentPacketHandler) RetirePath(id protocol.PathID) {
	if path := h.path(id); path != nil {
		path.retiredPath = true
	}
}

// PathQuiescent reports that a context has no unconsumed local result and no frames
// left to return to their logical owners, so reclamation can strand nothing. An early
// ACK clears a registration's packet but not its record, whose token still arrives.
func (h *sentPacketHandler) PathQuiescent(id protocol.PathID) bool {
	path := h.path(id)
	if path == nil {
		return true
	}
	if path.local != nil && path.local.owner.pending != 0 {
		return false
	}
	for _, p := range path.appDataPackets.history.Packets() {
		if p.Outstanding() {
			return false
		}
	}
	return true
}

// RemovePath ends a retired context at its final reclamation. A later ACK for the
// ID is ignored, because ReceivedAck can't tell it from any other absent lower ID.
// The root stays for its aggregation, and only releases path zero's retained span,
// which would otherwise charge every survivor's share of the tracking ceiling.
func (h *sentPacketHandler) RemovePath(id protocol.PathID) {
	if id == h.pathID {
		h.appDataPackets.history.releaseRetained()
		return
	}
	if path := h.paths[id]; path != nil {
		path.CloseDelivery()
	}
	delete(h.paths, id)
}

// RestartTupleECN starts this path's ECN validation over for a new tuple. Cumulative
// accounting and the PN space persist.
func (h *sentPacketHandler) RestartTupleECN() {
	if t, ok := h.ecnTracker.(*ecnTracker); ok {
		t.restartTuple()
	}
}

// EnablePathPolicy starts the liveness facts, from private admission on. Ordinary
// connections never pay for them.
func (h *sentPacketHandler) EnablePathPolicy() { h.pathPolicy = true }

func (h *sentPacketHandler) path(id protocol.PathID) *sentPacketHandler {
	if id == h.pathID {
		return h
	}
	if h.pathRoot != nil {
		return nil
	}
	return h.paths[id]
}

func (h *sentPacketHandler) validPacketContext(context PacketContext, level protocol.EncryptionLevel) bool {
	if h.pathID == 0 {
		return validPacketContext(context, level)
	}
	return context.PathID == h.pathID && level == protocol.Encryption1RTT
}

func (h *sentPacketHandler) earliestRecoveryPath() *sentPacketHandler {
	selected := h
	for _, path := range h.paths {
		if !path.alarm.Time.IsZero() && (selected.alarm.Time.IsZero() || path.alarm.Time.Before(selected.alarm.Time)) {
			selected = path
		}
	}
	return selected
}

// RecoveryOnPath exposes the existing per-path recovery operations only after
// explicit private admission. The root alone aggregates wakeups and capacity;
// frame callbacks remain owned by the connection's logical delivery machinery.
func (h *sentPacketHandler) RecoveryOnPath(id protocol.PathID) SentPacketHandler {
	if path := h.path(id); path != nil {
		return path
	}
	return nil
}

// PathLossDetectionTimeout is used by fixtures that need the unaggregated alarm.
func (h *sentPacketHandler) PathLossDetectionTimeout(id protocol.PathID) monotime.Time {
	if path := h.path(id); path != nil {
		return path.alarm.Time
	}
	return 0
}

// TakeAckedTuple reports the newest tuple whose ACK-eliciting traffic an ACK has
// newly acknowledged since the last call, whichever path carried that ACK. A path
// validation packet doesn't count, since it can be probing another tuple.
func (h *sentPacketHandler) TakeAckedTuple() (uint64, bool) {
	tuple, live := h.ackedTuple, h.ackedLive
	h.ackedTuple, h.ackedLive = 0, false
	return tuple, live
}

// ClearProbeCredit forgets the probes PTOs requested but this path hasn't sent. It
// keeps the PTO count and the alarm, so backoff and the next PTO are unaffected.
func (h *sentPacketHandler) ClearProbeCredit() {
	h.numProbesToSend = 0
}

// pathLossRecorder is a nonzero path's recorder. It carries only attributed losses;
// this path's other recovery events stay absent, as they were without a recorder.
type pathLossRecorder struct {
	qlogwriter.Recorder
}

func newPathLossRecorder(inner qlogwriter.Recorder) qlogwriter.Recorder {
	if inner == nil {
		return nil
	}
	return pathLossRecorder{inner}
}

func (r pathLossRecorder) RecordEvent(event qlogwriter.Event) {
	if _, ok := event.(qlog.PacketLost); ok {
		r.Recorder.RecordEvent(event)
	}
}

// Close leaves the connection's recorder open: it isn't this path's to close.
func (pathLossRecorder) Close() error { return nil }

// lossPath names a lost packet's path once path policy applies.
func (h *sentPacketHandler) lossPath(p *packet) *qlog.PathIdentity {
	if !h.pathPolicy {
		return nil
	}
	return &qlog.PathIdentity{PathID: uint64(p.Context.PathID), TupleGeneration: p.Context.TupleGeneration}
}
