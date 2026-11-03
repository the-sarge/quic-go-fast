//go:build darwin || linux || freebsd

package quic

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/netip"
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/testdata"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

var nativeShutdownAddress = &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}

const nativeShutdownALPN = "native-shutdown"

func nativeServerTLS() *tls.Config {
	conf := testdata.GetTLSConfig()
	conf.NextProtos = []string{nativeShutdownALPN}
	return conf
}

func nativeClientTLS() *tls.Config {
	return &tls.Config{RootCAs: testdata.GetRootCA(), ServerName: "localhost", NextProtos: []string{nativeShutdownALPN}}
}

func TestNativeIOStopAndJoin(t *testing.T) {
	require.ErrorIs(t, (&Transport{}).waitNativeIOStopped(t.Context()), ErrMultipathNotConfiguredV1)
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		var writes, clears int
		endpoint := startNativeEndpoint(endpointTestNative{
			deadline: func(deadline time.Time) error {
				if deadline.IsZero() {
					clears++
				}
				return nil
			},
			send: func(data []byte) (int, error) {
				writes++
				<-release
				return len(data), nil
			},
		}, 100*time.Millisecond, nil)
		transport := &Transport{responses: newEndpointResponses(endpoint)}
		require.ErrorIs(t, transport.waitNativeIOStopped(t.Context()), ErrMultipathNotStoppingV1)
		require.NoError(t, transport.responses.send(nil, nil, []byte("current"), nativeShutdownAddress, packetInfo{}))
		synctest.Wait()

		// Close returns while the native call holds its token.
		transport.close(nil)
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		require.ErrorIs(t, transport.waitNativeIOStopped(ctx), context.DeadlineExceeded, "the writer still holds the socket")

		// A wait that gave up took no authority, and the overdue token faulted the
		// endpoint without ending its borrow.
		record, faulted := endpoint.faultObservation()
		require.True(t, faulted)
		require.Equal(t, endpointFaultOverdue, record.cause)
		select {
		case <-endpoint.done:
			t.Fatal("the fault must not stand in for the native return")
		default:
		}

		// The late return ends the borrow and clears the deadline before the join.
		close(release)
		require.NoError(t, transport.waitNativeIOStopped(t.Context()))
		require.Equal(t, 1, writes)
		require.Equal(t, 1, clears)
		select {
		case <-endpoint.writer:
		default:
			t.Fatal("join published before the writer exited")
		}

		// Nothing reopens, and the join leaves the fault record readable.
		_, err := endpoint.attach(7)
		require.ErrorIs(t, err, net.ErrClosed)
		require.ErrorIs(t, transport.responses.send(nil, nil, []byte("late"), nativeShutdownAddress, packetInfo{}), net.ErrClosed)
		record, faulted = endpoint.faultObservation()
		require.True(t, faulted)
		require.Equal(t, endpointFaultOverdue, record.cause)
		require.Equal(t, 1, writes)
	})
}

func TestNativeIOGrantValidation(t *testing.T) {
	socket := func(t *testing.T) *net.UDPConn {
		t.Helper()
		conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		require.NoError(t, err)
		t.Cleanup(func() { conn.Close() })
		return conn
	}
	t.Run("budget", func(t *testing.T) {
		tr := &Transport{Conn: socket(t)}
		for _, budget := range []time.Duration{0, time.Millisecond - 1, time.Second + 1} {
			require.ErrorIs(t, tr.configureNativeIO(budget), ErrMultipathInvalidConfigV1)
		}
		require.Nil(t, tr.responses)
	})
	t.Run("socket domain", func(t *testing.T) {
		connected, err := net.DialUDP("udp4", nil, socket(t).LocalAddr().(*net.UDPAddr))
		require.NoError(t, err)
		defer connected.Close()
		require.ErrorIs(t, (&Transport{Conn: connected}).configureNativeIO(time.Second), ErrMultipathUnsupportedV1)
		wrapped := struct{ net.PacketConn }{socket(t)}
		require.ErrorIs(t, (&Transport{Conn: wrapped}).configureNativeIO(time.Second), ErrMultipathUnsupportedV1)
		require.ErrorIs(t, (&Transport{}).configureNativeIO(time.Second), ErrMultipathUnsupportedV1)
	})
	t.Run("excluded configurations", func(t *testing.T) {
		before := &Transport{Conn: socket(t)}
		require.NoError(t, before.ConfigureSendThreadQoSV1())
		require.ErrorIs(t, before.configureNativeIO(time.Second), ErrMultipathUnsupportedV1)
		require.Nil(t, before.responses)

		// A later conflicting configuration fails before any attachment.
		after := &Transport{Conn: socket(t)}
		require.NoError(t, after.configureNativeIO(time.Second))
		defer after.Close()
		require.NoError(t, after.ConfigureSendThreadQoSV1())
		require.ErrorIs(t, after.init(false), ErrMultipathUnsupportedV1)
	})
	t.Run("replaced socket", func(t *testing.T) {
		tr := &Transport{Conn: socket(t)}
		require.NoError(t, tr.configureNativeIO(time.Second))
		defer tr.Close()
		tr.Conn = socket(t)
		require.ErrorIs(t, tr.init(false), ErrMultipathUnsupportedV1)
	})
	t.Run("late", func(t *testing.T) {
		tr := &Transport{Conn: socket(t)}
		require.NoError(t, tr.init(false))
		defer tr.Close()
		require.ErrorIs(t, tr.configureNativeIO(time.Second), ErrMultipathInvalidConfigV1)
	})
	t.Run("grant returns exactly once", func(t *testing.T) {
		conn := socket(t)
		first := &Transport{Conn: conn}
		require.NoError(t, first.configureNativeIO(time.Second))
		require.ErrorIs(t, first.configureNativeIO(time.Second), ErrMultipathInvalidConfigV1, "repeated on one transport")
		second := &Transport{Conn: conn}
		require.ErrorIs(t, second.configureNativeIO(time.Second), ErrMultipathInvalidConfigV1, "the socket's grant is still held")

		require.NoError(t, first.Close())
		require.NoError(t, first.waitNativeIOStopped(t.Context()))
		require.ErrorIs(t, second.configureNativeIO(0), ErrMultipathInvalidConfigV1)
		require.NoError(t, second.configureNativeIO(time.Second), "the join returned the socket's grant")
		defer second.Close()
	})
}

// nativeCloseConnection runs a real connection loop over a scripted socket and
// captures the close bytes handed to the closed-state owner.
func nativeCloseConnection(t *testing.T, tc *testConnection, e *nativeEndpoint) (retained func() []byte, stopped <-chan error) {
	t.Helper()
	require.NoError(t, tc.conn.useNativeOrdinary(e, netip.MustParseAddrPort("1.2.3.4:4321"), packetInfo{}))
	tc.conn.idleTimeout = time.Hour
	sealing := tc.conn.emission.packer.cryptoSetup.(*MockSealingManager)
	sealing.EXPECT().GetInitialSealer().Return(nil, handshake.ErrKeysDropped).AnyTimes()
	sealing.EXPECT().GetHandshakeSealer().Return(nil, handshake.ErrKeysDropped).AnyTimes()
	var closePacket []byte
	tc.connRunner.EXPECT().Remove(gomock.Any()).AnyTimes()
	tc.connRunner.EXPECT().ReplaceWithClosed(gomock.Any(), gomock.Any(), gomock.Any()).Do(func(_ []protocol.ConnectionID, packet []byte, _ time.Duration) {
		closePacket = packet
	}).AnyTimes()
	done := make(chan error, 1)
	go func() { done <- tc.conn.run() }()
	return func() []byte { return closePacket }, done
}

func TestNativeLiveCloseDuringCurrentWrite(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		tc := newEmissionTestConnection(t, false)
		c := tc.conn
		release := make(chan struct{})
		var output [][]byte
		e := startNativeEndpoint(endpointTestNative{send: func(data []byte) (int, error) {
			if len(output) == 0 {
				<-release
			}
			output = append(output, bytes.Clone(data))
			return len(data), nil
		}}, 100*time.Millisecond, nil)
		defer func() { <-e.stop() }()
		require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("current datagram")}))
		retained, stopped := nativeCloseConnection(t, tc, e)
		c.scheduleSending()
		synctest.Wait()
		require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("queued datagram")}))
		c.scheduleSending()
		synctest.Wait()

		connIDLen := c.connIDManager.Get().Len()

		// Logical destruction finishes while the native call still holds its token.
		require.NoError(t, c.CloseWithError(42, "live close"))
		var closeErr *ApplicationError
		require.ErrorAs(t, <-stopped, &closeErr)
		require.Empty(t, output)
		require.Contains(t, string(retained()), "live close", "the closed handler keeps its own copy")

		close(release)
		synctest.Wait()
		require.Len(t, output, 3, "queued packets drain ahead of the close, as on the baseline route")
		require.Contains(t, string(output[0]), "current datagram")
		require.Contains(t, string(output[1]), "queued datagram")
		require.Contains(t, string(output[2]), "live close")
		require.Equal(t, retained(), output[2])

		// Packing after drain kept the packet-number space: recovery closes later.
		var last protocol.PacketNumber = -1
		for _, datagram := range output {
			_, pn, _, _, err := wire.ParseShortHeader(datagram, connIDLen)
			require.NoError(t, err)
			require.Greater(t, pn, last, "a reused number would reuse an AEAD nonce")
			last = pn
		}
		requireNativeParticipantsRetired(t, e)
	})
}

func requireNativeParticipantsRetired(t *testing.T, e *nativeEndpoint) {
	t.Helper()
	synctest.Wait()
	e.do(func(s *endpointState) {
		for _, p := range s.participants[:endpointParticipants] {
			require.Nil(t, p, "a resolved close obligation retires its participant")
		}
	})
}

type nativeShutdownRecorder struct {
	onClose func()
}

func (r *nativeShutdownRecorder) RecordEvent(qlogwriter.Event) {}

func (r *nativeShutdownRecorder) Close() error {
	r.onClose()
	return nil
}

// Closing local recovery can record, and a recorder must not be used after it
// closes, so recovery has to be closed by the time the recorder is.
func TestNativeCloseRecoveryBeforeRecorder(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		tc := newEmissionTestConnection(t, false)
		c := tc.conn
		var modeAtRecorderClose ackhandler.SendMode
		closed := false
		c.qlogger = &nativeShutdownRecorder{onClose: func() {
			closed = true
			modeAtRecorderClose = c.sentPacketHandler.SendMode(monotime.Now())
		}}
		e := startNativeEndpoint(endpointTestNative{}, 100*time.Millisecond, nil)
		defer func() { <-e.stop() }()
		_, stopped := nativeCloseConnection(t, tc, e)
		synctest.Wait()
		require.NoError(t, c.CloseWithError(1, "bye"))
		<-stopped
		require.True(t, closed)
		require.Equal(t, ackhandler.SendNone, modeAtRecorderClose)
	})
}

func TestNativeCloseObligation(t *testing.T) {
	address := netip.MustParseAddrPort("127.0.0.1:1234")
	closeRequest := func(id uint64) (endpointRequest, *packetBuffer) {
		buffer := endpointTestBuffer("close")
		return endpointRequest{identity: endpointIdentity{connection: id}, destination: address, buffers: []*packetBuffer{buffer}}, buffer
	}
	// occupy fills the connection-control class behind one blocked native call.
	occupy := func(t *testing.T, e *nativeEndpoint) {
		t.Helper()
		for id := uint64(100); id < 102; id++ {
			p, err := e.attach(id)
			require.NoError(t, err)
			for range 4 {
				require.NoError(t, e.offer(p, 2, endpointTestRequest(id, "occupy")))
			}
		}
		synctest.Wait()
	}
	t.Run("waits for capacity", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			release := make(chan struct{})
			var written []string
			e := startNativeEndpoint(endpointTestNative{send: func(data []byte) (int, error) {
				<-release
				written = append(written, string(data))
				return len(data), nil
			}}, time.Second, nil)
			occupy(t, e)
			p, err := e.attach(1)
			require.NoError(t, err)
			e.drain(p)
			request, _ := closeRequest(1)
			require.NoError(t, e.depositClose(p, request))
			fence := e.detach(p)
			synctest.Wait()
			select {
			case <-fence:
				t.Fatal("detach must wait for the close obligation")
			default:
			}
			close(release)
			<-fence
			<-e.stop() // other participants' requests can still be in the writer
			require.Contains(t, written, "close")
		})
	})
	t.Run("expires once", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			e := startNativeEndpoint(endpointTestNative{}, 10*time.Millisecond, nil)
			defer func() { <-e.stop() }()
			// Unconsumed results hold every record, and nothing else has a deadline.
			for id := uint64(100); id < 164; id++ {
				p, err := e.attach(id)
				require.NoError(t, err)
				for range 4 {
					require.NoError(t, e.offer(p, 0, endpointTestRequest(id, "unconsumed")))
					synctest.Wait()
				}
			}
			p, err := e.attach(1)
			require.NoError(t, err)
			e.drain(p)
			request, buffer := closeRequest(1)
			require.NoError(t, e.depositClose(p, request))
			fence := e.detach(p)
			<-fence // only the deposit deadline in control's timer can end this wait
			require.Zero(t, buffer.refCount, "expiry releases the obligation's storage")
		})
	})
	t.Run("seal releases", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			release := make(chan struct{})
			e := startNativeEndpoint(endpointTestNative{send: func(data []byte) (int, error) {
				<-release
				return len(data), nil
			}}, time.Second, nil)
			occupy(t, e)
			p, err := e.attach(1)
			require.NoError(t, err)
			e.drain(p)
			request, buffer := closeRequest(1)
			require.NoError(t, e.depositClose(p, request))
			e.seal()
			synctest.Wait()
			require.Zero(t, buffer.refCount, "sealing owes no close")
			close(release)
			<-e.stop()

			refused, unreleased := closeRequest(1)
			require.ErrorIs(t, e.depositClose(p, refused), net.ErrClosed, "refusal leaves the buffer with the caller")
			require.EqualValues(t, 1, unreleased.refCount)
			unreleased.Release()
		})
	})
}

func TestNativeTransportCloseKeepsCause(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		tc := newEmissionTestConnection(t, false)
		c := tc.conn
		e := startNativeEndpoint(endpointTestNative{}, 100*time.Millisecond, nil)
		_, stopped := nativeCloseConnection(t, tc, e)
		synctest.Wait()

		// A sealed endpoint refuses the busy connection before the transport's
		// destroy reaches it, as server shutdown and handshakes can delay it.
		e.seal()
		synctest.Wait()
		require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("refused")}))
		c.scheduleSending()
		synctest.Wait()
		select {
		case err := <-stopped:
			t.Fatalf("a sealed refusal ended the connection first: %v", err)
		default:
		}
		closed := &errTransportClosed{}
		c.destroy(closed)
		require.ErrorIs(t, <-stopped, ErrTransportClosed)
		require.ErrorIs(t, context.Cause(c.Context()), ErrTransportClosed)
	})
}

func TestNativeAbortReleasesParticipant(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tc := newEmissionTestConnection(t, false)
		e := startNativeEndpoint(endpointTestNative{}, 100*time.Millisecond, nil)
		defer func() { <-e.stop() }()
		require.NoError(t, tc.conn.useNativeOrdinary(e, netip.MustParseAddrPort("1.2.3.4:4321"), packetInfo{}))
		participant := tc.conn.emission.local.(*nativeOrdinarySender).participant
		tc.conn.abortUnstarted(errors.New("lost registration"))
		synctest.Wait()
		select {
		case <-participant.retired:
		default:
			t.Fatal("an aborted connection must give back its participant slot")
		}
		requireNativeParticipantsRetired(t, e)
	})
}

func TestNativeRouteExclusions(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		e := startNativeEndpoint(endpointTestNative{}, 100*time.Millisecond, nil)
		defer func() { <-e.stop() }()
		granted := &Transport{responses: newEndpointResponses(e)}

		// An ungranted origin would write around the endpoint, so no join could
		// be trusted.
		origin := newEmissionTestConnection(t, false)
		require.ErrorIs(t, granted.checkPathPolicy(origin.sendConn), ErrMultipathUnsupportedV1)

		// A route origin can't write to a socket outside the route.
		tc := newEmissionTestConnection(t, false)
		require.NoError(t, tc.conn.useNativeOrdinary(e, netip.MustParseAddrPort("1.2.3.4:4321"), packetInfo{}))
		defer tc.conn.emission.local.Close()
		require.ErrorIs(t, (&Transport{}).checkPathPolicy(tc.conn.emission.activeConn()), ErrMultipathUnsupportedV1)
		require.NoError(t, granted.checkPathPolicy(tc.conn.emission.activeConn()))
		require.NoError(t, (&Transport{}).checkPathPolicy(origin.sendConn))

		// Terminal receipts stay with R9, so credit can't land on the route.
		_, err := tc.conn.enableMultipathCredit(2, 1, 4)
		require.Error(t, err)
		require.Nil(t, tc.conn.pathCredit)
	})
}

// A path switch moves the connection's primary participant to the target's
// endpoint. Results still owed by the old endpoint have to wake and reconcile.
func TestNativePathSwitch(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		tc := newEmissionTestConnection(t, false)
		c := tc.conn
		oldRelease := make(chan struct{})
		var oldWrites, newWrites [][]byte
		oldEndpoint := startNativeEndpoint(endpointTestNative{send: func(data []byte) (int, error) {
			<-oldRelease
			oldWrites = append(oldWrites, bytes.Clone(data))
			return 0, &os.SyscallError{Syscall: "sendmsg", Err: syscall.ECONNREFUSED}
		}}, time.Second, nil)
		defer func() { <-oldEndpoint.stop() }()
		newEndpoint := startNativeEndpoint(endpointTestNative{send: func(data []byte) (int, error) {
			newWrites = append(newWrites, bytes.Clone(data))
			return len(data), nil
		}}, time.Second, nil)
		defer func() { <-newEndpoint.stop() }()
		target := &Transport{responses: newEndpointResponses(newEndpoint)}
		require.NoError(t, c.useNativeOrdinary(oldEndpoint, netip.MustParseAddrPort("1.2.3.4:4321"), packetInfo{addr: netip.MustParseAddr("127.0.0.2")}))
		queue := c.emission.local.(*nativeOrdinarySender)
		defer queue.Close()
		oldParticipant, oldWriteState := queue.participant, queue.writeState

		// One request is current on the old endpoint and one queued behind it.
		for _, payload := range []string{"current", "queued"} {
			require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte(payload)}))
			require.True(t, c.emission.send(monotime.Now(), true).progress)
			synctest.Wait()
		}

		next := NewMockSendConn(gomock.NewController(t))
		next.EXPECT().capabilities().Return(connCapabilities{}).AnyTimes()
		require.NotNil(t, c.emission.replaceLocalPath(target, next))
		require.Same(t, newEndpoint, queue.endpoint)
		require.NotSame(t, oldParticipant, queue.participant)
		require.NotSame(t, oldWriteState, queue.writeState, "first-send and GSO state belong to the path")
		require.Empty(t, queue.oob, "the old path's source metadata doesn't apply")
		require.Contains(t, queue.targets, oldParticipant)
		require.Len(t, queue.targets, 1)

		// New work goes to the target while the old call is still native.
		require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("after switch")}))
		require.True(t, c.emission.send(monotime.Now(), true).progress)
		synctest.Wait()
		require.Len(t, newWrites, 1)
		require.Contains(t, string(newWrites[0]), "after switch")

		// Drain the wakeup left by the target's own completion, then consume it.
		<-queue.completed()
		require.NoError(t, queue.consume())

		// The old endpoint's late result wakes the connection on the shared channel,
		// and its token settles on this pass. A destination error on a path the
		// connection left is no reason to end it.
		close(oldRelease)
		synctest.Wait()
		select {
		case <-queue.completed():
		default:
			t.Fatal("a result on the old endpoint must wake the connection")
		}
		require.NoError(t, queue.consume())
		require.Len(t, oldWrites, 1, "the old generation's queued request never reaches the socket")
		for _, record := range queue.records {
			require.Zero(t, record.admission.serial, "every record reconciled")
		}

		// A late old-endpoint fault doesn't end a connection that left it.
		oldEndpoint.do(func(s *endpointState) { s.fail(endpointFaultNative, net.ErrClosed) })
		require.NoError(t, queue.consume())
	})
}

// take on one endpoint must not swallow another endpoint's signal on the shared
// wake channel. Only the connection's select receives from it.
func TestNativeSharedWakeSurvivesOtherEndpoint(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		first := startNativeEndpoint(endpointTestNative{}, time.Second, nil)
		defer func() { <-first.stop() }()
		second := startNativeEndpoint(endpointTestNative{}, time.Second, nil)
		defer func() { <-second.stop() }()
		a, err := first.attach(1)
		require.NoError(t, err)
		b, err := second.attachShared(1, a.completed)
		require.NoError(t, err)

		// The signal is pending for b's result when the consumer passes over a.
		require.NoError(t, second.offer(b, 0, endpointTestRequest(1, "second")))
		synctest.Wait()
		_, ok := first.take(a)
		require.False(t, ok)
		require.NoError(t, first.offer(a, 0, endpointTestRequest(1, "first")))
		synctest.Wait()
		_, ok = first.take(a)
		require.True(t, ok)
		select {
		case <-a.completed:
		default:
			t.Fatal("a pass over one endpoint swallowed the other's wakeup")
		}
		_, ok = second.take(b)
		require.True(t, ok)
	})
}

func TestNativeSealStopsCapturedProducers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		writes := 0
		e := startNativeEndpoint(endpointTestNative{send: func(data []byte) (int, error) {
			writes++
			<-release
			return len(data), nil
		}}, time.Second, nil)
		transport := &Transport{responses: newEndpointResponses(e)}
		p, err := e.attach(1)
		require.NoError(t, err)

		current := endpointTestRequest(1, "current")
		require.NoError(t, e.offer(p, 0, current))
		synctest.Wait()
		queued := endpointTestRequest(1, "queued")
		require.NoError(t, e.offer(p, 0, queued))
		prepared, err := e.prepare(p, 0, 1452)
		require.NoError(t, err)
		child := transport.responses.child()

		transport.close(nil)
		synctest.Wait()

		// Queued work resolves as unsubmitted, once, while the current borrow stays.
		require.Zero(t, queued.buffers[0].refCount)
		require.EqualValues(t, 1, current.buffers[0].refCount)

		// Everything captured before the seal is refused, and refusal keeps
		// ownership with the producer.
		late := endpointTestRequest(1, "late")
		require.ErrorIs(t, e.submit(prepared, late), net.ErrClosed)
		require.ErrorIs(t, e.offer(p, 0, late), net.ErrClosed)
		require.EqualValues(t, 1, late.buffers[0].refCount)
		late.buffers[0].Release()
		require.ErrorIs(t, child.send(nil, nil, []byte("late response"), nativeShutdownAddress, packetInfo{}), net.ErrClosed)

		// A post-close public write neither dispatches nor initializes.
		written, err := transport.WriteTo([]byte("late write"), nativeShutdownAddress)
		require.Zero(t, written)
		require.ErrorIs(t, err, net.ErrClosed)
		require.Nil(t, transport.conn)

		close(release)
		require.NoError(t, transport.waitNativeIOStopped(t.Context()))
		require.Zero(t, current.buffers[0].refCount)
		require.Equal(t, 1, writes)
	})
}

func TestNativeParticipantLimit(t *testing.T) {
	fill := func(t *testing.T, e *nativeEndpoint) {
		t.Helper()
		for id := range uint64(endpointParticipants) {
			_, err := e.attach(id + 1)
			require.NoError(t, err)
		}
	}
	// The transport keeps its real socket for reading and the scripted writer for
	// sending, so the refusal is observed at the native boundary.
	start := func(t *testing.T, native endpointNativeIO) (*nativeEndpoint, *Transport) {
		t.Helper()
		e := startNativeEndpoint(native, time.Second, nil)
		t.Cleanup(func() { <-e.stop() })
		tr := &Transport{Conn: newUDPConnLocalhost(t), responses: newEndpointResponses(e)}
		t.Cleanup(func() { tr.Close() })
		return e, tr
	}
	// sendInitial makes an ungranted client send one Initial to the listener.
	sendInitial := func(t *testing.T, server *Transport) {
		t.Helper()
		client := &Transport{Conn: newUDPConnLocalhost(t)}
		t.Cleanup(func() { client.Close() })
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()
		_, err := client.Dial(ctx, server.Conn.LocalAddr(), nativeClientTLS(), nil)
		require.Error(t, err)
	}
	t.Run("dial", func(t *testing.T) {
		e, tr := start(t, endpointTestNative{})
		fill(t, e)
		_, err := tr.Dial(t.Context(), nativeShutdownAddress, nativeClientTLS(), nil)
		require.ErrorIs(t, err, ErrMultipathResourceLimitV1)
		e.do(func(s *endpointState) {
			for _, p := range s.participants[:endpointParticipants] {
				require.NotNil(t, p)
			}
		})
	})
	t.Run("server refusal", func(t *testing.T) {
		written := make(chan []byte, 16)
		e, tr := start(t, endpointTestNative{send: func(data []byte) (int, error) {
			written <- bytes.Clone(data)
			return len(data), nil
		}})
		fill(t, e)
		listener, err := tr.Listen(nativeServerTLS(), nil)
		require.NoError(t, err)
		defer listener.Close()
		sendInitial(t, tr)
		select {
		case data := <-written:
			header, _, _, err := wire.ParsePacket(data)
			require.NoError(t, err)
			require.Equal(t, protocol.PacketTypeInitial, header.Type, "CONNECTION_REFUSED, through the stateless class")
		case <-time.After(time.Second):
			t.Fatal("no refusal reached the endpoint")
		}
	})
	t.Run("server refusal with full response class", func(t *testing.T) {
		release := make(chan struct{})
		var writes atomic.Int32
		e, tr := start(t, endpointTestNative{send: func(data []byte) (int, error) {
			writes.Add(1)
			<-release
			return len(data), nil
		}})
		fill(t, e)
		for range 8 {
			require.NoError(t, e.writeResponse([]byte("occupy"), nativeShutdownAddress, packetInfo{}, nil))
		}
		listener, err := tr.Listen(nativeServerTLS(), nil)
		require.NoError(t, err)
		defer listener.Close()
		sendInitial(t, tr)
		require.EqualValues(t, 1, writes.Load())
		close(release)
		require.Eventually(t, func() bool {
			idle := false
			e.do(func(s *endpointState) { idle = s.classCount[endpointStateless] == 0 })
			return idle
		}, time.Second, time.Millisecond)
		require.EqualValues(t, 8, writes.Load(), "the refusal was dropped under its existing best-effort rule")
	})
}

// TestNativeRouteLoopback takes every route through real sockets on granted
// transports: Retry, handshake, stream data, a non-QUIC public write, a path switch,
// the live close and the join. It is a fixture boundary, not native qualification.
func TestNativeRouteLoopback(t *testing.T) {
	grant := func(t *testing.T, conn *net.UDPConn) *Transport {
		t.Helper()
		tr := &Transport{Conn: conn}
		require.NoError(t, tr.configureNativeIO(time.Second))
		t.Cleanup(func() { tr.Close() })
		return tr
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	exchange := func(t *testing.T, from, to *Conn, message string) {
		t.Helper()
		stream, err := from.OpenStreamSync(ctx)
		require.NoError(t, err)
		_, err = stream.Write([]byte(message))
		require.NoError(t, err)
		require.NoError(t, stream.Close())
		accepted, err := to.AcceptStream(ctx)
		require.NoError(t, err)
		data, err := io.ReadAll(accepted)
		require.NoError(t, err)
		require.Equal(t, message, string(data))
	}

	serverSocket, clientSocket, nextSocket := newUDPConnLocalhost(t), newUDPConnLocalhost(t), newUDPConnLocalhost(t)
	serverTransport := grant(t, serverSocket)
	retries := make(chan struct{}, 1)
	serverTransport.VerifySourceAddress = func(net.Addr) bool { // sends a Retry
		select {
		case retries <- struct{}{}:
		default:
		}
		return true
	}
	clientTransport, nextTransport := grant(t, clientSocket), grant(t, nextSocket)
	listener, err := serverTransport.Listen(nativeServerTLS(), nil)
	require.NoError(t, err)
	client, err := clientTransport.Dial(ctx, listener.Addr(), nativeClientTLS(), nil)
	require.NoError(t, err)
	server, err := listener.Accept(ctx)
	require.NoError(t, err)
	exchange(t, client, server, "over the endpoint")
	select {
	case <-retries:
	default:
		t.Fatal("the handshake didn't take the Retry route")
	}

	// A public non-QUIC write shares the same writer.
	receiver := newUDPConnLocalhost(t)
	written, err := clientTransport.WriteTo([]byte("public"), receiver.LocalAddr())
	require.NoError(t, err)
	require.Equal(t, len("public"), written)
	require.NoError(t, receiver.SetReadDeadline(time.Now().Add(5*time.Second)))
	var buffer [8]byte
	read, source, err := receiver.ReadFromUDP(buffer[:])
	require.NoError(t, err)
	require.Equal(t, "public", string(buffer[:read]))
	require.Equal(t, clientSocket.LocalAddr().String(), source.String())

	// The switch moves the connection to the other granted socket.
	path, err := client.AddPath(nextTransport)
	require.NoError(t, err)
	require.NoError(t, path.Probe(ctx))
	require.NoError(t, path.Switch())
	exchange(t, client, server, "after the switch")
	require.Eventually(t, func() bool { return server.RemoteAddr().String() == nextSocket.LocalAddr().String() }, time.Second, time.Millisecond)

	// The live close reaches the peer asynchronously.
	require.NoError(t, client.CloseWithError(7, "finished"))
	select {
	case <-server.Context().Done():
	case <-ctx.Done():
		t.Fatal("the live CONNECTION_CLOSE never reached the peer")
	}
	var closed *ApplicationError
	require.ErrorAs(t, context.Cause(server.Context()), &closed)
	require.True(t, closed.Remote)
	require.EqualValues(t, 7, closed.ErrorCode)

	// Each join returns its socket: it is writable again and can be re-granted.
	for _, tr := range []*Transport{clientTransport, nextTransport, serverTransport} {
		require.ErrorIs(t, tr.waitNativeIOStopped(ctx), ErrMultipathNotStoppingV1)
		require.NoError(t, tr.Close())
		require.NoError(t, tr.waitNativeIOStopped(ctx))
	}
	require.NoError(t, listener.Close())
	_, err = clientSocket.WriteToUDP([]byte("mine again"), serverSocket.LocalAddr().(*net.UDPAddr))
	require.NoError(t, err)
	again := &Transport{Conn: clientSocket}
	require.NoError(t, again.configureNativeIO(time.Second))
	require.NoError(t, again.Close())
}
