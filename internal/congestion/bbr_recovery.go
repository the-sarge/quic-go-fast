package congestion

import (
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
)

// Recovery owns exact episode membership. The reducer keeps only the saved
// policy state for that identity, never a second history of lost packets.
type bbrUndo struct {
	id                                  uint64
	valid                               bool
	window, inflightLong, inflightShort protocol.ByteCount
	bandwidthShort                      uint64
	// The draft's undo_state: a loss-driven exit from Startup or Up.
	exited bool
	phase  bbrPhase
}

func (b *BBRSender) beginRecovery(e FeedbackEvent) {
	r := e.RecoveryEpisode
	if r.Entered {
		b.undo = bbrUndo{id: r.ID, valid: true, window: b.window, inflightLong: b.inflightLong, inflightShort: b.inflightShort, bandwidthShort: b.bandwidthShort}
	}
	if r.Active {
		b.recoveryBoundary = max(b.recoveryBoundary, r.Boundary)
	}
}

func (b *BBRSender) finishRecovery(e FeedbackEvent) {
	r := e.RecoveryEpisode
	if !b.undo.valid || r.ID != b.undo.id {
		return
	}
	if r.UndoEligible {
		b.inflightLong = max(b.inflightLong, b.undo.inflightLong)
		b.inflightShort = max(b.inflightShort, b.undo.inflightShort)
		b.bandwidthShort = max(b.bandwidthShort, b.undo.bandwidthShort)
		b.fullBandwidth, b.plateau = 0, 0
		b.lossRanges = b.lossRanges[:0]
		b.lossBytes, b.lossFlight = 0, 0
		b.lossPending, b.lossOverflow = false, false
		// A probe ended by spurious loss resumes. An active CE response owns
		// the exit and forbids a new probe; ProbeRTT finishes its measurement.
		if b.undo.exited && !b.ce.active {
			switch {
			case b.undo.phase == bbrStartup && b.phase == bbrProbeRTT:
				b.probeRTTReturnStartup = true
			case b.undo.phase == bbrStartup && b.phase != bbrStartup:
				b.phase = bbrStartup
			case b.undo.phase == bbrUp && b.phase != bbrUp && b.phase != bbrProbeRTT:
				b.startProbeRefill(b.delivered)
			}
		}
	}
	if r.Exited || r.UndoEligible {
		b.window = max(b.window, b.undo.window)
		if b.InProbeRTT() {
			b.probeRTTSavedWindow = max(b.probeRTTSavedWindow, b.undo.window)
		}
		b.boundWindow()
		b.rate = b.phaseRate()
	}
	if r.UndoEligible || (!r.Active && !r.UndoPossible) {
		b.undo = bbrUndo{}
	}
}

// A loss-driven exit from Startup or Up is the draft's undo_state.
func (b *BBRSender) noteLossExit() {
	if b.undo.valid {
		b.undo.exited, b.undo.phase = true, b.phase
	}
}

// Persistent proof is transport-owned. CE is composed from the pre-event
// outputs first; replacing the model must preserve that independent safety state.
func (b *BBRSender) restartPersistent(e FeedbackEvent) {
	end := e.PersistentCongestion.EndOrdinal
	if !e.HasAck || end == 0 || end <= b.persistentEnd {
		return
	}
	ce, ordinal, last := b.ce, b.sentOrdinal, max(b.lastEvent, e.Time)
	b.Reset(b.pathGeneration, b.sampleGeneration+1, e.Delivery.Delivered)
	b.ce, b.sentOrdinal, b.lastEvent = ce, ordinal, last
	b.ce.roundStarted, b.ce.cleanRound, b.ce.roundDirty = false, false, true
	b.persistentEnd = end
	b.persistentWaiting, b.persistentModel = true, true
	b.initialWindow, b.window = 2*b.size, 2*b.size
	b.pacingSeed = bbrScale(uint64(b.initialWindow), uint64(time.Second), uint64(max(e.SmoothedRTT, time.Millisecond)))
	b.rate = 0
	b.rate = b.phaseRate()
}

func (b *BBRSender) acknowledgeRestart(e FeedbackEvent) {
	if !b.persistentWaiting || !e.HasAck {
		return
	}
	for _, p := range e.Acked {
		if p.RegistrationValid && p.AckEliciting && !p.MTUProbe && !p.PathProbe && p.Length > 0 && p.PathGeneration == b.pathGeneration && p.SampleGeneration >= b.modelSampleFloor && p.SampleGeneration == e.SampleGeneration && p.Ordinal <= b.sentOrdinal {
			b.persistentWaiting = false
			return
		}
	}
}
