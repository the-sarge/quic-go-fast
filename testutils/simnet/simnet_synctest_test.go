package simnet

import (
	"math"
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func newConn(simnet *Simnet, address *net.UDPAddr, linkSettings NodeBiDiLinkSettings) *SimConn {
	return simnet.NewEndpoint(address, linkSettings)
}

func TestSimpleSimNet(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		router := &Simnet{Router: &PerfectRouter{}}

		const latency = 10 * time.Millisecond
		linkSettings := NodeBiDiLinkSettings{
			Downlink: LinkSettings{},
			Uplink:   LinkSettings{},
			Latency:  latency,
		}

		addressA := net.UDPAddr{
			IP:   net.ParseIP("1.0.0.1"),
			Port: 8000,
		}
		connA := newConn(router, &addressA, linkSettings)
		addressB := net.UDPAddr{
			IP:   net.ParseIP("1.0.0.2"),
			Port: 8000,
		}
		connB := newConn(router, &addressB, linkSettings)

		router.Start()
		defer router.Close()

		start := time.Now()
		connA.WriteTo([]byte("hello"), &addressB)
		buf := make([]byte, 1024)
		n, from, err := connB.ReadFrom(buf)
		require.NoError(t, err)
		require.Equal(t, "hello", string(buf[:n]))
		require.Equal(t, addressA.String(), from.String())
		observedLatency := time.Since(start)

		// Only downlink has latency now (uplink is instant)
		expectedLatency := latency
		percentDiff := math.Abs(float64(observedLatency-expectedLatency) / float64(expectedLatency))
		t.Logf("observed latency: %v, expected latency: %v, percent diff: %v", observedLatency, expectedLatency, percentDiff)
		if percentDiff > 0.30 {
			t.Fatalf("latency is wrong: %v. percent off: %v", observedLatency, percentDiff)
		}
	})
}

func TestSimnetShutdownSaturatedEndpoint(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		n := &Simnet{Router: &PerfectRouter{}}
		addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8000}
		c := n.NewEndpoint(addr, NodeBiDiLinkSettings{})
		defer c.Close()
		require.NoError(t, n.Start())

		for i := range 33 {
			_, err := c.WriteTo([]byte{byte(i)}, addr)
			require.NoError(t, err)
		}
		synctest.Wait()
		require.Equal(t, 33, c.Stats().PacketsRcvd)

		closed := make(chan error, 1)
		go func() { closed <- n.Close() }()
		synctest.Wait()
		select {
		case err := <-closed:
			require.NoError(t, err)
		default:
			t.Error("network Close is blocked on an unread endpoint")
			// Release the old implementation so the failing test can finish.
			require.NoError(t, c.Close())
			require.NoError(t, <-closed)
			return
		}

		buf := make([]byte, 1)
		for i := range 32 {
			n, from, err := c.ReadFrom(buf)
			require.NoError(t, err)
			require.Equal(t, 1, n)
			require.Equal(t, addr, from)
			require.Equal(t, byte(i), buf[0])
		}
		require.Equal(t, 33, c.Stats().PacketsRcvd)
	})
}

func TestSimnetShutdownMultipleSaturatedLinks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		n := &Simnet{Router: &PerfectRouter{}}
		a := n.NewEndpoint(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8000}, NodeBiDiLinkSettings{})
		b := n.NewEndpoint(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8001}, NodeBiDiLinkSettings{})
		defer a.Close()
		defer b.Close()
		require.NoError(t, n.Start())

		for range 33 {
			_, err := a.WriteTo([]byte("a to b"), b.LocalAddr())
			require.NoError(t, err)
			_, err = b.WriteTo([]byte("b to a"), a.LocalAddr())
			require.NoError(t, err)
		}
		synctest.Wait()
		require.Equal(t, 33, a.Stats().PacketsRcvd)
		require.Equal(t, 33, b.Stats().PacketsRcvd)

		closed := make(chan error, 1)
		go func() { closed <- n.Close() }()
		synctest.Wait()
		select {
		case err := <-closed:
			require.NoError(t, err)
		default:
			t.Error("network Close is blocked joining saturated links")
			require.NoError(t, a.Close())
			require.NoError(t, b.Close())
			require.NoError(t, <-closed)
		}
	})
}

func TestSimnetLiveDeliveryBackpressure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		n := &Simnet{Router: &PerfectRouter{}}
		addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8000}
		c := n.NewEndpoint(addr, NodeBiDiLinkSettings{})
		require.NoError(t, n.Start())
		defer n.Close()
		defer c.Close()

		for i := range 34 {
			_, err := c.WriteTo([]byte{byte(i)}, addr)
			require.NoError(t, err)
		}
		synctest.Wait()
		// The 33rd receive attempt must wait for capacity before the worker
		// can attempt the 34th, rather than dropping packets while live.
		require.Equal(t, 33, c.Stats().PacketsRcvd)
		buf := make([]byte, 1)
		for i := range 34 {
			n, _, err := c.ReadFrom(buf)
			require.NoError(t, err)
			require.Equal(t, 1, n)
			require.Equal(t, byte(i), buf[0])
		}
		synctest.Wait()
		require.Equal(t, 34, c.Stats().PacketsRcvd)
	})
}

func TestSimnetShutdownEndpointFirst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		n := &Simnet{Router: &PerfectRouter{}}
		addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8000}
		c := n.NewEndpoint(addr, NodeBiDiLinkSettings{})
		require.NoError(t, n.Start())

		for range 33 {
			_, err := c.WriteTo([]byte("packet"), addr)
			require.NoError(t, err)
		}
		synctest.Wait()
		require.Equal(t, 33, c.Stats().PacketsRcvd)
		require.NoError(t, c.Close())
		require.NoError(t, n.Close())
	})
}
