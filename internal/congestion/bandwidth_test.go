package congestion

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBandwidthFromDelta(t *testing.T) {
	require.Equal(t, Bandwidth(8000), BandwidthFromDelta(1, time.Millisecond))
	require.Equal(t, Bandwidth(16000), BandwidthFromDelta(2, time.Millisecond))
	require.Equal(t, Bandwidth(4000), BandwidthFromDelta(1, 2*time.Millisecond))
}
