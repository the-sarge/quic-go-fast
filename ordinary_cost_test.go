//go:build ordinarycost && linux && amd64

package quic

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math"
	"math/bits"
	"math/rand/v2"
	"net"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/multipathguard"
	"github.com/quic-go/quic-go/internal/testdata"

	"github.com/stretchr/testify/require"
)

// This is R18's paired workload: interop/multipath/ordinary_cost.py runs one role per
// process on its own cores, and judges the results. Both roles take one shared wall-clock
// timeline, so no start message crosses the path being measured.
const (
	ordinaryCostALPN = "ordinary-cost"
	// ordinaryCostWriteBudget is the grant's native request duration, the default of a
	// socket the library owns.
	ordinaryCostWriteBudget = 100 * time.Millisecond

	ordinaryCostChunk       = 64 << 10
	ordinaryCostDatagram    = 1190
	ordinaryCostEcho        = 8
	ordinaryCostEchoPeriod  = 10 * time.Millisecond
	ordinaryCostFloodPacket = 100
	// The flood offers 10,000 unknown-CID packets each second, in 10 packet bursts.
	ordinaryCostFloodBurst  = 10
	ordinaryCostFloodPeriod = time.Millisecond
	// ordinaryCostBackgroundRate is the mixed cell's total background load in bytes each
	// second, one connection's in the opted-in profile and split evenly in the control.
	ordinaryCostBackgroundRate = 40 << 20

	ordinaryCostMemorySample = 20 * time.Second
	ordinaryCostDrain        = 2 * time.Second
	ordinaryCostStopTimeout  = 5 * time.Second
)

const (
	ordinaryCostStream byte = iota + 1
	ordinaryCostDatagrams
	ordinaryCostEchoes
)

const (
	ordinaryCostOrdinary   = "ordinary"
	ordinaryCostBackground = "background"
)

type ordinaryCostSpec struct {
	Role     string `json:"role"`
	Cell     string `json:"cell"`
	Profile  string `json:"profile"`
	Seed     uint64 `json:"seed"`
	Address  string `json:"address"`
	Output   string `json:"output"`
	StartAt  int64  `json:"start_at_unix_ns"`
	WarmupNS int64  `json:"warmup_ns"`
	WindowNS int64  `json:"window_ns"`
}

func (s ordinaryCostSpec) granted() bool { return s.Profile == "opted-in" }

func (s ordinaryCostSpec) timeline() ordinaryCostTimeline {
	start := time.Unix(0, s.StartAt)
	window := start.Add(time.Duration(s.WarmupNS))
	return ordinaryCostTimeline{start: start, window: window, end: window.Add(time.Duration(s.WindowNS))}
}

type ordinaryCostTimeline struct{ start, window, end time.Time }

func (l ordinaryCostTimeline) inWindow(t time.Time) bool {
	return !t.Before(l.window) && t.Before(l.end)
}

func (l ordinaryCostTimeline) seconds() int {
	return int(l.end.Sub(l.window) / time.Second)
}

// ordinaryCostHistogram is a log-linear latency histogram with 16 buckets per power of
// two, an upper-bound error of 6.25%. One goroutine owns it until its summary is read.
type ordinaryCostHistogram struct {
	counts [64 * 16]uint64
	total  uint64
	max    uint64
}

func ordinaryCostBucket(ns uint64) int {
	if ns < 16 {
		return int(ns)
	}
	exponent := bits.Len64(ns) - 5
	return exponent*16 + int(ns>>exponent)
}

// ordinaryCostBucketTop is the largest value bucket index holds.
func ordinaryCostBucketTop(index int) uint64 {
	if index < 16 {
		return uint64(index)
	}
	exponent := index/16 - 1
	mantissa := uint64(index%16 + 16)
	return (mantissa+1)<<exponent - 1
}

func (h *ordinaryCostHistogram) add(d time.Duration) {
	ns := uint64(max(d, 0))
	h.counts[ordinaryCostBucket(ns)]++
	h.total++
	h.max = max(h.max, ns)
}

func (h *ordinaryCostHistogram) merge(other *ordinaryCostHistogram) {
	for i, count := range other.counts {
		h.counts[i] += count
	}
	h.total += other.total
	h.max = max(h.max, other.max)
}

// quantile returns the upper bound of the bucket holding quantile q, in nanoseconds.
func (h *ordinaryCostHistogram) quantile(q float64) uint64 {
	if h.total == 0 {
		return 0
	}
	rank := uint64(math.Ceil(q * float64(h.total)))
	var seen uint64
	for i, count := range h.counts {
		seen += count
		if seen >= rank {
			return min(ordinaryCostBucketTop(i), h.max)
		}
	}
	return h.max
}

type ordinaryCostLatency struct {
	Count uint64  `json:"count"`
	P50US float64 `json:"p50_us"`
	P99US float64 `json:"p99_us"`
	MaxUS float64 `json:"max_us"`
}

func (h *ordinaryCostHistogram) summary() *ordinaryCostLatency {
	return &ordinaryCostLatency{
		Count: h.total,
		P50US: float64(h.quantile(0.50)) / 1e3,
		P99US: float64(h.quantile(0.99)) / 1e3,
		MaxUS: float64(h.max) / 1e3,
	}
}

// ordinaryCostSeries turns cumulative samples, one each second from the window's start,
// into the bytes of each second.
func ordinaryCostSeries(cumulative []uint64) []uint64 {
	series := make([]uint64, 0, max(len(cumulative)-1, 0))
	for i := 1; i < len(cumulative); i++ {
		series = append(series, cumulative[i]-cumulative[i-1])
	}
	return series
}

// ordinaryCostSample runs at each second of the window and returns when it ends. Each
// sample reads the counter at most a scheduling delay late, and a later sample recovers it.
func ordinaryCostSample(line ordinaryCostTimeline, read func() uint64) []uint64 {
	seconds := line.seconds()
	samples := make([]uint64, 0, seconds+1)
	for i := 0; i <= seconds; i++ {
		time.Sleep(time.Until(line.window.Add(time.Duration(i) * time.Second)))
		samples = append(samples, read())
	}
	return samples
}

func ordinaryCostTimeoutClose(err error) bool {
	var idle *IdleTimeoutError
	var handshake *HandshakeTimeoutError
	return errors.As(err, &idle) || errors.As(err, &handshake)
}

func ordinaryCostCloseCause(conn *Conn) (string, bool) {
	err := context.Cause(conn.Context())
	if err == nil {
		return "", false
	}
	return err.Error(), ordinaryCostTimeoutClose(err)
}

func ordinaryCostCPU() time.Duration {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		panic(err)
	}
	return time.Duration(usage.Utime.Nano() + usage.Stime.Nano())
}

var ordinaryCostHWM = regexp.MustCompile(`VmHWM:\s+(\d+) kB`)

func ordinaryCostRSSHighWater() (uint64, error) {
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0, err
	}
	match := ordinaryCostHWM.FindSubmatch(status)
	if match == nil {
		return 0, errors.New("no VmHWM")
	}
	return strconv.ParseUint(string(match[1]), 10, 64)
}

func ordinaryCostHeapInuse() uint64 {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats.HeapInuse
}

func ordinaryCostConfig(t testing.TB, multipath bool) *Config {
	t.Helper()
	conf := &Config{
		EnableDatagrams:                true,
		InitialStreamReceiveWindow:     4 << 20,
		MaxStreamReceiveWindow:         16 << 20,
		InitialConnectionReceiveWindow: 8 << 20,
		MaxConnectionReceiveWindow:     24 << 20,
		MaxIdleTimeout:                 30 * time.Second,
	}
	require.NoError(t, conf.SetCongestionControlV1("reno"))
	if multipath {
		require.NoError(t, conf.ConfigureMultipathV1(true, 2, "primary-backup"))
	}
	return conf
}

func ordinaryCostServerTLS() *tls.Config {
	conf := testdata.GetTLSConfig()
	conf.NextProtos = []string{ordinaryCostALPN}
	return conf
}

func ordinaryCostClientTLS() *tls.Config {
	return &tls.Config{RootCAs: testdata.GetRootCA(), ServerName: "localhost", NextProtos: []string{ordinaryCostALPN}}
}

func ordinaryCostTransport(t testing.TB, address string, granted bool) *Transport {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", address)
	require.NoError(t, err)
	socket, err := net.ListenUDP("udp", addr)
	require.NoError(t, err)
	t.Cleanup(func() { socket.Close() })
	tr := &Transport{Conn: socket, ConnectionIDLength: 4}
	if granted {
		require.NoError(t, tr.ConfigureMultipathIOV1(ordinaryCostWriteBudget))
	}
	return tr
}

// ordinaryCostRequest is what a client asks of a server stream.
type ordinaryCostRequest struct {
	kind  byte
	label byte
	rate  uint64 // bytes each second; zero offers as much as the sender accepts
}

func (r ordinaryCostRequest) labelName() string {
	if r.label == 0 {
		return ordinaryCostOrdinary
	}
	return ordinaryCostBackground
}

func (r ordinaryCostRequest) encode() []byte {
	b := make([]byte, 10)
	b[0], b[1] = r.kind, r.label
	binary.BigEndian.PutUint64(b[2:], r.rate)
	return b
}

func ordinaryCostDecode(b []byte) ordinaryCostRequest {
	return ordinaryCostRequest{kind: b[0], label: b[1], rate: binary.BigEndian.Uint64(b[2:])}
}

// ordinaryCostPacer holds a sender to its rate; a zero rate never waits.
type ordinaryCostPacer struct {
	rate uint64
	next time.Time
}

func (p *ordinaryCostPacer) spent(bytes int) {
	if p.rate == 0 {
		return
	}
	now := time.Now()
	if p.next.IsZero() {
		p.next = now
	}
	p.next = p.next.Add(time.Duration(uint64(bytes) * uint64(time.Second) / p.rate))
	if wait := p.next.Sub(now); wait > 0 {
		time.Sleep(wait)
	}
}

type ordinaryCostServer struct {
	line ordinaryCostTimeline

	mutex       sync.Mutex
	histograms  map[string]*ordinaryCostHistogram
	writeErrors uint64
	conns       []*ordinaryCostServerConn
	workers     sync.WaitGroup
	gso         atomic.Bool
}

type ordinaryCostServerConn struct {
	conn  *Conn
	label atomic.Value
}

func (s *ordinaryCostServer) record(label string, h *ordinaryCostHistogram, writeErrors uint64) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	merged, ok := s.histograms[label]
	if !ok {
		merged = &ordinaryCostHistogram{}
		s.histograms[label] = merged
	}
	merged.merge(h)
	s.writeErrors += writeErrors
}

// finished reports that the timeline is over, which explains a later send error.
func (s *ordinaryCostServer) finished() bool { return !time.Now().Before(s.line.end) }

func (s *ordinaryCostServer) bulk(str *Stream, request ordinaryCostRequest) {
	histogram := &ordinaryCostHistogram{}
	var failures uint64
	payload := make([]byte, ordinaryCostChunk)
	pacer := ordinaryCostPacer{rate: request.rate}
	for !s.finished() {
		begun := time.Now()
		_, err := str.Write(payload)
		if s.line.inWindow(begun) {
			histogram.add(time.Since(begun))
		}
		if err != nil {
			if !s.finished() {
				failures++
			}
			break
		}
		pacer.spent(len(payload))
	}
	str.CancelWrite(0)
	s.record(request.labelName(), histogram, failures)
}

func (s *ordinaryCostServer) datagrams(conn *Conn, request ordinaryCostRequest) {
	histogram := &ordinaryCostHistogram{}
	var failures uint64
	payload := make([]byte, ordinaryCostDatagram)
	var sequence uint64
	pacer := ordinaryCostPacer{rate: request.rate}
	for !s.finished() {
		sequence++
		binary.BigEndian.PutUint64(payload, sequence)
		begun := time.Now()
		err := conn.SendDatagram(payload)
		if s.line.inWindow(begun) {
			histogram.add(time.Since(begun))
		}
		if err != nil {
			if !s.finished() {
				failures++
			}
			break
		}
		pacer.spent(len(payload))
	}
	s.record(request.labelName(), histogram, failures)
}

func (s *ordinaryCostServer) echoes(str *Stream) {
	message := make([]byte, ordinaryCostEcho)
	for {
		if _, err := io.ReadFull(str, message); err != nil {
			return
		}
		if _, err := str.Write(message); err != nil {
			return
		}
	}
}

func (s *ordinaryCostServer) handle(entry *ordinaryCostServerConn) {
	defer s.workers.Done()
	for {
		str, err := entry.conn.AcceptStream(context.Background())
		if err != nil {
			return
		}
		header := make([]byte, 10)
		if _, err := io.ReadFull(str, header); err != nil {
			return
		}
		request := ordinaryCostDecode(header)
		entry.label.Store(request.labelName())
		s.workers.Add(1)
		go func() {
			defer s.workers.Done()
			switch request.kind {
			case ordinaryCostStream:
				s.bulk(str, request)
			case ordinaryCostDatagrams:
				s.datagrams(entry.conn, request)
			case ordinaryCostEchoes:
				s.echoes(str)
			}
		}()
	}
}

type ordinaryCostServerConnResult struct {
	Label        string `json:"label"`
	Multipath    bool   `json:"multipath_negotiated"`
	PacketsSent  uint64 `json:"packets_sent"`
	PacketsLost  uint64 `json:"packets_lost"`
	BytesSent    uint64 `json:"bytes_sent"`
	CloseCause   string `json:"close_cause"`
	TimeoutClose bool   `json:"timeout_close"`
}

type ordinaryCostServerResult struct {
	Go             string                          `json:"go"`
	Granted        bool                            `json:"granted"`
	GSOCapable     bool                            `json:"gso_capable"`
	WindowCPU      float64                         `json:"window_cpu_s"`
	RSSAt20S       uint64                          `json:"rss_kb_at_20s"`
	RSSEnd         uint64                          `json:"rss_kb_end"`
	HeapInuseEnd   uint64                          `json:"heap_inuse_end"`
	WriteErrors    uint64                          `json:"write_errors"`
	Joined         bool                            `json:"joined"`
	SendCompletion map[string]*ordinaryCostLatency `json:"send_completion"`
	Connections    []ordinaryCostServerConnResult  `json:"connections"`
}

func ordinaryCostRunServer(t testing.TB, spec ordinaryCostSpec) *ordinaryCostServerResult {
	t.Helper()
	line := spec.timeline()
	var key StatelessResetKey
	binary.BigEndian.PutUint64(key[:], spec.Seed)
	tr := ordinaryCostTransport(t, spec.Address, spec.granted())
	tr.StatelessResetKey = &key
	listener, err := tr.Listen(ordinaryCostServerTLS(), ordinaryCostConfig(t, spec.granted()))
	require.NoError(t, err)
	s := &ordinaryCostServer{line: line, histograms: map[string]*ordinaryCostHistogram{}}
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		for {
			conn, err := listener.Accept(context.Background())
			if err != nil {
				return
			}
			entry := &ordinaryCostServerConn{conn: conn}
			s.gso.Store(conn.ConnectionState().GSO)
			s.mutex.Lock()
			s.conns = append(s.conns, entry)
			s.mutex.Unlock()
			s.workers.Add(1)
			go s.handle(entry)
		}
	}()

	result := &ordinaryCostServerResult{Go: runtime.Version(), Granted: tr.responses != nil, SendCompletion: map[string]*ordinaryCostLatency{}}
	time.Sleep(time.Until(line.window))
	cpuStart := ordinaryCostCPU()
	sampleAt := min(ordinaryCostMemorySample, line.end.Sub(line.window)/3)
	time.Sleep(time.Until(line.window.Add(sampleAt)))
	result.RSSAt20S, err = ordinaryCostRSSHighWater()
	require.NoError(t, err)
	time.Sleep(time.Until(line.end))
	result.WindowCPU = (ordinaryCostCPU() - cpuStart).Seconds()
	result.RSSEnd, err = ordinaryCostRSSHighWater()
	require.NoError(t, err)
	result.HeapInuseEnd = ordinaryCostHeapInuse()

	// The client holds its connections past the end, so any close seen here is the run's.
	s.mutex.Lock()
	for _, entry := range s.conns {
		stats := entry.conn.ConnectionStats()
		cause, timedOut := ordinaryCostCloseCause(entry.conn)
		label, _ := entry.label.Load().(string)
		negotiated, err := entry.conn.MultipathNegotiatedV1()
		require.NoError(t, err)
		result.Connections = append(result.Connections, ordinaryCostServerConnResult{
			Label: label, Multipath: negotiated,
			PacketsSent: stats.PacketsSent, PacketsLost: stats.PacketsLost, BytesSent: stats.BytesSent,
			CloseCause: cause, TimeoutClose: timedOut,
		})
	}
	conns := len(s.conns)
	s.mutex.Unlock()
	require.NotZero(t, conns, "no connection reached the server")
	result.GSOCapable = s.gso.Load()

	time.Sleep(ordinaryCostDrain)
	require.NoError(t, listener.Close())
	require.NoError(t, tr.Close())
	if spec.granted() {
		ctx, cancel := context.WithTimeout(context.Background(), ordinaryCostStopTimeout)
		defer cancel()
		result.Joined = tr.WaitMultipathIOStoppedV1(ctx) == nil
	}
	s.workers.Wait()
	s.mutex.Lock()
	defer s.mutex.Unlock()
	result.WriteErrors = s.writeErrors
	for label, histogram := range s.histograms {
		result.SendCompletion[label] = histogram.summary()
	}
	return result
}

type ordinaryCostFlowResult struct {
	Label        string               `json:"label"`
	Series       []uint64             `json:"series"`
	UsefulBytes  uint64               `json:"useful_bytes"`
	Control      *ordinaryCostLatency `json:"control"`
	Drops        uint64               `json:"datagram_drops"`
	Unprocessed  uint64               `json:"unprocessed_drops"`
	CloseCause   string               `json:"close_cause"`
	TimeoutClose bool                 `json:"timeout_close"`
	Negotiated   bool                 `json:"multipath_negotiated"`
	Paths        []string             `json:"paths"`
}

type ordinaryCostFloodResult struct {
	Sent   uint64 `json:"sent"`
	Resets uint64 `json:"resets"`
}

type ordinaryCostClientResult struct {
	Go        string                    `json:"go"`
	WindowCPU float64                   `json:"window_cpu_s"`
	Flows     []*ordinaryCostFlowResult `json:"flows"`
	Flood     *ordinaryCostFloodResult  `json:"flood,omitempty"`
}

// ordinaryCostFlow is one client connection's receiving side.
type ordinaryCostFlow struct {
	label  string
	conn   *Conn
	useful atomic.Uint64
	echo   ordinaryCostHistogram
	result *ordinaryCostFlowResult
}

func (f *ordinaryCostFlow) request(t testing.TB, r ordinaryCostRequest) *Stream {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	str, err := f.conn.OpenStreamSync(ctx)
	require.NoError(t, err)
	_, err = str.Write(r.encode())
	require.NoError(t, err)
	return str
}

func (f *ordinaryCostFlow) readBulk(str *Stream) {
	buffer := make([]byte, ordinaryCostChunk)
	for {
		n, err := str.Read(buffer)
		f.useful.Add(uint64(n))
		if err != nil {
			return
		}
	}
}

func (f *ordinaryCostFlow) readDatagrams() {
	for {
		payload, err := f.conn.ReceiveDatagram(context.Background())
		if err != nil {
			return
		}
		f.useful.Add(uint64(len(payload)))
	}
}

// runEcho is a closed loop: the next message waits for the last reply and the next period.
func (f *ordinaryCostFlow) runEcho(line ordinaryCostTimeline, str *Stream) {
	message := make([]byte, ordinaryCostEcho)
	ticker := time.NewTicker(ordinaryCostEchoPeriod)
	defer ticker.Stop()
	for now := time.Now(); now.Before(line.end); now = <-ticker.C {
		begun := time.Now()
		if _, err := str.Write(message); err != nil {
			return
		}
		if _, err := io.ReadFull(str, message); err != nil {
			return
		}
		if line.inWindow(begun) {
			f.echo.add(time.Since(begun))
		}
	}
}

func ordinaryCostDial(t testing.TB, tr *Transport, address *net.UDPAddr, multipath bool) *Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	conn, err := tr.Dial(ctx, address, ordinaryCostClientTLS(), ordinaryCostConfig(t, multipath))
	require.NoError(t, err)
	return conn
}

// ordinaryCostFlood sends unknown-CID packets to the server's socket and counts the
// stateless resets that come back, from the shared timeline's start until its end.
func ordinaryCostFlood(t testing.TB, line ordinaryCostTimeline, address *net.UDPAddr, seed uint64) (*ordinaryCostFloodResult, func()) {
	t.Helper()
	socket, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	t.Cleanup(func() { socket.Close() })
	result := &ordinaryCostFloodResult{}
	var sent, resets atomic.Uint64
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		buffer := make([]byte, 2048)
		for {
			if _, _, err := socket.ReadFromUDP(buffer); err != nil {
				return
			}
			if line.inWindow(time.Now()) {
				resets.Add(1)
			}
		}
	}()
	go func() {
		defer wait.Done()
		random := rand.New(rand.NewPCG(seed, seed))
		packet := make([]byte, ordinaryCostFloodPacket)
		time.Sleep(time.Until(line.start))
		ticker := time.NewTicker(ordinaryCostFloodPeriod)
		defer ticker.Stop()
		for now := range ticker.C {
			if !now.Before(line.end) {
				return
			}
			for range ordinaryCostFloodBurst {
				for i := 0; i+8 <= len(packet); i += 8 {
					binary.BigEndian.PutUint64(packet[i:], random.Uint64())
				}
				packet[0] = 0x40 | packet[0]&0x3f
				if _, err := socket.WriteToUDP(packet, address); err != nil {
					continue
				}
				if line.inWindow(now) {
					sent.Add(1)
				}
			}
		}
	}()
	return result, func() {
		time.Sleep(time.Until(line.end.Add(ordinaryCostDrain / 2)))
		socket.Close()
		wait.Wait()
		result.Sent, result.Resets = sent.Load(), resets.Load()
	}
}

func ordinaryCostRunClient(t testing.TB, spec ordinaryCostSpec) *ordinaryCostClientResult {
	t.Helper()
	line := spec.timeline()
	address, err := net.ResolveUDPAddr("udp", spec.Address)
	require.NoError(t, err)
	var transports []*Transport
	dial := func(multipath, granted bool) (*Transport, *Conn) {
		tr := ordinaryCostTransport(t, "127.0.0.1:0", granted)
		transports = append(transports, tr)
		return tr, ordinaryCostDial(t, tr, address, multipath)
	}

	_, ordinaryConn := dial(false, false)
	ordinary := &ordinaryCostFlow{label: ordinaryCostOrdinary, conn: ordinaryConn}
	flows := []*ordinaryCostFlow{ordinary}
	type bulkFlow struct {
		flow *ordinaryCostFlow
		rate uint64
	}
	var bulks []bulkFlow
	var multipathConn *Conn
	var paths []*Path
	switch spec.Cell {
	case "stream-gso-on", "stream-gso-off", "stateless-pressure":
		bulks = append(bulks, bulkFlow{ordinary, 0})
	case "datagram-batch-on", "datagram-batch-off":
	case "mixed-bulk":
		bulks = append(bulks, bulkFlow{ordinary, 0})
		if spec.granted() {
			_, conn := dial(true, true)
			second := ordinaryCostTransport(t, "127.0.0.1:0", true)
			transports = append(transports, second)
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			_, err := conn.OpenPathV1(ctx, second, address)
			require.NoError(t, err)
			paths = conn.PathsV1()
			multipathConn = conn
			flow := &ordinaryCostFlow{label: ordinaryCostBackground, conn: conn}
			flows = append(flows, flow)
			bulks = append(bulks, bulkFlow{flow, ordinaryCostBackgroundRate})
		} else {
			// Two independent ordinary connections stand in for the multipath one, on the
			// same socket count, with the same total load.
			for range 2 {
				_, conn := dial(false, false)
				flow := &ordinaryCostFlow{label: ordinaryCostBackground, conn: conn}
				flows = append(flows, flow)
				bulks = append(bulks, bulkFlow{flow, ordinaryCostBackgroundRate / 2})
			}
		}
	default:
		t.Fatalf("unknown cell %q", spec.Cell)
	}
	require.False(t, time.Now().After(line.start), "setup outlasted its budget")
	time.Sleep(time.Until(line.start))

	var wait sync.WaitGroup
	for _, bulk := range bulks {
		label := byte(0)
		if bulk.flow.label == ordinaryCostBackground {
			label = 1
		}
		str := bulk.flow.request(t, ordinaryCostRequest{kind: ordinaryCostStream, label: label, rate: bulk.rate})
		wait.Add(1)
		go func(flow *ordinaryCostFlow) {
			defer wait.Done()
			flow.readBulk(str)
		}(bulk.flow)
	}
	if len(bulks) == 0 {
		ordinary.request(t, ordinaryCostRequest{kind: ordinaryCostDatagrams})
		wait.Add(1)
		go func() {
			defer wait.Done()
			ordinary.readDatagrams()
		}()
	}
	echo := ordinary.request(t, ordinaryCostRequest{kind: ordinaryCostEchoes})
	wait.Add(1)
	go func() {
		defer wait.Done()
		ordinary.runEcho(line, echo)
	}()
	var flood *ordinaryCostFloodResult
	finishFlood := func() {}
	if spec.Cell == "stateless-pressure" {
		flood, finishFlood = ordinaryCostFlood(t, line, address, spec.Seed)
	}

	samples := make([][]uint64, len(flows))
	var sampling sync.WaitGroup
	for i, flow := range flows {
		sampling.Add(1)
		go func() {
			defer sampling.Done()
			samples[i] = ordinaryCostSample(line, flow.useful.Load)
		}()
	}
	time.Sleep(time.Until(line.window))
	cpuStart := ordinaryCostCPU()
	sampling.Wait()
	result := &ordinaryCostClientResult{Go: runtime.Version(), WindowCPU: (ordinaryCostCPU() - cpuStart).Seconds()}

	// Stay connected through the server's end-of-run reading, then close.
	for i, flow := range flows {
		series := ordinaryCostSeries(samples[i])
		var useful uint64
		for _, bytes := range series {
			useful += bytes
		}
		cause, timedOut := ordinaryCostCloseCause(flow.conn)
		flow.result = &ordinaryCostFlowResult{
			Label: flow.label, Series: series, UsefulBytes: useful,
			Drops: flow.conn.DatagramReceiveDropsV1(), Unprocessed: flow.conn.UnprocessedPacketDropsV1(),
			CloseCause: cause, TimeoutClose: timedOut,
		}
		result.Flows = append(result.Flows, flow.result)
		if flow.conn == multipathConn {
			flow.result.Negotiated, err = flow.conn.MultipathNegotiatedV1()
			require.NoError(t, err)
			for _, path := range paths {
				state, err := path.StateV1()
				require.NoError(t, err)
				flow.result.Paths = append(flow.result.Paths, state.Lifecycle)
			}
		}
	}
	time.Sleep(ordinaryCostDrain)
	for _, flow := range flows {
		require.NoError(t, flow.conn.CloseWithError(0, ""))
	}
	wait.Wait()
	ordinary.result.Control = ordinary.echo.summary()
	finishFlood()
	result.Flood = flood
	for _, tr := range transports {
		require.NoError(t, tr.Close())
	}
	return result
}

// TestOrdinaryCostRole runs the one role that ORDINARY_COST_SPEC names.
func TestOrdinaryCostRole(t *testing.T) {
	var spec ordinaryCostSpec
	require.NoError(t, json.Unmarshal([]byte(os.Getenv("ORDINARY_COST_SPEC")), &spec), "the driver supplies the spec")
	if spec.granted() {
		multipathguard.Active.Store(true)
	}
	var result any
	switch spec.Role {
	case "server":
		result = ordinaryCostRunServer(t, spec)
	case "client":
		result = ordinaryCostRunClient(t, spec)
	default:
		t.Fatalf("unknown role %q", spec.Role)
	}
	data, err := json.MarshalIndent(result, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(spec.Output, data, 0o644))
}

func TestOrdinaryCostHistogram(t *testing.T) {
	var empty ordinaryCostHistogram
	require.Zero(t, empty.quantile(0.99))

	for _, ns := range []uint64{0, 1, 15, 16, 17, 31, 32, 33, 1000, 123456789, 1 << 40} {
		top := ordinaryCostBucketTop(ordinaryCostBucket(ns))
		require.GreaterOrEqual(t, top, ns, "the bucket of %d holds it", ns)
		require.LessOrEqual(t, float64(top), float64(ns)*1.0625+1, "the bucket of %d stays within 6.25%%", ns)
	}
	require.Less(t, ordinaryCostBucket(math.MaxInt64), 64*16)

	var h ordinaryCostHistogram
	for i := 1; i <= 1000; i++ {
		h.add(time.Duration(i) * time.Microsecond)
	}
	p99 := h.quantile(0.99)
	require.GreaterOrEqual(t, p99, uint64(990_000))
	require.LessOrEqual(t, p99, uint64(1_000_000), "a quantile never reads past the maximum")
	summary := h.summary()
	require.Equal(t, uint64(1000), summary.Count)
	require.Equal(t, 1000.0, summary.MaxUS)

	var merged ordinaryCostHistogram
	merged.merge(&h)
	merged.merge(&h)
	require.Equal(t, uint64(2000), merged.total)
	require.Equal(t, p99, merged.quantile(0.99))
	h.add(-time.Second)
	require.Equal(t, uint64(1), h.counts[0], "a negative duration reads as zero")
}

func TestOrdinaryCostAccounting(t *testing.T) {
	require.Empty(t, ordinaryCostSeries(nil))
	require.Equal(t, []uint64{5, 0, 7}, ordinaryCostSeries([]uint64{10, 15, 15, 22}))

	request := ordinaryCostRequest{kind: ordinaryCostStream, label: 1, rate: 1 << 40}
	decoded := ordinaryCostDecode(request.encode())
	require.Equal(t, request, decoded)
	require.Equal(t, ordinaryCostBackground, decoded.labelName())
	require.Equal(t, ordinaryCostOrdinary, ordinaryCostRequest{}.labelName())

	start := time.Now()
	line := ordinaryCostTimeline{start: start, window: start.Add(time.Second), end: start.Add(3 * time.Second)}
	require.False(t, line.inWindow(start))
	require.True(t, line.inWindow(line.window))
	require.False(t, line.inWindow(line.end), "the end belongs to the next phase")
	require.Equal(t, 2, line.seconds())

	unpaced := ordinaryCostPacer{}
	begun := time.Now()
	unpaced.spent(1 << 30)
	require.Less(t, time.Since(begun), time.Second)
	paced := ordinaryCostPacer{rate: 1 << 20}
	begun = time.Now()
	paced.spent(1 << 17)
	require.GreaterOrEqual(t, time.Since(begun), 100*time.Millisecond, "an eighth of a second's bytes")

	require.True(t, ordinaryCostTimeoutClose(&IdleTimeoutError{}))
	require.True(t, ordinaryCostTimeoutClose(&HandshakeTimeoutError{}))
	require.False(t, ordinaryCostTimeoutClose(&ApplicationError{}))
	require.False(t, ordinaryCostTimeoutClose(nil))
}

// TestOrdinaryCostCells runs both roles of every cell in one process for a moment. It
// checks the workload's wiring, and its numbers mean nothing: only the driver's separate
// processes and pinned cores measure.
func TestOrdinaryCostCells(t *testing.T) {
	for _, profile := range []string{"unregistered", "opted-in"} {
		for _, cell := range []string{"stream-gso-on", "stream-gso-off", "datagram-batch-on", "datagram-batch-off", "mixed-bulk", "stateless-pressure"} {
			t.Run(profile+"/"+cell, func(t *testing.T) {
				if profile == "opted-in" {
					activateMultipathProfile(t)
				}
				off := cell == "stream-gso-off" || cell == "datagram-batch-off"
				if off {
					t.Setenv("QUIC_GO_DISABLE_GSO", "true")
				}
				probe := newUDPConnLocalhost(t)
				address := probe.LocalAddr().String()
				require.NoError(t, probe.Close())
				spec := ordinaryCostSpec{
					Cell: cell, Profile: profile, Seed: 1, Address: address,
					StartAt: time.Now().Add(time.Second).UnixNano(), WarmupNS: int64(200 * time.Millisecond), WindowNS: int64(time.Second),
				}
				servers := make(chan *ordinaryCostServerResult, 1)
				go func() {
					defer close(servers)
					servers <- ordinaryCostRunServer(t, spec)
				}()
				client := ordinaryCostRunClient(t, spec)
				server := <-servers
				require.NotNil(t, server)

				require.Equal(t, profile == "opted-in", server.Granted)
				require.Equal(t, profile == "opted-in", server.Joined)
				require.Equal(t, !off, server.GSOCapable)
				require.Zero(t, server.WriteErrors)
				require.NotZero(t, server.SendCompletion[ordinaryCostOrdinary].Count)
				require.NotZero(t, server.WindowCPU)
				require.NotZero(t, server.RSSEnd)

				background := 0
				for _, flow := range client.Flows {
					require.Len(t, flow.Series, 1)
					require.NotZero(t, flow.UsefulBytes, flow.Label)
					require.Empty(t, flow.CloseCause)
					if flow.Label == ordinaryCostOrdinary {
						require.NotZero(t, flow.Control.Count)
						continue
					}
					background++
					if profile == "opted-in" {
						require.True(t, flow.Negotiated)
						require.Equal(t, []string{"live", "live"}, flow.Paths)
					}
				}
				switch {
				case cell != "mixed-bulk":
					require.Zero(t, background)
				case profile == "opted-in":
					require.Equal(t, 1, background)
				default:
					require.Equal(t, 2, background)
				}
				for _, conn := range server.Connections {
					require.Empty(t, conn.CloseCause)
				}
				if cell == "stateless-pressure" {
					require.NotZero(t, client.Flood.Sent)
					require.NotZero(t, client.Flood.Resets, "unknown connection IDs draw stateless resets")
				} else {
					require.Nil(t, client.Flood)
				}
			})
		}
	}
}
