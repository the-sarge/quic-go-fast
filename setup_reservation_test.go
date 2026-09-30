package quic

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/internal/testdata"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/quic-go/quic-go/testutils/events"

	"github.com/stretchr/testify/require"
)

// Consumers negotiate setup admission with standard and upstream QUIC types only.
type setupAdmissionV1 interface {
	ConfigureSetupAdmissionV1(acquire func(remote netip.AddrPort) (deadline time.Time, complete func() bool, claim func(), release func(transferred bool), ok bool)) error
}

type setupTransferV1 interface {
	TransferSetupReservationV1() error
}

// setupBudget is a caller budget shared across transports. It records every
// stage change and settlement so tests observe the fork's lifecycle calls.
type setupBudget struct {
	ttl       time.Duration
	onRelease func() // observes state at settlement, before the budget changes

	mu             sync.Mutex
	limit          int // total reservations
	completedLimit int // completed-unclaimed reservations
	held           int
	completed      int
	claimed        int
	acquired       int
	rejected       int
	admitted       []netip.AddrPort
	settlements    []bool // transferred flag per release
	deadlines      []time.Time
	violations     []string
	settled        chan bool
}

func newSetupBudget(limit, completedLimit int, ttl time.Duration) *setupBudget {
	return &setupBudget{limit: limit, completedLimit: completedLimit, ttl: ttl, settled: make(chan bool, 16)}
}

func (b *setupBudget) acquire(remote netip.AddrPort) (time.Time, func() bool, func(), func(bool), bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.held >= b.limit {
		b.rejected++
		return time.Time{}, nil, nil, nil, false
	}
	b.held++
	b.acquired++
	b.admitted = append(b.admitted, remote)
	const (
		handshaking = iota
		completed
		claimed
		settled
	)
	stage := handshaking
	complete := func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		if stage != handshaking {
			b.violations = append(b.violations, fmt.Sprintf("complete in stage %d", stage))
			return false
		}
		if b.completed >= b.completedLimit {
			return false
		}
		b.completed++
		stage = completed
		return true
	}
	claim := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if stage != completed {
			b.violations = append(b.violations, fmt.Sprintf("claim in stage %d", stage))
			return
		}
		b.completed--
		b.claimed++
		stage = claimed
	}
	release := func(transferred bool) {
		if b.onRelease != nil {
			b.onRelease()
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		switch stage {
		case settled:
			b.violations = append(b.violations, "second release")
			return
		case completed:
			b.completed--
		case claimed:
			b.claimed--
		}
		stage = settled
		b.held--
		b.settlements = append(b.settlements, transferred)
		b.settled <- transferred
	}
	deadline := time.Now().Add(b.ttl)
	b.deadlines = append(b.deadlines, deadline)
	return deadline, complete, claim, release, true
}

type setupBudgetState struct {
	held, completed, claimed, acquired, rejected int
	admitted                                     []netip.AddrPort
	settlements                                  []bool
	deadlines                                    []time.Time
}

func (b *setupBudget) state(t *testing.T) setupBudgetState {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	require.Empty(t, b.violations)
	return setupBudgetState{
		held: b.held, completed: b.completed, claimed: b.claimed,
		acquired: b.acquired, rejected: b.rejected,
		admitted:    append([]netip.AddrPort(nil), b.admitted...),
		settlements: append([]bool(nil), b.settlements...),
		deadlines:   append([]time.Time(nil), b.deadlines...),
	}
}

func (b *setupBudget) requireSettlement(t *testing.T, transferred bool) {
	t.Helper()
	select {
	case got := <-b.settled:
		require.Equal(t, transferred, got)
	case <-time.After(5 * time.Second):
		t.Fatal("reservation was not settled")
	}
}

func configureSetupAdmission(t *testing.T, tr *Transport, acquire func(netip.AddrPort) (time.Time, func() bool, func(), func(bool), bool)) error {
	t.Helper()
	api, ok := any(tr).(setupAdmissionV1)
	require.True(t, ok, "transport must expose a standard-type setup admission capability")
	return api.ConfigureSetupAdmissionV1(acquire)
}

func transferSetup(t *testing.T, conn *Conn) error {
	t.Helper()
	api, ok := any(conn).(setupTransferV1)
	require.True(t, ok, "connection must expose a setup transfer capability")
	return api.TransferSetupReservationV1()
}

// countServerConstructors observes the real server connection constructor,
// which precedes every per-connection worker.
func countServerConstructors(t *testing.T) *atomic.Int32 {
	t.Helper()
	var n atomic.Int32
	orig := newConnection
	newConnection = func(
		ctx context.Context,
		ctxCancel context.CancelCauseFunc,
		conn sendConn,
		runner connRunner,
		origDestConnID protocol.ConnectionID,
		retrySrcConnID *protocol.ConnectionID,
		clientDestConnID protocol.ConnectionID,
		destConnID protocol.ConnectionID,
		srcConnID protocol.ConnectionID,
		connIDGenerator ConnectionIDGenerator,
		statelessResetter *statelessResetter,
		conf *Config,
		tlsConf *tls.Config,
		tokenGenerator *handshake.TokenGenerator,
		clientAddressValidated bool,
		rtt time.Duration,
		qlogTrace qlogwriter.Trace,
		logger utils.Logger,
		v protocol.Version,
	) *wrappedConn {
		n.Add(1)
		return orig(ctx, ctxCancel, conn, runner, origDestConnID, retrySrcConnID, clientDestConnID, destConnID, srcConnID, connIDGenerator, statelessResetter, conf, tlsConf, tokenGenerator, clientAddressValidated, rtt, qlogTrace, logger, v)
	}
	t.Cleanup(func() { newConnection = orig })
	return &n
}

func newSetupTransport(t *testing.T, budget *setupBudget) *Transport {
	t.Helper()
	tr := &Transport{Conn: newUDPConnLocalhost(t), ConnectionIDLength: 8}
	require.NoError(t, configureSetupAdmission(t, tr, budget.acquire))
	t.Cleanup(func() { tr.Close() })
	return tr
}

func dialSetup(t *testing.T, ctx context.Context, ln *Listener) (*Conn, error) {
	t.Helper()
	clientTransport := &Transport{Conn: newUDPConnLocalhost(t)}
	t.Cleanup(func() { clientTransport.Close() })
	return clientTransport.Dial(ctx, ln.Addr(), candidateClientTLS(), nil)
}

// endpoint normalizes a connection address the way admission reports it.
func endpoint(t *testing.T, addr net.Addr) netip.AddrPort {
	t.Helper()
	ep, ok := packetEndpoint(addr)
	require.True(t, ok, "not a UDP endpoint: %v", addr)
	return ep
}

// clientOf pairs an accepted connection with the client that dialed it.
func clientOf(t *testing.T, server *Conn, clients ...*Conn) *Conn {
	t.Helper()
	remote := endpoint(t, server.RemoteAddr())
	var match *Conn
	for _, c := range clients {
		if endpoint(t, c.LocalAddr()) == remote {
			require.Nil(t, match, "clients share %s", remote)
			match = c
		}
	}
	require.NotNil(t, match, "no client dialed from %s", remote)
	return match
}

func requireRefused(t *testing.T, err error) {
	t.Helper()
	var transportErr *TransportError
	require.ErrorAs(t, err, &transportErr)
	require.Equal(t, ConnectionRefused, transportErr.ErrorCode)
	require.True(t, transportErr.Remote)
}

// A saturated budget refuses the Initial before any connection is constructed.
func TestSetupAdmissionRejectsBeforeConstruction(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	constructed := countServerConstructors(t)
	budget := newSetupBudget(0, 1, time.Minute)
	ln, err := newSetupTransport(t, budget).Listen(testdata.GetTLSConfig(), nil)
	require.NoError(t, err)

	_, err = dialSetup(t, ctx, ln)
	requireRefused(t, err)
	require.Zero(t, constructed.Load(), "rejected admission must precede allocation")
	state := budget.state(t)
	require.Positive(t, state.rejected)
	require.Zero(t, state.acquired)
}

func TestSetupAdmissionContract(t *testing.T) {
	budget := newSetupBudget(1, 1, time.Minute)
	tr := &Transport{Conn: newUDPConnLocalhost(t)}
	defer tr.Close()
	require.Error(t, configureSetupAdmission(t, tr, nil))
	require.NoError(t, configureSetupAdmission(t, tr, budget.acquire))
	require.Error(t, configureSetupAdmission(t, tr, budget.acquire), "configuration happens once")
	initialized := &Transport{Conn: newUDPConnLocalhost(t)}
	defer initialized.Close()
	ln, err := initialized.Listen(testdata.GetTLSConfig(), nil)
	require.NoError(t, err)
	require.Error(t, configureSetupAdmission(t, initialized, budget.acquire), "configuration precedes initialization")

	// An unconfigured server keeps ordinary behavior and has no reservation.
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	client, server := dialCandidatePair(t, ctx, ln)
	exchangeAdmissionStream(t, ctx, client, server)
	require.Error(t, transferSetup(t, server), "an unreserved connection has nothing to transfer")
	require.Error(t, transferSetup(t, client))
	require.Zero(t, budget.state(t).acquired)
}

// A saturated completed stage refuses the connection without releasing and
// reacquiring its total capacity; the release follows teardown.
func TestSetupAdmissionStageSaturation(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	constructed := countServerConstructors(t)
	budget := newSetupBudget(1, 0, time.Minute)
	ln, err := newSetupTransport(t, budget).Listen(testdata.GetTLSConfig(), nil)
	require.NoError(t, err)

	client, err := dialSetup(t, ctx, ln)
	if err == nil {
		<-client.Context().Done()
		err = context.Cause(client.Context())
	}
	requireRefused(t, err)
	budget.requireSettlement(t, false)
	acceptCtx, cancelAccept := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancelAccept()
	_, err = ln.Accept(acceptCtx)
	require.ErrorIs(t, err, context.DeadlineExceeded, "a refused connection is never queued")
	require.EqualValues(t, 1, constructed.Load())
	state := budget.state(t)
	require.Equal(t, 1, state.acquired, "a stage change never reacquires capacity")
	require.Equal(t, []bool{false}, state.settlements)
	require.Zero(t, state.held)
}

func (b *setupBudget) waitState(t *testing.T, cond func(setupBudgetState) bool, msg string) setupBudgetState {
	t.Helper()
	var state setupBudgetState
	require.Eventually(t, func() bool { state = b.state(t); return cond(state) }, 5*time.Second, time.Millisecond, msg)
	return state
}

// Acceptance keeps the reservation in place; the application either transfers
// it, which stops the deadline, or closes the connection, which releases it
// after teardown.
func TestSetupAdmissionAcceptedStage(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	budget := newSetupBudget(2, 2, 500*time.Millisecond)
	ln, err := newSetupTransport(t, budget).Listen(testdata.GetTLSConfig(), nil)
	require.NoError(t, err)
	clientA, err := dialSetup(t, ctx, ln)
	require.NoError(t, err)
	clientB, err := dialSetup(t, ctx, ln)
	require.NoError(t, err)
	budget.waitState(t, func(s setupBudgetState) bool { return s.completed == 2 }, "both handshakes reach the accept queue")

	// Accept follows handshake completion, not dial order.
	transferred, err := ln.Accept(ctx)
	require.NoError(t, err)
	peer := clientOf(t, transferred, clientA, clientB)
	state := budget.state(t)
	require.Equal(t, [3]int{2, 1, 1}, [3]int{state.held, state.completed, state.claimed}, "acceptance moves the stage without a capacity gap")
	require.NoError(t, transferSetup(t, transferred))
	budget.requireSettlement(t, true)
	require.ErrorIs(t, transferSetup(t, transferred), errSetupReservationEnded)

	canceled, err := ln.Accept(ctx)
	require.NoError(t, err)
	require.NotSame(t, peer, clientOf(t, canceled, clientA, clientB))
	require.NoError(t, canceled.CloseWithError(0, ""))
	budget.requireSettlement(t, false)
	require.ErrorIs(t, transferSetup(t, canceled), errSetupReservationEnded)

	// The transferred connection outlives its former setup deadline.
	time.Sleep(600 * time.Millisecond)
	exchangeAdmissionStream(t, ctx, peer, transferred)
	state = budget.state(t)
	require.Equal(t, 2, state.acquired)
	require.Zero(t, state.held)
	require.ElementsMatch(t, []bool{true, false}, state.settlements)
}

// Listener close refuses incomplete handshakes and releases them after
// teardown; completed candidates remain reserved for Accept to drain.
func TestSetupAdmissionListenerClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	constructed := countServerConstructors(t)
	budget := newSetupBudget(2, 1, time.Minute)
	ln, err := newSetupTransport(t, budget).Listen(testdata.GetTLSConfig(), nil)
	require.NoError(t, err)
	_, err = dialSetup(t, ctx, ln)
	require.NoError(t, err)
	budget.waitState(t, func(s setupBudgetState) bool { return s.completed == 1 }, "completed candidate is queued")

	// An Initial without a ClientHello leaves its connection mid-handshake.
	raw := newUDPConnLocalhost(t)
	_, err = raw.WriteTo(getValidInitialPacket(t, raw.LocalAddr(), randConnID(5), randConnID(8)).data, ln.Addr())
	require.NoError(t, err)
	require.Eventually(t, func() bool { return constructed.Load() == 2 }, 5*time.Second, time.Millisecond)
	require.Equal(t, 2, budget.state(t).held)

	require.NoError(t, ln.Close())
	budget.requireSettlement(t, false)
	state := budget.state(t)
	require.Equal(t, [2]int{1, 1}, [2]int{state.held, state.completed}, "the queued candidate stays reserved")
	queued, err := ln.Accept(ctx)
	require.NoError(t, err)
	require.NoError(t, transferSetup(t, queued))
	budget.requireSettlement(t, true)
	require.Zero(t, budget.state(t).held)
}

// The deadline is absolute: neither stage changes nor traffic extend it, and
// reaching it tears the connection down with an ordinary close, whether the
// handshake is incomplete or the connection was accepted but not transferred.
func TestSetupAdmissionAbsoluteDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	const ttl = 700 * time.Millisecond
	budget := newSetupBudget(2, 1, ttl)
	// Only the setup deadline, not the handshake timeout, can end the
	// incomplete candidate within the settlement wait.
	ln, err := newSetupTransport(t, budget).Listen(testdata.GetTLSConfig(), &Config{HandshakeIdleTimeout: time.Minute})
	require.NoError(t, err)
	raw := newUDPConnLocalhost(t)
	_, err = raw.WriteTo(getValidInitialPacket(t, raw.LocalAddr(), randConnID(5), randConnID(8)).data, ln.Addr())
	require.NoError(t, err)
	budget.waitState(t, func(s setupBudgetState) bool { return s.acquired == 1 }, "incomplete candidate is admitted")
	peer, err := dialSetup(t, ctx, ln)
	require.NoError(t, err)
	server, err := ln.Accept(ctx)
	require.NoError(t, err)

	// Keep reliable traffic flowing in both directions of the accepted setup.
	trafficErr := make(chan error, 1)
	go func() {
		for {
			str, err := peer.OpenUniStream()
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
	budget.requireSettlement(t, false)

	state := budget.state(t)
	require.Len(t, state.deadlines, 2)
	require.False(t, closedAt.Before(state.deadlines[1]), "the accepted setup closed before its deadline")
	require.Less(t, closedAt.Sub(state.deadlines[1]), ttl, "traffic must not renew the deadline")
	<-peer.Context().Done()
	requireRefused(t, context.Cause(peer.Context()))
	require.ErrorIs(t, transferSetup(t, server), errSetupReservationEnded)
	require.Zero(t, state.held)
}

// Every failure after admission settles the reservation exactly once, after
// the work that was started has ended.
func TestSetupAdmissionFailureRelease(t *testing.T) {
	t.Run("before_construction", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		constructed := countServerConstructors(t)
		budget := newSetupBudget(1, 1, time.Minute)
		tr := newSetupTransport(t, budget)
		tr.ConnContext = func(context.Context, *ClientInfo) (context.Context, error) {
			return nil, errors.New("application refused")
		}
		ln, err := tr.Listen(testdata.GetTLSConfig(), nil)
		require.NoError(t, err)
		_, err = dialSetup(t, ctx, ln)
		requireRefused(t, err)
		budget.requireSettlement(t, false)
		require.Zero(t, constructed.Load())
	})

	t.Run("registration", func(t *testing.T) {
		constructed := countServerConstructors(t)
		budget := newSetupBudget(1, 1, time.Minute)
		tr := newCandidateTransport(t, newUDPConnLocalhost(t))
		require.NoError(t, configureSetupAdmission(t, tr, budget.acquire))
		group := newCandidateGroup(t, tr)
		require.NoError(t, group.join(tr))
		// The group closes after admission and before mint, so the Initial
		// passes the server's fence check, reaches construction, and is then
		// rejected before registration.
		fenced := make(chan error, 1)
		tr.ConnContext = func(ctx context.Context, _ *ClientInfo) (context.Context, error) {
			fenced <- group.closeGroup(ctx)
			return ctx, nil
		}
		ln, err := tr.Listen(testdata.GetTLSConfig(), nil)
		require.NoError(t, err)
		raw := newUDPConnLocalhost(t)
		ln.baseServer.handlePacket(getValidInitialPacket(t, raw.LocalAddr(), randConnID(5), randConnID(8)))
		budget.requireSettlement(t, false)
		require.NoError(t, <-fenced)
		require.EqualValues(t, 1, constructed.Load())
		state := budget.state(t)
		require.Equal(t, []bool{false}, state.settlements)
		require.Zero(t, state.held)
	})

	t.Run("tls", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		budget := newSetupBudget(1, 1, time.Minute)
		tr := newSetupTransport(t, budget)
		// The failed handshake's local close leaves closed entries on the
		// transport; its capacity stays reserved until they are retired.
		retained := make(chan int, 1)
		budget.onRelease = func() {
			tr.mutex.Lock()
			retained <- len(tr.handlers)
			tr.mutex.Unlock()
		}
		ln, err := tr.Listen(testdata.GetTLSConfig(), nil)
		require.NoError(t, err)
		clientTransport := &Transport{Conn: newUDPConnLocalhost(t)}
		defer clientTransport.Close()
		tlsConf := candidateClientTLS()
		tlsConf.NextProtos = []string{"unsupported"}
		_, err = clientTransport.Dial(ctx, ln.Addr(), tlsConf, nil)
		require.Error(t, err)
		budget.requireSettlement(t, false)
		require.Zero(t, <-retained, "released while closed connection state was retained")
		state := budget.state(t)
		require.Equal(t, []bool{false}, state.settlements)
		require.Zero(t, state.held)
	})
}

// One caller budget bounds the total across listeners on separate transports.
func TestSetupAdmissionTwoListenerTotal(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	constructed := countServerConstructors(t)
	budget := newSetupBudget(1, 1, time.Minute)
	lnA, err := newSetupTransport(t, budget).Listen(testdata.GetTLSConfig(), nil)
	require.NoError(t, err)
	lnB, err := newSetupTransport(t, budget).Listen(testdata.GetTLSConfig(), nil)
	require.NoError(t, err)

	first, err := dialSetup(t, ctx, lnA)
	require.NoError(t, err)
	_, err = dialSetup(t, ctx, lnB)
	requireRefused(t, err)
	require.EqualValues(t, 1, constructed.Load(), "the second listener allocates nothing")

	conn, err := lnA.Accept(ctx)
	require.NoError(t, err)
	require.NoError(t, transferSetup(t, conn))
	budget.requireSettlement(t, true)
	fresh, err := dialSetup(t, ctx, lnB)
	require.NoError(t, err, "transferred capacity is available to every listener")
	require.EqualValues(t, 2, constructed.Load())
	state := budget.state(t)
	require.Equal(t, 2, state.acquired)
	require.Equal(t, []netip.AddrPort{endpoint(t, first.LocalAddr()), endpoint(t, fresh.LocalAddr())}, state.admitted)
	// The refused ClientHello's trailing Initials, whenever they arrive, are
	// refused without taking the transferred capacity.
	require.Equal(t, 1, state.rejected, "a refused attempt reaches admission once")
}

// Stage changes, transfer, the deadline and teardown race for one reservation.
// Every order yields one settlement, no stage change after it, and transfer
// succeeds only from the accepted stage before the deadline or teardown.
func TestSetupAdmissionStageRace(t *testing.T) {
	type op struct {
		name string
		run  func(*setupReservation) error
	}
	ops := []op{
		{"complete", func(r *setupReservation) error { r.completeHandshake(); return nil }},
		{"claim", func(r *setupReservation) error { r.claimAccepted(); return nil }},
		{"transfer", func(r *setupReservation) error { return r.transfer() }},
		{"expire", func(r *setupReservation) error { r.expire(); return nil }},
		{"teardown", func(r *setupReservation) error { r.teardown(); return nil }},
	}
	newReservation := func(t *testing.T) (*setupBudget, *setupReservation) {
		t.Helper()
		budget := newSetupBudget(1, 1, time.Minute)
		r, ok := setupAdmission(budget.acquire).acquire(nil)
		require.True(t, ok)
		r.bind(&Conn{closeChan: make(chan struct{}, 1)})
		return budget, r
	}

	var permute func([]op, int, func([]op))
	permute = func(s []op, k int, visit func([]op)) {
		if k == len(s) {
			visit(s)
			return
		}
		for i := k; i < len(s); i++ {
			s[k], s[i] = s[i], s[k]
			permute(s, k+1, visit)
			s[k], s[i] = s[i], s[k]
		}
	}
	permute(append([]op(nil), ops...), 0, func(order []op) {
		budget, r := newReservation(t)
		var names []string
		var transferErr error
		pos := map[string]int{}
		for i, o := range order {
			names = append(names, o.name)
			pos[o.name] = i
			if err := o.run(r); o.name == "transfer" {
				transferErr = err
			}
		}
		state := budget.state(t)
		wantTransfer := pos["complete"] < pos["claim"] && pos["claim"] < pos["transfer"] &&
			pos["transfer"] < pos["expire"] && pos["transfer"] < pos["teardown"]
		require.Equal(t, []bool{wantTransfer}, state.settlements, "order %v", names)
		require.Equal(t, wantTransfer, transferErr == nil, "order %v", names)
		require.Zero(t, state.held, "order %v", names)
	})

	// A reached deadline is authoritative before its delayed timer callback
	// runs: stage changes and transfer are refused, the ordinary close is
	// requested, and capacity is kept until teardown.
	for _, claimed := range []bool{false, true} {
		budget, r := newReservation(t)
		if claimed {
			require.True(t, r.completeHandshake())
			r.claimAccepted()
		}
		r.timer.Stop()
		r.deadline = time.Now().Add(-time.Millisecond)
		conn := r.conn
		if claimed {
			require.ErrorIs(t, r.transfer(), errSetupReservationEnded)
		} else {
			require.False(t, r.completeHandshake())
		}
		closeErr := conn.closeErr.Load()
		require.NotNil(t, closeErr, "an overdue reservation requests the ordinary close")
		var transportErr *TransportError
		require.ErrorAs(t, closeErr.err, &transportErr)
		require.Equal(t, ConnectionRefused, transportErr.ErrorCode)
		require.Empty(t, budget.state(t).settlements, "capacity is kept until teardown")
		r.teardown()
		require.Equal(t, []bool{false}, budget.state(t).settlements)
	}

	// The same operations concurrently, for the race detector.
	budget, r := newReservation(t)
	errs := make(chan error, len(ops))
	for _, o := range ops {
		go func() { errs <- o.run(r) }()
	}
	for range ops {
		<-errs
	}
	require.Len(t, budget.state(t).settlements, 1)
}

// refusalServer drives Initials into a server one at a time: each Initial's
// CONNECTION_REFUSED is read before the next is sent, so the server goroutine
// has finished with it without any sleep.
type refusalServer struct {
	*testServer
	recorder    *events.Recorder
	conn        *net.UDPConn // the client's socket; every Initial comes from it
	srcID       protocol.ConnectionID
	rcvTime     monotime.Time
	constructed atomic.Int32
}

func newRefusalServer(t *testing.T, opts *serverOpts) *refusalServer {
	t.Helper()
	s := &refusalServer{
		recorder: &events.Recorder{},
		conn:     newUDPConnLocalhost(t),
		srcID:    randConnID(6),
		rcvTime:  monotime.Now(),
	}
	opts.eventRecorder = s.recorder
	// A constructed connection is inert, so a regression fails the assertions
	// rather than the fixture.
	opts.newConn = func(context.Context, context.CancelCauseFunc, sendConn, connRunner,
		protocol.ConnectionID, *protocol.ConnectionID, protocol.ConnectionID, protocol.ConnectionID, protocol.ConnectionID,
		ConnectionIDGenerator, *statelessResetter, *Config, *tls.Config, *handshake.TokenGenerator,
		bool, time.Duration, qlogwriter.Trace, utils.Logger, protocol.Version,
	) *wrappedConn {
		s.constructed.Add(1)
		return &wrappedConn{Conn: &Conn{closeChan: make(chan struct{}, 1)}, testHooks: &connTestHooks{
			run:                     func() error { return nil },
			context:                 context.Background,
			handshakeComplete:       func() <-chan struct{} { return nil },
			handlePacket:            func(p receivedPacket) { p.buffer.Release() },
			closeWithTransportError: func(TransportErrorCode) {},
		}}
	}
	s.testServer = newTestServer(t, opts)
	return s
}

// requireRefusedAt sends an Initial for destID received at the given offset
// from the server's base time and requires CONNECTION_REFUSED in response.
func (s *refusalServer) requireRefusedAt(t *testing.T, destID protocol.ConnectionID, offset time.Duration) {
	t.Helper()
	p := getValidInitialPacket(t, s.conn.LocalAddr(), s.srcID, destID)
	p.rcvTime = s.rcvTime.Add(offset)
	s.handlePacket(p)
	checkConnectionClose(t, s.conn, s.recorder, destID, s.srcID, qerr.ConnectionRefused)
	s.recorder.Clear()
}

func (s *refusalServer) requireRefused(t *testing.T, destID protocol.ConnectionID) {
	t.Helper()
	s.requireRefusedAt(t, destID, 0)
}

// A refused attempt's later Initials are refused without reaching admission,
// even once capacity has been freed.
func TestSetupAdmissionRefusedAttemptStaysRefused(t *testing.T) {
	budget := newSetupBudget(0, 1, time.Minute)
	server := newRefusalServer(t, &serverOpts{setupAdmission: budget.acquire})
	destID := randConnID(8)

	server.requireRefused(t, destID)
	budget.mu.Lock()
	budget.limit = 1 // capacity frees before the trailing datagram arrives
	budget.mu.Unlock()
	server.requireRefused(t, destID)

	state := budget.state(t)
	require.Equal(t, 1, state.rejected, "the refused attempt reaches admission once")
	require.Zero(t, state.acquired)
	require.Zero(t, server.constructed.Load())
}

// An attempt is its Initial destination connection ID, not its address: one
// client socket dials many attempts.
func TestSetupAdmissionRefusalKeyedByAttempt(t *testing.T) {
	budget := newSetupBudget(0, 1, time.Minute)
	server := newRefusalServer(t, &serverOpts{setupAdmission: budget.acquire})

	server.requireRefused(t, randConnID(8))
	server.requireRefused(t, randConnID(8))
	require.Equal(t, 2, budget.state(t).rejected, "a new attempt from the same address reaches admission")
}

// Refusals by the application's callbacks, after admission, are remembered
// like admission refusals: the attempt's later Initials reach no callback.
func TestSetupAdmissionCallbackRefusalRemembered(t *testing.T) {
	refused := errors.New("application refused")
	for _, tc := range []struct {
		name string
		opts func(calls *atomic.Int32) *serverOpts
	}{
		{"GetConfigForClient", func(calls *atomic.Int32) *serverOpts {
			return &serverOpts{config: &Config{GetConfigForClient: func(*ClientInfo) (*Config, error) {
				calls.Add(1)
				return nil, refused
			}}}
		}},
		{"ConnContext", func(calls *atomic.Int32) *serverOpts {
			return &serverOpts{connContext: func(context.Context, *ClientInfo) (context.Context, error) {
				calls.Add(1)
				return nil, refused
			}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			budget := newSetupBudget(1, 1, time.Minute)
			var calls atomic.Int32
			opts := tc.opts(&calls)
			opts.setupAdmission = budget.acquire
			server := newRefusalServer(t, opts)
			destID := randConnID(8)

			server.requireRefused(t, destID)
			server.requireRefused(t, destID)
			require.EqualValues(t, 1, calls.Load())
			state := budget.state(t)
			require.Equal(t, 1, state.acquired)
			require.Equal(t, []bool{false}, state.settlements)
			require.Zero(t, server.constructed.Load())
		})
	}
}

// The memory is bounded: when full, the oldest refusal is forgotten and its
// attempt reaches admission again.
func TestSetupAdmissionRefusalMemoryCapacity(t *testing.T) {
	budget := newSetupBudget(0, 1, time.Minute)
	server := newRefusalServer(t, &serverOpts{setupAdmission: budget.acquire, refusedInitialsCapacity: 2})
	a, b, c := randConnID(8), randConnID(8), randConnID(8)

	server.requireRefused(t, a)
	server.requireRefused(t, b)
	server.requireRefused(t, c) // evicts a
	require.Equal(t, 3, budget.state(t).rejected)
	server.requireRefused(t, b)
	require.Equal(t, 3, budget.state(t).rejected, "b is still remembered")
	server.requireRefused(t, a) // evicts b
	require.Equal(t, 4, budget.state(t).rejected, "the evicted a reaches admission again")
	server.requireRefused(t, b)
	require.Equal(t, 5, budget.state(t).rejected, "the evicted b reaches admission again")
}

// An expired refusal refused again is the newest entry: a full memory evicts
// older refusals before it.
func TestSetupAdmissionRefusalMemoryRenewal(t *testing.T) {
	budget := newSetupBudget(0, 1, time.Minute)
	server := newRefusalServer(t, &serverOpts{setupAdmission: budget.acquire, refusedInitialsCapacity: 2})
	a, b, c := randConnID(8), randConnID(8), randConnID(8)

	server.requireRefusedAt(t, a, 0)
	server.requireRefusedAt(t, b, 4*time.Second)
	server.requireRefusedAt(t, a, refusedInitialTTL)                      // a expired and is refused afresh
	server.requireRefusedAt(t, c, refusedInitialTTL+100*time.Millisecond) // evicts b
	require.Equal(t, 4, budget.state(t).rejected)
	server.requireRefusedAt(t, a, refusedInitialTTL+200*time.Millisecond)
	require.Equal(t, 4, budget.state(t).rejected, "the renewed a is newer than b")
	server.requireRefusedAt(t, b, refusedInitialTTL+300*time.Millisecond)
	require.Equal(t, 5, budget.state(t).rejected, "the evicted b reaches admission again")
	require.Zero(t, server.constructed.Load())
}

// A refusal is remembered for refusedInitialTTL from the refused Initial's
// receive time.
func TestSetupAdmissionRefusalMemoryTTL(t *testing.T) {
	budget := newSetupBudget(0, 1, time.Minute)
	server := newRefusalServer(t, &serverOpts{setupAdmission: budget.acquire})
	destID := randConnID(8)

	server.requireRefusedAt(t, destID, 0)
	server.requireRefusedAt(t, destID, refusedInitialTTL-time.Nanosecond)
	require.Equal(t, 1, budget.state(t).rejected, "remembered within the TTL")
	server.requireRefusedAt(t, destID, refusedInitialTTL)
	require.Equal(t, 2, budget.state(t).rejected, "an expired refusal reaches admission again")
}

// Without setup admission nothing is remembered: the ordinary server consults
// its callbacks for every Initial, as upstream does.
func TestSetupAdmissionUnconfiguredRemembersNothing(t *testing.T) {
	var calls atomic.Int32
	server := newRefusalServer(t, &serverOpts{config: &Config{GetConfigForClient: func(*ClientInfo) (*Config, error) {
		calls.Add(1)
		return nil, errors.New("application refused")
	}}})
	destID := randConnID(8)

	server.requireRefused(t, destID)
	server.requireRefused(t, destID)
	require.EqualValues(t, 2, calls.Load())
	require.Nil(t, server.refusedInitials)
}
