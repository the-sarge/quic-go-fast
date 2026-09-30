package quic

import (
	"container/list"
	"context"
	"errors"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
)

var (
	errNoSetupReservation      = errors.New("quic: connection has no setup reservation")
	errSetupReservationEnded   = errors.New("quic: setup reservation already ended")
	errDialSetupRefused        = errors.New("quic: dial refused by setup admission")
	errDialEarlySetupAdmission = errors.New("quic: DialEarly is not supported with dial setup admission")
)

// setupAdmission is the caller's acquire callback, immutable after configuration.
type setupAdmission func(remote netip.AddrPort) (deadline time.Time, complete func() bool, claim func(), release func(transferred bool), ok bool)

// dialSetupAdmission is the caller's dial acquire callback, immutable after
// configuration.
type dialSetupAdmission func(ctx context.Context, remote netip.AddrPort) (deadline time.Time, complete func() bool, claim func(), release func(transferred bool), ok bool)

type setupStage uint8

const (
	setupHandshaking setupStage = iota // acquired, handshake in progress
	setupCompleted                     // queued for Accept, unclaimed
	setupClaimed                       // returned by Accept or Dial, not transferred
)

// setupReservation is the single owner of one accepted or dialed connection's
// setup capacity, from before its construction until one terminal settlement,
// including across a dial's recreation for version negotiation. The caller's
// callbacks run under mu, so stage changes and the release are serialized and
// nothing follows the release. Server and dial paths only request settlement;
// teardown is idempotent, so no path owns a competing release.
// An untransferred reservation settles once the connection's work has ended
// and every closing state it left on a transport has been retired.
type setupReservation struct {
	mu       sync.Mutex
	complete func() bool
	claim    func()
	release  func(transferred bool)
	deadline time.Time
	stage    setupStage
	expired  bool // the deadline requested teardown
	ended    bool // the connection's work has ended or never started
	closing  int  // closing states still retained by transports
	settled  bool
	timer    *time.Timer
	conn     *Conn // severed on settlement and while detached for recreation
}

// ConfigureSetupAdmissionV1 bounds server connection setup with a caller
// budget. Call it once, before any operation that initializes the transport;
// acquire must be non-nil. Transports without it keep ordinary behavior.
//
// acquire is called on the server's packet goroutine for each Initial that
// would create a connection, after Retry and token handling but before any
// per-connection allocation, application callback or worker. The remote
// endpoint is unmapped; it is the zero value for a non-UDP address. Returning
// ok false refuses the connection with CONNECTION_REFUSED and allocates
// nothing. Otherwise the reservation carries the returned absolute deadline and
// must include a non-nil release, which is the only way the library can return
// that capacity. A deadline not in the future or a nil complete or claim
// refuses the connection and calls release with false.
//
// A client attempt is identified by the destination connection ID of its
// Initials. Once an attempt is refused before construction, by acquire or by
// an error from GetConfigForClient or ConnContext, its later Initials that pass
// Retry and token handling, such as the rest of a ClientHello spanning several
// datagrams, are refused with CONNECTION_REFUSED without calling acquire,
// GetConfigForClient or ConnContext, so acquire sees that attempt once. A
// refusal is remembered for 5 seconds from the refused Initial's receipt, among
// the 1024 most recently refused attempts; an attempt forgotten earlier reaches
// acquire again.
//
// A reservation is held, without release and reacquisition, through the
// handshake, the completed-unclaimed accept queue and acceptance:
//   - complete moves it to the accept queue when the handshake completes (or,
//     for ListenEarly, when the connection is ready). False refuses the
//     connection with CONNECTION_REFUSED; the reservation stays in its stage
//     until that teardown ends.
//   - claim reports that Accept returned the connection.
//   - release is called exactly once: with true when the application calls
//     TransferSetupReservationV1 on the accepted connection, otherwise with
//     false after the connection's teardown has finished, including its
//     closing state, or when construction or registration fails.
//
// Reaching the deadline before transfer closes the connection with
// CONNECTION_REFUSED under ordinary bounded close handling. The deadline is
// authoritative when reached, even before that close runs: later stage changes
// and transfer are refused. Stage changes and traffic never extend it. Callbacks may be shared across
// transports, must be concurrency-safe and return promptly, and must not call
// into the transport or its connections.
func (t *Transport) ConfigureSetupAdmissionV1(acquire func(remote netip.AddrPort) (deadline time.Time, complete func() bool, claim func(), release func(transferred bool), ok bool)) error {
	t.packetIO.mutex.Lock()
	defer t.packetIO.mutex.Unlock()
	if t.packetIO.started {
		return errors.New("quic: setup admission configuration after initialization")
	}
	if t.setupAdmission != nil {
		return errors.New("quic: setup admission already configured")
	}
	if acquire == nil {
		return errors.New("quic: setup admission requires an acquire callback")
	}
	t.setupAdmission = acquire
	return nil
}

// ConfigureDialSetupAdmissionV1 bounds the setup of connections dialed on the
// transport with a caller budget, through the same reservation lifecycle as
// ConfigureSetupAdmissionV1, which remains a separate configuration for
// accepted connections. Call it once, before any operation that initializes
// the transport; acquire must be non-nil. Transports without it keep ordinary
// dial behavior.
//
// Dial calls acquire with its context before generating connection IDs,
// constructing the connection or sending any packet, so a caller running
// concurrent dials correlates each reservation with its own dial through
// values it put in that context. The remote endpoint is unmapped; it is the
// zero value for a non-UDP address. Returning ok false fails the dial and
// allocates nothing. Otherwise the reservation carries the returned absolute
// deadline and must include a non-nil release. A deadline not in the future
// or a nil complete or claim fails the dial and calls release with false.
// DialEarly fails before calling acquire, because a 0-RTT return precedes
// handshake completion.
//
// A reservation is held, without release and reacquisition, through the
// handshake, including a connection recreated for version negotiation, and
// the return from Dial:
//   - complete is called when the handshake completes, before Dial returns.
//     False closes the connection with CONNECTION_REFUSED and fails the dial;
//     the reservation stays in its stage until that teardown ends.
//   - claim reports that Dial is returning the connection. It immediately
//     follows a successful complete.
//   - release is called exactly once: with true when the application calls
//     TransferSetupReservationV1 on the dialed connection, otherwise with
//     false after the connection's teardown has finished, including its
//     closing state, or when the dial fails before its connection runs.
//
// The deadline behaves as for accepted connections: reaching it before
// transfer closes the connection with CONNECTION_REFUSED under ordinary
// bounded close handling, and neither recreation, stage changes nor traffic
// extend it. The callback requirements of ConfigureSetupAdmissionV1 apply.
func (t *Transport) ConfigureDialSetupAdmissionV1(acquire func(ctx context.Context, remote netip.AddrPort) (deadline time.Time, complete func() bool, claim func(), release func(transferred bool), ok bool)) error {
	t.packetIO.mutex.Lock()
	defer t.packetIO.mutex.Unlock()
	if t.packetIO.started {
		return errors.New("quic: dial setup admission configuration after initialization")
	}
	if t.dialSetupAdmission != nil {
		return errors.New("quic: dial setup admission already configured")
	}
	if acquire == nil {
		return errors.New("quic: dial setup admission requires an acquire callback")
	}
	t.dialSetupAdmission = acquire
	return nil
}

// TransferSetupReservationV1 ends an accepted or dialed connection's setup
// reservation, releasing it to the caller's established lifetime and stopping
// its deadline. It fails for a connection without a reservation, or whose
// reservation has already been transferred, expired or released.
func (c *Conn) TransferSetupReservationV1() error {
	return c.setup.transfer()
}

// completeSetup and claimSetup are the server's stage changes; completeDial is
// the dial's. Server test
// doubles carry no connection, which has no reservation.
func (c *Conn) completeSetup() bool {
	if c == nil {
		return true
	}
	return c.setup.completeHandshake()
}

func (c *Conn) claimSetup() {
	if c != nil {
		c.setup.claimAccepted()
	}
}

// refuseDialCompletion closes a dialed connection whose completion was
// refused under ordinary bounded close handling.
func (c *Conn) refuseDialCompletion() {
	c.closeLocal(&qerr.TransportError{ErrorCode: qerr.ConnectionRefused, ErrorMessage: "setup completion refused"})
}

// handlerSetup returns the reservation of a connection-ID entry's connection.
func handlerSetup(h packetHandler) *setupReservation {
	switch h := h.(type) {
	case *wrappedConn:
		if h.Conn != nil {
			return h.setup
		}
	case *Conn:
		return h.setup
	}
	return nil
}

// acquire admits one new connection before allocation. A nil reservation with
// ok true is the ordinary, unconfigured profile.
func (a setupAdmission) acquire(remote net.Addr) (_ *setupReservation, ok bool) {
	if a == nil {
		return nil, true
	}
	ep, _ := packetEndpoint(remote)
	return newSetupReservation(a(ep))
}

// A refused attempt is remembered for its client's trailing datagrams and early
// retransmissions, among the most recent refusals. ConfigureSetupAdmissionV1
// documents these values.
const (
	maxRefusedInitials = 1024
	refusedInitialTTL  = protocol.DefaultHandshakeIdleTimeout
)

// refusedInitials remembers the Initial destination connection IDs of attempts
// refused before construction. A ClientHello spanning several Initials reaches
// admission once per datagram; without this, a trailing datagram could acquire
// capacity freed after the refusal and hold it for a connection that cannot
// complete. Only the server's packet goroutine uses it. Entries expire after
// refusedInitialTTL from their receive time, and the oldest is evicted when
// full; either falls back to ordinary admission.
type refusedInitials struct {
	capacity int
	byID     map[protocol.ConnectionID]*list.Element // of *refusal
	order    list.List                               // oldest refusal first
}

type refusal struct {
	id protocol.ConnectionID
	at monotime.Time // receive time of the refused Initial
}

func newRefusedInitials(capacity int) *refusedInitials {
	return &refusedInitials{capacity: capacity}
}

// refused reports whether id belongs to an attempt refused within the TTL.
func (r *refusedInitials) refused(id protocol.ConnectionID, now monotime.Time) bool {
	if r == nil {
		return false
	}
	e, ok := r.byID[id]
	return ok && now.Before(e.Value.(*refusal).at.Add(refusedInitialTTL))
}

// record remembers a refusal as the newest entry. Refusing a remembered
// attempt again changes neither its TTL nor its eviction order; an expired
// entry refused again is renewed as the newest. A full memory reuses its
// oldest entry.
func (r *refusedInitials) record(id protocol.ConnectionID, now monotime.Time) {
	if r == nil {
		return
	}
	if e, ok := r.byID[id]; ok {
		if !r.refused(id, now) {
			e.Value.(*refusal).at = now
			r.order.MoveToBack(e)
		}
		return
	}
	if r.byID == nil {
		r.byID = make(map[protocol.ConnectionID]*list.Element)
	}
	var e *list.Element
	if r.order.Len() < r.capacity {
		e = r.order.PushBack(&refusal{id: id, at: now})
	} else {
		e = r.order.Front()
		oldest := e.Value.(*refusal)
		delete(r.byID, oldest.id)
		*oldest = refusal{id: id, at: now}
		r.order.MoveToBack(e)
	}
	r.byID[id] = e
}

// acquire admits one dial before its connection IDs and connection exist. A
// nil reservation with ok true is the ordinary, unconfigured profile.
func (a dialSetupAdmission) acquire(ctx context.Context, remote net.Addr) (_ *setupReservation, ok bool) {
	if a == nil {
		return nil, true
	}
	ep, _ := packetEndpoint(remote)
	return newSetupReservation(a(ctx, ep))
}

// newSetupReservation validates an acquire callback's results. A reservation
// that cannot be held is released before it is refused.
func newSetupReservation(deadline time.Time, complete func() bool, claim func(), release func(transferred bool), ok bool) (*setupReservation, bool) {
	if !ok || release == nil {
		return nil, false
	}
	r := &setupReservation{complete: complete, claim: claim, release: release, deadline: deadline}
	if complete == nil || claim == nil || !time.Now().Before(deadline) {
		r.teardown()
		return nil, false
	}
	return r, true
}

// bind attaches the reservation to its newly constructed connection. The first
// bind arms the absolute deadline and always succeeds. A connection recreated
// for version negotiation rebinds the detached reservation without re-arming
// it; false means the reservation ended meanwhile and the caller releases it.
func (r *setupReservation) bind(c *Conn) bool {
	if r == nil {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.timer != nil && !r.activeLocked() {
		return false
	}
	c.setup = r
	r.conn = c
	if r.timer == nil {
		r.timer = time.AfterFunc(time.Until(r.deadline), r.expire)
	}
	return true
}

// detach hands the reservation of a connection closed for recreation to the
// dial, which rebinds it to the recreated connection or releases it.
func (r *setupReservation) detach() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.conn = nil
}

// expire requests the ordinary close; the release follows that teardown. A
// detached reservation has no connection to close; its dial observes the
// expiry when rebinding.
func (r *setupReservation) expire() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expireLocked()
}

func (r *setupReservation) expireLocked() {
	if r.settled || r.expired || r.ended {
		return
	}
	r.expired = true
	if r.conn != nil {
		r.conn.closeLocal(errSetupDeadlineExceeded())
	}
}

func errSetupDeadlineExceeded() error {
	return &qerr.TransportError{ErrorCode: qerr.ConnectionRefused, ErrorMessage: "setup deadline exceeded"}
}

// activeLocked reports whether the reservation still admits a stage change or
// transfer. A reached deadline expires it even if its timer has not yet run.
func (r *setupReservation) activeLocked() bool {
	if r.settled || r.expired || r.ended {
		return false
	}
	if !time.Now().Before(r.deadline) {
		r.expireLocked()
		return false
	}
	return true
}

// completeHandshake moves the reservation to the accept queue stage. False
// refuses the connection.
func (r *setupReservation) completeHandshake() bool {
	if r == nil {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.activeLocked() || !r.complete() {
		return false
	}
	r.stage = setupCompleted
	return true
}

// claimAccepted records that Accept returned the connection. A connection
// that expired or ended while queued is returned without a claim.
func (r *setupReservation) claimAccepted() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stage != setupCompleted || !r.activeLocked() {
		return
	}
	r.claim()
	r.stage = setupClaimed
}

// completeDial moves a dialed reservation through completion and its claim
// under one lock, since Dial returns the connection it completes. False
// refuses the connection.
func (r *setupReservation) completeDial() bool {
	if r == nil {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.activeLocked() || !r.complete() {
		return false
	}
	r.claim()
	r.stage = setupClaimed
	return true
}

func (r *setupReservation) transfer() error {
	if r == nil {
		return errNoSetupReservation
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stage != setupClaimed || !r.activeLocked() {
		return errSetupReservationEnded
	}
	r.settleLocked(true)
	return nil
}

// teardown records that the connection's work has ended or was never started,
// and releases the reservation unless a closing state is still retained. It is
// idempotent.
func (r *setupReservation) teardown() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ended = true
	if r.closing == 0 {
		r.settleLocked(false)
	}
}

// holdClosing keeps the reservation while a transport retains the closed
// connection's state. It precedes teardown on the connection's goroutine.
// The returned function reports that state's retirement.
func (r *setupReservation) holdClosing() func() {
	if r == nil {
		return func() {}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.settled {
		return func() {}
	}
	r.closing++
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.closing--
		if r.ended && r.closing == 0 {
			r.settleLocked(false)
		}
	}
}

func (r *setupReservation) settleLocked(transferred bool) {
	if r.settled {
		return
	}
	r.settled = true
	if r.timer != nil {
		r.timer.Stop()
	}
	r.conn = nil
	r.release(transferred)
}
