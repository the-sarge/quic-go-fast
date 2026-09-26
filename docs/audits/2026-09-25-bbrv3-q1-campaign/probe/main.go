//go:build linux || darwin || windows

// Frozen native UDP calibration source/sink. IP-byte rates include the 28-byte
// IPv4/UDP header. Its counts calibrate the path, never QUIC application goodput.
package main

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"runtime"
	"time"
)

type result struct {
	GOMAXPROCS                                          int
	Role                                                string
	Sent, Received, Unique, Duplicate, Invalid, Outside uint64
	ECN                                                 [4]uint64
	IPBytesPerSecond                                    []uint64
	RTTNS                                               []int64
	Reordered                                           uint64
	MaxOneWayNS                                         int64
	PacketSamples                                       [][3]int64 // sequence, sender Unix ns, receiver Unix ns; clock offset remains separate

	ECNObservation        string
	SocketDropObservation string
	SocketDrops           uint32
	ReceiveBuffer         int
	PacingBatch           int
	Error                 string `json:",omitempty"`
}

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	role := flag.String("role", "", "send, receive, echo or ping")
	processors := flag.Int("processors", 4, "Go processors per probe process, 1..4")
	local := flag.String("local", "", "explicit IPv4 bind")
	peer := flag.String("peer", "", "peer IPv4 address")
	startNS := flag.Int64("start", 0, "start Unix nanoseconds")
	seconds := flag.Int("seconds", 10, "bounded duration")
	rate := flag.Int64("rate", 0, "offered IP bits/second; zero is unpaced")
	pacingBatch := flag.Int("pacing-batch", 1, "packets between pacing waits, 1..256; achieved rate must be measured")
	ect := flag.Bool("ect", false, "send ECT(0)")
	output := flag.String("output", "", "JSON receipt")
	flag.Parse()
	if *processors < 1 || *processors > 4 || *pacingBatch < 1 || *pacingBatch > 256 || *seconds < 1 || *seconds > 300 || *output == "" || *rate < 0 || (*role != "send" && *role != "receive" && *role != "echo" && *role != "ping") {
		return fmt.Errorf("invalid bounded probe config")
	}
	start := time.Unix(0, *startNS)
	end := start.Add(time.Duration(*seconds) * time.Second)
	if time.Until(start) < 0 || time.Until(start) > 2*time.Minute {
		return fmt.Errorf("start must be within next two minutes")
	}
	runtime.GOMAXPROCS(*processors)
	addr, e := net.ResolveUDPAddr("udp4", *local)
	if e != nil {
		return e
	}
	conn, e := net.ListenUDP("udp4", addr)
	if e != nil {
		return e
	}
	defer conn.Close()
	var remote *net.UDPAddr
	if *role == "send" || *role == "ping" {
		remote, e = net.ResolveUDPAddr("udp4", *peer)
		if e != nil {
			return e
		}
	}
	r := result{Role: *role, GOMAXPROCS: runtime.GOMAXPROCS(0), SocketDropObservation: socketDropObservation, ECNObservation: ecnObservation, IPBytesPerSecond: make([]uint64, *seconds)}
	r.PacingBatch = *pacingBatch
	r.ReceiveBuffer, e = configureSocket(conn, *ect)
	if e != nil {
		return e
	}
	conn.SetDeadline(end.Add(2 * time.Second))
	if *role == "send" || *role == "ping" {
		for time.Now().Before(start) {
			time.Sleep(time.Until(start))
		}
		p := make([]byte, 1432)
		copy(p, "Q1P1")
		for i := 16; i < len(p); i++ {
			p[i] = byte(i)
		}
		var seq uint32
		pacingAt := start
		for time.Now().Before(end) && seq < 1<<26 {
			binary.BigEndian.PutUint32(p[4:8], seq)
			binary.BigEndian.PutUint64(p[8:16], uint64(time.Now().UnixNano()))
			if _, e = conn.WriteToUDP(p, remote); e != nil {
				break
			}
			r.Sent++
			seq++
			if *role == "ping" {
				b := make([]byte, 1500)
				conn.SetReadDeadline(minTime(end, time.Now().Add(time.Second)))
				n, _, err := conn.ReadFromUDP(b)
				if err == nil && n == len(p) && binary.BigEndian.Uint32(b[4:8]) == seq-1 {
					r.RTTNS = append(r.RTTNS, time.Now().UnixNano()-int64(binary.BigEndian.Uint64(b[8:16])))
				}
				time.Sleep(50 * time.Millisecond)
			} else if *rate > 0 && seq%uint32(*pacingBatch) == 0 {
				pacingAt = pacingAt.Add(time.Duration(float64(1460*8) * float64(*pacingBatch) / float64(*rate) * float64(time.Second)))
				// Do not turn scheduler delays into an unbounded catch-up burst.
				// The achieved offered rate is measured from Sent, not assumed.
				if time.Since(pacingAt) > 100*time.Microsecond {
					pacingAt = time.Now()
				}
				if delay := time.Until(pacingAt); delay > 200*time.Microsecond {
					time.Sleep(delay - 200*time.Microsecond)
				}
				for time.Now().Before(pacingAt) {
				}
			}
		}
	} else {
		seen := make([]uint64, (1<<26)/64)
		var highest uint32
		var nextSample time.Time
		b := make([]byte, 1500)
		oob := make([]byte, 256)
		for {
			n, on, flags, from, err := conn.ReadMsgUDP(b, oob)
			now := time.Now()
			if err != nil {
				if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
					e = err
				}
				break
			}
			r.Received++
			if messageTruncated(flags) || n != 1432 || string(b[:4]) != "Q1P1" {
				r.Invalid++
				continue
			}
			seq := binary.BigEndian.Uint32(b[4:8])
			if seq >= 1<<26 {
				r.Invalid++
				continue
			}
			valid := true
			for i := 16; i < n; i++ {
				if b[i] != byte(i) {
					valid = false
					break
				}
			}
			if !valid {
				r.Invalid++
				continue
			}
			if err := readMetadata(oob[:on], &r); err != nil {
				e = err
				break
			}
			if seen[seq/64]&(uint64(1)<<(seq%64)) != 0 {
				r.Duplicate++
				continue
			}
			seen[seq/64] |= uint64(1) << (seq % 64)
			if now.Before(start) || !now.Before(end) {
				r.Outside++
				continue
			}

			if seq < highest {
				r.Reordered++
			}
			highest = max(highest, seq)
			sentNS := int64(binary.BigEndian.Uint64(b[8:16]))
			r.MaxOneWayNS = max(r.MaxOneWayNS, now.UnixNano()-sentNS)
			if !now.Before(nextSample) && len(r.PacketSamples) < 4000 {
				r.PacketSamples = append(r.PacketSamples, [3]int64{int64(seq), sentNS, now.UnixNano()})
				nextSample = now.Add(100 * time.Millisecond)
			}
			r.Unique++
			r.IPBytesPerSecond[int(now.Sub(start)/time.Second)] += 1460
			if *role == "echo" {
				if _, e = conn.WriteToUDP(b[:n], from); e != nil {
					break
				}
			}
		}
	}
	if e != nil {
		r.Error = e.Error()
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(*output, append(b, '\n'), 0600); err != nil {
		return err
	}
	if r.Invalid > 0 || r.SocketDrops > 0 {
		return fmt.Errorf("probe limitation; retain receipt")
	}
	return e
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
