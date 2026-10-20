package quic

import (
	"context"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/testdata"

	"github.com/stretchr/testify/require"
)

// Consumers negotiate the acknowledgment threshold with standard types only.
type ackThresholdV1 interface {
	ConfigureAckThresholdV1(threshold int) error
}

func configureAckThreshold(t *testing.T, tr *Transport, threshold int) error {
	t.Helper()
	api, ok := any(tr).(ackThresholdV1)
	require.True(t, ok, "transport must expose a standard-type acknowledgment threshold capability")
	return api.ConfigureAckThresholdV1(threshold)
}

// The threshold is configured once, before initialization, within 2 to 32.
func TestAckThresholdContract(t *testing.T) {
	for _, threshold := range []int{-1, 0, 1, 33} {
		tr := &Transport{Conn: newUDPConnLocalhost(t)}
		t.Cleanup(func() { tr.Close() })
		require.Error(t, configureAckThreshold(t, tr, threshold), "threshold %d", threshold)
		// A rejected value configures nothing.
		require.NoError(t, configureAckThreshold(t, tr, 16))
	}
	for _, threshold := range []int{2, 16, 32} {
		tr := &Transport{Conn: newUDPConnLocalhost(t)}
		t.Cleanup(func() { tr.Close() })
		require.NoError(t, configureAckThreshold(t, tr, threshold), "threshold %d", threshold)
		require.Error(t, configureAckThreshold(t, tr, threshold), "configuration happens once")
	}
	initialized := &Transport{Conn: newUDPConnLocalhost(t)}
	t.Cleanup(func() { initialized.Close() })
	_, err := initialized.Listen(testdata.GetTLSConfig(), nil)
	require.NoError(t, err)
	require.Error(t, configureAckThreshold(t, initialized, 16), "configuration precedes initialization")
}

// The threshold reaches every connection the transport dials or accepts.
func TestAckThresholdReachesConnections(t *testing.T) {
	for _, tc := range []struct {
		name      string
		threshold int
	}{
		{name: "configured", threshold: 16},
		{name: "default"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			serverTransport := &Transport{Conn: newUDPConnLocalhost(t)}
			t.Cleanup(func() { serverTransport.Close() })
			clientTransport := &Transport{Conn: newUDPConnLocalhost(t)}
			t.Cleanup(func() { clientTransport.Close() })
			if tc.threshold != 0 {
				require.NoError(t, configureAckThreshold(t, serverTransport, tc.threshold))
				require.NoError(t, configureAckThreshold(t, clientTransport, tc.threshold))
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			ln, err := serverTransport.Listen(testdata.GetTLSConfig(), nil)
			require.NoError(t, err)
			client, err := clientTransport.Dial(ctx, ln.Addr(), candidateClientTLS(), nil)
			require.NoError(t, err)
			server, err := ln.Accept(ctx)
			require.NoError(t, err)
			require.Equal(t, tc.threshold, client.receivedPacketHandler.AppDataAckThreshold())
			require.Equal(t, tc.threshold, server.receivedPacketHandler.AppDataAckThreshold())
			require.NoError(t, client.CloseWithError(0, ""))
			require.NoError(t, server.CloseWithError(0, ""))
		})
	}
}
