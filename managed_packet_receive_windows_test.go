//go:build windows

package quic

import (
	"bytes"
	"context"
	"net"
	"os"
	"testing"
	"time"

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
