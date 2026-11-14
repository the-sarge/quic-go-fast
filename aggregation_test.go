//go:build aggregation && linux && amd64

package quic

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"os"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/testdata"

	"github.com/stretchr/testify/require"
)

// This is B5's controller-matched aggregation workload (#1044).
// interop/multipath/aggregation.py runs one relay, one server and one client per
// observation, each on its own cores, and judges the results. The three roles share
// one wall-clock timeline, so no start message crosses a shaped path.
//
// The client is the sender. Its focal connections upload fixed-size STREAM objects and
// a fixed-rate DATAGRAM flow through the relay; the server counts what arrives. The
// relay shapes each path's client-to-server direction with a rate-limited drop-tail
// queue and delays both directions. It forwards payloads only, so ECN marks are not
// carried; every arm sees the same.
const (
	aggregationALPN = "aggregation"
	// aggregationWriteBudget is the grant's native request duration, the default of a
	// socket the library owns.
	aggregationWriteBudget = 100 * time.Millisecond

	aggregationObject = 1 << 20
	// aggregationObjectsInFlight is a flow's concurrent STREAM objects, split evenly
	// across the arm's focal connections like the DATAGRAM load, so every arm offers
	// the same load.
	aggregationObjectsInFlight = 4
	aggregationChunk           = 64 << 10
	aggregationDatagram        = 1000
	// aggregationDatagramRate is the focal DATAGRAM load in messages each second, split
	// evenly across the arm's focal connections.
	aggregationDatagramRate = 250
	// aggregationDialLead is how long before the timeline's start the client dials, so
	// the relay and server have bound their sockets.
	aggregationDialLead = 3 * time.Second
	aggregationDrain    = 2 * time.Second
	// ipUDPOverhead is charged with each payload, so a queue's rate is an IPv4 rate.
	ipUDPOverhead = 28
)

// Each stream and DATAGRAM starts with the flow's label and the sender's wall clock.
const (
	aggregationFocalA byte = iota + 1
	aggregationFocalB
	aggregationCompetitor
)

func aggregationLabelName(label byte) string {
	switch label {
	case aggregationFocalA:
		return "focal-a"
	case aggregationFocalB:
		return "focal-b"
	case aggregationCompetitor:
		return "competitor"
	}
	return fmt.Sprintf("unknown-%d", label)
}

// aggregationQueue is one bottleneck: an IPv4 rate and a drop-tail limit in bytes.
type aggregationQueue struct {
	RateBPS    uint64 `json:"rate_bps"`
	LimitBytes uint64 `json:"limit_bytes"`
}

// aggregationLink is one path through the relay: its forward bottleneck and its
// one-way propagation delay, applied in both directions.
type aggregationLink struct {
	Queue   int   `json:"queue"`
	DelayNS int64 `json:"delay_ns"`
}

type aggregationTopology struct {
	Queues []aggregationQueue `json:"queues"`
	A      aggregationLink    `json:"a"`
	B      aggregationLink    `json:"b"`
	// Competitor is used only when Competing is set.
	Competitor aggregationLink `json:"competitor"`
	Competing  bool            `json:"competing"`
}

const (
	aggregationRate  = 50_000_000
	aggregationDelay = int64(10 * time.Millisecond)
)

// aggregationTopologies are the three accepted topologies. Each queue holds one
// bandwidth-delay product of its paths' round trip.
var aggregationTopologies = map[string]aggregationTopology{
	"balanced": {
		Queues: []aggregationQueue{{aggregationRate, 125_000}, {aggregationRate, 125_000}},
		A:      aggregationLink{0, aggregationDelay},
		B:      aggregationLink{1, aggregationDelay},
	},
	"unequal": {
		Queues: []aggregationQueue{{aggregationRate, 125_000}, {aggregationRate / 2, 187_500}},
		A:      aggregationLink{0, aggregationDelay},
		B:      aggregationLink{1, 3 * aggregationDelay},
	},
	"shared": {
		Queues:     []aggregationQueue{{aggregationRate, 125_000}},
		A:          aggregationLink{0, aggregationDelay},
		B:          aggregationLink{0, aggregationDelay},
		Competitor: aggregationLink{0, aggregationDelay},
		Competing:  true,
	},
}

// aggregationArm is one controller-matched mode.
type aggregationArm struct {
	Controller string `json:"controller"`
	Multipath  bool   `json:"multipath"`
	// Scheduler is the private policy of a multipath arm.
	Scheduler string `json:"scheduler"`
	// Paths are the relay paths the arm's focal connections use: one ordinary
	// connection each, or one multipath connection over both.
	Paths string `json:"paths"`
}

var aggregationArms = map[string]aggregationArm{
	"A": {Controller: "bbrv3", Paths: "a"},
	"B": {Controller: "bbrv3", Paths: "b"},
	"C": {Controller: "bbrv3", Paths: "ab"},
	"D": {Controller: "reno", Multipath: true, Scheduler: "aggregate", Paths: "ab"},
	"E": {Controller: "bbrv3", Multipath: true, Scheduler: "aggregate", Paths: "ab"},
	"F": {Controller: "bbrv3", Multipath: true, Scheduler: "round-robin", Paths: "ab"},
}

func (a aggregationArm) evidence() multipathEvidence {
	evidence := multipathEvidence{bbr: a.Controller == "bbrv3"}
	switch a.Scheduler {
	case "aggregate":
		evidence.scheduler = scheduleAggregate
	case "round-robin":
		evidence.scheduler = scheduleRoundRobin
	}
	return evidence
}

func aggregationSchedulerName(s pathScheduler) string {
	switch s {
	case schedulePrimaryBackup:
		return "primary-backup"
	case scheduleAggregate:
		return "aggregate"
	case scheduleRoundRobin:
		return "round-robin"
	}
	return fmt.Sprintf("unknown-%d", s)
}

type aggregationSpec struct {
	Role            string `json:"role"`
	Topology        string `json:"topology"`
	Arm             string `json:"arm"`
	Seed            uint64 `json:"seed"`
	Server          string `json:"server"`
	FrontA          string `json:"front_a"`
	FrontB          string `json:"front_b"`
	FrontCompetitor string `json:"front_competitor"`
	Output          string `json:"output"`
	StartAt         int64  `json:"start_at_unix_ns"`
	WarmupNS        int64  `json:"warmup_ns"`
	WindowNS        int64  `json:"window_ns"`
}

type aggregationTimeline struct{ start, window, end time.Time }

func (s aggregationSpec) timeline() aggregationTimeline {
	start := time.Unix(0, s.StartAt)
	window := start.Add(time.Duration(s.WarmupNS))
	return aggregationTimeline{start: start, window: window, end: window.Add(time.Duration(s.WindowNS))}
}

func (l aggregationTimeline) inWindow(t time.Time) bool {
	return !t.Before(l.window) && t.Before(l.end)
}

func (l aggregationTimeline) seconds() int { return int(l.end.Sub(l.window) / time.Second) }

// second is t's index in the window's per-second series, or -1 outside it.
func (l aggregationTimeline) second(t time.Time) int {
	if !l.inWindow(t) {
		return -1
	}
	return int(t.Sub(l.window) / time.Second)
}

func aggregationConfig(t testing.TB, controller string, multipath bool) *Config {
	t.Helper()
	conf := &Config{
		EnableDatagrams:                true,
		InitialStreamReceiveWindow:     4 << 20,
		MaxStreamReceiveWindow:         16 << 20,
		InitialConnectionReceiveWindow: 8 << 20,
		MaxConnectionReceiveWindow:     32 << 20,
		MaxIdleTimeout:                 30 * time.Second,
	}
	require.NoError(t, conf.SetCongestionControlV1(controller))
	if multipath {
		require.NoError(t, conf.ConfigureMultipathV1(true, 2, "primary-backup"))
	}
	return conf
}

func aggregationServerTLS() *tls.Config {
	conf := testdata.GetTLSConfig()
	conf.NextProtos = []string{aggregationALPN}
	return conf
}

func aggregationClientTLS() *tls.Config {
	return &tls.Config{RootCAs: testdata.GetRootCA(), ServerName: "localhost", NextProtos: []string{aggregationALPN}}
}

func aggregationTransport(t testing.TB, address string, granted bool) *Transport {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp4", address)
	require.NoError(t, err)
	socket, err := net.ListenUDP("udp4", addr)
	require.NoError(t, err)
	t.Cleanup(func() { socket.Close() })
	tr := &Transport{Conn: socket, ConnectionIDLength: 4}
	t.Cleanup(func() { tr.Close() })
	if granted {
		require.NoError(t, tr.ConfigureMultipathIOV1(aggregationWriteBudget))
	}
	return tr
}

func aggregationCloseCause(conn *Conn) (string, bool) {
	err := context.Cause(conn.Context())
	if err == nil {
		return "", false
	}
	var idle *IdleTimeoutError
	var handshake *HandshakeTimeoutError
	return err.Error(), errors.As(err, &idle) || errors.As(err, &handshake)
}

// percentile returns the q quantile of sorted, in milliseconds, or 0 when empty.
func aggregationPercentile(sorted []time.Duration, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	index := int(q * float64(len(sorted)-1))
	return float64(sorted[index]) / float64(time.Millisecond)
}

type aggregationLatency struct {
	Count int     `json:"count"`
	P50MS float64 `json:"p50_ms"`
	P99MS float64 `json:"p99_ms"`
	MaxMS float64 `json:"max_ms"`
}

func aggregationSummary(samples []time.Duration) aggregationLatency {
	sorted := slices.Clone(samples)
	slices.Sort(sorted)
	return aggregationLatency{
		Count: len(sorted),
		P50MS: aggregationPercentile(sorted, 0.5),
		P99MS: aggregationPercentile(sorted, 0.99),
		MaxMS: aggregationPercentile(sorted, 1),
	}
}

// ---- relay ----

// relayQueue is a drop-tail bottleneck. busy is when its last admitted byte leaves.
type relayQueue struct {
	mu    sync.Mutex
	rate  uint64
	limit time.Duration
	busy  time.Time
}

func newRelayQueue(q aggregationQueue) *relayQueue {
	return &relayQueue{rate: q.RateBPS, limit: time.Duration(q.LimitBytes * 8 * uint64(time.Second) / q.RateBPS)}
}

// admit returns when a payload of size bytes finishes service, or false when the
// queue's backlog would exceed its limit.
func (q *relayQueue) admit(now time.Time, size int) (time.Time, bool) {
	service := time.Duration(uint64(size+ipUDPOverhead) * 8 * uint64(time.Second) / q.rate)
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.busy.Before(now) {
		q.busy = now
	}
	if q.busy.Sub(now)+service > q.limit {
		return time.Time{}, false
	}
	q.busy = q.busy.Add(service)
	return q.busy, true
}

type relayPacket struct {
	at   time.Time
	data []byte
}

type aggregationDirection struct {
	Received       uint64  `json:"received"`
	Dropped        uint64  `json:"dropped"`
	Overflow       uint64  `json:"overflow"`
	Delivered      uint64  `json:"delivered"`
	DeliveredBytes uint64  `json:"delivered_bytes"`
	WindowBytes    uint64  `json:"window_bytes"`
	SendErrors     uint64  `json:"send_errors"`
	LateOver1ms    uint64  `json:"late_over_1ms"`
	MaxLateMS      float64 `json:"max_late_ms"`
}

type aggregationLinkResult struct {
	Name    string               `json:"name"`
	Forward aggregationDirection `json:"forward"`
	Reverse aggregationDirection `json:"reverse"`
}

// relayLink forwards one path: front faces the client, back is connected to the server.
type relayLink struct {
	name          string
	front, back   *net.UDPConn
	client        atomic.Pointer[net.UDPAddr]
	queue         *relayQueue
	delay         time.Duration
	forward       chan relayPacket
	reverse       chan relayPacket
	line          aggregationTimeline
	mu            sync.Mutex
	result        aggregationLinkResult
	forwardDone   chan struct{}
	reverseDone   chan struct{}
	readerStopped sync.WaitGroup
}

const relayBacklog = 1 << 16

func newRelayLink(t testing.TB, name, front, server string, queue *relayQueue, delay time.Duration, line aggregationTimeline) *relayLink {
	t.Helper()
	frontAddr, err := net.ResolveUDPAddr("udp4", front)
	require.NoError(t, err)
	serverAddr, err := net.ResolveUDPAddr("udp4", server)
	require.NoError(t, err)
	l := &relayLink{
		name: name, queue: queue, delay: delay, line: line,
		forward: make(chan relayPacket, relayBacklog), reverse: make(chan relayPacket, relayBacklog),
		forwardDone: make(chan struct{}), reverseDone: make(chan struct{}),
	}
	l.result.Name = name
	l.front, err = net.ListenUDP("udp4", frontAddr)
	require.NoError(t, err)
	l.back, err = net.DialUDP("udp4", nil, serverAddr)
	require.NoError(t, err)
	for _, c := range []*net.UDPConn{l.front, l.back} {
		require.NoError(t, c.SetReadBuffer(8<<20))
		require.NoError(t, c.SetWriteBuffer(8<<20))
	}
	return l
}

func (l *relayLink) run() {
	l.readerStopped.Add(2)
	go l.readForward()
	go l.readReverse()
	go l.deliver(l.forward, l.forwardDone, func(b []byte) error { _, err := l.back.Write(b); return err }, true)
	go l.deliver(l.reverse, l.reverseDone, func(b []byte) error {
		client := l.client.Load()
		if client == nil {
			return errors.New("no client address")
		}
		_, err := l.front.WriteToUDP(b, client)
		return err
	}, false)
}

func (l *relayLink) readForward() {
	defer l.readerStopped.Done()
	buf := make([]byte, 65536)
	for {
		n, from, err := l.front.ReadFromUDP(buf)
		if err != nil {
			return
		}
		now := time.Now()
		l.client.Store(from)
		l.mu.Lock()
		l.result.Forward.Received++
		l.mu.Unlock()
		leaves, ok := l.queue.admit(now, n)
		if !ok {
			l.mu.Lock()
			l.result.Forward.Dropped++
			l.mu.Unlock()
			continue
		}
		l.enqueue(l.forward, relayPacket{at: leaves.Add(l.delay), data: slices.Clone(buf[:n])}, &l.result.Forward)
	}
}

func (l *relayLink) readReverse() {
	defer l.readerStopped.Done()
	buf := make([]byte, 65536)
	for {
		n, err := l.back.Read(buf)
		if err != nil {
			return
		}
		now := time.Now()
		l.mu.Lock()
		l.result.Reverse.Received++
		l.mu.Unlock()
		l.enqueue(l.reverse, relayPacket{at: now.Add(l.delay), data: slices.Clone(buf[:n])}, &l.result.Reverse)
	}
}

func (l *relayLink) enqueue(ch chan relayPacket, p relayPacket, stats *aggregationDirection) {
	select {
	case ch <- p:
	default:
		l.mu.Lock()
		stats.Overflow++
		l.mu.Unlock()
	}
}

// deliver sends each packet at its time. A direction's packets are in time order:
// its queue's departures are monotonic and its delay is constant.
func (l *relayLink) deliver(ch chan relayPacket, done chan struct{}, send func([]byte) error, forward bool) {
	defer close(done)
	for p := range ch {
		if wait := time.Until(p.at); wait > 0 {
			time.Sleep(wait)
		}
		sent := time.Now()
		late := sent.Sub(p.at)
		err := send(p.data)
		l.mu.Lock()
		stats := &l.result.Reverse
		if forward {
			stats = &l.result.Forward
		}
		if err != nil {
			stats.SendErrors++
		} else {
			stats.Delivered++
			stats.DeliveredBytes += uint64(len(p.data))
			if l.line.inWindow(sent) {
				stats.WindowBytes += uint64(len(p.data))
			}
		}
		if late > time.Millisecond {
			stats.LateOver1ms++
		}
		if ms := float64(late) / float64(time.Millisecond); ms > stats.MaxLateMS {
			stats.MaxLateMS = ms
		}
		l.mu.Unlock()
	}
}

func (l *relayLink) stop() aggregationLinkResult {
	l.front.Close()
	l.back.Close()
	l.readerStopped.Wait()
	close(l.forward)
	close(l.reverse)
	<-l.forwardDone
	<-l.reverseDone
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.result
}

type aggregationRelayResult struct {
	Go       string                  `json:"go"`
	Topology aggregationTopology     `json:"topology"`
	Links    []aggregationLinkResult `json:"links"`
}

func aggregationRunRelay(t testing.TB, spec aggregationSpec) *aggregationRelayResult {
	t.Helper()
	topology, ok := aggregationTopologies[spec.Topology]
	require.True(t, ok, "unknown topology %q", spec.Topology)
	line := spec.timeline()
	queues := make([]*relayQueue, len(topology.Queues))
	for i, q := range topology.Queues {
		queues[i] = newRelayQueue(q)
	}
	link := func(name, front string, l aggregationLink) *relayLink {
		return newRelayLink(t, name, front, spec.Server, queues[l.Queue], time.Duration(l.DelayNS), line)
	}
	links := []*relayLink{link("a", spec.FrontA, topology.A), link("b", spec.FrontB, topology.B)}
	if topology.Competing {
		links = append(links, link("competitor", spec.FrontCompetitor, topology.Competitor))
	}
	for _, l := range links {
		l.run()
	}
	time.Sleep(time.Until(line.end.Add(aggregationDrain)))
	result := &aggregationRelayResult{Go: runtime.Version(), Topology: topology}
	for _, l := range links {
		result.Links = append(result.Links, l.stop())
	}
	return result
}

// ---- server ----

type aggregationFlowStats struct {
	mu sync.Mutex
	// StreamSeries is in-order STREAM bytes read in each second of the window.
	StreamSeries      []uint64 `json:"stream_series"`
	StreamBytes       uint64   `json:"stream_useful_bytes"`
	ObjectsCompleted  int      `json:"objects_completed"`
	objectCompletion  []time.Duration
	Completion        aggregationLatency `json:"object_completion"`
	DatagramsUseful   uint64             `json:"datagrams_useful"`
	DatagramBytes     uint64             `json:"datagram_useful_bytes"`
	DatagramDuplicate uint64             `json:"datagram_duplicates"`
	datagramLatency   []time.Duration
	DatagramLatency   aggregationLatency `json:"datagram_latency"`
	seen              map[uint64]struct{}
}

type aggregationServerConnResult struct {
	Labels       []string `json:"labels"`
	Multipath    bool     `json:"multipath_negotiated"`
	CloseCause   string   `json:"close_cause"`
	TimeoutClose bool     `json:"timeout_close"`
}

type aggregationServerResult struct {
	Go          string                           `json:"go"`
	Granted     bool                             `json:"granted"`
	Joined      bool                             `json:"joined"`
	Flows       map[string]*aggregationFlowStats `json:"flows"`
	Connections []aggregationServerConnResult    `json:"connections"`
	StreamErrs  uint64                           `json:"stream_errors"`
}

type aggregationServer struct {
	line    aggregationTimeline
	mu      sync.Mutex
	flows   map[byte]*aggregationFlowStats
	errs    atomic.Uint64
	connsMu sync.Mutex
	conns   []*aggregationServerConn
}

type aggregationServerConn struct {
	conn   *Conn
	mu     sync.Mutex
	labels map[byte]bool
}

func (c *aggregationServerConn) note(label byte) {
	c.mu.Lock()
	c.labels[label] = true
	c.mu.Unlock()
}

func (s *aggregationServer) flow(label byte) *aggregationFlowStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.flows[label]
	if !ok {
		f = &aggregationFlowStats{StreamSeries: make([]uint64, s.line.seconds()), seen: make(map[uint64]struct{})}
		s.flows[label] = f
	}
	return f
}

func (s *aggregationServer) object(entry *aggregationServerConn, str *ReceiveStream) {
	var header [9]byte
	if _, err := io.ReadFull(str, header[:]); err != nil {
		if !time.Now().Before(s.line.end) {
			return
		}
		s.errs.Add(1)
		return
	}
	label := header[0]
	entry.note(label)
	started := time.Unix(0, int64(binary.BigEndian.Uint64(header[1:])))
	f := s.flow(label)
	buf := make([]byte, aggregationChunk)
	total := len(header)
	for {
		n, err := str.Read(buf)
		now := time.Now()
		if n > 0 {
			total += n
			if second := s.line.second(now); second >= 0 {
				f.mu.Lock()
				f.StreamSeries[second] += uint64(n)
				f.StreamBytes += uint64(n)
				f.mu.Unlock()
			}
		}
		if errors.Is(err, io.EOF) {
			if total == aggregationObject && s.line.inWindow(now) {
				f.mu.Lock()
				f.ObjectsCompleted++
				f.objectCompletion = append(f.objectCompletion, now.Sub(started))
				f.mu.Unlock()
			}
			return
		}
		if err != nil {
			if now.Before(s.line.end) {
				s.errs.Add(1)
			}
			return
		}
	}
}

func (s *aggregationServer) datagrams(entry *aggregationServerConn) {
	for {
		b, err := entry.conn.ReceiveDatagram(context.Background())
		if err != nil {
			return
		}
		now := time.Now()
		if len(b) != aggregationDatagram {
			s.errs.Add(1)
			continue
		}
		label := b[0]
		entry.note(label)
		seq := binary.BigEndian.Uint64(b[1:])
		sent := time.Unix(0, int64(binary.BigEndian.Uint64(b[9:])))
		f := s.flow(label)
		f.mu.Lock()
		if _, dup := f.seen[seq]; dup {
			f.DatagramDuplicate++
		} else {
			f.seen[seq] = struct{}{}
			// Useful delivery counts messages offered within the window.
			if s.line.inWindow(sent) {
				f.DatagramsUseful++
				f.DatagramBytes += uint64(len(b))
				f.datagramLatency = append(f.datagramLatency, now.Sub(sent))
			}
		}
		f.mu.Unlock()
	}
}

func (s *aggregationServer) handle(entry *aggregationServerConn) {
	go s.datagrams(entry)
	for {
		str, err := entry.conn.AcceptUniStream(context.Background())
		if err != nil {
			return
		}
		go s.object(entry, str)
	}
}

func aggregationRunServer(t testing.TB, spec aggregationSpec) *aggregationServerResult {
	t.Helper()
	line := spec.timeline()
	tr := aggregationTransport(t, spec.Server, true)
	// The receiver runs the public profile. It returns each path's acknowledgments on
	// that path while the path can carry them, so a sender path trains on same-path
	// feedback whatever the receiver's own policy.
	ln, err := tr.Listen(aggregationServerTLS(), aggregationConfig(t, "reno", true))
	require.NoError(t, err)
	s := &aggregationServer{line: line, flows: make(map[byte]*aggregationFlowStats)}
	var wait sync.WaitGroup
	wait.Add(1)
	go func() {
		defer wait.Done()
		for {
			conn, err := ln.Accept(context.Background())
			if err != nil {
				return
			}
			entry := &aggregationServerConn{conn: conn, labels: make(map[byte]bool)}
			s.connsMu.Lock()
			s.conns = append(s.conns, entry)
			s.connsMu.Unlock()
			go s.handle(entry)
		}
	}()
	time.Sleep(time.Until(line.end.Add(aggregationDrain)))
	result := &aggregationServerResult{Go: runtime.Version(), Granted: true, Flows: make(map[string]*aggregationFlowStats)}
	s.connsMu.Lock()
	for _, entry := range s.conns {
		negotiated, _ := entry.conn.MultipathNegotiatedV1()
		cause, timeout := aggregationCloseCause(entry.conn)
		entry.mu.Lock()
		var labels []string
		for label := range entry.labels {
			labels = append(labels, aggregationLabelName(label))
		}
		entry.mu.Unlock()
		slices.Sort(labels)
		result.Connections = append(result.Connections, aggregationServerConnResult{Labels: labels, Multipath: negotiated, CloseCause: cause, TimeoutClose: timeout})
	}
	s.connsMu.Unlock()
	s.mu.Lock()
	for label, f := range s.flows {
		f.mu.Lock()
		f.Completion = aggregationSummary(f.objectCompletion)
		f.DatagramLatency = aggregationSummary(f.datagramLatency)
		f.mu.Unlock()
		result.Flows[aggregationLabelName(label)] = f
	}
	s.mu.Unlock()
	result.StreamErrs = s.errs.Load()
	ln.Close()
	require.NoError(t, tr.Close())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result.Joined = tr.WaitMultipathIOStoppedV1(ctx) == nil
	wait.Wait()
	return result
}

// ---- client ----

type aggregationPathResult struct {
	WireID    uint32 `json:"wire_id"`
	Lifecycle string `json:"lifecycle"`
	LocalUse  string `json:"local_use"`
	Primary   bool   `json:"primary"`
}

type aggregationClientFlow struct {
	Label            string                  `json:"label"`
	Controller       string                  `json:"controller"`
	Multipath        bool                    `json:"multipath_negotiated"`
	Scheduler        string                  `json:"scheduler"`
	Paths            []aggregationPathResult `json:"paths"`
	ObjectsStarted   uint64                  `json:"objects_started"`
	StreamBytesSent  uint64                  `json:"stream_bytes_written"`
	DatagramsOffered uint64                  `json:"datagrams_offered"`
	DatagramErrors   uint64                  `json:"datagram_send_errors"`
	PacketsSent      uint64                  `json:"packets_sent"`
	PacketsLost      uint64                  `json:"packets_lost"`
	CloseCause       string                  `json:"close_cause"`
	TimeoutClose     bool                    `json:"timeout_close"`

	conn     *Conn
	label    byte
	rate     int
	inFlight int
	started  atomic.Uint64
	written  atomic.Uint64
	offered  atomic.Uint64
	dgramErr atomic.Uint64
}

type aggregationClientResult struct {
	Go    string                   `json:"go"`
	Flows []*aggregationClientFlow `json:"flows"`
}

func (f *aggregationClientFlow) objects(line aggregationTimeline, payload []byte) {
	ctx, cancel := context.WithDeadline(context.Background(), line.end)
	defer cancel()
	for time.Now().Before(line.end) {
		str, err := f.conn.OpenUniStreamSync(ctx)
		if err != nil {
			return
		}
		str.SetWriteDeadline(line.end)
		f.started.Add(1)
		var header [9]byte
		header[0] = f.label
		binary.BigEndian.PutUint64(header[1:], uint64(time.Now().UnixNano()))
		if _, err := str.Write(header[:]); err != nil {
			str.CancelWrite(0)
			return
		}
		remaining := aggregationObject - len(header)
		for remaining > 0 {
			n, err := str.Write(payload[:min(remaining, len(payload))])
			f.written.Add(uint64(n))
			remaining -= n
			if err != nil {
				str.CancelWrite(0)
				return
			}
		}
		str.Close()
	}
}

func (f *aggregationClientFlow) datagrams(line aggregationTimeline) {
	if f.rate == 0 {
		return
	}
	period := time.Second / time.Duration(f.rate)
	b := make([]byte, aggregationDatagram)
	b[0] = f.label
	var seq uint64
	for next := line.start; next.Before(line.end); next = next.Add(period) {
		time.Sleep(time.Until(next))
		seq++
		now := time.Now()
		binary.BigEndian.PutUint64(b[1:], seq)
		binary.BigEndian.PutUint64(b[9:], uint64(now.UnixNano()))
		err := f.conn.SendDatagram(b)
		if line.inWindow(now) {
			f.offered.Add(1)
			if err != nil {
				f.dgramErr.Add(1)
			}
		}
	}
}

func aggregationDial(t testing.TB, tr *Transport, front string, conf *Config) *Conn {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp4", front)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), aggregationDialLead)
	defer cancel()
	conn, err := tr.Dial(ctx, addr, aggregationClientTLS(), conf)
	require.NoError(t, err)
	return conn
}

func aggregationScheduler(t testing.TB, conn *Conn) string {
	t.Helper()
	var scheduler pathScheduler
	require.NoError(t, conn.runPathCommand(func(r *pathCreditRegistry, _ monotime.Time) error {
		scheduler = r.scheduler
		return nil
	}))
	return aggregationSchedulerName(scheduler)
}

func aggregationPaths(conn *Conn) []aggregationPathResult {
	var paths []aggregationPathResult
	for _, path := range conn.PathsV1() {
		state, err := path.StateV1()
		if err != nil {
			continue
		}
		paths = append(paths, aggregationPathResult{WireID: state.WireID, Lifecycle: state.Lifecycle, LocalUse: state.LocalUse, Primary: state.Primary})
	}
	slices.SortFunc(paths, func(a, b aggregationPathResult) int { return int(a.WireID) - int(b.WireID) })
	return paths
}

func aggregationRunClient(t testing.TB, spec aggregationSpec) *aggregationClientResult {
	t.Helper()
	topology, ok := aggregationTopologies[spec.Topology]
	require.True(t, ok, "unknown topology %q", spec.Topology)
	arm, ok := aggregationArms[spec.Arm]
	require.True(t, ok, "unknown arm %q", spec.Arm)
	line := spec.timeline()
	time.Sleep(time.Until(line.start.Add(-aggregationDialLead)))

	// Both granted sockets exist in every arm; the competitor's is ordinary.
	ta := aggregationTransport(t, "127.0.0.1:0", true)
	tb := aggregationTransport(t, "127.0.0.1:0", true)
	transports := []*Transport{ta, tb}
	var flows []*aggregationClientFlow
	switch {
	case arm.Multipath:
		conf := aggregationConfig(t, arm.Controller, true)
		conf.multipath.evidence = arm.evidence()
		conn := aggregationDial(t, ta, spec.FrontA, conf)
		addr, err := net.ResolveUDPAddr("udp4", spec.FrontB)
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(context.Background(), aggregationDialLead)
		defer cancel()
		path, err := conn.OpenPathV1(ctx, tb, addr)
		require.NoError(t, err)
		// A newly validated path starts as backup, outside aggregation.
		require.NoError(t, path.SetLocalUseV1("active"))
		flows = append(flows, &aggregationClientFlow{conn: conn, label: aggregationFocalA, rate: aggregationDatagramRate, inFlight: aggregationObjectsInFlight})
	case arm.Paths == "ab":
		flows = append(flows,
			&aggregationClientFlow{conn: aggregationDial(t, ta, spec.FrontA, aggregationConfig(t, arm.Controller, false)), label: aggregationFocalA, rate: aggregationDatagramRate / 2, inFlight: aggregationObjectsInFlight / 2},
			&aggregationClientFlow{conn: aggregationDial(t, tb, spec.FrontB, aggregationConfig(t, arm.Controller, false)), label: aggregationFocalB, rate: aggregationDatagramRate / 2, inFlight: aggregationObjectsInFlight / 2})
	case arm.Paths == "a":
		flows = append(flows, &aggregationClientFlow{conn: aggregationDial(t, ta, spec.FrontA, aggregationConfig(t, arm.Controller, false)), label: aggregationFocalA, rate: aggregationDatagramRate, inFlight: aggregationObjectsInFlight})
	case arm.Paths == "b":
		flows = append(flows, &aggregationClientFlow{conn: aggregationDial(t, tb, spec.FrontB, aggregationConfig(t, arm.Controller, false)), label: aggregationFocalA, rate: aggregationDatagramRate, inFlight: aggregationObjectsInFlight})
	default:
		t.Fatalf("arm %q has no paths", spec.Arm)
	}
	if topology.Competing {
		tc := aggregationTransport(t, "127.0.0.1:0", false)
		transports = append(transports, tc)
		flows = append(flows, &aggregationClientFlow{conn: aggregationDial(t, tc, spec.FrontCompetitor, aggregationConfig(t, "reno", false)), label: aggregationCompetitor, inFlight: aggregationObjectsInFlight})
	}
	for _, f := range flows {
		f.Label = aggregationLabelName(f.label)
		f.Controller = f.conn.CongestionControlV1()
		if arm.Multipath && f.label == aggregationFocalA {
			f.Scheduler = aggregationScheduler(t, f.conn)
		}
	}
	require.False(t, time.Now().After(line.start), "setup outlasted its budget")
	time.Sleep(time.Until(line.start))

	rng := rand.New(rand.NewPCG(spec.Seed, spec.Seed))
	payload := make([]byte, aggregationChunk)
	for i := range payload {
		payload[i] = byte(rng.Uint32())
	}
	var wait sync.WaitGroup
	for _, f := range flows {
		for range f.inFlight {
			wait.Add(1)
			go func() {
				defer wait.Done()
				f.objects(line, payload)
			}()
		}
		wait.Add(1)
		go func() {
			defer wait.Done()
			f.datagrams(line)
		}()
	}
	wait.Wait()
	// Paths and statistics are read before close, which would retire the paths.
	time.Sleep(time.Until(line.end.Add(aggregationDrain / 2)))
	result := &aggregationClientResult{Go: runtime.Version()}
	for _, f := range flows {
		f.Multipath, _ = f.conn.MultipathNegotiatedV1()
		if f.Multipath {
			f.Paths = aggregationPaths(f.conn)
		}
		stats := f.conn.ConnectionStats()
		f.PacketsSent, f.PacketsLost = stats.PacketsSent, stats.PacketsLost
		f.CloseCause, f.TimeoutClose = aggregationCloseCause(f.conn)
		f.ObjectsStarted, f.StreamBytesSent = f.started.Load(), f.written.Load()
		f.DatagramsOffered, f.DatagramErrors = f.offered.Load(), f.dgramErr.Load()
		result.Flows = append(result.Flows, f)
	}
	for _, f := range flows {
		f.conn.CloseWithError(0, "")
	}
	for _, tr := range transports {
		tr.Close()
	}
	return result
}

// TestAggregationRole runs the role named by AGGREGATION_SPEC and writes its result.
func TestAggregationRole(t *testing.T) {
	raw := os.Getenv("AGGREGATION_SPEC")
	if raw == "" {
		t.Skip("the aggregation driver supplies AGGREGATION_SPEC")
	}
	var spec aggregationSpec
	require.NoError(t, json.Unmarshal([]byte(raw), &spec))
	var result any
	switch spec.Role {
	case "relay":
		result = aggregationRunRelay(t, spec)
	case "server":
		result = aggregationRunServer(t, spec)
	case "client":
		result = aggregationRunClient(t, spec)
	default:
		t.Fatalf("unknown role %q", spec.Role)
	}
	out, err := json.MarshalIndent(map[string]any{"role": spec.Role, "spec": spec, "result": result}, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(spec.Output, out, 0o644))
}

// The relay's queue serves bytes at its IPv4 rate and drops what would exceed its
// backlog limit.
func TestAggregationRelayQueue(t *testing.T) {
	q := newRelayQueue(aggregationQueue{RateBPS: 8_000_000, LimitBytes: 3_000})
	now := time.Unix(100, 0)
	// 972 bytes plus 28 of headers take 1 ms at 8 Mbit/s.
	first, ok := q.admit(now, 972)
	require.True(t, ok)
	require.Equal(t, now.Add(time.Millisecond), first)
	second, ok := q.admit(now, 972)
	require.True(t, ok)
	require.Equal(t, now.Add(2*time.Millisecond), second, "service follows the backlog")
	third, ok := q.admit(now, 972)
	require.True(t, ok)
	require.Equal(t, now.Add(3*time.Millisecond), third, "the limit admits exactly its bytes")
	_, ok = q.admit(now, 1)
	require.False(t, ok, "past the limit, the tail drops")
	later := now.Add(10 * time.Millisecond)
	leaves, ok := q.admit(later, 972)
	require.True(t, ok)
	require.Equal(t, later.Add(time.Millisecond), leaves, "an idle queue starts service at arrival")
}

// Every topology's links name an existing queue, each queue holds one
// bandwidth-delay product, and only the shared topology carries a competitor.
func TestAggregationTopologies(t *testing.T) {
	require.ElementsMatch(t, []string{"balanced", "unequal", "shared"}, aggregationKeys(aggregationTopologies))
	for name, topology := range aggregationTopologies {
		links := []aggregationLink{topology.A, topology.B}
		if topology.Competing {
			links = append(links, topology.Competitor)
		}
		for _, l := range links {
			require.Less(t, l.Queue, len(topology.Queues), name)
			q := topology.Queues[l.Queue]
			bdp := q.RateBPS / 8 * uint64(2*l.DelayNS) / uint64(time.Second)
			require.Equal(t, bdp, q.LimitBytes, "%s: queue %d holds one BDP", name, l.Queue)
		}
		require.Equal(t, name == "shared", topology.Competing)
	}
	require.Equal(t, aggregationTopologies["shared"].A.Queue, aggregationTopologies["shared"].B.Queue, "both paths share the bottleneck")
	require.NotEqual(t, aggregationTopologies["balanced"].A.Queue, aggregationTopologies["balanced"].B.Queue, "balanced paths are disjoint")
}

// The six arms are controller-matched: BBR baselines, informational Reno aggregate,
// and BBR aggregate beside its forced round-robin comparison.
func TestAggregationArms(t *testing.T) {
	require.ElementsMatch(t, []string{"A", "B", "C", "D", "E", "F"}, aggregationKeys(aggregationArms))
	for _, name := range []string{"A", "B", "C"} {
		arm := aggregationArms[name]
		require.Equal(t, "bbrv3", arm.Controller)
		require.False(t, arm.Multipath)
	}
	require.Equal(t, multipathEvidence{scheduler: scheduleAggregate}, aggregationArms["D"].evidence())
	require.Equal(t, multipathEvidence{bbr: true, scheduler: scheduleAggregate}, aggregationArms["E"].evidence())
	require.Equal(t, multipathEvidence{bbr: true, scheduler: scheduleRoundRobin}, aggregationArms["F"].evidence())
	for _, name := range []string{"D", "E", "F"} {
		require.Equal(t, "ab", aggregationArms[name].Paths)
		require.True(t, aggregationArms[name].Multipath)
	}
}

func aggregationKeys[V any](m map[string]V) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
