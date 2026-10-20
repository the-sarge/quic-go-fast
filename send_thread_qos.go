package quic

import (
	"errors"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
)

// sendQoSDisableEnv is the kill switch, following QUIC_GO_DISABLE_SENDMSG_X.
// It is read when each connection is created.
const sendQoSDisableEnv = "QUIC_GO_DISABLE_SEND_QOS"

// sendQoSRaisedPriority is the lowest base priority that counts as raised: the
// base priority of the user-initiated class. The requested class is no
// evidence, because the kernel reports it even when the process's role caps
// the thread below it.
const sendQoSRaisedPriority = 37

// sendQoSCheckInterval bounds how often a raised worker re-reads its base
// priority. Reads follow sends, so an idle worker makes none.
const sendQoSCheckInterval = time.Second

// The outcome of a connection's send thread QoS. A connection without a
// controller is off.
const (
	sendQoSPending uint32 = iota
	sendQoSRaised
	sendQoSRefused
	sendQoSStoodDown
)

// sendQoSClass is a thread's QoS class and relative priority in the
// platform's encoding, recorded before a raise so that it can be restored.
type sendQoSClass uint64

// sendQoSThread operates on the calling goroutine's OS thread. raise leaves
// the class unchanged when it fails.
type sendQoSThread interface {
	lock()
	unlock()
	raise() (prior sendQoSClass, err error)
	basePriority() (int, error)
	restore(prior sendQoSClass) error
}

// sendQoSThreadSource returns the platform's thread operations, or nil where
// the platform has no hook. Tests replace it.
var sendQoSThreadSource = newPlatformSendQoSThread

// sendThreadQoS is one connection's send thread QoS. It outlives each send
// queue, so a refusal or stand-down holds across path changes. Only one send
// worker runs at a time: a path change closes the old queue, which waits for
// its worker to return, before it starts the next one.
type sendThreadQoS struct {
	state  atomic.Uint32
	thread sendQoSThread
	now    func() monotime.Time
}

func (q *sendThreadQoS) outcome() string {
	if q == nil {
		return "off"
	}
	switch q.state.Load() {
	case sendQoSRaised:
		return "raised"
	case sendQoSRefused:
		return "refused"
	case sendQoSStoodDown:
		return "stood_down"
	default:
		return "pending"
	}
}

// begin locks the calling goroutine to its thread and raises the thread to
// user-initiated, unless an earlier worker of this connection was refused or
// stood down. The raise counts only once the base priority shows it.
func (q *sendThreadQoS) begin() sendQoSHold {
	if q == nil {
		return sendQoSHold{}
	}
	if state := q.state.Load(); state != sendQoSPending && state != sendQoSRaised {
		return sendQoSHold{}
	}
	q.thread.lock()
	prior, err := q.thread.raise()
	if err != nil {
		q.thread.unlock()
		q.fail()
		return sendQoSHold{}
	}
	h := sendQoSHold{qos: q, prior: prior, held: true}
	if !q.verified() {
		h.release()
		q.fail()
		return sendQoSHold{}
	}
	q.state.Store(sendQoSRaised)
	h.nextCheck = q.now().Add(sendQoSCheckInterval)
	return h
}

// fail records a refusal for a first attempt and a stand-down once a raise
// has held.
func (q *sendThreadQoS) fail() {
	if !q.state.CompareAndSwap(sendQoSPending, sendQoSRefused) {
		q.state.Store(sendQoSStoodDown)
	}
}

func (q *sendThreadQoS) verified() bool {
	priority, err := q.thread.basePriority()
	return err == nil && priority >= sendQoSRaisedPriority
}

// sendQoSHold is one send worker's raise. The zero value holds nothing.
type sendQoSHold struct {
	qos       *sendThreadQoS
	prior     sendQoSClass
	held      bool
	nextCheck monotime.Time
}

// check stands the worker down if the raise has been taken away, re-reading
// at most once per interval. The worker keeps sending either way.
func (h *sendQoSHold) check() {
	if !h.held {
		return
	}
	now := h.qos.now()
	if now.Before(h.nextCheck) {
		return
	}
	if h.qos.verified() {
		h.nextCheck = now.Add(sendQoSCheckInterval)
		return
	}
	h.release()
	h.qos.state.Store(sendQoSStoodDown)
}

// release restores the class before unlocking, so Go's thread pool never gets
// a raised thread back. If the class cannot be restored, the goroutine stays
// locked and the runtime ends the thread when the goroutine exits. That relies
// on sendQueue.Run's callers: each runs the worker on a goroutine of its own
// that exits soon after Run returns.
func (h *sendQoSHold) release() {
	if !h.held {
		return
	}
	h.held = false
	if h.qos.thread.restore(h.prior) != nil {
		return
	}
	h.qos.thread.unlock()
}

// installSendThreadQoS runs after construction and before the connection loop
// starts its send worker.
func (c *Conn) installSendThreadQoS(enabled bool) {
	if !enabled {
		return
	}
	if disabled, _ := strconv.ParseBool(os.Getenv(sendQoSDisableEnv)); disabled {
		return
	}
	thread := sendQoSThreadSource()
	if thread == nil {
		return
	}
	c.emission.bindSendQoS(&sendThreadQoS{thread: thread, now: monotime.Now})
}

// bindSendQoS gives the connection's controller to the active queue; a path
// change gives it to each queue it creates.
func (e *packetEmission) bindSendQoS(q *sendThreadQoS) {
	e.sendQoS = q
	if sq, ok := e.queue.(*sendQueue); ok {
		sq.qos = q
	}
}

// ConfigureSendThreadQoSV1 makes the send worker of each connection created by
// this transport raise its thread to the user-initiated QoS class on Apple
// silicon Macs, so that the scheduler keeps it on performance cores while other
// programs load the machine. Call it once, before any operation that
// initializes the transport. Transports without it keep ordinary behavior, and
// on every other platform it has no effect.
//
// A launchd daemon or agent, an ssh session or the frontmost app can have the
// class; a command started from Terminal or an IDE, or an app that is not
// frontmost, cannot. Each worker therefore checks its thread's base priority
// after raising, and again about once a second while it sends. If the raise is
// refused at start or later taken away, the worker restores the thread's class
// and continues unraised for the rest of the connection, including on later
// paths. SendThreadQoSV1 reports the outcome.
//
// A raised worker is locked to its OS thread for as long as it holds the
// raise, so each such connection occupies one OS thread. Setting the
// QUIC_GO_DISABLE_SEND_QOS environment variable to true turns the raise off for
// connections created afterwards.
func (t *Transport) ConfigureSendThreadQoSV1() error {
	t.packetIO.mutex.Lock()
	defer t.packetIO.mutex.Unlock()
	if t.packetIO.started {
		return errors.New("quic: send thread QoS configuration after initialization")
	}
	if t.sendThreadQoS {
		return errors.New("quic: send thread QoS already configured")
	}
	t.sendThreadQoS = true
	return nil
}

// SendThreadQoSV1 reports this connection's send thread QoS: "off" when its
// transport did not configure it, the kill switch was set or the platform is
// not an Apple silicon Mac; "pending" before its send worker starts; "raised"
// while the raise holds; "refused" when the first raise did not take effect;
// and "stood_down" when a raise that held was later taken away. Refused and
// stood down are final for the connection. It is safe to call concurrently and
// keeps its value after the connection closes.
func (c *Conn) SendThreadQoSV1() string {
	return c.emission.sendQoS.outcome()
}
