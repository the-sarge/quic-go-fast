//go:build linux

package quic

import (
	"context"
	"errors"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/stretchr/testify/require"
)

var errEmptyWrite = errors.New("empty write")

// eioOnceConn is an ordinary caller wrapper around a loopback socket. Its first
// non-empty write without a UDP_SEGMENT control message fails with the error
// *net.UDPConn returns when sendmsg fails with EIO; later non-empty writes reach
// the socket. It counts empty writes and refuses each one, so resubmitting a
// datagram as empty pieces ends at the first piece.
type eioOnceConn struct {
	*net.UDPConn

	mu          sync.Mutex
	injected    bool
	emptyWrites int
}

func (c *eioOnceConn) WriteMsgUDP(b, oob []byte, addr *net.UDPAddr) (int, int, error) {
	if err := c.injectedError(b, oob, addr); err != nil {
		return 0, 0, err
	}
	return c.UDPConn.WriteMsgUDP(b, oob, addr)
}

func (c *eioOnceConn) injectedError(b, oob []byte, addr *net.UDPAddr) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(b) == 0 {
		c.emptyWrites++
		return errEmptyWrite
	}
	if c.injected {
		return nil
	}
	controls, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return err
	}
	for _, control := range controls {
		if control.Header.Level == unix.IPPROTO_UDP && control.Header.Type == unix.UDP_SEGMENT {
			return nil
		}
	}
	c.injected = true
	return &net.OpError{Op: "write", Net: "udp", Source: c.LocalAddr(), Addr: addr, Err: os.NewSyscallError("sendmsg", unix.EIO)}
}

// A caller's socket that fails an unsegmented write with EIO does not make the
// connection resend that datagram as empty pieces: the dial fails with the error.
func TestSendConnWrapperGSOErrorWithoutSegmentSize(t *testing.T) {
	ln := listenOrdinary(t, nil)
	conn := &eioOnceConn{UDPConn: newUDPConnLocalhost(t)}
	tr := &Transport{Conn: conn}
	t.Cleanup(func() { tr.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, err := tr.Dial(ctx, ln.Addr(), candidateClientTLS(), nil)

	conn.mu.Lock()
	injected, emptyWrites := conn.injected, conn.emptyWrites
	conn.mu.Unlock()
	require.True(t, injected)
	require.Zero(t, emptyWrites)
	require.ErrorIs(t, err, unix.EIO)
}
