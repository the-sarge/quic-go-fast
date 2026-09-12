//go:build w3bench

package quic

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"math/big"
	"testing"
	"time"
)

// Shared fixtures for the two-process W3 URO adoption harness
// (docs/audits/2026-09-11-w3-uro-protocol.md): the workload record size and
// the measurement-only TLS pinning that lets the receiver trust exactly the
// sender's certificate across processes, preserving the v1 single-process
// harness's RootCAs pinning without a shared CA.
//
// The sender generates one ephemeral self-signed certificate at startup and
// prints its SHA-256 fingerprint (W3BENCH_CERTPIN). The orchestration passes
// that pin to the receiver as W3BENCH_PEER_CERTPIN; the receiver accepts the
// TLS peer only when the presented leaf certificate hashes to that exact pin.
// Certificate identity is not part of the investigation; the pin exists only
// so a stray process cannot join the measured link.

const w3benchRecordSize = 1071

// w3benchServerTLS generates the sender's ephemeral certificate and returns
// a tls.Config presenting it plus the certificate's SHA-256 fingerprint.
func w3benchServerTLS(t *testing.T) (*tls.Config, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		DNSNames:     []string{"w3bench"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(der)
	cfg := &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
		NextProtos:   []string{"w3bench"},
	}
	return cfg, hex.EncodeToString(sum[:])
}

// w3benchClientTLS returns a tls.Config that accepts exactly the peer
// certificate whose SHA-256 fingerprint equals pin. It disables the default
// chain build (the harness pins a specific leaf, not a CA) and verifies the
// presented leaf against the pin itself.
func w3benchClientTLS(pin string) *tls.Config {
	return &tls.Config{
		InsecureSkipVerify: true, // pin verification below replaces chain building
		NextProtos:         []string{"w3bench"},
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return errW3benchNoPeerCert
			}
			sum := sha256.Sum256(rawCerts[0])
			if hex.EncodeToString(sum[:]) != pin {
				return errW3benchPinMismatch
			}
			return nil
		},
	}
}

type w3benchError string

func (e w3benchError) Error() string { return string(e) }

const (
	errW3benchNoPeerCert  = w3benchError("w3bench: peer presented no certificate")
	errW3benchPinMismatch = w3benchError("w3bench: peer certificate does not match the pinned fingerprint")
)
