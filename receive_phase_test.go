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

// dialPhasePair connects a client and server whose transports optionally
// start new connections in the closed receive phase.
func dialPhasePair(t *testing.T, ctx context.Context, clientClosed, serverClosed bool, clientRec, serverRec qlogwriter.Recorder) (client, server *Conn) {
	t.Helper()
	serverTransport := &Transport{Conn: newUDPConnLocalhost(t)}
	t.Cleanup(func() { serverTransport.Close() })
	clientTransport := &Transport{Conn: newUDPConnLocalhost(t)}
	t.Cleanup(func() { clientTransport.Close() })
	if serverClosed {
		require.NoError(t, configureReceivePhases(t, serverTransport, true))
	}
	if clientClosed {
		require.NoError(t, configureReceivePhases(t, clientTransport, true))
	}
	ln, err := serverTransport.Listen(testdata.GetTLSConfig(), phaseConfig(serverRec))
	require.NoError(t, err)
	client, err = clientTransport.Dial(ctx, ln.Addr(), candidateClientTLS(), phaseConfig(clientRec))
	require.NoError(t, err)
	server, err = ln.Accept(ctx)
	require.NoError(t, err)
	return client, server
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
	client, server := dialPhasePair(t, ctx, true, true, nil, nil)
	phase, err := openReceivePhase(t, ctx, server)
	require.NoError(t, err)
	require.EqualValues(t, 1, phase)
	_, err = openReceivePhase(t, ctx, server)
	require.ErrorIs(t, err, errReceivePhaseOpen, "the phase opens once")

	require.NoError(t, client.CloseWithError(0, ""))
	_, err = openReceivePhase(t, ctx, client)
	require.Error(t, err, "a closed connection cannot open its phase")
	require.NotErrorIs(t, err, errReceivePhaseOpen)

	// Unconfigured connections have no phase to open.
	plainClient, plainServer := dialPhasePair(t, ctx, false, false, nil, nil)
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

func receiveDatagramAsync(ctx context.Context, conn *Conn) <-chan receivedDatagram {
	ch := make(chan receivedDatagram, 1)
	go func() {
		data, err := conn.ReceiveDatagram(ctx)
		ch <- receivedDatagram{data: data, err: err}
	}()
	return ch
}

// Both perspectives start closed. A DATAGRAM processed before opening is
// discarded yet acknowledged, and a receiver already waiting before opening
// gets only the payload admitted after it.
func TestReceivePhaseDatagramBoundary(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	clientRec, serverRec := &events.Recorder{}, &events.Recorder{}
	client, server := dialPhasePair(t, ctx, true, true, clientRec, serverRec)

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
			waiting := receiveDatagramAsync(ctx, tc.receiver)
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
// carrying a DATAGRAM of the given length, until released.
type holdingRecorder struct {
	*events.Recorder
	length  int64
	once    sync.Once
	reached chan struct{}
	release chan struct{}
	unblock func()
}

// The caller defers unblock so the loop is released before transport cleanup.
func newHoldingRecorder(length int) *holdingRecorder {
	r := &holdingRecorder{
		Recorder: &events.Recorder{},
		length:   int64(length),
		reached:  make(chan struct{}),
		release:  make(chan struct{}),
	}
	r.unblock = sync.OnceFunc(func() { close(r.release) })
	return r
}

func (r *holdingRecorder) RecordEvent(ev qlogwriter.Event) {
	r.Recorder.RecordEvent(ev)
	pr, ok := ev.(qlog.PacketReceived)
	if !ok {
		return
	}
	for _, f := range pr.Frames {
		if d, ok := f.Frame.(*qlog.DatagramFrame); ok && d.Length == r.length {
			r.once.Do(func() { close(r.reached) })
			<-r.release
			return
		}
	}
}

// An open request cannot split a packet's admission: while the loop is
// inside the packet carrying a DATAGRAM, a canceled request is withdrawn and
// a pending one waits, so that payload stays in the closed phase.
func TestReceivePhaseOpenRace(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	held, late := []byte("payload whose packet holds the loop"), []byte("late payload")
	rec := newHoldingRecorder(len(held))
	defer rec.unblock()
	client, server := dialPhasePair(t, ctx, false, true, nil, rec)
	waiting := receiveDatagramAsync(ctx, server)
	require.NoError(t, client.SendDatagram(held))
	select {
	case <-rec.reached:
	case <-ctx.Done():
		t.Fatal("the DATAGRAM was not processed")
	}

	canceled, cancelOpen := context.WithCancel(ctx)
	cancelOpen()
	_, err := openReceivePhase(t, canceled, server)
	require.ErrorIs(t, err, context.Canceled, "a request the loop has not applied is withdrawn")

	type openResult struct {
		phase uint64
		err   error
	}
	api, ok := any(server).(receivePhaseOpenV1)
	require.True(t, ok)
	pending := make(chan openResult, 1)
	go func() {
		phase, err := api.OpenReceivePhaseV1(ctx)
		pending <- openResult{phase, err}
	}()
	select {
	case <-pending:
		t.Fatal("the transition cannot run while the loop is inside a packet")
	case <-time.After(50 * time.Millisecond):
	}
	rec.unblock()
	res := <-pending
	require.NoError(t, res.err)
	require.EqualValues(t, 1, res.phase)

	require.NoError(t, client.SendDatagram(late))
	got := <-waiting
	require.NoError(t, got.err)
	require.Equal(t, late, got.data, "the held payload was admitted before the transition")
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
