package quic

import (
	"context"
	"net"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// queueLifetimeConn exposes the socket read/write boundaries without a reader cache.
// Its packets have already been acquired, so returned pool addresses aren't reused.
type queueLifetimeConn struct {
	net.PacketConn
	packets   chan receivedPacket
	stop      chan struct{}
	stopOnce  sync.Once
	writeGate chan struct{}
	writes    int
	closed    bool
	deadline  time.Time
}

func newQueueLifetimeConn() *queueLifetimeConn {
	return &queueLifetimeConn{packets: make(chan receivedPacket, 8), stop: make(chan struct{})}
}

func (c *queueLifetimeConn) ReadPacket() (receivedPacket, error) {
	select {
	case p := <-c.packets:
		return p, nil
	case <-c.stop:
		return receivedPacket{}, net.ErrClosed
	}
}

func (c *queueLifetimeConn) WritePacket(b []byte, _ net.Addr, _ []byte, _ uint16, _ protocol.ECN) (int, error) {
	c.writes++
	if c.writeGate != nil {
		<-c.writeGate
	}
	return len(b), nil
}

func (c *queueLifetimeConn) SetReadDeadline(d time.Time) error {
	c.deadline = d
	if !d.IsZero() {
		c.stopOnce.Do(func() { close(c.stop) })
	}
	return nil
}
func (c *queueLifetimeConn) Close() error                   { c.closed = true; return nil }
func (c *queueLifetimeConn) capabilities() connCapabilities { return connCapabilities{} }

func queueLifetimePacket(data []byte) receivedPacket {
	b := getPacketBuffer()
	b.Data = append(b.Data, data...)
	return receivedPacket{buffer: b, data: b.Data, remoteAddr: &net.UDPAddr{Port: 1234}}
}

func TestTransportQueueLifetimeNonQUICPending(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newQueueLifetimeConn()
		tr := &Transport{Conn: c}
		defer tr.Close()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, _, err := tr.ReadNonQUICPacket(ctx, nil)
		require.ErrorIs(t, err, context.Canceled)
		p := queueLifetimePacket([]byte{0, 1, 2, 3})
		c.packets <- p
		synctest.Wait()
		require.Equal(t, 1, p.buffer.refCount)
		require.NoError(t, tr.Close())
		synctest.Wait()
		require.Empty(t, tr.nonQUICPackets)
		require.Zero(t, p.buffer.refCount)
		require.False(t, c.closed)
		require.True(t, c.deadline.IsZero())
	})
}

func TestTransportQueueLifetimeConcurrentReaders(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newQueueLifetimeConn()
		tr := &Transport{Conn: c}
		require.NoError(t, tr.init(false))
		defer tr.Close()
		published := tr.nonQUICPackets
		require.NotNil(t, published, "publish the queue before the listener or first reader starts")
		require.False(t, tr.readingNonQUICPackets.Load())
		// A first reader must opt in without replacing the published queue.
		var readers sync.WaitGroup
		for range 2 {
			readers.Go(func() {
				out := make([]byte, 4)
				n, addr, err := tr.ReadNonQUICPacket(context.Background(), out)
				if assert.NoError(t, err) {
					assert.Equal(t, []byte{0, 1, 2, 3}, out[:n])
					assert.Equal(t, &net.UDPAddr{Port: 1234}, addr)
				}
			})
		}
		synctest.Wait()
		require.Equal(t, published, tr.nonQUICPackets)
		packets := []receivedPacket{
			queueLifetimePacket([]byte{0, 1, 2, 3}),
			queueLifetimePacket([]byte{0, 1, 2, 3}),
			queueLifetimePacket([]byte{0, 1, 2, 3}),
		}
		// Drive the listener's consuming handoff before its terminal barrier.
		// Two readers and cleanup each dispose exclusively received input.
		for _, p := range packets {
			tr.handlePacket(p)
		}
		require.NoError(t, tr.Close())
		readers.Wait()
		synctest.Wait()
		for _, p := range packets {
			require.Zero(t, p.buffer.refCount)
		}
		require.Empty(t, published)
		require.Equal(t, published, tr.nonQUICPackets)
	})
}

func TestTransportQueueLifetimeReset(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newQueueLifetimeConn()
		c.writeGate = make(chan struct{})
		defer func() {
			select {
			case <-c.writeGate:
			default:
				close(c.writeGate)
			}
		}()
		tr := &Transport{Conn: c, ConnectionIDLength: 4, StatelessResetKey: &StatelessResetKey{1}}
		require.NoError(t, tr.init(false))
		defer tr.Close()
		// Acquire every input before allowing a return to avoid pool address reuse.
		var packets []receivedPacket
		for range 5 {
			packets = append(packets, queueLifetimePacket(append([]byte{0x40, 1, 2, 3, 4}, make([]byte, protocol.MinStatelessResetSize)...)))
		}
		c.packets <- packets[0]
		synctest.Wait()
		require.Equal(t, 1, c.writes)
		require.Equal(t, 1, packets[0].buffer.refCount, "active reset owns input through its write")
		for _, p := range packets[1:] {
			c.packets <- p
		}
		synctest.Wait()
		require.Len(t, tr.statelessResetQueue, 4)
		require.NoError(t, tr.Close())
		synctest.Wait()
		require.False(t, c.closed, "caller-owned socket must survive Close")
		require.True(t, c.deadline.IsZero(), "restore the read deadline")
		require.Equal(t, 1, c.writes, "Close must finish without joining the blocked write")
		// Let the existing sending owner finish and perform its terminal cleanup.
		close(c.writeGate)
		synctest.Wait()
		require.Empty(t, tr.statelessResetQueue)
		for _, p := range packets {
			require.Zero(t, p.buffer.refCount)
		}
	})
}
