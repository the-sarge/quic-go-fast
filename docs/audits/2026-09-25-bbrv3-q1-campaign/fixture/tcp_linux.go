package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"net"
	"os"
	"reflect"
	"runtime"
	"syscall"
	"time"
)

// TCP competitors use separate bulk and control connections on the same path.
// Both belong to the reserved competitor pair, never a focal endpoint core set.
func receiveTCP(ctx context.Context, bulk, control *net.TCPConn, cfg Run) (Result, error) {
	r := Result{Run: cfg, Controller: "cubic"}
	bulk.SetDeadline(cfg.end().Add(3 * time.Second))
	control.SetDeadline(cfg.end().Add(3 * time.Second))
	defer bulk.Close()
	defer control.Close()
	var remote Run
	if e := readJSON(control, &remote); e != nil {
		return r, e
	}
	if !reflect.DeepEqual(remote, cfg) {
		return r, fmt.Errorf("TCP competitor config mismatch")
	}
	if e := writeAll(control, []byte{1}); e != nil {
		return r, e
	}
	echoDone := make(chan error, 1)
	go func() {
		var p [32]byte
		for {
			_, e := io.ReadFull(control, p[:])
			if e != nil {
				echoDone <- e
				return
			}
			binary.BigEndian.PutUint64(p[16:24], uint64(time.Now().UnixNano()))
			binary.BigEndian.PutUint64(p[24:32], uint64(time.Now().UnixNano()))
			if e = writeAll(control, p[:]); e != nil {
				echoDone <- e
				return
			}
		}
	}()
	defer func() { control.Close(); <-echoDone }()
	counter := NewCounter(cfg.measuredStart(), time.Duration(cfg.MeasureMS)*time.Millisecond)
	p := make([]byte, cfg.PayloadBytes)
	var sequence uint64
	for {
		_, e := io.ReadFull(bulk, p)
		if e != nil {
			// The duration boundary may interrupt the last record. It is not admitted
			// as useful content. An early EOF or truncation is a failed competitor.
			if !time.Now().Before(cfg.end()) && (errors.Is(e, io.EOF) || errors.Is(e, io.ErrUnexpectedEOF) || os.IsTimeout(e)) {
				break
			}
			return r, e
		}
		if binary.BigEndian.Uint64(p[4:12]) != sequence {
			return r, fmt.Errorf("TCP record sequence mismatch")
		}
		sequence++
		if e = counter.Record(p, time.Now()); e != nil {
			return r, e
		}
	}
	r.Receiver = counter.Snapshot()
	// Export the authoritative receiver record locally. A congested bulk tail
	// may drain until this socket's deadline; it must not also carry a terminal
	// receipt after that deadline. The caller requires both peers' local records.
	return r, nil
}

func sendTCP(ctx context.Context, bulk, control *net.TCPConn, cfg Run) (Result, error) {
	r := Result{Run: cfg, Controller: "cubic"}
	bulk.SetDeadline(cfg.end().Add(3 * time.Second))
	control.SetDeadline(cfg.end().Add(3 * time.Second))
	defer bulk.Close()
	defer control.Close()
	if e := writeJSON(control, cfg); e != nil {
		return r, e
	}
	var ack [1]byte
	if _, e := io.ReadFull(control, ack[:]); e != nil {
		return r, e
	}
	if ack[0] != 1 {
		return r, fmt.Errorf("TCP config acknowledgment mismatch")
	}
	if e := waitUntil(ctx, cfg.start()); e != nil {
		return r, e
	}
	runCtx, cancel := context.WithDeadline(ctx, cfg.end())
	defer cancel()
	controlDone := make(chan Control, 1)
	go func() { controlDone <- observeControl(runCtx, control, cfg) }()
	// Join this observer even when bulk I/O fails before the planned boundary.
	joined := false
	defer func() {
		if !joined {
			cancel()
			<-controlDone
		}
	}()
	bulk.SetWriteDeadline(cfg.end())
	p := make([]byte, cfg.PayloadBytes)
	for time.Now().Before(cfg.end()) {
		if e := waitBulkDemand(runCtx, cfg, &r); e != nil {
			return r, e
		}
		encodePayload(p, r.SentMessages)
		before := time.Now()
		e := writeAll(bulk, p)
		r.SendCallNS += time.Since(before).Nanoseconds()
		if e != nil {
			if os.IsTimeout(e) && !time.Now().Before(cfg.end()) {
				break
			}
			return r, e
		}
		r.SentMessages++
	}
	bulk.CloseWrite()
	r.Control = <-controlDone
	joined = true
	control.Close()
	if r.Control.Error != "" {
		r.Errors = append(r.Errors, r.Control.Error)
	}
	return r, nil
}

// Configure every socket before SYN exchange. The small MSS keeps complete
// modeled IP packets within 1460 bytes even with the maximum TCP header.
func cubicSocket(_ string, _ string, raw syscall.RawConn) error {
	var optionErr error
	e := raw.Control(func(fd uintptr) {
		optionErr = unix.SetsockoptString(int(fd), unix.IPPROTO_TCP, unix.TCP_CONGESTION, "cubic")
		if optionErr == nil {
			optionErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_MAXSEG, 1380)
		}
	})
	return errors.Join(e, optionErr)
}
func verifyCubic(c *net.TCPConn) error {
	raw, e := c.SyscallConn()
	if e != nil {
		return e
	}
	var optionErr error
	e = raw.Control(func(fd uintptr) {
		name, err := unix.GetsockoptString(int(fd), unix.IPPROTO_TCP, unix.TCP_CONGESTION)
		if err != nil {
			optionErr = err
			return
		}
		mss, err := unix.GetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_MAXSEG)
		if err != nil {
			optionErr = err
			return
		}
		if name != "cubic" || mss > 1380 || mss < 1 {
			optionErr = fmt.Errorf("unexpected TCP configuration: congestion=%s mss=%d", name, mss)
		}
	})
	return errors.Join(e, optionErr)
}
func executeTCP(cfg Run, role, local, peer, output string) error {
	if cfg.Controller != "cubic" || cfg.Workload != "stream" || cfg.CompletionBytes != 0 {
		return fmt.Errorf("TCP mode requires a sustained CUBIC stream competitor")
	}
	common := cfg
	common.Controller = "reno"
	if e := common.validate(); e != nil {
		return e
	}
	if time.Until(cfg.start()) < 0 || time.Until(cfg.start()) > 2*time.Minute {
		return fmt.Errorf("TCP start must be in the next two minutes")
	}
	address, e := net.ResolveTCPAddr("tcp4", local)
	if e != nil {
		return e
	}
	if address.IP.To4() == nil || address.Port < 1 || address.Port > 65534 {
		return fmt.Errorf("explicit IPv4 address and two adjacent ports required")
	}
	runtime.GOMAXPROCS(4)
	ctx, cancel := context.WithDeadline(context.Background(), cfg.end().Add(4*time.Second))
	defer cancel()
	before := usage()
	var initial runtime.MemStats
	runtime.ReadMemStats(&initial)
	started := time.Now()
	var bulk, control *net.TCPConn
	connections := []**net.TCPConn{&bulk, &control}
	var listeners [2]*net.TCPListener
	if role == "receive" {
		for i := range listeners {
			bind := *address
			bind.Port += i
			lc := net.ListenConfig{Control: cubicSocket}
			ln, err := lc.Listen(ctx, "tcp4", bind.String())
			if err != nil {
				return err
			}
			listeners[i] = ln.(*net.TCPListener)
			defer listeners[i].Close()
			listeners[i].SetDeadline(cfg.end().Add(3 * time.Second))
		}
		fmt.Fprintln(os.Stderr, "TCP competitor ready", local)
	}
	for i, target := range connections {
		bind := *address
		bind.Port += i
		if role == "receive" {
			c, err := listeners[i].AcceptTCP()
			if err != nil {
				return err
			}
			*target = c
		} else {
			remote, err := net.ResolveTCPAddr("tcp4", peer)
			if err != nil {
				return err
			}
			if remote.IP.To4() == nil || remote.Port < 1 || remote.Port > 65534 {
				return fmt.Errorf("explicit peer IPv4 and adjacent ports required")
			}
			remote.Port += i
			dial := net.Dialer{LocalAddr: &bind, Control: cubicSocket}
			c, err := dial.DialContext(ctx, "tcp4", remote.String())
			if err != nil {
				return err
			}
			*target = c.(*net.TCPConn)
		}
		defer (*target).Close()
		if e = verifyCubic(*target); e != nil {
			return e
		}
	}
	setupNS := time.Since(started).Nanoseconds()
	var result Result
	if role == "receive" {
		result, e = receiveTCP(ctx, bulk, control, cfg)
	} else {
		result, e = sendTCP(ctx, bulk, control, cfg)
	}
	after := usage()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	after.CPUSeconds -= before.CPUSeconds
	after.TotalAllocBytes = mem.TotalAlloc - initial.TotalAlloc
	after.HeapBytesAtExit = mem.HeapAlloc
	receipt := map[string]any{"result": result, "source": sourceRevision, "platform": runtime.GOOS + "/" + runtime.GOARCH, "go_version": runtime.Version(), "gomaxprocs": 4, "resources": after, "connection_setup_ns": setupNS, "tcp_controller_verified": "cubic", "tcp_maxseg_bound": 1380, "control_topology": "separate TCP CUBIC connection; common modeled path", "elapsed_ns": time.Since(started).Nanoseconds()}
	receipt["receiver_accounting"] = "receiver-local JSON; require both peers to succeed and join by run identity"
	if e != nil {
		receipt["error"] = e.Error()
	}
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(output, append(data, '\n'), 0600); err != nil {
		return err
	}
	if e != nil {
		return e
	}
	if result.Receiver.Corrupt > 0 || len(result.Errors) > 0 || after.Error != "" {
		return fmt.Errorf("invalid TCP competitor observation")
	}
	return nil
}
