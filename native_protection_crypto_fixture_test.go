package quic

import (
	"context"
	"crypto/tls"
	"net"
	"testing"

	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/testdata"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

// nativeProtectionTestCrypto obtains constructor-proven 1-RTT protection without
// a production test constructor, mock capability, or socket. The peer opener
// authenticates emitted bytes; all-cipher differential coverage lives in the
// handshake package, where independent owners can use identical fixed secrets.
func nativeProtectionTestCrypto(t testing.TB, version protocol.Version) (handshake.ShortHeaderSealer, handshake.ShortHeaderOpener) {
	t.Helper()
	clientConfig := &tls.Config{ServerName: "localhost", RootCAs: testdata.GetRootCA(), NextProtos: []string{"native-protection-test"}}
	serverConfig := testdata.GetTLSConfig()
	serverConfig.NextProtos = []string{"native-protection-test"}
	var resetToken protocol.StatelessResetToken
	client := handshake.NewCryptoSetupClient(protocol.ConnectionID{}, &wire.TransportParameters{ActiveConnectionIDLimit: 2}, clientConfig, false, utils.NewRTTStats(), nil, utils.DefaultLogger, version)
	server := handshake.NewCryptoSetupServer(protocol.ConnectionID{}, &net.UDPAddr{IP: net.IPv6loopback, Port: 1234}, &net.UDPAddr{IP: net.IPv6loopback, Port: 4321}, &wire.TransportParameters{StatelessResetToken: &resetToken, ActiveConnectionIDLimit: 2}, serverConfig, false, utils.NewRTTStats(), nil, utils.DefaultLogger, version)
	t.Cleanup(func() {
		require.NoError(t, client.Close())
		require.NoError(t, server.Close())
	})
	require.NoError(t, client.StartHandshake(context.Background()))
	require.NoError(t, server.StartHandshake(context.Background()))
	var clientDone, serverDone bool
	for rounds := 0; !clientDone || !serverDone; rounds++ {
		require.Less(t, rounds, 16, "TLS event pump must make bounded progress")
		progress := false
		for _, side := range []struct {
			from, to handshake.CryptoSetup
			done     *bool
		}{{client, server, &clientDone}, {server, client, &serverDone}} {
			for {
				event := side.from.NextEvent()
				if event.Kind == handshake.EventNoEvent {
					break
				}
				progress = true
				switch event.Kind {
				case handshake.EventWriteInitialData:
					require.NoError(t, side.to.HandleMessage(event.Data, protocol.EncryptionInitial))
				case handshake.EventWriteHandshakeData:
					require.NoError(t, side.to.HandleMessage(event.Data, protocol.EncryptionHandshake))
				case handshake.EventHandshakeComplete:
					*side.done = true
				}
			}
		}
		require.True(t, progress, "TLS event pump stalled before completion")
	}
	sealer, err := server.Get1RTTSealer()
	require.NoError(t, err)
	opener, err := client.Get1RTTOpener()
	require.NoError(t, err)
	return sealer, opener
}
