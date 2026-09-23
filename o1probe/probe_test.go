//go:build openbsd

package o1probe

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/net/ipv6"
	"golang.org/x/sys/unix"
)

func socket(t *testing.T, family string) *net.UDPConn {
	t.Helper()
	addr := "127.0.0.1:0"
	if family == "udp6" {
		addr = "[::1]:0"
	}
	a, err := net.ResolveUDPAddr(family, addr)
	if err != nil {
		t.Fatal(err)
	}
	c, err := net.ListenUDP(family, a)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	})
	return c
}

func option(c *net.UDPConn, level, name, value int) error {
	raw, err := c.SyscallConn()
	if err != nil {
		return err
	}
	var result error
	err = raw.Control(func(fd uintptr) { result = unix.SetsockoptInt(int(fd), level, name, value) })
	if err != nil {
		return err
	}
	return result
}

func control(level, kind, value int) []byte {
	b := make([]byte, unix.CmsgSpace(4))
	h := (*unix.Cmsghdr)(unsafe.Pointer(&b[0]))
	h.Level, h.Type = int32(level), int32(kind)
	h.SetLen(unix.CmsgLen(4))
	binary.NativeEndian.PutUint32(b[unix.CmsgLen(0):], uint32(value))
	return b
}

func exchange(t *testing.T, rx, tx *net.UDPConn, oob []byte) ([]byte, error) {
	t.Helper()
	n, nn, err := tx.WriteMsgUDP([]byte("O1"), oob, rx.LocalAddr().(*net.UDPAddr))
	t.Logf("WriteMsgUDP payload=%d control=%d err=%v sent-oob=%x", n, nn, err, oob)
	if err != nil {
		return nil, err
	}
	if n != 2 {
		t.Fatalf("payload sent %d", n)
	}
	if err := rx.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	payload, ancillary := make([]byte, 16), make([]byte, 256)
	n, nn, flags, peer, err := rx.ReadMsgUDP(payload, ancillary)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload[:n]) != "O1" || peer.String() != tx.LocalAddr().String() || flags&unix.MSG_CTRUNC != 0 {
		t.Fatalf("unexpected datagram n=%d flags=%d peer=%v", n, flags, peer)
	}
	ancillary = ancillary[:nn]
	msgs, err := unix.ParseSocketControlMessage(ancillary)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("ReadMsgUDP flags=%d oob=%x", flags, ancillary)
	for _, m := range msgs {
		t.Logf("native cmsg level=%d type=%d data=%x", m.Header.Level, m.Header.Type, m.Data)
	}
	return ancillary, nil
}

func trafficClass(b []byte) (int, bool, error) {
	if os.Getenv("O1_DISABLE_PARSE") == "1" {
		return 0, false, nil
	}
	msgs, err := unix.ParseSocketControlMessage(b)
	if err != nil {
		return 0, false, err
	}
	present := false
	for _, m := range msgs {
		if m.Header.Level == unix.IPPROTO_IPV6 && m.Header.Type == unix.IPV6_TCLASS {
			present = true
		}
	}
	var cm ipv6.ControlMessage
	err = cm.Parse(b)
	return cm.TrafficClass, present, err
}

func TestIPv6Receive(t *testing.T) {
	rx, tx := socket(t, "udp6"), socket(t, "udp6")
	err := option(rx, unix.IPPROTO_IPV6, unix.IPV6_RECVTCLASS, 1)
	t.Logf("IPV6_RECVTCLASS=%d enable=%v", unix.IPV6_RECVTCLASS, err)
	if err != nil {
		t.Fatal(err)
	}
	for _, mark := range []int{0, 2, 1, 3} {
		t.Run(fmt.Sprint(mark), func(t *testing.T) {
			if err := option(tx, unix.IPPROTO_IPV6, unix.IPV6_TCLASS, mark); err != nil {
				t.Fatal(err)
			}
			b, err := exchange(t, rx, tx, nil)
			if err != nil {
				t.Fatal(err)
			}
			got, present, err := trafficClass(b)
			t.Logf("socket-default=%d peer-tclass=%d present=%v parse=%v", mark, got, present, err)
			if err != nil || !present || got != mark {
				t.Fatal("receive traffic class mismatch")
			}
		})
	}
}

func TestIPv6Send(t *testing.T) {
	rx, tx := socket(t, "udp6"), socket(t, "udp6")
	if err := option(rx, unix.IPPROTO_IPV6, unix.IPV6_RECVTCLASS, 1); err != nil {
		t.Fatal(err)
	}
	// A distinct default makes explicit Not-ECT distinguishable from omitted OOB.
	if err := option(tx, unix.IPPROTO_IPV6, unix.IPV6_TCLASS, 0x20); err != nil {
		t.Fatal(err)
	}
	for _, mark := range []int{0, 2, 1, 3} {
		t.Run(fmt.Sprint(mark), func(t *testing.T) {
			oob := control(unix.IPPROTO_IPV6, unix.IPV6_TCLASS, mark)
			if os.Getenv("O1_DISABLE_MARK") == "1" {
				oob = nil
			}
			b, err := exchange(t, rx, tx, oob)
			if err != nil {
				t.Fatal(err)
			}
			got, present, err := trafficClass(b)
			t.Logf("requested=%d peer-tclass=%d present=%v parse=%v", mark, got, present, err)
			if err != nil || !present || got != mark {
				t.Fatal("per-datagram traffic class mismatch")
			}
		})
	}
}

func TestIPv4Receive(t *testing.T) {
	rx, tx := socket(t, "udp4"), socket(t, "udp4")
	// OpenBSD exposes no IP_RECVTOS. Request documented ancillary metadata
	// to distinguish an operational ReadMsgUDP path from absent TOS support.
	for _, name := range []int{unix.IP_RECVDSTADDR, unix.IP_RECVTTL} {
		err := option(rx, unix.IPPROTO_IP, name, 1)
		t.Logf("IPv4 receive option=%d err=%v", name, err)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, mark := range []int{0, 2, 1, 3} {
		if err := option(tx, unix.IPPROTO_IP, unix.IP_TOS, mark); err != nil {
			t.Fatal(err)
		}
		t.Logf("IPv4 socket-wide IP_TOS=%d accepted; peer ECN not inferred", mark)
		if _, err := exchange(t, rx, tx, nil); err != nil {
			t.Fatal(err)
		}
	}
}

func TestIPv4Send(t *testing.T) {
	rx, tx := socket(t, "udp4"), socket(t, "udp4")
	for _, mark := range []int{0, 2, 1, 3} {
		t.Logf("IPv4 per-datagram IP_TOS=%d", mark)
		_, err := exchange(t, rx, tx, control(unix.IPPROTO_IP, unix.IP_TOS, mark))
		t.Logf("IPv4 outgoing result=%v; peer-observed TOS unavailable without receive API", err)
	}
}

func TestAbsentMalformedNoOption(t *testing.T) {
	rx, tx := socket(t, "udp6"), socket(t, "udp6")
	b, err := exchange(t, rx, tx, control(unix.IPPROTO_IPV6, unix.IPV6_TCLASS, 2))
	if err != nil {
		t.Fatal(err)
	}
	_, present, err := trafficClass(b)
	if err != nil || present {
		t.Fatalf("no receive option: present=%v err=%v", present, err)
	}
	if err := option(rx, unix.IPPROTO_IPV6, unix.IPV6_RECVTCLASS, 1); err != nil {
		t.Fatal(err)
	}
	b, err = exchange(t, rx, tx, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, present, err := trafficClass(b)
	if err != nil || !present || got != 0 {
		t.Fatalf("unmarked default: got=%d present=%v err=%v", got, present, err)
	}
	malformed := control(unix.IPPROTO_IPV6, unix.IPV6_TCLASS, 2)
	(*unix.Cmsghdr)(unsafe.Pointer(&malformed[0])).SetLen(len(malformed) + 64)
	_, parseErr := unix.ParseSocketControlMessage(malformed)
	_, sendErr := exchange(t, rx, tx, malformed)
	t.Logf("malformed cmsg authoritative parse=%v kernel-send=%v", parseErr, sendErr)
	if parseErr == nil || sendErr == nil {
		t.Fatal("malformed control was not rejected")
	}
}

func TestCloseCleanup(t *testing.T) {
	for _, family := range []string{"udp4", "udp6"} {
		rx := socket(t, family)
		addr := rx.LocalAddr().(*net.UDPAddr)
		tx, err := net.ListenUDP(family, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.Close(); err != nil {
			t.Fatal(err)
		}
		_, _, err = tx.WriteMsgUDP([]byte("closed"), nil, addr)
		t.Logf("%s post-close write=%v", family, err)
		if err == nil {
			t.Fatal("closed socket write succeeded")
		}
	}
}
