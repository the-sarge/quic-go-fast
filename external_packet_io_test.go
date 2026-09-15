package quic

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/testutils/events"

	"github.com/stretchr/testify/require"
)

type externalPacketIOV1 interface {
	ConfigureExternalPacketIOV1(net.PacketConn, bool, func([][]byte, []byte, *net.UDPAddr) (int, error)) error
	UDPBatchWriterV1(*net.UDPConn) (func([][]byte, []byte, *net.UDPAddr) (int, error), error)
}

func TestExternalPacketIORegistration(t *testing.T) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	defer conn.Close()
	tr := &Transport{Conn: conn}
	extension, ok := any(tr).(externalPacketIOV1)
	require.True(t, ok, "transport must provide both standard-type extension methods")
	require.NoError(t, extension.ConfigureExternalPacketIOV1(conn, true, nil))
	require.Error(t, extension.ConfigureExternalPacketIOV1(conn, false, nil))
	require.NoError(t, tr.Close())
	require.NoError(t, conn.SetWriteDeadline(time.Time{}))
}

func TestExternalPacketIOBatchDispatch(t *testing.T) {
	sender := listenExternalUDP(t)
	receiver := listenExternalUDP(t)
	var recorder events.Recorder
	tr := &Transport{Conn: sender, Tracer: &recorder}
	writer, err := tr.UDPBatchWriterV1(sender)
	require.NoError(t, err)
	calls := 0
	require.NoError(t, tr.ConfigureExternalPacketIOV1(sender, false, func(bufs [][]byte, oob []byte, addr *net.UDPAddr) (int, error) {
		calls++
		return writer(bufs, oob, addr)
	}))
	_, err = tr.WriteTo([]byte("prime"), receiver.LocalAddr())
	require.NoError(t, err)
	defer tr.Close()
	sc := newSendConn(tr.conn, receiver.LocalAddr(), packetInfo{}, utils.DefaultLogger)
	q := newSendQueue(sc, nil)
	for _, payload := range []string{"first", "second"} {
		q.Send(getPacketWithContents([]byte(payload)), 0, protocol.ECNUnsupported, sendMetadata{})
	}
	done := make(chan error, 1)
	go func() { done <- q.Run() }()
	require.NoError(t, receiver.SetReadDeadline(time.Now().Add(time.Second)))
	for _, payload := range []string{"prime", "first", "second"} {
		buf := make([]byte, 64)
		n, _, err := receiver.ReadFromUDP(buf)
		require.NoError(t, err)
		require.Equal(t, payload, string(buf[:n]))
	}
	q.Close()
	require.NoError(t, <-done)
	require.Equal(t, 1, calls, "registered wrapper must own batch submission")
	recorded := recorder.Events(qlog.DebugEvent{})
	require.Len(t, recorded, 1)
	require.NotContains(t, recorded[0].(qlog.DebugEvent).Message, "batch_calls", "initialization only reports capability state; live counters use debug logging")
}

func listenExternalUDP(t *testing.T) *net.UDPConn {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	return conn
}

// A non-comparable value implementing PacketConn must fail admission without
// triggering an interface-comparison panic.
type nonPointerPacketConn struct {
	net.PacketConn
	_ []byte
}

func TestExternalPacketIOInvalidIdentity(t *testing.T) {
	conn := listenExternalUDP(t)
	var typedNil *net.UDPConn
	for name, target := range map[string]net.PacketConn{
		"nil": nil, "typed nil": typedNil,
		"nonpointer":    nonPointerPacketConn{PacketConn: conn},
		"swapped":       listenExternalUDP(t),
		"outer wrapper": &struct{ net.PacketConn }{conn},
	} {
		t.Run(name, func(t *testing.T) {
			tr := &Transport{Conn: conn}
			require.Error(t, tr.ConfigureExternalPacketIOV1(target, false, nil))
			// Rejection did not consume the immutable registration slot.
			require.NoError(t, tr.ConfigureExternalPacketIOV1(conn, false, nil))
		})
	}
}

func TestExternalPacketIOSwappedBeforeClose(t *testing.T) {
	original, swapped := listenExternalUDP(t), listenExternalUDP(t)
	tr := &Transport{Conn: original}
	require.NoError(t, tr.ConfigureExternalPacketIOV1(original, true, nil))
	tr.Conn = swapped
	require.ErrorContains(t, tr.Close(), "changed")
	require.NoError(t, original.SetWriteDeadline(time.Time{}))
	require.NoError(t, swapped.SetWriteDeadline(time.Time{}))
}

func TestExternalPacketIOLateRegistration(t *testing.T) {
	for _, name := range []string{"WriteTo", "Close", "ReadNonQUICPacket", "Dial", "Listen", "AddPath"} {
		t.Run(name, func(t *testing.T) {
			conn := listenExternalUDP(t)
			tr := &Transport{Conn: conn}
			defer tr.Close()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			switch name {
			case "WriteTo":
				_, err := tr.WriteTo([]byte("data"), conn.LocalAddr())
				require.NoError(t, err)
			case "Close":
				require.NoError(t, tr.Close())
			case "ReadNonQUICPacket":
				_, _, err := tr.ReadNonQUICPacket(ctx, make([]byte, 32))
				require.ErrorIs(t, err, context.Canceled)
			case "Dial":
				_, err := tr.Dial(ctx, conn.LocalAddr(), &tls.Config{InsecureSkipVerify: true}, nil)
				require.ErrorIs(t, err, context.Canceled)
			case "Listen":
				ln, err := tr.Listen(&tls.Config{}, nil)
				require.NoError(t, err)
				defer ln.Close()
			case "AddPath":
				c := &Conn{peerParams: &wire.TransportParameters{}}
				c.pathManagerOutgoing.Store(newPathManagerOutgoing(nil, nil, func() {}))
				_, err := c.AddPath(tr)
				require.NoError(t, err)
			}
			require.ErrorContains(t, tr.ConfigureExternalPacketIOV1(conn, false, nil), "after initialization")
		})
	}
}

func TestUDPBatchWriter(t *testing.T) {
	sender, receiver := listenExternalUDP(t), listenExternalUDP(t)
	tr := &Transport{Conn: sender}
	writer, err := tr.UDPBatchWriterV1(sender)
	require.NoError(t, err)
	// Construction neither initializes the transport nor takes close authority.
	require.NoError(t, tr.ConfigureExternalPacketIOV1(sender, false, nil))
	defer tr.Close()
	n, err := writer([][]byte{[]byte("first"), {}, []byte("last")}, nil, receiver.LocalAddr().(*net.UDPAddr))
	require.NoError(t, err)
	require.Equal(t, 3, n)
	require.NoError(t, receiver.SetReadDeadline(time.Now().Add(time.Second)))
	for _, expected := range []string{"first", "", "last"} {
		buf := make([]byte, 64)
		n, _, err := receiver.ReadFromUDP(buf)
		require.NoError(t, err)
		require.Equal(t, expected, string(buf[:n]))
	}
	// A known size rejection returns the definite prefix for ordinary fallback.
	n, err = writer([][]byte{[]byte("prefix"), make([]byte, 65536), []byte("not sent")}, nil, receiver.LocalAddr().(*net.UDPAddr))
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		require.NoError(t, err)
	} else {
		require.Error(t, err)
	}
	require.Equal(t, 1, n)
	buf := make([]byte, 64)
	size, _, err := receiver.ReadFromUDP(buf)
	require.NoError(t, err)
	require.Equal(t, "prefix", string(buf[:size]))
	n, err = writer(nil, nil, nil)
	require.NoError(t, err)
	require.Zero(t, n)
	require.NoError(t, sender.Close())
	n, err = writer([][]byte{[]byte("closed")}, nil, receiver.LocalAddr().(*net.UDPAddr))
	require.ErrorIs(t, err, net.ErrClosed)
	require.Zero(t, n)
	_, err = tr.UDPBatchWriterV1(nil)
	require.Error(t, err)
}

func TestExternalPacketIOSwappedAfterInitialization(t *testing.T) {
	conn, other := listenExternalUDP(t), listenExternalUDP(t)
	tr := &Transport{Conn: conn}
	require.NoError(t, tr.ConfigureExternalPacketIOV1(conn, false, nil))
	_, err := tr.WriteTo([]byte("before"), other.LocalAddr())
	require.NoError(t, err)
	tr.Conn = other
	_, err = tr.WriteTo([]byte("after"), other.LocalAddr())
	require.ErrorContains(t, err, "changed")
	require.ErrorContains(t, tr.Close(), "changed")
	require.NoError(t, conn.SetWriteDeadline(time.Time{}))
}

func TestExternalPacketIODiagnostics(t *testing.T) {
	conn := listenExternalUDP(t)
	var recorder events.Recorder
	tr := &Transport{Conn: conn, Tracer: &recorder}
	require.NoError(t, tr.ConfigureExternalPacketIOV1(conn, true, nil))
	require.NoError(t, tr.Close())
	recorded := recorder.Events(qlog.DebugEvent{})
	require.Len(t, recorded, 1)
	event := recorded[0].(qlog.DebugEvent)
	require.Equal(t, "transport:external_packet_io", event.Name())
	require.Contains(t, event.Message, "receive_requested=true receive_permitted=true receive_supported=false receive_enabled=false")
	require.Contains(t, event.Message, "receive_disabled_reason=external_coalescing_unavailable")
	require.NotContains(t, event.Message, "batch_calls")
}

func TestExternalPacketIOConcurrentWriters(t *testing.T) {
	sender, receiver := listenExternalUDP(t), listenExternalUDP(t)
	tr := &Transport{Conn: sender}
	writer, err := tr.UDPBatchWriterV1(sender)
	require.NoError(t, err)
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	require.NoError(t, tr.ConfigureExternalPacketIOV1(sender, false, func(bufs [][]byte, oob []byte, addr *net.UDPAddr) (int, error) {
		entered <- struct{}{}
		<-release
		return writer(bufs, oob, addr)
	}))
	_, err = tr.WriteTo([]byte("prime"), receiver.LocalAddr())
	require.NoError(t, err)
	defer tr.Close()
	var queues []interface{ Close() }
	done := make(chan error, 2)
	for i := range 2 {
		q := newSendQueue(newSendConn(tr.conn, receiver.LocalAddr(), packetInfo{}, utils.DefaultLogger), nil)
		queues = append(queues, q)
		for j := range 2 {
			q.Send(getPacketWithContents([]byte{byte(i), byte(j)}), 0, protocol.ECNUnsupported, sendMetadata{})
		}
		go func() { done <- q.Run() }()
	}
	for range 2 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("independent send workers did not reach the callback concurrently")
		}
	}
	close(release)
	require.NoError(t, receiver.SetReadDeadline(time.Now().Add(time.Second)))
	received := make(map[string]int)
	for range 5 {
		buf := make([]byte, 64)
		n, _, err := receiver.ReadFromUDP(buf)
		require.NoError(t, err)
		received[string(buf[:n])]++
	}
	require.Equal(t, map[string]int{"prime": 1, "\x00\x00": 1, "\x00\x01": 1, "\x01\x00": 1, "\x01\x01": 1}, received)
	for _, q := range queues {
		q.Close()
		require.NoError(t, <-done)
	}
}

type externalWriteObserver struct {
	net.PacketConn
	writes atomic.Int32
}

func (c *externalWriteObserver) WriteTo(buf []byte, addr net.Addr) (int, error) {
	c.writes.Add(1)
	return c.PacketConn.WriteTo(buf, addr)
}

func TestExternalPacketIOBatchProgress(t *testing.T) {
	terminal := errors.New("terminal batch error")
	for _, tc := range []struct {
		name           string
		accepted       int
		err            error
		ordinaryWrites int32
	}{
		{name: "short prefix", accepted: 1, ordinaryWrites: 1},
		{name: "zero prefix", accepted: 0, ordinaryWrites: 2},
		{name: "terminal error", accepted: 1, err: terminal},
		{name: "negative count", accepted: -1},
		{name: "over count", accepted: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			udp, receiver := listenExternalUDP(t), listenExternalUDP(t)
			wrapper := &externalWriteObserver{PacketConn: udp}
			tr := &Transport{Conn: wrapper}
			writer, err := tr.UDPBatchWriterV1(udp)
			require.NoError(t, err)
			require.NoError(t, tr.ConfigureExternalPacketIOV1(wrapper, false, func(bufs [][]byte, oob []byte, addr *net.UDPAddr) (int, error) {
				if tc.accepted > 0 && tc.accepted <= len(bufs) {
					_, err := writer(bufs[:tc.accepted], oob, addr)
					if err != nil {
						return 0, err
					}
				}
				return tc.accepted, tc.err
			}))
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, _, err = tr.ReadNonQUICPacket(ctx, nil)
			require.ErrorIs(t, err, context.Canceled)
			defer tr.Close()
			q := newSendQueue(newSendConn(tr.conn, receiver.LocalAddr(), packetInfo{}, utils.DefaultLogger), nil)
			for _, payload := range []string{"first", "second"} {
				q.Send(getPacketWithContents([]byte(payload)), 0, protocol.ECNUnsupported, sendMetadata{})
			}
			done := make(chan error, 1)
			go func() { done <- q.Run() }()
			if tc.err != nil || tc.accepted < 0 || tc.accepted > 2 {
				err = <-done
				if tc.err != nil {
					require.ErrorIs(t, err, tc.err)
				} else {
					require.ErrorContains(t, err, "invalid send batch accepted count")
				}
				q.Close()
			} else {
				require.NoError(t, receiver.SetReadDeadline(time.Now().Add(time.Second)))
				for _, expected := range []string{"first", "second"} {
					buf := make([]byte, 64)
					n, _, err := receiver.ReadFromUDP(buf)
					require.NoError(t, err)
					require.Equal(t, expected, string(buf[:n]))
				}
				q.Close()
				require.NoError(t, <-done)
			}
			require.Equal(t, tc.ordinaryWrites, wrapper.writes.Load(), "only the definite unsent suffix may use ordinary fallback")
		})
	}
}
