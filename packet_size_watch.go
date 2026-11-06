package quic

import (
	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

// packetSizeWatch belongs to the run loop. Recovery only supplies ACK/loss facts.
// The window approximates three backed-off PTOs (1+2+4), regardless of which
// detector declared the losses. A successful jumbo ACK starts a fresh window.
type packetSizeWatch struct {
	generation                uint64
	deadline                  monotime.Time
	firstPacketNumber         protocol.PacketNumber
	losses, lostProbes        int
	checking, recheck, retire bool
	probe                     *packetSizeRecoveryProbe
}

func (c *Conn) watchingPacketSize() bool {
	return c.packetCeiling.size() > protocol.MaxPacketBufferSize && c.handshakeConfirmed && c.mtuFallback.started == 0 && c.effectivePacketCeiling() > protocol.MaxPacketBufferSize && c.mtuDiscoverer != nil && c.mtuDiscoverer.CurrentSize() > protocol.MaxPacketBufferSize
}

func (c *Conn) notePacketSizeLoss(context ackhandler.PacketContext, _ protocol.ByteCount, now monotime.Time) {
	if !c.watchingPacketSize() || context.PathID != 0 || context.TupleGeneration != c.pathGeneration {
		return
	}
	w := &c.mtuWatch
	if w.checking {
		return
	}
	if w.losses == 0 {
		w.generation = c.pathGeneration
		w.deadline = now.Add(7 * c.rttStats.PTO(true))
	}
	w.losses++
	c.scheduleSending()
}

// The run loop supplies this returned fact synchronously, before sending or a
// path switch. Retry eligibility does not depend on retiring this PING from the
// front of recovery history, and does not consume ordinary PTO dispatch credits.
func (c *Conn) notePacketSizePTO(outcome ackhandler.TimeoutOutcome) {
	if !outcome.PTO || outcome.PathID != 0 || outcome.EncLevel != protocol.Encryption1RTT {
		return
	}
	w := &c.mtuWatch
	c.notePacketSizeLivenessRetry(w.generation, w.probe)
}

func (c *Conn) notePacketSizeLivenessRetry(generation uint64, p *packetSizeRecoveryProbe) {
	if p == nil || p.sent == 0 || !c.watchingPacketSize() {
		return
	}
	w := &c.mtuWatch
	if generation != c.pathGeneration || w.generation != generation || !w.checking || w.recheck || w.probe != p {
		return
	}
	p.retryable = true
	c.scheduleSending()
}

func (c *Conn) notePacketSizeAck(context ackhandler.PacketContext, size protocol.ByteCount, number protocol.PacketNumber) {
	if !c.watchingPacketSize() || context.PathID != 0 || context.TupleGeneration != c.pathGeneration {
		return
	}
	w := &c.mtuWatch
	if size > protocol.MaxPacketBufferSize {
		if w.recheck && size < c.mtuDiscoverer.CurrentSize() {
			return
		}
		// Retirement runs after ACK processing, never inside history iteration.
		*w = packetSizeWatch{retire: w.checking || w.retire}
		c.scheduleSending()
		return
	}
	if !w.checking || number < w.firstPacketNumber {
		return
	}
	if !w.recheck {
		w.recheck, w.probe, w.retire = true, nil, true
		c.scheduleSending()
	} else if p := w.probe; p != nil && p.sent != 0 && number > p.number {
		p.laterSmallAck = true
	}
}

func (c *Conn) packetSizeWatchTime() monotime.Time {
	w := &c.mtuWatch
	if !c.watchingPacketSize() {
		return 0
	}
	if !w.checking {
		if w.losses >= 3 {
			return w.deadline
		}
		return 0
	}
	if w.recheck && w.probe != nil {
		return w.probe.deadline
	}
	return 0
}

// The connection owns the phase boundary; recovery owns its send order. Capturing
// the next number excludes earlier small packets even when their clocks tie.
func (c *Conn) startPacketSizeLiveness() {
	w := &c.mtuWatch
	w.checking, w.recheck, w.probe, w.lostProbes = true, false, nil, 0
	w.firstPacketNumber, _ = c.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
}

func (c *Conn) advancePacketSizeWatch(now monotime.Time) {
	w := &c.mtuWatch
	if w.retire {
		ackhandler.RetireMTURecoveryProbes(c.sentPacketHandler, now, false)
		w.retire = false
	}
	if !c.watchingPacketSize() {
		return
	}
	if !w.checking {
		if w.losses < 3 || now.Before(w.deadline) {
			return
		}
		c.startPacketSizeLiveness()
	}
	p := w.probe
	if !w.recheck {
		// Ordinary loss or a PTO opportunity can admit one retry. There is no
		// separate clock that can keep postponing the ordinary loss alarm.
		if p != nil && p.retryable {
			w.probe = nil
		}
		return
	}
	if p == nil || p.deadline == 0 || now.Before(p.deadline) {
		return
	}
	if !p.laterSmallAck {
		// Without later small delivery the probe has no loss verdict. Retry liveness,
		// including after a second outage immediately following the first small ACK.
		ackhandler.RetireMTURecoveryProbes(c.sentPacketHandler, now, false)
		c.startPacketSizeLiveness()
		return
	}
	ackhandler.RetireMTURecoveryProbes(c.sentPacketHandler, now, true)
	w.lostProbes++
	w.probe = nil
	if w.lostProbes == ackhandler.MaxMTURecoveryProbes {
		c.startPacketSizeFallback(now)
	}
}

func (c *Conn) preparePacketSizeWatch(now monotime.Time) packetSizeRecoveryIntent {
	c.advancePacketSizeWatch(now)
	w := &c.mtuWatch
	if !w.checking || !c.watchingPacketSize() {
		return packetSizeRecoveryIntent{}
	}
	if w.probe == nil {
		size := c.mtuDiscoverer.CurrentSize()
		if !w.recheck {
			size = 64
		}
		w.probe = &packetSizeRecoveryProbe{size: size, timeout: c.rttStats.PTO(true)}
	}
	p := w.probe
	if p.sent == 0 {
		intent := packetSizeRecoveryIntent{probe: p, followup: !w.recheck}
		if !w.recheck {
			generation := c.pathGeneration
			intent.lost = func() {
				// This callback reports an ordinary-loss fact in the connection context;
				// the next run-loop preparation decides whether to retry the same epoch.
				c.notePacketSizeLivenessRetry(generation, p)
			}
		}
		return intent
	}
	if w.recheck && !p.followupSent {
		return packetSizeRecoveryIntent{probe: p, followup: true}
	}
	return packetSizeRecoveryIntent{}
}
