// Two-flow path calibration for one qualification ticket (#777). Plain UDP
// sockets stand in for all four endpoints of the two-flow relay (relay2) and
// exercise it in one of three modes; prereq.py starts the relay with a chosen
// model time zero and judges the report against the registered criteria. A
// finite prerequisite check, not a maintained tool.
//
//   - ecn:   each flow in turn sends every ECN codepoint through the relay in
//     both directions; the far side records the codepoint it reads (#712's
//     calibrate/main.go exchange, per flow).
//   - In probe and blast modes both flows run at once, so a receiver also counts
//     any packet carrying the other flow's identity (a routing failure).
//   - probe: each flow sends timestamped probes in both directions; the far side
//     records each probe's send time and one-way delay.
//   - blast: both flows send faster than the bottleneck in one direction; the far
//     side records arrival times and bytes, for the delivered rate.
//
// Every stand-in runs on one host, so send and receive timestamps share the
// host's realtime clock.
package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Every probe and blast datagram starts with this header: magic, flow, send time.
const (
	magic      = 0x4c36 // "L6"
	headerSize = 12
)

func socket(addr string) (*net.UDPConn, error) {
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

func tosOOB(ecn uint8) []byte {
	b := make([]byte, unix.CmsgSpace(4))
	h := (*unix.Cmsghdr)(unsafe.Pointer(&b[0]))
	h.Level = unix.IPPROTO_IP
	h.Type = unix.IP_TOS
	h.SetLen(unix.CmsgLen(4))
	b[unix.CmsgLen(0)] = ecn
	return b
}

// readTOS accepts either control-message type, so a missing adapter in the
// relay shows up as Not-ECT here rather than as a parse failure.
func readTOS(oob []byte) (uint8, bool) {
	msgs, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return 0, false
	}
	for _, m := range msgs {
		if m.Header.Level == unix.IPPROTO_IP && (m.Header.Type == unix.IP_TOS || m.Header.Type == unix.IP_RECVTOS) && len(m.Data) > 0 {
			return m.Data[0] & 3, true
		}
	}
	return 0, false
}

type flowAddresses struct {
	Front, Back, Sender, Target string
}

// stand-ins for one flow's two endpoints.
type flow struct {
	name           string
	sender, target *net.UDPConn
	front, back    *net.UDPAddr
}

func (f *flow) close() {
	f.sender.Close()
	f.target.Close()
}

// leg is one flow in one direction.
type leg struct {
	Flow    string `json:"flow"`
	Forward bool   `json:"forward"`
	// ecn: [sent codepoint][read codepoint]; probe and blast leave it empty.
	Sent     [4]int    `json:"sent,omitempty"`
	Received [4][4]int `json:"received,omitempty"`
	// probe: [send unix ns, one-way delay ns]; blast: [arrival unix ns, UDP payload bytes].
	Samples [][2]int64 `json:"samples,omitempty"`
	// Packets read here that carry the other flow's identity, or no valid header.
	Foreign  int    `json:"foreign"`
	Unparsed int    `json:"unparsed"`
	Error    string `json:"error,omitempty"`
}

func header(b []byte, flowID uint16) {
	binary.BigEndian.PutUint16(b[0:2], magic)
	binary.BigEndian.PutUint16(b[2:4], flowID)
	binary.BigEndian.PutUint64(b[4:12], uint64(time.Now().UnixNano()))
}

// receive reads until quiet for the idle period, classifying each datagram.
func receive(c *net.UDPConn, flowID uint16, idle time.Duration, l *leg, record func(b []byte, ecn uint8, at time.Time)) {
	buf, oob := make([]byte, 2048), make([]byte, 128)
	for {
		_ = c.SetReadDeadline(time.Now().Add(idle))
		n, oobn, _, _, err := c.ReadMsgUDP(buf, oob)
		at := time.Now()
		if err != nil {
			var ne net.Error
			if !errors.As(err, &ne) || !ne.Timeout() {
				l.Error = err.Error()
			}
			return
		}
		ecn, ok := readTOS(oob[:oobn])
		if n < headerSize || binary.BigEndian.Uint16(buf[0:2]) != magic || !ok {
			l.Unparsed++
			continue
		}
		if binary.BigEndian.Uint16(buf[2:4]) != flowID {
			l.Foreign++
			continue
		}
		record(buf[:n], ecn, at)
	}
}

func endpoints(f *flow, forward bool) (from, to *net.UDPConn, via *net.UDPAddr) {
	if forward {
		return f.sender, f.target, f.front
	}
	return f.target, f.sender, f.back
}

// run sends on every leg concurrently and waits for every receiver to go idle.
func run(flows []*flow, forwards []bool, idle time.Duration, send func(l *leg, id uint16, from *net.UDPConn, via *net.UDPAddr) error,
	record func(l *leg) func(b []byte, ecn uint8, at time.Time)) []*leg {
	var legs []*leg
	var wg sync.WaitGroup
	for id, f := range flows {
		for _, fwd := range forwards {
			l := &leg{Flow: f.name, Forward: fwd}
			legs = append(legs, l)
			from, to, via := endpoints(f, fwd)
			done := make(chan struct{})
			wg.Add(2)
			go func() {
				defer wg.Done()
				defer close(done)
				receive(to, uint16(id), idle, l, record(l))
			}()
			go func() {
				defer wg.Done()
				if err := send(l, uint16(id), from, via); err != nil {
					l.Error = err.Error()
				}
				<-done
			}()
		}
	}
	wg.Wait()
	return legs
}

func main() {
	if err := execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func execute() error {
	mode := flag.String("mode", "", "ecn, probe or blast")
	var addrs [2]flowAddresses
	for i, name := range []string{"focal", "competitor"} {
		a := &addrs[i]
		flag.StringVar(&a.Front, name+"-front", "", "relay address the "+name+" sender uses")
		flag.StringVar(&a.Back, name+"-back", "", "relay address the "+name+" receiver uses")
		flag.StringVar(&a.Sender, name+"-sender", "", name+" sender stand-in")
		flag.StringVar(&a.Target, name+"-target", "", name+" receiver stand-in")
	}
	count := flag.Int("count", 200, "ecn: packets per codepoint and leg; probe: probes per leg")
	interval := flag.Duration("interval", 20*time.Millisecond, "probe spacing")
	reverse := flag.Bool("reverse", false, "blast: send toward the senders instead of the receivers")
	mbps := flag.Float64("mbps", 75, "blast: offered UDP payload rate per flow, Mbit/s")
	duration := flag.Duration("duration", 4*time.Second, "blast: sending time")
	payload := flag.Int("payload", 1400, "blast: UDP payload bytes")
	output := flag.String("output", "", "report JSON path")
	flag.Parse()
	var flows []*flow
	defer func() {
		for _, f := range flows {
			f.close()
		}
	}()
	for i, name := range []string{"focal", "competitor"} {
		a := addrs[i]
		f := &flow{name: name}
		var err error
		if f.sender, err = socket(a.Sender); err != nil {
			return err
		}
		if f.target, err = socket(a.Target); err != nil {
			f.sender.Close()
			return err
		}
		flows = append(flows, f)
		if f.front, err = net.ResolveUDPAddr("udp4", a.Front); err != nil {
			return err
		}
		if f.back, err = net.ResolveUDPAddr("udp4", a.Back); err != nil {
			return err
		}
	}
	var legs []*leg
	switch *mode {
	case "ecn":
		// One flow and direction at a time, as in #712's exchange.
		for _, f := range flows {
			for _, fwd := range []bool{true, false} {
				legs = append(legs, run([]*flow{f}, []bool{fwd}, 2*time.Second,
					func(l *leg, id uint16, from *net.UDPConn, via *net.UDPAddr) error {
						b := make([]byte, 1200)
						for cp := uint8(0); cp < 4; cp++ {
							for range *count {
								header(b, id)
								b[headerSize] = cp
								if _, _, err := from.WriteMsgUDP(b, tosOOB(cp), via); err != nil {
									return err
								}
								l.Sent[cp]++
								time.Sleep(500 * time.Microsecond)
							}
						}
						return nil
					},
					func(l *leg) func([]byte, uint8, time.Time) {
						return func(b []byte, ecn uint8, _ time.Time) {
							if len(b) <= headerSize || b[headerSize] > 3 {
								l.Unparsed++
								return
							}
							l.Received[b[headerSize]][ecn]++
						}
					})...)
			}
		}
	case "probe":
		legs = run(flows, []bool{true, false}, 2*time.Second,
			func(l *leg, id uint16, from *net.UDPConn, via *net.UDPAddr) error {
				b := make([]byte, 1200)
				for range *count {
					header(b, id)
					if _, err := from.WriteToUDP(b, via); err != nil {
						return err
					}
					time.Sleep(*interval)
				}
				return nil
			},
			func(l *leg) func([]byte, uint8, time.Time) {
				return func(b []byte, _ uint8, at time.Time) {
					sent := int64(binary.BigEndian.Uint64(b[4:12]))
					l.Samples = append(l.Samples, [2]int64{sent, at.UnixNano() - sent})
				}
			})
	case "blast":
		legs = run(flows, []bool{!*reverse}, time.Second,
			func(l *leg, id uint16, from *net.UDPConn, via *net.UDPAddr) error {
				b := make([]byte, *payload)
				gap := time.Duration(float64(*payload*8) / (*mbps * 1e6) * float64(time.Second))
				start := time.Now()
				for i := 0; ; i++ {
					due := start.Add(time.Duration(i) * gap)
					if due.Sub(start) >= *duration {
						return nil
					}
					if d := time.Until(due); d > 0 {
						time.Sleep(d)
					}
					header(b, id)
					if _, err := from.WriteToUDP(b, via); err != nil {
						return err
					}
				}
			},
			func(l *leg) func([]byte, uint8, time.Time) {
				return func(b []byte, _ uint8, at time.Time) {
					l.Samples = append(l.Samples, [2]int64{at.UnixNano(), int64(len(b))})
				}
			})
	default:
		return fmt.Errorf("mode must be ecn, probe or blast")
	}
	data, err := json.MarshalIndent(map[string]any{"mode": *mode, "legs": legs}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(*output, append(data, '\n'), 0o644)
}
