// Synthetic #734 harness for the wake-path instrument's preflight and the
// bare-timer context control. It runs a loop shaped like the connection's run
// loop (arm one timer, block in a select on the timer and a packet channel fed
// by a UDP read goroutine, take now, record an opportunity and a paced exit),
// with the recorder in wakerec.go (byte-identical to the fixture's except the
// package clause), and writes its own ground truth for the analysis to
// recover. A finite measurement aid, not a maintained tool.
//
// Cases (delay = -delta):
//
//	delivery    the timer is armed delay after the deadline the recorder logs, so
//	            the runtime delivers it at least delay late relative to the log;
//	scheduling  GOMAXPROCS 1; a second timer 2 µs later wakes a goroutine that
//	            takes the P and spins for delay, so the loop runs at least
//	            delay after its timer fired;
//	handler     the loop spins for delay after waking, before taking now;
//	reset       cycles of an early packet wake (re-arm, same deadline), a
//	            folded earlier arm, and an early packet wake that supersedes the
//	            deadline with a later one;
//	nontimer    the timer is armed 5 ms after the logged deadline and a packet,
//	            sent delay after the deadline, wakes the loop;
//	bare        no injection: the bare Go timer loop at the pacing cadence;
//	barepkt     bare, while a separate peer process sends -rate datagrams per
//	            second to a read goroutine that never wakes the loop;
//	peer        that peer process.
package main

import (
	"encoding/json"
	"flag"
	"net"
	"os"
	"runtime"
	"sync/atomic"
	"time"
)

var monoStart = time.Now()

func mono() int64 { return int64(time.Since(monoStart)) }

func spinUntil(t int64) {
	for mono() < t {
	}
}

type truthEvent struct {
	D, O                     int64 // monotonic, the recorder's caller clock
	Arms, Folded, Superseded int
	Delta                    int64 // injected delay, ns
	Sent                     int64 // nontimer: when the waking packet was sent (monotonic)
}

func main() {
	caseName := flag.String("case", "", "delivery|scheduling|handler|reset|nontimer|bare|barepkt|peer")
	out := flag.String("out", "", "output prefix")
	dur := flag.Duration("duration", 3*time.Second, "recorded window")
	delta := flag.Duration("delta", 300*time.Microsecond, "injected delay")
	period := flag.Duration("period", time.Millisecond, "pacing cadence: next deadline after an opportunity")
	listen := flag.String("listen", "127.0.0.1:24791", "barepkt: datagram address")
	rate := flag.Int("rate", 4000, "peer: datagrams per second")
	flag.Parse()
	if *caseName == "peer" {
		peer(*listen, *rate, *dur)
		return
	}
	if *caseName == "scheduling" {
		runtime.GOMAXPROCS(1)
	}
	d, p := int64(*delta), int64(*period)

	// Packet path: a peer goroutine sends a datagram when asked; the read
	// goroutine forwards each one to the loop as the transport's listen loop does.
	addr := "127.0.0.1:0"
	if *caseName == "barepkt" {
		addr = *listen
	}
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		panic(err)
	}
	pkt := make(chan struct{}, 1)
	var received atomic.Int64
	go readLoop(pc, pkt, *caseName != "barepkt", &received)
	tx, err := net.Dial("udp", pc.LocalAddr().String())
	if err != nil {
		panic(err)
	}
	sendAt := make(chan int64, 1)
	sent := make(chan int64, 16)
	go func() {
		for t := range sendAt {
			spinUntil(t)
			now := mono()
			tx.Write([]byte("x"))
			select {
			case sent <- now:
			default:
			}
		}
	}()
	spinT := time.NewTimer(time.Hour)
	go func() {
		for {
			<-spinT.C
			spinUntil(mono() + d)
		}
	}()

	start := time.Now().Add(300 * time.Millisecond).UnixNano()
	end := start + int64(*dur)
	wakeStart(*out, start, end, mono)

	timer := time.NewTimer(time.Hour)
	var truth []truthEvent
	var chain truthEvent
	var pending int64
	cycle, superseded := 0, false
	next := mono() + p
	var lastSent int64
	for time.Now().UnixNano() < end+int64(100*time.Millisecond) {
		armed, actual := next, next
		switch *caseName {
		case "delivery":
			actual = next + d
		case "nontimer":
			actual = next + int64(5*time.Millisecond)
			sendAt <- next + d
		case "reset":
			if chain.Arms == 0 {
				switch cycle % 3 {
				case 0, 2:
					sendAt <- next - int64(500*time.Microsecond)
				case 1:
					armed = next - int64(400*time.Microsecond)
					actual = armed
				}
			}
		}
		if armed < next {
			chain.Folded++
		}
		chain.Arms++
		wakeArmed(armed, next, 0)
		wait := time.Duration(actual - mono())
		timer.Reset(wait)
		if *caseName == "scheduling" {
			spinT.Reset(wait + 2*time.Microsecond)
		}
		woke := int64(0)
		select {
		case <-timer.C:
			woke = 1
		case <-pkt:
			woke = 5
		}
		wakeWoken(woke)
		for drained := false; !drained; {
			select {
			case lastSent = <-sent:
			default:
				drained = true
			}
		}
		if *caseName == "handler" {
			spinUntil(mono() + d)
		}
		now := mono()
		wakeOpportunity(now)
		if pending != 0 && now >= pending {
			chain.D, chain.O = pending, now
			if *caseName != "reset" && *caseName != "bare" && *caseName != "barepkt" {
				chain.Delta = d
			}
			if *caseName == "nontimer" {
				chain.Sent = lastSent
			}
			truth = append(truth, chain)
			chain, pending, superseded = truthEvent{}, 0, false
			cycle++
			next = now + p
		} else if *caseName == "reset" && cycle%3 == 2 && !superseded {
			next = pending + int64(300*time.Microsecond)
			chain.Superseded++
			superseded = true
		} else if pending == 0 {
			next = now + p
		} else {
			next = pending
		}
		wakeExited(next, 3, wsPaced, 0)
		pending = next
	}
	b, _ := json.Marshal(map[string]any{"case": *caseName, "delta_ns": d, "period_ns": p, "gomaxprocs": runtime.GOMAXPROCS(0),
		"late": truth, "received": received.Load()})
	os.WriteFile(*out+".truth.json", b, 0o644)
	time.Sleep(time.Second) // let wakeStart write its files
}

// readLoop forwards each datagram to the loop, as the transport's listen loop
// hands packets to the connection (barepkt only counts them).
func readLoop(pc net.PacketConn, pkt chan struct{}, forward bool, received *atomic.Int64) {
	buf := make([]byte, 1500)
	for {
		if _, _, err := pc.ReadFrom(buf); err != nil {
			return
		}
		received.Add(1)
		if forward {
			select {
			case pkt <- struct{}{}:
			default:
			}
		}
	}
}

// peer sends rate datagrams per second, evenly spaced by spinning.
func peer(addr string, rate int, dur time.Duration) {
	c, err := net.Dial("udp", addr)
	if err != nil {
		panic(err)
	}
	gap := int64(time.Second) / int64(rate)
	stop := mono() + int64(dur)
	for t := mono(); t < stop; t += gap {
		spinUntil(t)
		c.Write([]byte("x"))
	}
}
