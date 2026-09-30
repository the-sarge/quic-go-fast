package quic

import (
	"errors"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/quic-go/quic-go/internal/qerr"
)

var (
	errNoSetupReservation    = errors.New("quic: connection has no setup reservation")
	errSetupReservationEnded = errors.New("quic: setup reservation already ended")
)

// setupAdmission is the caller's acquire callback, immutable after configuration.
type setupAdmission func(remote netip.AddrPort) (deadline time.Time, complete func() bool, claim func(), release func(transferred bool), ok bool)

type setupStage uint8

const (
	setupHandshaking setupStage = iota // acquired, handshake in progress
	setupCompleted                     // queued for Accept, unclaimed
	setupClaimed                       // returned by Accept, not transferred
)

// setupReservation is the single owner of one server connection's setup
// capacity, from before its construction until one terminal settlement. The
// caller's callbacks run under mu, so stage changes and the release are
// serialized and nothing follows the release. Server paths only request
// settlement; teardown is idempotent, so no path owns a competing release.
type setupReservation struct {
	mu       sync.Mutex
	complete func() bool
	claim    func()
	release  func(transferred bool)
	deadline time.Time
	stage    setupStage
	expired  bool // the deadline requested teardown
	settled  bool
	timer    *time.Timer
	conn     *Conn // severed on settlement
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
// three non-nil callbacks; a deadline not in the future or a nil callback
// refuses the connection and releases the reservation.
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
//     false after the connection's teardown has finished, including when
//     construction or registration fails.
//
// Reaching the deadline before transfer closes the connection with
// CONNECTION_REFUSED under ordinary bounded close handling. Stage changes and
// traffic never extend the deadline. Callbacks may be shared across
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

// TransferSetupReservationV1 ends an accepted server connection's setup
// reservation, releasing it to the caller's established lifetime and stopping
// its deadline. It fails for a connection without a reservation, or whose
// reservation has already been transferred, expired or released.
func (c *Conn) TransferSetupReservationV1() error {
	return c.setup.transfer()
}

// completeSetup and claimSetup are the server's stage changes. Server test
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

// acquire admits one new connection before allocation. A nil reservation with
// ok true is the ordinary, unconfigured profile.
func (a setupAdmission) acquire(remote net.Addr) (_ *setupReservation, ok bool) {
	if a == nil {
		return nil, true
	}
	ep, _ := packetEndpoint(remote)
	deadline, complete, claim, release, ok := a(ep)
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

// bind attaches the reservation to its newly constructed connection and arms
// the absolute deadline.
func (r *setupReservation) bind(c *Conn) {
	if r == nil {
		return
	}
	c.setup = r
	r.mu.Lock()
	defer r.mu.Unlock()
	r.conn = c
	r.timer = time.AfterFunc(time.Until(r.deadline), r.expire)
}

// expire requests the ordinary close; the release follows that teardown.
func (r *setupReservation) expire() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.settled || r.expired {
		return
	}
	r.expired = true
	r.conn.closeLocal(&qerr.TransportError{ErrorCode: qerr.ConnectionRefused, ErrorMessage: "setup deadline exceeded"})
}

// completeHandshake moves the reservation to the accept queue stage. False
// refuses the connection.
func (r *setupReservation) completeHandshake() bool {
	if r == nil {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.settled || r.expired || !r.complete() {
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
	if r.settled || r.expired || r.stage != setupCompleted {
		return
	}
	r.claim()
	r.stage = setupClaimed
}

func (r *setupReservation) transfer() error {
	if r == nil {
		return errNoSetupReservation
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.settled || r.expired || r.stage != setupClaimed {
		return errSetupReservationEnded
	}
	r.settleLocked(true)
	return nil
}

// teardown releases the reservation once the connection's work has ended or
// was never started. It is idempotent.
func (r *setupReservation) teardown() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.settleLocked(false)
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
