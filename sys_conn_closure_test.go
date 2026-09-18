package quic

import (
	"fmt"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"

	"github.com/stretchr/testify/require"
)

// These tests close the rawConn deadline/close contract (Windows datapath
// plan, Slice W1): observed blocked reads return on deadline expiry or
// connection close, concurrent socket close does not hang or panic, the
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
	t.Cleanup(func() { udpConn.Close() })
	conn := v.wrap(t, udpConn)
	t.Cleanup(func() { conn.Close() })
	return conn, udpConn.LocalAddr().(*net.UDPAddr)
}

// closureReader owns one gated worker. Its result is read only after done closes.
type closureReader struct {
	id    string
	start func()
	done  chan struct{}
	err   error
}

func newClosureReader(t *testing.T, conn rawConn) *closureReader {
	t.Helper()
	gate := make(chan struct{})
	r := &closureReader{start: sync.OnceFunc(func() { close(gate) }), done: make(chan struct{})}
	identity := make(chan string, 1)
	t.Cleanup(func() {
		conn.Close()
		r.start() // Also release a negative control after an assertion failure.
		r.wait(t)
	})
	go r.runClosureReader(conn, gate, identity)
	select {
	case header := <-identity:
		fields := strings.Fields(header)
		require.Len(t, fields, 3, "unrecognized reader identity: %q", header)
		require.Equal(t, "goroutine", fields[0], "unrecognized reader identity: %q", header)
		_, err := strconv.ParseUint(fields[1], 10, 64)
		require.NoError(t, err, "unrecognized reader identity: %q", header)
		r.id = fields[1]
	case <-time.After(scaleDuration(5 * time.Second)):
		t.Fatal("reader did not announce its identity")
	}
	return r
}

// Keep this frame in the observed read path, in addition to the worker's ID.
func (r *closureReader) runClosureReader(conn rawConn, gate <-chan struct{}, identity chan<- string) {
	defer close(r.done)
	if c, ok := conn.(interface{ releaseReadBuffers() }); ok {
		defer c.releaseReadBuffers()
	}
	var stack [2048]byte
	n := runtime.Stack(stack[:], false)
	if n == len(stack) {
		identity <- "truncated reader identity stack"
	} else {
		header, _, _ := strings.Cut(string(stack[:n]), "\n")
		identity <- header
	}
	<-gate
	for {
		p, err := conn.ReadPacket()
		if err != nil {
			r.err = err
			return
		}
		p.buffer.Release()
	}
}

func (r *closureReader) wait(t *testing.T) error {
	t.Helper()
	select {
	case <-r.done:
		return r.err
	case <-time.After(scaleDuration(5 * time.Second)):
		t.Fatal("reader did not return after deadline or close")
		return nil
	}
}

// This observer deliberately depends on Go's diagnostic stack format and runtime
// frame names on the supported toolchains. It establishes a prior observation of
// this reader parked in runtime I/O wait, not a blocked kernel syscall or continuous
// parking until the trigger. Never substitute a sleep or skip if observation fails.
func (r *closureReader) requireIOWait(t *testing.T) {
	t.Helper()
	timer := time.NewTimer(scaleDuration(5 * time.Second))
	defer timer.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	var diagnostic string
	for {
		stack, complete := closureStacks()
		if closureReaderInIOWait(stack, complete, r.id) {
			return
		}
		diagnostic = string(stack)
		select {
		case <-r.done:
			t.Fatalf("reader %s returned before runtime I/O wait was observed: %v\n%s", r.id, r.err, diagnostic)
		case <-timer.C:
			t.Fatalf("reader %s runtime I/O wait not observed (complete stack: %t)\n%s", r.id, complete, diagnostic)
		case <-ticker.C:
		}
	}
}

func closureStacks() ([]byte, bool) {
	// A full buffer may be truncated. Grow within a fixed bound, then fail closed.
	for size := 64 << 10; size <= 4<<20; size *= 2 {
		buf := make([]byte, size)
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return buf[:n], true
		}
		if size == 4<<20 {
			return buf[:n], false
		}
	}
	panic("unreachable")
}

func closureReaderInIOWait(stack []byte, complete bool, id string) bool {
	if !complete || id == "" {
		return false
	}
	for record := range strings.SplitSeq(string(stack), "\n\n") {
		header, frames, ok := strings.Cut(record, "\n")
		if !ok || !strings.HasPrefix(header, "goroutine "+id+" [") || !strings.HasSuffix(header, "]:") {
			continue
		}
		state := strings.TrimSuffix(strings.TrimPrefix(header, "goroutine "+id+" ["), "]:")
		if state != "IO wait" && !strings.HasPrefix(state, "IO wait, ") {
			return false
		}
		if strings.Contains(frames, " frames elided") ||
			!strings.Contains(frames, "internal/poll.runtime_pollWait(") ||
			!strings.Contains(frames, "github.com/quic-go/quic-go.(*closureReader).runClosureReader(") {
			return false
		}
		for _, impl := range []string{"oobConn", "windowsConn", "basicConn"} {
			if strings.Contains(frames, "github.com/quic-go/quic-go.(*"+impl+").ReadPacket(") {
				return true
			}
		}
		return false
	}
	return false
}

func TestRawConnBlockedReadObservesDeadline(t *testing.T) {
	for _, v := range rawConnVariants() {
		t.Run(v.name, func(t *testing.T) {
			conn, _ := newClosureTestConn(t, v)
			reader := newClosureReader(t, conn)
			reader.start()
			reader.requireIOWait(t)
			require.NoError(t, conn.SetReadDeadline(time.Now().Add(scaleDuration(50*time.Millisecond))))
			err := reader.wait(t)
			require.ErrorIs(t, err, os.ErrDeadlineExceeded)
		})
	}
}

func TestRawConnDeadlineSetWhileReadBlocked(t *testing.T) {
	for _, v := range rawConnVariants() {
		t.Run(v.name, func(t *testing.T) {
			conn, _ := newClosureTestConn(t, v)
			reader := newClosureReader(t, conn)
			reader.start()
			reader.requireIOWait(t)
			require.NoError(t, conn.SetReadDeadline(time.Now()))
			err := reader.wait(t)
			require.ErrorIs(t, err, os.ErrDeadlineExceeded)
		})
	}
}

func TestRawConnBlockedReadObservesClose(t *testing.T) {
	for _, v := range rawConnVariants() {
		t.Run(v.name, func(t *testing.T) {
			conn, _ := newClosureTestConn(t, v)
			reader := newClosureReader(t, conn)
			reader.start()
			reader.requireIOWait(t)
			require.NoError(t, conn.Close())
			err := reader.wait(t)
			require.ErrorIs(t, err, net.ErrClosed)
		})
	}
}

func TestRawConnConcurrentCloseDuringRead(t *testing.T) {
	for _, v := range rawConnVariants() {
		t.Run(v.name, func(t *testing.T) {
			for i := range 20 {
				conn, addr := newClosureTestConn(t, v)
				sender, err := net.DialUDP("udp4", nil, addr)
				require.NoError(t, err)
				t.Cleanup(func() { sender.Close() })

				reader := newClosureReader(t, conn)
				reader.start()
				senderDone := make(chan struct{})
				t.Cleanup(func() {
					sender.Close()
					select {
					case <-senderDone:
					case <-time.After(scaleDuration(5 * time.Second)):
						t.Error("burst sender did not return after close")
					}
				})
				go func() {
					defer close(senderDone)
					for range 10 {
						if _, err := sender.Write([]byte("closure test payload")); err != nil {
							return
						}
					}
				}()
				// Supplemental schedule variation; it does not establish which
				// read/burst interleaving was exercised.
				if i%2 == 0 {
					time.Sleep(time.Duration(i) * 100 * time.Microsecond)
				}
				require.NoError(t, conn.Close())
				err = reader.wait(t)
				require.Error(t, err)
				select {
				case <-senderDone:
				case <-time.After(scaleDuration(5 * time.Second)):
					t.Fatal("burst sender did not finish")
				}
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
			if c, ok := conn.(interface{ releaseReadBuffers() }); ok {
				defer c.releaseReadBuffers()
			}

			require.NoError(t, conn.SetReadDeadline(time.Now().Add(scaleDuration(5*time.Second))))
			received := make(map[string]struct{})
			for b := range bursts {
				for i := range packetsPerBurst {
					_, err := sender.Write(fmt.Appendf(nil, "packet %d/%d", b, i))
					require.NoError(t, err)
				}
				for range packetsPerBurst {
					p, err := conn.ReadPacket()
					require.NoError(t, err)
					remote := p.remoteAddr.String()
					received[string(p.data)] = struct{}{}
					p.buffer.Release()
					require.Equal(t, sender.LocalAddr().String(), remote)
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

// The old error oracle also passes when the read starts after the trigger.
// Holding the gate makes that schedule deterministic without a timing campaign.
func TestRawConnLateReadDoesNotEstablishReadiness(t *testing.T) {
	for _, v := range rawConnVariants() {
		for _, action := range []string{"deadline", "close"} {
			t.Run(v.name+"/"+action, func(t *testing.T) {
				conn, _ := newClosureTestConn(t, v)
				reader := newClosureReader(t, conn)
				stack, complete := closureStacks()
				require.True(t, complete, "truncated stack cannot validate the negative control")
				require.Contains(t, string(stack), "goroutine "+reader.id+" [")
				require.False(t, closureReaderInIOWait(stack, complete, reader.id), "gated reader must not count as ready\n%s", stack)

				want := os.ErrDeadlineExceeded
				if action == "deadline" {
					require.NoError(t, conn.SetReadDeadline(time.Now()))
				} else {
					want = net.ErrClosed
					require.NoError(t, conn.Close())
				}
				reader.start()
				require.ErrorIs(t, reader.wait(t), want)
			})
		}
	}
}

func TestClosureReaderIOWaitObservation(t *testing.T) {
	// A representative diagnostic shape, not a general goroutine-stack grammar.
	const parked = "goroutine 42 [IO wait]:\n" +
		"internal/poll.runtime_pollWait(0x123, 0x72)\n\t/runtime/netpoll.go:351\n" +
		"github.com/quic-go/quic-go.(*basicConn).ReadPacket(0x123)\n\t/sys_conn.go:139\n" +
		"github.com/quic-go/quic-go.(*closureReader).runClosureReader(0x123)\n\t/sys_conn_closure_test.go:100\n"
	for _, tc := range []struct {
		name     string
		stack    string
		complete bool
		want     bool
	}{
		{name: "target parked", stack: parked, complete: true, want: true},
		{name: "truncated", stack: parked, complete: false},
		{name: "unrecognized", stack: "unrecognized runtime diagnostic", complete: true},
		{name: "unrelated parked reader", stack: strings.Replace(parked, "goroutine 42", "goroutine 43", 1), complete: true},
		{name: "target not parked", stack: strings.Replace(parked, "IO wait", "runnable", 1), complete: true},
		{name: "missing native read", stack: strings.Replace(parked, "(*basicConn).ReadPacket", "(*otherConn).ReadPacket", 1), complete: true},
		{name: "missing worker", stack: strings.Replace(parked, "runClosureReader", "otherWorker", 1), complete: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, closureReaderInIOWait([]byte(tc.stack), tc.complete, "42"))
		})
	}
}
