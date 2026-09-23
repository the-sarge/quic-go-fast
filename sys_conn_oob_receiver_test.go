//go:build darwin || linux || freebsd

package quic

import (
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type sysConnTestResult struct {
	packet receivedPacket
	err    error
}

// sysConnTestReceiver owns packets until shutdown, after the test's assertions.
// Only the test goroutine receives results and shuts the receiver down.
type sysConnTestReceiver struct {
	udpConn   *net.UDPConn
	conn      *oobConn
	results   chan sysConnTestResult
	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
	received  []*packetBuffer
}

func runSysConnServer(t *testing.T, network string, addr *net.UDPAddr) (*net.UDPAddr, *sysConnTestReceiver) {
	t.Helper()
	udpConn, err := net.ListenUDP(network, addr)
	require.NoError(t, err)
	t.Cleanup(func() { udpConn.Close() })
	conn, err := newConn(udpConn, true, true)
	require.NoError(t, err)
	require.True(t, conn.capabilities().DF)
	r := &sysConnTestReceiver{
		udpConn: udpConn, conn: conn,
		results: make(chan sysConnTestResult, 1),
		stop:    make(chan struct{}), done: make(chan struct{}),
	}
	t.Cleanup(r.close)
	go func() {
		defer close(r.done)
		for {
			p, err := conn.ReadPacket()
			select {
			case r.results <- sysConnTestResult{packet: p, err: err}:
			case <-r.stop:
				if p.buffer != nil {
					p.buffer.Release()
				}
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return udpConn.LocalAddr().(*net.UDPAddr), r
}

func (r *sysConnTestReceiver) receive(timeout time.Duration, phase string, sender, destination net.Addr) (receivedPacket, error) {
	context := fmt.Sprintf("phase=%s listener=%s sender=%v destination=%v", phase, r.udpConn.LocalAddr(), sender, destination)
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var result sysConnTestResult
	select {
	case result = <-r.results:
	case <-timer.C:
		// A result queued while the test was descheduled must not be hidden
		// by the timer becoming ready too. This does not wait any longer.
		select {
		case result = <-r.results:
		default:
			return receivedPacket{}, fmt.Errorf("%s: timeout waiting for packet", context)
		}
	}
	if result.err != nil {
		return receivedPacket{}, fmt.Errorf("%s: receive error (%T): %w", context, result.err, result.err)
	}
	r.received = append(r.received, result.packet.buffer)
	return result.packet, nil
}

func (r *sysConnTestReceiver) wait(t *testing.T, timeout time.Duration, phase string, sender, destination net.Addr) receivedPacket {
	t.Helper()
	t.Logf("phase=%s listener=%s sender=%v destination=%v", phase, r.udpConn.LocalAddr(), sender, destination)
	p, err := r.receive(timeout, phase, sender, destination)
	require.NoError(t, err)
	return p
}

func (r *sysConnTestReceiver) close() {
	r.closeOnce.Do(func() {
		close(r.stop)
		r.udpConn.Close()
		<-r.done
		r.conn.releaseReadBuffers()
		for _, buffer := range r.received {
			buffer.Release()
		}
		for {
			select {
			case result := <-r.results:
				if result.packet.buffer != nil {
					result.packet.buffer.Release()
				}
			default:
				return
			}
		}
	})
}

func TestSysConnReceiverErrorBeforeDelivery(t *testing.T) {
	addr, receiver := runSysConnServer(t, "udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, receiver.udpConn.Close())
	_, err := receiver.receive(scaleDuration(time.Second), "dual-stack IPv4", addr, addr)
	require.ErrorIs(t, err, net.ErrClosed)
	require.Contains(t, err.Error(), "phase=dual-stack IPv4")
	require.Contains(t, err.Error(), "listener="+addr.String())
	require.Contains(t, err.Error(), "sender="+addr.String())
	require.Contains(t, err.Error(), "destination="+addr.String())
	require.Contains(t, err.Error(), "*net.OpError")
}

func TestSysConnReceiverPacketBeforeError(t *testing.T) {
	addr, receiver := runSysConnServer(t, "udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	conn, err := net.DialUDP("udp4", nil, addr)
	require.NoError(t, err)
	defer conn.Close()
	_, err = conn.Write([]byte("before close"))
	require.NoError(t, err)
	// Wait for the real packet to reach the handoff before forcing termination.
	require.Eventually(t, func() bool { return len(receiver.results) == 1 }, scaleDuration(time.Second), time.Millisecond)
	require.NoError(t, receiver.udpConn.Close())
	p, err := receiver.receive(0, "IPv4 packet", conn.LocalAddr(), addr)
	require.NoError(t, err)
	require.Equal(t, "before close", string(p.data))
	_, err = receiver.receive(scaleDuration(time.Second), "IPv4 termination", conn.LocalAddr(), addr)
	require.ErrorIs(t, err, net.ErrClosed)
}

func TestSysConnReceiverNoDelivery(t *testing.T) {
	addr, receiver := runSysConnServer(t, "udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	_, err := receiver.receive(time.Millisecond, "IPv4 no delivery", nil, addr)
	require.ErrorContains(t, err, "timeout waiting for packet")
	require.Contains(t, err.Error(), "phase=IPv4 no delivery")
	require.NotErrorIs(t, err, net.ErrClosed)
}

func TestSysConnReceiverCleanup(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(fmt.Sprintf("pending=%t", pending), func(t *testing.T) {
			addr, receiver := runSysConnServer(t, "udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
			conn, err := net.DialUDP("udp4", nil, addr)
			require.NoError(t, err)
			defer conn.Close()
			_, err = conn.Write([]byte("cleanup"))
			require.NoError(t, err)
			if pending {
				require.Eventually(t, func() bool { return len(receiver.results) == 1 }, scaleDuration(time.Second), time.Millisecond)
				// A terminal error queues behind the pending packet; cleanup must
				// unblock the worker without requiring a result consumer.
				require.NoError(t, receiver.udpConn.Close())
			} else {
				p, err := receiver.receive(scaleDuration(time.Second), "IPv4 success", conn.LocalAddr(), addr)
				require.NoError(t, err)
				require.Equal(t, "cleanup", string(p.data))
			}
			receiver.close()
			select {
			case <-receiver.done:
			default:
				t.Fatal("receiver still running after cleanup")
			}
			require.Empty(t, receiver.results)
			// Registered cleanup remains safe after explicit shutdown.
			receiver.close()
		})
	}
}

func TestSysConnReceiverQueuedErrorAtTimeout(t *testing.T) {
	addr, receiver := runSysConnServer(t, "udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, receiver.udpConn.Close())
	// The worker only exits after publishing the error. Both the result and
	// zero-duration timer are ready when receive selects between them.
	select {
	case <-receiver.done:
	case <-time.After(scaleDuration(time.Second)):
		t.Fatal("receiver did not publish its terminal error")
	}
	_, err := receiver.receive(0, "IPv4 expired wait", nil, addr)
	require.ErrorIs(t, err, net.ErrClosed)
}
