//go:build darwin || linux || freebsd

package quic

import (
	"bytes"
	"testing"
	"testing/synctest"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

type nativeOrdinaryCloseCompletion struct {
	ackhandler.LocalSentPacketHandler
	tokens   []ackhandler.LocalCompletionToken
	outcomes []ackhandler.LocalPacketOutcome
	after    func(int)
}

func (o *nativeOrdinaryCloseCompletion) CompleteLocalPacket(token ackhandler.LocalCompletionToken, outcome ackhandler.LocalPacketOutcome, at monotime.Time) error {
	o.tokens = append(o.tokens, token)
	o.outcomes = append(o.outcomes, outcome)
	err := o.LocalSentPacketHandler.CompleteLocalPacket(token, outcome, at)
	o.after(len(o.tokens))
	return err
}

func TestNativeOrdinaryCompletionCloseInterruptsRecord(t *testing.T) {
	for _, suffix := range []bool{true, false} {
		name := "member_span"
		if suffix {
			name = "unhanded_suffix"
		}
		t.Run(name, func(t *testing.T) {
			nativeOrdinaryTest(t, func(t *testing.T) {
				tc, sender, socket := nativeProbeRecoveryConnection(t, "reno")
				completion := &nativeOrdinaryCloseCompletion{LocalSentPacketHandler: sender.recovery}
				sender.recovery = completion
				var buffers []*packetBuffer
				if suffix {
					first := getPacketBuffer()
					buffers = append(buffers, first)
					appendNativeGroupMember(t, tc, sender, first, "returned member", protocol.ECNNon, sendMetadata{})
					failed := getPacketBuffer()
					require.True(t, sender.reserveBuffer(failed.Cap()).available)
					for _, value := range []string{"first unhanded packet", "later unhanded packet"} {
						require.NoError(t, tc.conn.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte(value)}))
						_, err := tc.conn.emission.appendPacket(failed, 1200, false, protocol.ECNNon, monotime.Now())
						require.NoError(t, err)
					}
					failed.Release() // failed construction never transfers its payload
				} else {
					first := getLargePacketBuffer()
					buffers = append(buffers, first)
					require.True(t, sender.reserveBuffer(first.Cap()).available)
					_, pnLen := tc.conn.sentPacketHandler.PeekPacketNumber(protocol.Encryption1RTT)
					payloadSize := 1200 - int(wire.ShortHeaderLen(tc.conn.connIDManager.Get(), pnLen)) - 7 - 3
					for _, value := range []byte{0x41, 0x42, 0x43} {
						require.NoError(t, tc.conn.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: bytes.Repeat([]byte{value}, payloadSize)}))
						receipt, err := tc.conn.emission.appendPacket(first, 1200, false, protocol.ECNNon, monotime.Now())
						require.NoError(t, err)
						require.EqualValues(t, 1200, receipt.size)
					}
					sender.Send(first, 1200, protocol.ECNNon, sendMetadata{})
					tail := getPacketBuffer()
					buffers = append(buffers, tail)
					appendNativeGroupMember(t, tc, sender, tail, "later member", protocol.ECNNon, sendMetadata{})
				}
				record := sender.pending
				wantPackets := 4
				if suffix {
					wantPackets = 3
				}
				require.Equal(t, wantPackets, record.count)
				wantTokens := make([]ackhandler.LocalCompletionToken, record.count)
				for i, packet := range record.packets[:record.count] {
					wantTokens[i] = packet.token
				}
				closeAfter := 1
				if suffix {
					closeAfter = 2
				}
				completion.after = func(count int) {
					if count == closeAfter {
						require.Equal(t, ordinaryTerminalProcessing, record.state)
						sender.Close()
					}
				}
				sender.finish()
				synctest.Wait()
				require.Len(t, socket.writes, record.memberEnd(), "native facts exist before the outer consumer runs")
				for _, buffer := range buffers {
					require.Zero(t, buffer.refCount)
				}
				require.Empty(t, completion.tokens)
				require.NoError(t, sender.consume())
				require.True(t, sender.closed)
				require.Equal(t, wantTokens[:closeAfter], completion.tokens, "Close ends the consumer before another cleared identity is read")
				require.Equal(t, ackhandler.LocalPacketAccepted, completion.outcomes[0])
				if suffix {
					require.Equal(t, ackhandler.LocalPacketUnsubmitted, completion.outcomes[1])
				}
				for i := range sender.records {
					require.Equal(t, ordinaryLocalRecord{}, sender.records[i], "an interrupted consumer never restores or reuses Close's cleared records")
				}
			})
		})
	}
}
