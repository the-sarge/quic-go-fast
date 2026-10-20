package quic

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/testdata"

	"github.com/stretchr/testify/require"
)

// Consumers find the send thread QoS capability and its outcome with standard
// types only.
type sendThreadQoSV1 interface {
	ConfigureSendThreadQoSV1() error
}

type sendThreadQoSOutcomeV1 interface {
	SendThreadQoSV1() string
}

var _ sendThreadQoSOutcomeV1 = (*Conn)(nil)

// sendQoSExpectEnv, when set to raised or refused on an Apple silicon Mac,
// asserts what the launch context admits: raised from an ssh session or
// launchd, refused from a child of an app such as a terminal or an IDE.
const sendQoSExpectEnv = "QUIC_GO_TEST_SEND_QOS_EXPECT"

// fakeSendQoSThread records the order of thread operations. Its base priority
// is what the next read reports.
type fakeSendQoSThread struct {
	base       atomic.Int64
	raiseErr   error
	restoreErr error

	mu     sync.Mutex
	events []string
}

// fakeSendQoSPrior is the class the fake reports before each raise.
const fakeSendQoSPrior sendQoSClass = 0x15

func newFakeSendQoSThread(base int) *fakeSendQoSThread {
	f := &fakeSendQoSThread{}
	f.base.Store(int64(base))
	return f
}

func (f *fakeSendQoSThread) record(event string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, event)
}

func (f *fakeSendQoSThread) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.events...)
}

func (f *fakeSendQoSThread) lock()   { f.record("lock") }
func (f *fakeSendQoSThread) unlock() { f.record("unlock") }

func (f *fakeSendQoSThread) raise() (sendQoSClass, error) {
	f.record("raise")
	return fakeSendQoSPrior, f.raiseErr
}

func (f *fakeSendQoSThread) basePriority() (int, error) {
	f.record("read")
	return int(f.base.Load()), nil
}

func (f *fakeSendQoSThread) restore(prior sendQoSClass) error {
	f.record("restore")
	if prior != fakeSendQoSPrior {
		return errors.New("restored a class that was not recorded")
	}
	return f.restoreErr
}

func newTestSendThreadQoS(thread sendQoSThread) *sendThreadQoS {
	return &sendThreadQoS{thread: thread, now: monotime.Now}
}

// runQoSQueue starts a send queue bound to qos and returns it with its exit.
func runQoSQueue(conn sendConn, qos *sendThreadQoS) (*sendQueue, <-chan error) {
	q := newSendQueue(conn, nil).(*sendQueue)
	q.qos = qos
	done := make(chan error, 1)
	go func() { done <- q.Run() }()
	return q, done
}

func sendQoSPacket(t *testing.T, q *sendQueue) {
	t.Helper()
	q.Send(getPacketWithContents([]byte("foobar")), 0, protocol.ECNNon, sendMetadata{})
	synctest.Wait()
}

var (
	qosRaiseRefused = []string{"lock", "raise", "read", "restore", "unlock"}
	qosRaised       = []string{"lock", "raise", "read"}
	qosReleased     = []string{"restore", "unlock"}
)

// A launch context that squashes the class refuses the raise at start: the
// worker restores the class before unlocking and sends as before.
func TestSendQoSRefusedAtStart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		thread := newFakeSendQoSThread(31)
		qos := newTestSendThreadQoS(thread)
		conn := &fakeBatchSendConn{available: func() bool { return false }}
		q, done := runQoSQueue(conn, qos)
		sendQoSPacket(t, q)
		require.Equal(t, qosRaiseRefused, thread.recorded())
		require.Equal(t, "refused", qos.outcome())
		q.Close()
		require.NoError(t, <-done)
		require.Equal(t, qosRaiseRefused, thread.recorded(), "a refused worker holds nothing at exit")
		require.Len(t, conn.writes, 1)
	})
}

// A failed raise leaves the class unchanged, so the worker only unlocks.
func TestSendQoSRaiseError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		thread := newFakeSendQoSThread(37)
		thread.raiseErr = errors.New("EINVAL")
		qos := newTestSendThreadQoS(thread)
		q, done := runQoSQueue(&fakeBatchSendConn{available: func() bool { return false }}, qos)
		sendQoSPacket(t, q)
		require.Equal(t, []string{"lock", "raise", "unlock"}, thread.recorded())
		require.Equal(t, "refused", qos.outcome())
		q.Close()
		require.NoError(t, <-done)
	})
}

// While raised, the worker re-reads its base priority at most once per
// interval, and only after sending. A drop stands it down for good: it
// restores, unlocks and keeps sending.
func TestSendQoSStandsDownMidRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		thread := newFakeSendQoSThread(37)
		qos := newTestSendThreadQoS(thread)
		conn := &fakeBatchSendConn{available: func() bool { return false }}
		q, done := runQoSQueue(conn, qos)
		sendQoSPacket(t, q)
		require.Equal(t, qosRaised, thread.recorded())
		require.Equal(t, "raised", qos.outcome())

		time.Sleep(sendQoSCheckInterval / 2)
		sendQoSPacket(t, q)
		require.Equal(t, qosRaised, thread.recorded(), "no read before the interval passes")

		time.Sleep(sendQoSCheckInterval)
		require.Equal(t, qosRaised, thread.recorded(), "an idle worker makes no reads")
		sendQoSPacket(t, q)
		require.Equal(t, append(qosRaised, "read"), thread.recorded(), "a held raise is re-read")
		require.Equal(t, "raised", qos.outcome())

		thread.base.Store(31)
		time.Sleep(sendQoSCheckInterval)
		sendQoSPacket(t, q)
		standDown := append(append(qosRaised, "read", "read"), qosReleased...)
		require.Equal(t, standDown, thread.recorded())
		require.Equal(t, "stood_down", qos.outcome())

		time.Sleep(2 * sendQoSCheckInterval)
		sendQoSPacket(t, q)
		q.Close()
		require.NoError(t, <-done)
		require.Equal(t, standDown, thread.recorded(), "a stood-down worker makes no further calls")
		require.Len(t, conn.writes, 5)
	})
}

// Every exit of a raised worker restores the class before unlocking: a normal
// close, a per-packet send error and a batched send error.
func TestSendQoSRestoresOnExit(t *testing.T) {
	errSend := errors.New("send failed")
	for _, tc := range []struct {
		name    string
		conn    func() *fakeBatchSendConn
		send    int
		wantErr error
	}{
		{name: "close", conn: func() *fakeBatchSendConn {
			return &fakeBatchSendConn{available: func() bool { return false }}
		}},
		{name: "per-packet error", send: 1, wantErr: errSend, conn: func() *fakeBatchSendConn {
			return &fakeBatchSendConn{
				available: func() bool { return false },
				writeErr:  func(int, []byte) error { return errSend },
			}
		}},
		{name: "batched error", send: 2, wantErr: errSend, conn: func() *fakeBatchSendConn {
			return &fakeBatchSendConn{accept: func(int, [][]byte) (int, error) { return 0, errSend }}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				thread := newFakeSendQoSThread(37)
				qos := newTestSendThreadQoS(thread)
				q := newSendQueue(tc.conn(), nil).(*sendQueue)
				q.qos = qos
				for range tc.send {
					q.Send(getPacketWithContents([]byte("foobar")), 0, protocol.ECNNon, sendMetadata{})
				}
				done := make(chan error, 1)
				go func() { done <- q.Run() }()
				synctest.Wait()
				if tc.wantErr == nil {
					q.Close()
					require.NoError(t, <-done)
				} else {
					require.ErrorIs(t, <-done, tc.wantErr)
					q.Close()
				}
				require.Equal(t, append(qosRaised, qosReleased...), thread.recorded())
				require.Equal(t, "raised", qos.outcome(), "a normal exit is not a stand-down")
			})
		})
	}
}

// A class that cannot be restored keeps the goroutine locked, so the runtime
// ends the thread with the goroutine instead of reusing it.
func TestSendQoSFailedRestoreKeepsLock(t *testing.T) {
	for _, tc := range []struct {
		name string
		base int
		want []string
	}{
		{name: "refused at start", base: 31, want: []string{"lock", "raise", "read", "restore"}},
		{name: "exit", base: 37, want: []string{"lock", "raise", "read", "restore"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				thread := newFakeSendQoSThread(tc.base)
				thread.restoreErr = errors.New("EINVAL")
				q, done := runQoSQueue(&fakeBatchSendConn{available: func() bool { return false }}, newTestSendThreadQoS(thread))
				synctest.Wait()
				q.Close()
				require.NoError(t, <-done)
				require.Equal(t, tc.want, thread.recorded())
			})
		})
	}
}

// The outcome belongs to the connection: a later worker, such as the one a path
// change starts, raises again only while every earlier attempt held.
func TestSendQoSLatchSurvivesNewWorkers(t *testing.T) {
	for _, tc := range []struct {
		name    string
		first   int // base priority during the first worker
		drop    bool
		outcome string
		second  []string
	}{
		{name: "after refusal", first: 31, outcome: "refused"},
		{name: "after stand-down", first: 37, drop: true, outcome: "stood_down"},
		{name: "after a held raise", first: 37, outcome: "raised", second: qosRaised},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				thread := newFakeSendQoSThread(tc.first)
				qos := newTestSendThreadQoS(thread)
				q, done := runQoSQueue(&fakeBatchSendConn{available: func() bool { return false }}, qos)
				sendQoSPacket(t, q)
				if tc.drop {
					thread.base.Store(31)
					time.Sleep(sendQoSCheckInterval)
					sendQoSPacket(t, q)
				}
				q.Close()
				require.NoError(t, <-done)
				require.Equal(t, tc.outcome, qos.outcome())

				before := len(thread.recorded())
				q, done = runQoSQueue(&fakeBatchSendConn{available: func() bool { return false }}, qos)
				sendQoSPacket(t, q)
				if tc.second == nil {
					require.Empty(t, thread.recorded()[before:])
				} else {
					require.Equal(t, tc.second, thread.recorded()[before:])
				}
				q.Close()
				require.NoError(t, <-done)
				require.Equal(t, tc.outcome, qos.outcome())
			})
		})
	}
}

// A path change binds the connection's controller to the queue it creates.
func TestSendQoSBindsReplacementQueue(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var conn sendConn = &fakeBatchSendConn{}
		initial := newSendQueue(conn, nil)
		e := packetEmission{queue: initial, conn: &conn}
		qos := newTestSendThreadQoS(newFakeSendQoSThread(31))
		e.bindSendQoS(qos)
		require.Same(t, qos, initial.(*sendQueue).qos)
		go initial.Run()
		replacement := e.replacePath(&fakeBatchSendConn{}, nil)
		require.Same(t, qos, replacement.(*sendQueue).qos)
		go replacement.Run()
		synctest.Wait()
		replacement.Close()
	})
}

// The outcome is safe to read while path changes replace the queue, and a
// stand-down stays final across them.
func TestSendQoSOutcomeDuringPathChanges(t *testing.T) {
	var conn sendConn = &fakeBatchSendConn{available: func() bool { return false }}
	c := &Conn{}
	c.emission = packetEmission{queue: newSendQueue(conn, nil), conn: &conn}
	thread := newFakeSendQoSThread(31)
	restore := sendQoSThreadSource
	t.Cleanup(func() { sendQoSThreadSource = restore })
	sendQoSThreadSource = func() sendQoSThread { return thread }
	c.installSendThreadQoS(true)
	qos := c.emission.sendQoS

	stop := make(chan struct{})
	polled := make(chan struct{})
	go func() {
		defer close(polled)
		for {
			select {
			case <-stop:
				return
			default:
				_ = c.SendThreadQoSV1()
			}
		}
	}()
	queue := c.emission.queue
	go queue.Run()
	for range 50 {
		queue = c.emission.replacePath(&fakeBatchSendConn{available: func() bool { return false }}, nil)
		require.Same(t, qos, queue.(*sendQueue).qos)
		go queue.Run()
	}
	queue.Close()
	close(stop)
	<-polled
	require.Equal(t, "refused", c.SendThreadQoSV1())
	require.Equal(t, qosRaiseRefused, thread.recorded(), "only the first worker tried")
}

// Each outcome reads as its documented string; a connection without a
// controller reads "off".
func TestSendQoSOutcomeStrings(t *testing.T) {
	var off *sendThreadQoS
	require.Equal(t, "off", off.outcome())
	qos := newTestSendThreadQoS(newFakeSendQoSThread(31))
	for state, want := range map[uint32]string{
		sendQoSPending:   "pending",
		sendQoSRaised:    "raised",
		sendQoSRefused:   "refused",
		sendQoSStoodDown: "stood_down",
	} {
		qos.state.Store(state)
		require.Equal(t, want, qos.outcome())
	}
	var c Conn
	require.Equal(t, "off", c.SendThreadQoSV1())
}

// Installation creates a controller only when the transport asked for one, the
// platform has the hook and the kill switch is unset. Without one, a worker
// makes no thread calls.
func TestSendQoSInstall(t *testing.T) {
	thread := newFakeSendQoSThread(37)
	restore := sendQoSThreadSource
	t.Cleanup(func() { sendQoSThreadSource = restore })

	install := func(enabled bool) *Conn {
		c := &Conn{}
		var conn sendConn = &fakeBatchSendConn{available: func() bool { return false }}
		c.emission = packetEmission{queue: newSendQueue(conn, nil), conn: &conn}
		c.installSendThreadQoS(enabled)
		return c
	}

	sendQoSThreadSource = func() sendQoSThread { return thread }
	require.Equal(t, "off", install(false).SendThreadQoSV1(), "not configured")
	c := install(true)
	require.Equal(t, "pending", c.SendThreadQoSV1())
	require.Same(t, c.emission.sendQoS, c.emission.queue.(*sendQueue).qos)

	t.Setenv(sendQoSDisableEnv, "true")
	require.Equal(t, "off", install(true).SendThreadQoSV1(), "kill switch")
	t.Setenv(sendQoSDisableEnv, "false")
	require.Equal(t, "pending", install(true).SendThreadQoSV1())

	sendQoSThreadSource = func() sendQoSThread { return nil }
	require.Equal(t, "off", install(true).SendThreadQoSV1(), "platform without the hook")

	synctest.Test(t, func(t *testing.T) {
		c := install(false)
		q := c.emission.queue
		go q.Run()
		q.Send(getPacketWithContents([]byte("foobar")), 0, protocol.ECNNon, sendMetadata{})
		synctest.Wait()
		q.Close()
	})
	require.Empty(t, thread.recorded())
}

func configureSendThreadQoS(t *testing.T, tr *Transport) error {
	t.Helper()
	api, ok := any(tr).(sendThreadQoSV1)
	require.True(t, ok, "transport must expose a standard-type send thread QoS capability")
	return api.ConfigureSendThreadQoSV1()
}

// The capability is configured once, before initialization.
func TestSendQoSContract(t *testing.T) {
	tr := &Transport{Conn: newUDPConnLocalhost(t)}
	t.Cleanup(func() { tr.Close() })
	require.NoError(t, configureSendThreadQoS(t, tr))
	require.Error(t, configureSendThreadQoS(t, tr), "configuration happens once")

	initialized := &Transport{Conn: newUDPConnLocalhost(t)}
	t.Cleanup(func() { initialized.Close() })
	_, err := initialized.Listen(testdata.GetTLSConfig(), nil)
	require.NoError(t, err)
	require.Error(t, configureSendThreadQoS(t, initialized), "configuration precedes initialization")
}

// Connections a configured transport dials or accepts report the platform's
// outcome once their send workers have started; others report "off".
func TestSendQoSReachesConnections(t *testing.T) {
	for _, tc := range []struct {
		name       string
		configured bool
		disabled   bool
	}{
		{name: "configured", configured: true},
		{name: "kill switch", configured: true, disabled: true},
		{name: "default"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.disabled {
				t.Setenv(sendQoSDisableEnv, "1")
			}
			serverTransport := &Transport{Conn: newUDPConnLocalhost(t)}
			t.Cleanup(func() { serverTransport.Close() })
			clientTransport := &Transport{Conn: newUDPConnLocalhost(t)}
			t.Cleanup(func() { clientTransport.Close() })
			if tc.configured {
				require.NoError(t, configureSendThreadQoS(t, serverTransport))
				require.NoError(t, configureSendThreadQoS(t, clientTransport))
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			ln, err := serverTransport.Listen(testdata.GetTLSConfig(), nil)
			require.NoError(t, err)
			client, err := clientTransport.Dial(ctx, ln.Addr(), candidateClientTLS(), nil)
			require.NoError(t, err)
			server, err := ln.Accept(ctx)
			require.NoError(t, err)
			<-server.HandshakeComplete()

			want := []string{"off"}
			if tc.configured && !tc.disabled && sendQoSThreadSource() != nil {
				want = []string{"raised", "refused"}
				if expect := os.Getenv(sendQoSExpectEnv); expect != "" {
					want = []string{expect}
				}
			}
			for _, conn := range []*Conn{client, server} {
				require.Contains(t, want, conn.SendThreadQoSV1())
			}
			require.NoError(t, client.CloseWithError(0, ""))
			require.NoError(t, server.CloseWithError(0, ""))
		})
	}
}
