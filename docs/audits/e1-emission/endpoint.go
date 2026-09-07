//go:build ignore

// Disposable E1 measurement endpoint. Build this file explicitly; normal builds
// and test enumeration never start a capture. Only loopback QUIC v1 is supported.
package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	quic "github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/qlogwriter"
)

const datagramSize = 1071
const streamRecordSize = 65536

type manifest struct {
	Mode                         string
	Streams                      int
	Rate                         int64
	WarmupNS, MeasureNS, DrainNS int64
	StartNS                      int64
}

type counters struct {
	offered, admitted, delivered, ingressDrop atomic.Int64
	invalid, duplicate                        atomic.Int64
}

type result struct {
	Role                                                          string
	GSO                                                           bool
	GoVersion                                                     string
	GOMAXPROCS                                                    int
	Offered, Admitted, Delivered, IngressDrop, Invalid, Duplicate int64
	CPUSeconds                                                    float64
	AllocatedBytes                                                uint64
	ProbeNS                                                       []int64 `json:",omitempty"`
	ProbeP50NS, ProbeP99NS                                        int64
	ProbeMissed, ProbeFailures                                    int
	Error                                                         string `json:",omitempty"`
}

var failures = make(chan error, 1)
var qlogEnabled bool

type discardLog struct{}

func (discardLog) Write(b []byte) (int, error) { return len(b), nil }
func (discardLog) Close() error                { return nil }

func report(err error) {
	if err != nil {
		select {
		case failures <- err:
		default:
		}
	}
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}

func waitUntil(t time.Time) error {
	timer := time.NewTimer(time.Until(t))
	defer timer.Stop()
	select {
	case err := <-failures:
		return err
	case <-timer.C:
		return nil
	}
}

func template(n int) []byte {
	b := make([]byte, n)
	for i := 9; i < n; i++ {
		b[i] = byte(i * 31)
	}
	return b
}

func cpu() float64 {
	var r syscall.Rusage
	check(syscall.Getrusage(syscall.RUSAGE_SELF, &r))
	return float64(r.Utime.Sec+r.Stime.Sec) + float64(r.Utime.Usec+r.Stime.Usec)/1e6
}

func sample(start, end time.Time) <-chan result {
	done := make(chan result, 1)
	go func() {
		time.Sleep(time.Until(start))
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		c := cpu()
		time.Sleep(time.Until(end))
		used := cpu() - c
		runtime.ReadMemStats(&after)
		done <- result{CPUSeconds: used, AllocatedBytes: after.TotalAlloc - before.TotalAlloc}
	}()
	return done
}

func tlsConfig(server bool) *tls.Config {
	c := &tls.Config{NextProtos: []string{"qgf-e1-v1"}, MinVersion: tls.VersionTLS13}
	if !server {
		c.InsecureSkipVerify = true
		return c
	} // loopback fixture only
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	check(err)
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour * 24), DNSNames: []string{"localhost"}, KeyUsage: x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	check(err)
	c.Certificates = []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}
	return c
}

func config() *quic.Config {
	c := &quic.Config{Versions: []quic.Version{quic.Version1}, EnableDatagrams: true, InitialPacketSize: 1200, DisablePathMTUDiscovery: true, MaxIdleTimeout: 10 * time.Second, InitialStreamReceiveWindow: 8 << 20, MaxStreamReceiveWindow: 8 << 20, InitialConnectionReceiveWindow: 64 << 20, MaxConnectionReceiveWindow: 64 << 20, MaxIncomingUniStreams: 32}
	if qlogEnabled {
		c.Tracer = func(_ context.Context, client bool, id quic.ConnectionID) qlogwriter.Trace {
			trace := qlogwriter.NewConnectionFileSeq(discardLog{}, client, id, []string{qlog.EventSchema})
			go trace.Run()
			return trace
		}
	}
	return c
}

func phases(m manifest) (time.Time, time.Time, time.Time, time.Time) {
	start := time.Unix(0, m.StartNS)
	measure := start.Add(time.Duration(m.WarmupNS))
	end := measure.Add(time.Duration(m.MeasureNS))
	return start, measure, end, end.Add(time.Duration(m.DrainNS))
}

func packetCount(ns, rate int64) int64 {
	// Divide the subsecond product before adding whole seconds to avoid int64
	// overflow at the fixed 4 Gbps / 60 second workload.
	bits := ns/int64(time.Second)*rate + ns%int64(time.Second)*rate/int64(time.Second)
	return bits / (8 * datagramSize)
}

// An external pacer owns offered load. A bounded queue separates scheduled offers
// from SendDatagram's blocking admission. Overflow is counted, never hidden.
func sendDatagrams(c *quic.Conn, m manifest, stats *counters) {
	start, measure, end, _ := phases(m)
	type offer struct {
		seq   uint64
		phase byte
	}
	queue := make(chan offer, 32)
	done := make(chan struct{})
	go func() {
		defer close(done)
		b := template(datagramSize)
		for item := range queue {
			binary.BigEndian.PutUint64(b, item.seq)
			b[8] = item.phase
			if err := c.SendDatagram(b); err != nil {
				report(err)
				return
			}
			if item.phase == 1 {
				stats.admitted.Add(1)
			}
		}
	}()
	for phase, window := range [][2]time.Time{{start, measure}, {measure, end}} {
		time.Sleep(time.Until(window[0]))
		var offered int64
		total := packetCount(window[1].Sub(window[0]).Nanoseconds(), m.Rate)
		for offered < total {
			now := time.Now()
			due := min(total, packetCount(now.Sub(window[0]).Nanoseconds(), m.Rate))
			for offered < due {
				item := offer{seq: uint64(offered), phase: byte(phase)}
				if phase == 1 {
					stats.offered.Add(1)
				}
				select {
				case queue <- item:
				default:
					if phase == 1 {
						stats.ingressDrop.Add(1)
					}
				}
				offered++
			}
			if offered < total {
				// Yield while ahead of the schedule. A sub-millisecond sleep can
				// become a millisecond burst and saturate this fixture's queue
				// before the transport sees the prescribed offered load.
				runtime.Gosched()
			}
		}
	}
	close(queue)
	<-done
}

func receiveDatagrams(ctx context.Context, c *quic.Conn, m manifest, stats *counters) {
	expected := template(datagramSize)
	maxCount := packetCount(m.MeasureNS, m.Rate)
	seen := make([]uint64, (maxCount+63)/64)
	for {
		b, err := c.ReceiveDatagram(ctx)
		if err != nil {
			if ctx.Err() == nil {
				report(err)
			}
			return
		}
		if len(b) != datagramSize || b[8] > 1 || !bytes.Equal(b[9:], expected[9:]) {
			stats.invalid.Add(1)
			report(fmt.Errorf("invalid DATAGRAM payload"))
			return
		}
		if b[8] == 0 {
			continue
		}
		seq := binary.BigEndian.Uint64(b)
		if seq >= uint64(maxCount) {
			stats.invalid.Add(1)
			report(fmt.Errorf("out of range DATAGRAM sequence"))
			return
		}
		mask := uint64(1) << (seq % 64)
		if seen[seq/64]&mask != 0 {
			stats.duplicate.Add(1)
			report(fmt.Errorf("duplicate DATAGRAM"))
			return
		}
		seen[seq/64] |= mask
		stats.delivered.Add(1)
	}
}

func sendStream(s *quic.SendStream, m manifest, stats *counters) {
	start, measure, end, drainEnd := phases(m)
	b := template(streamRecordSize)
	check(s.SetWriteDeadline(drainEnd))
	time.Sleep(time.Until(start))
	for seq := uint64(0); time.Now().Before(end); seq++ {
		phase := byte(0)
		if !time.Now().Before(measure) {
			phase = 1
			stats.offered.Add(streamRecordSize)
		}
		binary.BigEndian.PutUint64(b, seq)
		b[8] = phase
		n, err := s.Write(b)
		if phase == 1 {
			stats.admitted.Add(int64(n))
		}
		if err != nil {
			report(fmt.Errorf("stream write: %w", err))
			return
		}
	}
	report(s.Close())
}

func receiveStream(s *quic.ReceiveStream, stats *counters) {
	var preamble [1]byte
	if _, err := io.ReadFull(s, preamble[:]); err != nil {
		report(err)
		return
	}
	b, expected := make([]byte, streamRecordSize), template(streamRecordSize)
	for seq := uint64(0); ; seq++ {
		n, err := io.ReadFull(s, b)
		if err == io.EOF && n == 0 {
			return
		}
		if err != nil {
			report(fmt.Errorf("stream record: %w", err))
			return
		}
		if binary.BigEndian.Uint64(b) != seq || b[8] > 1 || !bytes.Equal(b[9:], expected[9:]) {
			stats.invalid.Add(1)
			report(fmt.Errorf("invalid stream record"))
			return
		}
		if b[8] == 1 {
			stats.delivered.Add(streamRecordSize)
		}
	}
}

func probe(s *quic.Stream, start, end time.Time) result {
	r := result{ProbeNS: make([]int64, 0, int(end.Sub(start)/(10*time.Millisecond)))}
	b, reply := template(64), make([]byte, 64)
	for scheduled := start; scheduled.Before(end); scheduled = scheduled.Add(10 * time.Millisecond) {
		time.Sleep(time.Until(scheduled))
		now := time.Now()
		if behind := now.Sub(scheduled) / (10 * time.Millisecond); behind > 0 {
			remaining := (end.Sub(scheduled) + 10*time.Millisecond - 1) / (10 * time.Millisecond)
			missed := min(behind, remaining)
			r.ProbeMissed += int(missed)
			scheduled = scheduled.Add(missed * 10 * time.Millisecond)
			if !scheduled.Before(end) {
				break
			}
		}
		binary.BigEndian.PutUint64(b, uint64(scheduled.UnixNano()))
		check(s.SetDeadline(now.Add(time.Second)))
		_, err := s.Write(b)
		if err == nil {
			_, err = io.ReadFull(s, reply)
		}
		if err == nil && !bytes.Equal(b, reply) {
			err = fmt.Errorf("invalid probe reply")
		}
		if err != nil {
			r.ProbeFailures++
			r.Error = err.Error()
			report(fmt.Errorf("probe: %w", err))
			break
		}
		r.ProbeNS = append(r.ProbeNS, time.Since(now).Nanoseconds())
	}
	return r
}

func server(addr string) {
	ln, err := quic.ListenAddr(addr, tlsConfig(true), config())
	check(err)
	defer ln.Close()
	check(json.NewEncoder(os.Stdout).Encode(map[string]string{"ready": ln.Addr().String()}))
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	c, err := ln.Accept(ctx)
	check(err)
	defer c.CloseWithError(0, "finished")
	control, err := c.AcceptStream(ctx)
	check(err)
	var m manifest
	check(json.NewDecoder(control).Decode(&m))
	_, measure, _, end := phases(m)
	metrics := sample(measure, end)
	stats := new(counters)
	dataCtx, dataCancel := context.WithDeadline(ctx, end)
	defer dataCancel()
	var receivers sync.WaitGroup
	if m.Mode == "datagram" {
		receivers.Go(func() { receiveDatagrams(dataCtx, c, m, stats) })
	} else {
		for range m.Streams {
			s, err := c.AcceptUniStream(ctx)
			check(err)
			receivers.Go(func() { receiveStream(s, stats) })
		}
	}
	echo, err := c.AcceptStream(ctx)
	check(err)
	go func() {
		b := make([]byte, 64)
		for {
			if _, err := io.ReadFull(echo, b); err != nil {
				return
			}
			if _, err := echo.Write(b); err != nil {
				return
			}
		}
	}()
	_, err = control.Write([]byte{1})
	check(err)
	err = waitUntil(end)
	if err != nil {
		c.CloseWithError(1, err.Error())
		panic(err)
	}
	dataCancel()
	receivers.Wait()
	r := <-metrics
	r.Role, r.GSO, r.GoVersion, r.GOMAXPROCS = "server", c.ConnectionState().GSO, runtime.Version(), runtime.GOMAXPROCS(0)
	r.Delivered, r.Invalid, r.Duplicate = stats.delivered.Load(), stats.invalid.Load(), stats.duplicate.Load()
	check(json.NewEncoder(control).Encode(r))
	var ack [1]byte
	_, err = io.ReadFull(control, ack[:])
	check(err)
	_, err = control.Write([]byte{2})
	check(err)
	check(json.NewEncoder(os.Stdout).Encode(r))
	select {
	case <-c.Context().Done():
	case <-ctx.Done():
		panic(ctx.Err())
	}
}

func client(addr string, m manifest) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	check(err)
	defer udp.Close()
	tr := &quic.Transport{Conn: udp}
	defer tr.Close()
	remote, err := net.ResolveUDPAddr("udp4", addr)
	check(err)
	c, err := tr.Dial(ctx, remote, tlsConfig(false), config())
	check(err)
	defer c.CloseWithError(0, "finished")
	control, err := c.OpenStreamSync(ctx)
	check(err)
	m.StartNS = time.Now().Add(time.Second).UnixNano()
	check(json.NewEncoder(control).Encode(m))
	stats := new(counters)
	_, measure, end, drainEnd := phases(m)
	metrics := sample(measure, drainEnd)
	var senders sync.WaitGroup
	if m.Mode == "datagram" {
		senders.Go(func() { sendDatagrams(c, m, stats) })
	} else {
		for range m.Streams {
			s, err := c.OpenUniStreamSync(ctx)
			check(err)
			// A one-byte preamble publishes each stream before the start barrier.
			_, err = s.Write([]byte{0})
			check(err)
			senders.Go(func() { sendStream(s, m, stats) })
		}
	}
	echo, err := c.OpenStreamSync(ctx)
	check(err)
	_, err = echo.Write(make([]byte, 64))
	check(err)
	var greeting [64]byte
	_, err = io.ReadFull(echo, greeting[:])
	check(err)
	var ready [1]byte
	_, err = io.ReadFull(control, ready[:])
	check(err)
	probes := make(chan result, 1)
	go func() { probes <- probe(echo, measure, end) }()
	err = waitUntil(drainEnd)
	if err != nil {
		c.CloseWithError(1, err.Error())
		panic(err)
	}
	senders.Wait()
	r := <-metrics
	p := <-probes
	r.Role, r.GSO, r.GoVersion, r.GOMAXPROCS = "client", c.ConnectionState().GSO, runtime.Version(), runtime.GOMAXPROCS(0)
	r.Offered, r.Admitted, r.IngressDrop = stats.offered.Load(), stats.admitted.Load(), stats.ingressDrop.Load()
	r.ProbeNS, r.ProbeMissed, r.ProbeFailures = p.ProbeNS, p.ProbeMissed, p.ProbeFailures
	if len(r.ProbeNS) > 0 {
		sorted := append([]int64(nil), r.ProbeNS...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
		r.ProbeP50NS = sorted[(len(sorted)-1)*50/100]
		r.ProbeP99NS = sorted[(len(sorted)-1)*99/100]
	}
	var peer result
	check(json.NewDecoder(control).Decode(&peer))
	_, err = control.Write([]byte{1})
	check(err)
	_, err = io.ReadFull(control, ready[:])
	check(err)
	check(json.NewEncoder(os.Stdout).Encode(struct {
		Manifest       manifest
		Client, Server result
	}{m, r, peer}))
}

func main() {
	defer func() {
		if err := recover(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}()
	role := flag.String("role", "", "server or client")
	addr := flag.String("addr", "127.0.0.1:0", "loopback address")
	mode := flag.String("mode", "datagram", "datagram or stream")
	streams := flag.Int("streams", 1, "continuous streams")
	rate := flag.Int64("rate", 1_000_000_000, "offered application bits/s")
	measure := flag.Duration("measure", 60*time.Second, "measurement duration")
	warmup := flag.Duration("warmup", 2*time.Second, "warmup duration")
	drain := flag.Duration("drain", time.Second, "drain duration")
	flag.BoolVar(&qlogEnabled, "qlog", false, "diagnostic qlog serialization to discard sink")
	flag.Parse()
	if *role == "server" {
		server(*addr)
		return
	}
	if *role != "client" || (*mode != "datagram" && *mode != "stream") {
		panic("explicit supported role and mode required")
	}
	client(*addr, manifest{Mode: *mode, Streams: *streams, Rate: *rate, WarmupNS: int64(*warmup), MeasureNS: int64(*measure), DrainNS: int64(*drain)})
}
