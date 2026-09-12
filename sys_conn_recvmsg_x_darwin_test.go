//go:build darwin && !ios && !quic_go_no_private_syscalls

package quic

import (
	"net"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/net/ipv4"

	"github.com/stretchr/testify/require"
)

// engagedRecvmsgXForTesting marks the receive capability qualified without
// running the loopback self-check, so fake-syscall closure tests exercise
// the engaged path deterministically on any host.
func engagedRecvmsgXForTesting(t *testing.T) {
	t.Helper()
	resetRecvmsgXForTesting(t)
	recvmsgX.qualifyOnce.Do(func() {})
	recvmsgX.qualifyStarted.Store(true)
	recvmsgX.qualified.Store(true)
}

// rawConnStub satisfies syscall.RawConn for the fake-syscall tests: Read
// invokes the callback with a dummy fd until it reports readiness, exactly
// the poller contract the production wrapper relies on.
type rawConnStub struct{ readCalls int }

func (r *rawConnStub) Read(f func(uintptr) bool) error {
	r.readCalls++
	for !f(0) {
	}
	return nil
}
func (r *rawConnStub) Write(f func(uintptr) bool) error { return nil }
func (r *rawConnStub) Control(f func(uintptr)) error    { f(0); return nil }

// recordingBatchConn is the fallback the wrapper must delegate to whenever
// the capability declines: it records the message-slice length of each call
// and fills one message like a single-datagram x/net read would.
type recordingBatchConn struct {
	calls []int
	addr  net.Addr
}

func (c *recordingBatchConn) ReadBatch(ms []ipv4.Message, flags int) (int, error) {
	c.calls = append(c.calls, len(ms))
	copy(ms[0].Buffers[0], "fallback")
	ms[0].N = len("fallback")
	ms[0].NN = 0
	ms[0].Addr = c.addr
	return 1, nil
}

func makeReadMessages(n int) []ipv4.Message {
	ms := make([]ipv4.Message, n)
	for i := range ms {
		ms[i].Buffers = [][]byte{make([]byte, 1452)}
		ms[i].OOB = make([]byte, oobBufferSize)
	}
	return ms
}

// fakeRecvEntry scripts one kernel-delivered message for the fake syscall.
type fakeRecvEntry struct {
	payload []byte
	oob     []byte
	port    int
	v6Addr  [16]byte
}

// installFakeRecvmsgX makes rawRecvmsgX deliver the scripted entries by
// writing through the prepared msghdr_x pointers, the way the kernel does.
func installFakeRecvmsgX(t *testing.T, fn func(msgs []msghdrX) (int, syscall.Errno)) {
	t.Helper()
	orig := rawRecvmsgX
	rawRecvmsgX = func(_ int, msgs []msghdrX, _ int) (int, syscall.Errno) { return fn(msgs) }
	t.Cleanup(func() { rawRecvmsgX = orig })
}

func fillFakeEntries(msgs []msghdrX, entries []fakeRecvEntry) (int, syscall.Errno) {
	for i, e := range entries {
		buf := unsafe.Slice(msgs[i].Iov.Base, msgs[i].Iov.Len)
		copy(buf, e.payload)
		msgs[i].Datalen = uint64(len(e.payload))
		if len(e.oob) > 0 {
			ctrl := unsafe.Slice(msgs[i].Control, msgs[i].Controllen)
			copy(ctrl, e.oob)
		}
		msgs[i].Controllen = uint32(len(e.oob))
		sa := (*syscall.RawSockaddrInet6)(unsafe.Pointer(msgs[i].Name))
		*sa = syscall.RawSockaddrInet6{
			Len:    syscall.SizeofSockaddrInet6,
			Family: syscall.AF_INET6,
			Addr:   e.v6Addr,
		}
		bePort(unsafe.Pointer(&sa.Port), e.port)
		msgs[i].Namelen = syscall.SizeofSockaddrInet6
	}
	return len(entries), 0
}

// While the capability is not engaged, every read must delegate to the
// wrapped fallback path untouched — the preservation obligation for the
// capability-off configurations (kill switch, unqualified major, latch).
func TestRecvmsgXReadBatchDelegatesWhenOff(t *testing.T) {
	resetRecvmsgXForTesting(t)
	t.Setenv(recvmsgXDisableEnv, "true")
	recvmsgXEnsureQualified()
	installFakeRecvmsgX(t, func([]msghdrX) (int, syscall.Errno) {
		t.Fatal("recvmsg_x must not be invoked while the capability is off")
		return 0, 0
	})
	base := &recordingBatchConn{addr: &net.UDPAddr{IP: net.IPv6loopback, Port: 1}}
	conn := newRecvmsgXConn(base, &rawConnStub{})
	ms := makeReadMessages(batchSize)
	n, err := conn.ReadBatch(ms, 0)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, []int{batchSize}, base.calls, "the fallback read must see the caller's message slice unchanged")
	require.Equal(t, "fallback", string(ms[0].Buffers[0][:ms[0].N]))
}

// A single-message read must delegate even while engaged: the shared read
// loop only offers one message when the effective batch size is one, and
// that read must stay on the preserved path.
func TestRecvmsgXReadBatchSingleMessageDelegates(t *testing.T) {
	engagedRecvmsgXForTesting(t)
	installFakeRecvmsgX(t, func([]msghdrX) (int, syscall.Errno) {
		t.Fatal("recvmsg_x must not be invoked for a single-message read")
		return 0, 0
	})
	base := &recordingBatchConn{addr: &net.UDPAddr{IP: net.IPv6loopback, Port: 1}}
	conn := newRecvmsgXConn(base, &rawConnStub{})
	n, err := conn.ReadBatch(makeReadMessages(1), 0)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, []int{1}, base.calls)
}

// A full batch surfaces every message exactly once, each with its own
// payload bytes, its own ancillary data, and its own source address — the
// closure invariant, full-fill class.
func TestRecvmsgXReadBatchFullBatch(t *testing.T) {
	engagedRecvmsgXForTesting(t)
	entries := make([]fakeRecvEntry, batchSize)
	for i := range entries {
		entries[i] = fakeRecvEntry{
			payload: []byte{byte('a' + i), byte('a' + i)},
			oob:     []byte{byte(i + 1)},
			port:    1000 + i,
			v6Addr:  [16]byte{15: byte(i + 1)},
		}
	}
	installFakeRecvmsgX(t, func(msgs []msghdrX) (int, syscall.Errno) {
		require.Len(t, msgs, batchSize)
		return fillFakeEntries(msgs, entries)
	})
	base := &recordingBatchConn{}
	conn := newRecvmsgXConn(base, &rawConnStub{})
	ms := makeReadMessages(batchSize)
	readsBefore, datagramsBefore, _ := recvmsgXCountersSnapshot()
	n, err := conn.ReadBatch(ms, 0)
	require.NoError(t, err)
	require.Equal(t, batchSize, n)
	require.Empty(t, base.calls, "an engaged full batch must not touch the fallback path")
	for i, e := range entries {
		require.Equal(t, e.payload, ms[i].Buffers[0][:ms[i].N], "message %d payload", i)
		require.Equal(t, e.oob, ms[i].OOB[:ms[i].NN], "message %d must carry its own ancillary data", i)
		addr := ms[i].Addr.(*net.UDPAddr)
		require.Equal(t, e.port, addr.Port, "message %d source port", i)
		require.Equal(t, net.IP(e.v6Addr[:]), addr.IP, "message %d source address", i)
	}
	reads, datagrams, _ := recvmsgXCountersSnapshot()
	require.EqualValues(t, 1, reads-readsBefore, "one process-wide batch read")
	require.EqualValues(t, batchSize, datagrams-datagramsBefore)
	require.EqualValues(t, 1, conn.batchReads.Load(), "one per-conn batch read")
	require.EqualValues(t, batchSize, conn.batchDatagrams.Load())
}

// A partial batch surfaces exactly the delivered prefix; nothing is
// invented for the unfilled tail. Single-datagram fill is the boundary case
// of the same class.
func TestRecvmsgXReadBatchPartialAndSingleFill(t *testing.T) {
	for name, count := range map[string]int{"partial": 3, "single": 1} {
		t.Run(name, func(t *testing.T) {
			engagedRecvmsgXForTesting(t)
			entries := make([]fakeRecvEntry, count)
			for i := range entries {
				entries[i] = fakeRecvEntry{payload: []byte{byte('x' + i)}, port: 2000 + i, v6Addr: [16]byte{15: 1}}
			}
			installFakeRecvmsgX(t, func(msgs []msghdrX) (int, syscall.Errno) {
				return fillFakeEntries(msgs, entries)
			})
			conn := newRecvmsgXConn(&recordingBatchConn{}, &rawConnStub{})
			ms := makeReadMessages(batchSize)
			n, err := conn.ReadBatch(ms, 0)
			require.NoError(t, err)
			require.Equal(t, count, n)
			for i := range count {
				require.Equal(t, entries[i].payload, ms[i].Buffers[0][:ms[i].N])
				require.Zero(t, ms[i].NN, "no ancillary data was delivered for message %d", i)
			}
		})
	}
}

// ENOSYS latches the capability off for the process and the read completes
// through the fallback path: no datagram is surfaced from the suspect call,
// and the connection keeps reading.
func TestRecvmsgXENOSYSLatchFallsBack(t *testing.T) {
	engagedRecvmsgXForTesting(t)
	installFakeRecvmsgX(t, func([]msghdrX) (int, syscall.Errno) { return -1, syscall.ENOSYS })
	base := &recordingBatchConn{addr: &net.UDPAddr{IP: net.IPv6loopback, Port: 1}}
	conn := newRecvmsgXConn(base, &rawConnStub{})
	ms := makeReadMessages(batchSize)
	n, err := conn.ReadBatch(ms, 0)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, []int{1}, base.calls, "the fallback completes the read with a single message")
	require.True(t, recvmsgX.latched.Load(), "ENOSYS must latch the capability off")
	require.False(t, recvmsgXAvailable())
}

// A structurally invalid kernel result (ABI drift) latches the capability
// off and completes the read through the fallback path: the suspect batch
// is dropped, which UDP permits, so no datagram can surface twice or with
// another message's metadata.
func TestRecvmsgXStructuralLatch(t *testing.T) {
	valid := fakeRecvEntry{payload: []byte("ok"), port: 1, v6Addr: [16]byte{15: 1}}
	cases := map[string]func(msgs []msghdrX) (int, syscall.Errno){
		"over-count": func(msgs []msghdrX) (int, syscall.Errno) {
			fillFakeEntries(msgs, []fakeRecvEntry{valid})
			return len(msgs) + 1, 0
		},
		"zero-count": func(msgs []msghdrX) (int, syscall.Errno) { return 0, 0 },
		"negative-count": func(msgs []msghdrX) (int, syscall.Errno) {
			return -1, 0
		},
		"datalen-overflow": func(msgs []msghdrX) (int, syscall.Errno) {
			fillFakeEntries(msgs, []fakeRecvEntry{valid})
			msgs[0].Datalen = uint64(msgs[0].Iov.Len) + 1
			return 1, 0
		},
		"controllen-overflow": func(msgs []msghdrX) (int, syscall.Errno) {
			fillFakeEntries(msgs, []fakeRecvEntry{valid})
			msgs[0].Controllen = oobBufferSize + 1
			return 1, 0
		},
		"invalid-name": func(msgs []msghdrX) (int, syscall.Errno) {
			fillFakeEntries(msgs, []fakeRecvEntry{valid})
			msgs[0].Namelen = 7
			return 1, 0
		},
	}
	for name, fake := range cases {
		t.Run(name, func(t *testing.T) {
			engagedRecvmsgXForTesting(t)
			installFakeRecvmsgX(t, fake)
			base := &recordingBatchConn{addr: &net.UDPAddr{IP: net.IPv6loopback, Port: 1}}
			conn := newRecvmsgXConn(base, &rawConnStub{})
			ms := makeReadMessages(batchSize)
			n, err := conn.ReadBatch(ms, 0)
			require.NoError(t, err)
			require.Equal(t, 1, n)
			require.Equal(t, []int{1}, base.calls)
			require.True(t, recvmsgX.latched.Load(), "a structurally invalid result must latch")
			require.False(t, recvmsgXAvailable())
		})
	}
}

// An ordinary receive errno surfaces as a read error with the fallback
// path's semantics and must not latch the capability.
func TestRecvmsgXOrdinaryErrnoSurfaces(t *testing.T) {
	engagedRecvmsgXForTesting(t)
	installFakeRecvmsgX(t, func([]msghdrX) (int, syscall.Errno) { return -1, syscall.EBADF })
	conn := newRecvmsgXConn(&recordingBatchConn{}, &rawConnStub{})
	_, err := conn.ReadBatch(makeReadMessages(batchSize), 0)
	require.ErrorIs(t, err, syscall.EBADF)
	require.False(t, recvmsgX.latched.Load(), "an ordinary read error must not latch the capability")
}

// EAGAIN parks the read on the poller and retries; EINTR retries
// immediately. Neither surfaces to the caller nor latches.
func TestRecvmsgXEAGAINAndEINTRRetry(t *testing.T) {
	engagedRecvmsgXForTesting(t)
	entry := fakeRecvEntry{payload: []byte("later"), port: 9, v6Addr: [16]byte{15: 1}}
	var calls int
	installFakeRecvmsgX(t, func(msgs []msghdrX) (int, syscall.Errno) {
		calls++
		switch calls {
		case 1:
			return -1, syscall.EINTR
		case 2:
			return -1, syscall.EAGAIN
		}
		return fillFakeEntries(msgs, []fakeRecvEntry{entry})
	})
	conn := newRecvmsgXConn(&recordingBatchConn{}, &rawConnStub{})
	ms := makeReadMessages(batchSize)
	n, err := conn.ReadBatch(ms, 0)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, 3, calls)
	require.Equal(t, "later", string(ms[0].Buffers[0][:ms[0].N]))
	require.EqualValues(t, 1, conn.eagainWaits.Load())
	require.False(t, recvmsgX.latched.Load())
}
