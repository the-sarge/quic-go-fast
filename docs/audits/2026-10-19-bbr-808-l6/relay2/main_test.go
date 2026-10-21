package main

import (
	"bytes"
	"math/rand/v2"
	"net"
	"testing"
	"time"

	"q1campaign/model"
)

// 1,400-byte UDP payloads plus the charged IPv4/UDP headers.
const testPacket = ipUDPHeader + 1400

// One 1,428-byte packet at 100 Mbit/s.
const serialization = 114240 * time.Nanosecond

func newPath(t *testing.T, forward bool) *path {
	t.Helper()
	q, err := model.Scenario("L6", forward, 0, 1, 1460)
	if err != nil {
		t.Fatal(err)
	}
	return &path{q: q, r: &directionReport{}}
}

func admitAt(p *path, at time.Duration, flow, size int) bool {
	p.delivered(p.q.Advance(at))
	return p.admit(at, flow, make([]byte, size), 2)
}

func drain(p *path, at time.Duration) []model.Packet {
	out := p.q.Advance(at)
	p.delivered(out)
	return out
}

// The frozen L6 schedule gives the competitor a 25 ms round trip while its
// packets finish service in [100 s, 200 s) of model time (zero = measured
// start), in both directions; the focal flow and every other time get 100 ms.
func TestCompetitorDelaySchedule(t *testing.T) {
	cases := []struct {
		name  string
		at    time.Duration
		flow  int
		delay time.Duration
	}{
		{"competitor in warm-up", -30 * time.Second, competitor, 50 * time.Millisecond},
		{"competitor before the change", 50 * time.Second, competitor, 50 * time.Millisecond},
		{"competitor finishing just before 100 s", 100*time.Second - serialization - time.Nanosecond, competitor, 50 * time.Millisecond},
		{"competitor finishing at 100 s", 100*time.Second - serialization, competitor, 12500 * time.Microsecond},
		{"competitor late in the short path", 199900 * time.Millisecond, competitor, 12500 * time.Microsecond},
		{"competitor finishing at 200 s", 200*time.Second - serialization, competitor, 50 * time.Millisecond},
		{"competitor after the change", 250 * time.Second, competitor, 50 * time.Millisecond},
		{"focal during the short path", 150 * time.Second, focal, 50 * time.Millisecond},
	}
	for _, forward := range []bool{true, false} {
		for _, c := range cases {
			p := newPath(t, forward)
			if !admitAt(p, c.at, c.flow, testPacket) {
				t.Fatalf("forward=%v %s: rejected", forward, c.name)
			}
			due := c.at + serialization + c.delay
			if got := drain(p, due-time.Nanosecond); len(got) != 0 {
				t.Fatalf("forward=%v %s: delivered before %v", forward, c.name, due)
			}
			got := drain(p, due)
			if len(got) != 1 || got[0].ScheduledAt != due || flowOf(got[0]) != c.flow {
				t.Fatalf("forward=%v %s: got %+v, want one flow-%d packet at %v", forward, c.name, got, c.flow, due)
			}
		}
	}
}

// Both flows share one byte-capacity FIFO; an overflow is charged to the
// arriving packet's flow.
func TestSharedQueueOverflowDropsTheArrivingFlow(t *testing.T) {
	p := newPath(t, true)
	if p.q.Direction.Capacity != 1251220 {
		t.Fatalf("capacity %d, want 857 x 1460", p.q.Direction.Capacity)
	}
	at := 10 * time.Second
	for i := range 876 {
		if !admitAt(p, at, focal, testPacket) {
			t.Fatalf("focal packet %d rejected below capacity", i)
		}
	}
	if admitAt(p, at, competitor, testPacket) {
		t.Fatal("competitor packet admitted above shared capacity")
	}
	if !admitAt(p, at, competitor, ipUDPHeader+200) {
		t.Fatal("small competitor packet rejected though it fits")
	}
	focalBytes, competitorBytes := model.QueueBytesByFlow(p.q)
	if focalBytes != 876*testPacket || competitorBytes != ipUDPHeader+200 || focalBytes+competitorBytes != model.QueueBytes(p.q) {
		t.Fatalf("occupancy split %d/%d, total %d", focalBytes, competitorBytes, model.QueueBytes(p.q))
	}
	if p.r.Competitor.Dropped != 1 || p.r.Focal.Dropped != 0 || p.q.Stats.Overflow != 1 {
		t.Fatalf("drops focal=%d competitor=%d overflow=%d", p.r.Focal.Dropped, p.r.Competitor.Dropped, p.q.Stats.Overflow)
	}
}

func TestPerFlowCountersSumToDirectionTotals(t *testing.T) {
	for _, forward := range []bool{true, false} {
		p := newPath(t, forward)
		rng := rand.New(rand.NewPCG(7, 11))
		at := 99 * time.Second
		for range 40000 {
			// Bursts above the bottleneck rate force overflow drops in both flows.
			at += time.Duration(rng.IntN(100)) * time.Microsecond
			admitAt(p, at, rng.IntN(2), ipUDPHeader+100+rng.IntN(1300))
		}
		drain(p, at+time.Minute)
		f, c, s := p.r.Focal, p.r.Competitor, p.q.Stats
		if f.Received+c.Received != s.Received || f.Admitted+c.Admitted != s.Admitted || f.Delivered+c.Delivered != s.Delivered ||
			f.DeliveredBytes+c.DeliveredBytes != s.DeliveredBytes || f.Dropped+c.Dropped != s.Overflow {
			t.Fatalf("forward=%v: flows %+v %+v do not sum to %+v", forward, f, c, s)
		}
		if f.Dropped == 0 || c.Dropped == 0 || s.Admitted != s.Delivered || s.PropagationOverflow != 0 {
			t.Fatalf("forward=%v: want drops in both flows and a full drain, got %+v %+v %+v", forward, f, c, s)
		}
	}
}

// At 100 s the competitor's later packets overtake earlier ones still in
// propagation; at 200 s its delay grows, which delays but never reorders.
func TestShorterPathReordersOnlyTheCompetitor(t *testing.T) {
	for _, window := range []struct {
		start   time.Duration
		reorder bool
	}{{99950 * time.Millisecond, true}, {199950 * time.Millisecond, false}} {
		p := newPath(t, true)
		for i := range 100 {
			at := window.start + time.Duration(i)*time.Millisecond
			admitAt(p, at, focal, testPacket)
			admitAt(p, at, competitor, testPacket)
		}
		drain(p, window.start+time.Second)
		if p.r.Focal.Reordered != 0 || p.r.Focal.Delivered != 100 || p.r.Competitor.Delivered != 100 {
			t.Fatalf("start %v: focal %+v competitor %+v", window.start, p.r.Focal, p.r.Competitor)
		}
		if got := p.r.Competitor.Reordered > 0; got != window.reorder {
			t.Fatalf("start %v: competitor reordered %d (distance %d), want reordering=%v", window.start,
				p.r.Competitor.Reordered, p.r.Competitor.MaxReorderDistance, window.reorder)
		}
	}
}

// The arrival sequence lives only in the relay-owned headroom.
func TestSequenceStaysInHeadroom(t *testing.T) {
	p := newPath(t, false)
	b := make([]byte, testPacket)
	for i := ipUDPHeader; i < len(b); i++ {
		b[i] = byte(i)
	}
	want := bytes.Clone(b[ipUDPHeader:])
	p.delivered(p.q.Advance(time.Second))
	if !p.admit(time.Second, competitor, b, 0) {
		t.Fatal("rejected")
	}
	got := drain(p, 2*time.Second)
	if len(got) != 1 || !bytes.Equal(got[0].Bytes[ipUDPHeader:], want) || len(got[0].Bytes) != testPacket {
		t.Fatal("payload changed")
	}
}

func freeAddr(t *testing.T) string {
	t.Helper()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return c.LocalAddr().String()
}

func endpoint(t *testing.T) *net.UDPConn {
	t.Helper()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func expect(t *testing.T, c *net.UDPConn, want string, from string, after time.Duration, sent time.Time) {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 64)
	n, src, err := c.ReadFromUDP(buf)
	if err != nil || string(buf[:n]) != want || src.String() != from {
		t.Fatalf("got %q from %v (%v), want %q from %s", buf[:n], src, err, want, from)
	}
	if elapsed := time.Since(sent); elapsed < after {
		t.Fatalf("%q arrived after %v, before the %v propagation delay", want, elapsed, after)
	}
}

func silent(t *testing.T, c *net.UDPConn) {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	buf := make([]byte, 64)
	if n, src, err := c.ReadFromUDP(buf); err == nil {
		t.Fatalf("unexpected %q from %v", buf[:n], src)
	}
}

// Each flow's packets reach only that flow's endpoints, in both directions.
func TestServeRoutesEachFlowToItsOwnEndpoint(t *testing.T) {
	var cfg config
	cfg.scenario, cfg.seed, cfg.until = "L6", 1, time.Minute
	// Model time stays in warm-up, where both flows have 50 ms one-way delay.
	cfg.startUnixNS = time.Now().Add(time.Hour).UnixNano()
	var ends [2]struct{ sender, target *net.UDPConn }
	for i := range cfg.flows {
		ends[i].sender, ends[i].target = endpoint(t), endpoint(t)
		cfg.flows[i] = flowAddresses{Front: freeAddr(t), Back: freeAddr(t),
			Sender: ends[i].sender.LocalAddr().String(), Target: ends[i].target.LocalAddr().String()}
	}
	done, ready, result := make(chan struct{}), make(chan struct{}), make(chan report, 1)
	go func() {
		rep, err := serve(cfg, done, func() { close(ready) })
		if err != nil {
			t.Error(err)
		}
		result <- rep
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("relay not ready")
	}
	for i, name := range []string{"focal", "competitor"} {
		other := 1 - i
		front, _ := net.ResolveUDPAddr("udp4", cfg.flows[i].Front)
		back, _ := net.ResolveUDPAddr("udp4", cfg.flows[i].Back)
		sent := time.Now()
		if _, err := ends[i].sender.WriteToUDP([]byte(name+" data"), front); err != nil {
			t.Fatal(err)
		}
		expect(t, ends[i].target, name+" data", cfg.flows[i].Back, 50*time.Millisecond, sent)
		silent(t, ends[other].target)
		sent = time.Now()
		if _, err := ends[i].target.WriteToUDP([]byte(name+" ack"), back); err != nil {
			t.Fatal(err)
		}
		expect(t, ends[i].sender, name+" ack", cfg.flows[i].Front, 50*time.Millisecond, sent)
		silent(t, ends[other].sender)
	}
	close(done)
	rep := <-result
	for _, f := range []flowStats{rep.Forward.Focal, rep.Forward.Competitor, rep.Reverse.Focal, rep.Reverse.Competitor} {
		if f.Received != 1 || f.Delivered != 1 || f.SendErrors != 0 || f.Dropped != 0 {
			t.Fatalf("per-flow counters %+v", f)
		}
	}
}
