//go:build unix

package quic

import (
	"errors"
	"net"
	"net/netip"
	"os"
	"runtime"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

type endpointScalarTestRaw struct {
	write func(func(uintptr) bool) error
}

func newEndpointScalarTestSocket(t *testing.T) *endpointScalarSocket {
	t.Helper()
	return &endpointScalarSocket{
		endpointNativeIO: endpointTestNative{},
		raw: endpointScalarTestRaw{write: func(f func(uintptr) bool) error {
			require.True(t, f(91))
			return nil
		}},
		send:    func(_ uintptr, p, _ []byte, _ *unix.SockaddrInet4) (int, error) { return len(p), nil },
		network: "udp4", source: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234},
	}
}

func TestEndpointScalarArmsBeforeWait(t *testing.T) {
	request := endpointTestRequest(1, "aabb")
	defer request.members[0].buffer.Release()
	request.deadline = time.Now().Add(time.Second)
	request.members[0].gsoSize = 2 // Existing member writer owns scalar segmentation.
	var deadlines []time.Time
	var physical time.Time
	native := newEndpointScalarTestSocket(t)
	native.endpointNativeIO = endpointTestNative{deadline: func(d time.Time) error {
		deadlines = append(deadlines, d)
		physical = d
		return nil
	}}
	native.raw = endpointScalarTestRaw{write: func(f func(uintptr) bool) error {
		require.False(t, f(91))
		require.Equal(t, request.deadline, physical, "D must be armed before Go can wait")
		require.True(t, f(91))
		return nil
	}}
	calls := 0
	native.send = func(_ uintptr, p, _ []byte, _ *unix.SockaddrInet4) (int, error) {
		calls++
		if calls%2 == 1 {
			return 0, syscall.EAGAIN
		}
		return len(p), nil
	}
	token := &endpointToken{request: &request}
	io, _, err := prepareEndpointRequestIO(native, token)
	require.NoError(t, err)
	result := writeEndpointMember(io, token, request.members[0], nil, nil)
	require.NoError(t, result.err)
	require.Equal(t, 2, result.accepted)
	require.Zero(t, result.uncertain)
	require.Zero(t, result.unsubmitted)
	require.Equal(t, []time.Time{{}, request.deadline}, deadlines, "keep one D through both segments")
}

func TestEndpointScalarRetryPermission(t *testing.T) {
	for _, outcome := range []string{"success", "deadline", "revoked"} {
		t.Run(outcome, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				request := endpointRequest{
					deadline: time.Now().Add(time.Second), destination: netip.MustParseAddrPort("127.0.0.1:4321"),
					members: []endpointMember{{datagram: &endpointDatagram{}}},
				}
				token := &endpointToken{request: &request}
				native := newEndpointScalarTestSocket(t)
				calls := 0
				native.send = func(_ uintptr, p, _ []byte, _ *unix.SockaddrInet4) (int, error) {
					calls++
					if calls == 1 {
						switch outcome {
						case "deadline":
							time.Sleep(2 * time.Second)
						case "revoked":
							token.canceled.Store(true)
						}
						return 0, syscall.EINTR
					}
					return len(p), nil
				}
				io, _, err := prepareEndpointRequestIO(native, token)
				require.NoError(t, err)
				n, _, err := io.WriteMsgUDPAddrPort([]byte("p"), nil, netip.MustParseAddrPort("127.0.0.1:4321"))
				switch outcome {
				case "success":
					require.NoError(t, err)
					require.Equal(t, 1, n)
					require.Equal(t, 2, calls)
				case "deadline":
					require.ErrorIs(t, err, os.ErrDeadlineExceeded)
					require.Equal(t, 1, calls)
				case "revoked":
					require.ErrorIs(t, err, errEndpointRevoked)
					require.Equal(t, 1, calls)
				}
			})
		})
	}
}

func (r endpointScalarTestRaw) Control(func(uintptr)) error      { panic("unexpected control") }
func (r endpointScalarTestRaw) Read(func(uintptr) bool) error    { panic("unexpected read") }
func (r endpointScalarTestRaw) Write(f func(uintptr) bool) error { return r.write(f) }

func TestEndpointScalarFastWrite(t *testing.T) {
	var deadlines []time.Time
	data, oob := []byte("payload"), []byte("control")
	destination := netip.MustParseAddrPort("[::ffff:127.0.0.1]:4321")
	request := endpointRequest{
		destination: destination, deadline: time.Now().Add(time.Second),
		members: []endpointMember{{datagram: &endpointDatagram{data: data}}},
	}
	native := &endpointScalarSocket{
		endpointNativeIO: endpointTestNative{deadline: func(d time.Time) error {
			deadlines = append(deadlines, d)
			return nil
		}},
		raw: endpointScalarTestRaw{write: func(f func(uintptr) bool) error {
			require.True(t, f(91), "an immediate send must not wait")
			return nil
		}},
		send: func(fd uintptr, p, control []byte, addr *unix.SockaddrInet4) (int, error) {
			require.EqualValues(t, 91, fd)
			require.Equal(t, data, p)
			require.Equal(t, oob, control)
			require.Equal(t, [4]byte{127, 0, 0, 1}, addr.Addr)
			require.Equal(t, 4321, addr.Port)
			return len(p), nil
		},
		network: "udp4", source: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234},
	}
	io, scope, err := prepareEndpointRequestIO(native, &endpointToken{request: &request})
	require.NoError(t, err)
	require.NotNil(t, scope)
	n, nn, err := io.WriteMsgUDPAddrPort(data, oob, destination)
	require.NoError(t, err)
	require.Equal(t, len(data), n)
	require.Equal(t, len(oob), nn)
	require.Equal(t, []time.Time{{}}, deadlines, "fast output only validates with real zero")
	require.NoError(t, native.SetWriteDeadline(time.Time{}))
	require.Equal(t, []time.Time{{}, {}}, deadlines)
}

func TestEndpointScalarSendErrorSurface(t *testing.T) {
	for _, cause := range []syscall.Errno{syscall.EMSGSIZE, syscall.EPERM, syscall.ENOBUFS} {
		t.Run(cause.Error(), func(t *testing.T) {
			request := endpointTestRequest(1, "data")
			defer request.members[0].buffer.Release()
			request.deadline = time.Now().Add(time.Second)
			request.members[0].writeState = &endpointWriteState{}
			native := newEndpointScalarTestSocket(t)
			calls := 0
			native.send = func(uintptr, []byte, []byte, *unix.SockaddrInet4) (int, error) {
				calls++
				return 0, cause
			}
			token := &endpointToken{request: &request}
			io, _, err := prepareEndpointRequestIO(native, token)
			require.NoError(t, err)
			result := writeEndpointMember(io, token, request.members[0], nil, nil)
			require.ErrorIs(t, result.err, cause)
			var op *net.OpError
			require.ErrorAs(t, result.err, &op)
			require.Equal(t, "write", op.Op)
			require.Equal(t, "udp4", op.Net)
			require.Same(t, native.source, op.Source)
			require.Equal(t, "127.0.0.1:1234", op.Addr.String())
			require.Equal(t, "udp", op.Addr.Network())
			var system *os.SyscallError
			require.ErrorAs(t, result.err, &system)
			require.Equal(t, "sendmsg", system.Syscall)
			require.Equal(t, cause.Temporary(), op.Temporary())
			require.Equal(t, cause.Timeout(), op.Timeout())
			if cause == syscall.EPERM {
				require.Equal(t, 2, calls, "first-send EPERM still retries once")
			} else {
				require.Equal(t, 1, calls)
			}
			if cause == syscall.EMSGSIZE {
				require.Equal(t, 1, result.unsubmitted)
				require.Zero(t, result.uncertain)
			} else {
				require.Zero(t, result.unsubmitted)
				require.Equal(t, 1, result.uncertain)
			}
		})
	}
}

func TestEndpointScalarRawErrorSurface(t *testing.T) {
	for _, cause := range []error{os.ErrDeadlineExceeded, net.ErrClosed} {
		t.Run(cause.Error(), func(t *testing.T) {
			destination := netip.MustParseAddrPort("[::ffff:127.0.0.1]:0")
			request := endpointRequest{deadline: time.Now().Add(time.Second), destination: destination, members: []endpointMember{{datagram: &endpointDatagram{}}}}
			native := newEndpointScalarTestSocket(t)
			rawCalls := 0
			native.raw = endpointScalarTestRaw{write: func(f func(uintptr) bool) error {
				rawCalls++
				if errors.Is(cause, net.ErrClosed) && rawCalls == 1 {
					require.True(t, f(91))
					return nil
				}
				if errors.Is(cause, os.ErrDeadlineExceeded) {
					require.False(t, f(91))
				}
				return &net.OpError{Op: "raw-write", Net: "udp4", Source: native.source, Err: cause}
			}}
			native.send = func(_ uintptr, p, _ []byte, _ *unix.SockaddrInet4) (int, error) {
				if errors.Is(cause, net.ErrClosed) {
					return len(p), nil
				}
				return 0, syscall.EAGAIN
			}
			io, _, err := prepareEndpointRequestIO(native, &endpointToken{request: &request})
			require.NoError(t, err)
			if errors.Is(cause, net.ErrClosed) {
				n, nn, err := io.WriteMsgUDPAddrPort([]byte("first success"), []byte("first control"), destination)
				require.NoError(t, err)
				require.Equal(t, 13, n)
				require.Equal(t, 13, nn)
			}
			n, nn, err := io.WriteMsgUDPAddrPort([]byte("p"), []byte("oob"), destination)
			require.Zero(t, n)
			require.Zero(t, nn)
			require.ErrorIs(t, err, cause)
			var op *net.OpError
			require.ErrorAs(t, err, &op)
			require.Equal(t, "write", op.Op)
			require.Equal(t, "udp4", op.Net)
			require.Same(t, native.source, op.Source)
			require.Equal(t, "[::ffff:127.0.0.1]:0", op.Addr.String())
			require.Equal(t, "udp", op.Addr.Network())
			address, ok := op.Addr.(interface{ Addr() netip.Addr })
			require.True(t, ok)
			require.Equal(t, destination.Addr(), address.Addr())
			var system *os.SyscallError
			require.False(t, errors.As(err, &system), "poller causes must not become synthetic errnos")
			wantTimeout := errors.Is(cause, os.ErrDeadlineExceeded)
			require.Equal(t, wantTimeout, op.Timeout())
			require.Equal(t, wantTimeout, op.Temporary())
		})
	}
}

func TestEndpointScalarWholeRequestSelection(t *testing.T) {
	for _, geometry := range []string{"software-segments", "later-native-gso", "ipv6-zone"} {
		t.Run(geometry, func(t *testing.T) {
			request := endpointTestRequest(1, "first", "aabb")
			for _, member := range request.members {
				defer member.buffer.Release()
			}
			request.deadline = time.Now().Add(time.Second)
			request.members[1].gsoSize = 2
			switch geometry {
			case "later-native-gso":
				request.members[1].nativeGSO = true
			case "ipv6-zone":
				request.destination = netip.MustParseAddrPort("[fe80::1%test]:4321")
			}
			var deadlines []time.Time
			native := newEndpointScalarTestSocket(t)
			native.endpointNativeIO = endpointTestNative{deadline: func(d time.Time) error {
				deadlines = append(deadlines, d)
				return nil
			}}
			_, scope, err := prepareEndpointRequestIO(native, &endpointToken{request: &request})
			require.NoError(t, err)
			if geometry == "software-segments" {
				require.NotNil(t, scope)
				require.Equal(t, []time.Time{{}}, deadlines)
			} else {
				require.Nil(t, scope, "one unsupported member/address makes the whole request eager")
				require.Equal(t, []time.Time{request.deadline}, deadlines)
			}
		})
	}
}

func TestEndpointScalarInitialDeadlineFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		setupErr := errors.New("initial deadline control failed")
		native := newEndpointScalarTestSocket(t)
		var deadlines []time.Time
		native.endpointNativeIO = endpointTestNative{deadline: func(d time.Time) error {
			deadlines = append(deadlines, d)
			if len(deadlines) == 1 && d.IsZero() {
				return setupErr
			}
			return nil
		}}
		native.send = func(uintptr, []byte, []byte, *unix.SockaddrInet4) (int, error) {
			t.Fatal("initial setup failure must not submit")
			return 0, nil
		}
		e := startNativeEndpoint(native, time.Second, nil)
		defer func() { <-e.stop() }()
		p, err := e.attach(1)
		require.NoError(t, err)
		require.NoError(t, e.offer(p, 0, endpointTestRequest(1, "first", "tail")))
		<-p.completed
		result, ok := e.take(p)
		require.True(t, ok)
		for _, member := range result.members {
			require.ErrorIs(t, member.err, setupErr)
			require.Equal(t, 1, member.unsubmitted)
			require.Zero(t, member.accepted)
			require.Zero(t, member.uncertain)
		}
		require.Equal(t, []time.Time{{}, {}}, deadlines, "initial failure still attempts actual final reset")
		select {
		case <-e.fault:
		default:
			t.Fatal("deadline control failure must fault the endpoint")
		}
	})
}

func TestEndpointScalarLateArmFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		armErr := errors.New("late deadline control failed")
		native := newEndpointScalarTestSocket(t)
		var deadlines []time.Time
		native.endpointNativeIO = endpointTestNative{deadline: func(d time.Time) error {
			deadlines = append(deadlines, d)
			if !d.IsZero() {
				return &net.OpError{Op: "set", Net: "udp4", Addr: native.source, Err: armErr}
			}
			return nil
		}}
		calls := 0
		native.send = func(_ uintptr, p, _ []byte, _ *unix.SockaddrInet4) (int, error) {
			calls++
			if calls == 1 {
				require.Equal(t, "aa", string(p))
				return len(p), nil
			}
			require.Equal(t, "bb", string(p))
			return 0, syscall.EAGAIN
		}
		e := startNativeEndpoint(native, time.Second, nil)
		defer func() { <-e.stop() }()
		p, err := e.attach(1)
		require.NoError(t, err)
		request := endpointTestRequest(1, "aabb", "untouched")
		request.deadline = time.Now().Add(time.Second)
		request.members[0].gsoSize = 2
		require.NoError(t, e.offer(p, 0, request))
		<-p.completed
		result, ok := e.take(p)
		require.True(t, ok)
		require.ErrorIs(t, result.members[0].err, armErr)
		var op *net.OpError
		require.ErrorAs(t, result.members[0].err, &op)
		require.Equal(t, "write", op.Op)
		require.Equal(t, 1, result.members[0].accepted)
		require.Equal(t, 1, result.members[0].uncertain)
		require.Zero(t, result.members[0].unsubmitted)
		require.Equal(t, 1, result.members[1].unsubmitted)
		require.NoError(t, result.members[1].err)
		require.Equal(t, 2, calls)
		require.Equal(t, []time.Time{{}, request.deadline, {}}, deadlines)
		select {
		case <-e.fault:
		default:
			t.Fatal("a non-closed deadline control failure must still fault")
		}
	})
}

func TestEndpointScalarFinalFence(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reset := make(chan struct{})
		resetErr := errors.New("final deadline reset failed")
		zeroCalls := 0
		native := newEndpointScalarTestSocket(t)
		native.endpointNativeIO = endpointTestNative{deadline: func(d time.Time) error {
			if d.IsZero() {
				zeroCalls++
				if zeroCalls == 2 {
					<-reset
					return resetErr
				}
			}
			return nil
		}}
		payload := []byte("borrowed")
		native.send = func(_ uintptr, p, _ []byte, _ *unix.SockaddrInet4) (int, error) {
			require.Same(t, &payload[0], &p[0])
			return len(p), nil
		}
		e := startNativeEndpoint(native, time.Second, nil)
		defer func() { <-e.stop() }()
		transport := &Transport{responses: newEndpointResponses(e)}
		type receipt struct {
			n   int
			err error
		}
		returned := make(chan receipt, 1)
		go func() {
			n, err := transport.WriteTo(payload, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234})
			returned <- receipt{n, err}
		}()
		synctest.Wait()
		select {
		case <-returned:
			t.Fatal("public borrow returned before actual final reset")
		default:
		}
		close(reset)
		result := <-returned
		require.Equal(t, len(payload), result.n, "reset failure cannot rewrite accepted bytes")
		require.ErrorIs(t, result.err, resetErr)
		require.NotErrorIs(t, result.err, ErrMultipathUncertainV1)
	})
}

func TestEndpointScalarEmptyDatagramAndMetadata(t *testing.T) {
	receiver, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	defer receiver.Close()
	// Match ordinaryCostTransport's explicit IPv4 "udp" construction. On the
	// supported platform this must select production scalar I/O, not just work
	// through an eager fallback or a separately bound test adapter.
	socket, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	defer socket.Close()
	raw, err := socket.SyscallConn()
	require.NoError(t, err)
	native := newEndpointNativeIO(socket)
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		// Exercise the request/error/empty-datagram mechanics locally without
		// enabling this mechanism on an unqualified production platform.
		native = bindEndpointScalarSocket(socket, raw, func(fd uintptr, p, oob []byte, address *unix.SockaddrInet4) (int, error) {
			return unix.SendmsgN(int(fd), p, oob, address, unix.MSG_DONTWAIT)
		})
	}
	bound, ok := native.(*endpointScalarSocket)
	require.True(t, ok)
	require.Equal(t, "udp", bound.network)
	require.Same(t, socket.LocalAddr(), bound.source)
	e := startNativeEndpoint(native, time.Second, nil)
	defer func() { <-e.stop() }()
	transport := &Transport{responses: newEndpointResponses(e)}
	for _, data := range [][]byte{nil, []byte("after-empty")} {
		n, err := transport.WriteTo(data, receiver.LocalAddr())
		require.NoError(t, err)
		require.Equal(t, len(data), n)
	}
	require.NoError(t, receiver.SetReadDeadline(time.Now().Add(time.Second)))
	var buffer [64]byte
	n, _, err := receiver.ReadFromUDP(buffer[:])
	require.NoError(t, err)
	require.Zero(t, n, "valid empty public write must send a real datagram")
	n, _, err = receiver.ReadFromUDP(buffer[:])
	require.NoError(t, err)
	require.Equal(t, "after-empty", string(buffer[:n]), "metadata validation must not emit another empty datagram")
}
