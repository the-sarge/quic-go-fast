package quic

import (
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/stretchr/testify/require"
)

func TestRTTObservationExistingFlowControllers(t *testing.T) {
	zero := utils.NewRTTStats()
	zero.UpdateRTT(10*time.Millisecond, 0)
	c := &Conn{rttStats: zero, rttObservation: newRTTObservation(zero)}
	conn := newConnectionFlowController(100, 1000, func(protocol.ByteCount) bool { return true }, c.rttObservation, utils.DefaultLogger)
	stream := newStreamFlowController(4, conn, 100, 1000, protocol.MaxByteCount, c.rttObservation, utils.DefaultLogger)
	now := monotime.Time(time.Second)
	require.NoError(t, stream.UpdateHighestReceived(100, false, now))
	// Models retirement of zero and selection of the sticky primary. These
	// controllers were already constructed and must observe the same new value.
	primary := utils.NewRTTStats()
	primary.UpdateRTT(time.Second, 0)
	c.rttObservation.observe(primary)
	stream.AddBytesRead(60)
	require.EqualValues(t, 260, conn.GetWindowUpdate(now.Add(time.Second)))
	require.EqualValues(t, 260, stream.GetWindowUpdate(now.Add(time.Second)))
	require.Equal(t, time.Second, c.observedRTT().SmoothedRTT())
	require.Equal(t, 10*time.Millisecond, zero.SmoothedRTT(), "observation cannot mutate path recovery")
	c.rttObservation.freeze()
	primary.UpdateRTT(2*time.Second, 0)
	require.Equal(t, time.Second, c.observedRTT().SmoothedRTT())
}

func TestRTTObservationConcurrentReaders(t *testing.T) {
	zero, primary := utils.NewRTTStats(), utils.NewRTTStats()
	zero.UpdateRTT(10*time.Millisecond, 0)
	primary.UpdateRTT(time.Second, 0)
	c := &Conn{rttStats: zero, rttObservation: newRTTObservation(zero)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 1000 {
			rtt := c.rttObservation.SmoothedRTT()
			if rtt != 10*time.Millisecond && rtt != time.Second {
				t.Errorf("unexpected observation: %s", rtt)
				return
			}
			_ = c.ConnectionStats()
		}
	}()
	c.rttObservation.observe(primary)
	<-done
	c.rttObservation.freeze()
}
