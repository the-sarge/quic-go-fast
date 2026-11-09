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

type nativeNaturalTurnPolicy struct {
	emissionPolicy
	inspect func()
}

func (p nativeNaturalTurnPolicy) maxPacketSize() protocol.ByteCount {
	p.inspect()
	return p.emissionPolicy.maxPacketSize()
}

func TestNativeSoftwareNaturalTurnGroupsBeforeAdmission(t *testing.T) {
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
		observed := false
		c.emission.policy = nativeNaturalTurnPolicy{emissionPolicy: c, inspect: func() {
			for i := range sender.records {
				r := &sender.records[i]
				if len(r.members) == 0 || observed {
					continue
				}
				require.Equal(t, ordinaryBuilding, r.state, "a full physical member stays in the natural request")
				require.False(t, r.offered)
				require.Empty(t, sent, "no mutable request is exposed before its natural boundary")
				observed = true
			}
		}}
		queueNativeSoftwareStream(t, c)
		require.NoError(t, c.emission.advanceForPlatform(monotime.Now(), false, true).err)
		require.True(t, observed)
		r := sender.pending
		require.NotNil(t, r)
		require.Greater(t, len(r.members), 1)
		require.EqualValues(t, r.count, c.connStats.PacketsSent.Load())
		var expected [][]byte
		var buffers []*packetBuffer
		previous := protocol.InvalidPacketNumber
		for _, member := range r.request.members {
			require.Nil(t, member.protection, "software protection remains synchronous")
			buffers = append(buffers, member.buffer)
			data, segment := member.buffer.Data, int(member.gsoSize)
			if segment == 0 {
				segment = len(data)
			}
			for len(data) != 0 {
				length := min(segment, len(data))
				ciphertext := bytes.Clone(data[:length])
				expected = append(expected, ciphertext)
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
				data = data[length:]
			}
		}
		require.Len(t, expected, r.count)
		sender.finish()
		<-resetEntered
		synctest.Wait()
		require.Equal(t, expected, sent)
		var token *endpointToken
		endpoint.do(func(state *endpointState) { token = state.current })
		require.NotNil(t, token)
		require.Nil(t, token.completion.Load(), "all members retain the final deadline-reset borrow")
		for _, buffer := range buffers {
			require.Equal(t, 1, buffer.refCount)
		}
		close(releaseReset)
		synctest.Wait()
		require.NotEqual(t, ordinaryFree, r.state, "publication alone does not retire the local request")
		require.NoError(t, sender.consume())
		require.Equal(t, ordinaryFree, r.state)
		for _, buffer := range buffers {
			require.Zero(t, buffer.refCount)
		}
	})
}
