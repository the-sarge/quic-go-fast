//go:build darwin || linux || freebsd

package quic

import (
	"fmt"
	"net"
	"runtime"
	"sync"
	"syscall"
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

// listenDualStackUDP binds a dual-stack wildcard socket on a port that no IPv4
// socket holds. On darwin, a dual-stack bind with port 0 can be given a port
// that an IPv4 socket holds on 127.0.0.1 or 0.0.0.0, and IPv4 datagrams to
// 127.0.0.1 then go to that socket (#717, #528). The IPv4 port-0 allocator
// respects every IPv4 holder, and an explicit dual-stack bind succeeds next to
// a 0.0.0.0 socket, so an IPv4 reservation picks the port and holds it until
// the receiver is bound; the receiver then keeps IPv4 sockets off it. An IPv6
// socket can still hold the reserved port; that reservation stays open until
// return so that the next one gets a different port.
func listenDualStackUDP(t *testing.T) *net.UDPConn {
	t.Helper()
	if runtime.GOOS != "darwin" {
		conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero})
		require.NoError(t, err)
		return conn
	}
	var reservations []*net.UDPConn
	defer func() {
		for _, r := range reservations {
			r.Close()
		}
	}()
	for range 16 {
		reservation, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero})
		require.NoError(t, err)
		reservations = append(reservations, reservation)
		port := reservation.LocalAddr().(*net.UDPAddr).Port
		conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: port})
		if err == nil {
			return conn
		}
		require.ErrorIs(t, err, syscall.EADDRINUSE, "dual-stack bind on reserved port %d", port)
	}
	t.Fatal("IPv6 sockets held every reserved port")
	return nil
}

func runSysConnServer(t *testing.T, network string, addr *net.UDPAddr) (*net.UDPAddr, *sysConnTestReceiver) {
	t.Helper()
	var udpConn *net.UDPConn
	if network == "udp" {
		require.True(t, addr.IP == nil || addr.IP.IsUnspecified(), "dual-stack receiver address %s", addr)
		require.Zero(t, addr.Port, "dual-stack receiver address %s", addr)
		udpConn = listenDualStackUDP(t)
	} else {
		var err error
		udpConn, err = net.ListenUDP(network, addr)
		require.NoError(t, err)
	}
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

// On darwin, a dual-stack receiver bound with port 0 can share its port with
// an IPv4 socket, and IPv4 datagrams to 127.0.0.1 then go to that socket
// (#528). With 500 ports held, about 9 of these 300 receivers would collide
// if they were bound that way.
func TestListenDualStackUDPAvoidsHeldIPv4Ports(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the dual-stack port-0 collision is specific to darwin")
	}
	held := make(map[int]bool)
	for i := range 500 {
		ip := net.IPv4(127, 0, 0, 1)
		if i%5 == 0 {
			ip = net.IPv4zero
		}
		conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: ip})
		require.NoError(t, err)
		t.Cleanup(func() { conn.Close() })
		held[conn.LocalAddr().(*net.UDPAddr).Port] = true
	}
	sender := newUDPConnLocalhost(t)

	var collisions []string
	b := make([]byte, 16)
	for range 300 {
		conn := listenDualStackUDP(t)
		addr := conn.LocalAddr().(*net.UDPAddr)
		if held[addr.Port] {
			collisions = append(collisions, addr.String())
			conn.Close()
			continue
		}
		require.True(t, addr.IP.IsUnspecified() && addr.IP.To4() == nil, "local address %s", addr)
		_, err := sender.WriteToUDP([]byte("dual-stack"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: addr.Port})
		require.NoError(t, err)
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(scaleDuration(time.Second))))
		n, from, err := conn.ReadFromUDP(b)
		conn.Close()
		require.NoError(t, err, "IPv4 datagram to 127.0.0.1:%d", addr.Port)
		require.Equal(t, "dual-stack", string(b[:n]))
		require.Equal(t, sender.LocalAddr().(*net.UDPAddr).Port, from.Port)
	}
	require.Empty(t, collisions, "dual-stack receivers were given ports held by IPv4 sockets")
}
