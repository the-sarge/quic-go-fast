//go:build darwin && !ios && !quic_go_no_private_syscalls && d1bench

package quic

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/quic-go/quic-go/internal/protocol"
)

// Measurement harness for the D1 adoption protocol
// (docs/audits/2026-09-12-d1-sendmsgx-protocol.md). A verification aid for
// that finite investigation, excluded from ordinary builds and CI by the
// d1bench build tag; it is not a maintained product deliverable.
//
// One invocation runs one bulk transfer for one protocol cell and prints a
// single JSON line of metrics. Cells are selected with D1BENCH_CELL:
//
//	engaged      qualified host, batch capability on
//	disabled     QUIC_GO_DISABLE_SENDMSG_X=1 kill switch
//	unqualified  injected unlisted Darwin kernel major
//
// Usage: go test -c -tags d1bench -o d1bench.test .
//	D1BENCH_CELL=engaged ./d1bench.test -test.run TestD1MeasurementCell -test.v

// countingSendConn counts send syscalls at the rawConn.WritePacket seam: on
// darwin every WritePacket call is exactly one sendmsg. Batch submissions
// are counted by the engaged counters (one increment per sendmsg_x).
type countingSendConn struct {
	*oobConn
	writePackets int64 // sendQueue.Run goroutine plus handshake writes; read after close
}

func (c *countingSendConn) WritePacket(b []byte, addr net.Addr, oob []byte, gsoSize uint16, ecn protocol.ECN) (int, error) {
	c.writePackets++
	return c.oobConn.WritePacket(b, addr, oob, gsoSize, ecn)
}

// noBatchConn hides the underlying SyscallConn by embedding only the rawConn
// interface, so the client's sconn declines batching and the process-wide
// engaged counters measure the server (the measured sender) alone.
type noBatchConn struct {
	rawConn
	pc net.PacketConn
}

func (c *noBatchConn) ReadFrom(b []byte) (int, net.Addr, error)  { return c.pc.ReadFrom(b) }
func (c *noBatchConn) WriteTo(b []byte, a net.Addr) (int, error) { return c.pc.WriteTo(b, a) }
func (c *noBatchConn) SetDeadline(t time.Time) error             { return c.pc.SetDeadline(t) }
func (c *noBatchConn) SetReadDeadline(t time.Time) error         { return c.pc.SetReadDeadline(t) }
func (c *noBatchConn) SetWriteDeadline(t time.Time) error        { return c.pc.SetWriteDeadline(t) }

func d1benchTLSConfigs(t *testing.T) (server, client *tls.Config) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"d1bench"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	server = &tls.Config{Certificates: []tls.Certificate{cert}, NextProtos: []string{"d1bench"}}
	client = &tls.Config{RootCAs: roots, NextProtos: []string{"d1bench"}, ServerName: "d1bench"}
	return server, client
}

const d1benchRecordSize = 1071

func TestD1MeasurementCell(t *testing.T) {
	cell := os.Getenv("D1BENCH_CELL")
	totalBytes := 256 << 20
	if v := os.Getenv("D1BENCH_BYTES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("bad D1BENCH_BYTES: %v", err)
		}
		totalBytes = n
	}
	// whole records only
	totalBytes = (totalBytes + d1benchRecordSize - 1) / d1benchRecordSize * d1benchRecordSize

	switch cell {
	case "engaged":
	case "disabled":
		t.Setenv(sendmsgXDisableEnv, "1")
	case "unqualified":
		sendmsgXKernelMajor = func() (int, error) { return -1, nil }
	default:
		t.Fatalf("unknown D1BENCH_CELL %q (want engaged|disabled|unqualified)", cell)
	}
	sendmsgXEnsureQualified()
	wantAvailable := cell == "engaged"
	if got := sendmsgXAvailable(); got != wantAvailable {
		t.Fatalf("cell %s: sendmsgXAvailable() = %v, want %v", cell, got, wantAvailable)
	}

	serverTLS, clientTLS := d1benchTLSConfigs(t)
	quicConf := &Config{
		InitialStreamReceiveWindow:     4 << 20,
		MaxStreamReceiveWindow:         16 << 20,
		InitialConnectionReceiveWindow: 8 << 20,
		MaxConnectionReceiveWindow:     24 << 20,
	}

	// Server: the measured sender, on a dual-stack wildcard socket (the
	// production shape whose v4-mapped destination encoding the self-check
	// qualifies), with the WritePacket counting wrapper installed before any
	// traffic flows.
	serverUDP, err := net.ListenUDP("udp", nil)
	if err != nil {
		t.Fatal(err)
	}
	serverRaw, err := newConn(serverUDP, true, true)
	if err != nil {
		t.Fatal(err)
	}
	counting := &countingSendConn{oobConn: serverRaw}
	serverTr := &Transport{Conn: counting, createdConn: true, isSingleUse: true}
	defer serverTr.Close()
	ln, err := serverTr.Listen(serverTLS, quicConf)
	if err != nil {
		t.Fatal(err)
	}
	serverPort := serverUDP.LocalAddr().(*net.UDPAddr).Port

	// Client: loopback udp4, batching structurally declined so the global
	// counters measure the server alone.
	clientUDP, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	clientRaw, err := newConn(clientUDP, true, true)
	if err != nil {
		t.Fatal(err)
	}
	clientTr := &Transport{Conn: &noBatchConn{rawConn: clientRaw, pc: clientUDP}, createdConn: true, isSingleUse: true}
	defer clientTr.Close()

	subsBefore, batchedBefore, fallbackBefore := sendmsgXCountersSnapshot()

	// transfer: server streams fixed-size records to the client
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- func() error {
			conn, err := ln.Accept(context.Background())
			if err != nil {
				return err
			}
			str, err := conn.OpenUniStream()
			if err != nil {
				return err
			}
			// A bulk sender outpaces the packetizer: records are generated
			// in 32-record batches so several packets are queued at once.
			batch := make([]byte, 0, 32*d1benchRecordSize)
			record := make([]byte, d1benchRecordSize)
			for i := range record {
				record[i] = byte(i)
			}
			for sent := 0; sent < totalBytes; {
				batch = batch[:0]
				for len(batch) < cap(batch) && sent < totalBytes {
					batch = append(batch, record...)
					sent += len(record)
				}
				if _, err := str.Write(batch); err != nil {
					return err
				}
			}
			return str.Close()
		}()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	conn, err := clientTr.Dial(ctx, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: serverPort}, clientTLS, quicConf)
	if err != nil {
		t.Fatal(err)
	}
	str, err := conn.AcceptUniStream(ctx)
	if err != nil {
		t.Fatal(err)
	}

	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)
	start := time.Now()
	received := 0
	buf := make([]byte, 64<<10)
	for {
		n, err := str.Read(buf)
		received += n
		if err != nil {
			break
		}
	}
	elapsed := time.Since(start)
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)
	if received != totalBytes {
		t.Fatalf("received %d of %d bytes", received, totalBytes)
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
	conn.CloseWithError(0, "done")

	subsAfter, batchedAfter, fallbackAfter := sendmsgXCountersSnapshot()
	subs := subsAfter - subsBefore
	batched := batchedAfter - batchedBefore
	fallback := fallbackAfter - fallbackBefore
	perPacketWrites := counting.writePackets
	sendSyscalls := int64(subs) + perPacketWrites
	packetsSent := int64(batched) + perPacketWrites

	if cell != "engaged" && subs != 0 {
		t.Fatalf("cell %s: %d batch submissions on an inert path", cell, subs)
	}
	if cell != "engaged" && fallback == 0 {
		t.Fatalf("cell %s: fallback counters not observed", cell)
	}

	var ru unix.Rusage
	if err := unix.Getrusage(unix.RUSAGE_SELF, &ru); err != nil {
		t.Fatal(err)
	}

	out := map[string]any{
		"cell":              cell,
		"bytes":             received,
		"elapsed_ns":        elapsed.Nanoseconds(),
		"throughput_mbps":   float64(received) / elapsed.Seconds() / 1e6,
		"per_packet_writes": perPacketWrites,
		"batch_submissions": subs,
		"batch_packets":     batched,
		"fallback_packets":  fallback,
		"send_syscalls":     sendSyscalls,
		"packets_sent":      packetsSent,
		"syscalls_per_packet": func() float64 {
			if packetsSent == 0 {
				return 0
			}
			return float64(sendSyscalls) / float64(packetsSent)
		}(),
		"packets_per_submission": func() float64 {
			if subs == 0 {
				return 0
			}
			return float64(batched) / float64(subs)
		}(),
		"batched_fraction": func() float64 {
			if packetsSent == 0 {
				return 0
			}
			return float64(batched) / float64(packetsSent)
		}(),
		"total_alloc_bytes": memAfter.TotalAlloc - memBefore.TotalAlloc,
		"mallocs":           memAfter.Mallocs - memBefore.Mallocs,
		"num_gc":            memAfter.NumGC - memBefore.NumGC,
		"heap_inuse_bytes":  memAfter.HeapInuse,
		"max_rss_bytes":     ru.Maxrss, // bytes on darwin
		"capability":        wantAvailable,
	}
	line, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("D1BENCH %s\n", line)
}
