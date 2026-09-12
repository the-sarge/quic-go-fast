//go:build darwin && !ios && !quic_go_no_private_syscalls && d2bench

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
)

func d2benchTLSConfigs(t *testing.T) (server, client *tls.Config) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"d2bench"},
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
	server = &tls.Config{Certificates: []tls.Certificate{cert}, NextProtos: []string{"d2bench"}}
	client = &tls.Config{RootCAs: roots, NextProtos: []string{"d2bench"}, ServerName: "d2bench"}
	return server, client
}

// Measurement harness for the D2 adoption protocol
// (docs/audits/2026-09-12-d2-recvmsgx-protocol.md). A verification aid for
// that finite investigation, excluded from ordinary builds and CI by the
// d2bench build tag; it is not a maintained product deliverable.
//
// One invocation runs one bulk transfer for one protocol cell and prints a
// single JSON line of metrics. The MEASURED endpoint is the client — the
// receiver of the bulk stream — whose per-conn recvmsg_x wrapper counters
// isolate its receive path from the server's. The D1 send-batching
// capability stays engaged in every cell (the D1-era baseline this
// experiment compares against). Cells are selected with D2BENCH_CELL:
//
//	engaged      qualified host, receive-batch capability on
//	disabled     QUIC_GO_DISABLE_RECVMSG_X=1 kill switch
//	unqualified  injected unlisted Darwin kernel major
//
// Usage: go test -c -tags d2bench -o d2bench.test .
//	D2BENCH_CELL=engaged ./d2bench.test -test.run TestD2MeasurementCell -test.v

const d2benchRecordSize = 1071

func TestD2MeasurementCell(t *testing.T) {
	cell := os.Getenv("D2BENCH_CELL")
	totalBytes := 256 << 20
	if v := os.Getenv("D2BENCH_BYTES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("bad D2BENCH_BYTES: %v", err)
		}
		totalBytes = n
	}
	// whole records only
	totalBytes = (totalBytes + d2benchRecordSize - 1) / d2benchRecordSize * d2benchRecordSize

	switch cell {
	case "engaged":
	case "disabled":
		t.Setenv(recvmsgXDisableEnv, "1")
	case "unqualified":
		recvmsgXKernelMajor = func() (int, error) { return -1, nil }
	default:
		t.Fatalf("unknown D2BENCH_CELL %q (want engaged|disabled|unqualified)", cell)
	}
	// The D1-era baseline sends in batches in every cell: the send
	// capability must be engaged and identical across cells so the receive
	// comparison is the only varying factor.
	sendmsgXEnsureQualified()
	if !sendmsgXAvailable() {
		t.Fatal("the D1 send capability must engage on the qualified measurement host")
	}
	recvmsgXEnsureQualified()
	wantAvailable := cell == "engaged"
	if got := recvmsgXAvailable(); got != wantAvailable {
		t.Fatalf("cell %s: recvmsgXAvailable() = %v, want %v", cell, got, wantAvailable)
	}

	serverTLS, clientTLS := d2benchTLSConfigs(t)
	quicConf := &Config{
		InitialStreamReceiveWindow:     4 << 20,
		MaxStreamReceiveWindow:         16 << 20,
		InitialConnectionReceiveWindow: 8 << 20,
		MaxConnectionReceiveWindow:     24 << 20,
	}

	// Server: the bulk sender, on a dual-stack wildcard socket with the D1
	// batch-send path active (the D1-era production shape).
	serverUDP, err := net.ListenUDP("udp", nil)
	if err != nil {
		t.Fatal(err)
	}
	serverRaw, err := newConn(serverUDP, true, true)
	if err != nil {
		t.Fatal(err)
	}
	serverTr := &Transport{Conn: serverRaw, createdConn: true, isSingleUse: true}
	defer serverTr.Close()
	ln, err := serverTr.Listen(serverTLS, quicConf)
	if err != nil {
		t.Fatal(err)
	}
	serverPort := serverUDP.LocalAddr().(*net.UDPAddr).Port

	// Client: the measured receiver, on the same production socket shape
	// (dual-stack wildcard; the server's replies arrive with v4-mapped
	// sources, the representation the receive self-check qualifies).
	clientUDP, err := net.ListenUDP("udp", nil)
	if err != nil {
		t.Fatal(err)
	}
	clientRaw, err := newConn(clientUDP, true, true)
	if err != nil {
		t.Fatal(err)
	}
	wrapper, ok := clientRaw.batchConn.(*recvmsgXConn)
	if !ok {
		t.Fatalf("client batchConn is %T, want *recvmsgXConn", clientRaw.batchConn)
	}
	clientTr := &Transport{Conn: clientRaw, createdConn: true, isSingleUse: true}
	defer clientTr.Close()

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
			batch := make([]byte, 0, 32*d2benchRecordSize)
			record := make([]byte, d2benchRecordSize)
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

	batchReads := wrapper.batchReads.Load()
	batchDatagrams := wrapper.batchDatagrams.Load()
	eagainWaits := wrapper.eagainWaits.Load()
	fallbackReads := wrapper.fallbackReads.Load()
	// Counting method of record (see the protocol): the engaged path counts
	// every visible recvmsg_x invocation including parked EAGAIN attempts;
	// each delegated fallback read is counted as one syscall, a floor,
	// because the runtime poller's internal retries are not visible at this
	// seam. The asymmetry biases against the candidate.
	recvSyscalls := batchReads + eagainWaits + fallbackReads
	datagrams := batchDatagrams + fallbackReads

	if cell != "engaged" && batchReads != 0 {
		t.Fatalf("cell %s: %d batch reads on an inert path", cell, batchReads)
	}
	if cell != "engaged" && fallbackReads == 0 {
		t.Fatalf("cell %s: fallback counters not observed", cell)
	}
	if cell == "engaged" && batchDatagrams == 0 {
		t.Fatal("engaged cell delivered no batched datagrams")
	}

	var ru unix.Rusage
	if err := unix.Getrusage(unix.RUSAGE_SELF, &ru); err != nil {
		t.Fatal(err)
	}

	out := map[string]any{
		"cell":            cell,
		"bytes":           received,
		"elapsed_ns":      elapsed.Nanoseconds(),
		"throughput_mbps": float64(received) / elapsed.Seconds() / 1e6,
		"batch_reads":     batchReads,
		"batch_datagrams": batchDatagrams,
		"eagain_waits":    eagainWaits,
		"fallback_reads":  fallbackReads,
		"recv_syscalls":   recvSyscalls,
		"datagrams":       datagrams,
		"syscalls_per_datagram": func() float64 {
			if datagrams == 0 {
				return 0
			}
			return float64(recvSyscalls) / float64(datagrams)
		}(),
		"datagrams_per_batch_read": func() float64 {
			if batchReads == 0 {
				return 0
			}
			return float64(batchDatagrams) / float64(batchReads)
		}(),
		"batched_fraction": func() float64 {
			if datagrams == 0 {
				return 0
			}
			return float64(batchDatagrams) / float64(datagrams)
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
	fmt.Printf("D2BENCH %s\n", line)
}
