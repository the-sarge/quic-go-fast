package handshake

import (
	"crypto/cipher"
	"encoding/binary"

	"golang.org/x/crypto/chacha20"

	"github.com/quic-go/quic-go/internal/protocol"
)

// packetProtectionKind is minted only by the built-in cipher constructors.
// A matching algorithm name on an arbitrary interface is not sufficient.
type packetProtectionKind uint8

const (
	packetProtectionUnsupported packetProtectionKind = iota
	packetProtectionAES128
	packetProtectionAES256
	packetProtectionChaCha
)

// packetProtectionKey contains no connection, sealer, or mutable scratch state.
// The constructors prove these exact primitives support concurrent calls with
// independent buffers. The nonce-XOR and header-protection wrappers do not.
type packetProtectionKey struct {
	aead      cipher.AEAD
	iv        [aeadNonceLength]byte
	kind      packetProtectionKind
	headerAES cipher.Block
	headerKey [32]byte
}

// PacketProtection is an immutable reservation of one packet's key and number.
// The caller must consume its packet number at the original sealing point even
// if the associated native send is later canceled. Protect must run at most once
// on its plaintext buffer; retries reuse the resulting ciphertext.
type PacketProtection struct {
	key          *packetProtectionKey
	packetNumber protocol.PacketNumber
}

// PacketProtectionScratch belongs to one writer, never to a key generation.
// Reusing it across sequential transforms avoids per-packet nonce and mask
// allocations through the cipher interfaces. Concurrent writers need their own.
type PacketProtectionScratch struct {
	nonce [aeadNonceLength]byte
	mask  [16]byte
}

func (a *updatableAEAD) clearPacketProtection() {
	a.packetProtection = nil
	a.packetProtectionAEAD = nil
	a.packetProtectionHeader = nil
}

// packetProtectionPrimitives is a pure capability probe. It invokes no methods
// on supplied interfaces and does not allocate or populate the owner's cache.
func packetProtectionPrimitives(aead cipher.AEAD, header headerProtector) (*xorNonceAEAD, *aesHeaderProtector, *chachaHeaderProtector) {
	wrapped, ok := aead.(*xorNonceAEAD)
	if !ok || wrapped == nil || wrapped.protectionKind == packetProtectionUnsupported {
		return nil, nil, nil
	}
	switch hp := header.(type) {
	case *aesHeaderProtector:
		if hp == nil || hp.isLongHeader || hp.protectionKind != wrapped.protectionKind ||
			(wrapped.protectionKind != packetProtectionAES128 && wrapped.protectionKind != packetProtectionAES256) {
			return nil, nil, nil
		}
		return wrapped, hp, nil
	case *chachaHeaderProtector:
		if hp == nil || hp.isLongHeader || hp.protectionKind != packetProtectionChaCha || wrapped.protectionKind != packetProtectionChaCha {
			return nil, nil, nil
		}
		return wrapped, nil, hp
	default:
		return nil, nil, nil
	}
}

func newPacketProtectionKey(aead cipher.AEAD, header headerProtector) (*packetProtectionKey, *xorNonceAEAD) {
	wrapped, aesHP, chachaHP := packetProtectionPrimitives(aead, header)
	if wrapped == nil {
		return nil, nil
	}
	key := &packetProtectionKey{aead: wrapped.aead, iv: wrapped.nonceMask, kind: wrapped.protectionKind}
	if aesHP != nil {
		key.headerAES = aesHP.block
	} else {
		key.headerKey = chachaHP.key
	}
	return key, wrapped
}

// CanReservePacketProtection probes capability before an unsupported sealer can
// observe a deferred prefix. It performs no policy, cache, allocation or sealer
// method work. TryReservePacketProtection still resolves the live capability at
// the original Seal point after all intervening connection-side callbacks.
func CanReservePacketProtection(sealer ShortHeaderSealer) bool {
	a, ok := sealer.(*updatableAEAD)
	if !ok || a == nil || a.paths != nil {
		return false
	}
	wrapped, _, _ := packetProtectionPrimitives(a.sendAEAD, a.headerEncrypter)
	return wrapped != nil
}

// TryReservePacketProtection commits ordinary 1-RTT encryption policy at the
// original Seal call. It does not choose a key phase or consume the caller's PN.
// Unsupported, wrapped, FIPS and path sealers decline without policy mutation.
// A supported sealer can return its normal encryption-limit error (ok is true).
// Successful reservations retain only an immutable transform, independent of
// subsequent key updates, receive processing and synchronous fallback sealing.
func TryReservePacketProtection(sealer ShortHeaderSealer, pn protocol.PacketNumber) (protection PacketProtection, ok bool, err error) {
	a, concrete := sealer.(*updatableAEAD)
	if !concrete || a == nil || a.paths != nil {
		return PacketProtection{}, false, nil
	}
	key := a.packetProtection
	if key == nil || a.sendAEAD != a.packetProtectionAEAD || a.headerEncrypter != a.packetProtectionHeader {
		var source *xorNonceAEAD
		key, source = newPacketProtectionKey(a.sendAEAD, a.headerEncrypter)
		if key == nil {
			return PacketProtection{}, false, nil
		}
		a.packetProtection, a.packetProtectionAEAD, a.packetProtectionHeader = key, source, a.headerEncrypter
	}
	if err := a.checkEncryptionLimit(); err != nil {
		return PacketProtection{}, true, err
	}
	if a.firstSentWithCurrentKey == protocol.InvalidPacketNumber {
		a.firstSentWithCurrentKey = pn
	}
	if a.firstPacketNumber == protocol.InvalidPacketNumber {
		a.firstPacketNumber = pn
	}
	a.numSentWithCurrentKey++
	return PacketProtection{key: key, packetNumber: pn}, true, nil
}

// Protect transforms one exclusively owned, fully serialized short-header
// packet. raw already includes its reserved 16-byte tag; payloadOffset and pnLen
// describe the original unprotected header. The caller validates geometry before
// reservation and keeps this buffer exclusively borrowed until the transform
// and native write have returned. There are no policy callbacks or normal errors.
func (p PacketProtection) Protect(raw []byte, payloadOffset, pnLen int, scratch *PacketProtectionScratch) {
	key := p.key
	scratch.nonce = key.iv
	pn := uint64(p.packetNumber)
	for i := range 8 {
		scratch.nonce[11-i] ^= byte(pn)
		pn >>= 8
	}
	_ = key.aead.Seal(raw[payloadOffset:payloadOffset], scratch.nonce[:], raw[payloadOffset:len(raw)-16], raw[:payloadOffset])
	pnOffset := payloadOffset - pnLen
	sample := raw[pnOffset+4 : pnOffset+4+16]
	if key.kind == packetProtectionChaCha {
		clear(scratch.mask[:5])
		stream, err := chacha20.NewUnauthenticatedCipher(key.headerKey[:], sample[4:])
		if err != nil {
			panic(err) // fixed constructor key and validated 12-byte sample suffix
		}
		stream.SetCounter(binary.LittleEndian.Uint32(sample[:4]))
		stream.XORKeyStream(scratch.mask[:5], scratch.mask[:5])
	} else {
		key.headerAES.Encrypt(scratch.mask[:], sample)
	}
	raw[0] ^= scratch.mask[0] & 0x1f
	for i := range pnLen {
		raw[pnOffset+i] ^= scratch.mask[i+1]
	}
}
