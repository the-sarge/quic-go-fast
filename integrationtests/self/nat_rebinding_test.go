package self_test

import (
	"testing"

	"github.com/quic-go/quic-go/qlog"
	"github.com/stretchr/testify/require"
)

func TestNATRebinding(t *testing.T) {
	f := newNATRebindingFixture(t)
	f.startWriter(t, nil, nil)
	data, err := f.receive()
	require.NoError(t, err)
	require.Equal(t, PRData, data)
	f.conn.CloseWithError(0, "")
	tr := f.trace

	// A compact probe can be followed by an expanded probe to validate the MTU.
	pathChallenges := make(map[[8]byte]int)
	for _, event := range tr.recorder.Events(qlog.PacketSent{}) {
		p := event.(qlog.PacketSent)
		for _, f := range p.Frames {
			switch fr := f.Frame.(type) {
			case *qlog.PathChallengeFrame:
				pathChallenges[fr.Data] = p.Raw.Length
			}
		}
	}
	require.NotEmpty(t, pathChallenges)

	// check that a PATH_RESPONSE with the correct data was received
	var foundExpandedPathResponse bool
	for _, p := range tr.getRcvdShortHeaderPackets() {
		for _, f := range p.frames {
			switch fr := f.Frame.(type) {
			case *qlog.PathResponseFrame:
				require.Contains(t, pathChallenges, fr.Data)
				if pathChallenges[fr.Data] >= 1200 {
					foundExpandedPathResponse = true
				}
			}
		}
	}
	require.True(t, foundExpandedPathResponse)
}
