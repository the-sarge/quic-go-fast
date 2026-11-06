package quic

import (
	"testing"

	"github.com/quic-go/quic-go/internal/testdata"
	"github.com/stretchr/testify/require"
)

func TestConfigurePacketCeilingV1(t *testing.T) {
	for _, n := range []int{1451, 8973} {
		tr := &Transport{}
		require.Error(t, tr.ConfigurePacketCeilingV1(n))
	}
	for _, n := range []int{1452, 8972} {
		tr := &Transport{}
		require.NoError(t, tr.ConfigurePacketCeilingV1(n))
	}
	t.Run("after initialization", func(t *testing.T) {
		tr := &Transport{Conn: newUDPConnLocalhost(t)}
		t.Cleanup(func() { tr.Close() })
		ln, err := tr.Listen(testdata.GetTLSConfig(), nil)
		require.NoError(t, err)
		t.Cleanup(func() { ln.Close() })
		require.ErrorContains(t, tr.ConfigurePacketCeilingV1(8972), "after initialization")
	})
}
