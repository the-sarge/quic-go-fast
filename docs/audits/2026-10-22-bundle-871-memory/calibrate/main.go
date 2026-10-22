// ECN codepoint calibration for one Wayfinder ticket (#712). Two plain UDP
// sockets stand in for the endpoints; each sends every codepoint through the
// relay in both directions, and the far side records the codepoint it reads.
// A finite prerequisite check, not a maintained tool.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

type direction struct {
	Sent     [4]int    `json:"sent"`
	Received [4][4]int `json:"received"` // [sent codepoint][read codepoint]
	Other    int       `json:"unparsed"`
}

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
		return nil, err
	}
	var serr error
	if err = raw.Control(func(fd uintptr) {
		serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_RECVTOS, 1)
	}); err != nil {
		return nil, err
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

func exchange(from, to *net.UDPConn, via *net.UDPAddr, count int) (direction, error) {
	var d direction
	done := make(chan error, 1)
	go func() {
		buf, oob := make([]byte, 2048), make([]byte, 128)
		for got := 0; got < 4*count; got++ {
			_ = to.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, oobn, _, _, err := to.ReadMsgUDP(buf, oob)
			if err != nil {
				done <- fmt.Errorf("after %d packets: %w", got, err)
				return
			}
			ecn, ok := readTOS(oob[:oobn])
			if n < 1 || buf[0] > 3 || !ok {
				d.Other++
				continue
			}
			d.Received[buf[0]][ecn]++
		}
		done <- nil
	}()
	payload := make([]byte, 1200)
	for cp := uint8(0); cp < 4; cp++ {
		payload[0] = cp
		for i := 0; i < count; i++ {
			if _, _, err := from.WriteMsgUDP(payload, tosOOB(cp), via); err != nil {
				return d, err
			}
			d.Sent[cp]++
			time.Sleep(500 * time.Microsecond)
		}
	}
	return d, <-done
}

func main() {
	sender := flag.String("sender", "127.0.0.1:24762", "sender stand-in")
	target := flag.String("target", "127.0.0.1:24761", "receiver stand-in")
	front := flag.String("front", "127.0.0.1:24771", "relay address the sender uses")
	back := flag.String("back", "127.0.0.1:24772", "relay address the receiver uses")
	count := flag.Int("count", 200, "packets per codepoint and direction")
	output := flag.String("output", "", "report JSON path")
	flag.Parse()
	s, err := socket(*sender)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	r, err := socket(*target)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	f, _ := net.ResolveUDPAddr("udp4", *front)
	b, _ := net.ResolveUDPAddr("udp4", *back)
	fwd, ferr := exchange(s, r, f, *count)
	rev, rerr := exchange(r, s, b, *count)
	ok := ferr == nil && rerr == nil
	for _, d := range []direction{fwd, rev} {
		for cp := 0; cp < 4; cp++ {
			ok = ok && d.Received[cp][cp] == d.Sent[cp] && d.Other == 0
		}
	}
	rep := map[string]any{"forward": fwd, "reverse": rev, "pass": ok, "errors": errors.Join(ferr, rerr) != nil}
	if e := errors.Join(ferr, rerr); e != nil {
		rep["error"] = e.Error()
	}
	data, _ := json.MarshalIndent(rep, "", "  ")
	_ = os.WriteFile(*output, append(data, '\n'), 0o644)
	fmt.Println(string(data))
	if !ok {
		os.Exit(1)
	}
}
