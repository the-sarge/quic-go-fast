// Local-only two-flow impairment relay for one qualification ticket (#777,
// BBRv3 L6 coexistence). It applies the frozen Q1 packet-path model between two
// loopback endpoint pairs that share one bottleneck queue per direction, and
// never changes host network state. It is a finite experiment aid, not a
// maintained emulator.
//
// It is the #711/#712 relay (relay/main.go plus relay/linux-ecn.patch in the
// #748 slice 6 readiness record at 9b38f0ab) with these recorded changes:
//   - two socket pairs, one per flow. Arrivals on the competitor pair are
//     admitted with Packet.Competitor set in both directions, so the frozen L6
//     schedule (model.go: 12.5 ms one-way while service finishes in
//     [100 s, 200 s) of model time, otherwise 50 ms) applies to the
//     competitor's data and acknowledgments alike;
//   - deliveries are routed by that flag to the flow's own endpoint;
//   - per-flow counters, a per-flow arrival sequence carried in the relay-owned
//     header headroom (never written to the network) to count reordered
//     deliveries, and 100 ms samples of per-flow queue occupancy, drops and
//     delivered bytes;
//   - model time zero is the measured start, as Q1's router used it.
//
// The 10 ms sampler wakeup, the timer, the arrival channel and the event order
// are unchanged.
package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
	"q1campaign/model"
)

// IPv4 plus UDP headers: the model charges full IP packet bytes. The relay
// owns this headroom and stores the arrival sequence in its first eight bytes.
const ipUDPHeader = 28

const (
	focal      = 0
	competitor = 1
)

// sampleEvery is the number of 10 ms sampler ticks per recorded sample.
const sampleEvery = 10

type arrival struct {
	at      time.Duration
	forward bool
	flow    int
	bytes   []byte
	ecn     uint8
}

type flowStats struct {
	Received, Admitted, Dropped, Delivered uint64
	DeliveredBytes                         uint64
	// Deliveries whose arrival sequence is below the highest one already
	// delivered for this flow and direction, and the largest such distance.
	Reordered, MaxReorderDistance uint64
	ECNIn, ECNOut                 [4]uint64
	SendErrors, TruncatedRead     uint64
	MaxPayload                    int
}

type directionReport struct {
	Stats             model.Stats
	MaxLateNS         int64
	LateOver1ms       uint64
	Focal, Competitor flowStats
}

func (d *directionReport) flow(i int) *flowStats {
	if i == competitor {
		return &d.Competitor
	}
	return &d.Focal
}

type flowAddresses struct {
	Front, Back, Sender, Target string
}

type report struct {
	Scenario          string
	Seed              uint64
	Focal, Competitor flowAddresses
	StartUnixNS       int64
	EndUnixNS         int64
	Forward           directionReport
	Reverse           directionReport
	MaxArrivalLagNS   int64
	// Every 100 ms of model time: [ms, forward focal queue bytes, forward
	// competitor queue bytes, reverse focal queue bytes, reverse competitor
	// queue bytes, cumulative forward focal drops, cumulative forward competitor
	// drops, cumulative forward focal delivered bytes, cumulative forward
	// competitor delivered bytes, max forward lateness ns in the interval].
	Samples [][10]int64
	// Forward packets written per relay wakeup: index = batch size (last bucket = 64+).
	ForwardBatchHistogram [65]uint64
	Error                 string `json:",omitempty"`
}

type config struct {
	scenario    string
	seed        uint64
	flows       [2]flowAddresses
	startUnixNS int64
	until       time.Duration
}

// path is one direction of the shared bottleneck.
type path struct {
	q       *model.Queue
	r       *directionReport
	seq     [2]uint64 // last arrival sequence assigned, per flow
	highest [2]uint64 // highest arrival sequence delivered, per flow
}

// admit tags the packet with its per-flow arrival sequence and offers it to the
// model. Call q.Advance(at) first, as the model requires.
func (p *path) admit(at time.Duration, flow int, b []byte, ecn uint8) bool {
	f := p.r.flow(flow)
	f.Received++
	f.ECNIn[ecn&3]++
	f.MaxPayload = max(f.MaxPayload, len(b)-ipUDPHeader)
	p.seq[flow]++
	binary.BigEndian.PutUint64(b[:8], p.seq[flow])
	if !p.q.Admit(at, model.Packet{Bytes: b, ECN: ecn, Competitor: flow == competitor}) {
		f.Dropped++
		return false
	}
	f.Admitted++
	return true
}

// delivered accounts deliveries per flow before the caller writes them.
func (p *path) delivered(out []model.Packet) {
	for _, pk := range out {
		i := flowOf(pk)
		f := p.r.flow(i)
		f.Delivered++
		f.DeliveredBytes += uint64(len(pk.Bytes))
		f.ECNOut[pk.ECN&3]++
		if s := binary.BigEndian.Uint64(pk.Bytes[:8]); s < p.highest[i] {
			f.Reordered++
			f.MaxReorderDistance = max(f.MaxReorderDistance, p.highest[i]-s)
		} else {
			p.highest[i] = s
		}
	}
}

func flowOf(p model.Packet) int {
	if p.Competitor {
		return competitor
	}
	return focal
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func listen(addr string) (*net.UDPConn, error) {
	a, err := net.ResolveUDPAddr("udp4", addr)
	if err != nil {
		return nil, err
	}
	c, err := net.ListenUDP("udp4", a)
	if err != nil {
		return nil, err
	}
	raw, err := c.SyscallConn()
	if err != nil {
		c.Close()
		return nil, err
	}
	var serr error
	if err = raw.Control(func(fd uintptr) {
		serr = errors.Join(unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_RECVTOS, 1),
			unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_RCVBUF, 8<<20),
			unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_SNDBUF, 8<<20))
	}); err != nil {
		c.Close()
		return nil, err
	}
	if serr != nil {
		c.Close()
	}
	return c, serr
}

func readECN(oob []byte) uint8 {
	msgs, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return 0
	}
	for _, m := range msgs {
		// Linux ECN adapter (#712): Linux delivers the received TOS byte as an
		// IP_TOS control message, where Darwin uses IP_RECVTOS.
		if m.Header.Level == unix.IPPROTO_IP && (m.Header.Type == unix.IP_RECVTOS || m.Header.Type == unix.IP_TOS) && len(m.Data) > 0 {
			return m.Data[0] & 3
		}
	}
	return 0
}

func ecnOOB(ecn uint8) []byte {
	b := make([]byte, unix.CmsgSpace(4))
	h := (*unix.Cmsghdr)(unsafe.Pointer(&b[0]))
	h.Level = unix.IPPROTO_IP
	h.Type = unix.IP_TOS
	h.SetLen(unix.CmsgLen(4))
	data := b[unix.CmsgLen(0):]
	data[0] = ecn
	return b
}

func run() error {
	var cfg config
	flag.StringVar(&cfg.scenario, "scenario", "L6", "frozen model scenario with a competitor schedule")
	flag.Uint64Var(&cfg.seed, "seed", 1, "declared seed, shared by compared controllers")
	for i, name := range []string{"focal", "competitor"} {
		f := &cfg.flows[i]
		flag.StringVar(&f.Front, name+"-front", "", "address the "+name+" sender dials")
		flag.StringVar(&f.Back, name+"-back", "", "address the "+name+" receiver sees")
		flag.StringVar(&f.Sender, name+"-sender", "", name+" sender endpoint address")
		flag.StringVar(&f.Target, name+"-target", "", name+" receiver endpoint address")
	}
	flag.Int64Var(&cfg.startUnixNS, "start-unix-ns", 0, "model time zero (measured start)")
	flag.DurationVar(&cfg.until, "until", 0, "stop after this long since launch")
	output := flag.String("output", "", "report JSON path")
	flag.Parse()
	if cfg.until <= 0 || *output == "" {
		return fmt.Errorf("-until and -output are required")
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	done := make(chan struct{})
	go func() {
		<-stop
		close(done)
	}()
	rep, err := serve(cfg, done, func() { fmt.Fprintln(os.Stderr, "relay ready") })
	if err != nil {
		rep.Error = err.Error()
	}
	data, _ := json.MarshalIndent(rep, "", "  ")
	if werr := os.WriteFile(*output, append(data, '\n'), 0o644); werr != nil {
		return errors.Join(err, werr)
	}
	return err
}

// serve relays until cfg.until elapses or done closes, and returns the report.
func serve(cfg config, done <-chan struct{}, ready func()) (rep report, err error) {
	rep = report{Scenario: cfg.scenario, Seed: cfg.seed, Focal: cfg.flows[focal], Competitor: cfg.flows[competitor], StartUnixNS: cfg.startUnixNS}
	fwd, err := model.Scenario(cfg.scenario, true, 0, cfg.seed, 1460)
	if err != nil {
		return rep, err
	}
	rev, err := model.Scenario(cfg.scenario, false, 0, cfg.seed^0x9e3779b97f4a7c15, 1460)
	if err != nil {
		return rep, err
	}
	if !fwd.Schedule.Competitors || !rev.Schedule.Competitors {
		return rep, fmt.Errorf("scenario %s has no competitor schedule", cfg.scenario)
	}
	type sockets struct {
		front, back    *net.UDPConn
		sender, target *net.UDPAddr
	}
	var socks [2]sockets
	for i, f := range cfg.flows {
		s := &socks[i]
		if s.front, err = listen(f.Front); err != nil {
			return rep, err
		}
		defer s.front.Close()
		if s.back, err = listen(f.Back); err != nil {
			return rep, err
		}
		defer s.back.Close()
		if s.sender, err = net.ResolveUDPAddr("udp4", f.Sender); err != nil {
			return rep, err
		}
		if s.target, err = net.ResolveUDPAddr("udp4", f.Target); err != nil {
			return rep, err
		}
	}
	zero := time.Unix(0, cfg.startUnixNS)
	forward := path{q: fwd, r: &rep.Forward}
	reverse := path{q: rev, r: &rep.Reverse}
	arrivals := make(chan arrival, 1<<16)
	var truncated [2][2]atomic.Uint64 // [forward=0/reverse=1][flow]
	reader := func(c *net.UDPConn, fwd bool, flow int) {
		dir := 0
		if !fwd {
			dir = 1
		}
		for {
			buf := make([]byte, ipUDPHeader+2048)
			oob := make([]byte, 128)
			n, oobn, flags, _, err := c.ReadMsgUDP(buf[ipUDPHeader:], oob)
			if err != nil {
				return
			}
			if flags&unix.MSG_TRUNC != 0 {
				truncated[dir][flow].Add(1)
				continue
			}
			arrivals <- arrival{time.Since(zero), fwd, flow, buf[:ipUDPHeader+n], readECN(oob[:oobn])}
		}
	}
	for i := range socks {
		go reader(socks[i].front, true, i)
		go reader(socks[i].back, false, i)
	}
	deadline := time.After(cfg.until)
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	sampler := time.NewTicker(10 * time.Millisecond)
	defer sampler.Stop()
	ticks := 0
	var intervalLate int64
	send := func(p *path, fwd bool, out []model.Packet) {
		now := time.Since(zero)
		p.delivered(out)
		if fwd && len(out) > 0 {
			rep.ForwardBatchHistogram[min(len(out), 64)]++
		}
		for _, pk := range out {
			late := int64(now - pk.ScheduledAt)
			if fwd {
				intervalLate = max(intervalLate, late)
			}
			p.r.MaxLateNS = max(p.r.MaxLateNS, late)
			if late > int64(time.Millisecond) {
				p.r.LateOver1ms++
			}
			i := flowOf(pk)
			conn, to := socks[i].front, socks[i].sender
			if fwd {
				conn, to = socks[i].back, socks[i].target
			}
			if _, _, err := conn.WriteMsgUDP(pk.Bytes[ipUDPHeader:], ecnOOB(pk.ECN&3), to); err != nil {
				p.r.flow(i).SendErrors++
			}
		}
	}
	// The model requires Advance(at) before Admit(at); arrivals use their
	// read timestamp so scheduler delay cannot shrink queue occupancy.
	advance := func(at time.Duration) {
		send(&forward, true, fwd.Advance(at))
		send(&reverse, false, rev.Advance(at))
	}
	finish := func() {
		rep.EndUnixNS = time.Now().UnixNano()
		rep.Forward.Stats, rep.Reverse.Stats = fwd.Stats, rev.Stats
		for i := range socks {
			rep.Forward.flow(i).TruncatedRead = truncated[0][i].Load()
			rep.Reverse.flow(i).TruncatedRead = truncated[1][i].Load()
		}
	}
	ready()
	for {
		next := min(model.NextEvent(fwd), model.NextEvent(rev))
		wait := time.Hour
		if next != model.NoEvent {
			wait = max(0, next-time.Since(zero))
		}
		timer.Reset(wait)
		select {
		case a := <-arrivals:
			rep.MaxArrivalLagNS = max(rep.MaxArrivalLagNS, int64(time.Since(zero)-a.at))
			advance(a.at)
			p := &forward
			if !a.forward {
				p = &reverse
			}
			p.admit(a.at, a.flow, a.bytes, a.ecn)
		case <-timer.C:
			advance(time.Since(zero))
		case <-sampler.C:
			now := time.Since(zero)
			advance(now)
			if ticks++; ticks%sampleEvery == 0 {
				ff, fc := model.QueueBytesByFlow(fwd)
				rf, rc := model.QueueBytesByFlow(rev)
				rep.Samples = append(rep.Samples, [10]int64{now.Milliseconds(), int64(ff), int64(fc), int64(rf), int64(rc),
					int64(rep.Forward.Focal.Dropped), int64(rep.Forward.Competitor.Dropped),
					int64(rep.Forward.Focal.DeliveredBytes), int64(rep.Forward.Competitor.DeliveredBytes), intervalLate})
				intervalLate = 0
			}
		case <-deadline:
			finish()
			return rep, nil
		case <-done:
			finish()
			return rep, nil
		}
	}
}
