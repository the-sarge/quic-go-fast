//go:build darwin || linux || freebsd

package quic

import (
	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/protocol"
)

// This invocation owns its plan independently of the mutable completion sidecar.
// The ticket permits deferred handoff, not registration or native submission.
type nativeProtectionConstruction struct {
	emission     *packetEmission
	buffer       *packetBuffer
	sender       *nativeOrdinarySender
	record       *ordinaryLocalRecord
	identity     endpointIdentity
	startCount   int
	startMembers int
	registered   int
	disabled     bool
	plan         *nativeProtectionPlan
}

func (b *nativeProtectionConstruction) begin(e *packetEmission, buffer *packetBuffer) {
	*b = nativeProtectionConstruction{}
	sender, ok := e.local.(*nativeOrdinarySender)
	if !ok || e.queue != sender || sender.pending == nil || sender.pending.state != ordinaryBuilding || e.route != nil || e.pathCredit != nil {
		return
	}
	r := sender.pending
	b.emission, b.buffer, b.sender, b.record = e, buffer, sender, r
	b.identity, b.startCount, b.startMembers = r.identity, r.count, len(r.members)
}

func (b *nativeProtectionConstruction) matches() bool {
	if b.sender == nil || b.emission.local != b.sender || b.emission.queue != b.sender {
		return false
	}
	r := b.sender.pending
	return r == b.record && r.state == ordinaryBuilding && r.identity == b.identity &&
		b.emission.route == nil && b.emission.pathCredit == nil &&
		r.count == b.startCount+b.registered && len(r.packets) == r.count &&
		len(r.members) == b.startMembers && r.memberEnd() == b.startCount
}

func (b *nativeProtectionConstruction) hasPlan() bool { return b != nil && b.plan != nil }

func (b *nativeProtectionConstruction) registeredPacket() {
	if b != nil && b.sender != nil {
		b.registered++
	}
}

// The normal writer path needs no connection scratch. Allocate this once only
// when an unsupported observer or stale construction ticket needs ciphertext.
func (s *nativeOrdinarySender) synchronousProtectionScratch() *handshake.PacketProtectionScratch {
	if s.protectionScratch == nil {
		s.protectionScratch = new(handshake.PacketProtectionScratch)
	}
	return s.protectionScratch
}

// Materialize a reserved prefix before an unsupported observer or a changed
// construction target receives the buffer. Policy is never reserved twice.
func (b *nativeProtectionConstruction) synchronous() {
	if b == nil {
		return
	}
	b.disabled = true
	plan := b.plan
	b.plan = nil
	if plan != nil {
		plan.protectAll(b.sender.synchronousProtectionScratch())
		plan.release()
	}
}

func (b *nativeProtectionConstruction) discard() {
	if b != nil {
		plan := b.plan
		b.plan = nil
		plan.release()
	}
}

// A later unsupported sealer must observe the preceding packets as ciphertext.
// This check invokes no sealer methods and does not reserve encryption policy.
func (b *nativeProtectionConstruction) observeSealer(sealer handshake.ShortHeaderSealer) {
	if b != nil && b.sender != nil && !b.disabled && !handshake.CanReservePacketProtection(sealer) {
		b.synchronous()
	}
}

func (b *nativeProtectionConstruction) reserve(sealer sealer, pn protocol.PacketNumber, start, payloadOffset int, pnLen protocol.PacketNumberLen, length int) (bool, error) {
	if b == nil || b.sender == nil || b.disabled {
		return false, nil
	}
	if !b.matches() {
		b.synchronous()
		return false, nil
	}
	short, ok := sealer.(handshake.ShortHeaderSealer)
	if !ok {
		b.synchronous()
		return false, nil
	}
	if b.plan == nil {
		b.plan = newNativeProtectionPlan(b.buffer)
	}
	end := start + length
	if b.plan == nil || !b.plan.preflight(start, end, payloadOffset, pnLen) {
		b.synchronous()
		return false, nil
	}
	protection, supported, err := handshake.TryReservePacketProtection(short, pn)
	if err != nil {
		return true, err
	}
	if !supported {
		b.synchronous()
		return false, nil
	}
	b.plan.append(protection, start, end, payloadOffset, pnLen)
	b.buffer.Data = b.buffer.Data[:end]
	return true, nil
}

// send consumes the plan before Send can finish/drop a group and reenter.
// A stale ticket uses the original ciphertext path, not a new refusal policy.
func (b *nativeProtectionConstruction) send(e *packetEmission, buffer *packetBuffer, gso uint16, ecn protocol.ECN, metadata sendMetadata) {
	plan := b.plan
	b.plan = nil
	if plan == nil {
		e.queue.Send(buffer, gso, ecn, metadata)
		return
	}
	if !b.matches() || buffer != b.buffer || !plan.freeze() {
		plan.protectAll(b.sender.synchronousProtectionScratch())
		plan.release()
		e.queue.Send(buffer, gso, ecn, metadata)
		return
	}
	// matches resolved the current receiver after all handoff policy callbacks.
	e.queue.(*nativeOrdinarySender).sendProtected(buffer, gso, ecn, metadata, plan)
}
