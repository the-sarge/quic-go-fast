//go:build w3bench

package quic

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"math/big"
	"testing"
	"time"
)

// Shared fixtures for the two-process W3 URO adoption harness
// (docs/audits/2026-09-11-w3-uro-protocol.md): the workload record size and
// the deterministic measurement-only TLS identity that lets the receiver pin
// the sender's certificate across processes exactly as the v1 single-process
// harness pinned it via RootCAs. The fixed key is a test fixture for a
// closed measurement link between endpoints the orchestration itself starts;
// it is not a secret and must never be used outside the w3bench harness.

const w3benchRecordSize = 1071

// w3benchDeterministicReader yields a fixed byte stream so both harness
// halves derive the identical ECDSA key without exchanging material.
type w3benchDeterministicReader struct {
	counter uint64
	buf     []byte
}

func (r *w3benchDeterministicReader) Read(p []byte) (int, error) {
	for i := range p {
		if len(r.buf) == 0 {
			var ctr [8]byte
			binary.BigEndian.PutUint64(ctr[:], r.counter)
			sum := sha256.Sum256(append([]byte("w3bench-fixed-identity-20260912"), ctr[:]...))
			r.buf = sum[:]
			r.counter++
		}
		p[i] = r.buf[0]
		r.buf = r.buf[1:]
	}
	return len(p), nil
}

// w3benchIdentity returns the harness's fixed self-signed certificate and
// key. The sender presents it; the receiver trusts exactly this certificate.
func w3benchIdentity(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), &w3benchDeterministicReader{})
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:     time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		DNSNames:     []string{"w3bench"},
	}
	der, err := x509.CreateCertificate(&w3benchDeterministicReader{counter: 1 << 32}, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, roots
}
