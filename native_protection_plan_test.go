//go:build darwin || linux || freebsd

package quic

import (
	"bytes"
	"strconv"
	"testing"
	"unsafe"

	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

// Reuse the real in-memory TLS fixture; no production protection factory is exposed.
func nativeProtectionTestSealer(t *testing.T) handshake.ShortHeaderSealer {
	t.Helper()
	sealer, _ := nativeProtectionTestCrypto(t, protocol.Version1)
	if !handshake.CanReservePacketProtection(sealer) {
		t.Skip("runtime primitives do not provide the immutable capability")
	}
	return sealer
}

func nativeProtectionTestBuffer(capacity int) *packetBuffer {
	return &packetBuffer{Data: make([]byte, 0, capacity), refCount: 1}
}

// The reference protects an isolated packet with the same immutable handle. The
// plan must produce those bytes without including neighboring packets or tags.
func appendNativeProtectionTestPacket(t *testing.T, p *nativeProtectionPlan, sealer handshake.ShortHeaderSealer, pn protocol.PacketNumber, pnLen protocol.PacketNumberLen, payloadLen int) []byte {
	t.Helper()
	start := len(p.buffer.Data)
	raw, err := wire.AppendShortHeader(p.buffer.Data[start:], protocol.ConnectionID{}, pn, pnLen, sealer.KeyPhase())
	require.NoError(t, err)
	payloadOffset := len(raw)
	raw = append(raw, bytes.Repeat([]byte{byte(pn + 1)}, payloadLen)...)
	p.buffer.Data = p.buffer.Data[:start+len(raw)]
	end := len(p.buffer.Data) + nativeProtectionTagSize
	require.True(t, p.preflight(start, end, payloadOffset, pnLen))
	protection, supported, err := handshake.TryReservePacketProtection(sealer, pn)
	require.NoError(t, err)
	require.True(t, supported)
	require.NotEqual(t, handshake.PacketProtection{}, protection)
	p.append(protection, start, end, payloadOffset, pnLen)
	p.buffer.Data = p.buffer.Data[:end]
	reference := bytes.Clone(p.buffer.Data[start:end])
	var scratch handshake.PacketProtectionScratch
	protection.Protect(reference, payloadOffset, int(pnLen), &scratch)
	return reference
}

func TestNativeProtectionPlanStorageClasses(t *testing.T) {
	t.Logf("layout bytes: recipe=%d plan=%d single=%d large=%d endpointMember=%d nativeOrdinarySender=%d nativeProtectionConstruction=%d", unsafe.Sizeof(nativeProtectionRecipe{}), unsafe.Sizeof(nativeProtectionPlan{}), unsafe.Sizeof(nativeProtectionSingle{}), unsafe.Sizeof(nativeProtectionLarge{}), unsafe.Sizeof(endpointMember{}), unsafe.Sizeof(nativeOrdinarySender{}), unsafe.Sizeof(nativeProtectionConstruction{}))
	if strconv.IntSize == 64 {
		require.EqualValues(t, 24, unsafe.Sizeof(nativeProtectionRecipe{}))
		require.EqualValues(t, 40, unsafe.Sizeof(nativeProtectionPlan{}))
		require.EqualValues(t, 64, unsafe.Sizeof(nativeProtectionSingle{}))
		require.EqualValues(t, 1648, unsafe.Sizeof(nativeProtectionLarge{}))
	}
	for _, tc := range []struct{ capacity, entries int }{{protocol.MaxPacketBufferSize, 1}, {protocol.MaxLargePacketBufferSize, maxGSOSegments + 3}} {
		buffer := nativeProtectionTestBuffer(tc.capacity)
		p := newNativeProtectionPlan(buffer)
		require.NotNil(t, p)
		require.Same(t, buffer, p.buffer)
		require.Empty(t, p.entries)
		require.Equal(t, tc.entries, cap(p.entries))
		require.Zero(t, p.next)
		require.False(t, p.frozen)
		p.release()
		require.Equal(t, 1, buffer.refCount, "plan release never releases the separately owned buffer")
	}
	require.Nil(t, newNativeProtectionPlan(nil))
	for _, capacity := range []int{0, protocol.MaxPacketBufferSize + 1, protocol.MaxLargePacketBufferSize + 1, 1 << 16} {
		require.Nil(t, newNativeProtectionPlan(nativeProtectionTestBuffer(capacity)))
	}
	buffer := nativeProtectionTestBuffer(protocol.MaxPacketBufferSize)
	buffer.refCount = 2
	require.Nil(t, newNativeProtectionPlan(buffer))
	buffer.refCount = 1
	buffer.Data = buffer.Data[:5]
	p := newNativeProtectionPlan(buffer)
	require.NotNil(t, p, "a plan can bind after first-packet serialization")
	require.True(t, p.preflight(0, len(buffer.Data)+16, 2, 1))
	p.release()
}

func TestNativeProtectionPlanGeometryBeforeReservation(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		start, end, payloadOffset int
		pnLen                     protocol.PacketNumberLen
	}{
		{"nonzero first start", 1, 48, 10, 2},
		{"negative start", -1, 48, 10, 2},
		{"wrong tag end", 0, 47, 10, 2},
		{"offset truncation", 0, 1 << 16, 10, 2},
		{"payload in tag", 0, 48, 33, 2},
		{"PN outside header", 0, 48, 2, 2},
		{"negative payload", 0, 48, -1, 2},
		{"missing PN", 0, 48, 10, 0},
		{"long PN", 0, 48, 10, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buffer := nativeProtectionTestBuffer(protocol.MaxLargePacketBufferSize)
			p := newNativeProtectionPlan(buffer)
			defer p.release()
			buffer.Data = buffer.Data[:32]
			require.True(t, p.preflight(0, 48, 10, 2))
			require.False(t, p.preflight(tc.start, tc.end, tc.payloadOffset, tc.pnLen))
			require.Len(t, buffer.Data, 32)
			require.Empty(t, p.entries)
			require.Zero(t, p.next)
		})
	}
	buffer := nativeProtectionTestBuffer(protocol.MaxPacketBufferSize)
	p := newNativeProtectionPlan(buffer)
	defer p.release()
	buffer.Data = buffer.Data[:2]
	require.False(t, p.preflight(0, 18, 2, 1), "header-protection sample requires minimum plaintext padding")
	buffer.Data = buffer.Data[:protocol.MaxPacketBufferSize]
	require.False(t, p.preflight(0, len(buffer.Data)+16, 10, 2), "tag must fit the existing allocation")
}

func TestNativeProtectionPlanRejectsOverlappingRecipes(t *testing.T) {
	buffer := nativeProtectionTestBuffer(protocol.MaxLargePacketBufferSize)
	p := newNativeProtectionPlan(buffer)
	defer p.release()
	buffer.Data = buffer.Data[:32]
	require.True(t, p.preflight(0, 48, 10, 2))
	p.append(handshake.PacketProtection{}, 0, 48, 10, 2)
	buffer.Data = buffer.Data[:48+32]
	require.True(t, p.preflight(48, 96, 10, 2))
	require.False(t, p.preflight(47, 96, 10, 2), "a later packet cannot overlap a reserved tag")
	require.False(t, p.preflight(49, 96, 10, 2), "the recipe prefix must cover the buffer without a gap")
	require.Len(t, p.entries, 1)
	require.EqualValues(t, 48, p.entries[0].end)
}

func TestNativeProtectionPlanFixedCapacityAndFreeze(t *testing.T) {
	for _, capacity := range []int{protocol.MaxPacketBufferSize, protocol.MaxLargePacketBufferSize} {
		buffer := nativeProtectionTestBuffer(capacity)
		p := newNativeProtectionPlan(buffer)
		require.False(t, p.freeze())
		entryCap := cap(p.entries)
		for range entryCap {
			start := len(buffer.Data)
			buffer.Data = buffer.Data[:start+5]
			end := len(buffer.Data) + 16
			require.True(t, p.preflight(start, end, 2, 1))
			p.append(handshake.PacketProtection{}, start, end, 2, 1)
			require.False(t, p.freeze(), "tag length must be committed before freezing")
			buffer.Data = buffer.Data[:end]
		}
		start := len(buffer.Data)
		buffer.Data = buffer.Data[:start+5]
		require.False(t, p.preflight(start, len(buffer.Data)+16, 2, 1), "capacity exhaustion must be detected before Reserve")
		buffer.Data = buffer.Data[:start]
		require.Equal(t, entryCap, cap(p.entries), "recipe append never grows storage")
		require.True(t, p.freeze())
		require.True(t, p.validFor(buffer))
		require.False(t, p.freeze())
		require.False(t, p.validFor(&packetBuffer{Data: buffer.Data, refCount: 1}), "matching backing does not transfer buffer identity")
		buffer.Data = buffer.Data[:len(buffer.Data)-1]
		require.False(t, p.validFor(buffer))
		buffer.Data = buffer.Data[:start]
		require.True(t, p.validFor(buffer))
		p.release()
	}
}

func TestNativeProtectionPlanPacketAndTagBoundaries(t *testing.T) {
	sealer := nativeProtectionTestSealer(t)
	buffer := nativeProtectionTestBuffer(protocol.MaxLargePacketBufferSize)
	p := newNativeProtectionPlan(buffer)
	defer p.release()
	var expected []byte
	for pnLen := protocol.PacketNumberLen1; pnLen <= protocol.PacketNumberLen4; pnLen++ {
		expected = append(expected, appendNativeProtectionTestPacket(t, p, sealer, protocol.PacketNumber(pnLen), pnLen, 4-int(pnLen))...)
	}
	require.Len(t, buffer.Data, 4*21, "each packet reserves its complete tag before the next header")
	require.True(t, p.freeze())
	unprotected := bytes.Clone(buffer.Data)
	var scratch handshake.PacketProtectionScratch
	firstEnd := int(p.entries[0].end)
	p.protectNext(&scratch)
	require.Equal(t, expected[:firstEnd], buffer.Data[:firstEnd])
	require.Equal(t, unprotected[firstEnd:], buffer.Data[firstEnd:], "one transform must not touch later plaintext or tag slots")
	require.Zero(t, p.entries[0].protection)
	require.NotZero(t, p.entries[1].protection)
	p.protectAll(&scratch)
	require.Equal(t, expected, buffer.Data)
	require.False(t, p.hasUnprotected())
	p.protectAll(&scratch)
	require.Equal(t, expected, buffer.Data, "fallback and retry must not protect the same bytes again")
	for _, entry := range p.entries {
		require.Zero(t, entry.protection)
	}
	for i, capacity := range []int{protocol.MaxPacketBufferSize, protocol.MaxLargePacketBufferSize} {
		full := newNativeProtectionPlan(nativeProtectionTestBuffer(capacity))
		reference := appendNativeProtectionTestPacket(t, full, sealer, protocol.PacketNumber(10+i), 2, capacity-3-16)
		require.Len(t, full.buffer.Data, capacity)
		require.True(t, full.freeze())
		full.protectAll(&scratch)
		require.Equal(t, reference, full.buffer.Data, "the final tag must fit at each exact buffer-class boundary")
		full.release()
	}
}

func TestNativeProtectionPlanReleaseClearsUntransformedTail(t *testing.T) {
	sealer := nativeProtectionTestSealer(t)
	for _, transformed := range []int{0, 1, 3} {
		buffer := nativeProtectionTestBuffer(protocol.MaxLargePacketBufferSize)
		p := newNativeProtectionPlan(buffer)
		for pn := range 3 {
			appendNativeProtectionTestPacket(t, p, sealer, protocol.PacketNumber(10*transformed+pn), 2, 16)
		}
		var scratch handshake.PacketProtectionScratch
		for range transformed {
			p.protectNext(&scratch)
		}
		snapshot := bytes.Clone(buffer.Data)
		p.release()
		// No pool acquisition occurs until these post-release assertions finish;
		// the test does not depend on sync.Pool returning a particular address.
		require.Nil(t, p.buffer)
		require.Empty(t, p.entries)
		require.Zero(t, p.next)
		require.False(t, p.frozen)
		for _, entry := range p.entries[:cap(p.entries)] {
			require.Zero(t, entry.protection, "a pooled plan must retain no live key")
		}
		require.Equal(t, snapshot, buffer.Data, "discarding a plan does not transform or release its buffer")
		require.Equal(t, 1, buffer.refCount)
		require.Panics(t, p.release, "double release must not duplicate a pool entry")
		next := newNativeProtectionPlan(nativeProtectionTestBuffer(protocol.MaxLargePacketBufferSize))
		require.Empty(t, next.entries)
		require.Zero(t, next.next)
		require.False(t, next.frozen)
		next.release()
	}
}
