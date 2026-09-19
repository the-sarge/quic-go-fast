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

	// check that a PATH_CHALLENGE was sent
	var pathChallenge [8]byte
	var foundPathChallenge bool
	for _, p := range tr.getSentShortHeaderPackets() {
		for _, f := range p.frames {
			switch fr := f.Frame.(type) {
			case *qlog.PathChallengeFrame:
				pathChallenge = fr.Data
				foundPathChallenge = true
			}
		}
	}
	require.True(t, foundPathChallenge)

	// check that a PATH_RESPONSE with the correct data was received
	var foundPathResponse bool
	for _, p := range tr.getRcvdShortHeaderPackets() {
		for _, f := range p.frames {
			switch fr := f.Frame.(type) {
			case *qlog.PathResponseFrame:
				require.Equal(t, pathChallenge, fr.Data)
				foundPathResponse = true
			}
		}
	}
	require.True(t, foundPathResponse)
}
