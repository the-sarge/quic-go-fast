//go:build darwin || linux || freebsd

package quic

import (
	"bytes"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestNativeSoftwareEmptySupplyOffersPrefixAndYields(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		conf := &Config{InitialPacketSize: 1200, EnableDatagrams: true, DisablePathMTUDiscovery: true}
		require.NoError(t, conf.SetCongestionControlV1("reno"))
		tc := newConfiguredEmissionTestConnection(t, false, conf)
		c := tc.conn
		c.rttStats.UpdateRTT(100*time.Microsecond, 0)
		sealer, opener := nativeProtectionTestCrypto(t, protocol.Version1)
		manager := NewMockSealingManager(gomock.NewController(t))
		manager.EXPECT().Get1RTTSealer().Return(sealer, nil).AnyTimes()
		c.emission.packer.cryptoSetup = manager
		resetEntered, releaseReset := make(chan struct{}), make(chan struct{})
		var once sync.Once
		var sent [][]byte
		endpoint := startNativeEndpoint(endpointTestNative{
			send: func(data []byte) (int, error) {
				sent = append(sent, bytes.Clone(data))
				return len(data), nil
			},
			deadline: func(deadline time.Time) error {
				if deadline.IsZero() {
					once.Do(func() { close(resetEntered); <-releaseReset })
				}
				return nil
			},
		}, 100*time.Millisecond, nil)
		t.Cleanup(func() {
			select {
			case <-releaseReset:
			default:
				close(releaseReset)
			}
			<-endpoint.stop()
		})
		require.NoError(t, c.useNativeOrdinary(endpoint, nativeProbePeer, packetInfo{}))
		sender := c.emission.local.(*nativeOrdinarySender)
		t.Cleanup(sender.Close)
		continued := false
		c.emission.policy = nativeNaturalTurnPolicy{emissionPolicy: c, inspect: func() {
			if r := sender.pending; r != nil && len(r.members) != 0 {
				continued = true // next iteration, before its pool allocation/packing
			}
		}}
		queueNativeSoftwareStream(t, c)
		result := c.emission.advanceForPlatform(monotime.Now(), false, true)
		require.NoError(t, result.err)
		require.True(t, result.retry, "empty native supply exposes the completed prefix and returns to Conn")
		require.False(t, continued, "no subsequent construction occurs in the old turn")
		require.Nil(t, sender.pending)
		<-resetEntered
		synctest.Wait()
		var token *endpointToken
		var buffer *packetBuffer
		endpoint.do(func(state *endpointState) {
			token = state.current
			require.NotNil(t, token)
			require.Len(t, token.request.members, 1)
			buffer = token.request.members[0].buffer
		})
		require.EqualValues(t, protocol.MaxLargePacketBufferSize, buffer.Cap())
		require.Equal(t, 17*1200, len(buffer.Data), "the existing 20 KiB resource holds seventeen full 1200-byte packets")
		require.Len(t, sent, 17)
		require.EqualValues(t, len(sent), c.connStats.PacketsSent.Load())
		previous := protocol.InvalidPacketNumber
		for _, ciphertext := range sent {
			raw := bytes.Clone(ciphertext)
			pnOffset := 1 + c.connIDManager.Get().Len()
			opener.DecryptHeader(raw[pnOffset+4:pnOffset+20], &raw[0], raw[pnOffset:pnOffset+4])
			headerLen, truncated, pnLen, phase, err := wire.ParseShortHeader(raw, c.connIDManager.Get().Len())
			require.NoError(t, err)
			pn := protocol.DecodePacketNumber(pnLen, previous, truncated)
			require.Greater(t, pn, previous)
			previous = pn
			plain, err := opener.Open(nil, ciphertext[headerLen:], monotime.Now(), pn, phase, raw[:headerLen])
			require.NoError(t, err)
			require.Contains(t, string(plain), "physical-boundary")
		}
		require.Nil(t, token.completion.Load(), "final deadline reset still owns the native borrow")
		require.Equal(t, 1, buffer.refCount)
		var record *ordinaryLocalRecord
		for i := range sender.records {
			if sender.records[i].state != ordinaryFree {
				require.Nil(t, record)
				record = &sender.records[i]
			}
		}
		require.NotNil(t, record)
		close(releaseReset)
		synctest.Wait()
		require.True(t, endpoint.nativeWorkEmpty(sender.participant), "reconciled native work can be empty while the local record is still charged")
		require.NotEqual(t, ordinaryFree, record.state, "native publication does not retire local capacity")
		require.NoError(t, sender.consume())
		require.Equal(t, ordinaryFree, record.state)
		require.Zero(t, buffer.refCount)
	})
}
