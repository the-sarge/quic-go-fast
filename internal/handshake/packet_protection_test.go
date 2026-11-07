package handshake

import (
	"bytes"
	"crypto/fips140"
	"crypto/tls"
	"fmt"
	"testing"
	"unsafe"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/quic-go/quic-go/testutils/events"
	"github.com/stretchr/testify/require"
)

func packetProtectionTestOwner(t *testing.T, suite uint16, version protocol.Version, recorder qlogwriter.Recorder) *updatableAEAD {
	t.Helper()
	if fips140.Enabled() {
		t.Skip("deferred packet protection declines FIPS primitives")
	}
	a := newUpdatableAEAD(utils.NewRTTStats(), recorder, utils.DefaultLogger, version)
	cs := getCipherSuite(suite)
	a.SetReadKey(cs, bytes.Repeat([]byte{0x5a}, cs.Hash.Size()))
	a.SetWriteKey(cs, bytes.Repeat([]byte{0xa5}, cs.Hash.Size()))
	return a
}

// Both lanes start with the final tag space already reserved. The synchronous
// oracle still excludes that space from its plaintext, like encryptPacket.
func packetProtectionTestRaw(t *testing.T, pn protocol.PacketNumber, pnLen protocol.PacketNumberLen, kp protocol.KeyPhaseBit, cidLen, payloadLen int) ([]byte, int) {
	t.Helper()
	raw, err := wire.AppendShortHeader(nil, protocol.ParseConnectionID(bytes.Repeat([]byte{0x31}, cidLen)), pn, pnLen, kp)
	require.NoError(t, err)
	payloadOffset := len(raw)
	for i := range max(payloadLen, 4-int(pnLen)) {
		raw = append(raw, byte(i*17+3))
	}
	return append(raw, make([]byte, 16)...), payloadOffset
}

func packetProtectionTestScalar(t *testing.T, a ShortHeaderSealer, raw []byte, payloadOffset, pnLen int, pn protocol.PacketNumber) []byte {
	t.Helper()
	raw = bytes.Clone(raw)
	_, err := a.Seal(raw[payloadOffset:payloadOffset], raw[payloadOffset:len(raw)-16], pn, raw[:payloadOffset])
	require.NoError(t, err)
	pnOffset := payloadOffset - pnLen
	a.EncryptHeader(raw[pnOffset+4:pnOffset+4+16], &raw[0], raw[pnOffset:payloadOffset])
	return raw
}

func TestPacketProtectionReservationBytes(t *testing.T) {
	for _, suite := range []uint16{tls.TLS_AES_128_GCM_SHA256, tls.TLS_AES_256_GCM_SHA384, tls.TLS_CHACHA20_POLY1305_SHA256} {
		for _, version := range []protocol.Version{protocol.Version1, protocol.Version2} {
			t.Run(fmt.Sprintf("%s/%s", tls.CipherSuiteName(suite), version), func(t *testing.T) {
				scalar := packetProtectionTestOwner(t, suite, version, nil)
				deferred := packetProtectionTestOwner(t, suite, version, nil)
				peer := newUpdatableAEAD(utils.NewRTTStats(), nil, utils.DefaultLogger, version)
				cs := getCipherSuite(suite)
				peer.SetReadKey(cs, bytes.Repeat([]byte{0xa5}, cs.Hash.Size()))
				var scratch PacketProtectionScratch
				var pn protocol.PacketNumber = 0x1234
				for _, pnLen := range []protocol.PacketNumberLen{1, 2, 3, 4} {
					for _, cidLen := range []int{0, 8} {
						for _, payloadLen := range []int{0, 1, 77, 1130} {
							raw, offset := packetProtectionTestRaw(t, pn, pnLen, protocol.KeyPhaseZero, cidLen, payloadLen)
							plain := bytes.Clone(raw)
							want := packetProtectionTestScalar(t, scalar, raw, offset, int(pnLen), pn)
							job, ok, err := TryReservePacketProtection(deferred, pn)
							require.NoError(t, err)
							require.True(t, ok)
							require.Equal(t, plain, raw, "reservation does not transform bytes")
							guarded := append(bytes.Repeat([]byte{0xcc}, 19), raw...)
							guarded = append(guarded, bytes.Repeat([]byte{0xdd}, 23)...)
							packet := guarded[19 : 19+len(raw) : 19+len(raw)]
							job.Protect(packet, offset, int(pnLen), &scratch)
							require.Equal(t, want, packet)
							require.Equal(t, bytes.Repeat([]byte{0xcc}, 19), guarded[:19])
							require.Equal(t, bytes.Repeat([]byte{0xdd}, 23), guarded[19+len(raw):])
							opened := bytes.Clone(packet)
							pnOffset := offset - int(pnLen)
							peer.DecryptHeader(opened[pnOffset+4:pnOffset+20], &opened[0], opened[pnOffset:offset])
							require.Equal(t, plain[:offset], opened[:offset])
							payload, err := peer.Open(nil, opened[offset:], monotime.Time(1), pn, protocol.KeyPhaseZero, opened[:offset])
							require.NoError(t, err)
							require.True(t, bytes.Equal(plain[offset:len(plain)-16], payload), "authenticated plaintext matches, including empty payload")
							pn++
						}
					}
				}
				require.Equal(t, scalar.numSentWithCurrentKey, deferred.numSentWithCurrentKey)
			})
		}
	}
}

func TestPacketProtectionReservationChaChaVector(t *testing.T) {
	for _, tc := range []struct {
		version protocol.Version
		want    string
	}{
		{protocol.Version1, "4cfe4189655e5cd55c41f69080575d7999c25a5bfb"},
		{protocol.Version2, "5558b1c60ae7b6b932bc27d786f4bc2bb20f2162ba"},
	} {
		t.Run(tc.version.String(), func(t *testing.T) {
			a := packetProtectionTestOwner(t, tls.TLS_CHACHA20_POLY1305_SHA256, tc.version, nil)
			secret := splitHexString(t, "9ac312a7f877468ebe69422748ad00a1 5443f18203a07d6060f688f30f21632b")
			a.SetWriteKey(getCipherSuite(tls.TLS_CHACHA20_POLY1305_SHA256), secret)
			job, ok, err := TryReservePacketProtection(a, 654360564)
			require.NoError(t, err)
			require.True(t, ok)
			raw := append(splitHexString(t, "4200bff401"), make([]byte, 16)...)
			var scratch PacketProtectionScratch
			job.Protect(raw, 4, 3, &scratch)
			require.Equal(t, splitHexString(t, tc.want), raw)
		})
	}
}

func TestPacketProtectionReservationPolicy(t *testing.T) {
	setKeyUpdateIntervals(t, 3, 5)
	var scalarEvents, deferredEvents events.Recorder
	scalar := packetProtectionTestOwner(t, tls.TLS_AES_128_GCM_SHA256, protocol.Version1, &scalarEvents)
	deferred := packetProtectionTestOwner(t, tls.TLS_AES_128_GCM_SHA256, protocol.Version1, &deferredEvents)
	scalar.SetHandshakeConfirmed()
	deferred.SetHandshakeConfirmed()
	var scratch PacketProtectionScratch
	for pn := protocol.PacketNumber(7); pn < 19; pn++ {
		phase := scalar.KeyPhase()
		require.Equal(t, phase, deferred.KeyPhase())
		raw, offset := packetProtectionTestRaw(t, pn, 2, phase, 8, 19)
		want := packetProtectionTestScalar(t, scalar, raw, offset, 2, pn)
		job, ok, err := TryReservePacketProtection(deferred, pn)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, scalar.FirstPacketNumber(), deferred.FirstPacketNumber())
		require.Equal(t, scalar.firstSentWithCurrentKey, deferred.firstSentWithCurrentKey)
		require.Equal(t, scalar.numSentWithCurrentKey, deferred.numSentWithCurrentKey)
		require.Equal(t, scalarEvents.Events(), deferredEvents.Events())
		if pn == 7 { // canceled after reservation: commitment is not refunded
			continue
		}
		count := deferred.numSentWithCurrentKey
		job.Protect(raw, offset, 2, &scratch)
		require.Equal(t, want, raw)
		require.Equal(t, count, deferred.numSentWithCurrentKey)
		require.Equal(t, scalarEvents.Events(), deferredEvents.Events())
	}
	require.EqualValues(t, 7, deferred.FirstPacketNumber())
	require.EqualValues(t, 1, deferred.keyPhase, "no next update without a current-generation ACK")
	// Neither lane has authenticated a peer packet in this phase.
	require.Equal(t, scalar.SetLargestAcked(18), deferred.SetLargestAcked(18))
}

func TestPacketProtectionReservationEncryptionLimit(t *testing.T) {
	for _, suite := range []uint16{tls.TLS_AES_128_GCM_SHA256, tls.TLS_AES_256_GCM_SHA384} {
		t.Run(tls.CipherSuiteName(suite), func(t *testing.T) {
			a := packetProtectionTestOwner(t, suite, protocol.Version1, nil)
			a.numSentWithCurrentKey = 1<<23 - 1
			_, ok, err := TryReservePacketProtection(a, 41)
			require.NoError(t, err)
			require.True(t, ok)
			job, ok, err := TryReservePacketProtection(a, 42)
			require.True(t, ok)
			require.Equal(t, a.checkEncryptionLimit(), err)
			require.Zero(t, job)
			require.EqualValues(t, 1<<23, a.numSentWithCurrentKey)
			require.EqualValues(t, 41, a.FirstPacketNumber())
			require.EqualValues(t, 41, a.firstSentWithCurrentKey)
		})
	}
}

type packetProtectionWrappedSealer struct{ ShortHeaderSealer }

func TestPacketProtectionReservationDecline(t *testing.T) {
	for _, name := range []string{"wrapper", "nil", "typed nil", "paths", "unknown AEAD", "unknown header", "mixed cipher", "long header", "changed cached AEAD", "changed cached header"} {
		t.Run(name, func(t *testing.T) {
			a := packetProtectionTestOwner(t, tls.TLS_AES_128_GCM_SHA256, protocol.Version1, nil)
			var sealer ShortHeaderSealer = a
			switch name {
			case "wrapper":
				sealer = packetProtectionWrappedSealer{a}
			case "nil":
				sealer = nil
			case "typed nil":
				sealer = (*updatableAEAD)(nil)
			case "paths":
				a.paths = make(map[protocol.PathID]*pathProtection)
			case "unknown AEAD":
				known := a.sendAEAD.(*xorNonceAEAD)
				a.sendAEAD = &xorNonceAEAD{aead: known.aead, nonceMask: known.nonceMask}
			case "unknown header":
				known := a.headerEncrypter.(*aesHeaderProtector)
				a.headerEncrypter = &aesHeaderProtector{block: known.block}
			case "mixed cipher":
				a.headerEncrypter = newHeaderProtector(getCipherSuite(tls.TLS_CHACHA20_POLY1305_SHA256), []byte("different cipher"), false, protocol.Version1)
			case "long header":
				a.headerEncrypter = newHeaderProtector(a.suite, []byte("long header"), true, protocol.Version1)
			case "changed cached AEAD", "changed cached header":
				_, ok, err := TryReservePacketProtection(a, 1)
				require.NoError(t, err)
				require.True(t, ok)
				if name == "changed cached AEAD" {
					a.sendAEAD = &xorNonceAEAD{}
				} else {
					a.headerEncrypter = &aesHeaderProtector{}
				}
			}
			count, first, current := a.numSentWithCurrentKey, a.firstPacketNumber, a.firstSentWithCurrentKey
			cache := a.packetProtection
			require.False(t, CanReservePacketProtection(sealer))
			require.Same(t, cache, a.packetProtection, "capability probe does not change even a stale cache")
			job, ok, err := TryReservePacketProtection(sealer, 19)
			require.NoError(t, err)
			require.False(t, ok)
			require.Zero(t, job)
			require.Equal(t, count, a.numSentWithCurrentKey)
			require.Equal(t, first, a.firstPacketNumber)
			require.Equal(t, current, a.firstSentWithCurrentKey)
		})
	}
}

func TestPacketProtectionReservationFIPSFallback(t *testing.T) {
	if !fips140.Enabled() {
		t.Skip("FIPS constructor is selected only in the FIPS validation process")
	}
	a := newUpdatableAEAD(utils.NewRTTStats(), nil, utils.DefaultLogger, protocol.Version1)
	a.SetWriteKey(getCipherSuite(tls.TLS_AES_128_GCM_SHA256), bytes.Repeat([]byte{0x44}, 32))
	job, ok, err := TryReservePacketProtection(a, 19)
	require.NoError(t, err)
	require.False(t, ok)
	require.Zero(t, job)
	require.Zero(t, a.numSentWithCurrentKey)
	_, err = a.Seal(nil, []byte("fallback"), 19, []byte("header"))
	require.NoError(t, err)
	require.EqualValues(t, 1, a.numSentWithCurrentKey)
}

func TestPacketProtectionReservationConcurrentOldGeneration(t *testing.T) {
	for _, suite := range []uint16{tls.TLS_AES_128_GCM_SHA256, tls.TLS_AES_256_GCM_SHA384, tls.TLS_CHACHA20_POLY1305_SHA256} {
		t.Run(tls.CipherSuiteName(suite), func(t *testing.T) {
			a := packetProtectionTestOwner(t, suite, protocol.Version1, nil)
			oracle := packetProtectionTestOwner(t, suite, protocol.Version1, nil)
			peer := packetProtectionTestOwner(t, suite, protocol.Version1, nil)
			peer.SetWriteKey(getCipherSuite(suite), bytes.Repeat([]byte{0x5a}, getCipherSuite(suite).Hash.Size()))
			incoming := sealForTest(t, peer, nil, []byte("receive concurrently"), 1, []byte("header"))
			type pending struct {
				job       PacketProtection
				raw, want []byte
				offset    int
			}
			var jobs []pending
			for pn := range protocol.PacketNumber(67) {
				raw, offset := packetProtectionTestRaw(t, pn, 2, protocol.KeyPhaseZero, 8, 1100)
				want := packetProtectionTestScalar(t, oracle, raw, offset, 2, pn)
				job, ok, err := TryReservePacketProtection(a, pn)
				require.NoError(t, err)
				require.True(t, ok)
				jobs = append(jobs, pending{job: job, raw: raw, want: want, offset: offset})
			}
			start, done := make(chan struct{}), make(chan struct{})
			go func() {
				defer close(done)
				<-start
				var scratch PacketProtectionScratch
				for i := range jobs[:len(jobs)-1] {
					jobs[i].job.Protect(jobs[i].raw, jobs[i].offset, 2, &scratch)
				}
			}()
			defer func() { <-done }()
			close(start)
			for pn := protocol.PacketNumber(67); pn < 134; pn++ {
				if pn == 90 {
					a.rollKeys()
					oracle.rollKeys()
					require.Nil(t, a.packetProtection, "rotation releases the owner's old cached view")
				}
				raw, offset := packetProtectionTestRaw(t, pn, 2, a.keyPhase.Bit(), 8, 1100)
				got := packetProtectionTestScalar(t, a, raw, offset, 2, pn)
				want := packetProtectionTestScalar(t, oracle, raw, offset, 2, pn)
				require.Equal(t, want, got)
				opened, err := a.Open(nil, incoming, monotime.Time(1), 1, protocol.KeyPhaseZero, []byte("header"))
				require.NoError(t, err)
				require.Equal(t, []byte("receive concurrently"), opened)
			}
			<-done
			for i := range jobs[:len(jobs)-1] {
				require.Equal(t, jobs[i].want, jobs[i].raw)
			}
			// Even after the owner no longer retains old receive or cached send
			// keys, the outstanding immutable reservation retains its own key.
			a.prevRcvAEAD = nil
			a.prevRcvTrafficSecret = nil
			a.clearPacketProtection()
			last := &jobs[len(jobs)-1]
			var scratch PacketProtectionScratch
			last.job.Protect(last.raw, last.offset, 2, &scratch)
			require.Equal(t, last.want, last.raw)
		})
	}
}

func TestPacketProtectionLayout(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("the recorded layout is for 64-bit targets")
	}
	require.Equal(t, uintptr(16), unsafe.Sizeof(PacketProtection{}))
	require.Equal(t, uintptr(28), unsafe.Sizeof(PacketProtectionScratch{}))
	require.Equal(t, uintptr(32), unsafe.Sizeof(xorNonceAEAD{}))
	require.Equal(t, uintptr(40), unsafe.Sizeof(aesHeaderProtector{}))
	require.Equal(t, uintptr(39), unsafe.Sizeof(chachaHeaderProtector{}))
	t.Logf("bytes: owner=%d immutable_key=%d handle=%d scratch=%d xor=%d AES-header=%d ChaCha-header=%d", unsafe.Sizeof(updatableAEAD{}), unsafe.Sizeof(packetProtectionKey{}), unsafe.Sizeof(PacketProtection{}), unsafe.Sizeof(PacketProtectionScratch{}), unsafe.Sizeof(xorNonceAEAD{}), unsafe.Sizeof(aesHeaderProtector{}), unsafe.Sizeof(chachaHeaderProtector{}))
}

func TestPacketProtectionCapabilityProbe(t *testing.T) {
	for _, suite := range []uint16{tls.TLS_AES_128_GCM_SHA256, tls.TLS_AES_256_GCM_SHA384, tls.TLS_CHACHA20_POLY1305_SHA256} {
		t.Run(tls.CipherSuiteName(suite), func(t *testing.T) {
			a := packetProtectionTestOwner(t, suite, protocol.Version1, nil)
			require.True(t, CanReservePacketProtection(a))
			require.Nil(t, a.packetProtection, "probe does not allocate/populate the lazy cache")
			require.Zero(t, a.numSentWithCurrentKey)
			require.Equal(t, protocol.InvalidPacketNumber, a.firstSentWithCurrentKey)
			require.Equal(t, protocol.InvalidPacketNumber, a.firstPacketNumber)
		})
	}
}
