//go:build unix

package quic

import (
	"sync"

	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/protocol"
)

// Only constructor-minted packet protection is eligible, and its tag is 16 bytes.
const nativeProtectionTagSize = 16

type nativeProtectionRecipe struct {
	protection    handshake.PacketProtection
	start, end    uint16
	payloadOffset uint16
	pnLen         uint8
}

// A plan follows one buffer from construction through its actual native return.
// It never borrows sender, registration, frame, or connection key state. After
// claim only the writer changes next or clears a protection handle; publication
// ends that borrow before control can release the plan.
type nativeProtectionPlan struct {
	buffer    *packetBuffer
	entries   []nativeProtectionRecipe
	frozenLen uint16
	next      uint8
	frozen    bool
}

type nativeProtectionSingle struct {
	plan    nativeProtectionPlan
	entries [1]nativeProtectionRecipe
}

type nativeProtectionLarge struct {
	plan    nativeProtectionPlan
	entries [maxGSOSegments + 3]nativeProtectionRecipe
}

// Free plans retain their inline storage, but no buffer or key. The pool's free
// retention is separate from the live bound of one plan per owned buffer.
var nativeProtectionSinglePool, nativeProtectionLargePool sync.Pool

func newNativeProtectionPlan(buffer *packetBuffer) *nativeProtectionPlan {
	if buffer == nil || buffer.refCount != 1 {
		return nil
	}
	var p *nativeProtectionPlan
	switch cap(buffer.Data) {
	case protocol.MaxPacketBufferSize:
		if cached := nativeProtectionSinglePool.Get(); cached != nil {
			p = cached.(*nativeProtectionPlan)
		} else {
			storage := new(nativeProtectionSingle)
			p = &storage.plan
			p.entries = storage.entries[:0]
		}
	case protocol.MaxLargePacketBufferSize:
		if cached := nativeProtectionLargePool.Get(); cached != nil {
			p = cached.(*nativeProtectionPlan)
		} else {
			storage := new(nativeProtectionLarge)
			p = &storage.plan
			p.entries = storage.entries[:0]
		}
	default:
		return nil
	}
	p.buffer = buffer
	return p
}

// preflight runs before reserving policy state, while Data still ends at the
// plaintext. Each later recipe starts after the preceding reserved tag. The
// buffer stays on this construction invocation across reservation callbacks.
func (p *nativeProtectionPlan) preflight(start, end, payloadOffset int, pnLen protocol.PacketNumberLen) bool {
	if p == nil || p.buffer == nil || p.buffer.refCount != 1 || p.frozen || p.next != 0 || len(p.entries) == cap(p.entries) {
		return false
	}
	if start < 0 || end <= start || end > 1<<16-1 || end > cap(p.buffer.Data) || end != len(p.buffer.Data)+nativeProtectionTagSize || pnLen < 1 || pnLen > 4 {
		return false
	}
	if len(p.entries) == 0 {
		if start != 0 {
			return false
		}
	} else if start != int(p.entries[len(p.entries)-1].end) {
		return false
	}
	length := end - start
	if payloadOffset < int(pnLen)+1 || payloadOffset > length-nativeProtectionTagSize {
		return false
	}
	return payloadOffset-int(pnLen)+4+16 <= length
}

// append follows a successful reservation with the same preflighted geometry.
// The caller extends Data through end before any next-packet or batch observer.
// The fixed inline capacity was checked before the irreversible reservation.
func (p *nativeProtectionPlan) append(protection handshake.PacketProtection, start, end, payloadOffset int, pnLen protocol.PacketNumberLen) {
	n := len(p.entries)
	p.entries = p.entries[:n+1]
	p.entries[n] = nativeProtectionRecipe{
		protection: protection, start: uint16(start), end: uint16(end),
		payloadOffset: uint16(payloadOffset), pnLen: uint8(pnLen),
	}
}

func (p *nativeProtectionPlan) freeze() bool {
	if p == nil || p.buffer == nil || p.frozen || p.next != 0 || len(p.entries) == 0 || int(p.entries[len(p.entries)-1].end) != len(p.buffer.Data) {
		return false
	}
	p.frozenLen = uint16(len(p.buffer.Data))
	p.frozen = true
	return true
}

// validFor is an admission check, before a writer can mutate the plan. Preflight
// already proved every recipe's geometry; the frozen length binds that proof to
// this exact buffer, without another packet walk.
func (p *nativeProtectionPlan) validFor(buffer *packetBuffer) bool {
	if p == nil || p.buffer != buffer || buffer == nil || !p.frozen || p.next != 0 || len(p.entries) == 0 || int(p.frozenLen) != len(buffer.Data) {
		return false
	}
	switch cap(p.entries) {
	case 1:
		return cap(buffer.Data) == protocol.MaxPacketBufferSize
	case maxGSOSegments + 3:
		return cap(buffer.Data) == protocol.MaxLargePacketBufferSize
	default:
		return false
	}
}

// Ordinary short-header recipes describe exactly the datagrams that native GSO
// or scalar fallback will submit. Admission binds both representations together.
func (p *nativeProtectionPlan) validForMember(member endpointMember) bool {
	if !p.validFor(member.buffer) || len(p.entries) != member.datagrams() {
		return false
	}
	segment := len(member.buffer.Data)
	if member.gsoSize != 0 {
		segment = int(member.gsoSize)
	}
	for i, r := range p.entries {
		if int(r.start) != i*segment || int(r.end) != min((i+1)*segment, len(member.buffer.Data)) {
			return false
		}
	}
	return true
}

func (p *nativeProtectionPlan) hasUnprotected() bool {
	return p != nil && int(p.next) < len(p.entries)
}

// protectNext does no policy work. The writer checks concrete submission
// permission before the bounded member transform and again before native I/O.
// Construction also uses it to materialize an unsupported synchronous prefix.
func (p *nativeProtectionPlan) protectNext(scratch *handshake.PacketProtectionScratch) {
	r := &p.entries[p.next]
	r.protection.Protect(p.buffer.Data[r.start:r.end:r.end], int(r.payloadOffset), int(r.pnLen), scratch)
	r.protection = handshake.PacketProtection{}
	p.next++
}

func (p *nativeProtectionPlan) protectAll(scratch *handshake.PacketProtectionScratch) {
	for p.hasUnprotected() {
		p.protectNext(scratch)
	}
}

// release is only for the construction owner, an unclaimed disposition, or after
// the writer's actual-return publication. Callers detach their owning reference
// before this may make the header available to another buffer.
func (p *nativeProtectionPlan) release() {
	if p == nil {
		return
	}
	if p.buffer == nil {
		panic("native protection plan released twice")
	}
	// Earlier handles were cleared immediately after transformation. Geometry in
	// that prefix contains no references and is overwritten by the next owner.
	clear(p.entries[p.next:])
	p.entries = p.entries[:0]
	p.buffer = nil
	p.frozenLen, p.next, p.frozen = 0, 0, false
	switch cap(p.entries) {
	case 1:
		nativeProtectionSinglePool.Put(p)
	case maxGSOSegments + 3:
		nativeProtectionLargePool.Put(p)
	default:
		panic("invalid native protection plan capacity")
	}
}
