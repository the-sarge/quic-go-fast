package quic

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/testdata"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/qlogwriter"

	"github.com/stretchr/testify/require"
)

// Consumers negotiate dial setup admission with standard and upstream QUIC
// types only.
type dialSetupAdmissionV1 interface {
	ConfigureDialSetupAdmissionV1(acquire func(ctx context.Context, remote netip.AddrPort) (deadline time.Time, complete func() bool, claim func(), release func(transferred bool), ok bool)) error
}

// dialID is a value the caller authors into each Dial context.
type dialID struct{}

// dialAdmission is one acquire callback's view: the caller's own dial ID, the
// remote and whether the shared budget admitted it.
type dialAdmission struct {
	id     any
	remote netip.AddrPort
	ok     bool
}

// correlatedDials adapts a shared budget to dial admission and records which
// of the caller's dials each acquisition belonged to.
type correlatedDials struct {
	budget *setupBudget
	mu     sync.Mutex
	seen   []dialAdmission
}

func (d *correlatedDials) acquire(ctx context.Context, remote netip.AddrPort) (time.Time, func() bool, func(), func(bool), bool) {
	deadline, complete, claim, release, ok := d.budget.acquire(remote)
	d.mu.Lock()
	d.seen = append(d.seen, dialAdmission{id: ctx.Value(dialID{}), remote: remote, ok: ok})
	d.mu.Unlock()
	return deadline, complete, claim, release, ok
}

func (d *correlatedDials) admissions() []dialAdmission {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]dialAdmission(nil), d.seen...)
}

func configureDialSetupAdmission(t *testing.T, tr *Transport, acquire func(context.Context, netip.AddrPort) (time.Time, func() bool, func(), func(bool), bool)) error {
	t.Helper()
	api, ok := any(tr).(dialSetupAdmissionV1)
	require.True(t, ok, "transport must expose a standard-type dial setup admission capability")
	return api.ConfigureDialSetupAdmissionV1(acquire)
}

// countClientConstructors observes the real client connection constructor,
// which precedes the connection's first packet.
func countClientConstructors(t *testing.T) *atomic.Int32 {
	t.Helper()
	var n atomic.Int32
	orig := newClientConnection
	newClientConnection = func(
		ctx context.Context,
		conn sendConn,
		runner connRunner,
		destConnID protocol.ConnectionID,
		srcConnID protocol.ConnectionID,
		connIDGenerator ConnectionIDGenerator,
		statelessResetter *statelessResetter,
		conf *Config,
		tlsConf *tls.Config,
		initialPacketNumber protocol.PacketNumber,
		enable0RTT bool,
		hasNegotiatedVersion bool,
		qlogTrace qlogwriter.Trace,
		logger utils.Logger,
		v protocol.Version,
	) *wrappedConn {
		n.Add(1)
		return orig(ctx, conn, runner, destConnID, srcConnID, connIDGenerator, statelessResetter, conf, tlsConf, initialPacketNumber, enable0RTT, hasNegotiatedVersion, qlogTrace, logger, v)
	}
	t.Cleanup(func() { newClientConnection = orig })
	return &n
}

// newDialSetupTransport returns a client transport whose dials reserve setup
// capacity from budget.
func newDialSetupTransport(t *testing.T, budget *setupBudget) (*Transport, *correlatedDials) {
	t.Helper()
	dials := &correlatedDials{budget: budget}
	tr := &Transport{Conn: newUDPConnLocalhost(t)}
	require.NoError(t, configureDialSetupAdmission(t, tr, dials.acquire))
	t.Cleanup(func() { tr.Close() })
	return tr, dials
}

// requireSilent asserts that no datagram reaches conn.
func requireSilent(t *testing.T, conn net.PacketConn) {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(200*time.Millisecond)))
	_, _, err := conn.ReadFrom(make([]byte, protocol.MaxPacketBufferSize))
	var netErr net.Error
	require.ErrorAs(t, err, &netErr, "a refused dial must not send a datagram")
	require.True(t, netErr.Timeout())
}

func TestDialSetupAdmissionContract(t *testing.T) {
	budget := newSetupBudget(1, 1, time.Minute)
	dials := &correlatedDials{budget: budget}
	tr := &Transport{Conn: newUDPConnLocalhost(t)}
	defer tr.Close()
	require.Error(t, configureDialSetupAdmission(t, tr, nil))
	require.NoError(t, configureDialSetupAdmission(t, tr, dials.acquire))
	require.Error(t, configureDialSetupAdmission(t, tr, dials.acquire), "configuration happens once")
	require.NoError(t, configureSetupAdmission(t, tr, budget.acquire), "server admission is a separate configuration")

	initialized := &Transport{Conn: newUDPConnLocalhost(t)}
	defer initialized.Close()
	_, err := initialized.Listen(testdata.GetTLSConfig(), nil)
	require.NoError(t, err)
	require.Error(t, configureDialSetupAdmission(t, initialized, dials.acquire), "configuration precedes initialization")
	require.Empty(t, dials.admissions())

	// Dials keep ordinary behavior without the dial configuration, including
	// DialEarly on a transport whose accepted connections are reserved.
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	ln, err := (&Transport{Conn: newUDPConnLocalhost(t)}).Listen(testdata.GetTLSConfig(), nil)
	require.NoError(t, err)
	defer ln.Close()
	client, err := newSetupTransport(t, budget).DialEarly(ctx, ln.Addr(), candidateClientTLS(), nil)
	require.NoError(t, err)
	server, err := ln.Accept(ctx)
	require.NoError(t, err)
	exchangeAdmissionStream(t, ctx, client, server)
	require.ErrorIs(t, transferSetup(t, client), errNoSetupReservation)
	require.Zero(t, budget.state(t).acquired, "server admission never reserves dials")
}

// A refused acquisition fails the dial before any connection is constructed or
// datagram sent. Each acquisition sees the Dial context, so the caller
// correlates reservations and refusals with its own dials. DialEarly is refused
// before acquisition.
func TestDialSetupAdmissionRefusesBeforeConstruction(t *testing.T) {
	constructed := countClientConstructors(t)
	budget := newSetupBudget(1, 1, time.Minute)
	tr, dials := newDialSetupTransport(t, budget)
	serverA, serverB := newUDPConnLocalhost(t), newUDPConnLocalhost(t)

	// Dial A is admitted and holds the only reservation while it handshakes
	// with a server that never answers.
	ctxA, cancelA := context.WithCancel(context.WithValue(t.Context(), dialID{}, "a"))
	defer cancelA()
	errA := make(chan error, 1)
	go func() {
		_, err := tr.Dial(ctxA, serverA.LocalAddr(), candidateClientTLS(), nil)
		errA <- err
	}()
	budget.waitState(t, func(s setupBudgetState) bool { return s.acquired == 1 }, "dial A is admitted")

	ctxB, cancelB := context.WithTimeout(context.WithValue(t.Context(), dialID{}, "b"), 5*time.Second)
	defer cancelB()
	_, err := tr.Dial(ctxB, serverB.LocalAddr(), candidateClientTLS(), nil)
	require.ErrorIs(t, err, errDialSetupRefused)
	requireSilent(t, serverB)
	require.EqualValues(t, 1, constructed.Load(), "a refused dial allocates nothing")
	require.Equal(t, []dialAdmission{
		{id: "a", remote: endpoint(t, serverA.LocalAddr()), ok: true},
		{id: "b", remote: endpoint(t, serverB.LocalAddr()), ok: false},
	}, dials.admissions())

	// A 0-RTT return precedes handshake completion, so DialEarly never acquires.
	_, err = tr.DialEarly(ctxB, serverB.LocalAddr(), candidateClientTLS(), nil)
	require.ErrorIs(t, err, errDialEarlySetupAdmission)
	requireSilent(t, serverB)
	require.Len(t, dials.admissions(), 2)
	require.EqualValues(t, 1, constructed.Load())

	cancelA()
	require.ErrorIs(t, <-errA, context.Canceled)
	budget.requireSettlement(t, false)
	state := budget.state(t)
	require.Equal(t, []bool{false}, state.settlements)
	require.Zero(t, state.held)
}

// requireLocalRefusal asserts that this endpoint closed with CONNECTION_REFUSED.
func requireLocalRefusal(t *testing.T, err error) {
	t.Helper()
	var transportErr *TransportError
	require.ErrorAs(t, err, &transportErr)
	require.Equal(t, ConnectionRefused, transportErr.ErrorCode)
	require.False(t, transportErr.Remote)
}

// requireReleasedAfterRetirement makes budget observe tr's connection-ID
// entries at settlement, so a release while closed state is retained fails.
func requireReleasedAfterRetirement(t *testing.T, budget *setupBudget, tr *Transport) <-chan int {
	t.Helper()
	retained := make(chan int, 1)
	budget.onRelease = func() {
		tr.mutex.Lock()
		retained <- len(tr.handlers)
		tr.mutex.Unlock()
	}
	return retained
}

// A refused completion closes the handshaken connection with an ordinary
// close and fails the dial; capacity is held until its closing state retires.
func TestDialSetupAdmissionCompletionRefused(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	constructed := countClientConstructors(t)
	budget := newSetupBudget(1, 0, time.Minute)
	tr, _ := newDialSetupTransport(t, budget)
	retained := requireReleasedAfterRetirement(t, budget, tr)
	ln, err := (&Transport{Conn: newUDPConnLocalhost(t)}).Listen(testdata.GetTLSConfig(), nil)
	require.NoError(t, err)
	defer ln.Close()

	// The server may not complete its handshake before the close arrives, so
	// only the client's ordinary close is observed.
	_, err = tr.Dial(ctx, ln.Addr(), candidateClientTLS(), nil)
	requireLocalRefusal(t, err)
	budget.requireSettlement(t, false)
	require.Zero(t, <-retained, "released while closed connection state was retained")
	require.EqualValues(t, 1, constructed.Load())
	state := budget.state(t)
	require.Equal(t, 1, state.acquired, "a stage change never reacquires capacity")
	require.Equal(t, []bool{false}, state.settlements)
	require.Zero(t, state.held)
}

// Dial returning the connection claims its reservation; transfer then releases
// it to the caller's established lifetime and stops the deadline.
func TestDialSetupAdmissionTransfer(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	budget := newSetupBudget(1, 1, 500*time.Millisecond)
	tr, _ := newDialSetupTransport(t, budget)
	ln, err := (&Transport{Conn: newUDPConnLocalhost(t)}).Listen(testdata.GetTLSConfig(), nil)
	require.NoError(t, err)
	defer ln.Close()

	client, err := tr.Dial(ctx, ln.Addr(), candidateClientTLS(), nil)
	require.NoError(t, err)
	server, err := ln.Accept(ctx)
	require.NoError(t, err)
	state := budget.state(t)
	require.Equal(t, [3]int{1, 0, 1}, [3]int{state.held, state.completed, state.claimed}, "the dial returns a claimed reservation")
	require.NoError(t, transferSetup(t, client))
	budget.requireSettlement(t, true)
	require.ErrorIs(t, transferSetup(t, client), errSetupReservationEnded)

	// The transferred connection outlives its former setup deadline.
	time.Sleep(600 * time.Millisecond)
	exchangeAdmissionStream(t, ctx, client, server)
	state = budget.state(t)
	require.Equal(t, []bool{true}, state.settlements)
	require.Zero(t, state.held)
}

type failingConnIDGenerator struct{}

func (failingConnIDGenerator) GenerateConnectionID() (ConnectionID, error) {
	return ConnectionID{}, errors.New("connection ID generation failed")
}
func (failingConnIDGenerator) ConnectionIDLen() int { return 8 }

// Every dial that fails after admission releases its reservation exactly once:
// a canceled dial after its connection's work has ended, an early exit before
// construction, and a failed candidate-group bind after the unstarted
// connection is aborted.
func TestDialSetupAdmissionFailureRelease(t *testing.T) {
	for _, tc := range []struct {
		name        string
		transport   func(t *testing.T) *Transport
		dial        func(t *testing.T, tr *Transport) error
		wantErr     error
		constructed int32
	}{
		{
			name: "canceled",
			dial: func(t *testing.T, tr *Transport) error {
				ctx, cancel := context.WithCancel(t.Context())
				time.AfterFunc(100*time.Millisecond, cancel)
				_, err := tr.Dial(ctx, newUDPConnLocalhost(t).LocalAddr(), candidateClientTLS(), nil)
				return err
			},
			wantErr:     context.Canceled,
			constructed: 1,
		},
		{
			name: "transport_closed",
			dial: func(t *testing.T, tr *Transport) error {
				require.NoError(t, tr.Close())
				_, err := tr.Dial(t.Context(), newUDPConnLocalhost(t).LocalAddr(), candidateClientTLS(), nil)
				return err
			},
			wantErr: ErrTransportClosed,
		},
		{
			name: "connection_id_generation",
			transport: func(t *testing.T) *Transport {
				return &Transport{Conn: newUDPConnLocalhost(t), ConnectionIDGenerator: failingConnIDGenerator{}}
			},
			dial: func(t *testing.T, tr *Transport) error {
				_, err := tr.Dial(t.Context(), newUDPConnLocalhost(t).LocalAddr(), candidateClientTLS(), nil)
				return err
			},
		},
		{
			// The group closes after the dial passes its fence check and
			// before the connection is minted into it.
			name: "candidate_bind",
			transport: func(t *testing.T) *Transport {
				return newCandidateTransport(t, newUDPConnLocalhost(t))
			},
			dial: func(t *testing.T, tr *Transport) error {
				group := newCandidateGroup(t, tr)
				require.NoError(t, group.join(tr))
				fenced := make(chan error, 1)
				conf := &Config{Tracer: func(ctx context.Context, _ bool, _ ConnectionID) qlogwriter.Trace {
					fenced <- group.closeGroup(ctx)
					return nil
				}}
				_, err := tr.Dial(t.Context(), newUDPConnLocalhost(t).LocalAddr(), candidateClientTLS(), conf)
				require.NoError(t, <-fenced)
				return err
			},
			wantErr:     errCandidateFenced,
			constructed: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			constructed := countClientConstructors(t)
			budget := newSetupBudget(1, 1, time.Minute)
			tr := &Transport{Conn: newUDPConnLocalhost(t)}
			if tc.transport != nil {
				tr = tc.transport(t)
			}
			t.Cleanup(func() { tr.Close() })
			dials := &correlatedDials{budget: budget}
			require.NoError(t, configureDialSetupAdmission(t, tr, dials.acquire))

			err := tc.dial(t, tr)
			require.Error(t, err)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			}
			budget.requireSettlement(t, false)
			require.Equal(t, tc.constructed, constructed.Load())
			state := budget.state(t)
			require.Equal(t, 1, state.acquired)
			require.Equal(t, []bool{false}, state.settlements)
			require.Zero(t, state.held)
		})
	}
}

// A connection recreated for version negotiation keeps its dial's reservation:
// the old connection detaches it without teardown and the recreated connection
// rebinds it without reacquisition or a new deadline.
func TestDialSetupAdmissionRecreation(t *testing.T) {
	// The server offers only version 1, so a dial offering version 2 first is
	// recreated after the server's Version Negotiation packet.
	versions := &Config{Versions: []Version{Version2, Version1}}
	listen := func(t *testing.T) *Listener {
		t.Helper()
		ln, err := (&Transport{Conn: newUDPConnLocalhost(t)}).Listen(testdata.GetTLSConfig(), &Config{Versions: []Version{Version1}})
		require.NoError(t, err)
		t.Cleanup(func() { ln.Close() })
		return ln
	}

	t.Run("negotiated", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		constructed := countClientConstructors(t)
		budget := newSetupBudget(1, 1, time.Minute)
		tr, _ := newDialSetupTransport(t, budget)
		retained := requireReleasedAfterRetirement(t, budget, tr)
		client, err := tr.Dial(ctx, listen(t).Addr(), candidateClientTLS(), versions)
		require.NoError(t, err)
		require.Equal(t, Version1, client.ConnectionState().Version)
		require.EqualValues(t, 2, constructed.Load(), "the dial was recreated")
		state := budget.state(t)
		require.Equal(t, [3]int{1, 1, 1}, [3]int{state.acquired, state.held, state.claimed}, "recreation keeps the one reservation")
		require.Empty(t, state.settlements)

		require.NoError(t, client.CloseWithError(0, ""))
		budget.requireSettlement(t, false)
		require.Zero(t, <-retained, "released while closed connection state was retained")
		require.Len(t, budget.state(t).deadlines, 1)
	})

	// Cancellation that finds the old connection already closed for recreation
	// releases the detached reservation once and recreates nothing.
	t.Run("canceled_during_recreation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		budget := newSetupBudget(1, 1, time.Minute)
		tr, _ := newDialSetupTransport(t, budget)
		var constructed atomic.Int32
		orig := newClientConnection
		t.Cleanup(func() { newClientConnection = orig })
		newClientConnection = func(
			ctx context.Context,
			conn sendConn,
			runner connRunner,
			destConnID protocol.ConnectionID,
			srcConnID protocol.ConnectionID,
			connIDGenerator ConnectionIDGenerator,
			statelessResetter *statelessResetter,
			conf *Config,
			tlsConf *tls.Config,
			initialPacketNumber protocol.PacketNumber,
			enable0RTT bool,
			hasNegotiatedVersion bool,
			qlogTrace qlogwriter.Trace,
			logger utils.Logger,
			v protocol.Version,
		) *wrappedConn {
			constructed.Add(1)
			real := orig(ctx, conn, runner, destConnID, srcConnID, connIDGenerator, statelessResetter, conf, tlsConf, initialPacketNumber, enable0RTT, hasNegotiatedVersion, qlogTrace, logger, v).Conn
			// The real connection closes for recreation; the dial is then
			// canceled and destroys it before its run returns, so only the
			// cancellation is ready when the dial selects.
			destroyed := make(chan struct{})
			return &wrappedConn{Conn: real, testHooks: &connTestHooks{
				run: func() error {
					err := real.run()
					cancel()
					<-destroyed
					return err
				},
				destroy:           func(e error) { real.destroyImpl(e); close(destroyed) },
				handlePacket:      real.handlePacket,
				handshakeComplete: real.HandshakeComplete,
				context:           real.Context,
			}}
		}

		_, err := tr.Dial(ctx, listen(t).Addr(), candidateClientTLS(), versions)
		require.ErrorIs(t, err, context.Canceled)
		budget.requireSettlement(t, false)
		require.EqualValues(t, 1, constructed.Load(), "a canceled dial is not recreated")
		state := budget.state(t)
		require.Equal(t, 1, state.acquired)
		require.Equal(t, []bool{false}, state.settlements)
		require.Zero(t, state.held)
	})

	// A deadline reached while no connection holds the reservation still
	// ends it: the recreated connection cannot rebind and is aborted.
	t.Run("deadline_while_detached", func(t *testing.T) {
		budget := newSetupBudget(1, 1, time.Minute)
		r, ok := newSetupReservation(budget.acquire(netip.AddrPort{}))
		require.True(t, ok)
		require.True(t, r.bind(&Conn{closeChan: make(chan struct{}, 1)}))
		r.detach()
		r.expire()
		recreated := &Conn{closeChan: make(chan struct{}, 1)}
		require.False(t, r.bind(recreated), "an expired reservation admits no recreated connection")
		require.Nil(t, recreated.setup)
		require.Empty(t, budget.state(t).settlements, "capacity is kept until the dial releases it")
		r.teardown()
		require.Equal(t, []bool{false}, budget.state(t).settlements)
	})
}

// slowReadConn delays every received datagram, raising the measured RTT and
// with it how long a closed connection's entries are retained.
type slowReadConn struct {
	net.PacketConn
	delay time.Duration
}

func (c slowReadConn) ReadFrom(b []byte) (int, net.Addr, error) {
	n, addr, err := c.PacketConn.ReadFrom(b)
	if err == nil {
		time.Sleep(c.delay)
	}
	return n, addr, err
}

// A dialed connection's reservation is held while the transport retains its
// closed connection IDs, whether it or the peer closed it, and is released
// only when they are deleted, even when the transport closes first.
func TestDialSetupAdmissionClosedRetention(t *testing.T) {
	for _, local := range []bool{true, false} {
		name := "remote_close"
		if local {
			name = "local_close"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			budget := newSetupBudget(1, 1, time.Minute)
			dials := &correlatedDials{budget: budget}
			tr := &Transport{Conn: slowReadConn{PacketConn: newUDPConnLocalhost(t), delay: 50 * time.Millisecond}}
			require.NoError(t, configureDialSetupAdmission(t, tr, dials.acquire))
			retained := requireReleasedAfterRetirement(t, budget, tr)
			ln, err := (&Transport{Conn: newUDPConnLocalhost(t)}).Listen(testdata.GetTLSConfig(), nil)
			require.NoError(t, err)
			defer ln.Close()

			client, err := tr.Dial(ctx, ln.Addr(), candidateClientTLS(), nil)
			require.NoError(t, err)
			server, err := ln.Accept(ctx)
			require.NoError(t, err)
			if local {
				require.NoError(t, client.CloseWithError(0, ""))
			} else {
				require.NoError(t, server.CloseWithError(0, ""))
				<-client.Context().Done()
			}
			tr.mutex.Lock()
			closedEntries := len(tr.handlers)
			tr.mutex.Unlock()
			require.Positive(t, closedEntries, "the closed connection's IDs are retained")
			require.Equal(t, 1, budget.state(t).held, "retained closed state keeps its reservation")

			require.NoError(t, tr.Close())
			budget.requireSettlement(t, false)
			require.Zero(t, <-retained, "released while closed connection state was retained")
			state := budget.state(t)
			require.Equal(t, []bool{false}, state.settlements)
			require.Zero(t, state.held)
		})
	}
}

// The deadline is absolute: neither the claim nor traffic extend it, and
// reaching it before transfer closes the dialed connection with an ordinary
// close.
func TestDialSetupAdmissionAbsoluteDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	const ttl = 700 * time.Millisecond
	budget := newSetupBudget(1, 1, ttl)
	tr, _ := newDialSetupTransport(t, budget)
	ln, err := (&Transport{Conn: newUDPConnLocalhost(t)}).Listen(testdata.GetTLSConfig(), nil)
	require.NoError(t, err)
	defer ln.Close()
	client, err := tr.Dial(ctx, ln.Addr(), candidateClientTLS(), nil)
	require.NoError(t, err)
	server, err := ln.Accept(ctx)
	require.NoError(t, err)

	// Keep reliable traffic flowing on the dialed connection until it closes.
	trafficErr := make(chan error, 1)
	go func() {
		for {
			str, err := client.OpenUniStream()
			if err == nil {
				_, err = str.Write([]byte("setup traffic"))
			}
			if err == nil {
				err = str.Close()
			}
			if err != nil {
				trafficErr <- err
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	for {
		str, err := server.AcceptUniStream(ctx)
		if err != nil {
			break
		}
		if _, err := io.ReadAll(str); err != nil {
			break
		}
	}
	closedAt := time.Now()
	require.Error(t, <-trafficErr)
	budget.requireSettlement(t, false)

	state := budget.state(t)
	require.Len(t, state.deadlines, 1)
	require.False(t, closedAt.Before(state.deadlines[0]), "the dialed setup closed before its deadline")
	require.Less(t, closedAt.Sub(state.deadlines[0]), ttl, "traffic must not renew the deadline")
	requireLocalRefusal(t, context.Cause(client.Context()))
	requireRefused(t, context.Cause(server.Context()))
	require.ErrorIs(t, transferSetup(t, client), errSetupReservationEnded)
	require.Zero(t, state.held)
}
