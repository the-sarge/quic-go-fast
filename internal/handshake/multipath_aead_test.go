package handshake

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/tls"
	"encoding/hex"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/internal/utils"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
)

func TestMultipathNonceVector(t *testing.T) {
	// draft-ietf-quic-multipath-21 section 2.4: path 3, PN 54321.
	suite := getCipherSuite(tls.TLS_AES_128_GCM_SHA256)
	secret := []byte("independent nonce vector")
	key := hkdfExpandLabel(suite.Hash, secret, nil, hkdfLabelKeyV1, suite.KeyLen)
	iv := hkdfExpandLabel(suite.Hash, secret, nil, hkdfLabelIVV1, suite.IVLen())
	ppn, err := hex.DecodeString("00000003000000000000d431")
	require.NoError(t, err)
	for i := range iv {
		iv[i] ^= ppn[i]
	}
	block, err := aes.NewCipher(key)
	require.NoError(t, err)
	oracle, err := cipher.NewGCM(block)
	require.NoError(t, err)
	expected := oracle.Seal(nil, iv, []byte(msg), []byte(ad))
	a := createPathAEAD(suite, secret, protocol.Version1, 3)
	nonce, err := hex.DecodeString("000000000000d431")
	require.NoError(t, err)
	require.Equal(t, expected, a.Seal(nil, nonce, []byte(msg), []byte(ad)))
}

func TestMultipathIndependentPacketNumbers(t *testing.T) {
	client, server := multipathEndpoints(t)
	require.NoError(t, client.admitPath(3, utils.NewRTTStats()))
	require.NoError(t, server.admitPath(3, utils.NewRTTStats()))
	cp, err := client.path(3)
	require.NoError(t, err)
	sp, err := server.path(3)
	require.NoError(t, err)
	zero := sealForTest(t, client, nil, []byte(msg), 0x1337, []byte(ad))
	other := sealForTest(t, cp, nil, []byte(msg), 0x1337, []byte(ad))
	require.NotEqual(t, zero, other)
	_, err = server.Open(nil, zero, monotime.Now(), 0x1337, protocol.KeyPhaseZero, []byte(ad))
	require.NoError(t, err)
	require.Equal(t, protocol.PacketNumber(0x38), sp.DecodePacketNumber(0x38, protocol.PacketNumberLen1))
	_, err = sp.Open(nil, other, monotime.Now(), 0x1337, protocol.KeyPhaseZero, []byte(ad))
	require.NoError(t, err)
	require.Equal(t, protocol.PacketNumber(0x1338), sp.DecodePacketNumber(0x38, protocol.PacketNumberLen1))
	_, err = client.path(42)
	require.Error(t, err)
}

func TestMultipathAcknowledgesSentGeneration(t *testing.T) {
	client, server := multipathEndpoints(t)
	require.NoError(t, client.admitPath(3, utils.NewRTTStats()))
	require.NoError(t, server.admitPath(3, utils.NewRTTStats()))
	cp, err := client.path(3)
	require.NoError(t, err)
	client.SetHandshakeConfirmed()
	client.rollKeys()
	sealForTest(t, cp, nil, []byte(msg), 1, []byte(ad))
	// A large old-phase packet number cannot confirm the new generation.
	require.NoError(t, client.SetAcked(protocol.ApplicationKeyReceipt{Valid: true, PathID: 0, PacketNumber: 100000, KeyGeneration: 0}, monotime.Now()))
	require.False(t, client.updateAllowed())
	require.Error(t, client.SetAcked(protocol.ApplicationKeyReceipt{Valid: true, PathID: 3, PacketNumber: 1, KeyGeneration: 1}, monotime.Now()))
	server.rollKeys()
	encrypted := sealForTest(t, server, nil, []byte(msg), 2, []byte(ad))
	_, err = client.Open(nil, encrypted, monotime.Now(), 2, protocol.KeyPhaseOne, []byte(ad))
	require.NoError(t, err)
	now := monotime.Now()
	require.NoError(t, client.SetAcked(protocol.ApplicationKeyReceipt{Valid: true, PathID: 3, PacketNumber: 1, KeyGeneration: 1}, now))
	require.False(t, client.updateAllowed(), "confirmation starts the largest-PTO spacing interval")
}

func TestMultipathReorderedAndSilentPaths(t *testing.T) {
	client, server := multipathEndpoints(t)
	require.NoError(t, client.admitPath(3, utils.NewRTTStats()))
	require.NoError(t, server.admitPath(3, utils.NewRTTStats()))
	cp, err := client.path(3)
	require.NoError(t, err)
	sp, err := server.path(3)
	require.NoError(t, err)
	old := sealForTest(t, cp, nil, []byte(msg), 10000, []byte(ad))
	client.rollKeys()
	current := sealForTest(t, client, nil, []byte(msg), 1, []byte(ad))
	now := monotime.Now()
	_, err = server.Open(nil, current, now, 1, protocol.KeyPhaseOne, []byte(ad))
	require.NoError(t, err)
	// The silent path has no transition boundary. Its old high PN must use
	// the retained key, and a current-phase low PN must use current keys directly.
	_, err = sp.Open(nil, old, now, 10000, protocol.KeyPhaseZero, []byte(ad))
	require.NoError(t, err)
	currentOther := sealForTest(t, cp, nil, []byte(msg), 10001, []byte(ad))
	_, err = sp.Open(nil, currentOther, now, 10001, protocol.KeyPhaseOne, []byte(ad))
	require.NoError(t, err)
	_, err = sp.Open(nil, old, now, 10000, protocol.KeyPhaseZero, []byte(ad))
	require.NoError(t, err)
	_, err = sp.Open(nil, old, now.Add(server.keyRetentionDuration()), 10000, protocol.KeyPhaseZero, []byte(ad))
	require.ErrorIs(t, err, ErrKeysDropped)
}

func TestMultipathAggregateFailedTrials(t *testing.T) {
	client, _ := multipathEndpoints(t)
	require.NoError(t, client.admitPath(3, utils.NewRTTStats()))
	cp, err := client.path(3)
	require.NoError(t, err)
	client.invalidPacketLimit = 3
	_, err = client.Open(nil, []byte("invalid"), monotime.Now(), 1, protocol.KeyPhaseZero, nil)
	require.ErrorIs(t, err, ErrDecryptionFailed)
	client.rollKeys()
	// Unknown boundary: both previous and next trials consume the same budget.
	_, err = cp.Open(nil, []byte("invalid"), monotime.Now(), 1, protocol.KeyPhaseZero, nil)
	var transportErr *qerr.TransportError
	require.ErrorAs(t, err, &transportErr)
	require.Equal(t, qerr.AEADLimitReached, transportErr.ErrorCode)
}

func TestMultipathRetiredPathProtection(t *testing.T) {
	client, server := multipathEndpoints(t)
	require.NoError(t, client.admitPath(3, utils.NewRTTStats()))
	require.NoError(t, server.admitPath(3, utils.NewRTTStats()))
	cp, err := client.path(3)
	require.NoError(t, err)
	sp, err := server.path(3)
	require.NoError(t, err)
	encrypted := sealForTest(t, cp, nil, []byte(msg), 10, []byte(ad))
	require.NoError(t, server.retirePath(3))
	_, err = sp.Open(nil, encrypted, monotime.Now(), 10, protocol.KeyPhaseZero, []byte(ad))
	require.NoError(t, err)
	require.NoError(t, server.removePath(3))
	_, err = sp.Open(nil, encrypted, monotime.Now(), 10, protocol.KeyPhaseZero, []byte(ad))
	require.ErrorIs(t, err, ErrKeysDropped)
	require.Error(t, server.admitPath(3, utils.NewRTTStats()), "retired IDs cannot be reused")
	require.NoError(t, server.retirePath(0))
	_, err = ShortHeaderSealerForPath(server, 0)
	require.ErrorIs(t, err, ErrKeysDropped)
	cs := &cryptoSetup{aead: server, has1RTTSealer: true}
	_, err = cs.Get1RTTSealer()
	require.ErrorIs(t, err, ErrKeysDropped)
	ordinary, _ := multipathEndpoints(t)
	cs = &cryptoSetup{aead: ordinary, has1RTTSealer: true}
	_, err = cs.Get1RTTSealer()
	require.NoError(t, err, "ordinary path-zero acquisition is preserved")
}

func multipathEndpoints(t *testing.T) (*updatableAEAD, *updatableAEAD) {
	t.Helper()
	client := newUpdatableAEAD(utils.NewRTTStats(), nil, utils.DefaultLogger, protocol.Version1)
	server := newUpdatableAEAD(utils.NewRTTStats(), nil, utils.DefaultLogger, protocol.Version1)
	suite := getCipherSuite(tls.TLS_AES_128_GCM_SHA256)
	client.SetReadKey(suite, []byte("server traffic secret"))
	client.SetWriteKey(suite, []byte("client traffic secret"))
	server.SetReadKey(suite, []byte("client traffic secret"))
	server.SetWriteKey(suite, []byte("server traffic secret"))
	return client, server
}

func TestMultipathSilentPathAuthenticatesNextGeneration(t *testing.T) {
	client, server := multipathEndpoints(t)
	require.NoError(t, client.admitPath(3, utils.NewRTTStats()))
	require.NoError(t, server.admitPath(3, utils.NewRTTStats()))
	cp, err := client.path(3)
	require.NoError(t, err)
	sp, err := server.path(3)
	require.NoError(t, err)
	client.rollKeys()
	encrypted := sealForTest(t, client, nil, []byte(msg), 1, []byte(ad))
	_, err = server.Open(nil, encrypted, monotime.Now(), 1, protocol.KeyPhaseOne, []byte(ad))
	require.NoError(t, err)
	sealForTest(t, server, nil, []byte(msg), 2, []byte(ad)) // response in generation 1 permits the peer's next update
	client.rollKeys()
	encrypted = sealForTest(t, cp, nil, []byte(msg), 10, []byte(ad))
	// Real unpacking opens in place. A failed previous-key trial must leave
	// ciphertext available for the next-key trial on this silent path.
	decrypted, err := sp.Open(encrypted[:0], encrypted, monotime.Now(), 10, protocol.KeyPhaseZero, []byte(ad))
	require.NoError(t, err)
	require.Equal(t, msg, string(decrypted))
}

func TestMultipathConfirmedUpdateSpacing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client, server := multipathEndpoints(t)
		require.NoError(t, client.admitPath(3, utils.NewRTTStats()))
		require.NoError(t, server.admitPath(3, utils.NewRTTStats()))
		client.SetHandshakeConfirmed()
		client.rollKeys()
		server.rollKeys()
		sealForTest(t, client, nil, []byte(msg), 1, []byte(ad))
		encrypted := sealForTest(t, server, nil, []byte(msg), 1, []byte(ad))
		_, err := client.Open(nil, encrypted, monotime.Now(), 1, protocol.KeyPhaseOne, []byte(ad))
		require.NoError(t, err)
		require.NoError(t, client.SetAcked(protocol.ApplicationKeyReceipt{Valid: true, KeyGeneration: 1, PacketNumber: 1}, monotime.Now()))
		require.False(t, client.updateAllowed())
		time.Sleep(client.keyRetentionDuration())
		require.True(t, client.updateAllowed())
	})
}

func TestMultipathBoundedPathRecords(t *testing.T) {
	client, _ := multipathEndpoints(t)
	for id := protocol.PathID(1); id <= 4; id++ {
		require.NoError(t, client.admitPath(id, utils.NewRTTStats()))
		require.Error(t, client.admitPath(id+1, utils.NewRTTStats()), "two live slots including zero")
		require.NoError(t, client.retirePath(id))
	}
	require.NoError(t, client.admitPath(5, utils.NewRTTStats()))
	require.Error(t, client.retirePath(5), "four retained records")
	require.NoError(t, client.removePath(1))
	require.NoError(t, client.retirePath(5))
	require.NoError(t, client.admitPath(protocol.MaxPathID, utils.NewRTTStats()), "sparse IDs do not allocate proportional state")
}

func TestMultipathEncryptionCeilingDuringSpacing(t *testing.T) {
	client, _ := multipathEndpoints(t)
	require.NoError(t, client.admitPath(3, utils.NewRTTStats()))
	cp, err := client.path(3)
	require.NoError(t, err)
	client.SetHandshakeConfirmed()
	client.rollKeys()
	client.numSentWithCurrentKey = (1 << 23) - 1
	client.nextUpdateTime = monotime.Now().Add(time.Hour)
	encrypted, err := cp.Seal(nil, []byte(msg), 1, []byte(ad))
	require.NoError(t, err)
	require.NotEmpty(t, encrypted)
	encrypted, err = client.Seal(nil, []byte(msg), 1, []byte(ad))
	var limit *qerr.TransportError
	require.ErrorAs(t, err, &limit)
	require.Equal(t, qerr.AEADLimitReached, limit.ErrorCode)
	require.Empty(t, encrypted)
	require.Equal(t, protocol.KeyPhase(1), client.KeyGeneration(), "spacing must not be bypassed")
	// Reusing an already acquired sealer cannot bypass the connection-wide cap.
	_, err = cp.Seal(nil, []byte(msg), 2, []byte(ad))
	require.ErrorAs(t, err, &limit)
}

func sealForTest(t testing.TB, s ShortHeaderSealer, dst, src []byte, pn protocol.PacketNumber, ad []byte) []byte {
	t.Helper()
	sealed, err := s.Seal(dst, src, pn, ad)
	require.NoError(t, err)
	return sealed
}

func TestMultipathAdmissionDuringPreviousKeyOverlap(t *testing.T) {
	client, server := multipathEndpoints(t)
	client.rollKeys()
	require.NoError(t, client.admitPath(3, utils.NewRTTStats()))
	require.NoError(t, server.admitPath(3, utils.NewRTTStats()))
	cp, err := client.path(3)
	require.NoError(t, err)
	sp, err := server.path(3)
	require.NoError(t, err)
	encrypted := sealForTest(t, sp, nil, []byte(msg), 1, []byte(ad))
	now := monotime.Now()
	decrypted, err := cp.Open(nil, encrypted, now, 1, protocol.KeyPhaseZero, []byte(ad))
	require.NoError(t, err)
	require.Equal(t, msg, string(decrypted))
	require.Zero(t, client.invalidPacketCount, "authentic previous traffic is not a failed trial")
	server.rollKeys()
	current := sealForTest(t, server, nil, []byte(msg), 1, []byte(ad))
	_, err = client.Open(nil, current, now, 1, protocol.KeyPhaseOne, []byte(ad))
	require.NoError(t, err)
	_, err = cp.Open(nil, encrypted, now.Add(client.keyRetentionDuration()), 1, protocol.KeyPhaseZero, []byte(ad))
	require.Error(t, err)
	require.Nil(t, client.prevRcvTrafficSecret)
}
