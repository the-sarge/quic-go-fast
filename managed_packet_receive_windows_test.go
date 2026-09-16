//go:build windows

package quic

import (
	"bytes"
	"context"
	"net"
	"os"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
)

func TestWindowsManagedReceiveRegistration(t *testing.T) {
	t.Setenv("QUIC_GO_DISABLE_GRO", "0")
	probe, _ := newWindowsOwnedConn(t, true)
	requireUROCapableHost(t, probe)
	endpoint, acquire := newTestManagedEndpoint(t)
	lease, err := acquire()
	require.NoError(t, err)
	t.Cleanup(func() { lease.Close() })
	tr := &Transport{Conn: lease}
	require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil))
	udp := endpoint.(*managedPacketConn).endpoint.conn.(*net.UDPConn)
	require.Equal(t, protocol.MaxCoalescedPacketBufferSize, uroSocketOption(t, udp))
	require.NoError(t, lease.Close())
	peer := listenExternalUDP(t)
	_, err = peer.WriteTo([]byte("after lease"), endpoint.LocalAddr())
	require.NoError(t, err)
	require.NoError(t, endpoint.SetReadDeadline(time.Now().Add(time.Second)))
	b := make([]byte, 1500)
	n, addr, err := endpoint.ReadFrom(b)
	require.NoError(t, err)
	require.Equal(t, "after lease", string(b[:n]))
	require.Equal(t, peer.LocalAddr().String(), addr.String())
}

type windowsManagedFilter struct {
	net.PacketConn
	peer     net.Addr
	filtered chan struct{}
}

func (c *windowsManagedFilter) ReadFrom(b []byte) (int, net.Addr, error) {
	for {
		n, addr, err := c.PacketConn.ReadFrom(b)
		if err != nil || addr.String() == c.peer.String() {
			return n, addr, err
		}
		select {
		case c.filtered <- struct{}{}:
		default:
		}
	}
}

func TestWindowsManagedReceiveWrapperLifecycle(t *testing.T) {
	for _, network := range []string{"udp4", "udp6"} {
		t.Run(network, func(t *testing.T) {
			t.Setenv("QUIC_GO_DISABLE_GRO", "0")
			probe, _ := newWindowsOwnedConn(t, true)
			requireUROCapableHost(t, probe)
			ip := net.IPv4(127, 0, 0, 1)
			if network == "udp6" {
				ip = net.IPv6loopback
			}
			endpoint, acquire, err := (&Transport{}).NewManagedPacketEndpointV1(network, &net.UDPAddr{IP: ip})
			require.NoError(t, err)
			t.Cleanup(func() { endpoint.Close() })
			lease, err := acquire()
			require.NoError(t, err)
			t.Cleanup(func() { lease.Close() })
			listen := func() *net.UDPConn {
				t.Helper()
				c, err := net.ListenUDP(network, &net.UDPAddr{IP: ip})
				require.NoError(t, err)
				t.Cleanup(func() { c.Close() })
				return c
			}
			selected, foreign := listen(), listen()
			wrapper := &windowsManagedFilter{PacketConn: lease, peer: selected.LocalAddr(), filtered: make(chan struct{}, 1)}
			tr := &Transport{Conn: wrapper}
			require.NoError(t, tr.ConfigureManagedPacketIOV1(wrapper, lease, nil))
			t.Cleanup(func() { tr.Close() })
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, _, err = tr.ReadNonQUICPacket(ctx, make([]byte, 1500))
			require.ErrorIs(t, err, context.Canceled)
			_, err = foreign.WriteTo([]byte("foreign"), endpoint.LocalAddr())
			require.NoError(t, err)
			select {
			case <-wrapper.filtered:
			case <-time.After(scaleDuration(5 * time.Second)):
				t.Fatal("wrapper did not filter foreign datagram")
			}
			want := bytes.Repeat([]byte{1}, 1232)
			_, err = selected.WriteTo(want, endpoint.LocalAddr())
			require.NoError(t, err)
			ctx, cancel = context.WithTimeout(context.Background(), scaleDuration(5*time.Second))
			defer cancel()
			b := make([]byte, 1500)
			n, addr, err := tr.ReadNonQUICPacket(ctx, b)
			require.NoError(t, err)
			require.Equal(t, want, b[:n])
			require.Equal(t, selected.LocalAddr().String(), addr.String())
			if network == "udp6" {
				require.NoError(t, endpoint.Close(), "parent close interrupts the active reader")
			} else {
				require.NoError(t, lease.Close(), "return interrupts and joins the active reader")
				_, err = foreign.WriteTo([]byte("next ordinary read"), endpoint.LocalAddr())
				require.NoError(t, err)
				require.NoError(t, endpoint.SetReadDeadline(time.Now().Add(time.Second)))
				n, _, err = endpoint.ReadFrom(b)
				require.NoError(t, err)
				require.Equal(t, "next ordinary read", string(b[:n]), "the prior lease filter must not persist")
			}
			require.NoError(t, tr.Close())
		})
	}
}

func TestWindowsManagedReceiveDisabled(t *testing.T) {
	t.Setenv("QUIC_GO_DISABLE_GRO", "1")
	endpoint, acquire := newTestManagedEndpoint(t)
	lease, err := acquire()
	require.NoError(t, err)
	t.Cleanup(func() { lease.Close() })
	tr := &Transport{Conn: lease}
	require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil))
	require.NoError(t, lease.Close())
	peer := listenExternalUDP(t)
	want := bytes.Repeat([]byte{7}, 60000)
	_, err = peer.WriteTo(want, endpoint.LocalAddr())
	require.NoError(t, err)
	require.NoError(t, endpoint.SetReadDeadline(time.Now().Add(time.Second)))
	b := make([]byte, 65535)
	n, _, err := endpoint.ReadFrom(b)
	require.NoError(t, err)
	require.Equal(t, want, b[:n])
}

// Script only the native message-I/O boundary; the Windows decoder and the
// public endpoint/lease operations own splitting, deadlines and disposal.
func TestWindowsManagedReceiveHandback(t *testing.T) {
	for _, quicMode := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary sibling retained", true: "QUIC sibling discarded"}[quicMode], func(t *testing.T) {
			source := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 1234}
			script := &uroReadConn{t: t, payloads: [][]byte{[]byte("onetwo"), []byte("newend")}, oobs: [][]byte{coalescedInfoMsg(t, 3), coalescedInfoMsg(t, 3)}, addr: source}
			receiver := newUROConn(t, script)
			endpoint, acquire, err := newManagedPacketEndpoint(script)
			require.NoError(t, err)
			t.Cleanup(func() { endpoint.Close() })
			endpoint.(*managedPacketConn).endpoint.receiver = receiver
			lease, err := acquire()
			require.NoError(t, err)
			if quicMode {
				tr := &Transport{Conn: lease}
				require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil))
			}
			b := make([]byte, 64)
			n, addr, err := lease.ReadFrom(b)
			require.NoError(t, err)
			require.Equal(t, "one", string(b[:n]))
			addr.(*net.UDPAddr).IP[0] = 99
			require.Equal(t, "192.0.2.1", source.IP.String(), "public addresses must not alias retained siblings")
			require.Len(t, receiver.delivery.pending, 1)
			require.Equal(t, protocol.MaxCoalescedPacketBufferSize, cap(receiver.delivery.pending[0].buffer.slab.buf.Data))
			require.NoError(t, lease.SetReadDeadline(time.Now().Add(-time.Second)))
			_, _, err = lease.ReadFrom(b)
			require.ErrorIs(t, err, os.ErrDeadlineExceeded, "buffered siblings obey logical deadlines")
			require.NoError(t, lease.Close())
			n, addr, err = endpoint.ReadFrom(b)
			require.NoError(t, err)
			require.Equal(t, source.String(), addr.String())
			if quicMode {
				require.Equal(t, "new", string(b[:n]), "consumed QUIC storage must not replay")
			} else {
				require.Equal(t, "two", string(b[:n]))
			}
			next, err := acquire()
			require.NoError(t, err)
			n, _, err = next.ReadFrom(b)
			require.NoError(t, err)
			if quicMode {
				require.Equal(t, "end", string(b[:n]))
			} else {
				require.Equal(t, "new", string(b[:n]))
			}
			require.NoError(t, next.Close())
			require.NoError(t, endpoint.Close())
			require.Empty(t, receiver.delivery.pending)
		})
	}
}

func TestWindowsManagedReceiveRejectsTruncatedMetadata(t *testing.T) {
	script := &uroReadConn{t: t, payloads: [][]byte{[]byte("badbad"), []byte("ok!")}, oobs: [][]byte{coalescedInfoMsg(t, 3), nil}, flags: []int{windows.MSG_CTRUNC, 0}, addr: &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 1234}}
	receiver := newUROConn(t, script)
	endpoint, acquire, err := newManagedPacketEndpoint(script)
	require.NoError(t, err)
	t.Cleanup(func() { endpoint.Close() })
	endpoint.(*managedPacketConn).endpoint.receiver = receiver
	lease, err := acquire()
	require.NoError(t, err)
	tr := &Transport{Conn: lease}
	require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil))
	b := make([]byte, 64)
	n, _, err := lease.ReadFrom(b)
	require.NoError(t, err)
	require.Equal(t, "ok!", string(b[:n]))
	require.NoError(t, lease.Close())
	require.Empty(t, receiver.delivery.pending)
}

type windowsManagedNativeObserver struct {
	OOBCapablePacketConn
	aggregates int
}

func (c *windowsManagedNativeObserver) ReadMsgUDP(b, oob []byte) (int, int, int, *net.UDPAddr, error) {
	n, nn, flags, addr, err := c.OOBCapablePacketConn.ReadMsgUDP(b, oob)
	if err == nil && n > 1232 {
		c.aggregates++
	}
	return n, nn, flags, addr, err
}

// The separate peer uses Q04's existing one-burst protocol. FIONREAD observes
// queued bytes without consuming them before lease return. The platform decoder
// remains the metadata interpreter; this fixture only counts large native reads.
func TestWindowsManagedReceiveNativeHandback(t *testing.T) {
	peerAddress := os.Getenv("QUIC_GO_R01_W_URO_PEER")
	if peerAddress == "" {
		t.Skip("requires a separate endpoint via QUIC_GO_R01_W_URO_PEER")
	}
	peer, err := net.ResolveUDPAddr("udp4", peerAddress)
	require.NoError(t, err)
	t.Setenv("QUIC_GO_DISABLE_GRO", "0")
	endpoint, acquire, err := (&Transport{}).NewManagedPacketEndpointV1("udp4", &net.UDPAddr{IP: net.IPv4zero})
	require.NoError(t, err)
	t.Cleanup(func() { endpoint.Close() })
	lease, err := acquire()
	require.NoError(t, err)
	t.Cleanup(func() { lease.Close() })
	tr := &Transport{Conn: lease}
	require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil))
	require.NoError(t, tr.Close())
	e := endpoint.(*managedPacketConn).endpoint
	receiver, ok := e.receiver.(*windowsConn)
	require.True(t, ok, "native managed coalescing must be enabled")
	observer := &windowsManagedNativeObserver{OOBCapablePacketConn: receiver.OOBCapablePacketConn}
	receiver.OOBCapablePacketConn = observer
	udp := e.conn.(*net.UDPConn)
	raw, err := udp.SyscallConn()
	require.NoError(t, err)
	_, err = lease.WriteTo([]byte("Q04 URO"), peer)
	require.NoError(t, err)
	var queued uint32
	require.Eventually(t, func() bool {
		var ioctlErr error
		var returned uint32
		require.NoError(t, raw.Control(func(fd uintptr) {
			// winsock2.h: FIONREAD = _IOR('f', 127, u_long).
			ioctlErr = windows.WSAIoctl(windows.Handle(fd), 0x4004667f, nil, 0, (*byte)(unsafe.Pointer(&queued)), 4, &returned, nil, 0)
		}))
		require.NoError(t, ioctlErr)
		return queued > 0
	}, 10*time.Second, time.Millisecond)
	t.Logf("queued bytes before lease return: %d", queued)
	require.NoError(t, lease.Close())
	require.NoError(t, endpoint.SetReadDeadline(time.Now().Add(10*time.Second)))
	b := make([]byte, 1500)
	read := func(c net.PacketConn, i int) {
		t.Helper()
		size := 1232
		if i == 32 {
			size = 500
		}
		n, addr, err := c.ReadFrom(b)
		require.NoError(t, err)
		require.Equal(t, peer.String(), addr.String())
		require.Equal(t, bytes.Repeat([]byte{byte(i)}, size), b[:n])
	}
	read(endpoint, 1)
	next, err := acquire()
	require.NoError(t, err)
	t.Cleanup(func() { next.Close() })
	for i := 2; i <= 32; i++ {
		read(next, i)
	}
	require.Positive(t, observer.aggregates, "kernel coalescing must engage across separate endpoints")
	require.NoError(t, next.Close())
	t.Logf("managed Windows handback: %d coalesced read(s); 32 exact datagrams across ordinary read and next lease", observer.aggregates)
}
