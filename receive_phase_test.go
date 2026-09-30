package quic

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/testdata"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/quic-go/quic-go/testutils/events"

	"github.com/stretchr/testify/require"
)

// Consumers negotiate receive phases with standard types only.
type receivePhasesV1 interface {
	ConfigureReceivePhasesV1(closed bool) error
}

type receivePhaseOpenV1 interface {
	OpenReceivePhaseV1(ctx context.Context) (uint64, error)
}

func configureReceivePhases(t *testing.T, tr *Transport, closed bool) error {
	t.Helper()
	api, ok := any(tr).(receivePhasesV1)
	require.True(t, ok, "transport must expose a standard-type receive phase capability")
	return api.ConfigureReceivePhasesV1(closed)
}

func openReceivePhase(t *testing.T, ctx context.Context, conn *Conn) (uint64, error) {
	t.Helper()
	api, ok := any(conn).(receivePhaseOpenV1)
	require.True(t, ok, "connection must expose a receive phase open capability")
	return api.OpenReceivePhaseV1(ctx)
}

func phaseConfig(rec qlogwriter.Recorder) *Config {
	conf := &Config{EnableDatagrams: true}
	if rec != nil {
		conf.Tracer = func(context.Context, bool, ConnectionID) qlogwriter.Trace { return &events.Trace{Recorder: rec} }
	}
	return conf
}

type phasePair struct {
	clientClosed, serverClosed bool
	clientRec, serverRec       qlogwriter.Recorder
	serverConnContext          func(context.Context, *ClientInfo) (context.Context, error)
}

// dial connects a client and server whose transports optionally start new
// connections in the closed receive phase.
func (o phasePair) dial(t *testing.T, ctx context.Context) (client, server *Conn) {
	t.Helper()
	serverTransport := &Transport{Conn: newUDPConnLocalhost(t), ConnContext: o.serverConnContext}
	t.Cleanup(func() { serverTransport.Close() })
	clientTransport := &Transport{Conn: newUDPConnLocalhost(t)}
	t.Cleanup(func() { clientTransport.Close() })
	if o.serverClosed {
		require.NoError(t, configureReceivePhases(t, serverTransport, true))
	}
	if o.clientClosed {
		require.NoError(t, configureReceivePhases(t, clientTransport, true))
	}
	ln, err := serverTransport.Listen(testdata.GetTLSConfig(), phaseConfig(o.serverRec))
	require.NoError(t, err)
	client, err = clientTransport.Dial(ctx, ln.Addr(), candidateClientTLS(), phaseConfig(o.clientRec))
	require.NoError(t, err)
	server, err = ln.Accept(ctx)
	require.NoError(t, err)
	return client, server
}

// goWorker runs fn on a goroutine that the test joins at cleanup, after
// canceling its context, so no worker outlives its fixtures.
func goWorker[T any](t *testing.T, ctx context.Context, fn func(context.Context) T) <-chan T {
	t.Helper()
	ctx, cancel := context.WithCancel(ctx)
	result := make(chan T, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		result <- fn(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return result
}

// Configuration precedes initialization and happens once. Opening is a
// single transition of a configured, live connection.
func TestReceivePhaseContract(t *testing.T) {
	tr := &Transport{Conn: newUDPConnLocalhost(t)}
	defer tr.Close()
	require.NoError(t, configureReceivePhases(t, tr, true))
	require.Error(t, configureReceivePhases(t, tr, true), "configuration happens once")
	require.Error(t, configureReceivePhases(t, tr, false), "configuration happens once")
	initialized := &Transport{Conn: newUDPConnLocalhost(t)}
	defer initialized.Close()
	_, err := initialized.Listen(testdata.GetTLSConfig(), nil)
	require.NoError(t, err)
	require.Error(t, configureReceivePhases(t, initialized, true), "configuration precedes initialization")

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	// Canceling the application's connection context does not end the
	// connection, so it does not prevent opening.
	parentCancels := make(chan context.CancelFunc, 1)
	client, server := phasePair{
		clientClosed: true,
		serverClosed: true,
		serverConnContext: func(ctx context.Context, _ *ClientInfo) (context.Context, error) {
			ctx, cancel := context.WithCancel(ctx)
			parentCancels <- cancel
			return ctx, nil
		},
	}.dial(t, ctx)
	(<-parentCancels)()
	<-server.Context().Done()
	phase, err := openReceivePhase(t, ctx, server)
	require.NoError(t, err)
	require.EqualValues(t, 1, phase)
	payload := []byte("after the application context ended")
	require.NoError(t, client.SendDatagram(payload))
	got, err := server.ReceiveDatagram(ctx)
	require.NoError(t, err)
	require.Equal(t, payload, got)
	_, err = openReceivePhase(t, ctx, server)
	require.ErrorIs(t, err, errReceivePhaseOpen, "the phase opens once")

	// A closed connection reports its close error.
	require.NoError(t, client.CloseWithError(7, "done"))
	_, err = openReceivePhase(t, ctx, client)
	var appErr *ApplicationError
	require.ErrorAs(t, err, &appErr, "a closed connection cannot open its phase")
	require.EqualValues(t, 7, appErr.ErrorCode)
	require.False(t, appErr.Remote)

	// Unconfigured connections have no phase to open.
	plainClient, plainServer := phasePair{}.dial(t, ctx)
	_, err = openReceivePhase(t, ctx, plainServer)
	require.ErrorIs(t, err, errNoReceivePhase)
	_, err = openReceivePhase(t, ctx, plainClient)
	require.ErrorIs(t, err, errNoReceivePhase)
}

// processedDatagram waits until a connection's loop has handled every frame of
// the 1-RTT packet carrying a DATAGRAM of the given length; the packet event is
// recorded only after its frames are processed. It returns that packet number.
func processedDatagram(t *testing.T, rec *events.Recorder, length int) protocol.PacketNumber {
	t.Helper()
	var pn protocol.PacketNumber
	require.Eventually(t, func() bool {
		for _, ev := range rec.Events(qlog.PacketReceived{}) {
			for _, f := range ev.(qlog.PacketReceived).Frames {
				if d, ok := f.Frame.(*qlog.DatagramFrame); ok && d.Length == int64(length) {
					pn = ev.(qlog.PacketReceived).Header.PacketNumber
					return true
				}
			}
		}
		return false
	}, 5*time.Second, time.Millisecond, "the DATAGRAM was not processed")
	return pn
}

// requireAcked waits until the sender receives an ACK covering pn.
func requireAcked(t *testing.T, rec *events.Recorder, pn protocol.PacketNumber) {
	t.Helper()
	require.Eventually(t, func() bool {
		for _, ev := range rec.Events(qlog.PacketReceived{}) {
			for _, f := range ev.(qlog.PacketReceived).Frames {
				if ack, ok := f.Frame.(*qlog.AckFrame); ok && ack.AcksPacket(pn) {
					return true
				}
			}
		}
		return false
	}, 5*time.Second, time.Millisecond, "the packet carrying a discarded DATAGRAM was not acknowledged")
}

type receivedDatagram struct {
	data []byte
	err  error
}

func receiveDatagramAsync(t *testing.T, ctx context.Context, conn *Conn) <-chan receivedDatagram {
	t.Helper()
	return goWorker(t, ctx, func(ctx context.Context) receivedDatagram {
		data, err := conn.ReceiveDatagram(ctx)
		return receivedDatagram{data: data, err: err}
	})
}

// Both perspectives start closed. A DATAGRAM processed before opening is
// discarded yet acknowledged, and a receiver already waiting before opening
// gets only the payload admitted after it.
func TestReceivePhaseDatagramBoundary(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	clientRec, serverRec := &events.Recorder{}, &events.Recorder{}
	client, server := phasePair{clientClosed: true, serverClosed: true, clientRec: clientRec, serverRec: serverRec}.dial(t, ctx)

	for _, tc := range []struct {
		name                   string
		sender, receiver       *Conn
		senderRec, receiverRec *events.Recorder
	}{
		{"server receives", client, server, clientRec, serverRec},
		{"client receives", server, client, serverRec, clientRec},
	} {
		t.Run(tc.name, func(t *testing.T) {
			early, late := []byte("early payload before open"), []byte("late payload")
			waiting := receiveDatagramAsync(t, ctx, tc.receiver)
			require.NoError(t, tc.sender.SendDatagram(early))
			pn := processedDatagram(t, tc.receiverRec, len(early))
			requireAcked(t, tc.senderRec, pn)

			phase, err := openReceivePhase(t, ctx, tc.receiver)
			require.NoError(t, err)
			require.EqualValues(t, 1, phase)
			require.NoError(t, tc.sender.SendDatagram(late))
			got := <-waiting
			require.NoError(t, got.err)
			require.Equal(t, late, got.data, "only a payload admitted after opening is delivered")
		})
	}
}

// holdingRecorder holds the connection loop once it has processed the packet
// carrying a DATAGRAM of one of the given lengths, until that hold is
// released. The caller defers unblockAll so the loop is released before
// fixture cleanup.
type holdingRecorder struct {
	*events.Recorder
	holds map[int64]*loopHold
}

type loopHold struct {
	once    sync.Once
	reached chan struct{}
	release func()
	stop    chan struct{}
}

func newHoldingRecorder(lengths ...int) *holdingRecorder {
	r := &holdingRecorder{Recorder: &events.Recorder{}, holds: make(map[int64]*loopHold)}
	for _, l := range lengths {
		h := &loopHold{reached: make(chan struct{}), stop: make(chan struct{})}
		h.release = sync.OnceFunc(func() { close(h.stop) })
		r.holds[int64(l)] = h
	}
	return r
}

func (r *holdingRecorder) unblockAll() {
	for _, h := range r.holds {
		h.release()
	}
}

// awaitHold waits until the loop is held by the packet carrying a DATAGRAM of
// the given length.
func (r *holdingRecorder) awaitHold(t *testing.T, ctx context.Context, length int) *loopHold {
	t.Helper()
	h := r.holds[int64(length)]
	select {
	case <-h.reached:
	case <-ctx.Done():
		t.Fatal("the DATAGRAM was not processed")
	}
	return h
}

func (r *holdingRecorder) RecordEvent(ev qlogwriter.Event) {
	r.Recorder.RecordEvent(ev)
	pr, ok := ev.(qlog.PacketReceived)
	if !ok {
		return
	}
	for _, f := range pr.Frames {
		if d, ok := f.Frame.(*qlog.DatagramFrame); ok {
			if h, ok := r.holds[d.Length]; ok {
				h.once.Do(func() { close(h.reached) })
				<-h.stop
				return
			}
		}
	}
}

// requirePhaseState observes the phase owner's state under its mutex.
func requirePhaseState(t *testing.T, conn *Conn, want receivePhaseState, msg string) {
	t.Helper()
	require.Eventually(t, func() bool {
		p := conn.receivePhase
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.state == want
	}, 5*time.Second, time.Millisecond, msg)
}

type openResult struct {
	phase uint64
	err   error
}

func openAsync(t *testing.T, ctx context.Context, conn *Conn) <-chan openResult {
	t.Helper()
	api, ok := any(conn).(receivePhaseOpenV1)
	require.True(t, ok)
	return goWorker(t, ctx, func(ctx context.Context) openResult {
		phase, err := api.OpenReceivePhaseV1(ctx)
		return openResult{phase, err}
	})
}

// Only the connection loop commits the transition, between packets. While
// the loop is held inside a packet, a request is published but not applied;
// a request canceled before the commit leaves the phase closed however the
// cancellation is observed, and a live one commits once the loop resumes.
func TestReceivePhaseOpenRace(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	first, second := []byte("first payload whose packet holds the loop"), []byte("second held payload")
	late := []byte("late payload")
	rec := newHoldingRecorder(len(first), len(second))
	defer rec.unblockAll()
	client, server := phasePair{serverClosed: true, serverRec: rec}.dial(t, ctx)
	waiting := receiveDatagramAsync(t, ctx, server)

	require.NoError(t, client.SendDatagram(first))
	hold := rec.awaitHold(t, ctx, len(first))
	canceled, cancelNow := context.WithCancel(ctx)
	cancelNow()
	_, err := openReceivePhase(t, canceled, server)
	require.ErrorIs(t, err, context.Canceled, "a canceled request is never published")
	requirePhaseState(t, server, receivePhaseClosed, "a canceled request is never published")

	cancelable, cancelPending := context.WithCancel(ctx)
	defer cancelPending()
	pending := openAsync(t, cancelable, server)
	requirePhaseState(t, server, receivePhaseOpening, "the request is published while the loop is held")
	_, err = openReceivePhase(t, ctx, server)
	require.ErrorIs(t, err, errReceivePhaseOpening, "a concurrent request is rejected")
	// Cancel before the commit and release the loop without waiting for the
	// caller to withdraw: the loop owner rejects it if it runs first.
	cancelPending()
	hold.release()
	res := <-pending
	require.ErrorIs(t, res.err, context.Canceled)
	requirePhaseState(t, server, receivePhaseClosed, "a request canceled before the commit leaves the phase closed")

	require.NoError(t, client.SendDatagram(second))
	hold = rec.awaitHold(t, ctx, len(second))
	pending = openAsync(t, ctx, server)
	requirePhaseState(t, server, receivePhaseOpening, "the retry is published while the loop is held")
	select {
	case <-pending:
		t.Fatal("only the loop commits the transition")
	default:
	}
	hold.release()
	res = <-pending
	require.NoError(t, res.err)
	require.EqualValues(t, 1, res.phase)
	requirePhaseState(t, server, receivePhaseOpened, "the loop committed the retry")

	require.NoError(t, client.SendDatagram(late))
	got := <-waiting
	require.NoError(t, got.err)
	require.Equal(t, late, got.data, "payloads admitted while the phase was closed are never delivered")
}

// Ordinary transports, and transports configured with false, deliver
// DATAGRAMs without any receive phase.
func TestReceivePhaseDefaultDatagrams(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	for _, tc := range []struct {
		name       string
		configured bool
	}{
		{"unconfigured", false},
		{"configured open", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			serverTransport := &Transport{Conn: newUDPConnLocalhost(t)}
			defer serverTransport.Close()
			clientTransport := &Transport{Conn: newUDPConnLocalhost(t)}
			defer clientTransport.Close()
			if tc.configured {
				require.NoError(t, configureReceivePhases(t, serverTransport, false))
				require.NoError(t, configureReceivePhases(t, clientTransport, false))
			}
			ln, err := serverTransport.Listen(testdata.GetTLSConfig(), phaseConfig(nil))
			require.NoError(t, err)
			client, err := clientTransport.Dial(ctx, ln.Addr(), candidateClientTLS(), phaseConfig(nil))
			require.NoError(t, err)
			server, err := ln.Accept(ctx)
			require.NoError(t, err)

			for _, pair := range [][2]*Conn{{client, server}, {server, client}} {
				payload := []byte("ordinary payload")
				require.NoError(t, pair[0].SendDatagram(payload))
				got, err := pair[1].ReceiveDatagram(ctx)
				require.NoError(t, err)
				require.Equal(t, payload, got)
			}
			_, err = openReceivePhase(t, ctx, server)
			require.ErrorIs(t, err, errNoReceivePhase)
		})
	}
}
