package quic

import (
	"fmt"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"

	"github.com/stretchr/testify/require"
)

// These tests close the rawConn deadline/close contract (Windows datapath
// plan, Slice W1): every blocked read observes deadline expiry, connection
// close, and concurrent socket close without hanging or panicking, the
// poller sustains transfer, and writes after close fail cleanly. They run
// against the platform conn selected by wrapConn and against the basicConn
// fallback used for caller-supplied non-OOB-capable sockets, so they
// characterize the behavior every rawConn implementation must preserve.

// nonOOBPacketConn hides the OOB and syscall methods of a *net.UDPConn so
// wrapConn falls back to basicConn.
type nonOOBPacketConn struct{ net.PacketConn }

type rawConnVariant struct {
	name string
	wrap func(*testing.T, *net.UDPConn) rawConn
}

func rawConnVariants() []rawConnVariant {
	return []rawConnVariant{
		{
			name: "platform",
			wrap: func(t *testing.T, c *net.UDPConn) rawConn {
				conn, err := wrapConn(c, true)
				require.NoError(t, err)
				return conn
			},
		},
		{
			name: "basicConn",
			wrap: func(t *testing.T, c *net.UDPConn) rawConn {
				conn, err := wrapConn(&nonOOBPacketConn{PacketConn: c}, false)
				require.NoError(t, err)
				_, isBasic := conn.(*basicConn)
				require.True(t, isBasic)
				return conn
			},
		},
	}
}

func newClosureTestConn(t *testing.T, v rawConnVariant) (rawConn, *net.UDPAddr) {
	t.Helper()
	udpConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	conn := v.wrap(t, udpConn)
	t.Cleanup(func() { conn.Close() })
	return conn, udpConn.LocalAddr().(*net.UDPAddr)
}

// readPacketAsync guards against the failure mode under test: a read that
// never returns. It reports the read result, or fails the test on a hang.
func readPacketAsync(t *testing.T, conn rawConn) <-chan error {
	t.Helper()
	errChan := make(chan error, 1)
	go func() {
		for {
			p, err := conn.ReadPacket()
			if err != nil {
				errChan <- err
				return
			}
			p.buffer.Release()
		}
	}()
	return errChan
}

func requireReadUnblocked(t *testing.T, errChan <-chan error) error {
	t.Helper()
	select {
	case err := <-errChan:
		return err
	case <-time.After(scaleDuration(5 * time.Second)):
		t.Fatal("blocked read did not observe the deadline or close")
		return nil
	}
}

func TestRawConnBlockedReadObservesDeadline(t *testing.T) {
	for _, v := range rawConnVariants() {
		t.Run(v.name, func(t *testing.T) {
			conn, _ := newClosureTestConn(t, v)
			require.NoError(t, conn.SetReadDeadline(time.Now().Add(scaleDuration(50*time.Millisecond))))
			err := requireReadUnblocked(t, readPacketAsync(t, conn))
			require.ErrorIs(t, err, os.ErrDeadlineExceeded)
		})
	}
}

func TestRawConnDeadlineSetWhileReadBlocked(t *testing.T) {
	for _, v := range rawConnVariants() {
		t.Run(v.name, func(t *testing.T) {
			conn, _ := newClosureTestConn(t, v)
			errChan := readPacketAsync(t, conn)
			time.Sleep(scaleDuration(50 * time.Millisecond)) // let the read block
			require.NoError(t, conn.SetReadDeadline(time.Now()))
			err := requireReadUnblocked(t, errChan)
			require.ErrorIs(t, err, os.ErrDeadlineExceeded)
		})
	}
}

func TestRawConnBlockedReadObservesClose(t *testing.T) {
	for _, v := range rawConnVariants() {
		t.Run(v.name, func(t *testing.T) {
			conn, _ := newClosureTestConn(t, v)
			errChan := readPacketAsync(t, conn)
			time.Sleep(scaleDuration(50 * time.Millisecond)) // let the read block
			require.NoError(t, conn.Close())
			err := requireReadUnblocked(t, errChan)
			require.ErrorIs(t, err, net.ErrClosed)
		})
	}
}

func TestRawConnConcurrentCloseDuringRead(t *testing.T) {
	for _, v := range rawConnVariants() {
		t.Run(v.name, func(t *testing.T) {
			for i := 0; i < 20; i++ {
				conn, addr := newClosureTestConn(t, v)
				sender, err := net.DialUDP("udp4", nil, addr)
				require.NoError(t, err)

				errChan := readPacketAsync(t, conn)
				var wg sync.WaitGroup
				wg.Add(1)
				go func() {
					defer wg.Done()
					for j := 0; j < 10; j++ {
						if _, err := sender.Write([]byte("closure test payload")); err != nil {
							return
						}
					}
				}()
				// Close while the reader races the sender: sometimes
				// mid-burst, sometimes against an already-blocked read.
				if i%2 == 0 {
					time.Sleep(time.Duration(i) * 100 * time.Microsecond)
				}
				require.NoError(t, conn.Close())
				err = requireReadUnblocked(t, errChan)
				require.Error(t, err)
				wg.Wait()
				sender.Close()
			}
		})
	}
}

func TestRawConnSustainedTransfer(t *testing.T) {
	const (
		bursts          = 8
		packetsPerBurst = 32
	)
	for _, v := range rawConnVariants() {
		t.Run(v.name, func(t *testing.T) {
			conn, addr := newClosureTestConn(t, v)
			sender, err := net.DialUDP("udp4", nil, addr)
			require.NoError(t, err)
			defer sender.Close()

			require.NoError(t, conn.SetReadDeadline(time.Now().Add(scaleDuration(5*time.Second))))
			received := make(map[string]struct{})
			for b := 0; b < bursts; b++ {
				for i := 0; i < packetsPerBurst; i++ {
					_, err := sender.Write(fmt.Appendf(nil, "packet %d/%d", b, i))
					require.NoError(t, err)
				}
				for i := 0; i < packetsPerBurst; i++ {
					p, err := conn.ReadPacket()
					require.NoError(t, err)
					require.Equal(t, sender.LocalAddr().String(), p.remoteAddr.String())
					received[string(p.data)] = struct{}{}
					p.buffer.Release()
				}
			}
			require.Len(t, received, bursts*packetsPerBurst)
		})
	}
}

func TestRawConnWriteAfterClose(t *testing.T) {
	for _, v := range rawConnVariants() {
		t.Run(v.name, func(t *testing.T) {
			conn, addr := newClosureTestConn(t, v)
			require.NoError(t, conn.Close())
			_, err := conn.WritePacket([]byte("foobar"), addr, nil, 0, protocol.ECNUnsupported)
			require.ErrorIs(t, err, net.ErrClosed)
		})
	}
}
