package quic

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/mocks"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// Keep the production constructors' packer, sealing, recovery and queue intact:
// these cases protect assembly that transparent-protection fixtures replace.
func TestEmissionConstructionInitial(t *testing.T) {
	for _, name := range []string{"server", "client without token", "client with token"} {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			var tc *testConnection
			token := []byte{}
			var destID protocol.ConnectionID
			if name == "server" {
				tc = newServerTestConnection(t, ctrl, nil, false)
				// The wrapper passes an empty peer source ID as outbound destination;
				// tc.destConnID is the original destination used for Initial keys.
			} else {
				store := NewLRUTokenStore(1, 1)
				if name == "client with token" {
					token = []byte("stored address-validation token")
					store.Put("quic-go.net", &ClientToken{data: token, rtt: 42 * time.Millisecond})
				}
				tc = newClientTestConnection(t, ctrl, &Config{TokenStore: store, DisablePathMTUDiscovery: true}, false)
				destID = tc.destConnID
			}
			c := tc.conn
			defer c.ctxCancel(context.Canceled)
			defer c.cryptoStreamHandler.Close()
			q := c.emission.queue.(*sendQueue)
			// Even an assertion failure must release submitted buffers and join
			// the worker. It starts only after all recovery observations finish.
			var queued, written []byte
			tc.sendConn.EXPECT().Write(gomock.Any(), uint16(0), protocol.ECNUnsupported).DoAndReturn(func(b []byte, _ uint16, _ protocol.ECN) error {
				written = bytes.Clone(b)
				return nil
			}).Times(1)
			defer func() {
				done := make(chan error, 1)
				go func() { done <- q.Run() }()
				q.Close()
				require.NoError(t, <-done)
				require.Empty(t, q.queue)
				require.Equal(t, queued, written)
			}()
			if name == "client with token" {
				require.Equal(t, 42*time.Millisecond, c.rttStats.SmoothedRTT())
			}
			now := monotime.Now()
			if name == "server" {
				c.sentPacketHandler.ReceivedBytes(1200, now)
			}
			if name == "server" {
				_, err := c.initialStream.Write([]byte("eligible Initial crypto work"))
				require.NoError(t, err)
			} else {
				// Use the existing retransmission producer: fresh client stream
				// writes require a complete TLS ClientHello for scrambling.
				c.retransmissionQueue.addInitial(&wire.CryptoFrame{Data: []byte("eligible Initial crypto work")})
			}
			pn, _ := c.sentPacketHandler.PeekPacketNumber(protocol.EncryptionInitial)
			require.Zero(t, pn)
			require.NoError(t, c.triggerSending(now).err)
			require.Len(t, q.queue, 1)
			entry := <-q.queue
			queued = bytes.Clone(entry.buf.Data)
			q.queue <- entry // retain the original worker-owned buffer and metadata
			header, _, rest, err := wire.ParsePacket(queued)
			require.NoError(t, err)
			require.Empty(t, rest)
			require.Equal(t, protocol.PacketTypeInitial, header.Type)
			require.Equal(t, protocol.Version1, header.Version)
			require.Equal(t, tc.srcConnID, header.SrcConnectionID)
			require.Equal(t, destID, header.DestConnectionID)
			require.Equal(t, token, header.Token)
			next, _ := c.sentPacketHandler.PeekPacketNumber(protocol.EncryptionInitial)
			require.Equal(t, pn+1, next)
			// A live loss timer and accepted ACK demonstrate real registration,
			// before any socket I/O and on the sole recovery-owning goroutine.
			require.EqualValues(t, 1, c.connStats.PacketsSent.Load())
			require.EqualValues(t, len(queued), c.connStats.BytesSent.Load())
			require.NotZero(t, c.sentPacketHandler.GetLossDetectionTimeout())
			_, err = c.sentPacketHandler.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: pn, Largest: pn}}}, protocol.EncryptionInitial, now.Add(time.Millisecond))
			require.NoError(t, err)
			require.Equal(t, time.Millisecond, c.rttStats.LatestRTT())
		})
	}
}

func TestEmissionConstructionStartHandshakeError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctrl := gomock.NewController(t)
		tc := newServerTestConnection(t, ctrl, nil, false)
		c := tc.conn
		defer c.cryptoStreamHandler.Close()
		cs := mocks.NewMockCryptoSetup(ctrl)
		want := errors.New("StartHandshake failed")
		cs.EXPECT().StartHandshake(gomock.Any()).Return(want)
		c.cryptoStreamHandler = cs
		q := c.emission.queue.(*sendQueue)
		buf := getPacketBuffer()
		buf.Data = append(buf.Data, 0xff)
		q.Send(buf, 0, protocol.ECNUnsupported, sendMetadata{})
		defer func() {
			for len(q.queue) > 0 {
				(<-q.queue).buf.Release()
			}
		}()
		require.ErrorIs(t, c.run(), want)
		defer c.timer.Stop()
		synctest.Wait()
		require.ErrorIs(t, context.Cause(c.Context()), want)
		// Pending work stays queued and the socket has no Write expectation:
		// no worker started. Returning also demonstrates no unstarted join.
		require.Len(t, q.queue, 1)
	})
}
