// Frozen Linux UDP calibration source/sink. IP-byte rates include the 28-byte
// IPv4/UDP header. Its counts calibrate the path, never QUIC application goodput.
package main

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"golang.org/x/sys/unix"
	"net"
	"os"
	"runtime"
	"time"
)

type result struct {
	Role                                                string
	Sent, Received, Unique, Duplicate, Invalid, Outside uint64
	ECN                                                 [4]uint64
	IPBytesPerSecond                                    []uint64
	RTTNS                                               []int64
	Reordered                                           uint64
	MaxOneWayNS                                         int64
	PacketSamples                                       [][3]int64 // sequence, sender Unix ns, receiver Unix ns; clock offset remains separate

	SocketDrops   uint32
	ReceiveBuffer int
	Error         string `json:",omitempty"`
}

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	role := flag.String("role", "", "send, receive, echo or ping")
	local := flag.String("local", "", "explicit IPv4 bind")
	peer := flag.String("peer", "", "peer IPv4 address")
	startNS := flag.Int64("start", 0, "start Unix nanoseconds")
	seconds := flag.Int("seconds", 10, "bounded duration")
	rate := flag.Int64("rate", 0, "offered IP bits/second; zero is unpaced")
	ect := flag.Bool("ect", false, "send ECT(0)")
	output := flag.String("output", "", "JSON receipt")
	flag.Parse()
	if *seconds < 1 || *seconds > 300 || *output == "" || *rate < 0 || (*role != "send" && *role != "receive" && *role != "echo" && *role != "ping") {
		return fmt.Errorf("invalid bounded probe config")
	}
	start := time.Unix(0, *startNS)
	end := start.Add(time.Duration(*seconds) * time.Second)
	if time.Until(start) < 0 || time.Until(start) > 2*time.Minute {
		return fmt.Errorf("start must be within next two minutes")
	}
	runtime.GOMAXPROCS(4)
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
	r := result{Role: *role, IPBytesPerSecond: make([]uint64, *seconds)}
	raw, e := conn.SyscallConn()
	if e != nil {
		return e
	}
	var socketErr error
	e = raw.Control(func(fd uintptr) {
		for _, option := range [][3]int{{unix.SOL_SOCKET, unix.SO_RCVBUF, 4 << 20}, {unix.SOL_SOCKET, unix.SO_RXQ_OVFL, 1}, {unix.IPPROTO_IP, unix.IP_RECVTOS, 1}} {
			if err := unix.SetsockoptInt(int(fd), option[0], option[1], option[2]); err != nil {
				socketErr = err
				return
			}
		}
		if *ect {
			socketErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_TOS, 2)
		}
		r.ReceiveBuffer, _ = unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_RCVBUF)
	})
	if e != nil {
		return e
	}
	if socketErr != nil {
		return socketErr
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
			} else if *rate > 0 {
				due := start.Add(time.Duration(float64(r.Sent) * 1460 * 8 / float64(*rate) * float64(time.Second)))
				if delay := time.Until(due); delay >= 100*time.Microsecond {
					time.Sleep(delay)
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
			if flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || n != 1432 || string(b[:4]) != "Q1P1" {
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
			msgs, err := unix.ParseSocketControlMessage(oob[:on])
			if err != nil {
				e = err
				break
			}
			for _, m := range msgs {
				if m.Header.Level == unix.IPPROTO_IP && m.Header.Type == unix.IP_TOS && len(m.Data) > 0 {
					r.ECN[m.Data[0]&3]++
				}
				if m.Header.Level == unix.SOL_SOCKET && m.Header.Type == unix.SO_RXQ_OVFL && len(m.Data) >= 4 {
					r.SocketDrops = binary.NativeEndian.Uint32(m.Data[:4])
				}
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
