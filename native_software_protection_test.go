//go:build darwin || linux || freebsd

package quic

import (
	"bytes"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type e24SizePolicy struct {
	emissionPolicy
	conn *Conn
}

func (p e24SizePolicy) maxPacketSize() protocol.ByteCount {
	if p.conn.connStats.PacketsSent.Load() == 0 {
		return 1200
	}
	return 1100 // reset an already nonempty software buffer at its second begin
}

func TestE24SoftwareCiphertextBeforeNativeAndFinalFence(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		tc := newEmissionTestConnection(t, false)
		c := tc.conn
		sealer, opener := nativeProtectionTestCrypto(t, protocol.Version1)
		if !handshake.CanReservePacketProtection(sealer) {
			t.Skip("this placement comparison requires the existing deferred capability")
		}
		manager := NewMockSealingManager(gomock.NewController(t))
		manager.EXPECT().Get1RTTSealer().Return(sealer, nil).AnyTimes()
		c.emission.packer.cryptoSetup = manager
		writeEntered, releaseWrite := make(chan struct{}), make(chan struct{})
		resetEntered, releaseReset := make(chan struct{}), make(chan struct{})
		var writeOnce, resetOnce sync.Once
		var sent [][]byte
		endpoint := startNativeEndpoint(endpointTestNative{
			send: func(data []byte) (int, error) {
				sent = append(sent, bytes.Clone(data))
				writeOnce.Do(func() { close(writeEntered); <-releaseWrite })
				return len(data), nil
			},
			deadline: func(deadline time.Time) error {
				if deadline.IsZero() {
					resetOnce.Do(func() { close(resetEntered); <-releaseReset })
				}
				return nil
			},
		}, 100*time.Millisecond, nil)
		t.Cleanup(func() {
			for _, gate := range []chan struct{}{releaseWrite, releaseReset} {
				select {
				case <-gate:
				default:
					close(gate)
				}
			}
			<-endpoint.stop()
		})
		require.NoError(t, c.useNativeOrdinary(endpoint, nativeProbePeer, packetInfo{}))
		sender := c.emission.local.(*nativeOrdinarySender)
		t.Cleanup(sender.Close)
		c.emission.policy = e24SizePolicy{c, c}
		c.connFlowController.UpdateSendWindow(1 << 20)
		flow := newStreamFlowController(0, c.connFlowController, 1<<20, 1<<20, 1<<20, c.flowControlRTT(), c.logger)
		stream := newSendStream(c.ctx, 0, c, flow, false)
		require.NoError(t, stream.TryWriteAll(bytes.Repeat([]byte("software-domain"), 512)))
		require.NoError(t, c.emission.withSoftwareSegments(monotime.Now()).err)
		r := sender.pending
		require.NotNil(t, r)
		require.GreaterOrEqual(t, len(r.members), 2, "shape transition exercises both begin sites")
		require.Greater(t, r.count, 2)
		require.EqualValues(t, r.count, c.connStats.PacketsSent.Load())
		require.Empty(t, sent, "construction is inspected before endpoint admission")
		var buffers []*packetBuffer
		var expected [][]byte
		packetNumber := protocol.InvalidPacketNumber
		for _, member := range r.request.members {
			require.Nil(t, member.protection, "software constructor hands off ciphertext without a plan")
			require.False(t, member.nativeGSO)
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
				headerLen, truncated, pnLen, keyPhase, err := wire.ParseShortHeader(raw, c.connIDManager.Get().Len())
				require.NoError(t, err)
				pn := protocol.DecodePacketNumber(pnLen, packetNumber, truncated)
				require.Greater(t, pn, packetNumber, "native output preserves PN order, including legal gaps")
				if packetNumber == protocol.InvalidPacketNumber {
					require.Zero(t, pn, "the fresh fixture starts at PN zero")
				}
				require.Equal(t, protocol.KeyPhaseZero, keyPhase)
				// Four-byte header probing can also unmask bytes past the actual
				// PN. Open the untouched ciphertext with only the decoded header.
				plain, err := opener.Open(nil, ciphertext[headerLen:], monotime.Now(), pn, keyPhase, raw[:headerLen])
				require.NoError(t, err)
				require.Contains(t, string(plain), "software-domain")
				packetNumber = pn
				data = data[length:]
			}
		}
		require.Equal(t, r.count, len(expected))
		sender.finish()
		<-writeEntered
		synctest.Wait()
		for _, buffer := range buffers {
			require.Equal(t, 1, buffer.refCount)
		}
		close(releaseWrite)
		<-resetEntered
		synctest.Wait()
		require.Equal(t, expected, sent, "scalar native calls send exactly the already-protected bytes")
		var token *endpointToken
		endpoint.do(func(state *endpointState) { token = state.current })
		require.NotNil(t, token)
		require.Nil(t, token.completion.Load(), "deadline reset is part of the final native fence")
		for _, buffer := range buffers {
			require.Equal(t, 1, buffer.refCount)
		}
		close(releaseReset)
		synctest.Wait()
		require.NoError(t, sender.consume())
		for _, buffer := range buffers {
			require.Zero(t, buffer.refCount)
		}
		for i := range sender.records {
			require.Equal(t, ordinaryFree, sender.records[i].state)
		}
	})
}
