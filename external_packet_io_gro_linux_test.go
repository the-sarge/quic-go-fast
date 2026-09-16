//go:build linux

package quic

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/testutils/events"
	"github.com/stretchr/testify/require"
)

// Providing ReadBatch keeps receive I/O on the participating wrapper.
type externalGROConn struct {
	OOBCapablePacketConn
	batch batchConn
}

// This outer wrapper filters ReadMsgUDP but inherits net.Conn / SyscallConn.
// It has no participating ReadBatch path.
type externalGROReadMsgConn struct {
	*net.UDPConn
	selected *net.UDPAddr
}

func (c *externalGROReadMsgConn) ReadMsgUDP(b, oob []byte) (int, int, int, *net.UDPAddr, error) {
	for {
		n, nn, flags, addr, err := c.UDPConn.ReadMsgUDP(b, oob)
		if err != nil || addr.String() == c.selected.String() {
			return n, nn, flags, addr, err
		}
	}
}

func TestExternalGRONonBatchWrapper(t *testing.T) {
	udp := listenExternalUDP(t)
	conn := &externalGROReadMsgConn{UDPConn: udp, selected: udp.LocalAddr().(*net.UDPAddr)}
	var recorder events.Recorder
	tr := &Transport{Conn: conn, Tracer: &recorder}
	require.NoError(t, tr.ConfigureExternalPacketIOV1(conn, true, nil))
	require.NoError(t, tr.Close())
	require.Contains(t, externalPacketIOEvent(t, &recorder), "receive_eligible=false receive_enabled=false receive_disabled_reason=ineligible_wrapper")
	require.Equal(t, 0, groSocketOption(t, udp), "permission cannot activate descriptor-backed GRO around the wrapper")
}

type externalGROBatchFunc func([]ipv4.Message, int) (int, error)

func (f externalGROBatchFunc) ReadBatch(ms []ipv4.Message, flags int) (int, error) {
	return f(ms, flags)
}

func TestExternalGRORejectsInvalidRead(t *testing.T) {
	short := appendUDPGROMsg(nil, 100)
	(*unix.Cmsghdr)(unsafe.Pointer(&short[0])).SetLen(unix.CmsgLen(2))
	for _, tc := range []struct {
		name  string
		oob   []byte
		flags int
	}{
		{name: "zero segment", oob: appendUDPGROMsg(nil, 0)},
		{name: "negative segment", oob: appendUDPGROMsg(nil, -1)},
		{name: "segment beyond payload", oob: appendUDPGROMsg(nil, 201)},
		{name: "short segment metadata", oob: short},
		{name: "duplicate segment metadata", oob: appendUDPGROMsg(appendUDPGROMsg(nil, 100), 50)},
		{name: "truncated payload", oob: appendUDPGROMsg(nil, 100), flags: unix.MSG_TRUNC},
		{name: "truncated control", flags: unix.MSG_CTRUNC},
		{name: "malformed control", oob: []byte{1}},
		{name: "invalid IPv4 ECN", oob: lifetimeControlMessage(unix.IPPROTO_IP, msgTypeIPTOS, nil)},
		{name: "invalid IPv6 ECN", oob: lifetimeControlMessage(unix.IPPROTO_IPV6, unix.IPV6_TCLASS, []byte{1})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads := 0
			oc := newGROConn(t, externalGROBatchFunc(func(ms []ipv4.Message, _ int) (int, error) {
				reads++
				if reads > 1 {
					ms[0].N = copy(ms[0].Buffers[0], []byte("fresh"))
					ms[0].NN, ms[0].Flags = 0, 0
					return 1, nil
				}
				ms[0].N = copy(ms[0].Buffers[0], make([]byte, 200))
				ms[0].NN = copy(ms[0].OOB, tc.oob)
				ms[0].Flags = tc.flags
				return 1, nil
			}))
			t.Cleanup(oc.releaseReadBuffers)
			p, err := oc.ReadPacket()
			require.NoError(t, err)
			require.Equal(t, "fresh", string(p.data), "invalid aggregate must be discarded before QUIC parsing")
			p.buffer.Release()
		})
	}
}

func TestExternalGROPartialBatchError(t *testing.T) {
	want := errors.New("partial receive")
	oc := newGROConn(t, externalGROBatchFunc(func(ms []ipv4.Message, _ int) (int, error) {
		ms[0].N = copy(ms[0].Buffers[0], []byte("discard failed batch"))
		return 1, want
	}))
	p, err := oc.ReadPacket()
	require.ErrorIs(t, err, want)
	require.Nil(t, p.buffer)
	for _, b := range oc.buffers {
		require.Nil(t, b)
	}
	// A subsequent call must read anew rather than replaying the failed batch.
	oc.batchConn = externalGROBatchFunc(func(ms []ipv4.Message, _ int) (int, error) {
		ms[0].N = copy(ms[0].Buffers[0], []byte("fresh"))
		ms[0].NN = 0
		return 1, nil
	})
	t.Cleanup(oc.releaseReadBuffers)
	p, err = oc.ReadPacket()
	require.NoError(t, err)
	require.Equal(t, "fresh", string(p.data))
	p.buffer.Release()
}

func (c *externalGROConn) ReadBatch(ms []ipv4.Message, flags int) (int, error) {
	return c.batch.ReadBatch(ms, flags)
}

func TestExternalGROPermission(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		registered, allowed, disabled bool
		want                          int
	}{
		{name: "unregistered"},
		{name: "no permission", registered: true},
		{name: "explicit permission", registered: true, allowed: true, want: 1},
		{name: "disabled", registered: true, allowed: true, disabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.disabled {
				t.Setenv("QUIC_GO_DISABLE_GRO", "true")
			} else {
				t.Setenv("QUIC_GO_DISABLE_GRO", "false")
			}
			udp := listenExternalUDP(t)
			conn := &externalGROConn{OOBCapablePacketConn: udp, batch: ipv4.NewPacketConn(udp)}
			tr := &Transport{Conn: conn}
			if tc.registered {
				require.NoError(t, tr.ConfigureExternalPacketIOV1(conn, tc.allowed, nil))
			}
			require.NoError(t, tr.Close())
			require.Equal(t, tc.want, groSocketOption(t, udp))
			// Registration grants mutation, never Close ownership or raw handback.
			require.NoError(t, udp.SetReadDeadline(time.Time{}))
		})
	}
}

// The fixture filters at the registered ReadBatch boundary, retaining each
// slot's storage, source address, flags and OOB just as the participating adapter
// must. Native aggregation is observed before the fork splits the read.
func TestExternalGROWrapperNative(t *testing.T) {
	for _, network := range []string{"udp4", "udp6"} {
		t.Run(network, func(t *testing.T) {
			t.Setenv("QUIC_GO_DISABLE_GRO", "false")
			t.Setenv("QUIC_GO_DISABLE_GSO", "false")
			host := "127.0.0.1:0"
			if network == "udp6" {
				host = "[::1]:0"
			}
			listen := func() *net.UDPConn {
				t.Helper()
				addr, err := net.ResolveUDPAddr(network, host)
				require.NoError(t, err)
				udp, err := net.ListenUDP(network, addr)
				require.NoError(t, err)
				t.Cleanup(func() { udp.Close() })
				return udp
			}
			udp, selected, foreign := listen(), listen(), listen()
			native := ipv4.NewPacketConn(udp)
			var aggregates, filtered atomic.Int32
			conn := &externalGROConn{OOBCapablePacketConn: udp}
			conn.batch = externalGROBatchFunc(func(ms []ipv4.Message, flags int) (int, error) {
				for {
					// One native slot suffices to demonstrate filtering before splitting.
					n, err := native.ReadBatch(ms[:1], flags)
					if err != nil {
						return n, err
					}
					if ms[0].Addr.String() != selected.LocalAddr().String() {
						filtered.Add(1)
						continue
					}
					if ms[0].N > 1232 {
						controls, err := unix.ParseSocketControlMessage(ms[0].OOB[:ms[0].NN])
						if err != nil {
							return 0, err
						}
						for _, control := range controls {
							if control.Header.Level == unix.IPPROTO_UDP && control.Header.Type == unix.UDP_GRO {
								aggregates.Add(1)
							}
						}
					}
					return n, nil
				}
			})
			var recorder events.Recorder
			tr := &Transport{Conn: conn, Tracer: &recorder}
			require.NoError(t, tr.ConfigureExternalPacketIOV1(conn, true, nil))
			t.Cleanup(func() { require.NoError(t, tr.Close()) })
			canceled, cancel := context.WithCancel(context.Background())
			cancel()
			_, _, err := tr.ReadNonQUICPacket(canceled, make([]byte, 1232))
			require.ErrorIs(t, err, context.Canceled)
			if groSocketOption(t, udp) == 0 {
				t.Skip("native UDP_GRO unavailable")
			}
			raw, err := selected.SyscallConn()
			require.NoError(t, err)
			if !isGSOEnabled(raw) {
				t.Skip("native UDP_SEGMENT unavailable")
			}
			_, err = foreign.WriteToUDP(bytes.Repeat([]byte{9}, 1232), udp.LocalAddr().(*net.UDPAddr))
			require.NoError(t, err)
			segments := testCoalescedSegments(3, 1232)
			segments[2] = segments[2][:500]
			_, _, err = selected.WriteMsgUDP(bytes.Join(segments, nil), appendUDPSegmentSizeMsg(nil, 1232), udp.LocalAddr().(*net.UDPAddr))
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			for _, want := range segments {
				b := make([]byte, 1500)
				n, addr, err := tr.ReadNonQUICPacket(ctx, b)
				require.NoError(t, err)
				require.Equal(t, selected.LocalAddr().String(), addr.String())
				require.Equal(t, want, b[:n])
			}
			require.Positive(t, aggregates.Load(), "native wrapper engagement is required")
			require.Positive(t, filtered.Load(), "foreign traffic must traverse the wrapper filter")
			t.Logf("native wrapper: %d GRO aggregate(s), %d foreign read(s) filtered; 1232/1232/500-byte datagrams delivered", aggregates.Load(), filtered.Load())
			recorded := recorder.Events(qlog.DebugEvent{})
			require.Contains(t, recorded[0].(qlog.DebugEvent).Message, "receive_eligible=true receive_enabled=true receive_disabled_reason=none")
			require.NoError(t, tr.Close())
			require.NoError(t, udp.SetReadDeadline(time.Time{}))
		})
	}
}

func TestExternalGROSetupFailureRemainsCallerOwned(t *testing.T) {
	udp := listenExternalUDP(t)
	conn := &externalGROConn{OOBCapablePacketConn: udp, batch: ipv4.NewPacketConn(udp)}
	tr := &Transport{Conn: conn}
	require.NoError(t, tr.ConfigureExternalPacketIOV1(conn, true, nil))
	// Dial validates configuration after socket initialization has mutated GRO.
	_, err := tr.Dial(context.Background(), udp.LocalAddr(), &tls.Config{}, &Config{Versions: []Version{0xdeadbeef}})
	require.ErrorContains(t, err, "invalid QUIC version")
	require.Equal(t, 1, groSocketOption(t, udp))
	require.NoError(t, tr.Close())
	require.NoError(t, udp.SetReadDeadline(time.Time{}))
	// The existing resource owner performs terminal disposal even after failure.
	require.NoError(t, udp.Close())
	require.ErrorIs(t, udp.SetReadDeadline(time.Time{}), net.ErrClosed)
}

func TestExternalGROUnavailableWrapper(t *testing.T) {
	udp := listenExternalUDP(t)
	conn := &nonOOBPacketConn{PacketConn: udp}
	var recorder events.Recorder
	tr := &Transport{Conn: conn, Tracer: &recorder}
	require.NoError(t, tr.ConfigureExternalPacketIOV1(conn, true, nil))
	require.NoError(t, tr.Close())
	require.Contains(t, externalPacketIOEvent(t, &recorder), "receive_eligible=false receive_enabled=false receive_disabled_reason=ineligible_wrapper")
	require.Equal(t, 0, groSocketOption(t, udp))
}

func TestExternalGRORetainedSibling(t *testing.T) {
	segments := testCoalescedSegments(3, 1200)
	oc := newGROConn(t, &groBatchConn{t: t, payloads: [][]byte{bytes.Join(segments, nil)}, oobs: [][]byte{appendIPTOSMsg(appendUDPGROMsg(nil, 1200), 2)}, addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}})
	first, err := oc.ReadPacket()
	require.NoError(t, err)
	second, err := oc.ReadPacket()
	require.NoError(t, err)
	require.Equal(t, protocol.ECT0, second.ecn)
	first.buffer.Release()
	oc.releaseReadBuffers() // cancellation discards only the still-undelivered tail
	require.Equal(t, segments[1], second.data)
	slab := second.buffer.slab
	second.buffer.Release()
	require.True(t, slab.released())
}

// Ancillary options on a caller-owned dual-stack socket can exceed the fixed
// OOB capacity even for an ordinary datagram received with GRO enabled.
func TestExternalGROTruncatedControlKeepsTransportAlive(t *testing.T) {
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv6unspecified})
	require.NoError(t, err)
	t.Cleanup(func() { udp.Close() })
	raw, err := udp.SyscallConn()
	require.NoError(t, err)
	setExtraControl := func(enabled int) error {
		var optionErr error
		err := raw.Control(func(fd uintptr) {
			optionErr = errors.Join(
				unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_TIMESTAMPNS, enabled),
				unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_RECVTTL, enabled),
			)
		})
		return errors.Join(err, optionErr)
	}
	require.NoError(t, setExtraControl(1))
	start := make(chan struct{})
	observed := make(chan int, 1)
	var reads atomic.Int32
	native := ipv4.NewPacketConn(udp)
	conn := &externalGROConn{OOBCapablePacketConn: udp}
	conn.batch = externalGROBatchFunc(func(ms []ipv4.Message, flags int) (int, error) {
		first := reads.Add(1) == 1
		if first {
			<-start
		}
		n, err := native.ReadBatch(ms[:1], flags)
		if err == nil && first {
			// The next datagram has the ordinary metadata set. This isolates
			// recovery from one truncated read, not sustained option overflow.
			if err := setExtraControl(0); err != nil {
				return 0, err
			}
			observed <- ms[0].Flags
		}
		return n, err
	})
	tr := &Transport{Conn: conn}
	require.NoError(t, tr.ConfigureExternalPacketIOV1(conn, true, nil))
	t.Cleanup(func() { require.NoError(t, tr.Close()) })
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = tr.ReadNonQUICPacket(canceled, nil)
	close(start)
	require.ErrorIs(t, err, context.Canceled)
	sender := listenExternalUDP(t)
	target := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: udp.LocalAddr().(*net.UDPAddr).Port}
	_, err = sender.WriteToUDP([]byte("truncated"), target)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	select {
	case flags := <-observed:
		require.NotZero(t, flags&unix.MSG_CTRUNC, "native ancillary options must reproduce truncation")
		t.Logf("native receive flags=%#x (MSG_CTRUNC=%#x)", flags, unix.MSG_CTRUNC)
	case <-ctx.Done():
		t.Fatal("no native read observed")
	}
	_, err = sender.WriteToUDP([]byte("\x01fresh"), target)
	require.NoError(t, err)
	b := make([]byte, 1232)
	n, _, err := tr.ReadNonQUICPacket(ctx, b)
	require.NoError(t, err, "rejecting truncated metadata must not close unrelated connections")
	require.Equal(t, "\x01fresh", string(b[:n]))
}
