//go:build darwin || linux || freebsd || windows

package quic

import (
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
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

type copyingManagedPacketConn struct {
	net.PacketConn
	mode string
}

func (c *copyingManagedPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	if c.mode == "copy" {
		copyBuffer := make([]byte, len(b))
		n, addr, err := c.PacketConn.ReadFrom(copyBuffer)
		copy(b, copyBuffer[:n])
		return n, addr, err
	}
	n, addr, err := c.PacketConn.ReadFrom(b)
	if err == nil {
		if c.mode == "range" {
			n--
		} else {
			addr = &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4243}
		}
	}
	return n, addr, err
}

type rotatingManagedPacketConn struct {
	net.PacketConn
	afterRead func()
}

type pausingManagedPacketConn struct {
	net.PacketConn
	readDone chan struct{}
	resume   chan struct{}
}

func (c *pausingManagedPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	n, addr, err := c.PacketConn.ReadFrom(b)
	close(c.readDone)
	<-c.resume
	return n, addr, err
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
	for _, mode := range []string{"copy", "range", "address"} {
		t.Run(mode, func(t *testing.T) {
			addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4242}
			native := &managedECNFixtureConn{packet: receivedPacket{data: []byte("copied"), remoteAddr: addr, ecn: protocol.ECT0}}
			lease := &managedPacketLease{done: make(chan struct{})}
			endpoint := &managedPacketEndpoint{receiver: native, managedNative: native, managedECN: true, lease: lease}
			endpoint.idle = sync.NewCond(&endpoint.mutex)
			conn := &managedPacketConn{endpoint: endpoint, lease: lease}
			wrapper := &copyingManagedPacketConn{PacketConn: conn, mode: mode}
			raw := newManagedPacketRawConn(&basicConn{PacketConn: wrapper}, conn, &externalPacketIO{sendBatch: conn.WriteBatchV1}, false)

			packet, err := raw.ReadPacket()
			require.NoError(t, err)
			defer packet.buffer.Release()
			want := "copied"
			if mode == "range" {
				want = "copie"
			}
			require.Equal(t, []byte(want), packet.data)
			require.Equal(t, protocol.ECNUnsupported, packet.ecn)
		})
	}
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
	t.Run("manual stale token", func(t *testing.T) {
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
	})
	t.Run("lease close joins correlation", func(t *testing.T) {
		if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
			t.Skip("managed ECN correlation is qualified on Linux and Windows")
		}
		t.Setenv("QUIC_GO_DISABLE_ECN", "false")
		t.Setenv("QUIC_GO_DISABLE_GRO", "true")
		endpoint, acquire, err := (&Transport{}).NewManagedPacketEndpointV1("udp4", &net.UDPAddr{IP: net.IPv4zero})
		require.NoError(t, err)
		defer endpoint.Close()
		lease, err := acquire()
		require.NoError(t, err)
		wrapper := &pausingManagedPacketConn{PacketConn: lease, readDone: make(chan struct{}), resume: make(chan struct{})}
		tr := &Transport{Conn: wrapper}
		require.NoError(t, tr.ConfigureManagedPacketIOV1(wrapper, lease, lease.(managedBatchWriterV1).WriteBatchV1))
		raw := tr.wrapExternalPacketIO(&basicConn{PacketConn: wrapper})
		require.True(t, raw.capabilities().ECN)
		sender, err := net.DialUDP("udp4", nil, endpoint.LocalAddr().(*net.UDPAddr))
		require.NoError(t, err)
		defer sender.Close()
		_, err = sender.Write([]byte("old generation"))
		require.NoError(t, err)
		type readResult struct {
			packet receivedPacket
			err    error
		}
		readDone := make(chan readResult, 1)
		go func() {
			packet, err := raw.ReadPacket()
			readDone <- readResult{packet: packet, err: err}
		}()
		<-wrapper.readDone
		closeDone := make(chan error, 1)
		go func() { closeDone <- lease.Close() }()
		e := endpoint.(*managedPacketConn).endpoint
		for {
			e.mutex.Lock()
			returning, active := lease.(*managedPacketConn).lease.returning, e.active
			e.mutex.Unlock()
			if returning {
				if active <= 0 {
					close(wrapper.resume)
					result := <-readDone
					if result.packet.buffer != nil {
						result.packet.buffer.Release()
					}
					<-closeDone
					t.Fatal("adapter correlation was not joined while the wrapper held the result")
				}
				break
			}
			runtime.Gosched()
		}
		close(wrapper.resume)
		result := <-readDone
		require.NoError(t, result.err)
		result.packet.buffer.Release()
		require.NoError(t, <-closeDone)
		require.NoError(t, tr.Close())

		next, err := acquire()
		require.NoError(t, err)
		defer next.Close()
		nextTransport := &Transport{Conn: next}
		require.NoError(t, nextTransport.ConfigureManagedPacketIOV1(next, next, nil))
		nextRaw := nextTransport.wrapExternalPacketIO(&basicConn{PacketConn: next})
		_, err = sender.Write([]byte("new generation"))
		require.NoError(t, err)
		packet, err := nextRaw.ReadPacket()
		require.NoError(t, err)
		defer packet.buffer.Release()
		require.Equal(t, "new generation", string(packet.data))
	})
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
