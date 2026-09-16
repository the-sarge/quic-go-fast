package quic

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"os"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"golang.org/x/sys/windows"

	"github.com/stretchr/testify/require"
)

func TestTransportWindowsReadErrors(t *testing.T) {
	t.Setenv("QUIC_GO_DISABLE_RECEIVE_BUFFER_WARNING", "true")
	synctest.Test(t, func(t *testing.T) {
		tr, conn := newRetryTestTransport(t, false)
		temporary := &net.OpError{Op: "read", Net: "udp", Err: os.NewSyscallError("recvfrom", syscall.EINTR)}
		require.True(t, temporary.Temporary())
		require.False(t, temporary.Timeout())
		oversized := &net.OpError{Op: "read", Net: "udp", Err: os.NewSyscallError("recvfrom", windows.WSAEMSGSIZE)}
		require.False(t, oversized.Temporary())
		fatal := errors.New("fatal read error")
		var attempts []time.Time
		conn.readFrom = func(b []byte) (int, net.Addr, error) {
			attempts = append(attempts, time.Now())
			switch len(attempts) {
			case 1:
				return 0, nil, temporary
			case 2:
				return 0, nil, oversized
			case 3:
				return copy(b, "\x00recovered"), conn.LocalAddr(), nil
			default:
				return 0, nil, fatal
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		// The initial retry wait gives ReadNonQUICPacket time to enable reception.
		// Hold the fatal error until the successful packet has been consumed.
		resume := make(chan struct{})
		read := conn.readFrom
		conn.readFrom = func(b []byte) (int, net.Addr, error) {
			if len(attempts) == 3 {
				<-resume
			}
			return read(b)
		}
		defer tr.Close()
		defer close(resume)
		buf := make([]byte, 100)
		n, _, err := tr.ReadNonQUICPacket(ctx, buf)
		require.NoError(t, err)
		require.Equal(t, "\x00recovered", string(buf[:n]))
		require.Equal(t, 5*time.Millisecond, attempts[1].Sub(attempts[0]))
		require.Zero(t, attempts[2].Sub(attempts[1]), "oversized datagrams remain discard-and-continue")
		// Release the final read and verify the existing public error contract.
		resume <- struct{}{}
		<-tr.listening
		_, err = tr.Listen(&tls.Config{}, nil)
		require.ErrorIs(t, err, ErrTransportClosed)
		require.ErrorIs(t, err, fatal)
	})
}
