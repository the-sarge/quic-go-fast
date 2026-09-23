//go:build darwin || linux || freebsd

package quic

import (
	"errors"
	"net"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

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

func (*managedReadMsgFixture) ReadFrom([]byte) (int, net.Addr, error) {
	panic("unexpected ReadFrom")
}

func (*managedReadMsgFixture) WriteTo([]byte, net.Addr) (int, error) {
	panic("unexpected WriteTo")
}

func (*managedReadMsgFixture) Close() error                     { return nil }
func (*managedReadMsgFixture) LocalAddr() net.Addr              { return &net.UDPAddr{} }
func (*managedReadMsgFixture) SetDeadline(time.Time) error      { return nil }
func (*managedReadMsgFixture) SetReadDeadline(time.Time) error  { return nil }
func (*managedReadMsgFixture) SetWriteDeadline(time.Time) error { return nil }
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
