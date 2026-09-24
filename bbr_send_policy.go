package quic

import (
	"time"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

// bbrSendPolicy is connection-owned. It is selected only by private test plumbing
// until the complete controller is available. rate includes BBR's 1% margin;
// Reno's pacer and its burst/rate adjustments are deliberately independent.
type bbrSendPolicy struct {
	controller *congestion.BBRSender
	credit     *localSendCredit
	rate       uint64 // UDP payload bytes per second
	quantum    protocol.ByteCount
	tokens     int64 // byte-nanoseconds, retaining fractional byte credit
	updated    monotime.Time
}

func newBBRSendPolicy(rate uint64, size protocol.ByteCount, now monotime.Time) *bbrSendPolicy {
	p := &bbrSendPolicy{updated: now, credit: newLocalSendCredit()}
	p.update(rate, size, now)
	p.tokens = int64(p.quantum) * 1e9
	return p
}

func (p *bbrSendPolicy) update(rate uint64, size protocol.ByteCount, now monotime.Time) {
	if rate == 0 || size <= 0 || size > protocol.MaxPacketBufferSize {
		panic("invalid BBR send policy")
	}
	p.budget(now)
	p.rate = max(1, rate/100*99+rate%100*99/100)
	p.quantum = max(2*size, protocol.ByteCount(min(uint64(65536), p.rate/1000)))
	p.tokens = min(p.tokens, int64(p.quantum)*1e9)
}

func (p *bbrSendPolicy) budget(now monotime.Time) protocol.ByteCount {
	if now > p.updated {
		missing := int64(p.quantum)*1e9 - p.tokens
		delta := uint64(now.Sub(p.updated))
		if p.rate != 0 && delta >= ceilSendDelay(uint64(missing), p.rate) {
			p.tokens += missing
		} else {
			p.tokens += int64(delta * p.rate) // bounded above by missing
		}
		p.updated = now
	}
	return max(0, protocol.ByteCount(p.tokens/1e9))
}

func ceilSendDelay(deficit, rate uint64) uint64 {
	d := deficit / rate
	if deficit%rate != 0 {
		d++
	}
	return d
}

func (p *bbrSendPolicy) deadline(size protocol.ByteCount, now monotime.Time) monotime.Time {
	if p.budget(now) >= size {
		return 0
	}
	return now.Add(time.Duration(ceilSendDelay(uint64(int64(size)*1e9-p.tokens), p.rate)))
}

func (p *bbrSendPolicy) sent(size protocol.ByteCount, now monotime.Time) {
	if size < 0 || size > p.budget(now) {
		panic("BBR pacing credit exceeded")
	}
	p.tokens -= int64(size) * 1e9
}

// An isolated oversized probe waits for a full quantum, then retains its excess
// cost as pacing debt. Ordinary sends cannot borrow against future credit.
func (p *bbrSendPolicy) sentProbe(size protocol.ByteCount, now monotime.Time) {
	if size <= 0 || min(size, p.quantum) > p.budget(now) {
		panic("BBR probe pacing credit exceeded")
	}
	p.tokens -= int64(size) * 1e9
}
