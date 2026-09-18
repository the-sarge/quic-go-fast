package qtls

import (
	"crypto/fips140"
	"crypto/tls"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCipherSuiteSelection(t *testing.T) {
	t.Run("TLS_AES_128_GCM_SHA256", func(t *testing.T) { testCipherSuiteSelection(t, tls.TLS_AES_128_GCM_SHA256) })
	t.Run("TLS_CHACHA20_POLY1305_SHA256", func(t *testing.T) { testCipherSuiteSelection(t, tls.TLS_CHACHA20_POLY1305_SHA256) })
	t.Run("TLS_AES_256_GCM_SHA384", func(t *testing.T) { testCipherSuiteSelection(t, tls.TLS_AES_256_GCM_SHA384) })
}

func testCipherSuiteSelection(t *testing.T, cs uint16) {
	if fips140.Enabled() && cs == tls.TLS_CHACHA20_POLY1305_SHA256 {
		t.Skip("ChaCha20-Poly1305 is not allowed in FIPS 140-3 mode")
	}

	f := newCipherFixture(t, cs)
	conn, err := f.dial(t.Context())
	require.NoError(t, err)
	_, err = conn.Write([]byte("foobar"))
	require.NoError(t, err)
	require.Equal(t, cs, conn.ConnectionState().CipherSuite)
	require.NoError(t, conn.Close())
	result := f.wait(t)
	require.NoError(t, result.err)
	require.Equal(t, cs, result.cipher)
}
