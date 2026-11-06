package quic

import (
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
)

// The run loop owns each attempt. Recovery's ordinary loss callbacks never
// decide its verdict, including callbacks caused by a path change.
type packetSizeRecoveryProbe struct {
	size                                 protocol.ByteCount
	timeout                              time.Duration
	sent, deadline                       monotime.Time
	expired, followupSent, laterSmallAck bool
	retryable                            bool // an ordinary loss/PTO opportunity for this liveness epoch
}

type packetSizeRecoveryIntent struct {
	probe    *packetSizeRecoveryProbe
	followup bool
	lost     func()
}

func (c *Conn) packetSizeRecoveryTime() monotime.Time {
	if t := c.packetSizeWatchTime(); t != 0 {
		return t
	}
	f := &c.mtuFallback
	if !f.active || f.generation != c.pathGeneration || f.probe == nil || f.probe.expired {
		return 0
	}
	return f.probe.deadline
}

func (c *Conn) advancePacketSizeRecovery(now monotime.Time) {
	f := &c.mtuFallback
	if !f.active {
		return
	}
	if f.generation != c.pathGeneration {
		f.active, f.probe = false, nil
		return
	}
	p := f.probe
	if p == nil || p.deadline == 0 || now.Before(p.deadline) {
		return
	}
	p.expired = true
	if !p.laterSmallAck {
		return
	}
	ackhandler.RetireMTURecoveryProbes(c.sentPacketHandler, now, true)
	f.lost++
	f.probe = nil
	if f.lost < ackhandler.MaxMTURecoveryProbes {
		return
	}
	if f.size == c.effectiveInitialPacketSize {
		f.active = false
		ackhandler.RetireMTURecoveryProbes(c.sentPacketHandler, now, false)
		return
	}
	f.lost = 0
	c.reducePacketSizeForRecovery(now, c.effectiveInitialPacketSize)
}

func (c *Conn) preparePacketSizeRecovery(now monotime.Time) packetSizeRecoveryIntent {
	c.advancePacketSizeRecovery(now)
	if intent := c.preparePacketSizeWatch(now); intent.probe != nil {
		return intent
	}
	f := &c.mtuFallback
	if !f.active {
		return packetSizeRecoveryIntent{}
	}
	if f.probe == nil {
		f.probe = &packetSizeRecoveryProbe{size: f.size, timeout: c.rttStats.PTO(true)}
	}
	p := f.probe
	if p.sent == 0 {
		return packetSizeRecoveryIntent{probe: p}
	}
	if p.expired && !p.followupSent && !p.laterSmallAck {
		return packetSizeRecoveryIntent{probe: p, followup: true}
	}
	return packetSizeRecoveryIntent{}
}

// Recovery PINGs aren't retransmitted by a frame handler. The padded attempts
// use the MTU-probe loss exemption; follow-up PINGs retain ordinary loss rules.
type packetSizeRecoveryHandler struct{ lost func() }

func (*packetSizeRecoveryHandler) IsMTURecoveryPacket() {}

func (*packetSizeRecoveryHandler) OnAcked(wire.Frame) {}
func (h *packetSizeRecoveryHandler) OnLost(wire.Frame) {
	if h.lost != nil {
		h.lost()
	}
}

func (e *packetEmission) packetSizeRecovery(intent packetSizeRecoveryIntent, now monotime.Time) emissionResult {
	if e.queue.WouldBlock() {
		return emissionResult{stop: emissionQueueFull, available: e.queue.Available(), blocked: blockModeHardBlocked}
	}
	if !ackhandler.CanSendMTURecoveryProbe(e.selected()) {
		return emissionResult{stop: emissionHardBlocked, blocked: blockModeHardBlocked}
	}
	size := intent.probe.size
	if intent.followup {
		// Small enough to test liveness even when the initial-size probe is lost.
		// 64 bytes covers a short header with the maximum connection ID and AEAD tag.
		size = 64
	}
	if e.bbr != nil && !e.reserveLocal(size, false, false, now) {
		return e.localBlocked()
	}
	ping := ackhandler.Frame{Frame: &wire.PingFrame{}, Handler: &packetSizeRecoveryHandler{lost: intent.lost}}
	p, buf, err := e.packer.packMTUProbePacket(e.route, ping, size, e.version)
	if err != nil {
		return emissionResult{err: err}
	}
	p.IsPathMTUProbePacket = !intent.followup
	ecn := e.selected().ECNMode(true)
	e.policy.logShortHeaderPacket(p, ecn, buf.Len())
	if err := e.registerPacket(p, ecn, now); err != nil {
		buf.Release()
		return emissionResult{err: err}
	}
	if intent.followup {
		intent.probe.followupSent = true
		if intent.probe.sent == 0 {
			intent.probe.sent = now
		}
	} else {
		intent.probe.sent, intent.probe.deadline = now, now.Add(intent.probe.timeout)
	}
	e.handoff(buf, 0, ecn, sendMetadata{mtuProbe: !intent.followup})
	return emissionResult{progress: true, retry: true}
}
