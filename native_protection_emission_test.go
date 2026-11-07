//go:build darwin || linux || freebsd

package quic

import (
	"bytes"
	"crypto/fips140"
	"errors"
	"net/netip"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type nativeProtectionEmissionFixture struct {
	conn   *Conn
	sender *nativeOrdinarySender
	sealer handshake.ShortHeaderSealer
	opener handshake.ShortHeaderOpener
	now    monotime.Time
}

// Keep real construction, native registration and the grouped record. These
// tests call the actual writer function directly, so no background endpoint is
// required merely to observe the pre-write construction interval.
func newNativeProtectionEmissionFixture(t *testing.T) nativeProtectionEmissionFixture {
	t.Helper()
	return newNativeProtectionEmissionFixtureMode(t, true)
}

func newNativeProtectionEmissionFixtureMode(t *testing.T, deferred bool) nativeProtectionEmissionFixture {
	t.Helper()
	if deferred && fips140.Enabled() {
		t.Skip("this family requires the optional non-FIPS deferred capability")
	}
	c := newEmissionTestConnection(t, true).conn
	recovery, err := ackhandler.EnableLocalCompletions(c.sentPacketHandler)
	require.NoError(t, err)
	c.sentPacketHandler = recovery
	c.emission.recovery = &c.sentPacketHandler
	c.emission.packer.pnManager = recovery
	endpoint := &nativeEndpoint{budget: time.Minute}
	q := &nativeOrdinarySender{endpoint: endpoint, participant: &endpointParticipant{id: 1, endpoint: endpoint}, recovery: recovery, connection: c, generation: 1, destination: netip.MustParseAddrPort("127.0.0.1:1234"), writeState: &endpointWriteState{}}
	c.emission.local, c.emission.queue = q, q
	q.pending = q.reserve(nil)
	require.NotNil(t, q.pending)
	sealer, opener := nativeProtectionTestCrypto(t, protocol.Version1)
	if deferred && !handshake.CanReservePacketProtection(sealer) {
		t.Skip("runtime primitives do not provide the immutable capability")
	}
	framer := NewMockFrameSource(gomock.NewController(t))
	framer.EXPECT().HasData().Return(true).AnyTimes()
	framer.EXPECT().Append(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ []ackhandler.Frame, streams []ackhandler.StreamFrame, _ protocol.ByteCount, _ monotime.Time, version protocol.Version) ([]ackhandler.Frame, []ackhandler.StreamFrame, protocol.ByteCount) {
			frame := &wire.PingFrame{}
			return []ackhandler.Frame{{Frame: frame, Handler: &nativeProtectionFrameHandler{}}}, streams, frame.Length(version)
		},
	).AnyTimes()
	c.emission.packer.framer = framer
	return nativeProtectionEmissionFixture{conn: c, sender: q, sealer: sealer, opener: opener, now: monotime.Now()}
}

func nativeProtectionEmissionPlaintext(t *testing.T, opener handshake.ShortHeaderOpener, ciphertext []byte, payloadOffset, pnLen int, pn protocol.PacketNumber) []byte {
	t.Helper()
	raw := bytes.Clone(ciphertext)
	pnOffset := payloadOffset - pnLen
	opener.DecryptHeader(raw[pnOffset+4:pnOffset+20], &raw[0], raw[pnOffset:payloadOffset])
	payload, err := opener.Open(nil, raw[payloadOffset:], monotime.Now(), pn, protocol.KeyPhaseZero, raw[:payloadOffset])
	require.NoError(t, err)
	return payload
}

func TestNativeProtectionEmissionReservationBeforeWriter(t *testing.T) {
	f := newNativeProtectionEmissionFixture(t)
	sealing := NewMockSealingManager(gomock.NewController(t))
	sealing.EXPECT().Get1RTTSealer().Return(f.sealer, nil)
	f.conn.emission.packer.cryptoSetup = sealing
	buffer := getPacketBuffer()
	defer buffer.Release()
	var construction nativeProtectionConstruction
	construction.begin(&f.conn.emission, buffer)
	defer construction.discard()
	_, err := f.conn.emission.appendNativePacket(buffer, 1200, false, protocol.ECNNon, f.now, &construction)
	require.NoError(t, err)
	require.NotNil(t, construction.plan)
	require.Len(t, construction.plan.entries, 1)
	require.Equal(t, 1, f.sender.pending.count)
	require.Len(t, f.sender.pending.packets, 1)
	next, _ := f.sender.recovery.PeekPacketNumber(protocol.Encryption1RTT)
	require.EqualValues(t, 1, next, "reservation consumes PN before writer execution")
	localToken := f.sender.pending.packets[0].token

	plan := construction.plan
	r := plan.entries[0]
	plaintext := bytes.Clone(buffer.Data)
	require.Zero(t, plan.next)
	// ACK before native completion retires/pools recovery metadata. The buffer
	// plan must remain independent of that lifetime and still protect the bytes.
	_, err = f.sender.recovery.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: 0, Largest: 0}}}, protocol.Encryption1RTT, f.now.Add(time.Millisecond))
	require.NoError(t, err)
	var churn [8]ackhandler.PacketPreparation
	for i := range churn {
		churn[i] = ackhandler.PreparePacket()
		defer churn[i].Discard()
	}
	require.Equal(t, plaintext, buffer.Data, "registration/early ACK has not transformed serialized bytes")
	require.True(t, plan.freeze())
	construction.plan = nil
	defer plan.release()
	token := &endpointToken{request: endpointRequest{members: []endpointMember{{buffer: buffer, protection: plan, writeState: f.sender.writeState}}, deadline: time.Now().Add(time.Minute), destination: f.sender.destination}}
	var sent []byte
	var scratch handshake.PacketProtectionScratch
	result := writeEndpointRequestWithScratch(endpointTestNative{send: func(data []byte) (int, error) {
		sent = bytes.Clone(data)
		require.EqualValues(t, 1, plan.next)
		require.Zero(t, plan.entries[0].protection, "writer clears key handle before native callback")
		return len(data), nil
	}}, token, &scratch)
	require.NoError(t, result.failure())
	require.Equal(t, 1, result.members[0].accepted)
	require.Zero(t, result.members[0].uncertain)
	require.Zero(t, result.members[0].unsubmitted)
	require.Equal(t, plaintext[int(r.payloadOffset):len(plaintext)-16], nativeProtectionEmissionPlaintext(t, f.opener, sent, int(r.payloadOffset), int(r.pnLen), 0))
	require.NoError(t, f.sender.recovery.CompleteLocalPacket(localToken, ackhandler.LocalPacketAccepted, result.members[0].returnedAt))
}

type nativeProtectionEmissionObservedSealer struct {
	handshake.ShortHeaderSealer
	onOverhead func()
	seals      int
	fail       error
}

func (s *nativeProtectionEmissionObservedSealer) Overhead() int {
	if s.onOverhead != nil {
		callback := s.onOverhead
		s.onOverhead = nil
		callback()
	}
	return s.ShortHeaderSealer.Overhead()
}

func (s *nativeProtectionEmissionObservedSealer) Seal(dst, src []byte, pn protocol.PacketNumber, ad []byte) ([]byte, error) {
	s.seals++
	if s.fail != nil {
		return nil, s.fail
	}
	return s.ShortHeaderSealer.Seal(dst, src, pn, ad)
}

func TestNativeProtectionEmissionUnsupportedObserverRestoresPrefix(t *testing.T) {
	f := newNativeProtectionEmissionFixture(t)
	buffer := getLargePacketBuffer()
	defer buffer.Release()
	var construction nativeProtectionConstruction
	construction.begin(&f.conn.emission, buffer)
	defer construction.discard()
	wrapper := &nativeProtectionEmissionObservedSealer{ShortHeaderSealer: f.sealer}
	sealing := NewMockSealingManager(gomock.NewController(t))
	gomock.InOrder(sealing.EXPECT().Get1RTTSealer().Return(f.sealer, nil), sealing.EXPECT().Get1RTTSealer().Return(wrapper, nil))
	f.conn.emission.packer.cryptoSetup = sealing
	_, err := f.conn.emission.appendNativePacket(buffer, 1200, false, protocol.ECNNon, f.now, &construction)
	require.NoError(t, err)
	require.NotNil(t, construction.plan)
	first := construction.plan.entries[0]
	firstPlain := bytes.Clone(buffer.Data)
	observed := false
	wrapper.onOverhead = func() {
		observed = true
		require.Nil(t, construction.plan, "unknown sealer cannot observe a still-deferred prefix")
		require.Equal(t, firstPlain[int(first.payloadOffset):len(firstPlain)-16], nativeProtectionEmissionPlaintext(t, f.opener, buffer.Data[:first.end], int(first.payloadOffset), int(first.pnLen), 0))
	}
	_, pnLen := f.sender.recovery.PeekPacketNumber(protocol.Encryption1RTT)
	offset := int(wire.ShortHeaderLen(f.conn.connIDManager.Get(), pnLen))
	_, err = f.conn.emission.appendNativePacket(buffer, 1200, false, protocol.ECNNon, f.now, &construction)
	require.NoError(t, err)
	require.True(t, observed)
	require.Equal(t, 1, wrapper.seals, "fallback reserves/seals the unsupported packet only once")
	require.Nil(t, construction.plan)
	require.True(t, construction.disabled)
	require.Equal(t, firstPlain[int(first.payloadOffset):len(firstPlain)-16], nativeProtectionEmissionPlaintext(t, f.opener, buffer.Data[first.end:], offset, int(pnLen), 1))
	next, _ := f.sender.recovery.PeekPacketNumber(protocol.Encryption1RTT)
	require.EqualValues(t, 2, next)
	for _, packet := range f.sender.pending.packets {
		require.NoError(t, f.sender.recovery.CompleteLocalPacket(packet.token, ackhandler.LocalPacketUnsubmitted, monotime.Now()))
	}
}

type nativeProtectionEmissionObservedQueue struct {
	sender
	onSend func(*packetBuffer)
}

func (q nativeProtectionEmissionObservedQueue) Send(buffer *packetBuffer, _ uint16, _ protocol.ECN, _ sendMetadata) {
	q.onSend(buffer)
}

func TestNativeProtectionEmissionChangedTicketRestoresCiphertext(t *testing.T) {
	f := newNativeProtectionEmissionFixture(t)
	sealing := NewMockSealingManager(gomock.NewController(t))
	sealing.EXPECT().Get1RTTSealer().Return(f.sealer, nil)
	f.conn.emission.packer.cryptoSetup = sealing
	buffer := getPacketBuffer()
	defer buffer.Release()
	var construction nativeProtectionConstruction
	construction.begin(&f.conn.emission, buffer)
	defer construction.discard()
	var originalPlan *nativeProtectionPlan
	var recipe nativeProtectionRecipe
	var plaintext []byte
	sent := false
	replacement := nativeProtectionEmissionObservedQueue{onSend: func(got *packetBuffer) {
		sent = true
		require.Same(t, buffer, got)
		require.Nil(t, construction.plan)
		require.Nil(t, originalPlan.buffer, "old plan is released before the replacement queue observes bytes")
		require.Equal(t, plaintext[int(recipe.payloadOffset):len(plaintext)-16], nativeProtectionEmissionPlaintext(t, f.opener, got.Data, int(recipe.payloadOffset), int(recipe.pnLen), 0))
	}}
	f.conn.emission.policy = nativeProtectionTestPolicy{emissionPolicy: f.conn.emission.policy, onLog: func() {}, onActivity: func() {
		originalPlan = construction.plan
		require.NotNil(t, originalPlan)
		recipe = originalPlan.entries[0]
		plaintext = bytes.Clone(buffer.Data)
		f.conn.emission.queue = replacement
	}}
	_, err := f.conn.emission.appendNativePacket(buffer, 1200, false, protocol.ECNNon, f.now, &construction)
	require.NoError(t, err)
	require.False(t, construction.matches())
	require.Equal(t, plaintext, buffer.Data)
	f.conn.emission.handoff(buffer, 0, protocol.ECNNon, sendMetadata{}, &construction)
	require.True(t, sent)
	require.Len(t, f.sender.pending.packets, 1, "replacement queue does not move registration authority")
	require.NoError(t, f.sender.recovery.CompleteLocalPacket(f.sender.pending.packets[0].token, ackhandler.LocalPacketUnsubmitted, monotime.Now()))
}

// Isolate only free-pool factories, as the existing construction lifetime tests
// do. These sequential fixtures have no endpoint goroutine or live pooled owner.
func observeNativeProtectionEmissionStorage(t *testing.T, large bool) (**packetBuffer, **nativeProtectionPlan) {
	t.Helper()
	var buffer *packetBuffer
	var plan *nativeProtectionPlan
	if large {
		largeBufferPool = sync.Pool{New: func() any {
			buffer = &packetBuffer{Data: make([]byte, 0, protocol.MaxLargePacketBufferSize)}
			return buffer
		}}
		nativeProtectionLargePool = sync.Pool{New: func() any {
			storage := new(nativeProtectionLarge)
			storage.plan.entries = storage.entries[:0]
			plan = &storage.plan
			return plan
		}}
		t.Cleanup(func() {
			largeBufferPool = sync.Pool{New: func() any { return &packetBuffer{Data: make([]byte, 0, protocol.MaxLargePacketBufferSize)} }}
			nativeProtectionLargePool = sync.Pool{}
		})
	} else {
		bufferPool = sync.Pool{New: func() any {
			buffer = &packetBuffer{Data: make([]byte, 0, protocol.MaxPacketBufferSize)}
			return buffer
		}}
		nativeProtectionSinglePool = sync.Pool{New: func() any {
			storage := new(nativeProtectionSingle)
			storage.plan.entries = storage.entries[:0]
			plan = &storage.plan
			return plan
		}}
		t.Cleanup(func() {
			bufferPool = sync.Pool{New: func() any { return &packetBuffer{Data: make([]byte, 0, protocol.MaxPacketBufferSize)} }}
			nativeProtectionSinglePool = sync.Pool{}
		})
	}
	return &buffer, &plan
}

func requireNativeProtectionEmissionReleased(t *testing.T, buffer *packetBuffer, plan *nativeProtectionPlan) {
	t.Helper()
	require.NotNil(t, buffer)
	require.Zero(t, buffer.refCount)
	require.NotNil(t, plan)
	require.Nil(t, plan.buffer)
	require.Empty(t, plan.entries)
	for _, recipe := range plan.entries[:cap(plan.entries)] {
		require.Zero(t, recipe.protection)
	}
}

func TestNativeProtectionEmissionRegistrationRefusalReleasesStorage(t *testing.T) {
	f := newNativeProtectionEmissionFixture(t)
	buffer, plan := observeNativeProtectionEmissionStorage(t, false)
	sealing := NewMockSealingManager(gomock.NewController(t))
	sealing.EXPECT().Get1RTTSealer().Return(f.sealer, nil)
	f.conn.emission.packer.cryptoSetup = sealing
	observedCommit := false
	f.conn.emission.policy = nativeProtectionTestPolicy{emissionPolicy: f.conn.emission.policy, onLog: func() {
		observedCommit = true
		require.NotNil(t, *plan)
		require.Len(t, (*plan).entries, 1)
		require.NotZero(t, (*plan).entries[0].protection)
		next, _ := f.sender.recovery.PeekPacketNumber(protocol.Encryption1RTT)
		require.EqualValues(t, 1, next)
	}, onActivity: func() {}}
	result := f.conn.emission.withoutGSO(0) // registration rejects the zero send time
	require.Error(t, result.err)
	require.False(t, result.progress)
	require.True(t, observedCommit)
	next, _ := f.sender.recovery.PeekPacketNumber(protocol.Encryption1RTT)
	require.EqualValues(t, 1, next, "post-reservation refusal must not refund PN")
	require.Zero(t, f.sender.pending.count)
	require.Empty(t, f.sender.pending.packets)
	require.Empty(t, f.sender.pending.members, "no handoff happened")
	requireNativeProtectionEmissionReleased(t, *buffer, *plan)
	f.sender.finish()
	require.Nil(t, f.sender.pending)
}

func TestNativeProtectionEmissionReplacedPendingRestoresCiphertext(t *testing.T) {
	f := newNativeProtectionEmissionFixture(t)
	sealing := NewMockSealingManager(gomock.NewController(t))
	sealing.EXPECT().Get1RTTSealer().Return(f.sealer, nil)
	f.conn.emission.packer.cryptoSetup = sealing
	buffer := getPacketBuffer()
	var construction nativeProtectionConstruction
	construction.begin(&f.conn.emission, buffer)
	defer func() {
		construction.discard()
		if buffer.refCount != 0 {
			buffer.Release()
		}
	}()
	originalRecord := f.sender.pending
	originalIdentity := originalRecord.identity
	var originalPlan *nativeProtectionPlan
	var recipe nativeProtectionRecipe
	var plaintext []byte
	f.conn.emission.policy = nativeProtectionTestPolicy{emissionPolicy: f.conn.emission.policy, onLog: func() {}, onActivity: func() {
		originalPlan = construction.plan
		require.NotNil(t, originalPlan)
		recipe = originalPlan.entries[0]
		plaintext = bytes.Clone(buffer.Data)
		f.sender.settleLocal(originalRecord, errEndpointRevoked)
		f.sender.pending = f.sender.reserve(nil)
		require.Same(t, originalRecord, f.sender.pending, "exercise reuse of the exact record address")
		require.NotEqual(t, originalIdentity, f.sender.pending.identity)
		require.Same(t, buffer, originalPlan.buffer, "the unhanded plan belongs to construction, not the dropped group")
		require.NotZero(t, originalPlan.entries[0].protection)
	}}
	_, err := f.conn.emission.appendNativePacket(buffer, 1200, false, protocol.ECNNon, f.now, &construction)
	require.NoError(t, err)
	replacement := f.sender.pending
	require.Len(t, replacement.packets, 1, "registration resolves the fresh pending group")
	require.False(t, construction.matches())
	f.conn.emission.handoff(buffer, 0, protocol.ECNNon, sendMetadata{}, &construction)
	require.Nil(t, construction.plan)
	require.Nil(t, originalPlan.buffer)
	require.Len(t, replacement.request.members, 1)
	require.Nil(t, replacement.request.members[0].protection, "stale ticket cannot attach a plan to a replacement group")
	require.Equal(t, plaintext[int(recipe.payloadOffset):len(plaintext)-16], nativeProtectionEmissionPlaintext(t, f.opener, buffer.Data, int(recipe.payloadOffset), int(recipe.pnLen), 0))
	f.sender.settleLocal(f.sender.pending, errEndpointRevoked)
	require.Zero(t, buffer.refCount)
	require.Nil(t, f.sender.pending)
}

func TestNativeProtectionEmissionLaterSealErrorReleasesPrefix(t *testing.T) {
	f := newNativeProtectionEmissionFixture(t)
	buffer, plan := observeNativeProtectionEmissionStorage(t, true)
	wantError := errors.New("later synchronous sealer refusal")
	wrapper := &nativeProtectionEmissionObservedSealer{ShortHeaderSealer: f.sealer, fail: wantError}
	sealing := NewMockSealingManager(gomock.NewController(t))
	gomock.InOrder(sealing.EXPECT().Get1RTTSealer().Return(f.sealer, nil), sealing.EXPECT().Get1RTTSealer().Return(wrapper, nil))
	f.conn.emission.packer.cryptoSetup = sealing
	framer := NewMockFrameSource(gomock.NewController(t))
	framer.EXPECT().HasData().Return(true).AnyTimes()
	var frames []*nativeProtectionFrameHandler
	framer.EXPECT().Append(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(control []ackhandler.Frame, streams []ackhandler.StreamFrame, maxSize protocol.ByteCount, _ monotime.Time, version protocol.Version) ([]ackhandler.Frame, []ackhandler.StreamFrame, protocol.ByteCount) {
			frame := &wire.StreamFrame{StreamID: 0, DataLenPresent: true}
			frame.Data = make([]byte, frame.MaxDataLen(maxSize, version))
			handler := &nativeProtectionFrameHandler{}
			frames = append(frames, handler)
			return control, append(streams, ackhandler.StreamFrame{Frame: frame, Handler: handler}), frame.Length(version)
		},
	).Times(2)
	f.conn.emission.packer.framer = framer
	result := f.conn.emission.withGSO(f.now)
	require.ErrorIs(t, result.err, wantError)
	require.False(t, result.progress)
	require.Equal(t, 1, wrapper.seals)
	next, _ := f.sender.recovery.PeekPacketNumber(protocol.Encryption1RTT)
	require.EqualValues(t, 1, next, "only the reserved prefix consumes a PN")
	require.Empty(t, f.sender.pending.members)
	require.Len(t, f.sender.pending.packets, 1)
	requireNativeProtectionEmissionReleased(t, *buffer, *plan)
	f.sender.finish() // the existing partial-buffer disposition settles its prefix
	require.Nil(t, f.sender.pending)
	require.Len(t, frames, 2)
	require.Equal(t, 1, frames[0].lost)
	require.Zero(t, frames[1].lost, "failed unregistered packet has no completion token")
}

// Policy callbacks have no raw-byte input; the fixture captures exclusively owned
// bytes only to verify the ticket's independent lifetime across receiver changes.
type nativeProtectionTestPolicy struct {
	emissionPolicy
	onLog, onActivity func()
}

func (p nativeProtectionTestPolicy) logPreparedShortHeaderPacket(ackhandler.PreparedPacketView, nativeHeaderFacts, protocol.ECN, protocol.ByteCount) {
	p.onLog()
}

func (p nativeProtectionTestPolicy) notePreparedEmissionActivity(ackhandler.PreparedPacketView, monotime.Time) {
	p.onActivity()
}

type nativeProtectionFrameHandler struct{ lost int }

func (*nativeProtectionFrameHandler) OnAcked(wire.Frame)  {}
func (h *nativeProtectionFrameHandler) OnLost(wire.Frame) { h.lost++ }

func TestNativeProtectionConstructionInvalidGeometryDoesNotReserve(t *testing.T) {
	f := newNativeProtectionEmissionFixture(t)
	buffer := getPacketBuffer()
	defer buffer.Release()
	buffer.Data = buffer.Data[:10]
	var construction nativeProtectionConstruction
	construction.begin(&f.conn.emission, buffer)
	defer construction.discard()
	deferred, err := construction.reserve(f.sealer, 19, 0, 9, 2, 25) // end excludes one required tag byte
	require.NoError(t, err)
	require.False(t, deferred)
	require.Nil(t, construction.plan)
	require.Equal(t, protocol.InvalidPacketNumber, f.sealer.(interface{ FirstPacketNumber() protocol.PacketNumber }).FirstPacketNumber())
	require.True(t, construction.disabled)
}

func TestNativeProtectionEmissionFIPSFallback(t *testing.T) {
	if !fips140.Enabled() {
		t.Skip("requires actual FIPS constructors")
	}
	f := newNativeProtectionEmissionFixtureMode(t, false)
	sealing := NewMockSealingManager(gomock.NewController(t))
	sealing.EXPECT().Get1RTTSealer().Return(f.sealer, nil)
	f.conn.emission.packer.cryptoSetup = sealing
	buffer := getPacketBuffer()
	defer buffer.Release()
	var construction nativeProtectionConstruction
	construction.begin(&f.conn.emission, buffer)
	defer construction.discard()
	_, err := f.conn.emission.appendNativePacket(buffer, 1200, false, protocol.ECNNon, f.now, &construction)
	require.NoError(t, err)
	require.Nil(t, construction.plan)
	require.True(t, construction.disabled)
	require.EqualValues(t, 0, f.sealer.(interface{ FirstPacketNumber() protocol.PacketNumber }).FirstPacketNumber())
	pnLen := protocol.PacketNumberLen2
	offset := int(wire.ShortHeaderLen(f.conn.connIDManager.Get(), pnLen))
	require.NotEmpty(t, nativeProtectionEmissionPlaintext(t, f.opener, buffer.Data, offset, int(pnLen), 0))
	for _, packet := range f.sender.pending.packets {
		require.NoError(t, f.sender.recovery.CompleteLocalPacket(packet.token, ackhandler.LocalPacketUnsubmitted, monotime.Now()))
	}
}

func TestNativeProtectionNaturalTurnUsesWriter(t *testing.T) {
	if fips140.Enabled() {
		t.Skip("requires the optional non-FIPS capability")
	}
	nativeOrdinaryTest(t, func(t *testing.T) {
		tc, sender, socket := nativeProbeRecoveryConnection(t, "reno")
		socket.hold = nativeProbeHoldAll
		sealer, opener := nativeProtectionTestCrypto(t, protocol.Version1)
		if !handshake.CanReservePacketProtection(sealer) {
			t.Skip("runtime primitives do not provide the immutable capability")
		}
		sealing := NewMockSealingManager(gomock.NewController(t))
		sealing.EXPECT().Get1RTTSealer().Return(sealer, nil).AnyTimes()
		tc.conn.emission.packer.cryptoSetup = sealing
		for _, payload := range []string{"first", "second", "third"} {
			require.NoError(t, tc.conn.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte(payload)}))
		}
		result := tc.conn.emission.send(monotime.Now(), true)
		require.NoError(t, result.err)
		require.True(t, result.progress)
		synctest.Wait()
		var token *endpointToken
		sender.endpoint.do(func(s *endpointState) { token = s.current })
		require.NotNil(t, token)
		require.Len(t, token.request.members, 3)
		for i, member := range token.request.members {
			require.NotNil(t, member.protection)
			if i == 0 {
				require.EqualValues(t, 1, member.protection.next)
			} else {
				require.Zero(t, member.protection.next)
			}
		}
		socket.release()
		synctest.Wait()
		require.NoError(t, sender.consume())
		require.Len(t, socket.writes, 3)
		for i, write := range socket.writes {
			raw := bytes.Clone(write.data)
			// Initial short-header PNs use two bytes. Authenticate through the actual
			// peer opener; the packet payload includes the queued DATAGRAM frame.
			offset := int(wire.ShortHeaderLen(tc.conn.connIDManager.Get(), protocol.PacketNumberLen2))
			require.NotEmpty(t, nativeProtectionEmissionPlaintext(t, opener, raw, offset, 2, protocol.PacketNumber(i)))
		}
	})
}
