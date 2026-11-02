package handshake

import (
	"crypto/cipher"
	"encoding/binary"
	"fmt"
	"math"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/internal/utils"
)

// pathProtection is a bounded view of the connection's shared key schedule.
// Cipher instances use the same traffic keys with a path-qualified IV. This
// preserves the suite's nonce checks, including the TLS FIPS implementation.
// Only the connection goroutine may use these views.
type pathProtection struct {
	id                                      protocol.PathID
	highest, firstCurrent                   protocol.PacketNumber
	current, previous, next, send, nextSend cipher.AEAD
	rtt                                     *utils.RTTStats
	retired                                 bool
}

type pathAEAD struct {
	owner      *updatableAEAD
	protection *pathProtection
}

// admitPath is a private fixture seam until R9 supplies authenticated membership.
// Lookups from packets never allocate. IDs cannot be reused after removal.
func (a *updatableAEAD) admitPath(id protocol.PathID, rtt *utils.RTTStats) error {
	if id == 0 || id <= a.largestAdmittedPath || rtt == nil || a.rcvAEAD == nil || a.sendAEAD == nil || a.version != protocol.Version1 {
		return fmt.Errorf("invalid crypto path admission")
	}
	live := 1
	if a.paths != nil {
		live = 0
		for _, p := range a.paths {
			if !p.retired {
				live++
			}
		}
	}
	if live >= 2 || len(a.paths) >= 6 {
		return fmt.Errorf("crypto path limit reached")
	}
	if a.paths == nil {
		a.paths = map[protocol.PathID]*pathProtection{0: {
			id: 0, highest: a.highestRcvdPN, firstCurrent: a.firstRcvdWithCurrentKey,
			current: a.rcvAEAD, previous: a.prevRcvAEAD, next: a.nextRcvAEAD, send: a.sendAEAD, nextSend: a.nextSendAEAD, rtt: a.rttStats,
		}}
	}
	a.paths[id] = &pathProtection{
		id: id, highest: protocol.InvalidPacketNumber, firstCurrent: protocol.InvalidPacketNumber,
		current:  createPathAEAD(a.suite, a.rcvTrafficSecret, a.version, id),
		next:     createPathAEAD(a.suite, a.nextRcvTrafficSecret, a.version, id),
		send:     createPathAEAD(a.suite, a.sendTrafficSecret, a.version, id),
		nextSend: createPathAEAD(a.suite, a.nextSendTrafficSecret, a.version, id), rtt: rtt,
	}
	a.largestAdmittedPath = id
	return nil
}

func (a *updatableAEAD) path(id protocol.PathID) (*pathAEAD, error) {
	p := a.paths[id]
	if p == nil {
		return nil, ErrKeysDropped
	}
	return &pathAEAD{owner: a, protection: p}, nil
}

func (a *updatableAEAD) retirePath(id protocol.PathID) error {
	p := a.paths[id]
	if p == nil || p.retired {
		return ErrKeysDropped
	}
	retained := 0
	for _, p := range a.paths {
		if p.retired {
			retained++
		}
	}
	if retained >= 4 {
		return fmt.Errorf("crypto retained path limit reached")
	}
	p.retired = true
	return nil
}

// Removal is called only after the registry's retention/fence obligations end.
func (a *updatableAEAD) removePath(id protocol.PathID) error {
	p := a.paths[id]
	if p == nil || !p.retired || id == 0 {
		return fmt.Errorf("invalid crypto path removal")
	}
	delete(a.paths, id)
	return nil
}

func (p *pathAEAD) DecodePacketNumber(pn protocol.PacketNumber, n protocol.PacketNumberLen) protocol.PacketNumber {
	return protocol.DecodePacketNumber(n, p.protection.highest, pn)
}

func (p *pathAEAD) Open(dst, src []byte, now monotime.Time, pn protocol.PacketNumber, kp protocol.KeyPhaseBit, ad []byte) ([]byte, error) {
	if p.owner.paths[p.protection.id] != p.protection {
		return nil, ErrKeysDropped
	}
	return p.owner.openPath(p.protection, dst, src, now, pn, kp, ad)
}

func (p *pathAEAD) Seal(dst, src []byte, pn protocol.PacketNumber, ad []byte) []byte {
	if p.owner.paths[p.protection.id] != p.protection || p.protection.retired {
		panic("sealing on inactive crypto path")
	}
	return p.owner.sealPath(p.protection, dst, src, pn, ad)
}
func (p *pathAEAD) KeyPhase() protocol.KeyPhaseBit        { return p.owner.KeyPhase() }
func (p *pathAEAD) KeyGeneration() protocol.KeyPhase      { return p.owner.keyPhase }
func (a *updatableAEAD) KeyGeneration() protocol.KeyPhase { return a.keyPhase }
func (p *pathAEAD) Overhead() int                         { return p.owner.Overhead() }
func (p *pathAEAD) EncryptHeader(sample []byte, first *byte, pn []byte) {
	p.owner.EncryptHeader(sample, first, pn)
}

func (p *pathAEAD) DecryptHeader(sample []byte, first *byte, pn []byte) {
	p.owner.DecryptHeader(sample, first, pn)
}

func (a *updatableAEAD) sealPath(p *pathProtection, dst, src []byte, pn protocol.PacketNumber, ad []byte) []byte {
	if p.retired {
		panic("sealing on retired crypto path")
	}
	if a.firstSentWithCurrentKey == protocol.InvalidPacketNumber {
		a.firstSentWithCurrentKey = pn
	}
	if a.firstPacketNumber == protocol.InvalidPacketNumber {
		a.firstPacketNumber = pn
	}
	a.numSentWithCurrentKey++
	binary.BigEndian.PutUint64(a.nonceBuf, uint64(pn))
	return p.send.Seal(dst, a.nonceBuf, src, ad)
}

func (a *updatableAEAD) trial(key cipher.AEAD, dst, src, ad []byte) ([]byte, error) {
	dec, err := key.Open(dst, a.nonceBuf, src, ad)
	if err == nil {
		return dec, nil
	}
	a.invalidPacketCount++
	if a.invalidPacketCount >= a.invalidPacketLimit {
		return nil, &qerr.TransportError{ErrorCode: qerr.AEADLimitReached}
	}
	return nil, ErrDecryptionFailed
}

func (a *updatableAEAD) openPath(p *pathProtection, dst, src []byte, now monotime.Time, pn protocol.PacketNumber, kp protocol.KeyPhaseBit, ad []byte) ([]byte, error) {
	if a.prevRcvAEAD != nil && !a.prevRcvAEADExpiry.IsZero() && !now.Before(a.prevRcvAEADExpiry) {
		a.prevRcvAEAD = nil
		a.prevRcvAEADExpiry = 0
		for _, path := range a.paths {
			path.previous = nil
		}
	}
	binary.BigEndian.PutUint64(a.nonceBuf, uint64(pn))
	if kp == a.keyPhase.Bit() {
		dec, err := a.trial(p.current, dst, src, ad)
		if err != nil {
			return nil, err
		}
		a.authenticatedCurrent(p, pn, now)
		return dec, nil
	}
	// A known boundary disambiguates reordered old packets. An unknown
	// boundary admits at most two authenticated trials; failures share one limit.
	unknown := p.firstCurrent == protocol.InvalidPacketNumber
	if a.keyPhase > 0 && (unknown || pn < p.firstCurrent) {
		if p.previous != nil {
			trialDst := dst
			if unknown {
				trialDst = nil
			} // Open may overwrite dst even on authentication failure.
			dec, err := a.trial(p.previous, trialDst, src, ad)
			if err == nil {
				if unknown {
					dec = append(dst, dec...)
				}
				p.highest = max(p.highest, pn)
				return dec, nil
			}
			if err != ErrDecryptionFailed {
				return nil, err
			}
			if !unknown {
				return nil, err
			}
		} else if !unknown {
			return nil, ErrKeysDropped
		}
	}
	dec, err := a.trial(p.next, dst, src, ad)
	if err != nil {
		return nil, err
	}
	if a.keyPhase > 0 && a.firstSentWithCurrentKey == protocol.InvalidPacketNumber {
		return nil, &qerr.TransportError{ErrorCode: qerr.KeyUpdateError, ErrorMessage: "keys updated too quickly"}
	}
	a.rollKeys()
	a.startKeyDropTimer(now)
	a.authenticatedCurrent(p, pn, now)
	return dec, nil
}

func (a *updatableAEAD) authenticatedCurrent(p *pathProtection, pn protocol.PacketNumber, now monotime.Time) {
	p.highest = max(p.highest, pn)
	if p.firstCurrent == protocol.InvalidPacketNumber {
		p.firstCurrent = pn
	}
	if a.numRcvdWithCurrentKey == 0 && a.keyPhase > 0 && a.prevRcvAEADExpiry.IsZero() {
		a.startKeyDropTimer(now)
	}
	a.numRcvdWithCurrentKey++
}

func (a *updatableAEAD) keyRetentionDuration() time.Duration {
	pto := a.rttStats.PTO(true)
	for _, p := range a.paths {
		pto = max(pto, p.rtt.PTO(true))
	}
	if pto > time.Duration(math.MaxInt64)/3 {
		return time.Duration(math.MaxInt64)
	}
	return 3 * pto
}

func saturatingKeyDeadline(now monotime.Time, d time.Duration) monotime.Time {
	if int64(now) > math.MaxInt64-int64(d) {
		return monotime.Time(math.MaxInt64)
	}
	return now.Add(d)
}

func (a *updatableAEAD) SetAcked(receipt protocol.ApplicationKeyReceipt, now monotime.Time) error {
	if !receipt.Valid {
		return fmt.Errorf("missing acknowledged transmission")
	}
	if a.paths != nil && a.paths[receipt.PathID] == nil {
		return ErrKeysDropped
	}
	if receipt.KeyGeneration > a.keyPhase {
		return &qerr.TransportError{ErrorCode: qerr.KeyUpdateError, ErrorMessage: "ACK for future key generation"}
	}
	if receipt.KeyGeneration != a.keyPhase {
		return nil
	}
	if a.paths == nil {
		return a.SetLargestAcked(receipt.PacketNumber)
	}
	if a.numRcvdWithCurrentKey == 0 && a.firstSentWithCurrentKey != protocol.InvalidPacketNumber {
		return &qerr.TransportError{ErrorCode: qerr.KeyUpdateError, ErrorMessage: "received ACK for current key generation before peer updated keys"}
	}
	if !a.updateConfirmed {
		a.updateConfirmed = true
		if a.paths != nil {
			a.nextUpdateTime = saturatingKeyDeadline(now, a.keyRetentionDuration())
		}
	}
	// Legacy path-zero update scheduling also uses the concrete sent generation.
	a.largestAcked = max(a.largestAcked, receipt.PacketNumber)
	return nil
}

// ShortHeaderOpenerForPath resolves only previously admitted crypto identities.
func ShortHeaderOpenerForPath(opener ShortHeaderOpener, id protocol.PathID) (ShortHeaderOpener, error) {
	if id == 0 {
		return opener, nil
	}
	a, ok := opener.(*updatableAEAD)
	if !ok {
		return nil, ErrKeysDropped
	}
	return a.path(id)
}

func ShortHeaderSealerForPath(sealer ShortHeaderSealer, id protocol.PathID) (ShortHeaderSealer, error) {
	if id == 0 {
		return sealer, nil
	}
	a, ok := sealer.(*updatableAEAD)
	if !ok {
		return nil, ErrKeysDropped
	}
	p, err := a.path(id)
	if err != nil {
		return nil, err
	}
	if p.protection.retired {
		return nil, ErrKeysDropped
	}
	return p, nil
}
