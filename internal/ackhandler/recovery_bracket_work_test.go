//go:build bbrworkcount

package ackhandler

import (
	"math/bits"
	"testing"

	"github.com/quic-go/quic-go/internal/congestion"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
)

// The bracketed search never probes more than the predecessor's bound,
// bits.Len(n), and probes nothing in a space without skipped numbers.
func TestRecoveryLowerBoundProbes(t *testing.T) {
	for _, skipEvery := range []int{0, 1000, 7, 1} {
		r := &recoveryEvidence{measured: true}
		var pn protocol.PacketNumber
		for i := range maxRecoveryOutcomes + 3000 {
			if skipEvery > 0 && i%skipEvery == 0 {
				pn += 2
			}
			r.sent(congestion.PacketInfo{Space: protocol.Encryption1RTT, EncryptionLevel: protocol.Encryption1RTT, PacketNumber: pn, Ordinal: uint64(i) + 1, RegistrationValid: true}, 0)
			pn++
		}
		q := &r.index.spaces[recoverySpace(protocol.Encryption1RTT)]
		require.Equal(t, maxRecoveryOutcomes, q.n)
		for x := q.first - 2; x <= q.last+2; x += protocol.PacketNumber(1 + q.n/500) {
			resetServiceWork()
			got := r.lowerBound(q, x)
			nodes := serviceCounters[serviceNone].Nodes
			resetServiceWork()
			require.Equal(t, frozenLowerBound(r, q, x), got)
			require.LessOrEqual(t, nodes, uint64(bits.Len(uint(q.n))), "skip every %d, pn %d", skipEvery, x)
			if skipEvery == 0 {
				require.Zero(t, nodes, "no skipped numbers: no probe")
			}
		}
	}
}
