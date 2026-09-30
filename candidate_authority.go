package quic

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/quic-go/quic-go/internal/qerr"
)

var (
	errCandidateFenced  = errors.New("quic: candidate group no longer admits connections")
	errCandidateRevoked = errors.New("quic: candidate authority revoked")
)

// Connection authority states. Only candidateGroup transitions them.
const (
	authorityLive    uint32 = iota // ordinary work
	authorityClosing               // only the standard close submission
	authorityRevoked               // no further effects
)

// connAuthority is the opaque token minted for a connection on a group
// transport. Its CID entries and queued work carry it, so ownership never
// depends on decrypting input. A nil token is the ordinary, ungrouped profile.
type connAuthority struct {
	group *candidateGroup
	conn  *Conn
	state atomic.Uint32
	// closed receives the outcome of a close requested by a group transition.
	closed chan error
	// done closes after the connection's terminal close handling or cleanup.
	// It is fork-owned: an application-cancelled context does not end a
	// connection, so it cannot prove that no close is owed.
	done       chan struct{}
	terminated atomic.Bool
}

// permitsWork admits ordinary input and output. After a transition only the
// winner's token keeps it, including tokens retained by closed CID entries.
func (a *connAuthority) permitsWork() bool {
	return a == nil || (a.state.Load() == authorityLive && a.group.retains(a))
}

// permitsClose admits a close submission or retransmission: close-only
// authority survives fencing until revocation.
func (a *connAuthority) permitsClose() bool {
	if a == nil {
		return true
	}
	switch a.state.Load() {
	case authorityClosing:
		return true
	case authorityLive:
		return a.group.retains(a)
	}
	return false
}

// reportClose publishes the terminal close outcome awaited by a group
// transition. nil means no close was owed or it was submitted to the socket.
func (a *connAuthority) reportClose(err error) {
	if a == nil || a.state.Load() != authorityClosing {
		return
	}
	select {
	case a.closed <- err:
	default:
	}
}

// release removes a terminated connection from the group. Retained closed-CID
// entries keep the token and expire under the ordinary closing timers.
// It runs after any close outcome was published.
func (a *connAuthority) release() {
	if a == nil || !a.terminated.CompareAndSwap(false, true) {
		return
	}
	a.group.mu.Lock()
	delete(a.group.candidates, a)
	a.group.mu.Unlock()
	close(a.done)
}

// awaitClose waits for socket submission, never for delivery. A published
// outcome is authoritative over termination and cancellation.
func (a *connAuthority) awaitClose(ctx context.Context) error {
	select {
	case err := <-a.closed:
		return err
	case <-a.done:
	case <-ctx.Done():
	}
	select {
	case err := <-a.closed:
		return err
	case <-a.done:
		return nil // terminated before the transition could owe a close
	default:
		return &candidateCloseError{err: context.Cause(ctx)}
	}
}

// candidateCloseError reports a close that was not submitted. It wraps the
// socket or context error; NoUsablePathV1 distinguishes network-policy denial.
type candidateCloseError struct {
	noPath bool
	err    error
}

func candidateCloseFailure(err error) error {
	return &candidateCloseError{noPath: errors.Is(err, errNetworkAdmissionDenied), err: err}
}

func (e *candidateCloseError) Error() string {
	if e.noPath {
		return fmt.Sprintf("quic: candidate close not submitted, no usable path: %s", e.err)
	}
	return fmt.Sprintf("quic: candidate close not submitted: %s", e.err)
}

func (e *candidateCloseError) Unwrap() error { return e.err }

// NoUsablePathV1 reports that network admission denied the close, so no
// socket write was attempted.
func (e *candidateCloseError) NoUsablePathV1() bool { return e.noPath }

type candidateGroupState uint8

const (
	candidateGroupSetup candidateGroupState = iota
	candidateGroupSelected
	candidateGroupClosed
)

// candidateGroup owns the setup/selected/closing/revoked transitions for every
// connection on its transports. Callers request transitions; they never hold
// or duplicate this state.
type candidateGroup struct {
	op chan struct{} // serializes transitions under the caller's context

	mu         sync.Mutex
	state      candidateGroupState
	candidates map[*connAuthority]struct{}
	// winner is published before fenced; nil when closed without one.
	winner atomic.Pointer[connAuthority]
	// fenced is read on packet paths: once set, group transports create no
	// connections and send no responses on behalf of unknown connection IDs.
	fenced atomic.Bool
}

// CandidateGroupV1 creates a candidate-authority group spanning the
// transports passed to join. The receiver only provides discovery; creating
// a group has no networking effect. Every result uses standard or upstream
// QUIC types.
//
// join adds a transport before its initialization. The transport must already
// have ConfigureNetworkAdmissionV1, and may belong to only one group. Every
// connection it then dials or accepts is a candidate.
//
// selectWinner is one transition across all joined transports. It keeps
// winner, stops new connections and responses for unknown connection IDs,
// discards every other candidate's queued non-close work, submits one standard
// application close (code 0) for each of them, then revokes them. Late input
// for revoked connections is dropped without a stateless reset, and cannot
// affect winner. The wait for each close ends when its socket write returns,
// when the connection owed none, or when ctx is done; delivery is never awaited. A nil error means every owed close
// was submitted. Otherwise the joined error has one entry per unsubmitted
// close: an I/O failure wraps the socket error, cancellation wraps the context
// cause, and an entry whose NoUsablePathV1 method reports true was denied by
// network admission. Authority is checked when each queued work item or
// response starts; revocation stops those that have not started, while one
// already running, including its segment fallback, may complete.
//
// closeGroup applies the same close-and-revoke transition to every candidate
// when no winner has been selected, and is a no-op afterwards. A transition
// happens once; a later selectWinner fails without effect.
func (t *Transport) CandidateGroupV1() (join func(*Transport) error, selectWinner func(context.Context, *Conn) error, closeGroup func(context.Context) error) {
	g := &candidateGroup{op: make(chan struct{}, 1), candidates: make(map[*connAuthority]struct{})}
	return g.join, g.selectWinner, g.closeGroup
}

func (g *candidateGroup) join(t *Transport) error {
	if t == nil {
		return errors.New("quic: candidate group requires a transport")
	}
	t.packetIO.mutex.Lock()
	defer t.packetIO.mutex.Unlock()
	if t.packetIO.started {
		return errors.New("quic: candidate group join after initialization")
	}
	if t.candidates != nil {
		return errors.New("quic: transport already belongs to a candidate group")
	}
	if t.policyConn.network == nil {
		return errors.New("quic: candidate group requires network admission")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.state != candidateGroupSetup {
		return errors.New("quic: candidate group already transitioned")
	}
	t.candidates = g
	return nil
}

// mint registers conn before it is published or run.
func (g *candidateGroup) mint(conn *Conn) (*connAuthority, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.fenced.Load() {
		return nil, errCandidateFenced
	}
	a := &connAuthority{group: g, conn: conn, closed: make(chan error, 1), done: make(chan struct{})}
	g.candidates[a] = struct{}{}
	return a, nil
}

func (g *candidateGroup) acquire(ctx context.Context) error {
	select {
	case g.op <- struct{}{}:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

func (g *candidateGroup) selectWinner(ctx context.Context, winner *Conn) error {
	if winner == nil {
		return errors.New("quic: candidate selection requires a winner")
	}
	if err := g.acquire(ctx); err != nil {
		return err
	}
	defer func() { <-g.op }()
	g.mu.Lock()
	if g.state != candidateGroupSetup {
		g.mu.Unlock()
		return errors.New("quic: candidate group already transitioned")
	}
	wa := winner.authority
	if _, ok := g.candidates[wa]; !ok {
		g.mu.Unlock()
		return errors.New("quic: winner is not a live candidate of this group")
	}
	if wa.terminated.Load() {
		g.mu.Unlock()
		return errors.New("quic: winner has terminated")
	}
	g.state = candidateGroupSelected
	losers := g.fenceLocked(wa)
	g.mu.Unlock()
	return closeCandidates(ctx, losers)
}

func (g *candidateGroup) closeGroup(ctx context.Context) error {
	if err := g.acquire(ctx); err != nil {
		return err
	}
	defer func() { <-g.op }()
	g.mu.Lock()
	if g.state != candidateGroupSetup {
		g.mu.Unlock()
		return nil
	}
	g.state = candidateGroupClosed
	losers := g.fenceLocked(nil)
	g.mu.Unlock()
	return closeCandidates(ctx, losers)
}

// fenceLocked stops new candidates and restricts every candidate except keep
// to its close. The caller holds mu, so no connection can be minted between.
// Losers become close-only and the winner is published before transport
// paths observe the fence.
func (g *candidateGroup) fenceLocked(keep *connAuthority) []*connAuthority {
	losers := make([]*connAuthority, 0, len(g.candidates))
	for a := range g.candidates {
		if a != keep {
			a.state.Store(authorityClosing)
			losers = append(losers, a)
		}
	}
	g.winner.Store(keep)
	g.fenced.Store(true)
	return losers
}

// retains reports whether a live token keeps authority: every token before a
// transition, only the winner's afterwards. Tokens of connections released
// earlier stay governed while their closed CID entries expire.
func (g *candidateGroup) retains(a *connAuthority) bool {
	return g == nil || !g.fenced.Load() || g.winner.Load() == a
}

// closeCandidates requests each standard close, awaits submission under ctx,
// then revokes every loser whether or not its close was submitted.
func closeCandidates(ctx context.Context, losers []*connAuthority) error {
	for _, a := range losers {
		a.conn.closeLocal(&qerr.ApplicationError{})
	}
	errs := make([]error, 0, len(losers))
	for _, a := range losers {
		errs = append(errs, a.awaitClose(ctx))
	}
	for _, a := range losers {
		a.state.Store(authorityRevoked)
	}
	return errors.Join(errs...)
}

// bindCandidate mints the connection's authority before it is registered,
// published or run. Its send worker and close submission share the token.
func (c *Conn) bindCandidate(g *candidateGroup) error {
	if g == nil {
		return nil // the ordinary profile has no token
	}
	a, err := g.mint(c)
	if err != nil {
		return err
	}
	c.authority = a
	c.emission.bindAuthority(a)
	return nil
}

func (e *packetEmission) bindAuthority(a *connAuthority) {
	e.authority = a
	if q, ok := e.queue.(*sendQueue); ok {
		q.authority = a
	}
}

func (g *candidateGroup) isFenced() bool { return g != nil && g.fenced.Load() }

// handlerAuthority returns the token carried by a connection-ID entry.
func handlerAuthority(h packetHandler) *connAuthority {
	switch h := h.(type) {
	case *wrappedConn:
		return h.authority
	case *Conn:
		return h.authority
	case *closedLocalConn:
		return h.authority
	case *closedRemoteConn:
		return h.authority
	}
	return nil
}

func withAuthority(h packetHandler, a *connAuthority) packetHandler {
	switch h := h.(type) {
	case *closedLocalConn:
		h.authority = a
	case *closedRemoteConn:
		h.authority = a
	}
	return h
}
