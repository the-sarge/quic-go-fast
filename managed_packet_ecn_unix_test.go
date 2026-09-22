//go:build darwin || linux || freebsd

package quic

import (
	"errors"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

type managedECNFixtureConn struct {
	packet       receivedPacket
	writes       []managedECNFixtureWrite
	writeResult  error
	writeResults []error
}

type managedECNFixtureWrite struct {
	payload string
	oob     []byte
	ecn     protocol.ECN
}

func (c *managedECNFixtureConn) ReadPacket() (receivedPacket, error) {
	buffer := getCoalescedPacketBuffer()
	buffer.Data = append(buffer.Data[:0], c.packet.data...)
	p := c.packet
	p.data = buffer.Data
	p.buffer = buffer
	return p, nil
}

func (c *managedECNFixtureConn) WritePacket(b []byte, _ net.Addr, oob []byte, _ uint16, ecn protocol.ECN) (int, error) {
	c.writes = append(c.writes, managedECNFixtureWrite{payload: string(b), oob: append([]byte(nil), oob...), ecn: ecn})
	if len(c.writeResults) > 0 {
		err := c.writeResults[0]
		c.writeResults = c.writeResults[1:]
		if err != nil {
			return 0, err
		}
	}
	if c.writeResult != nil {
		return 0, c.writeResult
	}
	return len(b), nil
}

type copyingManagedPacketConn struct{ net.PacketConn }

func (c *copyingManagedPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	copyBuffer := make([]byte, len(b))
	n, addr, err := c.PacketConn.ReadFrom(copyBuffer)
	copy(b, copyBuffer[:n])
	return n, addr, err
}

type rotatingManagedPacketConn struct {
	net.PacketConn
	afterRead func()
}

func (c *rotatingManagedPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	n, addr, err := c.PacketConn.ReadFrom(b)
	if err == nil {
		c.afterRead()
	}
	return n, addr, err
}

func (*managedECNFixtureConn) LocalAddr() net.Addr             { return &net.UDPAddr{} }
func (*managedECNFixtureConn) SetReadDeadline(time.Time) error { return nil }
func (*managedECNFixtureConn) Close() error                    { return nil }
func (*managedECNFixtureConn) capabilities() connCapabilities  { return connCapabilities{ECN: true} }
func (*managedECNFixtureConn) releaseReadBuffers()             {}

func TestManagedPacketECNCorrelatesExactDirectRead(t *testing.T) {
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4242}
	native := &managedECNFixtureConn{packet: receivedPacket{data: []byte("marked"), remoteAddr: addr, ecn: protocol.ECNCE}}
	lease := &managedPacketLease{done: make(chan struct{})}
	endpoint := &managedPacketEndpoint{receiver: native, managedNative: native, managedECN: true, lease: lease}
	endpoint.idle = sync.NewCond(&endpoint.mutex)
	conn := &managedPacketConn{endpoint: endpoint, lease: lease}

	raw := newManagedPacketRawConn(&basicConn{PacketConn: conn}, conn, &externalPacketIO{}, true)
	packet, err := raw.ReadPacket()
	require.NoError(t, err)
	defer packet.buffer.Release()
	require.Equal(t, []byte("marked"), packet.data)
	require.Equal(t, addr, packet.remoteAddr)
	require.Equal(t, protocol.ECNCE, packet.ecn)
}

func TestManagedPacketECNRejectsUncorrelatedRead(t *testing.T) {
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4242}
	native := &managedECNFixtureConn{packet: receivedPacket{data: []byte("copied"), remoteAddr: addr, ecn: protocol.ECT0}}
	lease := &managedPacketLease{done: make(chan struct{})}
	endpoint := &managedPacketEndpoint{receiver: native, managedNative: native, managedECN: true, lease: lease}
	endpoint.idle = sync.NewCond(&endpoint.mutex)
	conn := &managedPacketConn{endpoint: endpoint, lease: lease}
	wrapper := &copyingManagedPacketConn{PacketConn: conn}
	raw := newManagedPacketRawConn(&basicConn{PacketConn: wrapper}, conn, &externalPacketIO{}, false)

	packet, err := raw.ReadPacket()
	require.NoError(t, err)
	defer packet.buffer.Release()
	require.Equal(t, []byte("copied"), packet.data)
	require.Equal(t, protocol.ECNUnsupported, packet.ecn)
}

func TestManagedPacketECNDoesNotPublishForUncheckedWrapper(t *testing.T) {
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4242}
	native := &managedECNFixtureConn{packet: receivedPacket{data: []byte("marked"), remoteAddr: addr, ecn: protocol.ECNCE}}
	lease := &managedPacketLease{done: make(chan struct{})}
	endpoint := &managedPacketEndpoint{receiver: native, managedNative: native, managedECN: true, lease: lease}
	endpoint.idle = sync.NewCond(&endpoint.mutex)
	conn := &managedPacketConn{endpoint: endpoint, lease: lease}
	raw := newManagedPacketRawConn(&basicConn{PacketConn: conn}, conn, &externalPacketIO{}, false)

	packet, err := raw.ReadPacket()
	require.NoError(t, err)
	defer packet.buffer.Release()
	require.Equal(t, protocol.ECNUnsupported, packet.ecn)
}

func TestManagedPacketECNRejectsStaleGeneration(t *testing.T) {
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4242}
	native := &managedECNFixtureConn{packet: receivedPacket{data: []byte("stale"), remoteAddr: addr, ecn: protocol.ECT1}}
	lease := &managedPacketLease{done: make(chan struct{})}
	endpoint := &managedPacketEndpoint{receiver: native, managedNative: native, managedECN: true, lease: lease}
	endpoint.idle = sync.NewCond(&endpoint.mutex)
	conn := &managedPacketConn{endpoint: endpoint, lease: lease}
	wrapper := &rotatingManagedPacketConn{PacketConn: conn, afterRead: func() {
		endpoint.mutex.Lock()
		endpoint.lease = &managedPacketLease{done: make(chan struct{})}
		endpoint.mutex.Unlock()
	}}
	raw := newManagedPacketRawConn(&basicConn{PacketConn: wrapper}, conn, &externalPacketIO{}, true)

	packet, err := raw.ReadPacket()
	require.NoError(t, err)
	defer packet.buffer.Release()
	require.Equal(t, "stale", string(packet.data))
	require.Equal(t, protocol.ECNUnsupported, packet.ecn)
}

func TestManagedPacketECNDirectMarkedWrite(t *testing.T) {
	native := &managedECNFixtureConn{}
	outer := &managedECNFixtureConn{}
	lease := &managedPacketLease{done: make(chan struct{})}
	endpoint := &managedPacketEndpoint{managedNative: native, managedECN: true, lease: lease}
	endpoint.idle = sync.NewCond(&endpoint.mutex)
	conn := &managedPacketConn{endpoint: endpoint, lease: lease}
	raw := newManagedPacketRawConn(outer, conn, &externalPacketIO{}, true)
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4242}

	n, err := raw.WritePacket([]byte("marked"), addr, nil, 0, protocol.ECT0)
	require.NoError(t, err)
	require.Equal(t, len("marked"), n)
	require.Equal(t, []managedECNFixtureWrite{{payload: "marked", ecn: protocol.ECT0}}, native.writes)
	require.Empty(t, outer.writes)

	n, err = raw.WritePacket([]byte("not-ect"), addr, nil, 0, protocol.ECNNon)
	require.NoError(t, err)
	require.Equal(t, len("not-ect"), n)
	require.Equal(t, []managedECNFixtureWrite{{payload: "not-ect", ecn: protocol.ECNUnsupported}}, outer.writes)
}

func TestManagedPacketECNCheckedSingletonPreservesNativeResult(t *testing.T) {
	permissionErr := &os.SyscallError{Syscall: "sendmsg", Err: unix.EPERM}
	for _, normalized := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged", true: "normalized"}[normalized], func(t *testing.T) {
			native := &managedECNFixtureConn{writeResult: permissionErr}
			lease := &managedPacketLease{done: make(chan struct{})}
			endpoint := &managedPacketEndpoint{managedNative: native, managedECN: true, lease: lease}
			endpoint.idle = sync.NewCond(&endpoint.mutex)
			conn := &managedPacketConn{endpoint: endpoint, lease: lease}
			callback := func(bufs [][]byte, oob []byte, addr *net.UDPAddr) (int, error) {
				n, err := conn.WriteBatchV1(bufs, oob, addr)
				if normalized {
					return 0, nil
				}
				return n, err
			}
			raw := newManagedPacketRawConn(&managedECNFixtureConn{}, conn, &externalPacketIO{sendBatch: callback}, false)

			n, err := raw.WritePacket([]byte("marked"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4242}, nil, 0, protocol.ECT1)
			require.Zero(t, n)
			if normalized {
				require.ErrorContains(t, err, "unchanged")
			} else {
				require.Same(t, permissionErr, err)
			}
			require.Len(t, native.writes, 1)
			require.NotEmpty(t, native.writes[0].oob)
			require.Equal(t, protocol.ECNUnsupported, native.writes[0].ecn, "the callback borrows already-encoded OOB")
		})
	}
}

func TestManagedPacketECNSerializesOverlappingCheckedSingletons(t *testing.T) {
	native := &managedECNFixtureConn{}
	lease := &managedPacketLease{done: make(chan struct{})}
	endpoint := &managedPacketEndpoint{managedNative: native, managedECN: true, lease: lease}
	endpoint.idle = sync.NewCond(&endpoint.mutex)
	conn := &managedPacketConn{endpoint: endpoint, lease: lease}
	firstEntered := make(chan struct{})
	resumeFirst := make(chan struct{})
	var calls atomic.Int32
	callback := func(bufs [][]byte, oob []byte, addr *net.UDPAddr) (int, error) {
		if calls.Add(1) == 1 {
			close(firstEntered)
			<-resumeFirst
		}
		return conn.WriteBatchV1(bufs, oob, addr)
	}
	raw := newManagedPacketRawConn(&managedECNFixtureConn{}, conn, &externalPacketIO{sendBatch: callback}, false)
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4242}
	results := make(chan error, 2)
	go func() { _, err := raw.WritePacket([]byte("first"), addr, nil, 0, protocol.ECT0); results <- err }()
	<-firstEntered
	secondStarted := make(chan struct{})
	go func() {
		close(secondStarted)
		_, err := raw.WritePacket([]byte("second"), addr, nil, 0, protocol.ECNCE)
		results <- err
	}()
	<-secondStarted
	close(resumeFirst)
	require.NoError(t, <-results)
	require.NoError(t, <-results)
	require.EqualValues(t, 2, calls.Load())
	require.Equal(t, []string{"first", "second"}, []string{native.writes[0].payload, native.writes[1].payload})
}

func TestManagedPacketECNProjectsOnlyManagedCapabilities(t *testing.T) {
	for _, tc := range []struct {
		name       string
		qualified  bool
		direct     bool
		callback   bool
		coalescing bool
		wantECN    bool
	}{
		{name: "direct", qualified: true, direct: true, wantECN: true},
		{name: "wrapped checked", qualified: true, callback: true, wantECN: true},
		{name: "wrapped unchecked", qualified: true},
		{name: "native unqualified", direct: true},
		{name: "ecn only", qualified: true, direct: true, wantECN: true},
		{name: "ecn and coalescing", qualified: true, direct: true, coalescing: true, wantECN: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lease := &managedPacketLease{done: make(chan struct{})}
			endpoint := &managedPacketEndpoint{managedECN: tc.qualified, receiveCoalescing: tc.coalescing, lease: lease}
			endpoint.idle = sync.NewCond(&endpoint.mutex)
			conn := &managedPacketConn{endpoint: endpoint, lease: lease}
			var callback func([][]byte, []byte, *net.UDPAddr) (int, error)
			if tc.callback {
				callback = func(bufs [][]byte, _ []byte, _ *net.UDPAddr) (int, error) { return len(bufs), nil }
			}
			base := &managedECNFixtureConn{}
			raw := newManagedPacketRawConn(base, conn, &externalPacketIO{sendBatch: callback}, tc.direct)
			cap := raw.capabilities()
			require.Equal(t, tc.wantECN, cap.ECN)
			require.Equal(t, tc.coalescing, cap.GRO)
			require.False(t, cap.GSO)
		})
	}
}

type managedReadMsgFixture struct {
	calls int
}

func (c *managedReadMsgFixture) ReadMsgUDP(b, oob []byte) (int, int, int, *net.UDPAddr, error) {
	c.calls++
	n := copy(b, []byte("fresh"))
	if c.calls == 1 {
		return n, copy(oob, []byte{1}), 0, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}, nil
	}
	return n, copy(oob, lifetimeControlMessage(unix.IPPROTO_IP, msgTypeIPTOS, []byte{protocol.ECT0.ToHeaderBits()})), 0, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}, nil
}

func (*managedReadMsgFixture) ReadFrom([]byte) (int, net.Addr, error) { panic("unexpected ReadFrom") }
func (*managedReadMsgFixture) WriteTo([]byte, net.Addr) (int, error)  { panic("unexpected WriteTo") }
func (*managedReadMsgFixture) Close() error                           { return nil }
func (*managedReadMsgFixture) LocalAddr() net.Addr                    { return &net.UDPAddr{} }
func (*managedReadMsgFixture) SetDeadline(time.Time) error            { return nil }
func (*managedReadMsgFixture) SetReadDeadline(time.Time) error        { return nil }
func (*managedReadMsgFixture) SetWriteDeadline(time.Time) error       { return nil }
func (*managedReadMsgFixture) SyscallConn() (syscall.RawConn, error) {
	return nil, errors.New("unused")
}
func (*managedReadMsgFixture) SetReadBuffer(int) error { return nil }
func (*managedReadMsgFixture) WriteMsgUDP([]byte, []byte, *net.UDPAddr) (int, int, error) {
	panic("unexpected WriteMsgUDP")
}

func TestManagedPacketECNDropsMalformedDatagramAndContinues(t *testing.T) {
	fixture := &managedReadMsgFixture{}
	reader := &oobConn{OOBCapablePacketConn: fixture, managedRead: true, managedOOB: make([]byte, oobBufferSize)}
	packet, err := reader.ReadPacket()
	require.NoError(t, err)
	defer packet.buffer.Release()
	require.Equal(t, "fresh", string(packet.data))
	require.Equal(t, protocol.ECT0, packet.ecn)
	require.Equal(t, 2, fixture.calls)
}
