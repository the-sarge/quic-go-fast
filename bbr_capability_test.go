package quic

import (
	"testing"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

type countingCapabilitiesConn struct {
	sendConn
	caps    connCapabilities
	queries int
}

func (c *countingCapabilitiesConn) capabilities() connCapabilities {
	c.queries++
	return c.caps
}

// A BBR opportunity queries capabilities once; a change between opportunities
// takes effect at the next one; outside an opportunity queries stay live.
func TestBBRCapabilitySnapshotPerOpportunity(t *testing.T) {
	c := newEmissionTestConnection(t, false).conn
	installEmissionBBRECN(c)
	conn := &countingCapabilitiesConn{sendConn: c.conn, caps: c.conn.capabilities()}
	conn.caps.ECN = true
	c.conn = conn
	now := monotime.Now()
	c.emission.bbr = newBBRSendPolicy(100_000_000, 1200, now)
	q := c.emission.queue.(*sendQueue)
	defer func() {
		for len(q.queue) > 0 {
			(<-q.queue).release()
		}
	}()
	for _, d := range []string{"first", "second"} {
		require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte(d)}))
	}

	conn.queries = 0
	require.True(t, c.emission.sendBounded(now, true).progress)
	require.Equal(t, 1, conn.queries, "GSO admission, GSO assembly and ECN marking share one query")
	first := <-q.queue
	defer first.release()
	require.Equal(t, protocol.ECT0, first.ecn)

	conn.caps.ECN = false
	conn.queries = 0
	require.True(t, c.emission.sendBounded(now, true).progress)
	require.Equal(t, 1, conn.queries)
	second := <-q.queue
	defer second.release()
	require.Equal(t, protocol.ECNUnsupported, second.ecn, "the change is seen by the next opportunity")

	conn.queries = 0
	c.sentPacketHandler.ECNMode(true)
	c.sentPacketHandler.ECNMode(true)
	require.Equal(t, 2, conn.queries, "outside an opportunity each ECN query is live")
}
