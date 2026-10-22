package congestion

import (
	"fmt"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/qlogwriter"
)

const (
	// maxDatagramSize is the default maximum packet size used in the Linux TCP implementation.
	// Used in QUIC for congestion window computations in bytes.
	initialMaxDatagramSize     = protocol.ByteCount(protocol.InitialPacketSize)
	maxBurstPackets            = 3
	renoBeta                   = 0.7 // Reno backoff factor.
	minCongestionWindowPackets = 2
	initialCongestionWindow    = 32
)

// renoUndoState is a snapshot, not another independently advanced frontier.
// Missing retained receipts leave outstanding losses unresolved.
type renoUndoState struct {
	congestionWindow, slowStartThreshold protocol.ByteCount
	numAckedPackets                      uint64
	cutback                              packetPrefix
	hasCutback, recovering               bool
	outstanding                          uint64
}

type cubicSender struct {
	hybridSlowStart HybridSlowStart
	rttStats        *utils.RTTStats
	connStats       *utils.ConnectionStats
	cubic           *Cubic
	pacer           *pacer
	clock           Clock

	reno bool

	// Only eligible sends advance the frontier, but it contains every prior
	// registration, including ACK-only and path packets in other spaces.
	registered, eligible, cutback  packetPrefix
	hasAck, hasCutback, recovering bool

	// Whether the last loss event caused us to exit slowstart.
	// Used for stats collection of slowstartPacketsLost
	lastCutbackExitedSlowstart bool

	// Congestion window in bytes.
	congestionWindow protocol.ByteCount

	// Slow start congestion window in bytes, aka ssthresh.
	slowStartThreshold protocol.ByteCount

	// ACK counter for the Reno implementation.
	numAckedPackets uint64
	undoState       renoUndoState

	initialCongestionWindow    protocol.ByteCount
	initialMaxCongestionWindow protocol.ByteCount

	maxDatagramSize protocol.ByteCount

	lastState qlog.CongestionState
	qlogger   qlogwriter.Recorder
}

var (
	_ SendAlgorithm               = &cubicSender{}
	_ SendAlgorithmWithDebugInfos = &cubicSender{}
)

// NewCubicSender makes a new cubic sender
func NewCubicSender(
	clock Clock,
	rttStats *utils.RTTStats,
	connStats *utils.ConnectionStats,
	initialMaxDatagramSize protocol.ByteCount,
	reno bool,
	qlogger qlogwriter.Recorder,
) *cubicSender {
	return newCubicSender(
		clock,
		rttStats,
		connStats,
		reno,
		initialMaxDatagramSize,
		initialCongestionWindow*initialMaxDatagramSize,
		protocol.MaxCongestionWindowPackets*initialMaxDatagramSize,
		qlogger,
	)
}

func newCubicSender(
	clock Clock,
	rttStats *utils.RTTStats,
	connStats *utils.ConnectionStats,
	reno bool,
	initialMaxDatagramSize,
	initialCongestionWindow,
	initialMaxCongestionWindow protocol.ByteCount,
	qlogger qlogwriter.Recorder,
) *cubicSender {
	c := &cubicSender{
		rttStats:                   rttStats,
		connStats:                  connStats,
		initialCongestionWindow:    initialCongestionWindow,
		initialMaxCongestionWindow: initialMaxCongestionWindow,
		congestionWindow:           initialCongestionWindow,
		slowStartThreshold:         protocol.MaxByteCount,
		cubic:                      NewCubic(clock),
		clock:                      clock,
		reno:                       reno,
		qlogger:                    qlogger,
		maxDatagramSize:            initialMaxDatagramSize,
	}
	c.pacer = newPacer(c.BandwidthEstimate)
	if c.qlogger != nil {
		c.lastState = qlog.CongestionStateSlowStart
		c.qlogger.RecordEvent(qlog.CongestionStateUpdated{
			State: qlog.CongestionStateSlowStart,
		})
	}
	return c
}

// TimeUntilSend returns when the next packet should be sent.
func (c *cubicSender) TimeUntilSend(_ protocol.ByteCount) monotime.Time {
	return c.pacer.TimeUntilSend()
}

func (c *cubicSender) HasPacingBudget(now monotime.Time) bool {
	return c.pacer.Budget(now) >= c.maxDatagramSize
}

func (c *cubicSender) maxCongestionWindow() protocol.ByteCount {
	return c.maxDatagramSize * protocol.MaxCongestionWindowPackets
}

func (c *cubicSender) minCongestionWindow() protocol.ByteCount {
	return c.maxDatagramSize * minCongestionWindowPackets
}

func (c *cubicSender) OnPacketSent(
	sentTime monotime.Time,
	_ protocol.ByteCount,
	packet PacketKey,
	bytes protocol.ByteCount,
	ackEliciting, pathProbe bool,
) {
	c.registered.note(packet)
	if pathProbe {
		return // Registration facts do not charge path probes to the pacer.
	}
	c.pacer.SentPacket(sentTime, bytes)
	if !ackEliciting {
		return
	}
	c.eligible = c.registered
	c.hybridSlowStart.OnPacketSent(c.eligible)
}

func (c *cubicSender) CanSend(bytesInFlight protocol.ByteCount) bool {
	return bytesInFlight < c.GetCongestionWindow()
}

func (c *cubicSender) InRecovery() bool {
	return c.hasAck && c.recovering
}

func (c *cubicSender) InSlowStart() bool {
	return c.GetCongestionWindow() < c.slowStartThreshold
}

func (c *cubicSender) GetCongestionWindow() protocol.ByteCount {
	return c.congestionWindow
}

func (c *cubicSender) MaybeExitSlowStart() {
	if c.InSlowStart() &&
		c.hybridSlowStart.ShouldExitSlowStart(c.rttStats.LatestRTT(), c.rttStats.MinRTT(), c.GetCongestionWindow()/c.maxDatagramSize) {
		// exit slow start
		c.slowStartThreshold = c.congestionWindow
		c.maybeQlogStateChange(qlog.CongestionStateCongestionAvoidance)
	}
}

func (c *cubicSender) OnPacketAcked(
	packet PacketKey,
	ackedBytes protocol.ByteCount,
	priorInFlight protocol.ByteCount,
	eventTime monotime.Time,
) {
	c.hasAck = true
	// An ACK can cross the prior boundary without crossing the latest one.
	// Remember that exit so restoring the snapshot cannot reactivate recovery.
	if c.undoState.outstanding > 0 && c.undoState.hasCutback && c.undoState.cutback.after(packet) {
		c.undoState.recovering = false
	}
	if c.hasCutback && c.cutback.after(packet) {
		c.recovering = false
	}
	if c.InRecovery() {
		return
	}
	c.maybeIncreaseCwnd(ackedBytes, priorInFlight, eventTime)
	if c.InSlowStart() {
		c.hybridSlowStart.OnPacketAcked(packet)
	}
}

func (c *cubicSender) OnCongestionEvent(packet PacketKey, lostBytes, priorInFlight protocol.ByteCount) {
	c.connStats.PacketsLost.Add(1)
	c.connStats.BytesLost.Add(uint64(lostBytes))
	// Recovery uses zero lost bytes only for validated CE. It cannot be
	// canceled by packet receipts, including when the cut below is suppressed.
	if lostBytes == 0 {
		c.OnPacketsDiscarded()
	}

	// TCP NewReno (RFC6582) says that once a loss occurs, any losses in packets
	// already sent should be treated as a single loss event, since it's expected.
	if c.hasCutback && !c.cutback.after(packet) {
		if c.undoState.outstanding > 0 && (!c.undoState.hasCutback || c.undoState.cutback.after(packet)) {
			c.undoState.outstanding++
		}
		return
	}
	if c.reno && lostBytes > 0 {
		c.undoState = renoUndoState{
			congestionWindow: c.congestionWindow, slowStartThreshold: c.slowStartThreshold,
			numAckedPackets: c.numAckedPackets, cutback: c.cutback,
			hasCutback: c.hasCutback, recovering: c.recovering, outstanding: 1,
		}
	}
	c.lastCutbackExitedSlowstart = c.InSlowStart()
	c.maybeQlogStateChange(qlog.CongestionStateRecovery)

	if c.reno {
		c.congestionWindow = protocol.ByteCount(float64(c.congestionWindow) * renoBeta)
	} else {
		c.congestionWindow = c.cubic.CongestionWindowAfterPacketLoss(c.congestionWindow)
	}
	if minCwnd := c.minCongestionWindow(); c.congestionWindow < minCwnd {
		c.congestionWindow = minCwnd
	}
	c.slowStartThreshold = c.congestionWindow
	c.cutback = c.eligible
	c.hasCutback, c.recovering = true, true
	// reset packet count from congestion avoidance mode. We start
	// counting again when we're out of recovery.
	c.numAckedPackets = 0
}

func (c *cubicSender) OnSpuriousLoss(packet PacketKey) {
	s := &c.undoState
	if s.outstanding == 0 || (s.hasCutback && !s.cutback.after(packet)) || c.cutback.after(packet) {
		return
	}
	s.outstanding--
	if s.outstanding != 0 {
		return
	}
	state := *s
	c.undoState = renoUndoState{}
	if c.congestionWindow > state.congestionWindow {
		return
	}
	c.congestionWindow, c.slowStartThreshold = state.congestionWindow, state.slowStartThreshold
	c.numAckedPackets = state.numAckedPackets
	c.cutback, c.hasCutback, c.recovering = state.cutback, state.hasCutback, state.recovering
	switch {
	case c.InRecovery():
		c.maybeQlogStateChange(qlog.CongestionStateRecovery)
	case c.InSlowStart():
		c.maybeQlogStateChange(qlog.CongestionStateSlowStart)
	default:
		c.maybeQlogStateChange(qlog.CongestionStateCongestionAvoidance)
	}
}

func (c *cubicSender) OnPacketsDiscarded() { c.undoState = renoUndoState{} }

// Called when we receive an ack. Normal TCP tracks how many packets one ack
// represents, but quic has a separate ack for each packet.
func (c *cubicSender) maybeIncreaseCwnd(
	ackedBytes protocol.ByteCount,
	priorInFlight protocol.ByteCount,
	eventTime monotime.Time,
) {
	// Do not increase the congestion window unless the sender is close to using
	// the current window.
	if !c.isCwndLimited(priorInFlight) {
		c.cubic.OnApplicationLimited()
		c.maybeQlogStateChange(qlog.CongestionStateApplicationLimited)
		return
	}
	if c.congestionWindow >= c.maxCongestionWindow() {
		return
	}
	if c.InSlowStart() {
		// TCP slow start, exponential growth, increase by one for each ACK.
		c.congestionWindow += c.maxDatagramSize
		c.maybeQlogStateChange(qlog.CongestionStateSlowStart)
		return
	}
	// Congestion avoidance
	c.maybeQlogStateChange(qlog.CongestionStateCongestionAvoidance)
	if c.reno {
		// Classic Reno congestion avoidance.
		c.numAckedPackets++
		if c.numAckedPackets >= uint64(c.congestionWindow/c.maxDatagramSize) {
			c.congestionWindow += c.maxDatagramSize
			c.numAckedPackets = 0
		}
	} else {
		c.congestionWindow = min(
			c.maxCongestionWindow(),
			c.cubic.CongestionWindowAfterAck(ackedBytes, c.congestionWindow, c.rttStats.MinRTT(), eventTime),
		)
	}
}

func (c *cubicSender) isCwndLimited(bytesInFlight protocol.ByteCount) bool {
	congestionWindow := c.GetCongestionWindow()
	if bytesInFlight >= congestionWindow {
		return true
	}
	availableBytes := congestionWindow - bytesInFlight
	slowStartLimited := c.InSlowStart() && bytesInFlight > congestionWindow/2
	return slowStartLimited || availableBytes <= maxBurstPackets*c.maxDatagramSize
}

// BandwidthEstimate returns the current bandwidth estimate
func (c *cubicSender) BandwidthEstimate() Bandwidth {
	srtt := c.rttStats.SmoothedRTT()
	if srtt == 0 {
		// This should never happen, but if it does, avoid division by zero.
		srtt = protocol.TimerGranularity
	}
	return BandwidthFromDelta(c.GetCongestionWindow(), srtt)
}

// OnRetransmissionTimeout is called on an retransmission timeout
func (c *cubicSender) OnRetransmissionTimeout(packetsRetransmitted bool) {
	c.OnPacketsDiscarded()
	c.cutback = packetPrefix{}
	c.hasCutback, c.recovering = false, false
	if !packetsRetransmitted {
		return
	}
	c.hybridSlowStart.Restart()
	c.cubic.Reset()
	c.slowStartThreshold = c.congestionWindow / 2
	c.congestionWindow = c.minCongestionWindow()
}

// OnConnectionMigration is called when the connection is migrated (?)
func (c *cubicSender) OnConnectionMigration() {
	c.OnPacketsDiscarded()
	c.hybridSlowStart.Restart()
	c.registered, c.eligible = packetPrefix{}, packetPrefix{}
	c.hasAck = false
	c.cutback = packetPrefix{}
	c.hasCutback, c.recovering = false, false
	c.lastCutbackExitedSlowstart = false
	c.cubic.Reset()
	c.numAckedPackets = 0
	c.congestionWindow = c.initialCongestionWindow
	c.slowStartThreshold = c.initialMaxCongestionWindow
}

func (c *cubicSender) maybeQlogStateChange(new qlog.CongestionState) {
	if c.qlogger == nil || new == c.lastState {
		return
	}
	c.qlogger.RecordEvent(qlog.CongestionStateUpdated{State: new})
	c.lastState = new
}

func (c *cubicSender) SetMaxDatagramSize(s protocol.ByteCount) {
	if s < c.maxDatagramSize {
		panic(fmt.Sprintf("congestion BUG: decreased max datagram size from %d to %d", c.maxDatagramSize, s))
	}
	cwndIsMinCwnd := c.congestionWindow == c.minCongestionWindow()
	c.maxDatagramSize = s
	if cwndIsMinCwnd {
		c.congestionWindow = c.minCongestionWindow()
	}
	c.pacer.SetMaxDatagramSize(s)
}
