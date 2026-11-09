//go:build darwin || linux || freebsd

package quic

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

// Warm through real native outcomes and ACK feedback, without replacing the
// recovery, pacing or storage owner. The virtual wait supplies a real RTT sample.
func warmNativeSoftwareRecovery(t *testing.T, tc *testConnection, sender *nativeOrdinarySender) {
	t.Helper()
	// Grow the real Reno window through actual sends and ACKs. A fresh
	// congestion window is itself 32 packets, independent of storage room.
	for range 3 {
		var packets []protocol.PacketNumber
		for range endpointBatch {
			pn, _ := sender.recovery.PeekPacketNumber(protocol.Encryption1RTT)
			appendNativeGroupMember(t, tc, sender, getPacketBuffer(), string(make([]byte, 1100)), protocol.ECNNon, sendMetadata{})
			packets = append(packets, pn)
		}
		sender.finish()
		synctest.Wait()
		require.NoError(t, sender.consume())
		time.Sleep(100 * time.Microsecond)
		// Each wire-valid singleton ACK names an actual emitted PN, including
		// the production generator's legal gaps.
		for _, pn := range packets {
			_, err := sender.recovery.ReceivedAck(&wire.AckFrame{AckRanges: []wire.AckRange{{Smallest: pn, Largest: pn}}}, protocol.Encryption1RTT, monotime.Now())
			require.NoError(t, err)
		}
	}
}

func TestNativeSoftwarePhysicalWindowUsesNaturalGrouping(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		tc, sender, socket := nativeSoftwareBoundaryConnection(t)
		warmNativeSoftwareRecovery(t, tc, sender)
		socket.hold = nativeProbeHoldAll
		queueNativeSoftwareStream(t, tc.conn)
		var result emissionResult
		for range len(sender.records) + 1 {
			result = tc.conn.emission.advanceForPlatform(monotime.Now(), false, true)
			require.NoError(t, result.err)
			sender.finish() // normal emission ends every actual opportunity
			if !result.retry {
				break
			}
		}
		require.False(t, result.retry, "existing policy or canonical records bound these real retries")
		var packets, datagrams, buffers, records int
		for i := range sender.records {
			r := &sender.records[i]
			if r.state == ordinaryFree {
				continue
			}
			records++
			packets += r.count
			datagrams += r.datagrams
			buffers += len(r.members)
			require.LessOrEqual(t, len(r.members), endpointBatch)
			require.LessOrEqual(t, int(r.backing), 1<<20)
		}
		require.Greater(t, datagrams, endpointBatch, "available physical storage is not a separate packet-equivalent budget")
		require.Equal(t, packets, datagrams, "each short-header datagram was immediately registered")
		require.EqualValues(t, packets+3*endpointBatch, tc.conn.connStats.PacketsSent.Load())
		require.Greater(t, records, 1, "an empty-supply prefix is followed by busy natural grouping")
		require.LessOrEqual(t, records, len(sender.records))
		require.Greater(t, buffers, records)
		// Real SendMode may stop on a partial final buffer. Its decision is
		// retained; storage density is not permission to bypass congestion.
		t.Logf("stop=%d packets=%d buffers=%d records=%d", result.stop, packets, buffers, records)
		require.NotEqual(t, ordinaryFree, sender.records[0].state)
		socket.release()
		synctest.Wait()
		require.NotEqual(t, ordinaryFree, sender.records[0].state, "native publication cannot retire the connection record")
		require.NoError(t, sender.consume())
		require.False(t, sender.WouldBlock(), "ordered consumption reaches true Free")
	})
}
